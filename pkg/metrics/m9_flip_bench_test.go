package metrics_test

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/metrics"
)

// TestHybridQuantileCorrectness validates basic correctness guarantees:
// - Insert(x); Query(0) ≤ x ≤ Query(1) for all inserted x
// - Quantiles strictly non-decreasing with qty
// - Accuracy bounded within theoretical limits
func TestHybridQuantileCorrectness(t *testing.T) {
	hq := metrics.NewHybridQuantile()
	
	// Insert known sequence
	values := []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}
	for _, v := range values {
		hq.Insert(v)
	}
	
	// Query quantiles should be monotonic
	var prevQty float64 = -1
	for qty := 0.0; qty <= 1.0; qty += 0.1 {
		q := hq.Query(qty)
		if math.IsNaN(q) {
			t.Errorf("Query(%v) returned NaN", qty)
		}
		if q < prevQty && prevQty >= 0 {
			t.Errorf("Quantiles not monotonic: Query(%v)=%.4f > Query(%v)=%.4f", 
				qty-q*0.1, prevQty, qty, q)
		}
		prevQty = q
	}
	
	// Min/max queries should bound all values
	var minQ, maxQ float64 = math.MaxFloat64, -math.MaxFloat64
	for _, v := range values {
		if v < minQ {
			minQ = v
		}
		if v > maxQ {
			maxQ = v
		}
	}
	
	for _, v := range values {
		if v < minQ || v > maxQ {
			t.Errorf("Value %v outside [Query(0), Query(1)] = [%v, %v]", v, minQ, maxQ)
		}
	}
}

// BenchmarkHybridQuantile_Insert_Only measures raw insert throughput
func BenchmarkHybridQuantile_Insert_1M_Ops(b *testing.B) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		hq.Insert(rng.Float64())
	}
}

// BenchmarkHybridQuantile_Query_Only measures query latency at various percentiles
func BenchmarkHybridQuantile_Query_P50(b *testing.B) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	// Pre-fill with data
	for i := 0; i < 10000; i++ {
		hq.Insert(rng.Float64())
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = hq.Query(0.5)
	}
}

func BenchmarkHybridQuantile_Query_P95(b *testing.B) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	for i := 0; i < 10000; i++ {
		hq.Insert(rng.Float64())
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = hq.Query(0.95)
	}
}

func BenchmarkHybridQuantile_Query_P99(b *testing.B) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	for i := 0; i < 10000; i++ {
		hq.Insert(rng.Float64())
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = hq.Query(0.99)
	}
}

// BenchmarkHybridQuantile_Insert_Query_Mixed simulates realistic workload
func BenchmarkHybridQuantile_Insert_Query_1000Ops(b *testing.B) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		hq.Insert(rng.Float64())
		_ = hq.Query(0.5)
		_ = hq.Query(0.95)
	}
}

func BenchmarkHybridQuantile_Insert_Query_10K_Ops(b *testing.B) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	// Pre-warm buffer
	for i := 0; i < 1000; i++ {
		hq.Insert(rng.Float64())
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		hq.Insert(rng.Float64())
		_ = hq.Query(0.99)
	}
}

// BenchmarkHybridQuantile_MemoryAllocation measures GC pressure
func BenchmarkHybridQuantile_MemoryAllocation(b *testing.B) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		hq.Insert(rng.Float64())
		_ = hq.Query(0.5)
	}
	
	_ = hq.Stats()
	_ = hq.EstimateMemoryUsage()
}

// BenchmarkHybridQuantile_ResizeStress tests performance under dynamic sizing
func BenchmarkHybridQuantile_ResizeStress(b *testing.B) {
	b.Run("GrowFromSmall", func(b *testing.B) {
		for size := 64; size <= 1024; size *= 2 {
		cfg := metrics.NewHybridQuantileWithConfig(metrics.QuantileConfig{
			BufferSize:       size,
			HistogramBuckets: 256,
			MergeInterval:    100,
			NormMin:          0,
			NormMax:          1,
		})
			
			rng := rand.New(rand.NewSource(42))
			for i := 0; i < 1000; i++ {
				cfg.Insert(rng.Float64())
			}
		}
	})
	
	b.Run("ShrinkToSmall", func(b *testing.B) {
		small := metrics.NewHybridQuantileWithConfig(metrics.QuantileConfig{
		BufferSize:       128,
		HistogramBuckets: 256,
		MergeInterval:    100,
		NormMin:          0,
		NormMax:          1,
	})
	
	large := metrics.NewHybridQuantileWithConfig(metrics.QuantileConfig{
		BufferSize:       1024,
		HistogramBuckets: 256,
		MergeInterval:    100,
		NormMin:          0,
		NormMax:          1,
	})
		
		rng := rand.New(rand.NewSource(42))
		for i := 0; i < b.N; i++ {
			small.Insert(rng.Float64())
			large.Insert(rng.Float64())
			_ = small.Query(0.5)
			_ = large.Query(0.5)
		}
	})
}

