// Package types provides shared data structures used across CloudAI Fusion packages.
// This package is intentionally free of external dependencies to avoid circular imports.
// It contains GPU topology and scheduler-related types that need to be shared between
// pkg/scheduler and pkg/plugin/builtin.
package types

import (
	"time"
)

// ============================================================================
// GPU Topology Types
// Shared between scheduler and plugin/builtin
// ============================================================================

// NVLinkConnection describes an NVLink connection between two GPUs
type NVLinkConnection struct {
	GPU1Index   int     `json:"gpu1_index"`
	GPU2Index   int     `json:"gpu2_index"`
	LinkType    string  `json:"link_type"` // NVL (NVLink), PHB (PCIe Hub), SYS (System/QPI), PIX (PCIe)
	BandwidthGB float64 `json:"bandwidth_gbps"`
	NVLinkGen   int     `json:"nvlink_generation"` // 2=NVLink 2.0, 3=NVLink 3.0 (600GB/s), 4=NVLink 4.0 (900GB/s)
	Active      bool    `json:"active"`             // Link is physically active
}

// DiscoveredGPU represents a discovered GPU device with full details
type DiscoveredGPU struct {
	Index           int     `json:"index"`
	UUID            string  `json:"uuid"`
	Name            string  `json:"name"`
	MemoryTotalMiB  int     `json:"memory_total_mib"`
	MemoryUsedMiB   int     `json:"memory_used_mib"`
	MemoryFreeMiB   int     `json:"memory_free_mib"`
	Utilization     float64 `json:"utilization_percent"`
	Temperature     int     `json:"temperature_celsius"`
	PowerUsageW     float64 `json:"power_usage_watts"`
	PowerLimitW     float64 `json:"power_limit_watts"`
	NUMANode        int     `json:"numa_node"`
	PCIBusID        string  `json:"pci_bus_id"`
	ComputeMode     string  `json:"compute_mode"` // Default, Exclusive_Thread, Exclusive_Process, Prohibited
	MIGEnabled      bool    `json:"mig_enabled"`
	MPSServerActive bool    `json:"mps_server_active"`
}

// NodeGPUTopology holds complete GPU topology for a node
type NodeGPUTopology struct {
	NodeName    string             `json:"node_name"`
	GPUs        []DiscoveredGPU    `json:"gpus"`
	NVLinks     []NVLinkConnection `json:"nvlink_connections"`
	NUMANodes   map[int][]int      `json:"numa_nodes"` // NUMA node → GPU indices
	P2PMatrix   map[string]string  `json:"p2p_matrix"` // "0-1" → "NVL" | "PHB" | "SYS"
	TotalGPUs   int                `json:"total_gpus"`
	HasNVLink   bool               `json:"has_nvlink"`
	HasNVSwitch bool               `json:"has_nvswitch"`
}

// ============================================================================
// Scheduler Extension Types
// Minimal representations passed from scheduler to plugins
// These mirror k8s.io/kubernetes/pkg/scheduler/framework types but are
// defined here to avoid circular imports
// ============================================================================

// WorkloadInfo is a minimal representation of the workload being scheduled.
// This is a simplified version that avoids dependencies on k8s API types.
type WorkloadInfo struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Namespace      string            `json:"namespace"`
	Type           string            `json:"type"`
	Priority       int               `json:"priority"`
	Framework      string            `json:"framework"`
	GPUCount       int               `json:"gpuCount"`
	GPUMemoryMB    int64             `json:"gpuMemoryMb"`
	CPUMillis      int64             `json:"cpuMillis"`
	MemoryMB       int64             `json:"memoryMb"`
	RequireNVLink  bool              `json:"requireNvlink"`
	PreferredNodes []string          `json:"preferredNodes,omitempty"`
	AvoidNodes     []string          `json:"avoidNodes,omitempty"`
	MaxCostPerHour float64           `json:"maxCostPerHour,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// NodeInfo is a minimal representation of a candidate node passed to scheduler
// plugins.
type NodeInfo struct {
	Name             string            `json:"name"`
	ClusterID        string            `json:"clusterId"`
	GPUType          string            `json:"gpuType"`
	GPUTotal         int               `json:"gpuTotal"`
	GPUFree          int               `json:"gpuFree"`
	GPUUtilization   float64           `json:"gpuUtilization"`
	MemoryTotalBytes int64             `json:"memoryTotalBytes"`
	MemoryFreeBytes  int64             `json:"memoryFreeBytes"`
	CPUCores         int               `json:"cpuCores"`
	CostPerHour      float64           `json:"costPerHour"`
	TopologyScore    float64           `json:"topologyScore"`
	Labels           map[string]string `json:"labels,omitempty"`
	Taints           []string          `json:"taints,omitempty"`
	IsSpot           bool              `json:"isSpot"`
}

// CycleState is a per-scheduling-cycle shared state that plugins can read and
// write during Filter/Score/Bind phases.
type CycleState struct {
	data map[string]interface{}
}

// NewCycleState creates an empty CycleState.
func NewCycleState() *CycleState {
	return &CycleState{data: make(map[string]interface{})}
}

// Write stores a value.
func (cs *CycleState) Write(key string, val interface{}) {
	if cs.data == nil {
		cs.data = make(map[string]interface{})
	}
	cs.data[key] = val
}

// Read retrieves a value.
func (cs *CycleState) Read(key string) (interface{}, bool) {
	if cs.data == nil {
		return nil, false
	}
	v, ok := cs.data[key]
	return v, ok
}

// Delete removes a value.
func (cs *CycleState) Delete(key string) {
	if cs.data != nil {
		delete(cs.data, key)
	}
}

// Additional helper methods for CycleState

// GetTopology returns cached topology if present
func (cs *CycleState) GetTopology() (*NodeGPUTopology, bool) {
	v, ok := cs.Read("topology")
	if !ok {
		return nil, false
	}
	topo, ok := v.(*NodeGPUTopology)
	return topo, ok
}

// SetTopology caches topology for use by subsequent plugins
func (cs *CycleState) SetTopology(topo *NodeGPUTopology) {
	cs.Write("topology", topo)
}

// GetWorkloadAnnotations returns workload annotations if cached
func (cs *CycleState) GetWorkloadAnnotations() (map[string]string, bool) {
	v, ok := cs.Read("workload_annotations")
	if !ok {
		return nil, false
	}
	annotations, ok := v.(map[string]string)
	return annotations, ok
}

// SetWorkloadAnnotations caches workload annotations
func (cs *CycleState) SetWorkloadAnnotations(annos map[string]string) {
	cs.Write("workload_annotations", annos)
}

// ============================================================================
// Scoring Related Types
// ============================================================================

// ScoreResult represents the scoring result from a plugin
type ScoreResult struct {
	Score   int64  `json:"score"`
	Reason  string `json:"reason,omitempty"`
	Plugin  string `json:"plugin"`
	Code    int    `json:"code"` // 0=Success, 1=Error, 2=Unschedulable
}

// IsSuccess returns true if the score result indicates success
func (sr *ScoreResult) IsSuccess() bool {
	return sr.Code == 0
}

// NewScoreResult creates a new score result
func NewScoreResult(score int64, code int, plugin, reason string) *ScoreResult {
	return &ScoreResult{
		Score:  score,
		Code:   code,
		Reason: reason,
		Plugin: plugin,
	}
}

// ============================================================================
// Utility Functions
// ============================================================================

// Now returns current time, useful for testing
var Now = func() time.Time {
	return time.Now()
}
