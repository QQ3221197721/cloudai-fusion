package tracing

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// ============================================================================
// Span Creation Benchmarks
// ============================================================================

// BenchmarkSpanStart measures the cost of starting a new span from context.
func BenchmarkSpanStart(b *testing.B) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tracer := tp.Tracer("bench")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tracer.Start(ctx, "test.span",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(attribute.String("op", "benchmark")),
		)
		s.End()
	}
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// BenchmarkSpanStartServerType simulates server-side HTTP span creation overhead.
func BenchmarkSpanStartServerType(b *testing.B) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tracer := tp.Tracer("http-server")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tracer.Start(ctx, "GET /api/v1/resource",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String("GET"),
				semconv.URLPath("/api/v1/resource"),
				semconv.ServerAddress("localhost:8080"),
			),
		)
		s.End()
	}
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// BenchmarkSpanChildCreation measures child span creation cost (typical in nested calls).
func BenchmarkSpanChildCreation(b *testing.B) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tracer := tp.Tracer("parent-child")
	ctx, parent := tracer.Start(context.Background(), "root-parent")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, child := tracer.Start(ctx, "child-operation",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		child.End()
	}
	parent.End()
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// ============================================================================
// Attribute & Event Benchmarks
// ============================================================================

// BenchmarkSpanSetAttributes measures SetAttributes() performance with various attribute sets.
func BenchmarkSpanSetAttributes(b *testing.B) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("bench-attrs")
	_, span := tracer.Start(context.Background(), "set-attrs-test")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		span.SetAttributes(
			attribute.String("attr.string", "value"),
			attribute.Int("attr.int", i),
			attribute.Bool("attr.bool", true),
			attribute.Float64("attr.float", float64(i)*0.001),
			attribute.StringSlice("attr.slice", []string{"a", "b", "c"}),
		)
	}
	b.StopTimer()
	span.End()
	_ = tp.Shutdown(context.Background())
}

// BenchmarkSpanAddEvent measures the cost of adding timestamped events to spans.
func BenchmarkSpanAddEvent(b *testing.B) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("bench-events")
	_, span := tracer.Start(context.Background(), "add-event-test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		span.AddEvent(
			"event-name",
			trace.WithTimestamp(time.Now()),
			trace.WithAttributes(attribute.Int("event.index", i%100)),
		)
	}
	b.StopTimer()
	span.End()
	_ = tp.Shutdown(context.Background())
}

// ============================================================================
// Context Propagation Benchmarks
// ============================================================================

// BenchmarkW3CTraceParentParse measures W3C traceparent header parsing overhead.
func BenchmarkW3CTraceParentParse(b *testing.B) {
	// W3C traceparent format: "00-<trace-id>-<span-id>-<flags>"
	traceID := "cf85e592f1b76a3f4f2e7b6c8d9e0a1b"
	spanID := "1c2d3e4f5a6b7c8d"
	flags := "01"
	header := "00-" + traceID + "-" + spanID + "-" + flags

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := ExtractHTTP(context.Background(), http.Header{
			"Traceparent": []string{header},
		})
		_ = ctx
	}
}

// BenchmarkInjectTraceContext measures InjectHTTP() overhead for outgoing requests.
func BenchmarkInjectTraceContext(b *testing.B) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer tp.Shutdown(context.Background())

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", "http://example.com/test", nil)
		InjectHTTP(ctx, req)
	}
}

// BenchmarkBaggageSetGet measures W3C Baggage set/get operations.
func BenchmarkBaggageSetGet(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := SetBaggage(context.Background(), "tenant.id", "acme", "user.id", "u-123", "trace.batch", fmt.Sprintf("%d", i))
		_ = GetBaggage(ctx, "tenant.id")
	}
}

// BenchmarkBaggageHighCardinality tests baggage under high key-value volume.
func BenchmarkBaggageHighCardinality(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		kv := make([]string, 0, 20)
		for j := 0; j < 10; j++ {
			kv = append(kv, fmt.Sprintf("key-%d", j), fmt.Sprintf("value-%d", i+j))
		}
		ctx = SetBaggage(ctx, kv...)
		_ = GetBaggage(ctx, "key-5")
	}
}

// ============================================================================
// Sampler Benchmarks
// ============================================================================

// BenchmarkAlwaysSample measures AlwaysSample sampler decision cost.
func BenchmarkAlwaysSample(b *testing.B) {
	sampler := sdktrace.AlwaysSample()
	params := sdktrace.SamplingParameters{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := sampler.ShouldSample(params)
		_ = result
	}
}

