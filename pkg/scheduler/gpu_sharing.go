// Package scheduler - gpu_sharing.go implements NVIDIA MPS and MIG GPU sharing.
// MPS (Multi-Process Service): Allows multiple CUDA processes to share a single GPU
// with fine-grained compute resource allocation.
// MIG (Multi-Instance GPU): Partitions a GPU into isolated instances with dedicated
// memory, cache, and compute units (supported on A100/H100).
// Uses nvidia-smi CLI for real MIG partition management and MPS daemon control.
package scheduler

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// GPU Sharing Configuration
// ============================================================================

// GPUSharingConfig holds GPU sharing configuration
type GPUSharingConfig struct {
	NvidiaSmiPath     string
	MPSPipeDirectory  string // default: /tmp/nvidia-mps
	MPSLogDirectory   string // default: /tmp/nvidia-log
	DefaultMIGProfile string // default: "1g.5gb" for A100
}

// GPUShareMode defines the GPU sharing mode
type GPUShareMode string

const (
	GPUShareExclusive GPUShareMode = "exclusive" // single workload per GPU
	GPUShareMPS       GPUShareMode = "mps"       // CUDA Multi-Process Service
	GPUShareMIG       GPUShareMode = "mig"       // Multi-Instance GPU
	GPUShareTimeslice GPUShareMode = "timeslice"  // Kubernetes time-slicing
)

// MIGProfile describes a MIG partition profile
type MIGProfile struct {
	Name       string `json:"name"`       // e.g., "1g.5gb", "2g.10gb", "3g.20gb", "4g.20gb", "7g.40gb"
	GPUSlices  int    `json:"gpu_slices"` // number of compute slices
	MemoryGB   int    `json:"memory_gb"`  // memory per instance
	MaxInstances int  `json:"max_instances"` // max instances of this profile per GPU
}

// MIGInstance represents an active MIG instance
type MIGInstance struct {
	ID         string `json:"id"`
	GPUIndex   int    `json:"gpu_index"`
	Profile    string `json:"profile"`
	UUID       string `json:"uuid"`
	MemoryMB   int    `json:"memory_mb"`
	InUse      bool   `json:"in_use"`
}

// MPSStatus represents the current MPS daemon status
type MPSStatus struct {
	Active       bool   `json:"active"`
	GPUIndex     int    `json:"gpu_index"`
	PipeDir      string `json:"pipe_directory"`
	LogDir       string `json:"log_directory"`
	ActiveClients int   `json:"active_clients"`
	MaxClients   int    `json:"max_clients"`    // GPU default limit
	ThreadPct    int    `json:"thread_percent"` // active thread percentage (0-100)
}

// GPUSharingState holds the current GPU sharing state for a node
type GPUSharingState struct {
	GPUIndex     int           `json:"gpu_index"`
	Mode         GPUShareMode  `json:"mode"`
	MIGInstances []MIGInstance `json:"mig_instances,omitempty"`
	MPSStatus    *MPSStatus    `json:"mps_status,omitempty"`
	ShareRatio   float64       `json:"share_ratio"` // 0-1, portion allocated
}

// ============================================================================
// GPU Sharing Manager
// ============================================================================

// GPUSharingManager manages GPU sharing via MPS and MIG
type GPUSharingManager struct {
	config       GPUSharingConfig
	gpuStates    map[int]*GPUSharingState  // GPU index → sharing state
	memoryStates map[int]*GPUMemoryState   // GPU index → memory state
	logger       *logrus.Logger
	mu           sync.RWMutex
}

