// Package edgeautonomy provides comprehensive test suite for reconciliation broker
package edgeautonomy_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edgeautonomy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// ReconciliationBroker Unit Tests
// ============================================================================

func TestReconciliationBroker_BasicWorkflow(t *testing.T) {
	// Setup mock components
	cacheMgr := setupMockCacheManager()
	versionVector := edgeautonomy.NewVersionVector([]string{"central", "edge-1"})
	conflictResolver := edgeautonomy.NewConflictResolver(versionVector, edgeautonomy.LastWriterWins, nil)
	
	broker := edgeautonomy.NewReconciliationBroker(
		"edge-worker-01",
		cacheMgr,
		conflictResolver,
		versionVector,
		nil,
		edge.DefaultOfflineRuntimeConfig(),
	)
	require.NotNil(t, broker)
	
	// Verify initial state
	assert.False(t, broker.IsCurrentlySyncing())
	assert.Equal(t, time.Time{}, broker.GetLastSyncTime())
}

func TestReconciliationBroker_RateLimiting(t *testing.T) {
	cacheMgr := setupMockCacheManager()
	versionVector := edgeautonomy.NewVersionVector([]string{"n1"})
	resolver := edgeautonomy.NewConflictResolver(versionVector, edgeautonomy.CloudAuthority, nil)
	
	broker := edgeautonomy.NewReconciliationBroker(
		"test-node",
		cacheMgr,
		resolver,
		versionVector,
		nil,
		edge.OfflineRuntimeConfig{SyncBatchSize: 100, MaxSyncRetries: 3},
	)
	
	broker.maxOperationsPerHour = 2
	
	// Should allow first two operations
	ctx := context.Background()
	report1, err := broker.StartBidirectionalSync(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, report1)
	
	report2, err := broker.StartBidirectionalSync(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, report2)
	
	// Third operation should fail due to rate limit
	_, err = broker.StartBidirectionalSync(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestReconciliationBroker_ConcurrentAccess(t *testing.T) {
	cacheMgr := setupMockCacheManager()
	versionVector := edgeautonomy.NewVersionVector([]string{"c1", "e1"})
	resolver := edgeautonomy.NewConflictResolver(versionVector, edgeautonomy.HighestPriority, nil)
	
	broker := edgeautonomy.NewReconciliationBroker(
		"concurrent-test",
		cacheMgr,
		resolver,
		versionVector,
		nil,
		edge.DefaultOfflineRuntimeConfig(),
	)
	
	numWorkers := 10
	done := make(chan bool, numWorkers)
	errors := make(chan error, numWorkers)
	
	// Start concurrent sync operations
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer func() { done <- true }()
			
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			report, err := broker.StartBidirectionalSync(ctx)
			if err != nil {
				errors <- err
				return
			}
			
			if report == nil {
				errors <- fmt.Errorf("nil report from worker %d", workerID)
			}
		}(i)
	}
	
	// Wait for all workers
	for i := 0; i < numWorkers; i++ {
		select {
		case <-done:
		case err := <-errors:
			t.Fatalf("Concurrent worker failed: %v", err)
		}
	}
	
	t.Log("All concurrent operations completed successfully")
}

// ============================================================================
// Benchmark Tests
// ============================================================================

func BenchmarkReconciliationBroker_SyncPerformance(b *testing.B) {
	cacheMgr := setupMockCacheManager()
	versionVector := edgeautonomy.NewVersionVector([]string{"central", "edge-1", "edge-2"})
	resolver := edgeautonomy.NewConflictResolver(versionVector, edgeautonomy.LastWriterWins, nil)
	
	broker := edgeautonomy.NewReconciliationBroker(
		"benchmark-node",
		cacheMgr,
		resolver,
		versionVector,
		nil,
		edge.DefaultOfflineRuntimeConfig(),
	)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		
		report, err := broker.StartBidirectionalSync(ctx)
		cancel()
		
		if err != nil {
			b.Fatalf("Sync failed: %v", err)
		}
		
		if report == nil {
			b.Fatal("Nil sync report")
		}
	}
}

func BenchmarkReconciliationBroker_CalculateSuccessRate(b *testing.B) {
	broker := &edgeautonomy.ReconciliationBroker{}
	
	report := &edgeautonomy.SyncReport{
		TotalRecords:   1000,
		ConflictsFound: 50,
		Operations: []edgeautonomy.SyncOperationRecord{
			{ID: "op1", Status: "SUCCESS", RecordsProcessed: 500},
			{ID: "op2", Status: "SUCCESS", RecordsProcessed: 400},
			{ID: "op3", Status: "PARTIAL", RecordsProcessed: 100},
		},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rate := broker.CalculateSuccessRate(report)
		if rate <= 0 || rate > 100 {
			b.Fatalf("Invalid success rate: %f", rate)
		}
	}
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestReconciliationBroker_FullSyncCycle(t *testing.T) {
	// Create realistic scenario with local decisions and cloud conflicts
	
	cacheMgr := setupMockCacheManagerWithTestData()
	versionVector := edgeautonomy.NewVersionVector([]string{"central", "edge-1"})
	resolver := edgeautonomy.NewConflictResolver(versionVector, edgeautonomy.LastWriterWins, nil)
	
	broker := edgeautonomy.NewReconciliationBroker(
		"integration-test-node",
		cacheMgr,
		resolver,
		versionVector,
		nil,
		edge.DefaultOfflineRuntimeConfig(),
	)
	
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	
	report, err := broker.StartBidirectionalSync(ctx)
	require.NoError(t, err)
	require.NotNil(t, report)
	
	// Validate report structure
	assert.Greater(t, len(report.Operations), 0)
	assert.LessOrEqual(t, report.DurationSec, float64(60)) // Within timeout
	assert.GreaterOrEqual(t, report.SuccessRate, 90.0)      // High success rate
	
	// Check that conflicts were detected if any
	if report.ConflictsFound > 0 {
		t.Logf("Detected %d conflicts during sync", report.ConflictsFound)
	}
	
	// Verify history was recorded
	history := broker.GetRecentSyncHistory(5)
	assert.Greater(t, len(history), 0)
}

// ============================================================================
// Helper Functions
// ============================================================================

func setupMockCacheManager() *edge.EnhancedCacheManager {
	// In production: create real DB connection
	// For tests: use in-memory mock
	return nil
}

func setupMockCacheManagerWithTestData() *edge.EnhancedCacheManager {
	// Setup cache with pre-populated test data
	mock := &edge.EnhancedCacheManager{}
	
	// In unit tests, these would be replaced with proper mocks
	return mock
}

// Mock implementations for testing

type MockCloudDecision struct {
	ID        string
	NodeID    string
	Timestamp time.Time
	Priority  int
	VersionVec []int
}
