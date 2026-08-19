package cloudprovider

import (
	"context"
	"testing"
)

// BenchmarkDiscoverInstanceTypes measures catalog discovery throughput and allocation cost.
func BenchmarkDiscoverInstanceTypes(b *testing.B) {
	d := NewTopologyDiscoverer()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profiles, _ := d.DiscoverInstanceTypes(ctx)
		if len(profiles) == 0 {
			b.Fatal("expected non-empty catalog")
		}
	}
}

// BenchmarkEstimateCost_Decomposition14 reports MAPE computation cost on a 14-day series.
func BenchmarkEstimateCost_Decomposition14(b *testing.B) {
	d := NewTopologyDiscoverer()
	n := 14
	historicalCosts := make([]float64, n)
	for i := range historicalCosts {
		historicalCosts[i] = 50.0 + float64(i)*1.5 + float64(i%7)*3.0
	}
	period := 7
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.EstimateCost(historicalCosts, period)
	}
}

// BenchmarkForecast_7Days exercises 7-step forecast generation with CI bounds.
func BenchmarkForecast_7Days(b *testing.B) {
	d := NewTopologyDiscoverer()
	historicalCosts := make([]float64, 21)
	for i := range historicalCosts {
		historicalCosts[i] = 50.0 + float64(i) + float64(i%7)*3.0
	}
	model := d.EstimateCost(historicalCosts, 7)
	steps := 7
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fc := model.Forecast(steps)
		if len(fc) != steps {
			b.Fatalf("expected %d forecast points, got %d", steps, len(fc))
		}
	}
}

// BenchmarkOptimize_MediumScale scores ~20 candidates under multi-objective criteria.
func BenchmarkOptimize_MediumScale(b *testing.B) {
	d := NewTopologyDiscoverer()
	ctx := context.Background()
	profiles, _ := d.DiscoverInstanceTypes(ctx)
	req := OptimizationRequest{
		DailyBudgetUSD:   80.0,
		MinReliability:   0.9,
		MaxDailySpendUSD: 80.0,
		PriorityWeights: Priorities{CostWeight: 0.4, PerformanceWeight: 0.4, ReliabilityWeight: 0.2},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decisions, violations := d.Optimize(ctx, req, profiles)
		if len(decisions) == 0 {
			b.Fatal("expected feasible decisions")
		}
		_ = violations
	}
}

// BenchmarkOrchestrator_EndToEnd measures end-to-end orchestration latency.
func BenchmarkOrchestrator_EndToEnd(b *testing.B) {
	o := NewAutoScalerOrchestrator()
	ctx := context.Background()
	costs := make([]float64, 14)
	for i := range costs {
		costs[i] = 60.0 + float64(i) + float64(i%7)*4.0
	}
	req := OptimizationRequest{
		DailyBudgetUSD: 120.0,
		PriorityWeights: Priorities{CostWeight: 0.5, PerformanceWeight: 0.3, ReliabilityWeight: 0.2},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = o.RecommendInstance(ctx, req)
		model := o.UpdateCostModel(ctx, costs)
		_ = o.PredictNextWeek(model)
	}
}