// NewGPUSharingManager creates a new GPU sharing manager
func NewGPUSharingManager(cfg GPUSharingConfig) *GPUSharingManager {
	if cfg.NvidiaSmiPath == "" {
		cfg.NvidiaSmiPath = "nvidia-smi"
	}
	if cfg.MPSPipeDirectory == "" {
		cfg.MPSPipeDirectory = "/tmp/nvidia-mps"
	}
	if cfg.MPSLogDirectory == "" {
		cfg.MPSLogDirectory = "/tmp/nvidia-log"
	}
	if cfg.DefaultMIGProfile == "" {
		cfg.DefaultMIGProfile = "1g.5gb"
	}

	return &GPUSharingManager{
		config:       cfg,
		gpuStates:    make(map[int]*GPUSharingState),
		memoryStates: make(map[int]*GPUMemoryState),
		logger:       logrus.StandardLogger(),
	}
}

// ============================================================================
// MIG (Multi-Instance GPU) Operations
// ============================================================================

// SupportedMIGProfiles returns MIG profiles for different GPU types
func SupportedMIGProfiles(gpuType string) []MIGProfile {
	switch {
	case strings.Contains(gpuType, "a100") && strings.Contains(gpuType, "80"):
		return []MIGProfile{
			{Name: "1g.10gb", GPUSlices: 1, MemoryGB: 10, MaxInstances: 7},
			{Name: "2g.20gb", GPUSlices: 2, MemoryGB: 20, MaxInstances: 3},
			{Name: "3g.40gb", GPUSlices: 3, MemoryGB: 40, MaxInstances: 2},
			{Name: "4g.40gb", GPUSlices: 4, MemoryGB: 40, MaxInstances: 1},
			{Name: "7g.80gb", GPUSlices: 7, MemoryGB: 80, MaxInstances: 1},
		}
	case strings.Contains(gpuType, "a100"):
		return []MIGProfile{
			{Name: "1g.5gb", GPUSlices: 1, MemoryGB: 5, MaxInstances: 7},
			{Name: "2g.10gb", GPUSlices: 2, MemoryGB: 10, MaxInstances: 3},
			{Name: "3g.20gb", GPUSlices: 3, MemoryGB: 20, MaxInstances: 2},
			{Name: "4g.20gb", GPUSlices: 4, MemoryGB: 20, MaxInstances: 1},
			{Name: "7g.40gb", GPUSlices: 7, MemoryGB: 40, MaxInstances: 1},
		}
	case strings.Contains(gpuType, "h100"):
		return []MIGProfile{
			{Name: "1g.10gb", GPUSlices: 1, MemoryGB: 10, MaxInstances: 7},
			{Name: "2g.20gb", GPUSlices: 2, MemoryGB: 20, MaxInstances: 3},
			{Name: "3g.40gb", GPUSlices: 3, MemoryGB: 40, MaxInstances: 2},
			{Name: "4g.40gb", GPUSlices: 4, MemoryGB: 40, MaxInstances: 1},
			{Name: "7g.80gb", GPUSlices: 7, MemoryGB: 80, MaxInstances: 1},
		}
	default:
		return nil // MIG not supported
	}
}

// EnableMIG enables MIG mode on a GPU via nvidia-smi
func (mgr *GPUSharingManager) EnableMIG(ctx context.Context, gpuIndex int) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// nvidia-smi -i <gpu> -mig 1
	cmd := exec.CommandContext(ctx, mgr.config.NvidiaSmiPath, "-i", strconv.Itoa(gpuIndex), "-mig", "1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to enable MIG on GPU %d: %w (output: %s)", gpuIndex, err, string(output))
	}

	mgr.mu.Lock()
	mgr.gpuStates[gpuIndex] = &GPUSharingState{
		GPUIndex: gpuIndex,
		Mode:     GPUShareMIG,
	}
	mgr.mu.Unlock()

	mgr.logger.WithField("gpu", gpuIndex).Info("MIG mode enabled")
	return nil
}

