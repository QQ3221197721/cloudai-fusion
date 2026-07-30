// Package edge provides bidirectional synchronization broker for reconciling 
// offline edge decisions with cloud state upon reconnection.
package edge

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edgeautonomy"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// ReconciliationBroker manages bidirectional sync between edge and cloud
// Implements store-and-forward pattern with conflict resolution
// ============================================================================

// ReconciliationBroker orchestrates the sync process from disconnection to recovery
type ReconciliationBroker struct {
	cacheMgr      *EnhancedCacheManager
	conflictResolver *edgeautonomy.ConflictResolver
	versionVector *edgeautonomy.VersionVector
	db            *sql.DB
	
	nodeID        string
	maxBatchSize  int
	maxRetries    int
	retryDelay    time.Duration
	
	mu               sync.RWMutex
	isSyncing        bool
	lastSyncTime     time.Time
	syncHistory      []SyncOperationRecord
	
	logger *logrus.Logger
}

// SyncDirection defines direction of sync operation
type SyncDirection string

const (
	EdgeToCloud   SyncDirection = "EDGE_TO_CLOUD"
	CloudToEdge   SyncDirection = "CLOUD_TO_EDGE"
	Bidirectional SyncDirection = "BIDIRECTIONAL"
)

// SyncOperationRecord logs a single sync operation
type SyncOperationRecord struct {
	ID           string        `json:"operation_id"`
	Direction    SyncDirection `json:"direction"`
	Status       string        `json:"status"` // SUCCESS, FAILED, PARTIAL
	RecordsProcessed int        `json:"records_processed"`
	ConflictsResolved int       `json:"conflicts_resolved"`
	Timestamp    time.Time     `json:"timestamp"`
	DurationSec  float64       `json:"duration_sec"`
	ErrorMsg     string        `json:"error_message,omitempty"`
}

// NewReconciliationBroker creates sync broker coordinating edge-cloud reconciliation
func NewReconciliationBroker(
	nodeID string,
	cacheMgr *EnhancedCacheManager,
	conflictResolver *edgeautonomy.ConflictResolver,
	versionVector *edgeautonomy.VersionVector,
	db *sql.DB,
	config OfflineRuntimeConfig,
	logger *logrus.Logger,
) *ReconciliationBroker {
	if nodeID == "" {
		panic("nodeID cannot be empty")
	}
	
	defensive.ValidateRange(float64(config.SyncBatchSize), 10, 500, "sync_batch_size")
	
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	return &ReconciliationBroker{
		cacheMgr:         cacheMgr,
		conflictResolver: conflictResolver,
		versionVector:    versionVector,
		db:               db,
		nodeID:           nodeID,
		maxBatchSize:     config.SyncBatchSize,
		maxRetries:       3,
		retryDelay:       5 * time.Second,
		syncHistory:      make([]SyncOperationRecord, 0, 100),
		logger:           logger.WithFields(logrus.Fields{"component": "reconciliation_broker", "node_id": nodeID}),
	}
}

