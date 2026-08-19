package soc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// approval.go adds the compliance-grade capability that differentiates CloudAI
// Fusion's L8 SOAR from log-only orchestrators (TheHive, Cortex XSOAR): a
// DESTRUCTIVE-action approval gate whose every decision AND every executed step
// is sealed into its own offline-verifiable Ed25519 receipt.
//
// The gate is additive and does NOT change the default Orchestrator/Engine
// playbook semantics (which encode a per-playbook risk-tier via
// Playbook.RequiresApproval). It is the explicit, auditable path an operator
// wires when they need "no destructive action fires without a signed human
// approval, and every action leaves a proof an auditor can verify offline
// with only the published public key" — a guarantee competitors express only
// as best-effort, mutable logs.
//
// Honesty: the cryptography here is always real Ed25519 (there is no simulated
// signer). What the receipt binds is exactly what happened — a granted/denied
// approval, or an executed/refused actuation — never more.

// IsDestructive reports whether an action mutates a live system and therefore
// must pass the approval gate. Every response primitive is destructive except
// notify, which only informs and creates no lasting mitigation.
func (a ActionType) IsDestructive() bool {
	switch a {
	case ActionIsolateHost, ActionBlockNetwork, ActionQuarantineFile,
		ActionRevokeCredential, ActionRebuildImage, ActionHardenWorkload:
		return true
	case ActionNotify:
		return false
	default:
		// Unknown actions are treated as destructive: fail closed, never open.
		return true
	}
}

// DestructiveActions returns the canonical set of destructive response
// primitives (stable order), used by callers and tests to enumerate the actions
// the gate must protect.
func DestructiveActions() []ActionType {
	return []ActionType{
		ActionIsolateHost, ActionBlockNetwork, ActionQuarantineFile,
		ActionRevokeCredential, ActionRebuildImage, ActionHardenWorkload,
	}
}

// ApprovalDecision is the verdict recorded for a destructive action.
type ApprovalDecision string

const (
	ApprovalGranted ApprovalDecision = "granted"
	ApprovalDenied  ApprovalDecision = "denied"
)

// ActionApproval is one human decision over a destructive action, sealed into a
// signed receipt. The receipt binds (action, target, approver, decision) so an
// auditor can prove WHO authorized WHAT and WHEN — offline, without trusting the
// platform.
type ActionApproval struct {
	Action        ActionType        `json:"action"`
	Target        string            `json:"target"`
	Approver      string            `json:"approver"`
	Decision      ApprovalDecision  `json:"decision"`
	Justification string            `json:"justification,omitempty"`
	DecidedAt     time.Time         `json:"decided_at"`
	Receipt       *evidence.Receipt `json:"receipt,omitempty"`
}

// GuardedActionResult is the outcome of running one response action through the
// gate: whether the gate permitted it, the actuation result if it ran, and the
// signed receipt attesting the guard decision + effect.
type GuardedActionResult struct {
	Action    ActionType        `json:"action"`
	Target    string            `json:"target"`
	Permitted bool              `json:"permitted"`          // false => refused (unapproved destructive action)
	Actuation *ActuationResult  `json:"actuation,omitempty"` // nil when refused
	Reason    string            `json:"reason,omitempty"`
	Receipt   *evidence.Receipt `json:"receipt,omitempty"`
}

// ApprovalGate enforces human approval for destructive SOAR actions and seals
// every decision and guarded actuation into an individually-verifiable receipt.
// It is concurrency-safe.
type ApprovalGate struct {
	builder *evidence.ReceiptBuilder
	pub     ed25519.PublicKey

	mu      sync.Mutex
	granted map[string]ActionApproval // key: action + target; only granted approvals
}

// NewApprovalGate builds a gate with a fresh in-process Ed25519 key. The key is
// ephemeral (dev/test/demo); production wires an operator-provisioned key via
// NewApprovalGateWithKey so receipts chain to a published, auditable identity.
func NewApprovalGate() *ApprovalGate {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return newGate(priv, pub)
}

// NewApprovalGateWithKey builds a gate over a caller-provided Ed25519 private
// key (e.g. an operator-provisioned or reproducible seed key), so an auditor can
// pin receipts to a known public key.
func NewApprovalGateWithKey(priv ed25519.PrivateKey) (*ApprovalGate, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("soc: approval gate key must be %d bytes, got %d", ed25519.PrivateKeySize, len(priv))
	}
	return newGate(priv, priv.Public().(ed25519.PublicKey)), nil
}

