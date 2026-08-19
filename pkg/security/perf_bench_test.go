package security

// perf_bench_test.go benchmarks Module 31 compliance audit + security hardening
// and Modules 33-36 supply-chain paths.
//
// HONESTY NOTE: SupplyChainManager.GenerateSBOM produces a fixed, SIMULATED
// 4-component SBOM (it does not introspect a real image filesystem the way Syft
// does), and VerifyImage performs POLICY/metadata matching over recorded
// signatures (the `Verified` flag is set out-of-band), not cryptographic cosign
// signature verification. Those benchmarks measure the in-memory policy engine
// only. The evidence-native compliance drift path (EvidenceComplianceEngine)
// and CIS/NIST/SOC2/PCI-DSS/HIPAA report generation are real Go computation;
// the former also produces a genuine Ed25519-signed receipt per check.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
	"time"
)

// BenchmarkGenerateSBOM measures the SIMULATED SBOM generator (fixed 4-component
// CycloneDX doc + SHA256 hashing). NOT comparable to Syft real-image SBOMs.
func BenchmarkGenerateSBOM(b *testing.B) {
	mgr := NewSupplyChainManager(SupplyChainConfig{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = mgr.GenerateSBOM("ghcr.io/cloudai-fusion/app:v1", "sha256:deadbeef")
	}
}

// BenchmarkVerifyImage measures the admission policy evaluation path (trusted
// registry + signature-presence + SBOM-presence checks over in-memory state).
// This is policy matching, not cosign crypto verification.
func BenchmarkVerifyImage(b *testing.B) {
	mgr := NewSupplyChainManager(SupplyChainConfig{})
	digest := "sha256:cafef00d"
	ref := "ghcr.io/cloudai-fusion/app:v1"
	mgr.RecordSignature(&ImageSignature{
		ImageRef: ref, Digest: digest, SignedBy: "ci@cloudai.io", Verified: true,
	})
	mgr.GenerateSBOM(ref, digest)

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.VerifyImage(ctx, ref, digest, "production"); err != nil {
			b.Fatalf("verify: %v", err)
		}
	}
}

// BenchmarkComplianceCIS measures CIS Kubernetes Benchmark report generation via
// the static (no K8s client) path — deterministic check evaluation + scoring.
func BenchmarkComplianceCIS(b *testing.B) {
	eng := NewComplianceEngine()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := eng.RunCISBenchmark(ctx, "cluster-1"); err != nil {
			b.Fatalf("cis: %v", err)
		}
	}
}

// BenchmarkComplianceNIST measures NIST 800-190 report generation (static path).
func BenchmarkComplianceNIST(b *testing.B) {
	eng := NewComplianceEngine()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := eng.RunNISTChecks(ctx, "cluster-1"); err != nil {
			b.Fatalf("nist: %v", err)
		}
	}
}

// BenchmarkEvidenceComplianceCheck measures a single continuous-compliance drift
// check that ALSO seals the outcome in a real Ed25519-signed evidence.Receipt.
func BenchmarkEvidenceComplianceCheck(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("keygen: %v", err)
	}
	eng := NewEvidenceComplianceEngine(priv, 0.1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := eng.CheckAndUpdate("CIS-5.2.2", "CIS", i%5, (i+1)%5)
		if err != nil {
			b.Fatalf("check: %v", err)
		}
		if rep.Receipt == nil || !rep.Receipt.Verify() {
			b.Fatal("receipt must verify")
		}
	}
}

// ============================================================================
// Module 31 — Compliance Framework Benchmarks (SOC2, PCI-DSS, HIPAA)
// ============================================================================

// BenchmarkComplianceSOC2 measures SOC2 Type II trust service criteria report
// generation (12 checks covering CC6-CC9 control families).
func BenchmarkComplianceSOC2(b *testing.B) {
	eng := NewComplianceEngine()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := eng.RunSOC2Audit(ctx, "cluster-1"); err != nil {
			b.Fatalf("soc2: %v", err)
		}
	}
}

// BenchmarkCompliancePCIDSS measures PCI-DSS v4.0 compliance report generation
// (20 checks across 11 requirement families).
func BenchmarkCompliancePCIDSS(b *testing.B) {
	eng := NewComplianceEngine()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := eng.RunPCIDSSAudit(ctx, "cluster-1"); err != nil {
			b.Fatalf("pci-dss: %v", err)
		}
	}
}

// BenchmarkComplianceHIPAA measures HIPAA Security Rule report generation
// (17 checks covering Administrative, Physical, Technical safeguards).
func BenchmarkComplianceHIPAA(b *testing.B) {
	eng := NewComplianceEngine()
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := eng.RunHIPAAAudit(ctx, "cluster-1"); err != nil {
			b.Fatalf("hipaa: %v", err)
		}
	}
}

// ============================================================================
// Module 31 — Hardening Validation Benchmarks
// ============================================================================

// BenchmarkHardeningPSSApply measures Pod Security Standards policy application
// to a namespace (label generation + policy storage + mutex contention).
func BenchmarkHardeningPSSApply(b *testing.B) {
	cfg := HardeningConfig{
		PSS:    DefaultPSSConfig(),
		Cosign: DefaultCosignConfig(),
	}
	hm := NewHardeningManager(cfg)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ns := fmt.Sprintf("bench-ns-%d", i%100)
		if _, err := hm.ApplyPSSToNamespace(ctx, ns); err != nil {
			// exempt namespaces are expected to error; skip
			continue
		}
	}
}

