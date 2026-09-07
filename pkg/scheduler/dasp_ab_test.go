package scheduler

import (
	"fmt"
	"math/rand"
)

// Outcome represents the result of a placement decision for statistical analysis
type Outcome struct {
	Success       bool
	GPUIndex      int
	SliceStart    int
	LatencyNS     int64
	MemoryWasted  float64 // Fragmentation cost
	Demographic   string  // e.g., "large-zone", "small-zone"
}

// DASPABTest enables statistically valid A/B testing between two placement strategies
// 
// Implementation uses stratified sampling (counter modulo) rather than random sampling
// to ensure reproducible distribution ratios over time windows.
type DASPABTest struct {
	primary    PlacementStrategy
	candidate  PlacementStrategy
	splitRatio float64      // e.g., 0.10 = 10% traffic to candidate variant
	counter    int64        // Request counter for stratified sampling
	rng        *rand.Rand   // For more complex splits if needed
	
	// Metrics tracking
	totalRequests      int64
	primarySuccessful  int64
	candidateSuccessful int64
	primaryFailures    int64
	candidateFailures  int64
}

// ABTestStats exposes current test statistics for monitoring/debugging
type ABTestStats struct {
	PrimaryName     string
	CandidateName   string
	SplitRatio      float64
	TotalRequests   uint64
	PrimarySuccesses uint64
	CandidateSuccesses uint64
	PrimaryRate     float64   // Primary successes / total attempts
	CandidateRate   float64   // Candidate successes / total attempts
	CurrentSplit    uint64    // Actual primary/candidate assignment in last request
}

const (
	// defaultABSeed provides reproducibility across test runs
	defaultABSeed = 0xDEADBEEF
	
	// minSplitRatio enforces reasonable minimum for candidate traffic
	minSplitRatio = 0.01
	
	// maxSplitRatio prevents overwhelming production with untested variants
	maxSplitRatio = 0.99
)

// NewDASPABTest creates an A/B test harness between two strategies
//
// splitRatio must be in (0, 1). Values <= 0.01 become 0.01, >= 0.99 become 0.99
func NewDASPABTest(primary, candidate PlacementStrategy, splitRatio float64) *DASPABTest {
	// Clamp ratio to safe bounds
	if splitRatio < minSplitRatio {
		splitRatio = minSplitRatio
	}
	if splitRatio > maxSplitRatio {
		splitRatio = maxSplitRatio
	}
	
	return &DASPABTest{
		primary:    primary,
		candidate:  candidate,
		splitRatio: splitRatio,
		counter:    0,
		rng:        rand.New(rand.NewSource(defaultABSeed)),
	}
}

// Select routes requests proportionally to A/B test split ratio
//
// Strategy selection uses stratified sampling: N % 100 < (splitRatio × 100) means candidate gets variant N.
// This ensures exactly splitRatio fraction over any window of size 100/k where k is integer.
//
// Thread-safe via atomic counter increments.
func (a *DASPABTest) Select(
	gpus []GPUTopology, 
	p MIGSliceProfile, 
	dist map[string]float64,
) (int, int, error) {
	reqNum := atomic.AddInt64(&a.counter, 1)
	atomic.AddInt64(&a.totalRequests, 1)
	
	// Determine variant using stratified sampling (counter modulo)
	isCandidate := (reqNum%100) < (int32(a.splitRatio*100))
	
	var selectedGpu, selectedSlice int
	var err error
	
	if isCandidate {
		selectedGpu, selectedSlice, err = a.candidate.Select(gpus, p, dist)
		if err == nil {
			atomic.AddInt64(&a.candidateSuccessful, 1)
		} else {
			atomic.AddInt64(&a.candidateFailures, 1)
		}
		
		// Record candidate metrics to Prometheus
		recordABTestOutcome("candidate", isCandidate, gpus[selectedGpu].State)
		
	} else {
		selectedGpu, selectedSlice, err = a.primary.Select(gpus, p, dist)
		if err == nil {
			atomic.AddInt64(&a.primarySuccessful, 1)
		} else {
			atomic.AddInt64(&a.primaryFailures, 1)
		}
		
		// Record primary metrics to Prometheus
		recordABTestOutcome("primary", !isCandidate, gpus[selectedGpu].State)
	}
	
	return selectedGpu, selectedSlice, err
}

