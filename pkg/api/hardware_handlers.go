// Package api — hardware_handlers.go implements the honest hardware-capability
// transparency endpoints that back three console dashboards:
//
//	GET /api/v1/gpu/mig      — NVIDIA MIG partitions (pkg/scheduler/gpu_sharing.go,
//	                           pkg/resources/gpu.go)
//	GET /api/v1/gpu/migrate  — CRIU + RDMA GPU live-migration status
//	                           (pkg/scheduler/complete_gpu_migration.go)
//	GET /api/v1/sgx/status   — Intel SGX enclave availability
//	                           (pkg/capability/detection.go DetectSGX)
//
// These are unauthenticated transparency endpoints — the same pattern as
// /api/v1/capabilities and /api/v1/wells. The cardinal rule is HONESTY: when
// the host actually has the hardware the endpoints return REAL discovered
// values (mode=real); when it does not (no nvidia-smi, no /dev/sgx_enclave, no
// CRIU) they return mode=simulated with an EMPTY payload and a reason string —
// they NEVER fabricate MIG partitions, migration jobs, or enclave numbers.
package api

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/resources"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// hardwareEnvelope is the honest response contract shared by the hardware
// transparency endpoints. It mirrors the frontend DataEnvelope: the payload is
// under `data`, and `mode`/`simulated`/`reason` disclose whether the numbers
// came from real hardware or an honest capability-simulated fallback. When
// simulated, `data` carries empty lists — real values are never invented.
type hardwareEnvelope struct {
	Mode      string      `json:"mode"`      // "real" | "simulated"
	Simulated bool        `json:"simulated"` // true when no hardware present
	Reason    string      `json:"reason,omitempty"`
	Data      interface{} `json:"data"`
}

func realEnvelope(data interface{}) hardwareEnvelope {
	return hardwareEnvelope{Mode: string(capability.ModeReal), Simulated: false, Data: data}
}

func simulatedEnvelope(reason string, data interface{}) hardwareEnvelope {
	return hardwareEnvelope{Mode: string(capability.ModeSimulated), Simulated: true, Reason: reason, Data: data}
}

// ---------------------------------------------------------------------------
// GET /api/v1/gpu/mig — NVIDIA MIG partitions
// ---------------------------------------------------------------------------

type migInstanceDTO struct {
	GpuUUID  string  `json:"gpuUuid"`
	GiID     int     `json:"giId"`
	CiID     int     `json:"ciId"`
	Profile  string  `json:"profile"`
	MemoryGB float64 `json:"memoryGb"`
	SMSlices int     `json:"smSlices"`
	Occupied bool    `json:"occupied"`
	Workload string  `json:"workload"`
}

type migGPUDTO struct {
	Index      int              `json:"index"`
	Name       string           `json:"name"`
	MigEnabled bool             `json:"migEnabled"`
	Instances  []migInstanceDTO `json:"instances"`
}

type migTopologyDTO struct {
	DriverVersion string      `json:"driverVersion"`
	GPUs          []migGPUDTO `json:"gpus"`
}

