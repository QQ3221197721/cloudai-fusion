// Package edgeautonomy - Common types generated for Edge Autonomy functionality
package edgeautonomy

import (
	"time"
)

// ============================================================================
// ADDED TYPES TO SUPPORT BUILDING IGNORED FILES
// ============================================================================

// LocalDecisionRecord represents a decision made at the edge
type LocalDecisionRecord struct {
	ID          string            `json:"id"`
	Version     int64             `json:"version"`
	Timestamp   time.Time         `json:"timestamp"`
	Priority    int               `json:"priority"`
	Cause       string            `json:"cause"`
	Action      *DecisionAction   `json:"action"`
	VersionVec  []int             `json:"version_vec"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// CloudDecisionRecord represents a decision received from cloud
type CloudDecisionRecord struct {
	ID          string            `json:"id"`
	Version     int64             `json:"version"`
	Timestamp   time.Time         `json:"timestamp"`
	Priority    int               `json:"priority"`
	Cause       string            `json:"cause"`
	Action      DecisionAction    `json:"action"`
	VersionVec  []int             `json:"version_vec"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ComparisonResult describes relationship between two decisions
type ComparisonResult string

const (
	LocalWins        ComparisonResult = "local_wins"
	CloudWins        ComparisonResult = "cloud_wins"
	Concurrent       ComparisonResult = "concurrent"
	Equivalent       ComparisonResult = "equivalent"
	LocalBeforeCloud ComparisonResult = "local_before_cloud"
	LocalAfterCloud  ComparisonResult = "local_after_cloud"
)

// compareVectors compares two version vectors
func compareVectors(vv1, vv2 []int) ComparisonResult {
	if len(vv1) != len(vv2) {
		return Concurrent
	}
	
	hasLess := false
	hasGreater := false
	
	for i := range vv1 {
		if vv1[i] < vv2[i] {
			hasLess = true
		} else if vv1[i] > vv2[i] {
			hasGreater = true
		}
		
		if hasLess && hasGreater {
			return Concurrent
		}
	}
	
	if hasLess {
		return LocalBeforeCloud
	} else if hasGreater {
		return LocalAfterCloud
	}
	return Equivalent
}

// Common metrics types for conflict resolution
type ConflictMetrics struct {
	TotalConflicts        int64
	ResolvedConflicts     int64
	AverageResolutionTime float64
	StrategyCounts        map[string]int64
}

// CacheMetrics for cache performance tracking
type CacheMetrics struct {
	Hits     int64
	Misses   int64
	Stores   int64
	Updates  int64
	Applies  int64
	Merges   int64
	Prunes   int64
}

func NewConflictMetrics() *ConflictMetrics {
	return &ConflictMetrics{
		StrategyCounts: make(map[string]int64),
	}
}

func NewCacheMetrics() *CacheMetrics {
	return &CacheMetrics{}
}

func (cm *ConflictMetrics) RecordConflict(strategy string) {
	cm.TotalConflicts++
	cm.StrategyCounts[strategy]++
}

func (cm *ConflictMetrics) RecordResolved() {
	cm.ResolvedConflicts++
}

func (cm *CacheMetrics) RecordHit() {
	cm.Hits++
}

func (cm *CacheMetrics) RecordMiss() {
	cm.Misses++
}

func (cm *CacheMetrics) RecordStore(id string) {
	cm.Stores++
}

func (cm *CacheMetrics) RecordUpdate(id string) {
	cm.Updates++
}

func (cm *CacheMetrics) RecordApply(id string) {
	cm.Applies++
}

func (cm *CacheMetrics) RecordMerge(id string) {
	cm.Merges++
}

func (cm *CacheMetrics) RecordPrune() {
	cm.Prunes++
}

func (cm *CacheMetrics) RecordMergePrune(count int) {
	// Track merged prune count
	_ = count
}

func (cm *ConflictMetrics) RecordPrune() {
	// No-op
}

