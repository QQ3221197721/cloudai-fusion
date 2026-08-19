package fed

import (
	"crypto/ed25519"
	"crypto/rand"
	"math"
	"testing"
)

func newTestPrivacyTracker(t *testing.T, totalEpsilon, delta, sensitivity, epsilonPerRound float64) *EvidencePrivacyTracker {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidencePrivacyTracker(priv, totalEpsilon, delta, sensitivity, epsilonPerRound)
}

// TestRecordRound_VerifiedNoiseProducesSignedProof feeds a round whose updates
// carry enough dispersion to back the claimed epsilon, and checks the signed
// proof, the budget accounting, and the noise commitment.
func TestRecordRound_VerifiedNoiseProducesSignedProof(t *testing.T) {
	// epsilon=1, delta=1e-5, sensitivity=1 => required sigma ~= 4.845.
	tracker := newTestPrivacyTracker(t, 10.0, 1e-5, 1.0, 1.0)

	// Two participants 10 apart per coordinate => sample sigma ~= 7.07 >= 4.845.
	round := FedRound{
		RoundID:      1,
		Participants: []string{"a", "b"},
		ModelVersion: "v1",
		Updates: []ModelUpdate{
			{ParticipantID: "a", Weights: []float64{0, 0, 0}, NumSamples: 100},
			{ParticipantID: "b", Weights: []float64{10, 10, 10}, NumSamples: 100},
		},
	}

	proof, err := tracker.RecordRound(round)
	if err != nil {
		t.Fatalf("record round: %v", err)
	}
	if !proof.NoiseVerified {
		t.Fatalf("expected noise to be verified: actual=%.4f required=%.4f", proof.ActualSigma, proof.RequiredSigma)
	}
	if proof.Receipt == nil || !proof.Receipt.Verify() {
		t.Fatal("privacy proof must carry a verifiable receipt")
	}
	if math.Abs(proof.EpsilonConsumed-1.0) > 1e-9 {
		t.Fatalf("expected 1.0 epsilon consumed, got %.4f", proof.EpsilonConsumed)
	}
	if math.Abs(proof.EpsilonRemaining-9.0) > 1e-9 {
		t.Fatalf("expected 9.0 epsilon remaining, got %.4f", proof.EpsilonRemaining)
	}

	// The receipt's commitment must be independently recomputable.
	want := noiseCommitmentHash(proof.RoundID, proof.ClaimedEpsilon, tracker.Verifier().delta, proof.ActualSigma)
	if want != proof.Commitment.Commitment {
		t.Fatal("noise commitment is not reproducible from its parameters")
	}
}

// TestRecordRound_UnderNoisedRejected proves the verifier rejects a round whose
// updates do not carry enough noise to back the claimed epsilon — the core
// "verifiable DP" guarantee competitors lack.
func TestRecordRound_UnderNoisedRejected(t *testing.T) {
	tracker := newTestPrivacyTracker(t, 10.0, 1e-5, 1.0, 1.0)

	round := FedRound{
		RoundID:      1,
		Participants: []string{"a", "b"},
		ModelVersion: "v1",
		Updates: []ModelUpdate{
			{ParticipantID: "a", Weights: []float64{0, 0}, NumSamples: 100},
			{ParticipantID: "b", Weights: []float64{1, 1}, NumSamples: 100}, // sigma ~= 0.7
		},
	}

	if _, err := tracker.RecordRound(round); err == nil {
		t.Fatal("expected under-noised round to be rejected")
	}
	// A rejected round must not consume any budget.
	if c := tracker.Verifier().Consumed(); c != 0 {
		t.Fatalf("rejected round must not consume budget, consumed=%.4f", c)
	}
}

