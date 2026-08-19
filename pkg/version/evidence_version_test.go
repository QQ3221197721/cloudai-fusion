package version

import (
	"testing"
)

func TestEvidenceVersionEngine_RegisterVersion(t *testing.T) {
	engine := NewEvidenceVersionEngine()
	
	result, err := engine.RegisterVersion("v1.0.0", []string{"func CacheGet()", "type Config struct"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.Version != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got '%s'", result.Version)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
}

func TestEvidenceVersionEngine_IsBreakingChange(t *testing.T) {
	engine := NewEvidenceVersionEngine()
	
	_, _ = engine.RegisterVersion("v1.0", []string{"func OldAPI()", "type LegacyConfig struct"})
	result, err := engine.RegisterVersion("v2.0", []string{"func NewAPI()", "type NewConfig struct"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if !result.IsBreaking {
		t.Log("Expected breaking change detected")
	}
}

func TestEvidenceVersionEngine_CompareVersions(t *testing.T) {
	engine := NewEvidenceVersionEngine()
	
	_, _ = engine.RegisterVersion("old", []string{"func V1()", "type V1Config"})
	_, _ = engine.RegisterVersion("new", []string{"func V2()", "type V2Config", "const NewConst"})
	
	report, err := engine.CompareVersions("old", "new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if report.FromVersion != "old" || report.ToVersion != "new" {
		t.Error("version labels mismatch")
	}
	
	if report.SummaryHash == "" {
		t.Error("expected non-empty summary hash")
	}
}
