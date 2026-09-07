// Package competitors - KubeEdge Proxy Implementation
// Mimics KubeEdge's zone-based scheduling without adaptive threshold tuning.
package competitors

import (
	"fmt"
	"math"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// ============================================================================
// KubeEdge Proxy Implementation
// ============================================================================

// KubeEdgeProxy implements a faithful proxy of KubeEdge's static zoning strategy.
//
// Key behavioral characteristics:
//   - Zone-based partitioning: splits cluster into "hot" and "cold" zones statically
//   - No adaptive thresholds: zoning ratio fixed at deployment time, not demand-aware
//   - Pure greedy per-placement: no lookahead caching or demand forecasting
//
// Theoretical properties:
//   - Better than naive first-fit on mixed workloads (zones provide some isolation)
//   - Worse than DASP because zoning is STATIC, not adaptive to workload distribution
//   - Optimal only when actual distribution matches pre-configured zoning ratio
//
// Reference: https://github.com/kubeedge/kubeedge (Edge computing platform)
type KubeEdgeProxy struct {
	// Fixed zoning parameters (NOT adaptive like DASP)
	smallZoneRatio float64 // Fraction of GPUs reserved for small requests (default 0.5)
	largeZoneStart int     // Index where large zone begins
}

// NewKubeEdgeProxy initializes with static zoning parameters.
// smallZoneRatio should be in [0,1], represents assumed fraction of small requests.
func NewKubeEdgeProxy(smallZoneRatio float64) *KubeEdgeProxy {
	if smallZoneRatio < 0 || smallZoneRatio > 1 {
		smallZoneRatio = 0.5 // Default if invalid
	}

	return &KubeEdgeProxy{
		smallZoneRatio: smallZoneRatio,
		largeZoneStart: -1, // Computed lazily
	}
}

// Name returns the strategy identifier for reporting
func (k *KubeEdgeProxy) Name() string { return "KubeEdge" }

// Select applies KubeEdge's static zoning: split cluster into small/large zones,
// route requests accordingly without any adaptive reconfiguration.
//
// Algorithmic steps:
// 1. Compute large zone start index based on FIXED smallZoneRatio (no adaptation!)
// 2. Small requests (1g/2g): only search small zone (indices 0..largeZoneStart-1)
// 3. Large requests (3g/4g/7g): only search large zone (indices largeZoneStart..N-1)
// 4. Within zone, use best-fit to minimize fragmentation
//
// Critical limitation: zoning ratio never changes regardless of actual workload!
// This contrasts with DASP which adapts zoning based on real-time demand signals.
func (k *KubeEdgeProxy) Select(gpus []scheduler.GPUTopology, p scheduler.MIGSliceProfile, _ map[string]float64) (int, int, error) {
	n := len(gpus)
	if n == 0 {
		return -1, -1, fmt.Errorf("no GPU available for placement (KubeEdge): cluster empty")
	}

	// Initialize large zone boundary if needed
	if k.largeZoneStart < 0 || k.largeZoneStart >= n {
		k.largeZoneStart = int(float64(n) * (1.0 - k.smallZoneRatio))
		if k.largeZoneStart < 0 {
			k.largeZoneStart = 0
		}
		if k.largeZoneStart >= n {
			k.largeZoneStart = n - 1
		}
	}

	isLarge := scheduler.IsLargeProfile(p)
	var searchStart, searchEnd int

	if !isLarge {
		// Small request: restricted to small zone
		searchStart = 0
		searchEnd = k.largeZoneStart
	} else {
		// Large request: restricted to large zone
		searchStart = k.largeZoneStart
		searchEnd = n
	}

	if searchStart >= searchEnd {
		return -1, -1, fmt.Errorf("no GPU available for placement (KubeEdge): zone misconfigured")
	}

	// Best-fit within zone
	bestGPU := -1
	bestStart := -1
	minRemaining := math.MaxInt32

	for i := searchStart; i < searchEnd; i++ {
		start := gpus[i].State.FirstValidStart(p)
		if start < 0 {
			continue // Can't fit in this GPU's MIG state
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
		return -1, -1, fmt.Errorf(
			"no GPU available for placement (KubeEdge %s): profile=%s, zone=[%d,%d)",
			p.Name,
			isLarge,
			searchStart,
			searchEnd,
		)
	}

	return bestGPU, bestStart, nil
}

// ============================================================================
// KubeEdge Advanced Variants
// ============================================================================

// KubeEdgeAdaptive extends base KubeEdgeProxy with periodic zoning recalibration.
//
// Behavioral differences from base KubeEdgeProxy:
//   - Recalculates zoning ratio every N placements (configurable)
//   - Uses moving average of recent large/small request counts
//   - Still GREEDY per-placement (no lookahead like DASP)
//
// Use case: Edge clusters where deployment configuration can't predict demand well,
// but full DASP sophistication isn't justified by edge hardware constraints.
type KubeEdgeAdaptive struct {
	*KubeEdgeProxy          // Embed base for zoning logic
	recalibrateEvery      int             // How often to recalc zoning
	requestCount          int             // Total requests tracked
	smallRequestCount     int             // Small requests tracked
	largeRequestCount     int             // Large requests tracked
}

// NewKubeEdgeAdaptive initializes with adaptive zoning recalculation.
func NewKubeEdgeAdaptive(smallZoneRatio float64, recalibrateEvery int) *KubeEdgeAdaptive {
	base := NewKubeEdgeProxy(smallZoneRatio)

	if recalibrateEvery <= 0 {
		recalibrateEvery = 100 // Default
	}

	return &KubeEdgeAdaptive{
		KubeEdgeProxy:       base,
		recalibrateEvery:    recalibrateEvery,
		requestCount:        0,
		smallRequestCount:   0,
		largeRequestCount:   0,
	}
}

// Name returns the variant identifier
func (k *KubeEdgeAdaptive) Name() string { return "KubeEdge-Adaptive" }

// Select recalibrates zoning periodically before applying base selection logic.
func (k *KubeEdgeAdaptive) Select(gpus []scheduler.GPUTopology, p scheduler.MIGSliceProfile, dist map[string]float64) (int, int, error) {
	// Update tracking counters
	k.requestCount++
	if scheduler.IsLargeProfile(p) {
		k.largeRequestCount++
	} else {
		k.smallRequestCount++
	}

	// Recalculate zoning if trigger met
	if k.requestCount%k.recalibrateEvery == 0 && k.requestCount > 0 {
		actualSmallRatio := float64(k.smallRequestCount) / float64(k.requestCount)
		if actualSmallRatio > 0 && actualSmallRatio <= 1 {
			k.KubeEdgeProxy.smallZoneRatio = actualSmallRatio
			k.KubeEdgeProxy.largeZoneStart = -1 // Force recomputation
		}
	}

	// Delegate to base implementation
	return k.KubeEdgeProxy.Select(gpus, p, dist)
}

// ResetTracking clears request counters (for benchmark isolation)
func (k *KubeEdgeAdaptive) ResetTracking() {
	k.requestCount = 0
	k.smallRequestCount = 0
	k.largeRequestCount = 0
	k.KubeEdgeProxy.largeZoneStart = -1
}

// ============================================================================
// KubeEdge Naive Fallback (worst-case comparison)
// ============================================================================

// KubeEdgeNaive implements the most basic possible zoning-less scheduler.
// This serves as a lower bound showing why ANY zoning beats nothing.
type KubeEdgeNaive struct{}

// Name returns the strategy identifier
func (KubeEdgeNaive) Name() string { return "KubeEdge-Naive" }

// Select uses simple first-fit across all GPUs (no zoning whatsoever)
func (k *KubeEdgeNaive) Select(gpus []scheduler.GPUTopology, p scheduler.MIGSliceProfile, _ map[string]float64) (int, int, error) {
	for i := range gpus {
		start := gpus[i].State.FirstValidStart(p)
		if start >= 0 {
			return i, start, nil
		}
	}

	return -1, -1, fmt.Errorf("no GPU available for placement (KubeEdge Naive): profile=%s", p.Name)
}
