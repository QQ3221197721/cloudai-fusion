package zk

import (
	"github.com/consensys/gnark/frontend"
	poseidon2 "github.com/consensys/gnark/std/hash/poseidon2"
)

// completenessCircuit is the Groth16 statement behind StmtScopeCompliance +
// StmtCompletePredicate (see docs/verifiable-moat-spec.md §3.2). It proves, in zero
// knowledge over a fixed number N of members:
//
//	∃ N private leaves (Namespace, Eidx, InScope, PayloadHash) such that
//	  (1) every leaf is in scope            — InScope[i] == 1               (scope-compliance)
//	  (2) every leaf shares ONE public scope — Poseidon(Namespace[i]) == ScopeCommit
//	  (3) the leaves' Poseidon commitment    — Poseidon(leaf_0..leaf_{N-1}) == Root
//
// (3) with a public Count fixes "exactly these N, no omission / no cherry-picking"
// under the predicate; (1)+(2) prove they are all in the declared scope — WITHOUT
// revealing any receipt. The in-circuit Poseidon2 (std/hash/poseidon2) is identical
// to the native hash in poseidon.go, so the public Root/ScopeCommit are computed
// off-circuit and checked here.
type completenessCircuit struct {
	// Public inputs (known to the verifier).
	Root        frontend.Variable `gnark:",public"` // Poseidon commitment over the member leaves
	ScopeCommit frontend.Variable `gnark:",public"` // Poseidon(namespace) — which scope this attests to

	// Private witness (never revealed): one entry per member, length fixed at compile time.
	Namespace   []frontend.Variable
	Eidx        []frontend.Variable
	InScope     []frontend.Variable
	PayloadHash []frontend.Variable
}

// newCircuit allocates a circuit whose witness slices hold exactly n members. The
// constraint system (and thus the verifying key) is parametric in n; the VKID binds
// it, so a verifier always checks against the matching key.
func newCircuit(n int) *completenessCircuit {
	return &completenessCircuit{
		Namespace:   make([]frontend.Variable, n),
		Eidx:        make([]frontend.Variable, n),
		InScope:     make([]frontend.Variable, n),
		PayloadHash: make([]frontend.Variable, n),
	}
}

func (c *completenessCircuit) Define(api frontend.API) error {
	h, err := poseidon2.New(api)
	if err != nil {
		return err
	}
	n := len(c.Namespace)
	leaves := make([]frontend.Variable, n)
	for i := 0; i < n; i++ {
		// (1) scope-compliance predicate: the member is in scope.
		api.AssertIsEqual(c.InScope[i], 1)

		// (2) every member belongs to the single public scope.
		h.Reset()
		h.Write(c.Namespace[i])
		api.AssertIsEqual(h.Sum(), c.ScopeCommit)

		// leaf = Poseidon(namespace, eidx, inScope, payloadHash) — matches leafField.
		h.Reset()
		h.Write(c.Namespace[i], c.Eidx[i], c.InScope[i], c.PayloadHash[i])
		leaves[i] = h.Sum()
	}

	// (3) the leaves hash to EXACTLY the public commitment — matches Commitment.
	h.Reset()
	h.Write(leaves...)
	api.AssertIsEqual(h.Sum(), c.Root)
	return nil
}
