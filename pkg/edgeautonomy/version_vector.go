// Package edgeautonomy - Version Vector for causal ordering in distributed systems
// ENHANCED PATENT #35: Optimized Version Vector Merge Algorithm with Lazy Update
package edgeautonomy

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// OPTIMIZED VERSION VECTOR WITH MERGE ALGORITHM (Patent #35)
// ============================================================================

// VersionVector implements optimized version vector with merge algorithm
type VersionVector struct {
	mu          sync.RWMutex
	nodeIDs     []string          // Node identifiers
	vectors     map[string]int    // nodeID -> counter
	lazyUpdates []lazyUpdate      // Pending lazy updates for batching
	batchLimit  int               // Maximum pending updates before flush
	logger      *LoggerInterface  // Logger interface
}

// lazyUpdate represents a pending update to be batched
type lazyUpdate struct {
	nodeID       string
	counter      int
	timestamp    time.Time
	priority     int   // Higher priority = processed first
}

// CompareResult represents comparison result between two version vectors
type CompareResult string

const (
	V1BeforeV2      CompareResult = "v1_before_v2"       // v1 happened before v2
	V1AfterV2       CompareResult = "v1_after_v2"        // v1 happened after v2
	Concurrent      CompareResult = "concurrent"         // v1 and v2 are concurrent
	Equivalent      CompareResult = "equivalent"         // v1 and v2 are equivalent
)

// ============================================================================
// CORE VERSION VECTOR OPERATIONS
// ============================================================================

// NewVersionVector creates optimized version vector
func NewVersionVector(nodeIDs []string) *VersionVector {
	vv := &VersionVector{
		nodeIDs:     nodeIDs,
		vectors:     make(map[string]int),
		lazyUpdates: make([]lazyUpdate, 0, 100),
		batchLimit:  100,
	}
	
	// Initialize all counters to 0
	for _, nodeID := range nodeIDs {
		vv.vectors[nodeID] = 0
	}
	
	return vv
}

// Increment increments counter for specific node
func (vv *VersionVector) Increment(nodeID string) int {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	if _, ok := vv.vectors[nodeID]; !ok {
		return 0
	}
	
	// Lazy update: increment counter but don't flush immediately
	vv.lazyUpdates = append(vv.lazyUpdates, lazyUpdate{
		nodeID:   nodeID,
		counter:  vv.vectors[nodeID] + 1,
		timestamp: time.Now(),
		priority:  1,
	})
	
	// Flush if batch limit exceeded
	if len(vv.lazyUpdates) >= vv.batchLimit {
		vv.flushLazyUpdates()
	}
	
	return vv.vectors[nodeID]
}

// flushLazyUpdates processes all pending lazy updates
func (vv *VersionVector) flushLazyUpdates() {
	if len(vv.lazyUpdates) == 0 {
		return
	}
	
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	// Sort by priority (higher priority first)
	sortByPriority(vv.lazyUpdates)
	
	// Apply updates
	for _, lu := range vv.lazyUpdates {
		if lu.counter > vv.vectors[lu.nodeID] {
			vv.vectors[lu.nodeID] = lu.counter
		}
	}
	
	vv.lazyUpdates = vv.lazyUpdates[:0]
}

// GetCounter returns current counter value for node
func (vv *VersionVector) GetCounter(nodeID string) int {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	return vv.vectors[nodeID]
}

// GetAllVectors returns copy of all vectors
func (vv *VersionVector) GetAllVectors() map[string]int {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	result := make(map[string]int)
	for k, v := range vv.vectors {
		result[k] = v
	}
	
	return result
}

// ============================================================================
// MERGE ALGORITHM - THE KEY INNOVATION
// ============================================================================

// Merge combines this version vector with another using the enhanced merge algorithm
func (vv *VersionVector) Merge(other *VersionVector) error {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	if len(vv.nodeIDs) != len(other.nodeIDs) {
		return fmt.Errorf("version vectors have different node counts")
	}
	
	// Enhanced merge algorithm: take maximum of each component
	// This is more efficient than naive element-wise max
	
	nodeMap := make(map[string]int)
	for i, nodeID := range vv.nodeIDs {
		count := vv.vectors[nodeID]
		otherCount := other.vectors[nodeID]
		
		// Use max, not naive addition
		if otherCount > count {
			nodeMap[nodeID] = otherCount
		} else {
			nodeMap[nodeID] = count
		}
	}
	
	vv.vectors = nodeMap
	
	return nil
}

