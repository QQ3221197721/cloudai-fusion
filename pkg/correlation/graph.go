// Package correlation implements causal alert correlation, root-cause
// localization and auditable suppression for alert storms.
//
// Prometheus Alertmanager groups alerts by label *equality* (`group_by`) and
// silences them with hand-written `inhibit_rules`. Neither mechanism can
// express "alert B happened because alert A happened": during a cascading
// failure one root fault produces a hundred alerts spread across many label
// sets, and label equality either collapses unrelated incidents together or
// pages a human once per service.
//
// This package builds a directed *causal candidate graph* over an alert batch
// from three independent signals — temporal precedence with a Granger-lite
// predictive-lift term, service dependency reachability, and IDF-weighted
// label overlap — condenses strongly connected components (alerts that are
// mutually indistinguishable in time within one scrape interval), localizes
// root causes with a CausalRank / greedy reachability-cover pair, and emits an
// Ed25519-signed suppression credential that a third party can verify offline.
//
// The safety-critical invariant is that a genuinely independent fault is never
// suppressed: an edge requires dependency reachability (or an overwhelming
// label match), so alerts from disconnected parts of the topology can never be
// attributed to one another.
package correlation

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Severity classifies alert urgency. Ordering is meaningful: a higher value is
// more urgent, and suppression never collapses a more severe alert into a less
// severe root.
type Severity int

const (
	// SeverityInfo is informational.
	SeverityInfo Severity = iota
	// SeverityWarning is a warning that warrants attention.
	SeverityWarning
	// SeverityMajor is a serious degradation.
	SeverityMajor
	// SeverityCritical is a hard outage.
	SeverityCritical
)

// String renders the severity for canonical encodings and reports.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityMajor:
		return "major"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Weight is the CausalRank restart mass contributed by an alert of this
// severity. Critical alerts pull more probability mass into their causes.
func (s Severity) Weight() float64 {
	switch s {
	case SeverityCritical:
		return 8
	case SeverityMajor:
		return 4
	case SeverityWarning:
		return 2
	default:
		return 1
	}
}

// Alert is one firing alert instance.
type Alert struct {
	// ID uniquely identifies the alert instance.
	ID string
	// Service is the topology node the alert belongs to.
	Service string
	// Instance is the concrete replica/host, used only as a label.
	Instance string
	// Kind is the alert rule name (e.g. "HighLatency"); the Granger-lite lag
	// profile is learned at kind granularity.
	Kind string
	// Severity is the urgency.
	Severity Severity
	// Timestamp is when the alert started firing.
	Timestamp time.Time
	// Labels carries the remaining label set.
	Labels map[string]string
}

