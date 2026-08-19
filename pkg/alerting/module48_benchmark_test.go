package alerting

// module48_benchmark_test.go collects the correctness assertions and
// benchmarks used for the Module 48 capability/performance comparison against
// Prometheus Alertmanager. It exercises only the real, already-shipped API:
//
//   - dedup / suppression : CausalCorrelationEngine.Correlate + isSimilar
//   - parent -> child      : root alert delivered, related alerts suppressed
//   - escalation           : EscalationPolicy.NextLevel / Escalate
//   - evidence-signed proof: EvidenceAlertManager.SendAlert
//
// No new production behaviour is introduced here; these are measurement and
// correctness harnesses over Paul's delivery.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Correctness: parent alert suppresses child alerts
// ---------------------------------------------------------------------------

// TestParentSuppressesChild verifies the correlation engine treats the first
// alert of a group as the delivered "parent" (root) and suppresses every
// subsequent similar "child" alert into that same group.
func TestParentSuppressesChild(t *testing.T) {
	e := &CausalCorrelationEngine{window: 5 * time.Minute}

	parent := EvidenceAlert{ID: "parent", Source: "db-primary", Labels: map[string]string{"cluster": "c1"}, Timestamp: time.Now()}
	if g := e.Correlate(parent); g != nil {
		t.Fatalf("first alert must be a fresh root (parent), got suppressed into group %s", g.ID)
	}

	const children = 5
	for i := 0; i < children; i++ {
		child := EvidenceAlert{ID: "child", Source: "db-primary", Labels: map[string]string{"cluster": "c1"}, Timestamp: time.Now()}
		g := e.Correlate(child)
		if g == nil {
			t.Fatalf("child %d must be suppressed into the parent group, got fresh root", i)
		}
		if g.RootAlert.ID != "parent" {
			t.Errorf("child %d joined group rooted at %q; want parent", i, g.RootAlert.ID)
		}
	}

	if len(e.groups) != 1 {
		t.Fatalf("expected exactly 1 group after parent+children, got %d", len(e.groups))
	}
	if got := len(e.groups[0].Related); got != children {
		t.Errorf("parent group should hold %d suppressed children, got %d", children, got)
	}
}

// ---------------------------------------------------------------------------
// Correctness: unacknowledged alert escalates by elapsed time
// ---------------------------------------------------------------------------

