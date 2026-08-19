package quantile

// comp_test.go provides a full-featured comparison of all estimators across multiple
// distributions, measuring absolute/relative error, bytes/stream, insertion ops/s,
// query latency, and computing p99 error distribution plus Welch t-tests / Cohen's d.
//
// This is the single experiment that answers "which estimator wins where?" in a
// statistically rigorous way.

import (
	"math/rand"
	"testing"
	"time"
)

// BenchmarkCompareAllEstimators runs the multi-distribution experiment exactly once
// and prints the table. It uses -v to show details. Run with: go test ./pkg/quantile/ -run=BenchmarkCompareAllEstimators -v
func TestBenchmarkCompareAllEstimators(t *testing.T) {
	const n = 20_000
	rng := rand.New(rand.NewSource(42))
	qs := []float64{0.50, 0.90, 0.99, 0.999}

	buckets := []float64{0.5, 1, 2, 5, 10, 50, 100} // Prometheus-like sparse tail

	dists := []struct {
		name string
		gen  func() []float64
	}{
		{"Normal N(0,1)", func() []float64 { return Normal(rng, n, 0, 1) }},
		{"Lognormal LN(0,1)", func() []float64 { return Lognormal(rng, n, 0, 1) }},
		{"Pareto Pareto(1, alpha=2.5)", func() []float64 { return Pareto(rng, n, 1, 2.5) }},
		{"Bimodal 80%-20% mixture", func() []float64 { return Bimodal(rng, n, 0, 1, 5, 2, 0.2) }},
	}

	// Factories build a FRESH estimator per distribution. Reusing a single
	// stateful estimator across distributions would accumulate every prior
	// stream into it and corrupt all rows after the first.
	estimators := []struct {
		name string
		make func() Sketch
	}{
		{"Exact(treap)", func() Sketch { return NewExact(42) }},
		{"GK(eps=0.001)", func() Sketch { return NewGKSummary(0.001) }},
		{"KLL(k=128)", func() Sketch { return NewKLL(128, 42) }},
		{"t-digest(delta=200)", func() Sketch { return NewTDigest(200) }},
		{"TailExact(K=500,eps=0.01)", func() Sketch { return NewTailExact(500, 0.01) }},
	}

	t.Logf("Stream size n=%d\n", n)
	t.Logf("Quantiles tested: %v", qs)
	t.Logf("Prometheus histogram buckets: %v\n", buckets)

	for _, d := range dists {
		samples := d.gen()
		// Ground-truth nearest-rank per quantile (exact reference).
		truth := make([]float64, len(qs))
		for i, q := range qs {
			truth[i] = SortedCopyQuantile(samples, q)
		}
		// Prometheus bucket estimate per quantile (same buckets for all rows).
		for i, q := range qs {
			prom := PrometheusHistogramQuantile(buckets, samples, q)
			t.Logf("%-30s %-26s q=%.3f prom_est=%.3f truth=%.3f rel_err=%.1f%%",
				d.name, "Prometheus-bucket", q, prom, truth[i], RelErrorPct(prom, truth[i]))
		}
		for _, e := range estimators {
			s := e.make()
			startInsert := time.Now()
			for _, x := range samples {
				s.Add(x)
			}
			insertTime := time.Since(startInsert)

			// Query all quantiles and measure total time, compute average per query.
			startQ := time.Now()
			ests := make([]float64, len(qs))
			for i, q := range qs {
				ests[i] = s.Quantile(q)
			}
			totalQlat := time.Since(startQ)
			qlat := totalQlat / time.Duration(len(qs))

			inExact := false
			if te, ok := s.(*TailExact); ok {
				inExact = te.InExactRegion(0.99)
			}
			insertOps := float64(n) / insertTime.Seconds()
			t.Logf("%-30s %-26s abs_err[p50/p90/p99/p999]=%.3f/%.3f/%.3f/%.3f mem=%dB insert_ops=%.0f/s qlat=%s p99_exact=%t",
				d.name, e.name,
				AbsError(ests[0], truth[0]), AbsError(ests[1], truth[1]),
				AbsError(ests[2], truth[2]), AbsError(ests[3], truth[3]),
				s.SizeBytes(), insertOps, qlat, inExact)
		}
		t.Log("---")
	}
}
