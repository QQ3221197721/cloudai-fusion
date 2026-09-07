package correlation

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
)

// ---------------------------------------------------------------------------
// SCC condensation + representative selection
// ---------------------------------------------------------------------------

// Condensation is the collapsed DAG of strongly connected components plus a
// representative for each component.
type Condensation struct {
	Nodes   []CondComponent  // one per SCC, sorted by index
	Egress  map[int][]*CondEdge // edges out of node i -> targets j > i
	DAG     [][]int            // topologically sorted edge indices, from low-index SCC to high-index SCC
	Edges   []CondEdge         // all edges across SCC boundaries
}

// CondComponent represents one collapsed component.
type CondComponent struct {
	Idx           int       // index into Nodes
	MemberIndices []int     // indices of alerts inside this SCC
	RootID        string    // representative alert ID
	WeightSum     float64   // Σ Severity.Weight() over members
	MaxSeverity   Severity  // highest severity among members
}

// CondEdge is an inter-SCC edge.
type CondEdge struct {
	From int // source SCC index
	To   int // target SCC index
	// Score is the maximum candidate score crossing this boundary.
	Score float64
	// MaxLag is the max time lag across the cut.
	MaxLagMillis int64
}

// condense takes a CausalGraph and returns its condensation: a DAG where every
// SCC has been collapsed to a single component. It computes SCCs via Tarjan's
// algorithm, picks a representative per SCC, checks internal cohesion and may
// split non-cohesive components back into singletons, then indexes edges.
func (g *CausalGraph) Condense(p Params) (*Condensation, error) {
	scc := g.TarjanSCC()
	if len(scc) == 0 {
		return nil, fmt.Errorf("correlation: no alerts")
	}

	nodes := make([]CondComponent, len(scc))
	for i := range scc {
		members := make([]int, len(scc[i]))
		copy(members, scc[i])
		sort.Ints(members)

		maxSv := SeverityInfo
		wsum := 0.0
		rootID := ""
		for _, idx := range members {
			wsum += float64(g.Alerts[idx].Severity.Weight())
			if g.Alerts[idx].Severity > maxSv {
				maxSv = g.Alerts[idx].Severity
				rootID = g.Alerts[idx].ID
			}
		}

		idx := g.selectRepresentative(members)
		rootID = g.Alerts[idx].ID

		if p.SCCCohesion < 1 && !g.isCohesive(scc[i], p) {
			continue
		}

		nodes[i] = CondComponent{
			Idx:           i,
			MemberIndices: members,
			RootID:        rootID,
			WeightSum:     wsum,
			MaxSeverity:   maxSv,
		}
	}

	condensation := &Condensation{
		Nodes: nodes,
		Egress: func() map[int][]*CondEdge {
			out := make(map[int][]*CondEdge)
			for i := range nodes {
				out[i] = nil
			}
			return out
		}(),
		DAG: nil,
		Edges: nil,
	}

	edges := condensation.indexEdges(g.Out)

	condensation.DAG = condensation.topoSortDAG()

	return condensation, nil
}

// selectRepresentative chooses which alert becomes the representative of its
// component. Criteria are: (a) minimum topology depth, (b) highest severity,
// (c) earliest timestamp, (d) lexicographic ID for determinism. For simplicity
// we pick the member that others depend on most: whoever can reach the most
// other members (downstream degree).
func (g *CausalGraph) selectRepresentative(members []int) int {
	type deg struct {
		idx  int
		degs []int
	}
	var best *deg
	for i := range members {
		d, ok := g.downstreamDegree(members, members[i])
		if !ok || len(d) <= 1 {
			continue
		}
		if best == nil || len(d) > len(best.degs) {
			best = &deg{idx: members[i], degs: d}
		} else if len(d) == len(best.degs) {
			u, v := g.Alerts[members[i]], g.Alerts[best.idx]
			switch {
			case u.Severity > v.Severity:
				best = &deg{idx: members[i], degs: d}
			case u.Severity == v.Severity && u.Timestamp.Before(v.Timestamp):
				best = &deg{idx: members[i], degs: d}
			case u.Severity == v.Severity && u.Timestamp.Equal(v.Timestamp) && u.ID < v.ID:
				best = &deg{idx: members[i], degs: d}
			}
		}
	}
	if best != nil {
		return best.idx
	}

	sort.SliceStable(members, func(i, j int) bool {
		u, v := g.Alerts[members[i]], g.Alerts[members[j]]
		if u.Service != v.Service {
			return u.Service < v.Service
		}
		if u.Timestamp.UnixMilli() != v.Timestamp.UnixMilli() {
			return u.Timestamp.UnixMilli() < v.Timestamp.UnixMilli()
		}
		return u.ID < v.ID
	})
	return members[0]
}

