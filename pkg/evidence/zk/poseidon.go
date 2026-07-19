// Package zk implements Moat A, Layer A1 (see docs/verifiable-moat-spec.md):
// zkEvidence — confidentiality-preserving proofs over a sealed set of receipts.
//
// A0 (pkg/evidence/completeness.go) proves "no more, no less" RELATIVE TO A SEALED
// SET, but it must hand the verifier the plaintext member receipts. A1 closes that
// gap: it proves properties of the sealed set (scope-compliance + completeness under
// a public predicate) to an adversarial verifier WITHOUT revealing the receipts,
// using a real zk-SNARK (gnark Groth16 over BN254) whose circuit operates on a
// Poseidon2 "mirror" commitment rather than the SHA-256 RFC 6962 tree (SHA-256 is
// prohibitively expensive in-circuit — the mirror is the spec's core mitigation).
//
// Honesty (spec principle 3): the prover reports its capability mode to
// pkg/capability; a simulated/dry-run prover must never back a consequential
// attestation in production. The verifier (VerifyZK) is pure and offline — it needs
// only the attestation and a pinned verifying key, exactly like cafctl verify-*.
package zk

import (
	"encoding/hex"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/poseidon2"
)

// The native Poseidon2 hash below is the Merkle–Damgard construction over the
// default BN254 Poseidon2 permutation (width 2, 6 full + 50 partial rounds). It is
// byte-for-byte equivalent to gnark's in-circuit std/hash/poseidon2 hasher:
//   - both start from a zero initial state,
//   - both chain state = Compress(state, dᵢ) with NO length padding,
//   - both use the identical 2→1 compression Compress(l,r) = Permutation([l,r])[1] + r.
// That equivalence is what makes an offline-computed public commitment provable
// in-circuit; any divergence would simply make Groth16 verification fail.

// hashFields returns the Merkle–Damgard Poseidon2 hash of xs as a field element.
func hashFields(xs ...fr.Element) fr.Element {
	h := poseidon2.NewMerkleDamgardHasher()
	for i := range xs {
		b := xs[i].Marshal() // 32 canonical big-endian bytes == one MD block
		_, _ = h.Write(b)
	}
	var out fr.Element
	out.SetBytes(h.Sum(nil))
	return out
}

// LeafWitness is the confidential per-receipt witness the circuit reasons over. It
// is a semantic projection of a sealed receipt (never the raw payload): the scope
// it belongs to, its per-engagement index, whether it is in scope, and a hash of
// its content. Callers (e.g. redteam) derive these from evidence.Evidence records;
// the zk layer stays domain-agnostic (spec principle 6).
type LeafWitness struct {
	Namespace   fr.Element // scope identifier as a field element (e.g. hash of "redteam/engagement/<id>")
	Eidx        uint64     // per-namespace monotonic index
	InScope     bool       // the public predicate under proof (here: target ∈ scope)
	PayloadHash fr.Element // hash of the receipt content, reduced into the field
}

// FieldFromBytes maps arbitrary bytes into the BN254 scalar field (reduced mod r).
// Used to turn a namespace string or a SHA-256 content hash into a circuit input.
func FieldFromBytes(b []byte) fr.Element {
	var e fr.Element
	e.SetBytes(b)
	return e
}

// leafField is the Poseidon leaf the circuit recomputes from the private witness.
func leafField(w LeafWitness) fr.Element {
	var eidx fr.Element
	eidx.SetUint64(w.Eidx)
	var inScope fr.Element
	if w.InScope {
		inScope.SetOne()
	}
	return hashFields(w.Namespace, eidx, inScope, w.PayloadHash)
}

// scopeField binds every member to one public scope commitment.
func scopeField(ns fr.Element) fr.Element { return hashFields(ns) }

// Commitment is the public Poseidon root the proof is about: the MD-Poseidon2 hash
// over the ordered member leaves. Exported so a holder of the A0 member set can
// recompute it and bind the (SHA-256) A0 proof to this (Poseidon) A1 attestation.
func Commitment(ws []LeafWitness) fr.Element {
	leaves := make([]fr.Element, len(ws))
	for i := range ws {
		leaves[i] = leafField(ws[i])
	}
	return hashFields(leaves...)
}

// ScopeCommitment returns the public scope commitment for a namespace field.
func ScopeCommitment(ns fr.Element) fr.Element { return scopeField(ns) }

// feHex / feFromHex serialise a field element as canonical 32-byte hex.
func feHex(e fr.Element) string {
	b := e.Bytes()
	return hex.EncodeToString(b[:])
}

func feFromHex(s string) (fr.Element, error) {
	var e fr.Element
	b, err := hex.DecodeString(s)
	if err != nil {
		return e, err
	}
	e.SetBytes(b)
	return e, nil
}