// CreateMIGInstance creates a MIG instance with the specified profile
func (mgr *GPUSharingManager) CreateMIGInstance(ctx context.Context, gpuIndex int, profile string) (*MIGInstance, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Step 1: Create GPU Instance
	// nvidia-smi mig -i <gpu> -cgi <profile> -C
	cmd := exec.CommandContext(ctx, mgr.config.NvidiaSmiPath, "mig", "-i", strconv.Itoa(gpuIndex), "-cgi", profile, "-C")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create MIG instance %s on GPU %d: %w (output: %s)", profile, gpuIndex, err, string(output))
	}

	instance := &MIGInstance{
		ID:       common.NewUUID(),
		GPUIndex: gpuIndex,
		Profile:  profile,
	}

	mgr.mu.Lock()
	state, ok := mgr.gpuStates[gpuIndex]
	if !ok {
		state = &GPUSharingState{GPUIndex: gpuIndex, Mode: GPUShareMIG}
		mgr.gpuStates[gpuIndex] = state
	}
	state.MIGInstances = append(state.MIGInstances, *instance)
	mgr.mu.Unlock()

	mgr.logger.WithFields(logrus.Fields{"gpu": gpuIndex, "profile": profile}).Info("MIG instance created")
	return instance, nil
}

// ListMIGInstances lists active MIG instances on a GPU
func (mgr *GPUSharingManager) ListMIGInstances(ctx context.Context, gpuIndex int) ([]MIGInstance, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// nvidia-smi mig -i <gpu> -lgi
	cmd := exec.CommandContext(ctx, mgr.config.NvidiaSmiPath, "mig", "-i", strconv.Itoa(gpuIndex), "-lgi")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If MIG is not enabled or no instances, return empty
		return nil, fmt.Errorf("failed to list MIG instances: %w", err)
	}

	return parseMIGInstances(gpuIndex, string(output)), nil
}

// DestroyMIGInstance destroys a MIG instance
func (mgr *GPUSharingManager) DestroyMIGInstance(ctx context.Context, gpuIndex int, instanceID string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// nvidia-smi mig -i <gpu> -dgi -gi <instance_id>
	cmd := exec.CommandContext(ctx, mgr.config.NvidiaSmiPath, "mig", "-i", strconv.Itoa(gpuIndex), "-dgi", "-gi", instanceID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to destroy MIG instance: %w (output: %s)", err, string(output))
	}

	mgr.mu.Lock()
	if state, ok := mgr.gpuStates[gpuIndex]; ok {
		filtered := make([]MIGInstance, 0)
		for _, inst := range state.MIGInstances {
			if inst.ID != instanceID {
				filtered = append(filtered, inst)
			}
		}
		state.MIGInstances = filtered
	}
	mgr.mu.Unlock()

	return nil
}

// parseMIGInstances parses nvidia-smi mig -lgi output
func parseMIGInstances(gpuIndex int, output string) []MIGInstance {
	var instances []MIGInstance
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Example line: "  GPU  0 Profile  19 : 1g.5gb  Instances: 1/7"
		if strings.Contains(line, "Profile") && strings.Contains(line, "g.") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if strings.Contains(p, "g.") && strings.Contains(p, "gb") {
					inst := MIGInstance{
						ID:       common.NewUUID(),
						GPUIndex: gpuIndex,
						Profile:  p,
					}
					// Try to parse memory from profile name
					if i > 0 {
						if memStr := strings.Split(p, "."); len(memStr) > 1 {
							memGB := strings.TrimSuffix(memStr[1], "gb")
							if mem, err := strconv.Atoi(memGB); err == nil {
								inst.MemoryMB = mem * 1024
							}
						}
					}
					instances = append(instances, inst)
				}
			}
		}
	}
	return instances
}

// ============================================================================
// MPS (Multi-Process Service) Operations
// ============================================================================