// BenchmarkHardeningImageVerify measures cosign-style image signature verification
// (registry allowlist check + SHA256 digest computation + Rekor entry generation).
func BenchmarkHardeningImageVerify(b *testing.B) {
	cfg := HardeningConfig{
		PSS:    DefaultPSSConfig(),
		Cosign: DefaultCosignConfig(),
	}
	hm := NewHardeningManager(cfg)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref := fmt.Sprintf("ghcr.io/cloudai-fusion/app:v%d", i%50)
		if _, err := hm.VerifyImage(ctx, ref); err != nil {
			b.Fatalf("verify: %v", err)
		}
	}
}

// BenchmarkHardeningImageSign measures ECDSA-P256 image signature generation
// (SHA256 digest + ECDSA sign + PEM public key export).
func BenchmarkHardeningImageSign(b *testing.B) {
	cfg := HardeningConfig{
		PSS:    DefaultPSSConfig(),
		Cosign: DefaultCosignConfig(),
	}
	hm := NewHardeningManager(cfg)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref := fmt.Sprintf("ghcr.io/cloudai-fusion/app:v%d", i%50)
		if _, err := hm.SignImage(ctx, ref); err != nil {
			b.Fatalf("sign: %v", err)
		}
	}
}

// ============================================================================
// Module 31 — Threat Detection Benchmarks
// ============================================================================

// BenchmarkThreatDetection measures rule-based threat detection throughput
// (5 detection rules: brute-force, privilege-escalation, anomalous-API,
// data-exfiltration, unauthorized-access) over a realistic audit window.
func BenchmarkThreatDetection(b *testing.B) {
	td := NewThreatDetector(ThreatDetectionConfig{
		BruteForceThreshold: 5,
		BruteForceWindow:    5 * time.Minute,
		APIRateThreshold:    100,
		APIRateWindow:       1 * time.Minute,
	})

	// Pre-populate a realistic audit window with mixed events
	now := time.Now()
	entries := make([]*AuditLogEntry, 0, 150)
	for j := 0; j < 50; j++ {
		entries = append(entries, &AuditLogEntry{
			Timestamp:    now.Add(-time.Duration(j) * time.Second),
			Username:     fmt.Sprintf("user-%d", j%10),
			Action:       "login",
			Status:       "failure",
			IPAddress:    fmt.Sprintf("10.0.0.%d", j%5),
			ResourceType: "auth",
			ResourceID:   "login-endpoint",
		})
		entries = append(entries, &AuditLogEntry{
			Timestamp:    now.Add(-time.Duration(j) * time.Second),
			Username:     fmt.Sprintf("user-%d", j%10),
			Action:       "read",
			Status:       "success",
			IPAddress:    fmt.Sprintf("10.0.0.%d", j%5),
			ResourceType: "secret",
			ResourceID:   fmt.Sprintf("secret-%d", j),
		})
		entries = append(entries, &AuditLogEntry{
			Timestamp:    now.Add(-time.Duration(j) * time.Second),
			Username:     fmt.Sprintf("user-%d", j%10),
			Action:       "update",
			Status:       "success",
			IPAddress:    fmt.Sprintf("10.0.0.%d", j%5),
			ResourceType: "deployment",
			ResourceID:   fmt.Sprintf("deploy-%d", j),
		})
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Ingest a fresh batch each iteration
		for _, e := range entries {
			td.IngestAuditEntry(e)
		}
		_ = td.RunDetection(ctx)
	}
}

// BenchmarkThreatDetectionIngest measures pure audit-log ingestion throughput
// (append + window pruning) without running detection rules.
func BenchmarkThreatDetectionIngest(b *testing.B) {
	td := NewThreatDetector(ThreatDetectionConfig{})
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry := &AuditLogEntry{
			Timestamp:    now.Add(-time.Duration(i%1000) * time.Millisecond),
			Username:     fmt.Sprintf("user-%d", i%20),
			Action:       "read",
			Status:       "success",
			IPAddress:    fmt.Sprintf("10.0.0.%d", i%10),
			ResourceType: "pod",
			ResourceID:   fmt.Sprintf("pod-%d", i),
		}
		td.IngestAuditEntry(entry)
	}
}

// ============================================================================
// Module 31 — Evidence Compliance Sign (focused receipt generation)
// ============================================================================

// BenchmarkEvidenceComplianceSign measures the Ed25519 receipt signing cost
// within the continuous compliance drift path with pre-warmed history state.
func BenchmarkEvidenceComplianceSign(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("keygen: %v", err)
	}
	eng := NewEvidenceComplianceEngine(priv, 0.1)
	// Pre-warm: populate history for 50 controls
	for j := 0; j < 50; j++ {
		ctrlID := fmt.Sprintf("CIS-%d.%d.%d", j/10, j%10, 1)
		eng.CheckAndUpdate(ctrlID, "CIS", 1.0, 1.0)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctrlID := fmt.Sprintf("CIS-%d.%d.%d", (i%50)/10, (i%50)%10, 1)
		rep, err := eng.CheckAndUpdate(ctrlID, "CIS", float64(i%5), float64((i+1)%5))
		if err != nil {
			b.Fatalf("check: %v", err)
		}
		if rep.Receipt == nil {
			b.Fatal("receipt must not be nil")
		}
	}
}
