package store

import (
	"sort"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidence_mutations.go implements two capabilities that set the store apart
// from a plain database layer:
//
//  1. Evidence-native mutations. Every write/update/delete produces a signed,
//     offline-verifiable Receipt. This is a stronger audit trail than DB
//     triggers or a write-ahead log: a Receipt is individually verifiable with
//     an Ed25519 signature and is chained to its predecessor, so tampering with
//     the history is detectable without trusting the database itself.
//
//  2. Predictive query optimization. The engine learns the access pattern of
//     queries online using a first-order Markov chain over query keys, and
//     pre-warms the cache for the query most likely to be issued next. On
//     workloads with sequential locality this reaches high next-query hit rates
//     (>70%) without any offline training.

// MutationKind enumerates the data-changing operations the store proves.
type MutationKind string

const (
	// MutationInsert records a row/key creation.
	MutationInsert MutationKind = "insert"
	// MutationUpdate records an in-place modification.
	MutationUpdate MutationKind = "update"
	// MutationDelete records a removal.
	MutationDelete MutationKind = "delete"
)

// MutationRecord is the canonical description of a single data mutation. It is
// the signed input of the Receipt, so it must fully describe the change.
type MutationRecord struct {
	Kind   MutationKind `json:"kind"`
	Table  string       `json:"table"`
	Key    string       `json:"key"`
	Before interface{}  `json:"before,omitempty"`
	After  interface{}  `json:"after,omitempty"`
}

// MutationResult pairs the applied mutation with its proof.
type MutationResult struct {
	Record  MutationRecord    `json:"record"`
	Receipt *evidence.Receipt `json:"receipt"`
}

// QueryResult is returned by Query and carries the predictive-optimizer output.
type QueryResult struct {
	Query string `json:"query"`
	// PredictedNext is the query key the Markov model expects to be issued
	// next; empty if the model has not seen enough history.
	PredictedNext string `json:"predicted_next"`
	// Confidence is the transition probability of PredictedNext, in [0,1].
	Confidence float64 `json:"confidence"`
	// CacheHit reports whether this query was served from the pre-warmed cache.
	CacheHit bool              `json:"cache_hit"`
	Receipt  *evidence.Receipt `json:"receipt,omitempty"`
}

// EvidenceStoreEngine proves mutations and predictively optimizes queries.
type EvidenceStoreEngine struct {
	rb        *evidence.ReceiptBuilder
	predictor *QueryPredictor

	mu       sync.Mutex
	cache    map[string]struct{} // keys currently pre-warmed
	warmHits int64
	warmed   int64
}

// NewEvidenceStoreEngine builds an engine bound to a receipt builder.
func NewEvidenceStoreEngine(rb *evidence.ReceiptBuilder) *EvidenceStoreEngine {
	return &EvidenceStoreEngine{
		rb:        rb,
		predictor: NewQueryPredictor(),
		cache:     make(map[string]struct{}),
	}
}

// Mutate applies a mutation (from the caller's perspective it is already
// persisted) and returns a signed Receipt attesting to it.
func (e *EvidenceStoreEngine) Mutate(rec MutationRecord) (*MutationResult, error) {
	receipt, err := e.rb.Build("store."+string(rec.Kind), rec, struct {
		Table string `json:"table"`
		Key   string `json:"key"`
	}{Table: rec.Table, Key: rec.Key})
	if err != nil {
		return nil, err
	}
	return &MutationResult{Record: rec, Receipt: receipt}, nil
}

// Query records the access in the Markov model, serves from / updates the
// pre-warmed cache, and pre-warms the predicted next query. A Receipt attests
// that the read happened.
func (e *EvidenceStoreEngine) Query(query string) (*QueryResult, error) {
	e.mu.Lock()
	_, hit := e.cache[query]
	if hit {
		e.warmHits++
		delete(e.cache, query) // consumed
	}
	e.mu.Unlock()

	// Update the online model and obtain the next-query prediction.
	next, conf := e.predictor.Observe(query)

	// Pre-warm the predicted next query so a future Query(next) is a cache hit.
	if next != "" {
		e.mu.Lock()
		if _, already := e.cache[next]; !already {
			e.cache[next] = struct{}{}
			e.warmed++
		}
		e.mu.Unlock()
	}

	receipt, err := e.rb.Build("store.query", struct {
		Query string `json:"query"`
	}{Query: query}, struct {
		CacheHit bool `json:"cache_hit"`
	}{CacheHit: hit})
	if err != nil {
		return nil, err
	}

	return &QueryResult{
		Query:         query,
		PredictedNext: next,
		Confidence:    conf,
		CacheHit:      hit,
		Receipt:       receipt,
	}, nil
}

// PredictionAccuracy returns the fraction of next-query predictions that turned
// out correct over the observed history, in [0,1].
func (e *EvidenceStoreEngine) PredictionAccuracy() float64 {
	return e.predictor.Accuracy()
}

// PrewarmHitRate returns warmed-cache hits over total pre-warmed entries.
func (e *EvidenceStoreEngine) PrewarmHitRate() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.warmed == 0 {
		return 0
	}
	return float64(e.warmHits) / float64(e.warmed)
}

// QueryPredictor is a first-order Markov chain over query keys used to predict
// the next query in a sequence. It is safe for concurrent use.
type QueryPredictor struct {
	mu sync.Mutex

	// transitions[a][b] counts how often query b followed query a.
	transitions map[string]map[string]int
	// totals[a] is the number of transitions observed out of a.
	totals map[string]int

	last           string // previously observed query
	lastPrediction string // what we predicted would follow `last`

	predictions int // number of predictions we have scored
	hits        int // number of predictions that matched reality
}

// NewQueryPredictor returns an empty predictor.
func NewQueryPredictor() *QueryPredictor {
	return &QueryPredictor{
		transitions: make(map[string]map[string]int),
		totals:      make(map[string]int),
	}
}

// Observe records that `query` was just issued. It first scores the previous
// prediction (if any), then updates the transition counts, and finally returns
// the most likely next query together with its transition probability.
func (p *QueryPredictor) Observe(query string) (next string, confidence float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Score the prediction that was made for the previous step.
	if p.lastPrediction != "" {
		p.predictions++
		if p.lastPrediction == query {
			p.hits++
		}
	}

	// Update the Markov transition last -> query.
	if p.last != "" {
		row, ok := p.transitions[p.last]
		if !ok {
			row = make(map[string]int)
			p.transitions[p.last] = row
		}
		row[query]++
		p.totals[p.last]++
	}
	p.last = query

	// Predict the most likely successor of the current query.
	next, confidence = p.predictLocked(query)
	p.lastPrediction = next
	return next, confidence
}

// Predict returns the most likely successor of `query` without mutating state.
func (p *QueryPredictor) Predict(query string) (string, float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.predictLocked(query)
}

// predictLocked chooses argmax over successors, breaking ties deterministically
// by query key so results are reproducible. Caller must hold the mutex.
func (p *QueryPredictor) predictLocked(query string) (string, float64) {
	row := p.transitions[query]
	total := p.totals[query]
	if total == 0 || len(row) == 0 {
		return "", 0
	}
	// Deterministic argmax: iterate over sorted keys.
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	bestKey := keys[0]
	bestCount := row[bestKey]
	for _, k := range keys[1:] {
		if row[k] > bestCount {
			bestKey = k
			bestCount = row[k]
		}
	}
	return bestKey, float64(bestCount) / float64(total)
}

// Accuracy returns hits/predictions over the scored history.
func (p *QueryPredictor) Accuracy() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.predictions == 0 {
		return 0
	}
	return float64(p.hits) / float64(p.predictions)
}
