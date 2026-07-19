package redteam

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// bench_record_test.go proves a recorded bench attestation is a first-class ledger
// citizen: the chain verifies and the embedded attestation still verifies against
// the pinned key.

func TestRecordBenchAttestation(t *testing.T) {
	ctx := context.Background()
	ledger := newSealedLedger(t, 0x71) // helper from completeness_test.go (same package)
	pub := ledger.Signer().PublicKey()

	m := Metrics{Cases: 3, Solved: 2, SolveRate: 2.0 / 3.0, ScopeViolations: 0, AllVerified: true}
	att, err := BuildBenchAttestation(ledger.Signer(), "cve-bench-tier2", nil, m, nil)
	if err != nil {
		t.Fatalf("build attestation: %v", err)
	}
	ev, err := RecordBenchAttestation(ctx, ledger, att)
	if err != nil || ev == nil {
		t.Fatalf("record bench: ev=%v err=%v", ev, err)
	}

	all, _ := ledger.Store().All(ctx)
	rep, _ := evidence.VerifyChain(all, pub)
	if !rep.Valid {
		t.Fatalf("chain with recorded bench attestation must verify, got %+v", rep)
	}

	var got BenchAttestation
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("decode bench payload: %v", err)
	}
	if err := VerifyBenchAttestation(&got, pub); err != nil {
		t.Fatalf("embedded bench attestation must verify: %v", err)
	}
	if got.Suite != "cve-bench-tier2" {
		t.Fatalf("suite mismatch: %q", got.Suite)
	}
}
