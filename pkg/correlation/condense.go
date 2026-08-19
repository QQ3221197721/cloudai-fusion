package correlation

// condense.go collapses the causal candidate graph into a DAG of strongly
// connected components.
//
// Cycles are unavoidable and must be handled rather than ignored. They arise
// from two real mechanisms:
//
//  1. Simultaneity — alerts landing inside the same scrape interval (Epsilon)
//     have no observable ordering, so the pair is a candidate in both
//     directions.
//  2. Cyclic dependencies — retry loops between services (A calls B, B calls A)
//     make both directions topologically reachable.
//
// Root-cause localization on a graph with cycles is ill-posed, so every SCC is
// collapsed to a single component. A component may only *speak for* its members
// (i.e. suppress them) when it is cohesive: the weakest edge holding the cycle
// together must clear Params.SCCCohesion. Non-cohesive components still take
// part in the DAG structure but never suppress their own members — a loose
// cycle is a sign the correlation is not trustworthy, and the safe answer is to
// page for all of them.

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
)

// Component is one collapsed strongly connected component.
type Component struct {
	// Idx is the component's index in Condensation.Comps (topologically sorted).
	Idx int
	// Members holds the alert indices in the component, ascending.
	Members []int
	// Rep is the alert index elected to represent the component.
	Rep int
	// Cohesion is the weakest internal edge score, 1.0 for singletons.
	Cohesion float64
	// Collapsible reports whether the component may suppress its own members.
	Collapsible bool
	// Weight is the sum of member severity weights (CausalRank restart mass).
	Weight float64
	// MaxSev is the highest member severity.
	MaxSev Severity
}

// CondEdge is a directed edge in the condensation.
type CondEdge struct {
	// From and To are component indices.
	From int
	To   int
	// Score is the strongest alert-level edge crossing this cut.
	Score float64
	// BestEdge indexes CausalGraph.Edges for the edge that achieved Score.
	BestEdge int
}

// Condensation is the acyclic component graph derived from a CausalGraph.
type Condensation struct {
	// Graph is the underlying alert-level graph.
	Graph *CausalGraph
	// Comps are the components in topological order: an edge always points
	// from a lower index to a higher one.
	Comps []Component
	// CompOf maps an alert index to its component index.
	CompOf []int
	// Adj holds outgoing condensation edges per component.
	Adj [][]CondEdge
	// Order is the topological order of component indices. Because Comps is
	// already renumbered topologically this is simply 0..n-1, retained for
	// clarity at call sites.
	Order []int
}

// Condense computes the SCC condensation of g.
func (g *CausalGraph) Condense() (*Condensation, error) {
	if len(g.Alerts) == 0 {
		return nil, fmt.Errorf("correlation: cannot condense an empty alert batch")
	}
	raw, rawCompOf := g.tarjanSCC()

	// Build raw component adjacency to derive a topological order.
	rawAdj := make([][]CondEdge, len(raw))
	seen := make(map[[2]int]int) // (from,to) -> position in rawAdj[from]
	for ei := range g.Edges {
		e := g.Edges[ei]
		cf, ct := rawCompOf[e.From], rawCompOf[e.To]
		if cf == ct {
			continue
		}
		key := [2]int{cf, ct}
		if pos, ok := seen[key]; ok {
			if e.Score > rawAdj[cf][pos].Score {
				rawAdj[cf][pos].Score = e.Score
				rawAdj[cf][pos].BestEdge = ei
			}
			continue
		}
		seen[key] = len(rawAdj[cf])
		rawAdj[cf] = append(rawAdj[cf], CondEdge{From: cf, To: ct, Score: e.Score, BestEdge: ei})
	}

	order := topoSort(rawAdj, len(raw))
	newIdx := make([]int, len(raw))
	for pos, old := range order {
		newIdx[old] = pos
	}

	c := &Condensation{
		Graph:  g,
		Comps:  make([]Component, len(raw)),
		CompOf: make([]int, len(g.Alerts)),
		Adj:    make([][]CondEdge, len(raw)),
		Order:  make([]int, len(raw)),
	}
	for i := range c.Order {
		c.Order[i] = i
	}
	for ai, old := range rawCompOf {
		c.CompOf[ai] = newIdx[old]
	}

	for old, members := range raw {
		ni := newIdx[old]
		sort.Ints(members)
		comp := Component{Idx: ni, Members: members, Cohesion: 1.0, MaxSev: SeverityInfo}
		for _, ai := range members {
			a := g.Alerts[ai]
			comp.Weight += a.Severity.Weight()
			if a.Severity > comp.MaxSev {
				comp.MaxSev = a.Severity
			}
		}
		if len(members) > 1 {
			comp.Cohesion = g.internalCohesion(members)
		}
		comp.Collapsible = len(members) == 1 || comp.Cohesion >= g.Params.SCCCohesion
		comp.Rep = g.electRepresentative(members)
		c.Comps[ni] = comp
	}

	for old := range rawAdj {
		ni := newIdx[old]
		edges := make([]CondEdge, 0, len(rawAdj[old]))
		for _, e := range rawAdj[old] {
			edges = append(edges, CondEdge{From: ni, To: newIdx[e.To], Score: e.Score, BestEdge: e.BestEdge})
		}
		sort.Slice(edges, func(i, j int) bool { return edges[i].To < edges[j].To })
		c.Adj[ni] = edges
	}

	return c, nil
}

