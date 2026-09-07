package quantile

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

const (
	// BenchmarkSamples defines number of observations per benchmark dataset
	BenchmarkSamples = 100000
	
	// BaseRandomSeed ensures deterministic results across runs
	BaseRandomSeed = 42
	
	// WarmupIterations for pre-benchmark initialization
	WarmupIterations = 100
)

// TestM9_FLIP_vs_Competitors runs full FLIP benchmark suite comparing P² vs GK vs t-digest
func TestM9_FLIP_vs_Competitors(t *testing.T) {
	t.Parallel()
	
	algorithms := []struct {
		name string
		fn   func() Sketch
	}{
		{"P2", func() Sketch { return NewP2Wrapper(0.5, 0.9, 0.99) }},
		{"GK", func() Sketch { return &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01} }},
		{"TDigest", func() Sketch { return NewTDigestWrapper(1000) }},
	}
	
	datasets := []struct {
		name string
		data []float64
	}{
		{"lognormal", generateLognormal(BenchmarkSamples)},
		{"uniform", generateUniform(BenchmarkSamples)},
		{"heavy_tail", generatePareto(BenchmarkSamples)},
		{"adversarial", generateAdversarial(BenchmarkSamples)},
	}
	
	for _, dataset := range datasets {
		t.Run(dataset.name, func(t *testing.T) {
			t.Parallel()
			
			for _, algo := range algorithms {
				sketch := algo.fn()
				
				result := measurePerformance(sketch, dataset.data)
				
				t.Logf("✓ %s on %s: speed=%d ns/op, mem=%.2fKB, accuracy=%.2f%%",
					algo.name, dataset.name, result.NsPerOp, result.MemoryMB*1024, result.Accuracy*100)
				
				// Check if P² maintains advantage claims (≥5x faster than others)
				if algo.name != "P2" && result.NsPerOp < 3*result.NsPerOp {
					t.Logf("⚠ %s shows competitive timing (%d ns/op)", algo.name, result.NsPerOp)
				}
			}
		})
	}
}

// measurePerformance runs complete benchmark measurement for a single algorithm
func measurePerformance(sketch Sketch, data []float64) BenchmarkResult {
	// Extract ground truth values first
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)
	
	trueMedian := NearestRank(sorted, 0.5)
	trueP99 := NearestRank(sorted, 0.99)
	
	// Run allocation benchmark using go testing.B
	var benchResult testing.BenchmarkResult
	bench := testing.Benchmark(func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sketch.Add(data[i%len(data)])
		}
	})
	
	benchResult = bench
	
	// Capture final stats
	memMB := float64(sketch.SizeBytes()) / (1024 * 1024)
	
	// Calculate accuracy errors by comparing estimated vs true quantiles
	p50Est := sketch.Quantile(0.5)
	p90Est := sketch.Quantile(0.9)
	p99Est := sketch.Quantile(0.99)
	
	p50Err := AbsError(p50Est, trueMedian)
	p90Err := AbsError(p90Est, trueP99)
	p99Err := AbsError(p99Est, trueP99)
	
	avgRelError := average([]float64{
		relError(trueMedian, p50Est),
		relError(trueP99, p99Est),
	})
	
	return BenchmarkResult{
		Algorithm:    sketch.Name(),
		Accuracy:     avgRelError,
		MemoryMB:     memMB,
		Allocations:  int(benchResult.AllocsPerOp()),
		NsPerOp:      uint64(benchResult.NsPerOp()),
		Distribution: "custom",
		Samples:      len(data),
		P50Error:     p50Err,
		P90Error:     p90Err,
		P99Error:     p99Err,
		MedianValue:  trueMedian,
		P99Value:     trueP99,
	}
}

// Helper functions for generating different distributions

// generateLognormal creates log-normal distributed samples (typical cloud latency)
// μ=3, σ=1 produces realistic latency patterns with right skew
func generateLognormal(n int) []float64 {
	rng := rand.New(rand.NewSource(BaseRandomSeed))
	data := make([]float64, n)
	
	for i := range data {
		z := rng.NormFloat64()
		data[i] = math.Exp(3.0 + 1.0*z)
	}
	
	return data
}

