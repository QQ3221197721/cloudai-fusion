package finops

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// completeness_test.go proves the CN-2 well end to end: real, measured reclaims in
// a month are sealed and a CompletenessProof verifies offline - using the SAME
// evidence primitive as the red team and the scheduler (Moat A / M0), with zero
// new cryptography. Realized savings become impossible to inflate or cherry-pick.

func TestMonthlyFinOpsCompleteness(t *testing.T) {
	ctx := context.Background()
	ledger := testLedger(t) // shared helper from reclaim_test.go (same package)
	eng := NewReclaimEngine(ReclaimEngineConfig{Action: &realReclaim{}, Recorder: ledger})

	month := time.Now().UTC().Format(monthLayout)

	// Two real, measured reclaims in the current month.
	for i, id := range []string{"node-1/gpu-0", "node-2/gpu-1"} {
		from := time.Now().UTC().Add(-time.Duration(2+i) * time.Hour)
		if _, _, err := eng.Reclaim(ctx, ReclaimTarget{
			ResourceID: id, Node: id, GPUType: "nvidia-a100", GPUCount: 1,
			HourlyRate: 3.0, PriceSource: "billing-api",
		}, idleSamples("dcgm", from, 3)); err != nil {
			t.Fatalf("reclaim %s: %v", id, err)
		}
	}

	// A reclaim from a PRIOR month must be grouped separately and excluded here.
	old := time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)
	if _, err := ledger.Record(ctx, evidence.RecordInput{
		Actor: "finops", Action: finopsReclaimAction, Subject: "node-9/gpu-9",
		Payload: map[string]any{"reclaim_id": "old", "reclaimed_at": old},
	}); err != nil {
		t.Fatalf("record prior-month reclaim: %v", err)
	}

	if _, err := eng.SealMonth(ctx, month); err != nil {
		t.Fatalf("seal month: %v", err)
	}
	proof, err := ledger.BuildCompletenessProof(ctx, MonthNamespace(month))
	if err != nil {
		t.Fatalf("build proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, ledger.Signer().PublicKey()); err != nil {
		t.Fatalf("monthly FinOps completeness must verify, got: %v", err)
	}

	// Exactly this month's two reclaims; the 2020-01 reclaim is excluded.
	if len(proof.Members) != 2 {
		t.Fatalf("expected 2 reclaims this month, got %d", len(proof.Members))
	}
	for _, m := range proof.Members {
		if got := reclaimReceiptMonth(m); got != month {
			t.Fatalf("member month = %q, want %q (cross-month leak)", got, month)
		}
	}
	all, _ := ledger.Store().All(ctx)
	if got := len(MonthReclaims(all, "2020-01")); got != 1 {
		t.Fatalf("expected the prior-month reclaim to be grouped separately, got %d", got)
	}

	// Tamper: omit a reclaim -> completeness must fail (savings cannot be hidden).
	proof.Members = proof.Members[:len(proof.Members)-1]
	proof.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]
	if err := evidence.VerifyCompleteness(proof, ledger.Signer().PublicKey()); err == nil {
		t.Fatal("omitting a reclaim MUST fail monthly completeness")
	}
}
