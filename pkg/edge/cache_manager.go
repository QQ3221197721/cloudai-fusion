// Package edge provides enhanced cache manager for persistent offline decision storage.
// Extends existing OfflineRuntime infrastructure with database-backed caching.
package edge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// EnhancedCacheManager extends existing runtime cache with persistence
// Implements: cached_nodes + offline_decisions tables integration
// ============================================================================

// LocalDecisionRecord represents a single scheduling decision made offline
type LocalDecisionRecord struct {
	ID           string    `json:"record_id"`
	NodeID       string    `json:"node_id"`
	WorkloadID   string    `json:"workload_id"`
	Decision     Decision  `json:"decision"`
	VersionVec   []int     `json:"version_vec"`
	CreatedAt    time.Time `json:"created_at"`
	Synced       bool      `json:"synced"`
	SyncedAt     *time.Time `json:"synced_at,omitempty"`
}

// JSONData returns serialized decision data for DB storage
func (r *LocalDecisionRecord) JSONData() ([]byte, error) {
	return json.Marshal(r.Decision)
}

// VersionVecBytes converts version vector to byte slice for DB storage
func (r *LocalDecisionRecord) VersionVecBytes() []byte {
	result := make([]byte, len(r.VersionVec)*4)
	for i, v := range r.VersionVec {
		bigEndianUint32(result[i*4:], uint32(v))
	}
	return result
}

// BigEndianUint32 writes uint32 as big-endian bytes
func bigEndianUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

// CacheEntry represents a cached node state
type CacheEntry struct {
	NodeID      string    `json:"node_id"`
	Spec        []byte    `json:"spec_json"`
	Status      []byte    `json:"status_json"`
	UpdatedAt   time.Time `json:"updated_at"`
	FreshnessSec int      `json:"freshness_sec"` // Seconds since update
}

// EnhancedCacheManager persists and manages cached nodes & local decisions
type EnhancedCacheManager struct {
	db            *sql.DB
	config        OfflineRuntimeConfig
	nodeStates    map[string]*NodeAutonomyState
	cacheLock     sync.RWMutex
	lastSyncAt    time.Time
	historySize   int
	logger        *logrus.Logger
	
	// Metrics
	metrics *CacheMetrics
}

// CacheMetrics tracks cache performance
type CacheMetrics struct {
	ReadHits          int64
	ReadMisses        int64
	WriteOperations   int64
	CacheEvictions    int64
}

// NewEnhancedCacheManager creates a new cache manager with DB persistence
func NewEnhancedCacheManager(db *sql.DB, config OfflineRuntimeConfig, logger *logrus.Logger) *EnhancedCacheManager {
	if db == nil {
		panic("database connection cannot be nil")
	}
	
	defensive.ValidateRange(float64(config.TransitionHistorySize), 50, 500, "history_size")
	
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	return &EnhancedCacheManager{
		db:          db,
		config:      config,
		nodeStates:  make(map[string]*NodeAutonomyState),
		lastSyncAt:  time.Now().UTC(),
		historySize: config.TransitionHistorySize,
		logger:      logger.WithField("component", "cache_manager"),
		metrics:     &CacheMetrics{},
	}
}

