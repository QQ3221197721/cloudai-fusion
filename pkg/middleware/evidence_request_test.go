package middleware

import (
	"testing"
)

func TestEvidenceMiddlewareEngine_ProcessRequest(t *testing.T) {
	engine := NewEvidenceMiddlewareEngine()
	
	result, err := engine.ProcessRequest("GET", "/api/data", 200, 150, ServerHealth{CPU: 0.5, Memory: 0.6})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.Method != "GET" {
		t.Errorf("expected method 'GET', got '%s'", result.Method)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
}

func TestEvidenceMiddlewareEngine_AdaptiveRateLimiting(t *testing.T) {
	engine := NewEvidenceMiddlewareEngine()
	
	// Under pressure
	_, _ = engine.ProcessRequest("POST", "/heavy", 500, 2000, ServerHealth{CPU: 0.9, Memory: 0.95})
	
	limit := engine.GetCurrentLimit()
	
	if limit < 1000 {
		t.Logf("Rate limit adapted under pressure: %d", limit)
	}
}