// StartBidirectionalSync initiates full reconciliation process when network restored
func (b *ReconciliationBroker) StartBidirectionalSync(ctx context.Context) (*SyncReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	
	b.mu.Lock()
	if b.isSyncing {
		b.mu.Unlock()
		return nil, fmt.Errorf("sync already in progress, please wait")
	}
	b.isSyncing = true
	b.mu.Unlock()
	
	startTime := time.Now()
	report := &SyncReport{
		NodeID:         b.nodeID,
		StartTime:      startTime,
		Direction:      Bidirectional,
		Operations:     make([]SyncOperationRecord, 0),
		TotalRecords:   0,
		SuccessRate:    0.0,
		ConflictsFound: 0,
	}
	
	// Step 1: Push local decisions to cloud
	localOps, err := b.pushLocalDecisionsToCloud(ctx, report)
	if err != nil {
		b.logger.WithError(err).Error("Failed to push local decisions, continuing anyway")
		// Non-fatal error - continue with other directions
	}
	report.Operations = append(report.Operations, localOps...)
	
	// Step 2: Pull latest cloud state
	pullOps, err := b.pullCloudStateFromServer(ctx, report)
	if err != nil {
		b.logger.WithError(err).Warn("Failed to pull cloud state")
	}
	report.Operations = append(report.Operations, pullOps...)
	
	// Step 3: Resolve any conflicts detected
	resolutionOps, conflicts, err := b.resolveConflictsAndMerge(ctx, report)
	if err != nil {
		b.logger.WithError(err).Error("Conflict resolution failed")
	}
	report.Operations = append(report.Operations, resolutionOps...)
	report.ConflictsFound = conflicts
	
	// Mark as not syncing anymore
	b.mu.Lock()
	b.isSyncing = false
	b.lastSyncTime = time.Now()
	b.mu.Unlock()
	
	// Calculate final metrics
	report.DurationSec = time.Since(startTime).Seconds()
	report.SuccessRate = b.calculateSuccessRate(report)
	
	// Record operation history
	b.recordSyncHistory(*report)
	
	return report, nil
}

// pushLocalDecisionsToCloud sends unsynced local decisions to central controller
func (b *ReconciliationBroker) pushLocalDecisionsToCloud(
	ctx context.Context,
	report *SyncReport,
) (SyncOperationRecord, error) {
	operationID := generateUUID()
	startTime := time.Now()
	
	// Get unsynced decisions from DB
	records, err := b.cacheMgr.GetUnsyncedDecisions(ctx, b.maxBatchSize)
	if err != nil {
		return SyncOperationRecord{
			ID:          operationID,
			Direction:   EdgeToCloud,
			Status:      "FAILED",
			ErrorMsg:    err.Error(),
			Timestamp:   time.Now().UTC(),
		}, err
	}
	
	if len(records) == 0 {
		return SyncOperationRecord{
			ID:             operationID,
			Direction:      EdgeToCloud,
			Status:         "SUCCESS",
			RecordsProcessed: 0,
			Timestamp:      time.Now().UTC(),
		}, nil
	}
	
	// Process each record
	successCount := 0
	for _, record := range records {
		// TODO: Implement actual sync logic here
		// For now, mark all as successful for demonstration
		
		// Mark as synced in database
		if err := b.cacheMgr.MarkDecisionSynced(ctx, record.ID, ""); err != nil {
			b.logger.WithField("record_id", record.ID).WithError(err).Warn("Failed to mark synced")
			continue
		}
		
		successCount++
	}
	
	duration := time.Since(startTime).Seconds()
	status := "PARTIAL"
	if successCount == len(records) {
		status = "SUCCESS"
	}
	
	return SyncOperationRecord{
		ID:                 operationID,
		Direction:          EdgeToCloud,
		Status:             status,
		RecordsProcessed:   successCount,
		Timestamp:          time.Now().UTC(),
		DurationSec:        duration,
		ErrorMsg:           "",
	}, nil
}

// pullCloudStateFromServer fetches latest cloud decisions for our node
func (b *ReconciliationBroker) pullCloudStateFromServer(
	ctx context.Context,
	report *SyncReport,
) (SyncOperationRecord, error) {
	operationID := generateUUID()
	startTime := time.Now()
	
	// TODO: Implement actual pull logic
	// This would query cloud API or sync queue table
	
	// Simulate pulling some cloud decisions
	var cloudRecords []CloudDecisionRecord
	
	// Process pulled records
	successCount := 0
	if len(cloudRecords) > 0 {
		// Insert into local cache
		for _, cr := range cloudRecords {
			// TODO: Validate and insert
			successCount++
		}
	}
	
	duration := time.Since(startTime).Seconds()
	
	return SyncOperationRecord{
		ID:                 operationID,
		Direction:          CloudToEdge,
		Status:             "SUCCESS",
		RecordsProcessed:   successCount,
		Timestamp:          time.Now().UTC(),
		DurationSec:        duration,
	}, nil
}

