package security

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// TestSigstore_VerifySignature_Valid verifies that real ECDSA-P256
// signature verification works correctly when a signature is generated over the digest.
func TestSigstore_VerifySignature_Valid(t *testing.T) {
	digest := "sha256:abcdef1234567890"

	signature, publicKey, _, err := signDigestECDSA(digest)
	if err != nil {
		t.Fatalf("failed to generate signature: %v", err)
	}

	sig := &ImageSignature{
		ID:        "test-sig-001",
		ImageRef:  "ghcr.io/cloudai-fusion/test:v1",
		Digest:    digest,
		Signature: signature,
		PublicKey: publicKey,
		SignedBy:  "ci@cloudai.io",
	}

	status, verr := VerifySignature(sig)
	if verr != nil {
		t.Errorf("unexpected error from VerifySignature: %v", verr)
	}
	if status != SignatureVerified {
		t.Errorf("expected SignatureVerified, got %q", status)
	}
}

// TestSigstore_VerifySignature_Tampered verifies that signature verification fails when
// the signature was generated for a different digest than the one being verified.
func TestSigstore_VerifySignature_Tampered(t *testing.T) {
	originalDigest := "sha256:origin123456789"
	tamperedDigest := "sha256:tamper123456789abc"

	signature, publicKey, _, err := signDigestECDSA(originalDigest)
	if err != nil {
		t.Fatalf("failed to generate signature: %v", err)
	}

	// Create a signature record but verify against a different digest.
	sig := &ImageSignature{
		ID:        "test-sig-tampered",
		ImageRef:  "ghcr.io/cloudai-fusion/tampered:v1",
		Digest:    tamperedDigest, // Tampering: digest doesn't match original signature
		Signature: signature,
		PublicKey: publicKey,
		SignedBy:  "ci@cloudai.io",
	}

	status, verr := VerifySignature(sig)
	if verr != nil {
		t.Errorf("unexpected error from VerifySignature: %v", verr)
	}
	if status != SignatureFailed {
		t.Errorf("expected SignatureFailed for tampered digest, got %q", status)
	}
}

// TestSigstore_VerifySignature_MissingMaterial verifies that verification returns
// unverified when the signature lacks either the public key or signature payload.
func TestSigstore_VerifySignature_MissingMaterial(t *testing.T) {
	testCases := []struct {
		name          string
		imageSig      *ImageSignature
		expectedStatus SignatureVerifyStatus
	}{
		{
			name: "empty_public_key_empty_signature",
			imageSig: &ImageSignature{
				ID:     "test-sig-empty",
				ImageRef: "ghcr.io/cloudai-fusion/empty:v1",
				Digest:   "sha256:empty123456789",
			},
			expectedStatus: SignatureUnverified,
		},
		{
			name: "missing_public_key",
			imageSig: &ImageSignature{
				ID:        "test-sig-nopubkey",
				ImageRef:  "ghcr.io/cloudai-fusion/nopubkey:v1",
				Digest:    "sha256:nopubkey123456",
				Signature: "c29tZWJhc2U2NHNpZw==", // Some base64 garbage
			},
			expectedStatus: SignatureUnverified,
		},
		{
			name: "missing_signature",
			imageSig: &ImageSignature{
				ID:        "test-sig-nosig",
				ImageRef:  "ghcr.io/cloudai-fusion/nosig:v1",
				Digest:    "sha256:nosig123456789",
				PublicKey: "invalid-pem-data",
			},
			expectedStatus: SignatureUnverified,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status, verr := VerifySignature(tc.imageSig)
			if verr != nil {
				t.Errorf("unexpected error for %s: %v", tc.name, verr)
			}
			if status != tc.expectedStatus {
				t.Errorf("expected %s for %s, got %q", tc.expectedStatus, tc.name, status)
			}
		})
	}
}

// TestSigstore_VerifySignature_InvalidPEM verifies that invalid PEM formatting in the
// public key results in a failed verification rather than an unverified state.
func TestSigstore_VerifySignature_InvalidPEM(t *testing.T) {
	signature, _, _, err := signDigestECDSA("sha256:invalidpem12345")
	if err != nil {
		t.Fatalf("failed to generate signature: %v", err)
	}

	sig := &ImageSignature{
		ID:        "test-sig-invalid-pem",
		ImageRef:  "ghcr.io/cloudai-fusion/invalidpem:v1",
		Digest:    "sha256:invalidpem12345",
		Signature: signature,
		PublicKey: "this-is-not-valid-pem-data-at-all",
	}

	status, verr := VerifySignature(sig)
	if verr == nil {
		t.Error("expected error from VerifySignature with invalid PEM, got nil")
	}
	if status != SignatureFailed {
		t.Errorf("expected SignatureFailed for invalid PEM, got %q", status)
	}
}