// internalCohesion returns the weakest edge score among edges whose endpoints
// both lie inside the component. Every SCC with more than one member has at
// least one internal edge, so the result is always a real observed score.
func (g *CausalGraph) internalCohesion(members []int) float64 {
	inComp := make(map[int]bool, len(members))
	for _, m := range members {
		inComp[m] = true
	}
	weakest := math.MaxFloat64
	for _, u := range members {
		for _, ei := range g.Out[u] {
			e := g.Edges[ei]
			if !inComp[e.To] {
				continue
			}
			if e.Score < weakest {
				weakest = e.Score
			}
		}
	}
	if weakest == math.MaxFloat64 {
		return 0
	}
	return weakest
}

// electRepresentative picks the alert that speaks for its component.
//
// Ranking, in order: (1) net topological influence — how many other members sit
// downstream of this alert's service minus how many sit upstream, so the member
// others depend on wins; (2) highest severity; (3) earliest timestamp;
// (4) lexicographic ID, which makes the choice deterministic.
func (g *CausalGraph) electRepresentative(members []int) int {
	if len(members) == 1 {
		return members[0]
	}
	inComp := make(map[int]bool, len(members))
	for _, m := range members {
		inComp[m] = true
	}
	best := members[0]
	bestInfluence := math.MinInt32
	for _, m := range members {
		influence := 0
		for _, ei := range g.Out[m] {
			if e := g.Edges[ei]; inComp[e.To] && e.Hops > 0 {
				influence++
			}
		}
		for _, ei := range g.In[m] {
			if e := g.Edges[ei]; inComp[e.From] && e.Hops > 0 {
				influence--
			}
		}
		if influence > bestInfluence {
			best, bestInfluence = m, influence
			continue
		}
		if influence < bestInfluence {
			continue
		}
		u, v := g.Alerts[m], g.Alerts[best]
		switch {
		case u.Severity != v.Severity:
			if u.Severity > v.Severity {
				best = m
			}
		case !u.Timestamp.Equal(v.Timestamp):
			if u.Timestamp.Before(v.Timestamp) {
				best = m
			}
		default:
			if u.ID < v.ID {
				best = m
			}
		}
	}
	return best
}

