// Package scheduler implements AI workload scheduling with GPU topology
// awareness, reinforcement learning-based optimization, fine-grained
// GPU sharing, heterogeneous resource management, and cost optimization.
package scheduler

// ScoreTopology calculates NVLink-aware topology score (0-100) using pure computation.
func ScoreTopology(topo *NodeGPUTopology, gpuCount int, requireNVLink bool, minBandwidth float64) float64 {
	score := 50.0 // Neutral baseline

	if topo == nil || len(topo.NVLinks) == 0 {
		if requireNVLink {
			return 80.0 // Boost if NVLink required but unavailable
		}
		return 50.0
	}

	// NVLink connectivity bonus (+20 points scaled by GPU count)
	pairCount := gpuCount * (gpuCount - 1) / 2
	if pairCount <= 0 {
		pairCount = 1
	}
	nvLinkBonus := float64(len(topo.NVLinks)) * 20.0 / float64(pairCount)
	if nvLinkBonus > 20.0 {
		nvLinkBonus = 20.0
	}
	score += nvLinkBonus

	// Bandwidth guarantee bonus (+5 points if met)
	if minBandwidth <= 0 || topo.MaxBandwidth >= minBandwidth {
		score += 5.0
	}

	// Clamp score to [0, 100]
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}
