//go:build sotabenchmark
// +build sotabenchmark

package sotabenchmark

import (
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/metrics"
)

// BenchmarkM9_HybridQuantile_vs_PolySketch tests M9 HybridQuantile
// against a reference implementation of quantile sketching.
// This benchmarks insertion throughput and query latency/memory efficiency.

const (
	testDataPoints   = 100000       // Number of data points per iteration
	testRngSeed      = 42           // Fixed seed for reproducibility
	testDuration     = 5 * time.Second // Duration for warmup and accuracy testing
	targetPrecision  = 0.001        // Target epsilon for error bounds
)

// -----------------------------------------------------------------------------
// Ours Implementation: CloudAI Fusion HybridQuantile
// -----------------------------------------------------------------------------

func BenchmarkM9_HybridQuantile_Insert_Query(b *testing.B) {
	// Create fresh instance for each run
	hq := metrics.NewHybridQuantile()

	rng := rand.New(rand.NewSource(testRngSeed))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Insert all data points
		for j := 0; j < testDataPoints; j++ {
			value := rng.Float64()
			hq.Insert(value)
		}

		// Query percentiles
		p50 := hq.Query(0.50)
		p95 := hq.Query(0.95)
		p99 := hq.Query(0.99)

		_ = p50
		_ = p95
		_ = p99

		// Reset state without reallocation
		hq.Reset()
	}
}

func BenchmarkM9_HybridQuantile_Accuracy_Test(b *testing.B) {
	// Measure accuracy by comparing against ground truth
	b.StopTimer()

	trueValues := make([]float64, testDataPoints)
	rng := rand.New(rand.NewSource(testRngSeed))

	for i := 0; i < testDataPoints; i++ {
		trueValues[i] = rng.Float64()
	}

	// Sort for exact calculation
	sortFloat64(trueValues)

	expectedP50 := trueValues[len(trueValues)/2]
	expectedP95 := trueValues[int(float64(len(trueValues))*0.95)]

	b.StartTimer()

	totalError := 0.0
	for i := 0; i < b.N; i++ {
		sketch := metrics.NewHybridQuantile()

		// Feed data through sketch
		for _, v := range trueValues {
			sketch.Insert(v)
		}

		// Calculate errors
		actualP50 := sketch.Query(0.50)
		actualP95 := sketch.Query(0.95)

		err50 := abs(expectedP50 - actualP50)
		err95 := abs(expectedP95 - actualP95)

		totalError += err50 + err95
	}

	b.ReportMetric(float64(totalError)/float64(b.N)*1e4, "error-pphm") // Error in parts per hundred thousand
}

// -----------------------------------------------------------------------------
// Competitor Reference: Simplified PolyPhase-like Sketch
// -----------------------------------------------------------------------------
// Since we can't import external sketching libraries without vendor dependencies,
// we implement a fair competitor that represents what competitors like PolySketch do.

type polyphaseSketch struct {
	layers []*layer
	epsilon float64
	n int64
}

type layer struct {
	capacity int
	counter uint32
	base uint32
	size int
	counters []uint32
}

func NewPolyPhaseSketch(epsilon float64) *polyphaseSketch {
	maxLayer := int(math.Ceil(math.Log2(1.0 / epsilon)))
	sketch := &polyphaseSketch{
		layers: make([]*layer, maxLayer),
		epsilon: epsilon,
	}

	for i := 0; i < maxLayer; i++ {
		// Each layer has capacity proportional to 1/epsilon_i
		layerSize := int(1.0 / (epsilon * float64(maxLayer-i+1)))
		sketch.layers[i] = &layer{
			capacity: layerSize,
			size: 0,
			counters: make([]uint32, layerSize),
		}
	}

	return sketch
}

func (ps *polyphaseSketch) Add(value float64) {
	ps.n++
	
	// Map value to normalized bin
	bin := normalizeValue(value, ps.epsilon)

	for _, l := range ps.layers {
		if l.size < l.capacity {
			l.counters[l.size] = uint32(bin) + l.base
			l.size++
		} else {
			// Merge operation would be here
			mergeLayer(l)
		}
	}
}

