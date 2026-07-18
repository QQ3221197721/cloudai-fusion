package redteam

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// authz.go is the authorization gate: the single choke point every action must
// pass. It enforces the signed Scope (target/technique/time-window/risk-tier/
// rate-limit), supports human-in-the-loop approval for high-risk tiers, and a
// kill-switch. Every decision - allow or deny - is atomically recorded to the
// evidence ledger, so scope enforcement is provable after the fact.

// Action is a single candidate operation the planner proposes.
type Action struct {
	ID        string         `json:"id"`
	Technique string         `json:"technique"` // MITRE ATT&CK ID
	Tool      string         `json:"tool"`
	Target    string         `json:"target"`
	RiskTier  RiskTier       `json:"risk_tier"`
	Params    map[string]any `json:"params,omitempty"`
}

// Decision is the gate's verdict for an action.
type Decision struct {
	Allowed       bool   `json:"allowed"`
	Reason        string `json:"reason,omitempty"`
	NeedsApproval bool   `json:"needs_approval"`
}

// Gate authorizes actions against a fixed, signed Scope for one engagement.
type Gate struct {
	engagementID string
	scope        Scope
	recorder     evidence.Recorder
	logger       *logrus.Logger
	now          func() time.Time // injectable clock (tests)

	mu      sync.Mutex
	aborted bool
	recent  []time.Time // action timestamps for rate limiting
}

// NewGate builds an authorization gate bound to an engagement's signed scope.
func NewGate(engagementID string, scope Scope, recorder evidence.Recorder, logger *logrus.Logger) *Gate {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Gate{
		engagementID: engagementID,
		scope:        scope,
		recorder:     recorder,
		logger:       logger,
		now:          time.Now,
	}
}

// Check evaluates an action against the scope and records the decision. A denied
// action yields a redteam.scope.deny receipt; an allowed one yields
// redteam.action.authorized. The decision and its receipt are produced atomically.
func (g *Gate) Check(ctx context.Context, a Action) Decision {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.aborted {
		return g.deny(ctx, a, "engagement aborted")
	}
	now := g.now()
	if !g.scope.WithinWindow(now) {
		return g.deny(ctx, a, "outside authorized time window")
	}
	if !g.scope.InScope(a.Target) {
		return g.deny(ctx, a, "target out of scope")
	}
	if !g.scope.TechniqueAllowed(a.Technique) {
		return g.deny(ctx, a, "technique not authorized")
	}
	if a.RiskTier > g.scope.MaxRiskTier {
		return g.deny(ctx, a, "risk tier exceeds authorization")
	}
	if !g.underRateLimit(now) {
		return g.deny(ctx, a, "rate limit exceeded")
	}

	// Authorized. Record the action timestamp for rate limiting.
	g.recent = append(g.recent, now)
	needsApproval := a.RiskTier >= g.scope.ApprovalReq
	recordActionAuthorized(ctx, g.recorder, g.logger, g.engagementID, a, needsApproval)
	return Decision{Allowed: true, NeedsApproval: needsApproval}
}

// deny records a denial receipt and returns the verdict (caller holds g.mu).
func (g *Gate) deny(ctx context.Context, a Action, reason string) Decision {
	recordScopeDeny(ctx, g.recorder, g.logger, g.engagementID, a, reason)
	return Decision{Allowed: false, Reason: reason}
}

// underRateLimit prunes stale timestamps and reports whether another action is
// permitted under the scope's rate limit (caller holds g.mu).
func (g *Gate) underRateLimit(now time.Time) bool {
	rl := g.scope.RateLimit
	if rl.MaxActions <= 0 || rl.Per <= 0 {
		return true // unlimited
	}
	cutoff := now.Add(-rl.Per)
	kept := g.recent[:0]
	for _, ts := range g.recent {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	g.recent = kept
	return len(g.recent) < rl.MaxActions
}

// Approve records a human decision on a high-risk action.
func (g *Gate) Approve(ctx context.Context, actionID, approver string, approved bool, tier RiskTier) {
	recordApproval(ctx, g.recorder, g.logger, g.engagementID, actionID, approver, approved, tier)
}

// Abort trips the kill-switch: subsequent Check calls are denied, and a
// redteam.engagement.abort receipt is recorded (idempotent).
func (g *Gate) Abort(ctx context.Context, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.aborted {
		return
	}
	g.aborted = true
	recordAbort(ctx, g.recorder, g.logger, g.engagementID, reason, StatusAborted)
}

// Aborted reports whether the kill-switch has been tripped.
func (g *Gate) Aborted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.aborted
}
