package observability

// evidence_traceproof.go signs trace/span correlation results and adds an
// independent innovation: probabilistic root-cause ranking via Bayesian
// inference over error propagation.
//
// Innovation — Bayesian Root Cause Ranking:
// Given per-component prior failure probabilities and observed error likelihoods
// (how strongly each component's signal correlates with the observed incident),
// we apply Bayes' rule: posterior ∝ prior × likelihood. Normalizing across all
// candidate components yields a ranked list of the most probable root causes —
// distinguishing true causes from downstream symptoms.

import (
	"crypto/ed25519"
	"crypto/rand"
	"sort"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceRootCause is one ranked candidate cause with its posterior probability.
type EvidenceRootCause struct {
	Component   string  `json:"component"`
	Prior       float64 `json:"prior"`
	Likelihood  float64 `json:"likelihood"`
	Posterior   float64 `json:"posterior"`
}

// EvidenceTraceResult is the signed outcome of a trace correlation.
type EvidenceTraceResult struct {
	TraceID     string               `json:"trace_id"`
	SpanCount   int                  `json:"span_count"`
	RankedCauses []EvidenceRootCause `json:"ranked_causes"`
	TopCause    string               `json:"top_cause"`
	Receipt     *evidence.Receipt    `json:"receipt"`
}

// EvidenceTraceEngine wraps trace correlation with receipts and Bayesian root
// cause ranking.
type EvidenceTraceEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	priors         map[string]float64 // component -> prior failure probability
}

// NewEvidenceTraceEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceTraceEngine() *EvidenceTraceEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceTraceEngine{
		receiptBuilder: evidence.NewReceiptBuilder("observability", privKey),
		priors:         make(map[string]float64),
	}
}

// SetPrior sets the base failure probability for a component.
func (e *EvidenceTraceEngine) SetPrior(component string, prior float64) {
	e.priors[component] = prior
}

// CorrelateTrace ranks candidate root causes by posterior probability.
// likelihoods maps component -> P(observed errors | component is the cause).
func (e *EvidenceTraceEngine) CorrelateTrace(traceID string, spanCount int, likelihoods map[string]float64) (*EvidenceTraceResult, error) {
	// Bayes: posterior_i ∝ prior_i × likelihood_i, then normalize.
	var evidenceSum float64
	unnormalized := make(map[string]float64, len(likelihoods))
	for comp, lk := range likelihoods {
		prior, ok := e.priors[comp]
		if !ok {
			prior = 0.01 // default weak prior for unseen components
		}
		p := prior * lk
		unnormalized[comp] = p
		evidenceSum += p
	}

	ranked := make([]EvidenceRootCause, 0, len(likelihoods))
	for comp, lk := range likelihoods {
		prior := e.priors[comp]
		if prior == 0 {
			prior = 0.01
		}
		posterior := 0.0
		if evidenceSum > 0 {
			posterior = unnormalized[comp] / evidenceSum
		}
		ranked = append(ranked, EvidenceRootCause{
			Component:  comp,
			Prior:      prior,
			Likelihood: lk,
			Posterior:  posterior,
		})
	}

	// Sort descending by posterior (stable on component name for determinism).
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Posterior == ranked[j].Posterior {
			return ranked[i].Component < ranked[j].Component
		}
		return ranked[i].Posterior > ranked[j].Posterior
	})

	topCause := ""
	if len(ranked) > 0 {
		topCause = ranked[0].Component
	}

	input := map[string]interface{}{
		"trace_id":    traceID,
		"span_count":  spanCount,
		"likelihoods": likelihoods,
	}
	output := map[string]interface{}{"ranked": ranked}
	receipt, err := e.receiptBuilder.Build("obs.correlate", input, output)
	if err != nil {
		return nil, err
	}

	return &EvidenceTraceResult{
		TraceID:      traceID,
		SpanCount:    spanCount,
		RankedCauses: ranked,
		TopCause:     topCause,
		Receipt:      receipt,
	}, nil
}
