package plugin

import (
	"context"
)

// ============================================================================
// Scheduler Extension Point Interfaces
// Modeled after Kubernetes Scheduling Framework (KEP-624):
//   https://kubernetes.io/docs/concepts/scheduling-eviction/scheduling-framework/
// ============================================================================

// NodeInfo is a minimal representation of a candidate node passed to scheduler
// plugins. It intentionally avoids importing the scheduler package to prevent
// circular dependencies — the scheduler engine converts its concrete types
// into NodeInfo before calling the plugin chain.
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

// WorkloadInfo is a minimal representation of the workload being scheduled.
type WorkloadInfo struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	Type            string            `json:"type"`
	Priority        int               `json:"priority"`
	Framework       string            `json:"framework"`
	GPUCount        int               `json:"gpuCount"`
	GPUMemoryMB     int64             `json:"gpuMemoryMb"`
	CPUMillis       int64             `json:"cpuMillis"`
	MemoryMB        int64             `json:"memoryMb"`
	RequireNVLink   bool              `json:"requireNvlink"`
	PreferredNodes  []string          `json:"preferredNodes,omitempty"`
	AvoidNodes      []string          `json:"avoidNodes,omitempty"`
	MaxCostPerHour  float64           `json:"maxCostPerHour,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
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
	cs.data[key] = val
}

// Read retrieves a value.
func (cs *CycleState) Read(key string) (interface{}, bool) {
	v, ok := cs.data[key]
	return v, ok
}

// Delete removes a value.
func (cs *CycleState) Delete(key string) {
	delete(cs.data, key)
}

// ============================================================================
// Extension Point Interfaces
// ============================================================================

// PreFilterPlugin runs before filtering to validate or enrich the workload.
type PreFilterPlugin interface {
	Plugin
	PreFilter(ctx context.Context, state *CycleState, workload *WorkloadInfo) *Result
}

// FilterPlugin decides whether a node can host the workload.
// Returning Unschedulable removes the node from candidates.
type FilterPlugin interface {
	Plugin
	Filter(ctx context.Context, state *CycleState, workload *WorkloadInfo, node *NodeInfo) *Result
}

// PostFilterPlugin runs if no node passed the Filter phase. It can relax
// constraints or trigger preemption.
type PostFilterPlugin interface {
	Plugin
	PostFilter(ctx context.Context, state *CycleState, workload *WorkloadInfo, filteredNodes map[string]*Result) (*PostFilterResult, *Result)
}

// PostFilterResult hints what the PostFilter plugin did.
type PostFilterResult struct {
	NominatedNodeName string `json:"nominatedNodeName,omitempty"`
}

// ScorePlugin assigns a numeric score to each candidate node.
// Scores are normalized to [0, 100] by the framework.
type ScorePlugin interface {
	Plugin
	Score(ctx context.Context, state *CycleState, workload *WorkloadInfo, node *NodeInfo) (int64, *Result)
	// ScoreWeight returns the weight of this scoring plugin. Default 1.
	ScoreWeight() int64
}

// NormalizeScorePlugin optionally normalizes scores across all nodes after
// all ScorePlugins have run.
type NormalizeScorePlugin interface {
	ScorePlugin
	NormalizeScore(ctx context.Context, state *CycleState, scores map[string]int64) *Result
}

// ReservePlugin is called after scoring but before binding to reserve
// resources on the selected node.
type ReservePlugin interface {
	Plugin
	Reserve(ctx context.Context, state *CycleState, workload *WorkloadInfo, nodeName string) *Result
	// Unreserve is called if a later phase fails, so the plugin can clean up.
	Unreserve(ctx context.Context, state *CycleState, workload *WorkloadInfo, nodeName string)
}

// PermitPlugin gates whether a workload is allowed to proceed to binding.
// It can return Wait to defer the decision.
type PermitPlugin interface {
	Plugin
	Permit(ctx context.Context, state *CycleState, workload *WorkloadInfo, nodeName string) (*Result, int64)
}

// PreBindPlugin runs before the workload is bound to the node
// (e.g., to create volumes or network resources).
type PreBindPlugin interface {
	Plugin
	PreBind(ctx context.Context, state *CycleState, workload *WorkloadInfo, nodeName string) *Result
}

// BindPlugin performs the actual binding of the workload to a node.
// Only one BindPlugin should handle a workload.
type BindPlugin interface {
	Plugin
	Bind(ctx context.Context, state *CycleState, workload *WorkloadInfo, nodeName string) *Result
}

// PostBindPlugin runs after successful binding for bookkeeping or notifications.
type PostBindPlugin interface {
	Plugin
	PostBind(ctx context.Context, state *CycleState, workload *WorkloadInfo, nodeName string)
}

// ============================================================================
// SchedulerPluginChain — runs the plugin chain for each extension point
// ============================================================================

// SchedulerPluginChain holds the ordered lists of scheduler plugins per phase.
type SchedulerPluginChain struct {
	registry *Registry
}

// NewSchedulerPluginChain wraps a registry for scheduler plugin execution.
func NewSchedulerPluginChain(reg *Registry) *SchedulerPluginChain {
	return &SchedulerPluginChain{registry: reg}
}

// RunFilterPlugins runs all FilterPlugins. Returns Success only if all pass.
func (c *SchedulerPluginChain) RunFilterPlugins(ctx context.Context, state *CycleState, w *WorkloadInfo, node *NodeInfo) *Result {
	for _, p := range c.registry.GetByExtension(ExtSchedulerFilter) {
		if fp, ok := p.(FilterPlugin); ok {
			r := fp.Filter(ctx, state, w, node)
			if r != nil && !r.IsSuccess() {
				return r
			}
		}
	}
	return SuccessResult("filter-chain")
}

// RunScorePlugins runs all ScorePlugins and returns weighted aggregate scores per node.
func (c *SchedulerPluginChain) RunScorePlugins(ctx context.Context, state *CycleState, w *WorkloadInfo, nodes []*NodeInfo) (map[string]int64, *Result) {
	scores := make(map[string]int64)
	for _, node := range nodes {
		scores[node.Name] = 0
	}

	for _, p := range c.registry.GetByExtension(ExtSchedulerScore) {
		sp, ok := p.(ScorePlugin)
		if !ok {
			continue
		}
		weight := sp.ScoreWeight()
		if weight <= 0 {
			weight = 1
		}
		for _, node := range nodes {
			score, r := sp.Score(ctx, state, w, node)
			if r != nil && !r.IsSuccess() {
				return nil, r
			}
			scores[node.Name] += score * weight
		}
	}
	return scores, SuccessResult("score-chain")
}

// RunPreBindPlugins runs all PreBindPlugins.
func (c *SchedulerPluginChain) RunPreBindPlugins(ctx context.Context, state *CycleState, w *WorkloadInfo, nodeName string) *Result {
	for _, p := range c.registry.GetByExtension(ExtSchedulerPreBind) {
		if pb, ok := p.(PreBindPlugin); ok {
			r := pb.PreBind(ctx, state, w, nodeName)
			if r != nil && !r.IsSuccess() {
				return r
			}
		}
	}
	return SuccessResult("prebind-chain")
}

// RunPostBindPlugins runs all PostBindPlugins (fire-and-forget).
func (c *SchedulerPluginChain) RunPostBindPlugins(ctx context.Context, state *CycleState, w *WorkloadInfo, nodeName string) {
	for _, p := range c.registry.GetByExtension(ExtSchedulerPostBind) {
		if pb, ok := p.(PostBindPlugin); ok {
			pb.PostBind(ctx, state, w, nodeName)
		}
	}
}

// RunReservePlugins runs all ReservePlugins. On failure, Unreserve is called
// on all previously-reserved plugins.
func (c *SchedulerPluginChain) RunReservePlugins(ctx context.Context, state *CycleState, w *WorkloadInfo, nodeName string) *Result {
	var reserved []ReservePlugin
	for _, p := range c.registry.GetByExtension(ExtSchedulerReserve) {
		rp, ok := p.(ReservePlugin)
		if !ok {
			continue
		}
		r := rp.Reserve(ctx, state, w, nodeName)
		if r != nil && !r.IsSuccess() {
			// Unreserve all previously reserved.
			for _, prev := range reserved {
				prev.Unreserve(ctx, state, w, nodeName)
			}
			return r
		}
		reserved = append(reserved, rp)
	}
	return SuccessResult("reserve-chain")
}
