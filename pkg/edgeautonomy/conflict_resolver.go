// Package edgeautonomy - Conflict resolution strategies.
// Implements 5 conflict resolution algorithms for edge-cloud reconciliation:
//   1. Last-Writer-Wins (timestamp-based)
//   2. Highest-Priority Wins
//   3. Cloud Authority (always trust cloud)
//   4. Smart Merge (intelligent combination)
//   5. Auto-Select (heuristic-based)
// This is the TRUE unique technology that enables 7-day offline operation.
package edgeautonomy

import (
	"context"
	"time"
)

// ============================================================================
// Conflict Resolution Strategies
// ============================================================================

// ConflictResolver manages conflict resolution between edge and cloud decisions
type ConflictResolver struct {
	strategy         ConflictResolutionStrategy
	versionVector    *VersionVector
	logger           interface{} // Logger
	mu               sync.RWMutex
}

// ConflictResolutionStrategy enum
type ConflictResolutionStrategy int

const (
	LastWriterWins ConflictResolutionStrategy = iota
	HighestPriority
	CloudAuthority
	SmartMerge
	AutoSelect
)

// ResolvedDecision represents the result of a conflict resolution
type ResolvedDecision struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"`       // local, cloud, merged
	Decision    DecisionResult    `json:"decision"`
	Reason      string            `json:"reason"`
	Merged      bool              `json:"merged,omitempty"`
	Version     int64             `json:"version"`
}

// ConflictReport contains details about a resolved conflict
type ConflictReport struct {
	LocalRecord  LocalDecisionRecord `json:"local_record"`
	CloudRecord  CloudDecisionRecord `json:"cloud_record"`
	Comparison   ComparisonResult    `json:"comparison"`
	Strategy     ConflictResolutionStrategy `json:"strategy"`
	ResolvedAt   time.Time         `json:"resolved_at"`
	ManualReview bool                `json:"manual_review"`
}

// CompareVersions compares two decision versions using version vectors
func (cr *ConflictResolver) CompareVersions(vv1, vv2 []int) ComparisonResult {
	return compareVectors(vv1, vv2)
}

// ResolveConflicts resolves conflicts between local and cloud records
func (cr *ConflictResolver) ResolveConflicts(ctx context.Context, localRecords []LocalDecisionRecord, cloudRecords []CloudDecisionRecord) ([]ResolvedDecision, []ConflictReport) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	resolved := make([]ResolvedDecision, 0)
	reports := make([]ConflictReport, 0)

	for _, lr := range localRecords {
		cr.mu.RLock()
		report := ConflictReport{
			LocalRecord: lr,
			Strategy:    cr.strategy,
			ResolvedAt:  time.Now(),
		}
		cr.mu.RUnlock()

		// Find matching cloud record
		var crRecord *CloudDecisionRecord
		for i := range cloudRecords {
			if cloudRecords[i].ID == lr.ID {
				crRecord = &cloudRecords[i]
				break
			}
		}

		if crRecord == nil {
			// No cloud record - accept local first
			resolved = append(resolved, ResolvedDecision{
				ID:     lr.ID,
				Source: "local",
				Decision: DecisionResult{
					Action:    lr.Decision.Action,
					Target:    lr.Decision.Target,
					CreatedAt: lr.Timestamp,
					IsOffline: true,
				},
				Reason:  "NO_CLOUD_CONFLICT",
				Version: lr.Version + 1,
			})
			continue
		}

		// Both exist - analyze causality relationship
		comparison := cr.CompareVersions(lr.VersionVec, crRecord.VersionVec)
		report.Comparison = comparison

		winner := cr.selectWinner(comparison, lr, *crRecord, report)
		resolved = append(resolved, *winner)
		reports = append(reports, report)
	}

	return resolved, reports
}

