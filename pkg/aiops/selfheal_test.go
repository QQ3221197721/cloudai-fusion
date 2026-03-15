package aiops

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// SelfHeal Config & Constructor
// ============================================================================

func TestDefaultSelfHealConfig(t *testing.T) {
	cfg := DefaultSelfHealConfig()
	if cfg.DetectionInterval != 15*time.Second {
		t.Errorf("unexpected DetectionInterval: %v", cfg.DetectionInterval)
	}
	if cfg.MaxAutoRemediations != 10 {
		t.Errorf("unexpected MaxAutoRemediations: %d", cfg.MaxAutoRemediations)
	}
	if !cfg.EnableAutoRemediate {
		t.Error("expected EnableAutoRemediate=true")
	}
	if cfg.DryRunMode {
		t.Error("expected DryRunMode=false")
	}
}

func TestNewSelfHealingEngine(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	if engine == nil {
		t.Fatal("engine should not be nil")
	}
	// Check default detectors registered
	if len(engine.detectors) < 5 {
		t.Errorf("expected at least 5 default detectors, got %d", len(engine.detectors))
	}
	// Check default playbooks registered
	if len(engine.playbooks) < 3 {
		t.Errorf("expected at least 3 default playbooks, got %d", len(engine.playbooks))
	}
	// Check default patterns
	if len(engine.rootCauseDB) < 3 {
		t.Errorf("expected at least 3 root cause patterns, got %d", len(engine.rootCauseDB))
	}
}

// ============================================================================
// Registration
// ============================================================================

func TestSelfHeal_RegisterDetector(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	before := len(engine.detectors)
	engine.RegisterDetector(&FaultDetector{
		ID: "custom-1", Name: "Custom Detector", Category: "custom", Severity: "medium", Enabled: true,
		Condition: FaultCondition{MetricName: "custom_metric", Operator: "gt", Threshold: 100},
	})
	if len(engine.detectors) != before+1 {
		t.Error("detector should be registered")
	}
}

func TestSelfHeal_RegisterPlaybook(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	before := len(engine.playbooks)
	engine.RegisterPlaybook(&RemediationPlaybook{
		ID: "pb-custom", Name: "Custom Playbook", Trigger: "custom",
		Steps: []RemediationStep{{Name: "step1", Action: "restart_pod", Timeout: time.Minute}},
	})
	if len(engine.playbooks) != before+1 {
		t.Error("playbook should be registered")
	}
}

func TestSelfHeal_RegisterHealthProbe(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	engine.RegisterHealthProbe(&HealthProbe{
		ID: "probe-1", Name: "API Health", Target: "http://localhost:8080/health",
		Type: "http", Interval: 10 * time.Second, Timeout: 5 * time.Second, FailThreshold: 3,
	})
	if _, ok := engine.healthProbes["probe-1"]; !ok {
		t.Error("probe should be registered")
	}
}

// ============================================================================
// Fault Detection
// ============================================================================