// TestRequiredSigma_AnalyticBound checks the Gaussian-mechanism sigma matches
// the closed-form value and scales correctly with epsilon and sensitivity.
func TestRequiredSigma_AnalyticBound(t *testing.T) {
	v := NewDPVerifier(10.0, 1e-5, 1.0)

	got := v.RequiredSigma(1.0)
	want := math.Sqrt(2 * math.Log(1.25/1e-5)) // sensitivity 1, epsilon 1
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("required sigma mismatch: got %.6f want %.6f", got, want)
	}

	// Halving epsilon doubles the required noise.
	if math.Abs(v.RequiredSigma(0.5)-2*want) > 1e-9 {
		t.Fatalf("sigma should scale as 1/epsilon: got %.6f", v.RequiredSigma(0.5))
	}

	// Larger sensitivity demands proportionally more noise.
	v2 := NewDPVerifier(10.0, 1e-5, 2.0)
	if math.Abs(v2.RequiredSigma(1.0)-2*want) > 1e-9 {
		t.Fatalf("sigma should scale with sensitivity: got %.6f", v2.RequiredSigma(1.0))
	}
}

// TestVerifyPrivacyGuarantee exercises the accept/reject boundary directly.
func TestVerifyPrivacyGuarantee(t *testing.T) {
	v := NewDPVerifier(10.0, 1e-5, 1.0)
	req := v.RequiredSigma(1.0)

	if !v.VerifyPrivacyGuarantee(1.0, req) {
		t.Fatal("sigma exactly at requirement must be accepted")
	}
	if !v.VerifyPrivacyGuarantee(1.0, req*1.5) {
		t.Fatal("over-noising must be accepted")
	}
	if v.VerifyPrivacyGuarantee(1.0, req*0.99) {
		t.Fatal("under-noising must be rejected")
	}
	if v.VerifyPrivacyGuarantee(0, 100) || v.VerifyPrivacyGuarantee(1.0, 0) {
		t.Fatal("non-positive inputs must be rejected")
	}
}

// TestBudgetExhaustion verifies sequential composition stops accepting rounds
// once the cumulative epsilon budget is spent.
func TestBudgetExhaustion(t *testing.T) {
	// Total budget only allows 2 rounds at epsilon=1 each.
	tracker := newTestPrivacyTracker(t, 2.0, 1e-5, 1.0, 1.0)

	mkRound := func(id int) FedRound {
		return FedRound{
			RoundID:      id,
			Participants: []string{"a", "b"},
			ModelVersion: "v",
			Updates: []ModelUpdate{
				{ParticipantID: "a", Weights: []float64{0, 0, 0}, NumSamples: 50},
				{ParticipantID: "b", Weights: []float64{10, 10, 10}, NumSamples: 50},
			},
		}
	}

	if _, err := tracker.RecordRound(mkRound(1)); err != nil {
		t.Fatalf("round 1 should succeed: %v", err)
	}
	if _, err := tracker.RecordRound(mkRound(2)); err != nil {
		t.Fatalf("round 2 should succeed: %v", err)
	}
	if _, err := tracker.RecordRound(mkRound(3)); err == nil {
		t.Fatal("round 3 must fail: budget exhausted")
	}
	if r := tracker.Verifier().Remaining(); r != 0 {
		t.Fatalf("budget should be fully spent, remaining=%.4f", r)
	}
	if len(tracker.Verifier().History()) != 2 {
		t.Fatalf("only accepted rounds should be committed, got %d", len(tracker.Verifier().History()))
	}
}

// TestEstimateNoiseSigma checks the dispersion estimator against a known value.
func TestEstimateNoiseSigma(t *testing.T) {
	// Single participant => no measurable dispersion.
	if s := estimateNoiseSigma([]ModelUpdate{{Weights: []float64{1, 2, 3}}}); s != 0 {
		t.Fatalf("single participant sigma should be 0, got %.4f", s)
	}
	// Two participants 0 and 10 => sample std = 10/sqrt(2).
	got := estimateNoiseSigma([]ModelUpdate{
		{Weights: []float64{0}},
		{Weights: []float64{10}},
	})
	want := 10.0 / math.Sqrt(2)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("sigma estimate mismatch: got %.6f want %.6f", got, want)
	}
}
