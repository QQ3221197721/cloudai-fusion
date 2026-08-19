// Package scheduler - dense_k_subgraph.go implements the dense k-subgraph problem for
// GPU topology-aware scheduling.
//
// Problem: model the N-GPU interconnect as a weighted undirected graph G=(V,E,w) where
// w(u,v) is the pairwise bandwidth between GPUs u and v (NVLink/NVSwitch high, PCIe switch
// medium, cross-socket low, cross-node lowest). To place a job requiring k GPUs we want the
// k-vertex subset S maximizing the intra-subset bandwidth sum W(S) = Σ_{u<v ∈ S} w(u,v).
// This is exactly the Densest-k-Subgraph (DkS) problem, which is NP-hard.
//
// This file provides:
//   - BandwidthGraph: the weighted-graph data structure + realistic DGX/HGX fixtures.
//   - ExactBB:   branch-and-bound exact solver (optimal, intended for small k, k≤8).
//   - Greedy2Opt: greedy seed expansion + 2-opt local search approximation (µs-level).
//   - Baseline solvers reproducing binpack, first-fit, random, and K8s NodeResourcesFit scoring.
//
// All data here is synthetic topology data; nothing queries real hardware.
package scheduler

import (
	"math/rand"
	"sort"
	"time"
)

// ============================================================================
// Bandwidth Tier Constants (GB/s, representing real interconnect classes)
// ============================================================================

const (
	// BandwidthTierNVSwitch is the per-link bandwidth of an NVSwitch full mesh
	// (DGX/HGX H100: 900 GB/s bidirectional per GPU through NVSwitch).
	BandwidthTierNVSwitch = 900.0
	// BandwidthTierNVLink is NVLink 3.0 direct bandwidth (A100: 600 GB/s).
	BandwidthTierNVLink = 600.0
	// BandwidthTierPCIeSwitch is bandwidth through a shared PCIe switch (Gen4 x16 ≈ 32 GB/s).
	BandwidthTierPCIeSwitch = 32.0
	// BandwidthTierCrossSocket is cross-socket host-bridge/UPI bandwidth.
	BandwidthTierCrossSocket = 16.0
	// BandwidthTierCrossNode is cross-node fabric bandwidth (InfiniBand/RoCE class).
	BandwidthTierCrossNode = 8.0
)

// ============================================================================
// Graph Data Structures
// ============================================================================

// GPUVertex is a single GPU node in the topology graph.
type GPUVertex struct {
	ID           int     `json:"id"`            // GPU index (0-based)
	Socket       int     `json:"socket"`        // NUMA socket
	Host         string  `json:"host"`          // physical host (for multi-node graphs)
	MemoryGB     float64 `json:"memory_gb"`     // GPU memory
	FreeFraction float64 `json:"free_fraction"` // free-resource fraction [0,1], used by K8s-style baselines
}

// BandwidthGraph is a weighted undirected graph of GPU interconnect bandwidths.
// Weight is a symmetric adjacency matrix; Weight[i][j] is the pairwise bandwidth (GB/s).
type BandwidthGraph struct {
	Nodes  []GPUVertex `json:"nodes"`
	Weight [][]float64 `json:"weight"`
}

// NewBandwidthGraph builds a graph from nodes and a symmetric weight matrix.
func NewBandwidthGraph(nodes []GPUVertex, weight [][]float64) *BandwidthGraph {
	return &BandwidthGraph{Nodes: nodes, Weight: weight}
}

// NumNodes returns the number of vertices.
func (g *BandwidthGraph) NumNodes() int { return len(g.Nodes) }

// GetWeight returns the edge weight between u and v (0 out of range or self-loop).
func (g *BandwidthGraph) GetWeight(u, v int) float64 {
	if u < 0 || u >= len(g.Weight) || v < 0 || v >= len(g.Weight) {
		return 0
	}
	return g.Weight[u][v]
}

// SetWeight sets a symmetric edge weight between vertices u and v.
func (g *BandwidthGraph) SetWeight(u, v int, w float64) {
	if u < 0 || u >= len(g.Weight) || v < 0 || v >= len(g.Weight) {
		return
	}
	g.Weight[u][v] = w
	g.Weight[v][u] = w
}

// SubsetWeight computes W(S) = Σ_{i<j ∈ S} w(i,j).
func (g *BandwidthGraph) SubsetWeight(subset []int) float64 {
	var w float64
	for i := 0; i < len(subset); i++ {
		for j := i + 1; j < len(subset); j++ {
			w += g.GetWeight(subset[i], subset[j])
		}
	}
	return w
}

