package redteam

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

func TestTenant_IsolationAcrossTenants(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()

	ea, err := mgr.CreateForTenant(ctx, "tenant-A", readOnlyScope(), "alice")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	// Owner can fetch; a different tenant cannot (not-found, no disclosure).
	if _, err := mgr.GetForTenant("tenant-A", ea.ID); err != nil {
		t.Fatalf("owner must access its engagement: %v", err)
	}
	if _, err := mgr.GetForTenant("tenant-B", ea.ID); err == nil {
		t.Fatal("cross-tenant access must be refused")
	}
	if len(mgr.ListByTenant("tenant-A")) != 1 || len(mgr.ListByTenant("tenant-B")) != 0 {
		t.Fatal("ListByTenant must isolate per tenant")
	}
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionTenantBind) != 1 {
		t.Fatalf("expected a tenant bind receipt, got %d", countAction(all, ActionTenantBind))
	}
	assertChainVerifies(t, ledger)
}

func TestCostMeter_EmitsVerifiableCostReceipt(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	ctx := context.Background()
	meter := NewCostMeter(ledger, nil)

	meter.Add("eng-1", CostBreakdown{LLMTokens: 1000, ToolSeconds: 5, EstimatedUSD: 0.02})
	meter.Add("eng-1", CostBreakdown{LLMTokens: 500, GPUSeconds: 3, EstimatedUSD: 0.01})
	total := meter.Flush(ctx, "eng-1")
	if total.LLMTokens != 1500 || total.EstimatedUSD != 0.03 {
		t.Fatalf("cost must accumulate, got %+v", total)
	}

	all, _ := ledger.Store().All(ctx)
	var payload map[string]any
	for _, ev := range all {
		if ev.Action == ActionCost {
			_ = json.Unmarshal(ev.Payload, &payload)
		}
	}
	if payload == nil || payload["usd_is_estimate"] != true {
		t.Fatalf("cost receipt must honestly flag the USD estimate, got %v", payload)
	}
	assertChainVerifies(t, ledger)
}

func TestNavigatorLayer_ExportsTechniques(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	reg := NewToolRegistry()
	reg.Register(fakeTool{})
	e, _ := mgr.Create(ctx, readOnlyScope(), "alice")
	exec := NewToolExecutor(e.ID, reg, ledger, nil)
	_, _ = NewEngine(mgr, StaticPlanner{Actions: []Action{
		{ID: "r1", Technique: "T1595", Tool: "fake", Target: "lab.local", RiskTier: RiskReadOnly},
	}}, exec, nil).Run(ctx, e.ID)

	all, _ := ledger.Store().All(ctx)
	rep, _ := BuildReport(ctx, e, all, ledger, ledger, nil)
	layer, err := NavigatorLayer(rep)
	if err != nil {
		t.Fatalf("navigator: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(layer, &parsed); err != nil {
		t.Fatalf("layer must be valid JSON: %v", err)
	}
	if parsed["domain"] != "enterprise-attack" || !strings.Contains(string(layer), "T1595") {
		t.Fatalf("navigator layer must map exercised techniques, got %s", string(layer))
	}
}

func TestExportDPOTraces_FromLedger(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	reg := NewToolRegistry()
	reg.Register(fakeTool{})
	e, _ := mgr.Create(ctx, readOnlyScope(), "alice")
	exec := NewToolExecutor(e.ID, reg, ledger, nil)
	// One in-scope (authorized) + one out-of-scope (denied) action.
	_, _ = NewEngine(mgr, StaticPlanner{Actions: []Action{
		{ID: "ok", Technique: "T1595", Tool: "fake", Target: "lab.local", RiskTier: RiskReadOnly},
		{ID: "bad", Technique: "T1595", Tool: "fake", Target: "evil.com", RiskTier: RiskReadOnly},
	}}, exec, nil).Run(ctx, e.ID)

	all, _ := ledger.Store().All(ctx)
	pairs := ExportDPOTraces(all)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 preference pair (1 chosen vs 1 rejected), got %d", len(pairs))
	}
	if pairs[0].Chosen == "" || pairs[0].Rejected == "" || pairs[0].Prompt == "" {
		t.Fatalf("preference pair must be populated: %+v", pairs[0])
	}
	jsonl, err := ExportDPOJSONL(pairs)
	if err != nil || len(jsonl) == 0 {
		t.Fatalf("JSONL export must be non-empty: err=%v", err)
	}
}

// TestAccept_CapabilityEnforceCleanInProduction is the spec's M5 acceptance:
// with all backends configured real, capability.Enforce() passes under the
// Production policy (the subsystem is production-clean when properly configured).
func TestAccept_CapabilityEnforceCleanInProduction(t *testing.T) {
	t.Cleanup(func() {
		capability.SetPolicy(runmode.Simulation)
		capability.Reset()
	})
	capability.Reset()

	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)                                    // redteam.authz = real
	_ = NewLLMPlanner(NewOpenAICompatClient("http://llm:8000", "k", "qwen2"), nil) // redteam.planner = real
	_ = NewWebExploitEngine(mgr, ledger, nil)                        // redteam.exploit.web = real
	_ = NewADPathEngine(mgr, ledger, nil, "kind-adrange")            // ad.pathing + ad.isolation = real
	_ = NewCostMeter(ledger, nil)                                    // redteam.finops = real
	if _, err := mgr.CreateForTenant(context.Background(), "tenant-A", readOnlyScope(), "alice"); err != nil {
		t.Fatalf("create: %v", err) // redteam.tenant = real
	}

	capability.SetPolicy(runmode.Production)
	if err := capability.Enforce(); err != nil {
		t.Fatalf("production Enforce must be clean with all-real backends: %v", err)
	}
	// Sanity: every reported redteam.* backend is real under this configuration.
	for _, b := range capability.Snapshot() {
		if strings.HasPrefix(b.Component, "redteam.") && b.Mode != capability.ModeReal {
			t.Fatalf("component %s must be real in production, got %s", b.Component, b.Mode)
		}
	}
}

// TestAccept_CVEBenchRegressionStable is the spec's M5 acceptance: the CVE-Bench
// suite runs with zero scope violations and 100%% verifiable receipts.
func TestAccept_CVEBenchRegressionStable(t *testing.T) {
	t.Cleanup(capability.Reset)
	reg := NewToolRegistry()
	reg.Register(fakeTool{})
	newL := func() *evidence.Ledger { return newLedger(t) }
	cases := []BenchCase{
		{Name: "c1", Scope: readOnlyScope(), ExpectFindingTechnique: "T1595",
			Actions: []Action{{ID: "a", Technique: "T1595", Tool: "fake", Target: "lab.local", RiskTier: RiskReadOnly}}},
		{Name: "c2", Scope: readOnlyScope(), ExpectFindingTechnique: "T1595",
			Actions: []Action{{ID: "b", Technique: "T1595", Tool: "fake", Target: "lab.local", RiskTier: RiskReadOnly}}},
	}
	_, m, err := RunSuite(context.Background(), newL, reg, cases, nil)
	if err != nil {
		t.Fatalf("suite: %v", err)
	}
	if m.ScopeViolations != 0 || !m.AllVerified || m.SolveRate != 1.0 {
		t.Fatalf("regression gate must be stable: %+v", m)
	}
}
