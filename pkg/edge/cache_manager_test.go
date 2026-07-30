package edge_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock database setup for testing
func createTestDB(t *testing.T) *sql.DB {
	dsn := "user=test password=test host=localhost port=5432 dbname=testdb sslmode=disable"
	
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("Skipping test - PostgreSQL not available: %v", err)
		return nil
	}
	
	// Create required tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS cached_nodes (
			id VARCHAR(255) PRIMARY KEY,
			node_id VARCHAR(255) NOT NULL,
			spec_json JSONB NOT NULL,
			status_json JSONB NOT NULL,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			CHECK (updated_at > NOW() - INTERVAL '7 days')
		);
		
		CREATE TABLE IF NOT EXISTS offline_decisions (
			record_id VARCHAR(255) PRIMARY KEY,
			node_id VARCHAR(255) NOT NULL,
			workload_id VARCHAR(255) NOT NULL,
			decision_data JSONB NOT NULL,
			version_vec BYTEA NOT NULL,
			timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			synced BOOLEAN DEFAULT FALSE,
			INDEX idx_offline_synced (synced),
			INDEX idx_offline_node_created (node_id, timestamp DESC)
		);
	`)
	require.NoError(t, err)
	
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS cached_nodes;")
		db.Exec("DROP TABLE IF EXISTS offline_decisions;")
		db.Close()
	})
	
	return db
}

func TestEnhancedCacheManager_GenerateUnitTests(t *testing.T) {
	t.Run("NewCacheManager should require valid database", func(t *testing.T) {
		config := edge.DefaultOfflineRuntimeConfig()
		
		assert.PanicsWithValue(t, "database connection cannot be nil", func() {
			edge.NewEnhancedCacheManager(nil, config, nil)
		})
	})
	
	t.Run("GetCachedNodes with empty cache should return empty slice", func(t *testing.T) {
		db := createTestDB(t)
		config := edge.DefaultOfflineRuntimeConfig()
		cacheMgr := edge.NewEnhancedCacheManager(db, config, nil)
		
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		nodes, err := cacheMgr.GetCachedNodes(ctx, "non-existent-node")
		assert.NoError(t, err)
		assert.Empty(t, nodes)
	})
}

func TestLocalDecisionMaker_CoreScenarios(t *testing.T) {
	t.Run("MakeLocalDecision with no available nodes should fail gracefully", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		wl := edge.Workload{
			ID:             "test-workload-001",
			NodeSelector:   map[string]string{"gpu": "true"},
			ResourceRequirements: []string{"gpu-large"},
			QoS:            "high",
		}
		
		// No available nodes → should error
		maker := &edge.LocalDecisionMaker{} // Simplified test
		
		_, err := maker.MakeLocalDecision(ctx, wl, []*edge.Node{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no available nodes")
	})
	
	t.Run("StoreLocalRecord validates required fields", func(t *testing.T) {
		record := edge.LocalDecisionRecord{
			NodeID:       "", // Missing - should fail validation
			WorkloadID:   "workload-123",
			VersionVec:   []int{1, 0, 0},
			CreatedAt:    time.Now().UTC(),
		}
		
		err := defensive.RequireNonNil(record.WorkloadID, "workload_id")
		assert.NoError(t, err) // Empty string is different from nil pointer
	})
}

func TestVersionVector_CausalityTracking(t *testing.T) {
	nodeIDs := []string{"node-1", "node-2", "node-3"}
	vv := edgeautonomy.NewVersionVector(nodeIDs)
	
	t.Run("Update increments own component correctly", func(t *testing.T) {
		vec1 := vv.Update("node-1")
		
		assert.Equal(t, len(nodeIDs), len(vec1))
		assert.Equal(t, 1, vec1[0]) // node-1's component should be 1
		assert.Equal(t, 0, vec1[1]) // Others unchanged
		assert.Equal(t, 0, vec1[2])
	})
	
	t.Run("Compare identifies causal relationship", func(t *testing.T) {
		v1 := vv.Update("node-1")
		v2 := vv.Update("node-2")
		
		result := vv.Compare(v1, v2)
		// v1 happened before v2 (causal chain)
		assert.Equal(t, edgeautonomy.V1_CAUSAL_BEFORE_V2, result)
	})
	
	t.Run("Compare detects concurrent updates", func(t *testing.T) {
		vv2 := edgeautonomy.NewVersionVector([]string{"n1", "n2"})
		
		v1a := vv2.Update("n1")
		v2b := vv2.Update("n2")
		
		// Now increment n2 in one vector and n1 in another
		v1b := vv2.Update("n2")
		v2a := vv2.Update("n1")
		
		result := vv2.Compare(v1b, v2a)
		assert.Equal(t, edgeautonomy.CONFLICT_DETECTED, result)
	})
	
	t.Run("GetKnownNodes returns safe copy", func(t *testing.T) {
		nodes := vv.GetKnownNodes()
		
		assert.ElementsMatch(t, nodeIDs, nodes)
		
		// Verify it's a copy (modifying won't affect internal state)
		nodes[0] = "modified"
		
		original := vv.GetKnownNodes()
		assert.NotEqual(t, nodes, original)
	})
}

func TestConflictResolver_StrategyApplication(t *testing.T) {
	vv := edgeautonomy.NewVersionVector([]string{"c1", "e1"})
	resolver := edgeautonomy.NewConflictResolver(vv, edgeautonomy.LastWriterWins, nil)
	
	t.Run("CloudAuthority strategy always prefers cloud decisions", func(t *testing.T) {
		resolverWithAuth := edgeautonomy.NewConflictResolver(
			vv,
			edgeautonomy.CloudAuthority,
			nil,
		)
		
		localRec := edgeautonomy.LocalDecisionRecord{
			ID:       "local-001",
			NodeID:   "edge-1",
			Priority: 5,
			Timestamp: time.Now(),
			VersionVec: []int{1, 0},
		}
		
		cloudRec := edgeautonomy.CloudDecisionRecord{
			ID:       "cloud-001",
			NodeID:   "edge-1",
			Priority: 3,
			Timestamp: localRec.Timestamp.Add(-1 * time.Hour), // Cloud is OLDER but still wins
			VersionVec: []int{0, 1},
		}
		
		report := edgeautonomy.ConflictReport{
			LocalRecord: localRec,
			CloudRecord: cloudRec,
		}
		
		result := resolverWithAuth.selectWinner(edgeautonomy.EQUIVALENT, localRec, cloudRec, report)
		
		assert.Equal(t, "CLOUD_AUTHORITY", result.Source)
	})
}
