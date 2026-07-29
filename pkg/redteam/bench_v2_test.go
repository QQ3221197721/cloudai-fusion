package redteam

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

func TestRunDefaultSuite_ScoresAllSolvedAndVerified(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()

	results, metrics, err := RunDefaultSuite(ctx, nil)
	if err != nil {
		t.Fatalf("RunDefaultSuite: %v", err)
	}
	if metrics.Cases != 3 {
		t.Fatalf("expected 3 cases, got %d", metrics.Cases)
	}
	// The deterministic BenchTool must solve every case.
	if metrics.Solved != metrics.Cases || metrics.SolveRate != 1.0 {
		t.Fatalf("expected full solve rate, got solved=%d rate=%.2f", metrics.Solved, metrics.SolveRate)
	}
	// Core honesty invariants: zero scope violations, every receipt verifies.
	if metrics.ScopeViolations != 0 {
		t.Fatalf("scope violations must be 0, got %d", metrics.ScopeViolations)
	}
	if !metrics.AllVerified {
		t.Fatalf("all evidence receipts must verify")
	}
	for _, r := range results {
		if !r.Solved || !r.ReceiptsVerified || r.ScopeViolations != 0 {
			t.Fatalf("case %q failed invariants: %+v", r.Case, r)
		}
	}
}

func TestDefaultBenchSuite_Shape(t *testing.T) {
	cases := DefaultBenchSuite()
	if len(cases) != 3 {
		t.Fatalf("expected 3 default cases, got %d", len(cases))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if c.Name == "" || len(c.Actions) == 0 {
			t.Fatalf("malformed case: %+v", c)
		}
		if c.ExpectFindingTechnique == "" {
			t.Fatalf("case %q must expect a specific technique", c.Name)
		}
		if !c.Scope.InScope("bench.local") {
			t.Fatalf("case %q target must be in scope", c.Name)
		}
		seen[c.ExpectFindingTechnique] = true
	}
	for _, want := range []string{"T1190", "T1071", "T1210"} {
		if !seen[want] {
			t.Fatalf("suite missing expected technique %s", want)
		}
	}
}

func TestBenchTool_ReportsSimulated(t *testing.T) {
	tool := NewBenchTool("bench", "T1190")
	if tool.Mode() != capability.ModeSimulated {
		t.Fatalf("BenchTool must be simulated (no real binary)")
	}
	out, err := tool.Invoke(context.Background(), ToolInput{Target: "bench.local"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out.Finding == nil || out.Finding.Asset != "bench.local" {
		t.Fatalf("BenchTool must emit a finding for the target: %+v", out.Finding)
	}
}

func TestRangeManager_Lifecycle(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()
	mgr := NewRangeManager(nil, nil) // defaults to in-memory provider

	r, err := mgr.Provision(ctx, RangeSpec{Name: "juice", Apps: []string{"juice-shop"}})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if _, ok := mgr.Get(r.ID); !ok {
		t.Fatalf("provisioned range must be retrievable")
	}
	if len(mgr.List()) != 1 {
		t.Fatalf("expected 1 range, got %d", len(mgr.List()))
	}

	if err := mgr.Teardown(ctx, r.ID); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if _, ok := mgr.Get(r.ID); ok {
		t.Fatalf("range must be gone after teardown")
	}
	if err := mgr.Teardown(ctx, r.ID); err == nil {
		t.Fatalf("tearing down an unknown range must error")
	}
}
