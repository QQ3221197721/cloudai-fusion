package observability

import "testing"

func TestEvidenceTraceEngine_Signed(t *testing.T) {
	e := NewEvidenceTraceEngine()
	res, err := e.CorrelateTrace("trace-abc", 5, map[string]float64{"db": 0.9})
	if err != nil {
		t.Fatalf("CorrelateTrace: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "observability" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceTraceEngine_BayesianNormalization(t *testing.T) {
	e := NewEvidenceTraceEngine()
	e.SetPrior("service-a", 0.1)
	e.SetPrior("service-b", 0.2)
	
	res, _ := e.CorrelateTrace("trace-xyz", 3, map[string]float64{
		"service-a": 0.8,
		"service-b": 0.6,
	})
	
	var sum float64
	for _, c := range res.RankedCauses {
		sum += c.Posterior
	}
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("posteriors must normalize to 1, got %.4f", sum)
	}
}

func TestEvidenceTraceEngine_RankByPosterior(t *testing.T) {
	e := NewEvidenceTraceEngine()
	e.SetPrior("frontend", 0.3)
	e.SetPrior("payments", 0.3)
	
	// payments has higher likelihood => must rank first
	res, _ := e.CorrelateTrace("trace-def", 7, map[string]float64{
		"payments": 0.95,
		"frontend": 0.2,
	})
	
	if len(res.RankedCauses) < 2 {
		t.Fatal("must have at least 2 ranked causes")
	}
	if res.RankedCauses[0].Component != "payments" {
		t.Errorf("expected payments first by posterior, got %s", res.RankedCauses[0].Component)
	}
}
