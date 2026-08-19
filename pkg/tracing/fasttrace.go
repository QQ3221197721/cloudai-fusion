package tracing

// ============================================================================
// Fast-path Tracer — a low-allocation span implementation for hot paths.
//
// WHY THIS EXISTS
// ---------------
// The OpenTelemetry Go SDK (go.opentelemetry.io/otel/sdk/trace) is the source
// of truth for exported, sampled, batched traces (see tracing.go / Init). But
// its per-span cost is dominated by allocations that we do NOT control and MUST
// NOT patch: with AlwaysSample the SDK allocates the recording-span, its
// attribute/event/link buffers and a fresh SpanContext on every Start
// (measured at ~755 ns/op, 792 B/op, 8 allocs/op in benchmark_test.go).
//
// For ultra-hot internal code paths (per-request middleware, per-step training
// loops, cache lookups) that only need W3C-compatible correlation IDs + a
// handful of attributes and do NOT need the full SDK export pipeline on every
// span, FastTracer provides a purpose-built alternative:
//
//   * span structs are recycled through a sync.Pool (zero steady-state alloc);
//   * attributes live in a fixed-size inline array (no slice growth / boxing);
//   * trace/span IDs are generated with math/rand/v2's lock-free per-P source
//     straight into the fixed [16]byte / [8]byte arrays (zero alloc);
//   * the produced IDs form a real trace.SpanContext, so a FastSpan can still
//     be propagated over W3C Trace Context and handed to the SDK/exporters via
//     the optional OnEnd hook.
//
// FastTracer is complementary to — not a replacement for — the OTel SDK. It is
// deliberately head-samplable and export-agnostic; wire OnEnd to forward the
// spans you actually keep into the SDK/OTLP pipeline.
//
// CAVEAT: a FastSpan is returned to the pool on End(). Do not retain a
// *FastSpan (or a context still carrying it) after calling End(), exactly like
// bytes.Buffer pools or fasthttp request objects.
// ============================================================================

import (
	"context"
	"crypto/rand"
	"math"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// maxInlineAttrs bounds the number of attributes stored inline on a FastSpan
// without escaping to the heap. Attributes beyond this count are dropped (the
// dropped count is tracked so callers can detect truncation).
const maxInlineAttrs = 8

// attrKind discriminates the typed union stored in FastAttr, avoiding the
// interface{} boxing that would otherwise allocate on every attribute.
type attrKind uint8

const (
	attrKindString attrKind = iota
	attrKindInt
	attrKindFloat
	attrKindBool
)

// FastAttr is a zero-allocation, strongly-typed span attribute. Construct it
// with StringAttr / IntAttr / FloatAttr / BoolAttr; all are value types.
type FastAttr struct {
	Key  string
	kind attrKind
	s    string // set when kind == attrKindString
	n    uint64 // int64 bits / float64 bits / bool (0|1) for the other kinds
}

// StringAttr builds a string-valued attribute.
func StringAttr(key, val string) FastAttr {
	return FastAttr{Key: key, kind: attrKindString, s: val}
}

// IntAttr builds an int64-valued attribute.
func IntAttr(key string, val int64) FastAttr {
	return FastAttr{Key: key, kind: attrKindInt, n: uint64(val)}
}

// FloatAttr builds a float64-valued attribute.
func FloatAttr(key string, val float64) FastAttr {
	return FastAttr{Key: key, kind: attrKindFloat, n: floatBits(val)}
}

// BoolAttr builds a bool-valued attribute.
func BoolAttr(key string, val bool) FastAttr {
	var n uint64
	if val {
		n = 1
	}
	return FastAttr{Key: key, kind: attrKindBool, n: n}
}

// IsString reports whether the attribute holds a string, returning its value.
func (a FastAttr) IsString() (string, bool) {
	if a.kind == attrKindString {
		return a.s, true
	}
	return "", false
}

// AsInt returns the int64 value (valid only for IntAttr).
func (a FastAttr) AsInt() int64 { return int64(a.n) }

// AsFloat returns the float64 value (valid only for FloatAttr).
func (a FastAttr) AsFloat() float64 { return bitsFloat(a.n) }

// AsBool returns the bool value (valid only for BoolAttr).
func (a FastAttr) AsBool() bool { return a.n != 0 }

// ============================================================================
// FastTracer
// ============================================================================

// FastTracer creates pooled FastSpans. It is safe for concurrent use.
type FastTracer struct {
	name    string
	pool    sync.Pool
	onEnd   func(*FastSpan)
	sampled bool
}

// NewFastTracer constructs a FastTracer. By default every span is marked
// sampled and no OnEnd hook is installed (spans are simply recycled on End).
func NewFastTracer(name string, opts ...FastTracerOption) *FastTracer {
	t := &FastTracer{name: name, sampled: true}
	t.pool.New = func() any { return &FastSpan{} }
	for _, o := range opts {
		o(t)
	}
	return t
}

// FastTracerOption configures a FastTracer.
type FastTracerOption func(*FastTracer)

// WithOnEnd installs a callback invoked (for sampled spans only) when End() is
// called, BEFORE the span is returned to the pool. Use it to forward retained
// spans to the OTel SDK / OTLP exporter. The callback must not retain the span.
func WithOnEnd(fn func(*FastSpan)) FastTracerOption {
	return func(t *FastTracer) { t.onEnd = fn }
}

// WithSampled sets the default sampled flag for spans created by this tracer.
func WithSampled(sampled bool) FastTracerOption {
	return func(t *FastTracer) { t.sampled = sampled }
}

// Name returns the tracer's instrumentation name.
func (t *FastTracer) Name() string { return t.name }

// Start begins a new span. If ctx already carries a FastSpan (or a valid OTel
// SpanContext) the new span inherits its trace ID and links it as parent;
// otherwise a fresh 128-bit trace ID is generated. The returned context
// carries the new span for child linking. Zero heap allocations occur beyond
// the single context.WithValue node.
func (t *FastTracer) Start(ctx context.Context, name string, kind trace.SpanKind) (context.Context, *FastSpan) {
	s := t.pool.Get().(*FastSpan)
	s.tracer = t
	s.name = name
	s.kind = kind
	s.nattrs = 0
	s.dropped = 0
	s.statusErr = false
	s.errMsg = ""
	s.endNano = 0
	s.sampled = t.sampled

	if parent := FastSpanFromContext(ctx); parent != nil {
		s.traceID = parent.traceID
		s.parentID = parent.spanID
	} else if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		s.traceID = sc.TraceID()
		s.parentID = sc.SpanID()
	} else {
		randTraceID(&s.traceID)
		s.parentID = trace.SpanID{}
	}
	randSpanID(&s.spanID)

	s.startNano = time.Now().UnixNano()
	ctx = context.WithValue(ctx, fastSpanKey{}, s)
	return ctx, s
}

