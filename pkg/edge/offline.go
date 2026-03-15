// Package edge provides offline operation management for edge-cloud collaboration.
// Implements WAL-style (Write-Ahead Logging) buffer for disconnected operation scenarios,
// conflict resolution strategies, delta sync, and bandwidth-aware synchronization.
package edge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// OfflineOperationType defines types of operations that can be buffered
type OfflineOperationType string

const (
	OpTypeDeployModel    OfflineOperationType = "deploy_model"
	OpTypeStopModel      OfflineOperationType = "stop_model"
	OpTypeUpdateConfig   OfflineOperationType = "update_config"
	OpTypeDeleteResource OfflineOperationType = "delete_resource"
	OpTypeSyncMetrics    OfflineOperationType = "sync_metrics"
)

// ConflictResolutionStrategy defines how to resolve conflicts during sync
type ConflictResolutionStrategy string

const (
	ConflictLastWriteWins  ConflictResolutionStrategy = "last_write_wins"
	ConflictCloudWins      ConflictResolutionStrategy = "cloud_wins"
	ConflictEdgeWins       ConflictResolutionStrategy = "edge_wins"
	ConflictMerge          ConflictResolutionStrategy = "merge"
	ConflictManual         ConflictResolutionStrategy = "manual"
)

// SyncDirection defines the direction of data synchronization
type SyncDirection string

const (
	SyncCloudToEdge   SyncDirection = "cloud_to_edge"
	SyncEdgeToCloud   SyncDirection = "edge_to_cloud"
	SyncBidirectional SyncDirection = "bidirectional"
)

// OfflineOperation represents a single operation to be executed when online
type OfflineOperation struct {
	ID            string                 `json:"id"`
	NodeID        string                 `json:"node_id"`
	Type          OfflineOperationType   `json:"type"`
	Payload       map[string]interface{} `json:"payload"`
	CreatedAt     time.Time              `json:"created_at"`
	RetryCount    int                    `json:"retry_count"`
	MaxRetries    int                    `json:"max_retries"`
	NextRetryAt   *time.Time             `json:"next_retry_at,omitempty"`
	LastError     string                 `json:"last_error,omitempty"`
	Status        string                 `json:"status"` // pending, processing, completed, failed, dropped
	Priority      int                    `json:"priority"`
	ExpiryTime    *time.Time             `json:"expiry_time,omitempty"`
}

// SyncStatus tracks the current synchronization state
type SyncStatus struct {
	NodeID              string    `json:"node_id"`
	LastSyncAt          time.Time `json:"last_sync_at"`
	IsOnline            bool      `json:"is_online"`
	PendingOperations   int       `json:"pending_operations"`
	FailedOperations    int       `json:"failed_operations"`
	TotalBufferedBytes  int64     `json:"total_buffered_bytes"`
	BufferUtilization   float64   `json:"buffer_utilization_percent"`
	CurrentBandwidthMbps float64  `json:"current_bandwidth_mbps"`
	AvgLatencyMs        float64   `json:"avg_latency_ms"`
	SyncDirection       SyncDirection `json:"sync_direction"`
	CompressionEnabled  bool      `json:"compression_enabled"`
	CompressionRatio    float64   `json:"compression_ratio"`
}

// DeltaSyncResult contains information about incremental sync
type DeltaSyncResult struct {
	TotalItems      int      `json:"total_items"`
	SyncedItems     int      `json:"synced_items"`
	SkippedItems    int      `json:"skipped_items"`
	FailedItems     int      `json:"failed_items"`
	TransferredBytes int64   `json:"transferred_bytes"`
	Duration        string   `json:"duration"`
	ChecksumBefore  string   `json:"checksum_before"`
	ChecksumAfter   string   `json:"checksum_after"`
}

// OfflineQueue manages buffered operations during disconnection
type OfflineQueue struct {
	nodeID           string
	maxBufferSizeMB  int
	operations       []*OfflineOperation
	mu               sync.RWMutex
	logger           *logrus.Logger
	totalEnqueued    int64
	totalDequeued    int64
	totalDropped     int64
	lastDrainAttempt time.Time
}

// NewOfflineQueue creates a new offline operation queue
func NewOfflineQueue(nodeID string, maxBufferSizeMB int, logger *logrus.Logger) *OfflineQueue {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if maxBufferSizeMB <= 0 {
		maxBufferSizeMB = 50 // Default 50MB buffer
	}
	return &OfflineQueue{
		nodeID:          nodeID,
		maxBufferSizeMB: maxBufferSizeMB,
		operations:      make([]*OfflineOperation, 0),
		logger:          logger,
	}
}

