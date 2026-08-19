package cost

import (
	"testing"
	"time"
)

func TestCalculateClusterCost(t *testing.T) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)

	t.Run("basic_h100_cost", func(t *testing.T) {
		tr := TimeRange{
			Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2025, 1, 1, 24, 0, 0, 0, time.UTC),
			Resources: []ResourceSnapshot{
				{Instances: []InstanceUsage{{InstanceID: "nvidia-h100-80gb", Provider: "aws", GPUCount: 1, VCPUCount: 4, StorageGB: 100, HoursFraction: 1}}},
			},
			EgressGB: 10,
		}
		report := calc.CalculateClusterCost("cluster1", tr)
		if report.GPUCost <= 0 {
			t.Errorf("expected positive gpu cost, got %.2f", report.GPUCost)
		}
		// 1 GPU * 24h * $39/hr = $936 (approx H100 on-demand)
		expectedGPU := tr.DurationHours() * 39.0
		if abs(report.GPUCost-expectedGPU) > 1.0 {
			t.Errorf("gpu cost %.2f; want ~%.2f", report.GPUCost, expectedGPU)
		}
		if report.TotalCost < report.GPUCost {
			t.Error("total cost should include gpu cost")
		}
	})

	t.Run("budget_alerts", func(t *testing.T) {
		budgets := []BudgetAlert{{Name: "monthly", Threshold: 100.0, Enabled: true}}
		calc.SetBudgets(budgets)
		tr := TimeRange{Start: time.Now(), End: time.Now().Add(10 * time.Hour)}
		report := calc.CalculateClusterCost("small", tr)
		st := report.BudgetStatus.String()
		_ = st
	})
}

func BenchmarkCostCalculation(b *testing.B) {
	repo := NewInMemoryPricingRepo()
	calc := NewCostCalculator(repo)
	tr := TimeRange{
		Start: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2025, 1, 1, 720, 0, 0, 0, time.UTC),
		Resources: []ResourceSnapshot{
			{Instances: []InstanceUsage{{InstanceID: "nvidia-a100-80gb", Provider: "aws", GPUCount: 8, VCPUCount: 32, StorageGB: 1000, HoursFraction: 1}}},
			{Instances: []InstanceUsage{{InstanceID: "nvidia-l40s", Provider: "azure", GPUCount: 4, VCPUCount: 16, StorageGB: 500, HoursFraction: 0.9}}},
		},
		EgressGB: 500,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calc.CalculateClusterCost("benchmark", tr)
	}
}
