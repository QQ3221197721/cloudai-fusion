package aiops

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// Capacity Planner Config Tests
// ============================================================================

func TestDefaultCapacityPlanConfig(t *testing.T) {
	cfg := DefaultCapacityPlanConfig()
	if cfg.ForecastHorizon != 90 {
		t.Errorf("expected ForecastHorizon=90, got %d", cfg.ForecastHorizon)
	}
	if cfg.SafetyMargin != 0.2 {
		t.Errorf("expected SafetyMargin=0.2, got %v", cfg.SafetyMargin)
	}
	if cfg.CPUThreshold != 0.8 {
		t.Errorf("expected CPUThreshold=0.8, got %v", cfg.CPUThreshold)
	}
	if cfg.GPUThreshold != 0.75 {
		t.Errorf("expected GPUThreshold=0.75, got %v", cfg.GPUThreshold)
	}
}

func TestNewCapacityPlanner(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	if p == nil {
		t.Fatal("planner should not be nil")
	}
	if p.clusters == nil || p.resourceHistory == nil {
		t.Error("maps should be initialized")
	}
}

// ============================================================================
// Cluster Registration & Snapshot Ingestion
// ============================================================================

func TestCapacity_RegisterCluster(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	cluster := &ClusterCapacity{
		ClusterID: "cluster-1", ClusterName: "prod-east",
		NodeCount: 10, TotalCPU: 80000, TotalMemory: 320 * 1024 * 1024 * 1024,
		TotalGPU: 8, TotalStorage: 10 * 1024 * 1024 * 1024 * 1024,
		MaxNodes: 50, MaxPods: 1500,
	}
	p.RegisterCluster(cluster)
	if _, ok := p.clusters["cluster-1"]; !ok {
		t.Error("cluster should be registered")
	}
}

func TestCapacity_IngestSnapshot(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	snap := ResourceSnapshot{
		Timestamp: time.Now(), ClusterID: "c1",
		CPUCapacity: 80000, CPUUsed: 40000,
		MemCapacity: 320 * 1e9, MemUsed: 160 * 1e9,
		GPUCapacity: 8, GPUAllocated: 4,
		NodeCount: 10, PodCount: 200,
	}
	p.IngestSnapshot(snap)
	if len(p.resourceHistory["c1"]) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(p.resourceHistory["c1"]))
	}
}

func TestCapacity_IngestSnapshot_Trimming(t *testing.T) {
	cfg := DefaultCapacityPlanConfig()
	cfg.HistoryWindow = 1 * time.Hour
	p := NewCapacityPlanner(cfg, nil)

	p.IngestSnapshot(ResourceSnapshot{Timestamp: time.Now().Add(-2 * time.Hour), ClusterID: "c1", CPUCapacity: 100, CPUUsed: 50})
	p.IngestSnapshot(ResourceSnapshot{Timestamp: time.Now(), ClusterID: "c1", CPUCapacity: 100, CPUUsed: 60})

	if len(p.resourceHistory["c1"]) != 1 {
		t.Errorf("old snapshot should be trimmed, got %d", len(p.resourceHistory["c1"]))
	}
}

// ============================================================================
// Forecast Tests
// ============================================================================

func TestCapacity_ForecastDemand_InsufficientData(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	p.RegisterCluster(&ClusterCapacity{ClusterID: "c1"})

	_, err := p.ForecastDemand(context.Background(), "c1")
	if err == nil {
		t.Error("expected error for insufficient data")
	}
}

func TestCapacity_ForecastDemand_UnregisteredCluster(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	for i := 0; i < 10; i++ {
		p.IngestSnapshot(ResourceSnapshot{
			Timestamp: time.Now().Add(-time.Duration(i) * 24 * time.Hour), ClusterID: "c1",
			CPUCapacity: 80000, CPUUsed: 40000,
		})
	}

	_, err := p.ForecastDemand(context.Background(), "c1")
	if err == nil {
		t.Error("expected error for unregistered cluster")
	}
}

