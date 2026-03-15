// Package builtin provides built-in plugins for the CloudAI Fusion scheduler
// framework. These serve as both default implementations and reference
// examples for third-party plugin developers.
package builtin

import (
	"context"
	"math"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// ============================================================================
// GPUResourceFilterPlugin — filters nodes without enough GPUs
// ============================================================================

// GPUResourceFilterPlugin rejects nodes that lack the required GPU count.
type GPUResourceFilterPlugin struct {
	plugin.BasePlugin
}

// NewGPUResourceFilterPlugin creates the default GPU filter.
func NewGPUResourceFilterPlugin() *GPUResourceFilterPlugin {
	return &GPUResourceFilterPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:            "gpu-resource-filter",
			Version:         "1.0.0",
			Description:     "Filters out nodes with insufficient free GPUs",
			ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerFilter},
			Priority:        10, // run early
			Tags:            map[string]string{"builtin": "true"},
		}),
	}
}

func (p *GPUResourceFilterPlugin) Filter(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) *plugin.Result {
	if w.GPUCount > 0 && node.GPUFree < w.GPUCount {
		return plugin.NewResult(plugin.Unschedulable, p.Metadata().Name,
			"node has insufficient free GPUs")
	}
	return plugin.SuccessResult(p.Metadata().Name)
}

// Factory for registry registration.
func GPUResourceFilterFactory() (plugin.Plugin, error) {
	return NewGPUResourceFilterPlugin(), nil
}

// ============================================================================
// TaintTolerationFilterPlugin — filters nodes with unmatched taints
// ============================================================================

// TaintTolerationFilterPlugin rejects nodes whose taints are not tolerated.
type TaintTolerationFilterPlugin struct {
	plugin.BasePlugin
}

// NewTaintTolerationFilterPlugin creates the taint filter.
func NewTaintTolerationFilterPlugin() *TaintTolerationFilterPlugin {
	return &TaintTolerationFilterPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:            "taint-toleration-filter",
			Version:         "1.0.0",
			Description:     "Filters nodes with taints that workload does not tolerate",
			ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerFilter},
			Priority:        20,
			Tags:            map[string]string{"builtin": "true"},
		}),
	}
}

func (p *TaintTolerationFilterPlugin) Filter(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) *plugin.Result {
	if len(node.Taints) == 0 {
		return plugin.SuccessResult(p.Metadata().Name)
	}
	// Simple: if workload has no tolerations label, reject tainted nodes
	if _, hasToleration := w.Labels["tolerations"]; !hasToleration && len(node.Taints) > 0 {
		return plugin.NewResult(plugin.Unschedulable, p.Metadata().Name,
			"node has taints and workload has no tolerations")
	}
	return plugin.SuccessResult(p.Metadata().Name)
}

func TaintTolerationFilterFactory() (plugin.Plugin, error) {
	return NewTaintTolerationFilterPlugin(), nil
}

// ============================================================================
// DefaultScorePlugin — multi-factor scoring (GPU util + memory + cost)
// ============================================================================

// DefaultScorePlugin implements the default scheduling scoring formula.
type DefaultScorePlugin struct {
	plugin.BasePlugin
}

// NewDefaultScorePlugin creates the default scorer.
func NewDefaultScorePlugin() *DefaultScorePlugin {
	return &DefaultScorePlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:            "default-score",
			Version:         "1.0.0",
			Description:     "Multi-factor scoring: GPU utilization balance + memory headroom",
			ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerScore},
			Priority:        50,
			Tags:            map[string]string{"builtin": "true"},
		}),
	}
}

func (p *DefaultScorePlugin) Score(_ context.Context, _ *plugin.CycleState, _ *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	// Prefer nodes with lower GPU utilization (more headroom).
	gpuScore := int64((100.0 - node.GPUUtilization))

	// Memory headroom bonus.
	memScore := int64(0)
	if node.MemoryTotalBytes > 0 {
		freeRatio := float64(node.MemoryFreeBytes) / float64(node.MemoryTotalBytes)
		memScore = int64(freeRatio * 100)
	}

	total := (gpuScore*7 + memScore*3) / 10 // weighted
	if total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}
	return total, plugin.SuccessResult(p.Metadata().Name)
}

