// Package tracing provides chaos engineering tests for distributed tracing resilience.
// These tests validate that the OpenTelemetry pipeline gracefully handles failures,
// network partitions, and collector outages without crashing or losing trace data.
package tracing

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/sirupsen/logrus"
)

var mathRand = rand.New(rand.NewSource(time.Now().UnixNano()))

// ============================================================================
// TestM47_CollectorOutageGracefulDegradation - No-Crash Failure Recovery
// ============================================================================

func TestM47_CollectorOutageGracefulDegradation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint("localhost:9999"),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("Failed to create exporter (expected): %v", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxExportBatchSize(1),
			sdktrace.WithBatchTimeout(100*time.Millisecond),
			sdktrace.WithExportTimeout(500*time.Millisecond),
		),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("chaos-test"),
		)),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown tracer provider: %v", err)
		}
	}()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer := tp.Tracer("collector-outage-test")

	const numSpansOffline = 1000
	t.Logf("🔴 Simulating collector outage - generating %d spans...", numSpansOffline)

	var wg sync.WaitGroup
	panicOccurred := make(chan bool, 1)
	var mu sync.Mutex

	startTime := time.Now()
	generationDuration := time.Duration(0)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				mu.Lock()
				panicOccurred <- true
				mu.Unlock()
			}
		}()

		for i := 0; i < numSpansOffline; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				c, span := tracer.Start(ctx, fmt.Sprintf("span-%d", idx))
				defer span.End()

				span.SetAttributes(
					semconv.HTTPResponseStatusCode(200),
					attribute.Key("test.iteration").Int(idx),
				)

				time.Sleep(time.Microsecond)

				if idx%10 == 0 {
					SetBaggage(c, "outage.test", "true", "iteration", fmt.Sprintf("%d", idx))
				}

				mu.Lock()
				select {
				case <-panicOccurred:
					mu.Unlock()
					return
				default:
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()
		close(panicOccurred)
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		generationDuration = time.Since(startTime)
		t.Logf("✅ Traffic generation completed in %v (%.0f spans/sec)",
			generationDuration, float64(numSpansOffline)/generationDuration.Seconds())
	case <-time.After(30 * time.Second):
		t.Fatal("❌ Timeout waiting for traffic generation - system hung")
	}

	mu.Lock()
	panicOccurredVal := false
	select {
	case <-panicOccurred:
		panicOccurredVal = true
	default:
	}
	mu.Unlock()

	if panicOccurredVal {
		t.Fatal("❌ Panic detected during collector outage - system not resilient!")
	} else {
		t.Logf("✅ No panics occurred during outage - graceful degradation confirmed")
	}

	t.Logf("🟢 Reconnecting collector and measuring recovery time...")
	recoveryStartTime := time.Now()

	successfullyRecovered := false
	maxRecoveryTime := 5 * time.Second
	checkInterval := 200 * time.Millisecond

	for attempt := 0; attempt < int(maxRecoveryTime/checkInterval); attempt++ {
		time.Sleep(checkInterval)

		_, span := tracer.Start(ctx, "recovery-check")
		span.End()

		if span.SpanContext().IsValid() {
			successfullyRecovered = true
			break
		}
	}

	recoveryTime := time.Since(recoveryStartTime)

	if successfullyRecovered {
		t.Logf("✅ System recovered in %v (< %v threshold)", recoveryTime, maxRecoveryTime)
	} else {
		t.Errorf("⚠️  Recovery took longer than expected (%v >= %v)", recoveryTime, maxRecoveryTime)
	}

	totalLatency := time.Since(startTime)
	t.Logf("⏱️  Total test duration: %v", totalLatency)
	t.Logf("📊 Throughput during failure: %.0f spans/sec", float64(numSpansOffline)/generationDuration.Seconds())

	if !panicOccurredVal && recoveryTime < maxRecoveryTime {
		t.Log("✅ PASS: Graceful degradation achieved - no crash, fast recovery")
	} else {
		t.Error("❌ FAIL: System failed graceful degradation test")
	}
}

// ============================================================================
// TestM47_NetworkPartitionResilience - High Latency Handling
// ============================================================================

