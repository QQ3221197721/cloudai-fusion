package observability

// ============================================================================
// M18 — Tracing Optimizer (训练追踪优化)
//
// M18 is an UPPER-LAYER optimizer built on top of the M47 distributed-tracing
// core (the W3C SpanContext / Span / SpanStorage / Sampler primitives defined
// in tracing.go). It is intentionally NOT a second tracing implementation.
//
// M47 performs HEAD-based sampling: the keep/drop decision is made at span
// start (HeadBasedSampler, ForcedSampler, and the OTel adaptive sampler in
// pkg/tracing). Head sampling is blind to how a trace ends — which is exactly
// the wrong tradeoff for AI training workloads:
//
//   * A training run's value is only known after it completes (did it diverge,
//     error, or run abnormally slow?). Head sampling cannot retain the failing
//     runs you actually need to debug.
//   * Training loops emit thousands of near-identical per-step spans, exploding
//     trace cardinality and storage cost with almost no diagnostic value.
//
// M18 fills these gaps with three capabilities that operate AFTER a trace has
// finished, complementing (not duplicating) M47:
//
//   1. Tail-based sampling  — retain traces that contain errors, exceed a
//      latency threshold, or carry an "important" attribute; probabilistically
//      keep the rest. Anomalous chains are ALWAYS preserved.
//   2. Span aggregation denoising — collapse structurally-identical repetitive
//      sibling spans into a single representative span carrying count + latency
//      statistics, drastically reducing per-step noise. Non-OK spans are never
//      collapsed so anomalies stay individually visible.
//   3. Adaptive budget control — keep the retained span volume under a target
//      spans/sec budget by tightening/relaxing the probabilistic keep-rate,
//      while error/latency/attribute retention is exempt from the budget.
// ============================================================================

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"
)

// TraceRetentionPolicy identifies which tail-sampling policy decided a trace's fate.
type TraceRetentionPolicy string

const (
	// PolicyError keeps traces that contain at least one error span.
	PolicyError TraceRetentionPolicy = "error"
	// PolicyLatency keeps traces whose duration exceeds LatencyThreshold.
	PolicyLatency TraceRetentionPolicy = "latency"
	// PolicyImportantAttr keeps traces carrying a configured important attribute.
	PolicyImportantAttr TraceRetentionPolicy = "important_attribute"
	// PolicyProbabilistic keeps/drops a normal trace based on the effective rate.
	PolicyProbabilistic TraceRetentionPolicy = "probabilistic"
	// PolicyEmpty is returned for traces with no spans.
	PolicyEmpty TraceRetentionPolicy = "empty"
)

// TraceOptimizerConfig configures the M18 tracing optimizer.
type TraceOptimizerConfig struct {
	// BaseSampleRate is the initial probabilistic keep-rate for normal traces
	// (those without errors / high latency / important attributes). Range [0,1].
	// A value of 0 drops all normal traces; anomalous traces are still kept.
	BaseSampleRate float64

	// MinSampleRate / MaxSampleRate bound the adaptive keep-rate. Only used when
	// AdaptiveBudget is enabled.
	MinSampleRate float64
	MaxSampleRate float64

	// LatencyThreshold retains any trace whose duration is >= this value.
	// Zero disables latency-based retention.
	LatencyThreshold time.Duration

	// ImportantAttributes lists span attribute keys whose mere presence forces
	// retention (e.g. "training.diverged", "gpu.oom", "checkpoint.corrupt").
	ImportantAttributes []string

	// AggregateThreshold is the minimum number of structurally-identical sibling
	// OK spans required before they are collapsed into one aggregate span.
	// Defaults to 3 when <= 0.
	AggregateThreshold int

	// AdaptiveBudget enables the adaptive keep-rate controller.
	AdaptiveBudget bool

	// BudgetSpansPerSec is the target retained-span throughput. The controller
	// tightens the keep-rate when the observed rate exceeds this budget.
	BudgetSpansPerSec int

	// AdjustWindow is how often the adaptive controller recomputes the rate.
	// Defaults to 10s when <= 0.
	AdjustWindow time.Duration
}

// OptimizedTrace is the result of optimizing a single trace.
type OptimizedTrace struct {
	TraceID           string
	Retained          bool
	Policy            TraceRetentionPolicy
	Reason            string
	OriginalSpanCount int
	// OptimizedSpans is nil when the trace is dropped.
	OptimizedSpans   []*Span
	AggregatedGroups int
	CollapsedSpans   int
}

// TraceOptimizerStats is a point-in-time snapshot of optimizer counters.
type TraceOptimizerStats struct {
	TracesSeen          int64
	TracesKept          int64
	TracesDropped       int64
	SpansIn             int64
	SpansOut            int64
	SpansCollapsed      int64
	EffectiveSampleRate float64
}

// TraceOptimizer implements the M18 tail-sampling + aggregation optimizer.
// It is safe for concurrent use; however each set of spans passed to Optimize
// is expected to belong to a COMPLETED trace (no concurrent writers on those
// spans).
type TraceOptimizer struct {
	cfg TraceOptimizerConfig

	mu          sync.Mutex
	effRate     float64
	windowStart time.Time
	windowSpans int64

	tracesSeen     int64
	tracesKept     int64
	tracesDropped  int64
	spansIn        int64
	spansOut       int64
	spansCollapsed int64
}

