//go:build integration

package redteam

import (
	"bytes"
	"context"
	"os/exec"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestRecon_Integration_RealNmapLocalhost runs ONLY with -tags integration and
// only if nmap is installed. It scans localhost (127.0.0.1) - an authorized,
// self-owned target - proving the real tool -> real verifiable evidence loop:
//
//	go test -tags integration ./pkg/redteam/ -run Integration -v
func TestRecon_Integration_RealNmapLocalhost(t *testing.T) {
	if _, err := exec.LookPath("nmap"); err != nil {
		t.Skip("nmap not installed; skipping real recon integration test")
	}
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x7b}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	mgr := NewManager(ledger, nil)
	ctx := context.Background()

	scope := Scope{
		Targets:         []Target{{Kind: TargetCIDR, Value: "127.0.0.0/8"}},
		AllowTechniques: []string{"T1046"},
		MaxRiskTier:     RiskReadOnly,
		ApprovalReq:     RiskExploit,
	}
	e, err := mgr.Create(ctx, scope, "integration")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	reg := NewToolRegistry()
	reg.Register(NewNmapTool())
	executor := NewToolExecutor(e.ID, reg, ledger, nil)
	planner := StaticPlanner{Actions: []Action{
		{ID: "n1", Technique: "T1046", Tool: "nmap", Target: "127.0.0.1", RiskTier: RiskReadOnly},
	}}

	res, err := NewEngine(mgr, planner, executor, nil).Run(ctx, e.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Executed != 1 {
		t.Fatalf("expected 1 executed action, got %+v", res)
	}

	all, _ := ledger.Store().All(ctx)
	var realRecon bool
	for _, ev := range all {
		if ev.Action != ActionRecon {
			continue
		}
		for _, b := range ev.Backends {
			if b.Component == "redteam.tool.nmap" && b.Mode == "real" {
				realRecon = true
			}
		}
	}
	if !realRecon {
		t.Fatal("expected a REAL nmap recon receipt")
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("recon evidence chain must verify: %+v", rep)
	}
	t.Logf("REAL nmap recon on 127.0.0.1 recorded and verified")
}
