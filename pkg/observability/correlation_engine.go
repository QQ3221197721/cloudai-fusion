package observability

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ============================================================================
// Cross-Module Correlation Engine
// ============================================================================
//
// This module analyzes events from multiple algorithm modules to identify
// root causes of system issues through cross-correlation.
//
// Key Features:
// - Multi-module event correlation (traces, anomalies, remediations, quantiles)
// - Temporal windowing for causal inference
// - Weighted ranking based on severity and timing
// - Natural language report generation

// TraceEvent represents a span or trace from M47 distributed tracing module
type TraceEvent struct {
	TraceID    string
	SpanID     string
	Service    string
	Operation  string
	LatencyMs  int64
	Errors     int64
	Status     string // "ok", "error", "timeout"
	Timestamp  time.Time
	Attributes map[string]interface{}
}

// AnomalyEvent represents drift or security anomaly from M29/M31 ML security
type AnomalyEvent struct {
	FeatureName   string
	PSIValue      float64 // Population Stability Index
	Severity      string  // "warning", "critical", "info"
	ModelVersion  string
	Threshold     float64
	Value         float64
	Description   string
	Category      string // "drift", "security", "performance"
	Timestamp     time.Time
	Metadata      map[string]interface{}
}

// RemediationEvent represents self-healing actions from M49 module
type RemediationEvent struct {
	FaultType       string
	ActionType      string
	DurationMs      int64
	Success         bool
	TraceID         string // Related trace ID if any
	RemediationID   string
	BeforeState     string
	AfterState      string
	Evidence        []string
	Timestamp       time.Time
}

// QuantileDeviation represents streaming percentile deviation from M9
type QuantileDeviation struct {
	Percentile  float64 // 0.5, 0.9, 0.99
	Value       float64
	Baseline    float64
	Deviation   float64 // percentage change from baseline
	Cause       string
	Time        time.Time
	Dimensions  map[string]string
}

// CorrelatedEvent represents an event correlated in root cause analysis
type CorrelatedEvent struct {
	Type           string
	Feature        string
	Action         string
	Success        bool
	Weight         float64 // importance weight [0,1]
	TimingOffset   time.Duration
	Description    func() string
	Metadata       map[string]interface{}
}

// RootCauseAnalysis result of correlating events for a specific issue
type RootCauseAnalysis struct {
	TraceID         string
	Events          []CorrelatedEvent
	Confidence      float64 // [0,1] how confident we are in this analysis
	Hypotheses      []string
	Recommendations []string
	GeneratedAt     time.Time
	Context         map[string]interface{}
}

// Store is an in-memory event store with TTL-based eviction
type TraceStore struct {
	mu     sync.RWMutex
	events []TraceEvent
	window time.Duration
	ttl    time.Duration
}

// NewTraceStore creates new trace event store
func NewTraceStore(window time.Duration) *TraceStore {
	return &TraceStore{
		events: make([]TraceEvent, 0),
		window: window,
		ttl:    window * 2,
	}
}

// Insert adds a trace event to the store
func (ts *TraceStore) Insert(event TraceEvent) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	event.Timestamp = time.Now()
	ts.events = append(ts.events, event)
	ts.cleanupOld()
}

// Query returns events within time window
func (ts *TraceStore) Query(start, end time.Time) []TraceEvent {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var results []TraceEvent
	for _, e := range ts.events {
		if !e.Timestamp.IsZero() && e.Timestamp.After(start) && e.Timestamp.Before(end) {
			results = append(results, e)
		}
	}
	return results
}

// Cleanup removes expired events
func (ts *TraceStore) Cleanup() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.cleanupOld()
}

func (ts *TraceStore) cleanupOld() {
	cutoff := time.Now().Add(-ts.ttl)
	valid := make([]TraceEvent, 0)
	for _, e := range ts.events {
		if e.Timestamp.After(cutoff) {
			valid = append(valid, e)
		}
	}
	ts.events = valid
}

func (ts *TraceStore) Size() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.events)
}

// AnomalyStore stores ML security anomalies
type AnomalyStore struct {
	mu     sync.RWMutex
	events []AnomalyEvent
	window time.Duration
}

// NewAnomalyStore creates new anomaly store
func NewAnomalyStore(window time.Duration) *AnomalyStore {
	return &AnomalyStore{
		events: make([]AnomalyEvent, 0),
		window: window,
	}
}

