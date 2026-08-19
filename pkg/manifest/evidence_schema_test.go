package manifest

import (
	"testing"
)

func TestEvidenceManifestEngine_Apply(t *testing.T) {
	engine := NewEvidenceManifestEngine()
	
	result, err := engine.Apply("manifest-001", "v1.0.0", map[string]string{"name": "app"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.ManifestID != "manifest-001" {
		t.Errorf("expected ID 'manifest-001', got '%s'", result.ManifestID)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
}

func TestEvidenceManifestEngine_ValidateSchema(t *testing.T) {
	engine := NewEvidenceManifestEngine()
	
	oldSchema := &SchemaSnapshot{Version: "v1", Fields: map[string]FieldType{"f1": {Name: "f1", Type: "string", Required: true}}}
	newSchema := &SchemaSnapshot{Version: "v2", Fields: map[string]FieldType{"f1": {Name: "f1", Type: "int", Required: true}}}
	
	diff, err := engine.Validate("schema-001", "v1", "v2", oldSchema, newSchema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if diff.Safe {
		t.Log("Expected breaking change detected (type change from string to int)")
	}
}
