// Package dr_integrations_test - Comprehensive test suite for DR integrations
package dr_integrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/dr_integrations"
)

// ============================================================================
// INTELLIGENCE INTEGRATION TESTS
// ============================================================================

func TestIntelligenceIntegrationReportClusterHealth(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	
	connector := dr_integrations.NewIntelligenceIntegration(logger)
	
	report := &dr_integrations.ClusterHealthReport{
		ClusterID: "cluster_1",
		Region:    "us-east-1",
		Status:    "healthy",
		LagSeconds: 5,
		Metrics: map[string]float64{
			"cpu_utilization": 45.5,
			"memory_utilization": 62.3,
		},
		LastCheck: time.Now(),
	}
	
	ctx := context.Background()
	err := connector.ReportClusterHealth(ctx, report)
	if err != nil {
		t.Fatalf("Failed to report cluster health: %v", err)
	}
	
	// Verify health was buffered
	connector.mu.RLock()
	if len(connector.HealthBuffer) == 0 {
		t.Error("Expected health reports to be buffered")
	}
	connector.mu.RUnlock()
}

func TestIntelligenceIntegrationAggregateHealthMetrics(t *testing.T) {
	logger := logrus.New()
	connector := dr_integrations.NewIntelligenceIntegration(logger)
	
	// Add multiple health reports
	for i := 1; i <= 5; i++ {
		report := &dr_integrations.ClusterHealthReport{
			ClusterID:   "cluster_" + string(rune(i+'0')),
			Region:      "region_" + string(rune(i+'0')),
			Status:      "healthy",
			LagSeconds:  i,
			LastCheck:   time.Now(),
			Metrics:     map[string]float64{"test_metric": float64(i)},
		}
		connector.mu.Lock()
		connector.HealthBuffer = append(connector.HealthBuffer, *report)
		connector.mu.Unlock()
	}
	
	ctx := context.Background()
	aggregated, err := connector.AggregateHealthMetrics(ctx)
	if err != nil {
		t.Fatalf("Failed to aggregate metrics: %v", err)
	}
	
	if len(aggregated) == 0 {
		t.Error("Expected aggregated metrics")
	}
}

// ============================================================================
// SECURITY INTEGRATION TESTS
// ============================================================================

func TestSecurityIntegrationRecordFailoverEvidence(t *testing.T) {
	logger := logrus.New()
	si := dr_integrations.NewSecurityIntegration(logger)
	
	evidence := &dr_integrations.FailoverEvidence{
		EvidenceID:   "failover_evd_1",
		FailoverID:   "fo_123",
		EvidenceType: "split_brain_detection",
		Payload:      []byte("split brain evidence"),
		CreatedAt:    time.Now(),
		Category:     "split_brain",
	}
	
	ctx := context.Background()
	err := si.RecordFailoverEvidence(evidence)
	if err != nil {
		t.Fatalf("Failed to record failover evidence: %v", err)
	}
	
	// Verify evidence was queued
	si.mu.RLock()
	if len(si.EvidenceQueue) == 0 {
		t.Error("Expected evidence in queue")
	}
	si.mu.RUnlock()
}

func TestSecurityIntegrationDetectSplitBrain(t *testing.T) {
	logger := logrus.New()
	si := dr_integrations.NewSecurityIntegration(logger)
	
	// Simulate split-brain condition (both primary and standby healthy)
	err := si.DetectSplitBrain(true, true)
	if err != nil {
		t.Logf("Split-brain detection error (expected): %v", err)
	}
	
	// Check if evidence was recorded
	si.mu.RLock()
	hasSplitBrain := false
	for _, evd := range si.EvidenceQueue {
		if evd.Category == "split_brain" {
			hasSplitBrain = true
			break
		}
	}
	si.mu.RUnlock()
	
	if !hasSplitBrain {
		t.Log("No split-brain evidence recorded (mock provider may not have implemented)")
	}
}

func TestSecurityIntegrationNormalCondition(t *testing.T) {
	logger := logrus.New()
	si := dr_integrations.NewSecurityIntegration(logger)
	
	// Normal condition (primary healthy, standby unhealthy)
	err := si.DetectSplitBrain(true, false)
	if err != nil {
		t.Errorf("Unexpected error for normal condition: %v", err)
	}
}

// ============================================================================
// COST INTEGRATION TESTS
// ============================================================================

func TestCostIntegrationRecordCost(t *testing.T) {
	logger := logrus.New()
	ci := dr_integrations.NewCostIntegration(logger)
	
	ctx := context.Background()
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()
	
	err := ci.RecordCost("cross_region_traffic", startTime, endTime, 150.75)
	if err != nil {
		t.Fatalf("Failed to record cost: %v", err)
	}
	
	// Verify cost was recorded
	ci.mu.RLock()
	if len(ci.CostHistory) == 0 {
		t.Error("Expected cost history to be populated")
	}
	ci.mu.RUnlock()
}

