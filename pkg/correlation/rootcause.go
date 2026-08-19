package correlation

// rootcause.go localizes root causes on the condensation DAG.
//
// Two classical results are combined:
//
//   - CausalRank: a personalized PageRank variant run on the *reversed*
//     condensation. Probability mass starts at every alert in proportion to its
//     severity and then flows backwards along causal edges towards its causes.
//     Components with no causes (dependency sources) are absorbing, so they
//     accumulate exactly the effect mass they explain. This ranks candidates by
//     how much of the storm they account for, not by how loud they are.
//
//   - Greedy reachability cover: choosing the smallest set of components from
//     which every alert is reachable is a minimum dominating-set problem on the
//     reachability relation, which is NP-hard. The greedy maximum-gain heuristic
//     is the standard (1 + ln n) approximation for set cover; gain is broken by
//     CausalRank so that when two candidates explain equally much, the one
//     carrying more severity mass wins.
//
// Confidence is then propagated from each selected root along the DAG with a
// fuzzy t-norm (Params.Composition) and every alert is attributed to the root
// that reaches it with the highest confidence.

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// RootCause is one localized root-cause candidate.
type RootCause struct {
	// Comp is the component index in Condensation.Comps.
	Comp int
	// AlertID is the representative alert of that component.
	AlertID string
	// Rank is the CausalRank score.
	Rank float64
	// Order is the 1-based position in the ranked root list.
	Order int
	// CoveredComps and CoveredAlerts count what this root explains.
	CoveredComps  int
	CoveredAlerts int
	// SCCSize is the number of alerts inside the root's component.
	SCCSize int
	// Collapsible reports whether the root component may suppress its members.
	Collapsible bool
	// MemberIDs lists the alert IDs inside the root component.
	MemberIDs []string
}

// AlertAttribution records which root explains an alert and how strongly.
type AlertAttribution struct {
	// RootAlertID is the attributed root. An empty string means the alert is
	// unattributed and must always be emitted.
	RootAlertID string
	// Confidence is the composed path confidence in [0,1].
	Confidence float64
	// PathHops is the number of condensation edges between root and alert.
	PathHops int
	// SameComponent is true when the alert folds into its own component's
	// representative rather than travelling along causal edges.
	SameComponent bool
	// IsRootRep marks the representative alert of a selected root component.
	IsRootRep bool
	// LastEdge is the final alert-level edge on the winning path, retained as
	// evidence. It is nil for roots and for same-component folds.
	LastEdge *Edge
}

// Localization is the full root-cause attribution of one alert batch.
type Localization struct {
	Graph   *CausalGraph
	Cond    *Condensation
	Roots   []RootCause
	Attrib  map[string]AlertAttribution
	Elapsed time.Duration
}

// compose applies the configured fuzzy t-norm to two confidences.
func (p Params) compose(a, b float64) float64 {
	if p.Composition == CompositionProduct {
		return a * b
	}
	return math.Min(a, b)
}

// Localize condenses the graph, ranks candidates with CausalRank, selects roots
// with the greedy reachability cover and attributes every alert to a root.
func Localize(g *CausalGraph) (*Localization, error) {
	if g == nil || len(g.Alerts) == 0 {
		return nil, fmt.Errorf("correlation: cannot localize an empty alert batch")
	}
	start := time.Now()

	cond, err := g.Condense()
	if err != nil {
		return nil, err
	}

	rank := cond.CausalRank(0.85, 200)
	reach := cond.reachSets()
	cover := cond.greedyCover(rank, reach)

	loc := &Localization{
		Graph:  g,
		Cond:   cond,
		Attrib: make(map[string]AlertAttribution, len(g.Alerts)),
	}

	loc.Roots = make([]RootCause, 0, len(cover))
	for _, ci := range cover {
		comp := cond.Comps[ci]
		alerts := 0
		comps := 0
		for j := range cond.Comps {
			if reach[ci].has(j) {
				comps++
				alerts += len(cond.Comps[j].Members)
			}
		}
		members := make([]string, len(comp.Members))
		for i, ai := range comp.Members {
			members[i] = g.Alerts[ai].ID
		}
		sort.Strings(members)
		loc.Roots = append(loc.Roots, RootCause{
			Comp:          ci,
			AlertID:       g.Alerts[comp.Rep].ID,
			Rank:          rank[ci],
			CoveredComps:  comps,
			CoveredAlerts: alerts,
			SCCSize:       len(comp.Members),
			Collapsible:   comp.Collapsible,
			MemberIDs:     members,
		})
	}
	sort.SliceStable(loc.Roots, func(i, j int) bool {
		if loc.Roots[i].Rank != loc.Roots[j].Rank {
			return loc.Roots[i].Rank > loc.Roots[j].Rank
		}
		return loc.Roots[i].Comp < loc.Roots[j].Comp
	})
	for i := range loc.Roots {
		loc.Roots[i].Order = i + 1
	}

	loc.attribute(cover)
	loc.Elapsed = time.Since(start)
	return loc, nil
}