// Enqueue adds an operation to the offline queue
func (q *OfflineQueue) Enqueue(ctx context.Context, op *OfflineOperation) error {
	if err := apperrors.CheckContext(ctx); err != nil {
		return err
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Check buffer size limit
	currentSize := q.calculateBufferSize()
	opSize := q.estimateOpSize(op)
	
	if currentSize+opSize > int64(q.maxBufferSizeMB)*1024*1024 {
		// Buffer full - apply drop policy (drop lowest priority)
		if !q.dropLowestPriority(op) {
			return fmt.Errorf("offline buffer full (%d MB limit)", q.maxBufferSizeMB)
		}
	}

	op.ID = fmt.Sprintf("op-%s-%d", q.nodeID, q.totalEnqueued)
	op.CreatedAt = time.Now().UTC()
	op.Status = "pending"
	op.NextRetryAt = nil
	
	if op.MaxRetries == 0 {
		op.MaxRetries = 3
	}

	q.operations = append(q.operations, op)
	q.totalEnqueued++

	q.logger.WithFields(logrus.Fields{
		"node":   q.nodeID,
		"op_id":  op.ID,
		"type":   op.Type,
		"size":   opSize,
		"buffer": q.calculateBufferSize(),
	}).Debug("Enqueued offline operation")

	return nil
}

// Dequeue retrieves the next pending operation (highest priority, oldest first)
func (q *OfflineQueue) Dequeue() *OfflineOperation {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.operations) == 0 {
		return nil
	}

	// Find highest priority pending operation
	bestIdx := -1
	bestPriority := -1
	now := time.Now().UTC()

	for i, op := range q.operations {
		if op.Status != "pending" {
			continue
		}
		// Skip if not yet retry time
		if op.NextRetryAt != nil && op.NextRetryAt.After(now) {
			continue
		}
		if op.Priority > bestPriority {
			bestPriority = op.Priority
			bestIdx = i
		}
	}

	if bestIdx < 0 {
		return nil
	}

	op := q.operations[bestIdx]
	op.Status = "processing"
	q.operations = append(q.operations[:bestIdx], q.operations[bestIdx+1:]...)
	q.totalDequeued++

	return op
}

// MarkCompleted marks an operation as completed
func (q *OfflineQueue) MarkCompleted(opID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Already removed from queue, just log
	q.logger.WithField("op_id", opID).Debug("Operation marked completed")
}

// MarkFailed marks an operation as failed and schedules retry
func (q *OfflineQueue) MarkFailed(opID string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Re-queue with incremented retry count
	for _, op := range q.operations {
		if op.ID == opID {
			op.RetryCount++
			op.LastError = err.Error()
			op.Status = "pending"
			
			if op.RetryCount >= op.MaxRetries {
				op.Status = "failed"
				q.logger.WithFields(logrus.Fields{
					"op_id":  opID,
					"retries": op.RetryCount,
				}).Warn("Operation exceeded max retries")
			} else {
				// Exponential backoff: 1s, 2s, 4s, 8s...
				backoff := time.Duration(1<<uint(op.RetryCount)) * time.Second
				nextRetry := time.Now().UTC().Add(backoff)
				op.NextRetryAt = &nextRetry
				q.logger.WithFields(logrus.Fields{
					"op_id":      opID,
					"retry":      op.RetryCount,
					"next_retry": nextRetry,
				}).Debug("Scheduled operation retry")
			}
			break
		}
	}
}

// GetStatus returns current queue status
func (q *OfflineQueue) GetStatus() *SyncStatus {
	q.mu.RLock()
	defer q.mu.RUnlock()

	pending := 0
	failed := 0
	for _, op := range q.operations {
		switch op.Status {
		case "pending":
			pending++
		case "failed":
			failed++
		}
	}

	bufferSize := q.calculateBufferSize()
	maxBuffer := int64(q.maxBufferSizeMB) * 1024 * 1024

	return &SyncStatus{
		NodeID:             q.nodeID,
		PendingOperations:  pending,
		FailedOperations:   failed,
		TotalBufferedBytes: bufferSize,
		BufferUtilization:  float64(bufferSize) / float64(maxBuffer) * 100,
	}
}

// ListOperations returns all operations filtered by status
func (q *OfflineQueue) ListOperations(status string) []*OfflineOperation {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if status == "" {
		result := make([]*OfflineOperation, len(q.operations))
		copy(result, q.operations)
		return result
	}

	result := make([]*OfflineOperation, 0)
	for _, op := range q.operations {
		if op.Status == status {
			result = append(result, op)
		}
	}
	return result
}

// Clear removes all operations from the queue
func (q *OfflineQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	dropped := len(q.operations)
	q.operations = make([]*OfflineOperation, 0)
	q.totalDropped += int64(dropped)

	q.logger.WithField("dropped", dropped).Warn("Cleared offline queue")
}

// calculateBufferSize estimates total buffered size in bytes
func (q *OfflineQueue) calculateBufferSize() int64 {
	total := int64(0)
	for _, op := range q.operations {
		total += q.estimateOpSize(op)
	}
	return total
}

// estimateOpSize estimates serialized size of an operation
func (q *OfflineQueue) estimateOpSize(op *OfflineOperation) int64 {
	data, _ := json.Marshal(op)
	return int64(len(data))
}

