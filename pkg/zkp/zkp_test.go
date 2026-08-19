package zkp_test

import (
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/zkp"
)

func TestZKP_Version(t *testing.T) {
	if zkp.Version == "" {
		t.Error("Expected non-empty ZKP version")
	}
}

func TestZKP_StatementConstants(t *testing.T) {
	// These re-exported statement constants must be usable.
	if zkp.StmtCompletePredicate == zkp.StmtScopeCompliance {
		t.Error("Expected distinct statement constants")
	}
}

func TestGroth16Prover_TypeAlias(t *testing.T) {
	// Groth16Prover is a struct type alias re-exported from evidence/zk.
	// Ensure a zero value can be instantiated (compile-time guarantee).
	var prover zkp.Groth16Prover
	_ = prover
}

func TestLeafWitness_TypeAlias(t *testing.T) {
	// LeafWitness is re-exported; ensure the type alias resolves.
	var ws []zkp.LeafWitness
	if ws != nil {
		t.Error("Expected nil slice initially")
	}
}
