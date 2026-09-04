// Package scheduler provides theoretical foundations for DkSP reduction proofs (Task #263).
package scheduler

import (
	"math/rand"
	"time"
)

// ============================================================================
// PART 1: CLIQUE -> DkSP reduction machinery
// ============================================================================

// cliquereducer implements polynomial-time reduction from MAX-CLIQUE to DkSP.
type cliquereducer struct{}

func newCLIQUEReducer() *cliquereducer { return &cliquereducer{} }

// toUnitWeightGraph maps weighted graph to CLIQUE input form (binary adjacency).
func (r *cliquereducer) toUnitWeightGraph(g *BandwidthGraph) *BandwidthGraph {
	n := g.NumNodes()
	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			if i != j && g.GetWeight(i, j) > 0 {
				weight[i][j] = 1.0
			}
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

// cliqueThreshold returns k(k-1)/2 edges required for size-k clique.
func cliqueThreshold(k int) float64 {
	if k < 2 {
		return 0
	}
	return float64(k*(k-1)) / 2.0
}

// maxCliqueBrute enumerates all subsets to find maximum clique (exact for n<=22).
func (r *cliquereducer) maxCliqueBrute(g *BandwidthGraph) (int, []int) {
	n := g.NumNodes()
	adj := make([][]bool, n)
	for i := 0; i < n; i++ {
		adj[i] = make([]bool, n)
		for j := 0; j < n; j++ {
			adj[i][j] = (i != j) && (g.GetWeight(i, j) >= 1.0-1e-9)
		}
	}

	bestSize := 0
	var bestSet []int

	for mask := 1; mask < (1 << uint(n)); mask++ {
		size := 0
		for b := 0; b < n; b++ {
			if (mask & (1 << uint(b))) != 0 {
				size++
			}
		}
		if size <= bestSize || size < 2 {
			continue
		}

		subset := make([]int, 0, size)
		for b := 0; b < n; b++ {
			if (mask & (1 << uint(b))) != 0 {
				subset = append(subset, b)
			}
		}

		isClique := true
	outerLoop:
		for i := 0; i < size; i++ {
			for j := i + 1; j < size; j++ {
				if !adj[subset[i]][subset[j]] {
					isClique = false
					break outerLoop
				}
			}
		}

		if isClique {
			bestSize = size
			bestSet = subset
		}
	}
	return bestSize, bestSet
}

// ============================================================================
// PART 2: Topology generators (all synthetic — multi-GPU NVLink unmeasured)
// ============================================================================

// scaleFreeConfig: Barabasi-Albert preferential attachment parameters.
type scaleFreeConfig struct {
	n int // vertices
	m int // edges per new node (m0=m+1 initial core)
}

// erdosRenyiConfig: G(n,p) random graph parameters.
type erdosRenyiConfig struct {
	n int     // vertices
	p float64 // edge probability
}

// buildScaleFreeTopo constructs scale-free network via preferential attachment.
func buildScaleFreeTopo(cfg scaleFreeConfig, rng *rand.Rand) *BandwidthGraph {
	n, m := cfg.n, cfg.m
	if n < 2 {
		panic("scale-free: n>=2")
	}
	if m < 1 {
		m = 1
	} else if m >= n {
		m = n - 1
	}

	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
	}

	degree := make([]int, n)
	addEdge := func(u, v int) {
		if u == v || weight[u][v] > 0 {
			return
		}
		w := 600.0 + rng.Float64()*50
		weight[u][v], weight[v][u] = w, w
		degree[u]++
		degree[v]++
	}

	m0 := m + 1
	if m0 > n {
		m0 = n
	}
	for i := 0; i < m0; i++ {
		for j := i + 1; j < m0; j++ {
			addEdge(i, j)
		}
	}

	for i := m0; i < n; i++ {
		selected := make(map[int]bool)
		for len(selected) < m {
			totalDegree := 0
			for j := 0; j < i; j++ {
				totalDegree += degree[j]
			}
			if totalDegree == 0 {
				j := rng.Intn(i)
				selected[j] = true
				continue
			}
			r := rng.Intn(totalDegree)
			accum := 0
			for j := 0; j < i; j++ {
				accum += degree[j]
				if accum > r && !selected[j] {
					selected[j] = true
					break
				}
			}
		}
		for j := range selected {
			addEdge(i, j)
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

// buildErdosRenyiTopo constructs G(n,p) uniform-random graph.
func buildErdosRenyiTopo(cfg erdosRenyiConfig, rng *rand.Rand) *BandwidthGraph {
	n, p := cfg.n, cfg.p
	if n < 2 {
		panic("erdos-renyi: n>=2")
	}

	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if rng.Float64() < p {
				w := 600.0 + rng.Float64()*50
				weight[i][j], weight[j][i] = w, w
			}
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

// buildRealA100Mesh constructs canonical HGX/DGX A100 topology (SYNTHETIC per M3 report).
func buildRealA100Mesh() *BandwidthGraph {
	const n = 8
	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dist := absDiff(i, j)
			var w float64
			switch {
			case dist == 1:
				w = 900
			case dist <= 3:
				w = 600
			default:
				w = 32
			}
			weight[i][j], weight[j][i] = w, w
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// ============================================================================
// PART 3: NaiveBFSSolver — models K8s spreading (topology-blind traversal order)
// ============================================================================

// naiveBFSSolver performs BFS placement ignoring bandwidth weights entirely.
type naiveBFSSolver struct{}

func newNaiveBFSSolver() *naiveBFSSolver { return &naiveBFSSolver{} }

// solve performs BFS from node 0 until k vertices collected.
func (ns *naiveBFSSolver) solve(g *BandwidthGraph, k int) *DenseKSubgraphResult {
	start := time.Now()
	n := g.NumNodes()
	if k <= 0 {
		return &DenseKSubgraphResult{Method: "naive-bfs", Subset: []int{}, LatencyNS: time.Since(start).Nanoseconds()}
	}
	if k > n {
		k = n
	}

	visited := make([]bool, n)
	queue := make([]int, 0, n)
	result := make([]int, 0, k)

	queue = append(queue, 0)
	for len(queue) > 0 && len(result) < k {
		u := queue[0]
		queue = queue[1:]
		if visited[u] {
			continue
		}
		visited[u] = true
		result = append(result, u)
		for v := 0; v < n; v++ {
			if !visited[v] && g.GetWeight(u, v) > 0 {
				queue = append(queue, v)
			}
		}
	}

	// Fall back to lowest-index unvisited nodes if component exhausted.
	for v := 0; v < n && len(result) < k; v++ {
		if !visited[v] {
			visited[v] = true
			result = append(result, v)
		}
	}

	totalW := 0.0
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			totalW += g.GetWeight(result[i], result[j])
		}
	}

	return &DenseKSubgraphResult{Subset: result, TotalWeight: totalW, Method: "naive-bfs", LatencyNS: time.Since(start).Nanoseconds()}
}

// ============================================================================
// PART 4: AdversarialTrapBuilder — worst-case topologies for naive spreading
// ============================================================================

// adversarialTrapBuilder constructs graphs where naive BFS fails but density-aware pack succeeds.
type adversarialTrapBuilder struct {
	rng *rand.Rand
}

func newAdversarialTrapBuilder(seed int64) *adversarialTrapBuilder {
	return &adversarialTrapBuilder{rng: rand.New(rand.NewSource(seed))}
}

// baitStarTrap creates hub-leaves trap: node-0 connected to weak leaves; disjoint strong cluster.
func (atb *adversarialTrapBuilder) baitStarTrap(hubDegree, clusterSize int) *BandwidthGraph {
	n := hubDegree + clusterSize + 1
	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
	}

	hub := 0
	for i := 0; i < hubDegree; i++ {
		leaf := i + 1
		w := 1.0 + atb.rng.Float64()*0.1
		weight[hub][leaf], weight[leaf][hub] = w, w
	}

	clusterStart := hubDegree + 1
	for i := 0; i < clusterSize; i++ {
		for j := i + 1; j < clusterSize; j++ {
			u, v := clusterStart+i, clusterStart+j
			w := 600.0 + atb.rng.Float64()*50
			weight[u][v], weight[v][u] = w, w
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

// chainOfCliquesTrap builds linear chain of cliques linked by weak bridges.
func (atb *adversarialTrapBuilder) chainOfCliquesTrap(numCliques, cliqueSize int) *BandwidthGraph {
	n := numCliques * cliqueSize
	if n < 2 {
		panic("chain-of-cliques: n too small")
	}

	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
	}

	for c := 0; c < numCliques; c++ {
		base := c * cliqueSize
		for i := 0; i < cliqueSize; i++ {
			for j := i + 1; j < cliqueSize; j++ {
				u, v := base+i, base+j
				w := 600.0 + atb.rng.Float64()*50
				weight[u][v], weight[v][u] = w, w
			}
		}
		if c > 0 {
			u := (c-1)*cliqueSize + cliqueSize - 1
			v := c * cliqueSize
			w := 1.0 + atb.rng.Float64()*0.1
			weight[u][v], weight[v][u] = w, w
		}
	}
	return NewBandwidthGraph(nodes, weight)
}
