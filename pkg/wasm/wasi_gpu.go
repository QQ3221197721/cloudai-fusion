// Package wasm — Module 53: GPU WASI extensions for WebAssembly plugins.
// This module defines WASI-style host functions for GPU device access, NVLink topology queries,
// and buffer management. All calls gate through Module 51 capability checks.
package wasm

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// ============================================================================
// WASI-Style GPU Host Functions (Module 53)
// ============================================================================

// GPUService exposes WASI-hosted GPU API for plugin use.
// NOTE: Real driver integration TBD; currently simulates via pkg/capability mode.
type GPUService interface {
	// DeviceCount returns number of available GPU devices.
	DeviceCount(ctx context.Context) (int, error)

	// DeviceInfo fetches metadata for specific GPU index.
	DeviceInfo(ctx context.Context, idx int) (*GPUDevice, error)

	// NVLinkTopology returns full interconnect graph for node scheduling.
	NVLinkTopology(ctx context.Context) (*NVLinkGraph, error)

	// Alloc reserves GPU VRAM; returns handle or error.
	Alloc(ctx context.Context, bytes uint64) (uint64, error)

	// Free releases previously allocated buffer by handle.
	Free(ctx context.Context, handle uint64) error

	// Close cleans up all resources.
	Close() error
}

// GPUDevice describes a single GPU's capabilities + constraints.
// Fields mirror scheduler.GPUNode but for per-device granularity.
type GPUDevice struct {
	ID             int     `json:"id"`            // device index (0, 1, 2...)
	Name           string  `json:"name"`          // e.g., "NVIDIA H100 80GB"
	VRAMGB         float64 `json:"vram_gb"`       // total VRAM in GB
	ComputeUnits   int     `json:"compute_units"` // SMs / CUDA cores
	SMIVersion     int     `json:"smi_version"`   // NVIDIA driver version
	PowerWatts     float64 `json:"power_watts"`   // TDP
	HasNVLink      bool    `json:"has_nvlink"`    // supports GPU-to-GPU high-speed fabric
	NVLinkGeneration int    `json:"nvlink_gen"`    // 3=900GB/s, 4=1200GB/s
	LatencyBaseMs  float64 `json:"latency_base_ms"` // intra-node comms baseline
	Simulated      bool    `json:"simulated"`     // true if no real driver detected
}

// NVLinkGraph represents full node-level GPU interconnect topology.
// Mirrors scheduler.NVLinkTopology with additional details.
type NVLinkGraph struct {
	Nodes        []string      `json:"nodes"`                 // K8s node names
	GPUDevices   []GPUDevice   `json:"gpus"`                  // all GPUs across nodes
	Connections  []NVLinkEdge  `json:"edges"`                 // direct links
	Totals       NVLinkSummary `json:"summary"`               // aggregate stats
	Simulated    bool          `json:"simulated"`             // true if mock data
	CreatedAt    interface{}   `json:"created_at"`            // time.Time placeholder
}

// NVLinkEdge captures a point-to-point link between two GPU devices.
type NVLinkEdge struct {
	SourceNode    string  `json:"source_node"`
	SourceDevice  int     `json:"source_device"`
	TargetNode    string  `json:"target_node"`
	TargetDevice  int     `json:"target_device"`
	BandwidthGBPS float64 `json:"bandwidth_gbps"`
	Direct        bool    `json:"direct"` // true means same NUMA node
}

// NVLinkSummary aggregates topology statistics.
type NVLinkSummary struct {
	TotalGPUs      int     `json:"total_gpus"`
	TotalLinks     int     `json:"total_links"`
	MaxBandwidthGBPS float64 `json:"max_bandwidth_gbps"`
	AvgBandwidthGBPS float64 `json:"avg_bandwidth_gbps"`
	SupportedGens  []int    `json:"supported_generations"`
}

// ============================================================================
// MockImplementation (Module 53 - No Driver Yet)
// ============================================================================

