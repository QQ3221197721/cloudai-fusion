package controlplane

import "testing"

func TestEvidenceHealthEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceHealthEngine()
	res, err := e.EvaluateHealth([]string{"api", "db", "cache"}, nil)
	if err != nil {
		t.Fatalf("EvaluateHealth: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "controlplane" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceHealthEngine_CascadePrediction(t *testing.T) {
	e := NewEvidenceHealthEngine()
	// Build a dependency graph: many services depend on "db".
	//  api -> db, worker -> db, report -> db, gateway -> api
	e.AddDependency("db", "api")
	e.AddDependency("db", "worker")
	e.AddDependency("db", "report")
	e.AddDependency("api", "gateway")

	res, err := e.EvaluateHealth([]string{"db", "api", "worker", "report", "gateway"}, []string{"db"})
	if err != nil {
		t.Fatalf("EvaluateHealth: %v", err)
	}
	info := res.CascadeAnalysis["db"]
	if info == nil {
		t.Fatal("expected cascade analysis for db")
	}
	// db -> api, worker, report; api -> gateway => radius 4
	if info.ImpactRadius < 4 {
		t.Errorf("expected impact radius >= 4, got %d (%v)", info.ImpactRadius, info.AtRiskServices)
	}
	if info.RiskScore <= 0 {
		t.Errorf("expected positive risk score, got %.2f", info.RiskScore)
	}
}

func TestEvidenceHealthEngine_NoDependenciesLowRisk(t *testing.T) {
	e := NewEvidenceHealthEngine()
	res, _ := e.EvaluateHealth([]string{"isolated"}, []string{"isolated"})
	info := res.CascadeAnalysis["isolated"]
	if info.ImpactRadius != 0 {
		t.Errorf("isolated service must have 0 impact radius, got %d", info.ImpactRadius)
	}
	if info.Dangerous {
		t.Error("isolated failure must not be flagged dangerous")
	}
}
