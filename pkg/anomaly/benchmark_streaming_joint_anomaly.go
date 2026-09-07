package anomaly

import (
	"fmt"
	"testing"
)

// ===========================================================================
// BENCHMARKS FOR STREAMING JOINT ANOMALY DETECTION
// Compare against sklearn baselines via python-engine (sklearn_baseline.py)
// ===========================================================================

// BenchmarkStreamingDetectorCorrelationFlip tests the primary use case:
// correlation-flip joint anomalies where marginals stay N(0,1).
func BenchmarkStreamingDetectorCorrelationFlip(b *testing.B) {
	d := 20
	n := 5000
	warmup := 500

	_ = GenerateDataset(ScenarioCorrelationFlip, d, n, warmup, 0.15, 0.7, 42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sd := NewStreamingDetector(d, 0.975)
		for j := 0; j < warmup; j++ {
			x := make([]float64, d)
			for k := range x {
				x[k] = rand.NormFloat64()
			}
			sd.Observe(x)
		}

		// Score evaluation points
		for k := 0; k < warmup && k < 100; k++ {
			x := make([]float64, d)
			for j := range x {
				x[j] = rand.NormFloat64()
			}
			sd.Observe(x)
		}
	}
}

// BenchmarkThreeSigma baseline for univariate detection.
func BenchmarkThreeSigma(b *testing.B) {
	d := 20
	n := 5000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dt := NewThreeSigmaDetector(d, 3.0)
		for j := 0; j < n; j++ {
			x := make([]float64, d)
			for k := range x {
				x[k] = rand.NormFloat64()
			}
			dt.Observe(x, false)
		}
	}
}

// BenchmarkOfflineMahalanobis as upper-bound offline reference.
func BenchmarkOfflineMahalanobis(b *testing.B) {
	d := 20
	n := 1000

	X := GenerateGaussianNormal(d, n, 42)

	b.ResetMetric("n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off := NewOfflineMahalanobisDetector(d, 0.975)
		err := off.FitLedoitWolf(X)
		if err != nil {
			b.Fatalf("Fit failed: %v", err)
		}

		for j := 0; j < 100; j++ {
			x := make([]float64, d)
			for k := range x {
				x[k] = rand.NormFloat64()
			}
			off.ScorePoint(x)
		}
	}
}

// BenchmarkLedoitWolfShrinkage tests the shrinkage computation.
func BenchmarkLedoitWolfShrinkage(b *testing.B) {
	d := 50
	n := 1000

	X := GenerateGaussianNormal(d, n, 42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LedoitWolfShrinkage(X)
	}
}

// BenchmarkCholeskyRank1Update tests the rank-1 update performance.
func BenchmarkCholeskyRank1Update(b *testing.B) {
	d := 50

	rnd := rand.New(rand.NewSource(888))
	A := newMatrix(d)
	for i := 0; i < d; i++ {
		A[i][i] = rnd.Float64()*5 + 1
		for j := 0; j < i; j++ {
			val := rnd.Float64() * 0.5
			A[i][j] = val
			A[j][i] = val
		}
	}

	L, _ := CholeskyDecomposition(A)
	w := make([]float64, d)
	for i := range w {
		w[i] = rnd.NormFloat64()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Lcopy := matCopy(L)
		CholeskyRank1Update(Lcopy, w)
		_ = Lcopy
	}
}

// BenchmarkCholeskyDecomposition for comparison.
func BenchmarkCholeskyDecomposition(b *testing.B) {
	d := 50

	rnd := rand.New(rand.NewSource(999))
	A := newMatrix(d)
	for i := 0; i < d; i++ {
		A[i][i] = rnd.Float64()*5 + 1
		for j := 0; j < i; j++ {
			val := rnd.Float64() * 0.5
			A[i][j] = val
			A[j][i] = val
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CholeskyDecomposition(A)
	}
}

// BenchmarkWelfordEstimatorOnline tests streaming mean/covariance update.
func BenchmarkWelfordEstimatorOnline(b *testing.B) {
	d := 50
	n := 10000

	est := NewWelfordEstimator(d)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			x := make([]float64, d)
			for k := range x {
				x[k] = rand.NormFloat64()
			}
			est.Observe(x)
		}
	}
}

// BenchmarkEWMAVersion tests exponentially weighted version.
func BenchmarkWelfordEstimatorEWMA(b *testing.B) {
	d := 50
	n := 10000

	est := NewEWWelfordEstimator(d, 0.1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			x := make([]float64, d)
			for k := range x {
				x[k] = rand.NormFloat64()
			}
			est.Observe(x)
		}
	}
}

// BenchmarkFullPipelineCorrelationFlip end-to-end on correlation flip scenario.
func BenchmarkFullPipelineCorrelationFlip(b *testing.B) {
	d := 30
	n := 5000
	warmup := 1000

	ds := GenerateDataset(ScenarioCorrelationFlip, d, n, warmup, 0.15, 0.7, 42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sd := NewStreamingDetector(d, 0.975)

		// Warmup phase
		for j := 0; j < warmup; j++ {
			sd.Observe(ds.X[j])
		}

		// Evaluation phase
		for j := warmup; j < len(ds.X); j++ {
			sd.Observe(ds.X[j])
		}
	}
}

// BenchmarkSizeComparison compares different dimensionalities.
func BenchmarkSizeComparison(b *testing.B) {
	dimensions := []int{10, 20, 50, 100}

	for _, d := range dimensions {
		b.Run(fmt.Sprintf("d=%d", d), func(b *testing.B) {
			n := 2000
			x := make([][]float64, n)
			for i := range x {
				x[i] = make([]float64, d)
				for j := range x[i] {
					x[i][j] = rand.NormFloat64()
				}
			}

			sd := NewStreamingDetector(d, 0.975)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < n; j++ {
					sd.Observe(x[j])
				}
			}
		})
	}
}

// BenchmarkMemoryAllocation checks heap allocations.
func BenchmarkStreamingDetectorAllocs(b *testing.B) {
	d := 50
	n := 2000

	sd := NewStreamingDetector(d, 0.975)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < n; j++ {
			x := make([]float64, d)
			for k := range x {
				x[k] = rand.NormFloat64()
			}
			sd.Observe(x)
		}
	}
}
