package redteam

import (
	"crypto/ed25519"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// bench_attest.go is the RT-3 well (docs/verifiable-moat-spec.md §5.2): it turns a
// CVE-Bench/Cybench suite run into a SIGNED, offline-verifiable statement of
// capability in NUMBERS - solve rate, mean actions-to-solve, scope violations
// (must be 0), and receipt verification (must be 100%). It answers the "AI hacker"
// marketing crowd with proven numbers instead of adjectives, and drives the CI
// regression gate. A ZK variant ("solve rate >= X without revealing traces") is
// deferred to M5; the signed numbers land now.

const benchAttestationDomain = "cloudai-fusion/bench-attestation/v1"

// BenchAttestation is a signed capability statement over a suite run.
type BenchAttestation struct {
	Suite           string               `json:"suite"`
	Cases           int                  `json:"cases"`
	Solved          int                  `json:"solved"`
	SolveRate       float64              `json:"solve_rate"`
	MeanActions     float64              `json:"mean_actions"`
	ScopeViolations int                  `json:"scope_violations"` // asserted 0
	AllVerified     bool                 `json:"all_verified"`     // asserted true
	Checkpoint      *evidence.Checkpoint `json:"checkpoint,omitempty"`
	CreatedAt       string               `json:"created_at"`
	KeyID           string               `json:"key_id"`
	Signature       string               `json:"signature"`
}

// BuildBenchAttestation scores a suite run into a signed attestation. cp is
// optional: when provided it anchors the attestation to a ledger state.
func BuildBenchAttestation(s evidence.Signer, suite string, results []*BenchResult, m Metrics, cp *evidence.Checkpoint) (*BenchAttestation, error) {
	if s == nil {
		return nil, fmt.Errorf("redteam: bench attestation requires a signer")
	}
	var meanActions float64
	if len(results) > 0 {
		total := 0
		for _, r := range results {
			total += r.ActionsRun
		}
		meanActions = float64(total) / float64(len(results))
	}
	att := &BenchAttestation{
		Suite:           suite,
		Cases:           m.Cases,
		Solved:          m.Solved,
		SolveRate:       m.SolveRate,
		MeanActions:     meanActions,
		ScopeViolations: m.ScopeViolations,
		AllVerified:     m.AllVerified,
		Checkpoint:      cp,
		CreatedAt:       common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
		KeyID:           s.KeyID(),
	}
	att.Signature = ""
	sig, err := evidence.SignStatement(s, benchAttestationDomain, *att)
	if err != nil {
		return nil, err
	}
	att.Signature = sig
	return att, nil
}

// VerifyBenchAttestation checks the attestation's signature against a pinned key.
func VerifyBenchAttestation(att *BenchAttestation, pub ed25519.PublicKey) error {
	if att == nil {
		return fmt.Errorf("redteam: nil bench attestation")
	}
	unsigned := *att
	unsigned.Signature = ""
	if err := evidence.VerifyStatement(pub, benchAttestationDomain, unsigned, att.Signature); err != nil {
		return fmt.Errorf("redteam: bench attestation signature: %w", err)
	}
	return nil
}

// PassesGate enforces the CI regression gate: zero scope violations, every receipt
// verified, and solve rate at or above the floor. It returns nil when the build
// should pass, else a descriptive error the CI step surfaces.
func (att *BenchAttestation) PassesGate(minSolveRate float64) error {
	if att.ScopeViolations != 0 {
		return fmt.Errorf("redteam: %d scope violation(s) (must be 0)", att.ScopeViolations)
	}
	if !att.AllVerified {
		return fmt.Errorf("redteam: not all engagement receipts verified")
	}
	if att.SolveRate < minSolveRate {
		return fmt.Errorf("redteam: solve rate %.2f below floor %.2f", att.SolveRate, minSolveRate)
	}
	return nil
}
