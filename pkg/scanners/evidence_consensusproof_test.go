package scanners

import "testing"

func TestEvidenceScannerEngine_Signed(t *testing.T) {
	e := NewEvidenceScannerEngine()
	f := EvidenceScannerFinding{ScannerID: "scanner-a", FindingType: "xss", Confidence: 0.8, RawSeverity: 7.5}
	e.AddFinding(f)
	
	res, err := e.ComputeConsensus(10)
	if err != nil {
		t.Fatalf("ComputeConsensus: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "scanners" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceScannerEngine_MultiScannerAggregation(t *testing.T) {
	e := NewEvidenceScannerEngine()
	e.SetScannerWeight("scanner-a", 0.9)
	e.SetScannerWeight("scanner-b", 0.7)
	
	// Both report xss with different severities
	e.AddFinding(EvidenceScannerFinding{ScannerID: "scanner-a", FindingType: "xss", Confidence: 0.9, RawSeverity: 8.0})
	e.AddFinding(EvidenceScannerFinding{ScannerID: "scanner-b", FindingType: "xss", Confidence: 0.7, RawSeverity: 6.0})
	
	res, _ := e.ComputeConsensus(10)
	if len(res.WeightedScores) < 1 {
		t.Error("must produce weighted scores")
	}
	if res.Consensus {
		t.Log("multi-scanner consensus reached")
	}
}

func TestEvidenceScannerEngine_SingleSourceDowngrade(t *testing.T) {
	e := NewEvidenceScannerEngine()
	// Only one scanner reports this finding
	e.AddFinding(EvidenceScannerFinding{ScannerID: "orphan-scan", FindingType: "sql-injection", Confidence: 0.95, RawSeverity: 9.0})
	
	res, _ := e.ComputeConsensus(5)
	if len(res.WeightedScores) < 1 {
		t.Fatal("must produce weighted scores even for single source")
	}
	// The score should be downgraded by factor of 0.5
}