func TestCapacity_ForecastDemand_CancelledContext(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.ForecastDemand(ctx, "c1")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestCapacity_ForecastDemand_Success(t *testing.T) {
	cfg := DefaultCapacityPlanConfig()
	cfg.ForecastHorizon = 30
	p := NewCapacityPlanner(cfg, nil)

	p.RegisterCluster(&ClusterCapacity{
		ClusterID: "c1", ClusterName: "prod", NodeCount: 10,
		TotalCPU: 80000, TotalMemory: 320 * 1e9, TotalGPU: 8, TotalStorage: 10 * 1e12,
	})

	// Add snapshots with rising utilization
	for i := 0; i < 30; i++ {
		cpuUsed := int64(40000 + i*500) // rising CPU
		p.IngestSnapshot(ResourceSnapshot{
			Timestamp:       time.Now().Add(-time.Duration(30-i) * 24 * time.Hour),
			ClusterID:       "c1",
			CPUCapacity:     80000,
			CPUUsed:         cpuUsed,
			MemCapacity:     int64(320 * 1e9),
			MemUsed:         int64(160 * 1e9),
			GPUCapacity:     8,
			GPUAllocated:    4,
			StorageCapacity: int64(10 * 1e12),
			StorageUsed:     int64(5 * 1e12),
		})
	}

	forecast, err := p.ForecastDemand(context.Background(), "c1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if forecast.ClusterID != "c1" {
		t.Errorf("expected cluster c1, got %s", forecast.ClusterID)
	}
	if len(forecast.DataPoints) != 30 {
		t.Errorf("expected 30 data points, got %d", len(forecast.DataPoints))
	}
	if forecast.Confidence <= 0 || forecast.Confidence > 1 {
		t.Errorf("confidence should be in (0,1], got %v", forecast.Confidence)
	}
}

// ============================================================================
// Generate Plan Tests
// ============================================================================

func TestCapacity_GeneratePlan_NoData(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	_, err := p.GeneratePlan(context.Background(), "missing")
	if err == nil {
		t.Error("expected error for missing cluster")
	}
}

func TestCapacity_GeneratePlan_HighUtilization(t *testing.T) {
	cfg := DefaultCapacityPlanConfig()
	cfg.ForecastHorizon = 10
	p := NewCapacityPlanner(cfg, nil)

	p.RegisterCluster(&ClusterCapacity{
		ClusterID: "c1", ClusterName: "prod", NodeCount: 10,
		TotalCPU: 80000, TotalMemory: int64(320 * 1e9), TotalGPU: 8, TotalStorage: int64(10 * 1e12),
	})

	for i := 0; i < 10; i++ {
		p.IngestSnapshot(ResourceSnapshot{
			Timestamp:       time.Now().Add(-time.Duration(10-i) * 24 * time.Hour),
			ClusterID:       "c1",
			CPUCapacity:     80000,
			CPUUsed:         72000, // 90% utilization - above threshold
			MemCapacity:     int64(320 * 1e9),
			MemUsed:         int64(300 * 1e9), // ~94%
			GPUCapacity:     8,
			GPUAllocated:    7, // ~88%
			StorageCapacity: int64(10 * 1e12),
			StorageUsed:     int64(9 * 1e12), // 90%
			PendingPods:     5,
		})
	}

	plan, err := p.GeneratePlan(context.Background(), "c1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Recommendations) == 0 {
		t.Error("expected at least one recommendation for high utilization")
	}
	if plan.Priority == "" {
		t.Error("plan priority should be set")
	}
	if plan.EstimatedCost <= 0 {
		t.Errorf("estimated cost should be positive, got %v", plan.EstimatedCost)
	}
}

func TestCapacity_GetPlans(t *testing.T) {
	cfg := DefaultCapacityPlanConfig()
	cfg.ForecastHorizon = 10
	p := NewCapacityPlanner(cfg, nil)

	p.RegisterCluster(&ClusterCapacity{ClusterID: "c1", TotalCPU: 80000, TotalMemory: int64(320 * 1e9), TotalGPU: 8, TotalStorage: int64(10 * 1e12)})
	for i := 0; i < 10; i++ {
		p.IngestSnapshot(ResourceSnapshot{Timestamp: time.Now().Add(-time.Duration(10-i) * 24 * time.Hour), ClusterID: "c1", CPUCapacity: 80000, CPUUsed: 70000, MemCapacity: int64(320 * 1e9), MemUsed: int64(300 * 1e9), GPUCapacity: 8, GPUAllocated: 7, StorageCapacity: int64(10 * 1e12), StorageUsed: int64(9 * 1e12)})
	}
	p.GeneratePlan(context.Background(), "c1")

	plans := p.GetPlans("c1")
	if len(plans) == 0 {
		t.Error("expected plans for c1")
	}
	allPlans := p.GetPlans("")
	if len(allPlans) != len(plans) {
		t.Error("empty filter should return all plans")
	}
}

// ============================================================================
// Capacity Overview Tests
// ============================================================================

func TestCapacity_GetCapacityOverview(t *testing.T) {
	p := NewCapacityPlanner(DefaultCapacityPlanConfig(), nil)
	p.RegisterCluster(&ClusterCapacity{
		ClusterID: "c1", ClusterName: "prod", NodeCount: 10, TotalGPU: 8,
		CPUUtilization: 0.5, MemUtilization: 0.6, GPUUtilization: 0.3, StorageUtilization: 0.4,
	})
	p.RegisterCluster(&ClusterCapacity{
		ClusterID: "c2", ClusterName: "staging", NodeCount: 5, TotalGPU: 4,
		CPUUtilization: 0.95, MemUtilization: 0.92, GPUUtilization: 0.9, StorageUtilization: 0.88,
	})

	overview := p.GetCapacityOverview()
	if overview.TotalNodes != 15 {
		t.Errorf("expected 15 nodes, got %d", overview.TotalNodes)
	}
	if overview.TotalGPU != 12 {
		t.Errorf("expected 12 GPU, got %d", overview.TotalGPU)
	}

	c1 := overview.Clusters["c1"]
	if c1.Status != "healthy" {
		t.Errorf("c1 should be healthy, got %s", c1.Status)
	}
	c2 := overview.Clusters["c2"]
	if c2.Status != "critical" {
		t.Errorf("c2 should be critical, got %s", c2.Status)
	}
}

// ============================================================================
// Helper Functions Tests
// ============================================================================

func TestLinearTrend(t *testing.T) {
	if linearTrend([]float64{1}) != 0 {
		t.Error("single value should have 0 trend")
	}
	if linearTrend([]float64{1, 2, 3, 4, 5}) <= 0 {
		t.Error("ascending values should have positive trend")
	}
	if linearTrend([]float64{5, 4, 3, 2, 1}) >= 0 {
		t.Error("descending values should have negative trend")
	}
}

func TestEMA(t *testing.T) {
	if ema(nil, 0.3) != 0 {
		t.Error("empty should return 0")
	}
	if ema([]float64{10}, 0.3) != 10 {
		t.Error("single value should return itself")
	}
	result := ema([]float64{1, 2, 3, 4, 5}, 0.3)
	if result <= 0 || result > 5 {
		t.Errorf("unexpected EMA: %v", result)
	}
}

func TestStdDev(t *testing.T) {
	if stdDev([]float64{1}) != 0 {
		t.Error("single value should have 0 stddev")
	}
	if stdDev([]float64{5, 5, 5}) != 0 {
		t.Error("uniform values should have 0 stddev")
	}
	sd := stdDev([]float64{1, 2, 3, 4, 5})
	if sd <= 0 {
		t.Errorf("expected positive stddev, got %v", sd)
	}
}

func TestClampFloat(t *testing.T) {
	if clampFloat(0.5, 0, 1) != 0.5 {
		t.Error("0.5 in [0,1] should be 0.5")
	}
	if clampFloat(-0.5, 0, 1) != 0 {
		t.Error("-0.5 clamped to [0,1] should be 0")
	}
	if clampFloat(1.5, 0, 1) != 1 {
		t.Error("1.5 clamped to [0,1] should be 1")
	}
}

func TestDetermineUrgency(t *testing.T) {
	if determineUrgency(0.99, 0.8) != "immediate" {
		t.Error("1.24x threshold should be immediate")
	}
	if determineUrgency(0.89, 0.8) != "7days" {
		t.Error("1.11x threshold should be 7days")
	}
	if determineUrgency(0.81, 0.8) != "30days" {
		t.Error("1.01x threshold should be 30days")
	}
	if determineUrgency(0.7, 0.8) != "90days" {
		t.Error("below threshold should be 90days")
	}
}

func TestForecastUrgency(t *testing.T) {
	if forecastUrgency(5, 14) != "immediate" {
		t.Error("5 days with 14-day lead should be immediate")
	}
	if forecastUrgency(10, 14) != "7days" {
		t.Error("10 days with 14-day lead should be 7days")
	}
	if forecastUrgency(20, 14) != "30days" {
		t.Error("20 days with 14-day lead should be 30days")
	}
	if forecastUrgency(100, 14) != "90days" {
		t.Error("100 days should be 90days")
	}
}

func TestDeterminePlanPriority(t *testing.T) {
	if determinePlanPriority([]CapacityRecommendation{{Urgency: "immediate"}}) != "critical" {
		t.Error("immediate urgency should be critical priority")
	}
	if determinePlanPriority([]CapacityRecommendation{{Urgency: "7days"}}) != "high" {
		t.Error("7days urgency should be high priority")
	}
	if determinePlanPriority([]CapacityRecommendation{{Urgency: "30days"}}) != "medium" {
		t.Error("30days urgency should be medium priority")
	}
	if determinePlanPriority([]CapacityRecommendation{{Urgency: "90days"}}) != "low" {
		t.Error("90days urgency should be low priority")
	}
}

func TestEstimateNodeCost(t *testing.T) {
	tests := []struct {
		resourceType string
		count        int
		wantMin      float64
	}{
		{"cpu", 2, 200},
		{"memory", 1, 100},
		{"gpu", 1, 1000},
		{"storage", 3, 100},
		{"compute", 1, 100},
		{"unknown", 1, 0},
	}
	for _, tt := range tests {
		cost := estimateNodeCost(CapacityRecommendation{ResourceType: tt.resourceType, Count: tt.count})
		if cost < tt.wantMin {
			t.Errorf("estimateNodeCost(%s, %d) = %v, want >= %v", tt.resourceType, tt.count, cost, tt.wantMin)
		}
	}
}
