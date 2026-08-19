package mesh

import (
	"math"
	"sort"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidence_routing.go provides two capabilities on top of the service mesh:
//
//  1. Call proof. Every service-to-service call produces a signed Receipt
//     attesting "A called B at time T with latency L". This is an independent,
//     cryptographic record that does not depend on trusting tracing backends.
//
//  2. Anomaly-aware routing. Per-callee latency is tracked with exponential
//     smoothing (an EMA of latency plus an EMA of squared deviation). A sudden
//     trend change produces a high anomaly score, and the router steers traffic
//     AWAY from anomalous callees before a circuit breaker would ever trip.

// CallResult is returned by RecordCall and carries the delivery proof plus the
// freshly computed anomaly score for the callee.
type CallResult struct {
	Caller    string            `json:"caller"`
	Callee    string            `json:"callee"`
	LatencyMs int               `json:"latency_ms"`
	Anomaly   float64           `json:"anomaly"`
	Receipt   *evidence.Receipt `json:"receipt"`
}

// RouteDecision is returned by Route: the elected callee and why.
type RouteDecision struct {
	Callee  string  `json:"callee"`
	Anomaly float64 `json:"anomaly"`
	AvgMs   float64 `json:"avg_ms"`
}

// EvidenceRoutingEngine proves calls and performs anomaly-aware routing.
type EvidenceRoutingEngine struct {
	rb    *evidence.ReceiptBuilder
	alpha float64

	mu       sync.Mutex
	trackers map[string]*serviceTracker
}

// NewEvidenceRoutingEngine builds an engine. alpha is the EMA smoothing factor
// in (0,1]; larger values weight recent samples more heavily. If alpha is out of
// range it defaults to 0.3.
func NewEvidenceRoutingEngine(rb *evidence.ReceiptBuilder, alpha float64) *EvidenceRoutingEngine {
	if alpha <= 0 || alpha > 1 {
		alpha = 0.3
	}
	return &EvidenceRoutingEngine{
		rb:       rb,
		alpha:    alpha,
		trackers: make(map[string]*serviceTracker),
	}
}

// RecordCall ingests one observed call and returns a Receipt proving it, along
// with the callee's updated anomaly score.
func (e *EvidenceRoutingEngine) RecordCall(caller, callee string, latencyMs int) (*CallResult, error) {
	e.mu.Lock()
	tr := e.trackers[callee]
	if tr == nil {
		tr = &serviceTracker{}
		e.trackers[callee] = tr
	}
	anomaly := tr.observe(float64(latencyMs), e.alpha)
	e.mu.Unlock()

	receipt, err := e.rb.Build("mesh.call", struct {
		Caller string `json:"caller"`
		Callee string `json:"callee"`
	}{Caller: caller, Callee: callee}, struct {
		LatencyMs int     `json:"latency_ms"`
		Anomaly   float64 `json:"anomaly"`
	}{LatencyMs: latencyMs, Anomaly: anomaly})
	if err != nil {
		return nil, err
	}

	return &CallResult{
		Caller:    caller,
		Callee:    callee,
		LatencyMs: latencyMs,
		Anomaly:   anomaly,
		Receipt:   receipt,
	}, nil
}

// Anomaly returns the current anomaly score for a callee, in [0,1].
func (e *EvidenceRoutingEngine) Anomaly(callee string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if tr := e.trackers[callee]; tr != nil {
		return tr.anomaly
	}
	return 0
}

// Route chooses the best callee among candidates: lowest anomaly first, then
// lowest average latency. Unknown candidates get a neutral (zero) anomaly and a
// large latency prior so proven-good callees win.
func (e *EvidenceRoutingEngine) Route(candidates []string) (*RouteDecision, error) {
	if len(candidates) == 0 {
		return nil, errNoCandidates
	}
	e.mu.Lock()
	type scored struct {
		name    string
		anomaly float64
		avg     float64
	}
	scoredList := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		s := scored{name: c, anomaly: 0, avg: math.MaxFloat64}
		if tr := e.trackers[c]; tr != nil {
			s.anomaly = tr.anomaly
			s.avg = tr.emaLatency
		}
		scoredList = append(scoredList, s)
	}
	e.mu.Unlock()

	sort.SliceStable(scoredList, func(i, j int) bool {
		if scoredList[i].anomaly != scoredList[j].anomaly {
			return scoredList[i].anomaly < scoredList[j].anomaly
		}
		return scoredList[i].avg < scoredList[j].avg
	})
	best := scoredList[0]
	return &RouteDecision{Callee: best.name, Anomaly: best.anomaly, AvgMs: best.avg}, nil
}

// serviceTracker maintains exponential-smoothing state for a single callee.
type serviceTracker struct {
	samples    int
	emaLatency float64 // EMA of latency
	emaVar     float64 // EMA of squared deviation from the previous mean
	anomaly    float64 // last computed anomaly score in [0,1]
}

// observe applies one latency sample and returns the anomaly score. The score
// is |sample - prevEMA| / (3·std), clamped to [0,1]: a change larger than three
// smoothed standard deviations reads as a full-blown anomaly.
func (t *serviceTracker) observe(v, alpha float64) float64 {
	if t.samples == 0 {
		t.emaLatency = v
		t.emaVar = 0
		t.samples = 1
		t.anomaly = 0
		return 0
	}
	prev := t.emaLatency
	dev := v - prev

	// Update the smoothed mean and the smoothed squared deviation.
	t.emaLatency = alpha*v + (1-alpha)*prev
	t.emaVar = alpha*dev*dev + (1-alpha)*t.emaVar

	std := math.Sqrt(t.emaVar)
	if std < 1e-9 {
		std = 1
	}
	score := math.Abs(dev) / (3 * std)
	if score > 1 {
		score = 1
	}
	t.samples++
	t.anomaly = score
	return score
}

// errNoCandidates is returned when Route is called with an empty candidate set.
var errNoCandidates = meshRoutingError("mesh: no routing candidates")

type meshRoutingError string

func (e meshRoutingError) Error() string { return string(e) }
