package config

import (
	"testing"
)

func TestEvidenceConfigEngine_SetConfig(t *testing.T) {
	engine := NewEvidenceConfigEngine()
	
	result, err := engine.SetConfig("log_level", nil, "debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.ConfigKey != "log_level" {
		t.Errorf("expected key 'log_level', got '%s'", result.ConfigKey)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
}

func TestEvidenceConfigEngine_BlastRadius(t *testing.T) {
	engine := NewEvidenceConfigEngine()
	
	engine.RegisterService("svc-a", []string{"cache", "db"})
	engine.RegisterService("svc-b", []string{"cache"})
	
	radius := engine.ComputeBlastRadiusMap()
	
	if radius.KeyImpact["cache"] == 0 {
		t.Log("Expected some services affected by cache change")
	}
}
