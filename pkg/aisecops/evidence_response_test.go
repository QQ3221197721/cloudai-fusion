package aisecops

import (
	"crypto/ed25519"
	"testing"
)

func newTestResponseEngine(t *testing.T, thr float64) *EvidenceResponseEngine {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return NewEvidenceResponseEngine(priv, thr)
}

func TestResponseEngine_HighConfidenceAutoExecutes(t *testing.T) {
	e := newTestResponseEngine(t, 0.7)
	// Prime the history: this action has always worked for this threat type.
	for i := 0; i < 50; i++ {
		e.RecordOutcome(ActionIsolate, "ransomware", true)
	}
	dec, err := e.Decide(ActionIsolate, ThreatSignal{
		IncidentID: "inc-1", ThreatType: "ransomware",
		Severity: 0.95, Detection: 0.95, BlastRadius: 0.1, AssetCritical: 0.5,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !dec.AutoExecuted {
		t.Fatalf("expected auto-execution, got escalate (conf=%.3f, hist=%.3f)", dec.Confidence, dec.HistRate)
	}
	if dec.Action != ActionIsolate {
		t.Fatalf("expected action isolate, got %s", dec.Action)
	}
	if dec.Receipt == nil || !dec.Receipt.Verify() {
		t.Fatalf("receipt missing or invalid signature")
	}
}

func TestResponseEngine_LowConfidenceEscalates(t *testing.T) {
	e := newTestResponseEngine(t, 0.7)
	// No history + weak detection + huge blast radius => escalate to human.
	dec, err := e.Decide(ActionRemediate, ThreatSignal{
		IncidentID: "inc-2", ThreatType: "unknown",
		Severity: 0.3, Detection: 0.35, BlastRadius: 0.95, AssetCritical: 0.9,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.AutoExecuted {
		t.Fatalf("expected escalation, got auto-execute (conf=%.3f)", dec.Confidence)
	}
	if dec.Action != ActionEscalate {
		t.Fatalf("expected action escalate, got %s", dec.Action)
	}
	if !dec.Receipt.Verify() {
		t.Fatalf("receipt signature invalid")
	}
}

func TestResponseEngine_BlastRadiusSuppressesAutoAction(t *testing.T) {
	e := newTestResponseEngine(t, 0.7)
	for i := 0; i < 50; i++ {
		e.RecordOutcome(ActionBlock, "c2", true)
	}
	sig := ThreatSignal{IncidentID: "inc-3", ThreatType: "c2", Severity: 0.9, Detection: 0.9, AssetCritical: 0.5}

	lowBlast := sig
	lowBlast.BlastRadius = 0.05
	dLow, _ := e.Decide(ActionBlock, lowBlast)

	highBlast := sig
	highBlast.BlastRadius = 0.98
	dHigh, _ := e.Decide(ActionBlock, highBlast)

	if !(dLow.Confidence > dHigh.Confidence) {
		t.Fatalf("expected blast radius to lower confidence: low=%.3f high=%.3f", dLow.Confidence, dHigh.Confidence)
	}
}

func TestWilsonLowerBound_Pessimism(t *testing.T) {
	// 1/1 must be far less certain than 100/100.
	small := wilsonLowerBound(1, 1)
	large := wilsonLowerBound(100, 100)
	if !(small < large) {
		t.Fatalf("expected 1/1 (%.3f) to be less trusted than 100/100 (%.3f)", small, large)
	}
	if got := wilsonLowerBound(0, 0); got != 0.5 {
		t.Fatalf("empty prior expected 0.5, got %.3f", got)
	}
}

func TestResponseEngine_ReceiptsChain(t *testing.T) {
	e := newTestResponseEngine(t, 0.7)
	sig := ThreatSignal{IncidentID: "inc", ThreatType: "t", Severity: 0.5, Detection: 0.5, BlastRadius: 0.5}
	d1, _ := e.Decide(ActionBlock, sig)
	d2, _ := e.Decide(ActionBlock, sig)
	if d2.Receipt.PreviousReceiptID != d1.Receipt.ID {
		t.Fatalf("expected receipt chaining: %q -> %q", d1.Receipt.ID, d2.Receipt.PreviousReceiptID)
	}
}

func TestResponseEngine_Snapshot(t *testing.T) {
	e := newTestResponseEngine(t, 0.7)
	e.RecordOutcome(ActionIsolate, "b", true)
	e.RecordOutcome(ActionIsolate, "b", false)
	e.RecordOutcome(ActionBlock, "a", true)
	snap := e.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 policy entries, got %d", len(snap))
	}
	// Deterministic sort: "block|a" precedes "isolate|b".
	if snap[0].Key != "block|a" {
		t.Fatalf("snapshot not sorted deterministically: %s", snap[0].Key)
	}
}

func BenchmarkResponseEngine_Decide(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(nil)
	e := NewEvidenceResponseEngine(priv, 0.7)
	for i := 0; i < 20; i++ {
		e.RecordOutcome(ActionIsolate, "ransomware", true)
	}
	sig := ThreatSignal{IncidentID: "inc", ThreatType: "ransomware", Severity: 0.9, Detection: 0.9, BlastRadius: 0.2}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Decide(ActionIsolate, sig); err != nil {
			b.Fatal(err)
		}
	}
}