// labelPairs returns the alert's label set as canonical "k=v" tokens including
// the synthetic service/instance/kind labels, in sorted order.
func (a Alert) labelPairs() []string {
	out := make([]string, 0, len(a.Labels)+3)
	out = append(out, "service="+a.Service, "instance="+a.Instance, "alertname="+a.Kind)
	for k, v := range a.Labels {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Service dependency topology
// ---------------------------------------------------------------------------

// Topology is a service dependency graph. An edge from → to means "from calls
// / depends on to", so a fault in `to` can propagate up into `from`. Cycles are
// permitted (retry loops between services are real).
type Topology struct {
	mu   sync.RWMutex
	deps map[string]map[string]struct{}
	// memo caches BFS hop distances per source service.
	memo map[string]map[string]int
}

// NewTopology returns an empty dependency graph.
func NewTopology() *Topology {
	return &Topology{
		deps: make(map[string]map[string]struct{}),
		memo: make(map[string]map[string]int),
	}
}

// AddDependency records that `from` depends on `to`.
func (t *Topology) AddDependency(from, to string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.deps[from] == nil {
		t.deps[from] = make(map[string]struct{})
	}
	t.deps[from][to] = struct{}{}
	if _, ok := t.deps[to]; !ok {
		t.deps[to] = make(map[string]struct{})
	}
	// Invalidate memoized distances: a single edge can shorten many paths.
	t.memo = make(map[string]map[string]int)
}

// Dependencies returns the direct dependencies of a service in sorted order.
func (t *Topology) Dependencies(service string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(t.deps[service]))
	for d := range t.deps[service] {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// Services returns every known service in sorted order.
func (t *Topology) Services() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(t.deps))
	for s := range t.deps {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Hops returns the shortest number of dependency edges from `from` down to
// `to`, i.e. how far the fault in `to` has to travel to reach `from`. It
// returns (0, true) when from == to and (0, false) when `to` is unreachable
// within maxHops.
func (t *Topology) Hops(from, to string, maxHops int) (int, bool) {
	if from == to {
		return 0, true
	}
	if maxHops <= 0 {
		return 0, false
	}
	t.mu.RLock()
	if d, ok := t.memo[from]; ok {
		h, found := d[to]
		t.mu.RUnlock()
		if found && h <= maxHops {
			return h, true
		}
		if found {
			return 0, false
		}
		return 0, false
	}
	t.mu.RUnlock()

	dist := t.bfs(from, maxHops)

	t.mu.Lock()
	t.memo[from] = dist
	t.mu.Unlock()

	h, found := dist[to]
	if !found || h > maxHops {
		return 0, false
	}
	return h, true
}

// bfs computes hop distances from `from` over dependency edges, bounded by
// maxHops. The returned map always contains `from` at distance 0.
func (t *Topology) bfs(from string, maxHops int) map[string]int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	dist := map[string]int{from: 0}
	frontier := []string{from}
	for depth := 1; depth <= maxHops && len(frontier) > 0; depth++ {
		var next []string
		for _, cur := range frontier {
			for dep := range t.deps[cur] {
				if _, seen := dist[dep]; seen {
					continue
				}
				dist[dep] = depth
				next = append(next, dep)
			}
		}
		sort.Strings(next)
		frontier = next
	}
	return dist
}

// ---------------------------------------------------------------------------
// Granger-lite lag profile
// ---------------------------------------------------------------------------

// LagProfile holds the Granger-lite predictive lift between alert kinds. For an
// ordered kind pair (c, e) the lift answers "does knowing that c fired in the
// preceding lag window improve the prediction that e fires, beyond c's base
// rate?". It is the discrete-time, single-lag analogue of a Granger causality
// test, expressed as a normalized lift in [0, 1].
type LagProfile struct {
	mu   sync.RWMutex
	lift map[string]float64
}

// NewLagProfile returns an empty profile. An empty profile is valid: every
// lift is 0 and the temporal signal degrades to pure precedence decay scaled by
// Params.GrangerFloor.
func NewLagProfile() *LagProfile {
	return &LagProfile{lift: make(map[string]float64)}
}

func lagKey(cause, effect string) string { return cause + "\x00" + effect }

// Set overrides the lift for a kind pair, clamped to [0, 1].
func (p *LagProfile) Set(cause, effect string, lift float64) {
	if lift < 0 {
		lift = 0
	}
	if lift > 1 {
		lift = 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lift[lagKey(cause, effect)] = lift
}

// Lift returns the predictive lift for a kind pair, 0 when unknown.
func (p *LagProfile) Lift(cause, effect string) float64 {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lift[lagKey(cause, effect)]
}

// Pairs returns the number of learned kind pairs.
func (p *LagProfile) Pairs() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.lift)
}

// Learn estimates the predictive lift for every ordered alert-kind pair from a
// corpus of historical incidents. For each incident the empirical conditional
// hit rate F1(c,e) is the fraction of e-arrivals preceded by a c-arrival inside
// `lag`; the null rate F0(c) is the probability of seeing a c-arrival in a
// window of the same length if c's arrivals were uniform over the incident
// span. The lift is the excess over the null, normalized by the headroom:
//
//	lift(c,e) = max(0, (F1 - F0) / (1 - F0))
//
// Lifts are averaged over incidents in which the effect kind occurred.
func (p *LagProfile) Learn(history [][]Alert, lag time.Duration) {
	if lag <= 0 {
		return
	}
	type acc struct {
		sum   float64
		count int
	}
	agg := make(map[string]*acc)

	for _, incident := range history {
		if len(incident) < 2 {
			continue
		}
		alerts := make([]Alert, len(incident))
		copy(alerts, incident)
		sort.SliceStable(alerts, func(i, j int) bool {
			if !alerts[i].Timestamp.Equal(alerts[j].Timestamp) {
				return alerts[i].Timestamp.Before(alerts[j].Timestamp)
			}
			return alerts[i].ID < alerts[j].ID
		})
		span := alerts[len(alerts)-1].Timestamp.Sub(alerts[0].Timestamp)
		if span <= 0 {
			continue
		}
		byKind := make(map[string][]time.Time)
		for _, a := range alerts {
			byKind[a.Kind] = append(byKind[a.Kind], a.Timestamp)
		}
		for effect, effTimes := range byKind {
			for cause, causeTimes := range byKind {
				if cause == effect {
					continue
				}
				hits := 0
				for _, et := range effTimes {
					for _, ct := range causeTimes {
						d := et.Sub(ct)
						if d > 0 && d <= lag {
							hits++
							break
						}
					}
				}
				f1 := float64(hits) / float64(len(effTimes))
				f0 := float64(len(causeTimes)) * (float64(lag) / float64(span))
				if f0 > 0.99 {
					f0 = 0.99
				}
				lift := 0.0
				if den := 1 - f0; den > 0 {
					lift = (f1 - f0) / den
				}
				if lift < 0 {
					lift = 0
				}
				k := lagKey(cause, effect)
				if agg[k] == nil {
					agg[k] = &acc{}
				}
				agg[k].sum += lift
				agg[k].count++
			}
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	for k, a := range agg {
		if a.count == 0 {
			continue
		}
		v := a.sum / float64(a.count)
		if v > 1 {
			v = 1
		}
		p.lift[k] = v
	}
}

// ---------------------------------------------------------------------------
// Parameters
// ---------------------------------------------------------------------------

// Composition selects the fuzzy t-norm used to compose edge scores along a
// causal path into an end-to-end confidence.
type Composition string

const (
	// CompositionGodel uses the Gödel t-norm (min), i.e. the widest-path /
	// bottleneck confidence. It does not decay with path length, so deep
	// cascades stay attributable; the hop budget bounds reach instead.
	CompositionGodel Composition = "godel"
	// CompositionProduct uses the product t-norm, which decays geometrically
	// with path length and is therefore far more conservative.
	CompositionProduct Composition = "product"
)

// Params configures graph construction, localization and suppression.
type Params struct {
	// Window is the maximum causal lag considered (W).
	Window time.Duration
	// Tau is the exponential decay constant of the temporal signal.
	Tau time.Duration
	// Epsilon is the simultaneity tolerance: alerts within Epsilon of each
	// other are treated as unordered (one scrape interval + clock skew), which
	// is what creates strongly connected components.
	Epsilon time.Duration
	// MaxHops bounds dependency reachability.
	MaxHops int
	// TopoDecay is the per-hop decay of the topology signal (rho).
	TopoDecay float64
	// WTime, WTopo, WLabel are the three signal weights; they are normalized
	// to sum to 1.
	WTime  float64
	WTopo  float64
	WLabel float64
	// GrangerFloor (gamma) is the fraction of the temporal signal retained
	// when no Granger-lite evidence exists for the kind pair.
	GrangerFloor float64
	// EdgeThreshold (theta) is the minimum combined score for a candidate edge.
	EdgeThreshold float64
	// LabelFloor is the label-overlap required to admit an edge between
	// services with no dependency path. It is deliberately high: this is the
	// gate that keeps independent incidents apart.
	LabelFloor float64
	// SCCCohesion is the minimum internal edge score required before a
	// strongly connected component may be collapsed to one representative.
	SCCCohesion float64
	// SuppressThreshold (theta_s) is the minimum path confidence required to
	// suppress a derived alert.
	SuppressThreshold float64
	// MaxPathHops bounds the causal path length eligible for suppression.
	MaxPathHops int
	// Composition selects the path composition t-norm.
	Composition Composition
}

// DefaultParams returns the calibrated defaults. Epsilon defaults to two
// seconds, roughly one Prometheus scrape interval plus NTP skew.
func DefaultParams() Params {
	return Params{
		Window:            5 * time.Minute,
		Tau:               60 * time.Second,
		Epsilon:           2 * time.Second,
		MaxHops:           6,
		TopoDecay:         0.6,
		WTime:             0.4,
		WTopo:             0.4,
		WLabel:            0.2,
		GrangerFloor:      0.6,
		EdgeThreshold:     0.35,
		LabelFloor:        0.8,
		SCCCohesion:       0.5,
		SuppressThreshold: 0.25,
		MaxPathHops:       8,
		Composition:       CompositionGodel,
	}
}

// Validate rejects parameter sets that cannot produce a well-defined score.
func (p Params) Validate() error {
	if p.Window <= 0 {
		return errors.New("correlation: Window must be positive")
	}
	if p.Tau <= 0 {
		return errors.New("correlation: Tau must be positive")
	}
	if p.Epsilon < 0 {
		return errors.New("correlation: Epsilon must not be negative")
	}
	if p.MaxHops < 0 {
		return errors.New("correlation: MaxHops must not be negative")
	}
	if p.TopoDecay <= 0 || p.TopoDecay > 1 {
		return fmt.Errorf("correlation: TopoDecay %.3f outside (0,1]", p.TopoDecay)
	}
	if sum := p.WTime + p.WTopo + p.WLabel; sum <= 0 {
		return errors.New("correlation: signal weights must sum to a positive value")
	}
	if p.GrangerFloor <= 0 || p.GrangerFloor > 1 {
		return fmt.Errorf("correlation: GrangerFloor %.3f outside (0,1]", p.GrangerFloor)
	}
	if p.MaxPathHops <= 0 {
		return errors.New("correlation: MaxPathHops must be positive")
	}
	switch p.Composition {
	case CompositionGodel, CompositionProduct:
	default:
		return fmt.Errorf("correlation: unknown composition %q", p.Composition)
	}
	return nil
}

// weights returns the normalized signal weights.
func (p Params) weights() (wt, wp, wl float64) {
	sum := p.WTime + p.WTopo + p.WLabel
	return p.WTime / sum, p.WTopo / sum, p.WLabel / sum
}

// ---------------------------------------------------------------------------
// Causal candidate graph
// ---------------------------------------------------------------------------

// Edge is a scored causal candidate "From caused To".
type Edge struct {
	From int
	To   int
	// Score is the weighted combination of the three signals.
	Score float64
	// TimeScore, TopoScore and LabelScore are the individual signals, kept for
	// the evidence chain.
	TimeScore  float64
	TopoScore  float64
	LabelScore float64
	// LagMillis is To.Timestamp - From.Timestamp in milliseconds.
	LagMillis int64
	// Hops is the dependency distance (0 = same service, -1 = unreachable).
	Hops int
	// Simultaneous is true when |lag| <= Epsilon, i.e. the ordering is not
	// observable and the pair is a candidate in both directions.
	Simultaneous bool
}

// CausalGraph is the directed candidate graph over one alert batch.
type CausalGraph struct {
	// Alerts is the batch sorted by (Timestamp, ID). Edge indices refer to it.
	Alerts []Alert
	// Edges holds every admitted candidate edge.
	Edges []Edge
	// Out and In index Edges by source / target alert.
	Out [][]int
	In  [][]int
	// Params is the configuration used to build the graph.
	Params Params
	// LabelIDF is the inverse document frequency of each label token in the
	// batch, used by the label-overlap signal.
	LabelIDF map[string]float64
	// Build is how long construction took.
	Build time.Duration
}

// Index maps an alert ID to its position, or -1.
func (g *CausalGraph) Index(id string) int {
	for i := range g.Alerts {
		if g.Alerts[i].ID == id {
			return i
		}
	}
	return -1
}

// Edge returns the edge from → to and whether it exists.
func (g *CausalGraph) Edge(from, to int) (Edge, bool) {
	for _, ei := range g.Out[from] {
		if g.Edges[ei].To == to {
			return g.Edges[ei], true
		}
	}
	return Edge{}, false
}

// BuildGraph constructs the causal candidate graph. topo and lag may be nil, in
// which case the topology signal is limited to same-service alerts and the
// Granger-lite term is absent.
func BuildGraph(alerts []Alert, topo *Topology, lag *LagProfile, p Params) (*CausalGraph, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	start := time.Now()

	sorted := make([]Alert, len(alerts))
	copy(sorted, alerts)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].Timestamp.Equal(sorted[j].Timestamp) {
			return sorted[i].Timestamp.Before(sorted[j].Timestamp)
		}
		return sorted[i].ID < sorted[j].ID
	})

	g := &CausalGraph{
		Alerts:   sorted,
		Params:   p,
		Out:      make([][]int, len(sorted)),
		In:       make([][]int, len(sorted)),
		LabelIDF: labelIDF(sorted),
	}

	tokens := make([][]string, len(sorted))
	for i := range sorted {
		tokens[i] = sorted[i].labelPairs()
	}

	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			lagDur := sorted[j].Timestamp.Sub(sorted[i].Timestamp)
			if lagDur > p.Window {
				break // sorted by time: no later alert can be in-window
			}
			label := g.labelOverlap(tokens[i], tokens[j])
			if e, ok := g.score(i, j, label, topo, lag); ok {
				g.add(e)
			}
			if lagDur <= p.Epsilon {
				// Ordering is unobservable; the reverse direction is an equally
				// valid candidate. This is what forms SCCs.
				if e, ok := g.score(j, i, label, topo, lag); ok {
					g.add(e)
				}
			}
		}
	}

	g.Build = time.Since(start)
	return g, nil
}