// dropLowestPriority removes lowest priority operation to make room
func (q *OfflineQueue) dropLowestPriority(newOp *OfflineOperation) bool {
	if len(q.operations) == 0 {
		return false
	}

	// Don't drop if new op has higher priority than all existing
	allLower := true
	for _, op := range q.operations {
		if op.Priority >= newOp.Priority {
			allLower = false
			break
		}
	}
	if allLower {
		return false
	}

	// Find and remove lowest priority pending operation
	lowestIdx := -1
	lowestPriority := 999999
	for i, op := range q.operations {
		if op.Status == "pending" && op.Priority < lowestPriority {
			lowestPriority = op.Priority
			lowestIdx = i
		}
	}

	if lowestIdx >= 0 {
		dropped := q.operations[lowestIdx]
		q.operations = append(q.operations[:lowestIdx], q.operations[lowestIdx+1:]...)
		q.totalDropped++
		q.logger.WithFields(logrus.Fields{
			"dropped_op": dropped.ID,
			"priority":   dropped.Priority,
		}).Warn("Dropped low-priority operation")
		return true
	}

	return false
}

// ============================================================================
// Conflict Resolution
// ============================================================================

// ConflictResolver handles conflict detection and resolution during sync
type ConflictResolver struct {
	strategy ConflictResolutionStrategy
	logger   *logrus.Logger
}

// NewConflictResolver creates a conflict resolver with specified strategy
func NewConflictResolver(strategy ConflictResolutionStrategy, logger *logrus.Logger) *ConflictResolver {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &ConflictResolver{
		strategy: strategy,
		logger:   logger,
	}
}

// ResolveConflict resolves a conflict between cloud and edge versions
func (r *ConflictResolver) ResolveConflict(cloudVersion, edgeVersion map[string]interface{}) (map[string]interface{}, error) {
	switch r.strategy {
	case ConflictLastWriteWins:
		cloudTime := extractTimestamp(cloudVersion)
		edgeTime := extractTimestamp(edgeVersion)
		if cloudTime.After(edgeTime) {
			return cloudVersion, nil
		}
		return edgeVersion, nil

	case ConflictCloudWins:
		return cloudVersion, nil

	case ConflictEdgeWins:
		return edgeVersion, nil

	case ConflictMerge:
		return mergeMaps(cloudVersion, edgeVersion), nil

	default:
		return nil, fmt.Errorf("manual conflict resolution required")
	}
}

func extractTimestamp(data map[string]interface{}) time.Time {
	if ts, ok := data["updated_at"].(time.Time); ok {
		return ts
	}
	return time.Time{}
}

func mergeMaps(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

// ============================================================================
// Bandwidth-Aware Sync
// ============================================================================

// BandwidthMonitor tracks network conditions for adaptive sync
type BandwidthMonitor struct {
	mu                sync.RWMutex
	currentBandwidthMbps float64
	avgLatencyMs      float64
	lastMeasurement   time.Time
	history           []BandwidthSample
}

// BandwidthSample is a historical bandwidth measurement
type BandwidthSample struct {
	BandwidthMbps float64
	LatencyMs     float64
	Timestamp     time.Time
}

// NewBandwidthMonitor creates a bandwidth monitor
func NewBandwidthMonitor() *BandwidthMonitor {
	return &BandwidthMonitor{
		history: make([]BandwidthSample, 0, 60), // Keep last 60 samples
	}
}

// Update records a new bandwidth measurement
func (m *BandwidthMonitor) Update(bandwidthMbps, latencyMs float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentBandwidthMbps = bandwidthMbps
	m.avgLatencyMs = latencyMs
	m.lastMeasurement = time.Now().UTC()

	sample := BandwidthSample{
		BandwidthMbps: bandwidthMbps,
		LatencyMs:     latencyMs,
		Timestamp:     m.lastMeasurement,
	}

	m.history = append(m.history, sample)
	if len(m.history) > 60 {
		m.history = m.history[1:]
	}
}

// GetAdaptiveInterval returns recommended sync interval based on conditions
func (m *BandwidthMonitor) GetAdaptiveInterval(baseInterval time.Duration) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentBandwidthMbps < 1.0 {
		return baseInterval * 5 // Very slow: 5x interval
	}
	if m.currentBandwidthMbps < 10.0 {
		return baseInterval * 2 // Slow: 2x interval
	}
	if m.avgLatencyMs > 500 {
		return baseInterval * 2 // High latency: 2x interval
	}
	return baseInterval // Normal conditions
}

// ShouldCompress returns true if compression is beneficial
func (m *BandwidthMonitor) ShouldCompress(minBandwidthMbps float64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.currentBandwidthMbps < minBandwidthMbps
}

// GetAverageBandwidth returns average bandwidth over history
func (m *BandwidthMonitor) GetAverageBandwidth() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.history) == 0 {
		return m.currentBandwidthMbps
	}

	sum := 0.0
	for _, s := range m.history {
		sum += s.BandwidthMbps
	}
	return sum / float64(len(m.history))
}