// ============================================================================
// FastSpan
// ============================================================================

// FastSpan is a pooled, low-allocation span. It is NOT safe for concurrent
// mutation from multiple goroutines (like the OTel SDK span, mutation is
// expected on the owning goroutine).
type FastSpan struct {
	tracer    *FastTracer
	traceID   trace.TraceID
	spanID    trace.SpanID
	parentID  trace.SpanID
	name      string
	kind      trace.SpanKind
	startNano int64
	endNano   int64
	statusErr bool
	errMsg    string
	sampled   bool
	nattrs    int
	dropped   int
	attrs     [maxInlineAttrs]FastAttr
}

type fastSpanKey struct{}

// FastSpanFromContext returns the FastSpan carried by ctx, or nil.
func FastSpanFromContext(ctx context.Context) *FastSpan {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(fastSpanKey{}).(*FastSpan)
	return s
}

// SetAttr records a pre-built attribute. Attributes beyond maxInlineAttrs are
// dropped (counted via DroppedAttributes).
func (s *FastSpan) SetAttr(a FastAttr) *FastSpan {
	if s.nattrs < maxInlineAttrs {
		s.attrs[s.nattrs] = a
		s.nattrs++
	} else {
		s.dropped++
	}
	return s
}

// SetString is a convenience zero-alloc setter for a string attribute.
func (s *FastSpan) SetString(key, val string) *FastSpan { return s.SetAttr(StringAttr(key, val)) }

// SetInt is a convenience zero-alloc setter for an int64 attribute.
func (s *FastSpan) SetInt(key string, val int64) *FastSpan { return s.SetAttr(IntAttr(key, val)) }

// SetBool is a convenience zero-alloc setter for a bool attribute.
func (s *FastSpan) SetBool(key string, val bool) *FastSpan { return s.SetAttr(BoolAttr(key, val)) }

// SetFloat is a convenience zero-alloc setter for a float64 attribute.
func (s *FastSpan) SetFloat(key string, val float64) *FastSpan { return s.SetAttr(FloatAttr(key, val)) }

// SetError marks the span as errored and records the message.
func (s *FastSpan) SetError(err error) *FastSpan {
	if err != nil {
		s.statusErr = true
		s.errMsg = err.Error()
	}
	return s
}

