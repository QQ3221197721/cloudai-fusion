package websocket

import (
	"testing"
	"time"
)

func TestEvidenceWSEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceWSEngine()
	res, err := e.HandleConnection("wss://example.com/ws", true, time.Millisecond*120)
	if err != nil {
		t.Fatalf("HandleConnection: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "websocket" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceWSEngine_FibonacciBackoff(t *testing.T) {
	e := NewEvidenceWSEngine()
	var bases []float64
	for i := 1; i <= 10; i++ {
		e.SetConsecutiveFailures(i)
		res, _ := e.HandleConnection("wss://fail", false, 0)
		bases = append(bases, res.BackoffSeconds)
	}
	// Verify monotonic increase (with jitter allowance).
	for i := 1; i < len(bases); i++ {
		if bases[i] <= bases[i-1]-0.1 { // allow 100ms jitter wiggle room
			t.Errorf("backoff not increasing sufficiently at retry=%d: %.2f vs %.2f",
				i+1, bases[i], bases[i-1])
		}
	}
}

func TestEvidenceWSEngine_JitterBounds(t *testing.T) {
	e := NewEvidenceWSEngine()
	sumBackoff := 0.0
	// Enough samples for the mean of the +/-0.5s uniform jitter to converge
	// within the tight +/-0.01 tolerance below (200 samples is too few).
	count := 2000
	for i := 0; i < count; i++ {
		// Reset before each attempt so the Fibonacci base stays fixed and we
		// isolate the jitter distribution (HandleConnection auto-increments it).
		e.SetConsecutiveFailures(1)
		res, _ := e.HandleConnection("wss://test", false, 0)
		sumBackoff += res.BackoffSeconds
	}
	meanBackoff := sumBackoff / float64(count)
	// Mean must be within jitter range of base value (1 second).
	if meanBackoff < 0.99 || meanBackoff > 1.01 {
		t.Errorf("mean backoff (%.3f) out of expected jitter range [0.99, 1.01]", meanBackoff)
	}
}

func TestEvidenceWSEngine_CappedBackoff(t *testing.T) {
	e := NewEvidenceWSEngine()
	e.SetConsecutiveFailures(70)
	res1, _ := e.HandleConnection("wss://max", false, 0)
	e.SetConsecutiveFailures(71)
	res2, _ := e.HandleConnection("wss://max", false, 0)
	if res2.BackoffSeconds > res1.BackoffSeconds+1.0 {
		t.Error("capped attempts at 70 must not grow beyond Fib(70)")
	}
}