// tarjanSCC computes strongly connected components with an iterative version of
// Tarjan's algorithm (iterative to stay safe on large storms where recursion
// depth would follow the longest causal chain). It returns the components and a
// per-alert component index.
func (g *CausalGraph) tarjanSCC() ([][]int, []int) {
	n := len(g.Alerts)
	const unvisited = -1

	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	compOf := make([]int, n)
	for i := range index {
		index[i] = unvisited
		compOf[i] = unvisited
	}

	var comps [][]int
	var stack []int
	counter := 0

	type frame struct {
		v  int
		ei int
	}

	for root := 0; root < n; root++ {
		if index[root] != unvisited {
			continue
		}
		index[root] = counter
		low[root] = counter
		counter++
		stack = append(stack, root)
		onStack[root] = true
		call := []frame{{v: root}}

		for len(call) > 0 {
			f := &call[len(call)-1]
			v := f.v
			if f.ei < len(g.Out[v]) {
				w := g.Edges[g.Out[v][f.ei]].To
				f.ei++
				if index[w] == unvisited {
					index[w] = counter
					low[w] = counter
					counter++
					stack = append(stack, w)
					onStack[w] = true
					call = append(call, frame{v: w})
				} else if onStack[w] && index[w] < low[v] {
					low[v] = index[w]
				}
				continue
			}

			// v is finished.
			call = call[:len(call)-1]
			if len(call) > 0 {
				parent := call[len(call)-1].v
				if low[v] < low[parent] {
					low[parent] = low[v]
				}
			}
			if low[v] == index[v] {
				var comp []int
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					compOf[w] = len(comps)
					comp = append(comp, w)
					if w == v {
						break
					}
				}
				sort.Ints(comp)
				comps = append(comps, comp)
			}
		}
	}
	return comps, compOf
}

// topoSort returns a deterministic topological order of the component DAG using
// Kahn's algorithm with an ascending-index tie-break.
func topoSort(adj [][]CondEdge, n int) []int {
	indeg := make([]int, n)
	for from := range adj {
		for _, e := range adj[from] {
			indeg[e.To]++
		}
	}
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	order := make([]int, 0, n)
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		order = append(order, v)
		for _, e := range adj[v] {
			indeg[e.To]--
			if indeg[e.To] == 0 {
				queue = insertSorted(queue, e.To)
			}
		}
	}
	if len(order) != n {
		// Unreachable for a condensation, which is acyclic by construction.
		// Fail closed rather than silently dropping components.
		placed := make([]bool, n)
		for _, v := range order {
			placed[v] = true
		}
		for i := 0; i < n; i++ {
			if !placed[i] {
				order = append(order, i)
			}
		}
	}
	return order
}

// insertSorted inserts v into an ascending slice, preserving determinism.
func insertSorted(s []int, v int) []int {
	i := sort.SearchInts(s, v)
	s = append(s, 0)
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

// CollapsedCount returns how many alerts would disappear if every collapsible
// component were reduced to its representative. It is an upper bound on the
// compression achievable by SCC collapsing alone.
func (c *Condensation) CollapsedCount() int {
	saved := 0
	for _, comp := range c.Comps {
		if comp.Collapsible && len(comp.Members) > 1 {
			saved += len(comp.Members) - 1
		}
	}
	return saved
}

// Summary renders component statistics for reports and test logs.
func (c *Condensation) Summary() string {
	multi, loose, largest := 0, 0, 0
	for _, comp := range c.Comps {
		if len(comp.Members) > 1 {
			multi++
			if !comp.Collapsible {
				loose++
			}
		}
		if len(comp.Members) > largest {
			largest = len(comp.Members)
		}
	}
	return fmt.Sprintf("components=%d multi_member_sccs=%d non_cohesive_sccs=%d largest_scc=%d",
		len(c.Comps), multi, loose, largest)
}

// ---------------------------------------------------------------------------
// bitset — reachability sets for the greedy cover
// ---------------------------------------------------------------------------

type bitset []uint64

func newBitset(n int) bitset { return make(bitset, (n+63)/64+1) }

func (b bitset) set(i int) { b[i/64] |= 1 << uint(i%64) }

func (b bitset) has(i int) bool { return b[i/64]&(1<<uint(i%64)) != 0 }

func (b bitset) or(o bitset) {
	for i := range b {
		b[i] |= o[i]
	}
}

func (b bitset) count() int {
	c := 0
	for _, w := range b {
		c += bits.OnesCount64(w)
	}
	return c
}
