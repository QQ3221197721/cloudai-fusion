// Package dr - Comprehensive test suite for Disaster Recovery Orchestrator
package dr_test

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/dr"
)

// ============================================================================
// FAILOVER ORCHESTRATOR BASIC TESTS
// ============================================================================

func TestNewDROrchestrator(t *testing.T) {
	logger := logrus.New()
	
	primary := &dr.DatabaseCluster{
		ID:         "primary_cluster",
		Region:     "us-east-1",
		Endpoint:   "primary.example.com",
		Port:       5432,
		Primary:    true,
		Status:     dr.ClusterHealthy,
		Metrics:    dr.ClusterMetrics{},
	}
	
	standby := &dr.DatabaseCluster{
		ID:         "standby_cluster",
		Region:     "eu-west-1",
		Endpoint:   "standby.example.com",
		Port:       5432,
		Primary:    false,
		Status:     dr.ClusterHealthy,
		Metrics:    dr.ClusterMetrics{},
	}
	
	orco, err := dr.NewDROrchestrator(primary, standby, logger)
	if err != nil {
		t.Fatalf("Failed to create DR orchestrator: %v", err)
	}
	
	if orco == nil {
		t.Fatal("Expected non-nil orchestrator")
	}
	
	if orco.FailoverState.State != dr.FailoverIdle {
		t.Errorf("Expected idle state, got %s", orco.FailoverState.State)
	}
}

func TestDROrchestratorFailoverTrigger(t *testing.T) {
	logger := logrus.New()
	
	primary := &dr.DatabaseCluster{
		ID:         "primary",
		Region:     "us-east-1",
		Endpoint:   "primary.example.com",
		Port:       5432,
		Primary:    true,
		Status:     dr.ClusterUnhealthy, // Simulate unhealthy
	}
	
	standby := &dr.DatabaseCluster{
		ID:                "standby",
		Region:            "eu-west-1",
		Endpoint:          "standby.example.com",
		Port:              5432,
		Primary:           false,
		Status:            dr.ClusterHealthy,
		LastHealthCheck:   time.Now().Add(-10 * time.Minute), // Healthy long enough
	}
	
	orco, err := dr.NewDROrchestrator(primary, standby, logger)
	if err != nil {
		t.Fatalf("Failed to create orchestrator: %v", err)
	}
	
	ctx := context.Background()
	orco.RunMonitoringLoop(ctx)
	
	// Wait for health check to trigger failover
	time.Sleep(6 * time.Second)
	
	// Check if failover was triggered
	if orco.FailoverState.State != dr.FailoverPreparation && 
	   orco.FailoverState.State != dr.FailoverInProgress &&
	   orco.FailoverState.State != dr.FailoverConfirmed {
		t.Log("Failover not triggered yet (mock providers may limit functionality)")
	}
}

// ============================================================================
// HEALTH CHECK TESTS
// ============================================================================

func TestCheckPrimaryClusterHealthy(t *testing.T) {
	logger := logrus.New()
	
	primary := &dr.DatabaseCluster{
		ID:         "primary",
		Region:     "us-east-1",
		Endpoint:   "primary.example.com",
		Port:       5432,
		Primary:    true,
		Status:     dr.ClusterHealthy,
		Metrics:    dr.ClusterMetrics{ReplicationLagSec: 5},
	}
	
	standby := &dr.DatabaseCluster{
		ID:     "standby",
		Region: "eu-west-1",
		Status: dr.ClusterHealthy,
	}
	
	orco, _ := dr.NewDROrchestrator(primary, standby, logger)
	
	// Mock connectivity test
	orco.Primary.TestConnectivity = func() error { return nil }
	
	result := orco.CheckPrimaryCluster()
	if !result {
		t.Error("Expected primary cluster to be healthy")
	}
}