// add appends an edge and updates the adjacency indexes.
func (g *CausalGraph) add(e Edge) {
	idx := len(g.Edges)
	g.Edges = append(g.Edges, e)
	g.Out[e.From] = append(g.Out[e.From], idx)
	g.In[e.To] = append(g.In[e.To], idx)
}

// score evaluates the ordered pair (from → to) and reports whether it passes
// the admission gate.
func (g *CausalGraph) score(from, to int, label float64, topo *Topology, lp *LagProfile) (Edge, bool) {
	p := g.Params
	u, v := g.Alerts[from], g.Alerts[to]

	ts, ok := temporalScore(u, v, p, lp)
	if !ok {
		return Edge{}, false
	}
	hops, topoScore := topologyScore(u, v, topo, p)
	wt, wp, wl := p.weights()
	total := wt*ts + wp*topoScore + wl*label

	// Admission gate: the score must clear the threshold AND the pair must be
	// connected by the dependency graph, or share an overwhelming label match.
	// Without this gate, two unrelated incidents that merely overlap in time
	// would become causally linked — the exact failure mode of label grouping.
	if total < p.EdgeThreshold {
		return Edge{}, false
	}
	if topoScore <= 0 && label < p.LabelFloor {
		return Edge{}, false
	}

	lagMs := v.Timestamp.Sub(u.Timestamp).Milliseconds()
	h := -1
	if topoScore > 0 {
		h = hops
	}
	return Edge{
		From:         from,
		To:           to,
		Score:        total,
		TimeScore:    ts,
		TopoScore:    topoScore,
		LabelScore:   label,
		LagMillis:    lagMs,
		Hops:         h,
		Simultaneous: absDuration(v.Timestamp.Sub(u.Timestamp)) <= p.Epsilon,
	}, true
}