func TestSelfHeal_DetectFaults_CancelledContext(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := engine.DetectFaults(ctx, nil)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestSelfHeal_DetectFaults_NoMetrics(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	faults, err := engine.DetectFaults(context.Background(), map[string]float64{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(faults) != 0 {
		t.Errorf("expected 0 faults with no matching metrics, got %d", len(faults))
	}
}

func TestSelfHeal_DetectFaults_Triggered(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	metrics := map[string]float64{
		"node_cpu_percent":       98,   // > 95 → trigger node-cpu-high
		"node_memory_percent":    85,   // < 90 → no trigger
		"gpu_temperature_celsius": 95,  // > 90 → trigger gpu-temp-high
	}

	faults, err := engine.DetectFaults(context.Background(), metrics)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(faults) < 2 {
		t.Errorf("expected at least 2 faults (CPU + GPU temp), got %d", len(faults))
	}

	// Check correlation (multiple faults should be correlated)
	hasCorrelation := false
	for _, f := range faults {
		if len(f.Correlated) > 0 {
			hasCorrelation = true
			break
		}
	}
	if !hasCorrelation {
		t.Error("expected correlated faults")
	}
}

func TestSelfHeal_DetectFaults_AllOperators(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	// Test "lt" operator
	engine.RegisterDetector(&FaultDetector{
		ID: "test-lt", Name: "Low Value", Category: "test", Severity: "low", Enabled: true,
		Condition: FaultCondition{MetricName: "test_lt", Operator: "lt", Threshold: 10},
	})
	// Test "eq" operator
	engine.RegisterDetector(&FaultDetector{
		ID: "test-eq", Name: "Exact Value", Category: "test", Severity: "low", Enabled: true,
		Condition: FaultCondition{MetricName: "test_eq", Operator: "eq", Threshold: 0},
	})
	// Test "ne" operator
	engine.RegisterDetector(&FaultDetector{
		ID: "test-ne", Name: "Not Zero", Category: "test", Severity: "low", Enabled: true,
		Condition: FaultCondition{MetricName: "test_ne", Operator: "ne", Threshold: 0},
	})

	metrics := map[string]float64{
		"test_lt": 5,  // < 10 → trigger
		"test_eq": 0,  // == 0 → trigger
		"test_ne": 1,  // != 0 → trigger
	}

	faults, err := engine.DetectFaults(context.Background(), metrics)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundLT, foundEQ, foundNE := false, false, false
	for _, f := range faults {
		switch f.DetectorID {
		case "test-lt":
			foundLT = true
		case "test-eq":
			foundEQ = true
		case "test-ne":
			foundNE = true
		}
	}
	if !foundLT || !foundEQ || !foundNE {
		t.Errorf("expected all operators to trigger: lt=%v eq=%v ne=%v", foundLT, foundEQ, foundNE)
	}
}

func TestSelfHeal_DetectFaults_DisabledDetector(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	engine.RegisterDetector(&FaultDetector{
		ID: "disabled-1", Name: "Disabled", Category: "test", Severity: "high", Enabled: false,
		Condition: FaultCondition{MetricName: "always_fire", Operator: "gt", Threshold: 0},
	})

	faults, _ := engine.DetectFaults(context.Background(), map[string]float64{"always_fire": 100})
	for _, f := range faults {
		if f.DetectorID == "disabled-1" {
			t.Error("disabled detector should not trigger")
		}
	}
}

// ============================================================================
// Incident Creation
// ============================================================================

func TestSelfHeal_CreateIncident_Empty(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	inc := engine.CreateIncident(nil)
	if inc != nil {
		t.Error("expected nil for empty faults")
	}
}

func TestSelfHeal_CreateIncident(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	faults := []*FaultEvent{
		{ID: "f1", DetectorName: "CPU High", Category: "node", Severity: "high", Description: "CPU at 98%", Resource: "node-1"},
		{ID: "f2", DetectorName: "Memory High", Category: "node", Severity: "critical", Description: "Memory at 95%", Resource: "node-1"},
	}

	inc := engine.CreateIncident(faults)
	if inc == nil {
		t.Fatal("incident should not be nil")
	}
	if inc.Severity != "critical" {
		t.Errorf("expected critical severity (highest), got %s", inc.Severity)
	}
	if inc.Status != "open" {
		t.Errorf("expected open status, got %s", inc.Status)
	}
	if len(inc.FaultEvents) != 2 {
		t.Errorf("expected 2 fault events, got %d", len(inc.FaultEvents))
	}
	if len(inc.Timeline) != 1 {
		t.Errorf("expected 1 timeline event, got %d", len(inc.Timeline))
	}
}

// ============================================================================
// Remediation
// ============================================================================

func TestSelfHeal_Remediate_Disabled(t *testing.T) {
	cfg := DefaultSelfHealConfig()
	cfg.EnableAutoRemediate = false
	engine := NewSelfHealingEngine(cfg, nil)

	inc := &Incident{ID: "inc-1", Category: "pod"}
	_, err := engine.Remediate(context.Background(), inc)
	if err == nil {
		t.Error("expected error when auto-remediation disabled")
	}
}

func TestSelfHeal_Remediate_CancelledContext(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := engine.Remediate(ctx, &Incident{ID: "inc-1", Category: "pod"})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestSelfHeal_Remediate_NoPlaybook(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	inc := &Incident{ID: "inc-1", Category: "nonexistent"}
	_, err := engine.Remediate(context.Background(), inc)
	if err == nil {
		t.Error("expected error for missing playbook")
	}
}

func TestSelfHeal_Remediate_Success(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	faults := []*FaultEvent{
		{ID: "f1", Category: "pod", Severity: "high", Description: "Pod restart loop"},
	}
	inc := engine.CreateIncident(faults)

	result, err := engine.Remediate(context.Background(), inc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("remediation should succeed")
	}
	if inc.Status != "resolved" {
		t.Errorf("incident should be resolved, got %s", inc.Status)
	}
	if inc.ResolvedAt == nil {
		t.Error("resolved time should be set")
	}
	if len(result.StepResults) == 0 {
		t.Error("should have step results")
	}
}

func TestSelfHeal_Remediate_DryRun(t *testing.T) {
	cfg := DefaultSelfHealConfig()
	cfg.DryRunMode = true
	engine := NewSelfHealingEngine(cfg, nil)

	inc := engine.CreateIncident([]*FaultEvent{
		{ID: "f1", Category: "pod", Severity: "high", Description: "test"},
	})

	result, err := engine.Remediate(context.Background(), inc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.DryRun {
		t.Error("should be marked as dry run")
	}
	for _, step := range result.StepResults {
		if step.Output == "" {
			t.Error("dry run step should have output")
		}
	}
}

func TestSelfHeal_Remediate_MaxExecutions(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	// Exhaust max executions for pod playbook
	pb := engine.playbooks["pb-pod-restart"]
	pb.ExecutionCount = pb.MaxExecutions

	inc := engine.CreateIncident([]*FaultEvent{
		{ID: "f1", Category: "pod", Severity: "high"},
	})
	_, err := engine.Remediate(context.Background(), inc)
	if err == nil {
		t.Error("expected error when max executions exceeded")
	}
}

func TestSelfHeal_Remediate_Cooldown(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	pb := engine.playbooks["pb-pod-restart"]
	pb.LastExecution = time.Now()

	inc := engine.CreateIncident([]*FaultEvent{
		{ID: "f1", Category: "pod", Severity: "high"},
	})
	_, err := engine.Remediate(context.Background(), inc)
	if err == nil {
		t.Error("expected error during cooldown period")
	}
}

// ============================================================================
// Root Cause Analysis
// ============================================================================

func TestSelfHeal_AnalyzeRootCause_KnownPattern(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	inc := &Incident{
		ID:       "inc-1",
		Category: "pod",
		FaultEvents: []string{"f1", "f2"},
	}

	analysis := engine.AnalyzeRootCause(inc)
	if analysis == nil {
		t.Fatal("analysis should not be nil")
	}
	if analysis.ProbableCause == "" {
		t.Error("probable cause should not be empty")
	}
	if analysis.Confidence <= 0 {
		t.Error("confidence should be > 0 for matching pattern")
	}
	if analysis.RecommendedFix == "" {
		t.Error("recommended fix should not be empty")
	}
	if len(analysis.FaultTree) == 0 {
		t.Error("fault tree should not be empty")
	}
	if inc.RootCause == nil {
		t.Error("incident should have root cause set")
	}
}

func TestSelfHeal_AnalyzeRootCause_UnknownPattern(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	inc := &Incident{
		ID:       "inc-1",
		Category: "unknown_category",
		FaultEvents: []string{"f1"},
	}

	analysis := engine.AnalyzeRootCause(inc)
	if analysis.Confidence >= 0.5 {
		t.Errorf("unknown pattern should have low confidence, got %v", analysis.Confidence)
	}
}

// ============================================================================
// Incident & Health Summary
// ============================================================================

func TestSelfHeal_GetIncidents(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	// Create incidents
	engine.CreateIncident([]*FaultEvent{{ID: "f1", Category: "pod", Severity: "high"}})
	engine.CreateIncident([]*FaultEvent{{ID: "f2", Category: "node", Severity: "critical"}})

	all := engine.GetIncidents("", 10)
	if len(all) != 2 {
		t.Errorf("expected 2 incidents, got %d", len(all))
	}
	open := engine.GetIncidents("open", 10)
	if len(open) != 2 {
		t.Errorf("expected 2 open incidents, got %d", len(open))
	}
}

func TestSelfHeal_GetHealthSummary_Healthy(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	engine.RegisterHealthProbe(&HealthProbe{ID: "p1", LastResult: "success"})
	engine.RegisterHealthProbe(&HealthProbe{ID: "p2", LastResult: "success"})

	summary := engine.GetHealthSummary()
	if summary.OverallStatus != "healthy" {
		t.Errorf("expected healthy, got %s", summary.OverallStatus)
	}
	if summary.TotalProbes != 2 || summary.HealthyProbes != 2 {
		t.Errorf("expected 2/2 healthy probes, got %d/%d", summary.HealthyProbes, summary.TotalProbes)
	}
}

func TestSelfHeal_GetHealthSummary_WithIncidents(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	engine.CreateIncident([]*FaultEvent{{ID: "f1", Category: "node", Severity: "critical"}})
	engine.CreateIncident([]*FaultEvent{{ID: "f2", Category: "pod", Severity: "high"}})

	summary := engine.GetHealthSummary()
	if summary.OverallStatus != "critical" {
		t.Errorf("expected critical with critical incident, got %s", summary.OverallStatus)
	}
	if summary.OpenIncidents != 2 {
		t.Errorf("expected 2 open incidents, got %d", summary.OpenIncidents)
	}
	if summary.CriticalIncidents != 1 {
		t.Errorf("expected 1 critical incident, got %d", summary.CriticalIncidents)
	}
}

func TestSelfHeal_GetHealthSummary_MTTR(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	now := time.Now()
	resolved := now.Add(-1 * time.Minute)
	engine.incidents = append(engine.incidents, &Incident{
		ID: "inc-1", Status: "resolved",
		CreatedAt: now.Add(-11 * time.Minute), ResolvedAt: &resolved,
		TTR: 10 * time.Minute,
	})

	summary := engine.GetHealthSummary()
	if summary.MTTR != 10*time.Minute {
		t.Errorf("expected MTTR=10m, got %v", summary.MTTR)
	}
}

// ============================================================================
// MTTR Trend
// ============================================================================

func TestSelfHeal_GetMTTRTrend(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)

	now := time.Now()
	resolved := now.Add(-1 * time.Hour)
	engine.incidents = append(engine.incidents,
		&Incident{ID: "i1", Status: "resolved", CreatedAt: now.Add(-2 * time.Hour), ResolvedAt: &resolved, TTR: 5 * time.Minute},
		&Incident{ID: "i2", Status: "resolved", CreatedAt: now.Add(-3 * time.Hour), ResolvedAt: &resolved, TTR: 10 * time.Minute},
	)

	trend := engine.GetMTTRTrend(7)
	if len(trend) == 0 {
		t.Error("expected at least one MTTR data point")
	}
}

func TestSelfHeal_GetMTTRTrend_Empty(t *testing.T) {
	engine := NewSelfHealingEngine(DefaultSelfHealConfig(), nil)
	trend := engine.GetMTTRTrend(7)
	if len(trend) != 0 {
		t.Errorf("expected 0 data points, got %d", len(trend))
	}
}