// ============================================================================
// Fixtures: realistic DGX/HGX topologies (synthetic; no hardware access)
// ============================================================================

// BuildDGXH100Topo builds an 8-GPU DGX H100 topology: NVSwitch full mesh, every pair at 900 GB/s.
func BuildDGXH100Topo() *BandwidthGraph {
	n := 8
	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, Socket: i / 4, Host: "dgx-h100-0", MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			weight[i][j] = BandwidthTierNVSwitch
			weight[j][i] = BandwidthTierNVSwitch
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

// BuildDualSocketA100Topo builds a dual-socket 4+4 A100 topology:
// within a socket, GPUs form an NVLink full mesh (600 GB/s); across sockets, host-bridge (16 GB/s).
func BuildDualSocketA100Topo() *BandwidthGraph {
	n := 8
	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		socket := 0
		if i >= 4 {
			socket = 1
		}
		nodes[i] = GPUVertex{ID: i, Socket: socket, Host: "hgx-a100-0", MemoryGB: 80, FreeFraction: 1.0}
		weight[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			var w float64
			if nodes[i].Socket == nodes[j].Socket {
				w = BandwidthTierNVLink
			} else {
				w = BandwidthTierCrossSocket
			}
			weight[i][j] = w
			weight[j][i] = w
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

// BuildMultiNodeClusterTopo builds a 2-node cluster (hosts×gpusPerHost GPUs):
// intra-host NVSwitch full mesh (900 GB/s); inter-host fabric (8 GB/s).
func BuildMultiNodeClusterTopo(hosts, gpusPerHost int) *BandwidthGraph {
	if hosts < 1 {
		hosts = 2
	}
	if gpusPerHost < 1 {
		gpusPerHost = 8
	}
	n := hosts * gpusPerHost
	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		h := i / gpusPerHost
		nodes[i] = GPUVertex{
			ID: i, Socket: (i % gpusPerHost) / (gpusPerHost / 2),
			Host: hostName(h), MemoryGB: 80, FreeFraction: 1.0,
		}
		weight[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			var w float64
			if nodes[i].Host == nodes[j].Host {
				w = BandwidthTierNVSwitch
			} else {
				w = BandwidthTierCrossNode
			}
			weight[i][j] = w
			weight[j][i] = w
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

func hostName(h int) string {
	return "node-" + string(rune('0'+h))
}

// BuildRandomTopology generates a random but structurally-realistic topology on n GPUs.
// It picks a random architecture family (full-mesh island / dual-socket / multi-node) and
// assigns random free-resource fractions so the K8s-style baselines exercise their scoring.
func BuildRandomTopology(rng *rand.Rand, n int) *BandwidthGraph {
	if n < 2 {
		n = 2
	}
	nodes := make([]GPUVertex, n)
	weight := make([][]float64, n)
	for i := range nodes {
		nodes[i] = GPUVertex{ID: i, MemoryGB: 80, FreeFraction: rng.Float64()}
		weight[i] = make([]float64, n)
	}

	// Randomly partition GPUs into 1..4 "islands" (NVLink/NVSwitch domains).
	numIslands := 1 + rng.Intn(4)
	if numIslands > n {
		numIslands = n
	}
	island := make([]int, n)
	for i := range island {
		island[i] = rng.Intn(numIslands)
	}
	// Random intra-island tier (NVSwitch or NVLink) and inter-island tier (PCIe/socket/node).
	intraTier := BandwidthTierNVSwitch
	if rng.Intn(2) == 0 {
		intraTier = BandwidthTierNVLink
	}
	interTiers := []float64{BandwidthTierPCIeSwitch, BandwidthTierCrossSocket, BandwidthTierCrossNode}
	interTier := interTiers[rng.Intn(len(interTiers))]

	for i := 0; i < n; i++ {
		nodes[i].Socket = island[i]
		for j := i + 1; j < n; j++ {
			var w float64
			if island[i] == island[j] {
				// small jitter so ties don't dominate
				w = intraTier * (0.9 + 0.2*rng.Float64())
			} else {
				w = interTier * (0.9 + 0.2*rng.Float64())
			}
			weight[i][j] = w
			weight[j][i] = w
		}
	}
	return NewBandwidthGraph(nodes, weight)
}

// ============================================================================
// Solver Interface and Result
// ============================================================================

// DenseKSubgraphResult is the output of any dense-k-subgraph solver.
type DenseKSubgraphResult struct {
	Subset      []int   `json:"subset"`       // selected vertex IDs
	TotalWeight float64 `json:"total_weight"` // Σ intra-subset edge weights
	Method      string  `json:"method"`       // solver name
	LatencyNS   int64   `json:"latency_ns"`   // wall-clock solve latency
}

// DenseKSolver solves the dense k-subgraph problem.
type DenseKSolver interface {
	Name() string
	Solve(g *BandwidthGraph, k int) *DenseKSubgraphResult
}

// ============================================================================
// Exact Solver: Branch-and-Bound with an admissible upper bound
// ============================================================================

// ExactBB is an exact DkS solver via branch-and-bound. It enumerates k-subsets with pruning
// driven by an admissible (over-estimating) upper bound. Intended for small k (k≤8).
type ExactBB struct{}

// NewExactBB creates an exact branch-and-bound solver.
func NewExactBB() *ExactBB { return &ExactBB{} }

// Name implements DenseKSolver.
func (s *ExactBB) Name() string { return "exact-bnb" }

// Solve returns the optimal (maximum-weight) k-subset.
func (s *ExactBB) Solve(g *BandwidthGraph, k int) *DenseKSubgraphResult {
	start := time.Now()
	n := g.NumNodes()
	res := &DenseKSubgraphResult{Method: "exact-bnb", Subset: []int{}}
	if k <= 0 || k > n {
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}
	if k == 1 {
		res.Subset = []int{0}
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}

	// Order candidates by weighted degree (descending) to reach good incumbents early.
	order := make([]int, n)
	deg := make([]float64, n)
	for i := 0; i < n; i++ {
		order[i] = i
		var d float64
		for j := 0; j < n; j++ {
			d += g.GetWeight(i, j)
		}
		deg[i] = d
	}
	sort.SliceStable(order, func(a, b int) bool { return deg[order[a]] > deg[order[b]] })

	bestW := -1.0
	var bestSel []int
	cur := make([]int, 0, k)

	var dfs func(pos int, curW float64)
	dfs = func(pos int, curW float64) {
		r := k - len(cur)
		if r == 0 {
			if curW > bestW {
				bestW = curW
				bestSel = append([]int(nil), cur...)
			}
			return
		}
		if n-pos < r {
			return
		}
		if curW+s.upperBound(g, cur, order[pos:], r) <= bestW {
			return
		}
		for i := pos; i <= n-r; i++ {
			v := order[i]
			add := 0.0
			for _, u := range cur {
				add += g.GetWeight(v, u)
			}
			cur = append(cur, v)
			dfs(i+1, curW+add)
			cur = cur[:len(cur)-1]
		}
	}
	dfs(0, 0)

	if bestW < 0 {
		bestW = 0
	}
	res.Subset = bestSel
	res.TotalWeight = bestW
	res.LatencyNS = time.Since(start).Nanoseconds()
	return res
}

// upperBound returns an admissible over-estimate of the extra weight obtainable by adding r
// vertices chosen from cand to the current subset cur. When adding r vertices, at most
// T = r*|cur| + r*(r-1)/2 new edges are created; the sum of the T largest available candidate
// edges is therefore a valid upper bound on the additional weight.
func (s *ExactBB) upperBound(g *BandwidthGraph, cur, cand []int, r int) float64 {
	if r <= 0 {
		return 0
	}
	ws := make([]float64, 0, len(cand)*len(cand)/2+len(cand)*len(cur))
	// edges among candidates
	for a := 0; a < len(cand); a++ {
		for b := a + 1; b < len(cand); b++ {
			if w := g.GetWeight(cand[a], cand[b]); w > 0 {
				ws = append(ws, w)
			}
		}
	}
	// edges from candidates to current subset
	for _, c := range cand {
		for _, u := range cur {
			if w := g.GetWeight(c, u); w > 0 {
				ws = append(ws, w)
			}
		}
	}
	T := r*len(cur) + r*(r-1)/2
	if T >= len(ws) {
		sum := 0.0
		for _, w := range ws {
			sum += w
		}
		return sum
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(ws)))
	sum := 0.0
	for i := 0; i < T; i++ {
		sum += ws[i]
	}
	return sum
}

// ============================================================================
// Approximate Solver: greedy seed expansion + 2-opt local search
// ============================================================================

// Greedy2Opt approximates DkS: it seeds from the top edges, greedily expands by marginal gain,
// then runs 2-opt swaps to a local optimum. Multi-start over several seeds keeps the best.
type Greedy2Opt struct {
	MaxSeeds int // number of top-weight seed edges to try (multi-start)
}

// NewGreedy2Opt creates the approximation solver. maxSeeds<=0 defaults to 8.
func NewGreedy2Opt(maxSeeds int) *Greedy2Opt {
	if maxSeeds <= 0 {
		maxSeeds = 8
	}
	return &Greedy2Opt{MaxSeeds: maxSeeds}
}

// Name implements DenseKSolver.
func (s *Greedy2Opt) Name() string { return "greedy-2opt" }

// Solve returns an approximate maximum-weight k-subset.
func (s *Greedy2Opt) Solve(g *BandwidthGraph, k int) *DenseKSubgraphResult {
	start := time.Now()
	n := g.NumNodes()
	res := &DenseKSubgraphResult{Method: "greedy-2opt", Subset: []int{}}
	if k <= 0 || k > n {
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}
	if k == 1 {
		res.Subset = []int{0}
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}

	// Collect and rank seed edges by weight.
	type edge struct {
		u, v int
		w    float64
	}
	edges := make([]edge, 0, n*(n-1)/2)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			edges = append(edges, edge{i, j, g.GetWeight(i, j)})
		}
	}
	sort.Slice(edges, func(a, b int) bool { return edges[a].w > edges[b].w })

	seeds := s.MaxSeeds
	if seeds > len(edges) {
		seeds = len(edges)
	}
	if seeds < 1 {
		seeds = 1
	}

	bestW := -1.0
	var bestSel []int
	for si := 0; si < seeds; si++ {
		subset := s.greedyExpand(g, k, edges[si].u, edges[si].v)
		subset = s.twoOpt(g, subset)
		if w := g.SubsetWeight(subset); w > bestW {
			bestW = w
			bestSel = append([]int(nil), subset...)
		}
	}
	if bestW < 0 {
		bestW = 0
	}
	res.Subset = bestSel
	res.TotalWeight = bestW
	res.LatencyNS = time.Since(start).Nanoseconds()
	return res
}

// greedyExpand grows a subset from a seed edge, repeatedly adding the vertex with maximum
// marginal gain (sum of edges to the current subset). Ties break to the lower index.
func (s *Greedy2Opt) greedyExpand(g *BandwidthGraph, k, seedU, seedV int) []int {
	n := g.NumNodes()
	inSet := make([]bool, n)
	subset := make([]int, 0, k)
	subset = append(subset, seedU, seedV)
	inSet[seedU] = true
	inSet[seedV] = true

	for len(subset) < k {
		bestNext := -1
		bestGain := -1.0
		for c := 0; c < n; c++ {
			if inSet[c] {
				continue
			}
			gain := 0.0
			for _, u := range subset {
				gain += g.GetWeight(c, u)
			}
			if gain > bestGain {
				bestGain = gain
				bestNext = c
			}
		}
		if bestNext < 0 {
			break
		}
		subset = append(subset, bestNext)
		inSet[bestNext] = true
	}
	return subset
}

// twoOpt performs 2-opt swaps: replace an in-subset vertex with an outside vertex whenever it
// increases the subset weight. Runs to a local optimum. Swapping vIn→vOut changes weight by
// (connectivity of vOut to the rest) − (connectivity of vIn to the rest), since edges among the
// remaining members are unaffected.
func (s *Greedy2Opt) twoOpt(g *BandwidthGraph, subset []int) []int {
	n := g.NumNodes()
	inSet := make([]bool, n)
	for _, v := range subset {
		inSet[v] = true
	}
	improved := true
	for improved {
		improved = false
		for i := 0; i < len(subset); i++ {
			vIn := subset[i]
			conIn := 0.0
			for _, u := range subset {
				if u != vIn {
					conIn += g.GetWeight(vIn, u)
				}
			}
			for vOut := 0; vOut < n; vOut++ {
				if inSet[vOut] {
					continue
				}
				conOut := 0.0
				for _, u := range subset {
					if u != vIn {
						conOut += g.GetWeight(vOut, u)
					}
				}
				if conOut > conIn {
					inSet[vIn] = false
					inSet[vOut] = true
					subset[i] = vOut
					improved = true
					vIn = vOut
					conIn = conOut
				}
			}
		}
	}
	return subset
}

// ============================================================================
// Baseline Solvers (topology-blind): binpack, first-fit, random, K8s NodeResourcesFit
// ============================================================================

// FirstFitSolver selects the first k GPUs in device-plugin index order (K8s device-plugin default).
type FirstFitSolver struct{}

// Name implements DenseKSolver.
func (*FirstFitSolver) Name() string { return "first-fit" }

// Solve implements DenseKSolver.
func (*FirstFitSolver) Solve(g *BandwidthGraph, k int) *DenseKSubgraphResult {
	start := time.Now()
	n := g.NumNodes()
	res := &DenseKSubgraphResult{Method: "first-fit", Subset: []int{}}
	if k <= 0 || k > n {
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}
	subset := make([]int, k)
	for i := 0; i < k; i++ {
		subset[i] = i
	}
	res.Subset = subset
	res.TotalWeight = g.SubsetWeight(subset)
	res.LatencyNS = time.Since(start).Nanoseconds()
	return res
}

// BinPackSolver reproduces K8s NodeResourcesFit "MostAllocated" scoring: prefer the most-utilized
// GPUs (lowest free fraction) to consolidate. It is topology-blind.
type BinPackSolver struct{}

// Name implements DenseKSolver.
func (*BinPackSolver) Name() string { return "binpack" }

// Solve implements DenseKSolver.
func (*BinPackSolver) Solve(g *BandwidthGraph, k int) *DenseKSubgraphResult {
	start := time.Now()
	res := &DenseKSubgraphResult{Method: "binpack", Subset: []int{}}
	subset, ok := pickByFreeFraction(g, k, true)
	if !ok {
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}
	res.Subset = subset
	res.TotalWeight = g.SubsetWeight(subset)
	res.LatencyNS = time.Since(start).Nanoseconds()
	return res
}

// K8sDefaultSolver reproduces K8s NodeResourcesFit "LeastAllocated" scoring (the kube-scheduler
// default): prefer GPUs with the most free resource (highest free fraction), spreading load.
// It treats nvidia.com/gpu as an opaque count and never inspects NVLink topology.
type K8sDefaultSolver struct{}

// Name implements DenseKSolver.
func (*K8sDefaultSolver) Name() string { return "k8s-default" }

// Solve implements DenseKSolver.
func (*K8sDefaultSolver) Solve(g *BandwidthGraph, k int) *DenseKSubgraphResult {
	start := time.Now()
	res := &DenseKSubgraphResult{Method: "k8s-default", Subset: []int{}}
	subset, ok := pickByFreeFraction(g, k, false)
	if !ok {
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}
	res.Subset = subset
	res.TotalWeight = g.SubsetWeight(subset)
	res.LatencyNS = time.Since(start).Nanoseconds()
	return res
}

// pickByFreeFraction selects k vertices ordered by free fraction. mostAllocated=true prefers the
// lowest free fraction (MostAllocated/binpack); false prefers the highest (LeastAllocated/spread).
// Ties break to the lower index for determinism.
func pickByFreeFraction(g *BandwidthGraph, k int, mostAllocated bool) ([]int, bool) {
	n := g.NumNodes()
	if k <= 0 || k > n {
		return nil, false
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		fa, fb := g.Nodes[idx[a]].FreeFraction, g.Nodes[idx[b]].FreeFraction
		if fa == fb {
			return idx[a] < idx[b]
		}
		if mostAllocated {
			return fa < fb
		}
		return fa > fb
	})
	return append([]int(nil), idx[:k]...), true
}

// RandomSolver picks k distinct GPUs uniformly at random.
type RandomSolver struct {
	rng *rand.Rand
}

// NewRandomSolver creates a random solver with the given RNG (nil → fixed seed 42).
func NewRandomSolver(rng *rand.Rand) *RandomSolver {
	if rng == nil {
		rng = rand.New(rand.NewSource(42))
	}
	return &RandomSolver{rng: rng}
}

// Name implements DenseKSolver.
func (s *RandomSolver) Name() string { return "random" }

// Solve implements DenseKSolver.
func (s *RandomSolver) Solve(g *BandwidthGraph, k int) *DenseKSubgraphResult {
	start := time.Now()
	n := g.NumNodes()
	res := &DenseKSubgraphResult{Method: "random", Subset: []int{}}
	if k <= 0 || k > n {
		res.LatencyNS = time.Since(start).Nanoseconds()
		return res
	}
	perm := s.rng.Perm(n)
	subset := append([]int(nil), perm[:k]...)
	sort.Ints(subset)
	res.Subset = subset
	res.TotalWeight = g.SubsetWeight(subset)
	res.LatencyNS = time.Since(start).Nanoseconds()
	return res
}
