package tracing

// evidence_trace.go layers two independent barriers over distributed tracing:
//
//  1. Evidence-native barrier — each trace completion is sealed into a signed,
//     offline-verifiable evidence.Receipt binding (traceID, path, spans) to the
//     measured total latency. We can prove "trace T on path P completed at time
//     X with total latency L".
//
//  2. Independent-innovation barrier — latency fingerprinting builds a per-path
//     statistical fingerprint (running mean + stddev via Welford). A new trace
//     is flagged anomalous when its total latency deviates from the fingerprint
//     by more than k standard deviations (a z-score test), giving early warning
//     of degradation or attack patterns that reshape latency profiles.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"math"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TraceResult is the verifiable result of completing one distributed trace.
type TraceResult struct {
	TraceID      string            `json:"trace_id"`
	Path         string            `json:"path"`
	SpanCount    int               `json:"span_count"`
	TotalLatency float64           `json:"total_latency"`
	ZScore       float64           `json:"z_score"`
	Anomalous    bool              `json:"anomalous"`
	Receipt      *evidence.Receipt `json:"receipt,omitempty"`
}

// LatencyFingerprint summarizes the historical latency profile for a path,
// maintained online with Welford's algorithm.
type LatencyFingerprint struct {
	Path   string  `json:"path"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
	Count  int     `json:"count"`

	m2 float64 // running sum of squared deviations (Welford)
}

// EvidenceTracingEngine seals trace completions and maintains latency fingerprints.
type EvidenceTracingEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	anomalyStdDevs float64 // z-score threshold for anomaly flag

	mu    sync.Mutex
	paths map[string]*LatencyFingerprint
}

// NewEvidenceTracingEngine builds an engine with a freshly generated key.
func NewEvidenceTracingEngine() *EvidenceTracingEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceTracingEngine{
		receiptBuilder: evidence.NewReceiptBuilder("tracing", priv),
		anomalyStdDevs: 3.0,
		paths:          make(map[string]*LatencyFingerprint),
	}
}

// CompleteTrace records trace completion, tests the total latency against the
// path's established fingerprint (before folding it in), updates the
// fingerprint, and returns a signed receipt.
func (e *EvidenceTracingEngine) CompleteTrace(traceID, path string, spanLatencies []float64) (*TraceResult, error) {
	if traceID == "" || path == "" {
		return nil, fmt.Errorf("tracing: traceID and path must not be empty")
	}
	total := sumFloats(spanLatencies)

	e.mu.Lock()
	fp := e.ensureFingerprint(path)
	z, anomalous := e.scoreLocked(fp, total)
	e.updateFingerprint(fp, total)
	e.mu.Unlock()

	result := &TraceResult{
		TraceID:      traceID,
		Path:         path,
		SpanCount:    len(spanLatencies),
		TotalLatency: total,
		ZScore:       z,
		Anomalous:    anomalous,
	}
	input := struct {
		TraceID   string  `json:"trace_id"`
		Path      string  `json:"path"`
		SpanCount int     `json:"span_count"`
		Total     float64 `json:"total"`
	}{traceID, path, len(spanLatencies), total}
	receipt, err := e.receiptBuilder.Build("tracing.complete", input, result)
	if err != nil {
		return nil, fmt.Errorf("tracing: seal trace: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

// Fingerprint returns a copy of the current fingerprint for a path.
func (e *EvidenceTracingEngine) Fingerprint(path string) (LatencyFingerprint, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fp, ok := e.paths[path]
	if !ok {
		return LatencyFingerprint{}, false
	}
	return *fp, true
}

// ---------------------------------------------------------------------------
// INNOVATION: latency-fingerprint anomaly scoring
// ---------------------------------------------------------------------------

// scoreLocked computes the z-score of value against the fingerprint's current
// baseline and whether it exceeds the anomaly threshold. Paths with fewer than
// 4 established samples (or zero variance) never fire, avoiding cold-start noise.
// Caller holds e.mu.
func (e *EvidenceTracingEngine) scoreLocked(fp *LatencyFingerprint, value float64) (z float64, anomalous bool) {
	if fp.Count < 4 || fp.StdDev <= 0 {
		return 0, false
	}
	z = (value - fp.Mean) / fp.StdDev
	return z, math.Abs(z) > e.anomalyStdDevs
}

func (e *EvidenceTracingEngine) ensureFingerprint(path string) *LatencyFingerprint {
	if _, ok := e.paths[path]; !ok {
		e.paths[path] = &LatencyFingerprint{Path: path}
	}
	return e.paths[path]
}

// updateFingerprint folds value into the running mean/variance using Welford's
// online algorithm, then materializes the population stddev. Caller holds e.mu.
func (e *EvidenceTracingEngine) updateFingerprint(fp *LatencyFingerprint, value float64) {
	fp.Count++
	delta := value - fp.Mean
	fp.Mean += delta / float64(fp.Count)
	delta2 := value - fp.Mean
	fp.m2 += delta * delta2
	if fp.Count > 1 {
		fp.StdDev = math.Sqrt(fp.m2 / float64(fp.Count))
	}
}

func sumFloats(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s
}
