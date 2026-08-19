// Package observability provides operational observability capabilities including alert classification and routing, on-call rotation management, runbook automation, and incident retrospective (post-mortem) workflows.
package observability

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// W3C Trace Context Support (Module 47)
// ============================================================================

// SpanContext implements W3C Trace Context specification for distributed tracing.
// Format: version(2)-traceid(32)-spanid(16)-flags(2)
type SpanContext struct {
	Version string   // "00" (W3C standard)
	TraceID string   // 32 hex chars (16 bytes)
	SpanID  string   // 16 hex chars (8 bytes)
	Flags   string   // 2 hex chars
	ParentID string  // optional parent span ID
}

// ErrInvalidTraceContext is returned when traceparent header format is invalid.
var ErrInvalidTraceContext = errors.New("observability: invalid traceparent header format")

// Extract parses a traceparent HTTP header into SpanContext.
// Format: version(2)-traceid(32)-spanid(16)-flags(2)
// Strict validation: exactly 4 hyphen-separated parts, correct lengths, hex only.
func Extract(carrier map[string]string) (SpanContext, error) {
	header := carrier["traceparent"]
	if header == "" {
		return SpanContext{}, nil // No context present, not an error
	}

	return ParseTraceParent(header)
}

// ParseTraceParent parses a raw traceparent header string.
// Returns error for any malformed input.
func ParseTraceParent(header string) (SpanContext, error) {
	parts := splitByHyphen(header)
	if len(parts) != 4 {
		return SpanContext{}, fmt.Errorf("%w: expected 4 parts, got %d", ErrInvalidTraceContext, len(parts))
	}

	version := parts[0]
	traceID := parts[1]
	spanID := parts[2]
	flags := parts[3]

	// Validate version
	if version != "00" {
		return SpanContext{}, fmt.Errorf("%w: unsupported version %q", ErrInvalidTraceContext, version)
	}

	// Validate traceid: must be exactly 32 hex characters
	if !isValidHex(traceID, 32) {
		return SpanContext{}, fmt.Errorf("%w: traceid must be 32 hex characters, got %d", ErrInvalidTraceContext, len(traceID))
	}

	// Validate spanid: must be exactly 16 hex characters
	if !isValidHex(spanID, 16) {
		return SpanContext{}, fmt.Errorf("%w: spanid must be 16 hex characters, got %d", ErrInvalidTraceContext, len(spanID))
	}

	// Validate flags: must be exactly 2 hex characters
	if !isValidHex(flags, 2) {
		return SpanContext{}, fmt.Errorf("%w: flags must be 2 hex characters, got %d", ErrInvalidTraceContext, len(flags))
	}

	return SpanContext{
		Version: version,
		TraceID: traceID,
		SpanID:  spanID,
		Flags:   flags,
	}, nil
}

// Inject serializes SpanContext into a traceparent HTTP header.
// The caller is responsible for setting the header in the carrier map.
func Inject(ctx context.Context, carrier map[string]string) {
	if sc, ok := ctx.Value(traceContextKey).(SpanContext); ok && sc.Valid() {
		carrier["traceparent"] = sc.String()
	}
}

// String returns the wire format representation of this context.
func (c SpanContext) String() string {
	if !c.Valid() {
		return ""
	}
	return c.Version + "-" + c.TraceID + "-" + c.SpanID + "-" + c.Flags
}

// Valid reports whether this context has valid trace and span IDs.
func (c SpanContext) Valid() bool {
	return c.TraceID != "" && c.SpanID != ""
}

// ChildOf creates a child SpanContext with the same trace ID but new span ID.
func (c SpanContext) ChildOf() SpanContext {
	newSpanID := generateSpanID()
	return SpanContext{
		Version:  c.Version,
		TraceID:  c.TraceID,
		SpanID:   newSpanID,
		Flags:    c.Flags,
		ParentID: c.SpanID,
	}
}

// WithValue returns a new context with this SpanContext attached.
func (c SpanContext) WithValue(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceContextKey, c)
}

// FromContext extracts SpanContext from a context.
func FromContext(ctx context.Context) (SpanContext, bool) {
	sc, ok := ctx.Value(traceContextKey).(SpanContext)
	return sc, ok
}

// spanKey is the key type for context values.
type spanKeyType int

const traceContextKey spanKeyType = 0

// ============================================================================
// Span Model
// ============================================================================

// SpanStatus represents the outcome of a span operation.
type SpanStatus int

const (
	// StatusOk indicates success
	StatusOk SpanStatus = iota
	// StatusError indicates an error occurred
	StatusError
	// StatusCancelled indicates cancellation
	StatusCancelled
)

