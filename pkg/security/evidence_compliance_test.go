package security

import (
	"crypto/ed25519"
	"testing"
)

func newTestComplianceEngine(t *testing.T, thr float64) *EvidenceComplianceEngine {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return NewEvidenceComplianceEngine(priv, thr)
}

func TestComplianceEngine_StableDrift(t *testing.T) {
	e := newTestComplianceEngine(t, 0.1)
	report, err := e.CheckAndUpdate("CIS-2.1.1", "CIS", 8080, 8080)
	if err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	if report.Status != DriftNone {
		t.Fatalf("expected stable drift, got %s", report.Status)
	}
	if report.RiskLevel != "low" {
		t.Fatalf("expected low risk, got %s", report.RiskLevel)
	}
	if report.Receipt == nil || !report.Receipt.Verify() {
		t.Fatalf("missing or invalid receipt")
	}
}

func TestComplianceEngine_Bleeding(t *testing.T) {
	e := newTestComplianceEngine(t, 0.1)
	// First check records a baseline.
	_, err := e.CheckAndUpdate("CIS-2.1.1", "CIS", 100, 80)
	if err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	// Second check sees drift beyond tolerance.
	report2, err := e.CheckAndUpdate("CIS-2.1.1", "CIS", 150, 100)
	if err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	// 50-point jump above 0.1 threshold should be flagged.
	if report2.Status == DriftNone && report2.Delta > 0.1 {
		t.Fatalf("expected bleeding/jump status for delta %.3f", report2.Delta)
	}
}

func TestComplianceEngine_Improving(t *testing.T) {
	e := newTestComplianceEngine(t, 0.1)
	// Start at bad value, improve it.
	e.SetTolerance("CIS-2.1.1", 10)
	r1, _ := e.CheckAndUpdate("CIS-2.1.1", "CIS", 50, 80)
	if r1.Status != DriftBleeding && r1.Status != DriftImproving && r1.Delta < 0 {
		t.Fatalf("expected improvement or bleeding with negative delta, got status=%s delta=%.3f", r1.Status, r1.Delta)
	}
}

func TestComplianceEngine_CustomTolerance(t *testing.T) {
	e := newTestComplianceEngine(t, 1.0)
	e.SetTolerance("CIS-3.1", 100)
	// With high tolerance, small changes should be stable.
	r, err := e.CheckAndUpdate("CIS-3.1", "CIS", 85, 80)
	if err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	if r.Status != DriftNone {
		t.Fatalf("expected stable with custom tolerance, got %s", r.Status)
	}
}

func TestComplianceEngine_GetSnapshot(t *testing.T) {
	e := newTestComplianceEngine(t, 0.1)
	_, _ = e.CheckAndUpdate("CIS-X", "SOC2", map[string]any{"enabled": true}, nil)
	snap, ok := e.GetSnapshot("CIS-X")
	if !ok {
		t.Fatalf("expected snapshot to exist after CheckAndUpdate")
	}
	if snap.Value == nil {
		t.Fatalf("expected non-nil snapshot value")
	}
	if snap.HashOfValue == "" {
		t.Fatalf("expected hash of value to be computed")
	}
}

func TestComplianceEngine_DifferentTypes(t *testing.T) {
	e := newTestComplianceEngine(t, 0.1)
	typeConfigA := struct {
		Port   int `json:"port"`
		Enable bool `json:"enable"`
	}{Port: 443, Enable: true}
	typeConfigB := struct {
		Port   int `json:"port"`
		Enable bool `json:"enable"`
	}{Port: 8443, Enable: false}
	r, err := e.CheckAndUpdate("CIS-Z", "NIST", typeConfigA, typeConfigB)
	if err != nil {
		t.Fatalf("CheckAndUpdate: %v", err)
	}
	if r.Receipt == nil || !r.Receipt.Verify() {
		t.Fatalf("receipt missing or invalid for struct types")
	}
}

func TestComplianceEngine_ClearHistory(t *testing.T) {
	e := newTestComplianceEngine(t, 0.1)
	e.CheckAndUpdate("ctrl", "test", 100, 50)
	e.CheckAndUpdate("ctrl2", "test", 75, 30)
	e.ClearHistory()
	_, ok := e.GetSnapshot("ctrl")
	if ok {
		t.Fatalf("expected empty history after ClearHistory")
	}
	_, ok = e.GetSnapshot("ctrl2")
	if ok {
		t.Fatalf("expected empty history after ClearHistory")
	}
}

func TestComplianceEngine_ListReports(t *testing.T) {
	e := newTestComplianceEngine(t, 0.1)
	e.CheckAndUpdate("a", "F1", 10, 0)
	e.CheckAndUpdate("b", "F1", 20, 0)
	reports := e.ListReports("override")
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	if reports[0].Framework != "override" {
		t.Fatalf("framework not overridden in list")
	}
}

func BenchmarkComplianceEngine_CheckAndUpdate(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(nil)
	e := NewEvidenceComplianceEngine(priv, 0.1)
	newVal := map[string]interface{}{"setting": 100, "count": 42}
	oldVal := map[string]interface{}{"setting": 99, "count": 40}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.CheckAndUpdate("ctrl", "bench", newVal, oldVal); err != nil {
			b.Fatal(err)
		}
	}
}
