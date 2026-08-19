package devsecops

// perf_bench_test.go measures DevSecOps policy gates for Modules 33-36. These
// evaluate real policy logic (thresholds, severity filters) and produce signed
// evidence.Receipts. There is no external binary invocation here; findings are
// in-memory structures.

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// BenchmarkSASTGate evaluates SAST gate over N synthetic SAST findings with a
// critical-threshold and known-vulnerability filter.
func BenchmarkSASTGate(b *testing.B) {
	gate := &SASTGate{CriticalThreshold: 1, KnownVulnCount: 1}
	findings := make([]Finding, 0, 100)
	for i := 0; i < 100; i++ {
		findings = append(findings, Finding{ID: "bug-" + itos(i), Type: "SAST", Severity: SeverityLow})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gate.Evaluate(findings)
	}
}

// BenchmarkDASTGate evaluates DAST gate over endpoint findings.
func BenchmarkDASTGate(b *testing.B) {
	gate := &DASTGate{MaxHighEndpoints: 2}
	findings := make([]Finding, 0, 50)
	for i := 0; i < 50; i++ {
		findings = append(findings, Finding{Type: "DAST", Severity: SeverityLow, Location: "/api/v1"})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gate.Evaluate(findings)
	}
}

// BenchmarkDependencyGate evaluates CVE-based dependency gate with a severity
// allowlist. Only non-tolerated severities count against MaxCVEs.
func BenchmarkDependencyGate(b *testing.B) {
	gate := &DependencyGate{MaxCVEs: 3, AllowedSeverities: []string{"low", "medium"}}
	findings := make([]Finding, 0, 100)
	for i := 0; i < 100; i++ {
		sev := SeverityLow
		if i%7 == 0 {
			sev = SeverityHigh
		} else if i%13 == 0 {
			sev = SeverityCritical
		}
		findings = append(findings, Finding{Type: "Dependency", Severity: sev})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gate.Evaluate(findings)
	}
}

// BenchmarkSupplyChainVerifier runs signature+SBOM checks over multiple artifacts.
// It uses keylessVerifier which only checks digest present + signatures exist.
func BenchmarkSupplyChainVerifier(b *testing.B) {
	v := &SupplyChainVerifier{Required: true, Artifacts: []Artifact{
		{Name: "app", Digest: "sha256:abc", Signatures: []string{"sig"}, SBOM: &SBOM{Format: "cyclonedx", Components: []SBOMComponent{{Name: "gin", Version: "1.9.1"}}}},
		{Name: "lib", Digest: "sha256:def", Signatures: []string{"sig"}, SBOM: &SBOM{Format: "spdx", Components: []SBOMComponent{{Name: "logrus", Version: "1.9.3"}}}},
	}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if res := v.Evaluate(nil); res != GatePass {
			b.Fatalf("expected pass, got %s", res)
		}
	}
}

// BenchmarkEvidenceSecurityGate evaluates a single critical-count gate that also
// builds a signed receipt AND invokes auto-remediation suggestions.
func BenchmarkEvidenceSecurityGate(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	gate := NewEvidenceSecurityGate(priv, 0) // blocks on 1 critical
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		findings := []EvidenceFinding{{Severity: "critical", Package: "lodash", Category: "cve"}}
		res, err := gate.EvaluateGate("artifact-1", findings)
		if err != nil {
			b.Fatalf("gate: %v", err)
		}
		if res.Receipt == nil || !res.Receipt.Verify() {
			b.Fatal("receipt must verify")
		}
		if len(res.Suggestions) == 0 {
			b.Fatal("must have suggestions")
		}
	}
}

// BenchmarkAutoRemediation looks up CVE-fix mappings by name and substring match.
func BenchmarkAutoRemediation(b *testing.B) {
	eng := NewAutoRemediationEngine()
	pkgs := []string{"lodash", "lodash.merge", "jackson-databind", "log4j-core", "openssl"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, pkg := range pkgs {
			eng.lookup(pkg)
		}
	}
}

func itos(i int) string {
	s := "0000"
	d := i % 10000
	for d > 0 {
		s = string(rune('0'+d%10)) + s[1:]
		d /= 10
	}
	return s
}
