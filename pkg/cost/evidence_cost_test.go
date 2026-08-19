package cost

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func newTestCostEngine(t *testing.T) *EvidenceCostEngine {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidenceCostEngine(priv)
}

// TestCalculateCost_ProducesVerifiableReceipt proves a cost claim is sealed into
// a signed, offline-verifiable receipt and that the totals are computed.
func TestCalculateCost_ProducesVerifiableReceipt(t *testing.T) {
	engine := newTestCostEngine(t)

	report, err := engine.CalculateCost(ResourceUsage{
		WorkloadType: "gpu-training",
		Provider:     "aws",
		Region:       "us-east-1",
		InstanceType: "nvidia-a100-80gb",
		GPUCount:     8,
		VCPUCount:    32,
		StorageGB:    500,
		Hours:        24,
		EgressGB:     100,
	})
	if err != nil {
		t.Fatalf("calculate cost: %v", err)
	}
	if report.Receipt == nil || !report.Receipt.Verify() {
		t.Fatal("cost report must carry a verifiable receipt")
	}
	if report.TotalCost <= 0 {
		t.Fatalf("expected positive total cost, got %.4f", report.TotalCost)
	}
	// 8 GPUs * 24h * $7.56 = the dominant term; total must exceed GPU cost floor.
	if report.GPUCost < 8*24*7.0 {
		t.Fatalf("gpu cost lower than expected floor: %.2f", report.GPUCost)
	}
}

// TestArbitrage_LearnsCheapestPlacement verifies the Q-learning agent, after
// observing prices across providers and training, recommends migrating an
// expensive placement to the cheapest one with >=20% savings.
func TestArbitrage_LearnsCheapestPlacement(t *testing.T) {
	engine := newTestCostEngine(t)
	agent := engine.ArbitrageAgent()

	// Feed real cross-cloud prices for the same A100 workload.
	engine.mustCost(t, "inference", "aws", "us-east-1", "nvidia-a100-80gb")   // $7.56/gpu-hr
	engine.mustCost(t, "inference", "azure", "eastus", "nvidia-a100-80gb")    // $7.50
	engine.mustCost(t, "inference", "gcp", "us-central1", "nvidia-a100-80gb") // $6.00 (cheapest)

	agent.Train(2000)

	// Currently on the most expensive (aws), each with 1 GPU + 0 vCPU.
	rec, err := agent.RecommendMigration(Placement{
		WorkloadType: "inference",
		Provider:     "aws",
		Region:       "us-east-1",
		InstanceType: "nvidia-a100-80gb",
		CostPerHour:  7.56,
	})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if rec.TargetProvider != "gcp" {
		t.Fatalf("expected agent to learn gcp is cheapest, got %q (proj=%.2f)", rec.TargetProvider, rec.ProjectedCostPerHour)
	}
	if rec.SavingsPct < 0.20 {
		t.Fatalf("expected >=20%% savings, got %.2f%%", rec.SavingsPct*100)
	}
	if !rec.Recommended {
		t.Fatal("agent should recommend the cheaper placement")
	}
}

// TestArbitrage_NoRecommendationWhenAlreadyCheapest ensures the agent does not
// recommend a move when the current placement is already the cheapest.
func TestArbitrage_NoRecommendationWhenAlreadyCheapest(t *testing.T) {
	engine := newTestCostEngine(t)
	agent := engine.ArbitrageAgent()

	engine.mustCost(t, "batch", "aws", "us-east-1", "nvidia-a100-80gb")
	engine.mustCost(t, "batch", "gcp", "us-central1", "nvidia-a100-80gb")
	agent.Train(1000)

	rec, err := agent.RecommendMigration(Placement{
		WorkloadType: "batch",
		Provider:     "gcp",
		Region:       "us-central1",
		InstanceType: "nvidia-a100-80gb",
		CostPerHour:  6.00,
	})
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if rec.Recommended {
		t.Fatalf("should not recommend moving away from cheapest: %+v", rec)
	}
}

// TestArbitrage_UnknownWorkloadErrors verifies an error for an unobserved workload.
func TestArbitrage_UnknownWorkloadErrors(t *testing.T) {
	engine := newTestCostEngine(t)
	if _, err := engine.ArbitrageAgent().RecommendMigration(Placement{WorkloadType: "unknown"}); err == nil {
		t.Fatal("expected error for unobserved workload")
	}
}

// mustCost is a test helper that runs a 1-GPU cost calculation, which also feeds
// the observed per-hour price to the arbitrage agent.
func (e *EvidenceCostEngine) mustCost(t *testing.T, workload, provider, region, instance string) {
	t.Helper()
	if _, err := e.CalculateCost(ResourceUsage{
		WorkloadType: workload,
		Provider:     provider,
		Region:       region,
		InstanceType: instance,
		GPUCount:     1,
		Hours:        1,
	}); err != nil {
		t.Fatalf("cost calc (%s/%s): %v", provider, instance, err)
	}
}
