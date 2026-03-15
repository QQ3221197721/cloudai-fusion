package aiops

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// Config & Constructor Tests
// ============================================================================

func TestDefaultAutoscaleConfig(t *testing.T) {
	cfg := DefaultAutoscaleConfig()
	if cfg.EvaluationInterval != 30*time.Second {
		t.Errorf("unexpected EvaluationInterval: %v", cfg.EvaluationInterval)
	}
	if cfg.ScaleUpMaxStep != 10 {
		t.Errorf("unexpected ScaleUpMaxStep: %d", cfg.ScaleUpMaxStep)
	}
	if !cfg.EnablePredictive {
		t.Error("expected EnablePredictive=true")
	}
}

func TestNewPredictiveAutoscaler(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	if a == nil {
		t.Fatal("autoscaler should not be nil")
	}
	if a.workloads == nil || a.metricsDB == nil {
		t.Error("maps should be initialized")
	}
}

// ============================================================================
// Workload Registration
// ============================================================================

func TestRegisterWorkload(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	w := &ScalableWorkload{
		ID: "wl-1", Name: "api-server", Namespace: "default", Kind: "Deployment",
		CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 20,
		TargetMetrics: []TargetMetric{
			{Name: "cpu", Type: "utilization", TargetValue: 70, Weight: 0.6, ScaleUpAt: 80, ScaleDownAt: 30},
		},
	}
	a.RegisterWorkload(w)
	if _, ok := a.workloads["wl-1"]; !ok {
		t.Error("workload should be registered")
	}
}

func TestSetScalingRule(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	rule := &ScalingRule{WorkloadID: "wl-1"}
	a.SetScalingRule(rule)
	if _, ok := a.scalingRules["wl-1"]; !ok {
		t.Error("rule should be set")
	}
}

// ============================================================================
// Metrics Ingestion
// ============================================================================

func TestIngestMetrics(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	samples := []MetricSample{
		{Timestamp: time.Now(), WorkloadID: "wl-1", MetricName: "cpu", Value: 75, Replicas: 3},
		{Timestamp: time.Now(), WorkloadID: "wl-1", MetricName: "cpu", Value: 80, Replicas: 3},
	}
	a.IngestMetrics(samples)
	if len(a.metricsDB["wl-1"]) != 2 {
		t.Errorf("expected 2 samples, got %d", len(a.metricsDB["wl-1"]))
	}
}

func TestIngestMetrics_Trimming(t *testing.T) {
	cfg := DefaultAutoscaleConfig()
	cfg.MetricHistoryWindow = 1 * time.Hour
	a := NewPredictiveAutoscaler(cfg, nil)

	a.IngestMetrics([]MetricSample{
		{Timestamp: time.Now().Add(-2 * time.Hour), WorkloadID: "wl-1", MetricName: "cpu", Value: 50},
		{Timestamp: time.Now(), WorkloadID: "wl-1", MetricName: "cpu", Value: 70},
	})
	if len(a.metricsDB["wl-1"]) != 1 {
		t.Errorf("old sample should be trimmed, got %d samples", len(a.metricsDB["wl-1"]))
	}
}

// ============================================================================
// Evaluate Tests
// ============================================================================

func TestEvaluate_UnregisteredWorkload(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	_, err := a.Evaluate(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unregistered workload")
	}
}

func TestEvaluate_CancelledContext(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Evaluate(ctx, "wl-1")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestEvaluate_NoMetrics(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	a.RegisterWorkload(&ScalableWorkload{
		ID: "wl-1", Name: "test", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 20,
		TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 70, Weight: 1.0}},
	})
	d, err := a.Evaluate(context.Background(), "wl-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Direction != "none" {
		t.Errorf("expected direction=none with no metrics, got %s", d.Direction)
	}
}