// StartMPS starts the NVIDIA MPS daemon for a GPU
func (mgr *GPUSharingManager) StartMPS(ctx context.Context, gpuIndex int, threadPercentage int) error {
	if threadPercentage <= 0 || threadPercentage > 100 {
		threadPercentage = 100
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Set environment and start MPS control daemon
	// CUDA_VISIBLE_DEVICES=<gpu> CUDA_MPS_PIPE_DIRECTORY=<dir> nvidia-cuda-mps-control -d
	env := fmt.Sprintf("CUDA_VISIBLE_DEVICES=%d", gpuIndex)
	pipeDir := fmt.Sprintf("CUDA_MPS_PIPE_DIRECTORY=%s", mgr.config.MPSPipeDirectory)
	logDir := fmt.Sprintf("CUDA_MPS_LOG_DIRECTORY=%s", mgr.config.MPSLogDirectory)

	cmd := exec.CommandContext(ctx, "nvidia-cuda-mps-control", "-d")
	cmd.Env = append(cmd.Environ(), env, pipeDir, logDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start MPS on GPU %d: %w (output: %s)", gpuIndex, err, string(output))
	}

	// Set active thread percentage if specified
	if threadPercentage < 100 {
		setCmd := exec.CommandContext(ctx, "bash", "-c",
			fmt.Sprintf("echo 'set_active_thread_percentage %d' | CUDA_MPS_PIPE_DIRECTORY=%s nvidia-cuda-mps-control",
				threadPercentage, mgr.config.MPSPipeDirectory))
		setCmd.Run() //nolint:errcheck
	}

	mgr.mu.Lock()
	mgr.gpuStates[gpuIndex] = &GPUSharingState{
		GPUIndex: gpuIndex,
		Mode:     GPUShareMPS,
		MPSStatus: &MPSStatus{
			Active:    true,
			GPUIndex:  gpuIndex,
			PipeDir:   mgr.config.MPSPipeDirectory,
			LogDir:    mgr.config.MPSLogDirectory,
			ThreadPct: threadPercentage,
		},
	}
	mgr.mu.Unlock()

	mgr.logger.WithFields(logrus.Fields{
		"gpu": gpuIndex, "thread_pct": threadPercentage,
	}).Info("MPS daemon started")
	return nil
}

// StopMPS stops the NVIDIA MPS daemon
func (mgr *GPUSharingManager) StopMPS(ctx context.Context, gpuIndex int) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo quit | CUDA_MPS_PIPE_DIRECTORY=%s nvidia-cuda-mps-control",
			mgr.config.MPSPipeDirectory))
	cmd.Run() //nolint:errcheck

	mgr.mu.Lock()
	delete(mgr.gpuStates, gpuIndex)
	mgr.mu.Unlock()

	mgr.logger.WithField("gpu", gpuIndex).Info("MPS daemon stopped")
	return nil
}

// GetMPSStatus queries the current MPS daemon status
func (mgr *GPUSharingManager) GetMPSStatus(ctx context.Context, gpuIndex int) (*MPSStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c",
		fmt.Sprintf("echo get_server_list | CUDA_MPS_PIPE_DIRECTORY=%s nvidia-cuda-mps-control",
			mgr.config.MPSPipeDirectory))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("MPS not active on GPU %d", gpuIndex)
	}

	status := &MPSStatus{
		Active:   true,
		GPUIndex: gpuIndex,
		PipeDir:  mgr.config.MPSPipeDirectory,
	}

	// Count client PIDs
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			status.ActiveClients++
		}
	}

	return status, nil
}

// ============================================================================
// GPU Sharing Decision Logic
// ============================================================================

// RecommendShareMode recommends the best GPU sharing mode for a workload
func RecommendShareMode(workloadType string, gpuMemRequired int64, gpuType string, currentUtil float64) GPUShareMode {
	// Training workloads: prefer exclusive or MIG (need isolation)
	if workloadType == "training" || workloadType == "fine-tuning" {
		if gpuMemRequired > 20*1024*1024*1024 { // > 20GB
			return GPUShareExclusive
		}
		// Check if MIG is available for this GPU
		profiles := SupportedMIGProfiles(gpuType)
		if len(profiles) > 0 {
			return GPUShareMIG
		}
		return GPUShareExclusive
	}

	// Inference workloads: good candidate for MPS sharing
	if workloadType == "inference" || workloadType == "serving" {
		if currentUtil < 50 { // GPU is underutilized
			return GPUShareMPS
		}
		profiles := SupportedMIGProfiles(gpuType)
		if len(profiles) > 0 {
			return GPUShareMIG
		}
		return GPUShareMPS
	}

	// Batch: depends on size
	if currentUtil < 30 {
		return GPUShareMPS
	}
	return GPUShareExclusive
}

