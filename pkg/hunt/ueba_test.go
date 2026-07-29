package hunt

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// TestUEBA_NumericDeviation trains a stable baseline then verifies a large
// outlier is flagged with a high Z-score while in-baseline values are quiet.
func TestUEBA_NumericDeviation(t *testing.T) {
	a := NewAnalyzer(AnalyzerConfig{ZThreshold: 3.0, MinSamples: 20})
	// Baseline: bytes_out around ~1000 with small variance.
	for i := 0; i < 40; i++ {
		v := 1000.0 + float64(i%5) // 1000..1004
		a.Train(Observation{Entity: "user:alice", Metrics: map[string]float64{"bytes_out": v}})
	}
	// In-baseline value → no anomaly.
	if an := a.Observe(Observation{Entity: "user:alice", Metrics: map[string]float64{"bytes_out": 1002}}); len(an) != 0 {
		t.Fatalf("in-baseline value must not be anomalous, got %+v", an)
	}
	// Massive spike → flagged.
	an := a.Observe(Observation{Entity: "user:alice", Metrics: map[string]float64{"bytes_out": 5_000_000}})
	if len(an) != 1 || an[0].Kind != AnomalyNumericDeviation || an[0].Feature != "bytes_out" {
		t.Fatalf("expected a numeric_deviation on bytes_out, got %+v", an)
	}
	if an[0].Score < 3.0 {
		t.Fatalf("expected Z-score >= 3, got %.2f", an[0].Score)
	}
}

// TestUEBA_LearningGate verifies that with too few samples no scoring happens
// (an analyzer cannot honestly flag anomalies before it has a baseline).
func TestUEBA_LearningGate(t *testing.T) {
	a := NewAnalyzer(AnalyzerConfig{MinSamples: 20})
	for i := 0; i < 5; i++ {
		a.Train(Observation{Entity: "h", Metrics: map[string]float64{"x": 1}})
	}
	if an := a.Observe(Observation{Entity: "h", Metrics: map[string]float64{"x": 9999}}); len(an) != 0 {
		t.Fatalf("must not score before MinSamples reached, got %+v", an)
	}
}

// TestUEBA_CategoricalRarity covers first-seen and rare-value detection.
func TestUEBA_CategoricalRarity(t *testing.T) {
	a := NewAnalyzer(AnalyzerConfig{MinCatSamples: 20, RarityThreshold: 0.05})
	// Baseline: alice always logs in from CN.
	for i := 0; i < 30; i++ {
		a.Train(Observation{Entity: "user:alice", Categories: map[string]string{"country": "CN"}})
	}
	// A brand-new country → first_seen.
	an := a.Observe(Observation{Entity: "user:alice", Categories: map[string]string{"country": "RU"}})
	if len(an) != 1 || an[0].Kind != AnomalyFirstSeen || an[0].Value != "RU" {
		t.Fatalf("expected first_seen for RU, got %+v", an)
	}
	// Known country → quiet.
	if an := a.Observe(Observation{Entity: "user:alice", Categories: map[string]string{"country": "CN"}}); len(an) != 0 {
		t.Fatalf("known country must be quiet, got %+v", an)
	}
}

// TestEngine_AnalyzeBehavior_EndToEnd wires the analyzer through the hunt engine:
// train a baseline, then a spike yields a MITRE-mapped, evidence-backed finding.
func TestEngine_AnalyzeBehavior_EndToEnd(t *testing.T) {
	t.Cleanup(capability.Reset)
	eng := NewEngine(intel.NewMemoryStore(), nil, nil)

	base := make([]Observation, 0, 30)
	for i := 0; i < 30; i++ {
		base = append(base, Observation{
			Entity:     "user:bob",
			Metrics:    map[string]float64{"egress_bytes": 500 + float64(i%3)},
			Categories: map[string]string{"src_country": "US"},
		})
	}
	eng.TrainBehavior(base)

	f, err := eng.AnalyzeBehavior(context.Background(), "nightly", []Observation{{
		Entity:     "user:bob",
		Metrics:    map[string]float64{"egress_bytes": 2_000_000}, // spike
		Categories: map[string]string{"src_country": "KP"},        // new country
	}})
	if err != nil {
		t.Fatalf("analyze behavior: %v", err)
	}
	if len(f) < 2 {
		t.Fatalf("expected >=2 findings (egress spike + new country), got %d: %+v", len(f), f)
	}
	// egress_bytes → T1048 exfiltration; src_country → T1078 valid accounts.
	techniques := map[string]bool{}
	for _, x := range f {
		techniques[x.Technique] = true
	}
	if !techniques["T1048"] || !techniques["T1078"] {
		t.Fatalf("expected T1048 and T1078 mappings, got %+v", techniques)
	}
}

// TestEngine_AnalyzeBehavior_SignsEvidence proves the behavior analysis records a
// signed receipt into the evidence ledger.
func TestEngine_AnalyzeBehavior_SignsEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x71}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	eng := NewEngine(intel.NewMemoryStore(), nil, nil)
	eng.SetEvidenceRecorder(ledger)

	base := make([]Observation, 0, 25)
	for i := 0; i < 25; i++ {
		base = append(base, Observation{Entity: "svc:api", Metrics: map[string]float64{"rps": 100}})
	}
	eng.TrainBehavior(base)
	_, _ = eng.AnalyzeBehavior(context.Background(), "svc-check", []Observation{
		{Entity: "svc:api", Metrics: map[string]float64{"rps": 100000}},
	})

	all, _ := ledger.Store().All(context.Background())
	found := false
	for _, ev := range all {
		if ev.Action == behaviorAction {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a %q evidence receipt", behaviorAction)
	}
}