func (as *AnomalyStore) Insert(event AnomalyEvent) {
	as.mu.Lock()
	defer as.mu.Unlock()

	event.Timestamp = time.Now()
	as.events = append(as.events, event)

	// Keep only recent events
	limit := time.Now().Add(-as.window * 2)
	valid := make([]AnomalyEvent, 0)
	for _, e := range as.events {
		if e.Timestamp.After(limit) {
			valid = append(valid, e)
		}
	}
	as.events = valid
}

func (as *AnomalyStore) Query(start, end time.Time) []AnomalyEvent {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var results []AnomalyEvent
	for _, e := range as.events {
		if e.Timestamp.After(start) && e.Timestamp.Before(end) {
			results = append(results, e)
		}
	}
	return results
}

// RemediationStore stores healing actions
type RemediationStore struct {
	mu           sync.RWMutex
	events       []RemediationEvent
	hourWindow   time.Duration
}

// NewRemediationStore creates new remediation store
func NewRemediationStore(hourWindow time.Duration) *RemediationStore {
	return &RemediationStore{
		events:     make([]RemediationEvent, 0),
		hourWindow: hourWindow,
	}
}

func (rs *RemediationStore) Insert(event RemediationEvent) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	event.Timestamp = time.Now()
	rs.events = append(rs.events, event)

	// Prune old events (>6 hours)
	cutoff := time.Now().Add(-6 * time.Hour)
	valid := make([]RemediationEvent, 0)
	for _, e := range rs.events {
		if e.Timestamp.After(cutoff) {
			valid = append(valid, e)
		}
	}
	rs.events = valid
}

// QueryByTraceID finds remediations related to specific trace
func (rs *RemediationStore) QueryByTraceID(traceID string) []RemediationEvent {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if traceID == "" {
		return nil
	}

	var results []RemediationEvent
	for _, e := range rs.events {
		if e.TraceID == traceID {
			results = append(results, e)
		}
	}
	return results
}

// CorrelationEngine orchestrates cross-module correlation
type CorrelationEngine struct {
	traceStore         *TraceStore
	anomalyStore       *AnomalyStore
	remediationStore   *RemediationStore
	mu                 sync.RWMutex
	timeWindow         time.Duration
	maxResults         int
	enableAutoCleanup  bool
}

// CorrelationEngineConfig controls engine behavior
type CorrelationEngineConfig struct {
	TraceWindow       time.Duration
	AnomalyWindow     time.Duration
	RemediationWindow time.Duration
	MaxResults        int
}

// DefaultCorrelationEngineConfig returns sensible defaults
func DefaultCorrelationEngineConfig() CorrelationEngineConfig {
	return CorrelationEngineConfig{
		TraceWindow:       time.Minute,
		AnomalyWindow:     5 * time.Minute,
		RemediationWindow: time.Hour,
		MaxResults:        10,
	}
}

// NewCorrelationEngine creates new correlation engine with defaults
func NewCorrelationEngine() *CorrelationEngine {
	return NewCorrelationEngineWithConfig(DefaultCorrelationEngineConfig())
}

// NewCorrelationEngineWithConfig creates engine with custom config
func NewCorrelationEngineWithConfig(cfg CorrelationEngineConfig) *CorrelationEngine {

	ce := &CorrelationEngine{
		traceStore:       NewTraceStore(cfg.TraceWindow),
		anomalyStore:     NewAnomalyStore(cfg.AnomalyWindow),
		remediationStore: NewRemediationStore(cfg.RemediationWindow),

		timeWindow: cfg.TraceWindow,
		maxResults: cfg.MaxResults,

		enableAutoCleanup: true,
	}

	// Start background cleanup if enabled
	if ce.enableAutoCleanup {
		go ce.backgroundCleanup()
	}

	return ce
}

// backgroundCleanup runs periodic garbage collection
func (ce *CorrelationEngine) backgroundCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ce.traceStore.Cleanup()
	}
}

// RecordTrace ingests a trace event
func (ce *CorrelationEngine) RecordTrace(event TraceEvent) {
	ce.traceStore.Insert(event)
}

// RecordAnomaly ingests an ML security anomaly
func (ce *CorrelationEngine) RecordAnomaly(event AnomalyEvent) {
	ce.anomalyStore.Insert(event)
}

// RecordRemediation ingests a self-healing action
func (ce *CorrelationEngine) RecordRemediation(event RemediationEvent) {
	ce.remediationStore.Insert(event)
}

