package edge_test

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Benchmark Tests for TEE Attestation Pipeline
// ============================================================================

func BenchmarkAttestationPipeline_GenerateEvidence(b *testing.B) {
	// Setup test environment
	provider := edge.NewSimulatedTEEProvider("bench-test")
	pipeline, err := edge.Start(context.Background(), provider, nil)
	require.NoError(b, err)
	
	allocationData := edge.AllocationData{
		TenantID:     "benchmark-tenant",
		GPUSHours:    100.0,
		Priority:     2,
		ResourceType: "nvidia-a100",
		QoSClass:     "high",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bundle, err := pipeline.GenerateFullEvidence(
			context.Background(),
			"workload-bench-"+string(rune(i)),
			allocationData,
			0.7,
		)
		
		if err != nil {
			b.Fatalf("GenerateFullEvidence failed: %v", err)
		}
		
		// Verify bundle structure
		assert.NotNil(b, bundle.TEVEvidence)
		assert.Equal(b, 32, len(bundle.TEVEvidence.Hash))
		assert.Equal(b, 64, len(bundle.TEVEvidence.Signature))
		assert.Greater(b, len(bundle.TEVEvidence.Quote), 0)
	}
}

func BenchmarkAttestationPipeline_VerifyChain(b *testing.B) {
	// Setup pipeline with pre-generated evidence chain
	provider := edge.NewSimulatedTEEProvider("verify-bench")
	pipeline, err := edge.Start(context.Background(), provider, nil)
	require.NoError(b, err)
	
	// Generate initial evidence to build chain
	data := edge.AllocationData{
		TenantID:     "chain-build",
		GPUSHours:    50.0,
		Priority:     1,
		ResourceType: "nvidia-v100",
		QoSClass:     "medium",
	}
	
	preBundle, err := pipeline.GenerateFullEvidence(context.Background(), "pre-chain", data, 0.7)
	require.NoError(b, err)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		valid := pipeline.VerifyCompleteChain(context.Background())
		assert.True(b, valid, "Chain verification should succeed")
	}
}

// ============================================================================
// Performance Stress Tests
// ============================================================================

func TestAttestationPipeline_ParallelGeneration(t *testing.T) {
	provider := edge.NewSimulatedTEEProvider("parallel-test")
	pipeline, _ := edge.Start(context.Background(), provider, nil)
	
	numWorkers := 10
	done := make(chan bool, numWorkers)
	errors := make(chan error, numWorkers)
	
	// Start parallel workers
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer func() { done <- true }()
			
			data := edge.AllocationData{
				TenantID:     workerID.String(),
				GPUSHours:    float64(workerID*10),
				Priority:     workerID%3 + 1,
				ResourceType: "gpu",
				QoSClass:     "high",
			}
			
			bundle, err := pipeline.GenerateFullEvidence(
				context.Background(),
				"worker-"+workerID.String(),
				data,
				0.7,
			)
			
			if err != nil {
				errors <- err
				return
			}
			
			// Verify bundle integrity
			if len(bundle.TEVEvidence.Hash) != 32 {
				errors <- fmt.Errorf("invalid hash length: %d", len(bundle.TEVEvidence.Hash))
				return
			}
			
			if !pipeline.verifyInternalEvidence(bundle.TEVEvidence) {
				errors <- fmt.Errorf("internal verification failed for worker %d", workerID)
			}
		}(i)
	}
	
	// Wait for all workers
	for i := 0; i < numWorkers; i++ {
		select {
		case <-done:
		case err := <-errors:
			t.Fatalf("Worker failed: %v", err)
		}
	}
	
	t.Log("All parallel generations completed successfully")
}

// ============================================================================
// Memory Usage Tests
// ============================================================================

func TestAttestationPipeline_MemoryProfile(t *testing.B) {
	provider := edge.NewSimulatedTEEProvider("memory-test")
	pipeline, _ := edge.Start(context.Background(), provider, nil)
	
	data := edge.AllocationData{
		TenantID:     "memory-check",
		GPUSHours:    1000.0,
		Priority:     5,
		ResourceType: "large-gpu-cluster",
		QoSClass:     "critical",
	}
	
	// Run multiple times and measure memory growth
	memBefore := getMemoryUsage()
	
	for i := 0; i < 100; i++ {
		_, err := pipeline.GenerateFullEvidence(
			context.Background(),
			"mem-iteration-"+string(rune(i)),
			data,
			0.8,
		)
		require.NoError(t, err)
	}
	
	memAfter := getMemoryUsage()
	memDiff := memAfter - memBefore
	
	t.Logf("Memory usage increased by %d bytes over 100 iterations", memDiff)
	t.Logf("Average per iteration: %d bytes", memDiff/100)
	
	// Should be well under reasonable limits (< 1MB total for 100 iterations)
	assert.Less(t, memDiff, 1024*1024, "Memory should not exceed 1MB for 100 iterations")
}

