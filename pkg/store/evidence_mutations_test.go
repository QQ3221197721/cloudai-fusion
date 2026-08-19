package store

import (
	"crypto/ed25519"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func newTestEngine(t *testing.T) *EvidenceStoreEngine {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	key := ed25519.NewKeyFromSeed(seed)
	rb := evidence.NewReceiptBuilder("store", key)
	return NewEvidenceStoreEngine(rb)
}

func TestMutate_ProducesVerifiableReceipt(t *testing.T) {
	e := newTestEngine(t)
	res, err := e.Mutate(MutationRecord{
		Kind:   MutationInsert,
		Table:  "workloads",
		Key:    "wl-1",
		After:  map[string]any{"status": "running"},
	})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if res.Receipt == nil {
		t.Fatal("expected a receipt")
	}
	if !res.Receipt.Verify() {
		t.Fatal("receipt signature must verify")
	}
	if res.Receipt.Operation != "store.insert" {
		t.Fatalf("operation = %q, want store.insert", res.Receipt.Operation)
	}
}

func TestMutations_ChainTogether(t *testing.T) {
	e := newTestEngine(t)
	var receipts []*evidence.Receipt
	for i, k := range []MutationKind{MutationInsert, MutationUpdate, MutationDelete} {
		r, err := e.Mutate(MutationRecord{Kind: k, Table: "t", Key: "k"})
		if err != nil {
			t.Fatalf("mutate %d: %v", i, err)
		}
		receipts = append(receipts, r.Receipt)
	}
	if err := evidence.VerifyChainOfReceipts(receipts); err != nil {
		t.Fatalf("chain verify: %v", err)
	}
}

// TestPredictiveOptimizer_LearnsSequentialPattern feeds a repeating access
// pattern and asserts the Markov predictor exceeds the 70% accuracy bar.
func TestPredictiveOptimizer_LearnsSequentialPattern(t *testing.T) {
	e := newTestEngine(t)
	// A deterministic cycle A->B->C->A... is perfectly predictable after the
	// first pass. Over many cycles accuracy converges toward 1.0.
	pattern := []string{"A", "B", "C"}
	for i := 0; i < 300; i++ {
		if _, err := e.Query(pattern[i%len(pattern)]); err != nil {
			t.Fatalf("query: %v", err)
		}
	}
	if acc := e.PredictionAccuracy(); acc < 0.70 {
		t.Fatalf("prediction accuracy = %.3f, want >= 0.70", acc)
	}
}

func TestPredictiveOptimizer_PrewarmProducesCacheHits(t *testing.T) {
	e := newTestEngine(t)
	pattern := []string{"X", "Y", "Z"}
	for i := 0; i < 60; i++ {
		if _, err := e.Query(pattern[i%len(pattern)]); err != nil {
			t.Fatalf("query: %v", err)
		}
	}
	if hr := e.PrewarmHitRate(); hr < 0.70 {
		t.Fatalf("prewarm hit rate = %.3f, want >= 0.70", hr)
	}
}

func TestQueryPredictor_ArgmaxIsDeterministic(t *testing.T) {
	p := NewQueryPredictor()
	// Build a distribution where B is clearly the most likely successor of A.
	seq := []string{"A", "B", "A", "B", "A", "C", "A", "B"}
	for _, q := range seq {
		p.Observe(q)
	}
	next, conf := p.Predict("A")
	if next != "B" {
		t.Fatalf("predicted %q, want B", next)
	}
	if conf <= 0.5 {
		t.Fatalf("confidence = %.3f, want > 0.5", conf)
	}
}

func TestQueryPredictor_ColdStart(t *testing.T) {
	p := NewQueryPredictor()
	next, conf := p.Observe("first")
	if next != "" || conf != 0 {
		t.Fatalf("cold start should not predict, got %q/%.3f", next, conf)
	}
}
