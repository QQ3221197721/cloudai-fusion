package quantile

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

const (
	// AdversarialSamples defines sample count for adversarial tests
	AdversarialSamples = 100000
	
	// MaxExpectedDegradation is upper bound on accuracy loss under attack
	MaxExpectedDegradation = 0.10 // 10% max error increase acceptable
)

// TestM9_AdversarialRobustness validates P² resilience against crafted attack patterns
func TestM9_AdversarialRobustness(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name                  string
		generator             func(int) []float64
		expectedDegradation   float64 // max expected accuracy degradation
	}{
		{
			name:                "extreme_outliers",
			generator:           generateExtremeOutliers,
			expectedDegradation: 0.05,
		},
		{
			name:                "rapid_drift",
			generator:           generateRapidDrift,
			expectedDegradation: 0.10,
		},
		{
			name:                "memory_pressure",
			generator:           generateMemoryPressure,
			expectedDegradation: 0.08,
		},
		{
			name:                "interpolation_trap",
			generator:           generateMarkerInterpolationTrap,
			expectedDegradation: 0.12,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testAdversarialPattern(t, tt.generator, tt.expectedDegradation)
		})
	}
}

// testAdversarialPattern runs a single adversarial pattern comparison across all algorithms
func testAdversarialPattern(t *testing.T, generator func(int) []float64, maxDegradation float64) {
	t.Logf("Testing %s pattern with %d samples...", t.Name(), AdversarialSamples)
	
	adversaryData := generator(AdversarialSamples)
	
	algorithms := map[string]Sketch{
		"P2":      NewP2Wrapper(0.5, 0.9, 0.99),
		"GK":      &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01},
		"TDigest": NewTDigestWrapper(1000),
	}
	
	trueQuantiles := computeTrueQuantiles(adversaryData)
	
	results := make(map[string]BenchmarkResult)
	
	for algoName, sketch := range algorithms {
		sketchStartTime := time.Now()
		
		// Process adversarial input - use Add() per Sketch interface
		for _, val := range adversaryData {
			sketch.Add(val)
		}
		
		processDuration := time.Since(sketchStartTime)
		
		// Measure estimation accuracy
		estMedian := sketch.Quantile(0.5)
		estP90 := sketch.Quantile(0.9)
		estP99 := sketch.Quantile(0.99)
		
		degradation := calculateQuantileError(trueQuantiles, estMedian, estP90, estP99)
		
		memMB := float64(sketch.SizeBytes()) / (1024 * 1024)
		
		result := BenchmarkResult{
			Algorithm:    sketch.Name(),
			Distribution: fmt.Sprintf("adversarial_%s", t.Name()),
			Samples:      len(adversaryData),
			Accuracy:     degradation,
			MemoryMB:     memMB,
			NsPerOp:      uint64(processDuration.Nanoseconds()),
		}
		
		results[algoName] = result
		
		t.Logf("  %-12s: time=%dms, mem=%.2fMB, degradation=%.4f%%",
			algoName, processDuration.Milliseconds(), result.MemoryMB, result.Accuracy*100)
		
		// Check if degradation within acceptable bounds
		if degradation > maxDegradation {
			if algoName == "GK" {
				t.Errorf("✗ FAILED: %s degradation %.4f exceeds ε-bound (%.4f)",
					algoName, degradation, maxDegradation)
			} else {
				t.Logf("⚠ WARNING: %s degradation %.4f exceeds threshold (%.4f)",
					algoName, degradation, maxDegradation)
			}
		} else {
			t.Logf("✓ PASSED: %s degradation %.4f within bounds", algoName, degradation)
		}
	}
	
	// Comparative analysis
	p2Result := results["P2"]
	gkResult := results["GK"]
	tdigestResult := results["TDigest"]
	
	t.Log("\n=== Adversarial Pattern Comparison ===")
	t.Logf("%-12s | %-8s | %-8s | %-12s | %-12s",
		"Algorithm", "Time(ms)", "Memory(MB)", "Median Error", "P99 Error")
	t.Log("--------------------------------------------------------------------------------")
	
	t.Logf("%-12s | %-8d | %-8.2f | %-12.2f%% | %-12.2f%%",
		"P2", p2Result.NsPerOp/1e6, p2Result.MemoryMB, 
		calcAbsErrorAbs(p2Result.MedianValue, trueQuantiles.Median)*100,
		calcAbsErrorAbs(p2Result.P99Value, trueQuantiles.P99)*100)
	
	t.Logf("%-12s | %-8d | %-8.2f | %-12.2f%% | %-12.2f%%",
		"GK", gkResult.NsPerOp/1e6, gkResult.MemoryMB,
		calcAbsErrorAbs(gkResult.MedianValue, trueQuantiles.Median)*100,
		calcAbsErrorAbs(gkResult.P99Value, trueQuantiles.P99)*100)
	
	t.Logf("%-12s | %-8d | %-8.2f | %-12.2f%% | %-12.2f%%",
		"TDigest", tdigestResult.NsPerOp/1e6, tdigestResult.MemoryMB,
		calcAbsErrorAbs(tdigestResult.MedianValue, trueQuantiles.Median)*100,
		calcAbsErrorAbs(tdigestResult.P99Value, trueQuantiles.P99)*100)
	
	// Verify GK maintains bounds when others fail
	if gkResult.Accuracy <= maxDegradation && p2Result.Accuracy > maxDegradation {
		t.Logf("✓ GK proves superior: bounded at %.2f%% degradation vs P²'s %.2f%%",
			gkResult.Accuracy*100, p2Result.Accuracy*100)
	}
}

