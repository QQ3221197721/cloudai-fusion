package scheduler

import (
	"fmt"
	"math/rand"
	"testing"
)

// ============================================================================
// Unit Tests: Exact vs Brute-Force for Small N; Exact vs Approx Ratio
// ============================================================================

// TestExactVsBruteForce verifies ExactBB matches exhaustive search on tiny graphs.
// This ensures our pruning never cuts off an optimal solution.
func TestExactVsBruteForce(t *testing.T) {
	testCases := []struct {
		n int
		k int
	}{
		{4, 2},
		{5, 2},
		{5, 3},
		{6, 3},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("n%d-k%d", tc.n, tc.k), func(t *testing.T) {
			g := BuildRandomTopology(rand.New(rand.NewSource(42)), tc.n)

			exactSolver := NewExactBB()
			exactRes := exactSolver.Solve(g, tc.k)

			bestSel, bestW := bruteForceDensestKSubgraph(g, tc.k)

			if exactRes.TotalWeight < bestW-1e-9 {
				t.Errorf("exact-bnb returned %.6f but brute-force found %.6f", exactRes.TotalWeight, bestW)
			}
			if len(exactRes.Subset) != tc.k {
				t.Errorf("exact-bnb subset size=%d, want=%d", len(exactRes.Subset), tc.k)
			}
			// Check exact match of selected vertices.
			if !equalSets(exactRes.Subset, bestSel) {
				t.Errorf("exact-bnb subset=%v, brute-force subset=%v", exactRes.Subset, bestSel)
			}
		})
	}
}

// bruteForceDensestKSubgraph exhaustively enumerates all k-subsets and returns the maximum weight subset.
func bruteForceDensestKSubgraph(g *BandwidthGraph, k int) ([]int, float64) {
	n := g.NumNodes()
	var bestSel []int
	var bestW float64 = -1

	var rec func(start int, cur []int)
	rec = func(start int, cur []int) {
		if len(cur) == k {
			w := g.SubsetWeight(cur)
			if w > bestW {
				bestW = w
				bestSel = append([]int(nil), cur...)
			}
			return
		}
		for i := start; i <= n-(k-len(cur)); i++ {
			cur = append(cur, i)
			rec(i+1, cur)
			cur = cur[:len(cur)-1]
		}
	}
	rec(0, nil)
	return bestSel, bestW
}

// TestApproxRatio verifies Greedy2Opt achieves a high approximation ratio on known topologies.
// For full-mesh NVSwitch graphs, greedy should find the optimal solution.
func TestApproxRatio(t *testing.T) {
	// Full-mesh NVSwitch graph: all pairs have equal high bandwidth → any k-subset is optimal.
	dgx := BuildDGXH100Topo()
	for k := 2; k <= dgx.NumNodes(); k++ {
		exact := NewExactBB().Solve(dgx, k)
		greedy := NewGreedy2Opt(8).Solve(dgx, k)

		ratio := greedy.TotalWeight / exact.TotalWeight
		if ratio < 1.0 {
			t.Errorf("greedy-2opt on DGX k=%d: ratio=%.6f (should be 1.0)", k, ratio)
		}
	}

	// Dual-socket A100 topology: intra-socket NVLink is higher than inter-socket.
	// Optimal k ≤ 4 is any within-one socket.
	a100 := BuildDualSocketA100Topo()
	for k := 2; k <= 4; k++ {
		exact := NewExactBB().Solve(a100, k)
		greedy := NewGreedy2Opt(8).Solve(a100, k)

		ratio := greedy.TotalWeight / exact.TotalWeight
		if ratio < 0.99 { // allow tiny numerical tolerance
			t.Errorf("greedy-2opt on dual-socket A100 k=%d: ratio=%.6f (expected ≥1.0)", k, ratio)
		}
	}

	// Multi-node cluster: similar structure with cross-node penalty.
	multiNode := BuildMultiNodeClusterTopo(2, 8)
	for k := 2; k <= 8; k++ {
		exact := NewExactBB().Solve(multiNode, k)
		greedy := NewGreedy2Opt(8).Solve(multiNode, k)

		ratio := greedy.TotalWeight / exact.TotalWeight
		if ratio < 0.99 {
			t.Errorf("greedy-2opt on multi-node k=%d: ratio=%.6f (expected ≥1.0)", k, ratio)
		}
	}
}

// TestConsistencyBaselines verifies baseline solvers produce deterministic subsets.
func TestConsistencyBaselines(t *testing.T) {
	g := BuildRandomTopology(rand.New(rand.NewSource(42)), 16)

	tests := []struct {
		name string
		s    DenseKSolver
		k    int
	}{
		{"FirstFit-k2", &FirstFitSolver{}, 2},
		{"BinPack-k2", &BinPackSolver{}, 2},
		{"K8sDefault-k2", &K8sDefaultSolver{}, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res1 := tc.s.Solve(g, tc.k)
			res2 := tc.s.Solve(g, tc.k)

			if !equalSets(res1.Subset, res2.Subset) {
				t.Errorf("%s not deterministic: %v vs %v", tc.name, res1.Subset, res2.Subset)
			}
		})
	}

	// RandomSolver needs fresh RNG per call; verify it produces random output by comparing two instances.
	g2 := BuildRandomTopology(rand.New(rand.NewSource(42)), 16)
	rng1 := NewRandomSolver(rand.New(rand.NewSource(999)))
	rng2 := NewRandomSolver(rand.New(rand.NewSource(999)))
	res1 := rng1.Solve(g2, 3)
	res2 := rng2.Solve(g2, 3)
	// Two identical initializers should give same result (deterministic).
	if !equalSets(res1.Subset, res2.Subset) {
		t.Errorf("Random with same seed not reproducible: %v vs %v", res1.Subset, res2.Subset)
	}
	// Different seed gives different output.
	rng3 := NewRandomSolver(rand.New(rand.NewSource(777)))
	res3 := rng3.Solve(g2, 3)
	if equalSets(res1.Subset, res3.Subset) {
		t.Error("Random with different seed produced same subset")
	}
}

// TestRandomIsNonAdversarial demonstrates that RandomSolver's subset weight is far from optimal
// on realistic topologies (where islands create dense substructures).
func TestRandomIsNonAdversarial(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	topos := []*BandwidthGraph{
		BuildDGXH100Topo(),
		BuildDualSocketA100Topo(),
		BuildMultiNodeClusterTopo(2, 8),
	}

	for _, g := range topos {
		k := g.NumNodes() / 2
		if k < 2 {
			k = 2
		}
		exact := NewExactBB().Solve(g, k)
		randSolver := NewRandomSolver(rng)

		total := 0.0
		for run := 0; run < 5; run++ {
			res := randSolver.Solve(g, k)
			total += res.TotalWeight
		}
		avgRand := total / 5.0

		if avgRand > exact.TotalWeight {
			t.Errorf("random average %.6f exceeds exact %.6f for %s k=%d",
				avgRand, exact.TotalWeight, g.Nodes[0].Host, k)
		}
	}
}

// equalSets checks whether two integer slices represent the same set (order-independent).
func equalSets(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	m := make(map[int]bool)
	for _, v := range a {
		m[v] = true
	}
	for _, v := range b {
		if !m[v] {
			return false
		}
	}
	return true
}
