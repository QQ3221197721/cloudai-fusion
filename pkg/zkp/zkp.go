// Package zkp provides zero-knowledge proof primitives and APIs for CloudAI Fusion.
//
// This package re-exports the REAL, production zero-knowledge proof implementation
// that lives in pkg/evidence/zk (Groth16 over BN254 with an in-circuit Poseidon2
// commitment). Older callers imported pkg/zkp expecting a stable facade; rather than
// maintaining a second (skeleton) implementation, this package now aliases the
// battle-tested types and functions so there is a single source of truth.
package zkp

import (
	"context"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
)

// Version information for ZKP package.
const Version = "2.0.0"

// ============================================================================
// Re-exported real ZKP types from pkg/evidence/zk
// ============================================================================

// Groth16Prover is the REAL prover: it compiles the completeness circuit for the
// given member count, runs a Groth16 setup, and produces a succinct proof.
type Groth16Prover = zk.Groth16Prover

// DryRunProver is the SIMULATED prover: it computes public commitments but produces
// no cryptographic proof (labeled honestly, blocked in production by pkg/capability).
type DryRunProver = zk.DryRunProver

// Prover produces a ZKAttestation for a statement over confidential witnesses.
type Prover = zk.Prover

// Attestation is the offline-verifiable artifact carrying public inputs, the
// succinct proof, and the verifying-key id it must be checked against.
type Attestation = zk.ZKAttestation

// Statement enumerates the properties the zk layer can attest to.
type Statement = zk.Statement

// LeafWitness is a single confidential member fed into the circuit.
type LeafWitness = zk.LeafWitness

// Statement constants re-exported for convenience.
const (
	StmtScopeCompliance   = zk.StmtScopeCompliance
	StmtCompletePredicate = zk.StmtCompletePredicate
)

// ============================================================================
// Re-exported real ZKP functions
// ============================================================================

// Verify verifies an attestation against a pinned verifying key, fully offline.
func Verify(att *Attestation, vkBytes []byte) error {
	return zk.VerifyZK(att, vkBytes)
}

// ProveCompleteness runs the real Groth16 prover over the given witnesses,
// producing an attestation and its serialized verifying key.
func ProveCompleteness(ctx context.Context, predicate string, ws []LeafWitness) (*Attestation, []byte, error) {
	p := Groth16Prover{}
	return p.Prove(ctx, StmtCompletePredicate, predicate, ws)
}