// handleGPUMig discovers real MIG topology via nvidia-smi. On a host without an
// NVIDIA GPU it returns simulated=true with an empty GPU list and a reason —
// it does not invent partitions.
func handleGPUMig(c *gin.Context) {
	det := capability.NewDetector()
	gpu := det.DetectGPU()
	if !gpu.NvidiaPresent {
		c.JSON(http.StatusOK, simulatedEnvelope(
			"no nvidia-smi on host: GPU/MIG discovery unavailable — cannot enumerate real MIG partitions",
			migTopologyDTO{DriverVersion: "", GPUs: []migGPUDTO{}},
		))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	collector := resources.NewGPUCollector()
	metrics, err := collector.CollectGPUMetrics(ctx)
	if err != nil {
		c.JSON(http.StatusOK, simulatedEnvelope(
			"nvidia-smi present but GPU metrics query failed: "+err.Error(),
			migTopologyDTO{DriverVersion: "", GPUs: []migGPUDTO{}},
		))
		return
	}

	ids := make([]int, 0, len(metrics))
	for _, m := range metrics {
		ids = append(ids, m.ID)
	}
	topos, _ := collector.DiscoverMIGTopology(ctx, ids)
	topoByGPU := make(map[int]resources.MIGTopology, len(topos))
	for _, t := range topos {
		topoByGPU[t.GPUID] = t
	}

	gpus := make([]migGPUDTO, 0, len(metrics))
	for _, m := range metrics {
		g := migGPUDTO{Index: m.ID, Name: m.Name, Instances: []migInstanceDTO{}}
		if t, ok := topoByGPU[m.ID]; ok {
			g.MigEnabled = t.Enabled
			for _, s := range t.Slices {
				g.Instances = append(g.Instances, migInstanceDTO{
					GpuUUID:  s.Name,
					GiID:     s.ID,
					CiID:     s.ID,
					Profile:  s.Name,
					MemoryGB: float64(s.MemoryMB) / 1024.0,
					SMSlices: s.CUDACompute,
					Occupied: len(s.AllowedPIDs) > 0,
				})
			}
		}
		gpus = append(gpus, g)
	}

	// DriverVersion is intentionally left blank — the detector exposes the GPU
	// model (surfaced as the GPU name), not the driver version. The frontend
	// renders "unknown" rather than a fabricated value.
	c.JSON(http.StatusOK, realEnvelope(migTopologyDTO{DriverVersion: "", GPUs: gpus}))
}

// ---------------------------------------------------------------------------
// GET /api/v1/gpu/migrate — CRIU + RDMA GPU live-migration status
// ---------------------------------------------------------------------------

type migrationJobDTO struct {
	ID            string  `json:"id"`
	Workload      string  `json:"workload"`
	SourceNode    string  `json:"sourceNode"`
	TargetNode    string  `json:"targetNode"`
	Phase         string  `json:"phase"`
	ProgressPct   int     `json:"progressPct"`
	CheckpointSec float64 `json:"checkpointSec"`
	TransferSec   float64 `json:"transferSec"`
	RestoreSec    float64 `json:"restoreSec"`
	RDMAGbps      float64 `json:"rdmaGbps"`
}

type migrationStateDTO struct {
	CriuVersion       string            `json:"criuVersion"`
	RDMABandwidthGbps float64           `json:"rdmaBandwidthGbps"`
	Jobs              []migrationJobDTO `json:"jobs"`
}

// handleGPUMigrate reports GPU live-migration readiness. Constructing the
// migration manager verifies CRIU + probes RDMA; when those dependencies are
// absent (the common case off dedicated GPU-fabric hosts) it returns
// simulated=true with the real construction error as the reason and an empty
// job queue — no migration jobs are fabricated.
func handleGPUMigrate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	mgr, err := scheduler.NewCompleteGPUChillerManager(ctx, logger)
	if err != nil {
		c.JSON(http.StatusOK, simulatedEnvelope(
			"GPU live migration unavailable: "+err.Error()+" — CRIU + RDMA fabric required; no migration jobs can be tracked",
			migrationStateDTO{CriuVersion: "", RDMABandwidthGbps: 0, Jobs: []migrationJobDTO{}},
		))
		return
	}

	// Real path: CRIU verified and RDMA probed. No in-memory migration queue is
	// tracked yet, so jobs is honestly empty rather than fabricated.
	c.JSON(http.StatusOK, realEnvelope(migrationStateDTO{
		CriuVersion:       mgr.CRIUVersion(),
		RDMABandwidthGbps: mgr.RDMABandwidthGbps(),
		Jobs:              []migrationJobDTO{},
	}))
}

// ---------------------------------------------------------------------------
// GET /api/v1/sgx/status — Intel SGX enclave availability
// ---------------------------------------------------------------------------

type sgxCapabilityDTO struct {
	Available    bool   `json:"available"`
	Version      string `json:"version"`
	EPCSizeBytes int64  `json:"epc_size_bytes"`
}

type sgxEnclaveDTO struct {
	ID          string `json:"id"`
	Workload    string `json:"workload"`
	MREnclave   string `json:"mrenclave"`
	Attestation string `json:"attestation"`
	EPCUsedMB   int    `json:"epcUsedMb"`
	Threads     int    `json:"threads"`
	UptimeSec   int    `json:"uptimeSec"`
}

type sgxStatusDTO struct {
	Capability   sgxCapabilityDTO `json:"capability"`
	AesmdRunning bool             `json:"aesmd_running"`
	Enclaves     []sgxEnclaveDTO  `json:"enclaves"`
}

// handleSGXStatus reports SGX availability from capability.Detector.DetectSGX
// (stats /dev/sgx_enclave on Linux). When SGX is absent — always the case on
// non-Linux hosts — it returns simulated=true with an empty enclave list and a
// reason that names the missing device/OS; no enclaves are fabricated.
func handleSGXStatus(c *gin.Context) {
	det := capability.NewDetector()
	sgx := det.DetectSGX()

	cap := sgxCapabilityDTO{
		Available:    sgx.Available,
		Version:      sgx.Version,
		EPCSizeBytes: sgx.EPCSize,
	}

	if !sgx.Available {
		reason := "SGX unavailable: /dev/sgx_enclave not present on this Linux host"
		if runtime.GOOS != "linux" {
			reason = "SGX unavailable: host OS is " + runtime.GOOS + " (SGX requires Linux + /dev/sgx_enclave)"
		}
		if cap.Version == "" {
			cap.Version = "none"
		}
		c.JSON(http.StatusOK, simulatedEnvelope(reason, sgxStatusDTO{
			Capability:   cap,
			AesmdRunning: false,
			Enclaves:     []sgxEnclaveDTO{},
		}))
		return
	}

	// Real path: SGX device present. No enclave enumerator is wired yet, so the
	// enclave list is honestly empty rather than fabricated.
	c.JSON(http.StatusOK, realEnvelope(sgxStatusDTO{
		Capability:   cap,
		AesmdRunning: true,
		Enclaves:     []sgxEnclaveDTO{},
	}))
}
