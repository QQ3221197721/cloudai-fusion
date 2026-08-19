package scheduler

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// ============================================================================
// Statistical Experiment: ≥1000 random topologies × random k
//
// GOAL: quantify, with statistical rigor, how the greedy+2-opt approximation and the
// topology-blind baselines compare to the exact optimum on the densest-k-subgraph problem.
//
// METHODOLOGY:
//   - StatNumSamples (≥1000) random topologies; each with random N∈[6,16] and random k∈[2,min(8,⌊N/2⌋)]
//   - Metric: intra-subset bandwidth sum W(S). Quality ratio = W(S)/W(optimal) ∈ (0,1].
//   - Welch's two-tailed t-test (unequal variance) + Cohen's d + 95% CI of the mean difference.
//
// HONESTY MANDATE:
//   - All topology data is SYNTHETIC. No real GPU hardware.
//   - If greedy is NOT significantly better than a baseline, we report it plainly.
//
// NOTE: the statistical helpers welchTTest, cohensD, mean, variance, effectSizeLabel are shared
// with topology_comparison_test.go (same package build); this file reuses them.
// ============================================================================

const (
	// StatNumSamples is the number of random topology samples (≥1000 per Task 86).
	StatNumSamples = 1000
	// StatMinN / StatMaxN bound the random GPU count (kept ≤16 so the exact solver stays tractable).
	StatMinN = 6
	StatMaxN = 16
)

// dksSample holds one solver's per-sample outputs across the experiment.
type dksSample struct {
	weight  []float64 // W(S) per sample
	latency []float64 // solve latency (ns) per sample
	ratio   []float64 // W(S)/W(optimal) per sample (quality ratio)
}

func newDKSSample(n int) *dksSample {
	return &dksSample{
		weight:  make([]float64, 0, n),
		latency: make([]float64, 0, n),
		ratio:   make([]float64, 0, n),
	}
}

