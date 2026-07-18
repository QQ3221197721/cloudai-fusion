package redteam

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// report.go produces a verifiable engagement report. The report embeds a signed
// checkpoint (STH) over the evidence ledger plus the ordered list of the
// engagement's receipts (by sequence + hash), so a third party can prove the
// report reflects EXACTLY the recorded actions - the subsystem's core promise.

// ActionReport is the receipt emitted when a report is generated.
const ActionReport = "redteam.report"

// Checkpointer is the minimal surface a report needs to embed a signed STH.
// *evidence.Ledger satisfies it.
type Checkpointer interface {
	Checkpoint(ctx context.Context) (*evidence.Checkpoint, error)
}

// ReportReceiptRef is a compact, verifiable reference to one ledger receipt.
type ReportReceiptRef struct {
	Seq     uint64 `json:"seq"`
	Action  string `json:"action"`
	Subject string `json:"subject"`
	Hash    string `json:"hash"`
}

// Report is a verifiable engagement report.
type Report struct {
	EngagementID string               `json:"engagement_id"`
	Status       string               `json:"status"`
	CreatedBy    string               `json:"created_by"`
	GeneratedAt  string               `json:"generated_at"`
	ScopeTargets int                  `json:"scope_targets"`
	Techniques   []string             `json:"techniques"`    // distinct MITRE IDs exercised
	ActionCounts map[string]int       `json:"action_counts"` // by receipt action
	Findings     []*Finding           `json:"findings"`
	Receipts     []ReportReceiptRef   `json:"receipts"`
	Checkpoint   *evidence.Checkpoint `json:"checkpoint,omitempty"` // signed STH over the ledger
}

// BuildReport assembles the report for an engagement from the full ledger, embeds
// a signed checkpoint, and records a redteam.report receipt referencing the STH.
func BuildReport(ctx context.Context, e *Engagement, all []*evidence.Evidence, cp Checkpointer, recorder evidence.Recorder, logger *logrus.Logger) (*Report, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	rep := &Report{
		EngagementID: e.ID,
		Status:       string(e.Status),
		CreatedBy:    e.CreatedBy,
		GeneratedAt:  common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
		ScopeTargets: len(e.Scope.Targets),
		ActionCounts: map[string]int{},
		Findings:     append([]*Finding{}, e.Findings...),
	}

	techSet := map[string]bool{}
	for _, ev := range all {
		if !receiptBelongsTo(ev, e.ID) {
			continue
		}
		rep.ActionCounts[ev.Action]++
		rep.Receipts = append(rep.Receipts, ReportReceiptRef{
			Seq: ev.Seq, Action: ev.Action, Subject: ev.Subject, Hash: ev.Hash,
		})
		if tech := receiptTechnique(ev); tech != "" {
			techSet[tech] = true
		}
	}
	for tech := range techSet {
		rep.Techniques = append(rep.Techniques, tech)
	}
	sort.Strings(rep.Techniques)

	// Embed a signed checkpoint (STH) so the report is offline-verifiable.
	if cp != nil {
		if ckpt, err := cp.Checkpoint(ctx); err == nil {
			rep.Checkpoint = ckpt
		} else {
			logger.WithError(err).Warn("redteam: report checkpoint unavailable")
		}
	}

	recordReport(ctx, recorder, logger, rep)
	return rep, nil
}

// recordReport emits a redteam.report receipt referencing the signed checkpoint.
func recordReport(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, rep *Report) {
	payload := map[string]any{
		"engagement_id": rep.EngagementID,
		"status":        rep.Status,
		"action_counts": rep.ActionCounts,
		"techniques":    rep.Techniques,
		"finding_count": len(rep.Findings),
		"receipt_count": len(rep.Receipts),
	}
	if rep.Checkpoint != nil {
		payload["checkpoint_root"] = rep.Checkpoint.RootHash
		payload["tree_size"] = rep.Checkpoint.TreeSize
	}
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionReport,
		Subject: rep.EngagementID,
		Input:   map[string]any{"engagement_id": rep.EngagementID},
		Output:  map[string]any{"findings": len(rep.Findings), "receipts": len(rep.Receipts)},
		Payload: payload,
		Backends: []evidence.BackendFact{
			{Component: "redteam.report", Mode: "real", Driver: "signed-checkpoint"},
		},
	})
}

// receiptBelongsTo reports whether a receipt belongs to an engagement (by subject
// or by an engagement_id embedded in its payload).
func receiptBelongsTo(ev *evidence.Evidence, engagementID string) bool {
	if ev.Subject == engagementID {
		return true
	}
	return receiptEngagementID(ev) == engagementID
}

func receiptEngagementID(ev *evidence.Evidence) string {
	var p map[string]any
	if json.Unmarshal(ev.Payload, &p) == nil {
		if v, ok := p["engagement_id"].(string); ok {
			return v
		}
	}
	return ""
}

func receiptTechnique(ev *evidence.Evidence) string {
	var p map[string]any
	if json.Unmarshal(ev.Payload, &p) == nil {
		if v, ok := p["technique"].(string); ok {
			return v
		}
	}
	return ""
}

// EngagementReceipts returns the subset of receipts belonging to an engagement.
// It is a read-only filter (no side effects) so API/report consumers can list an
// engagement's evidence without emitting a new report receipt.
func EngagementReceipts(all []*evidence.Evidence, engagementID string) []*evidence.Evidence {
	out := make([]*evidence.Evidence, 0, len(all))
	for _, ev := range all {
		if receiptBelongsTo(ev, engagementID) {
			out = append(out, ev)
		}
	}
	return out
}
