package scheduler

import (
	"math/rand"
	"testing"
)

// TestReductionLemma verifies the CLIQUE <=_p DkSP reduction lemma on small random instances.
func TestReductionLemma(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	exactSolver := NewExactBB()

	testCases := []struct {
		n     int
		k     int
		pEdge float64
	}{
		{5, 3, 0.7},
		{8, 4, 0.6},
		{10, 5, 0.5},
		{12, 6, 0.4},
	}

	for i, tc := range testCases {
		weight := make([][]float64, tc.n)
		for j := range weight {
			weight[j] = make([]float64, tc.n)
		}
		for j := 0; j < tc.n; j++ {
			for l := j + 1; l < tc.n; l++ {
				if rng.Float64() < tc.pEdge {
					weight[j][l], weight[l][j] = 1.0, 1.0
				}
			}
		}

		g := NewBandwidthGraph(make([]GPUVertex, tc.n), weight)

		cliqueSize, _ := newCLIQUEReducer().maxCliqueBrute(g)
		dksResult := exactSolver.Solve(g, tc.k)
		threshold := cliqueThreshold(tc.k)

		cliqueExists := cliqueSize >= tc.k
		dksMeetsThreshold := dksResult.TotalWeight >= threshold-1e-9

		t.Logf("Test #%d (n=%d,k=%d,p=%.2f): clique_of_k_exists=%v dks_value=%.2f threshold=%.2f",
			i, tc.n, tc.k, tc.pEdge, cliqueExists, dksResult.TotalWeight, threshold)

		if cliqueExists != dksMeetsThreshold {
			t.Errorf("Reduction lemma failed: expected (clique==%v) == (dks>=threshold)", cliqueExists)
		}
	}
}

// TestBaitStarTrap exposes naive BFS spreading failure vs greedy pack.
func TestBaitStarTrapVsNaiveBFS(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	atb := &adversarialTrapBuilder{rng: rng}
	naiveSolver := &naiveBFSSolver{}
	greedySolver := NewGreedy2Opt(10)

	hubDegree, clusterSize, targetK := 8, 10, 6
	g := atb.baitStarTrap(hubDegree, clusterSize)

	bfsResult := naiveSolver.solve(g, targetK)
	greedyResult := greedySolver.Solve(g, targetK)

	t.Logf("Bait-star trap (h=%d leaves, c=%d cluster): BFS_weight=%.2f greedy_weight=%.2f",
		hubDegree, clusterSize, bfsResult.TotalWeight, greedyResult.TotalWeight)

	if greedyResult.TotalWeight > bfsResult.TotalWeight+1e-9 {
		improvement := (greedyResult.TotalWeight - bfsResult.TotalWeight) / (bfsResult.TotalWeight + 1e-9) * 100
		t.Logf("[GREEDY WINS] density-aware pack beats topology-blind BFS by %.1f%%", improvement)
	}
}

// TestChainOfCliquesTrap evaluates weak-bridge resilience of greedy solver.
func TestChainOfCliquesTrapVsGreedy(t *testing.T) {
	rng := rand.New(rand.NewSource(456))
	atb := &adversarialTrapBuilder{rng: rng}
	naiveSolver := &naiveBFSSolver{}
	greedySolver := NewGreedy2Opt(15)

	numCliques, cliqueSize, targetK := 5, 8, 12
	g := atb.chainOfCliquesTrap(numCliques, cliqueSize)

	bfsResult := naiveSolver.solve(g, targetK)
	greedyResult := greedySolver.Solve(g, targetK)

	t.Logf("Chain-of-cliques (%dx%d): BFS_weight=%.2f greedy_weight=%.2f gap=%.2f",
		numCliques, cliqueSize, bfsResult.TotalWeight, greedyResult.TotalWeight,
		greedyResult.TotalWeight-bfsResult.TotalWeight)

	if greedyResult.TotalWeight > bfsResult.TotalWeight+1e-9 {
		t.Logf("[GREEDY WINS] greedy pack recovers dense cliques better than BFS")
	}
}

// TestTopologyClassComparison benchmarks DkS quality across scale-free / ER / A100 mesh.
func TestTopologyClassComparison(t *testing.T) {
	rng := rand.New(rand.NewSource(789))
	solver := NewGreedy2Opt(20)
	exactSolver := NewExactBB()

	scaleFreeConfig := scaleFreeConfig{n: 16, m: 2}
	erdosRenyiConfig := erdosRenyiConfig{n: 16, p: 0.25}
	kValues := []int{2, 3, 4, 5, 6, 7, 8}

	t.Log("=== Dense-k-Subgraph Greedy/Optimal Ratio ===")

	tests := []struct {
		name  string
		build func() *BandwidthGraph
	}{
		{"Scale-Free", func() *BandwidthGraph { return buildScaleFreeTopo(scaleFreeConfig, rng) }},
		{"Erdos-Renyi", func() *BandwidthGraph { return buildErdosRenyiTopo(erdosRenyiConfig, rng) }},
		{"Real-A100-Mesh", func() *BandwidthGraph { return buildRealA100Mesh() }},
	}

	for _, test := range tests {
		var totalQuality float64
		count := 0
		t.Logf("[%s]", test.name)

		for _, k := range kValues {
			g := test.build()
			exactRes := exactSolver.Solve(g, k)
			greedyRes := solver.Solve(g, k)

			quality := 1.0
			if exactRes.TotalWeight > 1e-9 {
				quality = greedyRes.TotalWeight / exactRes.TotalWeight
			}
			t.Logf("  k=%-2d optimal=%.2f greedy=%.2f ratio=%.1f%% speedup=%.2fx",
				k, exactRes.TotalWeight, greedyRes.TotalWeight, quality*100,
				float64(exactRes.LatencyNS)/float64(greedyRes.LatencyNS+1))

			totalQuality += quality
			count++
		}
		t.Logf("%s AVG: greedy achieves %.1f%% of optimal", test.name, totalQuality/float64(count)*100)
	}
}

// TestDenseKSubgraphOnRealA100Mesh skipped — requires real multi-GPU NVLink (SYNTHETIC per M3 report).
func TestDenseKSubgraphOnRealA100Mesh(t *testing.T) {
	t.Skip("Skipping hardware-dependent test (real 8xA100 unavailable per M3 validation report)")
}
