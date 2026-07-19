package evidence

import (
	"bytes"
	"context"
	"testing"
)

// completeness_test.go proves Moat A / Layer A0: a sealed namespace yields a
// completeness proof that verifies offline against a pinned key, and FAILS on any
// omission, addition, edit, or reorder — turning report.go's "no more, no less"
// comment into a machine-checked theorem.

func newCompletenessLedger(t *testing.T, seed byte) *Ledger {
	t.Helper()
	signer, err := NewSignerFromSeed(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := NewLedger(LedgerConfig{Store: NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func recordMember(t *testing.T, l *Ledger, ns, action string, i int) *Evidence {
	t.Helper()
	ev, err := l.Record(context.Background(), RecordInput{
		Actor:   "test",
		Action:  action,
		Subject: ns,
		Input:   map[string]any{"i": i},
		Payload: map[string]any{"namespace": ns, "n": i},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return ev
}

// seedProof records members (plus an unrelated receipt from another namespace),
// seals the namespace, and returns a freshly built, valid completeness proof.
func seedProof(t *testing.T, l *Ledger, ns string, n int) *CompletenessProof {
	t.Helper()
	ctx := context.Background()
	var members []*Evidence
	for i := 0; i < n; i++ {
		members = append(members, recordMember(t, l, ns, "redteam.recon", i))
	}
	// An unrelated receipt that must NOT be part of the sealed set.
	_ = recordMember(t, l, "scheduler/tenant/other", "schedule.bind", 99)

	if _, err := l.SealNamespace(ctx, ns, "redteam", members); err != nil {
		t.Fatalf("seal: %v", err)
	}
	proof, err := l.BuildCompletenessProof(ctx, ns)
	if err != nil {
		t.Fatalf("build proof: %v", err)
	}
	return proof
}

func TestCompleteness_VerifiesExactSealedSet(t *testing.T) {
	l := newCompletenessLedger(t, 0x42)
	proof := seedProof(t, l, "redteam/engagement/e-1", 5)

	if err := VerifyCompleteness(proof, l.Signer().PublicKey()); err != nil {
		t.Fatalf("a valid proof must verify, got: %v", err)
	}
	if len(proof.Members) != 5 {
		t.Fatalf("expected 5 members, got %d", len(proof.Members))
	}
}

func TestCompleteness_EmptyNamespaceIsValid(t *testing.T) {
	l := newCompletenessLedger(t, 0x07)
	ctx := context.Background()
	ns := "finops/tenant/2026-07"
	if _, err := l.SealNamespace(ctx, ns, "finops", nil); err != nil {
		t.Fatalf("seal empty: %v", err)
	}
	proof, err := l.BuildCompletenessProof(ctx, ns)
	if err != nil {
		t.Fatalf("build proof: %v", err)
	}
	if err := VerifyCompleteness(proof, l.Signer().PublicKey()); err != nil {
		t.Fatalf("empty sealed set must verify, got: %v", err)
	}
	if len(proof.Members) != 0 {
		t.Fatalf("expected 0 members, got %d", len(proof.Members))
	}
}

func TestCompleteness_DetectsOmission(t *testing.T) {
	l := newCompletenessLedger(t, 0x11)
	proof := seedProof(t, l, "redteam/engagement/e-2", 4)

	// Drop the last member (and its proof) — an omission.
	proof.Members = proof.Members[:len(proof.Members)-1]
	proof.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]

	if err := VerifyCompleteness(proof, l.Signer().PublicKey()); err == nil {
		t.Fatal("omission MUST fail verification, but it passed")
	}
}

func TestCompleteness_DetectsAddition(t *testing.T) {
	l := newCompletenessLedger(t, 0x22)
	proof := seedProof(t, l, "redteam/engagement/e-3", 3)

	// Duplicate a member+proof — an addition (count no longer matches the seal).
	proof.Members = append(proof.Members, proof.Members[0])
	proof.MemberProofs = append(proof.MemberProofs, proof.MemberProofs[0])

	if err := VerifyCompleteness(proof, l.Signer().PublicKey()); err == nil {
		t.Fatal("addition MUST fail verification, but it passed")
	}
}

func TestCompleteness_DetectsEditedMember(t *testing.T) {
	l := newCompletenessLedger(t, 0x33)
	proof := seedProof(t, l, "redteam/engagement/e-4", 3)

	// Tamper a member's content in place.
	proof.Members[1].Action = "redteam.tampered"

	if err := VerifyCompleteness(proof, l.Signer().PublicKey()); err == nil {
		t.Fatal("edited member MUST fail verification, but it passed")
	}
}

func TestCompleteness_DetectsReorder(t *testing.T) {
	l := newCompletenessLedger(t, 0x44)
	proof := seedProof(t, l, "redteam/engagement/e-5", 4)

	// Swap two members AND their proofs identically: each member still matches its
	// own inclusion proof (step 4 passes), but the recomputed subtree root differs
	// from the sealed root (step 5 fails) — proving order is bound.
	proof.Members[0], proof.Members[1] = proof.Members[1], proof.Members[0]
	proof.MemberProofs[0], proof.MemberProofs[1] = proof.MemberProofs[1], proof.MemberProofs[0]

	if err := VerifyCompleteness(proof, l.Signer().PublicKey()); err == nil {
		t.Fatal("reordered members MUST fail verification, but it passed")
	}
}

func TestCompleteness_DetectsWrongKey(t *testing.T) {
	l := newCompletenessLedger(t, 0x55)
	proof := seedProof(t, l, "redteam/engagement/e-6", 2)

	// A different key must not validate the sealed evidence.
	other := newCompletenessLedger(t, 0x66)
	if err := VerifyCompleteness(proof, other.Signer().PublicKey()); err == nil {
		t.Fatal("verification under the wrong key MUST fail, but it passed")
	}
}
