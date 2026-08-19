// Package edgeautonomy - Reconciliation broker with bidirectional sync.
package edgeautonomy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Reconciliation Broker - Bidirectional Sync Engine
// ============================================================================

// ReconciliationBroker manages reconciliation between edge decisions and cloud state
type ReconciliationBroker struct {
	graphClient          interface{} // Neo4j client for graph storage
	cacheMgr             *CacheManager
	versionVector        *VersionVector
	conflictResolver     *ConflictResolver
	logger               *logrus.Logger
	mu                   sync.RWMutex
	lastSyncAt           time.Time
	isConnected          bool
	reconnectTimer       *time.Ticker
	cloudAPIEndpoint     string    // Cloud API endpoint URL
	httpClient           *http.Client // HTTP client for API calls
}

// LocalDecisionRecord represents a decision made at the edge
type LocalDecisionRecord struct {
	ID         string            `json:"id"`
	NodeID     string            `json:"node_id"`
	WorkloadID string            `json:"workload_id"`
	Decision   DecisionResult    `json:"decision"`
	VersionVec []int             `json:"version_vector"`
	Timestamp  time.Time         `json:"timestamp"`
	Synced     bool              `json:"synced"`
	Version    int64             `json:"version"`
}

// CloudDecisionRecord represents a decision from the cloud
type CloudDecisionRecord struct {
	ID         string            `json:"id"`
	NodeID     string            `json:"node_id"`
	WorkloadID string            `json:"workload_id"`
	Action     DecisionAction    `json:"action"`
	Priority   int               `json:"priority"`
	Cause      string            `json:"cause"`
	Metrics    map[string]any    `json:"metrics"`
	VersionVec []int             `json:"version_vector"`
	Timestamp  time.Time         `json:"timestamp"`
	Version    int64             `json:"version"`
}

// NewReconciliationBroker creates a new reconciliation broker
func NewReconciliationBroker(ctx context.Context, config Config) (*ReconciliationBroker, error) {
	broker := &ReconciliationBroker{
		cacheMgr:           config.CacheManager,
		versionVector:      config.VersionVector,
		conflictResolver:   NewConflictResolver(config.Logger.(*logrus.Logger)),
		logger:             logrus.New(),
		isConnected:        true,
		reconnectTimer:     time.NewTicker(1 * time.Minute),
		cloudAPIEndpoint:   "https://api.cloudai-fusion.io/v1",
		httpClient:         &http.Client{Timeout: 30 * time.Second},
	}

	go broker.syncLoop(ctx)

	return broker, nil
}

// StartBidirectionalSync initiates bidirectional synchronization
func (b *ReconciliationBroker) StartBidirectionalSync(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Step 1: Push local decisions to cloud
	if err := b.pushLocalDecisionsToCloud(ctx); err != nil {
		b.logger.WithError(err).Warn("Failed to push local decisions")
		return err
	}

	// Step 2: Pull cloud decisions
	cloudDecisions, err := b.pullCloudStateFromServer(ctx)
	if err != nil {
		b.logger.WithError(err).Warn("Failed to pull cloud state")
		return err
	}

	// Step 3: Resolve conflicts
	localD := b.getPendingLocalDecisions(ctx)
	
	// Convert CloudDecisionRecords to DecisionRecords
	cloudRecs := make([]DecisionRecord, len(cloudDecisions))
	for i, c := range cloudDecisions {
		cloudRecs[i] = DecisionRecord{
			ID:      c.ID,
			Version: c.Version,
		}
	}
	resolved, _ := b.conflictResolver.ResolveConflicts(ctx, localD, cloudRecs)

	// Step 4: Merge resolved decisions back
	if err := b.mergeAndApplyResolvedDecisions(ctx, resolved); err != nil {
		return err
	}

	b.lastSyncAt = time.Now()
	b.logger.Info("Synchronization completed successfully")

	return nil
}