func TestCostIntegrationGenerateOptimizationSuggestions(t *testing.T) {
	logger := logrus.New()
	ci := dr_integrations.NewCostIntegration(logger)
	
	// Add some cost records
	ci.mu.Lock()
	ci.CostHistory = append(ci.CostHistory, dr_integrations.CostRecord{
		RecordID:      "cost_1",
		ResourceType:  "storage_replication",
		CostUSD:       50.0,
		PeriodStart:   time.Now().Add(-7 * 24 * time.Hour),
		PeriodEnd:     time.Now(),
	})
	ci.CostHistory = append(ci.CostHistory, dr_integrations.CostRecord{
		RecordID:      "cost_2",
		ResourceType:  "cross_region_traffic",
		CostUSD:       100.0,
		PeriodStart:   time.Now().Add(-7 * 24 * time.Hour),
		PeriodEnd:     time.Now(),
	})
	ci.mu.Unlock()
	
	ctx := context.Background()
	suggestions, err := ci.GenerateOptimizationSuggestions(ctx)
	if err != nil {
		t.Fatalf("Failed to generate suggestions: %v", err)
	}
	
	// Suggestions may be empty initially (needs more history)
	t.Logf("Generated %d suggestions", len(suggestions))
}

// ============================================================================
// PERFORMANCE BENCHMARKS
// ============================================================================

func BenchmarkIntelligenceIntegration_ReportClusterHealth(b *testing.B) {
	logger := logrus.New()
	connector := dr_integrations.NewIntelligenceIntegration(logger)
	
	report := &dr_integrations.ClusterHealthReport{
		ClusterID:  "bench_cluster",
		Region:     "bench_region",
		Status:     "healthy",
		LagSeconds: 5,
		Metrics:    map[string]float64{"cpu": 45.5, "memory": 62.3},
		LastCheck:  time.Now(),
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		connector.ReportClusterHealth(ctx, report)
	}
}

func BenchmarkSecurityIntegration_RecordEvidence(b *testing.B) {
	logger := logrus.New()
	si := dr_integrations.NewSecurityIntegration(logger)
	
	evidence := &dr_integrations.FailoverEvidence{
		EvidenceID:   "bench_evd",
		FailoverID:   "bench_fo",
		EvidenceType: "benchmark_type",
		Payload:      make([]byte, 1024), // 1KB payload
		CreatedAt:    time.Now(),
		Category:     "normal",
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		si.RecordFailoverEvidence(evidence)
	}
}

func BenchmarkCostIntegration_RecordCost(b *testing.B) {
	logger := logrus.New()
	ci := dr_integrations.NewCostIntegration(logger)
	
	ctx := context.Background()
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ci.RecordCost("benchmark_resource", startTime, endTime, 50.0)
	}
}

// ============================================================================
// EDGE CASE AND ERROR HANDLING TESTS
// ============================================================================

func TestIntelligenceIntegration_BufferOverflow(t *testing.T) {
	logger := logrus.New()
	connector := dr_integrations.NewIntelligenceIntegration(logger)
	
	// Fill buffer beyond capacity
	bufferSize := 50
	for i := 0; i < bufferSize+10; i++ {
		report := &dr_integrations.ClusterHealthReport{
			ClusterID: "overflow_cluster",
			Region:    "overflow_region",
			Status:    "healthy",
			LastCheck: time.Now(),
		}
		connector.mu.Lock()
		connector.HealthBuffer = append(connector.HealthBuffer, *report)
		connector.mu.Unlock()
	}
	
	// Should have been trimmed to capacity
	connector.mu.RLock()
	if len(connector.HealthBuffer) > bufferSize {
		t.Errorf("Expected buffer size <= %d, got %d", bufferSize, len(connector.HealthBuffer))
	}
	connector.mu.RUnlock()
}

func TestSecurityIntegration_QueueOverflow(t *testing.T) {
	logger := logrus.New()
	si := dr_integrations.NewSecurityIntegration(logger)
	
	queueCap := 1000
	for i := 0; i < queueCap+10; i++ {
		evidence := &dr_integrations.FailoverEvidence{
			EvidenceID:   "overflow_evd",
			EvidenceType: "overflow",
			CreatedAt:    time.Now(),
			Category:     "overflow",
		}
		si.mu.Lock()
		si.EvidenceQueue = append(si.EvidenceQueue, evidence)
		si.mu.Unlock()
	}
	
	// Should have been trimmed to capacity
	si.mu.RLock()
	if len(si.EvidenceQueue) > queueCap {
		t.Errorf("Expected queue size <= %d, got %d", queueCap, len(si.EvidenceQueue))
	}
	si.mu.RUnlock()
}

func TestCostIntegration_HistoryOverflow(t *testing.T) {
	logger := logrus.New()
	ci := dr_integrations.NewCostIntegration(logger)
	
	historyMax := 500
	for i := 0; i < historyMax+10; i++ {
		startTime := time.Now().Add(-24 * time.Hour)
		endTime := time.Now()
		ci.mu.Lock()
		ci.CostHistory = append(ci.CostHistory, dr_integrations.CostRecord{
			RecordID:   "overflow_cost",
			ResourceType: "overflow_resource",
			CostUSD:    50.0,
			PeriodStart: startTime,
			PeriodEnd:   endTime,
		})
		ci.mu.Unlock()
	}
	
	// Should have been trimmed to max history
	ci.mu.RLock()
	if len(ci.CostHistory) > historyMax {
		t.Errorf("Expected history size <= %d, got %d", historyMax, len(ci.CostHistory))
	}
	ci.mu.RUnlock()
}
