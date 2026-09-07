package builtin

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// NVLinkScorePlugin implements ScorePlugin interface for NVLink-aware GPU placement scoring
type NVLinkScorePlugin struct {
	plugin.BasePlugin
	discoverer *scheduler.TopologyDiscoverer
}

// NewNVLinkScorePlugin creates a new NVLink scoring plugin instance
func NewNVLinkScorePlugin() *NVLinkScorePlugin {
	return &NVLinkScorePlugin{
		BasePlugin: plugin.BasePlugin{Name: "nvlink-topology-scorer"},
		discoverer: scheduler.NewTopologyDiscoverer("", ""),
	}
}

// Score assigns a numeric score (0-100) to each candidate node based on NVLink topology
// Returns score + result indicating why this score was given
func (p *NVLinkScorePlugin) Score(ctx context.Context, state *scheduler.CycleState, workload *scheduler.WorkloadInfo, node *scheduler.NodeInfo) (int64, *plugin.Result) {
	// Get GPU requirement from workload
	gpuCount := int(workload.Spec.GPUResources.Requests["nvidia.com/gpu"])
	if gpuCount == 0 {
		// No GPU requirement, return neutral baseline score
		return 50, plugin.SuccessResult("no-gpu-request")
	}

	// Discover topology for this node
	topo, err := p.discoverer.DiscoverTopology(ctx, node.Name)
	if err != nil {
		// Graceful degradation: unknown topology gets neutral score
		return 50, plugin.SuccessResult("topo-unavailable")
	}

	if topo.TotalGPUs < uint32(gpuCount) {
		// Not enough GPUs on this node
		return 0, plugin.FailureResult("insufficient-gpus", fmt.Sprintf("node has %d, need %d", topo.TotalGPUs, gpuCount))
	}

	// Calculate NVLink-aware score using nvlink package
	requireNVLink := workload.Annotations.GetAnnotation("cloudai-fusion.io/require-nvlink", "false") == "true"
	minBandwidthStr := workload.Annotations.GetAnnotation("cloudai-fusion.io/min-bandwidth-gbps", "")
	minBandwidth := 0.0
	if minBandwidthStr != "" {
		fmt.Sscanf(minBandwidthStr, "%f", &minBandwidth)
	}

	score := scheduler.ScoreTopology(topo, gpuCount, requireNVLink, minBandwidth)

	return int64(score), plugin.SuccessResult(fmt.Sprintf("nvlink-topology-score=%.2f", score))
}

// ScoreWeight returns the weight of this scoring plugin (default 1)
// Higher weight means this plugin's score has more influence in blended final score
func (p *NVLinkScorePlugin) ScoreWeight() int64 {
	return 1 // Equal weighting with other plugins (e.g., resource-util, cost-scoring)
}

// Factory for registry registration
func NVLinkScoreFactory() (plugin.Plugin, error) {
	return NewNVLinkScorePlugin(), nil
}