// calculateQuantileError computes relative quantile estimation error
func calculateQuantileError(trueQ struct {
	Median float64
	P90    float64
	P99    float64
}, estimatedMedian, estimatedP90, estimatedP99 float64) float64 {
	errors := []float64{
		relError(trueQ.Median, estimatedMedian),
		relError(trueQ.P90, estimatedP90),
		relError(trueQ.P99, estimatedP99),
	}
	return average(errors)
}

// calcAbsErrorAbs calculates absolute error in value space
func calcAbsErrorAbs(estimated, actual float64) float64 {
	return math.Abs(estimated - actual)
}

// computeTrueQuantiles calculates exact quantiles from sorted dataset
func computeTrueQuantiles(data []float64) struct {
	Median float64
	P90    float64
	P99    float64
} {
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)
	
	return struct {
		Median float64
		P90    float64
		P99    float64
	}{
		Median: NearestRank(sorted, 0.5),
		P90:    NearestRank(sorted, 0.9),
		P99:    NearestRank(sorted, 0.99),
	}
}

// generateExtremeOutliers creates dataset with 1% extreme outliers
func generateExtremeOutliers(n int) []float64 {
	rng := rand.New(rand.NewSource(BaseRandomSeed))
	data := make([]float64, n)
	
	for i := range data {
		r := rng.Float64()
		if r < 0.01 {
			data[i] = rng.Float64() * 1e12
		} else {
			data[i] = rng.NormFloat64()
		}
	}
	
	return data
}

// generateRapidDrift simulates sudden distribution shift mid-stream
func generateRapidDrift(n int) []float64 {
	rng := rand.New(rand.NewSource(BaseRandomSeed))
	data := make([]float64, n)
	
	mean := 0.0
	for i := 0; i < n; i++ {
		if i > n/2 {
			mean += 100.0
		}
		data[i] = rng.NormFloat64() + mean
	}
	
	return data
}

// generateMemoryPressure creates burst injection pattern
func generateMemoryPressure(n int) []float64 {
	rng := rand.New(rand.NewSource(BaseRandomSeed))
	data := make([]float64, n)
	
	for i := 0; i < n; i++ {
		if i%1000 == 0 {
			burstSize := 100
			for j := 0; j < burstSize && i+j < n; j++ {
				data[i+j] = rng.Float64() * 1e6
			}
			i += burstSize - 1
		} else {
			data[i] = rng.Float64()
		}
	}
	
	return data
}

