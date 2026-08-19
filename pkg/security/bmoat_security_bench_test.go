package security

// bmoat_security_bench_test.go - Performance Moat Benchmarks (Module 31-36)
// Implements the user-required "optimize to surpass" strategy:
//   - ECDSA-P256 / Ed25519 verification latency and throughput
//   - Batch ECDSA verify parallelization vs sequential baseline
//   - SBOM parsing/joining throughput (real JSON unmarshal)
//   - WAF Aho-Corasick ops/s and zero-allocation path
//   - IP ACL judgment latency
//   - Supply chain policy check latency
//   - Evidence compliance Ed25519 receipt sign latency
// All benchmarks use -benchtime=5x as per user requirement.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Crypto Verification Benchmarks (ECDSA-P256, Ed25519)
// ============================================================================

func BenchmarkVerifySignature_ECDSA_P256(b *testing.B) {
	// Generate a valid ECDSA-P256 signature over a digest, once.
	digest := "sha256:benchmark-test"
	signature, publicKey, _, err := signDigestECDSA(digest)
	if err != nil {
		b.Fatalf("sign: %v", err)
	}
	validSig := &ImageSignature{
		Digest:    digest,
		Signature: signature,
		PublicKey: publicKey,
	}
	// Sanity check the fixture verifies before timing.
	if st, _ := VerifySignature(validSig); st != SignatureVerified {
		b.Fatalf("fixture must verify, got %s", st)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if st, _ := VerifySignature(validSig); st != SignatureVerified {
			b.Fatalf("verify failed: %s", st)
		}
	}
}

func BenchmarkVerifySignature_Ed25519_Receipt(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("keygen: %v", err)
	}

	msg := []byte("compliance-check-event-data")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		signature := ed25519.Sign(priv, msg)
		_ = ed25519.Verify(priv.Public().(ed25519.PublicKey), msg, signature)
	}
}

// BenchmarkBatchVerifySignatures_Sequential is the baseline for comparison.
func BenchmarkBatchVerifySignatures_Sequental(b *testing.B) {
	// Generate multiple signatures with different digests
	sigs := make([]*ImageSignature, 0, 10)
	for i := 0; i < 10; i++ {
		digest := fmt.Sprintf("sha256:digest-%d", i)
		signature, publicKey, _, _ := signDigestECDSA(digest)
		sigs = append(sigs, &ImageSignature{
			Digest:    digest,
			Signature: signature,
			PublicKey: publicKey,
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BatchVerifySignatures(sigs)
	}
}

// BenchmarkBatchVerifySignatures_Parallel measures the optimized multi-core path.
func BenchmarkBatchVerifySignatures_Parallel(b *testing.B) {
	// Same setup as sequental, but larger batch sizes to stress parallelism
	sigs := make([]*ImageSignature, 0, 50)
	for i := 0; i < 50; i++ {
		digest := fmt.Sprintf("sha256:digest-%d", i)
		signature, publicKey, _, _ := signDigestECDSA(digest)
		sigs = append(sigs, &ImageSignature{
			Digest:    digest,
			Signature: signature,
			PublicKey: publicKey,
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BatchVerifySignatures(sigs)
	}
}

// ============================================================================
// SBOM Parsing/Generation Throughput
// ============================================================================

func BenchmarkGenerateSBOM_Realistic(b *testing.B) {
	mgr := NewSupplyChainManager(SupplyChainConfig{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sbom := mgr.GenerateSBOM(fmt.Sprintf("ghcr.io/cloudai-fusion/app:v%d", i%100), fmt.Sprintf("sha256:%064x", i))
		_ = sbom.TotalPkgs
	}
}

// BenchmarkParseSBOM_JSON uses real JSON unmarshal of CycloneDX-formatted SBOM.
func BenchmarkParseSBOM_JSON(b *testing.B) {
	// Create a realistic SBOM JSON payload (CycloneDX-like)
	sbomJSON := []byte(`{
		"id": "sbom-123",
		"image_ref": "ghcr.io/cloudai-fusion/app:v1",
		"digest": "sha256:deadbeef",
		"format": "cyclonedx",
		"components": [
			{"name": "alpine", "version": "3.19", "type": "os", "ecosystem": "apk"},
			{"name": "go", "version": "1.25.0", "type": "framework", "ecosystem": "go"},
			{"name": "gin", "version": "1.9.1", "type": "library", "ecosystem": "go"}
		],
		"total_packages": 3,
		"licenses": ["MIT", "BSD-3-Clause"],
		"generated_at": "2026-08-18T00:00:00Z"
	}`)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var sbom SBOM
		if err := json.Unmarshal(sbomJSON, &sbom); err != nil {
			b.Fatalf("unmarshal: %v", err)
		}
		_ = sbom.TotalPkgs
	}
}

// BenchmarkMarshalSBOM_JSON measures JSON serialization cost.
func BenchmarkMarshalSBOM_JSON(b *testing.B) {
	sbom := &SBOM{
		ID:          "test-sbom",
		ImageRef:    "ghcr.io/cloudai-fusion/app:v1",
		Digest:      "sha256:abcdef",
		Format:      SBOMFormatCycloneDX,
		Components:  []SBOMComponent{{Name: "alpine", Version: "3.19"}},
		TotalPkgs:   1,
		Licenses:    []string{"MIT"},
		GeneratedAt: time.Now(),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(sbom)
	}
}

// ============================================================================
// WAF Aho-Corasick Benchmarks (OPS/S) + Zero-Allocation Path
// ============================================================================

func BenchmarkAhoCorasick_OpsPerSec_100Rules(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns()[:100])
	ac.Build()

	input := benchInput // defined in ahocorasick_bench_test.go

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches := ac.Search(input)
		_ = len(matches)
	}
}

// BenchmarkAhoCorasick_ZeroAlloc_VisitMatches measures the no-allocation path.
func BenchmarkAhoCorasick_ZeroAlloc_VisitMatches(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns()[:100])
	ac.Build()

	input := benchInput
	matchCount := 0

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.SearchInto(input, func(m ACMatch) {
			matchCount++
		})
	}
	_ = matchCount
}

