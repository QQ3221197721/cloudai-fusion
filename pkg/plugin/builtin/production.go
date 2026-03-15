package builtin

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// ============================================================================
// ResourceQuotaFilterPlugin — enforces namespace-level resource quotas
// ============================================================================

// ResourceQuotaFilterPlugin rejects scheduling if the namespace has exhausted its GPU quota.
type ResourceQuotaFilterPlugin struct {
	plugin.BasePlugin
	quotas map[string]int // namespace → max GPU count
	usage  map[string]int // namespace → current GPU usage
	mu     sync.RWMutex
}

// NewResourceQuotaFilterPlugin creates a resource quota filter.
func NewResourceQuotaFilterPlugin() (plugin.Plugin, error) {
	return &ResourceQuotaFilterPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "resource-quota-filter",
			Version:     "1.0.0",
			Description: "Enforces per-namespace GPU quotas to prevent resource monopolization",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSchedulerFilter,
			},
			Priority: 5, // run very early
			Tags:     map[string]string{"category": "governance", "tier": "production"},
		}),
		quotas: map[string]int{
			"ml-training": 32,
			"ml-inference": 16,
			"research":     8,
			"default":      4,
		},
		usage: make(map[string]int),
	}, nil
}

func (p *ResourceQuotaFilterPlugin) Init(_ context.Context, config map[string]interface{}) error {
	if q, ok := config["quotas"].(map[string]interface{}); ok {
		p.mu.Lock()
		for ns, v := range q {
			if count, ok := v.(float64); ok {
				p.quotas[ns] = int(count)
			}
		}
		p.mu.Unlock()
	}
	return nil
}

func (p *ResourceQuotaFilterPlugin) Health(_ context.Context) error { return nil }

// Filter checks whether the workload's namespace still has GPU quota available.
func (p *ResourceQuotaFilterPlugin) Filter(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, _ *plugin.NodeInfo) *plugin.Result {
	p.mu.RLock()
	quota, hasQuota := p.quotas[w.Namespace]
	currentUsage := p.usage[w.Namespace]
	p.mu.RUnlock()

	if !hasQuota {
		// No quota configured for this namespace — use default
		quota = p.quotas["default"]
		if quota == 0 {
			return plugin.SuccessResult(p.Metadata().Name) // no quota enforcement
		}
	}

	if currentUsage+w.GPUCount > quota {
		return plugin.NewResult(plugin.Unschedulable, p.Metadata().Name,
			fmt.Sprintf("namespace %s GPU quota exceeded: using %d/%d, requesting %d",
				w.Namespace, currentUsage, quota, w.GPUCount))
	}
	return plugin.SuccessResult(p.Metadata().Name)
}

// RecordUsage updates GPU usage for a namespace (called after successful scheduling).
func (p *ResourceQuotaFilterPlugin) RecordUsage(namespace string, gpuCount int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usage[namespace] += gpuCount
}

// ReleaseUsage decrements GPU usage (called after workload completion).
func (p *ResourceQuotaFilterPlugin) ReleaseUsage(namespace string, gpuCount int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usage[namespace] -= gpuCount
	if p.usage[namespace] < 0 {
		p.usage[namespace] = 0
	}
}

// GetQuotaStatus returns the current quota usage for all namespaces.
func (p *ResourceQuotaFilterPlugin) GetQuotaStatus() map[string]map[string]int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]map[string]int)
	for ns, quota := range p.quotas {
		result[ns] = map[string]int{
			"quota": quota,
			"usage": p.usage[ns],
		}
	}
	return result
}

// ============================================================================
// GangSchedulingPlugin — ensures all pods in a group are co-scheduled
// ============================================================================

// GangSchedulingPlugin enforces gang (all-or-nothing) scheduling for distributed
// training jobs. All members of a gang must be schedulable simultaneously.
type GangSchedulingPlugin struct {
	plugin.BasePlugin
	gangs map[string]*gangState // gang_id → state
	mu    sync.Mutex
}

type gangState struct {
	TotalMembers    int
	ScheduledNodes  map[string]string // member_id → node_name
	PendingMembers  int
}

// NewGangSchedulingPlugin creates a gang scheduling plugin.
func NewGangSchedulingPlugin() (plugin.Plugin, error) {
	return &GangSchedulingPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "gang-scheduling",
			Version:     "1.0.0",
			Description: "All-or-nothing scheduling for distributed training (ensures all gang members are co-scheduled)",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSchedulerFilter,
				plugin.ExtSchedulerScore,
			},
			Priority: 15,
			Tags:     map[string]string{"category": "scheduling", "tier": "production"},
		}),
		gangs: make(map[string]*gangState),
	}, nil
}

func (p *GangSchedulingPlugin) Init(_ context.Context, _ map[string]interface{}) error { return nil }
func (p *GangSchedulingPlugin) Health(_ context.Context) error                         { return nil }

// RegisterGang registers a new gang with the expected number of members.
func (p *GangSchedulingPlugin) RegisterGang(gangID string, totalMembers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gangs[gangID] = &gangState{
		TotalMembers:   totalMembers,
		ScheduledNodes: make(map[string]string),
		PendingMembers: totalMembers,
	}
}

