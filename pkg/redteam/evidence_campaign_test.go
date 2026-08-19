package redteam

import (
	"context"
	"testing"
)

func newTestCampaignExecutor(t testing.TB) *EvidenceCampaignExecutor {
	e, err := NewEvidenceCampaignExecutor(EvidenceCampaignConfig{})
	if err != nil {
		if t != nil {
			t.Fatalf("NewEvidenceCampaignExecutor: %v", err)
		}
	}
	return e
}

func TestEvidenceCampaign_ReceiptVerifies(t *testing.T) {
	e := newTestCampaignExecutor(t)
	target := Target{Kind: TargetHost, Value: "10.0.0.5"}

	res, err := e.ExecuteCampaign(context.Background(), target)
	if err != nil {
		t.Fatalf("ExecuteCampaign: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("coverage receipt must verify")
	}
	if res.Receipt.Module != "redteam.campaign" || res.Receipt.Operation != "ExecuteCampaign" {
		t.Errorf("unexpected receipt module/op: %s/%s", res.Receipt.Module, res.Receipt.Operation)
	}
	if !res.Verifiable {
		t.Error("result should be marked verifiable")
	}
	if res.ProofHash == "" {
		t.Error("proof hash must be present")
	}
}

func TestEvidenceCampaign_CoverageGuidedReachesTarget(t *testing.T) {
	e, err := NewEvidenceCampaignExecutor(EvidenceCampaignConfig{
		TargetCoverageRate: 0.9,
		MaxGenerations:     60,
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	res, err := e.ExecuteCampaign(context.Background(), Target{Kind: TargetHost, Value: "host-1"})
	if err != nil {
		t.Fatalf("ExecuteCampaign: %v", err)
	}
	// Coverage-guided evolution must drive coverage substantially above a random
	// single-shot baseline (which would touch only a handful of techniques).
	if res.Coverage.Rate < 0.5 {
		t.Errorf("coverage-guided mutation underperformed: rate=%.2f", res.Coverage.Rate)
	}
	if res.Coverage.TotalTIDs != len(DefaultMITREMatrix().Techniques) {
		t.Errorf("total TIDs mismatch: %d", res.Coverage.TotalTIDs)
	}
}

func TestEvidenceCampaign_ProofHashDeterministic(t *testing.T) {
	cov := &CampaignCoverage{CoveredTIDs: []string{"T1046", "T1595"}, TotalTIDs: 20, Rate: 0.1}
	h1 := cov.encode()
	// Reordering the covered list must not change the canonical encoding.
	cov2 := &CampaignCoverage{CoveredTIDs: []string{"T1595", "T1046"}, TotalTIDs: 20, Rate: 0.1}
	h2 := cov2.encode()
	if string(h1) != string(h2) {
		t.Error("coverage encoding must be order-independent (canonical)")
	}
}

func TestEvidenceCampaign_ReceiptBindsTarget(t *testing.T) {
	e := newTestCampaignExecutor(t)
	r1, err := e.ExecuteCampaign(context.Background(), Target{Kind: TargetHost, Value: "a"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := e.ExecuteCampaign(context.Background(), Target{Kind: TargetHost, Value: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Receipt.InputHash == r2.Receipt.InputHash {
		t.Error("distinct targets must yield distinct input hashes")
	}
}

func TestEvidenceCampaign_UncoveredTIDsShrink(t *testing.T) {
	e := newTestCampaignExecutor(t)
	covered := map[string]bool{}
	before := len(e.getUncoveredTIDs(covered))
	gen := e.seedGeneration()
	e.evaluateAndRecordCoverage(gen, covered)
	after := len(e.getUncoveredTIDs(covered))
	if after > before {
		t.Error("uncovered set must not grow after evaluation")
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkEvidenceCampaign_Execute(b *testing.B) {
	e, err := NewEvidenceCampaignExecutor(EvidenceCampaignConfig{})
	if err != nil {
		b.Fatalf("construct: %v", err)
	}
	target := Target{Kind: TargetHost, Value: "10.0.0.5"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := e.ExecuteCampaign(context.Background(), target)
		if err != nil {
			b.Fatal(err)
		}
		if !res.Receipt.Verify() {
			b.Fatal("invalid receipt")
		}
	}
}

func BenchmarkEvidenceCampaign_CoverageCalc(b *testing.B) {
	e, _ := NewEvidenceCampaignExecutor(EvidenceCampaignConfig{})
	covered := map[string]bool{}
	for _, t := range e.matrix.Techniques {
		covered[t.TID] = true
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.calculateCoverage(covered)
	}
	// Target: full ATT&CK matrix coverage scan well under 100ms.
}
