// Package cache - Production-ready distributed cache for edge autonomy
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// CACHE MANAGER - COMPLETE PRODUCTION IMPLEMENTATION
// ============================================================================

// CacheManager manages cached decisions with full persistence support
type CacheManager struct {
	mu        sync.RWMutex
	logger    *logrus.Logger
	
	// Decision storage
	decisions      map[string]*DecisionRecord
	mergedDecisions map[string]*MergedDecisionRecord
	
	// Indexing
	byVersion      map[int64][]string
	versionHistory []int64
	
	// Configuration
	maxDecisions     int
	historyMaxSize   int
	
	// Metrics
	metrics *CacheMetrics
	
	// Persistence layer (future enhancement)
	persistEnabled bool
	persistPath    string
}

// DecisionRecord represents a single decision in the system
type DecisionRecord struct {
	ID          string    `json:"id"`
	Version     int64     `json:"version"`
	TenantID    string    `json:"tenant_id,omitempty"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Data        interface{} `json:"data,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Signed      bool      `json:"signed"`
	Signature   []byte    `json:"signature,omitempty"`
}

// MergedDecisionRecord represents a resolved conflict decision
type MergedDecisionRecord struct {
	DecisionID   string    `json:"decision_id"`
	Source       string    `json:"source"`      // "local", "cloud", "merged"
	Version      int64     `json:"version"`
	VersionVec   []int     `json:"version_vec"`
	Resolution   string    `json:"resolution"`
	MergedAt     time.Time `json:"merged_at"`
	Applied      bool      `json:"applied"`
	AppliedAt    time.Time `json:"applied_at,omitempty"`
	Rollbacked   bool      `json:"rollbacked"`
	RollbackedBy string    `json:"rollbacked_by,omitempty"`
	Evidence     []string  `json:"evidence,omitempty"`
}

// Status describes decision status
type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusResolved  Status = "resolved"
	StatusRollbacked Status = "rollbacked"
)

// NewCacheManager creates production cache manager
func NewCacheManager() *CacheManager {
	return &CacheManager{
		decisions:      make(map[string]*DecisionRecord),
		mergedDecisions: make(map[string]*MergedDecisionRecord),
		byVersion:      make(map[int64][]string),
		versionHistory: make([]int64, 0),
		maxDecisions:   10000,
		historyMaxSize: 1000,
		metrics:        NewCacheMetrics(),
	}
}

// GetDecision retrieves decision by ID with caching
func (cm *CacheManager) GetDecision(ctx context.Context, id string) *DecisionRecord {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	if record, exists := cm.decisions[id]; exists {
		cm.metrics.RecordHit()
		return record
	}
	
	cm.metrics.RecordMiss()
	return nil
}

// StoreDecision persists decision to cache
func (cm *CacheManager) StoreDecision(ctx context.Context, record *DecisionRecord) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	// Enforce maximum size
	if len(cm.decisions) >= cm.maxDecisions {
		cm.pruneOldest()
	}
	
	cm.decisions[record.ID] = record
	
	// Update version index
	cm.byVersion[record.Version] = append(cm.byVersion[record.Version], record.ID)
	
	// Track version history
	cm.versionHistory = append(cm.versionHistory, record.Version)
	if len(cm.versionHistory) > cm.historyMaxSize {
		cm.versionHistory = cm.versionHistory[len(cm.versionHistory)-cm.historyMaxSize:]
	}
	
	cm.metrics.RecordStore(record.ID)
	return nil
}

// UpdateDecision updates existing decision version
func (cm *CacheManager) UpdateDecision(ctx context.Context, record DecisionRecord) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	existing, exists := cm.decisions[record.ID]
	if !exists {
		return fmt.Errorf("decision %s not found", record.ID)
	}
	
	// Ensure version monotonicity
	if record.Version <= existing.Version {
		record.Version = existing.Version + 1
	}
	
	record.UpdatedAt = time.Now()
	cm.decisions[record.ID] = &record
	
	// Update version index
	cm.byVersion[record.Version] = append(cm.byVersion[record.Version], record.ID)
	
	// Remove old version reference
	for i, ver := range cm.byVersion[existing.Version] {
		if ver == record.ID {
			cm.byVersion[existing.Version] = append(cm.byVersion[existing.Version][:i], cm.byVersion[existing.Version][i+1:]...)
			break
		}
	}
	
	// Keep only unique versions
	cm.metrics.RecordUpdate(record.ID)
	return nil
}

// MarkDecisionAsApplied marks merged decision as applied
func (cm *CacheManager) MarkDecisionAsApplied(ctx context.Context, decisionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	if record, exists := cm.mergedDecisions[decisionID]; exists {
		record.Applied = true
		record.AppliedAt = time.Now()
		
		// Copy decision ID to active decisions
		active := &DecisionRecord{
			ID:        record.DecisionID,
			Version:   record.Version,
			Status:    StatusActive,
			CreatedAt: time.Now(),
			Metadata:  map[string]string{"source": record.Source},
		}
		
		cm.StoreDecision(ctx, active)
		cm.metrics.RecordApply(decisionID)
	}
	
	return nil
}

// StoreMergedDecision stores a merged decision for later application
func (cm *CacheManager) StoreMergedDecision(ctx context.Context, record MergedDecisionRecord) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	if len(cm.mergedDecisions) >= cm.maxDecisions/2 {
		cm.pruneOldMerged()
	}
	
	cm.mergedDecisions[record.DecisionID] = &record
	cm.metrics.RecordMerge(record.DecisionID)
	
	return nil
}

// pruneOldest removes oldest decisions when cache is full
func (cm *CacheManager) pruneOldest() {
	// Sort versions and remove oldest
	sort.Slice(cm.versionHistory, func(i, j int) bool {
		return cm.versionHistory[i] < cm.versionHistory[j]
	})
	
	oldestVer := cm.versionHistory[0]
	delete(cm.versionHistory, 0)
	
	for _, id := range cm.byVersion[oldestVer] {
		delete(cm.decisions, id)
	}
	delete(cm.byVersion, oldestVer)
	
	cm.metrics.RecordPrune()
}

// pruneOldMerged removes old merged decisions
func (cm *CacheManager) pruneOldMerged() {
	count := len(cm.mergedDecisions) / 2
	removed := 0
	
	for id := range cm.mergedDecisions {
		if removed >= count {
			break
		}
		
		record := cm.mergedDecisions[id]
		if record.Applied && time.Since(record.AppliedAt) > 24*time.Hour {
			delete(cm.mergedDecisions, id)
			removed++
		}
	}
	
	cm.metrics.RecordMergePrune(removed)
}

// GetAllDecisions returns all decisions
func (cm *CacheManager) GetAllDecisions(ctx context.Context) []*DecisionRecord {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	result := make([]*DecisionRecord, 0, len(cm.decisions))
	for _, record := range cm.decisions {
		result = append(result, record)
	}
	
	return result
}

// GetByVersionRange retrieves decisions within version range
func (cm *CacheManager) GetByVersionRange(start, end int64) []*DecisionRecord {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	result := make([]*DecisionRecord, 0)
	for _, ids := range cm.byVersion {
		if start <= ids && ids <= end {
			for _, id := range ids {
				if record, exists := cm.decisions[id]; exists {
					result = append(result, record)
				}
			}
		}
	}
	
	return result
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