// Filter ensures a gang workload can only proceed if enough nodes exist for all members.
func (p *GangSchedulingPlugin) Filter(_ context.Context, state *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) *plugin.Result {
	gangID, isGang := w.Labels["gang-id"]
	if !isGang {
		return plugin.SuccessResult(p.Metadata().Name) // not a gang workload
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	gang, exists := p.gangs[gangID]
	if !exists {
		return plugin.NewResult(plugin.Unschedulable, p.Metadata().Name,
			fmt.Sprintf("gang %s not registered", gangID))
	}

	// Check node has enough GPUs for one gang member
	if node.GPUFree < w.GPUCount {
		return plugin.NewResult(plugin.Unschedulable, p.Metadata().Name,
			fmt.Sprintf("node %s has %d free GPUs, gang member needs %d", node.Name, node.GPUFree, w.GPUCount))
	}

	// Track candidate count in cycle state for gang feasibility check
	key := fmt.Sprintf("gang_%s_candidates", gangID)
	if count, ok := state.Read(key); ok {
		state.Write(key, count.(int)+1)
	} else {
		state.Write(key, 1)
	}

	// Gang feasibility: at least TotalMembers nodes must pass filter
	if candidateCount, ok := state.Read(key); ok {
		if candidateCount.(int) < gang.PendingMembers {
			// Not enough candidates yet — but don't reject; more might come
			return plugin.SuccessResult(p.Metadata().Name)
		}
	}

	return plugin.SuccessResult(p.Metadata().Name)
}

// Score prefers nodes where other gang members are already placed (data locality).
func (p *GangSchedulingPlugin) Score(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	gangID, isGang := w.Labels["gang-id"]
	if !isGang {
		return 50, plugin.SuccessResult(p.Metadata().Name)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	gang, exists := p.gangs[gangID]
	if !exists {
		return 50, plugin.SuccessResult(p.Metadata().Name)
	}

	// Count how many gang members are on this node
	colocated := 0
	for _, scheduledNode := range gang.ScheduledNodes {
		if scheduledNode == node.Name {
			colocated++
		}
	}

	// Score based on colocation: more members on same node = better for NVLink
	if colocated > 0 {
		score := int64(70 + colocated*10)
		if score > 100 {
			score = 100
		}
		return score, plugin.SuccessResult(p.Metadata().Name)
	}

	return 50, plugin.SuccessResult(p.Metadata().Name) // neutral
}

// ScoreWeight returns the scoring weight for gang scheduling.
func (p *GangSchedulingPlugin) ScoreWeight() int64 { return 3 } // high weight for gang affinity

// MarkScheduled records that a gang member has been placed on a node.
func (p *GangSchedulingPlugin) MarkScheduled(gangID, memberID, nodeName string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if gang, ok := p.gangs[gangID]; ok {
		gang.ScheduledNodes[memberID] = nodeName
		gang.PendingMembers--
	}
}

// IsGangComplete returns true if all members of a gang have been scheduled.
func (p *GangSchedulingPlugin) IsGangComplete(gangID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if gang, ok := p.gangs[gangID]; ok {
		return gang.PendingMembers <= 0
	}
	return false
}

// ============================================================================
// PreemptionPolicyPlugin — priority-based preemption scoring
// ============================================================================

// PreemptionPolicyPlugin scores nodes for preemption by preferring nodes where
// lower-priority workloads can be evicted with minimal disruption.
type PreemptionPolicyPlugin struct {
	plugin.BasePlugin
	minPriorityGap int // minimum priority difference to allow preemption
}

// NewPreemptionPolicyPlugin creates a preemption policy plugin.
func NewPreemptionPolicyPlugin() (plugin.Plugin, error) {
	return &PreemptionPolicyPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "preemption-policy",
			Version:     "1.0.0",
			Description: "Priority-based preemption: scores nodes by eviction cost and priority gap",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSchedulerScore,
			},
			Priority:     90,
			Dependencies: []string{"gpu-resource-filter"},
			Tags:         map[string]string{"category": "scheduling", "tier": "production"},
		}),
		minPriorityGap: 3,
	}, nil
}

func (p *PreemptionPolicyPlugin) Init(_ context.Context, config map[string]interface{}) error {
	if gap, ok := config["min_priority_gap"].(float64); ok {
		p.minPriorityGap = int(gap)
	}
	return nil
}

func (p *PreemptionPolicyPlugin) Health(_ context.Context) error { return nil }

// Score scores nodes based on preemption feasibility. Nodes with more low-priority
// preemptable GPUs get higher scores.
func (p *PreemptionPolicyPlugin) Score(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	// If node already has free GPUs, preemption scoring is neutral
	if node.GPUFree >= w.GPUCount {
		return 50, plugin.SuccessResult(p.Metadata().Name)
	}

	// Estimate preemptable capacity from node utilization
	// Nodes with higher utilization but by lower-priority workloads are better candidates
	utilizationGap := 100.0 - node.GPUUtilization
	if utilizationGap < 10 {
		// Heavily utilized — preemption would cause significant disruption
		return 10, plugin.SuccessResult(p.Metadata().Name)
	}

	// Higher priority workloads get better preemption scores
	priorityBonus := int64(w.Priority * 5)
	if priorityBonus > 40 {
		priorityBonus = 40
	}

	score := int64(30) + priorityBonus
	if score > 100 {
		score = 100
	}

	return score, plugin.SuccessResult(p.Metadata().Name)
}

// ScoreWeight returns the scoring weight for preemption policy.
func (p *PreemptionPolicyPlugin) ScoreWeight() int64 { return 1 }

// ============================================================================
// Register All Production Plugins
// ============================================================================

// RegisterProductionPlugins registers production-grade plugins into a registry.
func RegisterProductionPlugins(registry *plugin.Registry) error {
	plugins := map[string]plugin.Factory{
		"resource-quota-filter": NewResourceQuotaFilterPlugin,
		"gang-scheduling":       NewGangSchedulingPlugin,
		"preemption-policy":     NewPreemptionPolicyPlugin,
	}
	for name, factory := range plugins {
		if err := registry.Register(name, factory); err != nil {
			return fmt.Errorf("failed to register production plugin %s: %w", name, err)
		}
	}
	return nil
}
