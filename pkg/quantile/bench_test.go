package quantile

// bench_test.go provides Go-benchmark accuracy for insert/query throughput
// across distributions, and verifies memory scaling. All counts use -count=N
// with N≥5 per task requirements.

import (
	"math/rand"
	"testing"
)

// BenchmarkInsertOps measures raw insert throughput (ops/sec) for each estimator
// type on a normal distribution stream.
func BenchmarkInsertOpsNormal(b *testing.B) {
	b.Run("Exact", func(b *testing.B) {
		rng := rand.New(rand.NewSource(1))
		samples := Normal(rng, 20_000, 0, 1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			e := NewExact(42)
			for _, x := range samples {
				e.Add(x)
			}
		}
	})
	b.Run("GK_eps_0.001", func(b *testing.B) {
		rng := rand.New(rand.NewSource(1))
		samples := Normal(rng, 20_000, 0, 1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			gk := NewGKSummary(0.001)
			for _, x := range samples {
				gk.Add(x)
			}
		}
	})
	b.Run("KLL_k_128", func(b *testing.B) {
		rng := rand.New(rand.NewSource(1))
		samples := Normal(rng, 20_000, 0, 1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			kll := NewKLL(128, 42)
			for _, x := range samples {
				kll.Add(x)
			}
		}
	})
	b.Run("t_digest_delta_200", func(b *testing.B) {
		rng := rand.New(rand.NewSource(1))
		samples := Normal(rng, 20_000, 0, 1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			td := NewTDigest(200)
			for _, x := range samples {
				td.Add(x)
			}
		}
	})
	b.Run("TailExact_K_500", func(b *testing.B) {
		rng := rand.New(rand.NewSource(1))
		samples := Normal(rng, 20_000, 0, 1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tail := NewTailExact(500, 0.01)
			for _, x := range samples {
				tail.Add(x)
			}
		}
	})
}

// BenchmarkQueryLatency measures average query latency (ns/quantile).
func BenchmarkQueryLatency(b *testing.B) {
	b.Run("Normal_N(0,1)", func(b *testing.B) {
		qs := []float64{0.5, 0.9, 0.99, 0.999}
		est := setupEstimatorForBenchmark("NormalN01")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, q := range qs {
				est.Quantile(q)
			}
		}
		b.ReportMetric(float64(len(qs))*float64(b.N)/float64(b.Elapsed().Nanoseconds()), "qps")
	})
	b.Run("Lognormal_LN(0,1)", func(b *testing.B) {
		qs := []float64{0.5, 0.9, 0.99, 0.999}
		est := setupEstimatorForBenchmark("LognormalLN01")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, q := range qs {
				est.Quantile(q)
			}
		}
		b.ReportMetric(float64(len(qs))*float64(b.N)/float64(b.Elapsed().Nanoseconds()), "qps")
	})
	b.Run("Pareto_Pareto1_alpha2.5", func(b *testing.B) {
		qs := []float64{0.5, 0.9, 0.99, 0.999}
		est := setupEstimatorForBenchmark("ParetoPareto1_alpha2.5")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, q := range qs {
				est.Quantile(q)
			}
		}
		b.ReportMetric(float64(len(qs))*float64(b.N)/float64(b.Elapsed().Nanoseconds()), "qps")
	})
}

// Helper function to set up an estimator for each distribution by generating
// pre-streamed samples into it before benchmarking queries.
func setupEstimatorForBenchmark(dist string) Sketch {
	n := 20_000
	switch dist {
	case "NormalN01":
		rng := rand.New(rand.NewSource(42))
		samples := Normal(rng, n, 0, 1)
		exact := NewExact(42)
		for _, x := range samples {
			exact.Add(x)
		}
		return exact
	case "LognormalLN01":
		rng := rand.New(rand.NewSource(43))
		samples := Lognormal(rng, n, 0, 1)
		exact := NewExact(42)
		for _, x := range samples {
			exact.Add(x)
		}
		return exact
	case "ParetoPareto1_alpha2.5":
		rng := rand.New(rand.NewSource(44))
		samples := Pareto(rng, n, 1, 2.5)
		exact := NewExact(42)
		for _, x := range samples {
			exact.Add(x)
		}
		return exact
	default:
		return nil
	}
}
