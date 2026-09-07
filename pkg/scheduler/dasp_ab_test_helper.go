package scheduler

import (
	"sync/atomic"
)

// DASPABTest implements A/B testing between primary and candidate placement strategies
type DASPABTest struct {
	primary       PlacementStrategy
	candidate     PlacementStrategy
	splitRatio    float64 // e.g., 0.10 = 10% of requests go to candidate
	counter       int64   // Request counter for stratified sampling
	primaryHits   int64   // Atomic counter for primary strategy selections
	candidateHits int64   // Atomic counter for candidate strategy selections
}

// NewDASPABTest creates an AB test harness
func NewDASPABTest(primary, candidate PlacementStrategy, splitRatio float64) *DASPABTest {
	return &DASPABTest{
		primary:    primary,
		candidate:  candidate,
		splitRatio: splitRatio,
		counter:    0,
	}
}

// Select routes requests proportionally for A/B testing using stratified sampling
func (a *DASPABTest) Select(gpus []GPUTopology, profile MIGSliceProfile, dist map[string]float64) (int, int, error) {
	reqNum := atomic.AddInt64(&a.counter, 1)

	// Use modulo for deterministic stratified sampling
	if float64(reqNum%100) < (a.splitRatio * 100) {
		gpuIdx, startSlice, err := a.candidate.Select(gpus, profile, dist)
		atomic.AddInt64(&a.candidateHits, 1)
		return gpuIdx, startSlice, err
	}

	gpuIdx, startSlice, err := a.primary.Select(gpus, profile, dist)
	atomic.AddInt64(&a.primaryHits, 1)
	return gpuIdx, startSlice, err
}

// GetStats returns current AB test statistics
func (a *DASPABTest) GetStats() ABTestStats {
	return ABTestStats{
		SplitRatio:    a.splitRatio,
		TotalRequests: atomic.LoadInt64(&a.counter),
		PrimaryHits:   atomic.LoadInt64(&a.primaryHits),
		CandidateHits: atomic.LoadInt64(&a.candidateHits),
	}
}

// Reset resets all counters in the AB test harness
func (a *DASPABTest) Reset() {
	atomic.StoreInt64(&a.counter, 0)
	atomic.StoreInt64(&a.primaryHits, 0)
	atomic.StoreInt64(&a.candidateHits, 0)
}

// ABTestStats captures metrics from AB testing session
type ABTestStats struct {
	SplitRatio    float64
	TotalRequests int64
	PrimaryHits   int64
	CandidateHits int64
}
