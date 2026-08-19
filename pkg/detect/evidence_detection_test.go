package detect

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func newTestDetectionEngine(t *testing.T) *EvidenceDetectionEngine {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidenceDetectionEngine(priv)
}

// TestAdaptiveThreshold_SpikeDetectedNormalSuppressed feeds 100 normal values,
// then verifies a sudden spike IS detected while normal variation is NOT.
func TestAdaptiveThreshold_SpikeDetectedNormalSuppressed(t *testing.T) {
	engine := newTestDetectionEngine(t)

	// Learning phase: 100 normal-ish values around 50 (+/- small jitter).
	base := []float64{49, 50, 51, 50, 48, 52, 50, 49, 51, 50}
	for i := 0; i < 100; i++ {
		v := base[i%len(base)]
		res, err := engine.Detect(map[string]interface{}{
			"rule_id": "cpu_rule",
			"metric":  "cpu",
			"value":   v,
		})
		if err != nil {
			t.Fatalf("detect normal: %v", err)
		}
		if !res.Receipt.Verify() {
			t.Fatalf("receipt failed verification at i=%d", i)
		}
	}

	// A value within normal variation must NOT trigger (suppressed).
	normal, err := engine.Detect(map[string]interface{}{
		"rule_id": "cpu_rule", "metric": "cpu", "value": 51.0,
	})
	if err != nil {
		t.Fatalf("detect normal probe: %v", err)
	}
	if normal.Triggered {
		t.Errorf("normal variation should not trigger, got Triggered=true")
	}
	if !normal.Suppressed {
		t.Errorf("normal variation should be suppressed by adaptive threshold")
	}

	// A sudden spike must be detected.
	spike, err := engine.Detect(map[string]interface{}{
		"rule_id": "cpu_rule", "metric": "cpu", "value": 500.0,
	})
	if err != nil {
		t.Fatalf("detect spike: %v", err)
	}
	if !spike.Triggered {
		t.Errorf("spike should be detected, got Triggered=false")
	}
	if spike.Suppressed {
		t.Errorf("spike must not be suppressed")
	}
	if !spike.Receipt.Verify() {
		t.Errorf("spike receipt failed verification")
	}
}

func TestMovingStats_LearningPeriod(t *testing.T) {
	s := &MovingStats{Alpha: 0.1}
	// During the first 10 observations nothing is flagged.
	for i := 0; i < 9; i++ {
		if s.IsAnomaly(1000, 3.0) {
			t.Fatalf("should not flag during learning period at i=%d", i)
		}
		s.Update(10)
	}
	if s.Count != 9 {
		t.Fatalf("expected count 9, got %d", s.Count)
	}
}

func TestDetect_NoNumericValueUsesRuleMatch(t *testing.T) {
	engine := newTestDetectionEngine(t)
	res, err := engine.Detect(map[string]interface{}{"rule_id": "sig_rule"})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if !res.Triggered {
		t.Errorf("rule match with no numeric value should trigger")
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Errorf("expected verifiable receipt")
	}
}

func BenchmarkDetectWithEvidence(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	engine := NewEvidenceDetectionEngine(priv)
	// Warm the baseline.
	for i := 0; i < 50; i++ {
		_, _ = engine.Detect(map[string]interface{}{"rule_id": "r", "metric": "m", "value": 50.0})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.Detect(map[string]interface{}{"rule_id": "r", "metric": "m", "value": 51.0})
	}
}