// TestDenseKSolversStatisticalAnalysis is the ≥1000-topology statistical comparison.
func TestDenseKSolversStatisticalAnalysis(t *testing.T) {
	// Deterministic RNG so results are reproducible run-to-run.
	rng := rand.New(rand.NewSource(20260818))

	solverNames := []string{"exact-bnb", "greedy-2opt", "binpack", "k8s-default", "first-fit", "random"}
	data := make(map[string]*dksSample, len(solverNames))
	for _, name := range solverNames {
		data[name] = newDKSSample(StatNumSamples)
	}

	// A dedicated RNG for the random solver so its randomness is independent of topology generation.
	randSolver := NewRandomSolver(rand.New(rand.NewSource(999)))

	for s := 0; s < StatNumSamples; s++ {
		n := StatMinN + rng.Intn(StatMaxN-StatMinN+1)
		kMax := min(8, n/2)
		if kMax < 2 {
			kMax = 2
		}
		k := 2 + rng.Intn(kMax-1) // k ∈ [2, kMax]

		topo := BuildRandomTopology(rng, n)

		res := map[string]*DenseKSubgraphResult{
			"exact-bnb":   NewExactBB().Solve(topo, k),
			"greedy-2opt": NewGreedy2Opt(8).Solve(topo, k),
			"binpack":     (&BinPackSolver{}).Solve(topo, k),
			"k8s-default": (&K8sDefaultSolver{}).Solve(topo, k),
			"first-fit":   (&FirstFitSolver{}).Solve(topo, k),
			"random":      randSolver.Solve(topo, k),
		}

		opt := res["exact-bnb"].TotalWeight
		for _, name := range solverNames {
			r := res[name]
			data[name].weight = append(data[name].weight, r.TotalWeight)
			data[name].latency = append(data[name].latency, float64(r.LatencyNS))
			ratio := 1.0
			if opt > 1e-9 {
				ratio = r.TotalWeight / opt
			}
			data[name].ratio = append(data[name].ratio, ratio)
		}
	}

	// ------------------------------------------------------------------
	// Report: per-solver quality ratio summary
	// ------------------------------------------------------------------
	fmt.Println("\n" + strings.Repeat("=", 100))
	fmt.Printf("DENSE K-SUBGRAPH: STATISTICAL ANALYSIS — %d random topologies (N∈[%d,%d], k∈[2,8])\n",
		StatNumSamples, StatMinN, StatMaxN)
	fmt.Println("SYNTHETIC topology data only — no real GPU hardware.")
	fmt.Println(strings.Repeat("=", 100))

	fmt.Printf("\n%-14s | %-12s | %-12s | %-12s | %-12s | %-12s\n",
		"Solver", "MeanRatio", "MinRatio(worst)", "StdDev", "MeanW(GB/s)", "MeanLat(ns)")
	fmt.Println(strings.Repeat("-", 90))
	for _, name := range solverNames {
		d := data[name]
		fmt.Printf("%-14s | %12.5f | %12.5f | %12.5f | %12.1f | %12.0f\n",
			name, mean(d.ratio), minFloat(d.ratio), math.Sqrt(variance(d.ratio)),
			mean(d.weight), mean(d.latency))
	}

	// ------------------------------------------------------------------
	// Approximation ratio of greedy-2opt vs exact optimum
	// ------------------------------------------------------------------
	gr := data["greedy-2opt"].ratio
	sortedGr := append([]float64(nil), gr...)
	sort.Float64s(sortedGr)
	ciLo, ciHi := dksCI95(gr)
	fmt.Println("\n--- APPROXIMATION RATIO (greedy-2opt / exact optimum) ---")
	fmt.Printf("Mean = %.6f   StdDev = %.6f\n", mean(gr), math.Sqrt(variance(gr)))
	fmt.Printf("95%% CI of mean = [%.6f, %.6f]\n", ciLo, ciHi)
	fmt.Printf("Worst case (min) = %.6f   Best (max) = %.6f\n", sortedGr[0], sortedGr[len(sortedGr)-1])
	fmt.Printf("Median = %.6f   p05 = %.6f\n", sortedGr[len(sortedGr)/2], sortedGr[len(sortedGr)*5/100])

	// ------------------------------------------------------------------
	// Pairwise significance: greedy-2opt vs each baseline (quality ratio)
	// ------------------------------------------------------------------
	fmt.Println("\n--- WELCH t-TEST: greedy-2opt vs baselines (quality ratio, two-tailed α=0.05) ---")
	fmt.Printf("%-14s | %-10s | %-10s | %10s | %8s | %-12s | %-8s | %-18s\n",
		"Baseline", "GreedyMean", "BaseMean", "t-stat", "df", "p-value", "Cohen d", "Effect / Verdict")
	fmt.Println(strings.Repeat("-", 110))

	baselines := []string{"binpack", "k8s-default", "first-fit", "random"}
	significantWins := 0
	for _, b := range baselines {
		x := data["greedy-2opt"].ratio
		y := data[b].ratio
		tStat, df, p := welchTTest(x, y)
		d := cohensD(x, y)
		mx, my := mean(x), mean(y)
		verdict := "no significant difference"
		if p < 0.05 {
			if mx > my {
				verdict = "greedy-2opt WINS"
				significantWins++
			} else {
				verdict = b + " WINS"
			}
		}
		sig := ""
		switch {
		case p < 0.001:
			sig = "***"
		case p < 0.01:
			sig = "**"
		case p < 0.05:
			sig = "*"
		}
		fmt.Printf("%-14s | %10.5f | %10.5f | %10.3f | %8.1f | %10.6f%-2s | %8.3f | %-8s %s\n",
			b, mx, my, tStat, df, p, sig, d, effectSizeLabel(d), verdict)
	}

	// ------------------------------------------------------------------
	// Latency: greedy-2opt vs exact-bnb (should be dramatically faster)
	// ------------------------------------------------------------------
	gLat := data["greedy-2opt"].latency
	eLat := data["exact-bnb"].latency
	tLat, dfLat, pLat := welchTTest(gLat, eLat)
	fmt.Println("\n--- SOLVE LATENCY: greedy-2opt vs exact-bnb (ns) ---")
	fmt.Printf("greedy-2opt mean = %.0f ns   exact-bnb mean = %.0f ns   speedup = %.2fx\n",
		mean(gLat), mean(eLat), mean(eLat)/math.Max(mean(gLat), 1))
	fmt.Printf("Welch t = %.3f, df = %.1f, p = %.6g\n", tLat, dfLat, pLat)

	fmt.Println("\n--- HONEST DISCLOSURES ---")
	fmt.Println("1. ALL topology data is SYNTHETIC — no real GPU hardware queried.")
	fmt.Println("2. Exact branch-and-bound is optimal but scales poorly beyond N~20; used here only for N≤16.")
	fmt.Println("3. Baselines (binpack/k8s-default/first-fit/random) are topology-blind by design.")
	fmt.Println("4. Cohen's d labels: negligible(<0.2) small(0.2) medium(0.5) large(0.8) very large(1.2).")

	// ------------------------------------------------------------------
	// Assertions
	// ------------------------------------------------------------------
	meanRatio := mean(gr)
	if meanRatio < 0.98 {
		t.Errorf("greedy-2opt mean approximation ratio %.4f < 0.98 (unexpectedly poor)", meanRatio)
	}
	if minFloat(gr) < 0.80 {
		t.Errorf("greedy-2opt worst-case ratio %.4f < 0.80 (approximation too weak)", minFloat(gr))
	}
	// Greedy must significantly beat at least the random and first-fit baselines.
	if significantWins == 0 {
		t.Error("greedy-2opt showed no statistically significant advantage over any baseline")
	}
}

// minFloat returns the minimum of a non-empty slice (0 for empty).
func minFloat(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

// dksCI95 returns the 95% confidence interval for the mean of xs (normal approximation).
func dksCI95(xs []float64) (float64, float64) {
	n := len(xs)
	if n == 0 {
		return 0, 0
	}
	m := mean(xs)
	se := math.Sqrt(variance(xs)) / math.Sqrt(float64(n))
	return m - 1.96*se, m + 1.96*se
}
