package evidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"testing"

	"github.com/sigstore/sigstore/pkg/signature"
)

// ============================================================================
// M5 vs Cosign/Sigstore Stack Head-to-Head Benchmark
// 
// Purpose: Honest, fair comparison of verification performance between:
//   - M5: Ed25519 signatures + Merkle inclusion proofs (our audit evidence chain)
//   - Cosign: ECDSA P-256 signatures via sigstore package (same primitive as cosign blob signing)
//
// Competitor Implementation Choice:
//   Using github.com/sigstore/sigstore/pkg/signature Go library rather than cosign CLI.
//   Rationale:
//     1. More precise measurement of the cryptographic primitive (ECDSA P-256)
//     2. Avoids subprocess overhead that would vary wildly with system load
//     3. Sigstore is the SAME crypto primitive used by cosign for default blob signatures
//     4. Allows proper JSON output for automated parsing (-json flag)
//     
//     Note: cosign binary verification would add massive I/O and process startup costs.
//           If cosign CLI were used, it would measure "cosign tool + crypto" not just crypto.
//           The library benchmark isolates the cryptographic verification which is fairer.
//     
//     Tradeoff: Our Merkle inclusion proofs are a feature cosign lacks by default.
//               Cosign relies on Rekor for transparency, but does not ship with
//               local offline verification of inclusion proofs without network access.
//               This benchmark measures SIGNATURE VERIFICATION, not full transparency stacks.
//
// Work Unit Normalization:
//   - Same effective payload size (~74 bytes for signature input)
//   - Same metric: operations per second, latency per op in nanoseconds
//   - count=6 median for statistical robustness
//   - -benchtime=2s ensures stable measurements
//
// Security Level: Both provide ~128-bit security (Ed25519 = NIST P-256 equivalent)
// ============================================================================

// ============================================================================
// SECTION 1: M5 Cryptographic Primitives (Ed25519 + Merkle Chain)
// ============================================================================

// BenchmarkM5_Ed25519_Verify measures our native Ed25519 verification cost
// over an authentic receipt payload structure (real crypto, exact payload size).
// This is what Verify() in pkg/evidence/receipt.go does under the hood.
func BenchmarkM5_Ed25519_Verify(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	
	// Real receipt signablePayload: moduleHashSeparator + timestamp + inputHash + partialOutputHash
	msg := make([]byte, 74)
	copy(msg, []byte("mihard")) // mimics module-hash separator pattern (6 chars)
	for i := 6; i < 14; i++ {
		msg[i] = byte(i % 256)
	}
	for j := 14; j < 46; j++ {
		msg[j] = byte((j + 31) % 256)
	}
	for j := 46; j < 74; j++ {
		msg[j] = byte((j + 1) % 256)
	}
	sig := ed25519.Sign(priv, msg)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !ed25519.Verify(pub, msg, sig) {
			b.Fatal("verify failed")
		}
	}
}

// BenchmarkM5_Ed25519_Sign measures our signing throughput for comparison.
func BenchmarkM5_Ed25519_Sign(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	
	msg := make([]byte, 74)
	copy(msg, []byte("mihard"))
	for i := 6; i < 74; i++ {
		msg[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sig := ed25519.Sign(priv, msg)
		if len(sig) != 64 {
			b.Fatal("invalid signature size")
		}
	}
}

// BenchmarkM5_MerklePath_Reconstruction builds real RFC 6962 inclusion proofs
// and verifies them by recomputing the root from leaf + audit path.
// This is exactly what VerifyRekorInclusion does when validating Rekor's
// inclusion proof at runtime — measuring our TRANSPARENCY MOAT feature.
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
// SECTION 2: Cosign/Sigstack Stack (ECDSA P-256 Signature Verification)
// ============================================================================

// generateCosignArtifact produces a representative cosign artifact payload.
// Cosign typically signs container blobs, attestation JSON, or SBOMs.
// This ~74-byte payload mirrors M5's signature input size.
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

// BenchmarkSigstore_ECDSA_Verify measures sigstore's ECDSA P-256 verification
// over a realistic artifact (JSON-like attestation payload). Uses the same
// underlying cryptographic primitive as AWS/KMS cosign signatures.
//
// This is the CORRECT competitor baseline: measuring ECDSA P-256 which is what
// cosign defaults to for blob signatures (see cosign/cmd/cosign/signblob.go).
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
// SECTION 3: Side-by-side Throughput Comparison (Pure Verification Timing)
// These benchmarks use StopTimer()/StartTimer() to isolate verification-only time.
// ============================================================================

// BenchmarkThroughput_M5_Ed25519_Verify runs verification only measurement.
func BenchmarkThroughput_M5_Ed25519_Verify(b *testing.B) {
	b.StopTimer()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	msg := make([]byte, 74)
	copy(msg, []byte("mihard"))
	for i := 6; i < 74; i++ {
		msg[i] = byte(i % 256)
	}
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
// SECTION 4: Signature Size Comparison (Storage Overhead)
// Ed25519 = 64 bytes fixed; ECDSA P-256 = ~71 bytes ASN.1 DER encoded.
// ============================================================================

func BenchmarkSignatureSize_M5_Ed25519(b *testing.B) {
	b.ReportMetric(float64(ed25519.SignatureSize), "size/op-B")
	msg := make([]byte, 74)
	copy(msg, []byte("mihard"))
	for i := 6; i < 74; i++ {
		msg[i] = byte(i % 256)
	}

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
