// Package scheduler - FLIP Competitor Strategy Implementations
// Standalone implementations of 2026 GPU schedulers for fair benchmarking against DASP.
// These avoid import cycles by staying within the same package as PlacementStrategy interface.
package scheduler

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"math"
)

// ============================================================================
// HAMi Proxy (Device-Level Spreading)
// ============================================================================

// HamiProxy implements NVIDIA HAMi's binpack-first strategy with device-level spreading.
// This creates fragmentation on adversarial patterns due to contamination effect.
type HamiProxy struct{}

// Name returns the strategy identifier for reporting
func (HamiProxy) Name() string { return "HAMi" }

// Select finds best GPU by maximizing remaining free slices after placement (spreading strategy).
func (h *HamiProxy) Select(gpus []GPUTopology, p MIGSliceProfile, _ map[string]float64) (int, int, error) {
	bestGPU := -1
	bestStart := -1
	maxFree := -1 // We WANT maximum remaining = spreading!

	for i := range gpus {
		start := gpus[i].State.firstValidStart(p)
		if start < 0 {
			continue
		}

		free := gpus[i].State.remaining()
		if free > maxFree {
			maxFree = free
			bestGPU = i
			bestStart = start
		}
	}

	if bestGPU == -1 {
		return -1, -1, fmt.Errorf("no GPU available for HAMi placement: profile=%s", p.Name)
	}

	return bestGPU, bestStart, nil
}

// ============================================================================
// Volcano Proxy (Aggressive Binpacking)
// ============================================================================

// VolcanoProxy implements Volcano scheduler's aggressive binpack-first strategy.
// Packs workloads onto fewest GPUs, optimal for uniform but vulnerable to mixed demands.
type VolcanoProxy struct{}

// Name returns the strategy identifier
func (VolcanoProxy) Name() string { return "Volcano" }

// Select finds best GPU by minimizing remaining free slices (aggressive packing).
func (v *VolcanoProxy) Select(gpus []GPUTopology, p MIGSliceProfile, _ map[string]float64) (int, int, error) {
	bestGPU := -1
	bestStart := -1
	minRemaining := math.MaxInt32 // We WANT minimum remaining = binpack!

	for i := range gpus {
		start := gpus[i].State.firstValidStart(p)
		if start < 0 {
			continue
		}

		remaining := gpus[i].State.remaining()
		newRemaining := remaining - p.Size

		if newRemaining >= 0 && newRemaining < minRemaining {
			minRemaining = newRemaining
			bestGPU = i
			bestStart = start
		}
	}

	if bestGPU == -1 {
		return -1, -1, fmt.Errorf("no GPU available for Volcano placement: profile=%s", p.Name)
	}

	return bestGPU, bestStart, nil
}

// ============================================================================
// KubeEdge Proxy (Static Zoning)
// ============================================================================

// KubeEdgeProxy implements static zoning based on pre-configured ratio.
// Adaptive threshold tuning absent - uses deployment-time configuration only.
type KubeEdgeProxy struct {
	smallZoneRatio float64
	largeZoneStart int
}

// NewKubeEdgeProxy initializes with fixed small-zone ratio [0,1].
func NewKubeEdgeProxy(smallZoneRatio float64) *KubeEdgeProxy {
	if smallZoneRatio < 0 || smallZoneRatio > 1 {
		smallZoneRatio = 0.5
	}
	return &KubeEdgeProxy{
		smallZoneRatio: smallZoneRatio,
		largeZoneStart: -1,
	}
}

// Name returns strategy identifier
func (k *KubeEdgeProxy) Name() string { return "KubeEdge" }

