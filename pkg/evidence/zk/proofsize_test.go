package zk

import (
	"context"
	"testing"
)

// TestProofAndVKSize records the serialized artifact sizes of a real Groth16
// attestation across several member counts. It documents an architectural
// property used in the Module 5 capability comparison (docs/performance-validation-module-5.md):
// the Groth16-BN254 proof is SUCCINCT and CONSTANT-SIZE regardless of how many
// confidential receipts N it attests over, while the verifying key grows with the
// circuit (N). Run with: go test ./pkg/evidence/zk -run TestProofAndVKSize -v
func TestProofAndVKSize(t *testing.T) {
	ctx := context.Background()
	for _, n := range []int{2, 8, 16, 32, 64} {
		att, vk, err := Groth16Prover{}.Prove(ctx, StmtScopeCompliance, "in scope", members("bench/size", n))
		if err != nil {
			t.Fatalf("prove n=%d: %v", n, err)
		}
		if err := VerifyZK(att, vk); err != nil {
			t.Fatalf("verify n=%d: %v", n, err)
		}
		t.Logf("N=%-3d proof=%d bytes  vk=%d bytes  vkid=%s...", n, len(att.Proof), len(vk), att.VKID[:12])
	}
}
