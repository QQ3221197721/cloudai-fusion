package redteam

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// fakeTool is a deterministic Tool for unit tests: it returns canned output and a
// finding, so the recon evidence + finding path can be verified without binaries.
type fakeTool struct{ mode capability.Mode }

func (fakeTool) Name() string         { return "fake" }
func (fakeTool) Techniques() []string { return []string{"T1595"} }
func (f fakeTool) Mode() capability.Mode {
	if f.mode == "" {
		return capability.ModeReal
	}
	return f.mode
}
func (fakeTool) Invoke(_ context.Context, in ToolInput) (ToolOutput, error) {
	return ToolOutput{
		Raw:     []byte("open: 80,443"),
		Summary: "fake scan ok",
		Finding: &Finding{Asset: in.Target, Severity: "info", Title: "fake finding"},
		Mode:    capability.ModeReal,
		Driver:  "fake",
	}, nil
}

func TestToolExecutor_EmitsReconEvidenceAndFinding(t *testing.T) {
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
	res, err := NewEngine(mgr, planner, exec, nil).Run(ctx, e.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Authorized != 1 || res.Executed != 1 || res.Findings != 1 {
		t.Fatalf("unexpected run result: %+v", res)
	}

	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionScopeGrant) != 1 || countAction(all, ActionActionAuthorized) != 1 ||
		countAction(all, ActionRecon) != 1 || countAction(all, ActionFinding) != 1 {
		t.Fatalf("unexpected receipt mix: grant=%d auth=%d recon=%d finding=%d",
			countAction(all, ActionScopeGrant), countAction(all, ActionActionAuthorized),
			countAction(all, ActionRecon), countAction(all, ActionFinding))
	}

	// The recon receipt must fingerprint the real tool I/O and be tagged real.
	var recon map[string]any
	for _, ev := range all {
		if ev.Action == ActionRecon {
			_ = json.Unmarshal(ev.Payload, &recon)
		}
	}
	if recon == nil {
		t.Fatal("missing recon receipt payload")
	}
	if recon["mode"] != "real" {
		t.Fatalf("recon mode must be real, got %v", recon["mode"])
	}
	if recon["output_hash"] != sha256Hex([]byte("open: 80,443")) {
		t.Fatalf("recon output_hash must fingerprint the tool output, got %v", recon["output_hash"])
	}
	if recon["input_hash"] == "" || recon["input_hash"] == nil {
		t.Fatal("recon must record an input fingerprint")
	}
	assertChainVerifies(t, ledger)
}

func TestCommandTool_SimModeIsHonest(t *testing.T) {
	t.Cleanup(capability.Reset)
	ct := &CommandTool{
		name:       "ghosttool",
		binary:     "definitely-not-installed-xyz-caf",
		techniques: []string{"T1046"},
		argsFn:     func(target string, _ map[string]any) []string { return []string{target} },
	}
	if ct.Mode() != capability.ModeSimulated {
		t.Fatal("a missing binary must report simulated mode")
	}
	out, err := ct.Invoke(context.Background(), ToolInput{Target: "lab.local"})
	if err != nil {
		t.Fatalf("sim invoke must not error: %v", err)
	}
	if out.Mode != capability.ModeSimulated {
		t.Fatalf("sim invocation must be tagged simulated, got %v", out.Mode)
	}

	reg := NewToolRegistry()
	reg.Register(ct)
	var mode capability.Mode
	var found bool
	for _, b := range capability.Snapshot() {
		if b.Component == "redteam.tool.ghosttool" {
			found, mode = true, b.Mode
		}
	}
	if !found || mode != capability.ModeSimulated {
		t.Fatalf("registry must honestly report the tool as simulated (found=%v mode=%v)", found, mode)
	}
}

func TestInMemoryRange_ProvisionTeardownReceipts(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	ctx := context.Background()
	p := NewInMemoryRangeProvider(ledger, nil)

	r, err := p.Provision(ctx, RangeSpec{Name: "r1", Apps: []string{"juice-shop"}})
	if err != nil || r.ID == "" {
		t.Fatalf("provision: r=%+v err=%v", r, err)
	}
	if err := p.Teardown(ctx, r.ID); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	if err := p.Teardown(ctx, r.ID); err == nil {
		t.Fatal("tearing down an unknown range must error")
	}

	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionRangeProvision) != 1 || countAction(all, ActionRangeTeardown) != 1 {
		t.Fatalf("expected 1 provision + 1 teardown receipt")
	}
	// Provision receipt must honestly tag the in-memory (simulated) driver.
	for _, ev := range all {
		if ev.Action == ActionRangeProvision {
			var sim bool
			for _, b := range ev.Backends {
				if b.Component == "redteam.range" && b.Mode == "simulated" && b.Driver == "in-memory" {
					sim = true
				}
			}
			if !sim {
				t.Fatalf("in-memory range must be tagged simulated, got %+v", ev.Backends)
			}
		}
	}
	assertChainVerifies(t, ledger)
}

func TestToolRegistry_ReportsToolCapability(t *testing.T) {
	t.Cleanup(capability.Reset)
	reg := NewToolRegistry()
	reg.Register(NewNmapTool())
	reg.Register(NewHTTPXTool())

	seen := map[string]bool{}
	for _, b := range capability.Snapshot() {
		seen[b.Component] = true
	}
	if !seen["redteam.tool.nmap"] || !seen["redteam.tool.httpx"] {
		t.Fatalf("both recon tools must be reported to capability, seen=%v", seen)
	}
}
