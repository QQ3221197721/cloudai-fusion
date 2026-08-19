package soc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// approval_test.go pins the Module 32 differentiator: every destructive SOAR
// action is (1) refused unless a human approval was granted, and (2) sealed into
// an offline-verifiable Ed25519 receipt. These are the two claims the capability
// matrix rests on, so they are proven by construction here.

// TestActionType_IsDestructive asserts the classification: all mutating
// primitives are destructive, only notify is not, and unknown fails closed.
func TestActionType_IsDestructive(t *testing.T) {
	for _, a := range DestructiveActions() {
		if !a.IsDestructive() {
			t.Fatalf("%q must be classified destructive", a)
		}
	}
	if ActionNotify.IsDestructive() {
		t.Fatal("notify must NOT be destructive")
	}
	if !ActionType("unknown-future-action").IsDestructive() {
		t.Fatal("unknown actions must fail closed (treated destructive)")
	}
}

// TestApprovalGate_InterceptsAllUnapprovedDestructive is the headline test:
// a response carrying EVERY destructive action plus notify, run with NO prior
// approval, must refuse 100% of the destructive actions (none actuated) while
// letting notify through — and every refusal must carry a verifiable receipt.
func TestApprovalGate_InterceptsAllUnapprovedDestructive(t *testing.T) {
	gate := NewApprovalGate()
	act := NewRecordingActuator()

	resp := Response{ID: "r1", FindingID: "f1"}
	for _, a := range DestructiveActions() {
		resp.Actions = append(resp.Actions, ResponseAction{Type: a, Target: "asset-1", Automated: true})
	}
	resp.Actions = append(resp.Actions, ResponseAction{Type: ActionNotify, Target: "asset-1", Automated: true})

	results := gate.GuardedActuate(context.Background(), act, resp)
	if len(results) != len(resp.Actions) {
		t.Fatalf("expected %d guarded results, got %d", len(resp.Actions), len(results))
	}

	destructiveRefused, notifyRan := 0, 0
	for _, r := range results {
		if r.Receipt == nil || !r.Receipt.Verify() {
			t.Fatalf("action %q must carry a verifiable receipt", r.Action)
		}
		if !bytes.Equal(r.Receipt.SignerPublicKey, gate.PublicKey()) {
			t.Fatalf("action %q receipt must be signed by the gate key", r.Action)
		}
		switch {
		case r.Action.IsDestructive():
			if r.Permitted {
				t.Fatalf("unapproved destructive action %q must be refused", r.Action)
			}
			if r.Actuation != nil {
				t.Fatalf("refused destructive action %q must not actuate", r.Action)
			}
			destructiveRefused++
		default: // notify
			if !r.Permitted || r.Actuation == nil {
				t.Fatalf("notify must always run")
			}
			notifyRan++
		}
	}
	if destructiveRefused != len(DestructiveActions()) {
		t.Fatalf("interception rate must be 100%%: refused %d/%d", destructiveRefused, len(DestructiveActions()))
	}
	if notifyRan != 1 {
		t.Fatalf("notify must have run exactly once, got %d", notifyRan)
	}
	// Not a single destructive mitigation may be active: nothing touched state.
	if got := len(act.Active()); got != 0 {
		t.Fatalf("no mitigation may be active after a fully-refused response, got %d", got)
	}
}

// TestApprovalGate_GrantAuthorizesExactlyOne proves the positive path: a granted
// approval lets exactly its (action,target) execute, while other destructive
// actions remain refused. The grant decision itself is a verifiable receipt.
func TestApprovalGate_GrantAuthorizesExactlyOne(t *testing.T) {
	gate := NewApprovalGate()
	act := NewRecordingActuator()

	// Grant isolate-host on host-9 only.
	ap, err := gate.Decide(ActionIsolateHost, "host-9", "soc-lead@corp", "confirmed C2 beacon", true)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if ap.Decision != ApprovalGranted || ap.Receipt == nil || !ap.Receipt.Verify() {
		t.Fatalf("granted approval must carry a verifiable receipt: %+v", ap)
	}
	if !gate.Permits(ActionIsolateHost, "host-9") {
		t.Fatal("granted (isolate-host, host-9) must be permitted")
	}
	// A different target / different action is still gated.
	if gate.Permits(ActionIsolateHost, "host-OTHER") {
		t.Fatal("approval must not leak to a different target")
	}
	if gate.Permits(ActionBlockNetwork, "host-9") {
		t.Fatal("approval must not leak to a different action")
	}

	resp := Response{ID: "r2", Actions: []ResponseAction{
		{Type: ActionIsolateHost, Target: "host-9"},   // approved
		{Type: ActionBlockNetwork, Target: "host-9"},  // NOT approved
		{Type: ActionNotify, Target: "host-9"},        // always
	}}
	results := gate.GuardedActuate(context.Background(), act, resp)
	permittedDestructive := 0
	for _, r := range results {
		if r.Action == ActionIsolateHost && !r.Permitted {
			t.Fatal("approved isolate-host must be permitted")
		}
		if r.Action == ActionBlockNetwork && r.Permitted {
			t.Fatal("unapproved block-network must be refused")
		}
		if r.Action.IsDestructive() && r.Permitted {
			permittedDestructive++
		}
	}
	if permittedDestructive != 1 {
		t.Fatalf("exactly one destructive action was approved, %d executed", permittedDestructive)
	}
	if activeCountRA(act, ActionIsolateHost) != 1 {
		t.Fatal("approved isolate-host must leave an active mitigation")
	}
	if activeCountRA(act, ActionBlockNetwork) != 0 {
		t.Fatal("refused block-network must NOT leave a mitigation")
	}
}