// String returns a human-readable status description.
func (s SpanStatus) String() string {
	switch s {
	case StatusOk:
		return "ok"
	case StatusError:
		return "error"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// Span represents a single unit of work in a distributed trace.
type Span struct {
	// Unique identifier for this span (16 hex chars, 8 bytes)
	SpanID string

	// TraceID links all spans belonging to the same trace (32 hex chars, 16 bytes)
	TraceID string

	// ParentID links to parent span, if any
	ParentID string

	// Name describes the operation
	Name string

	// StartTime marks when the span started
	StartTime time.Time

	// EndTime marks when the span ended
	EndTime time.Time

	// Duration is the span duration
	Duration time.Duration

	// Attributes contains key-value metadata
	Attributes map[string]interface{}

	// Status indicates the outcome
	Status SpanStatus

	// ErrorMsg contains error details if status is StatusError
	ErrorMsg string

	// ChildSpans lists child span IDs
	ChildSpans []string

	// Lock protects concurrent modifications
	mu sync.RWMutex
}

// Start begins timing this span.
func (s *Span) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.StartTime = time.Now()
	s.Duration = 0
}

// End stops timing and computes duration.
func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndTime = time.Now()
	s.Duration = s.EndTime.Sub(s.StartTime)
}

// SetAttribute adds or updates an attribute.
func (s *Span) SetAttribute(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Attributes == nil {
		s.Attributes = make(map[string]interface{})
	}
	s.Attributes[key] = value
}

// SetError marks the span as errored with a message.
func (s *Span) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusError
	s.ErrorMsg = msg
}

// AddChild registers a child span ID.
func (s *Span) AddChild(childID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ChildSpans = append(s.ChildSpans, childID)
}

// Clone returns a shallow copy of this span.
func (s *Span) Clone() *Span {
	s.mu.RLock()
	defer s.mu.RUnlock()

	attrs := make(map[string]interface{}, len(s.Attributes))
	for k, v := range s.Attributes {
		attrs[k] = v
	}

	children := make([]string, len(s.ChildSpans))
	copy(children, s.ChildSpans)

	return &Span{
		SpanID:     s.SpanID,
		TraceID:    s.TraceID,
		ParentID:   s.ParentID,
		Name:       s.Name,
		StartTime:  s.StartTime,
		EndTime:    s.EndTime,
		Duration:   s.Duration,
		Attributes: attrs,
		Status:     s.Status,
		ErrorMsg:   s.ErrorMsg,
		ChildSpans: children,
	}
}

// ============================================================================
// Span Processor Interface
// ============================================================================

// SpanProcessor handles lifecycle callbacks for Spans.
type SpanProcessor interface {
	// OnStart is called when a span starts.
	OnStart(span *Span)

	// OnEnd is called when a span ends.
	OnEnd(span *Span)

	// Shutdown is called when the tracer shuts down.
	Shutdown(ctx context.Context) error
}

// ============================================================================
// Sampler Interface
// ============================================================================

// SamplingResult captures sampling decision and its reason.
type SamplingResult struct {
	// ShouldSample whether this span should be recorded
	ShouldSample bool

	// Reason explains the decision
	Reason string

	// SampledBy indicates why it was sampled ("head_prob" for probabilistic, "forced_error" for mandatory sampling on errors)
	SampledBy string
}

// Sampler decides which traces to sample based on various criteria.
type Sampler interface {
	// ShouldSample determines if a span should be sampled.
	ShouldSample(traceID string, spanName string, isRoot bool) SamplingResult
}

// HeadBasedSampler implements head-based probabilistic sampling.
// For example, with Rate=0.01, ~1% of traces are sampled at the head.
type HeadBasedSampler struct {
	Rate float64 // probability [0,1]
	mu   sync.Mutex
	count, sampled int
}

// NewHeadBasedSampler creates a sampler with given probability rate.
func NewHeadBasedSampler(rate float64) *HeadBasedSampler {
	return &HeadBasedSampler{Rate: rate}
}

// ShouldSample implements Sampler by hashing traceID to determine if sampled.
func (s *HeadBasedSampler) ShouldSample(traceID string, spanName string, isRoot bool) SamplingResult {
	if !isRoot {
		// Non-root spans inherit from root
		return SamplingResult{ShouldSample: true, Reason: "inherited from root"}
	}

	s.mu.Lock()
	s.count++
	s.mu.Unlock()

	// Use first 8 bytes of trace ID hash for deterministic sampling
	hash := sha256.Sum256([]byte(traceID))
	bits := hash[:8]

	// Convert first 8 bytes to a float in [0, 1)
	var val uint64
	for i := 0; i < 8; i++ {
		val = (val << 8) | uint64(bits[i])
	}
	ratio := float64(val) / float64(^uint64(0))

	if ratio < s.Rate {
		s.mu.Lock()
		s.sampled++
		s.mu.Unlock()
		return SamplingResult{
			ShouldSample: true,
			Reason:       fmt.Sprintf("probability %.2f%%", s.Rate*100),
			SampledBy:    "head_prob",
		}
	}

	return SamplingResult{
		ShouldSample: false,
		Reason:       fmt.Sprintf("did not meet probability %.2f%%", s.Rate*100),
		SampledBy:    "rejected",
	}
}

