// Package edgeautonomy provides conflict resolution strategies for reconciling 
// local offline decisions with cloud state after reconnection.
package edgeautonomy

import (
	"fmt"
	"time"
)

// ============================================================================
// Conflict Resolution Strategies
// Implements multiple policies for handling concurrent updates from edge/cloud
// ============================================================================

// ConflictStrategy defines how to resolve conflicts between local and cloud decisions
type ConflictStrategy int

const (
	LastWriterWins ConflictStrategy = iota // Timestamp-based winner
	HighestPriority                        // Higher priority wins
	CloudAuthority                         // Cloud always wins (safety-first)
	MergeCompatible                        // Attempt intelligent merge
	AUTO_SELECT                            // Auto-select based on scenario
)

// String returns human-readable strategy name
func (cs ConflictStrategy) String() string {
	strategies := []string{
		"LAST_WRITER_WINS",
		"HIGHEST_PRIORITY",
		"CLOUD_AUTHORITY",
		"MERGE_COMPATIBLE",
		"AUTO_SELECT",
	}
	if cs < 0 || int(cs) >= len(strategies) {
		return "UNKNOWN_STRATEGY"
	}
	return strategies[cs]
}

// ============================================================================
// Core Conflict Types
// ============================================================================

// ConflictReport documents a detected conflict and its resolution
type ConflictReport struct {
	LocalRecord  LocalDecisionRecord
	CloudRecord  CloudDecisionRecord
	Comparison   ComparisonResult
	Strategy     ConflictStrategy
	Winner       *ResolvedDecision
	Reason       string
	ResolvedAt   time.Time
	ManualReview bool // True if requires human intervention
}

// LocalDecisionRecord represents a decision made at the edge during offline period
type LocalDecisionRecord struct {
	ID          string            `json:"record_id"`
	NodeID      string            `json:"node_id"`
	WorkloadID  string            `json:"workload_id"`
	Timestamp   time.Time         `json:"timestamp"`
	Priority    int               `json:"priority"`
	VersionVec  []int             `json:"version_vec"`
	Decision    Decision          `json:"decision"`
}

