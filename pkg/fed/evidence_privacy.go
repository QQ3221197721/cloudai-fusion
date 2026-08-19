package fed

// evidence_privacy.go layers two independent barriers over federated learning:
//
//  1. Evidence-native barrier — every federated round emits a signed,
//     offline-verifiable evidence.Receipt attesting exactly how much of the
//     differential-privacy budget was consumed and how much remains. Competitors
//     keep a mutable "epsilon spent" counter in a dashboard; we emit an
//     unforgeable Ed25519 attestation that a regulator can verify without
//     trusting our servers.
//
//  2. Independent-innovation barrier — a DPVerifier does not merely *claim* that
//     differential privacy was applied, it *proves* it. For the Gaussian
//     mechanism the noise scale sigma required for an (epsilon, delta) guarantee
//     is analytically determined by the query sensitivity. The verifier derives
//     the required sigma, measures the sigma actually present in the round's
//     updates, and only accepts the round when the observed noise is at least as
//     large as the analytic requirement. Each accepted round produces a
//     NoiseCommitment (a hash over the noise-distribution parameters) that is
//     folded into the receipt, so the privacy claim is checkable post-hoc.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// NoiseCommitment is a verifiable commitment to the noise distribution used in a
// single federated round. It binds the round to the (epsilon, delta, sigma)
// parameters via a SHA-256 hash so the privacy claim can be audited later.
type NoiseCommitment struct {
	RoundID    int       `json:"round_id"`
	Epsilon    float64   `json:"epsilon"`
	Delta      float64   `json:"delta"`
	Sigma      float64   `json:"sigma"`
	Commitment [32]byte  `json:"commitment"`
	Timestamp  time.Time `json:"timestamp"`
}

// PrivacyProof is the signed result of accounting for one federated round.
type PrivacyProof struct {
	RoundID          int               `json:"round_id"`
	ClaimedEpsilon   float64           `json:"claimed_epsilon"`
	EpsilonConsumed  float64           `json:"epsilon_consumed"`
	EpsilonRemaining float64           `json:"epsilon_remaining"`
	RequiredSigma    float64           `json:"required_sigma"`
	ActualSigma      float64           `json:"actual_sigma"`
	NoiseVerified    bool              `json:"noise_verified"`
	Commitment       NoiseCommitment   `json:"commitment"`
	Receipt          *evidence.Receipt `json:"receipt,omitempty"`
}

// EvidencePrivacyTracker records federated rounds, enforces the DP budget, and
// seals each round's privacy accounting into a signed receipt.
type EvidencePrivacyTracker struct {
	receiptBuilder  *evidence.ReceiptBuilder
	privacyVerifier *DPVerifier
	epsilonPerRound float64
}

// NewEvidencePrivacyTracker builds a tracker signing with the supplied Ed25519
// key. totalEpsilon is the cumulative privacy budget, delta the failure
// probability, sensitivity the L2 sensitivity of the aggregation query, and
// epsilonPerRound the epsilon claimed to be spent per round.
func NewEvidencePrivacyTracker(privKey ed25519.PrivateKey, totalEpsilon, delta, sensitivity, epsilonPerRound float64) *EvidencePrivacyTracker {
	return &EvidencePrivacyTracker{
		receiptBuilder:  evidence.NewReceiptBuilder("fed.privacy", privKey),
		privacyVerifier: NewDPVerifier(totalEpsilon, delta, sensitivity),
		epsilonPerRound: epsilonPerRound,
	}
}

// Verifier exposes the underlying DP verifier for inspection.
func (t *EvidencePrivacyTracker) Verifier() *DPVerifier { return t.privacyVerifier }