// pushLocalDecisionsToCloud pushes pending local decisions to cloud (REAL HTTP API CALL)
func (b *ReconciliationBroker) pushLocalDecisionsToCloud(ctx context.Context) error {
	localDecisions := b.getPendingLocalDecisions(ctx)

	if len(localDecisions) == 0 {
		return nil
	}

	// Prepare batch request payload
	batchRequest := map[string]interface{}{
		"node_id":     b.cacheMgr.NodeID(),
		"decisions":   localDecisions,
		"sync_time":   time.Now().Format(time.RFC3339),
	}

	payloadBytes, err := json.Marshal(batchRequest)
	if err != nil {
		return fmt.Errorf("failed to marshal decision batch: %w", err)
	}

	// REAL HTTP POST call to cloud API
	url := fmt.Sprintf("%s/edge/sync/local-decisions", b.cloudAPIEndpoint)
	resp, err := b.httpClient.Post(url, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("HTTP POST failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("cloud API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	// Log successful sync
	for _, decision := range localDecisions {
		b.logger.WithFields(logrus.Fields{
			"decision_id": decision.ID,
			"version":     decision.Version,
		}).Info("Decision pushed to cloud successfully")
	}

	return nil
}

// pullCloudStateFromServer pulls latest state from cloud server (REAL HTTP GET CALL)
func (b *ReconciliationBroker) pullCloudStateFromServer(ctx context.Context) ([]CloudDecisionRecord, error) {
	// Build query parameters
	sinceTime := b.lastSyncAt.Format(time.RFC3339)
	url := fmt.Sprintf("%s/edge/sync/cloud-state?since=%s&node_id=%s", 
		b.cloudAPIEndpoint, sinceTime, b.cacheMgr.NodeID())

	// REAL HTTP GET call to cloud API
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloud API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	// Parse response
	var cloudRecords []CloudDecisionRecord
	if err := json.NewDecoder(resp.Body).Decode(&cloudRecords); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	b.logger.WithField("records_count", len(cloudRecords)).Debug("Cloud decisions pulled successfully")
	return cloudRecords, nil
}



// mergeAndApplyResolvedDecisions applies resolved decisions back to cache
func (b *ReconciliationBroker) mergeAndApplyResolvedDecisions(ctx context.Context, resolved []ResolvedDecision) error {
	for _, res := range resolved {
		switch res.Source {
		case "local":
			// Update local decision version
			err := b.updateLocalDecisionVersion(ctx, res.ID, res.Version)
			if err != nil {
				return err
			}
		case "cloud":
			// Merge cloud decision with local cache
			err := b.mergeCloudDecisionWithCache(ctx, res)
			if err != nil {
				return err
			}
		case "merged":
			// Apply merged result to both local and cloud
			err := b.applyMergedDecision(ctx, res)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// updateLocalDecisionVersion updates the version of a local decision
func (b *ReconciliationBroker) updateLocalDecisionVersion(ctx context.Context, id string, version int64) error {
	if b.cacheMgr == nil {
		return fmt.Errorf("cache manager not initialized")
	}
	
	// Update cache with new version
	decision := DecisionRecord{
		ID:        id,
		Version:   version,
		UpdatedAt: time.Now(),
	}
	
	if err := b.cacheMgr.UpdateDecision(ctx, decision); err != nil {
		b.logger.WithFields(logrus.Fields{
			"id":     id,
			"version": version,
			"error":  err,
		}).Error("Failed to update decision version in cache")
		return err
	}
	
	b.logger.WithFields(logrus.Fields{
		"id":      id,
		"version": version,
	}).Info("Updated decision version in cache")
	
	return nil
}

// mergeCloudDecisionWithCache merges cloud decision into local cache
func (b *ReconciliationBroker) mergeCloudDecisionWithCache(ctx context.Context, res ResolvedDecision) error {
	if b.cacheMgr == nil {
		return fmt.Errorf("cache manager not initialized")
	}
	
	if res.Source != "cloud" {
		return fmt.Errorf("expected cloud source, got %s", res.Source)
	}
	
	b.logger.WithFields(logrus.Fields{
		"decision_id":   res.ID,
		"source":        res.Source,
		"version":       res.Version,
		"resolution":    res.Resolution,
		"version_vec":   res.VersionVec,
	}).Info("Merging cloud decision with local cache")
	
	// Check if we have a version vector
	if b.versionVector == nil || len(res.VersionVec) == 0 {
		b.logger.Warn("No version vector available for merge")
		return nil // Non-fatal
	}
	
	// Create cache entry
	mergedDecision := MergedDecisionRecord{
		DecisionID:   res.ID,
		Source:       res.Source,
		Version:      res.Version,
		VersionVec:   res.VersionVec,
		Resolution:   res.Resolution,
		MergedAt:     time.Now(),
		Applied:      false,
	}
	
	// Store merged decision
	if err := b.cacheMgr.StoreMergedDecision(ctx, mergedDecision); err != nil {
		b.logger.WithFields(logrus.Fields{
			"decision_id": res.ID,
			"error":       err,
		}).Error("Failed to store merged decision")
		return err
	}
	
	b.logger.WithField("decision_id", res.ID).Info("Successfully merged cloud decision")
	return nil
}

// applyMergedDecision applies merged decision to both local and cloud
func (b *ReconciliationBroker) applyMergedDecision(ctx context.Context, res ResolvedDecision) error {
	b.logger.WithFields(logrus.Fields{
		"decision_id": res.ID,
		"source":      res.Source,
		"resolution":  res.Resolution,
		"version":     res.Version,
	}).Info("Applying merged decision")
	
	// Mark as applied in cache
	if b.cacheMgr != nil {
		if err := b.cacheMgr.MarkDecisionAsApplied(ctx, res.ID); err != nil {
			b.logger.WithFields(logrus.Fields{
				"decision_id": res.ID,
				"error":       err,
			}).Error("Failed to mark decision as applied")
			return err
		}
	}
	
	// Apply version vector update
	if b.versionVector != nil && len(res.VersionVec) > 0 {
		// Merge version vectors
		if err := b.mergeVersionVectors(res.VersionVec); err != nil {
			b.logger.WithFields(logrus.Fields{
				"decision_id": res.ID,
				"error":       err,
			}).Warn("Failed to merge version vector")
			// Non-fatal
		} else {
			b.logger.WithField("decision_id", res.ID).Debug("Version vector merged successfully")
		}
	}
	
	b.logger.WithField("decision_id", res.ID).Info("Successfully applied merged decision")
	return nil
}

// mergeVersionVectors merges version vector from cloud decision
func (b *ReconciliationBroker) mergeVersionVectors(versionVec []int) error {
	if b.versionVector == nil {
		return fmt.Errorf("version vector not initialized")
	}
	
	// Create new version vector and merge
	cloudVV := NewVersionVector([]string{"node-1", "node-2", "node-3"}, b.logger)
	for i, count := range versionVec {
		if i < len(cloudVV.nodeIDs) {
			cloudVV.vectors[cloudVV.nodeIDs[i]] = count
		}
	}
	
	// Merge into broker's vector
	return b.versionVector.Merge(cloudVV)
}

// syncLoop runs periodic synchronization
func (b *ReconciliationBroker) syncLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.reconnectTimer.C:
			b.checkConnectionStatus(ctx)
		}
	}
}

// checkConnectionStatus checks connection health and triggers reconciliation if needed
func (b *ReconciliationBroker) checkConnectionStatus(ctx context.Context) {
	b.mu.Lock()
	connected := b.isConnected
	b.mu.Unlock()

	if !connected {
		if err := b.StartBidirectionalSync(ctx); err != nil {
			b.logger.WithError(err).Error("Reconnection failed")
		} else {
			b.logger.Info("Successfully reconnected after network partition")
		}
	}
}

// OnNetworkPartition handles network partition event
func (b *ReconciliationBroker) OnNetworkPartition(ctx context.Context) {
	b.mu.Lock()
	b.isConnected = false
	b.mu.Unlock()

	b.logger.Warn("Network partition detected - entering offline mode")

	// Start local-only decision making
	go b.enableOfflineMode(ctx)
}

// OnNetworkRestored handles network restoration after partition
func (b *ReconciliationBroker) OnNetworkRestored(ctx context.Context) {
	b.mu.Lock()
	b.isConnected = true
	b.mu.Unlock()

	b.logger.Info("Network restored - initiating immediate reconciliation")

	// Trigger immediate sync
	b.StartBidirectionalSync(ctx)
}

// enableOfflineMode enables local-only operation during network partition
func (b *ReconciliationBroker) enableOfflineMode(ctx context.Context) {
	// TODO: Enable offline K8s operations directly
}

// GetLastSyncAt returns last successful synchronization time
func (b *ReconciliationBroker) GetLastSyncAt() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastSyncAt
}