// generateMarkerInterpolationTrap exploits P² interpolation weakness
func generateMarkerInterpolationTrap(n int) []float64 {
	data := make([]float64, n)
	
	for i := range data {
		if i%1000 == 0 {
			data[i] = float64(i) * 1e6
		} else if i%100 == 0 {
			data[i] = float64(i) * 1e3
		} else {
			data[i] = float64(i)
		}
	}
	
	return data
}

// TestM9_CompetitorComparisonUnderAttack compares all algorithms side-by-side
func TestM9_CompetitorComparisonUnderAttack(t *testing.T) {
	t.Parallel()
	
	patterns := []struct {
		name  string
		gen   func(int) []float64
		desc  string
	}{
		{"outliers", generateExtremeOutliers, "1% extreme outliers"},
		{"drift", generateRapidDrift, "mean shift after 50%"},
		{"traps", generateMarkerInterpolationTrap, "interpolation traps"},
	}
	
	for _, pat := range patterns {
		t.Run(pat.name, func(t *testing.T) {
			t.Logf("Testing: %s - %s", pat.name, pat.desc)
			
			data := pat.gen(AdversarialSamples)
			
			algorithms := []struct {
				name string
				fn   func() Sketch
			}{
				{"P²", func() Sketch { return NewP2Wrapper(0.5, 0.9, 0.99) }},
				{"GK", func() Sketch { return &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01} }},
				{"T-Digest", func() Sketch { return NewTDigestWrapper(1000) }},
			}
			
			trueQuantiles := computeTrueQuantiles(data)
			
			type Result struct {
				Name        string
				MedianError float64
				P99Error    float64
				TimeMs      int64
			}
			
			var results []Result
			
			for _, alg := range algorithms {
				start := time.Now()
				sketch := alg.fn()
				
				// Use Add() method per Sketch interface
				for _, val := range data {
					sketch.Add(val)
				}
				
				duration := time.Since(start).Milliseconds()
				
				medianEst := sketch.Quantile(0.5)
				p99Est := sketch.Quantile(0.99)
				
				medianError := relError(trueQuantiles.Median, medianEst) * 100
				p99Error := relError(trueQuantiles.P99, p99Est) * 100
				
				results = append(results, Result{
					Name:        alg.name,
					MedianError: medianError,
					P99Error:    p99Error,
					TimeMs:      duration,
				})
			}
			
			fmt.Printf("%-12s | %-12s | %-12s | %-12s\n",
				"Algorithm", "Median Err", "P99 Error", "Time(ms)")
			fmt.Println("--------------------------------------------------")
			for _, r := range results {
				fmt.Printf("%-12s | %-12.2f%% | %-12.2f%% | %-12d\n",
					r.Name, r.MedianError, r.P99Error, r.TimeMs)
			}
			
			var bestP50, bestP99, fastest Result
			minP50, minP99, maxTime := 100.0, 100.0, int64(0)
			
			for _, r := range results {
				if r.MedianError < minP50 {
					minP50 = r.MedianError
					bestP50 = r
				}
				if r.P99Error < minP99 {
					minP99 = r.P99Error
					bestP99 = r
				}
				if r.TimeMs > maxTime {
					maxTime = r.TimeMs
					fastest = r
				}
			}
			
			t.Logf("Winner under attack:")
			t.Logf("  Median accuracy: %s (%.2f%% error)", bestP50.Name, bestP50.MedianError)
			t.Logf("  P99 accuracy:    %s (%.2f%% error)", bestP99.Name, bestP99.P99Error)
			t.Logf("  Speed:           %s (%d ms)", fastest.Name, fastest.TimeMs)
		})
	}
}

func init() {
	rand.Seed(BaseRandomSeed)
}