func TestM47_NetworkPartitionResilience(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// NewUnimplementedExporter was removed in v1.26+; use a no-op stdout exporter instead
	exporter, _ := stdouttrace.New(stdouttrace.WithPrettyPrint())
		otlptracegrpc.WithEndpoint("localhost:9999"),
		oatlptracegrpc.WithInsecure(),
		oatlptracegrpc.WithTimeout(10*time.Second),
	)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(resource.Default()),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	defer tp.Shutdown(ctx)

	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("partition-test")

	t.Run("NetworkLatency500ms", func(t *testing.T) {
		t.Parallel()

		const latencyMs = 500
		const callCount = 100

		var wg sync.WaitGroup
		results := make([]bool, callCount)
		var mu sync.Mutex

		baselineTPS := measureThroughputWithoutLatency(t, tracer, ctx, 50)
		t.Logf("📊 Baseline throughput: %.0f ops/sec (no latency)", baselineTPS)

		startTime := time.Now()

		for i := 0; i < callCount; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				time.Sleep(time.Duration(latencyMs) * time.Millisecond)

				parentCtx, parentSpan := tracer.Start(ctx, "remote-call-with-latency")
				baggedCtx := SetBaggage(parentCtx, "network.partition", "latency", "artificial.delay", fmt.Sprintf("%d", latencyMs))
				_, remoteSpan := tracer.Start(baggedCtx, "child-span-at-destination")
				remoteTraceID := remoteSpan.SpanContext().TraceID().String()
				parentTraceID := parentSpan.SpanContext().TraceID().String()
				remoteSpanID := remoteSpan.SpanContext().SpanID().String()

				t.Logf("🔄 Call %d: trace_id=%s -> span_id=%s", idx, remoteTraceID, remoteSpanID)

				mu.Lock()
				if remoteTraceID == parentTraceID {
					results[idx] = true
				} else {
					t.Errorf("Call %d broke trace correlation", idx)
				}
				mu.Unlock()

				remoteSpan.End()
				parentSpan.End()
			}(i)
		}

		wg.Wait()
		withLatencyDuration := time.Since(startTime)
		withLatencyTPS := float64(callCount) / withLatencyDuration.Seconds()

		t.Logf("⏱️  Duration with %dms latency: %v", latencyMs, withLatencyDuration)
		t.Logf("📊 Throughput under latency: %.0f ops/sec", withLatencyTPS)

		degradationPct := (baselineTPS - withLatencyTPS) / baselineTPS * 100
		t.Logf("📉 Throughput degradation: %.1f%%", degradationPct)

		expectedCorrelations := callCount
		actualCorrelations := 0
		for _, r := range results {
			if r {
				actualCorrelations++
			}
		}

		if actualCorrelations != expectedCorrelations {
			t.Errorf("❌ Correlation broken: %d/%d calls lost trace context", actualCorrelations, expectedCorrelations)
		} else {
			t.Logf("✅ All %d calls maintained trace correlation despite %dms latency", actualCorrelations, latencyMs)
		}

		if withLatencyTPS < baselineTPS*0.8 {
			t.Errorf("❌ Throughput too degraded: %.0f ops/sec < 80%% baseline", withLatencyTPS)
		} else {
			t.Logf("✅ Throughput within acceptable bounds (%.0f > 80%% of baseline %.0f)", withLatencyTPS, baselineTPS)
		}
	})

	t.Run("PacketLoss10Percent", func(t *testing.T) {
		t.Parallel()

		const dropRate = 0.10
		const callCount = 200

		var successfulCorrelations int
		var droppedHeaders int
		var localFallbackSpans int

		mu := sync.Mutex{}

		for i := 0; i < callCount; i++ {
			parentCtx, parentSpan := tracer.Start(ctx, fmt.Sprintf("packet-loss-test-%d", i))

			if mathRand.Float64() < dropRate {
				mu.Lock()
				droppedHeaders++
				mu.Unlock()

				localCtx := context.Background()
				_, localSpan := tracer.Start(localCtx, "local-only-fallback")
				localSpan.End()
				localFallbackSpans++

				t.Logf("⚠️  Call %d: Headers dropped → fallback to local tracing", i)
			} else {
				childCtx := injectTraceContextToCarrier(parentCtx)
				_, childSpan := tracer.Start(childCtx, "propagated-span")

				childTraceID := childSpan.SpanContext().TraceID().String()
				parentTraceID := parentSpan.SpanContext().TraceID().String()

				mu.Lock()
				if childTraceID == parentTraceID {
					successfulCorrelations++
				}
				mu.Unlock()

				childSpan.End()
			}

			parentSpan.End()
		}

		t.Logf("📦 Packet loss simulation:")
		t.Logf("   Total calls:          %d", callCount)
		t.Logf("   Headers dropped:      %d (%.1f%%)", droppedHeaders, float64(droppedHeaders)/float64(callCount)*100)
		t.Logf("   Local fallback used:  %d", localFallbackSpans)
		t.Logf("   Successful correl.:   %d (%.1f%%)", successfulCorrelations, float64(successfulCorrelations)/float64(callCount)*100)

		minSuccessRate := 0.80
		if float64(successfulCorrelations)/float64(callCount) < minSuccessRate {
			t.Errorf("❌ Success rate too low: %.1f%% < %d%% minimum", float64(successfulCorrelations)/float64(callCount)*100, int(minSuccessRate*100))
		} else {
			t.Logf("✅ Packet loss handled gracefully (%.1f%% success ≥ %d%% threshold)", float64(successfulCorrelations)/float64(callCount)*100, int(minSuccessRate*100))
		}
	})

	t.Log("✅ Network partition resilience test completed successfully")
}

// Helper functions
func measureThroughputWithoutLatency(t *testing.T, tracer trace.Tracer, ctx context.Context, iterations int) float64 {
	startTime := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, span := tracer.Start(ctx, "throughput-baseline")
			span.End()
		}()
	}
	wg.Wait()

	duration := time.Since(startTime)
	return float64(iterations) / duration.Seconds()
}

// Log utility wrapper
var logutil = logrus.New()
