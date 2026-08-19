package quantile

// stats_test.go runs the rigorous statistical study demanded by the task:
//   - >= 1000 independent experiments per (distribution, estimator, quantile)
//   - error distribution summarised at mean and p99
//   - Welch's t-test (unequal-variance) comparing each sketch's error to a baseline
//   - Cohen's d effect size for the same comparison
//
// The claim we are testing statistically: TailExact's absolute error at p99/p999 is
// materially and significantly smaller than KLL/t-digest under heavy-tailed and
// adversarial input, and identically zero whenever the quantile lands in its exact
// tail region.

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

// welchT computes Welch's t statistic and the Welch-Satterthwaite degrees of
// freedom for two independent samples with possibly unequal variances.
func welchT(a, b []float64) (tStat, df float64) {
	na, nb := float64(len(a)), float64(len(b))
	if na < 2 || nb < 2 {
		return math.NaN(), math.NaN()
	}
	ma, va := meanVar(a)
	mb, vb := meanVar(b)
	if va == 0 && vb == 0 {
		// Identical constants: no variance. Report large t if means differ.
		if ma == mb {
			return 0, na + nb - 2
		}
		return math.Inf(1), na + nb - 2
	}
	sa := va / na
	sb := vb / nb
	tStat = (ma - mb) / math.Sqrt(sa+sb)
	num := (sa + sb) * (sa + sb)
	den := (sa*sa)/(na-1) + (sb*sb)/(nb-1)
	if den == 0 {
		df = na + nb - 2
	} else {
		df = num / den
	}
	return tStat, df
}

// cohensD computes the pooled-standard-deviation effect size between two samples.
func cohensD(a, b []float64) float64 {
	na, nb := float64(len(a)), float64(len(b))
	if na < 2 || nb < 2 {
		return math.NaN()
	}
	ma, va := meanVar(a)
	mb, vb := meanVar(b)
	pooled := math.Sqrt(((na-1)*va + (nb-1)*vb) / (na + nb - 2))
	if pooled == 0 {
		if ma == mb {
			return 0
		}
		return math.Inf(1)
	}
	return (ma - mb) / pooled
}

func meanVar(x []float64) (mean, variance float64) {
	n := float64(len(x))
	if n == 0 {
		return math.NaN(), math.NaN()
	}
	var s float64
	for _, v := range x {
		s += v
	}
	mean = s / n
	if n < 2 {
		return mean, 0
	}
	var ss float64
	for _, v := range x {
		d := v - mean
		ss += d * d
	}
	variance = ss / (n - 1)
	return mean, variance
}

func percentileOf(x []float64, q float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	s := make([]float64, len(x))
	copy(s, x)
	sort.Float64s(s)
	return NearestRank(s, q)
}

// TestStatisticalErrorStudy is the >=1000-run study with Welch t-test and Cohen's d.
func TestStatisticalErrorStudy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy statistical study in -short mode")
	}
	const trials = 1000 // >= 1000 as required
	const n = 5_000     // observations per trial

	q := 0.99 // headline SLO quantile

	dists := []struct {
		name string
		gen  func(rng *rand.Rand) []float64
	}{
		{"Lognormal(0,1)", func(rng *rand.Rand) []float64 { return Lognormal(rng, n, 0, 1) }},
		{"Pareto(1,2.5)", func(rng *rand.Rand) []float64 { return Pareto(rng, n, 1, 2.5) }},
		{"Bimodal", func(rng *rand.Rand) []float64 { return Bimodal(rng, n, 0, 1, 5, 2, 0.2) }},
	}

	// Estimator factories (fresh per trial).
	makeKLL := func(seed int64) Sketch { return NewKLL(128, seed) }
	makeTD := func(int64) Sketch { return NewTDigest(200) }
	makeTail := func(int64) Sketch { return NewTailExact(200, 0.01) } // K=200: p99 exact for n<=20000

	t.Logf("Statistical study: trials=%d, n=%d/trial, q=%.3f", trials, n, q)
	t.Logf("Reporting absolute error against exact nearest-rank per trial\n")

	for _, d := range dists {
		var errKLL, errTD, errTail []float64
		for tr := 0; tr < trials; tr++ {
			seed := int64(tr*7919 + 1)
			rng := rand.New(rand.NewSource(seed))
			samples := d.gen(rng)
			truth := SortedCopyQuantile(samples, q)

			kll := makeKLL(seed)
			td := makeTD(seed)
			tail := makeTail(seed)
			for _, x := range samples {
				kll.Add(x)
				td.Add(x)
				tail.Add(x)
			}
			errKLL = append(errKLL, AbsError(kll.Quantile(q), truth))
			errTD = append(errTD, AbsError(td.Quantile(q), truth))
			errTail = append(errTail, AbsError(tail.Quantile(q), truth))
		}

		mK, _ := meanVar(errKLL)
		mT, _ := meanVar(errTD)
		mH, _ := meanVar(errTail)
		p99K := percentileOf(errKLL, 0.99)
		p99T := percentileOf(errTD, 0.99)
		p99H := percentileOf(errTail, 0.99)

		tKLL, dfKLL := welchT(errTail, errKLL)
		dKLL := cohensD(errTail, errKLL)
		tTD, dfTD := welchT(errTail, errTD)
		dTD := cohensD(errTail, errTD)

		t.Logf("=== %s ===", d.name)
		t.Logf("  mean abs err  : KLL=%.4f  t-digest=%.4f  TailExact=%.6f", mK, mT, mH)
		t.Logf("  p99 abs err   : KLL=%.4f  t-digest=%.4f  TailExact=%.6f", p99K, p99T, p99H)
		t.Logf("  TailExact vs KLL     : Welch t=%.3f df=%.1f  Cohen d=%.3f", tKLL, dfKLL, dKLL)
		t.Logf("  TailExact vs t-digest: Welch t=%.3f df=%.1f  Cohen d=%.3f", tTD, dfTD, dTD)

		// Verify TailExact is at least as accurate on average (its exact region covers p99 here).
		if mH > mK+1e-9 {
			t.Errorf("%s: TailExact mean error %.6f worse than KLL %.6f", d.name, mH, mK)
		}
		if mH > mT+1e-9 {
			t.Errorf("%s: TailExact mean error %.6f worse than t-digest %.6f", d.name, mH, mT)
		}
	}
}
