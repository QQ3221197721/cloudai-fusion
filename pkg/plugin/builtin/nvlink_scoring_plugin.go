package builtin

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/shared/types"
)

// NVLinkScorePlugin implements ScorePlugin interface for NVLink-aware GPU placement scoring
// Note: discoverer is no longer stored here to avoid circular dependency
// Topology discovery is now done by the scheduler and passed via CycleState
type NVLinkScorePlugin struct {
	plugin.BasePlugin
}

// NewNVLinkScorePlugin creates a new NVLink scoring plugin instance
func NewNVLinkScorePlugin() *NVLinkScorePlugin {
	return &NVLinkScorePlugin{
		BasePlugin: plugin.BasePlugin{Name: "nvlink-topology-scorer"},
	}
}

// Score assigns a numeric score (0-100) to each candidate node based on NVLink topology
// Uses topology from CycleState instead of discovering directly
func (p *NVLinkScorePlugin) Score(ctx context.Context, state *types.CycleState, workload *types.WorkloadInfo, node *types.NodeInfo) (int64, *plugin.Result) {
	// Get GPU requirement from workload (using shared types)
	gpuCount := workload.GPUCount
	if gpuCount == 0 {
		// No GPU requirement, return neutral baseline score
		return 50, plugin.SuccessResult("no-gpu-request")
	}

	// Get topology from CycleState (pre-computed by scheduler)
	topo, ok := state.GetTopology()
	if !ok || topo == nil {
		// Graceful degradation: unknown topology gets neutral score
		return 50, plugin.SuccessResult("topo-unavailable")
	}

	if int(topo.TotalGPUs) < gpuCount {
		// Not enough GPUs on this node
		return 0, plugin.ErrorResult("insufficient-gpus", fmt.Errorf("node has %d, need %d", topo.TotalGPUs, gpuCount))
	}

	// Check if NVLink is required
	requireNVLink := workload.RequireNVLink
	
	// Check minimum bandwidth from annotations (if available in workload metadata)
	minBandwidth := 0.0
	if workload.Labels != nil {
		if bwStr, exists := workload.Labels["cloudai-fusion.io/min-bandwidth-gbps"]; exists && bwStr != "" {
			fmt.Sscanf(bwStr, "%f", &minBandwidth)
		}
	}

	// Calculate NVLink-aware score using shared type utilities
	score := calculateNVLinkScore(topo, gpuCount, requireNVLink, minBandwidth)

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

// calculateNVLinkScore computes the NVLink-aware topology score based on:
// - Whether NVLink connectivity is required
// - Total bandwidth available
// - Number of GPUs needed
//
// Scoring algorithm:
//   - 0-50 points for basic GPU availability
//   - 0-50 points for NVLink quality (bonus for full-mesh, penalties for partial/no-NVLink)
func calculateNVLinkScore(topo *types.NodeGPUTopology, gpuCount int, requireNVLink bool, minBandwidth float64) float64 {
	// Base score for having enough GPUs (up to 50 points)
	baseScore := 50.0
	if int(topo.TotalGPUs) >= gpuCount {
		baseScore = 50.0
	} else {
		// Proportional score if not enough GPUs
		baseScore = 50.0 * float64(topo.TotalGPUs) / float64(gpuCount)
		if baseScore > 50 {
			baseScore = 50
		}
	}

	// NVLink bonus/penalty (up to 50 points)
	nvlinkScore := 0.0

	if !topo.HasNVLink {
		if requireNVLink {
			// Hard penalty: no NVLink when required
			nvlinkScore = 0.0
		} else {
			// Neutral score when NVLink not required but also not present
			nvlinkScore = 25.0
		}
	} else {
		// Check bandwidth requirements
		totalBandwidth := calculateTotalBandwidth(topo.Connections, gpuCount)
		
		if minBandwidth > 0 && totalBandwidth < minBandwidth {
			// Not meeting minimum bandwidth requirement
			if requireNVLink {
				nvlinkScore = 10.0 // Low but non-zero
			} else {
				nvlinkScore = 20.0
			}
		} else if topo.HasNVSwitch || topo.FullMesh() {
			// Full-mesh with NVSwitch - best case scenario
			nvlinkScore = 50.0
		} else if len(topo.Connections) >= (gpuCount-1)*gpuCount/2 {
			// Nearly full connectivity
			nvlinkScore = 40.0
		} else if totalBandwidth >= 100.0 {
			// Good bandwidth
			nvlinkScore = 30.0
		} else {
			// Partial NVLink
			nvlinkScore = 20.0
		}
	}

	return baseScore + nvlinkScore
}

// calculateTotalBandwidth computes total NVLink bandwidth across specified GPUs
func calculateTotalBandwidth(connections []types.NVLinkConnection, gpuIndices int) float64 {
	if len(connections) == 0 {
		return 0
	}

	totalBW := 0.0
	for _, conn := range connections {
		if conn.GPU1Index < gpuIndices && conn.GPU2Index < gpuIndices {
			totalBW += conn.BandwidthGB
		}
	}
	return totalBW
}

// FullMesh checks if the topology has full mesh connectivity (all-to-all)
func (n *NodeGPUTopology) FullMesh() bool {
	if n.TotalGPUs <= 1 {
		return true
	}
	expectedEdges := (n.TotalGPUs * (n.TotalGPUs - 1)) / 2
	return len(n.NVLinks) >= expectedEdges
}

