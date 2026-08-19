package scheduler

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
)

// ============================================================================
// Benchmark: dense-k-subgraph solver performance (solve latency + allocation quality)
//
// Commands (PowerShell):
//   go test ./pkg/scheduler/ -bench="DenseK" -benchmem -count=5 -run=^$
//
// Metrics: ns/op is solve latency; allocation quality is reported separately by the
// BenchmarkDenseKApproximationRatio harness. All topologies are synthetic (no hardware).
// ============================================================================

// benchSolversFor returns the solver set to benchmark for a given k.
// The exact solver is included only for k≤8 (where branch-and-bound stays tractable).
func benchSolversFor(k int) []DenseKSolver {
	sols := make([]DenseKSolver, 0, 6)
	if k <= 8 {
		sols = append(sols, NewExactBB())
	}
	sols = append(sols,
		NewGreedy2Opt(8),
		&BinPackSolver{},
		&FirstFitSolver{},
		&K8sDefaultSolver{},
		NewRandomSolver(rand.New(rand.NewSource(42))),
	)
	return sols
}

// benchTopology runs each solver across a range of k on a fixed topology.
func benchTopology(b *testing.B, topo *BandwidthGraph, kMin, kMax int) {
	for k := kMin; k <= kMax; k++ {
		for _, s := range benchSolversFor(k) {
			s := s
			k := k
			b.Run(fmt.Sprintf("k%d/%s", k, s.Name()), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = s.Solve(topo, k)
				}
			})
		}
	}
}

// BenchmarkDenseKDGXH100 benchmarks solvers on an 8-GPU DGX H100 NVSwitch full mesh.
func BenchmarkDenseKDGXH100(b *testing.B) {
	topo := BuildDGXH100Topo()
	benchTopology(b, topo, 2, 8)
}

// BenchmarkDenseKDualSocketA100 benchmarks solvers on a dual-socket 4+4 A100 topology.
func BenchmarkDenseKDualSocketA100(b *testing.B) {
	topo := BuildDualSocketA100Topo()
	benchTopology(b, topo, 2, 8)
}

// BenchmarkDenseKMultiNode benchmarks solvers on a 2-node × 8-GPU cluster topology.
func BenchmarkDenseKMultiNode(b *testing.B) {
	topo := BuildMultiNodeClusterTopo(2, 8)
	benchTopology(b, topo, 2, 8)
}

// BenchmarkDenseKScaling measures exact vs greedy latency as N grows (fixed k=6).
func BenchmarkDenseKScaling(b *testing.B) {
	k := 6
	for _, n := range []int{10, 12, 14, 16} {
		topo := BuildRandomTopology(rand.New(rand.NewSource(int64(n))), n)
		for _, s := range []DenseKSolver{NewExactBB(), NewGreedy2Opt(8)} {
			s := s
			n := n
			b.Run(fmt.Sprintf("N%d/%s", n, s.Name()), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = s.Solve(topo, k)
				}
			})
		}
	}
}

// BenchmarkDenseKApproximationRatio reports the allocation-quality metric: the approximation
// ratio distribution of greedy-2opt vs the exact optimum across many random topologies.
// It performs one measurement pass of b.N samples and prints summary statistics.
func BenchmarkDenseKApproximationRatio(b *testing.B) {
	rng := rand.New(rand.NewSource(20260818))
	exact := NewExactBB()
	greedy := NewGreedy2Opt(8)

	ratios := make([]float64, 0, b.N)
	var greedyLatSum, exactLatSum float64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := StatMinN + rng.Intn(StatMaxN-StatMinN+1)
		kMax := min(8, n/2)
		if kMax < 2 {
			kMax = 2
		}
		k := 2 + rng.Intn(kMax-1)
		topo := BuildRandomTopology(rng, n)

		resExact := exact.Solve(topo, k)
		resGreedy := greedy.Solve(topo, k)

		r := 1.0
		if resExact.TotalWeight > 1e-9 {
			r = resGreedy.TotalWeight / resExact.TotalWeight
		}
		ratios = append(ratios, r)
		greedyLatSum += float64(resGreedy.LatencyNS)
		exactLatSum += float64(resExact.LatencyNS)
	}
	b.StopTimer()

	if len(ratios) == 0 {
		return
	}
	sort.Float64s(ratios)
	m := benchMean(ratios)
	sd := benchStdDev(ratios)
	fmt.Printf("\n[ApproximationRatio] samples=%d mean=%.6f stddev=%.6f min=%.6f p05=%.6f median=%.6f max=%.6f\n",
		len(ratios), m, sd, ratios[0], ratios[len(ratios)*5/100], ratios[len(ratios)/2], ratios[len(ratios)-1])
	fmt.Printf("[ApproximationRatio] mean greedy latency=%.0f ns, mean exact latency=%.0f ns, speedup=%.2fx\n",
		greedyLatSum/float64(len(ratios)), exactLatSum/float64(len(ratios)),
		exactLatSum/math.Max(greedyLatSum, 1))
}

func benchMean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func benchStdDev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	m := benchMean(xs)
	ss := 0.0
	for _, x := range xs {
		ss += (x - m) * (x - m)
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}
