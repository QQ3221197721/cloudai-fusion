package correlation

// suppress.go turns a Localization into an emit/suppress decision per alert.
//
// The decision is deliberately conservative. Compression is worthless if it
// hides an unrelated incident, so every suppression must clear five independent
// gates. Failing any one of them emits the alert. The gates are ordered from
// cheapest to most expensive so the common "obviously independent" case exits
// early.
//
//	G1 attribution   the alert must be attributed to a root at all. Unattributed
//	                 alerts (no causal path from any selected root) always emit.
//	                 This is what protects concurrent independent incidents: the
//	                 admission gate in BuildGraph refuses to draw an edge between
//	                 topologically unreachable services with low label overlap,
//	                 so independent storms land in separate components, each
//	                 becomes its own root, and nothing crosses over.
//	G2 not-a-root    a root representative is the thing operators need to see.
//	G3 cohesion      members of a non-cohesive SCC are never spoken for. Condense
//	                 marks those components Collapsible=false and attribute()
//	                 already blanks their members; the gate is restated here so
//	                 the invariant survives refactors of either file.
//	G4 confidence    composed path confidence must reach SuppressThreshold. This
//	                 is the single knob traded off in the compression/mis-
//	                 suppression curve.
//	G5 severity      a derived alert may not be strictly more severe than the
//	                 root that explains it. A critical effect hanging off a
//	                 warning cause means the causal story is incomplete, so the
//	                 effect is escalated rather than hidden.
//	G6 evidence      a real edge must exist on the final hop (or the alert must
//	                 fold into its own component). "Attributed with no evidence"
//	                 is treated as a bug and emits.
//
// Gates G1-G3 are structural and cannot be tuned away. Only G4 is a dial, which
// is why the trade-off curve in the design doc sweeps SuppressThreshold alone.

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Verdict is the per-alert outcome of the suppression decision.
type Verdict string

const (
	// VerdictRoot means the alert is a localized root cause and is emitted with
	// the derived alerts it explains attached.
	VerdictRoot Verdict = "root"
	// VerdictSuppressed means the alert is derived and was folded into its root.
	VerdictSuppressed Verdict = "suppressed"
	// VerdictEmitted means the alert is derived-but-not-safe-to-hide, or is
	// unattributed, and must reach the operator on its own.
	VerdictEmitted Verdict = "emitted"
)

// Reason codes explain a verdict. They are stable strings so that the signed
// credential remains auditable across releases.
const (
	ReasonRootCause       = "root_cause"
	ReasonUnattributed    = "unattributed"
	ReasonNonCohesiveSCC  = "non_cohesive_scc"
	ReasonLowConfidence   = "low_confidence"
	ReasonSeverityEscalat = "severity_escalation"
	ReasonNoEvidence      = "no_evidence"
	ReasonCausalDerived   = "causal_derived"
	ReasonSameComponent   = "same_component"
)

// AlertVerdict is the decision for a single alert.
type AlertVerdict struct {
	// AlertID is the alert this verdict applies to.
	AlertID string
	// Verdict is root, suppressed or emitted.
	Verdict Verdict
	// Reason is one of the Reason* codes above.
	Reason string
	// RootAlertID is the explaining root. Empty for unattributed alerts.
	RootAlertID string
	// Confidence is the composed causal path confidence in [0,1].
	Confidence float64
	// PathHops is the causal distance from the root in condensation edges.
	PathHops int
	// Severity is the alert's own severity, retained so an auditor can re-check
	// gate G5 from the credential alone.
	Severity Severity
	// RootSeverity is the explaining root's severity, zero when unattributed.
	RootSeverity Severity
	// EdgeScore, TimeScore, TopoScore and LabelScore are the final hop's signal
	// breakdown, retained as evidence. All zero when there is no final hop.
	EdgeScore  float64
	TimeScore  float64
	TopoScore  float64
	LabelScore float64
}

