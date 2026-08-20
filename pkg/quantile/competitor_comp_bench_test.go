//go:build compbench

package quantile

import (
	"math/rand"
	"testing"

	"github.com/DataDog/sketches-go/ddsketch"
	tdigest "github.com/caio/go-tdigest"
)

// This file benchmarks our TailExact vs two real competitors:
// - github.com/caio/go-tdigest v3.1.0 (cluster-based centroid aggregation)
// - github.com/DataDog/sketches-go v1.4.8 (DDSketch, relative-error guarantee)
//
// Run: go test ./pkg/quantile/ -tags compbench -bench=. -benchmem -count=6 -run=^$

var _rng = rand.New(rand.NewSource(42))

// ddRelAccuracy is DDSketch's relative-accuracy guarantee (1%).
const ddRelAccuracy = 0.01

// BenchmarkQuantile_DDSketch_Insert inserts points into DDSketch and measures ops/s
func BenchmarkQuantile_DDSketch_Insert(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	d := make([]float64, b.N)
	for i := range d {
		d[i] = rng.Float64() + 1e-9 // DDSketch requires strictly positive values
	}

	sk, _ := ddsketch.NewDefaultDDSketch(ddRelAccuracy)
	b.ResetTimer()
	b.ReportAllocs()
	for _, x := range d {
		_ = sk.Add(x)
	}
}

// BenchmarkQuantile_DDSketch_Quantile queries p50/p90/p99 on full DDSketch
func BenchmarkQuantile_DDSketch_Quantile(b *testing.B) {
	const n = 20_000
	rng := rand.New(rand.NewSource(42))
	d := make([]float64, n)
	for i := range d {
		d[i] = rng.Float64() + 1e-9
	}

	sk, _ := ddsketch.NewDefaultDDSketch(ddRelAccuracy)
	for _, x := range d {
		_ = sk.Add(x)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = sk.GetValueAtQuantile(0.5)
		_, _ = sk.GetValueAtQuantile(0.9)
		_, _ = sk.GetValueAtQuantile(0.99)
	}
}

// BenchmarkQuantile_Tdigest_Insert inserts points into t-digest and measures ops/s
func BenchmarkQuantile_Tdigest_Insert(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	d := make([]float64, b.N)
	for i := range d {
		d[i] = rng.Float64()
	}

	td, _ := tdigest.New()
	b.ResetTimer()
	b.ReportAllocs()
	for _, x := range d {
		_ = td.Add(x)
	}
}

// BenchmarkQuantile_Tdigest_Quantile queries p50/p90/p99 on full t-digest
func BenchmarkQuantile_Tdigest_Quantile(b *testing.B) {
	const n = 20_000
	rng := rand.New(rand.NewSource(42))
	d := make([]float64, n)
	for i := range d {
		d[i] = rng.Float64()
	}

	td, _ := tdigest.New()
	for _, x := range d {
		_ = td.Add(x)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = td.Quantile(0.5)
		_ = td.Quantile(0.9)
		_ = td.Quantile(0.99)
	}
}

// tailExactInsert runs insertion on TailExact(K=500,eps=0.01) for fair comparison
func BenchmarkQuantile_TailExact_Insert(b *testing.B) {
	rng := rand.New(rand.NewSource(42))
	d := make([]float64, b.N)
	for i := range d {
		d[i] = rng.Float64()
	}

	te := NewTailExact(500, 0.01)
	b.ResetTimer()
	b.ReportAllocs()
	for _, x := range d {
		te.Add(x)
	}
}

// tailExactQuery measures Quantile(p50/p90/p99) throughput on TailExact
func BenchmarkQuantile_TailExact_Query(b *testing.B) {
	const n = 20_000
	rng := rand.New(rand.NewSource(42))
	d := make([]float64, n)
	for i := range d {
		d[i] = rng.Float64()
	}

	te := NewTailExact(500, 0.01)
	for _, x := range d {
		te.Add(x)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = te.Quantile(0.5)
		_ = te.Quantile(0.9)
		_ = te.Quantile(0.99)
	}
}

// errorBench computes absolute error on a known distribution for both TailExact & t-digest
// Truth is computed from the same stream using exact sorted method
func TestError_Comparison_TdigestVsTailExact(t *testing.T) {
	const n = 50_000
	rng := rand.New(rand.NewSource(42))
	d := make([]float64, n)
	for i := range d {
		d[i] = rng.Float64()
	}

	// Ground truth
	truthP50 := SortedCopyQuantile(d, 0.5)
	truthP99 := SortedCopyQuantile(d, 0.99)

	// t-digest
	td, _ := tdigest.New()
	for _, x := range d {
		_ = td.Add(x)
	}
	p50TD := td.Quantile(0.5)
	p99TD := td.Quantile(0.99)

	// DDSketch (relative accuracy 1%)
	sk, _ := ddsketch.NewDefaultDDSketch(ddRelAccuracy)
	for _, x := range d {
		_ = sk.Add(x + 1e-9)
	}
	p50DD, _ := sk.GetValueAtQuantile(0.5)
	p99DD, _ := sk.GetValueAtQuantile(0.99)

	// TailExact
	te := NewTailExact(500, 0.01)
	for _, x := range d {
		te.Add(x)
	}
	p50TE := te.Quantile(0.5)
	p99TE := te.Quantile(0.99)

	t.Logf("Distribution: Uniform[0,1], n=%d", n)
	t.Logf("Ground truth: p50=%.6f p99=%.6f", truthP50, truthP99)
	t.Logf("t-digest:     p50=%.6f err=%.6f", p50TD, AbsError(p50TD, truthP50))
	t.Logf("DDSketch:     p50=%.6f err=%.6f", p50DD, AbsError(p50DD, truthP50))
	t.Logf("TailExact:    p50=%.6f err=%.6f", p50TE, AbsError(p50TE, truthP50))
	t.Logf("---")
	t.Logf("t-digest:     p99=%.6f err=%.6f", p99TD, AbsError(p99TD, truthP99))
	t.Logf("DDSketch:     p99=%.6f err=%.6f", p99DD, AbsError(p99DD, truthP99))
	t.Logf("TailExact:    p99=%.6f err=%.6f", p99TE, AbsError(p99TE, truthP99))
}
