package zk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateFakeWitnesses builds n in-scope confidential witnesses for the "demo"
// namespace, mirroring what `cafctl zk-demo generate` constructs. Each witness is a
// semantic projection of a sealed evidence receipt: same scope, a monotonic index,
// in-scope=true, and a SHA-256 content hash reduced into the BN254 scalar field.
func generateFakeWitnesses(n int) []LeafWitness {
	nsFE := FieldFromBytes([]byte("demo"))
	ws := make([]LeafWitness, n)
	for i := range ws {
		h := sha256.Sum256([]byte(fmt.Sprintf("witness-%d", i)))
		ws[i] = LeafWitness{
			Namespace:   nsFE,
			Eidx:        uint64(i),
			InScope:     true,
			PayloadHash: FieldFromBytes(h[:]),
		}
	}
	return ws
}

// publicWitnessFor reconstructs the public-only witness for an attestation, exactly
// as VerifyZK does internally. It is used by the test to time the raw
// groth16.Verify call separately from JSON/vk deserialization.
func publicWitnessFor(t *testing.T, att *ZKAttestation) witness.Witness {
	t.Helper()
	root, err := feFromHex(att.PublicRoot)
	require.NoError(t, err, "decode public_root")
	scope, err := feFromHex(att.ScopeCommit)
	require.NoError(t, err, "decode scope_commit")

	pub := newCircuit(att.Count)
	pub.Root = root
	pub.ScopeCommit = scope
	for i := 0; i < att.Count; i++ {
		var zero fr.Element
		pub.Namespace[i] = zero
		pub.Eidx[i] = zero
		pub.InScope[i] = zero
		pub.PayloadHash[i] = zero
	}
	w, err := frontend.NewWitness(pub, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err, "build public witness")
	return w
}

// TestDemoEndToEnd exercises the full zk-demo pipeline the CLI command drives:
// build witnesses -> generate a real Groth16 proof -> verify it offline -> measure
// that raw verification is fast. A green run proves the whole A1 pipeline
// (native Poseidon2 == in-circuit Poseidon2, real prover, offline verifier) works.
func TestDemoEndToEnd(t *testing.T) {
	// Step 1: construct the confidential witness set (10 demo records).
	witnesses := generateFakeWitnesses(10)
	require.Len(t, witnesses, 10)

	// Step 2: generate a real proof.
	prover := Groth16Prover{}
	att, vk, err := prover.Prove(context.Background(), StmtCompletePredicate, "all-in-scope", witnesses)
	require.NoError(t, err, "Prove must succeed for an all-in-scope witness set")
	require.NotNil(t, att, "attestation must not be nil")
	assert.NotEmpty(t, att.Proof, "attestation must carry a real proof")
	assert.NotEmpty(t, att.VKID, "attestation must pin a verifying key id")
	assert.NotEmpty(t, vk, "verifying key bytes must be returned")
	assert.Equal(t, "real", att.Mode, "prover must report real mode")
	assert.Equal(t, 10, att.Count, "member count must match witness count")

	// Step 3: verify offline against the pinned verifying key.
	err = VerifyZK(att, vk)
	require.NoError(t, err, "VerifyZK must accept a freshly generated attestation")

	// Step 4: measure raw Groth16 verification time. Deserialize the proof and vk
	// (as an auditor would), warm up once so the measurement reflects steady-state
	// verify cost, then time a single verification. Groth16 verification over BN254
	// with a handful of public inputs is genuinely sub-millisecond to low-ms.
	verifyingKey := groth16.NewVerifyingKey(ecc.BN254)
	_, err = verifyingKey.ReadFrom(bytes.NewReader(vk))
	require.NoError(t, err, "read verifying key")

	proof := groth16.NewProof(ecc.BN254)
	_, err = proof.ReadFrom(bytes.NewReader(att.Proof))
	require.NoError(t, err, "read proof")

	pubWitness := publicWitnessFor(t, att)

	// Warm up (not timed): first call primes any lazy initialization.
	require.NoError(t, groth16.Verify(proof, verifyingKey, pubWitness), "warmup verify")

	start := time.Now()
	err = groth16.Verify(proof, verifyingKey, pubWitness)
	elapsed := time.Since(start)
	require.NoError(t, err, "timed verify must pass")

	t.Logf("raw groth16.Verify elapsed: %s", elapsed)
	assert.Less(t, elapsed, 5*time.Millisecond, "offline verification must be fast (<5ms)")
}

// TestDemoTamperedProofRejected confirms the demo pipeline is sound: mutating a
// public input makes the very same proof fail verification. This is what protects
// the moat — a published attestation cannot be re-pointed at different claims.
func TestDemoTamperedProofRejected(t *testing.T) {
	witnesses := generateFakeWitnesses(10)
	att, vk, err := Groth16Prover{}.Prove(context.Background(), StmtCompletePredicate, "all-in-scope", witnesses)
	require.NoError(t, err)

	// Flip the public root to a different valid field element.
	att.PublicRoot = feHex(FieldFromBytes([]byte("tampered-root")))
	err = VerifyZK(att, vk)
	require.Error(t, err, "verification must fail after tampering with the public root")
}

// TestDemoRoundTripSerialization confirms an attestation survives a JSON round-trip
// (as written to proof.json by the CLI) and still verifies. The proof []byte is
// base64-encoded by encoding/json; this guards against silent corruption.
func TestDemoRoundTripSerialization(t *testing.T) {
	witnesses := generateFakeWitnesses(8)
	att, vk, err := Groth16Prover{}.Prove(context.Background(), StmtScopeCompliance, "", witnesses)
	require.NoError(t, err)

	data, err := json.Marshal(att)
	require.NoError(t, err)

	var restored ZKAttestation
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, att.VKID, restored.VKID)
	assert.Equal(t, att.PublicRoot, restored.PublicRoot)
	assert.Equal(t, att.Count, restored.Count)
	require.NoError(t, VerifyZK(&restored, vk), "restored attestation must still verify")
}
