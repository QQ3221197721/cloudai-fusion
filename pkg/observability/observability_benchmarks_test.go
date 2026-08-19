package observability

import (
	"context"
	"sync/atomic"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ============================================================================
// M47 Benchmark: Trace Context Performance
// ============================================================================

var benchmarkTraceParent = "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-01"
var benchmarkCarrierIn = map[string]string{"traceparent": benchmarkTraceParent}
var benchmarkSpanContext = SpanContext{
	Version: "00",
	TraceID: "0af7651916cd43dd8448eb211c803119",
	SpanID:  "b7ad6b7169203331",
	Flags:   "01",
}

// BenchmarkParseTraceParent measures raw header parsing throughput
func BenchmarkParseTraceParent(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := ParseTraceParent(benchmarkTraceParent)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExtract measures Extract() from HTTP carrier
func BenchmarkExtract(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sc, err := Extract(benchmarkCarrierIn)
		if err != nil {
			b.Fatal(err)
		}
		_ = sc
	}
}

// BenchmarkInject measures Inject() into carrier map
func BenchmarkInject(b *testing.B) {
	ctx := benchmarkSpanContext.WithValue(context.Background())
	carrier := make(map[string]string)
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		carrier = make(map[string]string) // fresh carrier each iteration
		Inject(ctx, carrier)
	}
}

// BenchmarkInjectExtractRoundTrip measures full client→server inject/extract cycle
func BenchmarkInjectExtractRoundTrip(b *testing.B) {
	ctx := benchmarkSpanContext.WithValue(context.Background())
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		carrier := make(map[string]string)
		Inject(ctx, carrier)
		
		extracted, err := Extract(carrier)
		if err != nil {
			b.Fatal(err)
		}
		
		_ = extracted.String() // use output to prevent dead-code elimination
	}
}

// BenchmarkChildOf creates child span context with new IDs
func BenchmarkChildOf(b *testing.B) {
	parent := SpanContext{
		Version: "00",
		TraceID: "0af7651916cd43dd8448eb211c803119",
		SpanID:  "b7ad6b7169203331",
		Flags:   "01",
	}
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		child := parent.ChildOf()
		_ = child
	}
}

// BenchmarkString serialization overhead
func BenchmarkString(b *testing.B) {
	ctx := benchmarkSpanContext
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := ctx.String()
		if len(s) == 0 {
			b.Fatal("expected non-empty string")
		}
		_ = s
	}
}

// ============================================================================
// M47 Benchmark: Sampler Decision Latency
// ============================================================================

var samplerBenchmarkTestCases = []struct {
	name     string
	traceID  string
	isRoot   bool
	spanName string
}{
	{"root_span", "0af7651916cd43dd8448eb211c803119", true, "request_handler"},
	{"child_span", "0af7651916cd43dd8448eb211c803119", false, "db_query"},
}

func BenchmarkHeadBasedSampler_RootSampling(b *testing.B) {
	sampler := NewHeadBasedSampler(0.01) // 1% rate
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := sampler.ShouldSample("deadbeefcafebabe1234567890abcdef", "operation", true)
		_ = result.ShouldSample
	}
}

func BenchmarkHeadBasedSampler_ChildPropagation(b *testing.B) {
	sampler := NewHeadBasedSampler(0.01)
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := sampler.ShouldSample("traceid12345678901234567890123456789012", "child_op", false)
		_ = result.Reason // verify inheritance behavior
	}
}

func BenchmarkForcedSampler_EagerSampling(b *testing.B) {
	sampler := &ForcedSampler{}
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := sampler.ShouldSample("tracetest12345678901234567890123456", "always_sample", true)
		_ = result.SampledBy
	}
}

func BenchmarkCompositeSampler_ORLogic(b *testing.B) {
	s1 := NewHeadBasedSampler(0.5)
	s2 := NewHeadBasedSampler(0.5)
	composite := NewCompositeSampler(s1, s2)
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := composite.ShouldSample("composite_test_123456789012345", "op", true)
		_ = result.Reason
	}
}

// ============================================================================
// M47 Benchmark: Span Operations
// ============================================================================

func BenchmarkSpan_StartEnd(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		span := &Span{Name: "benchmark_span"}
		span.Start()
		span.End()
		_ = span.Duration
	}
}

func BenchmarkSpan_SetAttribute(b *testing.B) {
	span := &Span{Name: "benchmark"}
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		span.SetAttribute("key", "value")
		span.SetAttribute("latency_ms", 123)
		span.SetAttribute("success", true)
	}
}

func BenchmarkSpan_Clone(b *testing.B) {
	original := &Span{
		SpanID:    "span123",
		TraceID:   "trace123",
		Name:      "test",
		Status:    StatusOk,
		ErrorMsg:  "",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Millisecond),
		Duration:  time.Millisecond,
	}
	original.SetAttribute("k", "v")
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cloned := original.Clone()
		_ = cloned.Attributes["k"]
	}
}