func TestEvaluate_ScaleUp(t *testing.T) {
	cfg := DefaultAutoscaleConfig()
	cfg.Tolerance = 0.1
	a := NewPredictiveAutoscaler(cfg, nil)

	a.RegisterWorkload(&ScalableWorkload{
		ID: "wl-1", Name: "test", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 50,
		TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 50, Weight: 1.0}},
	})

	// Ingest high CPU samples
	now := time.Now()
	for i := 0; i < 10; i++ {
		a.IngestMetrics([]MetricSample{
			{Timestamp: now.Add(-time.Duration(i) * 10 * time.Second), WorkloadID: "wl-1", MetricName: "cpu", Value: 90, Replicas: 3},
		})
	}

	d, err := a.Evaluate(context.Background(), "wl-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Direction != "up" {
		t.Errorf("expected scale up, got direction=%s (desired=%d)", d.Direction, d.DesiredReplicas)
	}
	if d.DesiredReplicas <= 3 {
		t.Errorf("desired replicas should be > 3, got %d", d.DesiredReplicas)
	}
}

func TestEvaluate_ScaleDown(t *testing.T) {
	cfg := DefaultAutoscaleConfig()
	cfg.Tolerance = 0.1
	cfg.ScaleDownCooldown = 0
	a := NewPredictiveAutoscaler(cfg, nil)

	a.RegisterWorkload(&ScalableWorkload{
		ID: "wl-1", Name: "test", CurrentReplicas: 10, MinReplicas: 1, MaxReplicas: 50,
		TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 70, Weight: 1.0}},
	})

	now := time.Now()
	for i := 0; i < 10; i++ {
		a.IngestMetrics([]MetricSample{
			{Timestamp: now.Add(-time.Duration(i) * 10 * time.Second), WorkloadID: "wl-1", MetricName: "cpu", Value: 10, Replicas: 10},
		})
	}

	d, err := a.Evaluate(context.Background(), "wl-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Direction != "down" {
		t.Errorf("expected scale down, got direction=%s", d.Direction)
	}
}

func TestEvaluate_CooldownBlocks(t *testing.T) {
	cfg := DefaultAutoscaleConfig()
	cfg.ScaleUpCooldown = 10 * time.Minute
	a := NewPredictiveAutoscaler(cfg, nil)

	a.RegisterWorkload(&ScalableWorkload{
		ID: "wl-1", Name: "test", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 50,
		LastScaleTime: time.Now(), LastScaleDir: "up",
		TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 50, Weight: 1.0}},
	})

	now := time.Now()
	for i := 0; i < 10; i++ {
		a.IngestMetrics([]MetricSample{
			{Timestamp: now.Add(-time.Duration(i) * 10 * time.Second), WorkloadID: "wl-1", MetricName: "cpu", Value: 95, Replicas: 3},
		})
	}

	d, err := a.Evaluate(context.Background(), "wl-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Blocked {
		t.Error("should be blocked by cooldown")
	}
	if d.Direction != "none" {
		t.Errorf("blocked decision direction should be none, got %s", d.Direction)
	}
}

func TestEvaluate_RespectsMaxReplicas(t *testing.T) {
	cfg := DefaultAutoscaleConfig()
	cfg.ScaleUpMaxStep = 100
	a := NewPredictiveAutoscaler(cfg, nil)

	a.RegisterWorkload(&ScalableWorkload{
		ID: "wl-1", Name: "test", CurrentReplicas: 5, MinReplicas: 1, MaxReplicas: 8,
		TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 50, Weight: 1.0}},
	})

	now := time.Now()
	for i := 0; i < 10; i++ {
		a.IngestMetrics([]MetricSample{
			{Timestamp: now.Add(-time.Duration(i) * 10 * time.Second), WorkloadID: "wl-1", MetricName: "cpu", Value: 200, Replicas: 5},
		})
	}

	d, _ := a.Evaluate(context.Background(), "wl-1")
	if d.DesiredReplicas > 8 {
		t.Errorf("should not exceed MaxReplicas=8, got %d", d.DesiredReplicas)
	}
}

// ============================================================================
// ApplyDecision Tests
// ============================================================================

