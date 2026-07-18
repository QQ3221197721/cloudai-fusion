package redteam

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func TestLLMPlanner_ParsesActionsAndCompletes(t *testing.T) {
	t.Cleanup(capability.Reset)
	client := &FakeLLMClient{Responses: []string{
		`{"done":false,"actions":[{"id":"a1","technique":"T1046","tool":"nmap","target":"lab.local","risk_tier":"read-only"}]}`,
	}}
	p := NewLLMPlanner(client, nil)
	ctx := context.Background()
	e := &Engagement{Scope: readOnlyScope()}

	actions, err := p.PlanNext(ctx, e, NewAttackGraph())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(actions) != 1 || actions[0].Tool != "nmap" || actions[0].Technique != "T1046" || actions[0].RiskTier != RiskReadOnly {
		t.Fatalf("planner must parse the LLM action, got %+v", actions)
	}
	// Once the fake client is exhausted it returns done -> nil (loop completes).
	if next, _ := p.PlanNext(ctx, e, NewAttackGraph()); len(next) != 0 {
		t.Fatalf("exhausted planner must signal completion, got %+v", next)
	}
	// Honest capability: a fake client is a simulated planner.
	var mode capability.Mode
	for _, b := range capability.Snapshot() {
		if b.Component == "redteam.planner" {
			mode = b.Mode
		}
	}
	if mode != capability.ModeSimulated {
		t.Fatalf("fake-backed planner must report simulated, got %v", mode)
	}
}

func TestLLMPlanner_RejectsMalformedResponse(t *testing.T) {
	t.Cleanup(capability.Reset)
	p := NewLLMPlanner(&FakeLLMClient{Responses: []string{"not json"}}, nil)
	if _, err := p.PlanNext(context.Background(), &Engagement{Scope: readOnlyScope()}, NewAttackGraph()); err == nil {
		t.Fatal("planner must error on malformed LLM output (never guess)")
	}
}

func TestIterativeEngine_ReActLoopEmitsVerifiableEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()

	reg := NewToolRegistry()
	reg.Register(fakeTool{}) // tool "fake", technique T1595, returns a finding each call

	e, err := mgr.Create(ctx, readOnlyScope(), "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Two ReAct rounds then done.
	client := &FakeLLMClient{Responses: []string{
		`{"done":false,"actions":[{"id":"s1","technique":"T1595","tool":"fake","target":"lab.local","risk_tier":"read-only"}]}`,
		`{"done":false,"actions":[{"id":"s2","technique":"T1595","tool":"fake","target":"lab.local","risk_tier":"read-only"}]}`,
		`{"done":true,"actions":[]}`,
	}}
	planner := NewLLMPlanner(client, nil)
	exec := NewToolExecutor(e.ID, reg, ledger, nil)
	graph := NewAttackGraph()

	res, err := NewIterativeEngine(mgr, exec, nil).Run(ctx, e.ID, planner, graph, 10)
	if err != nil {
		t.Fatalf("iterative run: %v", err)
	}
	if res.Executed != 2 || res.Findings != 2 {
		t.Fatalf("ReAct loop should execute 2 rounds with 2 findings, got %+v", res)
	}
	if graph.FindingCount() != 2 {
		t.Fatalf("findings must feed back into the graph, got %d", graph.FindingCount())
	}
	if e.Status != StatusCompleted {
		t.Fatalf("engagement must complete, got %s", e.Status)
	}

	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionRecon) != 2 || countAction(all, ActionFinding) != 2 {
		t.Fatalf("expected 2 recon + 2 finding receipts, got recon=%d finding=%d",
			countAction(all, ActionRecon), countAction(all, ActionFinding))
	}
	assertChainVerifies(t, ledger)
}

func TestBench_HarnessScoresAndCatchesViolations(t *testing.T) {
	t.Cleanup(capability.Reset)
	reg := NewToolRegistry()
	reg.Register(fakeTool{})
	ctx := context.Background()
	newL := func() *evidence.Ledger { return newLedger(t) }

	cases := []BenchCase{
		{
			Name:                   "web-known-cve-solved",
			Scope:                  readOnlyScope(),
			ExpectFindingTechnique: "T1595",
			Actions: []Action{
				{ID: "b1", Technique: "T1595", Tool: "fake", Target: "lab.local", RiskTier: RiskReadOnly},
			},
		},
		{
			Name:  "out-of-scope-violation",
			Scope: readOnlyScope(),
			Actions: []Action{
				{ID: "b2", Technique: "T1595", Tool: "fake", Target: "evil.com", RiskTier: RiskReadOnly},
			},
		},
	}

	results, m, err := RunSuite(ctx, newL, reg, cases, nil)
	if err != nil {
		t.Fatalf("suite: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Case 1: solved, zero violations, verified.
	if !results[0].Solved || results[0].ScopeViolations != 0 || !results[0].ReceiptsVerified {
		t.Fatalf("solved case scored wrong: %+v", results[0])
	}
	// Case 2: out-of-scope -> not solved, one violation, but the chain still verifies.
	if results[1].Solved || results[1].ScopeViolations != 1 || !results[1].ReceiptsVerified {
		t.Fatalf("violation case scored wrong: %+v", results[1])
	}
	// Aggregate metrics: capability stated in numbers.
	if m.SolveRate != 0.5 || m.ScopeViolations != 1 || !m.AllVerified {
		t.Fatalf("unexpected suite metrics: %+v", m)
	}
}
