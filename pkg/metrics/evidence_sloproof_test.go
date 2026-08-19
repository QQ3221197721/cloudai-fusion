package metrics

import "testing"

func TestEvidenceSLOEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceSLOEngine()
	res, err := e.EvaluateSLO("api-availability", 0, 1.0)
	if err != nil {
		t.Fatalf("EvaluateSLO: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "metrics" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceSLOEngine_BurnRatePrediction(t *testing.T) {
	e := NewEvidenceSLOEngine()
	// Budget drains linearly: 1.0 -> 0.6 over 400s => burn 0.001/s.
	// At elapsed 400 with 0.6 remaining, exhaustion is ~600s later (t=1000).
	samples := []struct {
		t   float64
		rem float64
	}{
		{0, 1.0}, {100, 0.9}, {200, 0.8}, {300, 0.7}, {400, 0.6},
	}
	var last *EvidenceSLOResult
	for _, s := range samples {
		r, err := e.EvaluateSLO("api-availability", s.t, s.rem)
		if err != nil {
			t.Fatalf("EvaluateSLO: %v", err)
		}
		last = r
	}
	if !last.Exhausting {
		t.Fatal("expected budget to be flagged as exhausting")
	}
	if last.BurnRatePerSecond <= 0 {
		t.Errorf("expected positive burn rate, got %.6f", last.BurnRatePerSecond)
	}
	// Zero crossing at t=1000, current elapsed=400 => ~600s remaining.
	if last.SecondsToExhaustion < 500 || last.SecondsToExhaustion > 700 {
		t.Errorf("exhaustion prediction out of range: %.1f", last.SecondsToExhaustion)
	}
}

func TestEvidenceSLOEngine_NotBurningWhenStable(t *testing.T) {
	e := NewEvidenceSLOEngine()
	for i := 0; i < 5; i++ {
		r, _ := e.EvaluateSLO("stable", float64(i*60), 0.95)
		if i == 4 && r.Exhausting {
			t.Error("stable budget must not be flagged as exhausting")
		}
	}
}
