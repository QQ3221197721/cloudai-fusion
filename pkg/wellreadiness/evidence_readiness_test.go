package wellreadiness

import (
	"testing"
)

func TestEvidenceWellreadinessEngine_EvaluateReadiness(t *testing.T) {
	engine := NewEvidenceWellreadinessEngine()
	
	result, err := engine.EvaluateReadiness("cache", 0.95, "healthy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.Component != "cache" {
		t.Errorf("expected component 'cache', got '%s'", result.Component)
	}
	
	if result.Status != "ready" {
		t.Errorf("expected status 'ready', got '%s'", result.Status)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	} else if !result.Receipt.Verify() {
		t.Error("receipt verification failed")
	}
}

func TestEvidenceWellreadinessEngine_TrackMaturity(t *testing.T) {
	engine := NewEvidenceWellreadinessEngine()
	
	report := engine.TrackMaturity("database")
	
	if report.Component != "database" {
		t.Errorf("expected component 'database', got '%s'", report.Component)
	}
}

func TestEvidenceWellreadinessEngine_WindowManagement(t *testing.T) {
	engine := NewEvidenceWellreadinessEngine()
	
	for i := 0; i < 10; i++ {
		engine.EvaluateReadiness("api", float64(i)/10.0, "test")
	}
	
	report := engine.TrackMaturity("api")
	
	if report.MeanScore <= 0 {
		t.Errorf("expected positive mean score, got %f", report.MeanScore)
	}
}