// CausalRank runs personalized PageRank on the reversed condensation.
// Restart mass is proportional to component severity weight; components with no
// causes are absorbing so that root candidates accumulate the effect mass they
// explain instead of leaking it back into the restart distribution.
func (c *Condensation) CausalRank(damping float64, maxIter int) []float64 {
	n := len(c.Comps)
	restart := make([]float64, n)
	total := 0.0
	for i, comp := range c.Comps {
		restart[i] = comp.Weight
		total += comp.Weight
	}
	if total <= 0 {
		for i := range restart {
			restart[i] = 1 / float64(n)
		}
	} else {
		for i := range restart {
			restart[i] /= total
		}
	}

	// Reversed, row-normalized transition: effect -> its causes.
	type link struct {
		to int
		w  float64
	}
	rev := make([][]link, n)
	for from := range c.Adj {
		for _, e := range c.Adj[from] {
			rev[e.To] = append(rev[e.To], link{to: from, w: e.Score})
		}
	}
	for v := range rev {
		sum := 0.0
		for _, l := range rev[v] {
			sum += l.w
		}
		if sum > 0 {
			for i := range rev[v] {
				rev[v][i].w /= sum
			}
		}
	}

	r := make([]float64, n)
	copy(r, restart)
	next := make([]float64, n)
	for iter := 0; iter < maxIter; iter++ {
		for i := range next {
			next[i] = (1 - damping) * restart[i]
		}
		for v := range rev {
			if len(rev[v]) == 0 {
				next[v] += damping * r[v] // absorbing source
				continue
			}
			for _, l := range rev[v] {
				next[l.to] += damping * r[v] * l.w
			}
		}
		delta := 0.0
		for i := range r {
			delta += math.Abs(next[i] - r[i])
			r[i] = next[i]
		}
		if delta < 1e-12 {
			break
		}
	}
	return r
}

// reachSets returns, for every component, the set of components reachable from
// it in the condensation (including itself).
func (c *Condensation) reachSets() []bitset {
	n := len(c.Comps)
	sets := make([]bitset, n)
	// Comps are topologically ordered, so processing backwards lets each node
	// reuse its successors' sets.
	for i := n - 1; i >= 0; i-- {
		b := newBitset(n)
		b.set(i)
		for _, e := range c.Adj[i] {
			b.or(sets[e.To])
		}
		sets[i] = b
	}
	return sets
}

// greedyCover selects the root components: repeatedly take the candidate that
// newly explains the most alerts, breaking ties by CausalRank and then by index.
func (c *Condensation) greedyCover(rank []float64, reach []bitset) []int {
	n := len(c.Comps)
	// Weight each component by how many alerts it holds so that covering a
	// large SCC counts more than covering a singleton.
	weightOf := make([]int, n)
	for i := range c.Comps {
		weightOf[i] = len(c.Comps[i].Members)
	}

	covered := newBitset(n)
	chosen := make([]bool, n)
	var out []int

	for {
		best, bestGain, bestRank := -1, 0, -1.0
		for i := 0; i < n; i++ {
			if chosen[i] {
				continue
			}
			gain := 0
			for j := 0; j < n; j++ {
				if reach[i].has(j) && !covered.has(j) {
					gain += weightOf[j]
				}
			}
			if gain == 0 {
				continue
			}
			if gain > bestGain || (gain == bestGain && rank[i] > bestRank) {
				best, bestGain, bestRank = i, gain, rank[i]
			}
		}
		if best < 0 {
			break
		}
		chosen[best] = true
		covered.or(reach[best])
		out = append(out, best)
	}

	sort.Ints(out)
	return out
}