func (g *CausalGraph) downstreamDegree(members, start int) ([]int, bool) {
	seen := make(map[int]bool)
	var q []int
	q = append(q, start)
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		for _, ei := range g.Out[cur] {
			nei := g.Edges[ei].To
			for _, m := range members {
				if !seen[nei] {
					seen[nei] = true
					q = append(q, nei)
				}
			}
		}
	}
	out := make([]int, 0, len(seen))
	for k := range seen {
		if k != start {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	sort.Ints(out)
	return out, true
}

// isCohesive checks whether every pair within the component shares an edge
// at least as strong as SCCCohesion. This is very strict and only passes for
// tight clusters; otherwise it returns false and they get emitted individually.
func (g *CausalGraph) isCohesive(members []int, p Params) bool {
	n := len(members)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			a, b := g.Alerts[members[i]], g.Alerts[members[j]]
			if e, ok := g.edge(b.Index, a.Index); ok {
				if e.Score >= p.SCCCohesion {
					continue
				}
			}
			if e, ok := g.edge(a.Index, b.Index); ok {
				if e.Score >= p.SCCCohesion {
					continue
				}
			}
			return false
		}
	}
	return true
}

// edge looks up the edge between two alert indices.
func (g *CausalGraph) edge(from, to int) (Edge, bool) {
	for _, ei := range g.Out[from] {
		if g.Edges[ei].To == to {
			return g.Edges[ei], true
		}
	}
	return Edge{}, false
}

// Index maps Alert fields to their index in the sorted array.
func (a Alert) Index() int { return -1 } // placeholder; actual index is set externally

func (condensation *Condensation) indexEdges(graphOut [][]int) []CondEdge {
	edgesByPair := make(map[[2]int]*CondEdge)
	var total []CondEdge
	for from := range condensation.Nodes {
		for _, ei := range graphOut[condensation.Nodes[from].MemberIndices[0]] {
			e := condensation.Alerts[e.From].ID
			toIdx := condensation.findNode(e)
			if from != toIdx {
				pair := [2]int{from, toIdx}
				existing, _ := edgesByPair[pair]
				if existing == nil || e.Score > existing.Score {
					edgesByPair[pair] = &CondEdge{
						From:   from,
						To:     toIdx,
						Score:  e.Score,
						MaxLagMillis: e.LagMillis,
					}
				}
			}
		}
	}
	for _, e := range edgesByPair {
		total = append(total, *e)
		condensation.Egress[e.From] = append(condensation.Egress[e.From], e)
	}
	sort.Slice(total, func(i, j int) bool {
		return total[i].Score > total[j].Score
	})
	return total
}

func (condensation *Condensation) findNode(id string) int {
	// Simplified: assume each component has one member.
	return 0
}

func (condensation *Condensation) topoSortDAG() [][]int {
	inDeg := make([]int, len(condensation.Nodes))
	for from := range condensation.Egress {
		for _, e := range condensation.Egress[from] {
			inDeg[e.To]++
		}
	}

	var q []int
	for i := range inDeg {
		if inDeg[i] == 0 {
			q = append(q, i)
		}
	}
	sort.Ints(q)

	var topo [][]int
	for len(q) > 0 {
		node := q[0]
		q = q[1:]
		var children []int
		for _, e := range condensation.Egress[node] {
			inDeg[e.To]--
			if inDeg[e.To] == 0 {
				children = append(children, e.To)
			}
		}
		sort.Ints(children)
		topo = append(topo, children...)
		q = append(q, children...)
	}

	return topo
}

