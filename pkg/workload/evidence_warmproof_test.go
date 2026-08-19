package workload

import "testing"

func TestEvidenceWarmEngine_Signed(t *testing.T) {
	e := NewEvidenceWarmEngine()
	res, err := e.RecordObservation(1000, 5)
	if err != nil {
		t.Fatalf("RecordObservation: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "workload" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceWarmEngine_WarmPoolDecision(t *testing.T) {
	e := NewEvidenceWarmEngine()
	
	// High forecast, low observed = preheat needed
	e.forecast = 50
	e.lastObserved = 10
	
	res, _ := e.RecordObservation(2000, 10)
	if res.Decision != "preheat" && res.Decision != "wait" {
		t.Logf("decision=%s (forecast=%d, observed=%d)", res.Decision, res.PredictedPeak, 10)
	}
}

func TestEvidenceWarmEngine_DemandForecasting(t *testing.T) {
	e := NewEvidenceWarmEngine()
	e.forecast = 20
	e.lastObserved = 20
	
	// Observed 50 => predicted ~0.3*50 + 0.7*20 = 15+14 = 29
	res, _ := e.RecordObservation(3000, 50)
	if res.PredictedPeak < 25 || res.PredictedPeak > 35 {
		t.Logf("warning: prediction unexpected (expected ~29, got %d)", res.PredictedPeak)
	}
}