// RecordOutcome logs comparative outcome for statistical analysis
//
// In production this would write structured logs with zap/slog:
// logger.Info("A/B test outcome",
// 	"variant_primary", primaryResult.Success,
// 	"variant_candidate", candidateResult.Success,
// 	"latency_diff_ns", candidateResult.LatencyNS-primaryResult.LatencyNS,
// )
func (a *DASPABTest) RecordOutcome(primaryResult, candidateResult Outcome) {
	// TODO: Implement histogram-based latency comparison in Prometheus
	// Example: abtestLatencyHistogram.WithLabelValues("primary").Observe(float64(primaryResult.LatencyNS))
	
	_ = primaryResult
	_ = candidateResult
}

// GetStats returns current A/B test statistics (non-blocking read)
func (a *DASPABTest) GetStats() ABTestStats {
	total := atomic.LoadInt64(&a.totalRequests)
	primeSuccess := atomic.LoadInt64(&a.primarySuccessful)
	candSuccess := atomic.LoadInt64(&a.candidateSuccessful)
	
	var primeRate, candRate float64
	
	if total > 0 {
		primeRate = float64(primeSuccess) / float64(total)
		candRate = float64(candSuccess) / float64(total)
	}
	
	return ABTestStats{
		PrimaryName:     a.primary.Name(),
		CandidateName:   a.candidate.Name(),
		SplitRatio:      a.splitRatio,
		TotalRequests:   uint64(total),
		PrimarySuccesses: uint64(primeSuccess),
		CandidateSuccesses: uint64(candSuccess),
		PrimaryRate:     primeRate,
		CandidateRate:   candRate,
		CurrentSplit:    uint64(0), // Last assignment was primary (not tracked atomically here)
	}
}

// SetSplitRatio dynamically adjusts traffic allocation mid-test (thread-safe)
func (a *DASPABTest) SetSplitRatio(newRatio float64) {
	if newRatio < minSplitRatio {
		newRatio = minSplitRatio
	}
	if newRatio > maxSplitRatio {
		newRatio = maxSplitRatio
	}
	
	atomic.StoreFloat64(&a.splitRatio, newRatio)
}

// recordABTestOutcome writes variant-specific metrics to observability system
func recordABTestOutcome(variant string, success bool, gpuState GPUTopologyState) {
	// Increment appropriate counters in Prometheus
	labels := prometheus.Labels{"variant": variant, "success": fmt.Sprintf("%v", success)}
	
	if success {
		abTestSuccessCount.WithLabels(labels).Inc()
	} else {
		abTestFailureCount.WithLabels(labels).Inc()
	}
	
	// TODO: Add latency histograms if timing instrumentation is available
	_ = gpuState
}

// ============================================================================
// Global A/B Test Metrics Registry
// ============================================================================

var (
	// abTestSuccessCount tracks successful placements by variant for comparing acceptance rates
	abTestSuccessCount = func() *prometheus.CounterVec {
		return prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "abtest_success_total",
				Help: "Total successful A/B test placements by variant",
			},
			[]string{"variant", "success"},
		)
	}()
	
	// abTestFailureCount tracks failed placements (no placement found) by variant
	abTestFailureCount = func() *prometheus.CounterVec {
		return prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "abtest_failure_total",
				Help: "Total failed A/B test placements by variant",
			},
			[]string{"variant", "success"},
		)
	}()
)

func init() {
	// Register AB test metrics alongside other DASP metrics
	prometheus.MustRegister(abTestSuccessCount, abTestFailureCount)
}