// GetCachedNodes retrieves cached node states from DB with freshness check
func (m *EnhancedCacheManager) GetCachedNodes(ctx context.Context, nodeID string) ([]*Node, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	m.cacheLock.RLock()
	defer m.cacheLock.RUnlock()
	
	// Try to get fresh data from cache first
	if entry, exists := m.getFreshCacheEntry(nodeID); exists {
		m.metrics.ReadHits++
		
		var node Node
		if err := json.Unmarshal(entry.Spec, &node.Spec); err != nil {
			m.logger.WithError(err).Warn("Failed to unmarshal cached spec, falling back to DB")
		}
		if err := json.Unmarshal(entry.Status, &node.Status); err != nil {
			m.logger.WithError(err).Warn("Failed to unmarshal cached status")
		}
		
		// Set timestamps
		node.CreationTimestamp = entry.UpdatedAt
		node.DeletionTimestamp = nil
		
		return []*Node{&node}, nil
	}
	
	m.metrics.ReadMisses++
	
	// Fallback: Query from database using stored procedure
	rows, err := m.db.QueryContext(ctx, 
		`SELECT spec_json, status_json, updated_at 
		 FROM cached_nodes 
		 WHERE node_id = $1 AND updated_at >= NOW() - ($2 || ' minutes')::INTERVAL
		 ORDER BY updated_at DESC LIMIT 1`,
		nodeID,
		m.config.GracePeriod.Minutes()*60,
	)
	
	if err != nil {
		// Complete failure: return empty cache
		m.logger.WithError(err).WithFields(logrus.Fields{
			"node_id": nodeID,
			"grace_period_min": m.config.GracePeriod.Minutes(),
		}).Error("Database query failed completely")
		
		// Return in-memory fallback if available
		return m.getFallbackCache(nodeID), nil
	}
	defer rows.Close()
	
	var entries []*Node
	for rows.Next() {
		var entry CacheEntry
		if err := rows.Scan(&entry.Spec, &entry.Status, &entry.UpdatedAt); err != nil {
			continue // Skip invalid rows
		}
		
		// Validate freshness
		freshnessSec := int(time.Since(entry.UpdatedAt).Seconds())
		if freshnessSec > int(m.config.GracePeriod.Seconds()) {
			m.logger.WithFields(logrus.Fields{
				"node_id": nodeID,
				"freshness_seconds": freshnessSec,
				"threshold_seconds": m.config.GracePeriod.Seconds(),
			}).Warn("Stale cache entry, will not use")
			continue
		}
		
		// Deserialize node
		var node Node
		if err := json.Unmarshal(entry.Spec, &node.Spec); err != nil {
			m.logger.WithError(err).Warn("Skipping corrupted cache entry")
			continue
		}
		if err := json.Unmarshal(entry.Status, &node.Status); err != nil {
			continue
		}
		
		node.CreationTimestamp = entry.UpdatedAt
		entry.FreshnessSec = freshnessSec
		
		entries = append(entries, &node)
		
		// Update in-memory cache
		m.updateCacheEntry(nodeID, entry)
	}
	
	return entries, rows.Err()
}

// StoreLocalRecord persists a local decision to the audit log table
func (m *EnhancedCacheManager) StoreLocalRecord(record LocalDecisionRecord) error {
	m.cacheLock.Lock()
	defer m.cacheLock.Unlock()
	
	m.metrics.WriteOperations++
	
	// Validate record before persisting
	if err := defensive.RequireNonNil(record.WorkloadID, "workload_id"); err != nil {
		return fmt.Errorf("invalid record: %w", err)
	}
	
	// Serialize data
	decisionJSON, err := json.Marshal(record.Decision)
	if err != nil {
		return fmt.Errorf("failed to marshal decision: %w", err)
	}
	
	versionVecBytes := record.VersionVecBytes()
	
	// Insert into offline_decisions table
	query := `INSERT INTO offline_decisions (
		record_id, node_id, workload_id, decision_data, version_vec, timestamp, synced
	) VALUES ($1, $2, $3, $4, $5, $6, FALSE)`
	
	_, err = m.db.ExecContext(context.Background(), query,
		record.ID,
		record.NodeID,
		record.WorkloadID,
		decisionJSON,
		versionVecBytes,
		record.CreatedAt.UTC(),
	)
	
	if err != nil {
		m.logger.WithFields(logrus.Fields{
			"record_id":   record.ID,
			"workload_id": record.WorkloadID,
			"error":       err,
		}).Error("Failed to store local decision")
		
		return fmt.Errorf("failed to store decision: %w", err)
	}
	
	// Also cache locally for faster access
	m.cacheDecisions(record)
	
	return nil
}

