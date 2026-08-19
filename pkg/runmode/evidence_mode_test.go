package runmode

import (
	"testing"
)

func TestEvidenceRunmodeEngine_SwitchMode(t *testing.T) {
	engine := NewEvidenceRunmodeEngine()
	
	result, err := engine.SwitchMode("dev", "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.ToMode != "staging" {
		t.Errorf("expected toMode 'staging', got '%s'", result.ToMode)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
	
	if !result.Receipt.Verify(engine.privKey) {
		t.Error("receipt verification failed")
	}
}

func TestEvidenceRunmodeEngine_FidelityScoring(t *testing.T) {
	engine := NewEvidenceRunmodeEngine()
	
	err := engine.RecordFidelitySample("dev", 120.0, 0.02, 800)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	report := engine.ComputeFidelity("dev")
	
	if report.Mode != "dev" {
		t.Errorf("expected mode 'dev', got '%s'", report.Mode)
	}
}
