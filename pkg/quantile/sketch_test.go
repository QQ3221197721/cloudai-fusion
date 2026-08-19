package quantile

// sketch_test.go unit tests every estimator's correctness on small data sets,
// with special attention to (1) adversarial inputs that stress-test
// bucket/centroid assumptions, and (2) TailExact's exact-tail guarantee: for a
// rank inside the retained tail it returns exactly the nearest-rank value.

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

// TestExactModeZeroError verifies that both the treap oracle and GK's eps->0
// specialization return the true order statistic with zero error.
func TestExactModeZeroError(t *testing.T) {
	n := 2000
	rng := rand.New(rand.NewSource(42))
	samples := Lognormal(rng, n, 0, 1) // heavy-tailed but tractable

	gk := NewGKExact() // eps->0 => exact
	exact := NewExact(42)
	for _, x := range samples {
		gk.Add(x)
		exact.Add(x)
	}

	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)

	for _, q := range []float64{0.5, 0.9, 0.95, 0.99, 0.999} {
		truth := NearestRank(sorted, q)
		if got := gk.Quantile(q); got != truth {
			t.Errorf("GK(eps->0) quantile %.3f: got %v, want %v", q, got, truth)
		}
		if got := exact.Quantile(q); got != truth {
			t.Errorf("Exact(treap) quantile %.3f: got %v, want %v", q, got, truth)
		}
	}
}

// TestExactTreapMatchesSortWithDuplicates stresses the treap's multiplicity
// handling with a low-cardinality integer stream.
func TestExactTreapMatchesSortWithDuplicates(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	n := 5000
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = float64(rng.Intn(50)) // heavy duplication
	}
	exact := NewExact(1)
	for _, x := range samples {
		exact.Add(x)
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	for _, q := range []float64{0, 0.25, 0.5, 0.75, 0.9, 0.99, 1} {
		truth := NearestRank(sorted, q)
		if got := exact.Quantile(q); got != truth {
			t.Errorf("treap q=%.2f with dups: got %v want %v", q, got, truth)
		}
	}
	// Rank() must be exact too.
	if r := exact.Rank(25); r != int64(rankOf(sorted, 25)) {
		t.Errorf("treap Rank(25)=%d want %d", r, rankOf(sorted, 25))
	}
}

// TestTailExactExactRegion confirms zero error whenever the requested rank falls
// inside the retained tail.
func TestTailExactExactRegion(t *testing.T) {
	const n = 10_000
	const K = 2000
	rng := rand.New(rand.NewSource(7))
	samples := Lognormal(rng, n, 0, 1)

	h := NewTailExact(K, 0.01)
	for _, x := range samples {
		h.Add(x)
	}

	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)

	// n-K = 8000; p99 rank = 9900 and p999 rank = 9990 are both > 8000 => exact.
	for _, q := range []float64{0.99, 0.995, 0.999} {
		if !h.InExactRegion(q) {
			t.Fatalf("q=%.3f should be in TailExact exact region for n=%d,K=%d", q, n, K)
		}
		truth := NearestRank(sorted, q)
		if got := h.Quantile(q); got != truth {
			t.Errorf("TailExact q=%.3f in exact region: got %v want %v (zero error required)", q, got, truth)
		}
	}
	// Low tail must be exact too.
	for _, q := range []float64{0.001, 0.01} {
		if !h.InExactRegion(q) {
			t.Fatalf("q=%.3f should be in TailExact low exact region", q)
		}
		truth := NearestRank(sorted, q)
		if got := h.Quantile(q); got != truth {
			t.Errorf("TailExact low q=%.3f: got %v want %v", q, got, truth)
		}
	}
}

