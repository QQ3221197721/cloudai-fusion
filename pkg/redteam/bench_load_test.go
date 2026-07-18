package redteam

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func TestLoadBenchCases_AndRegressionGate(t *testing.T) {
	t.Cleanup(capability.Reset)
	dir := t.TempDir()
	caseJSON := `{
      "scope": {
        "targets": [{"kind":"host","value":"lab.local"}],
        "allow_techniques": ["T1595"],
        "max_risk_tier": 0,
        "approval_required_at": 1
      },
      "actions": [
        {"id":"b1","technique":"T1595","tool":"fake","target":"lab.local","risk_tier":0}
      ],
      "expect_finding_technique": "T1595"
    }`
	if err := os.WriteFile(filepath.Join(dir, "web-cve.json"), []byte(caseJSON), 0o600); err != nil {
		t.Fatalf("write case: %v", err)
	}

	cases, err := LoadBenchCases(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cases) != 1 || cases[0].Name != "web-cve" || cases[0].ExpectFindingTechnique != "T1595" {
		t.Fatalf("unexpected loaded case: %+v", cases)
	}

	// Regression gate: run the suite and enforce the acceptance invariants.
	reg := NewToolRegistry()
	reg.Register(fakeTool{})
	newL := func() *evidence.Ledger { return newLedger(t) }
	results, m, err := RunSuite(context.Background(), newL, reg, cases, nil)
	if err != nil {
		t.Fatalf("suite: %v", err)
	}
	if !results[0].Solved {
		t.Fatalf("loaded case must be solved: %+v", results[0])
	}
	// The CI gate: zero scope violations, 100%% receipts verified, full solve.
	if m.ScopeViolations != 0 || !m.AllVerified || m.SolveRate != 1.0 {
		t.Fatalf("regression gate failed: %+v", m)
	}
}
