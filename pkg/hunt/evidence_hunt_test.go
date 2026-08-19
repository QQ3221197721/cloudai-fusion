package hunt

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func newTestHuntEngine(t *testing.T) *EvidenceHuntEngine {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return NewEvidenceHuntEngine(priv)
}

func evAt(base time.Time, offsetMs int64, typ string) Event {
	return Event{
		Timestamp: base.Add(time.Duration(offsetMs) * time.Millisecond),
		EventType: typ,
		Source:    "host-1",
		Target:    "resource-1",
	}
}

func TestHuntEngine_MinesKnownPattern(t *testing.T) {
	e := newTestHuntEngine(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// login then priv_esc within 5 min => matches TP-001.
	events := []Event{
		evAt(base, 0, "login"),
		evAt(base, 60000, "priv_esc"), // 1 minute later
	}
	results, err := e.Mine(events)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least one pattern match")
	}
	found := false
	for _, r := range results {
		if r.Pattern.PatternID == "TP-001" {
			found = true
			if len(r.Matches) == 0 {
				t.Fatalf("TP-001 matched but no concrete matches recorded")
			}
			if r.Receipt == nil || !r.Receipt.Verify() {
				t.Fatalf("receipt missing or invalid for TP-001")
			}
		}
	}
	if !found {
		t.Fatalf("expected TP-001 (login->priv_esc) to be discovered")
	}
}

func TestHuntEngine_RespectsTimeWindow(t *testing.T) {
	e := newTestHuntEngine(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// priv_esc happens 10 minutes after login => beyond TP-001's 5-minute window.
	events := []Event{
		evAt(base, 0, "login"),
		evAt(base, 600000, "priv_esc"),
	}
	results, _ := e.Mine(events)
	for _, r := range results {
		if r.Pattern.PatternID == "TP-001" {
			t.Fatalf("TP-001 should NOT match when priv_esc is outside the 5-minute window")
		}
	}
}

func TestHuntEngine_RespectsOrder(t *testing.T) {
	e := newTestHuntEngine(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// priv_esc BEFORE login => TP-001 should not match (order matters).
	events := []Event{
		evAt(base, 0, "priv_esc"),
		evAt(base, 60000, "login"),
	}
	results, _ := e.Mine(events)
	for _, r := range results {
		if r.Pattern.PatternID == "TP-001" {
			t.Fatalf("TP-001 requires login BEFORE priv_esc; matched wrongly")
		}
	}
}

func TestHuntEngine_CustomPattern(t *testing.T) {
	e := newTestHuntEngine(t)
	e.RegisterPattern(&TemporalPattern{
		PatternID:  "CUSTOM-1",
		Sequence:   []string{"a", "b", "c"},
		DeltaMaxMs: 10000,
		Confidence: 0.5,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{
		evAt(base, 0, "a"),
		evAt(base, 1000, "b"),
		evAt(base, 2000, "c"),
	}
	results, err := e.Mine(events)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Pattern.PatternID == "CUSTOM-1" {
			found = true
			m := r.Matches[0]
			if m.ActualDeltaMs != 2000 {
				t.Fatalf("expected actual delta 2000ms, got %d", m.ActualDeltaMs)
			}
		}
	}
	if !found {
		t.Fatalf("expected 3-event CUSTOM-1 pattern to match")
	}
}

func TestHuntEngine_HistoricalRateLearning(t *testing.T) {
	e := newTestHuntEngine(t)
	for i := 0; i < 20; i++ {
		e.RecordOutcome("TP-002", true)
	}
	e.RecordOutcome("TP-002", false)
	snap := e.Snapshot()
	found := false
	for _, s := range snap {
		if s.Key == "TP-002" {
			found = true
			if s.Total != 21 || s.Success != 20 {
				t.Fatalf("expected 20/21, got %d/%d", s.Success, s.Total)
			}
			if s.WilsonLower <= 0.5 {
				t.Fatalf("expected high wilson lower bound for 20/21, got %.3f", s.WilsonLower)
			}
		}
	}
	if !found {
		t.Fatalf("expected TP-002 in snapshot")
	}
}

func TestHuntEngine_EmptyEvents(t *testing.T) {
	e := newTestHuntEngine(t)
	results, err := e.Mine(nil)
	if err != nil {
		t.Fatalf("Mine on empty: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for empty event stream")
	}
}

func TestHuntEngine_GetPatterns(t *testing.T) {
	e := newTestHuntEngine(t)
	pats := e.GetPatterns()
	if len(pats) < 3 {
		t.Fatalf("expected at least 3 seeded patterns, got %d", len(pats))
	}
}

func BenchmarkHuntEngine_Mine(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(nil)
	e := NewEvidenceHuntEngine(priv)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []Event{
		evAt(base, 0, "scan"),
		evAt(base, 30000, "exploit"),
		evAt(base, 45000, "login"),
		evAt(base, 90000, "priv_esc"),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Mine(events); err != nil {
			b.Fatal(err)
		}
	}
}