// GetGPUSharingStates returns all GPU sharing states
func (mgr *GPUSharingManager) GetGPUSharingStates() map[int]*GPUSharingState {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	result := make(map[int]*GPUSharingState, len(mgr.gpuStates))
	for k, v := range mgr.gpuStates {
		result[k] = v
	}
	return result
}

// ============================================================================
// GPU Memory Overselling & Isolation
// ============================================================================

// MemoryOversellPolicy defines the memory overselling configuration
type MemoryOversellPolicy struct {
	Enabled           bool    `json:"enabled"`
	MaxOversellRatio  float64 `json:"max_oversell_ratio"`  // e.g., 1.5 = 150% of physical memory
	SoftLimitRatio    float64 `json:"soft_limit_ratio"`    // OOM-kill threshold per workload
	HardLimitRatio    float64 `json:"hard_limit_ratio"`    // absolute maximum
	EvictionPriority  string  `json:"eviction_priority"`   // "lru", "priority", "memory-usage"
	SwapEnabled       bool    `json:"swap_enabled"`        // unified memory (host↔device) swap
}

// MemoryIsolationGroup represents an isolated memory region for a workload
type MemoryIsolationGroup struct {
	ID             string  `json:"id"`
	GPUIndex       int     `json:"gpu_index"`
	WorkloadID     string  `json:"workload_id"`
	AllocatedMiB   int     `json:"allocated_mib"`
	UsedMiB        int     `json:"used_mib"`
	SoftLimitMiB   int     `json:"soft_limit_mib"`
	HardLimitMiB   int     `json:"hard_limit_mib"`
	OversellFactor float64 `json:"oversell_factor"`
	Priority       int     `json:"priority"`
}

// GPUMemoryState tracks memory allocation state for a GPU
type GPUMemoryState struct {
	GPUIndex         int                      `json:"gpu_index"`
	PhysicalTotalMiB int                      `json:"physical_total_mib"`
	PhysicalUsedMiB  int                      `json:"physical_used_mib"`
	VirtualTotalMiB  int                      `json:"virtual_total_mib"` // with overselling
	VirtualUsedMiB   int                      `json:"virtual_used_mib"`
	OversellRatio    float64                  `json:"oversell_ratio"`
	IsolationGroups  []*MemoryIsolationGroup  `json:"isolation_groups"`
}

// DefaultMemoryOversellPolicy returns a conservative overselling policy
func DefaultMemoryOversellPolicy() *MemoryOversellPolicy {
	return &MemoryOversellPolicy{
		Enabled:          true,
		MaxOversellRatio: 1.5,
		SoftLimitRatio:   0.85,
		HardLimitRatio:   0.95,
		EvictionPriority: "priority",
		SwapEnabled:      false,
	}
}

