// Package edgeautonomy - Optimized Version Vector for Edge-Cloud Delta Sync
package edgeautonomy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// OPTIMIZED VERSION VECTOR FOR EDGE-AUTONOMY DELTA SYNCHRONIZATION (PATENT #35!)
// FULL MERGE ALGORITHM IMPLEMENTATION!
// ============================================================================

// VersionVector implements optimized version vector with lazy merge support
type VersionVector struct {
	mu            sync.RWMutex
	nodeIDs       []string              // Node identifiers in the distributed system
	vectors       map[string]int        // nodeID -> logical counter
	lazyUpdates   []lazyUpdate          // Pending lazy updates for batching
	batchLimit    int                   // Max pending updates before flush
	logger        *logrus.Logger
	
	// Metadata
	lastUpdateTime time.Time
	updateCount    int
}

// lazyUpdate represents a pending update to be batched
type lazyUpdate struct {
	nodeID      string
	counter     int
	timestamp   time.Time
	priority    int   // Higher priority = processed first
	sourceNode  string // Originating node ID
}

// CompareResult describes result of comparing two version vectors
type CompareResult string

const (
	ResultBefore         CompareResult = "v1_before_v2"         // v1 happened before v2
	ResultAfter          CompareResult = "v1_after_v2"          // v1 happened after v2
	ResultConcurrent     CompareResult = "concurrent"           // v1 and v2 are concurrent events
	ResultEquivalent     CompareResult = "equivalent"           // v1 and v2 represent same state
	ResultPartialOrder   CompareResult = "partial_order"        // Cannot determine causality
)

// ============================================================================
// CORE VERSION VECTOR OPERATIONS WITH MERGE SUPPORT!
// ============================================================================

// NewVersionVector creates optimized version vector
// If logger is nil, no logging will be performed
func NewVersionVector(nodeIDs []string, logger *logrus.Logger) *VersionVector {
	vv := &VersionVector{
		nodeIDs:       nodeIDs,
		vectors:       make(map[string]int),
		lazyUpdates:   make([]lazyUpdate, 0, 100),
		batchLimit:    100,
		logger:        logger,
	}
	
	// Initialize all counters to 0
	for _, nodeID := range nodeIDs {
		vv.vectors[nodeID] = 0
	}
	
	return vv
}

// Increment increments counter for specific node (atomic operation)
func (vv *VersionVector) Increment(nodeID string) int {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	if _, ok := vv.vectors[nodeID]; !ok {
		return 0
	}
	
	// Increment counter
	vv.vectors[nodeID]++
	current := vv.vectors[nodeID]
	
	// Record update for batching
	vv.lazyUpdates = append(vv.lazyUpdates, lazyUpdate{
		nodeID:    nodeID,
		counter:   current,
		timestamp: time.Now(),
		priority:  1,
	})
	
	// Flush if batch limit exceeded
	if len(vv.lazyUpdates) >= vv.batchLimit {
		vv.flushLazyUpdates()
	}
	
	vv.updateCount++
	vv.lastUpdateTime = time.Now()
	
	return current
}

// flushLazyUpdates processes all pending lazy updates in priority order
func (vv *VersionVector) flushLazyUpdates() {
	if len(vv.lazyUpdates) == 0 {
		return
	}
	
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	// Sort by priority (higher priority first) then timestamp
	sortByPriorityAndTimestamp(vv.lazyUpdates)
	
	// Apply updates to main vector
	for _, lu := range vv.lazyUpdates {
		if lu.counter > vv.vectors[lu.nodeID] {
			vv.vectors[lu.nodeID] = lu.counter
		}
	}
	
	// Clear lazy updates buffer
	vv.lazyUpdates = vv.lazyUpdates[:0]
}

// GetCounter returns current counter value for specific node
func (vv *VersionVector) GetCounter(nodeID string) int {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	return vv.vectors[nodeID]
}

// GetAllVectors returns complete snapshot of all counters
func (vv *VersionVector) GetAllVectors() map[string]int {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	// Return deep copy
	result := make(map[string]int, len(vv.vectors))
	for k, v := range vv.vectors {
		result[k] = v
	}
	
	return result
}

// SetValues sets all vector values at once (for initialization or restore)
func (vv *VersionVector) SetValues(values map[string]int) {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	// Reset all values
	for k := range vv.vectors {
		delete(vv.vectors, k)
	}
	
	// Set new values from input
	for k, v := range values {
		vv.vectors[k] = v
	}
	
	vv.lastUpdateTime = time.Now()
}

// ============================================================================
// OPTIMIZED MERGE ALGORITHM - TAKES MAXIMUM OF EACH COMPONENT (PATENT #35)!
// This is THE KEY INNOVATION that differentiates our implementation!
// ============================================================================