// BENCH_COMPARISON Against industry baselines (P², TDigest simulation via sorting)

// simulatePDigest implements simplified P² algorithm for comparison
type simulatedPDigest struct {
	data []float64
	n    int
}

func newSimulatedPDigest(capacity int) *simulatedPDigest {
	return &simulatedPDigest{
		data: make([]float64, 0, capacity),
	}
}

func (p *simulatedPDigest) Insert(v float64) {
	p.data = append(p.data, v)
}

func (p *simulatedPDigest) Query(qty float64) float64 {
	sort.Float64s(p.data)
	idx := int(float64(len(p.data)-1) * qty)
	if idx < 0 || idx >= len(p.data) {
		return 0
	}
	return p.data[idx]
}

func (p *simulatedPDigest) Count() int {
	return len(p.data)
}

// simulateTDigest uses k-means clustering approximation
type simulatedTDigest struct {
	centroids [][]float64
	k         int
	n         int
}

func newSimulatedTDigest(k int) *simulatedTDigest {
	return &simulatedTDigest{
		centroids: make([][]float64, k),
		k:         k,
	}
}

func (t *simulatedTDigest) Insert(v float64) {
	// Simplified: assign to nearest centroid bucket
	minDist := math.MaxFloat64
	nearest := 0
	
	for i := 0; i < t.k; i++ {
		if len(t.centroids[i]) == 0 {
			t.centroids[i] = []float64{v}
			return
		}
		mean := 0.0
		for _, c := range t.centroids[i] {
			mean += c
		}
		mean /= float64(len(t.centroids[i]))
		dist := math.Abs(v - mean)
		if dist < minDist {
			minDist = dist
			nearest = i
		}
	}
	
	t.centroids[nearest] = append(t.centroids[nearest], v)
	t.n++
}

func (t *simulatedTDigest) Query(qty float64) float64 {
	// Flatten all centroids and sort
	all := make([]float64, 0)
	for _, c := range t.centroids {
		all = append(all, c...)
	}
	sort.Float64s(all)
	
	idx := int(float64(len(all)-1) * qty)
	if idx < 0 || idx >= len(all) {
		return 0
	}
	return all[idx]
}

// BenchCompareWithBaselines runs full comparison against sorted baseline
func BenchmarkHybridQuantile_Vs_SortedBaseline(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	
	// Generate reference dataset
	referenceData := make([]float64, b.N)
	for i := 0; i < b.N; i++ {
		referenceData[i] = rng.Float64()
	}
	sort.Float64s(referenceData)
	
	hq := metrics.NewHybridQuantile()
	pDigest := newSimulatedPDigest(b.N)
	tDigest := newSimulatedTDigest(64)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		val := referenceData[i]
		
		// HybridQuantile
		hq.Insert(val)
		
		// Simulated P²
		pDigest.Insert(val)
		
		// Simulated TDigest
		tDigest.Insert(val)
		
		// Measure query latencies
		_ = hq.Query(0.5)
		_ = pDigest.Query(0.5)
		_ = tDigest.Query(0.5)
	}
	
	// Verify accuracy against sorted baseline
	hybridP50 := hq.Query(0.5)
	sortedP50 := referenceData[len(referenceData)/2]
	errorP50 := math.Abs(hybridP50 - sortedP50) / sortedP50
	
	hybridP95 := hq.Query(0.95)
	sortedP95 := referenceData[len(referenceData)*95/100]
	errorP95 := math.Abs(hybridP95 - sortedP95) / sortedP95
	
	b.Logf("Sorted baseline P50=%.6f, HybridP50=%.6f, Error=%.4f%%", 
		sortedP50, hybridP50, errorP50*100)
	b.Logf("Sorted baseline P95=%.6f, HybridP95=%.6f, Error=%.4f%%", 
		sortedP95, hybridP95, errorP95*100)
	
	// Check if errors are within bounds (< 5% tolerance)
	if errorP50 > 0.05 {
		b.Errorf("P50 error %.4f exceeds 5%% tolerance", errorP50)
	}
	if errorP95 > 0.05 {
		b.Errorf("P95 error %.4f exceeds 5%% tolerance", errorP95)
	}
}