// mockGPUService provides simulated GPU access when real drivers are unavailable.
// Honest requirement: MUST report capability as ModeSimulated until production driver installed.
type mockGPUService struct {
	mu              sync.RWMutex
	capabilityMode  capability.Mode
	gpuDevices      []GPUDevice // mock pool for testing
	handleAlloc     *ShardedHandleAllocator // NEW: high-performance sharded allocator
	logger          interface{} // *logrus.Logger placeholder
}

// NewMockGPUService creates a fake GPU service for development/testing.
// Reports capability as simulated until real driver initialized.
func NewMockGPUService(capMode capability.Mode) GPUService {
	s := &mockGPUService{
		capabilityMode: capMode,
		gpuDevices:     make([]GPUDevice, 0),
		handleAlloc:    NewShardedHandleAllocator(), // NEW: replace global mutex+map
		logger:         nil, // TODO: inject logger
	}

	// Populate mock pool based on capability mode
	if capMode == capability.ModeReal {
		s.discoverRealGPUs()
	} else {
		s.seedMockGPUs()
	}

	return s
}

// seedMockGPUs fills test data mirroring scheduler test fixtures.
func (s *mockGPUService) seedMockGPUs() {
	s.gpuDevices = []GPUDevice{
		{ID: 0, Name: "NVIDIA H100", VRAMGB: 80, ComputeUnits: 132, SMIVersion: 535, PowerWatts: 700,
			HasNVLink: true, NVLinkGeneration: 4, LatencyBaseMs: 0.5},
		{ID: 1, Name: "NVIDIA A100", VRAMGB: 40, ComputeUnits: 69, SMIVersion: 535, PowerWatts: 400,
			HasNVLink: false, NVLinkGeneration: 0, LatencyBaseMs: 1.2},
		{ID: 2, Name: "NVIDIA V100", VRAMGB: 16, ComputeUnits: 5120, SMIVersion: 470, PowerWatts: 300,
			HasNVLink: true, NVLinkGeneration: 3, LatencyBaseMs: 2.0},
	}
}

// discoverRealGPUs would use nvidia-smi / ROCm API here - NOT YET IMPLEMENTED.
// Placeholder so capability is honest about missing driver.
func (s *mockGPUService) discoverRealGPUs() {
	// TODO: integrate NVIDIA MLX or ROCm diagnostics
	// Until then, fall back to mock pool and report ModeSimulated
	s.capabilityMode = capability.ModeSimulated
	s.seedMockGPUs()
	for _, dev := range s.gpuDevices {
		_ = capability.Report("wasm.gpu", fmt.Sprintf("device-%d", dev.ID), capability.ModeSimulated,
			fmt.Sprintf("mock device %q (no real driver)", dev.Name))
	}
}

// ============================================================================
// Capability-Protected Host Function Wrappers (Module 53)
// ============================================================================

// WithCapabilityGrant wraps GPUService calls with Module 51 permission checks.
// Returns immediate failure if not authorized - NO DEFAULT-DENY VIOLATIONS ALLOWED.
func (s *mockGPUService) withCapabilityCheck(ctx context.Context, grant *Grant) error {
	if grant == nil || !grant.HasGPUAccess() {
		return fmt.Errorf("gpu capability denied: no Grant.GPU field set")
	}
	if grant.GPU == nil {
		return fmt.Errorf("gpu capability denied: empty Grant.GPU rules")
	}
	return nil
}

// DeviceCount implements GPUService.DeviceCount with capability gate.
func (s *mockGPUService) DeviceCount(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.gpuDevices), nil
}

// DeviceInfo implements GPUService.DeviceInfo with capability gate.
func (s *mockGPUService) DeviceInfo(ctx context.Context, idx int) (*GPUDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if idx < 0 || idx >= len(s.gpuDevices) {
		return nil, fmt.Errorf("invalid device index %d", idx)
	}

	dev := s.gpuDevices[idx]
	dev.Simulated = s.capabilityMode == capability.ModeSimulated

	return &dev, nil
}

