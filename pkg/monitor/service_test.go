package monitor

import (
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	svc, err := NewService(ServiceConfig{
		MetricsPort: 0,
	})
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestDefaultAlertRules(t *testing.T) {
	svc, _ := NewService(ServiceConfig{})
	rules := svc.GetAlertRules()
	if len(rules) != 5 {
		t.Fatalf("expected 5 default alert rules, got %d", len(rules))
	}

	ids := map[string]bool{}
	for _, r := range rules {
		ids[r.ID] = true
	}
	expected := []string{
		"gpu-high-util", "gpu-memory-exhaustion",
		"cluster-unreachable", "scheduling-queue-buildup", "cost-spike",
	}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("missing alert rule: %s", id)
		}
	}
}

func TestAlertRuleSeverities(t *testing.T) {
	svc, _ := NewService(ServiceConfig{})
	rules := svc.GetAlertRules()

	criticalCount := 0
	warningCount := 0
	for _, r := range rules {
		switch r.Severity {
		case AlertSeverityCritical:
			criticalCount++
		case AlertSeverityWarning:
			warningCount++
		}
	}
	if criticalCount != 2 {
		t.Errorf("expected 2 critical rules, got %d", criticalCount)
	}
	if warningCount != 3 {
		t.Errorf("expected 3 warning rules, got %d", warningCount)
	}
}

func TestAlertRuleProperties(t *testing.T) {
	svc, _ := NewService(ServiceConfig{})
	rules := svc.GetAlertRules()

	// Find GPU high utilization rule
	var gpuRule *AlertRule
	for _, r := range rules {
		if r.ID == "gpu-high-util" {
			gpuRule = r
			break
		}
	}
	if gpuRule == nil {
		t.Fatal("gpu-high-util rule not found")
	}
	if gpuRule.Threshold != 95 {
		t.Errorf("expected threshold 95, got %f", gpuRule.Threshold)
	}
	if gpuRule.Duration != 5*time.Minute {
		t.Errorf("expected 5m duration, got %v", gpuRule.Duration)
	}
	if gpuRule.Status != "active" {
		t.Errorf("expected active, got %q", gpuRule.Status)
	}
}

func TestGetRecentEvents_Empty(t *testing.T) {
	svc, _ := NewService(ServiceConfig{})
	events := svc.GetRecentEvents(10)
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestGetRecentEvents_LimitExceedsActual(t *testing.T) {
	svc, _ := NewService(ServiceConfig{})
	// No events added, requesting 100 should return 0 safely
	events := svc.GetRecentEvents(100)
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestAlertSeverityConstants(t *testing.T) {
	if string(AlertSeverityCritical) != "critical" {
		t.Error("AlertSeverityCritical mismatch")
	}
	if string(AlertSeverityWarning) != "warning" {
		t.Error("AlertSeverityWarning mismatch")
	}
	if string(AlertSeverityInfo) != "info" {
		t.Error("AlertSeverityInfo mismatch")
	}
}

func TestAlertEventStruct(t *testing.T) {
	now := time.Now()
	event := AlertEvent{
		ID:       "evt-1",
		RuleID:   "gpu-high-util",
		RuleName: "GPU High Utilization",
		Severity: AlertSeverityWarning,
		Message:  "GPU utilization above 95%",
		Status:   "firing",
		FiredAt:  now,
	}
	if event.ID != "evt-1" {
		t.Error("ID mismatch")
	}
	if event.Status != "firing" {
		t.Error("Status mismatch")
	}
	if event.ResolvedAt != nil {
		t.Error("ResolvedAt should be nil")
	}
}

func TestRecordAPIRequest(t *testing.T) {
	// Should not panic
	RecordAPIRequest("GET", "/healthz", "200", 50*time.Millisecond)
	RecordAPIRequest("POST", "/api/v1/clusters", "201", 120*time.Millisecond)
}

func TestUpdateGPUMetrics(t *testing.T) {
	// Should not panic
	UpdateGPUMetrics("cluster-1", "node-1", "0", "nvidia-a100", 85.0, 34359738368, 72, 280.0)
}