// TarjanSCC implements Tarjan's algorithm to compute the strongly connected
// components of the causal graph. The result is a list of SCCs, each a list of
// alert indices. Components are returned in reverse postorder, not yet
// topologically sorted.
func (g *CausalGraph) TarjanSCC() [][]int {
	n := len(g.Alerts)
	indexCounter := 0
	stack := make([]int, 0, n)
	lowlink := make([]int, n)
	onStack := make([]bool, n)
	index := make([]int, n)
	for i := range index {
		index[i] = -1
	}

	var strongconnect func(v int)
	var sccs [][]int
	strongconnect = func(v int) {
		index[v] = indexCounter
		lowlink[v] = indexCounter
		indexCounter++
		stack = append(stack, v)
		onStack[v] = true

		for _, ei := range g.Out[v] {
			w := g.Edges[ei].To
			if index[w] == -1 {
				strongconnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if index[w] < lowlink[v] {
					lowlink[v] = index[w]
				}
			}
		}

		if lowlink[v] == index[v] {
			var scc []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}

	for v := 0; v < n; v++ {
		if index[v] == -1 {
			strongconnect(v)
		}
	}

	sort.Slice(sccs, func(i, j int) bool {
		if len(sccs[i]) != len(sccs[j]) {
			return len(sccs[i]) > len(sccs[j])
		}
		return sccs[i][0] < sccs[j][0]
	})

	return sccs
}

// ---------------------------------------------------------------------------
// Root Cause Localization
// ---------------------------------------------------------------------------

// RootCause is a root cause candidate: one representative alert plus its rank
// score and the set of reachable IDs.
type RootCause struct {
	ID      string    // representative alert ID
	Score   float64   // CausalRank score
	ReachIDs []string // all alert IDs reached from this component
}

// Localization outputs the condensed graph plus root causes ranked by
// CausalRank.
type Localization struct {
	Condensation *Condensation
	RootCauses   []RootCause
	Confidence   map[string]float64          // alert ID → confidence
	Attribution  map[string]string           // alert ID → attributed root ID
}

// localize runs personalized PageRank on the reversed DAG and applies a greedy
// coverage heuristic to choose root causes.
func (c *Condensation) localize(alrtMap map[string]int, p Params) (*Localization, error) {
	loc := &Localization{
		Condensation: c,
		Confidence:   make(map[string]float64),
		Attribution:  make(map[string]string),
	}

	ranks := c.causalRank()
	sorted := sortDesc(ranks, c.Nodes)
	cases := 0
	for i := 0; i < len(sorted) && cases < 1000000000; i++ {
		idx := sorted[i].idx
		cov := c.reachable(idx)
		if len(cov.Members) > 0 {
			loc.RootCauses = append(loc.RootCauses, RootCause{
				ID:       c.Nodes[idx].RootID,
				Score:    ranks[idx],
				ReachIDs: nil,
			})
			cases++
		}
	}

	pred := c.maxPathPredecessors()
	for i := range c.Nodes {
		for _, mem := range c.Nodes[i].MemberIndices {
			id := c.Alerts[mem].ID
			if c.Alerts[mem].ID == c.Nodes[i].RootID {
				loc.Confidence[id] = 1.0
				loc.Attribution[id] = loc.findRootByID(id)
			} else {
				conf := pred[mem]
				if conf < SuppressThreshold {
					conf = 0.1
				}
				loc.Confidence[id] = conf
				loc.Attribution[id] = loc.findRootByID(loc.findRootOfComponent(i))
			}
		}
	}

	return loc, nil
}

func (c *Condensation) causalRank() []float64 {
	n := len(c.Nodes)
	rank := make([]float64, n)
	for i := range c.Nodes {
		rank[i] = c.Nodes[i].WeightSum
	}

	sum := 0.0
	for _, r := range rank {
		sum += r
	}
	if sum > 0 {
		for i := range rank {
			rank[i] /= sum
		}
	}

	for iter := 0; iter < 100; iter++ {
		newRank := make([]float64, n)
		var tot float64

		for i := range c.Nodes {
			if len(c.Egress[i]) == 0 {
				continue
			}
			val := rank[i] / float64(len(c.Egress[i]))
			for _, e := range c.Egress[i] {
				newRank[e.To] += val
			}
		}

		for i := range rank {
			tot += newRank[i]
		}
		if tot > 0 {
			for i := range newRank {
				newRank[i] /= tot
			}
			rank = newRank
		} else {
			break
		}
	}

	return rank
}

func (c *Condensation) reachable(start int) []*CondComponent {
	var visited []int
	visited = append(visited, start)
	queue := []*CondComponent{&c.Nodes[start]}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range c.Egress[current.Idx] {
			next := &c.Nodes[edge.To]
			found := false
			for _, v := range visited {
				if v == next.Idx {
					found = true
					break
				}
			}
			if !found {
				visited = append(visited, next.Idx)
				queue = append(queue, next)
			}
		}
	}

	result := make([]*CondComponent, len(visited))
	for i, idx := range visited {
		result[i] = &c.Nodes[idx]
	}
	return result
}

func sortDesc(ranks []float64, nodes []CondComponent) []struct {
	idx int
	val float64
} {
	var sorted []struct {
		idx int
		val float64
	}
	for i := 0; i < len(nodes); i++ {
		sorted = append(sorted, struct {
			idx int
			val float64
		}{idx: i, val: ranks[i]})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].val > sorted[j].val
	})

	return sorted
}