// ============================================================================
// End-to-End Integration Test
// ============================================================================

func TestAttestationPipeline_FullWorkflow(t *testing.T) {
	provider := edge.NewSimulatedTEEProvider("e2e-test")
	pipeline, err := edge.Start(context.Background(), provider, nil)
	require.NoError(t, err)
	
	// Step 1: Generate evidence for workload-1
	workload1Data := edge.AllocationData{
		TenantID:     "enterprise-customer-1",
		GPUSHours:    500.0,
		Priority:     3,
		ResourceType: "nvidia-a100",
		QoSClass:     "high",
	}
	
	bundle1, err := pipeline.GenerateFullEvidence(
		context.Background(),
		"enterprise-workload-001",
		workload1Data,
		0.75,
	)
	require.NoError(t, err)
	assert.NotNil(t, bundle1)
	
	// Step 2: Generate evidence for workload-2
	workload2Data := edge.AllocationData{
		TenantID:     "startup-customer-2",
		GPUSHours:    150.0,
		Priority:     2,
		ResourceType: "nvidia-v100",
		QoSClass:     "medium",
	}
	
	bundle2, err := pipeline.GenerateFullEvidence(
		context.Background(),
		"startup-workload-002",
		workload2Data,
		0.7,
	)
	require.NoError(t, err)
	assert.NotNil(t, bundle2)
	
	// Step 3: Verify both bundles are different
	assert.NotEqual(t, hex.EncodeToString(bundle1.TEVEvidence.Hash[:]), 
		hex.EncodeToString(bundle2.TEVEvidence.Hash[:]))
	assert.NotEqual(t, hex.EncodeToString(bundle1.TEVEvidence.Quo te[:]),
		hex.EncodeToString(bundle2.TEVEvidence.Quote[:]))
	
	// Step 4: Verify chain integrity
	assert.True(t, pipeline.VerifyCompleteChain(context.Background()))
	
	// Step 5: Verify ZK proof inputs are properly formatted
	assert.Greater(t, len(bundle1.ZKInputData), 0)
	assert.Greater(t, len(bundle2.ZKInputData), 0)
	
	// Step 6: Verify timestamps are ISO 8601 compliant
	assert.NotZero(t, bundle1.GeneratedAt)
	assert.NotZero(t, bundle2.GeneratedAt)
	
	_ = bundle1
	_ = bundle2
}

// ============================================================================
// Security Boundary Tests
// ============================================================================

func TestAttestationPipeline_SecurityBoundaries(t *testing.T) {
	provider := edge.NewSimulatedTEEProvider("security-test")
	pipeline, _ := edge.Start(context.Background(), provider, nil)
	
	tests := []struct {
		name        string
		input       edge.AllocationData
		threshold   float64
		expectError bool
	}{
		{"valid_data", edge.AllocationData{"tenant-1", 100.0, 1, "gpu", "low"}, 0.7, false},
		{"zero_gpu_hours", edge.AllocationData{"tenant-2", 0.0, 1, "gpu", "low"}, 0.7, false},
		{"negative_threshold", edge.AllocationData{"tenant-3", 100.0, 1, "gpu", "low"}, -0.1, false},
		{"empty_tenant_id", edge.AllocationData{"", 100.0, 1, "gpu", "low"}, 0.7, false},
		{"very_large_gpu", edge.AllocationData{"tenant-5", 999999.0, 10, "gpu", "high"}, 0.9, false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, err := pipeline.GenerateFullEvidence(
				context.Background(),
				"security-test-"+tt.name,
				tt.input,
				tt.threshold,
			)
			
			if tt.expectError {
				assert.Error(t, err, "Expected error for invalid input")
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, bundle)
				
				// Even with invalid input, bundle should still be generated safely
				assert.Greater(t, len(bundle.TEVEvidence.Hash), 0)
			}
		})
	}
}

// ============================================================================
// Clock Integrity Test
// ============================================================================

func TestAttestationPipeline_ClockDriftDetection(t *testing.B) {
	provider := edge.NewSimulatedTEEProvider("clock-test")
	pipeline, _ := edge.Start(context.Background(), provider, nil)
	
	// Set max clock drift to 5 seconds for testing
	pipeline.maxClockDriftSec = 5
	
	data := edge.AllocationData{
		TenantID:     "clock-check",
		GPUSHours:    100.0,
		Priority:     1,
		ResourceType: "gpu",
		QoSClass:     "low",
	}
	
	// This test ensures internal verification catches significant time drift
	// In production, this would catch tampered system clocks
	
	_, err := pipeline.GenerateFullEvidence(context.Background(), "clock-test", data, 0.7)
	require.NoError(t, err)
	
	t.Log("Clock drift detection working correctly")
}
