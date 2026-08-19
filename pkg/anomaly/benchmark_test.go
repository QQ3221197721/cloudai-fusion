package anomaly

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// ===========================================================================
// MICRO-BENCHMARKS: per-component and per-point cost, complexity validation.
// Run: go test ./pkg/anomaly/ -bench=. -benchmem -count=5 -run=^$
// ===========================================================================

// BenchmarkCholeskyRank1Update measures the O(d^2) incremental update.
func BenchmarkCholeskyRank1Update(b *testing.B) {
	for _, d := range []int{10, 25, 50, 100} {
		b.Run(fmt.Sprintf("d=%d", d), func(b *testing.B) {
			rnd := rand.New(rand.NewSource(888))
			A := spdMatrix(d, rnd)
			L, _ := CholeskyDecomposition(A)
			w0 := make([]float64, d)
			for i := range w0 {
				w0[i] = rnd.NormFloat64()
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				Lc := matCopy(L)
				w := copyVector(w0)
				CholeskyRank1Update(Lc, w)
			}
		})
	}
}

// BenchmarkCholeskyDecomposition measures the O(d^3) full factorization for contrast.
func BenchmarkCholeskyDecomposition(b *testing.B) {
	for _, d := range []int{10, 25, 50, 100} {
		b.Run(fmt.Sprintf("d=%d", d), func(b *testing.B) {
			rnd := rand.New(rand.NewSource(999))
			A := spdMatrix(d, rnd)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				CholeskyDecomposition(A)
			}
		})
	}
}

// BenchmarkStreamingObserve measures amortized per-point Observe cost.
func BenchmarkStreamingObserve(b *testing.B) {
	for _, d := range []int{10, 25, 50, 100} {
		b.Run(fmt.Sprintf("d=%d", d), func(b *testing.B) {
			rnd := rand.New(rand.NewSource(7))
			sd := NewStreamingDetector(d, 0.975)
			// warm up
			for i := 0; i < d+10; i++ {
				sd.Observe(randVec(d, rnd))
			}
			pts := make([][]float64, 1024)
			for i := range pts {
				pts[i] = randVec(d, rnd)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sd.Observe(pts[i&1023])
			}
		})
	}
}

// BenchmarkOfflineMahalanobisScore measures the offline baseline scoring cost.
func BenchmarkOfflineMahalanobisScore(b *testing.B) {
	d := 50
	rnd := rand.New(rand.NewSource(11))
	X := GenerateGaussianNormal(d, 1000, 11)
	off := NewOfflineMahalanobisDetector(d, 0.975)
	if err := off.FitLedoitWolf(X); err != nil {
		b.Fatal(err)
	}
	pts := make([][]float64, 1024)
	for i := range pts {
		pts[i] = randVec(d, rnd)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off.ScorePoint(pts[i&1023])
	}
}

// BenchmarkThreeSigmaObserve measures the univariate baseline cost.
func BenchmarkThreeSigmaObserve(b *testing.B) {
	d := 50
	rnd := rand.New(rand.NewSource(13))
	ts := NewThreeSigmaDetector(d, 3.0)
	pts := make([][]float64, 1024)
	for i := range pts {
		pts[i] = randVec(d, rnd)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts.Observe(pts[i&1023], false)
	}
}

// BenchmarkStreamingAdaptiveObserve measures per-point cost of adaptive threshold.
func BenchmarkStreamingAdaptiveObserve(b *testing.B) {
	for _, d := range []int{10, 50} {
		b.Run(fmt.Sprintf("d=%d", d), func(b *testing.B) {
			rnd := rand.New(rand.NewSource(7))
			sd := NewStreamingDetectorAdaptive(d, 0.85)
			// warm up
			for i := 0; i < d+10; i++ {
				sd.Observe(randVec(d, rnd))
			}
			pts := make([][]float64, 1024)
			for i := range pts {
				pts[i] = randVec(d, rnd)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				sd.Observe(pts[i&1023])
			}
		})
	}
}

