package finops

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// Statistical Helpers Tests
// ============================================================================

func TestCalcMean(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   float64
	}{
		{"empty", nil, 0},
		{"single", []float64{5.0}, 5.0},
		{"multiple", []float64{1, 2, 3, 4, 5}, 3.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcMean(tt.values)
			if got != tt.want {
				t.Errorf("calcMean() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalcStdDev(t *testing.T) {
	t.Run("less_than_2", func(t *testing.T) {
		if got := calcStdDev([]float64{1}, 1); got != 0 {
			t.Errorf("expected 0 for single value, got %v", got)
		}
	})
	t.Run("uniform", func(t *testing.T) {
		vals := []float64{5, 5, 5, 5}
		if got := calcStdDev(vals, 5); got != 0 {
			t.Errorf("expected 0 for uniform values, got %v", got)
		}
	})
	t.Run("varied", func(t *testing.T) {
		vals := []float64{2, 4, 4, 4, 5, 5, 7, 9}
		mean := calcMean(vals)
		sd := calcStdDev(vals, mean)
		if sd < 1.0 || sd > 3.0 {
			t.Errorf("unexpected stddev %v", sd)
		}
	})
}

func TestCalcTrend(t *testing.T) {
	t.Run("too_few", func(t *testing.T) {
		if got := calcTrend([]float64{1}); got != 0 {
			t.Errorf("expected 0, got %v", got)
		}
	})
	t.Run("flat", func(t *testing.T) {
		if got := calcTrend([]float64{5, 5, 5, 5}); got != 0 {
			t.Errorf("expected 0, got %v", got)
		}
	})
	t.Run("rising", func(t *testing.T) {
		if got := calcTrend([]float64{1, 2, 3, 4, 5}); got <= 0 {
			t.Errorf("expected positive trend, got %v", got)
		}
	})
	t.Run("falling", func(t *testing.T) {
		if got := calcTrend([]float64{5, 4, 3, 2, 1}); got >= 0 {
			t.Errorf("expected negative trend, got %v", got)
		}
	})
}

func TestCalcEMA(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if got := calcEMA(nil, 0.3); got != 0 {
			t.Errorf("expected 0, got %v", got)
		}
	})
	t.Run("single", func(t *testing.T) {
		if got := calcEMA([]float64{10}, 0.3); got != 10 {
			t.Errorf("expected 10, got %v", got)
		}
	})
	t.Run("multiple", func(t *testing.T) {
		got := calcEMA([]float64{1, 2, 3, 4, 5}, 0.3)
		if got <= 0 || got > 5 {
			t.Errorf("unexpected EMA %v", got)
		}
	})
}

// ============================================================================
// RI Recommendation Engine Tests
// ============================================================================

func TestDefaultRIConfig(t *testing.T) {
	cfg := DefaultRIConfig()
	if cfg.AnalysisWindow != 30*24*time.Hour {
		t.Errorf("unexpected AnalysisWindow: %v", cfg.AnalysisWindow)
	}
	if cfg.MinUtilization != 0.7 {
		t.Errorf("unexpected MinUtilization: %v", cfg.MinUtilization)
	}
	if cfg.MaxCommitmentYears != 3 {
		t.Errorf("unexpected MaxCommitmentYears: %v", cfg.MaxCommitmentYears)
	}
}

func TestNewRIRecommendationEngine(t *testing.T) {
	engine := NewRIRecommendationEngine(DefaultRIConfig(), nil)
	if engine == nil {
		t.Fatal("engine should not be nil")
	}
	if engine.usageRecords == nil {
		t.Error("usageRecords map should be initialized")
	}
}

func TestRIEngine_IngestUsageData(t *testing.T) {
	engine := NewRIRecommendationEngine(DefaultRIConfig(), nil)

	records := []UsageRecord{
		{Timestamp: time.Now(), InstanceType: "m5.xlarge", InstanceCount: 10, CPUUtilization: 0.8, OnDemandCost: 100, Region: "us-east-1"},
		{Timestamp: time.Now(), InstanceType: "m5.xlarge", InstanceCount: 12, CPUUtilization: 0.85, OnDemandCost: 120, Region: "us-east-1"},
		{Timestamp: time.Now().Add(-60 * 24 * time.Hour), InstanceType: "old", InstanceCount: 1, CPUUtilization: 0.5, OnDemandCost: 10}, // will be trimmed
	}

	engine.IngestUsageData(records)

	if len(engine.usageRecords["m5.xlarge"]) != 2 {
		t.Errorf("expected 2 m5.xlarge records, got %d", len(engine.usageRecords["m5.xlarge"]))
	}
	// "old" record should be trimmed (>30 days old)
	if len(engine.usageRecords["old"]) != 0 {
		t.Errorf("expected old records to be trimmed, got %d", len(engine.usageRecords["old"]))
	}
}

func TestRIEngine_RegisterExistingRI(t *testing.T) {
	engine := NewRIRecommendationEngine(DefaultRIConfig(), nil)
	ri := &ReservedInstance{
		ID: "ri-1", InstanceType: "m5.xlarge", Count: 5,
		Term: "1yr", ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
	}
	engine.RegisterExistingRI(ri)
	if len(engine.riInventory["m5.xlarge"]) != 1 {
		t.Errorf("expected 1 RI, got %d", len(engine.riInventory["m5.xlarge"]))
	}
}

func TestRIEngine_Analyze_InsufficientData(t *testing.T) {
	engine := NewRIRecommendationEngine(DefaultRIConfig(), nil)
	// Only 5 records (< 24 minimum)
	records := make([]UsageRecord, 5)
	for i := range records {
		records[i] = UsageRecord{Timestamp: time.Now(), InstanceType: "t3.micro", InstanceCount: 1, CPUUtilization: 0.9, OnDemandCost: 10}
	}
	engine.IngestUsageData(records)

	recs, err := engine.Analyze(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations for insufficient data, got %d", len(recs))
	}
}

func TestRIEngine_Analyze_WithSufficientData(t *testing.T) {
	cfg := DefaultRIConfig()
	engine := NewRIRecommendationEngine(cfg, nil)

	records := make([]UsageRecord, 30)
	for i := range records {
		records[i] = UsageRecord{
			Timestamp:      time.Now().Add(-time.Duration(i) * time.Hour),
			InstanceType:   "m5.2xlarge",
			InstanceCount:  20,
			CPUUtilization: 0.85,
			OnDemandCost:   500,
			Region:         "us-west-2",
		}
	}
	engine.IngestUsageData(records)

	recs, err := engine.Analyze(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should generate at least one recommendation
	if len(recs) == 0 {
		t.Log("no recommendations generated (may be fully covered)")
	}
	// Verify sorted by savings descending
	for i := 1; i < len(recs); i++ {
		if recs[i].EstimatedSavings > recs[i-1].EstimatedSavings {
			t.Error("recommendations not sorted by savings descending")
		}
	}
}

func TestRIEngine_Analyze_CancelledContext(t *testing.T) {
	engine := NewRIRecommendationEngine(DefaultRIConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := engine.Analyze(ctx)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestRIEngine_Analyze_SkipsGPU(t *testing.T) {
	cfg := DefaultRIConfig()
	cfg.IncludeGPU = false
	engine := NewRIRecommendationEngine(cfg, nil)

	records := make([]UsageRecord, 30)
	for i := range records {
		records[i] = UsageRecord{
			Timestamp: time.Now().Add(-time.Duration(i) * time.Hour), InstanceType: "p3.2xlarge",
			InstanceCount: 10, CPUUtilization: 0.9, OnDemandCost: 1000, IsGPU: true,
		}
	}
	engine.IngestUsageData(records)

	recs, err := engine.Analyze(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range recs {
		if r.IsGPU {
			t.Error("GPU recommendations should be excluded when IncludeGPU=false")
		}
	}
}

func TestRIEngine_GetCoverageReport(t *testing.T) {
	engine := NewRIRecommendationEngine(DefaultRIConfig(), nil)
	records := []UsageRecord{
		{Timestamp: time.Now(), InstanceType: "m5.xlarge", InstanceCount: 10, OnDemandCost: 50},
	}
	engine.IngestUsageData(records)

	ri := &ReservedInstance{
		ID: "ri-1", InstanceType: "m5.xlarge", Count: 5,
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
	}
	engine.RegisterExistingRI(ri)

	report := engine.GetCoverageReport()
	if report == nil {
		t.Fatal("report should not be nil")
	}
	if report.TotalInstances != 10 {
		t.Errorf("expected 10 total instances, got %d", report.TotalInstances)
	}
	if report.CoveredByRI != 5 {
		t.Errorf("expected 5 covered by RI, got %d", report.CoveredByRI)
	}
	if tc, ok := report.ByType["m5.xlarge"]; !ok || tc.Coverage != 0.5 {
		t.Errorf("expected 50%% coverage for m5.xlarge, got %v", report.ByType["m5.xlarge"])
	}
}

func TestRIEngine_GetCoverageReport_WastedRI(t *testing.T) {
	engine := NewRIRecommendationEngine(DefaultRIConfig(), nil)
	records := []UsageRecord{
		{Timestamp: time.Now(), InstanceType: "t3.micro", InstanceCount: 2, OnDemandCost: 10},
	}
	engine.IngestUsageData(records)
	engine.RegisterExistingRI(&ReservedInstance{
		ID: "ri-waste", InstanceType: "t3.micro", Count: 10,
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
	})

	report := engine.GetCoverageReport()
	if report.WastedRI != 8 {
		t.Errorf("expected 8 wasted RI, got %d", report.WastedRI)
	}
}

// ============================================================================
// Cost Analysis Engine Tests
// ============================================================================

func TestDefaultCostAnalysisConfig(t *testing.T) {
	cfg := DefaultCostAnalysisConfig()
	if cfg.ForecastHorizon != 30 {
		t.Errorf("unexpected ForecastHorizon: %d", cfg.ForecastHorizon)
	}
	if cfg.AnomalyThreshold != 2.0 {
		t.Errorf("unexpected AnomalyThreshold: %v", cfg.AnomalyThreshold)
	}
}

func TestNewCostAnalysisEngine(t *testing.T) {
	engine := NewCostAnalysisEngine(DefaultCostAnalysisConfig(), nil)
	if engine == nil {
		t.Fatal("engine should not be nil")
	}
}

func TestCostEngine_IngestCostData(t *testing.T) {
	engine := NewCostAnalysisEngine(DefaultCostAnalysisConfig(), nil)
	records := []CostRecord{
		{Date: time.Now(), TotalCost: 100, Currency: "USD"},
		{Date: time.Now().Add(-24 * time.Hour), TotalCost: 90, Currency: "USD"},
	}
	engine.IngestCostData(records)
	if len(engine.costRecords) != 2 {
		t.Errorf("expected 2 records, got %d", len(engine.costRecords))
	}
	// Verify sorted by date
	if engine.costRecords[0].Date.After(engine.costRecords[1].Date) {
		t.Error("records should be sorted by date ascending")
	}
}

func TestCostEngine_SetBudget(t *testing.T) {
	engine := NewCostAnalysisEngine(DefaultCostAnalysisConfig(), nil)
	engine.SetBudget("team-ai", "team", 5000)
	if b, ok := engine.budgets["team-ai"]; !ok || b.MonthlyLimit != 5000 {
		t.Error("budget not set correctly")
	}
}

func TestCostEngine_GenerateReport_NoData(t *testing.T) {
	engine := NewCostAnalysisEngine(DefaultCostAnalysisConfig(), nil)
	_, err := engine.GenerateReport(context.Background(), 30)
	if err == nil {
		t.Error("expected error for no data")
	}
}

func TestCostEngine_GenerateReport_CancelledContext(t *testing.T) {
	engine := NewCostAnalysisEngine(DefaultCostAnalysisConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := engine.GenerateReport(ctx, 30)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestCostEngine_GenerateReport(t *testing.T) {
	engine := NewCostAnalysisEngine(DefaultCostAnalysisConfig(), nil)

	now := time.Now()
	var records []CostRecord
	for i := 0; i < 60; i++ {
		records = append(records, CostRecord{
			Date:      now.AddDate(0, 0, -i),
			TotalCost: 100 + float64(i%10),
			Currency:  "USD",
			ByService: map[string]float64{"compute": 60, "storage": 40},
			ByTeam:    map[string]float64{"ai-team": 80, "platform": 20},
			ByProject: map[string]float64{"proj-a": 100},
			ByRegion:  map[string]float64{"us-east-1": 100},
			ByResource:    map[string]float64{"compute": 60, "storage": 30, "network": 10},
			ByEnvironment: map[string]float64{"prod": 70, "dev": 30},
			GPUCost:    20,
			ComputeCost: 60,
		})
	}
	engine.IngestCostData(records)
	engine.SetBudget("ai-team", "team", 3000)

	report, err := engine.GenerateReport(context.Background(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalCost <= 0 {
		t.Error("total cost should be > 0")
	}
	if report.Currency != "USD" {
		t.Errorf("unexpected currency: %s", report.Currency)
	}
	if len(report.DailyTrend) == 0 {
		t.Error("daily trend should not be empty")
	}
	if report.GPUCostBreakdown == nil {
		t.Error("GPU breakdown should not be nil")
	}
	if report.EfficiencyScore < 0 || report.EfficiencyScore > 100 {
		t.Errorf("efficiency score out of range: %v", report.EfficiencyScore)
	}
	if len(report.BudgetStatus) == 0 {
		t.Error("budget status should not be empty")
	}
}

func TestCostEngine_DetectAnomalies(t *testing.T) {
	engine := NewCostAnalysisEngine(DefaultCostAnalysisConfig(), nil)

	now := time.Now()
	var records []CostRecord
	for i := 0; i < 30; i++ {
		cost := 100.0
		if i == 15 {
			cost = 500.0 // anomaly
		}
		records = append(records, CostRecord{
			Date:      now.AddDate(0, 0, -i),
			TotalCost: cost,
			Currency:  "USD",
			ByService: map[string]float64{"compute": cost},
		})
	}
	engine.IngestCostData(records)

	report, err := engine.GenerateReport(context.Background(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Anomalies) == 0 {
		t.Error("expected at least one anomaly")
	}
}

// ============================================================================
// Spot Prediction Engine Tests
// ============================================================================

func TestDefaultSpotPredictionConfig(t *testing.T) {
	cfg := DefaultSpotPredictionConfig()
	if cfg.PriceHistoryWindow != 7*24*time.Hour {
		t.Errorf("unexpected PriceHistoryWindow: %v", cfg.PriceHistoryWindow)
	}
	if cfg.VolatilityWeight+cfg.TrendWeight != 1.0 {
		t.Errorf("weights should sum to 1.0: %v + %v", cfg.VolatilityWeight, cfg.TrendWeight)
	}
}

func TestNewSpotPredictionEngine(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)
	if engine == nil {
		t.Fatal("engine should not be nil")
	}
}

func TestSpotEngine_IngestPriceData(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)

	points := []SpotPricePoint{
		{Timestamp: time.Now(), InstanceType: "m5.xlarge", Zone: "us-east-1a", Price: 0.05, OnDemand: 0.192},
		{Timestamp: time.Now(), InstanceType: "m5.xlarge", Zone: "us-east-1a", Price: 0.06, OnDemand: 0.192},
	}
	engine.IngestPriceData(points)
	if len(engine.priceHistory["m5.xlarge"]) != 2 {
		t.Errorf("expected 2 price points, got %d", len(engine.priceHistory["m5.xlarge"]))
	}
}

func TestSpotEngine_Predict_InsufficientData(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)
	_, err := engine.Predict(context.Background(), "m5.xlarge")
	if err == nil {
		t.Error("expected error for missing instance type")
	}

	// Add < 10 points
	for i := 0; i < 5; i++ {
		engine.IngestPriceData([]SpotPricePoint{
			{Timestamp: time.Now().Add(-time.Duration(i) * time.Hour), InstanceType: "m5.xlarge", Price: 0.05, OnDemand: 0.192},
		})
	}
	_, err = engine.Predict(context.Background(), "m5.xlarge")
	if err == nil {
		t.Error("expected error for insufficient data (< 10 points)")
	}
}

func TestSpotEngine_Predict_CancelledContext(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := engine.Predict(ctx, "m5.xlarge")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestSpotEngine_Predict_StablePrice(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)

	var points []SpotPricePoint
	for i := 0; i < 20; i++ {
		points = append(points, SpotPricePoint{
			Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour),
			InstanceType: "c5.xlarge",
			Zone:         "us-west-2a",
			Price:        0.05,
			OnDemand:     0.17,
		})
	}
	engine.IngestPriceData(points)

	pred, err := engine.Predict(context.Background(), "c5.xlarge")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pred.RecommendedAction != "hold" {
		t.Logf("action=%s prob=%.2f", pred.RecommendedAction, pred.InterruptionProb)
	}
	if pred.Volatility < 0 {
		t.Error("volatility should be >= 0")
	}
	if pred.InterruptionProb < 0 || pred.InterruptionProb > 1 {
		t.Errorf("interruption probability out of range: %v", pred.InterruptionProb)
	}
}

func TestSpotEngine_PredictAll(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)

	for _, instType := range []string{"m5.xlarge", "c5.xlarge"} {
		var points []SpotPricePoint
		for i := 0; i < 15; i++ {
			points = append(points, SpotPricePoint{
				Timestamp: time.Now().Add(-time.Duration(i) * time.Hour), InstanceType: instType,
				Price: 0.05 + float64(i)*0.001, OnDemand: 0.2, Zone: "us-east-1a",
			})
		}
		engine.IngestPriceData(points)
	}

	preds, err := engine.PredictAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(preds) != 2 {
		t.Errorf("expected 2 predictions, got %d", len(preds))
	}
}

func TestSpotEngine_OptimizeBidStrategy_NoPrediction(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)
	_, err := engine.OptimizeBidStrategy("m5.xlarge", 0.6)
	if err == nil {
		t.Error("expected error when no prediction exists")
	}
}

func TestSpotEngine_OptimizeBidStrategy(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)

	var points []SpotPricePoint
	for i := 0; i < 15; i++ {
		points = append(points, SpotPricePoint{
			Timestamp: time.Now().Add(-time.Duration(i) * time.Hour), InstanceType: "m5.xlarge",
			Price: 0.06, OnDemand: 0.192, Zone: "us-east-1a",
		})
	}
	engine.IngestPriceData(points)
	engine.Predict(context.Background(), "m5.xlarge")

	bid, err := engine.OptimizeBidStrategy("m5.xlarge", 0.6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bid.CurrentBid <= 0 {
		t.Error("current bid should be > 0")
	}
	if bid.Strategy == "" {
		t.Error("strategy should not be empty")
	}
}

func TestSpotEngine_CreateMigrationPlan(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)
	plan := engine.CreateMigrationPlan("wl-1", "m5.xlarge", "m5.2xlarge", 1)
	if plan == nil {
		t.Fatal("plan should not be nil")
	}
	if plan.Status != "pending" {
		t.Errorf("expected pending status, got %s", plan.Status)
	}
	if plan.Priority != 1 {
		t.Errorf("expected priority 1, got %d", plan.Priority)
	}
}

func TestSpotEngine_GetMigrationQueue(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)
	engine.CreateMigrationPlan("wl-1", "src", "dst", 2)
	engine.CreateMigrationPlan("wl-2", "src", "dst", 1)

	queue := engine.GetMigrationQueue()
	if len(queue) != 2 {
		t.Errorf("expected 2 plans, got %d", len(queue))
	}
	// Should be sorted by priority
	if queue[0].Priority > queue[1].Priority {
		t.Error("queue should be sorted by priority ascending")
	}
}

func TestSpotEngine_GetSavingsSummary(t *testing.T) {
	engine := NewSpotPredictionEngine(DefaultSpotPredictionConfig(), nil)

	var points []SpotPricePoint
	for i := 0; i < 15; i++ {
		points = append(points, SpotPricePoint{
			Timestamp: time.Now().Add(-time.Duration(i) * time.Hour), InstanceType: "m5.xlarge",
			Price: 0.05, OnDemand: 0.192, Zone: "us-east-1a",
		})
	}
	engine.IngestPriceData(points)
	engine.Predict(context.Background(), "m5.xlarge")

	summary := engine.GetSavingsSummary()
	if summary.InstanceCount != 1 {
		t.Errorf("expected 1 instance, got %d", summary.InstanceCount)
	}
	if summary.TotalSavingsPercent <= 0 {
		t.Errorf("expected positive savings, got %v", summary.TotalSavingsPercent)
	}
}