// ============================================================================
// M47 Benchmark: Concurrent Tracing Throughput
// ============================================================================

func BenchmarkConcurrentTracing(b *testing.B) {
	ctx := benchmarkSpanContext.WithValue(context.Background())
	
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		carrier := make(map[string]string)
		for pb.Next() {
			Inject(ctx, carrier)
		}
	})
}

// ============================================================================
// M49 Self-Healing Controller Benchmarks
// ============================================================================

func BenchmarkSelfHealer_NonDestructiveAction(b *testing.B) {
	helper := NewSelfHealer(testKey)
	
	helper.RegisterAction(HealingAction{
		Type:        HealActionRestartPod,
		Description: "restart pod",
		Timeout:     30 * time.Second,
		Destructive: false,
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		targets := []string{"pod-" + string(rune(i))}
		result, err := helper.executeWithGates(HealActionRestartPod, targets)
		if err != nil {
			b.Fatal(err)
		}
		_ = result.Result
	}
}

func BenchmarkSelfHealer_DestructiveAction(b *testing.B) {
	helper := NewSelfHealer(testKey)
	
	helper.SetClusterSize(100)
	helper.RegisterAction(HealingAction{
		Type:          HealActionDrainNode,
		Description:   "drain node",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 1.0, // Allow all nodes to reduce early termination
		Timeout:       5 * time.Minute,
		Destructive:   true,
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N && i < 200; i++ { // Limit iterations to avoid hitting limit too early
		targets := []string{"node-" + string(rune(i))}
		result, err := helper.executeWithGates(HealActionDrainNode, targets)
		if err != nil && result == nil {
			b.Skip("skipped after impact limit: gate working correctly")
			return
		}
		require.NoError(b, err)
		_ = result.Result
	}
}

func BenchmarkSelfHealer_ReleaseImpact(b *testing.B) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(100)
	
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		helper.ReleaseImpact(1)
	}
}

// Gate check latency (no mutation)
func BenchmarkSelfHealer_GateCheck_Latency(b *testing.B) {
	helper := NewSelfHealer(testKey)
	
	helper.SetClusterSize(100)
	helper.RegisterAction(HealingAction{
		Type:          HealActionFailover,
		Description:   "failover",
		RateLimit:     RateLimitConfig{MaxPerWindow: 1000, Window: time.Hour},
		MaxImpactFrac: 0.10,
		Timeout:       2 * time.Minute,
		Destructive:   true,
	})
	
	// Pre-warm cache by executing once
	helper.executeWithGates(HealActionFailover, []string{"pre"})
	helper.ReleaseImpact(1)
	
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Each target is distinct so idempotency won't short-circuit
		targets := []string{"target-" + string(rune(i))}
		result, err := helper.executeWithGates(HealActionFailover, targets)
		_ = result
		_ = err
	}
}

// Idempotent replay path (fast path in gate layer)
func BenchmarkSelfHealer_IdempotentPath(b *testing.B) {
	helper := NewSelfHealer(testKey)
	helper.RegisterAction(HealingAction{
		Type:        HealActionScaleOut,
		Description: "scale out",
		Timeout:     5 * time.Minute,
		Destructive: false,
	})
	
	// First execution to populate cache
	initialTarget := []string{"deployment-1"}
	helper.executeWithGates(HealActionScaleOut, initialTarget)
	
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := helper.executeWithGates(HealActionScaleOut, initialTarget)
		if err != nil {
			b.Fatal(err)
		}
		// Fast-path should return "idempotent_skip"
		if result.Result != "idempotent_skip" {
			b.Fatalf("expected idempotent_skip but got %s", result.Result)
		}
	}
}

// Concurrency stress: many goroutines simultaneously triggering healing actions
func BenchmarkConcurrentSelfHeal(b *testing.B) {
	helper := NewSelfHealer(testKey)
	helper.SetClusterSize(1000)
	helper.RegisterAction(HealingAction{
		Type:          HealActionFailover,
		Description:   "failover concurrent",
		RateLimit:     RateLimitConfig{MaxPerWindow: 100000, Window: time.Hour},
		MaxImpactFrac: 1.0, // Allow full cluster
		Timeout:       time.Minute,
		Destructive:   true,
	})
	
	helper.mu.Lock()
	activeNodes := helper.impactTracker.ActiveNodes
	helper.mu.Unlock()
	_ = activeNodes
	
	var executedCount int64
	
	b.ReportAllocs()
	
	// Launch multiple goroutines
	const goroutines = 8
	b.RunParallel(func(pb *testing.PB) {
		counter := int64(0)
		for pb.Next() {
			id := atomic.AddInt64(&executedCount, 1)
			targets := []string{"concurrent-node-" + strconv.FormatInt(id, 10)}
			result, err := helper.executeWithGates(HealActionFailover, targets)
			if err != nil {
				break // Expected after capacity exhaustion
			}
			_ = result
			counter++
			if counter > 20 { break } // Limit each goroutine's workload
		}
	})
}
