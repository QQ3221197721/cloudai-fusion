// Package edgeautonomy provides core version vector implementation for causality tracking.
// Extends existing policy receipt structure with distributed system consistency guarantees.
package edgeautonomy

import (
	"fmt"
	"sync"
)

// ============================================================================
// VersionVector - Causality Tracking Algorithm
// Implements efficient vector clock for detecting concurrent vs causal updates
// ============================================================================

// ComparisonResult represents the relationship between two version vectors
type ComparisonResult int

const (
	EQUIVALENT        ComparisonResult = iota // Same causality
	V1_CAUSAL_BEFORE_V2                       // V1 happened before V2
	V1_CAUSAL_AFTER_V2                        // V1 happened after V2  
	CONFLICT_DETECTED                         // Concurrent updates (both less and greater)
	UNKNOWN_RELATIONSHIP                      // Invalid input or incompatible sizes
)

// String returns human-readable description of comparison result
func (cr ComparisonResult) String() string {
	switch cr {
	case EQUIVALENT:
		return "EQUIVALENT"
	case V1_CAUSAL_BEFORE_V2:
		return "V1 CAUSAL BEFORE V2"
	case V1_CAUSAL_AFTER_V2:
		return "V1 CAUSAL AFTER V2"
	case CONFLICT_DETECTED:
		return "CONFLICT_DETECTED"
	default:
		return "UNKNOWN_RELATIONSHIP"
	}
}

// VersionVector implements a lightweight vector clock for edge node coordination.
// Each node maintains its own vector component, and all nodes share knowledge of 
// other nodes in the system through the nodeIDs slice.
type VersionVector struct {
	nodeIDs   []string
	vectors   map[string][]int  // nodeID -> vector clock value array
	mu        sync.RWMutex
	maxSize   int               // Maximum number of known nodes
	versionNum int              // Current version number (for debugging/logging)
}

// NewVersionVector creates a new version vector with specified node IDs
func NewVersionVector(nodeIDs []string) *VersionVector {
	if len(nodeIDs) == 0 {
		panic("nodeIDs cannot be empty")
	}
	
	// Deduplicate node IDs
	seen := make(map[string]bool)
	uniqueNodes := make([]string, 0, len(nodeIDs))
	for _, nid := range nodeIDs {
		if !seen[nid] {
			seen[nid] = true
			uniqueNodes = append(uniqueNodes, nid)
		}
	}
	
	vv := &VersionVector{
		nodeIDs: uniqueNodes,
		vectors: make(map[string][]int),
		maxSize: len(uniqueNodes),
	}
	
	// Initialize vector for each known node
	for _, nid := range uniqueNodes {
		vv.vectors[nid] = make([]int, len(uniqueNodes))
	}
	
	return vv
}

// Update increments this node's component and returns a copy of the current state
func (vv *VersionVector) Update(nodeID string) []int {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	// Find our index in the node list
	myIdx := -1
	for i, nid := range vv.nodeIDs {
		if nid == nodeID {
			myIdx = i
			break
		}
	}
	
	if myIdx < 0 {
		panic(fmt.Sprintf("unknown node ID %s in version vector", nodeID))
	}
	
	// Make a deep copy of current state
	currentVec := make([]int, len(vv.nodeIDs))
	if vec, exists := vv.vectors[nodeID]; exists {
		copy(currentVec, vec)
	}
	
	// Increment our own component
	currentVec[myIdx]++
	
	// Update stored state
	vv.vectors[nodeID] = currentVec
	
	// Track version for debugging
	vv.versionNum++
	
	return currentVec
}

// GetVector returns current state without modifying it (thread-safe read)
func (vv *VersionVector) GetVector(nodeID string) ([]int, error) {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	vec, exists := vv.vectors[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found in version vector", nodeID)
	}
	
	// Return copy to prevent external mutation
	result := make([]int, len(vec))
	copy(result, vec)
	
	return result, nil
}

// Compare determines causal relationship between two version vectors
func (vv *VersionVector) Compare(v1, v2 []int) ComparisonResult {
	if len(v1) != len(v2) || len(v1) != len(vv.nodeIDs) {
		return UNKNOWN_RELATIONSHIP
	}
	
	hasLess := false
	hasGreater := false
	
	for i := range v1 {
		switch {
		case v1[i] < v2[i]:
			hasLess = true
		case v1[i] > v2[i]:
			hasGreater = true
		}
		
		// Early exit if both conditions met
		if hasLess && hasGreater {
			return CONFLICT_DETECTED
		}
	}
	
	switch {
	case !hasLess && hasGreater:
		return V1_CAUSAL_BEFORE_V2 // V2 dominates V1
	case hasLess && !hasGreater:
		return V1_CAUSAL_AFTER_V2 // V1 dominates V2
	default:
		return EQUIVALENT // Identical
	}
}

// MergeWith incorporates another vector into this one (for synchronization)
func (vv *VersionVector) MergeWith(otherNodeID string, otherVec []int) error {
	vv.mu.Lock()
	defer vv.mu.Unlock()
	
	// Validate size compatibility
	if len(otherVec) != len(vv.nodeIDs) {
		return fmt.Errorf("incompatible vector size: got %d, expected %d", 
			len(otherVec), len(vv.nodeIDs))
	}
	
	// Initialize if this is first interaction with this node
	if _, exists := vv.vectors[otherNodeID]; !exists {
		vv.vectors[otherNodeID] = make([]int, len(vv.nodeIDs))
	}
	
	// Element-wise maximum merge
	for i, val := range otherVec {
		if val > vv.vectors[otherNodeID][i] {
			vv.vectors[otherNodeID][i] = val
		}
	}
	
	return nil
}

// GetKnownNodes returns list of all known nodes (safe copy)
func (vv *VersionVector) GetKnownNodes() []string {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	
	result := make([]string, len(vv.nodeIDs))
	copy(result, vv.nodeIDs)
	
	return result
}

// GetVersion returns current version number for debugging
func (vv *VersionVector) GetVersion() int {
	vv.mu.RLock()
	defer vv.mu.RUnlock()
	return vv.versionNum
}
