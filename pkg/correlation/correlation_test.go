package correlation

// correlation_test.go covers the algorithm's correctness invariants and, above
// all, its safety invariant: independent incidents must never suppress each
// other. The safety tests are written to fail loudly with the offending alert
// pair rather than a bare boolean, because a regression here is an operational
// incident, not a style nit.

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func testParams() Params { return DefaultParams() }

// --- scoring -------------------------------------------------------------

func TestTemporalScoreShape(t *testing.T) {
	p := testParams()
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	mk := func(off time.Duration) Alert {
		return Alert{ID: "x", Service: "s", Kind: "K", Timestamp: base.Add(off)}
	}
	lp := NewLagProfile()

	// Simultaneous within epsilon scores 1 before the Granger blend.
	if s, ok := temporalScore(mk(0), mk(time.Second), p, lp); !ok || s <= 0 {
		t.Fatalf("within-epsilon pair should score positive, got %v ok=%v", s, ok)
	}
	// Monotone decay with lag.
	s1, _ := temporalScore(mk(0), mk(10*time.Second), p, lp)
	s2, _ := temporalScore(mk(0), mk(60*time.Second), p, lp)
	if !(s1 > s2) {
		t.Fatalf("temporal score must decay with lag: s(10s)=%v s(60s)=%v", s1, s2)
	}
	// Beyond the window there is no candidate at all.
	if _, ok := temporalScore(mk(0), mk(p.Window+time.Second), p, lp); ok {
		t.Fatalf("pair beyond window must be rejected")
	}
	// Backwards in time beyond epsilon is not a candidate cause.
	if _, ok := temporalScore(mk(30*time.Second), mk(0), p, lp); ok {
		t.Fatalf("effect before cause must be rejected")
	}
}

func TestTopologyHopsAndDecay(t *testing.T) {
	topo := NewTopology()
	topo.AddDependency("a", "b")
	topo.AddDependency("b", "c")
	p := testParams()

	if d, ok := topo.Hops("a", "c", p.MaxHops); !ok || d != 2 {
		t.Fatalf("a->c should be 2 hops, got %d ok=%v", d, ok)
	}
	if _, ok := topo.Hops("c", "a", p.MaxHops); ok {
		t.Fatalf("dependency edges are directed: c must not reach a")
	}

	// cause=c (deep), effect=a (shallow): a depends on c, so c can explain a.
	_, near := topologyScore(Alert{Service: "b"}, Alert{Service: "a"}, topo, p)
	_, far := topologyScore(Alert{Service: "c"}, Alert{Service: "a"}, topo, p)
	if !(near > far && far > 0) {
		t.Fatalf("topology score must decay with distance: 1hop=%v 2hop=%v", near, far)
	}
	_, unreachable := topologyScore(Alert{Service: "lonely"}, Alert{Service: "a"}, topo, p)
	if unreachable != 0 {
		t.Fatalf("unreachable services must score 0, got %v", unreachable)
	}
}

