package scanners

// perf_bench_test.go benchmarks scanner paths that produce real cryptographic receipts.
//
// Note: ParseSARIF is a JSON parser (no crypto). The evidence-native Consensus engine
// produces weighted-aggregation outcomes + Ed25519-signed receipts; we benchmark both
// the aggregation logic AND the signing step.

import (
	"testing"
)

const sampleSARIF = `{
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/v2.1.0/sarif-schema-2.1.0.json",
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "gosec", "semanticVersion": "v0.1.0"}},
    "results": [
      {"ruleId": "G101", "level": "error", "message": {"text": "Potential hardcoded credentials"}},
      {"ruleId": "G401", "level": "warning", "message": {"text": "Weak cryptography"}}
    ]
  }]
}`

// BenchmarkParseSARIF measures SARIF v2.1.0 parsing performance over a small fixed report.
func BenchmarkParseSARIF(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseSARIF([]byte(sampleSARIF)); err != nil {
			b.Fatalf("parse: %v", err)
		}
	}
}

// BenchmarkEvidenceConsensusSingle scans multiple findings from different scanners,
// computes weighted scores using reliability x confidence weighting, then seals the
// result in an Ed25519-signed receipt. This is our closest analog to "multi-tool SBOM
// consensus scoring" plus cryptographic attestation.
func BenchmarkEvidenceConsensusSingle(b *testing.B) {
	eng := NewEvidenceScannerEngine()
	eng.SetScannerWeight("scanner-a", 0.8)
	eng.SetScannerWeight("scanner-b", 0.6)

	f1 := EvidenceScannerFinding{ScannerID: "scanner-a", FindingType: "hardcoded-secrets", Confidence: 0.9, RawSeverity: 8.0}
	f2 := EvidenceScannerFinding{ScannerID: "scanner-a", FindingType: "weak-crypto", Confidence: 0.7, RawSeverity: 5.0}
	f3 := EvidenceScannerFinding{ScannerID: "scanner-b", FindingType: "hardcoded-secrets", Confidence: 0.75, RawSeverity: 7.5}
	f4 := EvidenceScannerFinding{ScannerID: "scanner-b", FindingType: "weak-crypto", Confidence: 0.6, RawSeverity: 4.5}

	eng.AddFinding(f1)
	eng.AddFinding(f2)
	eng.AddFinding(f3)
	eng.AddFinding(f4)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := eng.ComputeConsensus(20)
		if err != nil {
			b.Fatalf("consensus: %v", err)
		}
		if res.Receipt == nil || !res.Receipt.Verify() {
			b.Fatal("receipt must verify")
		}
	}
}

// BenchmarkEvidenceConsensusLarge scales up the number of findings and scanners to
// measure how weighted consensus performs under larger inputs (more types & records).
func BenchmarkEvidenceConsensusLarge(b *testing.B) {
	eng := NewEvidenceScannerEngine()
	eng.SetScannerWeight("scan-1", 0.9)
	eng.SetScannerWeight("scan-2", 0.7)
	eng.SetScannerWeight("scan-3", 0.5)

	findings := []EvidenceScannerFinding{}
	types := []string{"hardcoded-secrets", "weak-crypto", "insecure-mux", "exposed-traces"}
	scanners := []string{"scan-1", "scan-2", "scan-3"}
	baseSeverity := 5.0

	for t := 0; t < len(types); t++ {
		for s := 0; s < len(scanners); s++ {
			confidence := 0.8 - float64(t+s)*0.05
			if confidence < 0.5 {
				confidence = 0.5
			}
			findings = append(findings, EvidenceScannerFinding{
				ScannerID: scanners[s], FindingType: types[t],
				Confidence: confidence, RawSeverity: baseSeverity + float64(t),
			})
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := eng.ComputeConsensus(len(findings))
		if err != nil {
			b.Fatalf("consensus: %v", err)
		}
	}
}