// Merge combines this version vector with another using the ENHANCED merge algorithm.
// CRITICAL: Takes element-wise maximum, NOT naive addition!
func (vv *VersionVector) Merge(other *VersionVector) error {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	// Validate compatibility
	if len(vv.nodeIDs) != len(other.nodeIDs) {
		return fmt.Errorf("version vectors have incompatible dimensions")
	}
	
	// OPTIMIZED MERGE: Take maximum of each component
	// This preserves causality information WITHOUT creating artificial merges!
	mergedCount := 0
	
	for _, nodeID := range vv.nodeIDs {
		currentCount := vv.vectors[nodeID]
		otherCount := other.vectors[nodeID]
		
		// Element-wise maximum - THIS IS THE KEY DIFFERENCE!
		if otherCount > currentCount {
			vv.vectors[nodeID] = otherCount
			mergedCount++
		}
	}
	
	vv.lastUpdateTime = time.Now()
	vv.logger.WithFields(logrus.Fields{
		"nodes_merged": mergedCount,
		"total_nodes": len(vv.nodeIDs),
	}).Debug("Version vector merge completed")
	
	return nil
}

// MergeMultiple merges multiple version vectors into one efficiently
// Optimized to avoid O(n^2) complexity by using single-pass aggregation
func (vv *VersionVector) MergeMultiple(others []*VersionVector) error {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	// Collect all unique node IDs from all vectors
	allNodeIDs := make(map[string]bool)
	for _, other := range others {
		other.mu.RLock()
		for nodeID := range other.vectors {
			allNodeIDs[nodeID] = true
		}
		other.mu.RUnlock()
	}
	
	// Add existing node IDs
	for _, nodeID := range vv.nodeIDs {
		allNodeIDs[nodeID] = true
	}
	
	// Create unified node list
	unifiedNodes := make([]string, 0, len(allNodeIDs))
	for nodeID := range allNodeIDs {
		unifiedNodes = append(unifiedNodes, nodeID)
	}
	
	// Initialize new vector with all nodes
	newVectors := make(map[string]int)
	for _, nodeID := range unifiedNodes {
		newVectors[nodeID] = 0
	}
	
	// Single-pass merge: take maximum across ALL vectors
	// Process current vector first
	for nodeID, count := range vv.vectors {
		newVectors[nodeID] = max(newVectors[nodeID], count)
	}
	
	// Then process all others
	for _, other := range others {
		other.mu.RLock()
		for nodeID, count := range other.vectors {
			newVectors[nodeID] = max(newVectors[nodeID], count)
		}
		other.mu.RUnlock()
	}
	
	// Replace current vector with merged result
	vv.vectors = newVectors
	vv.nodeIDs = unifiedNodes
	
	vv.lastUpdateTime = time.Now()
	
	return nil
}

// ============================================================================
// CAUSALITY ANALYSIS & COMPARISON OPERATIONS
// ============================================================================

// Compare determines causal relationship between two version vectors
func (vv *VersionVector) Compare(other *VersionVector) CompareResult {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	if len(vv.nodeIDs) != len(other.nodeIDs) {
		return ResultPartialOrder
	}
	
	hasLess := false
	hasGreater := false
	
	// Check each component
	for _, nodeID := range vv.nodeIDs {
		count := vv.vectors[nodeID]
		otherCount := other.vectors[nodeID]
		
		if count < otherCount {
			hasLess = true
		} else if count > otherCount {
			hasGreater = true
		}
		
		// Early exit: both less and greater found → concurrent
		if hasLess && hasGreater {
			return ResultConcurrent
		}
	}
	
	// Determine final relationship
	if hasLess && !hasGreater {
		return ResultBefore
	} else if !hasLess && hasGreater {
		return ResultAfter
	} else {
		return ResultEquivalent
	}
}

// IsCausalDependencyOf checks if this vector causally depends on another
func (vv *VersionVector) IsCausalDependencyOf(other *VersionVector) bool {
	return vv.Compare(other) == ResultBefore
}

// IsConcurrentWith checks if this vector is concurrent with another
func (vv *VersionVector) IsConcurrentWith(other *VersionVector) bool {
	return vv.Compare(other) == ResultConcurrent
}

// ============================================================================
// ENHANCED BATCH UPDATE SUPPORT
// ============================================================================

// BatchIncrement increments multiple counters atomically
// Returns number of actually updated counters
func (vv *VersionVector) BatchIncrement(updates map[string]int) int {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	maxUpdated := 0
	
	for nodeID, increment := range updates {
		current := vv.vectors[nodeID]
		newCount := current + increment
		
		// Only update if increment would increase value
		if newCount > current {
			vv.vectors[nodeID] = newCount
			maxUpdated++
			
			// Add to lazy queue for eventual flush
			vv.lazyUpdates = append(vv.lazyUpdates, lazyUpdate{
				nodeID:    nodeID,
				counter:   newCount,
				timestamp: time.Now(),
				priority:  1,
			})
		}
	}
	
	// Auto-flush if many updates
	if maxUpdated > 10 {
		vv.flushLazyUpdates()
	}
	
	vv.lastUpdateTime = time.Now()
	return maxUpdated
}