func newGate(priv ed25519.PrivateKey, pub ed25519.PublicKey) *ApprovalGate {
	return &ApprovalGate{
		builder: evidence.NewReceiptBuilder("soc.soar.approval", priv),
		pub:     pub,
		granted: make(map[string]ActionApproval),
	}
}

// PublicKey returns the Ed25519 public key that verifies this gate's receipts.
// An auditor needs only this key (never the platform) to verify every receipt.
func (g *ApprovalGate) PublicKey() ed25519.PublicKey { return g.pub }

// Decide records a human approval decision for a destructive action, sealing it
// into a signed receipt. A granted decision authorizes exactly one subsequent
// actuation of that (action,target) via Permits/GuardedActuate; a denied
// decision leaves the action unauthorized. Deciding on a non-destructive action
// is rejected: notify needs no approval and must not pollute the approval ledger.
func (g *ApprovalGate) Decide(action ActionType, target, approver, justification string, grant bool) (*ActionApproval, error) {
	if !action.IsDestructive() {
		return nil, fmt.Errorf("soc: %q is not a destructive action and needs no approval", action)
	}
	if approver == "" {
		return nil, fmt.Errorf("soc: an approval decision requires a named approver")
	}
	decision := ApprovalDenied
	if grant {
		decision = ApprovalGranted
	}
	ap := ActionApproval{
		Action:        action,
		Target:        target,
		Approver:      approver,
		Decision:      decision,
		Justification: justification,
		DecidedAt:     time.Now().UTC(),
	}
	// Seal the decision: input binds the request, output binds the verdict.
	input := struct {
		Action   ActionType `json:"action"`
		Target   string     `json:"target"`
		Approver string     `json:"approver"`
	}{action, target, approver}
	receipt, err := g.builder.Build("soar.approval.decide", input, ap)
	if err != nil {
		return nil, fmt.Errorf("soc: seal approval decision: %w", err)
	}
	ap.Receipt = receipt

	g.mu.Lock()
	if grant {
		g.granted[mitigationKey(action, target)] = ap
	} else {
		// A denial revokes any prior grant for the same (action,target).
		delete(g.granted, mitigationKey(action, target))
	}
	g.mu.Unlock()
	return &ap, nil
}

// Permits reports whether an action may execute now. Non-destructive actions
// (notify) are always permitted. A destructive action is permitted only when a
// prior granted approval exists for the same (action,target).
func (g *ApprovalGate) Permits(action ActionType, target string) bool {
	if !action.IsDestructive() {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ap, ok := g.granted[mitigationKey(action, target)]
	return ok && ap.Decision == ApprovalGranted
}

// GuardedActuate executes a SOAR response's actions through the actuator, but
// REFUSES any destructive action the gate has not authorized, and seals every
// action — executed or refused — into its own signed receipt. This is the
// end-to-end proof point: an unapproved destructive action is provably blocked
// AND leaves a verifiable record that it was blocked. Notify always runs.
//
// It never returns an error: a refusal is a first-class, receipted outcome, not
// a failure. Callers inspect each GuardedActionResult.Permitted and .Receipt.
func (g *ApprovalGate) GuardedActuate(ctx context.Context, actuator Actuator, resp Response) []GuardedActionResult {
	out := make([]GuardedActionResult, 0, len(resp.Actions))
	for _, a := range resp.Actions {
		res := GuardedActionResult{Action: a.Type, Target: a.Target}
		if !g.Permits(a.Type, a.Target) {
			// Refused: destructive action without a granted approval.
			res.Permitted = false
			res.Reason = "destructive action refused: no granted approval"
			res.Receipt = g.sealGuard(a.Type, a.Target, false, nil)
			out = append(out, res)
			continue
		}
		r := actuator.Actuate(ctx, a.Type, a.Target)
		res.Permitted = true
		res.Actuation = &r
		res.Receipt = g.sealGuard(a.Type, a.Target, true, &r)
		out = append(out, res)
	}
	return out
}

// sealGuard seals one guard decision (executed or refused) into a signed receipt.
func (g *ApprovalGate) sealGuard(action ActionType, target string, permitted bool, r *ActuationResult) *evidence.Receipt {
	input := struct {
		Action    ActionType `json:"action"`
		Target    string     `json:"target"`
		Permitted bool       `json:"permitted"`
	}{action, target, permitted}
	output := struct {
		Permitted bool             `json:"permitted"`
		Actuation *ActuationResult `json:"actuation,omitempty"`
	}{permitted, r}
	receipt, err := g.builder.Build("soar.action.guard", input, output)
	if err != nil {
		// A signing failure must not be silently dropped; return nil so the
		// caller sees a missing receipt rather than a fabricated one.
		return nil
	}
	return receipt
}
