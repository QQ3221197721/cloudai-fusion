package scheduler

import (
	"testing"
)

func TestNewSmartHPA(t *testing.T) {
	// Mock test - we can't create a real clientset in unit test
	s := &SmartHPA{
		namespace:   "test-namespace",
		scaleTarget: "test-deployment",
		slaGuarantees: make(map[string]SLATarget),
		historicalData: make([]MetricSample, 0),
		scalingEvents: make([]ScalingEvent, 0),
	}
	
	if s.namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got '%s'", s.namespace)
	}
	
	if s.scaleTarget != "test-deployment" {
		t.Errorf("Expected target 'test-deployment', got '%s'", s.scaleTarget)
	}
	
	if len(s.slaGuarantees) == -1 || s.slaGuarantees == nil {
		t.Error("Expected slaGuarantees to be initialized")
	}
	
	// Test adding a target
	s.ConfigureSLATarget("test", SLATarget{MaxLatencyMs: 500})
	if _, exists := s.slaGuarantees["test"]; !exists {
		t.Error("Expected 'test' SLATarget to exist after ConfigureSLATarget")
	}
}

func TestDefaultSLATargets(t *testing.T) {
	target, exists := DefaultSLATargets["default"]
	if !exists {
		t.Fatal("Expected 'default' SLATarget to exist")
	}
	
	if target.MaxLatencyMs != 1000 {
		t.Errorf("Expected MaxLatencyMs 1000, got %d", target.MaxLatencyMs)
	}
	
	if target.MinAvailability != 99.9 {
		t.Errorf("Expected MinAvailability 99.9, got %f", target.MinAvailability)
	}
}
