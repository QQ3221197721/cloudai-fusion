package redteam

import (
	"context"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// bench_record.go makes RT-3's signed capability numbers a first-class ledger
// citizen: recording a BenchAttestation appends it to the evidence chain, so the
// published numbers (solve rate, 0 violations, 100% verified) are tamper-evident,
// inclusion-provable, and exportable alongside the engagements they summarize - on
// top of the attestation's own detached signature. Evidence is additive.

// ActionBench tags a recorded BenchAttestation receipt.
const ActionBench = "redteam.bench"

// RecordBenchAttestation appends a signed BenchAttestation as a redteam.bench receipt.
func RecordBenchAttestation(ctx context.Context, rec evidence.Recorder, att *BenchAttestation) (*evidence.Evidence, error) {
	if rec == nil || att == nil {
		return nil, nil
	}
	return rec.Record(ctx, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionBench,
		Subject: att.Suite,
		Input:   map[string]any{"suite": att.Suite, "cases": att.Cases},
		Output: map[string]any{
			"solve_rate": att.SolveRate, "scope_violations": att.ScopeViolations, "all_verified": att.AllVerified,
		},
		Payload: att,
		Backends: []evidence.BackendFact{
			{Component: "redteam.bench", Mode: "real", Driver: "signed-attestation"},
		},
	})
}