// TestSigstore_VerifyImage_RealCryptoVsMock implements an honest downgrade test showing that
// the new crypto path reports real vs simulated backend correctly.
func TestSigstore_VerifyImage_RealCryptoVsMock(t *testing.T) {
	ctx := context.Background()

	// Scenario A: Real cryptographic verification with proper ECDSA material.
	t.Run("RealCryptoWithMaterial", func(t *testing.T) {
		mgr := NewSupplyChainManager(SupplyChainConfig{})
		
		// Generate a legitimate signature.
		digest := "sha256:realcrypto123456"
		signature, publicKey, _, err := signDigestECDSA(digest)
		if err != nil {
			t.Fatalf("failed to generate signature: %v", err)
		}

		mgr.RecordSignature(&ImageSignature{
			ID:        "real-crypto-sig",
			ImageRef:  "ghcr.io/cloudai-fusion/app-realcrypto:v1",
			Digest:    digest,
			Signature: signature,
			PublicKey: publicKey,
			SignedBy:  "ci@cloudai.io",
		})

		// Generate SBOM to avoid SBOM requirement block.
		mgr.GenerateSBOM("ghcr.io/cloudai-fusion/app-realcrypto:v1", digest)

		result, err := mgr.VerifyImage(ctx, 
			"ghcr.io/cloudai-fusion/app-realcrypto:v1", 
			digest, 
			"production")
		if err != nil {
			t.Fatalf("VerifyImage failed: %v", err)
		}

		if !result.Allowed {
			t.Errorf("expected image to be allowed, got Allowed=false, Reason=%q", result.Reason)
		}

		// Find the signature check result.
		foundSignCheck := false
		for i := range result.Checks {
			check := result.Checks[i]
			if check.Name == "signature" {
				foundSignCheck = true
				if check.Status != "pass" {
					t.Errorf("expected signature check status 'pass', got %q", check.Status)
				}
				if check.Detail != "ECDSA-P256 signature cryptographically verified" {
					t.Errorf("unexpected detail: %q", check.Detail)
				}
			}
		}
		if !foundSignCheck {
			t.Error("signature check not found in result checks")
		}
	})

	// Scenario B: Honest downgrade - signature recorded without ECDSA material (mock).
	t.Run("MockWithoutMaterial", func(t *testing.T) {
		mgr := NewSupplyChainManager(SupplyChainConfig{})
		
		// Record a signature WITHOUT ECDSA material (the old mock style).
		digest := "sha256:mocksigner1234567"
		mgr.RecordSignature(&ImageSignature{
			ID:       "mock-signature-only",
			ImageRef: "ghcr.io/cloudai-fusion/app-mock:v1",
			Digest:   digest,
			SignedBy: "ci@cloudai.io",
			// Note: no PublicKey, no Signature fields — this simulates legacy behavior
		})

		result, err := mgr.VerifyImage(ctx, 
			"ghcr.io/cloudai-fusion/app-mock:v1", 
			digest, 
			"production")
		if err != nil {
			t.Fatalf("VerifyImage failed: %v", err)
		}

		// In enforce mode with missing material, we should NOT allow the image.
		if result.Allowed {
			t.Errorf("expected image to be blocked due to unverified signature (enforce mode), got Allowed=true")
		}

		// Verify it's explicitly marked as unverified.
		foundSignCheck := false
		for i := range result.Checks {
			check := result.Checks[i]
			if check.Name == "signature" {
				foundSignCheck = true
				if check.Status != "unverified" {
					t.Errorf("expected signature check status 'unverified', got %q", check.Status)
				}
			}
		}
		if !foundSignCheck {
			t.Error("signature check not found in result checks")
		}
	})
}

