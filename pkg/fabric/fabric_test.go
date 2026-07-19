package fabric_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/finops"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// fabric_test.go is the capstone of MF: ONE Fabric over ONE ledger seals and
// completeness-proves THREE heterogeneous pillars (red team, scheduling, FinOps)
// through the identical primitive, and a brand-new cross-pillar well is onboarded
// with a single Register + a one-line predicate. This is the spec's "closed under
// composition, open for extension" invariant demonstrated as runnable fact - the
// architecture, not a patch.

func newLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x9c}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func TestFabricUnifiesAllPillars(t *testing.T) {
	ctx := context.Background()
	ledger := newLedger(t)
	pub := ledger.Signer().PublicKey()

	// --- Pillar II: red team (real Manager) -> 2 receipts (grant + finding). ---
	mgr := redteam.NewManager(ledger, nil)
	eng, err := mgr.Create(ctx, redteam.Scope{
		Targets:         []redteam.Target{{Kind: redteam.TargetCIDR, Value: "10.0.0.0/24"}},
		AllowTechniques: []string{"*"},
	}, "tester")
	if err != nil {
		t.Fatalf("create engagement: %v", err)
	}
	_ = mgr.Start(eng.ID)
	if err := mgr.AddFinding(ctx, eng.ID, &redteam.Finding{
		Asset: "10.0.0.5", Severity: "high", Title: "open service", Technique: "T1046",
	}); err != nil {
		t.Fatalf("add finding: %v", err)
	}

	// --- Pillar I: scheduling (real exported types) -> 2 team-x bind receipts. ---
	for _, id := range []string{"wl-1", "wl-2"} {
		dec := &scheduler.SchedulingDecision{
			WorkloadID: id, WorkloadName: id, Namespace: "team-x",
			ChosenNode: "gpu-node-1", Score: 100, DecidedAt: time.Now().UTC(),
			Candidates: []scheduler.CandidateScore{{NodeName: "gpu-node-1", Score: 100, Chosen: true}},
		}
		if _, err := ledger.Record(ctx, evidence.RecordInput{
			Actor: "scheduler", Action: "schedule.bind", Subject: id, Payload: dec,
		}); err != nil {
			t.Fatalf("record bind %s: %v", id, err)
		}
	}

	// --- Pillar I: FinOps (real ReclaimEngine) -> 1 reclaim receipt this month. ---
	fe := finops.NewReclaimEngine(finops.ReclaimEngineConfig{Recorder: ledger})
	now := time.Now().UTC()
	samples := []finops.UtilizationSample{
		{Timestamp: now.Add(-2 * time.Hour), GPUUtilization: 2, Source: "dcgm"},
		{Timestamp: now.Add(-1 * time.Hour), GPUUtilization: 3, Source: "dcgm"},
		{Timestamp: now, GPUUtilization: 1, Source: "dcgm"},
	}
	if _, _, err := fe.Reclaim(ctx, finops.ReclaimTarget{
		ResourceID: "n1/g0", GPUCount: 1, HourlyRate: 2, PriceSource: "static-table",
	}, samples); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	month := now.Format("2006-01")

	// --- One Fabric registers every pillar's KeyFunc: the whole cost of onboarding. ---
	f := fabric.New(ledger)
	for _, w := range []fabric.Well{
		{Name: "redteam", Prefix: "redteam/engagement", Actor: "redteam", KeyOf: redteam.EngagementKeyOf},
		{Name: "scheduler", Prefix: "scheduler/tenant", Actor: "scheduler", KeyOf: scheduler.TenantKeyOf},
		{Name: "finops", Prefix: "finops/month", Actor: "finops", KeyOf: finops.MonthKeyOf},
	} {
		if err := f.Register(w); err != nil {
			t.Fatalf("register %s: %v", w.Name, err)
		}
	}

	// The SAME Seal/Completeness/Verify path proves all three heterogeneous wells.
	check := func(well, key string, wantMembers int) {
		t.Helper()
		if _, err := f.Seal(ctx, well, key); err != nil {
			t.Fatalf("seal %s/%s: %v", well, key, err)
		}
		proof, err := f.Completeness(ctx, well, key)
		if err != nil {
			t.Fatalf("completeness %s/%s: %v", well, key, err)
		}
		if err := evidence.VerifyCompleteness(proof, pub); err != nil {
			t.Fatalf("verify %s/%s: %v", well, key, err)
		}
		if len(proof.Members) != wantMembers {
			t.Fatalf("%s/%s: got %d members, want %d", well, key, len(proof.Members), wantMembers)
		}
	}
	check("redteam", eng.ID, 2)
	check("scheduler", "team-x", 2)
	check("finops", month, 1)

	// --- Open for extension: a brand-new CROSS-PILLAR well in one Register + a
	// one-line predicate. "audit/day" bundles every pillar's receipts for a day. ---
	if err := f.Register(fabric.Well{
		Name: "daily-audit", Prefix: "audit/day", Actor: "audit",
		KeyOf: func(e *evidence.Evidence) string { return e.Timestamp.UTC().Format("2006-01-02") },
	}); err != nil {
		t.Fatalf("register daily-audit: %v", err)
	}
	today := now.Format("2006-01-02")
	if _, err := f.Seal(ctx, "daily-audit", today); err != nil {
		t.Fatalf("seal daily-audit: %v", err)
	}
	auditProof, err := f.Completeness(ctx, "daily-audit", today)
	if err != nil {
		t.Fatalf("completeness daily-audit: %v", err)
	}
	if err := evidence.VerifyCompleteness(auditProof, pub); err != nil {
		t.Fatalf("verify daily-audit: %v", err)
	}
	// 2 redteam + 2 scheduler + 1 finops = 5 non-seal receipts recorded today
	// (prior seals are excluded by the Fabric).
	if len(auditProof.Members) != 5 {
		t.Fatalf("daily-audit: got %d cross-pillar members, want 5", len(auditProof.Members))
	}
	if len(f.Wells()) != 4 {
		t.Fatalf("expected 4 registered wells, got %d", len(f.Wells()))
	}

	// Tamper the cross-pillar bundle -> completeness must fail, exactly as for a
	// single pillar. One spine, one guarantee, everywhere.
	auditProof.Members = auditProof.Members[:len(auditProof.Members)-1]
	auditProof.MemberProofs = auditProof.MemberProofs[:len(auditProof.MemberProofs)-1]
	if err := evidence.VerifyCompleteness(auditProof, pub); err == nil {
		t.Fatal("tampering a cross-pillar audit bundle MUST fail completeness")
	}
}
