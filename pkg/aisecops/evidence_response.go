// evidence_response.go adds an evidence-native automated response layer on top
// of the aisecops orchestrator. Every automated response decision (isolate /
// block / remediate / escalate) is backed by a cryptographically signed
// Receipt, and — the independent innovation of this file — is gated by a
// Response Confidence Score that fuses the live threat signal with the
// historical success rate of comparable past responses. This prevents
// over-reaction: low-confidence, high-blast-radius actions are escalated to a
// human instead of being executed automatically.
package aisecops

import (
	"crypto/ed25519"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ResponseAction is the concrete automated action a decision may take.
type ResponseAction string

const (
	ActionIsolate   ResponseAction = "isolate"   // network-isolate a host/workload
	ActionBlock     ResponseAction = "block"     // block an IP / principal / indicator
	ActionRemediate ResponseAction = "remediate" // apply a remediation (patch, kill, quarantine)
	ActionEscalate  ResponseAction = "escalate"  // hand off to a human analyst
)

// ThreatSignal describes the live incident the response engine reacts to.
type ThreatSignal struct {
	IncidentID string  `json:"incident_id"`
	ThreatType string  `json:"threat_type"` // e.g. "ransomware", "bruteforce", "c2"
	Severity   float64 `json:"severity"`    // normalized detector severity in [0,1]
	Detection  float64 `json:"detection"`   // detector's own confidence in [0,1]
	// BlastRadius estimates how disruptive the action is (0 = harmless, 1 = takes
	// down a critical production asset). It is the over-reaction penalty term.
	BlastRadius   float64 `json:"blast_radius"`
	AssetCritical float64 `json:"asset_critical"` // criticality of the target asset in [0,1]
}

// ResponseDecision is the auditable output of the engine.
type ResponseDecision struct {
	IncidentID string         `json:"incident_id"`
	Action     ResponseAction `json:"action"`
	// AutoExecuted is true when the confidence cleared the auto-act threshold.
	// When false the action is downgraded to ActionEscalate.
	AutoExecuted bool    `json:"auto_executed"`
	Confidence   float64 `json:"confidence"`         // fused confidence in [0,1]
	HistRate     float64 `json:"historical_success"` // Wilson lower bound of past success
	Rationale    string  `json:"rationale"`
	DecidedAt    time.Time
	Receipt      *evidence.Receipt `json:"-"`
}

// responseStat is the running Bernoulli outcome tally for one (action,threat) key.
type responseStat struct {
	success int
	total   int
}

// EvidenceResponseEngine scores and proves automated response decisions.
type EvidenceResponseEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	// autoThreshold is the minimum fused confidence required to auto-execute
	// rather than escalate to a human.
	autoThreshold float64

	mu    sync.Mutex
	stats map[string]*responseStat // key: action|threatType
}

// NewEvidenceResponseEngine builds an engine signing under the "aisecops" module.
// autoThreshold defaults to 0.7 when a non-positive value is supplied.
func NewEvidenceResponseEngine(privKey ed25519.PrivateKey, autoThreshold float64) *EvidenceResponseEngine {
	if autoThreshold <= 0 || autoThreshold >= 1 {
		autoThreshold = 0.7
	}
	return &EvidenceResponseEngine{
		receiptBuilder: evidence.NewReceiptBuilder("aisecops", privKey),
		autoThreshold:  autoThreshold,
		stats:          make(map[string]*responseStat),
	}
}

func statKey(a ResponseAction, threatType string) string { return string(a) + "|" + threatType }

// RecordOutcome feeds back the real-world result of a previously taken action so
// the confidence model learns. This is what makes the score adaptive rather than
// a fixed heuristic.
func (e *EvidenceResponseEngine) RecordOutcome(a ResponseAction, threatType string, success bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := statKey(a, threatType)
	s := e.stats[k]
	if s == nil {
		s = &responseStat{}
		e.stats[k] = s
	}
	s.total++
	if success {
		s.success++
	}
}