// TestUnacknowledgedEscalationTiming walks wall-clock elapsed time across the
// cumulative level timeouts and asserts NextLevel advances at each boundary,
// i.e. an alert left unacknowledged is promoted to more urgent levels on time.
func TestUnacknowledgedEscalationTiming(t *testing.T) {
	policy := &EscalationPolicy{
		Levels: []EscalationLevel{
			{Timeout: 1 * time.Minute},  // level 0 owns [0, 1m)
			{Timeout: 2 * time.Minute},  // level 1 owns [1m, 3m)
			{Timeout: 5 * time.Minute},  // level 2 owns [3m, 8m)
		},
	}

	cases := []struct {
		elapsed time.Duration
		want    int
	}{
		{0, 0},
		{59 * time.Second, 0},
		{1 * time.Minute, 1},           // boundary: exactly 1m is no longer < 1m
		{2*time.Minute + 59*time.Second, 1},
		{3 * time.Minute, 2},
		{7*time.Minute + 59*time.Second, 2},
		{8 * time.Minute, -1},          // fully escalated / exhausted
		{20 * time.Minute, -1},
	}
	for _, c := range cases {
		if got := policy.NextLevel(c.elapsed); got != c.want {
			t.Errorf("NextLevel(%s)=%d; want %d", c.elapsed, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Correctness: suppression window expiry allows re-notification
// ---------------------------------------------------------------------------

// TestSuppressionWindowExpiryAllowsResend proves that once the correlation
// window elapses, an alert that was previously suppressed is delivered fresh
// again (a new root group is created) rather than being silently swallowed
// forever.
func TestSuppressionWindowExpiryAllowsResend(t *testing.T) {
	const window = 60 * time.Millisecond
	e := &CausalCorrelationEngine{window: window}

	a := EvidenceAlert{ID: "a", Source: "svc", Labels: map[string]string{"k": "v"}, Timestamp: time.Now()}

	if g := e.Correlate(a); g != nil {
		t.Fatalf("first alert must be fresh, got suppressed into %s", g.ID)
	}
	if g := e.Correlate(a); g == nil {
		t.Fatalf("second alert inside window must be suppressed, got fresh root")
	}

	// Let the window expire.
	time.Sleep(window + 40*time.Millisecond)

	if g := e.Correlate(a); g != nil {
		t.Fatalf("after window expiry the alert must be delivered fresh again, got suppressed into %s", g.ID)
	}
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

// BenchmarkIsSimilarMatchLatency measures the per-comparison cost of the
// suppression rule matcher (source equality OR >50% label overlap). This is the
// unit of work Alertmanager performs as label-set fingerprint/inhibition
// matching.
func BenchmarkIsSimilarMatchLatency(b *testing.B) {
	e := &CausalCorrelationEngine{window: 5 * time.Minute}
	a := EvidenceAlert{Source: "node-1", Labels: map[string]string{"host": "web-1", "az": "us-east-1a", "job": "node"}}
	c := EvidenceAlert{Source: "node-2", Labels: map[string]string{"host": "web-1", "az": "us-east-1a", "job": "node"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.isSimilar(a, c)
	}
}

// BenchmarkCorrelateDedupThroughput measures the end-to-end dedup/suppression
// decision throughput against a pre-seeded root group (the common storm case:
// one root, many correlated children collapsed into it).
func BenchmarkCorrelateDedupThroughput(b *testing.B) {
	e := &CausalCorrelationEngine{window: time.Hour}
	// Seed a single root group that everything correlates into.
	_ = e.Correlate(EvidenceAlert{ID: "root", Source: "storm", Labels: map[string]string{"k": "v"}, Timestamp: time.Now()})
	child := EvidenceAlert{ID: "child", Source: "storm", Labels: map[string]string{"k": "v"}, Timestamp: time.Now()}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Correlate(child)
	}
}

// BenchmarkCorrelateScan measures the linear-scan cost of matching an incoming
// alert against a set of distinct existing groups (worst case: no early match).
func BenchmarkCorrelateScan(b *testing.B) {
	const groups = 100
	incoming := EvidenceAlert{Source: "unique-source", Labels: map[string]string{"z": "z"}, Timestamp: time.Now()}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		e := &CausalCorrelationEngine{window: time.Hour}
		for g := 0; g < groups; g++ {
			_ = e.Correlate(EvidenceAlert{Source: "src-" + string(rune('A'+g%26)) + string(rune('0'+g/26)), Labels: map[string]string{"g": string(rune(g))}, Timestamp: time.Now()})
		}
		b.StartTimer()
		_ = e.isSimilarScan(incoming)
	}
}

// isSimilarScan is a benchmark-only helper mirroring Correlate's linear scan
// without mutating the engine, so BenchmarkCorrelateScan measures pure match
// cost over N groups. It relies only on the exported grouping state.
func (e *CausalCorrelationEngine) isSimilarScan(alert EvidenceAlert) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, g := range e.groups {
		if e.isSimilar(alert, g.RootAlert) {
			return true
		}
	}
	return false
}

// BenchmarkEscalationNextLevelLatency measures the escalation check that maps
// elapsed unacknowledged time to a level index.
func BenchmarkEscalationNextLevelLatency(b *testing.B) {
	policy := &EscalationPolicy{
		Levels: []EscalationLevel{
			{Timeout: 1 * time.Minute},
			{Timeout: 2 * time.Minute},
			{Timeout: 5 * time.Minute},
			{Timeout: 10 * time.Minute},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = policy.NextLevel(4 * time.Minute)
	}
}

// BenchmarkEscalateDelivery measures a full escalation walk where the first
// level delivers successfully via an in-memory channel (no network).
func BenchmarkEscalateDelivery(b *testing.B) {
	policy := &EscalationPolicy{
		Levels: []EscalationLevel{
			{Channels: []NotificationChannel{noopChannel{}}},
		},
	}
	alert := Alert{ID: "e1", Severity: SeverityHigh, Source: "bench", Message: "x", Timestamp: time.Now()}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = policy.Escalate(ctx, alert)
	}
}

// noopChannel is a zero-cost NotificationChannel for isolating escalation logic
// from real I/O in benchmarks.
type noopChannel struct{}

func (noopChannel) Name() string                              { return "noop" }
func (noopChannel) ValidateConfig() error                     { return nil }
func (noopChannel) Send(context.Context, Alert) error         { return nil }

// BenchmarkSendAlertEvidenceSigned measures the evidence-native path: dedup
// decision + signed, offline-verifiable delivery proof. This is the capability
// Alertmanager has no equivalent for.
func BenchmarkSendAlertEvidenceSigned(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	mgr := NewEvidenceAlertManager(priv)
	alert := EvidenceAlert{ID: "a", Source: "src", Labels: map[string]string{"k": "v"}, Timestamp: time.Now()}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mgr.SendAlert(alert)
	}
}