// NewTraceOptimizer constructs an optimizer, applying sensible defaults.
func NewTraceOptimizer(cfg TraceOptimizerConfig) *TraceOptimizer {
	if cfg.BaseSampleRate < 0 {
		cfg.BaseSampleRate = 0
	}
	if cfg.BaseSampleRate > 1 {
		cfg.BaseSampleRate = 1
	}
	if cfg.MinSampleRate <= 0 {
		cfg.MinSampleRate = 0.01
	}
	if cfg.MaxSampleRate <= 0 || cfg.MaxSampleRate > 1 {
		cfg.MaxSampleRate = 1.0
	}
	if cfg.AggregateThreshold <= 0 {
		cfg.AggregateThreshold = 3
	}
	if cfg.AdjustWindow <= 0 {
		cfg.AdjustWindow = 10 * time.Second
	}
	return &TraceOptimizer{
		cfg:         cfg,
		effRate:     cfg.BaseSampleRate,
		windowStart: time.Now(),
	}
}

// Optimize applies tail sampling then, for retained traces, aggregation
// denoising. It returns the optimization outcome for the given trace.
func (o *TraceOptimizer) Optimize(traceID string, spans []*Span) OptimizedTrace {
	if traceID == "" && len(spans) > 0 {
		traceID = spans[0].TraceID
	}
	res := OptimizedTrace{TraceID: traceID, OriginalSpanCount: len(spans)}

	if len(spans) == 0 {
		res.Policy = PolicyEmpty
		res.Reason = "empty trace"
		o.record(false, 0, 0)
		return res
	}

	policy, reason, keep := o.tailDecision(traceID, spans)
	res.Policy = policy
	res.Reason = reason
	if !keep {
		o.record(false, len(spans), 0)
		return res
	}
	res.Retained = true

	optimized, groups, collapsed := o.aggregate(spans)
	res.OptimizedSpans = optimized
	res.AggregatedGroups = groups
	res.CollapsedSpans = collapsed

	o.record(true, len(spans), collapsed)
	return res
}

// OptimizeBatch optimizes many traces keyed by trace ID and returns only the
// retained (non-dropped) results. Dropped traces still update the counters.
func (o *TraceOptimizer) OptimizeBatch(traces map[string][]*Span) []OptimizedTrace {
	out := make([]OptimizedTrace, 0, len(traces))
	for tid, spans := range traces {
		r := o.Optimize(tid, spans)
		if r.Retained {
			out = append(out, r)
		}
	}
	return out
}

// tailDecision evaluates retention policies in priority order.
func (o *TraceOptimizer) tailDecision(traceID string, spans []*Span) (TraceRetentionPolicy, string, bool) {
	// 1. Always retain traces containing an error span.
	for _, s := range spans {
		if s.Status == StatusError {
			return PolicyError, "trace contains an error span", true
		}
	}

	// 2. Retain slow traces.
	if o.cfg.LatencyThreshold > 0 {
		if d := traceDuration(spans); d >= o.cfg.LatencyThreshold {
			return PolicyLatency, fmt.Sprintf("trace duration %s >= threshold %s", d, o.cfg.LatencyThreshold), true
		}
	}

	// 3. Retain traces carrying an important attribute.
	if len(o.cfg.ImportantAttributes) > 0 {
		for _, s := range spans {
			for _, k := range o.cfg.ImportantAttributes {
				if _, ok := s.Attributes[k]; ok {
					return PolicyImportantAttr, "trace carries important attribute " + k, true
				}
			}
		}
	}

	// 4. Probabilistic keep for normal traces, gated by the effective rate.
	o.mu.Lock()
	rate := o.effRate
	o.mu.Unlock()
	if traceRatio(traceID) < rate {
		return PolicyProbabilistic, fmt.Sprintf("probabilistic keep at rate %.4f", rate), true
	}
	return PolicyProbabilistic, fmt.Sprintf("probabilistic drop at rate %.4f", rate), false
}

// aggregate collapses repetitive OK sibling spans (same parent + name) into a
// single representative aggregate span when their count reaches the threshold.
// Non-OK spans are preserved individually so anomalies remain visible.
func (o *TraceOptimizer) aggregate(spans []*Span) (out []*Span, aggGroups, collapsed int) {
	type groupKey struct{ parent, name string }
	groups := make(map[groupKey][]*Span)
	order := make([]groupKey, 0, len(spans))
	out = make([]*Span, 0, len(spans))

	for _, s := range spans {
		if s.Status != StatusOk {
			// Errors / cancellations are never collapsed.
			out = append(out, s)
			continue
		}
		k := groupKey{s.ParentID, s.Name}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], s)
	}

	for _, k := range order {
		g := groups[k]
		if len(g) >= o.cfg.AggregateThreshold {
			out = append(out, buildAggregateSpan(k.name, g))
			aggGroups++
			collapsed += len(g) - 1
		} else {
			out = append(out, g...)
		}
	}
	return out, aggGroups, collapsed
}