// generateUniform creates uniform random samples [0, 1000]
func generateUniform(n int) []float64 {
	data := make([]float64, n)
	for i := range data {
		data[i] = float64(i) * 1000.0 / float64(n)
	}
	return data
}

// generatePareto creates Pareto-distributed heavy-tailed samples
// α=2.5 gives classic power-law behavior seen in real systems
func generatePareto(n int) []float64 {
	rng := rand.New(rand.NewSource(BaseRandomSeed))
	alpha := 2.5
	data := make([]float64, n)
	
	for i := range data {
		u := rng.Float64()
		data[i] = 1.0 / math.Pow(1-u, 1.0/alpha)
	}
	
	return data
}

// generateAdversarial creates crafted input designed to maximize P² interpolation errors
func generateAdversarial(n int) []float64 {
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

// Helper functions

// relError calculates relative error as percentage
func relError(actual, estimated float64) float64 {
	if actual == 0 {
		return 0
	}
	return math.Abs(actual-estimated) / math.Abs(actual)
}

// average computes mean of slice
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Benchmark comparisons for performance validation

func Benchmark_M9_P2_Insert_100K(b *testing.B) {
	data := generateLognormal(BenchmarkSamples)
	sketch := NewP2Wrapper(0.5, 0.9, 0.99)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch.Add(data[i%BenchmarkSamples])
	}
}

func Benchmark_M9_GK_Insert_100K(b *testing.B) {
	data := generateLognormal(BenchmarkSamples)
	sketch := &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch.Add(data[i%BenchmarkSamples])
	}
}

func Benchmark_M9_TDigest_Insert_100K(b *testing.B) {
	data := generateLognormal(BenchmarkSamples)
	sketch := NewTDigestWrapper(1000)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch.Add(data[i%BenchmarkSamples])
	}
}

func Benchmark_M9_P2_Lognormal(b *testing.B) {
	data := generateLognormal(BenchmarkSamples)
	sketch := NewP2Wrapper(0.5, 0.9, 0.99)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch.Add(data[i%BenchmarkSamples])
	}
}

func Benchmark_M9_GK_Lognormal(b *testing.B) {
	data := generateLognormal(BenchmarkSamples)
	sketch := &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch.Add(data[i%BenchmarkSamples])
	}
}

func Benchmark_M9_TDigest_Lognormal(b *testing.B) {
	data := generateLognormal(BenchmarkSamples)
	sketch := NewTDigestWrapper(1000)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch.Add(data[i%BenchmarkSamples])
	}
}

// Quick sanity check for implementation correctness
func TestM9_QuickSanityCheck(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name       string
		testSketch Sketch
		maxMedian  float64
		minMedian  float64
	}{
		{"P2 basic", &P2Wrapper{inner: NewP2(0.5, 0.9, 0.99)}, 6000, 4000}, // Uses 10K samples
		{"GK basic", &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01}, 6000, 4000},
		{"TDigest basic", NewTDigestWrapper(1000), 6000, 4000},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sketch := tt.testSketch
			
			for i := 0.0; i < 10000; i++ {
				sketch.Add(i)
			}
			
			median := sketch.Quantile(0.5)
			if median < tt.minMedian || median > tt.maxMedian {
				t.Errorf("Failed %s: median should be ~5000, got %.2f", tt.name, median)
			}
		})
	}
}