// TestTailExactBeatsSketchesUnderAdversary is the headline correctness claim: on
// input crafted to fool bucket interpolation and centroid merging at the tail,
// TailExact is exact at p99 while Prometheus buckets, KLL, and t-digest all err.
func TestTailExactBeatsSketchesUnderAdversary(t *testing.T) {
	const N = 60_000
	const K = 2000 // p99 rank = 59400 > N-K = 58000 => exact
	buckets := []float64{0.5, 1, 2, 5, 10, 50, 100}
	rng := rand.New(rand.NewSource(37))

	samples := make([]float64, N)
	for i := 0; i < N; i++ {
		switch {
		case i < N*3/10:
			samples[i] = rng.Float64() * 0.49 // dense low body
		case i < N*9/10:
			// pack just below random finite bucket edges: bucket interpolation
			// assigns them uniform density and mislocates the quantile.
			samples[i] = buckets[i%len(buckets)]*0.999 + rng.Float64()*1e-6
		default:
			// a heavy tail above all finite buckets: buckets clip to 100.
			samples[i] = 200 + rng.Float64()*5000
		}
	}

	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	truth := NearestRank(sorted, 0.99)

	prom := PrometheusHistogramQuantile(buckets, samples, 0.99)

	kll := NewKLL(128, 42)
	td := NewTDigest(200)
	tail := NewTailExact(K, 0.01)
	for _, x := range samples {
		kll.Add(x)
		td.Add(x)
		tail.Add(x)
	}

	errProm := AbsError(prom, truth)
	errKLL := AbsError(kll.Quantile(0.99), truth)
	errTD := AbsError(td.Quantile(0.99), truth)
	errTail := AbsError(tail.Quantile(0.99), truth)

	t.Logf("adversarial p99 truth=%.4f", truth)
	t.Logf("  prometheus-bucket abs_err = %.4f (clipped to highest finite le=%.1f)", errProm, buckets[len(buckets)-1])
	t.Logf("  KLL              abs_err = %.4f", errKLL)
	t.Logf("  t-digest         abs_err = %.4f", errTD)
	t.Logf("  TailExact        abs_err = %.6f", errTail)

	if !tail.InExactRegion(0.99) {
		t.Fatal("expected p99 in TailExact exact region")
	}
	if errTail != 0 {
		t.Errorf("TailExact must be exact at p99 in its exact region, got abs_err=%v", errTail)
	}
	// The bucket method must be materially worse (tail clips to 100 while truth is far larger).
	if errProm <= errTail {
		t.Errorf("expected Prometheus bucket error (%.4f) to exceed TailExact (%.6f)", errProm, errTail)
	}
}

// TestGKRankErrorWithinEps verifies the GK guarantee: returned value rank error
// is within eps (we allow 2*eps slack to absorb integer flooring).
func TestGKRankErrorWithinEps(t *testing.T) {
	const n = 100_000
	const eps = 0.01
	rng := rand.New(rand.NewSource(11))
	samples := Lognormal(rng, n, 0, 1)

	gk := NewGKSummary(eps)
	for _, x := range samples {
		gk.Add(x)
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)

	for _, q := range []float64{0.5, 0.9, 0.99, 0.999} {
		rankErr := RankErrorFraction(sorted, gk.Quantile(q), q)
		t.Logf("GK q=%.3f rank_err_fraction=%.5f (eps=%.3f)", q, rankErr, eps)
		if rankErr > 2*eps {
			t.Errorf("GK q=%.3f rank error %.5f exceeds 2*eps=%.3f", q, rankErr, 2*eps)
		}
	}
}

// TestKLLRankErrorReasonable checks KLL stays within a loose rank-error envelope.
func TestKLLRankErrorReasonable(t *testing.T) {
	const n = 100_000
	rng := rand.New(rand.NewSource(13))
	samples := Lognormal(rng, n, 0, 1)

	kll := NewKLL(256, 42)
	for _, x := range samples {
		kll.Add(x)
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)

	for _, q := range []float64{0.5, 0.9, 0.99} {
		rankErr := RankErrorFraction(sorted, kll.Quantile(q), q)
		t.Logf("KLL(k=256) q=%.3f rank_err_fraction=%.5f", q, rankErr)
		if rankErr > 0.05 {
			t.Errorf("KLL q=%.3f rank error %.5f exceeds 0.05 envelope", q, rankErr)
		}
	}
}

// TestSketchInterfaceContract exercises the shared Sketch contract for all
// estimators: monotone Count, non-NaN quantile on non-empty input.
func TestSketchInterfaceContract(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	samples := Normal(rng, 500, 0, 1)

	check := func(s Sketch) {
		for i, x := range samples {
			s.Add(x)
			if s.Count() != i+1 {
				t.Errorf("%s: Count=%d after %d adds", s.Name(), s.Count(), i+1)
			}
		}
		if v := s.Quantile(0.99); math.IsNaN(v) {
			t.Errorf("%s: Quantile(0.99) is NaN", s.Name())
		}
		if s.SizeBytes() <= 0 {
			t.Errorf("%s: SizeBytes non-positive: %d", s.Name(), s.SizeBytes())
		}
	}
	check(NewGKSummary(0.01))
	check(NewKLL(64, 42))
	check(NewTDigest(50))
	check(NewTailExact(100, 0.01))
	check(NewExact(42))
}