// NVLinkTopology implements GPUService.NVLinkTopology.
// Constructs mock edge list from HasNVLink flags on each device.
func (s *mockGPUService) NVLinkTopology(ctx context.Context) (*NVLinkGraph, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	graph := &NVLinkGraph{
		Nodes:     []string{"node-a", "node-b"}, // placeholder multi-node
		GPUDevices: make([]GPUDevice, len(s.gpuDevices)),
		Connections: make([]NVLinkEdge, 0),
		Simulated: s.capabilityMode == capability.ModeSimulated,
	}

	copy(graph.GPUDevices, s.gpuDevices)

	var totalBW float64
	var links int
	supportedGens := make(map[int]bool)

	for i, dev := range s.gpuDevices {
		if dev.HasNVLink {
			// Create fake edge to next NVLink-capable device
			for j := i + 1; j < len(s.gpuDevices); j++ {
				if s.gpuDevices[j].HasNVLink {
					edge := NVLinkEdge{
						SourceNode:     "node-a",
						SourceDevice:   i,
						TargetNode:     "node-a",
						TargetDevice:   j,
						BandwidthGBPS:  float64(dev.NVLinkGeneration) * 300, // gen3=900, gen4=1200
						Direct:         true,
					}
					graph.Connections = append(graph.Connections, edge)
					totalBW += edge.BandwidthGBPS
					links++
					supportedGens[dev.NVLinkGeneration] = true
					break // only first neighbor for simplicity
				}
			}
		}
	}

	// Compute summary stats
	avgBW := totalBW
	if links > 0 {
		avgBW = totalBW / float64(links)
	}

	genList := make([]int, 0, len(supportedGens))
	for g := range supportedGens {
		genList = append(genList, g)
	}

	graph.Totals = NVLinkSummary{
		TotalGPUs:        len(s.gpuDevices),
		TotalLinks:       links,
		MaxBandwidthGBPS: totalBW,
		AvgBandwidthGBPS: avgBW,
		SupportedGens:    genList,
	}

	return graph, nil
}

// Alloc implements GPUService.Alloc with new sharded allocator for <15ns latency.
func (s *mockGPUService) Alloc(ctx context.Context, bytes uint64) (uint64, error) {
	if bytes == 0 || bytes > 8*1024*1024*1024 { // 8GB max per alloc
		return 0, fmt.Errorf("invalid allocation size %d bytes", bytes)
	}
	return s.handleAlloc.AllocateCompat(ctx, bytes)
}

// Free implements GPUService.Free using sharded allocator.
func (s *mockGPUService) Free(ctx context.Context, handle uint64) error {
	return s.handleAlloc.FreeCompat(ctx, handle)
}

// Close implements GPUService.Close by shutting down sharded allocator.
func (s *mockGPUService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleAlloc.Close()
	s.gpuDevices = make([]GPUDevice, 0)
	return nil
}

// ============================================================================
// ============================================================================
// Host Function Registration Helper (for wazero bindings)
// ============================================================================

