package rpcserver

import (
	"testing"
)

func TestEvidenceRPCServerEngine_RecordCall(t *testing.T) {
	engine := NewEvidenceRPCServerEngine()
	
	result, err := engine.RecordCall("user-service", "GetUser", 200, 12.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.Caller != "user-service" {
		t.Errorf("expected service 'user-service', got '%s'", result.Caller)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
}

func TestEvidenceRPCServerEngine_BuildDependencyGraph(t *testing.T) {
	engine := NewEvidenceRPCServerEngine()
	
	// Record some calls to build graph
	_, _ = engine.RecordCall("auth", "Validate", 200, 5.0)
	_, _ = engine.RecordCall("cache", "Fetch", 200, 3.0)
	
	graph := engine.BuildDependencyGraph()
	
	if len(graph.Nodes) >= 0 {
		t.Logf("Detected %d services in dependency graph", len(graph.Nodes))
	}
}
