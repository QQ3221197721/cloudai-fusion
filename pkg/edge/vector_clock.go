// Package edgeautonomy - Vector Clock for distributed system coordination
package edge

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// OPTIMIZED VECTOR CLOCK FOR EDGE-AUTONOMY DISTRIBUTED SYSTEMS
// IMPLEMENTS PROPER CAUSAL ORDERING WITH EVENT MERGING!
// ============================================================================

// CausalVectorClock implements causal ordering in distributed systems
type CausalVectorClock struct {
	mu       sync.RWMutex
	processes map[string]int
	logger   *logrus.Logger
}

// Event represents a distributed event with timestamp
type Event struct {
	ID         string
	ProcessID  string
	Timestamp  time.Time
	Message    interface{}
	Operation  string // "create", "update", "delete", "sync"
	Version    int
	Metadata   map[string]string
}

// ============================================================================
// CORE VECTOR CLOCK OPERATIONS
// ============================================================================

// NewCausalVectorClock creates new clock instance
func NewCausalVectorClock(processIDs []string, logger *logrus.Logger) *CausalVectorClock {
	vc := &CausalVectorClock{
		processes: make(map[string]int),
		logger:    logger,
	}
	
	// Initialize all process IDs to 0
	for _, pid := range processIDs {
		vc.processes[pid] = 0
	}
	
	return vc
}

// Tick increments own counter and returns new timestamp
func (vc *CausalVectorClock) Tick() time.Time {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	
	now := time.Now()
	
	// Increment own counter
	if vc.processes["self"] == 0 {
		vc.processes["self"] = 1
	} else {
		vc.processes["self"]++
	}
	
	return now
}

// GetTimestamp returns current vector clock state
func (vc *CausalVectorClock) GetTimestamp() map[string]int {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	
	result := make(map[string]int)
	for k, v := range vc.processes {
		result[k] = v
	}
	
	return result
}

// Compare compares two vector clocks
func (vc *CausalVectorClock) Compare(other *CausalVectorClock) int {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	hasLess := false
	hasGreater := false
	
	for pid := range vc.processes {
		selfVal := vc.processes[pid]
		otherVal := other.processes[pid]
		
		if selfVal < otherVal {
			hasLess = true
		} else if selfVal > otherVal {
			hasGreater = true
		}
		
		if hasLess && hasGreater {
			return 2 // Concurrent
		}
	}
	
	if hasLess {
		return -1 // Before
	} else if hasGreater {
		return 1 // After
	}
	return 0 // Equal
}

// Merge combines two vector clocks (element-wise maximum)
func (vc *CausalVectorClock) Merge(other *CausalVectorClock) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	// Merge into common set of processes
	allProcesses := make(map[string]bool)
	for pid := range vc.processes {
		allProcesses[pid] = true
	}
	for pid := range other.processes {
		allProcesses[pid] = true
	}
	
	// Take maximum for each process
	for pid := range allProcesses {
		selfVal := vc.processes[pid]
		otherVal := other.processes[pid]
		
		if otherVal > selfVal {
			vc.processes[pid] = otherVal
		}
	}
}

// SendEvent creates event with timestamp
func (vc *CausalVectorClock) SendEvent(event Event) time.Time {
	event.Timestamp = vc.Tick()
	event.Version = vc.processes["self"]
	return event.Timestamp
}

// ReceiveEvent updates clock based on received event
func (vc *CausalVectorClock) ReceiveEvent(event Event) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	
	// Update local clock to max(local, event.timestamp)
	for pid, eventVer := range vc.extractVersionFromEvent(&event) {
		currentVer := vc.processes[pid]
		if eventVer > currentVer {
			vc.processes[pid] = eventVer
		}
	}
}

// Extract version from event metadata
func (vc *CausalVectorClock) extractVersionFromEvent(event *Event) map[string]int {
	versionMap := make(map[string]int)
	versionMap[event.ProcessID] = event.Version
	return versionMap
}

// CompareFromMaps compares two vector clock maps and returns their causal relationship.
// Returns:
//   -1 if a happens-before b (a has smaller value for some key and not greater for any)
//    0 if a equals b (all values identical)
//   +1 if a happens-after b (a has greater value for some key and not smaller for any)
//   +2 if concurrent (neither dominates - typical in distributed systems)
func (vc *CausalVectorClock) CompareFromMaps(a, b map[string]int) int {
	hasLess, hasGreater := false, false
	allKeys := make(map[string]bool)
	for k := range a { allKeys[k] = true }
	for k := range b { allKeys[k] = true }
	for key := range allKeys {
		aVal, bVal := a[key], b[key]
		if aVal < bVal {
			hasLess = true
		} else if aVal > bVal {
			hasGreater = true
		}
		if hasLess && hasGreater { return 2 } // Concurrent
	}
	if hasLess { return -1 }
	if hasGreater { return 1 }
	return 0
}