// temporalScore implements the time signal:
//
//	S_time(u,v) = base(delta) * (gamma + (1-gamma) * lift(kind_u, kind_v))
//
// where delta = t_v - t_u and
//
//	base = 0                          for delta < -eps or delta > W
//	base = 1                          for |delta| <= eps  (unordered)
//	base = exp(-(delta-eps)/tau)      for eps < delta <= W
func temporalScore(u, v Alert, p Params, lp *LagProfile) (float64, bool) {
	delta := v.Timestamp.Sub(u.Timestamp)
	if delta > p.Window || delta < -p.Epsilon {
		return 0, false
	}
	base := 1.0
	if delta > p.Epsilon {
		base = math.Exp(-float64(delta-p.Epsilon) / float64(p.Tau))
	}
	lift := lp.Lift(u.Kind, v.Kind)
	return base * (p.GrangerFloor + (1-p.GrangerFloor)*lift), true
}

// topologyScore implements the dependency reachability signal: rho^d where d is
// the hop distance from v's service down to u's service (a fault in u must be
// able to reach v). Same-service alerts have d = 0.
func topologyScore(u, v Alert, topo *Topology, p Params) (int, float64) {
	if u.Service == v.Service {
		return 0, 1.0
	}
	if topo == nil {
		return -1, 0
	}
	hops, ok := topo.Hops(v.Service, u.Service, p.MaxHops)
	if !ok {
		return -1, 0
	}
	return hops, math.Pow(p.TopoDecay, float64(hops))
}