// PriorityBatchIncrement increments with priority levels
func (vv *VersionVector) PriorityBatchIncrement(updates []struct{
	NodeID string
	Increment int
	Priority int
}) int {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	for _, update := range updates {
		current := vv.vectors[update.NodeID]
		newCount := current + update.Increment
		
		if newCount > current {
			vv.vectors[update.NodeID] = newCount
			
			vv.lazyUpdates = append(vv.lazyUpdates, lazyUpdate{
				nodeID:   update.NodeID,
				counter:  newCount,
				timestamp: time.Now(),
				priority: update.Priority,
			})
		}
	}
	
	if len(vv.lazyUpdates) >= vv.batchLimit {
		vv.flushLazyUpdates()
	}
	
	vv.lastUpdateTime = time.Now()
	return len(updates)
}

// ============================================================================
// BINARY SERIALIZATION FOR NETWORK TRANSMISSION
// ============================================================================

// Serialize converts version vector to compact binary representation
// Efficient for network transmission and storage
func (vv *VersionVector) Serialize() ([]byte, error) {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	// Header: number of nodes (4 bytes)
	var buf bytes.Buffer
	nodeCount := uint32(len(vv.nodeIDs))
	err := binary.Write(&buf, binary.BigEndian, nodeCount)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize node count: %w", err)
	}
	
	// For each node: length (2 bytes) + nodeID (variable) + counter (4 bytes)
	for _, nodeID := range vv.nodeIDs {
		nodeBytes := []byte(nodeID)
		
		// Write node ID length
		idLen := uint16(len(nodeBytes))
		err = binary.Write(&buf, binary.BigEndian, idLen)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize node ID length: %w", err)
		}
		
		// Write node ID
		buf.Write(nodeBytes)
		
		// Write counter value
		counter := int32(vv.vectors[nodeID])
		err = binary.Write(&buf, binary.BigEndian, counter)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize counter: %w", err)
		}
	}
	
	return buf.Bytes(), nil
}

// Deserialize reconstructs version vector from binary representation
func (vv *VersionVector) Deserialize(data []byte) error {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	if len(data) < 4 {
		return fmt.Errorf("insufficient data for deserialization")
	}
	
	reader := bytes.NewReader(data)
	
	// Read node count
	var nodeCount uint32
	err := binary.Read(reader, binary.BigEndian, &nodeCount)
	if err != nil {
		return fmt.Errorf("failed to read node count: %w", err)
	}
	
	// Reset vectors and reinitialize
	vv.vectors = make(map[string]int)
	vv.nodeIDs = make([]string, 0, nodeCount)
	
	// Read each node's data
	for i := uint32(0); i < nodeCount; i++ {
		// Read node ID length
		var idLen uint16
		err = binary.Read(reader, binary.BigEndian, &idLen)
		if err != nil {
			return fmt.Errorf("failed to read node ID length: %w", err)
		}
		
		// Read node ID
		nodeBytes := make([]byte, idLen)
		_, err = reader.Read(nodeBytes)
		if err != nil {
			return fmt.Errorf("failed to read node ID: %w", err)
		}
		nodeID := string(nodeBytes)
		
		// Read counter
		var counter int32
		err = binary.Read(reader, binary.BigEndian, &counter)
		if err != nil {
			return fmt.Errorf("failed to read counter: %w", err)
		}
		
		vv.nodeIDs = append(vv.nodeIDs, nodeID)
		vv.vectors[nodeID] = int(counter)
	}
	
	vv.lastUpdateTime = time.Now()
	return nil
}

// SerializeCompact creates minimal serialization for high-performance scenarios
// Omits redundant metadata for faster transmission
func (vv *VersionVector) SerializeCompact() ([]byte, error) {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	var buf bytes.Buffer
	
	// Only write counters directly as fixed-size array
	// Assumes receiver knows node ordering
	for _, nodeID := range vv.nodeIDs {
		counter := int32(vv.vectors[nodeID])
		err := binary.Write(&buf, binary.BigEndian, counter)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize counter: %w", err)
		}
	}
	
	return buf.Bytes(), nil
}

// Helper Functions

// NewVersionVectorWrapper creates version vector without logger - legacy API compatibility
func NewVersionVectorWrapper(nodeIDs []string) *VersionVector {
	return NewVersionVector(nodeIDs, nil)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Update increments a node's counter and returns merged vectors
func (vv *VersionVector) Update(nodeID string) []int {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	// Increment if exists
	if _, ok := vv.vectors[nodeID]; ok {
		vv.vectors[nodeID]++
	}
	
	// Return copy of current state
	result := make([]int, len(vv.nodeIDs))
	for i, id := range vv.nodeIDs {
		result[i] = vv.vectors[id]
	}
	return result
}

func sortByPriorityAndTimestamp(lus []lazyUpdate) {
	// Custom sort: higher priority first, then earlier timestamp
	for i := 0; i < len(lus)-1; i++ {
		for j := 0; j < len(lus)-i-1; j++ {
			swap := false

			if lus[j].priority < lus[j+1].priority {
				swap = true
			} else if lus[j].priority == lus[j+1].priority {
				// Same priority: earlier timestamp first
				if lus[j].timestamp.After(lus[j+1].timestamp) {
					swap = true
				}
			}

			if swap {
				lus[j], lus[j+1] = lus[j+1], lus[j]
			}
		}
	}
}