func TestAdmissionGateBlocksUnrelatedServices(t *testing.T) {
	// Two services with no dependency path and only the ambient cluster/env
	// labels in common must not produce an edge, however close in time.
	topo := NewTopology()
	topo.AddDependency("left", "left-db")
	topo.AddDependency("right", "right-db")
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	alerts := []Alert{
		{ID: "l1", Service: "left-db", Kind: "ConnectionRefused", Severity: SeverityCritical, Timestamp: base, Labels: map[string]string{"cluster": "prod"}},
		{ID: "r1", Service: "right-db", Kind: "ConnectionRefused", Severity: SeverityCritical, Timestamp: base.Add(2 * time.Second), Labels: map[string]string{"cluster": "prod"}},
	}
	g, err := BuildGraph(alerts, topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Edges {
		t.Fatalf("admission gate leaked an edge between unrelated services: %+v", e)
	}
}

// --- SCC handling --------------------------------------------------------

func TestCondenseCollapsesSimultaneousCycle(t *testing.T) {
	// Two alerts on the same service inside one scrape bucket are mutually
	// candidate causes, forming a real 2-cycle that must be condensed.
	topo := NewTopology()
	topo.AddDependency("svc", "svc-db")
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	alerts := []Alert{
		{ID: "s1", Service: "svc", Kind: "HighLatency", Severity: SeverityMajor, Timestamp: base, Labels: map[string]string{"tier": "app"}},
		{ID: "s2", Service: "svc", Kind: "QueueBacklog", Severity: SeverityMajor, Timestamp: base.Add(500 * time.Millisecond), Labels: map[string]string{"tier": "app"}},
	}
	g, err := BuildGraph(alerts, topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	cond, err := g.Condense()
	if err != nil {
		t.Fatal(err)
	}
	if len(cond.Comps) != 1 || len(cond.Comps[0].Members) != 2 {
		t.Fatalf("expected one 2-member SCC, got %s (%d comps)", cond.Summary(), len(cond.Comps))
	}
	if !cond.Comps[0].Collapsible {
		t.Fatalf("same-service same-bucket SCC should be cohesive, cohesion=%v", cond.Comps[0].Cohesion)
	}
}

func TestCondensationIsADAGInTopologicalOrder(t *testing.T) {
	for _, sc := range buildCorpus() {
		g, err := BuildGraph(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
		if err != nil {
			t.Fatalf("%s: %v", sc.Name, err)
		}
		cond, err := g.Condense()
		if err != nil {
			t.Fatalf("%s: %v", sc.Name, err)
		}
		for from := range cond.Adj {
			for _, e := range cond.Adj[from] {
				if e.To <= from {
					t.Fatalf("%s: condensation edge %d->%d violates topological numbering", sc.Name, from, e.To)
				}
			}
		}
	}
}

// --- root cause ----------------------------------------------------------

func TestLocalizeFindsCascadeRoot(t *testing.T) {
	sc := buildCascade(4242, 0)
	g, err := BuildGraph(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	loc, err := Localize(g)
	if err != nil {
		t.Fatal(err)
	}
	want := sc.TrueRoots["inc-cascade"]
	found := false
	for _, r := range loc.Roots {
		if r.AlertID == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("cascade root %s not among localized roots %v (%s)", want, loc.RootIDs(), loc.Summary())
	}
}

func TestConcurrentIncidentsYieldOneRootEach(t *testing.T) {
	sc := buildConcurrent(777, 0)
	dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Roots) < sc.Incidents {
		t.Fatalf("expected at least %d roots for %d independent incidents, got %d (%s)",
			sc.Incidents, sc.Incidents, len(dec.Roots), dec.Summary())
	}
	// Every incident must own at least one root.
	covered := map[string]bool{}
	for _, r := range dec.Roots {
		covered[sc.IncidentOf[r.AlertID]] = true
	}
	for inc := range sc.TrueRoots {
		if !covered[inc] {
			t.Fatalf("incident %s has no localized root; roots=%v", inc, dec.Roots)
		}
	}
}

// --- safety: the central invariant --------------------------------------

// TestZeroMisSuppressionAcrossCorpus is the safety gate demanded by the task:
// no alert may ever be suppressed by a root belonging to a different incident.
func TestZeroMisSuppressionAcrossCorpus(t *testing.T) {
	corpus := buildCorpus()
	if len(corpus) < 100 {
		t.Fatalf("corpus must hold at least 100 scenarios, got %d", len(corpus))
	}
	violations := 0
	for _, sc := range corpus {
		dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
		if err != nil {
			t.Fatalf("%s: %v", sc.Name, err)
		}
		for _, v := range dec.Verdicts {
			if !v.Suppressed() {
				continue
			}
			if v.RootAlertID == "" {
				t.Fatalf("%s: alert %s suppressed with no root", sc.Name, v.AlertID)
			}
			if !sc.sameIncident(v.AlertID, v.RootAlertID) {
				violations++
				t.Errorf("%s: MIS-SUPPRESSION %s (incident %s) hidden by %s (incident %s) conf=%.3f hops=%d reason=%s",
					sc.Name, v.AlertID, sc.IncidentOf[v.AlertID], v.RootAlertID,
					sc.IncidentOf[v.RootAlertID], v.Confidence, v.PathHops, v.Reason)
			}
		}
	}
	if violations > 0 {
		t.Fatalf("mis-suppression must be zero, got %d violations across %d scenarios", violations, len(corpus))
	}
}

// TestConcurrentClassNeverSuppressesAcrossIslands isolates the concurrent class
// so a failure points straight at the admission gate.
func TestConcurrentClassNeverSuppressesAcrossIslands(t *testing.T) {
	for _, sc := range buildCorpus() {
		if sc.Class != "concurrent" {
			continue
		}
		dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
		if err != nil {
			t.Fatalf("%s: %v", sc.Name, err)
		}
		for _, v := range dec.Verdicts {
			if v.Suppressed() && !sc.sameIncident(v.AlertID, v.RootAlertID) {
				t.Fatalf("%s: cross-island suppression %s <- %s", sc.Name, v.AlertID, v.RootAlertID)
			}
		}
	}
}

// TestSeverityEscalationNeverSuppressed asserts gate G5.
func TestSeverityEscalationNeverSuppressed(t *testing.T) {
	for _, sc := range buildCorpus() {
		dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
		if err != nil {
			t.Fatalf("%s: %v", sc.Name, err)
		}
		for _, v := range dec.Verdicts {
			if v.Suppressed() && v.Severity > v.RootSeverity {
				t.Fatalf("%s: alert %s (%s) suppressed by less severe root %s (%s)",
					sc.Name, v.AlertID, v.Severity, v.RootAlertID, v.RootSeverity)
			}
		}
	}
}

// TestRootsAreNeverSuppressed asserts gate G2.
func TestRootsAreNeverSuppressed(t *testing.T) {
	for _, sc := range buildCorpus() {
		dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
		if err != nil {
			t.Fatalf("%s: %v", sc.Name, err)
		}
		roots := map[string]bool{}
		for _, r := range dec.Roots {
			roots[r.AlertID] = true
		}
		for _, v := range dec.Verdicts {
			if roots[v.AlertID] && v.Suppressed() {
				t.Fatalf("%s: localized root %s was suppressed", sc.Name, v.AlertID)
			}
		}
	}
}

// TestSuppressThresholdOneSuppressesNothingUnsafe checks the dial's upper end:
// at threshold 1.0 only perfect-confidence paths may be folded.
func TestSuppressThresholdMonotonicity(t *testing.T) {
	sc := buildSPOF(31337, 0)
	prev := -1
	for _, th := range []float64{0.05, 0.25, 0.5, 0.75, 0.95} {
		p := testParams()
		p.SuppressThreshold = th
		dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), p)
		if err != nil {
			t.Fatal(err)
		}
		if prev >= 0 && dec.SuppressedCount > prev {
			t.Fatalf("suppression must not grow as the threshold tightens: th=%.2f gave %d after %d",
				th, dec.SuppressedCount, prev)
		}
		prev = dec.SuppressedCount
	}
}

// --- determinism ---------------------------------------------------------

func TestDecisionIsDeterministic(t *testing.T) {
	sc := buildPartition(9001, 0)
	first, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
		if err != nil {
			t.Fatal(err)
		}
		if again.GraphDigest != first.GraphDigest {
			t.Fatalf("graph digest is not stable: %s vs %s", again.GraphDigest, first.GraphDigest)
		}
		if len(again.Verdicts) != len(first.Verdicts) {
			t.Fatalf("verdict count changed between runs")
		}
		for j := range again.Verdicts {
			a, b := again.Verdicts[j], first.Verdicts[j]
			if a.AlertID != b.AlertID || a.Verdict != b.Verdict || a.RootAlertID != b.RootAlertID {
				t.Fatalf("verdict %d differs between runs: %+v vs %+v", j, a, b)
			}
		}
	}
}