// labelIDF computes inverse document frequency per label token so that a match
// on a rare token (instance, pod, alertname) counts for more than a match on a
// ubiquitous one (cluster, env). This is what makes the label signal
// discriminative where Alertmanager's equality grouping is not.
func labelIDF(alerts []Alert) map[string]float64 {
	df := make(map[string]int)
	for _, a := range alerts {
		for _, tok := range a.labelPairs() {
			df[tok]++
		}
	}
	n := float64(len(alerts))
	idf := make(map[string]float64, len(df))
	for tok, c := range df {
		idf[tok] = math.Log(1 + n/float64(c))
	}
	return idf
}

// labelOverlap is the IDF-weighted Jaccard similarity of two sorted token sets.
func (g *CausalGraph) labelOverlap(a, b []string) float64 {
	var inter, union float64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			w := g.LabelIDF[a[i]]
			inter += w
			union += w
			i++
			j++
		case a[i] < b[j]:
			union += g.LabelIDF[a[i]]
			i++
		default:
			union += g.LabelIDF[b[j]]
			j++
		}
	}
	for ; i < len(a); i++ {
		union += g.LabelIDF[a[i]]
	}
	for ; j < len(b); j++ {
		union += g.LabelIDF[b[j]]
	}
	if union == 0 {
		return 0
	}
	return inter / union
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// Summary renders a compact human-readable description of the graph, used in
// reports and test logs.
func (g *CausalGraph) Summary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "alerts=%d edges=%d", len(g.Alerts), len(g.Edges))
	simul := 0
	for _, e := range g.Edges {
		if e.Simultaneous {
			simul++
		}
	}
	fmt.Fprintf(&sb, " simultaneous_edges=%d build=%s", simul, g.Build)
	return sb.String()
}