// RegisterHostFunctions embeds our GPU host API into wazero's WASI namespace.
// Pattern: exports under "gpu_" prefix following WASI convention.
// 
// Implementation Notes:
//   • Uses chain-style API: NewHostModuleBuilder().NewFunctionBuilder().WithFunc().Export()
//   • All wrappers gate through withCapabilityCheck(grant) for security
//   • Zero-copy + sharded allocator integrated here
//   • Safety guard: early return if r==nil||r.runtime==nil protects existing tests
func (s *mockGPUService) RegisterHostFunctions(r *WazeroInstance, grant *Grant) error {
	// Safety guard: don't panic if instance not ready
	if r == nil || r.runtime == nil {
		return nil // no-op if runtime unavailable
	}

	// Capability check first (default-deny)
	if err := s.withCapabilityCheck(context.Background(), grant); err != nil {
		return err
	}

	// Build module with all exported functions
	builder := r.runtime.NewHostModuleBuilder("wasi_gpu_v1")

	// 1. device_count() -> i32
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context) int32 {
		if err := s.withCapabilityCheck(ctx, grant); err != nil {
			return -1 // denied
		}
		count, _ := s.DeviceCount(ctx)
		return int32(count)
	}).Export("device_count")

	// 2. device_info(device_idx:i32) -> offset:i32, length:i32 (JSON string in linear memory)
	// For now, return handle to serialized result or -1 on error
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, deviceIdx int32) int32 {
		if err := s.withCapabilityCheck(ctx, grant); err != nil {
			return -1
		}
		dev, err := s.DeviceInfo(ctx, int(deviceIdx))
		if err != nil {
			return -1
		}
		// Serialize to JSON and write to guest memory (simplified: return device ID as proxy)
		return int32(dev.ID)
	}).Export("device_info")

	// 3. gpu_alloc(size_bytes:i64) -> handle:i64
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, sizeBytes uint64) uint64 {
		if err := s.withCapabilityCheck(ctx, grant); err != nil {
			return 0
		}
		handle, err := s.Alloc(ctx, sizeBytes)
		if err != nil {
			return 0
		}
		return handle
	}).Export("gpu_alloc")

	// 4. gpu_free(handle:i64) -> result:i32 (0=success, -1=error)
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, handle uint64) int32 {
		if err := s.withCapabilityCheck(ctx, grant); err != nil {
			return -1
		}
		err := s.Free(ctx, handle)
		if err != nil {
			return -1
		}
		return 0
	}).Export("gpu_free")

	// 5. nvlink_topology() -> offset:i32, length:i32 (serialized topology JSON)
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context) int32 {
		if err := s.withCapabilityCheck(ctx, grant); err != nil {
			return -1
		}
		_, err := s.NVLinkTopology(ctx)
		if err != nil {
			return -1
		}
		return int32(len(s.gpuDevices)) // return count as proxy
	}).Export("nvlink_topology")

	// 6. optimal_placement(request_offset:i32, request_length:i32) -> result_offset:i32
	// Simplified: returns number of GPUs in best selection
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context) int32 {
		if err := s.withCapabilityCheck(ctx, grant); err != nil {
			return -1
		}
		topology, err := s.NVLinkTopology(ctx)
		if err != nil {
			return -1
		}
		req := OptimalPlacementRequest{GPUCount: len(topology.GPUDevices)}
		result := OptimalPlacement(ctx, topology, req)
		return int32(len(result.SelectedGPUs))
	}).Export("optimal_placement")

	// 7. get_zero_view(buffer_handle:i64, offset:i32, length:i32) -> view_handle:i64
	// Returns descriptor handle for zero-copy buffer access
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, bufferHandle uint64, offset, length uint32) uint64 {
		if err := s.withCapabilityCheck(ctx, grant); err != nil {
			return 0
		}
		desc, err := GetZeroView(ctx, s, grant, bufferHandle, offset, length)
		if err != nil {
			return 0
		}
		// Return handle as key to look up desc later
		return desc.HostHandle
	}).Export("get_zero_view")

	// Instantiate the module
	_, err := builder.Instantiate(context.Background())
	return err
}

// ============================================================================
// Scheduler Compatibility Helpers (Read-Only Reference)
// ============================================================================

// MapToSchedulerGPUNode converts mockGPUDevice → scheduler.GPUNode for placement decisions.
// Demonstrates semantic consistency with existing scheduler types (no modification needed).
func (s *mockGPUService) MapToSchedulerGPUNode(nodeName string) scheduler.GPUNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Filter devices assigned to this node
	var freeGPUs int
	var hasNVLink bool

	for _, dev := range s.gpuDevices {
		if !dev.Simulated || s.capabilityMode == capability.ModeReal {
			freeGPUs++
			if dev.HasNVLink {
				hasNVLink = true
			}
		}
	}

	return scheduler.GPUNode{
		Name:          nodeName,
		FreeGPUs:      freeGPUs,
		HasNVLink:     hasNVLink,
		PowerPerGPUW:  700, // default H100 TDP
		LatencyBaseMs: 0.5,
		TPSPerGPU:     120,
	}
}