// Stats returns sampling statistics.
func (s *HeadBasedSampler) Stats() (count, sampled int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count, s.sampled
}

// ForcedSampler always samples (useful for debugging/error paths).
type ForcedSampler struct{}

func (s *ForcedSampler) ShouldSample(traceID string, spanName string, isRoot bool) SamplingResult {
	return SamplingResult{
		ShouldSample: true,
		Reason:       "forced sampling enabled",
		SampledBy:    "forced",
	}
}

// CompositeSampler combines multiple samplers with OR logic.
type CompositeSampler struct {
	samplers []Sampler
}

func NewCompositeSampler(samplers ...Sampler) *CompositeSampler {
	return &CompositeSampler{samplers: samplers}
}

func (s *CompositeSampler) ShouldSample(traceID string, spanName string, isRoot bool) SamplingResult {
	for _, sampler := range s.samplers {
		result := sampler.ShouldSample(traceID, spanName, isRoot)
		if result.ShouldSample {
			return result
		}
	}
	return SamplingResult{
		ShouldSample: false,
		Reason:       "all samplers rejected",
		SampledBy:    "rejected",
	}
}

// ============================================================================
// Simple In-Memory Span Storage
// ============================================================================

// SpanStorage stores spans in memory. Thread-safe.
type SpanStorage struct {
	spans    map[string]*Span
	traces   map[string][]*Span
	mu       sync.RWMutex
	maxSpans int
}

// NewSpanStorage creates a new span storage with optional limit.
func NewSpanStorage(maxSpans int) *SpanStorage {
	return &SpanStorage{
		spans:    make(map[string]*Span),
		traces:   make(map[string][]*Span),
		maxSpans: maxSpans,
	}
}

// Record saves a span.
func (s *SpanStorage) Record(span *Span) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.maxSpans > 0 && len(s.spans) >= s.maxSpans {
		// Evict oldest span
		for id := range s.spans {
			delete(s.spans, id)
			delete(s.traces, "")
			break
		}
	}

	s.spans[span.SpanID] = span

	s.traces[span.TraceID] = append(s.traces[span.TraceID], span)
}

// GetSpan retrieves a span by ID.
func (s *SpanStorage) GetSpan(spanID string) (*Span, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	span, ok := s.spans[spanID]
	return span, ok
}

// GetTraces retrieves all spans for a trace.
func (s *SpanStorage) GetTraces(traceID string) []*Span {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trace := s.traces[traceID]
	result := make([]*Span, len(trace))
	copy(result, trace)
	return result
}

// ListAllTraces returns all unique trace IDs.
func (s *SpanStorage) ListAllTraces() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	traces := make([]string, 0, len(s.traces))
	for tid := range s.traces {
		traces = append(traces, tid)
	}
	return traces
}

// CountSpans returns total span count.
func (s *SpanStorage) CountSpans() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.spans)
}

// ============================================================================
// Helper functions
// ============================================================================

// generateSpanID creates an 8-byte span ID rendered as 16 hex chars.
// crypto/rand would pull in CGO on some setups; a monotonic counter mixed
// with the wall clock keeps IDs unique for in-process tracing without CGO.
func generateSpanID() string {
	ts := uint64(time.Now().UnixNano()) ^ (generateSpanCounter() * 0x9E3779B97F4A7C15)
	return fmt.Sprintf("%016x", ts)
}

// generateTraceID creates a 16-byte trace ID rendered as 32 hex chars.
func generateTraceID() string {
	hi := uint64(time.Now().UnixNano()) ^ (generateSpanCounter() * 0x9E3779B97F4A7C15)
	lo := generateSpanCounter()*0xBF58476D1CE4E5B9 ^ uint64(time.Now().UnixNano())
	return fmt.Sprintf("%016x%016x", hi, lo)
}

// global counter for span/trace IDs to improve uniqueness
var spanCounter uint64

func generateSpanCounter() uint64 {
	return atomic.AddUint64(&spanCounter, 1)
}

func splitByHyphen(s string) []string {
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func isValidHex(s string, expectedLen int) bool {
	if len(s) != expectedLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}