// BenchmarkPerPointRealistic measures the honest per-point cost in the PRODUCTION
// stream regime (n=3000, the same geometry as the sklearn export): each b.N
// iteration replays a full fresh 3000-point stream and the reported ns/point is
// total / (b.N * n). Unlike a hot-loop micro-benchmark whose count n grows into
// the millions (pushing the 0.85 quantile out of TailExact's exact tail into the
// cheap GK body and thus UNDER-reporting the adaptive cost), this keeps n bounded
// at 3000 so the tail-quantile query exercises the expensive exact-tail path -
// exactly the regime the latency budget is defined against. It reports both the
// plain streaming detector and the adaptive one so the 2x ratio can be read off
// a single consistent methodology (no per-call time.Now() overhead).
func BenchmarkPerPointRealistic(b *testing.B) {
	const (
		d      = 10
		n      = 3000
		warmup = 800
	)
	ds := GenerateDataset(ScenarioElliptical, d, n, warmup, 0.15, 0.75, 0)

	b.Run("stream", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sd := NewStreamingDetector(d, 0.975)
			for j := 0; j < len(ds.X); j++ {
				sd.Observe(ds.X[j])
			}
		}
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(ds.X)), "ns/point")
	})

	b.Run("adaptive_0.85", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sd := NewStreamingDetectorAdaptive(d, 0.85)
			for j := 0; j < len(ds.X); j++ {
				sd.Observe(ds.X[j])
			}
		}
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*len(ds.X)), "ns/point")
	})
}

// BenchmarkLedoitWolfBatch measures the offline shrinkage computation.
func BenchmarkLedoitWolfBatch(b *testing.B) {
	d := 50
	X := GenerateGaussianNormal(d, 1000, 17)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LedoitWolfShrinkage(X)
	}
}

// ---------------------------------------------------------------------------
// COMPLEXITY VALIDATION (not a Benchmark; runs under -run to log scaling ratios).
// ---------------------------------------------------------------------------

// TestPerPointComplexityScaling empirically checks the amortized per-point cost
// scales like O(d^2): doubling d should roughly quadruple the per-point time.
func TestPerPointComplexityScaling(t *testing.T) {
	measure := func(d int) float64 {
		rnd := rand.New(rand.NewSource(int64(d) * 101))
		sd := NewStreamingDetector(d, 0.975)
		for i := 0; i < d+10; i++ {
			sd.Observe(randVec(d, rnd))
		}
		const reps = 4000
		pts := make([][]float64, reps)
		for i := range pts {
			pts[i] = randVec(d, rnd)
		}
		start := time.Now()
		for i := 0; i < reps; i++ {
			sd.Observe(pts[i])
		}
		return float64(time.Since(start).Nanoseconds()) / float64(reps)
	}

	t25 := measure(25)
	t50 := measure(50)
	t100 := measure(100)
	t.Logf("per-point ns: d=25 -> %.1f, d=50 -> %.1f, d=100 -> %.1f", t25, t50, t100)
	t.Logf("ratio d50/d25 = %.2f (O(d^2) predicts ~4), d100/d50 = %.2f", t50/t25, t100/t50)
	// Non-strict: hardware noise makes exact ratios unreliable; we only require
	// that cost grows sub-cubically (ratio < 8 = 2^3) which excludes O(d^3).
	if t50/t25 > 8 {
		t.Errorf("d50/d25 ratio %.2f suggests worse than O(d^2)", t50/t25)
	}
}

// spdMatrix builds a random symmetric positive-definite d x d matrix.
func spdMatrix(d int, rnd *rand.Rand) [][]float64 {
	A := newMatrix(d)
	for i := 0; i < d; i++ {
		A[i][i] = rnd.Float64()*5 + float64(d) // diagonally dominant => SPD
		for j := 0; j < i; j++ {
			v := rnd.Float64() * 0.5
			A[i][j] = v
			A[j][i] = v
		}
	}
	return A
}

// randVec returns a length-d standard-normal vector.
func randVec(d int, rnd *rand.Rand) []float64 {
	v := make([]float64, d)
	for i := range v {
		v[i] = rnd.NormFloat64()
	}
	return v
}
