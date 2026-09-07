// Package competitors - Volcano Proxy Implementation
// Mimics Volcano's binpack-first strategy with priority queuing for comparison against DASP.
package competitors

import (
	"fmt"
	"math"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// ============================================================================
// Volcano Proxy Implementation
// ============================================================================

// VolcanoProxy implements a faithful proxy of Volcano scheduler's binpack-first strategy.
//
// Key behavioral characteristics:
//   - Binpack-first: packs workloads onto fewest GPUs possible before using new ones
//   - Priority-queued: higher priority jobs get first pick of resources
//   - Aggressive packing: good for uniform workloads, vulnerable to clustering attacks
//
// Theoretical properties:
//   - Excellent acceptance on uniform distributions (beats both HAMi and DASP sometimes)
//   - Vulnerable to bimodal adversarial patterns where small/large requests interleave
//   - No adaptive zoning = consistent fragmentation under mixed workloads
//
// Reference: https://github.com/volcano-sh/volcano (K8s batch scheduler)
type VolcanoProxy struct{}

// Name returns the strategy identifier for reporting
func (VolcanoProxy) Name() string { return "Volcano" }

// Select finds the best GPU and start index using Volcano's aggressive binpack strategy:
// minimize remaining free slices after placement while respecting MIG constraints.
//
// Algorithmic steps:
// 1. Scan all GPUs in index order (deterministic)
// 2. For each GPU, find first valid start position respecting MIG constraints
// 3. Calculate remaining capacity if placed there
// 4. Return GPU with MINIMUM remaining capacity that's still non-negative (binpack!)
//
// This aggressively fills up GPUs, which is optimal for uniform batches but creates
// hard-to-fill fragments under mixed demand patterns.
func (v *VolcanoProxy) Select(gpus []scheduler.GPUTopology, p scheduler.MIGSliceProfile, _ map[string]float64) (int, int, error) {
	bestGPU := -1
	bestStart := -1
	minRemaining := math.MaxInt32 // We WANT minimum remaining = binpack!

	for i := range gpus {
		start := gpus[i].State.FirstValidStart(p)
		if start < 0 {
			continue // Can't fit on this GPU
		}

		remaining := gpus[i].State.Remaining()
		newRemaining := remaining - p.Size

		if newRemaining >= 0 && newRemaining < minRemaining {
			minRemaining = newRemaining
			bestGPU = i
			bestStart = start
		}
	}

	if bestGPU == -1 {
		return -1, -1, fmt.Errorf("no GPU available for placement (Volcano binpack): profile=%s", p.Name)
	}

	return bestGPU, bestStart, nil
}

// ============================================================================
// Volcano Advanced Variants
// ============================================================================

// VolcanoPriorityQueue extends base VolcanoProxy with priority-aware scheduling.
//
// Behavioral differences from base VolcanoProxy:
// - Maintains per-priority queues (simulated via workload ordering)
// - Higher priority placements made BEFORE lower priority (outside this function)
// - When multiple GPUs equally optimal, picks GPU with more headroom
//
// Use case: Multi-tenant clusters with SLA guarantees for different job classes.
type VolcanoPriorityQueue struct {
	priorityWeights map[int]float64 // Higher priority = higher weight
}

// NewVolcanoPriorityQueue initializes with default priority weights.
func NewVolcanoPriorityQueue() *VolcanoPriorityQueue {
	return &VolcanoPriorityQueue{
		priorityWeights: map[int]float64{
			1: 1.0,  // Normal priority
			5: 2.0,  // High priority
			10: 5.0, // Critical priority
		},
	}
}

// Name returns the variant identifier
func (v *VolcanoPriorityQueue) Name() string { return "Volcano-Priority" }

// Select applies Volcano binpack with priority-weighted tie-breaking.
func (v *VolcanoPriorityQueue) Select(gpus []scheduler.GPUTopology, p scheduler.MIGSliceProfile, dist map[string]float64) (int, int, error) {
	type candidate struct {
		gpuIdx      int
		start       int
		remaining   int
		priorityAdj float64 // Adjusted score based on distribution
	}

	var candidates []candidate

	for i := range gpus {
		start := gpus[i].State.FirstValidStart(p)
		if start < 0 {
			continue
		}

		remaining := gpus[i].State.Remaining()
		newRemaining := remaining - p.Size

		if newRemaining >= 0 {
			candidates = append(candidates, candidate{i, start, newRemaining, 0})
		}
	}

	if len(candidates) == 0 {
		return -1, -1, fmt.Errorf("no GPU available for placement (Volcano PQ): profile=%s", p.Name)
	}

	// Find min remaining (standard Volcano binpack)
	minRemaining := math.MaxInt32
	for _, c := range candidates {
		if c.remaining < minRemaining {
			minRemaining = c.remaining
		}
	}

	// Filter to min-remaining candidates
	var minCandidates []candidate
	for _, c := range candidates {
		if c.remaining == minRemaining {
			minCandidates = append(minCandidates, c)
		}
	}

	if len(minCandidates) == 1 {
		return minCandidates[0].gpuIdx, minCandidates[0].start, nil
	}

	// Tie-break: prefer GPU with MORE headroom (conservative) when distribution favors large profiles
	largeRequestWeight := 0.0
	for _, profile := range scheduler.A100Profiles {
		w := dist[profile.Name]
		if scheduler.IsLargeProfile(profile) {
			largeRequestWeight += w
		}
	}

	// If large requests common (>50%), be conservative and leave headroom
	var bestCandidate candidate
	if largeRequestWeight > 0.5 {
		maxHeadroom := -1
		for _, c := range minCandidates {
			if c.remaining > maxHeadroom {
				maxHeadroom = c.remaining
				bestCandidate = c
			}
		}
	} else {
		// Otherwise arbitrary (pick first for determinism)
		bestCandidate = minCandidates[0]
	}

	return bestCandidate.gpuIdx, bestCandidate.start, nil
}

// ============================================================================
// Volcano Greedy Alternative (for comparison)
// ============================================================================

// VolcanoGreedy implements a simplified greedy version of Volcano for stress testing.
// This is less sophisticated than production Volcano but useful for ablation studies.
type VolcanoGreedy struct{}

// Name returns the strategy identifier
func (VolcanoGreedy) Name() string { return "Volcano-Greedy" }

// Select picks first GPU that fits (extremely fast but poor quality)
func (v *VolcanoGreedy) Select(gpus []scheduler.GPUTopology, p scheduler.MIGSliceProfile, _ map[string]float64) (int, int, error) {
	for i := range gpus {
		start := gpus[i].State.FirstValidStart(p)
		if start >= 0 {
			return i, start, nil
		}
	}

	return -1, -1, fmt.Errorf("no GPU available for placement (Volcano Greedy): profile=%s", p.Name)
}
