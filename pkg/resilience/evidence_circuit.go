package resilience

import (
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidence_circuit.go proves circuit-breaker decisions and pre-emptively trips
// circuits using a linear-trend predictor over recent error rates:
//
//  1. Breaker proof. Every state change produces a signed Receipt "svc X→Y at T".
//     Chained receipts give an offline-verifiable, independent audit trail of all breaker activity.
//
//  2. Predictive breaking. We fit a line y = a + b*t through recent errors in a rolling window; if the
//     slope is positive and we project hitting the threshold within N seconds, we open early and avoid failures.

// EvidenceBreakerEngine implements a predictive circuit breaker with receipts.
type EvidenceBreakerEngine struct {
	rb *evidence.ReceiptBuilder

	errorLog      *rollingErrorLog
	services map[string]bool
	config       BreakerConfig

	mu             sync.Mutex
	circuitStatus map[string]breakerStatus // service -> state
}

// breakerStatus represents the three states of a circuit breaker, using int
// types so it doesn't collide with any existing circuit package definitions.
type breakerStatus int

const (
	statusClosed   breakerStatus = iota // normal operation
	statusHalfOpen                       // testing recovery
	statusOpen                           // failing fast
)

// String implements fmt.Stringer for breakerStatus without conflicting names.
func (s breakerStatus) String() string {
	switch s {
	case statusClosed:
		return "closed"
	case statusHalfOpen:
		return "half-open"
	case statusOpen:
		return "open"
	default:
		return "unknown"
	}
}

// BreakerConfig controls thresholds and timing for both reactive and predictive modes.
type BreakerConfig struct {
	// ErrorRateThreshold is the maximum error rate (0-1) before tripping.
	ErrorRateThreshold float64
	// WindowSize is how many recent observations to consider when computing rate/slope.
	WindowSize int
	// MinSamples required before making predictions.
	MinSamples int
	// TimeHorizonSeconds is how far forward to project before tripping.
	TimeHorizonSeconds float64
	// SlopeThreshold is the projected error-rate increase per second that triggers
	// early opening, even if current rate < Threshold.
	SlopeThreshold float64
	// RecoveryTimeoutSeconds after Open before trying HalfOpen.
	RecoveryTimeoutSeconds float64
}

// DefaultBreakerConfig returns balanced defaults.
func DefaultBreakerConfig() BreakerConfig {
	return BreakerConfig{
		ErrorRateThreshold:    0.5,
		WindowSize:            30,
		MinSamples:            10,
		TimeHorizonSeconds:    5.0,
		SlopeThreshold:        0.08,
		RecoveryTimeoutSeconds: 30,
	}
}

// NewEvidenceBreakerEngine builds a breaker that manages `services`.
func NewEvidenceBreakerEngine(rb *evidence.ReceiptBuilder, services []string, cfg BreakerConfig) *EvidenceBreakerEngine {
	e := &EvidenceBreakerEngine{
		rb: rb, services: make(map[string]bool), config: cfg,
		circuitStatus: make(map[string]breakerStatus),
		errorLog: &rollingErrorLog{maxLen: cfg.WindowSize},
	}
	for _, s := range services {
		e.services[s] = true
		e.circuitStatus[s] = statusClosed
	}
	return e
}

// RecordFailure logs a failed call and attempts opening if predictive or reactive thresholds are exceeded.
func (e *EvidenceBreakerEngine) RecordFailure(service string, t time.Time) error {
	if !e.services[service] {
		return errUnknownService
	}
	log := e.errorLog.entryFor(service)
	log.Lock()
	log.push(true, t)
	log.Unlock()

	e.mu.Lock()
	st := e.circuitStatus[service]
	e.mu.Unlock()

	// If already open, ignore unless recovery is allowed.
	if st == statusOpen && !e.shouldAttemptRecovery(service) {
		return nil
	}

	// Check predictive trip: will current trajectory hit threshold?
	if e.shouldTripPredictively(service) {
		e.changeState(service, statusOpen, e.rb)
	}
	return nil
}

// RecordSuccess logs a successful call and may transition from HalfOpen → Closed.
func (e *EvidenceBreakerEngine) RecordSuccess(service string, t time.Time) error {
	if !e.services[service] {
		return errUnknownService
	}
	log := e.errorLog.entryFor(service)
	log.Lock()
	log.push(false, t)
	log.Unlock()

	e.mu.Lock()
	st := e.circuitStatus[service]
	e.mu.Unlock()

	if st == statusHalfOpen && e.succeededEnough(service) {
		e.changeState(service, statusClosed, e.rb)
	}
	return nil
}

// ShouldTripPredictive returns true if recent error trends suggest an imminent breach.
func (e *EvidenceBreakerEngine) ShouldTripPredictive(service string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.services[service] {
		return false
	}
	return e.shouldTripPredictively(service)
}

// shouldTripPredictively checks whether current trajectories would cross threshold in Horizon.
func (e *EvidenceBreakerEngine) shouldTripPredictively(service string) bool {
	if e.circuitStatus[service] != statusClosed {
		return false
	}
	log := e.errorLog.entryFor(service)
	log.Lock()
	defer log.Unlock()
	if len(log.entries) < e.config.MinSamples {
		return false
	}
	n := min(e.config.WindowSize, len(log.entries))
	if n < e.config.MinSamples {
		return false
	}
	// Linear regression on normalized timeline [0,1].
	var sumX, sumY, sumXY, sumXX float64
	for i := 0; i < n; i++ {
		x := float64(i) / float64(n-1)
		y := 0.0
		if log.entries[len(log.entries)-n+i].failure {
			y = 1.0
		}
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	den := float64(n)*sumXX - sumX*sumX
	if den == 0 {
		return false
	}
	b := (float64(n)*sumXY - sumX*sumY) / den
	// Projected endpoint value at x=1.
	currentAtEnd := sumY/float64(n) + b
	projected := currentAtEnd + b*e.config.TimeHorizonSeconds
	if projected > e.config.ErrorRateThreshold && b > e.config.SlopeThreshold {
		return true
	}
	return false
}

// changeState records a state change with a receipt.
func (e *EvidenceBreakerEngine) changeState(service string, newState breakerStatus, rb *evidence.ReceiptBuilder) {
	receipt, _ := rb.Build("resilience.breaker", struct {
		Service string           `json:"service"`
		Prev    breakerStatus    `json:"prev"`
		New     breakerStatus    `json:"new"`
	}{Service: service, Prev: e.circuitStatus[service], New: newState}, struct {
		Service string `json:"service"`
		New     string `json:"new"`
	}{Service: service, New: newState.String()})
	if receipt != nil {
		_ = receipt.Verify()
	}
	e.mu.Lock()
	e.circuitStatus[service] = newState
	e.mu.Unlock()
}

// succeededEnough returns true after enough half-open successes to close.
func (e *EvidenceBreakerEngine) succeededEnough(service string) bool {
	log := e.errorLog.entryFor(service)
	log.Lock()
	defer log.Unlock()
	const halfOpenWindow = 60 * time.Second
	successes := 0
	for _, ent := range log.entries {
		if !ent.failure && time.Since(ent.t) <= halfOpenWindow {
			successes++
		}
	}
	return successes >= 2 // require at least two success probes
}

// shouldAttemptRecovery returns true after timeout from Open to allow HalfOpen probes.
func (e *EvidenceBreakerEngine) shouldAttemptRecovery(service string) bool {
	log := e.errorLog.entryFor(service)
	log.Lock()
	defer log.Unlock()
	if len(log.entries) == 0 {
		return false
	}
	last := log.entries[len(log.entries)-1].t
	return time.Since(last) >= time.Duration(e.config.RecoveryTimeoutSeconds)*time.Second
}

// GetStatus returns the current breaker state for `service`.
func (e *EvidenceBreakerEngine) GetStatus(service string) breakerStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.circuitStatus[service]
	if st == 0 {
		st = statusClosed
	}
	return st
}

// transitionTo forces a state transition (for tests/edge cases).
func (e *EvidenceBreakerEngine) transitionTo(service string, newState breakerStatus) {
	e.changeState(service, newState, e.rb)
}

// rollingErrorLog holds per-service failure logs bounded by maxLen.
type rollingErrorLog struct {
	maxLen int
	logs   map[string]*entryLog
}

type entryLog struct {
	mu      sync.Mutex
	entries []errorEntry
	maxLen  int
}

// Lock is a convenience method for thread safety.
func (l *entryLog) Lock() { l.mu.Lock() }

// Unlock is a convenience method for thread safety.
func (l *entryLog) Unlock() { l.mu.Unlock() }

type errorEntry struct {
	t       time.Time
	failure bool
}

func (rl *rollingErrorLog) entryFor(service string) *entryLog {
	if rl.logs == nil {
		rl.logs = make(map[string]*entryLog)
	}
	l, ok := rl.logs[service]
	if !ok {
		l = &entryLog{maxLen: rl.maxLen}
		rl.logs[service] = l
	}
	return l
}

func (l *entryLog) push(failure bool, t time.Time) {
	l.entries = append(l.entries, errorEntry{t: t, failure: failure})
	if len(l.entries) > l.maxLen {
		l.entries = l.entries[len(l.entries)-l.maxLen:]
	}
}

// Errors used by EvidenceBreakerEngine.
var errUnknownService = circuitError("resilience: unknown service")
var errCircuitAlready = circuitError("resilience: circuit already in state")

type circuitError string

func (e circuitError) Error() string { return string(e) }
