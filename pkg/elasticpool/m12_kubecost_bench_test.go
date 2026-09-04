package elasticpool_test

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/elasticpool"
)

// ============================================================================
// M12 Elastic Pool T2 FLIP: Our FSM-based pool vs OpenCost/Kubecost proxy
// 
// Competitor: OpenCost-style cost aggregation + simple pool allocation (proxy)
// Our Implementation: FSMElasticPool with attested ledger for budget enforcement
// 
// Goal: Compare pool allocation/release latency and budget check performance
// Expected: Our approach competitive on raw allocation, better on budget guards
// ============================================================================

func BenchmarkKubecostStyleProxy(b *testing.B) {
	// Simulate Kubecost/OpenCost-style cost aggregation from multiple resources
	costData := make(map[string]float64, 100)
	for i := 0; i < 100; i++ {
		key := "resource-" + string(rune('a'+i%26)) + "-" + itoa(i)
		costData[key] = float64(i%50+1) * 0.01
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var totalCost float64
		for _, cost := range costData {
			totalCost += cost
		}
		_ = totalCost
	}
}

func BenchmarkOurPoolAllocateRelease(b *testing.B) {
	ctx := context.Background()

	signer, _ := evidence.GenerateEphemeralSigner()
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})

	store, err := elasticpool.NewFSMElasticPool(b.TempDir(), ledger)
	if err != nil {
		b.Fatalf("failed to create store: %v", err)
	}

	poolObj, err := store.CreatePool(ctx, elasticpool.PoolInput{
		Name:            "benchmark-pool",
		GPUType:         "A100-80G",
		SlotsPerNode:    8,
		MinNodes:        1,
		MaxNodes:        10,
		CostPerNodeHour: 0.50,
	})
	if err != nil {
		b.Fatalf("failed to create pool: %v", err)
	}

	// Add some nodes first
	for i := 0; i < 3; i++ {
		_, err := store.AddNode(ctx, poolObj.ID)
		if err != nil {
			b.Logf("AddNode error: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lease, err := store.Acquire(ctx, poolObj.ID, "test-service-"+itoa(i), 1)
		if err == nil && lease != nil {
			store.Release(ctx, lease.ID)
		}
	}
}

func BenchmarkBoth_ScaleDecisionLatency(b *testing.B) {
	ctx := context.Background()

	// Kubecost proxy path - aggregate costs from 50 resources
	kubeCostData := make(map[string][]float64, 50)
	for i := 0; i < 50; i++ {
		kubeCostData["resource"] = append(kubeCostData["resource"], float64(i)*0.01)
	}

	// Our pool path - use real FSMElasticPool
	signer, _ := evidence.GenerateEphemeralSigner()
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	store, _ := elasticpool.NewFSMElasticPool(b.TempDir(), ledger)
	poolObj, _ := store.CreatePool(ctx, elasticpool.PoolInput{
		Name:            "scaling-test",
		GPUType:         "A100",
		SlotsPerNode:    8,
		MinNodes:        1,
		MaxNodes:        20,
		CostPerNodeHour: 0.50,
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Path 1: Aggregate costs (Kubecost proxy)
		var kTotal float64
		for _, values := range kubeCostData {
			for _, v := range values {
				kTotal += v
			}
		}

		// Path 2: Our FSMElasticPool allocate/release (same iteration)
		lease, _ := store.Acquire(ctx, poolObj.ID, "bench-"+itoa(i), 1)
		if lease != nil {
			store.Release(ctx, lease.ID)
		}

		_ = kTotal
	}
}

// itoa is helper for deterministic key generation (no external deps)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
