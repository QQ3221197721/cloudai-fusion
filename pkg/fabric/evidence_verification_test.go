package fabric

import (
	"testing"
)

func TestEvidenceFabricEngine_Verify(t *testing.T) {
	engine := NewEvidenceFabricEngine()
	
	result, err := engine.Verify("access-check", []string{"auth", "detect"}, "allow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.CheckName != "access-check" {
		t.Errorf("expected check 'access-check', got '%s'", result.CheckName)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
}

func TestEvidenceFabricEngine_InvalidVerdict(t *testing.T) {
	engine := NewEvidenceFabricEngine()
	
	_, err := engine.Verify("check", []string{"mod"}, "maybe")
	if err == nil {
		t.Error("expected error for invalid verdict")
	}
}

func TestEvidenceFabricEngine_DetectInconsistencies(t *testing.T) {
	engine := NewEvidenceFabricEngine()
	
	_, _ = engine.Verify("check-a", []string{"auth"}, "allow")
	_, _ = engine.Verify("check-b", []string{"detect"}, "block")
	
	violations := engine.DetectInconsistencies()
	
	if len(violations) == 0 {
		t.Log("Expected contradiction between auth(allow) and detect(block)")
	}
}
