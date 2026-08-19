package detect

// evidence_detection.go layers two independent barriers over raw rule
// detection:
//
//  1. Evidence-native barrier — every detection decision is sealed into a
//     signed, offline-verifiable evidence.Receipt. Competitors emit logs that
//     can be edited after the fact; we emit an unforgeable Ed25519 attestation
//     over (input event hash, output decision hash).
//
//  2. Independent-innovation barrier — an AdaptiveThresholdEngine learns a
//     per-metric baseline online (exponential moving average of mean and
//     variance) and only lets a rule fire when the observed value deviates
//     beyond mean + sensitivity*stddev. Values inside the learned envelope are
//     suppressed as false positives, cutting alert fatigue without any static
//     tuning.

import (
	"crypto/ed25519"
	"math"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceDetectionEngine wraps detection with receipts + adaptive thresholds.
type EvidenceDetectionEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	adaptive       *AdaptiveThresholdEngine
}

// NewEvidenceDetectionEngine builds an engine signing with the supplied key and
// a fresh 3-sigma adaptive threshold learner.
func NewEvidenceDetectionEngine(privKey ed25519.PrivateKey) *EvidenceDetectionEngine {
	return &EvidenceDetectionEngine{
		receiptBuilder: evidence.NewReceiptBuilder("detect", privKey),
		adaptive:       NewAdaptiveThresholdEngine(3.0),
	}
}

// DetectionResult contains the outcome + evidence.
type DetectionResult struct {
	Triggered  bool
	RuleID     string
	Suppressed bool // true = adaptive threshold suppressed a false positive
	Timestamp  time.Time
	Receipt    *evidence.Receipt
}

// Detect evaluates an event, applies the adaptive threshold, and returns a
// signed proof of the decision.
//
// Recognised event keys:
//   - "rule_id" (string): identifier of the matched detection rule
//   - "metric"  (string): baseline key the value belongs to (falls back to rule_id)
//   - "value"   (number): the observed metric value the adaptive engine judges
//
// When a numeric value is present the adaptive engine decides whether the value
// is anomalous: anomalies are Triggered, in-baseline values are Suppressed. When
// no numeric value is present a matching rule fires directly.
func (e *EvidenceDetectionEngine) Detect(event map[string]interface{}) (*DetectionResult, error) {
	ruleID, _ := event["rule_id"].(string)
	metric, _ := event["metric"].(string)
	if metric == "" {
		metric = ruleID
	}

	result := &DetectionResult{RuleID: ruleID, Timestamp: time.Now()}

	if value, ok := toFloat(event["value"]); ok {
		anomaly := e.adaptive.Observe(metric, value)
		result.Triggered = anomaly
		result.Suppressed = !anomaly
	} else {
		// No numeric signal to score — a matched rule fires on its own.
		result.Triggered = ruleID != ""
	}

	output := map[string]interface{}{
		"rule_id":    result.RuleID,
		"triggered":  result.Triggered,
		"suppressed": result.Suppressed,
	}
	receipt, err := e.receiptBuilder.Build("detect", event, output)
	if err != nil {
		return nil, err
	}
	result.Receipt = receipt
	return result, nil
}

// toFloat coerces common numeric representations to float64.
func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// AdaptiveThresholdEngine (INNOVATION) learns a baseline per metric using an
// exponential moving average and only reports values beyond a sensitivity-scaled
// standard deviation as anomalies.
type AdaptiveThresholdEngine struct {
	mu          sync.RWMutex
	baselines   map[string]*MovingStats
	sensitivity float64 // default 3.0 (3-sigma)
}

// NewAdaptiveThresholdEngine creates an engine with the given sensitivity in
// standard deviations (values <= 0 default to 3.0).
func NewAdaptiveThresholdEngine(sensitivity float64) *AdaptiveThresholdEngine {
	if sensitivity <= 0 {
		sensitivity = 3.0
	}
	return &AdaptiveThresholdEngine{
		baselines:   make(map[string]*MovingStats),
		sensitivity: sensitivity,
	}
}

// Observe scores value against the metric's learned baseline (returning whether
// it is anomalous) and then folds value into the baseline. Scoring happens
// before the update so a spike is judged against history, not itself.
func (a *AdaptiveThresholdEngine) Observe(metric string, value float64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	stats := a.baselines[metric]
	if stats == nil {
		stats = &MovingStats{Alpha: 0.1}
		a.baselines[metric] = stats
	}
	anomaly := stats.IsAnomaly(value, a.sensitivity)
	stats.Update(value)
	return anomaly
}

// MovingStats holds the online EMA estimate of a metric's mean and variance.
type MovingStats struct {
	Mean     float64
	Variance float64
	Count    int64
	Alpha    float64 // EMA decay (default 0.1)
}

// Update folds a new observation into the EMA mean/variance estimate.
func (s *MovingStats) Update(value float64) {
	if s.Alpha == 0 {
		s.Alpha = 0.1
	}
	s.Count++
	if s.Count == 1 {
		s.Mean = value
		s.Variance = 0
		return
	}
	diff := value - s.Mean
	s.Mean += s.Alpha * diff
	s.Variance = (1 - s.Alpha) * (s.Variance + s.Alpha*diff*diff)
}

// IsAnomaly reports whether value is more than sensitivity standard deviations
// from the learned mean. The first observations form a learning period during
// which nothing is flagged.
func (s *MovingStats) IsAnomaly(value float64, sensitivity float64) bool {
	if s.Count < 10 { // need learning period
		return false
	}
	stddev := math.Sqrt(s.Variance)
	return math.Abs(value-s.Mean) > sensitivity*stddev
}
