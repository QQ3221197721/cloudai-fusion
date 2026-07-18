package redteam

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func TestBuildReport_EmbedsSignedCheckpoint(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()

	reg := NewToolRegistry()
	reg.Register(fakeTool{})
	e, err := mgr.Create(ctx, readOnlyScope(), "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	exec := NewToolExecutor(e.ID, reg, ledger, nil)
	planner := StaticPlanner{Actions: []Action{
		{ID: "r1", Technique: "T1595", Tool: "fake", Target: "lab.local", RiskTier: RiskReadOnly},
	}}
	if _, err := NewEngine(mgr, planner, exec, nil).Run(ctx, e.ID); err != nil {
		t.Fatalf("run: %v", err)
	}

	all, _ := ledger.Store().All(ctx)
	rep, err := BuildReport(ctx, e, all, ledger, ledger, nil)
	if err != nil {
		t.Fatalf("build report: %v", err)
	}

	// The report must embed a signed checkpoint (STH) that verifies.
	if rep.Checkpoint == nil {
		t.Fatal("report must embed a signed checkpoint")
	}
	if err := evidence.VerifyCheckpoint(rep.Checkpoint, ledger.Signer().PublicKey()); err != nil {
		t.Fatalf("embedded checkpoint must verify: %v", err)
	}
	// It must summarize the engagement's real receipts + techniques.
	if rep.ActionCounts[ActionRecon] < 1 {
		t.Fatalf("report must count the recon receipt, got %+v", rep.ActionCounts)
	}
	if len(rep.Receipts) == 0 {
		t.Fatal("report must reference the engagement receipts")
	}
	var sawTech bool
	for _, tech := range rep.Techniques {
		if tech == "T1595" {
			sawTech = true
		}
	}
	if !sawTech {
		t.Fatalf("report must record exercised techniques, got %v", rep.Techniques)
	}
	// Generating a report records exactly one redteam.report receipt.
	all2, _ := ledger.Store().All(ctx)
	if countAction(all2, ActionReport) != 1 {
		t.Fatalf("expected exactly 1 redteam.report receipt, got %d", countAction(all2, ActionReport))
	}
	assertChainVerifies(t, ledger)
}