// AnalyzeRootCause performs comprehensive root cause analysis
func (ce *CorrelationEngine) AnalyzeRootCause(traceID string) RootCauseAnalysis {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-ce.timeWindow)

	// Gather related events
	traces := ce.traceStore.Query(windowStart, now)
	anomalies := ce.anomalyStore.Query(windowStart, now)
	remediations := ce.remediationStore.QueryByTraceID(traceID)

	// Build correlation graph and rank by likelihood
	events := ce.rankEvents(anomalies, remediations, traces)

	// Calculate overall confidence
	confidence := ce.calculateConfidence(events)

	// Generate hypotheses and recommendations
	hypotheses := ce.generateHypotheses(events)
	recommendations := ce.generateRecommendations(events, anomalies, remediations)

	return RootCauseAnalysis{
		TraceID:         traceID,
		Events:          events[:min(len(events), ce.maxResults)],
		Confidence:      confidence,
		Hypotheses:      hypotheses,
		Recommendations: recommendations,
		GeneratedAt:     now,
		Context: map[string]interface{}{
			"anomaly_count":   len(anomalies),
			"trace_count":     len(traces),
			"remediation_count": len(remediations),
		},
	}
}

// rankEvents scores and ranks all correlated events by importance
func (ce *CorrelationEngine) rankEvents(
	anomalies []AnomalyEvent,
	remediations []RemediationEvent,
	traces []TraceEvent,
) []CorrelatedEvent {

	var scores []CorrelatedEvent

	// Score critical anomalies heavily - they're likely root causes
	for _, anom := range anomalies {
		weight := 0.4 // Base weight
		if anom.Severity == "critical" {
			weight = 0.8
		} else if anom.Severity == "warning" {
			weight = 0.5
		}

		// Boost weight if PSI value is very high
		if anom.PSIValue > 0.25 {
			weight *= 1.2
		}

		score := CorrelatedEvent{
			Type:         "anomaly",
			Feature:      anom.FeatureName,
			Weight:       weight,
			TimingOffset: 0,
			Description: func(e AnomalyEvent) func() string {
				return func() string {
					return fmt.Sprintf("ML Drift: %s (PSI=%.2f, severity=%s)", 
						e.FeatureName, e.PSIValue, e.Severity)
				}
			}(anom),
			Metadata: anom.Metadata,
		}
		scores = append(scores, score)
	}

	// Score failed remediations as potential persistent issues
	for _, rem := range remediations {
		weight := 0.3 // Failed attempts suggest hard problem
		if rem.Success {
			weight = 0.6
		}

		score := CorrelatedEvent{
			Type:         "remediation",
			Action:       rem.ActionType,
			Success:      rem.Success,
			Weight:       weight,
			TimingOffset: 0,
			Description: func(r RemediationEvent) func() string {
				return func() string {
					status := "success"
					if !r.Success {
						status = "failed"
					}
					return fmt.Sprintf("Healing action: %s (%s)", r.ActionType, status)
				}
			}(rem),
			Metadata: map[string]interface{}{
				"fault_type":  rem.FaultType,
				"duration_ms": rem.DurationMs,
			},
		}
		scores = append(scores, score)
	}

	// Score high-latency error traces
	for _, trace := range traces {
		if trace.Errors > 0 || trace.Status == "error" {
			weight := 0.5
			if trace.LatencyMs > 5000 { // >5 seconds latency
				weight *= 1.2
			}

			score := CorrelatedEvent{
				Type:         "trace_error",
				Weight:       weight,
				TimingOffset: 0,
				Description: func(t TraceEvent) func() string {
					return func() string {
						return fmt.Sprintf("Error in service %s: %.0f ms latency, %d errors",
							t.Service, t.LatencyMs, t.Errors)
					}
				}(trace),
				Metadata: map[string]interface{}{
					"latency_ms": trace.LatencyMs,
					"service":    trace.Service,
				},
			}
			scores = append(scores, score)
		}
	}

	// Sort by weight descending (highest priority first)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Weight > scores[j].Weight
	})

	return scores
}

// calculateConfidence computes overall analysis confidence score
func (ce *CorrelationEngine) calculateConfidence(events []CorrelatedEvent) float64 {
	if len(events) == 0 {
		return 0.0
	}

	totalWeight := 0.0
	sumWeights := 0.0

	for _, e := range events {
		sumWeights += e.Weight
		totalWeight += e.Weight * e.Weight
	}

	if sumWeights == 0 {
		return 0.0
	}

	confidence := totalWeight / sumWeights

	// Cap at 0.95 max confidence
	if confidence > 0.95 {
		confidence = 0.95
	}

	return confidence
}

