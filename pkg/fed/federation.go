// Package fed implements a federated learning coordinator with pluggable
// aggregation strategies (FedAvg, FedMedian, FedProx), differential-privacy
// budget accounting, and encrypted weight exchange.
package fed

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
)

// ModelUpdate is a participant's contribution to a federated round.
type ModelUpdate struct {
	ParticipantID string
	Weights       []float64 // flattened model parameters
	NumSamples    int       // number of local training samples (weighting factor)
}

// FedRound tracks a single round of federated training.
type FedRound struct {
	RoundID      int
	Participants []string
	ModelVersion string
	Updates      []ModelUpdate
}

// AggregationStrategy combines participant updates into a global model.
type AggregationStrategy interface {
	Name() string
	Aggregate(updates []ModelUpdate) ([]float64, error)
}

// FedAvgAggregator implements the classic FedAvg weighted-average algorithm:
// the global model is the sample-weighted mean of client updates.
type FedAvgAggregator struct{}

func (FedAvgAggregator) Name() string { return "fedavg" }

// Aggregate computes sum(n_k * w_k) / sum(n_k) elementwise.
func (FedAvgAggregator) Aggregate(updates []ModelUpdate) ([]float64, error) {
	if len(updates) == 0 {
		return nil, errors.New("fedavg: no updates to aggregate")
	}
	dim := len(updates[0].Weights)
	if dim == 0 {
		return nil, errors.New("fedavg: empty weight vector")
	}
	totalSamples := 0
	for _, u := range updates {
		if len(u.Weights) != dim {
			return nil, fmt.Errorf("fedavg: dimension mismatch: got %d, want %d", len(u.Weights), dim)
		}
		if u.NumSamples <= 0 {
			return nil, fmt.Errorf("fedavg: participant %s has non-positive sample count", u.ParticipantID)
		}
		totalSamples += u.NumSamples
	}

	global := make([]float64, dim)
	for _, u := range updates {
		w := float64(u.NumSamples) / float64(totalSamples)
		for i, v := range u.Weights {
			global[i] += w * v
		}
	}
	return global, nil
}

// FedMedianAggregator implements coordinate-wise median aggregation, robust to
// Byzantine participants.
type FedMedianAggregator struct{}

func (FedMedianAggregator) Name() string { return "fedmedian" }

func (FedMedianAggregator) Aggregate(updates []ModelUpdate) ([]float64, error) {
	if len(updates) == 0 {
		return nil, errors.New("fedmedian: no updates to aggregate")
	}
	dim := len(updates[0].Weights)
	global := make([]float64, dim)
	col := make([]float64, len(updates))
	for i := 0; i < dim; i++ {
		for j, u := range updates {
			if len(u.Weights) != dim {
				return nil, fmt.Errorf("fedmedian: dimension mismatch for %s", u.ParticipantID)
			}
			col[j] = u.Weights[i]
		}
		global[i] = median(col)
	}
	return global, nil
}

func median(vals []float64) float64 {
	c := make([]float64, len(vals))
	copy(c, vals)
	sort.Float64s(c)
	n := len(c)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// FedProxAggregator implements FedProx: FedAvg with a proximal term that pulls
// aggregated weights toward the previous global model, controlled by Mu.
type FedProxAggregator struct {
	Mu           float64   // proximal term coefficient (0 = pure FedAvg)
	GlobalWeights []float64 // previous global model for the proximal pull
}

func (a FedProxAggregator) Name() string { return "fedprox" }

func (a FedProxAggregator) Aggregate(updates []ModelUpdate) ([]float64, error) {
	base, err := (FedAvgAggregator{}).Aggregate(updates)
	if err != nil {
		return nil, err
	}
	if a.Mu <= 0 || len(a.GlobalWeights) != len(base) {
		return base, nil
	}
	// Apply proximal regularization: w = (base + mu*global) / (1 + mu)
	out := make([]float64, len(base))
	for i := range base {
		out[i] = (base[i] + a.Mu*a.GlobalWeights[i]) / (1 + a.Mu)
	}
	return out, nil
}

// PrivacyBudget tracks differential-privacy epsilon consumption across rounds
// using basic sequential composition (epsilons add up).
type PrivacyBudget struct {
	mu          sync.Mutex
	TotalEpsilon float64 // maximum allowed cumulative epsilon
	Delta        float64
	spent        float64
	rounds       int
}

// NewPrivacyBudget creates a budget with a max total epsilon.
func NewPrivacyBudget(totalEpsilon, delta float64) *PrivacyBudget {
	return &PrivacyBudget{TotalEpsilon: totalEpsilon, Delta: delta}
}

// Consume attempts to spend epsilon for one round. It returns an error when the
// budget would be exhausted, leaving the budget unchanged.
func (b *PrivacyBudget) Consume(epsilon float64) error {
	if epsilon < 0 {
		return errors.New("privacy: epsilon must be non-negative")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spent+epsilon > b.TotalEpsilon {
		return fmt.Errorf("privacy: budget exhausted: spent=%.4f + %.4f > total=%.4f", b.spent, epsilon, b.TotalEpsilon)
	}
	b.spent += epsilon
	b.rounds++
	return nil
}

// Remaining returns the epsilon left in the budget.
func (b *PrivacyBudget) Remaining() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return math.Max(0, b.TotalEpsilon-b.spent)
}

// Spent returns cumulative epsilon consumed.
func (b *PrivacyBudget) Spent() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent
}

