package cloudprovider

import (
	"context"
	"math"
	"testing"
)

// TestTopologyDiscoverer_DiscoverInstanceTypes verifies the discoverer returns a
// deterministic, sorted, non-empty catalog spanning multiple regions.
func TestTopologyDiscoverer_DiscoverInstanceTypes(t *testing.T) {
	d := NewTopologyDiscoverer()
	ctx := context.Background()

	profiles, err := d.DiscoverInstanceTypes(ctx)
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected non-empty catalog")
	}

	// Verify ascending price ordering (deterministic contract).
	for i := 1; i < len(profiles); i++ {
		if profiles[i-1].HourlyCostUSD > profiles[i].HourlyCostUSD {
			t.Fatalf("catalog not sorted by cost at index %d: %.4f > %.4f",
				i, profiles[i-1].HourlyCostUSD, profiles[i].HourlyCostUSD)
		}
	}

	// GPU instances must carry non-zero GPU metadata.
	foundGPU := false
	for _, p := range profiles {
		if p.GPUCount > 0 {
			foundGPU = true
			if p.GPUMemoryGB <= 0 || p.GPUModel == "" {
				t.Errorf("GPU instance %s has incomplete GPU metadata", p.Name)
			}
		}
	}
	if !foundGPU {
		t.Error("expected at least one GPU instance in catalog")
	}
}

// TestTopologyDiscoverer_RegionalPricing verifies price varies by region without compounding.
func TestTopologyDiscoverer_RegionalPricing(t *testing.T) {
	d := NewTopologyDiscoverer()

	us, err := d.GetInstanceProfile("g5.xlarge", "us-east-1")
	if err != nil {
		t.Fatalf("us-east-1 lookup failed: %v", err)
	}
	eu, err := d.GetInstanceProfile("g5.xlarge", "eu-west-1")
	if err != nil {
		t.Fatalf("eu-west-1 lookup failed: %v", err)
	}
	ap, err := d.GetInstanceProfile("g5.xlarge", "ap-northeast-1")
	if err != nil {
		t.Fatalf("ap-northeast-1 lookup failed: %v", err)
	}

	// Base us price is 1.006; EU = 1.05x, AP = 1.18x (no compounding).
	if math.Abs(us.HourlyCostUSD-1.006) > 1e-6 {
		t.Errorf("us price wrong: got %.6f want 1.006", us.HourlyCostUSD)
	}
	if math.Abs(eu.HourlyCostUSD-1.006*1.05) > 1e-6 {
		t.Errorf("eu price wrong: got %.6f want %.6f", eu.HourlyCostUSD, 1.006*1.05)
	}
	if math.Abs(ap.HourlyCostUSD-1.006*1.18) > 1e-6 {
		t.Errorf("ap price wrong: got %.6f want %.6f", ap.HourlyCostUSD, 1.006*1.18)
	}
}

// TestEstimateCost_DecompositionMAPE feeds a synthetic trend+seasonal series and asserts
// the additive decomposition achieves low MAPE (< 15%) on a well-behaved signal.
func TestEstimateCost_DecompositionMAPE(t *testing.T) {
	d := NewTopologyDiscoverer()

	// Construct 28 days = 4 weeks with weekly (period=7) seasonality + linear trend.
	period := 7
	n := 28
	series := make([]float64, n)
	seasonalPattern := []float64{10, 12, 15, 14, 13, 8, 6} // weekday cost cycle
	for i := 0; i < n; i++ {
		trend := 100.0 + 2.0*float64(i) // rising base cost
		season := seasonalPattern[i%period]
		series[i] = trend + season
	}

	res := d.EstimateCost(series, period)
	if len(res.Trend) != n {
		t.Fatalf("expected trend length %d, got %d", n, len(res.Trend))
	}
	if res.MAPE > 15.0 {
		t.Errorf("MAPE too high on clean signal: %.2f%% (want < 15%%)", res.MAPE)
	}
	t.Logf("decomposition MAPE=%.3f%% variance=%.3f", res.MAPE, res.Variance)
}

