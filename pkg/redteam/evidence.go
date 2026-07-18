package redteam

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidence.go defines the redteam.* receipt taxonomy. Every authorization
// decision, lifecycle transition, approval, and finding is emitted as a signed,
// hash-chained, Merkle-committed receipt via pkg/evidence. A verifier (cafctl)
// can later prove that an engagement report matches EXACTLY the set of actions
// taken - no more, no less. This is the subsystem's moat.

// evidence action names (kept stable; they appear in the ledger and reports).
const (
	ActionScopeGrant       = "redteam.scope.grant"
	ActionScopeDeny        = "redteam.scope.deny"
	ActionActionAuthorized = "redteam.action.authorized"
	ActionApproval         = "redteam.approval"
	ActionAbort            = "redteam.engagement.abort"
	ActionFinding          = "redteam.finding"
)

// backend facts (honest component/mode/driver tagging, like every subsystem).
var (
	backendAuthz      = evidence.BackendFact{Component: "redteam.authz", Mode: "real", Driver: "scope-gate"}
	backendEngagement = evidence.BackendFact{Component: "redteam.engagement", Mode: "real", Driver: "state-machine"}
)

// emit is a small helper that records a receipt and logs (never panics) on error.
func emit(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, in evidence.RecordInput) {
	if rec == nil {
		return
	}
	if _, err := rec.Record(ctx, in); err != nil && logger != nil {
		logger.WithError(err).WithField("action", in.Action).Warn("redteam: failed to emit evidence")
	}
}

// recordScopeGrant records the signed authorization contract for an engagement.
func recordScopeGrant(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, e *Engagement, principal string) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionScopeGrant,
		Subject: e.ID,
		Input:   map[string]any{"principal": principal},
		Output:  map[string]any{"status": string(e.Status)},
		Payload: map[string]any{
			"engagement_id": e.ID,
			"principal":     principal,
			"scope":         e.Scope,
			"max_risk_tier": e.Scope.MaxRiskTier.String(),
		},
		Backends: []evidence.BackendFact{backendEngagement},
	})
}

// recordScopeDeny records a refused, out-of-scope (or otherwise unauthorized)
// action. This is the heart of provable scope enforcement.
func recordScopeDeny(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, engagementID string, a Action, reason string) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionScopeDeny,
		Subject: a.Target,
		Input:   map[string]any{"engagement_id": engagementID, "technique": a.Technique, "tool": a.Tool},
		Output:  map[string]any{"allowed": false, "reason": reason},
		Payload: map[string]any{
			"engagement_id": engagementID,
			"action_id":     a.ID,
			"technique":     a.Technique,
			"tool":          a.Tool,
			"target":        a.Target,
			"risk_tier":     a.RiskTier.String(),
			"reason":        reason,
		},
		Backends: []evidence.BackendFact{backendAuthz},
	})
}

// recordActionAuthorized records that the gate authorized an action. In M0 no
// tool executes; this proves what WAS authorized (the decision ledger).
func recordActionAuthorized(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, engagementID string, a Action, needsApproval bool) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionActionAuthorized,
		Subject: a.Target,
		Input:   map[string]any{"engagement_id": engagementID, "technique": a.Technique, "tool": a.Tool},
		Output:  map[string]any{"allowed": true, "needs_approval": needsApproval},
		Payload: map[string]any{
			"engagement_id":  engagementID,
			"action_id":      a.ID,
			"technique":      a.Technique,
			"tool":           a.Tool,
			"target":         a.Target,
			"risk_tier":      a.RiskTier.String(),
			"needs_approval": needsApproval,
		},
		Backends: []evidence.BackendFact{backendAuthz},
	})
}

// recordApproval records a human decision on a high-risk action.
func recordApproval(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, engagementID, actionID, approver string, approved bool, tier RiskTier) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionApproval,
		Subject: actionID,
		Input:   map[string]any{"engagement_id": engagementID, "approver": approver, "risk_tier": tier.String()},
		Output:  map[string]any{"approved": approved},
		Payload: map[string]any{
			"engagement_id": engagementID,
			"action_id":     actionID,
			"approver":      approver,
			"approved":      approved,
			"risk_tier":     tier.String(),
		},
		Backends: []evidence.BackendFact{backendAuthz},
	})
}

// recordAbort records a kill-switch or scope-expiry termination.
func recordAbort(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, engagementID, reason string, final Status) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionAbort,
		Subject: engagementID,
		Input:   map[string]any{"reason": reason},
		Output:  map[string]any{"status": string(final)},
		Payload: map[string]any{
			"engagement_id": engagementID,
			"reason":        reason,
			"final_status":  string(final),
		},
		Backends: []evidence.BackendFact{backendEngagement},
	})
}

// recordFinding records a security finding tied to an engagement.
func recordFinding(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, engagementID string, f *Finding) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionFinding,
		Subject: f.Asset,
		Input:   map[string]any{"engagement_id": engagementID, "technique": f.Technique},
		Output:  map[string]any{"severity": f.Severity, "title": f.Title},
		Payload: map[string]any{
			"engagement_id": engagementID,
			"finding_id":    f.ID,
			"asset":         f.Asset,
			"severity":      f.Severity,
			"title":         f.Title,
			"technique":     f.Technique,
		},
		Backends: []evidence.BackendFact{backendEngagement},
	})
}