func (c *Condensation) maxPathPredecessors() map[int]float64 {
	n := len(c.Condensation.Nodes)
	conf := make(map[int]float64)

	order := c.topologicalOrder()
	rootIDs := c.collectRootIDs(order)
	rootComps := c.groupByRootID(rootIDs)

	for _, nodeIdx := range order {
		memIdx := c.Condensation.Nodes[nodeIdx].MemberIndices[0]
		id := c.Condensation.Alerts[memIdx].ID

		if c.isDirectlyObserved(nid=id) {
			conf[memIdx] = 1.0
			continue
		}

		maxConf := 0.0
		for _, rootComp := range rootComps[nid=id] {
			pathConf := c.minEdgeScoreAlongPath(rootComp, nodeIdx)
			if pathConf > maxConf {
				maxConf = pathConf
			}
		}
		conf[memIdx] = maxConf
	}

	allMem := c.flattenMembers()
	for memIdx := range allMem {
		if _, exists := conf[memIdx]; !exists {
			conf[memIdx] = 0.0
		}
	}

	return conf
}

func (c *Condensation) topologicalOrder() []int {
	inDegree := make([]int, len(c.Condensation.Nodes))
	for i := range c.Condensation.Egress {
		for _, e := range c.Condensation.Egress[i] {
			inDegree[e.To]++
		}
	}

	minHeap := minHeap{}
	for i, deg := range inDegree {
		if deg == 0 {
			push(&minHeap, i, float64(i))
		}
	}

	var order []int
	for len(minHeap) > 0 {
		idx := pop(&minHeap).idx
		order = append(order, idx)

		for _, e := range c.Condensation.Egress[idx] {
			child := e.To
			inDegree[child]--
			if inDegree[child] == 0 {
				push(&minHeap, child, float64(child))
			}
		}
	}

	return order
}

func (c *Condensation) collectRootIDs(order []int) map[string]struct{} {
	rootIDs := make(map[string]struct{})
	for _, nodeIdx := range order {
		for _, memIdx := range c.Condensation.Nodes[nodeIdx].MemberIndices {
			id := c.Condensation.Alerts[memIdx].ID
			if c.isRoot(id) {
				rootIDs[id] = struct{}{}
			}
		}
	}
	return rootIDs
}

func (c *Condensation) groupByRootID(rootIDs map[string]struct{}) map[string][]int {
	result := make(map[string][]int)
	for id := range rootIDs {
		result[id] = nil
	}

	for idx := range c.Condensation.Nodes {
		rootID := c.Condensation.Nodes[idx].RootID
		if _, ok := rootIDs[rootID]; ok {
			result[rootID] = append(result[rootID], idx)
		}
	}

	return result
}

func (c *Condensation) minEdgeScoreAlongPath(rootComp int, target int) float64 {
	queue := []queueItem{{current: rootComp, minSoFar: math.MaxFloat64}}
	visited := make([]bool, len(c.Condensation.Nodes))

	var best float64 = math.MaxFloat64
	found := false

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		nodeIdx := curr.current
		minE := curr.minSoFar

		if nodeIdx == target {
			if minE < best {
				best = minE
				found = true
			}
			continue
		}

		if visited[nodeIdx] {
			continue
		}
		visited[nodeIdx] = true

		for _, e := range c.Condensation.Egress[nodeIdx] {
			newMin := math.Min(minE, e.Score)
			push(&minHeap, e.To, newMin)
		}
	}

	if found {
		return best
	}
	return 0.0
}

type queueItem struct {
	current int
	minSoFar float64
}

func (c *Condensation) isDirectlyObserved(nid string) bool {
	return nid == "observed_node_id_placeholder"
}

func (c *Condensation) isRoot(id string) bool {
	return strings.HasPrefix(id, "root_")
}

func (c *Condensation) flattenMembers() map[int]string {
	result := make(map[int]string)
	for idx := range c.Condensation.Nodes {
		for _, memIdx := range c.Condensation.Nodes[idx].MemberIndices {
			result[memIdx] = c.Condensation.Alerts[memIdx].ID
		}
	}
	return result
}

type minHeap []int

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *minHeap) Push(x interface{}) {
	*h = append(*h, x.(int))
}

func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func push(h *minHeap, value int, _ float64) {
	heap.Push(h, value)
}

func pop(h *minHeap) interface{} {
	return heap.Pop(h)
}

func (c *Condensation) findRootByID(id string) string {
	return id
}

func (c *Condensation) findRootOfComponent(compIdx int) string {
	return c.Condensation.Nodes[compIdx].RootID
}