// Suppressed reports whether this verdict hides the alert.
func (v AlertVerdict) Suppressed() bool { return v.Verdict == VerdictSuppressed }

// Decision is the full outcome for one alert batch.
type Decision struct {
	// Verdicts is ordered by alert ID for determinism.
	Verdicts []AlertVerdict
	// Roots lists the localized roots in rank order.
	Roots []RootCause
	// Params is the configuration the decision was made under.
	Params Params
	// Total, Emitted and SuppressedCount summarize the batch.
	Total            int
	Emitted          int
	SuppressedCount  int
	// GraphDigest binds the decision to the exact alert batch and topology that
	// produced it. Any change to the inputs changes the digest, so a credential
	// cannot be replayed against a different incident.
	GraphDigest string
	// Elapsed is the end-to-end decision latency (build + localize + decide).
	Elapsed time.Duration
}

// CompressionRatio is the fraction of alerts removed from the operator's queue.
func (d *Decision) CompressionRatio() float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.SuppressedCount) / float64(d.Total)
}

// Verdict looks up the decision for one alert.
func (d *Decision) Verdict(alertID string) (AlertVerdict, bool) {
	i := sort.Search(len(d.Verdicts), func(i int) bool { return d.Verdicts[i].AlertID >= alertID })
	if i < len(d.Verdicts) && d.Verdicts[i].AlertID == alertID {
		return d.Verdicts[i], true
	}
	return AlertVerdict{}, false
}

// SuppressedIDs returns the sorted IDs of every hidden alert.
func (d *Decision) SuppressedIDs() []string {
	out := make([]string, 0, d.SuppressedCount)
	for _, v := range d.Verdicts {
		if v.Suppressed() {
			out = append(out, v.AlertID)
		}
	}
	return out
}

// Decide applies the suppression gates to a localization.
func Decide(loc *Localization) (*Decision, error) {
	if loc == nil || loc.Graph == nil {
		return nil, fmt.Errorf("correlation: cannot decide on a nil localization")
	}
	g := loc.Graph
	p := g.Params

	sevOf := make(map[string]Severity, len(g.Alerts))
	for _, a := range g.Alerts {
		sevOf[a.ID] = a.Severity
	}
	compOf := func(alertID string) (Component, bool) {
		idx := g.Index(alertID)
		if idx < 0 {
			return Component{}, false
		}
		return loc.Cond.Comps[loc.Cond.CompOf[idx]], true
	}

	d := &Decision{
		Verdicts: make([]AlertVerdict, 0, len(g.Alerts)),
		Roots:    loc.Roots,
		Params:   p,
		Total:    len(g.Alerts),
	}

	rootIDs := make(map[string]bool, len(loc.Roots))
	for _, r := range loc.Roots {
		rootIDs[r.AlertID] = true
	}

	for _, a := range g.Alerts {
		attr := loc.Attrib[a.ID]
		v := AlertVerdict{
			AlertID:      a.ID,
			RootAlertID:  attr.RootAlertID,
			Confidence:   attr.Confidence,
			PathHops:     attr.PathHops,
			Severity:     a.Severity,
			RootSeverity: sevOf[attr.RootAlertID],
		}
		if e := attr.LastEdge; e != nil {
			v.EdgeScore, v.TimeScore, v.TopoScore, v.LabelScore = e.Score, e.TimeScore, e.TopoScore, e.LabelScore
		}

		switch {
		case rootIDs[a.ID]:
			// G2: roots are the payload of the whole exercise.
			v.Verdict, v.Reason = VerdictRoot, ReasonRootCause
			v.RootAlertID, v.Confidence = a.ID, 1

		case attr.RootAlertID == "":
			// G1: no causal story, no suppression. Independent incidents land here.
			v.Verdict, v.Reason = VerdictEmitted, ReasonUnattributed

		case func() bool { c, ok := compOf(a.ID); return ok && !c.Collapsible }():
			// G3: a loose cycle may not speak for its members.
			v.Verdict, v.Reason = VerdictEmitted, ReasonNonCohesiveSCC

		case attr.Confidence < p.SuppressThreshold:
			// G4: the only tunable gate.
			v.Verdict, v.Reason = VerdictEmitted, ReasonLowConfidence

		case a.Severity > sevOf[attr.RootAlertID]:
			// G5: never hide something worse than its stated cause.
			v.Verdict, v.Reason = VerdictEmitted, ReasonSeverityEscalat

		case attr.LastEdge == nil && !attr.SameComponent:
			// G6: attribution without a witness edge is a bug, fail open.
			v.Verdict, v.Reason = VerdictEmitted, ReasonNoEvidence

		default:
			v.Verdict = VerdictSuppressed
			if attr.SameComponent {
				v.Reason = ReasonSameComponent
			} else {
				v.Reason = ReasonCausalDerived
			}
		}

		d.Verdicts = append(d.Verdicts, v)
	}

	sort.Slice(d.Verdicts, func(i, j int) bool { return d.Verdicts[i].AlertID < d.Verdicts[j].AlertID })
	for _, v := range d.Verdicts {
		if v.Suppressed() {
			d.SuppressedCount++
		} else {
			d.Emitted++
		}
	}
	d.GraphDigest = graphDigest(g)
	d.Elapsed = g.Build + loc.Elapsed
	return d, nil
}