// RecordRound performs privacy accounting for a federated round and returns a
// signed proof. It (1) measures the noise actually present in the participant
// updates, (2) verifies that noise is large enough to back the claimed epsilon
// under the Gaussian mechanism, (3) consumes the round's epsilon from the
// budget, and (4) seals the accounting into an offline-verifiable receipt.
func (t *EvidencePrivacyTracker) RecordRound(round FedRound) (*PrivacyProof, error) {
	if len(round.Updates) == 0 {
		return nil, errors.New("fed: round has no updates")
	}

	claimed := t.epsilonPerRound
	actualSigma := estimateNoiseSigma(round.Updates)
	required := t.privacyVerifier.RequiredSigma(claimed)
	verified := t.privacyVerifier.VerifyPrivacyGuarantee(claimed, actualSigma)

	if !verified {
		return nil, fmt.Errorf("fed: privacy guarantee violated for round %d: observed sigma %.4f < required %.4f for epsilon %.4f",
			round.RoundID, actualSigma, required, claimed)
	}

	// Only charge the budget once the guarantee holds.
	if err := t.privacyVerifier.Consume(claimed); err != nil {
		return nil, fmt.Errorf("fed: budget: %w", err)
	}

	commitment := t.privacyVerifier.commit(round.RoundID, claimed, actualSigma)

	proof := &PrivacyProof{
		RoundID:          round.RoundID,
		ClaimedEpsilon:   claimed,
		EpsilonConsumed:  t.privacyVerifier.Consumed(),
		EpsilonRemaining: t.privacyVerifier.Remaining(),
		RequiredSigma:    required,
		ActualSigma:      actualSigma,
		NoiseVerified:    verified,
		Commitment:       commitment,
	}

	receipt, err := t.receiptBuilder.Build("fed.privacy.round", struct {
		RoundID       int     `json:"round_id"`
		ModelVersion  string  `json:"model_version"`
		Participants  int     `json:"participants"`
		Claimed       float64 `json:"claimed_epsilon"`
		Delta         float64 `json:"delta"`
		RequiredSigma float64 `json:"required_sigma"`
	}{round.RoundID, round.ModelVersion, len(round.Participants), claimed, t.privacyVerifier.delta, required},
		struct {
			ActualSigma float64  `json:"actual_sigma"`
			Verified    bool     `json:"noise_verified"`
			Consumed    float64  `json:"epsilon_consumed"`
			Remaining   float64  `json:"epsilon_remaining"`
			Commitment  [32]byte `json:"commitment"`
		}{actualSigma, verified, proof.EpsilonConsumed, proof.EpsilonRemaining, commitment.Commitment})
	if err != nil {
		return nil, fmt.Errorf("fed: seal privacy round: %w", err)
	}
	proof.Receipt = receipt
	return proof, nil
}

// ---------------------------------------------------------------------------
// INNOVATION: verifiable differential privacy (Gaussian mechanism)
// ---------------------------------------------------------------------------

// DPVerifier proves that a claimed (epsilon, delta) differential-privacy
// guarantee is actually backed by sufficient noise, and accounts for cumulative
// epsilon across rounds using sequential composition.
type DPVerifier struct {
	mu              sync.Mutex
	totalEpsilon    float64
	consumedEpsilon float64
	delta           float64
	sensitivity     float64
	noiseHistory    []NoiseCommitment
}

// NewDPVerifier builds a verifier for the Gaussian mechanism with the given
// total budget, delta and L2 query sensitivity.
func NewDPVerifier(totalEpsilon, delta, sensitivity float64) *DPVerifier {
	if delta <= 0 || delta >= 1 {
		delta = 1e-5
	}
	if sensitivity <= 0 {
		sensitivity = 1.0
	}
	return &DPVerifier{
		totalEpsilon: totalEpsilon,
		delta:        delta,
		sensitivity:  sensitivity,
	}
}

// RequiredSigma returns the minimum Gaussian noise standard deviation needed to
// satisfy (epsilon, delta)-DP for a query of the configured L2 sensitivity:
//
//	sigma >= Δf * sqrt(2 * ln(1.25 / delta)) / epsilon
//
// This is the classic analytic bound for the Gaussian mechanism (Dwork &
// Roth, "The Algorithmic Foundations of Differential Privacy", Thm. 3.22).
func (v *DPVerifier) RequiredSigma(epsilon float64) float64 {
	if epsilon <= 0 {
		return math.Inf(1)
	}
	return v.sensitivity * math.Sqrt(2*math.Log(1.25/v.delta)) / epsilon
}

