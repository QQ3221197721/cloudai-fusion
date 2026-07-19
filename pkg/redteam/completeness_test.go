package redteam

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// completeness_test.go proves the redteam side of Moat A / M0: a completed (or
// aborted) engagement is sealed, and the resulting CompletenessProof verifies
// offline - turning report.go's "no more, no less" promise into a theorem.

func newSealedLedger(t *testing.T, seed byte) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func TestEngagementSeal_CompletenessVerifies(t *testing.T) {
	ctx := context.Background()
	ledger := newSealedLedger(t, 0x5a)
	mgr := NewManager(ledger, nil)

	scope := Scope{
		Targets:         []Target{{Kind: TargetCIDR, Value: "10.0.0.0/24"}},
		AllowTechniques: []string{"*"},
		MaxRiskTier:     RiskReadOnly,
	}
	e, err := mgr.Create(ctx, scope, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := mgr.Start(e.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := mgr.AddFinding(ctx, e.ID, &Finding{
		Asset: "10.0.0.5", Severity: "high", Title: "open service", Technique: "T1046",
	}); err != nil {
		t.Fatalf("finding: %v", err)
	}
	if err := mgr.Complete(e.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	proof, err := ledger.BuildCompletenessProof(ctx, NamespaceFor(e.ID))
	if err != nil {
		t.Fatalf("build proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, ledger.Signer().PublicKey()); err != nil {
		t.Fatalf("engagement completeness proof must verify, got: %v", err)
	}
	// At least the scope grant + finding; and the seal is not a member of itself.
	if len(proof.Members) < 2 {
		t.Fatalf("expected >=2 member receipts (grant, finding), got %d", len(proof.Members))
	}
	for _, m := range proof.Members {
		if m.Action == evidence.ActionSubtreeSeal {
			t.Fatal("the seal must not be a member of itself")
		}
	}

	// Tamper: drop a member -> completeness must fail (proves "no less").
	proof.Members = proof.Members[:len(proof.Members)-1]
	proof.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]
	if err := evidence.VerifyCompleteness(proof, ledger.Signer().PublicKey()); err == nil {
		t.Fatal("omitting an engagement receipt MUST fail completeness")
	}
}

func TestEngagementSeal_AbortAlsoSeals(t *testing.T) {
	ctx := context.Background()
	ledger := newSealedLedger(t, 0x6b)
	mgr := NewManager(ledger, nil)

	scope := Scope{
		Targets:         []Target{{Kind: TargetHost, Value: "example.test"}},
		AllowTechniques: []string{"*"},
	}
	e, err := mgr.Create(ctx, scope, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = mgr.Start(e.ID)
	if err := mgr.Abort(ctx, e.ID, "kill-switch"); err != nil {
		t.Fatalf("abort: %v", err)
	}

	proof, err := ledger.BuildCompletenessProof(ctx, NamespaceFor(e.ID))
	if err != nil {
		t.Fatalf("build proof after abort: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, ledger.Signer().PublicKey()); err != nil {
		t.Fatalf("aborted engagement completeness must verify, got: %v", err)
	}
}