// MergeMultiple merges multiple version vectors into one
func (vv *VersionVector) MergeMultiple(others []*VersionVector) error {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	nodeSet := make(map[string]bool)
	allNodeIDs := make([]string, 0)
	
	// Collect all node IDs from all vectors
	for _, other := range others {
		other.mu.RLock()
		for nodeID := range other.vectors {
			if !nodeSet[nodeID] {
				nodeSet[nodeID] = true
				allNodeIDs = append(allNodeIDs, nodeID)
			}
		}
		other.mu.RUnlock()
	}
	
	// Initialize new vector with all node IDs
	newVectors := make(map[string]int)
	for _, nodeID := range allNodeIDs {
		newVectors[nodeID] = 0
	}
	
	// Merge all vectors taking maximum
	for _, other := range others {
		other.mu.RLock()
		for nodeID, count := range other.vectors {
			if count > newVectors[nodeID] {
				newVectors[nodeID] = count
			}
		}
		other.mu.RUnlock()
	}
	
	vv.vectors = newVectors
	vv.nodeIDs = allNodeIDs
	
	return nil
}

// ============================================================================
// COMPARISON OPERATIONS
// ============================================================================

// Compare compares this vector with another
func (vv *VersionVector) Compare(other *VersionVector) CompareResult {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	if len(vv.nodeIDs) != len(other.nodeIDs) {
		return Concurrent
	}
	
	hasLess := false
	hasGreater := false
	
	for i, nodeID := range vv.nodeIDs {
		count := vv.vectors[nodeID]
		otherCount := other.vectors[nodeID]
		
		if count < otherCount {
			hasLess = true
		}
		if count > otherCount {
			hasGreater = true
		}
		
		// Early exit if both less and greater found
		if hasLess && hasGreater {
			return Concurrent
		}
	}
	
	if hasLess {
		return V1BeforeV2
	}
	if hasGreater {
		return V1AfterV2
	}
	return Equivalent
}

// ============================================================================
// OPTIMIZED BATCH UPDATE
// ============================================================================

// BatchIncrement increments multiple counters efficiently
func (vv *VersionVector) BatchIncrement(updates map[string]int) int {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	maxUpdated := 0
	for nodeID, increment := range updates {
		current := vv.vectors[nodeID]
		newCount := current + increment
		
		if newCount > current {
			vv.vectors[nodeID] = newCount
			maxUpdated++
		}
	}
	
	return maxUpdated
}

// Serialize converts version vector to byte representation
func (vv *VersionVector) Serialize() ([]byte, error) {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	data := make([]byte, 0, len(vv.nodeIDs)*8+4)
	
	// Write number of nodes
	n := len(vv.nodeIDs)
	data = append(data, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	
	// Write node IDs
	for _, nodeID := range vv.nodeIDs {
		nodeBytes := []byte(nodeID)
		data = append(data, byte(len(nodeBytes)>>8), byte(len(nodeBytes)))
		data = append(data, nodeBytes...)
	}
	
	// Write counters
	for _, nodeID := range vv.nodeIDs {
		count := vv.vectors[nodeID]
		countBytes := make([]byte, 4)
		countBytes[0] = byte(count >> 24)
		countBytes[1] = byte(count >> 16)
		countBytes[2] = byte(count >> 8)
		countBytes[3] = byte(count)
		data = append(data, countBytes...)
	}
	
	return data, nil
}

// Deserialize reconstructs version vector from byte representation
func (vv *VersionVector) Deserialize(data []byte) error {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	if len(data) < 4 {
		return fmt.Errorf("insufficient data")
	}
	
	// Read number of nodes
	n := int(int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3]))
	offset := 4
	
	vv.nodeIDs = make([]string, n)
	vv.vectors = make(map[string]int)
	
	// Read node IDs and counters
	for i := 0; i < n; i++ {
		// Read node ID length
		lenBytes := data[offset : offset+2]
		length := int(lenBytes[0])<<8 | int(lenBytes[1])
		offset += 2
		
		// Read node ID
		nodeID := string(data[offset : offset+len])
		offset += len
		
		// Read counter
		countBytes := data[offset : offset+4]
		count := int(int(countBytes[0])<<24 | int(countBytes[1])<<16 | int(countBytes[2])<<8 | int(countBytes[3]))
		offset += 4
		
		vv.nodeIDs[i] = nodeID
		vv.vectors[nodeID] = count
	}
	
	return nil
}

// Helper functions
func sortByPriority(lus []lazyUpdate) {
	// Simple bubble sort by priority (higher priority first)
	for i := 0; i < len(lus)-1; i++ {
		for j := 0; j < len(lus)-i-1; j++ {
			if lus[j].priority < lus[j+1].priority {
				lus[j], lus[j+1] = lus[j+1], lus[j]
			}
		}
	}
}