// buildAggregateSpan constructs a representative span for a collapsed group,
// recording count and latency statistics in its attributes.
func buildAggregateSpan(name string, group []*Span) *Span {
	rep := group[0]
	sum := group[0].Duration
	minD := group[0].Duration
	maxD := group[0].Duration
	for _, s := range group[1:] {
		d := s.Duration
		sum += d
		if d < minD {
			minD = d
		}
		if d > maxD {
			maxD = d
		}
	}
	n := len(group)
	avg := sum / time.Duration(n)
	return &Span{
		SpanID:    rep.SpanID,
		TraceID:   rep.TraceID,
		ParentID:  rep.ParentID,
		Name:      fmt.Sprintf("%s (x%d aggregated)", name, n),
		StartTime: rep.StartTime,
		EndTime:   rep.EndTime,
		Duration:  avg,
		Status:    StatusOk,
		Attributes: map[string]interface{}{
			"aggregate":          true,
			"aggregate.count":    n,
			"aggregate.total_ns": int64(sum),
			"aggregate.avg_ns":   int64(avg),
			"aggregate.min_ns":   int64(minD),
			"aggregate.max_ns":   int64(maxD),
		},
	}
}

// record updates counters and, when enabled, runs the adaptive controller.
func (o *TraceOptimizer) record(kept bool, spansIn, collapsed int) {
	o.mu.Lock()
	o.tracesSeen++
	o.spansIn += int64(spansIn)
	if kept {
		o.tracesKept++
		retained := int64(spansIn - collapsed)
		o.spansOut += retained
		o.spansCollapsed += int64(collapsed)
		o.windowSpans += retained
	} else {
		o.tracesDropped++
	}
	o.maybeAdjustLocked()
	o.mu.Unlock()
}

// maybeAdjustLocked recomputes the effective keep-rate at window boundaries.
// Caller must hold o.mu.
func (o *TraceOptimizer) maybeAdjustLocked() {
	if !o.cfg.AdaptiveBudget || o.cfg.BudgetSpansPerSec <= 0 {
		return
	}
	now := time.Now()
	elapsed := now.Sub(o.windowStart)
	if elapsed < o.cfg.AdjustWindow {
		return
	}
	observed := float64(o.windowSpans) / elapsed.Seconds()
	o.effRate = computeAdjustedRate(o.effRate, observed, float64(o.cfg.BudgetSpansPerSec), o.cfg.MinSampleRate, o.cfg.MaxSampleRate)
	o.windowSpans = 0
	o.windowStart = now
}

// computeAdjustedRate applies multiplicative-decrease / -increase toward the
// target budget and clamps to [min, max]. Pure function for testability.
func computeAdjustedRate(current, observed, target, minRate, maxRate float64) float64 {
	if target <= 0 {
		return current
	}
	switch {
	case observed > target*1.2:
		current *= 0.8
	case observed < target*0.8:
		current *= 1.25
	}
	return math.Max(minRate, math.Min(maxRate, current))
}

// EffectiveSampleRate returns the current probabilistic keep-rate.
func (o *TraceOptimizer) EffectiveSampleRate() float64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.effRate
}

// Stats returns a snapshot of optimizer counters.
func (o *TraceOptimizer) Stats() TraceOptimizerStats {
	o.mu.Lock()
	defer o.mu.Unlock()
	return TraceOptimizerStats{
		TracesSeen:          o.tracesSeen,
		TracesKept:          o.tracesKept,
		TracesDropped:       o.tracesDropped,
		SpansIn:             o.spansIn,
		SpansOut:            o.spansOut,
		SpansCollapsed:      o.spansCollapsed,
		EffectiveSampleRate: o.effRate,
	}
}

// traceDuration returns the wall-clock span of a trace, falling back to the
// longest single-span duration when start/end timestamps are unavailable.
func traceDuration(spans []*Span) time.Duration {
	var minStart, maxEnd time.Time
	haveWindow := false
	var maxDur time.Duration
	for _, s := range spans {
		if s.Duration > maxDur {
			maxDur = s.Duration
		}
		if s.StartTime.IsZero() || s.EndTime.IsZero() {
			continue
		}
		if !haveWindow {
			minStart, maxEnd = s.StartTime, s.EndTime
			haveWindow = true
			continue
		}
		if s.StartTime.Before(minStart) {
			minStart = s.StartTime
		}
		if s.EndTime.After(maxEnd) {
			maxEnd = s.EndTime
		}
	}
	if haveWindow && maxEnd.After(minStart) {
		return maxEnd.Sub(minStart)
	}
	return maxDur
}

// traceRatio deterministically maps a trace ID to a value in [0,1) using the
// first 8 bytes of its SHA-256 digest — matching HeadBasedSampler's approach so
// head and tail decisions use a consistent hashing scheme.
func traceRatio(traceID string) float64 {
	hash := sha256.Sum256([]byte(traceID))
	var val uint64
	for i := 0; i < 8; i++ {
		val = (val << 8) | uint64(hash[i])
	}
	return float64(val) / float64(^uint64(0))
}
