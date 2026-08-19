package fed

import (
	"math"
	"testing"
)

func TestFedAvgAggregation(t *testing.T) {
	updaters := []ModelUpdate{
		{ParticipantID: "p1", Weights: []float64{0.9, 0.1, 0.2}, NumSamples: 100},
		{ParticipantID: "p2", Weights: []float64{0.7, 0.3, 0.6}, NumSamples: 100},
	}
	strategy := FedAvgAggregator{}
	global, err := strategy.Aggregate(updaters)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	expected := []float64{0.8, 0.2, 0.4} // simple average of each dimension
	for i := range global {
		diff := math.Abs(global[i] - expected[i])
		if diff > 1e-9 {
			t.Errorf("global[%d]=%.10f; want %.10f", i, global[i], expected[i])
		}
	}
}

func TestPrivacyBudget(t *testing.T) {
	b := NewPrivacyBudget(1.0, 1e-5)

	t.Run("consume_success", func(t *testing.T) {
		if err := b.Consume(0.2); err != nil {
			t.Fatalf("Consume(0.2): %v", err)
		}
		if sp := b.Spent(); sp != 0.2 {
			t.Errorf("Spent=%.4f; want 0.2", sp)
		}
		if rem := b.Remaining(); rem != 0.8 {
			t.Errorf("Remaining=%.4f; want 0.8", rem)
		}
	})

	t.Run("exhaust_budget", func(t *testing.T) {
		err := b.Consume(0.9)
		if err == nil {
			t.Error("expected error when exceeding budget")
		}
		// Budget should remain unchanged after failed consume
		if sp := b.Spent(); sp != 0.2 {
			t.Errorf("Spent=%.4f after failed consume; want 0.2", sp)
		}
	})

	t.Run("exact_exhaustion", func(t *testing.T) {
		clean := NewPrivacyBudget(1.0, 0)
		if err := clean.Consume(0.4); err != nil {
			t.Fatalf("Consume(0.4): %v", err)
		}
		if err := clean.Consume(0.6); err != nil {
			t.Fatalf("Consume(0.6): %v", err)
		}
		if err := clean.Consume(0.0001); err == nil {
			t.Error("expected exhaustion at or just above budget")
		}
	})
}

func TestFederationCoordinator(t *testing.T) {
	strategy := FedAvgAggregator{}
	budget := NewPrivacyBudget(10.0, 1e-5)
	exchange, _ := NewSecureExchange(make([]byte, 32))
	coord := NewFederationCoordinator(strategy, budget, exchange)

	updates := []ModelUpdate{
		{ParticipantID: "a", Weights: []float64{1.0, 2.0}, NumSamples: 10},
		{ParticipantID: "b", Weights: []float64{0.5, 1.5}, NumSamples: 10},
	}

	round, err := coord.RunRound(updates, 0.1)
	if err != nil {
		t.Fatalf("RunRound: %v", err)
	}
	if len(round.Participants) != 2 {
		t.Errorf("Participants=%d; want 2", len(round.Participants))
	}

	gm := coord.GlobalModel()
	if len(gm) != 2 {
		t.Errorf("GlobalModel dimension=%d; want 2", len(gm))
	}
}

func BenchmarkFedAvgAggregation(b *testing.B) {
	nUpdates := 10
	dim := 10000
	baseWeights := make([][]float64, nUpdates)
	for i := range baseWeights {
		w := make([]float64, dim)
		for j := range w {
			w[j] = float64(i+j) / 100.0
		}
		baseWeights[i] = w
	}
	updates := make([]ModelUpdate, nUpdates)
	for i := 0; i < nUpdates; i++ {
		updates[i] = ModelUpdate{ParticipantID: string(rune('A'+i)), Weights: baseWeights[i], NumSamples: 100}
	}
	strategy := FedAvgAggregator{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := strategy.Aggregate(updates)
		if err != nil {
			b.Fatalf("Aggregate error: %v", err)
		}
	}
}
