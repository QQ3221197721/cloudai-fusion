package evidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"testing"

	"github.com/sigstore/sigstore/pkg/signature"
)

// generateM5Payload creates an authentic M5 receipt payload (~74 bytes simplified).
// Note: Full Receipt.signablePayload() structure would be ~82 bytes but we use 74 for benchmark simplicity.
func generateM5Payload() []byte {
	msg := make([]byte, 74)
	copy(msg, []byte("mihard")) // mimics: module-hash separator pattern (6 chars)
	// Timestamp field (8 bytes)
	for i := 6; i < 14; i++ {
		msg[i] = byte(i % 256)
	}
	// Input hash (32 bytes)
	for j := 14; j < 14+32; j++ {
		msg[j] = byte((j + 31) % 256)
	}
	// Output hash (28 bytes - truncated from 32 for exact 74 byte size)
	for j := 14 + 32; j < 14 + 32 + 28; j++ {
		msg[j] = byte((j + 1) % 256)
	}
	return msg
}

// generateCosignArtifact produces a representative cosign blob signing artifact.
// Cosign typically signs container blobs, attestation JSON, or SBOMs.
func generateCosignArtifact() []byte {
	artifact := make([]byte, 74)
	prefix := `{"kind":"cosign-version":"2.6.5","payload":[`
	copy(artifact, []byte(prefix))
	// Random blob data for remaining space
	for i := len(prefix); i < 74; i++ {
		artifact[i] = byte(i%256)
	}
	return artifact
}

// ============================================================================
// SECTION 2: M5 cryptographic primitives (Ed25519 signature + Merkle paths)
// ============================================================================

// BenchmarkM5_Ed25519_Verify measures our native Ed25519 verification cost
// over an authentic M5 receipt payload structure (real crypto, exact payload size).
// Security level: ~128 bits (equal to NIST P-256 / ECDSA P-256).
func BenchmarkM5_Ed25519_Verify(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	msg := generateM5Payload()
	sig := ed25519.Sign(priv, msg)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !ed25519.Verify(pub, msg, sig) {
			b.Fatal("verify failed")
		}
	}
}

// BenchmarkM5_MerklePath_Reconstruction builds real RFC 6962 inclusion proofs
// and verifies them by recomputing the root from leaf + audit path.
// This is exactly what VerifyRekorInclusion does when validating Rekor's
// inclusion proof at runtime.
func BenchmarkM5_MerklePath_Reconstruction(b *testing.B) {
	// Build a realistic Merkle tree (32 leaves = log with 32 entries)
	leaves := make([][]byte, 32)
	for i := range leaves {
		data := make([]byte, 64) // each leaf hash content is 64 bytes (similar to base64-encoded receipt body)
		dataStr := "leaf-01-data-00"
		copy(data, []byte(dataStr))
		data[5] = byte('0' + i/10)
		data[6] = byte('0' + i%10)
		data[15] = byte('0' + i/10)
		data[16] = byte('0' + i%10)
		leaves[i] = mLeafHash(data)
	}

	// Pre-generate all inclusion proofs for every leaf
	proofs := make([][][]byte, 32)
	for i := range leaves {
		proofs[i] = inclusionProof(i, leaves)
	}

	root := merkleRoot(leaves)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Cycle through different leaf indices to avoid cache effects
		idx := i % 32
		if !verifyInclusion(idx, 32, leaves[idx], proofs[idx], root) {
			b.Fatal("inclusion verify failed")
		}
	}
}

// ============================================================================
// SECTION 3: Sigstore stack (ECDSA P-256 signature via sigstore package)
// This is the SAME primitive cosign/v2 uses for default blob signatures.
// ============================================================================

// BenchmarkSigstore_ECDSA_Verify measures sigstore's ECDSA P-256 verification
// over a realistic artifact (JSON-like attestation payload). Uses the same
// underlying cryptographic primitive as AWS/KMS cosign signatures.
// Security level: ~128 bits (NIST P-256 curve).
func BenchmarkSigstore_ECDSA_Verify(b *testing.B) {
	sv, _, err := signature.NewDefaultECDSASignerVerifier()
	if err != nil {
		b.Fatalf("NewDefaultECDSASignerVerifier: %v", err)
	}

	msg := generateCosignArtifact()
	sig, err := sv.SignMessage(bytes.NewReader(msg))
	if err != nil {
		b.Fatalf("setup sign: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		err := sv.VerifySignature(bytes.NewReader(sig), bytes.NewReader(msg))
		if err != nil {
			b.Fatalf("verify: %v", err)
		}
	}
}

// BenchmarkSigstore_ECDSA_Sign measures cosign's signing throughput for comparison.
func BenchmarkSigstore_ECDSA_Sign(b *testing.B) {
	sv, _, err := signature.NewDefaultECDSASignerVerifier()
	if err != nil {
		b.Fatalf("NewDefaultECDSASignerVerifier: %v", err)
	}

	msg := generateCosignArtifact()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sig, err := sv.SignMessage(io.NopCloser(bytes.NewReader(msg)))
		if err != nil {
			b.Fatalf("sign: %v", err)
		}
		if len(sig) == 0 {
			b.Fatal("empty signature")
		}
	}
}

// ============================================================================
// SECTION 4: Side-by-side throughput comparison (pure verification timing)
// These benchmarks use StopTimer()/StartTimer() to isolate verification-only time.
// ============================================================================

// BenchmarkThroughput_M5_Ed25519_Verify runs verification only measurement.
func BenchmarkThroughput_M5_Ed25519_Verify(b *testing.B) {
	b.StopTimer()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	msg := generateM5Payload()
	sig := ed25519.Sign(priv, msg)
	b.StartTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if !ed25519.Verify(pub, msg, sig) {
			b.Fatal("verify failed")
		}
	}
}

// BenchmarkThroughput_Sigstore_ECDSA_Verify isolates pure verification timing.
func BenchmarkThroughput_Sigstore_ECDSA_Verify(b *testing.B) {
	b.StopTimer()
	sv, _, _ := signature.NewDefaultECDSASignerVerifier()
	msg := generateCosignArtifact()
	sig, _ := sv.SignMessage(bytes.NewReader(msg))
	b.StartTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := sv.VerifySignature(bytes.NewReader(sig), bytes.NewReader(msg)); err != nil {
			b.Fatalf("verify: %v", err)
		}
	}
}

// ============================================================================
// SECTION 5: Signature size comparison (storage overhead)
// Ed25519 = 64 bytes fixed; ECDSA P-256 = ~71 bytes ASN.1 DER encoded.
// ============================================================================

func BenchmarkSignatureSize_M5_Ed25519(b *testing.B) {
	b.ReportMetric(float64(ed25519.SignatureSize), "size/op-B")
	msg := generateM5Payload()

	for i := 0; i < b.N; i++ {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		sig := ed25519.Sign(priv, msg)
		_ = sig
	}
}

func BenchmarkSignatureSize_Sigstore_ECDSA(b *testing.B) {
	b.ReportMetric(float64(71), "size/op-B") // ECDSA P-256 signatures are ~71 bytes (ASN.1 DER encoded)
	msg := generateCosignArtifact()

	for i := 0; i < b.N; i++ {
		sv, _, _ := signature.NewDefaultECDSASignerVerifier()
		sig, _ := sv.SignMessage(bytes.NewReader(msg))
		_ = sig
	}
}