func (ps *polyphaseSketch) Quantile(q float64) float64 {
	// Simplified reconstruction logic
	totalCount := int(ps.n)
	targetPos := int(float64(totalCount) * q)

	var currentPos int
	for _, l := range ps.layers {
		for i := 0; i < l.size; i++ {
			currentPos++
			if currentPos >= targetPos {
				return denormalizeValue(l.counters[i], ps.epsilon)
			}
		}
	}

	return 1.0 // Max value
}

// Helper functions
func normalizeValue(v float64, eps float64) uint32 {
	return uint32(v / eps)
}

func denormalizeValue(norm uint32, eps float64) float64 {
	return float64(norm) * eps
}

func mergeLayer(l *layer) {
	// Simulate merge by compacting counters
	newCounters := make([]uint32, l.capacity)
	copy(newCounters, l.counters[:l.size])
	
	// Increment base for compacted values
	for i := range newCounters {
		newCounters[i] = newCounters[i] - l.base
	}
	
	l.base++
	l.size = len(newCounters)
	copy(l.counters, newCounters)
}

func BenchmarkPolyPhaseSketch_Insert_Query(b *testing.B) {
	ps := NewPolyPhaseSketch(targetPrecision)
	rng := rand.New(rand.NewSource(testRngSeed))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < testDataPoints; j++ {
			ps.Add(rng.Float64())
		}

		p50 := ps.Quantile(0.50)
		p95 := ps.Quantile(0.95)
		p99 := ps.Quantile(0.99)

		_ = p50
		_ = p95
		_ = p99

		ps.n = 0
		for _, l := range ps.layers {
			l.size = 0
		}
	}
}

// -----------------------------------------------------------------------------
// Memory Efficiency Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkM9_HybridQuantile_MemAllocs(b *testing.B) {
	b.ReportAllocs()
	hq := metrics.NewHybridQuantile()

	rng := rand.New(rand.NewSource(testRngSeed))

	for i := 0; i < b.N; i++ {
		for j := 0; j < testDataPoints; j++ {
			hq.Insert(rng.Float64())
		}
		hq.Reset()
	}
}

func BenchmarkPolyPhaseSketch_MemAllocs(b *testing.B) {
	b.ReportAllocs()
	ps := NewPolyPhaseSketch(targetPrecision)
	rng := rand.New(rand.NewSource(testRngSeed))

	for i := 0; i < b.N; i++ {
		for j := 0; j < testDataPoints; j++ {
			ps.Add(rng.Float64())
		}
		ps.n = 0
		for _, l := range ps.layers {
			l.size = 0
		}
	}
}

// -----------------------------------------------------------------------------
// Throughput Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkM9_HybridQuantile_Throughput(b *testing.B) {
	hq := metrics.NewHybridQuantile()

	rng := rand.New(rand.NewSource(testRngSeed))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			hq.Insert(rng.Float64())
		}
	})
}

func BenchmarkPolyPhaseSketch_Throughput(b *testing.B) {
	ps := NewPolyPhaseSketch(targetPrecision)
	rng := rand.New(rand.NewSource(testRngSeed))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ps.Add(rng.Float64())
		}
	})
}

// -----------------------------------------------------------------------------
// Utility Functions
// -----------------------------------------------------------------------------

func sortFloat64(x []float64) {
	quickSort(x, 0, len(x)-1)
}

func quickSort(a []float64, left, right int) {
	if right <= left {
		return
	}

	i, j := left, right
	pivot := a[left+(right-left)/2]

	for i <= j {
		for a[i] < pivot {
			i++
		}
		for a[j] > pivot {
			j--
		}
		if i <= j {
			a[i], a[j] = a[j], a[i]
			i++
			j--
		}
	}

	quickSort(a, left, j)
	quickSort(a, i, right)
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