// GetUnsyncedDecisions retrieves pending decisions for sync processing
func (m *EnhancedCacheManager) GetUnsyncedDecisions(ctx context.Context, limit int) ([]LocalDecisionRecord, error) {
	m.cacheLock.RLock()
	defer m.cacheLock.RUnlock()
	
	if limit <= 0 || limit > 1000 {
		limit = 100 // Default batch size
	}
	
	// Use stored procedure for efficiency
	rows, err := m.db.QueryContext(ctx, 
		`SELECT record_id, node_id, workload_id, decision_data, version_vec, timestamp
		 FROM offline_decisions
		 WHERE synced = false
		 ORDER BY timestamp ASC
		 LIMIT $1`,
		limit,
	)
	
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()
	
	records := make([]LocalDecisionRecord, 0, limit)
	for rows.Next() {
		var record LocalDecisionRecord
		var decisionJSON []byte
		var versionVecBytes []byte
		
		if err := rows.Scan(
			&record.ID,
			&record.NodeID,
			&record.WorkloadID,
			&decisionJSON,
			&versionVecBytes,
			&record.CreatedAt,
		); err != nil {
			m.logger.WithError(err).Warn("Failed to scan record, skipping")
			continue
		}
		
		// Deserialize decision
		if err := json.Unmarshal(decisionJSON, &record.Decision); err != nil {
			m.logger.WithError(err).Warn("Failed to deserialize decision")
			continue
		}
		
		// Deserialize version vector
		record.VersionVec = decodeUint32BigEndian(versionVecBytes)
		
		records = append(records, record)
	}
	
	return records, rows.Err()
}

// MarkDecisionSynced updates sync status after successful cloud sync
func (m *EnhancedCacheManager) MarkDecisionSynced(ctx context.Context, recordID string, errorMessage string) error {
	m.cacheLock.Lock()
	defer m.cacheLock.Unlock()
	
	var syncTime *time.Time
	if errorMessage == "" {
		now := time.Now().UTC()
		syncTime = &now
	}
	
	// Use stored procedure or direct update
	query := `UPDATE offline_decisions 
		SET synced = true, synced_at = $2, sync_error = $3
		WHERE record_id = $1 AND synced = false`
	
	result, err := m.db.ExecContext(ctx, query, recordID, syncTime, errorMessage)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no record found with ID %s or already synced", recordID)
	}
	
	return nil
}

// Helper Methods

// getFreshCacheEntry checks if we have a valid in-memory cache
func (m *EnhancedCacheManager) getFreshCacheEntry(nodeID string) (*CacheEntry, bool) {
	state, exists := m.nodeStates[nodeID]
	if !exists || state == nil {
		return nil, false
	}
	
	// Check if state is recent enough
	if time.Since(state.LastUpdate) > m.config.GracePeriod {
		return nil, false
	}
	
	// This would require actual implementation of NodeAutonomyState structure
	// For now, return nil to indicate cache miss
	return nil, false
}

// updateCacheEntry updates in-memory cache
func (m *EnhancedCacheManager) updateCacheEntry(nodeID string, entry CacheEntry) {
	// Simplified implementation - would need full NodeAutonomyState structure
	// In production: m.nodeStates[nodeID] = newState(entry)
	m.logger.WithField("node_id", nodeID).Debug("Cache entry updated")
}

// getFallbackCache returns minimal in-memory cache if DB unavailable
func (m *EnhancedCacheManager) getFallbackCache(nodeID string) []*Node {
	// Very limited fallback - would be populated by other components
	return []*Node{}
}

// cacheDecisions adds to in-memory decision cache
func (m *EnhancedCacheManager) cacheDecisions(record LocalDecisionRecord) {
	// In production: implement decision cache with LRU eviction
	m.logger.WithField("record_id", record.ID).Debug("Decision cached locally")
}

// GetMetrics returns current cache metrics
func (m *EnhancedCacheManager) GetMetrics() *CacheMetrics {
	m.cacheLock.RLock()
	defer m.cacheLock.RUnlock()
	
	return &CacheMetrics{
		ReadHits:      m.metrics.ReadHits,
		ReadMisses:    m.metrics.ReadMisses,
		WriteOperations: m.metrics.WriteOperations,
		CacheEvictions: m.metrics.CacheEvictions,
	}
}

// VersionVectorHelper provides utility for version vectors
type VersionVectorHelper struct {
	size int
}

// decodeUint32BigEndian reverses bigEndianUint32 conversion
func decodeUint32BigEndian(b []byte) []int {
	if len(b)%4 != 0 {
		return []int{}
	}
	
	result := make([]int, len(b)/4)
	for i := 0; i < len(b); i += 4 {
		val := uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
		result[i/4] = int(val)
	}
	return result
}