// BenchmarkAhoCorasick_MatchAny reports detection-only speed (first-match-exit).
func BenchmarkAhoCorasick_MatchAny(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns()[:100])
	ac.Build()

	input := benchInput

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ac.MatchAny(input)
	}
}

// ============================================================================
// IP ACL Judgment Latency
// ============================================================================

func BenchmarkIPACL_Judgment_NoRules(b *testing.B) {
	acl := NewIPAccessList(nil, nil)
	ip := "10.0.0.1"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = acl.IsAllowed(ip)
	}
}

func BenchmarkIPACL_Judgment_BlocklistOnly(b *testing.B) {
	acl := NewIPAccessList(nil, []string{"192.168.1.0/24"})
	allowedIP := "10.0.0.1"
	blockedIP := "192.168.1.100"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = acl.IsAllowed(allowedIP)
		_ = acl.IsAllowed(blockedIP)
	}
}

func BenchmarkIPACL_Judgment_AllowlistOnly(b *testing.B) {
	acl := NewIPAccessList([]string{"10.0.0.0/8"}, nil)
	allowedIP := "10.0.0.1"
	notAllowedIP := "192.168.1.1"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = acl.IsAllowed(allowedIP)
		_ = acl.IsAllowed(notAllowedIP)
	}
}

// ============================================================================
// Supply Chain Policy Check Latency
// ============================================================================

func BenchmarkSupplyChainPolicyCheck(b *testing.B) {
	mgr := NewSupplyChainManager(SupplyChainConfig{})

	// Pre-populate real (cryptographically verifiable) signatures + SBOMs so the
	// production policy (RequireSignature + RequireSBOM, enforce) reaches the full
	// allow path. This measures the complete admission-decision latency, not an
	// early-exit deny.
	digests := make([]string, 100)
	for i := 0; i < 100; i++ {
		digest := fmt.Sprintf("sha256:%064x", i)
		digests[i] = digest
		imageRef := "ghcr.io/cloudai-fusion/app:v1"
		signature, publicKey, _, err := signDigestECDSA(digest)
		if err != nil {
			b.Fatalf("sign: %v", err)
		}
		mgr.RecordSignature(&ImageSignature{
			Digest:    digest,
			Signature: signature,
			PublicKey: publicKey,
			SignedBy:  "ci@cloudai.io",
		})
		mgr.GenerateSBOM(imageRef, digest)
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, _ := mgr.VerifyImage(ctx, "ghcr.io/cloudai-fusion/app:v1", digests[i%100], "production")
		if res == nil || !res.Allowed {
			b.Fatalf("expected allow, got %+v", res)
		}
	}
}

// ============================================================================
// Evidence Compliance Sign (Ed25519 Receipt)
// ============================================================================

func BenchmarkEvidenceComplianceSign_FastPath(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("keygen: %v", err)
	}

	engine := NewEvidenceComplianceEngine(priv, 0.1)
	
	// Pre-warm history (like in perf_bench_test.go)
	for j := 0; j < 50; j++ {
		ctrlID := fmt.Sprintf("CIS-%d.%d.%d", j/10, j%10, 1)
		engine.CheckAndUpdate(ctrlID, "CIS", float64(j%5), float64((j+1)%5))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctrlID := fmt.Sprintf("CIS-%d.%d.%d", (i%50)/10, (i%50)%10, 1)
		rep, err := engine.CheckAndUpdate(ctrlID, "CIS", float64(i%5), float64((i+1)%5))
		if err != nil {
			b.Fatalf("check: %v", err)
		}
		if rep.Receipt == nil || !rep.Receipt.Verify() {
			b.Fatal("receipt must verify")
		}
	}
}

// ============================================================================
// Naive vs AC Multi-Pattern Matching Comparison
// ============================================================================

func BenchmarkNaiveStringMatching_100Rules(b *testing.B) {
	pats := DefaultWAFPatterns()[:100]
	patterns := make([]string, len(pats))
	for i, p := range pats {
		patterns[i] = p.Pattern
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		found := false
		for _, p := range patterns {
			if strings.Contains(strings.ToLower(benchInput), strings.ToLower(p)) {
				found = true
				break
			}
		}
		_ = found
	}
}

// The existing BenchmarkAhoCorasick_vs_Regexp_Comparative already provides AC vs regexp.
// This additional naive baseline shows the magnitude of O(N*M) vs O(N+M+Z).
