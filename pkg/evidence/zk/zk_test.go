package zk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// members builds n in-scope witnesses for one namespace.
func members(ns string, n int) []LeafWitness {
	nsFE := FieldFromBytes([]byte(ns))
	ws := make([]LeafWitness, n)
	for i := 0; i < n; i++ {
		h := sha256.Sum256([]byte{byte(i), 0xAB})
		ws[i] = LeafWitness{
			Namespace:   nsFE,
			Eidx:        uint64(i),
			InScope:     true,
			PayloadHash: FieldFromBytes(h[:]),
		}
	}
	return ws
}

// TestGroth16Prover_ProveAndVerify is the end-to-end proof that the native Poseidon2
// (poseidon.go) matches gnark's in-circuit Poseidon2 (circuit.go): if they diverged,
// the public commitment computed off-circuit would not satisfy the circuit and
// Groth16 verification would fail. A green run proves the whole A1 pipeline works.
func TestGroth16Prover_ProveAndVerify(t *testing.T) {
	ws := members("redteam/engagement/E1", 3)
	att, vk, err := Groth16Prover{}.Prove(context.Background(), StmtScopeCompliance, "target in scope", ws)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if att.Mode != "real" || len(att.Proof) == 0 || att.VKID == "" {
		t.Fatalf("expected a real proof, got mode=%q proofLen=%d vkid=%q", att.Mode, len(att.Proof), att.VKID)
	}
	if att.Count != 3 {
		t.Fatalf("Count = %d, want 3", att.Count)
	}
	if err := VerifyZK(att, vk); err != nil {
		t.Fatalf("VerifyZK on a valid attestation: %v", err)
	}
}

func TestVerifyZK_TamperedPublicInputFails(t *testing.T) {
	ws := members("scheduler/tenant/T1", 3)
	att, vk, err := Groth16Prover{}.Prove(context.Background(), StmtCompletePredicate, "fair", ws)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	// Flip the public root: the proof no longer corresponds to these public inputs.
	orig := att.PublicRoot
	att.PublicRoot = att.ScopeCommit // any different valid hex
	if att.PublicRoot == orig {
		att.PublicRoot = feHex(FieldFromBytes([]byte("different")))
	}
	if err := VerifyZK(att, vk); err == nil {
		t.Fatal("expected verification to FAIL on a tampered public root")
	}
}

func TestVerifyZK_WrongVKFails(t *testing.T) {
	ws := members("finops/tenant/2026-07", 2)
	att, _, err := Groth16Prover{}.Prove(context.Background(), StmtScopeCompliance, "", ws)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	// A different circuit/setup yields a different vk; its bytes won't match VKID.
	_, otherVK, err := Groth16Prover{}.Prove(context.Background(), StmtScopeCompliance, "", members("other", 3))
	if err != nil {
		t.Fatalf("Prove(other): %v", err)
	}
	if err := VerifyZK(att, otherVK); err == nil {
		t.Fatal("expected verification to FAIL with a mismatched verifying key")
	}
}

// TestScopeViolationIsUnprovable is the soundness check: a member that is NOT in
// scope makes the circuit unsatisfiable, so no passing proof can be produced. The
// prover cannot forge scope-compliance.
func TestScopeViolationIsUnprovable(t *testing.T) {
	ws := members("redteam/engagement/E2", 3)
	ws[1].InScope = false // one out-of-scope action
	_, _, err := Groth16Prover{}.Prove(context.Background(), StmtScopeCompliance, "", ws)
	if err == nil {
		t.Fatal("expected Prove to FAIL for an out-of-scope member (unsatisfiable circuit)")
	}
}

// TestRecordAttestation binds a real attestation into the ledger as an
// evidence.zk.attest receipt and confirms the receipt is signed + chained.
func TestRecordAttestation(t *testing.T) {
	ws := members("redteam/engagement/REC", 2)
	att, _, err := Groth16Prover{}.Prove(context.Background(), StmtScopeCompliance, "", ws)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x2c}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	rec, err := RecordAttestation(context.Background(), l, "redteam/engagement/REC", att)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if rec.Action != ActionZKAttest || rec.Subject != "redteam/engagement/REC" {
		t.Fatalf("unexpected receipt: action=%q subject=%q", rec.Action, rec.Subject)
	}
	if r := evidence.VerifyRecord(rec, signer.PublicKey()); !r.OK() {
		t.Fatalf("recorded attestation must verify: %s", r.Error)
	}
}

func TestDryRunProver_NotVerifiable(t *testing.T) {
	ws := members("redteam/engagement/E3", 2)
	att, vk, err := DryRunProver{}.Prove(context.Background(), StmtScopeCompliance, "", ws)
	if err != nil {
		t.Fatalf("DryRun Prove (non-production): %v", err)
	}
	if att.Mode != "simulated" || len(att.Proof) != 0 {
		t.Fatalf("dry-run must be labeled simulated with no proof, got mode=%q proofLen=%d", att.Mode, len(att.Proof))
	}
	// The public commitment must still match the real prover's (binding to A0 holds).
	real := feHex(Commitment(ws))
	if att.PublicRoot != real {
		t.Fatalf("dry-run commitment %s != real commitment %s", att.PublicRoot, real)
	}
	// A simulated attestation cannot be cryptographically verified.
	if err := VerifyZK(att, vk); err == nil {
		t.Fatal("expected VerifyZK to reject a proof-less (simulated) attestation")
	}
}