// BenchmarkTraceIDRatioBased measures TraceIDRatioBased sampling overhead.
func BenchmarkTraceIDRatioBased(b *testing.B) {
	sampler := sdktrace.TraceIDRatioBased(0.1) // 10% sample rate
	params := sdktrace.SamplingParameters{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := sampler.ShouldSample(params)
		_ = result
	}
}

// BenchmarkAdaptiveSamplerShouldSample measures our adaptive sampler performance.
func BenchmarkAdaptiveSamplerShouldSample(b *testing.B) {
	sampler := newAdaptiveSampler(adaptiveSamplerConfig{
		MinRate:           0.01,
		MaxRate:           1.0,
		TargetSpansPerSec: 100,
		InitialRate:       0.1,
	})
	params := sdktrace.SamplingParameters{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := sampler.ShouldSample(params)
		_ = result
	}
}

// ============================================================================
// Batch Exporter Benchmarks
// ============================================================================

// BenchmarkBatchSpanProcessorShutdown measures batch processor flush cost.
func BenchmarkBatchSpanProcessorShutdown(b *testing.B) {
	exportCount := 0
	exporter := &mockExporter{
		ExportSpansFunc: func(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
			exportCount += len(spans)
			return nil
		},
	}
	
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tracer := tp.Tracer("batch-benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tracer.Start(context.Background(), "batch-test")
		s.End()
	}
	
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
	_ = exportCount
}

// ============================================================================
// Span Builders Benchmarks
// ============================================================================

// BenchmarkStartDBSpan measures DB span builder overhead.
func BenchmarkStartDBSpan(b *testing.B) {
	ctx := context.Background()
	cfg := DBSpanConfig{
		System:    "postgresql",
		Operation: "SELECT",
		Table:     "users",
		Statement: "SELECT * FROM users WHERE id = ?",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := StartDBSpan(ctx, cfg)
		EndDBSpan(s, nil)
	}
}

// BenchmarkStartCacheSpan measures cache span builder overhead.
func BenchmarkStartCacheSpan(b *testing.B) {
	ctx := context.Background()
	cfg := CacheSpanConfig{
		System:    "redis",
		Operation: "GET",
		Key:       "cache:key:user:123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := StartCacheSpan(ctx, cfg)
		EndCacheSpan(s, false, nil)
	}
}

// BenchmarkStartPublishSpan measures messaging span builder overhead.
func BenchmarkStartPublishSpan(b *testing.B) {
	ctx := context.Background()
	cfg := MsgSpanConfig{
		System:      "nats",
		Destination: "orders.topic",
		Operation:   "publish",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := StartPublishSpan(ctx, cfg)
		EndMsgSpan(s, nil)
	}
}

// ============================================================================
// Middleware Pattern Benchmark (Gin-style)
// ============================================================================

// BenchmarkMiddlewarePattern simulates the Gin middleware pattern for HTTP spans.
func BenchmarkMiddlewarePattern(b *testing.B) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
	))
	tracer := tp.Tracer("gin-middleware")

	methods := []string{"GET", "POST", "PUT", "DELETE"}
	paths := []string{"/api/v1/clusters", "/api/v1/tasks", "/api/v1/users", "/health"}
	statuses := []int{200, 201, 400, 404, 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate extract -> start span -> end span cycle
		ctx := context.Background()
		
		method := methods[i%4]
		path := paths[i%4]
		status := statuses[i%5]
		
		ctx, span := tracer.Start(ctx, method+" "+path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(method),
				semconv.URLPath(path),
			),
		)
		
		span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		span.End()
		
		_ = ctx
	}
	
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// ============================================================================
// Allocation Benchmarks
// ============================================================================

// BenchmarkSpanStartAlloc checks allocations for span creation.
func BenchmarkSpanStartAlloc(b *testing.B) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("alloc-check")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tracer.Start(ctx, "span-allocation",
			trace.WithSpanKind(trace.SpanKindInternal),
		)
		s.End()
	}
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// BenchmarkSpanWithAttributesAlloc measures allocation impact of attributes.
func BenchmarkSpanWithAttributesAlloc(b *testing.B) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("attrs-alloc")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tracer.Start(ctx, "with-attrs",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("method", "GET"),
				attribute.String("host", "localhost"),
				attribute.Int("port", 8080),
				attribute.String("path", "/api/test"),
				attribute.String("version", "1.26.0"),
			),
		)
		s.End()
	}
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// ============================================================================
// Concurrent Span Performance
// ============================================================================

// BenchmarkConcurrentSpanCreationParallel measures parallel span creation throughput.
func BenchmarkConcurrentSpanCreationParallel(b *testing.B) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tracer := tp.Tracer("parallel-spans")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			_, s := tracer.Start(ctx, "concurrent-span",
				trace.WithSpanKind(trace.SpanKindInternal),
				trace.WithAttributes(attribute.Int("iteration", i)),
			)
			s.End()
			i++
		}
	})
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// ============================================================================
// Span End and Flush Benchmarks
// ============================================================================