// --- evidence chain ------------------------------------------------------

func TestCredentialRoundTripAndTamperDetection(t *testing.T) {
	sc := buildSPOF(555, 0)
	dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub

	data, err := CanonicalForm(dec)
	if err != nil {
		t.Fatal(err)
	}
	cred := NewCredential(dec, "test-signer", time.Hour)
	cred.IncidentID = "inc-spof"
	if err := cred.Issue(data, priv, time.Now().Add(-time.Minute), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !cred.Verify(data, priv) {
		t.Fatalf("freshly issued credential failed verification")
	}

	// Tamper with the canonical bytes: verification must fail.
	tampered := make([]byte, len(data))
	copy(tampered, data)
	tampered[len(tampered)/2] ^= 0xFF
	if cred.Verify(tampered, priv) {
		t.Fatalf("credential verified against tampered decision bytes")
	}

	// A credential from a different incident must not verify here.
	other := buildCascade(556, 0)
	otherDec, err := Correlate(other.Alerts, other.Topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	otherData, err := CanonicalForm(otherDec)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Verify(otherData, priv) {
		t.Fatalf("credential verified against a different incident")
	}
}

func TestCanonicalFormIsStable(t *testing.T) {
	sc := buildMixed(2026, 0)
	dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	a, err := CanonicalForm(dec)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalForm(dec)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("canonical form is not stable across calls")
	}
}

// --- Granger-lite --------------------------------------------------------

func TestLagProfileLearnRewardsRealPrecedence(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	var history [][]Alert
	for i := 0; i < 40; i++ {
		start := base.Add(time.Duration(i) * time.Hour)
		history = append(history, []Alert{
			{ID: "c", Service: "db", Kind: "ConnectionRefused", Timestamp: start},
			{ID: "e", Service: "api", Kind: "TimeoutExceeded", Timestamp: start.Add(5 * time.Second)},
			{ID: "n", Service: "cron", Kind: "QueueBacklog", Timestamp: start.Add(4 * time.Hour)},
		})
	}
	lp := NewLagProfile()
	lp.Learn(history, 30*time.Second)
	strong := lp.Lift("ConnectionRefused", "TimeoutExceeded")
	weak := lp.Lift("QueueBacklog", "ConnectionRefused")
	if !(strong > weak) {
		t.Fatalf("learned lift must favour the real precedence: strong=%v weak=%v", strong, weak)
	}
}

// --- baselines -----------------------------------------------------------

func TestBaselinesRunOnCorpus(t *testing.T) {
	baselines := []Baseline{
		&NoDedup{},
		&NaiveTimeWindowDedup{Window: 5 * time.Minute},
		&AlertmanagerGrouping{
			GroupBy: []string{"cluster", "alertname"},
			InhibitRules: []InhibitRule{{
				SourceMatch:       "",
				TargetMatch:       "",
				SeveritySourceMin: SeverityCritical,
				SeverityTargetMax: SeverityWarning,
				EqualLabels:       []string{"cluster"},
			}},
		},
	}
	for _, sc := range buildCorpus()[:10] {
		for _, b := range baselines {
			dec, err := b.Decide(sc.Alerts)
			if err != nil {
				t.Fatalf("%s/%s: %v", sc.Name, b.Name(), err)
			}
			if dec.Total != len(sc.Alerts) {
				t.Fatalf("%s/%s: total %d != %d alerts", sc.Name, b.Name(), dec.Total, len(sc.Alerts))
			}
			if dec.Emitted+dec.SuppressedCount != dec.Total {
				t.Fatalf("%s/%s: verdict accounting broken: %d+%d != %d",
					sc.Name, b.Name(), dec.Emitted, dec.SuppressedCount, dec.Total)
			}
		}
	}
}

func TestNoDedupCompressesNothing(t *testing.T) {
	sc := buildCascade(1, 0)
	dec, err := (&NoDedup{}).Decide(sc.Alerts)
	if err != nil {
		t.Fatal(err)
	}
	if dec.CompressionRatio() != 0 {
		t.Fatalf("no_dedup must compress nothing, got %.3f", dec.CompressionRatio())
	}
}

// --- input validation ----------------------------------------------------

func TestBuildGraphRejectsBadParams(t *testing.T) {
	p := testParams()
	p.Window = 0
	if _, err := BuildGraph([]Alert{{ID: "a", Service: "s", Timestamp: time.Now()}}, NewTopology(), NewLagProfile(), p); err == nil {
		t.Fatalf("zero window must be rejected")
	}
}

func TestLocalizeRejectsEmptyBatch(t *testing.T) {
	if _, err := Localize(&CausalGraph{}); err == nil {
		t.Fatalf("empty batch must be rejected")
	}
}
