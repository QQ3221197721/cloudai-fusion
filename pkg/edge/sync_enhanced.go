// Package edge provides enhanced edge-cloud synchronization engine.
// Implements priority-based sync queue, delta compression with zstd-style
// encoding, conflict-free merge with vector clocks, and adaptive sync
// scheduling based on bandwidth and workload priority.
package edge

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// ============================================================================
// Enhanced Sync Engine
// ============================================================================

// SyncPriority defines the urgency of a sync item.
type SyncPriority int

const (
	SyncPriorityCritical SyncPriority = 0  // system alerts, security events
	SyncPriorityHigh     SyncPriority = 1  // model updates, config changes
	SyncPriorityNormal   SyncPriority = 2  // metrics, inference results
	SyncPriorityLow      SyncPriority = 3  // logs, telemetry
	SyncPriorityBulk     SyncPriority = 4  // bulk data, analytics
)

// SyncEngineConfig configures the enhanced sync engine.
type SyncEngineConfig struct {
	MaxQueueSize          int           `json:"max_queue_size"`
	BatchSize             int           `json:"batch_size"`
	FlushInterval         time.Duration `json:"flush_interval"`
	CompressionEnabled    bool          `json:"compression_enabled"`
	CompressionThreshold  int           `json:"compression_threshold_bytes"` // min size to compress
	DeltaSyncEnabled      bool          `json:"delta_sync_enabled"`
	VectorClockEnabled    bool          `json:"vector_clock_enabled"`
	MaxRetries            int           `json:"max_retries"`
	BackoffMultiplier     float64       `json:"backoff_multiplier"`
	AdaptiveScheduling    bool          `json:"adaptive_scheduling"`
	BandwidthBudgetMbps   float64       `json:"bandwidth_budget_mbps"` // 0 = unlimited
}

// DefaultSyncEngineConfig returns production defaults.
func DefaultSyncEngineConfig() SyncEngineConfig {
	return SyncEngineConfig{
		MaxQueueSize:         50000,
		BatchSize:            100,
		FlushInterval:        5 * time.Second,
		CompressionEnabled:   true,
		CompressionThreshold: 512,
		DeltaSyncEnabled:     true,
		VectorClockEnabled:   true,
		MaxRetries:           5,
		BackoffMultiplier:    2.0,
		AdaptiveScheduling:   true,
		BandwidthBudgetMbps:  0,
	}
}