// TestSigstore_CapabilityReporting verifies that capability.Report is called
// exactly once per manager with real vs simulated mode depending on whether
// any signatures contain ECDSA material.
func TestSigstore_CapabilityReporting(t *testing.T) {
	// Reset the global registry before each test.
	capability.Reset()

	// Case A: Had material → report real.
	t.Run("ReportRealWhenMaterialPresent", func(t *testing.T) {
		mgr := NewSupplyChainManager(SupplyChainConfig{})
		
		digest := "sha256:hadmaterial12345"
		signature, publicKey, _, err := signDigestECDSA(digest)
		if err != nil {
			t.Fatalf("failed to generate signature: %v", err)
		}

		mgr.RecordSignature(&ImageSignature{
			ID:        "cap-report-sig",
			ImageRef:  "ghcr.io/cloudai-fusion/capreport:v1",
			Digest:    digest,
			Signature: signature,
			PublicKey: publicKey,
			SignedBy:  "ci@cloudai.io",
		})

		// Trigger verification to cause capability reporting.
		_, err = mgr.VerifyImage(context.Background(),
			"ghcr.io/cloudai-fusion/capreport:v1",
			digest,
			"production")
		if err != nil {
			t.Fatalf("VerifyImage failed: %v", err)
		}

		snapshot := capability.Snapshot()
		var sigRecord bool
		for _, b := range snapshot {
			if b.Component == "security.supply_chain.signature" {
				sigRecord = true
				if b.Mode != capability.ModeReal {
					t.Errorf("expected ModeReal, got %q", b.Mode)
				}
				if b.Driver != "crypto/ecdsa+P-256" {
					t.Errorf("expected driver 'crypto/ecdsa+P-256', got %q", b.Driver)
				}
			}
		}
		if !sigRecord {
			t.Error("capability report for security.supply_chain.signature not found")
		}
	})

	// Case B: No material → report simulated.
	t.Run("ReportSimulatedWhenNoMaterial", func(t *testing.T) {
		mgr := NewSupplyChainManager(SupplyChainConfig{})
		
		digest := "sha256:nomaterial12345678"
		mgr.RecordSignature(&ImageSignature{
			ID:        "nomaterial-sig",
			ImageRef:  "ghcr.io/cloudai-fusion/nomaterial:v1",
			Digest:    digest,
			SignedBy:  "ci@cloudai.io",
			PublicKey: "", // Missing
			Signature: "", // Missing
		})

		// Trigger verification to cause capability reporting.
		_, err := mgr.VerifyImage(context.Background(),
			"ghcr.io/cloudai-fusion/nomaterial:v1",
			digest,
			"staging") // staging uses warn policy
		if err != nil {
			t.Fatalf("VerifyImage failed: %v", err)
		}

		snapshot := capability.Snapshot()
		var sigRecord bool
		for _, b := range snapshot {
			if b.Component == "security.supply_chain.signature" {
				sigRecord = true
				if b.Mode != capability.ModeSimulated {
					t.Errorf("expected ModeSimulated when no material present, got %q", b.Mode)
				}
			}
		}
		if !sigRecord {
			t.Error("capability report for security.supply_chain.signature not found")
		}
	})
}

// BenchmarkSigstore_Verification measures the performance of ECDSA-P256 verification
// across many iterations. This benchmark provides realistic crypto costs for M33.
func BenchmarkSigstore_Verification(b *testing.B) {
	digest := "sha256:benchmark123456789"
	signature, publicKey, _, err := signDigestECDSA(digest)
	if err != nil {
		b.Fatalf("failed to generate signature: %v", err)
	}

	ctx := context.Background()
	mgr := NewSupplyChainManager(SupplyChainConfig{})
	mgr.RecordSignature(&ImageSignature{
		ID:        "bench-sig",
		ImageRef:  "ghcr.io/cloudai-fusion/bench:v1",
		Digest:    digest,
		Signature: signature,
		PublicKey: publicKey,
		SignedBy:  "ci@cloudai.io",
	})
	// Production policy requires BOTH a verified signature and an SBOM; record
	// the SBOM keyed by the same digest so the admission decision is "allow".
	mgr.GenerateSBOM("ghcr.io/cloudai-fusion/bench:v1", digest)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := mgr.VerifyImage(ctx, "ghcr.io/cloudai-fusion/bench:v1", digest, "production")
		if err != nil {
			b.Fatalf("verify: %v", err)
		}
		if !result.Allowed {
			b.Fatal("unexpected deny")
		}
	}
}
