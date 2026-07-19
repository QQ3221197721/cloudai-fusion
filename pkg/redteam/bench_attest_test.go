package redteam

import (
	"bytes"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// bench_attest_test.go proves RT-3: a suite run signs into an offline-verifiable
// capability statement, and the CI gate rejects scope violations, unverifiable
// receipts, and solve-rate regressions.

func benchSigner(t *testing.T, seed byte) evidence.Signer {
	t.Helper()
	s, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return s
}

func TestBenchAttestation_SignVerifyAndGate(t *testing.T) {
	s := benchSigner(t, 0x61)
	results := []*BenchResult{
		{Case: "a", Solved: true, ActionsRun: 3, ScopeViolations: 0, ReceiptsVerified: true},
		{Case: "b", Solved: true, ActionsRun: 5, ScopeViolations: 0, ReceiptsVerified: true},
	}
	m := Metrics{Cases: 2, Solved: 2, SolveRate: 1.0, ScopeViolations: 0, AllVerified: true}

	att, err := BuildBenchAttestation(s, "cve-bench-tier2", results, m, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if att.MeanActions != 4 {
		t.Fatalf("mean actions = %.1f, want 4", att.MeanActions)
	}
	if err := VerifyBenchAttestation(att, s.PublicKey()); err != nil {
		t.Fatalf("valid attestation must verify: %v", err)
	}
	if err := att.PassesGate(0.5); err != nil {
		t.Fatalf("a clean run must pass the gate: %v", err)
	}

	// Tamper the solve rate -> signature must fail.
	bad := *att
	bad.SolveRate = 0.99
	if err := VerifyBenchAttestation(&bad, s.PublicKey()); err == nil {
		t.Fatal("tampered solve rate MUST fail verification")
	}
	// Wrong key -> fails.
	if err := VerifyBenchAttestation(att, benchSigner(t, 0x62).PublicKey()); err == nil {
		t.Fatal("verification under the wrong key MUST fail")
	}
}

func TestBenchAttestation_GateRejectsRegressions(t *testing.T) {
	s := benchSigner(t, 0x63)

	// A scope violation is the hard invariant: it MUST fail the gate.
	viol, _ := BuildBenchAttestation(s, "suite", nil,
		Metrics{Cases: 1, Solved: 1, SolveRate: 1, ScopeViolations: 1, AllVerified: true}, nil)
	if err := viol.PassesGate(0.0); err == nil {
		t.Fatal("a scope violation MUST fail the gate")
	}
	// Unverifiable receipts MUST fail.
	unver, _ := BuildBenchAttestation(s, "suite", nil,
		Metrics{Cases: 1, Solved: 1, SolveRate: 1, ScopeViolations: 0, AllVerified: false}, nil)
	if err := unver.PassesGate(0.0); err == nil {
		t.Fatal("unverifiable receipts MUST fail the gate")
	}
	// Solve-rate regression MUST fail.
	low, _ := BuildBenchAttestation(s, "suite", nil,
		Metrics{Cases: 10, Solved: 3, SolveRate: 0.3, ScopeViolations: 0, AllVerified: true}, nil)
	if err := low.PassesGate(0.5); err == nil {
		t.Fatal("solve rate below the floor MUST fail the gate")
	}
	// A healthy run passes.
	ok, _ := BuildBenchAttestation(s, "suite", nil,
		Metrics{Cases: 10, Solved: 8, SolveRate: 0.8, ScopeViolations: 0, AllVerified: true}, nil)
	if err := ok.PassesGate(0.5); err != nil {
		t.Fatalf("a healthy run must pass the gate: %v", err)
	}
}