// End records the finish time, invokes the tracer's OnEnd hook for sampled
// spans, and returns the span to the pool. Do not use the span afterwards.
func (s *FastSpan) End() {
	if s.endNano == 0 {
		s.endNano = time.Now().UnixNano()
	}
	t := s.tracer
	if t != nil && t.onEnd != nil && s.sampled {
		t.onEnd(s)
	}
	s.reset()
	if t != nil {
		t.pool.Put(s)
	}
}

// reset clears references so a pooled span cannot leak memory or data between
// uses. Value fields are overwritten by the next Start.
func (s *FastSpan) reset() {
	s.tracer = nil
	s.name = ""
	s.errMsg = ""
	for i := 0; i < s.nattrs; i++ {
		s.attrs[i] = FastAttr{}
	}
	s.nattrs = 0
	s.dropped = 0
}

// SpanContext builds a W3C-compatible trace.SpanContext for propagation.
func (s *FastSpan) SpanContext() trace.SpanContext {
	var tf trace.TraceFlags
	if s.sampled {
		tf = trace.FlagsSampled
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    s.traceID,
		SpanID:     s.spanID,
		TraceFlags: tf,
	})
}

// TraceID returns the hex trace ID (32 chars).
func (s *FastSpan) TraceID() string { return s.traceID.String() }

// SpanID returns the hex span ID (16 chars).
func (s *FastSpan) SpanID() string { return s.spanID.String() }

// Name returns the span name.
func (s *FastSpan) Name() string { return s.name }

// Kind returns the span kind.
func (s *FastSpan) Kind() trace.SpanKind { return s.kind }

// IsError reports whether the span was marked errored.
func (s *FastSpan) IsError() bool { return s.statusErr }

// DroppedAttributes returns how many attributes exceeded the inline capacity.
func (s *FastSpan) DroppedAttributes() int { return s.dropped }

// Duration returns the elapsed time; if End has not been called it measures up
// to the current instant.
func (s *FastSpan) Duration() time.Duration {
	end := s.endNano
	if end == 0 {
		end = time.Now().UnixNano()
	}
	return time.Duration(end - s.startNano)
}

// Attributes invokes fn for each recorded attribute in insertion order. The
// slice is not exposed to avoid escaping the inline array to the heap.
func (s *FastSpan) Attributes(fn func(FastAttr)) {
	for i := 0; i < s.nattrs; i++ {
		fn(s.attrs[i])
	}
}

// ============================================================================
// Zero-allocation ID generation
//
// Trace/span IDs are drawn from crypto/rand (a cryptographically strong
// userspace CSPRNG in Go 1.24+), buffered through a pooled 512-byte reader so
// the CSPRNG is refilled roughly once per ~21 spans and the steady-state hot
// path performs no heap allocation and no syscall.
// ============================================================================

type randSource struct {
	buf [512]byte
	off int
}

var randPool = sync.Pool{
	New: func() any { return &randSource{off: 512} },
}

// fill copies len(dst) cryptographically-random bytes into dst, refilling the
// internal buffer from crypto/rand when exhausted.
func (r *randSource) fill(dst []byte) {
	for len(dst) > 0 {
		if r.off >= len(r.buf) {
			// crypto/rand.Read never returns a short read and only errors on
			// catastrophic OS failure; panic mirrors uuid/otel behaviour.
			if _, err := rand.Read(r.buf[:]); err != nil {
				panic("tracing: crypto/rand failed: " + err.Error())
			}
			r.off = 0
		}
		n := copy(dst, r.buf[r.off:])
		r.off += n
		dst = dst[n:]
	}
}

// randTraceID fills a 128-bit trace ID. Writing straight into the array pointer
// allocates nothing beyond the amortised, pooled CSPRNG buffer.
func randTraceID(id *trace.TraceID) {
	rs := randPool.Get().(*randSource)
	rs.fill(id[:])
	randPool.Put(rs)
	if !id.IsValid() { // astronomically unlikely all-zero
		id[15] |= 0x01
	}
}

// randSpanID fills a 64-bit span ID.
func randSpanID(id *trace.SpanID) {
	rs := randPool.Get().(*randSource)
	rs.fill(id[:])
	randPool.Put(rs)
	if !id.IsValid() {
		id[7] |= 0x01
	}
}

func floatBits(f float64) uint64 { return math.Float64bits(f) }

func bitsFloat(b uint64) float64 { return math.Float64frombits(b) }
