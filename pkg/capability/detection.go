// Package capability provides hardware capability detection for CloudAI Fusion.
package capability

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ============================================================================
// HARDWARE CAPABILITY DETECTION - COMPLEMENTARY TO THE RUN-MODE REGISTRY
// ============================================================================

// SGXCapability describes SGX enclave availability
type SGXCapability struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`                  // "2.0", "1.x"
	EPCSize   int64  `json:"epc_size_bytes,omitempty"` // Enclave Page Cache size
}

// GPUCapability describes GPU presence
type GPUCapability struct {
	Available     bool   `json:"available"`
	Model         string `json:"model,omitempty"`
	VRAMMB        uint64 `json:"vram_mb,omitempty"`
	ComputeCap    string `json:"compute_cap,omitempty"` // "8.0", "9.0"
	MIGSupported  bool   `json:"mig_supported,omitempty"`
	NvidiaPresent bool   `json:"nvidia_present"`
}

// EBPFCapability describes eBPF kernel support
type EBPFCapability struct {
	Available     bool   `json:"available"`
	KernelVersion string `json:"kernel_version,omitempty"`
	MapSupport    bool   `json:"map_support"` // BPF_MAP_TYPE_*
	SupportLevel  int    `json:"support_level"` // 1=minimal, 2=full, 3=advanced
}

// FeatureFlags captures all detected hardware features in a unified structure
type FeatureFlags struct {
	SGX        SGXCapability  `json:"sgx"`
	GPU        GPUCapability  `json:"gpu"`
	EBPF       EBPFCapability `json:"ebpf"`
	Hypervisor string         `json:"hypervisor,omitempty"` // "azure", "aws", "gcp", ""
	Committed  bool           `json:"committed"`            // Whether a full scan ran
}

// Detector runs hardware capability discovery
type Detector struct {
	flags FeatureFlags
	log   func(format string, args ...interface{})
}

// NewDetector creates a new hardware detector
func NewDetector() *Detector {
	return &Detector{
		log: func(format string, args ...interface{}) {},
	}
}

// SetLogger sets custom logging function
func (d *Detector) SetLogger(log func(format string, args ...interface{})) {
	d.log = log
}

// DetectSGX checks whether SGX is available on this host
func (d *Detector) DetectSGX() SGXCapability {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sgx := SGXCapability{Available: false}

	if runtime.GOOS != "linux" {
		d.log("SGX detection skipped: not Linux")
		return sgx
	}

	// Check /dev/sgx_enclave exists
	if _, err := os.Stat("/dev/sgx_enclave"); err != nil {
		d.log("SGX not found: missing /dev/sgx_enclave")
		return sgx
	}

	output, _ := exec.CommandContext(ctx, "lsmod").Output()
	if strings.Contains(string(output), "intel_sgx") || strings.Contains(string(output), "sgx_driver") {
		sgx.Version = "2.0"
		if epcBytes, err := os.ReadFile("/sys/fs/sgx/enclaves"); err == nil {
			sgx.EPCSize = parseBytes(epcBytes)
		}
		sgx.Available = true
	} else if ver, err := os.ReadFile("/proc/cpuinfo"); err == nil && bytes.Contains(ver, []byte("microcode")) {
		sgx.Version = "1.x"
	}

	return sgx
}

// DetectGPU checks for NVIDIA GPUs with CUDA drivers
func (d *Detector) DetectGPU() GPUCapability {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gpu := GPUCapability{}

	nvidiaBinary, err := exec.LookPath("nvidia-smi")
	if err != nil {
		d.log("No nvidia-smi found, skipping GPU detection")
		return gpu
	}
	gpu.NvidiaPresent = true

	output, err := exec.CommandContext(ctx, nvidiaBinary, "-L").Output()
	if err != nil {
		d.log("Failed to query GPU list: %v", err)
		return gpu
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return gpu
	}

	gpu.Available = true
	gpu.Model = strings.TrimSpace(lines[0])

	// Query VRAM
	memOut, _ := exec.CommandContext(ctx, nvidiaBinary,
		"--query-gpu=memory.total", "--format=csv,noheader,nounits").Output()
	gpu.VRAMMB = parseMegaBytes(strings.TrimSpace(string(memOut)))

	// Check MIG support
	migOut, _ := exec.CommandContext(ctx, nvidiaBinary, "-q", "-d", "MIG").Output()
	gpu.MIGSupported = strings.Contains(string(migOut), "enabled") ||
		strings.Contains(string(migOut), "Enabled")

	return gpu
}

// DetectEBPF checks kernel-level eBPF support
func (d *Detector) DetectEBPF() EBPFCapability {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ebpf := EBPFCapability{}

	if runtime.GOOS != "linux" {
		return ebpf
	}

	// /sys/kernel/btf presence indicates BTF enabled
	if _, err := os.Stat("/sys/kernel/btf"); err == nil {
		ebpf.Available = true
		ebpf.MapSupport = true
		ebpf.SupportLevel = 2
	}

	// Kernel version
	if out, err := exec.CommandContext(ctx, "uname", "-r").Output(); err == nil {
		ebpf.KernelVersion = strings.TrimSpace(string(out))
	}

	// bpftool availability implies advanced support
	if _, err := exec.CommandContext(ctx, "bpftool", "prog", "list").Output(); err == nil {
		ebpf.Available = true
		ebpf.SupportLevel = 3
	}

	return ebpf
}

// DetectHypervisor identifies if running in a known cloud VM
func (d *Detector) DetectHypervisor() string {
	productName, _ := os.ReadFile("/sys/class/dmi/id/product_name")
	name := string(productName)

	switch {
	case strings.Contains(name, "Amazon EC2"):
		return "aws"
	case strings.Contains(name, "Google"):
		return "gcp"
	case strings.Contains(name, "Virtual Machine"):
		return "azure"
	default:
		return ""
	}
}

// DetectAll performs a full hardware capability scan
func (d *Detector) DetectAll(ctx context.Context) FeatureFlags {
	d.flags.SGX = d.DetectSGX()
	d.flags.GPU = d.DetectGPU()
	d.flags.EBPF = d.DetectEBPF()
	d.flags.Hypervisor = d.DetectHypervisor()
	d.flags.Committed = true
	return d.flags
}

// GracefulDegradation returns a policy describing which features must degrade
// given the detected capabilities. The map values describe the fallback taken.
func (d *Detector) GracefulDegradation() map[string]string {
	policy := make(map[string]string)

	if !d.flags.SGX.Available {
		policy["tee"] = "software-attestation-fallback"
	}
	if !d.flags.GPU.Available {
		policy["gpu-scheduling"] = "cpu-only-mode"
	}
	if !d.flags.EBPF.Available {
		policy["ebpf-observability"] = "userspace-metrics-fallback"
	}

	return policy
}

// Flags returns the last-detected feature flags snapshot
func (d *Detector) Flags() FeatureFlags {
	return d.flags
}

// parseBytes parses a byte-count from raw file bytes
func parseBytes(b []byte) int64 {
	var n int64
	for _, c := range bytes.TrimSpace(b) {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// parseMegaBytes parses a decimal megabyte value from a string
func parseMegaBytes(s string) uint64 {
	var n uint64
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}