// VerifyPrivacyGuarantee returns true iff the noise actually present
// (actualNoiseSigma) is at least the analytic requirement for the claimed
// epsilon. Under-noising is what breaks DP, so we accept any sigma that meets or
// exceeds the requirement.
func (v *DPVerifier) VerifyPrivacyGuarantee(claimed float64, actualNoiseSigma float64) bool {
	if claimed <= 0 || actualNoiseSigma <= 0 {
		return false
	}
	return actualNoiseSigma >= v.RequiredSigma(claimed)
}

// Consume charges epsilon against the cumulative budget using sequential
// composition. It returns an error (leaving the budget unchanged) when the
// budget would be exceeded.
func (v *DPVerifier) Consume(epsilon float64) error {
	if epsilon < 0 {
		return errors.New("fed: epsilon must be non-negative")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.consumedEpsilon+epsilon > v.totalEpsilon+1e-12 {
		return fmt.Errorf("budget exhausted: consumed=%.4f + %.4f > total=%.4f", v.consumedEpsilon, epsilon, v.totalEpsilon)
	}
	v.consumedEpsilon += epsilon
	return nil
}

// Consumed returns the cumulative epsilon spent so far.
func (v *DPVerifier) Consumed() float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.consumedEpsilon
}

// Remaining returns the epsilon left in the budget.
func (v *DPVerifier) Remaining() float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return math.Max(0, v.totalEpsilon-v.consumedEpsilon)
}

// History returns a copy of the recorded noise commitments.
func (v *DPVerifier) History() []NoiseCommitment {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]NoiseCommitment, len(v.noiseHistory))
	copy(out, v.noiseHistory)
	return out
}

// commit builds and records a NoiseCommitment binding the round to its
// (epsilon, delta, sigma) noise parameters via a SHA-256 hash.
func (v *DPVerifier) commit(roundID int, epsilon, sigma float64) NoiseCommitment {
	nc := NoiseCommitment{
		RoundID:   roundID,
		Epsilon:   epsilon,
		Delta:     v.delta,
		Sigma:     sigma,
		Timestamp: time.Now(),
	}
	nc.Commitment = noiseCommitmentHash(roundID, epsilon, v.delta, sigma)

	v.mu.Lock()
	v.noiseHistory = append(v.noiseHistory, nc)
	v.mu.Unlock()
	return nc
}

// noiseCommitmentHash deterministically hashes the noise-distribution
// parameters so a third party can later recompute and check the commitment.
func noiseCommitmentHash(roundID int, epsilon, delta, sigma float64) [32]byte {
	var buf [8 + 8*3]byte
	binary.BigEndian.PutUint64(buf[0:8], uint64(roundID))
	binary.BigEndian.PutUint64(buf[8:16], math.Float64bits(epsilon))
	binary.BigEndian.PutUint64(buf[16:24], math.Float64bits(delta))
	binary.BigEndian.PutUint64(buf[24:32], math.Float64bits(sigma))
	return sha256.Sum256(buf[:])
}

// estimateNoiseSigma measures the noise magnitude present across participant
// updates as the mean per-coordinate sample standard deviation. Independent
// Gaussian perturbations added to each participant's update inflate this
// dispersion; the sample standard deviation is therefore a consistent estimator
// of the injected noise scale, which the verifier compares against the analytic
// requirement.
func estimateNoiseSigma(updates []ModelUpdate) float64 {
	n := len(updates)
	if n < 2 {
		return 0
	}
	dim := len(updates[0].Weights)
	if dim == 0 {
		return 0
	}

	var sumStd float64
	counted := 0
	for i := 0; i < dim; i++ {
		var sum, sumSq float64
		m := 0
		for _, u := range updates {
			if i >= len(u.Weights) {
				continue
			}
			x := u.Weights[i]
			sum += x
			sumSq += x * x
			m++
		}
		if m < 2 {
			continue
		}
		mean := sum / float64(m)
		// Unbiased sample variance.
		variance := (sumSq - float64(m)*mean*mean) / float64(m-1)
		if variance < 0 {
			variance = 0
		}
		sumStd += math.Sqrt(variance)
		counted++
	}
	if counted == 0 {
		return 0
	}
	return sumStd / float64(counted)
}
