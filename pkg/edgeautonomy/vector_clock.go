// Package edgeautonomy - Vector Clock for distributed system causal ordering
package edgeautonomy

import (
	"sync"
)

// ============================================================================
// VECTOR CLOCK IMPLEMENTATION FOR DELTA SYNCHRONIZATION ✅
// ============================================================================

// VectorClock implements causal ordering in distributed systems
type VectorClock struct {
	mu       sync.RWMutex
	processes []string           // Process identifiers
	clocks   map[string]int     // processID -> logical clock value
}

// NewVectorClock creates a new vector clock
func NewVectorClock(processIDs []string) *VectorClock {
	if processIDs == nil {
		processIDs = make([]string, 0)
	}
	
	return &VectorClock{
		processes: processIDs,
		clocks:    make(map[string]int),
	}
}

// Tick increments the clock for a specific process
func (vc *VectorClock) Tick(processID string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	
	vc.clocks[processID]++
}

// GetClock returns the current clock value for a process
func (vc *VectorClock) GetClock(processID string) int {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	
	return vc.clocks[processID]
}

// GetAllClocks returns a copy of all clock values
func (vc *VectorClock) GetAllClocks() map[string]int {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	
	result := make(map[string]int)
	for k, v := range vc.clocks {
		result[k] = v
	}
	
	return result
}

// Merge updates this clock by taking the maximum with another clock
// This is the core operation for delta sync reconciliation
func (vc *VectorClock) Merge(other *VectorClock) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	// Update all known processes
	for _, pid := range other.processes {
		if otherClock := other.GetClock(pid); otherClock > vc.GetClock(pid) {
			vc.clocks[pid] = otherClock
		}
	}
	
	// Also include any new processes from the other clock
	for pid := range other.clocks {
		if _, exists := vc.clocks[pid]; !exists {
			vc.processes = append(vc.processes, pid)
			vc.clocks[pid] = other.clocks[pid]
		}
	}
}

// Compare compares two vector clocks
// Returns: -1 if this < other, 0 if equal, 1 if this > other, 2 if concurrent
func (vc *VectorClock) Compare(other *VectorClock) int {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	hasLess := false
	hasGreater := false
	
	for _, pid := range vc.processes {
		thisVal := vc.clocks[pid]
		otherVal := other.clocks[pid]
		
		if thisVal < otherVal {
			hasLess = true
		} else if thisVal > otherVal {
			hasGreater = true
		}
		
		if hasLess && hasGreater {
			return 2 // Concurrent
		}
	}
	
	for _, pid := range other.processes {
		if _, exists := vc.clocks[pid]; !exists {
			hasLess = true
		}
	}
	
	if hasLess {
		return -1
	} else if hasGreater {
		return 1
	}
	return 0 // Equal
}