// TestForecast_ProducesBoundedIntervals verifies forecast points have valid CI ordering.
func TestForecast_ProducesBoundedIntervals(t *testing.T) {
	d := NewTopologyDiscoverer()

	period := 7
	series := make([]float64, 21)
	for i := range series {
		series[i] = 50.0 + float64(i) + float64(i%period)*3.0
	}
	res := d.EstimateCost(series, period)

	fc := res.Forecast(7)
	if len(fc) != 7 {
		t.Fatalf("expected 7 forecast points, got %d", len(fc))
	}
	for i, p := range fc {
		if p.Lower > p.Value || p.Value > p.Upper {
			t.Errorf("point %d violates CI ordering: lower=%.2f value=%.2f upper=%.2f",
				i, p.Lower, p.Value, p.Upper)
		}
		if p.Lower < 0 {
			t.Errorf("point %d lower bound negative: %.2f", i, p.Lower)
		}
		if math.Abs(p.ConfidenceLevel-0.95) > 1e-9 {
			t.Errorf("point %d unexpected confidence level %.3f", i, p.ConfidenceLevel)
		}
	}
}

// TestOptimize_RespectsConstraintsAndRanking verifies the multi-objective optimizer
// filters over-budget candidates and returns score-descending recommendations.
func TestOptimize_RespectsConstraintsAndRanking(t *testing.T) {
	d := NewTopologyDiscoverer()
	ctx := context.Background()

	profiles, _ := d.DiscoverInstanceTypes(ctx)

	req := OptimizationRequest{
		DailyBudgetUSD:   48.0, // $2/hr average
		MinReliability:   0.9,
		MaxDailySpendUSD: 48.0,
		PriorityWeights: Priorities{
			CostWeight: 0.4, PerformanceWeight: 0.4, ReliabilityWeight: 0.2,
		},
	}

	decisions, violations := d.Optimize(ctx, req, profiles)
	if len(decisions) == 0 {
		t.Fatal("expected at least one feasible decision")
	}

	// Score must be non-increasing.
	for i := 1; i < len(decisions); i++ {
		if decisions[i-1].Score < decisions[i].Score {
			t.Errorf("decisions not sorted by score at %d: %.4f < %.4f",
				i, decisions[i-1].Score, decisions[i].Score)
		}
	}

	// The most expensive shape (h100.8xlarge at $99/hr) must be rejected by budget.
	for _, dec := range decisions {
		if dec.TargetInstanceType == "h100.8xlarge" {
			t.Error("h100.8xlarge should be filtered out by budget constraint")
		}
	}
	t.Logf("feasible=%d violations=%d top=%s score=%.4f",
		len(decisions), len(violations), decisions[0].TargetInstanceType, decisions[0].Score)
}

// TestOrchestrator_EndToEnd exercises the full recommend → cost-model → forecast flow.
func TestOrchestrator_EndToEnd(t *testing.T) {
	o := NewAutoScalerOrchestrator()
	ctx := context.Background()

	req := OptimizationRequest{
		DailyBudgetUSD:   100.0,
		MinReliability:   0.9,
		MaxDailySpendUSD: 100.0,
		PriorityWeights:  Priorities{CostWeight: 0.5, PerformanceWeight: 0.3, ReliabilityWeight: 0.2},
	}
	decisions, _ := o.RecommendInstance(ctx, req)
	if len(decisions) == 0 {
		t.Fatal("orchestrator returned no recommendations")
	}

	// Feed synthetic 14-day spend history and forecast next week.
	costs := make([]float64, 14)
	for i := range costs {
		costs[i] = 80.0 + float64(i)*1.5 + float64(i%7)*4.0
	}
	model := o.UpdateCostModel(ctx, costs)
	forecast := o.PredictNextWeek(model)
	if len(forecast) != 7 {
		t.Fatalf("expected 7-day forecast, got %d", len(forecast))
	}
	for i, f := range forecast {
		if f.Value <= 0 {
			t.Errorf("forecast day %d non-positive: %.2f", i, f.Value)
		}
	}
}

// TestContextCancellation verifies discovery and optimize honor a canceled context.
func TestContextCancellation(t *testing.T) {
	d := NewTopologyDiscoverer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := d.DiscoverInstanceTypes(ctx); err == nil {
		t.Error("expected context error from DiscoverInstanceTypes")
	}
	if err := d.Refresh(ctx); err == nil {
		t.Error("expected context error from Refresh")
	}
}
