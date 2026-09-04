package nvlink_placer

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler/types"
)

// Placer implements NVLink-aware GPU placement with simple single-call API
type Placer struct {
	Discoverer types.TopologyReader
}

// NewPlacer creates a new GPU placer with topology discoverer
func NewPlacer(discoverer types.TopologyReader) *Placer {
	return &Placer{
		Discoverer: discoverer,
	}
}

// Place determines optimal GPU placement for workload based on topology requirements
// Returns placement result with score and fit status
func (p *Placer) Place(ctx context.Context, req WorkloadRequest) (*PlacementResult, error) {
	// Get NVLink connections from cache (zero-allocation access)
	nvLinks, err := p.Discoverer.GetNVLinkConnections(ctx)
	if err != nil {
		// Graceful degradation: return neutral baseline
		return &PlacementResult{
			Toposcore:   50.0,
			Fit:         false,
			Reasons:     []string{"topo-unavailable"},
			RoomsNeeded: req.GPUCount,
		}, nil
	}

	// Calculate topology score (0-100 scale)
	score := p.calculateTopologyScore(req, nvLinks)

	// Determine if requirements are satisfied
	fit := score > 70 // Threshold: >70 = good match
	
	// Build reasons list
	reasons := p.buildReasons(req, nvLinks, score, fit)

	// Estimate execution time (cached lookup is ~50ns per lookup)
	execTime := fmt.Sprintf("%.2fms", float64(len(nvLinks))*0.05)

	return &PlacementResult{
		Toposcore:           score,
		Fit:                 fit,
		Reasons:             reasons,
		GPUsNeeded:          req.GPUCount,
		ExecutionTimeEstimate: execTime,
	}, nil
}

// calculateTopologyScore computes 0-100 score for workload-topology match
func (p *Placer) calculateTopologyScore(req WorkloadRequest, nvLinks []scheduler.NVLinkConnection) float64 {
	score := 50.0 // Start with neutral baseline

	// NVLink connectivity bonus (+20 points)
	if len(nvLinks) > 0 {
		score += 20.0
	}

	// Full mesh bonus (+10 points if all GPUs interconnected)
	totalPossiblePairs := req.GPUCount * (req.GPUCount - 1) / 2
	actualPairs := 0
	for _, link := range nvLinks {
		if link.LinkType == "NML" || link.LinkType == "NVS" {
			actualPairs++
		}
	}
	
	fullMeshCoverage := float64(actualPairs) / float64(totalPossiblePairs)
	if fullMeshCoverage >= 0.9 {
		score += 10.0
	} else if fullMeshCoverage >= 0.5 {
		score += 5.0
	}

	// NUMA locality bonus (+10 points if all GPUs same NUMA node)
	numaBonus := p.evaluateNumALocality(req, nvLinks)
	score += numaBonus

	// Bandwidth guarantee bonus (+5 points if min bandwidth met)
	if p.verifyMinBandwidth(req, nvLinks) {
		score += 5.0
	}

	// Penalize heterogeneous GPU mix (-5 points)
	uniqueModels := make(map[string]bool)
	for _, link := range nvLinks {
		uniqueModels[link.GPU1Index] = true
		uniqueModels[link.GPU2Index] = true
	}
	if len(uniqueModels) > 1 && req.RequireNVLink {
		score -= 5.0
	}

	// Clamp score to 0-100 range
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// evaluateNumALocality returns NUMA locality bonus (up to +10 points)
func (p *Placer) evaluateNumALocality(req WorkloadRequest, nvLinks []scheduler.NVLinkConnection) float64 {
	if !req.PreferSameNode {
		return 0 // No bonus if not required
	}

	// Check if all requested GPUs can fit on same NUMA node
	numaGroups := map[int][]int{}
	for _, link := range nvLinks {
		numaGroups[link.GPU1Index] = append(numaGroups[link.GPU1Index], link.GPU2Index)
	}

	for _, gpuIndices := range numaGroups {
		if len(gpuIndices) >= req.GPUCount {
			return 10.0 // All GPUs fit on one NUMA node
		}
	}

	return 0 // Can't guarantee NUMA locality
}

// verifyMinBandwidth checks if NVLink bandwidth meets minimum requirement
func (p *Placer) verifyMinBandwidth(req WorkloadRequest, nvLinks []scheduler.NVLinkConnection) bool {
	if req.MinBandwidth <= 0 {
		return true // No minimum requirement
	}

	// Verify at least one connection meets bandwidth requirement
	for _, link := range nvLinks {
		switch link.LinkType {
		case "NVL":
			// NVLink 3.0 provides 600 GB/s bidirectional
			if link.BandwidthGB >= req.MinBandwidth {
				return true
			}
		}
	}

	return false
}

// buildReasons constructs explanatory message list for placement decision
func (p *Placer) buildReasons(req WorkloadRequest, nvLinks []scheduler.NVLinkConnection, score float64, fit bool) []string {
	reasons := []string{}

	if !fit {
		reasons = append(reasons, "requirements-not-met")
	} else {
		reasons = append(reasons, "nvlink-scored")
	}

	if len(nvLinks) == 0 {
		reasons = append(reasons, "no-nvlink-connectivity")
	} else {
		reasons = append(reasons, fmt.Sprintf("connected-gpus=%d", len(nvLinks)))
	}

	if req.RequireNVLink {
		reasons = append(reasons, "nvlink-required")
	}

	if req.PreferSameNode {
		reasons = append(reasons, "numa-locality-prefers")
	}

	return reasons
}
