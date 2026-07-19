package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// completeness_test.go proves the CN-1 well end to end: real schedule.bind
// receipts for a tenant are sealed and a CompletenessProof verifies offline -
// using the SAME evidence primitive as the red team (Moat A / M0), with zero new
// cryptography. This is the Verifiable Fabric thesis (one spine, every pillar)
// made concrete and runnable.

func TestTenantSchedulingCompleteness(t *testing.T) {
	ctx := context.Background()
	engine, _ := NewEngine(EngineConfig{SchedulingInterval: 100 * time.Millisecond})
	ledger := newTestLedger(t) // shared helper from decision_test.go (same package)
	engine.SetEvidenceRecorder(ledger)

	enqueue := func(name, tenant string) {
		engine.mu.Lock()
		engine.queue = append(engine.queue, &Workload{
			ID:              common.NewUUID(),
			Name:            name,
			Namespace:       tenant, // tenant key (matches the DRF fairness ledger)
			Type:            common.WorkloadTypeTraining,
			Status:          common.WorkloadStatusQueued,
			Priority:        5,
			ResourceRequest: common.ResourceRequest{GPUCount: 1, GPUType: common.GPUTypeNvidiaA100},
			QueuedAt:        common.NowUTC(),
		})
		engine.mu.Unlock()
	}
	enqueue("job-a1", "team-a")
	enqueue("job-a2", "team-a")
	enqueue("job-b1", "team-b") // must NOT appear in team-a's sealed set

	// Drain the queue: each workload produces exactly one bind (or reject) receipt.
	for i := 0; i < 5; i++ {
		engine.schedulingCycle(ctx)
	}

	// Seal team-a's scheduling namespace and prove completeness — reusing the
	// identical evidence primitive as the red team, with no new cryptography.
	if _, err := engine.SealTenant(ctx, "team-a"); err != nil {
		t.Fatalf("seal tenant: %v", err)
	}
	proof, err := ledger.BuildCompletenessProof(ctx, TenantNamespace("team-a"))
	if err != nil {
		t.Fatalf("build proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, ledger.Signer().PublicKey()); err != nil {
		t.Fatalf("tenant scheduling completeness must verify, got: %v", err)
	}

	// The sealed set is EXACTLY team-a's scheduling receipts — none omitted, and
	// team-b's decisions excluded.
	all, _ := ledger.Store().All(ctx)
	wantA := TenantDecisions(all, "team-a")
	if len(wantA) == 0 {
		t.Fatal("expected team-a to have scheduling receipts")
	}
	if len(proof.Members) != len(wantA) {
		t.Fatalf("proof captured %d members, want %d team-a receipts", len(proof.Members), len(wantA))
	}
	for _, m := range proof.Members {
		if got := schedulingReceiptTenant(m); got != "team-a" {
			t.Fatalf("member tenant = %q, want team-a (cross-tenant leak)", got)
		}
	}

	// Tamper: omit one of the tenant's decisions -> completeness must fail
	// (proves "no less" for scheduling, exactly as for red-team reports).
	proof.Members = proof.Members[:len(proof.Members)-1]
	proof.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]
	if err := evidence.VerifyCompleteness(proof, ledger.Signer().PublicKey()); err == nil {
		t.Fatal("omitting a tenant scheduling decision MUST fail completeness")
	}
}