// wilsonLowerBound returns the lower bound of the Wilson score interval for a
// Bernoulli proportion at ~95% confidence (z=1.96). With zero observations it
// returns a neutral prior of 0.5. Using the lower bound (rather than the raw
// ratio) is deliberately pessimistic: a 1/1 success does not yet justify full
// trust, so the engine won't over-trust actions it has barely tried.
func wilsonLowerBound(success, total int) float64 {
	if total == 0 {
		return 0.5
	}
	const z = 1.96
	n := float64(total)
	phat := float64(success) / n
	z2 := z * z
	denom := 1 + z2/n
	centre := phat + z2/(2*n)
	margin := z * math.Sqrt((phat*(1-phat)+z2/(4*n))/n)
	lb := (centre - margin) / denom
	if lb < 0 {
		return 0
	}
	return lb
}

func (e *EvidenceResponseEngine) historicalRate(a ResponseAction, threatType string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.stats[statKey(a, threatType)]
	if s == nil {
		return wilsonLowerBound(0, 0)
	}
	return wilsonLowerBound(s.success, s.total)
}

// clamp01 keeps x within [0,1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// sigmoid is the logistic squashing function used by the scoring model.
func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// scoreConfidence fuses the live signal with historical performance through a
// small logistic model. Positive-weighted features (detector confidence,
// severity, historical success) raise confidence; the blast-radius term is
// weighted negatively so disruptive actions must clear a higher evidential bar.
func scoreConfidence(sig ThreatSignal, histRate float64) float64 {
	// Logistic weights tuned so a strong, well-proven, low-blast action lands
	// near ~0.9 and a weak, unproven, high-blast action lands near ~0.2.
	const (
		bias      = -1.4
		wHist     = 3.2
		wDetect   = 2.4
		wSeverity = 1.1
		wBlast    = -2.6
		wAsset    = 0.6
	)
	z := bias +
		wHist*(clamp01(histRate)-0.5) +
		wDetect*(clamp01(sig.Detection)-0.5) +
		wSeverity*(clamp01(sig.Severity)-0.5) +
		wBlast*clamp01(sig.BlastRadius) +
		wAsset*(clamp01(sig.AssetCritical)-0.5)
	return sigmoid(z)
}

// Decide is the core operation: it scores the requested action, decides whether
// to auto-execute or escalate, and returns a signed, verifiable receipt.
func (e *EvidenceResponseEngine) Decide(requested ResponseAction, sig ThreatSignal) (*ResponseDecision, error) {
	if requested == "" {
		return nil, fmt.Errorf("aisecops: response action is required")
	}
	histRate := e.historicalRate(requested, sig.ThreatType)
	conf := scoreConfidence(sig, histRate)

	dec := &ResponseDecision{
		IncidentID: sig.IncidentID,
		Action:     requested,
		Confidence: conf,
		HistRate:   histRate,
		DecidedAt:  time.Now().UTC(),
	}

	if conf >= e.autoThreshold {
		dec.AutoExecuted = true
		dec.Rationale = fmt.Sprintf("confidence %.3f >= threshold %.2f: auto-executing %s (hist=%.3f)",
			conf, e.autoThreshold, requested, histRate)
	} else {
		// Over-reaction guard: downgrade to human escalation.
		dec.Action = ActionEscalate
		dec.AutoExecuted = false
		dec.Rationale = fmt.Sprintf("confidence %.3f < threshold %.2f: escalating requested %s to human (hist=%.3f, blast=%.2f)",
			conf, e.autoThreshold, requested, histRate, sig.BlastRadius)
	}

	receipt, err := e.receiptBuilder.Build("response_decision", sig, dec)
	if err != nil {
		return nil, fmt.Errorf("aisecops: build receipt: %w", err)
	}
	dec.Receipt = receipt
	return dec, nil
}

// PolicySnapshot is a deterministic, sorted view of the learned success rates,
// useful for dashboards and offline audit of why the engine behaves as it does.
type PolicySnapshot struct {
	Key         string  `json:"key"`
	Total       int     `json:"total"`
	Success     int     `json:"success"`
	WilsonLower float64 `json:"wilson_lower"`
}

// Snapshot returns the current learned policy sorted by key for determinism.
func (e *EvidenceResponseEngine) Snapshot() []PolicySnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]PolicySnapshot, 0, len(e.stats))
	for k, s := range e.stats {
		out = append(out, PolicySnapshot{
			Key:         k,
			Total:       s.total,
			Success:     s.success,
			WilsonLower: wilsonLowerBound(s.success, s.total),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
