package redteam

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// bench.go is the CVE-Bench / Cybench-style acceptance harness. It runs an
// engagement against a benchmark case and reports capability in NUMBERS, not
// adjectives: solve rate, actions-to-solve, scope violations (must be 0), and
// evidence-verification pass rate (must be 100%). This is how the subsystem
// proves its ability honestly and catches regressions in CI.

// BenchCase is one benchmark scenario: a signed scope, a plan, and a success
// condition (a finding of the expected technique, if specified).
type BenchCase struct {
	Name                   string   `json:"name"`
	Scope                  Scope    `json:"scope"`
	Actions                []Action `json:"actions"`
	ExpectFindingTechnique string   `json:"expect_finding_technique"` // empty = any finding solves
}

// BenchResult is the scored outcome of a benchmark case.
type BenchResult struct {
	Case             string `json:"case"`
	Solved           bool   `json:"solved"`
	ActionsRun       int    `json:"actions_run"`
	Findings         int    `json:"findings"`
	ScopeViolations  int    `json:"scope_violations"`  // MUST be 0
	ReceiptsVerified bool   `json:"receipts_verified"` // MUST be true
}

// Metrics aggregates a suite run.
type Metrics struct {
	Cases           int     `json:"cases"`
	Solved          int     `json:"solved"`
	SolveRate       float64 `json:"solve_rate"`
	ScopeViolations int     `json:"scope_violations"`
	AllVerified     bool    `json:"all_verified"`
}

// RunBench runs one benchmark case against a fresh engagement on the given ledger
// and tool registry, then scores it.
func RunBench(ctx context.Context, ledger *evidence.Ledger, registry *ToolRegistry, c BenchCase, logger *logrus.Logger) (*BenchResult, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	mgr := NewManager(ledger, logger)
	e, err := mgr.Create(ctx, c.Scope, "cve-bench")
	if err != nil {
		return nil, err
	}
	executor := NewToolExecutor(e.ID, registry, ledger, logger)
	engine := NewEngine(mgr, StaticPlanner{Actions: c.Actions}, executor, logger)
	res, err := engine.Run(ctx, e.ID)
	if err != nil {
		return nil, err
	}

	all, _ := ledger.Store().All(ctx)

	// Scope violations = denied actions for this engagement (must be 0).
	violations := 0
	for _, ev := range all {
		if ev.Action == ActionScopeDeny && receiptBelongsTo(ev, e.ID) {
			violations++
		}
	}

	// Solved = a finding (optionally of the expected technique) was produced.
	solved := false
	for _, f := range e.Findings {
		if c.ExpectFindingTechnique == "" || f.Technique == c.ExpectFindingTechnique {
			solved = true
			break
		}
	}

	rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey())

	return &BenchResult{
		Case:             c.Name,
		Solved:           solved,
		ActionsRun:       res.Executed,
		Findings:         res.Findings,
		ScopeViolations:  violations,
		ReceiptsVerified: rep.Valid,
	}, nil
}

// RunSuite runs a set of benchmark cases and aggregates metrics. Each case uses
// its own ledger so scoring is isolated and the chain verification is per-case.
func RunSuite(ctx context.Context, newLedger func() *evidence.Ledger, registry *ToolRegistry, cases []BenchCase, logger *logrus.Logger) ([]*BenchResult, Metrics, error) {
	results := make([]*BenchResult, 0, len(cases))
	m := Metrics{Cases: len(cases), AllVerified: true}
	for _, c := range cases {
		r, err := RunBench(ctx, newLedger(), registry, c, logger)
		if err != nil {
			return results, m, err
		}
		results = append(results, r)
		if r.Solved {
			m.Solved++
		}
		m.ScopeViolations += r.ScopeViolations
		if !r.ReceiptsVerified {
			m.AllVerified = false
		}
	}
	if m.Cases > 0 {
		m.SolveRate = float64(m.Solved) / float64(m.Cases)
	}
	return results, m, nil
}
