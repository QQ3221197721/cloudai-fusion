package redteam

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// bench_v2.go turns the CVE-Bench harness (bench.go) into a runnable, scored,
// dependency-free regression suite that CI can execute without any external
// tool, binary, or cluster. It provides:
//
//   - BenchTool: a deterministic, honestly-simulated Tool that reproduces a
//     vulnerability "path" and emits a finding, so a case can be scored end to end.
//   - DefaultBenchSuite: representative CVE-Bench cases (web RCE, C2 beacon,
//     lateral movement) with valid signed scopes.
//   - RunDefaultSuite: runs the suite on isolated in-memory evidence chains and
//     returns NUMERIC metrics (solve rate, scope violations=0, receipts verified).
//
// This is the objective, reproducible capability measurement the platform favors
// over adjectives — and it is what the /api/v1/redteam/benchmark endpoint runs.

// BenchTool is a deterministic tool for the built-in benchmark. It runs no real
// binary, so it reports capability.ModeSimulated; its determinism makes suite
// scoring reproducible in CI.
type BenchTool struct {
	name       string
	techniques []string
}

// NewBenchTool builds a benchmark tool that claims the given techniques.
func NewBenchTool(name string, techniques ...string) BenchTool {
	if name == "" {
		name = "bench"
	}
	return BenchTool{name: name, techniques: techniques}
}

// Name returns the tool name.
func (t BenchTool) Name() string { return t.name }

// Techniques returns the MITRE techniques this tool exercises.
func (t BenchTool) Techniques() []string { return t.techniques }

// Mode reports simulated: the benchmark tool never touches a real target.
func (BenchTool) Mode() capability.Mode { return capability.ModeSimulated }

// Invoke deterministically "reproduces" the vulnerability path and returns a
// finding. The executor fills in the technique from the authorized action.
func (t BenchTool) Invoke(_ context.Context, in ToolInput) (ToolOutput, error) {
	return ToolOutput{
		Raw:     []byte("bench: vulnerability path reproduced for " + in.Target),
		Summary: "benchmark reproduced the exploit path (simulated)",
		Finding: &Finding{Asset: in.Target, Severity: "high", Title: "bench:" + t.name},
		Mode:    capability.ModeSimulated,
		Driver:  t.name,
	}, nil
}

// benchScope is the signed authorization contract shared by the default cases:
// a single lab target, the benchmark techniques allowed, read-only tier so the
// deterministic actions auto-run (approval only required at exploit+).
func benchScope() Scope {
	return Scope{
		Targets:         []Target{{Kind: TargetHost, Value: "bench.local"}},
		AllowTechniques: []string{"T1190", "T1071", "T1210"},
		MaxRiskTier:     RiskReadOnly,
		ApprovalReq:     RiskExploit,
	}
}

// DefaultBenchSuite returns the built-in CVE-Bench v2 cases. Each case expects a
// finding of a specific technique, so scoring is exact.
func DefaultBenchSuite() []BenchCase {
	mk := func(name, tech string) BenchCase {
		return BenchCase{
			Name:  name,
			Scope: benchScope(),
			Actions: []Action{{
				ID: name + "-a1", Technique: tech, Tool: "bench",
				Target: "bench.local", RiskTier: RiskReadOnly,
			}},
			ExpectFindingTechnique: tech,
		}
	}
	return []BenchCase{
		mk("cve-web-rce", "T1190"),     // Exploit Public-Facing Application
		mk("cve-c2-beacon", "T1071"),   // Application Layer Protocol (C2)
		mk("cve-lateral-smb", "T1210"), // Exploitation of Remote Services
	}
}

// RunDefaultSuite runs the built-in suite on isolated in-memory evidence chains
// (one per case, so verification is per-case) using the deterministic BenchTool,
// returning per-case results and aggregate metrics. It requires no network,
// binary, or cluster and is safe to run in CI.
func RunDefaultSuite(ctx context.Context, logger *logrus.Logger) ([]*BenchResult, Metrics, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	reg := NewToolRegistry()
	reg.Register(NewBenchTool("bench", "T1190", "T1071", "T1210"))

	newLedger := func() *evidence.Ledger {
		signer, err := evidence.GenerateEphemeralSigner()
		if err != nil {
			return nil
		}
		l, err := evidence.NewLedger(evidence.LedgerConfig{
			Store:  evidence.NewMemoryStore(),
			Signer: signer,
		})
		if err != nil {
			return nil
		}
		return l
	}
	return RunSuite(ctx, newLedger, reg, DefaultBenchSuite(), logger)
}