// TestApprovalGate_DenyRevokesGrant proves a denial revokes a prior grant, so a
// later escalation cannot ride an old approval.
func TestApprovalGate_DenyRevokesGrant(t *testing.T) {
	gate := NewApprovalGate()
	if _, err := gate.Decide(ActionRevokeCredential, "alice", "lead@corp", "ok", true); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if !gate.Permits(ActionRevokeCredential, "alice") {
		t.Fatal("grant must permit")
	}
	if _, err := gate.Decide(ActionRevokeCredential, "alice", "lead@corp", "false positive", false); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if gate.Permits(ActionRevokeCredential, "alice") {
		t.Fatal("denial must revoke the prior grant")
	}
}

// TestApprovalGate_RejectsNonDestructiveAndEmptyApprover guards the API contract.
func TestApprovalGate_RejectsNonDestructiveAndEmptyApprover(t *testing.T) {
	gate := NewApprovalGate()
	if _, err := gate.Decide(ActionNotify, "x", "lead@corp", "", true); err == nil {
		t.Fatal("deciding on a non-destructive action must error")
	}
	if _, err := gate.Decide(ActionIsolateHost, "x", "", "", true); err == nil {
		t.Fatal("an anonymous approval must be rejected")
	}
}

// TestApprovalGate_ReceiptTamperFails proves the receipt is unforgeable: any
// mutation of the sealed content invalidates the signature (offline detection).
func TestApprovalGate_ReceiptTamperFails(t *testing.T) {
	gate := NewApprovalGate()
	ap, err := gate.Decide(ActionIsolateHost, "host-1", "lead@corp", "confirmed", true)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !ap.Receipt.Verify() {
		t.Fatal("fresh receipt must verify")
	}
	// Flip a byte of the output hash: verification must now fail.
	tampered := *ap.Receipt
	tampered.OutputHash[0] ^= 0xFF
	if tampered.Verify() {
		t.Fatal("tampered receipt must fail verification")
	}
}

// TestApprovalGate_DeterministicKeyVerifiesOffline proves an auditor can verify
// a gate's receipts with only the published public key (no platform trust).
func TestApprovalGate_DeterministicKeyVerifiesOffline(t *testing.T) {
	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	gate, err := NewApprovalGateWithKey(priv)
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	published := priv.Public().(ed25519.PublicKey)

	ap, err := gate.Decide(ActionBlockNetwork, "10.0.0.9", "auditor@corp", "known C2", true)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	// Offline check: signer key equals the published key AND signature verifies.
	if !bytes.Equal(ap.Receipt.SignerPublicKey, published) {
		t.Fatal("receipt must be signed by the published key")
	}
	if !ap.Receipt.Verify() {
		t.Fatal("receipt must verify against its embedded public key offline")
	}
}

// TestApprovalGate_GuardsRealEnginePlaybook wires the gate to a real
// approval-required playbook (account-takeover / T1078) produced by the
// Orchestrator, proving the differentiator on the actual response path: the
// blocking actions are refused (and receipted) until approved.
func TestApprovalGate_GuardsRealEnginePlaybook(t *testing.T) {
	o := NewOrchestrator(nil)
	f := newFinding(WellIdentity, "T1078", "alice", "impossible travel", intel.SeverityCritical, nil)
	resp := o.Respond(f)
	if resp.Playbook != "account-takeover" {
		t.Fatalf("playbook = %q, want account-takeover", resp.Playbook)
	}

	gate := NewApprovalGate()
	act := NewRecordingActuator()
	results := gate.GuardedActuate(context.Background(), act, resp)

	for _, r := range results {
		if r.Receipt == nil || !r.Receipt.Verify() {
			t.Fatalf("every guarded action must carry a verifiable receipt (%q)", r.Action)
		}
		if r.Action.IsDestructive() && r.Permitted {
			t.Fatalf("destructive action %q must be refused without approval", r.Action)
		}
	}
	// No revoke-credential / isolate-host may be active (all gated).
	if activeCountRA(act, ActionRevokeCredential) != 0 || activeCountRA(act, ActionIsolateHost) != 0 {
		t.Fatal("gated playbook must not actuate any destructive action")
	}
}

// activeCountRA counts active mitigations of a given type on a RecordingActuator.
func activeCountRA(ra *RecordingActuator, action ActionType) int {
	n := 0
	for _, m := range ra.Active() {
		if m.Action == action {
			n++
		}
	}
	return n
}