// Select applies static zoning: routes small/large requests to different zones.
func (k *KubeEdgeProxy) Select(gpus []GPUTopology, p MIGSliceProfile, _ map[string]float64) (int, int, error) {
	n := len(gpus)
	if n == 0 {
		return -1, -1, fmt.Errorf("no GPU available for KubeEdge: cluster empty")
	}

	// Compute zone boundary if needed
	if k.largeZoneStart < 0 || k.largeZoneStart >= n {
		k.largeZoneStart = int(float64(n) * (1.0 - k.smallZoneRatio))
		if k.largeZoneStart < 0 {
			k.largeZoneStart = 0
		}
		if k.largeZoneStart >= n {
			k.largeZoneStart = n - 1
		}
	}

	isLarge := IsLargeProfile(p)
	var searchStart, searchEnd int

	if !isLarge {
		searchStart = 0
		searchEnd = k.largeZoneStart
	} else {
		searchStart = k.largeZoneStart
		searchEnd = n
	}

	if searchStart >= searchEnd {
		return -1, -1, fmt.Errorf("zone misconfigured: [%d,%d)", searchStart, searchEnd)
	}

	// Best-fit within zone
	bestGPU := -1
	bestStart := -1
	minRemaining := math.MaxInt32

	for i := searchStart; i < searchEnd; i++ {
		start := gpus[i].State.firstValidStart(p)
		if start < 0 {
			continue
		}

		remaining := gpus[i].State.remaining()
		newRemaining := remaining - p.Size

		if newRemaining >= 0 && newRemaining < minRemaining {
			minRemaining = newRemaining
			bestGPU = i
			bestStart = start
		}
	}

	if bestGPU == -1 {
		return -1, -1, fmt.Errorf("no GPU available in zone [%d,%d): profile=%s", searchStart, searchEnd, p.Name)
	}

	return bestGPU, bestStart, nil
}

// ============================================================================
// Execution Helpers for Competitors
// ============================================================================

// RunHamiScheduling executes workload trace through HAMi proxy and returns acceptance count.
func RunHamiScheduling(proxy *HamiProxy, initialCluster []GPUTopology, workload []common.BenchmarkWorkload) int {
	gpus := deepCopyCluster(initialCluster)
	accepted := 0

	for _, wl := range workload {
		profileName := ""
		if swl, ok := wl.(*common.SimpleBenchmarkWorkload); ok {
			profileName = swl.Profile()
		} else if bwl, ok := wl.(*common.BatchBenchmarkWorkload); ok {
			profileName = bwl.Profile()
		}
		
		if profileName == "" {
			continue
		}

		profile, err := profileByName(profileName)
		if err != nil {
			continue
		}

		gpuIdx, startIdx, err := proxy.Select(gpus, profile, nil)
		if err == nil && gpuIdx >= 0 {
			gpus[gpuIdx].State.Allocate(startIdx, profile.Size)
			accepted++
		}
	}

	return accepted
}

// RunVolcanoScheduling executes workload trace through Volcano proxy.
func RunVolcanoScheduling(proxy *VolcanoProxy, initialCluster []GPUTopology, workload []common.BenchmarkWorkload) int {
	gpus := deepCopyCluster(initialCluster)
	accepted := 0

	for _, wl := range workload {
		profileName := ""
		if swl, ok := wl.(*common.SimpleBenchmarkWorkload); ok {
			profileName = swl.Profile()
		} else if bwl, ok := wl.(*common.BatchBenchmarkWorkload); ok {
			profileName = bwl.Profile()
		}
		
		if profileName == "" {
			continue
		}

		profile, err := profileByName(profileName)
		if err != nil {
			continue
		}

		gpuIdx, startIdx, err := proxy.Select(gpus, profile, nil)
		if err == nil && gpuIdx >= 0 {
			gpus[gpuIdx].State.Allocate(startIdx, profile.Size)
			accepted++
		}
	}

	return accepted
}

// RunKubeEdgeScheduling executes workload trace through KubeEdge proxy.
func RunKubeEdgeScheduling(proxy *KubeEdgeProxy, initialCluster []GPUTopology, workload []common.BenchmarkWorkload) int {
	gpus := deepCopyCluster(initialCluster)
	accepted := 0

	for _, wl := range workload {
		profileName := ""
		if swl, ok := wl.(*common.SimpleBenchmarkWorkload); ok {
			profileName = swl.Profile()
		} else if bwl, ok := wl.(*common.BatchBenchmarkWorkload); ok {
			profileName = bwl.Profile()
		}

		if profileName == "" {
			continue
		}

		profile, err := profileByName(profileName)
		if err != nil {
			continue
		}

		gpuIdx, startIdx, err := proxy.Select(gpus, profile, nil)
		if err == nil && gpuIdx >= 0 {
			_ = gpus[gpuIdx].State.Allocate(startIdx, profile.Size)
			accepted++
		}
	}

	return accepted
}
