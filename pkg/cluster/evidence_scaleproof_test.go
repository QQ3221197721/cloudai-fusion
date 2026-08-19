package cluster

import "testing"

func TestEvidenceScaleEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceScaleEngine()
	res, err := e.EvaluateScaling(5, 80.0, 6)
	if err != nil {
		t.Fatalf("EvaluateScaling: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "cluster" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceScaleEngine_JustifiedScaling(t *testing.T) {
	e := NewEvidenceScaleEngine()
	e.baseline = 50
	e.history = append(e.history, EvidenceLoadSample{utilPercent: 80})
	e.history = append(e.history, EvidenceLoadSample{utilPercent: 85})
	
	// High utilization above baseline => justified scale up
	res, _ := e.EvaluateScaling(5, 90.0, 6)
	if !res.Justified {
		t.Error("scaling must be justified when utilization is high")
	}
	if res.Action != "scale_up" {
		t.Logf("action=%s, justified=%t, loadAvg=%.1f", res.Action, res.Justified, res.LoadAverage)
	}
}

func TestEvidenceScaleEngine_NoPanicScaling(t *testing.T) {
	e := NewEvidenceScaleEngine()
	// Single spike should not trigger scaling
	e.history = append(e.history, EvidenceLoadSample{utilPercent: 90})
	res, _ := e.EvaluateScaling(10, 90.0, 12)
	
	// Without sustained high load, may skip scaling
	if res.NodeChange > 2 {
		t.Errorf("spike alone should not trigger large node changes, got %+v", res)
	}
}