// SecureExchange encrypts model weights with AES-256-GCM before transmission.
type SecureExchange struct {
	key []byte // 32-byte AES-256 key
}

// NewSecureExchange builds an exchange with the provided 32-byte key.
func NewSecureExchange(key []byte) (*SecureExchange, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secure-exchange: key must be 32 bytes, got %d", len(key))
	}
	return &SecureExchange{key: key}, nil
}

// Encrypt serializes and encrypts a weight vector. The nonce is prepended to
// the ciphertext.
func (s *SecureExchange) Encrypt(weights []float64) ([]byte, error) {
	plaintext := float64sToBytes(weights)
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt, returning the weight vector.
func (s *SecureExchange) Decrypt(ciphertext []byte) ([]float64, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("secure-exchange: ciphertext too short")
	}
	nonce, data := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, fmt.Errorf("secure-exchange: decrypt failed: %w", err)
	}
	return bytesToFloat64s(plaintext)
}

// FederationCoordinator orchestrates federated rounds.
type FederationCoordinator struct {
	mu       sync.Mutex
	strategy AggregationStrategy
	budget   *PrivacyBudget
	exchange *SecureExchange
	rounds   []*FedRound
	globalModel []float64
	roundCtr int
}

// NewFederationCoordinator builds a coordinator.
func NewFederationCoordinator(strategy AggregationStrategy, budget *PrivacyBudget, exchange *SecureExchange) *FederationCoordinator {
	return &FederationCoordinator{strategy: strategy, budget: budget, exchange: exchange}
}

// RunRound aggregates updates into a new global model, charging the privacy
// budget epsilonPerRound if a budget is configured.
func (c *FederationCoordinator) RunRound(updates []ModelUpdate, epsilonPerRound float64) (*FedRound, error) {
	if c.strategy == nil {
		return nil, errors.New("coordinator: no aggregation strategy configured")
	}
	if c.budget != nil {
		if err := c.budget.Consume(epsilonPerRound); err != nil {
			return nil, err
		}
	}

	global, err := c.strategy.Aggregate(updates)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.roundCtr++
	participants := make([]string, 0, len(updates))
	for _, u := range updates {
		participants = append(participants, u.ParticipantID)
	}
	round := &FedRound{
		RoundID:      c.roundCtr,
		Participants: participants,
		ModelVersion: fmt.Sprintf("v%d", c.roundCtr),
		Updates:      updates,
	}
	c.rounds = append(c.rounds, round)
	c.globalModel = global
	return round, nil
}

// GlobalModel returns a copy of the current global model.
func (c *FederationCoordinator) GlobalModel() []float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]float64, len(c.globalModel))
	copy(out, c.globalModel)
	return out
}

// ---------------------------------------------------------------------------
// serialization helpers
// ---------------------------------------------------------------------------

func float64sToBytes(vals []float64) []byte {
	buf := make([]byte, 8*len(vals))
	for i, v := range vals {
		bits := math.Float64bits(v)
		for j := 0; j < 8; j++ {
			buf[i*8+j] = byte(bits >> (8 * j))
		}
	}
	return buf
}

func bytesToFloat64s(b []byte) ([]float64, error) {
	if len(b)%8 != 0 {
		return nil, errors.New("secure-exchange: byte length not a multiple of 8")
	}
	out := make([]float64, len(b)/8)
	for i := range out {
		var bits uint64
		for j := 0; j < 8; j++ {
			bits |= uint64(b[i*8+j]) << (8 * j)
		}
		out[i] = math.Float64frombits(bits)
	}
	return out, nil
}