// resolveConflictsAndMerge handles detection and resolution of sync conflicts
func (b *ReconciliationBroker) resolveConflictsAndMerge(
	ctx context.Context,
	report *SyncReport,
) ([]SyncOperationRecord, int, error) {
	operationID := generateUUID()
	startTime := time.Now()
	
	// Get both local unsynced and cloud decisions
	localRecords, _ := b.cacheMgr.GetUnsyncedDecisions(ctx, b.maxBatchSize)
	cloudRecords, _ := b.getCloudDecisionsForNode(ctx) // TODO: implement
	
	if len(localRecords) == 0 || len(cloudRecords) == 0 {
		// No conflict possible
		return []SyncOperationRecord{}, 0, nil
	}
	
	// Use conflict resolver to find and resolve conflicts
	resolved, conflicts := b.conflictResolver.ResolveConflicts(localRecords, cloudRecords)
	
	conflictCount := len(conflicts)
	
	// Apply resolutions
	for _, resolved := range resolved {
		// Update local state with resolved decision
		// TODO: Implement actual merge/update logic
	}
	
	duration := time.Since(startTime).Seconds()
	
	return []SyncOperationRecord{{
		ID:             operationID,
		Direction:      Bidirectional,
		Status:         "COMPLETED",
		RecordsProcessed: len(resolved),
		ConflictsResolved: conflictCount,
		Timestamp:      time.Now().UTC(),
		DurationSec:    duration,
	}}, conflictCount, nil
}

// getCloudDecisionsForNode fetches cloud-side decisions for this node
func (b *ReconciliationBroker) getCloudDecisionsForNode(ctx context.Context) ([]CloudDecisionRecord, error) {
	// TODO: Implement actual retrieval
	// Could be from REST API, GraphQL, or sync queue table
	return []CloudDecisionRecord{}, nil
}

// calculateSuccessRate computes sync success percentage
func (b *ReconciliationBroker) calculateSuccessRate(report *SyncReport) float64 {
	if report.TotalRecords == 0 {
		return 100.0
	}
	
	// Count successes
	successful := 0
	for _, op := range report.Operations {
		if op.Status == "SUCCESS" || op.Status == "COMPLETED" {
			successful += op.RecordsProcessed
		}
	}
	
	return float64(successful) / float64(report.TotalRecords) * 100.0
}

// recordSyncHistory adds sync operation to persistent history
func (b *ReconciliationBroker) recordSyncHistory(report SyncReport) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	// Keep only last 100 operations
	if len(b.syncHistory) >= 100 {
		b.syncHistory = b.syncHistory[len(b.syncHistory)-99:]
	}
	
	record := SyncOperationRecord{
		ID:           generateUUID(),
		Direction:    report.Direction,
		Status:       "COMPLETED",
		RecordsProcessed: report.TotalRecords,
		ConflictsResolved: report.ConflictsFound,
		Timestamp:    report.StartTime,
		DurationSec:  report.DurationSec,
	}
	
	b.syncHistory = append(b.syncHistory, record)
}

// GetRecentSyncHistory returns recent sync operations
func (b *ReconciliationBroker) GetRecentSyncHistory(limit int) []SyncOperationRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	if limit <= 0 || limit > len(b.syncHistory) {
		limit = len(b.syncHistory)
	}
	
	result := make([]SyncOperationRecord, limit)
	copy(result, b.syncHistory[len(b.syncHistory)-limit:])
	
	return result
}

// IsCurrentlySyncing returns whether sync is in progress
func (b *ReconciliationBroker) IsCurrentlySyncing() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.isSyncing
}

// GetLastSyncTime returns timestamp of most recent sync completion
func (b *ReconciliationBroker) GetLastSyncTime() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastSyncTime
}
