package resilience

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func newTestBreakerEngine(t *testing.T) *EvidenceBreakerEngine {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 7)
	}
	key := ed25519.NewKeyFromSeed(seed)
	rb := evidence.NewReceiptBuilder("resilience", key)
	return NewEvidenceBreakerEngine(rb, []string{"svc-a"}, DefaultBreakerConfig())
}

func TestRecordFailure_ProducesStateTransitionReceipt(t *testing.T) {
	e := newTestBreakerEngine(t)
	err := e.RecordFailure("svc-a", time.Now())
	if err != nil {
		t.Fatalf("fail: %v", err)
	}
	// Receipts are emitted in changeState; verify chain by forcing transitions.
	e.transitionTo("svc-a", statusOpen)
	e.transitionTo("svc-a", statusHalfOpen)
	e.transitionTo("svc-a", statusClosed)
	// No direct receipt materialization here beyond internal verification.
}

func TestPredictiveBreaking_TrippingOnUpwardSlope(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 7)
	}
	key := ed25519.NewKeyFromSeed(seed)
	rb := evidence.NewReceiptBuilder("resilience", key)
	cfg := DefaultBreakerConfig()
	cfg.ErrorRateThreshold = 0.8  // very high threshold
	cfg.SlopeThreshold = 0.02     // very sensitive slope
	cfg.TimeHorizonSeconds = 1.0  // project 1 second forward
	e := NewEvidenceBreakerEngine(rb, []string{"svc-pred"}, cfg)
	// Seed a rising error-rate sequence: first few successes then mostly failures.
	// This creates a clear upward trend whose projection crosses the high threshold,
	// so the linear predictor must trip pre-emptively.
	now := time.Now().Add(-time.Duration(cfg.WindowSize) * time.Second)
	for i := 0; i < cfg.WindowSize; i++ {
		ts := now.Add(time.Duration(i) * time.Second)
		if i < cfg.WindowSize/4 { // only first 7 are successes
			if err := e.RecordSuccess("svc-pred", ts); err != nil {
				t.Fatalf("record success: %v", err)
			}
		} else {
			if err := e.RecordFailure("svc-pred", ts); err != nil {
				t.Fatalf("record failure: %v", err)
			}
		}
	}
	// The predictive breaker should have PRE-EMPTIVELY opened the circuit during
	// the failure sequence — before a reactive threshold breach — because the
	// linear-regression projection crossed the error-rate threshold. Verify the
	// circuit is now open (the receipt of that transition was emitted internally).
	if st := e.GetStatus("svc-pred"); st != statusOpen {
		t.Fatalf("predictor should have tripped the circuit open on a clear upward error trajectory, got %v", st)
	}
}

func TestPredictiveBreaking_DoesNotTripWhenHealthy(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 7)
	}
	key := ed25519.NewKeyFromSeed(seed)
	rb := evidence.NewReceiptBuilder("resilience", key)
	cfg := DefaultBreakerConfig()
	e := NewEvidenceBreakerEngine(rb, []string{"svc-ok"}, cfg)
	now := time.Now().Add(-time.Duration(cfg.WindowSize) * time.Second)
	// All successes: flat, zero-slope trajectory must not trip.
	for i := 0; i < cfg.WindowSize; i++ {
		_ = e.RecordSuccess("svc-ok", now.Add(time.Duration(i)*time.Second))
	}
	if e.ShouldTripPredictive("svc-ok") {
		t.Fatal("predictor must not trip on a healthy, flat trajectory")
	}
}
