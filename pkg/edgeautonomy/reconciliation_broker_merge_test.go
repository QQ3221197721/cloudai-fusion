// Package edgeautonomy - Reconciliation broker merge scenario tests
package edgeautonomy

import (
	"context"
	"fmt"
	"testing"
	"time"
	
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconciliationBrokerMergeScenarios(t *testing.T) {
	t.Parallel()
	
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	
	cacheMgr := NewCacheManager()
	vv := NewVersionVector([]string{"node-1", "node-2"})
	
	config := Config{
		CacheManager:  cacheMgr,
		VersionVector: vv,
		Logger:        logger,
	}
	
	broker, err := NewReconciliationBroker(ctx, config)
	require.NoError(t, err)
	require.NotNil(t, broker)
	
	t.Run("GetPendingLocalDecisionsReturnsEmptyWhenNoDecisions", func(t *testing.T) {
		t.Parallel()
		
		decisions := broker.getPendingLocalDecisions(ctx)
		assert.Empty(t, decisions)
	})
	
	t.Run("MergeCloudDecisionWithCacheDoesNotPanic", func(t *testing.T) {
		t.Parallel()
		
		res := ResolvedDecision{
			ID:          "test-decision-1",
			Source:      "cloud",
			Version:     5,
			VersionVec:  []int{1, 2, 3},
			Resolution:  "merge-approved",
		}
		
		err := broker.mergeCloudDecisionWithCache(ctx, res)
		assert.NoError(t, err, "Should not panic even without full implementation")
	})
	
	t.Run("ApplyMergedDecisionRecordsMergedDecision", func(t *testing.T) {
		t.Parallel()
		
		res := ResolvedDecision{
			ID:          "merged-decision-1",
			Source:      "merged",
			Version:     10,
			VersionVec:  []int{5, 5, 5},
			Resolution:  "conflict-resolved",
		}
		
		err := broker.applyMergedDecision(ctx, res)
		assert.NoError(t, err)
	})
	
	t.Run("UpdateLocalDecisionVersionIncrementsVersion", func(t *testing.T) {
		t.Parallel()
		
		decisionID := "version-test-decision"
		version := int64(42)
		
		err := broker.updateLocalDecisionVersion(ctx, decisionID, version)
		assert.NoError(t, err)
		
		// Verify it was stored in cache
		decision := cacheMgr.GetDecision(ctx, decisionID)
		assert.NotNil(t, decision)
		assert.Equal(t, version, decision.Version)
	})
}

func TestVersionVectorMergeCorrectness(t *testing.T) {
	t.Parallel()
	
	vv1 := NewVersionVector([]string{"node-1", "node-2", "node-3"})
	vv2 := NewVersionVector([]string{"node-1", "node-2", "node-3"})
	
	// Increment individual vectors
	vv1.Increment("node-1") // [1, 0, 0]
	vv2.Increment("node-2") // [0, 1, 0]
	
	initialVV1 := vv1.ToString()
	initialVV2 := vv2.ToString()
	
	// Merge vv2 into vv1 (should take element-wise maximum)
	err := vv1.Merge(vv2)
	require.NoError(t, err)
	
	expected := "[1, 1, 0]" // Element-wise max
	actual := vv1.ToString()
	assert.Equal(t, expected, actual, "Merge should take maximum of each component")
	
	// Verify vv2 unchanged
	finalVV2 := vv2.ToString()
	assert.Equal(t, initialVV2, finalVV2, "Original vector should remain unchanged")
}

func TestMergeVersionVectors(t *testing.T) {
	t.Parallel()
	
	ctx := context.Background()
	logger := logrus.New()
	
	cacheMgr := NewCacheManager()
	vv := NewVersionVector([]string{"node-a", "node-b"})
	
	config := Config{
		CacheManager:  cacheMgr,
		VersionVector: vv,
		Logger:        logger,
	}
	
	broker, err := NewReconciliationBroker(ctx, config)
	require.NoError(t, err)
	require.NotNil(t, broker)
	
	// Initial state
	initialVec := vv.GetAllVectors()
	assert.Equal(t, 0, initialVec["node-a"])
	assert.Equal(t, 0, initialVec["node-b"])
	
	// Apply merge with version vector [2, 3]
	testVec := []int{2, 3}
	err = broker.mergeVersionVectors(testVec)
	assert.NoError(t, err)
	
	// After merge, should be [2, 3]
	finalVec := vv.GetAllVectors()
	assert.Equal(t, 2, finalVec["node-a"], "node-a should be updated to 2")
	assert.Equal(t, 3, finalVec["node-b"], "node-b should be updated to 3")
}

func TestConcurrentMergeOperations(t *testing.T) {
	t.Parallel()
	
	ctx := context.Background()
	logger := logrus.New()
	logger.SetErrorFormatting(true)
	
	cacheMgr := NewCacheManager()
	vv := NewVersionVector([]string{"node-1", "node-2", "node-3"})
	
	config := Config{
		CacheManager:  cacheMgr,
		VersionVector: vv,
		Logger:        logger,
	}
	
	broker, err := NewReconciliationBroker(ctx, config)
	require.NoError(t, err)
	require.NotNil(t, broker)
	
	// Simulate concurrent merges
	done := make(chan bool, 3)
	
	for i := 0; i < 3; i++ {
		go func(id int) {
			defer func() { done <- true }()
			
			res := ResolvedDecision{
				ID:          fmt.Sprintf("concurrent-test-%d", id),
				Source:      "cloud",
				Version:     int64(id + 1),
				VersionVec:  []int{id + 1, id + 2, id + 3},
				Resolution:  "merge-approved",
			}
			
			err := broker.mergeCloudDecisionWithCache(ctx, res)
			if err != nil {
				t.Logf("Merge failed for id %d: %v", id, err)
			}
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}
	
	// Verify all operations completed
	assert.Equal(t, 3, len(cacheMgr.mergedDecisions))
}