// CloudDecisionRecord represents a cloud-authorized scheduling decision
type CloudDecisionRecord struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"node_id"`
	WorkloadID string   `json:"workload_id"`
	Timestamp time.Time `json:"timestamp"`
	Priority  int       `json:"priority"`
	VersionVec []int   `json:"version_vec"`
	Decision  Decision  `json:"decision"`
}

// ResolvedDecision represents the outcome of conflict resolution
type ResolvedDecision struct {
	ID        string `json:"record_id"`
	Source    string // "LOCAL_FIRST", "CLOUD_AUTHORITY", "MERGED"
	Decision  Decision `json:"decision"`
	Reason    string  `json:"reason"`
	Merged    bool    `json:"merged,omitempty"` // If MergeCompatible was used
}

// ============================================================================
// ConflictResolver implements reconciliation logic with multiple strategies
// ============================================================================

// ConflictResolver handles all aspects of detecting and resolving conflicts
type ConflictResolver struct {
	versionVector  *VersionVector
	strategy       ConflictStrategy
	maxConcurrent  int
	resolutionRate float64 // Successful resolutions per second
	logger         interface{} // Logger interface for flexibility
	
	// Metrics
	metrics ConflictMetrics
}

// ConflictMetrics tracks resolution performance
type ConflictMetrics struct {
	TotalResolutions    int64
	SuccessfulResolutions int64
	ManualInterventions int64
	AverageResolutionTime time.Duration
}

// NewConflictResolver creates a new resolver with specified strategy
func NewConflictResolver(vv *VersionVector, strategy ConflictStrategy, logger interface{}) *ConflictResolver {
	if vv == nil {
		panic("version vector cannot be nil")
	}
	
	return &ConflictResolver{
		versionVector: vv,
		strategy:      strategy,
		maxConcurrent: 100,
		resolutionRate: 0.0, // Will be updated dynamically
		logger:        logger,
		metrics:       ConflictMetrics{},
	}
}

// ResolveConflicts is the main entry point - processes batch conflicts
func (r *ConflictResolver) ResolveConflicts(
	localRecords []LocalDecisionRecord,
	cloudRecords []CloudDecisionRecord,
) ([]ResolvedDecision, []ConflictReport) {
	r.metrics.TotalResolutions += int64(len(localRecords))
	
	resolved := make([]ResolvedDecision, 0)
	reports := make([]ConflictReport, 0)
	
	// Index cloud records by workload ID for O(1) lookup
	cloudIndex := r.indexCloudRecords(cloudRecords)
	
	for _, lr := range localRecords {
		cr, exists := cloudIndex[lr.WorkloadID]
		
		if !exists {
			// No cloud record → accept local first
			resolved = append(resolved, ResolvedDecision{
				ID: lr.ID,
				Source: "LOCAL_FIRST",
				Decision: lr.Decision,
				Reason: "NO_CLOUD_CONFLICT",
			})
			continue
		}
		
		// Both exist → analyze causality relationship
		comparison := r.versionVector.Compare(lr.VersionVec, cr.VersionVec)
		
		report := ConflictReport{
			LocalRecord:  lr,
			CloudRecord:  cr,
			Comparison:   comparison,
			Strategy:     r.strategy,
			ResolvedAt:   time.Now().UTC(),
			ManualReview: false,
		}
		
		winning := r.selectWinner(comparison, lr, cr, report)
		
		resolved = append(resolved, *winning)
		reports = append(reports, report)
		
		// Update metrics
		r.updateMetrics(true)
	}
	
	return resolved, reports
}

// selectWinner applies the configured strategy to determine resolution
func (r *ConflictResolver) selectWinner(
	comp ComparisonResult,
	lr LocalDecisionRecord,
	cr CloudDecisionRecord,
	report ConflictReport,
) *ResolvedDecision {
	switch comp {
	case EQUIVALENT:
		// Same causality → no actual conflict
		return &ResolvedDecision{
			ID: lr.ID,
			Source: "SAME_DECISION",
			Decision: lr.Decision,
			Reason: "IDENTICAL_DECISION",
		}
		
	case V1_CAUSAL_BEFORE_V2:
		// CR happened after LR → chain of events
		// Cloud is more recent, trust cloud authority
		return &ResolvedDecision{
			ID: cr.ID,
			Source: "CLOUD_AUTHORITY",
			Decision: cr.Decision,
			Reason: "CLOUD_MORE_RECENT",
		}
		
	case V1_CAUSAL_AFTER_V2:
		// LR happened after CR → local update should win
		return &ResolvedDecision{
			ID: lr.ID,
			Source: "LOCAL_FIRST",
			Decision: lr.Decision,
			Reason: "LOCAL_MORE_RECENT",
		}
		
	case CONFLICT_DETECTED:
		// Truly concurrent updates → apply strategy
		return r.applyStrategicResolution(comp, lr, cr, report)
		
	default:
		// Unknown/invalid → default to cloud authority for safety
		return &ResolvedDecision{
			ID: cr.ID,
			Source: "CLOUD_AUTHORITY",
			Decision: cr.Decision,
			Reason: "UNKNOWN_COMPARISON_DEFAULT_TO_CLOUD",
		}
	}
}

// applyStrategicResolution uses specific conflict resolution policy
func (r *ConflictResolver) applyStrategicResolution(
	comp ComparisonResult,
	lr LocalDecisionRecord,
	cr CloudDecisionRecord,
	report ConflictReport,
) *ResolvedDecision {
	switch r.strategy {
	case LastWriterWins:
		// Timestamp comparison
		if lr.Timestamp.After(cr.Timestamp) {
			return &ResolvedDecision{
				ID: lr.ID,
				Source: "LOCAL_FIRST",
				Decision: lr.Decision,
				Reason: fmt.Sprintf("LAST_WRITER_WINS_LOCAL_%s", lr.Timestamp.Format(time.RFC3339)),
			}
		}
		return &ResolvedDecision{
			ID: cr.ID,
			Source: "CLOUD_AUTHORITY",
			Decision: cr.Decision,
			Reason: fmt.Sprintf("LAST_WRITER_WINS_CLOUD_%s", cr.Timestamp.Format(time.RFC3339)),
		}
		
	case HighestPriority:
		// Priority comparison
		if lr.Priority > cr.Priority {
			return &ResolvedDecision{
				ID: lr.ID,
				Source: "LOCAL_FIRST",
				Decision: lr.Decision,
				Reason: fmt.Sprintf("HIGHER_PRIORITY_LOCAL_%d", lr.Priority),
			}
		}
		return &ResolvedDecision{
			ID: cr.ID,
			Source: "CLOUD_AUTHORITY",
			Decision: cr.Decision,
			Reason: fmt.Sprintf("HIGHER_PRIORITY_CLOUD_%d", cr.Priority),
		}
		
	case CloudAuthority:
		// Always trust cloud for safety-critical systems
		report.ManualReview = lr.Priority > cr.Priority // Flag high-priority conflicts
		return &ResolvedDecision{
			ID: cr.ID,
			Source: "CLOUD_AUTHORITY",
			Decision: cr.Decision,
			Reason: "CLOUD_ALWAYS_WIN_SAFETY_POLICY",
		}
		
	case MergeCompatible:
		// Attempt intelligent merge of compatible fields
		merged, canMerge := r.trySmartMerge(lr, cr)
		
		if canMerge {
			report.ManualReview = false
			return &ResolvedDecision{
				ID: lr.ID,
				Source: "MERGED",
				Decision: merged,
				Reason: "INTELLIGENT_MERGE_APPLIED",
				Merged: true,
			}
		}
		
		// Fall back to cloud authority if merge fails
		report.ManualReview = true
		return &ResolvedDecision{
			ID: cr.ID,
			Source: "CLOUD_AUTHORITY",
			Decision: cr.Decision,
			Reason: "MERGE_FAILED_FALLBACK_TO_CLOUD",
		}
		
	default:
		// AUTO_SELECT - use heuristics
		return r.autoSelectWinner(lr, cr)
	}
}

// trySmartMerge attempts to intelligently merge conflicting decisions
func (r *ConflictResolver) trySmartMerge(
	lr LocalDecisionRecord,
	cr CloudDecisionRecord,
) (Decision, bool) {
	// Strategy: prefer local resources requests but cloud QoS class
	if lr.Decision.QoSClass != cr.Decision.QoSClass {
		// Different QoS levels - usually cloud is correct
		return cr.Decision, false
	}
	
	// Try to merge resource requests (union set)
	mergedResources := r.mergeResourceLists(lr.Decision.ResourceRequests, cr.Decision.ResourceRequests)
	
	return Decision{
		ID: lr.ID,
		NodeID: lr.NodeID,
		WorkloadID: lr.WorkloadID,
		ResourceRequests: mergedResources,
		QoSClass: lr.Decision.QoSClass, // Keep local's QoS
		Timestamp: maxTime(lr.Timestamp, cr.Timestamp),
	}, true
}

func (r *ConflictResolver) mergeResourceLists(a, b []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(a)+len(b))
	
	for _, item := range append(a, b...) {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	
	return result
}

func maxTime(t1, t2 time.Time) time.Time {
	if t1.After(t2) {
		return t1
	}
	return t2
}

// autoSelectWinner uses heuristics for automatic selection
func (r *ConflictResolver) autoSelectWinner(lr LocalDecisionRecord, cr CloudDecisionRecord) *ResolvedDecision {
	// Heuristic: prefer higher priority, then later timestamp
	if lr.Priority > cr.Priority {
		return &ResolvedDecision{
			ID: lr.ID,
			Source: "AUTO_LOCAL_PRIORITY",
			Decision: lr.Decision,
			Reason: "AUTO_SELECTED_HIGHER_PRIORITY",
		}
	}
	
	if lr.Timestamp.After(cr.Timestamp) {
		return &ResolvedDecision{
			ID: lr.ID,
			Source: "AUTO_LOCAL_TIME",
			Decision: lr.Decision,
			Reason: "AUTO_SELECTED_LATER_TIMESTAMP",
		}
	}
	
	return &ResolvedDecision{
		ID: cr.ID,
		Source: "AUTO_CLOUD_BASELINE",
		Decision: cr.Decision,
		Reason: "AUTO_DEFAULT_TO_CLOUD",
	}
}

// Helper functions
func (r *ConflictResolver) indexCloudRecords(records []CloudDecisionRecord) map[string]*CloudDecisionRecord {
	index := make(map[string]*CloudDecisionRecord)
	
	for i := range records {
		rec := &records[i]
		index[rec.WorkloadID] = rec
	}
	
	return index
}

func (r *ConflictResolver) updateMetrics(successful bool) {
	if successful {
		r.metrics.SuccessfulResolutions++
	} else {
		r.metrics.ManualInterventions++
	}
}