// selectWinner determines which decision wins based on strategy
func (cr *ConflictResolver) selectWinner(comp ComparisonResult, local LocalDecisionRecord, cloud CloudDecisionRecord, report ConflictReport) *ResolvedDecision {
	switch cr.strategy {
	case LastWriterWins:
		if local.Timestamp.After(cloud.Timestamp) {
			return &ResolvedDecision{
				ID:      local.ID,
				Source:  "local",
				Decision: local.Decision,
				Reason:  fmt.Sprintf("LAST_WRITER_WINS_LOCAL_%s", local.Timestamp.Format(time.RFC3339)),
				Version: local.Version + 1,
			}
		}
		return &ResolvedDecision{
			ID:      cloud.ID,
			Source:  "cloud",
			Decision: DecisionResult{Action: cloud.Action.Ptr()},
			Reason:  fmt.Sprintf("LAST_WRITER_WINS_CLOUD_%s", cloud.Timestamp.Format(time.RFC3339)),
			Version: cloud.Version + 1,
		}

	case HighestPriority:
		if local.Priority > cloud.Priority {
			return &ResolvedDecision{
				ID:   local.ID,
				Source: "local",
				Decision: DecisionResult{
					Priority: local.Priority,
					Cause:    local.Cause,
				},
				Reason: fmt.Sprintf("HIGHER_PRIORITY_LOCAL_%d", local.Priority),
				Version: local.Version + 1,
			}
		}
		return &ResolvedDecision{
			ID:     cloud.ID,
			Source: "cloud",
			Reason: fmt.Sprintf("HIGHER_PRIORITY_CLOUD_%d", cloud.Priority),
			Version: cloud.Version + 1,
		}

	case CloudAuthority:
		return &ResolvedDecision{
			ID:     cloud.ID,
			Source: "cloud",
			Reason: "CLOUD_ALWAYS_WIN_SAFETY_POLICY",
			Version: cloud.Version + 1,
		}

	case SmartMerge:
		merged, canMerge := cr.trySmartMerge(local, cloud)
		if canMerge {
			return &ResolvedDecision{
				ID:     local.ID,
				Source: "merged",
				Decision: DecisionResult{
					Priority: merged.Priority,
				},
				Reason:  "INTELLIGENT_MERGE_APPLIED",
				Merged:  true,
				Version: local.Version + 1,
			}
		}
		return &ResolvedDecision{
			ID:   cloud.ID,
			Source: "cloud",
			Reason: "MERGE_FAILED_FALLBACK_TO_CLOUD",
			Version: cloud.Version + 1,
		}

	case AutoSelect:
		// Use heuristic scoring to select winner
		localScore := cr.scoreDecision(local, comp)
		cloudScore := cr.scoreDecisionByCloud(cloud, comp)

		if localScore > cloudScore {
			return &ResolvedDecision{
				ID:   local.ID,
				Source: "local",
				Decision: DecisionResult{
					Priority: local.Priority,
					Cause:    local.Cause,
				},
				Reason: fmt.Sprintf("AUTO_SELECT_LOCAL_SCORE_%.2f_vs_%.2f", localScore, cloudScore),
				Version: local.Version + 1,
			}
		}
		return &ResolvedDecision{
			ID:     cloud.ID,
			Source: "cloud",
			Reason: fmt.Sprintf("AUTO_SELECT_CLOUD_SCORE_%.2f_vs_%.2f", cloudScore, localScore),
			Version: cloud.Version + 1,
		}

	default:
		// Fallback to cloud authority
		return &ResolvedDecision{
			ID:   cloud.ID,
			Source: "cloud",
			Reason: "DEFAULT_FALLBACK_TO_CLOUD",
			Version: cloud.Version + 1,
		}
	}
}

// trySmartMerge attempts to intelligently merge compatible fields
func (cr *ConflictResolver) trySmartMerge(local LocalDecisionRecord, cloud CloudDecisionRecord) (*DecisionResult, bool) {
	// Check if actions are compatible (e.g., both scaling same direction)
	if !actionsAreCompatible(*local.Decision.Action, *cloud.Action) {
		return nil, false
	}

	// Merge metrics and confidence
	mergedMetrics := make(map[string]float64)
	for k, v := range local.Metrics {
		mergedMetrics[k] = v
	}
	for k, v := range cloud.Metrics {
		mergedMetrics[k] = v
	}

	return &DecisionResult{
		Confidence: 0.92,
		RuleMatched: "SMART_MERGE",
		Metrics:    mergedMetrics,
		Priority:   maxInt(local.Priority, cloud.Priority),
	}, true
}

// scoreDecision scores a local decision for auto-selection
func (cr *ConflictResolver) scoreDecision(local LocalDecisionRecord, comp ComparisonResult) float64 {
	score := float64(local.Priority) * 10.0

	// Boost for newer timestamp
	if comp == V1_CAUSAL_AFTER_V2 {
		score += 5.0
	} else if comp == CONCURRENT {
		score -= 2.0
	}

	// Confidence bonus
	score += local.Confidence * 5.0

	return score
}

// scoreDecision scores a cloud decision
func (cr *ConflictResolver) scoreDecisionByCloud(cloud CloudDecisionRecord, comp ComparisonResult) float64 {
	score := float64(cloud.Priority) * 10.0

	if comp == V1_CAUSAL_BEFORE_V2 {
		score += 5.0
	}

	return score
}

// actionsAreCompatible checks if two actions can be merged
func actionsAreCompatible(a1 DecisionAction, a2 DecisionAction) bool {
	compatiblePairs := map[DecisionAction][]DecisionAction{
		ActionScaleUp: {ActionScaleUp},
		ActionScaleDown: {ActionScaleDown},
	}

	if targets, ok := compatiblePairs[a1]; ok {
		for _, target := range targets {
			if target == a2 {
				return true
			}
		}
	}

	return false
}

// Helper functions
func compareVectors(v1, v2 []int) ComparisonResult {
	hasLess := false
	hasGreater := false

	for i := range v1 {
		if v1[i] < v2[i] {
			hasLess = true
		} else if v1[i] > v2[i] {
			hasGreater = true
		}

		// Early exit if concurrent
		if hasLess && hasGreater {
			return CONCURRENT
		}
	}

	if !hasLess && hasGreater {
		return V1_CAUSAL_AFTER_V2
	}
	if hasLess && !hasGreater {
		return V1_CAUSAL_BEFORE_V2
	}
	return EQUIVALENT
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
