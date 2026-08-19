package tracing

import "testing"

// TestCompleteTrace_ProducesVerifiableReceipt proves each trace completion is
// sealed into a signed, offline-verifiable receipt with the measured latency.
func TestCompleteTrace_ProducesVerifiableReceipt(t *testing.T) {
	engine := NewEvidenceTracingEngine()

	res, err := engine.CompleteTrace("trace-1", "api->db", []float64{5, 10, 3})
	if err != nil {
		t.Fatalf("complete trace: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("trace must carry a verifiable receipt")
	}
	if res.TotalLatency != 18 {
		t.Fatalf("expected total latency 18, got %.1f", res.TotalLatency)
	}
	if res.SpanCount != 3 {
		t.Fatalf("expected 3 spans, got %d", res.SpanCount)
	}
}

// TestCompleteTrace_RejectsBadInput verifies input validation.
func TestCompleteTrace_RejectsBadInput(t *testing.T) {
	engine := NewEvidenceTracingEngine()
	if _, err := engine.CompleteTrace("", "p", nil); err == nil {
		t.Fatal("expected error for empty trace id")
	}
	if _, err := engine.CompleteTrace("t", "", nil); err == nil {
		t.Fatal("expected error for empty path")
	}
}

// TestLatencyFingerprint_DetectsAnomaly builds a stable baseline for a path and
// then verifies a wildly slow trace is flagged as a fingerprint deviation.
func TestLatencyFingerprint_DetectsAnomaly(t *testing.T) {
	engine := NewEvidenceTracingEngine()

	// Establish a tight ~30ms baseline with small jitter so the fingerprint has
	// real (non-zero) variance.
	baseline := [][]float64{{10, 10, 10}, {11, 9, 10}, {9, 11, 10}, {10, 11, 9}, {12, 9, 10}}
	for i := 0; i < 20; i++ {
		if _, err := engine.CompleteTrace("t", "svc->cache", baseline[i%len(baseline)]); err != nil {
			t.Fatalf("baseline trace: %v", err)
		}
	}
	fp, ok := engine.Fingerprint("svc->cache")
	if !ok || fp.Count < 20 {
		t.Fatalf("expected established fingerprint, got %+v", fp)
	}

	// A 10x slower trace must be flagged anomalous.
	slow, err := engine.CompleteTrace("t-slow", "svc->cache", []float64{100, 100, 100})
	if err != nil {
		t.Fatalf("slow trace: %v", err)
	}
	if !slow.Anomalous {
		t.Fatalf("expected anomaly flag for 300ms trace against 30ms baseline, z=%.2f", slow.ZScore)
	}
	if slow.ZScore <= 3 {
		t.Fatalf("expected z-score above threshold, got %.2f", slow.ZScore)
	}
}

// TestLatencyFingerprint_NoColdStartFalsePositive ensures the first few traces
// (before a stable baseline exists) are never flagged anomalous.
func TestLatencyFingerprint_NoColdStartFalsePositive(t *testing.T) {
	engine := NewEvidenceTracingEngine()
	for i := 0; i < 3; i++ {
		res, err := engine.CompleteTrace("t", "cold->path", []float64{float64(i * 100)})
		if err != nil {
			t.Fatalf("trace: %v", err)
		}
		if res.Anomalous {
			t.Fatalf("cold-start trace %d must not be flagged anomalous", i)
		}
	}
}