// AllocateMemoryIsolationGroup creates an isolated memory region for a workload
func (mgr *GPUSharingManager) AllocateMemoryIsolationGroup(
	gpuIndex int,
	workloadID string,
	requiredMiB int,
	priority int,
	policy *MemoryOversellPolicy,
) (*MemoryIsolationGroup, error) {
	if policy == nil {
		policy = DefaultMemoryOversellPolicy()
	}

	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	state, ok := mgr.memoryStates[gpuIndex]
	if !ok {
		return nil, fmt.Errorf("GPU %d memory state not initialized", gpuIndex)
	}

	// Calculate virtual capacity with overselling
	virtualCap := int(float64(state.PhysicalTotalMiB) * policy.MaxOversellRatio)
	available := virtualCap - state.VirtualUsedMiB

	if requiredMiB > available {
		return nil, fmt.Errorf("insufficient GPU memory on GPU %d: need %dMiB, available %dMiB (virtual)",
			gpuIndex, requiredMiB, available)
	}

	// Create isolation group with soft/hard limits
	group := &MemoryIsolationGroup{
		ID:             fmt.Sprintf("mig-%s-%d", workloadID[:8], gpuIndex),
		GPUIndex:       gpuIndex,
		WorkloadID:     workloadID,
		AllocatedMiB:   requiredMiB,
		UsedMiB:        0,
		SoftLimitMiB:   int(float64(requiredMiB) * policy.SoftLimitRatio),
		HardLimitMiB:   int(float64(requiredMiB) * policy.HardLimitRatio),
		OversellFactor: policy.MaxOversellRatio,
		Priority:       priority,
	}

	state.IsolationGroups = append(state.IsolationGroups, group)
	state.VirtualUsedMiB += requiredMiB
	state.OversellRatio = float64(state.VirtualUsedMiB) / float64(state.PhysicalTotalMiB)

	mgr.logger.WithFields(logrus.Fields{
		"gpu":       gpuIndex,
		"workload":  workloadID,
		"allocated": requiredMiB,
		"oversell":  fmt.Sprintf("%.2f", state.OversellRatio),
	}).Info("Memory isolation group allocated")

	return group, nil
}

// ReleaseMemoryIsolationGroup releases an isolated memory region
func (mgr *GPUSharingManager) ReleaseMemoryIsolationGroup(gpuIndex int, groupID string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	state, ok := mgr.memoryStates[gpuIndex]
	if !ok {
		return fmt.Errorf("GPU %d memory state not found", gpuIndex)
	}

	for i, g := range state.IsolationGroups {
		if g.ID == groupID {
			state.VirtualUsedMiB -= g.AllocatedMiB
			state.IsolationGroups = append(state.IsolationGroups[:i], state.IsolationGroups[i+1:]...)
			if state.PhysicalTotalMiB > 0 {
				state.OversellRatio = float64(state.VirtualUsedMiB) / float64(state.PhysicalTotalMiB)
			}
			return nil
		}
	}
	return fmt.Errorf("isolation group %s not found on GPU %d", groupID, gpuIndex)
}

// SelectEvictionCandidate selects a workload to evict when physical memory is exhausted
func (mgr *GPUSharingManager) SelectEvictionCandidate(gpuIndex int, policy *MemoryOversellPolicy) *MemoryIsolationGroup {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	state, ok := mgr.memoryStates[gpuIndex]
	if !ok || len(state.IsolationGroups) == 0 {
		return nil
	}

	var candidate *MemoryIsolationGroup
	for _, g := range state.IsolationGroups {
		switch policy.EvictionPriority {
		case "priority":
			if candidate == nil || g.Priority < candidate.Priority {
				candidate = g
			}
		case "memory-usage":
			if candidate == nil || g.UsedMiB > candidate.UsedMiB {
				candidate = g
			}
		default: // lru - pick first (oldest)
			if candidate == nil {
				candidate = g
			}
		}
	}
	return candidate
}

// InitGPUMemoryState initializes memory tracking for a GPU
func (mgr *GPUSharingManager) InitGPUMemoryState(gpuIndex int, physicalTotalMiB int) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	mgr.memoryStates[gpuIndex] = &GPUMemoryState{
		GPUIndex:         gpuIndex,
		PhysicalTotalMiB: physicalTotalMiB,
		VirtualTotalMiB:  int(float64(physicalTotalMiB) * 1.5),
		IsolationGroups:  make([]*MemoryIsolationGroup, 0),
	}
}

// GetGPUMemoryStates returns all GPU memory states
func (mgr *GPUSharingManager) GetGPUMemoryStates() map[int]*GPUMemoryState {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	result := make(map[int]*GPUMemoryState, len(mgr.memoryStates))
	for k, v := range mgr.memoryStates {
		result[k] = v
	}
	return result
}