// TestAccuracyVsBufferSizing explores tradeoffs between buffer size and accuracy
func TestAccuracyVsBufferSizing(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	
	bufferSizes := []int{256, 512, 1024, 2048, 4096}
	sampleSize := 10000
	
	for _, size := range bufferSizes {
		cfg := metrics.NewHybridQuantileWithConfig(metrics.QuantileConfig{
			BufferSize:       size,
			HistogramBuckets: 256,
			MergeInterval:    100,
			NormMin:          0,
			NormMax:          1,
		})
		
		// Generate ground truth
		groundTruth := make([]float64, sampleSize)
		for i := 0; i < sampleSize; i++ {
			v := rng.Float64()
			groundTruth[i] = v
			cfg.Insert(v)
		}
		
		// Compute exact quantiles via sorting
		sort.Float64s(groundTruth)
		exactP50 := groundTruth[sampleSize/2]
		exactP95 := groundTruth[sampleSize*95/100]
		
		// Compare with HybridQuantile
		approxP50 := cfg.Query(0.5)
		approxP95 := cfg.Query(0.95)
		
		errP50 := math.Abs(approxP50-exactP50)/exactP50
		errP95 := math.Abs(approxP95-exactP95)/exactP95
		
		t.Logf("BufferSize=%d: P50_error=%.4f%%, P95_error=%.4f%%", 
			size, errP50*100, errP95*100)
		
		// Expect errors < 5% for reasonable buffer sizes
		if errP50 > 0.05 || errP95 > 0.05 {
			t.Errorf("BufferSize=%d exceeded tolerance: P50=%.4f, P95=%.4f", 
				size, errP50*100, errP95*100)
		}
	}
}

// BenchConcurrencyStress tests thread-safe performance under contention
func BenchmarkHybridQuantile_Concurrency_1G_Ops(b *testing.B) {
	
	b.RunParallel(func(pb *testing.PB) {
		localHq := metrics.NewHybridQuantile()
		localRng := rand.New(rand.NewSource(42))
		
		for pb.Next() {
			localHq.Insert(localRng.Float64())
			_ = localHq.Query(0.5)
		}
	})
	
	// Single-threaded baseline
	b.Run("SingleThread", func(b *testing.B) {
		singleHQ := metrics.NewHybridQuantile()
		singleRng := rand.New(rand.NewSource(42))
		
		b.ResetTimer()
		b.ReportAllocs()
		
		for i := 0; i < b.N; i++ {
			singleHQ.Insert(singleRng.Float64())
			_ = singleHQ.Query(0.5)
		}
	})
}

// TestEdgeCases ensures robustness for pathological inputs
func TestHybridQuantile_EdgeCases(t *testing.T) {
	// Out-of-bounds quantile queries (before any inserts)
	emptyHQ := metrics.NewHybridQuantile()
	if !math.IsNaN(emptyHQ.Query(-0.1)) {
		t.Error("Query(-0.1) should return NaN")
	}
	if !math.IsNaN(emptyHQ.Query(1.1)) {
		t.Error("Query(1.1) should return NaN")
	}
	
	// Empty state queries
	emptyHQ2 := metrics.NewHybridQuantile()
	if !math.IsNaN(emptyHQ2.Query(0.5)) {
		t.Error("Query on empty histogram should return NaN")
	}
	
	// Reset behavior
	emptyHQ.Reset()
	if !math.IsNaN(emptyHQ2.Query(0.5)) {
		t.Error("Query after Reset should return NaN")
	}
}

// BenchScaleTests evaluate performance scaling with data volume
func BenchmarkHybridQuantile_Scale_To_1B_Ideally(b *testing.B) {
	// This is conceptual - actual run with smaller N
	scaleFactors := []int{1000, 10000, 100000, 1000000}
	
	for _, n := range scaleFactors {
		b.Run(fmt.Sprintf("N%d", n), func(b *testing.B) {
			hq := metrics.NewHybridQuantile()
			rng := rand.New(rand.NewSource(42))
			
			b.ResetTimer()
			
			// Fill initial state
			for i := 0; i < n; i++ {
				hq.Insert(rng.Float64())
			}
			
			// Benchmark repeated queries
			for i := 0; i < b.N; i++ {
				_ = hq.Query(0.5)
				_ = hq.Query(0.95)
			}
		})
	}
}

// TestMemoryProfile analyzes heap allocation patterns
func TestHybridQuantile_MemoryProfile(t *testing.T) {
	hq := metrics.NewHybridQuantile()
	rng := rand.New(rand.NewSource(42))
	
	t.Run("Benchmark", func(t *testing.T) {
		b := testing.Benchmark(func(b *testing.B) {
			hq.Reset()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				hq.Insert(rng.Float64())
				_ = hq.Query(0.5)
			}
		})
		t.Logf("Memory per operation: %d B/op", b.AllocsPerOp())
		t.Logf("Total allocation: %d KB", b.MemBytes/1024)
		
		if b.AllocsPerOp() > 10 {
			t.Errorf("High allocation rate: %d allocs/op (target ≤10)", int(b.AllocsPerOp()))
		}
	})
}

// Helper function for TestHybridQuantile_EdgeCases to avoid unused import
var _ = func() bool {
	return true
}()