func (p *DefaultScorePlugin) ScoreWeight() int64 { return 1 }

func DefaultScoreFactory() (plugin.Plugin, error) {
	return NewDefaultScorePlugin(), nil
}

// ============================================================================
// CostAwareScorePlugin — prefer cheaper nodes
// ============================================================================

// CostAwareScorePlugin scores nodes based on cost efficiency.
type CostAwareScorePlugin struct {
	plugin.BasePlugin
}

// NewCostAwareScorePlugin creates the cost scorer.
func NewCostAwareScorePlugin() *CostAwareScorePlugin {
	return &CostAwareScorePlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:            "cost-aware-score",
			Version:         "1.0.0",
			Description:     "Scores nodes by cost efficiency — lower cost-per-GPU-hour wins",
			ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerScore},
			Priority:        60,
			Tags:            map[string]string{"builtin": "true"},
		}),
	}
}

func (p *CostAwareScorePlugin) Score(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	// If workload has a max cost constraint, reject expensive nodes.
	if w.MaxCostPerHour > 0 && node.CostPerHour > w.MaxCostPerHour {
		return 0, plugin.SuccessResult(p.Metadata().Name)
	}

	// Score inversely proportional to cost. Cap at $100/hr.
	maxCost := 100.0
	if node.CostPerHour <= 0 {
		return 80, plugin.SuccessResult(p.Metadata().Name) // unknown cost → neutral score
	}
	score := int64((1.0 - math.Min(node.CostPerHour, maxCost)/maxCost) * 100)
	if score < 0 {
		score = 0
	}

	// Spot instance bonus.
	if node.IsSpot {
		score = int64(math.Min(float64(score)+15, 100))
	}
	return score, plugin.SuccessResult(p.Metadata().Name)
}

func (p *CostAwareScorePlugin) ScoreWeight() int64 { return 1 }

func CostAwareScoreFactory() (plugin.Plugin, error) {
	return NewCostAwareScorePlugin(), nil
}

// ============================================================================
// TopologyAwareScorePlugin — prefer nodes with NVLink topology
// ============================================================================

// TopologyAwareScorePlugin scores nodes based on GPU interconnect topology.
type TopologyAwareScorePlugin struct {
	plugin.BasePlugin
}

// NewTopologyAwareScorePlugin creates the topology scorer.
func NewTopologyAwareScorePlugin() *TopologyAwareScorePlugin {
	return &TopologyAwareScorePlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:            "topology-aware-score",
			Version:         "1.0.0",
			Description:     "Scores nodes by GPU interconnect topology (NVLink/NVSwitch)",
			ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerScore},
			Priority:        55,
			Dependencies:    []string{"default-score"},
			Tags:            map[string]string{"builtin": "true"},
		}),
	}
}

func (p *TopologyAwareScorePlugin) Score(_ context.Context, _ *plugin.CycleState, w *plugin.WorkloadInfo, node *plugin.NodeInfo) (int64, *plugin.Result) {
	if !w.RequireNVLink {
		return 50, plugin.SuccessResult(p.Metadata().Name) // neutral
	}
	// TopologyScore is pre-computed by the topology discoverer [0, 1.0].
	score := int64(node.TopologyScore * 100)
	if score > 100 {
		score = 100
	}
	return score, plugin.SuccessResult(p.Metadata().Name)
}

func (p *TopologyAwareScorePlugin) ScoreWeight() int64 { return 2 } // higher weight for topology

func TopologyAwareScoreFactory() (plugin.Plugin, error) {
	return NewTopologyAwareScorePlugin(), nil
}

// ============================================================================
// RegisterDefaults registers all built-in plugins into a registry
// ============================================================================

// RegisterDefaults registers all built-in plugins into the given registry.
func RegisterDefaults(reg *plugin.Registry) error {
	defaults := map[string]plugin.Factory{
		"gpu-resource-filter":    GPUResourceFilterFactory,
		"taint-toleration-filter": TaintTolerationFilterFactory,
		"default-score":          DefaultScoreFactory,
		"cost-aware-score":       CostAwareScoreFactory,
		"topology-aware-score":   TopologyAwareScoreFactory,
	}

	for name, factory := range defaults {
		if err := reg.Register(name, factory); err != nil {
			return err
		}
	}
	return nil
}