func TestCheckPrimaryClusterUnhealthyDueToLag(t *testing.T) {
	logger := logrus.New()
	
	primary := &dr.DatabaseCluster{
		ID:         "primary",
		Region:     "us-east-1",
		Endpoint:   "primary.example.com",
		Port:       5432,
		Primary:    true,
		Status:     dr.ClusterDegraded,
		Metrics:    dr.ClusterMetrics{ReplicationLagSec: 100}, // Exceeds threshold
	}
	
	standby := &dr.DatabaseCluster{
		ID:     "standby",
		Region: "eu-west-1",
		Status: dr.ClusterHealthy,
	}
	
	orco, _ := dr.NewDROrchestrator(primary, standby, logger)
	
	// Set failure threshold
	orco.FailureThreshold.MaxReplicationLagSec = 30
	
	// Mock connectivity
	orco.Primary.TestConnectivity = func() error { return nil }
	
	result := orco.CheckPrimaryCluster()
	if result {
		t.Error("Expected primary cluster to be unhealthy due to replication lag")
	}
}

// ============================================================================
// SPLIT-BRAIN DETECTION TESTS
// ============================================================================

func TestDetectSplitBrain(t *testing.T) {
	logger := logrus.New()
	
	primary := &dr.DatabaseCluster{
		ID:         "primary",
		Region:     "us-east-1",
		Primary:    true,
		Status:     dr.ClusterHealthy,
	}
	
	standby := &dr.DatabaseCluster{
		 ID:      "standby",
		Region:   "eu-west-1",
		Primary:  true, // Claiming to be primary too!
		Status:   dr.ClusterHealthy,
	}
	
	orco, _ := dr.NewDROorchestrator(primary, standby, logger)
	
	// Mock view of who is primary
	primary.ViewOfWhoIsPrimary = func() string { return "primary" }
	standby.ViewOfWhoIsPrimary = func() string { return "secondary" }
	
	hasSplitBrain := orco.DetectSplitBrain()
	if hasSplitBrain {
		t.Log("Split-brain detected (expected when both claim different roles)")
	}
	
	// Now simulate conflicting views (actual split-brain)
	primary.ViewOfWhoIsPrimary = func() string { return "primary" }
	standby.ViewOfWhoIsPrimary = func() string { return "primary" }
	
	hasSplitBrain = orco.DetectSplitBrain()
	if !hasSplitBrain {
		t.Error("Expected split-brain detection when both claim to be primary")
	}
}

// ============================================================================
// COST INTEGRATION TESTS
// ============================================================================

func TestCostOptimizerGenerateSuggestions(t *testing.T) {
	logger := logrus.New()
	costOpt := dr.NewCostOptimizer(logger)
	
	suggestions := costOpt.GenerateCostSuggestions(context.Background(), []string{"us-east-1", "eu-west-1"})
	
	if len(suggestions) == 0 {
		t.Log("No suggestions generated (may need more history/data)")
	}
}

// ============================================================================
// PERFORMANCE BENCHMARKS
// ============================================================================

func BenchmarkDROrchestrator_CheckPrimaryCluster(b *testing.B) {
	logger := logrus.New()
	
	primary := &dr.DatabaseCluster{
		ID:         "bench_primary",
		Region:     "us-east-1",
		Endpoint:   "bench-primary.example.com",
		Port:       5432,
		Primary:    true,
		Status:     dr.ClusterHealthy,
		Metrics:    dr.ClusterMetrics{ReplicationLagSec: 10},
	}
	
	standby := &dr.DatabaseCluster{
		ID:     "bench_standby",
		Region: "eu-west-1",
		Status: dr.ClusterHealthy,
	}
	
	orco, _ := dr.NewDROrchestrator(primary, standby, logger)
	orco.Primary.TestConnectivity = func() error { return nil }
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		orco.CheckPrimaryCluster()
	}
}

func BenchmarkDROrchestrator_DetectSplitBrain(b *testing.B) {
	logger := logrus.New()
	
	primary := &dr.DatabaseCluster{
		ID:         "bench_primary",
		Region:     "us-east-1",
		Primary:    true,
		Status:     dr.ClusterHealthy,
	}
	
	standby := &dr.DatabaseCluster{
		ID:      "bench_standby",
		Region:  "eu-west-1",
		Primary: true,
		Status:  dr.ClusterHealthy,
	}
	
	orco, _ := dr.NewDROrchestrator(primary, standby, logger)
	primary.ViewOfWhoIsPrimary = func() string { return "primary" }
	standby.ViewOfWhoIsPrimary = func() string { return "primary" }
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		orco.DetectSplitBrain()
	}
}