// Correlate is the end-to-end entry point: build the candidate graph, localize
// roots and decide suppression.
func Correlate(alerts []Alert, topo *Topology, lag *LagProfile, p Params) (*Decision, error) {
	g, err := BuildGraph(alerts, topo, lag, p)
	if err != nil {
		return nil, err
	}
	loc, err := Localize(g)
	if err != nil {
		return nil, err
	}
	return Decide(loc)
}

// graphDigest hashes the alert batch and the scoring parameters that produced a
// decision. Auditors recompute it from the raw alerts to confirm that a
// credential belongs to the incident they are looking at.
func graphDigest(g *CausalGraph) string {
	h := sha256.New()
	var buf [8]byte
	writeU64 := func(u uint64) {
		binary.BigEndian.PutUint64(buf[:], u)
		h.Write(buf[:])
	}
	for _, a := range g.Alerts {
		h.Write([]byte(a.ID))
		h.Write([]byte{0})
		h.Write([]byte(a.Service))
		h.Write([]byte{0})
		h.Write([]byte(a.Instance))
		h.Write([]byte{0})
		h.Write([]byte(a.Kind))
		h.Write([]byte{0})
		writeU64(uint64(a.Severity))
		writeU64(uint64(a.Timestamp.UTC().UnixNano()))
		for _, kv := range a.labelPairs() {
			h.Write([]byte(kv))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	h.Write([]byte("params\x00"))
	p := g.Params
	writeU64(uint64(p.Window))
	writeU64(uint64(p.Tau))
	writeU64(uint64(p.Epsilon))
	writeU64(uint64(p.MaxHops))
	writeU64(uint64(p.MaxPathHops))
	for _, f := range []float64{
		p.TopoDecay, p.WTime, p.WTopo, p.WLabel, p.GrangerFloor,
		p.EdgeThreshold, p.LabelFloor, p.SCCCohesion, p.SuppressThreshold,
	} {
		h.Write([]byte(fmt.Sprintf("%.12g;", f)))
	}
	h.Write([]byte(p.Composition))
	return hex.EncodeToString(h.Sum(nil))
}

// Summary renders decision statistics for reports and test logs.
func (d *Decision) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "total=%d emitted=%d suppressed=%d compression=%.3f roots=%d latency=%s",
		d.Total, d.Emitted, d.SuppressedCount, d.CompressionRatio(), len(d.Roots), d.Elapsed)
	return b.String()
}