// generateHypotheses builds plausible explanations for observed issues
func (ce *CorrelationEngine) generateHypotheses(events []CorrelatedEvent) []string {
	var hypotheses []string

	hasCriticalAnomaly := false
	hasFailedRemediation := false
	hasRepeatedErrors := false

	for _, e := range events {
		switch e.Type {
		case "anomaly":
			if e.Weight > 0.7 {
				hasCriticalAnomaly = true
			}
		case "remediation":
			if !e.Success {
				hasFailedRemediation = true
			}
		case "trace_error":
			hasRepeatedErrors = true
		}
	}

	if hasCriticalAnomaly && hasFailedRemediation {
		hypotheses = append(hypotheses, 
			"Model drift caused service degradation; automated recovery insufficient")
	} else if hasCriticalAnomaly {
		hypotheses = append(hypotheses, 
			"Significant feature drift detected correlating with performance issues")
	} else if hasFailedRemediation {
		hypotheses = append(hypotheses, 
			"Persistent fault pattern preventing successful recovery")
	} else if hasRepeatedErrors {
		hypotheses = append(hypotheses, 
			"Intermittent service errors suggesting transient dependency failure")
	} else {
		hypotheses = append(hypotheses, 
			"Complex multi-factor issue requiring manual investigation")
	}

	return hypotheses
}

// generateRecommendations suggests next actions
func (ce *CorrelationEngine) generateRecommendations(
	events []CorrelatedEvent,
	anomalies []AnomalyEvent,
	remediations []RemediationEvent,
) []string {

	var recs []string

	for _, e := range events {
		if e.Type == "anomaly" && e.Weight > 0.7 {
			recs = append(recs, fmt.Sprintf("Investigate feature drift in %s - consider model retraining", e.Feature))
		}
	}

	for _, e := range events {
		if e.Type == "remediation" && !e.Success {
			recs = append(recs, "Manual intervention required - automated healing unsuccessful")
		}
	}

	if len(remediations) > 3 {
		recs = append(recs, "Review remediation strategies - multiple attempts detected")
	}

	if len(anomalies) > 2 {
		recs = append(recs, "Conduct root cause analysis on multiple drifted features")
	}

	recs = append(recs, "Enable enhanced logging for affected services")

	return recs
}

// String generates natural language report
func (a RootCauseAnalysis) String() string {
	sb := fmt.Sprintf("=== Root Cause Analysis Report ===\n")
	sb += fmt.Sprintf("Trace ID: %s\n", a.TraceID)
	sb += fmt.Sprintf("Generated: %s\n", a.GeneratedAt.Format(time.RFC3339))
	sb += fmt.Sprintf("Confidence: %.0f%%\n\n", a.Confidence*100)

	sb += "--- Top Correlated Events ---\n"
	for i, event := range a.Events {
		sb += fmt.Sprintf("%d. %s (weight: %.0f%%)\n", i+1, event.Description(), event.Weight*100)
	}

	sb += "\n--- Hypotheses ---\n"
	for i, hyp := range a.Hypotheses {
		sb += fmt.Sprintf("%d. %s\n", i+1, hyp)
	}

	sb += "\n--- Recommendations ---\n"
	for i, rec := range a.Recommendations {
		sb += fmt.Sprintf("%d. %s\n", i+1, rec)
	}

	return sb
}

// GetMetrics provides aggregate statistics about stored events
func (ce *CorrelationEngine) GetMetrics() map[string]int {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	traces := ce.traceStore.Size()
	anomalies := len(ce.anomalyStore.Query(time.Now().Add(-24*time.Hour), time.Now()))
	remediations := len(ce.remediationStore.QueryByTraceID(""))

	return map[string]int{
		"trace_events":      traces,
		"anomaly_events":    anomalies,
		"remediation_events": remediations,
	}
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ContextFromRequest extracts relevant context from HTTP request
func ContextFromRequest(ctx context.Context, traceID string) map[string]interface{} {
	result := map[string]interface{}{
		"trace_id": traceID,
	}

	if userAgent, ok := ctx.Value("user_agent").(string); ok {
		result["user_agent"] = userAgent
	}

	if clientIP, ok := ctx.Value("client_ip").(string); ok {
		result["client_ip"] = clientIP
	}

	return result
}