func TestApplyDecision_NoneDirection(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	err := a.ApplyDecision(&ScalingDecision{Direction: "none"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestApplyDecision_Blocked(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	err := a.ApplyDecision(&ScalingDecision{Direction: "up", Blocked: true})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestApplyDecision_NotFound(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	err := a.ApplyDecision(&ScalingDecision{WorkloadID: "missing", Direction: "up"})
	if err == nil {
		t.Error("expected error for missing workload")
	}
}

func TestApplyDecision_Success(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	a.RegisterWorkload(&ScalableWorkload{
		ID: "wl-1", Name: "test", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 20,
	})

	err := a.ApplyDecision(&ScalingDecision{
		WorkloadID:      "wl-1",
		CurrentReplicas: 3,
		DesiredReplicas: 5,
		Direction:       "up",
		Reason:          "test scale up",
		MetricValues:    map[string]float64{"cpu": 90},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.workloads["wl-1"].CurrentReplicas != 5 {
		t.Errorf("replicas should be 5, got %d", a.workloads["wl-1"].CurrentReplicas)
	}
	if len(a.history) != 1 {
		t.Errorf("expected 1 event in history, got %d", len(a.history))
	}
}

// ============================================================================
// EvaluateAll & GetHistory Tests
// ============================================================================

func TestEvaluateAll(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	a.RegisterWorkload(&ScalableWorkload{ID: "wl-1", Name: "a", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 20, TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 70, Weight: 1.0}}})
	a.RegisterWorkload(&ScalableWorkload{ID: "wl-2", Name: "b", CurrentReplicas: 5, MinReplicas: 1, MaxReplicas: 20, TargetMetrics: []TargetMetric{{Name: "cpu", TargetValue: 70, Weight: 1.0}}})

	decisions, err := a.EvaluateAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(decisions))
	}
}

func TestGetHistory(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	a.RegisterWorkload(&ScalableWorkload{ID: "wl-1", Name: "test", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 20})

	for i := 0; i < 5; i++ {
		a.ApplyDecision(&ScalingDecision{WorkloadID: "wl-1", Direction: "up", DesiredReplicas: 3 + i + 1, MetricValues: map[string]float64{}})
	}

	events := a.GetHistory("wl-1", 3)
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	allEvents := a.GetHistory("", 10)
	if len(allEvents) != 5 {
		t.Errorf("expected 5 total events, got %d", len(allEvents))
	}
}

// ============================================================================
// GetScalingSummary & Helpers
// ============================================================================

func TestGetScalingSummary(t *testing.T) {
	a := NewPredictiveAutoscaler(DefaultAutoscaleConfig(), nil)
	a.RegisterWorkload(&ScalableWorkload{ID: "wl-1", Name: "test", CurrentReplicas: 3, MinReplicas: 1, MaxReplicas: 20})

	a.ApplyDecision(&ScalingDecision{WorkloadID: "wl-1", Direction: "up", DesiredReplicas: 5, Predicted: true, MetricValues: map[string]float64{}})
	a.ApplyDecision(&ScalingDecision{WorkloadID: "wl-1", Direction: "down", DesiredReplicas: 3, MetricValues: map[string]float64{}})

	summary := a.GetScalingSummary()
	if summary.WorkloadCount != 1 {
		t.Errorf("expected 1 workload, got %d", summary.WorkloadCount)
	}
	if summary.TotalEvents != 2 {
		t.Errorf("expected 2 events, got %d", summary.TotalEvents)
	}
	if summary.ScaleUpCount != 1 || summary.ScaleDownCount != 1 {
		t.Errorf("expected 1 up + 1 down, got %d up + %d down", summary.ScaleUpCount, summary.ScaleDownCount)
	}
	if summary.PredictiveCount != 1 {
		t.Errorf("expected 1 predictive, got %d", summary.PredictiveCount)
	}
}

func TestClampInt(t *testing.T) {
	if clampInt(5, 1, 10) != 5 {
		t.Error("5 in [1,10] should be 5")
	}
	if clampInt(-1, 1, 10) != 1 {
		t.Error("-1 clamped to [1,10] should be 1")
	}
	if clampInt(100, 1, 10) != 10 {
		t.Error("100 clamped to [1,10] should be 10")
	}
}

func TestSortScalingEventsByTime(t *testing.T) {
	events := []ScalingEvent{
		{Timestamp: time.Now().Add(-2 * time.Hour)},
		{Timestamp: time.Now()},
		{Timestamp: time.Now().Add(-1 * time.Hour)},
	}
	SortScalingEventsByTime(events)
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.After(events[i-1].Timestamp) {
			t.Error("should be sorted descending by timestamp")
		}
	}
}