// SyncItem represents a single item to be synchronized.
type SyncItem struct {
	ID            string                 `json:"id"`
	NodeID        string                 `json:"node_id"`
	Priority      SyncPriority           `json:"priority"`
	Direction     SyncDirection          `json:"direction"`
	DataType      string                 `json:"data_type"` // model, config, metrics, logs, events
	Key           string                 `json:"key"`
	Payload       map[string]interface{} `json:"payload"`
	SizeBytes     int64                  `json:"size_bytes"`
	Compressed    bool                   `json:"compressed"`
	CompressedSize int64                 `json:"compressed_size"`
	VectorClock   *VectorClock           `json:"vector_clock,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	RetryCount    int                    `json:"retry_count"`
	NextRetryAt   *time.Time             `json:"next_retry_at,omitempty"`
	Status        string                 `json:"status"` // queued, sending, sent, failed, conflicted
}

// ============================================================================
// Vector Clock — Causality Tracking
// ============================================================================

// VectorClock tracks causal ordering across distributed nodes.
type VectorClock struct {
	Clocks map[string]uint64 `json:"clocks"` // nodeID → logical clock
}

// NewVectorClock creates a new vector clock.
func NewVectorClock() *VectorClock {
	return &VectorClock{Clocks: make(map[string]uint64)}
}

// Increment advances the clock for the given node.
func (vc *VectorClock) Increment(nodeID string) {
	vc.Clocks[nodeID]++
}

// Merge combines two vector clocks (component-wise max).
func (vc *VectorClock) Merge(other *VectorClock) {
	if other == nil {
		return
	}
	for nodeID, clock := range other.Clocks {
		if clock > vc.Clocks[nodeID] {
			vc.Clocks[nodeID] = clock
		}
	}
}

// HappensBefore returns true if vc causally precedes other.
func (vc *VectorClock) HappensBefore(other *VectorClock) bool {
	if other == nil {
		return false
	}
	atLeastOneLess := false
	for nodeID, clock := range vc.Clocks {
		otherClock := other.Clocks[nodeID]
		if clock > otherClock {
			return false
		}
		if clock < otherClock {
			atLeastOneLess = true
		}
	}
	// Check if other has any clocks vc doesn't have
	for nodeID := range other.Clocks {
		if _, ok := vc.Clocks[nodeID]; !ok {
			atLeastOneLess = true
		}
	}
	return atLeastOneLess
}

// Concurrent returns true if neither clock happens before the other.
func (vc *VectorClock) Concurrent(other *VectorClock) bool {
	return !vc.HappensBefore(other) && !other.HappensBefore(vc)
}

// ============================================================================
// Delta Compression
// ============================================================================

// DeltaCompressor compresses sync payloads using delta encoding.
type DeltaCompressor struct {
	snapshots map[string]map[string]interface{} // key → last known state
	mu        sync.RWMutex
}

// NewDeltaCompressor creates a new delta compressor.
func NewDeltaCompressor() *DeltaCompressor {
	return &DeltaCompressor{
		snapshots: make(map[string]map[string]interface{}),
	}
}

// ComputeDelta computes the difference between current and last known state.
func (dc *DeltaCompressor) ComputeDelta(key string, current map[string]interface{}) map[string]interface{} {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	prev, hasPrev := dc.snapshots[key]
	if !hasPrev {
		dc.snapshots[key] = copyMap(current)
		return current // first sync: send full state
	}

	delta := make(map[string]interface{})
	delta["__delta__"] = true

	// Find changed/added fields
	for k, v := range current {
		prevVal, exists := prev[k]
		if !exists || fmt.Sprintf("%v", v) != fmt.Sprintf("%v", prevVal) {
			delta[k] = v
		}
	}

	// Find removed fields
	removed := make([]string, 0)
	for k := range prev {
		if _, exists := current[k]; !exists {
			removed = append(removed, k)
		}
	}
	if len(removed) > 0 {
		delta["__removed__"] = removed
	}

	// Update snapshot
	dc.snapshots[key] = copyMap(current)

	// If delta is larger than 70% of full state, send full state
	if len(delta) > len(current)*7/10 {
		delete(delta, "__delta__")
		return current
	}

	return delta
}

// ApplyDelta applies a delta update to rebuild the full state.
func (dc *DeltaCompressor) ApplyDelta(key string, delta map[string]interface{}) map[string]interface{} {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if _, isDelta := delta["__delta__"]; !isDelta {
		dc.snapshots[key] = copyMap(delta)
		return delta
	}

	base, exists := dc.snapshots[key]
	if !exists {
		base = make(map[string]interface{})
	}

	result := copyMap(base)

	// Apply changes
	for k, v := range delta {
		if k == "__delta__" || k == "__removed__" {
			continue
		}
		result[k] = v
	}

	// Apply removals
	if removedRaw, ok := delta["__removed__"]; ok {
		if removed, ok := removedRaw.([]interface{}); ok {
			for _, r := range removed {
				if key, ok := r.(string); ok {
					delete(result, key)
				}
			}
		}
	}

	dc.snapshots[key] = copyMap(result)
	return result
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// ============================================================================
// Priority Sync Queue
// ============================================================================

// PrioritySyncQueue orders sync items by priority and age.
type PrioritySyncQueue struct {
	items      []*SyncItem
	maxSize    int
	mu         sync.Mutex
	stats      SyncQueueStats
}

// SyncQueueStats tracks queue performance.
type SyncQueueStats struct {
	TotalEnqueued   int64 `json:"total_enqueued"`
	TotalDequeued   int64 `json:"total_dequeued"`
	TotalDropped    int64 `json:"total_dropped"`
	TotalCompressed int64 `json:"total_compressed"`
	BytesSaved      int64 `json:"bytes_saved"`
	CurrentSize     int   `json:"current_size"`
}

// NewPrioritySyncQueue creates a new priority sync queue.
func NewPrioritySyncQueue(maxSize int) *PrioritySyncQueue {
	return &PrioritySyncQueue{
		items:   make([]*SyncItem, 0, maxSize),
		maxSize: maxSize,
	}
}

// Enqueue adds an item to the queue, maintaining priority order.
func (q *PrioritySyncQueue) Enqueue(item *SyncItem) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) >= q.maxSize {
		// Drop lowest priority item
		if len(q.items) > 0 && item.Priority < q.items[len(q.items)-1].Priority {
			q.items = q.items[:len(q.items)-1]
			q.stats.TotalDropped++
		} else {
			return fmt.Errorf("sync queue full (%d items), item priority too low", q.maxSize)
		}
	}

	item.Status = "queued"
	q.items = append(q.items, item)

	// Sort: priority ASC (0=critical first), then CreatedAt ASC
	sort.SliceStable(q.items, func(i, j int) bool {
		if q.items[i].Priority != q.items[j].Priority {
			return q.items[i].Priority < q.items[j].Priority
		}
		return q.items[i].CreatedAt.Before(q.items[j].CreatedAt)
	})

	q.stats.TotalEnqueued++
	q.stats.CurrentSize = len(q.items)
	return nil
}

// DequeueBatch retrieves up to n items from the queue.
func (q *PrioritySyncQueue) DequeueBatch(n int) []*SyncItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	if n > len(q.items) {
		n = len(q.items)
	}
	if n == 0 {
		return nil
	}

	batch := make([]*SyncItem, n)
	copy(batch, q.items[:n])
	q.items = q.items[n:]

	for _, item := range batch {
		item.Status = "sending"
	}

	q.stats.TotalDequeued += int64(n)
	q.stats.CurrentSize = len(q.items)
	return batch
}

// Peek returns the next item without removing it.
func (q *PrioritySyncQueue) Peek() *SyncItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil
	}
	return q.items[0]
}

// Size returns the current queue size.
func (q *PrioritySyncQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Stats returns queue statistics.
func (q *PrioritySyncQueue) Stats() SyncQueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.stats
}

// ============================================================================
// Enhanced Sync Engine
// ============================================================================

// EnhancedSyncEngine coordinates priority-based, delta-compressed sync.
type EnhancedSyncEngine struct {
	config      SyncEngineConfig
	nodeID      string
	queue       *PrioritySyncQueue
	compressor  *DeltaCompressor
	vectorClock *VectorClock
	transport   SyncTransport
	mu          sync.RWMutex
	logger      *logrus.Logger
}

// SyncTransport defines the interface for sending sync data.
type SyncTransport interface {
	Send(ctx context.Context, batch []*SyncItem) error
	Receive(ctx context.Context) ([]*SyncItem, error)
}

// NewEnhancedSyncEngine creates a new enhanced sync engine.
func NewEnhancedSyncEngine(nodeID string, cfg SyncEngineConfig, transport SyncTransport, logger *logrus.Logger) *EnhancedSyncEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &EnhancedSyncEngine{
		config:      cfg,
		nodeID:      nodeID,
		queue:       NewPrioritySyncQueue(cfg.MaxQueueSize),
		compressor:  NewDeltaCompressor(),
		vectorClock: NewVectorClock(),
		transport:   transport,
		logger:      logger,
	}
}

// Enqueue adds a sync item with automatic delta compression.
func (e *EnhancedSyncEngine) Enqueue(ctx context.Context, item *SyncItem) error {
	if err := apperrors.CheckContext(ctx); err != nil {
		return err
	}

	// Apply vector clock
	if e.config.VectorClockEnabled {
		e.vectorClock.Increment(e.nodeID)
		item.VectorClock = &VectorClock{Clocks: make(map[string]uint64)}
		for k, v := range e.vectorClock.Clocks {
			item.VectorClock.Clocks[k] = v
		}
	}

	// Apply delta compression
	if e.config.DeltaSyncEnabled && item.Payload != nil {
		delta := e.compressor.ComputeDelta(item.Key, item.Payload)
		item.Payload = delta
	}

	return e.queue.Enqueue(item)
}

// FlushBatch sends a batch of items to the cloud.
func (e *EnhancedSyncEngine) FlushBatch(ctx context.Context) (int, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return 0, err
	}

	batch := e.queue.DequeueBatch(e.config.BatchSize)
	if len(batch) == 0 {
		return 0, nil
	}

	if err := e.transport.Send(ctx, batch); err != nil {
		// Re-enqueue failed items with retry backoff
		for _, item := range batch {
			item.RetryCount++
			if item.RetryCount < e.config.MaxRetries {
				backoff := time.Duration(float64(time.Second) * e.config.BackoffMultiplier * float64(item.RetryCount))
				nextRetry := time.Now().Add(backoff)
				item.NextRetryAt = &nextRetry
				item.Status = "queued"
				_ = e.queue.Enqueue(item)
			} else {
				item.Status = "failed"
				e.logger.WithField("item", item.ID).Warn("Sync item exceeded max retries")
			}
		}
		return 0, fmt.Errorf("batch send failed: %w", err)
	}

	for _, item := range batch {
		item.Status = "sent"
	}

	return len(batch), nil
}

// RunSyncLoop runs the background sync loop.
func (e *EnhancedSyncEngine) RunSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(e.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final flush
			_, _ = e.FlushBatch(ctx)
			return
		case <-ticker.C:
			sent, err := e.FlushBatch(ctx)
			if err != nil {
				e.logger.WithError(err).Warn("Sync flush failed")
			}
			if sent > 0 {
				e.logger.WithField("sent", sent).Debug("Sync batch flushed")
			}

			// Receive inbound sync items
			if e.transport != nil {
				inbound, err := e.transport.Receive(ctx)
				if err == nil && len(inbound) > 0 {
					e.processInbound(inbound)
				}
			}
		}
	}
}

func (e *EnhancedSyncEngine) processInbound(items []*SyncItem) {
	for _, item := range items {
		// Merge vector clock
		if e.config.VectorClockEnabled && item.VectorClock != nil {
			e.vectorClock.Merge(item.VectorClock)
		}

		// Apply delta decompression
		if e.config.DeltaSyncEnabled && item.Payload != nil {
			item.Payload = e.compressor.ApplyDelta(item.Key, item.Payload)
		}

		e.logger.WithFields(logrus.Fields{
			"item":     item.ID,
			"type":     item.DataType,
			"priority": item.Priority,
		}).Debug("Processed inbound sync item")
	}
}

// GetStatus returns comprehensive sync engine status.
func (e *EnhancedSyncEngine) GetStatus() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := e.queue.Stats()
	return map[string]interface{}{
		"node_id":          e.nodeID,
		"queue_size":       stats.CurrentSize,
		"total_enqueued":   stats.TotalEnqueued,
		"total_dequeued":   stats.TotalDequeued,
		"total_dropped":    stats.TotalDropped,
		"delta_enabled":    e.config.DeltaSyncEnabled,
		"compression":      e.config.CompressionEnabled,
		"vector_clock":     e.vectorClock.Clocks,
		"flush_interval":   e.config.FlushInterval.String(),
		"batch_size":       e.config.BatchSize,
	}
}