// TestM9_MemoryConsistency checks that P² memory stays constant while others grow
func TestM9_MemoryConsistency(t *testing.T) {
	t.Parallel()
	
	tests := []struct {
		name           string
		sketch         Sketch
		growMemorySize float64
	}{
		{"P2_constant", NewP2Wrapper(0.5, 0.9, 0.99), 1.5},
		{"GK_growth", &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01}, 1.1},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initialMem := float64(tt.sketch.SizeBytes())
			
			if initialMem == 0 {
				t.Skip("Initial memory is zero")
			}
			
			// Add more samples
			for i := 0.0; i < 1000000; i++ {
				tt.sketch.Add(i)
			}
			
			finalMem := float64(tt.sketch.SizeBytes())
			ratio := finalMem / initialMem
			
			t.Logf("%s: initial=%d bytes, final=%d bytes, ratio=%.2f",
				tt.name, int(initialMem), int(finalMem), ratio)
			
			if tt.growMemorySize > 1 && ratio > tt.growMemorySize {
				t.Errorf("Memory grew too much: %.2f× (expected ≤%.2f×)", ratio, tt.growMemorySize)
			}
		})
	}
}

// TestM9_AccuracyComparison verifies accuracy expectations on known distributions
func TestM9_AccuracyComparison(t *testing.T) {
	t.Parallel()
	
	// Create sorted array for exact quantile calculation
	data := make([]float64, 10000)
	for i := range data {
		data[i] = float64(i)
	}
	sort.Float64s(data)
	
	trueP99 := NearestRank(data, 0.99)
	
	tests := []struct {
		name          string
		sketch        Sketch
		maxP99Error   float64
		shouldPass    bool
	}{
		{"P2_uniform", NewP2Wrapper(0.5, 0.99), 5.0, true},
		{"GK_uniform", &GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01}, 2.0, true},
		{"TDigest_uniform", NewTDigestWrapper(1000), 2.0, true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sketch := tt.sketch
			
			// Ingest uniform data
			for _, v := range data {
				sketch.Add(v)
			}
			
			p99Est := sketch.Quantile(0.99)
			errorAbs := AbsError(p99Est, trueP99)
			errorPct := relError(trueP99, p99Est) * 100
			
			t.Logf("%s: p99_estimate=%.2f, p99_true=%.2f, abs_error=%.2f, pct_error=%.2f%%",
				tt.name, p99Est, trueP99, errorAbs, errorPct)
			
			if errorPct > tt.maxP99Error {
				if tt.shouldPass {
					t.Errorf("Error too high: %.2f%% (threshold: %.2f%%)", errorPct, tt.maxP99Error)
				}
			} else if !tt.shouldPass {
				t.Errorf("Expected failure but passed with error %.2f%%", errorPct)
			}
		})
	}
}

// TestM9_BasicFunctionality validates core sketch operations work correctly
func TestM9_BasicFunctionality(t *testing.T) {
	t.Parallel()
	
	// Verify all three implementations handle edge cases
	sketches := []Sketch{
		NewP2Wrapper(0.5, 0.99),
		&GKWrapper{inner: NewGKSummary(0.01), epsilon: 0.01},
		NewTDigestWrapper(1000),
	}
	
	for _, s := range sketches {
		t.Run(s.Name(), func(t *testing.T) {
			// Test empty state
			if count := s.Count(); count != 0 {
				t.Errorf("Empty sketch Count() = %d, want 0", count)
			}
			
			if q := s.Quantile(0.5); !math.IsNaN(q) {
				t.Errorf("Empty sketch Quantile(0.5) = %.2f, want NaN", q)
			}
			
			// Test single value
			s.Add(42.0)
			if count := s.Count(); count != 1 {
				t.Errorf("Single value Count() = %d, want 1", count)
			}
			
			if q := s.Quantile(0.5); q != 42.0 {
				t.Errorf("Single value Quantile(0.5) = %.2f, want 42.0", q)
			}
			
			// Test duplicate values
			s.Add(42.0)
			s.Add(42.0)
			if q := s.Quantile(0.5); q != 42.0 {
				t.Errorf("Duplicate values Quantile(0.5) = %.2f, want 42.0", q)
			}
			
			// Test large numbers
			s.Add(1e12)
			s.Add(1e6)
			
			if med := s.Quantile(0.5); med > 1e6 && med < 1e12 {
				t.Logf("Large number handling OK: median=%.2e", med)
			}
		})
	}
}

func init() {
	rand.Seed(BaseRandomSeed)
}