// BenchmarkSpanEndSequential measures sequential span end overhead.
func BenchmarkSpanEndSequential(b *testing.B) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("end-sequential")
	ctx, root := tracer.Start(context.Background(), "root")

	spans := make([]trace.Span, b.N/100)
	for i := range spans {
		_, s := tracer.Start(ctx, "child")
		spans[i] = s
	}

	b.ResetTimer()
	for _, s := range spans {
		s.End()
	}
	b.StopTimer()
	root.End()
	_ = tp.Shutdown(context.Background())
}

// ============================================================================
// Comparison Against OpenTelemetry Official SDK Numbers
// ============================================================================

// BenchmarkOpenTelemetrySDKComparison_SpanStart measures our OTel integration
// against official OTEL Go SDK expectations.
func BenchmarkOpenTelemetrySDKComparison_SpanStart(b *testing.B) {
	exporter := &mockExporter{}
	
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))),
	)
	tracer := tp.Tracer("otel-comparison")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tracer.Start(ctx, "otel-standard-span",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("service.name", "comparison-service"),
				attribute.String("service.version", "1.0.0"),
			),
		)
		s.End()
	}
	b.StopTimer()
	_ = tp.Shutdown(context.Background())
}

// ============================================================================
// FastTracer Benchmarks — zero-allocation hot path
// ============================================================================

// BenchmarkFastSpanStart is a direct comparison against the baseline OTel SDK span creation cost.
// Target: ≤611 ns/op and ≤7 allocs/op (baseline: ~611ns/564B/7allocs with TraceIDRatioBased sampler).
func BenchmarkFastSpanStart(b *testing.B) {
	tr := NewFastTracer("fastbench")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tr.Start(ctx, "op", trace.SpanKindInternal)
		s.SetInt("i", int64(i)) // one attribute setter
		s.End()
	}
}

// BenchmarkFastSpanStartMinimal tests the absolute fastest case: no attributes at all.
// The only allocation is the single context.WithValue node.
func BenchmarkFastSpanStartMinimal(b *testing.B) {
	tr := NewFastTracer("fastbench-minimal")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tr.Start(ctx, "op", trace.SpanKindInternal)
		s.End()
	}
}

// BenchmarkFastSpanStartFull exercises max inline capacity (8 attrs) to verify
// performance under heavy attribute usage without heap escape.
func BenchmarkFastSpanStartFull(b *testing.B) {
	tr := NewFastTracer("fastbench-full")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tr.Start(ctx, "op", trace.SpanKindInternal)
		s.SetString("k1", fmt.Sprintf("v1-%d", i))
		s.SetInt("k2", int64(i))
		s.SetBool("k3", true)
		s.SetFloat("k4", float64(i)*0.01)
		s.SetInt("k5", int64(i%100))
		s.SetString("k6", fmt.Sprintf("v6-%d", i))
		s.SetBool("k7", false)
		s.SetInt("k8", int64(i*3))
		s.End()
	}
}

// BenchmarkFastSpanContextOverhead isolates the context.WithValue allocation cost.
// This is the lower-bound per-span overhead even if we returned a bare pointer.
func BenchmarkFastSpanContextOverhead(b *testing.B) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx = context.WithValue(ctx, "key", i)
		_ = ctx.Value("key")
	}
}

// BenchmarkFastSpanStartConcurrentParallel tests parallel throughput under concurrency.
func BenchmarkFastSpanStartConcurrentParallel(b *testing.B) {
	tr := NewFastTracer("fastbench-parallel")
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			_, s := tr.Start(ctx, "op", trace.SpanKindInternal)
			s.SetInt("i", int64(i))
			s.End()
			i++
		}
	})
}

// BenchmarkFastSpanEndLatency measures End() overhead for sampled spans.
func BenchmarkFastSpanEndLatency(b *testing.B) {
	tr := NewFastTracer("fastbench-end")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, s := tr.Start(context.Background(), "inline", trace.SpanKindInternal)
		s.SetError(nil)
		s.End()
	}
}

// ============================================================================
// Mock Exporter for Testing
// ============================================================================

type mockExporter struct {
	ExportSpansFunc func(ctx context.Context, spans []sdktrace.ReadOnlySpan) error
}

func (m *mockExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if m.ExportSpansFunc != nil {
		return m.ExportSpansFunc(ctx, spans)
	}
	return nil
}

func (m *mockExporter) Shutdown(ctx context.Context) error {
	return nil
}