// attribute assigns every alert to the root that explains it with the highest
// confidence. Component-level confidences are computed by a single multi-source
// relaxation in topological order, then pushed down to the alerts.
func (l *Localization) attribute(cover []int) {
	g, cond := l.Graph, l.Cond
	p := g.Params
	n := len(cond.Comps)

	isRoot := make([]bool, n)
	for _, ci := range cover {
		isRoot[ci] = true
	}

	type state struct {
		root int // component index of the attributed root, -1 when unattributed
		conf float64
		hops int
		edge int // index into g.Edges for the last hop, -1 when none
	}
	best := make([]state, n)
	for i := range best {
		best[i] = state{root: -1, conf: 0, hops: 0, edge: -1}
	}
	for _, ci := range cover {
		best[ci] = state{root: ci, conf: 1, hops: 0, edge: -1}
	}

	// Comps are topologically ordered: index order is a valid relaxation order.
	for _, ci := range cond.Order {
		cur := best[ci]
		if cur.root < 0 || cur.hops >= p.MaxPathHops {
			continue
		}
		for _, e := range cond.Adj[ci] {
			cand := p.compose(cur.conf, e.Score)
			if cand <= best[e.To].conf {
				continue
			}
			best[e.To] = state{root: cur.root, conf: cand, hops: cur.hops + 1, edge: e.BestEdge}
		}
	}

	repIDOf := func(ci int) string { return g.Alerts[cond.Comps[ci].Rep].ID }

	for ci := range cond.Comps {
		comp := cond.Comps[ci]
		repID := g.Alerts[comp.Rep].ID
		st := best[ci]

		var repAttr AlertAttribution
		switch {
		case isRoot[ci]:
			repAttr = AlertAttribution{RootAlertID: repID, Confidence: 1, PathHops: 0, IsRootRep: true}
		case st.root >= 0:
			repAttr = AlertAttribution{
				RootAlertID: repIDOf(st.root),
				Confidence:  st.conf,
				PathHops:    st.hops,
			}
			if st.edge >= 0 {
				e := g.Edges[st.edge]
				repAttr.LastEdge = &e
			}
		default:
			repAttr = AlertAttribution{}
		}
		l.Attrib[repID] = repAttr

		for _, ai := range comp.Members {
			id := g.Alerts[ai].ID
			if id == repID {
				continue
			}
			if !comp.Collapsible {
				// A loose cycle cannot speak for its members: emit them all.
				l.Attrib[id] = AlertAttribution{}
				continue
			}
			member := AlertAttribution{SameComponent: true}
			if repAttr.RootAlertID != "" {
				member.RootAlertID = repAttr.RootAlertID
				member.Confidence = p.compose(repAttr.Confidence, comp.Cohesion)
				member.PathHops = repAttr.PathHops
			}
			if e, ok := g.Edge(comp.Rep, ai); ok {
				ec := e
				member.LastEdge = &ec
			}
			l.Attrib[id] = member
		}
	}
}

// RootIDs returns the representative alert IDs of the selected roots in rank
// order.
func (l *Localization) RootIDs() []string {
	out := make([]string, len(l.Roots))
	for i, r := range l.Roots {
		out[i] = r.AlertID
	}
	return out
}

// Summary renders localization statistics for reports and test logs.
func (l *Localization) Summary() string {
	return fmt.Sprintf("roots=%d %s localize=%s", len(l.Roots), l.Cond.Summary(), l.Elapsed)
}
