package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Single Entry Logging Benchmarks
// ============================================================================

// BenchmarkLoggerInfo measures baseline log entry creation overhead.
func BenchmarkLoggerInfo(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info("benchmark message")
	}
}

// BenchmarkLoggerDebug measures debug-level logging overhead.
func BenchmarkLoggerDebug(b *testing.B) {
	l := New(Config{
		Level:     "debug",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Debug("benchmark debug message")
	}
}

// BenchmarkLoggerError measures error-level logging overhead.
func BenchmarkLoggerError(b *testing.B) {
	l := New(Config{
		Level:     "error",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	err := fmt.Errorf("benchmark error %d", b.N%100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Error(err)
	}
}

// ============================================================================
// Context-Aware Logging Benchmarks
// ============================================================================

// BenchmarkWithContextTraceID measures WithContext overhead with trace ID.
func BenchmarkWithContextTraceID(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	ctx := context.Background()
	ctx = WithTraceID(ctx, "cf85e592f1b76a3f4f2e7b6c8d9e0a1b")
	ctx = WithSpanID(ctx, "1c2d3e4f5a6b7c8d")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry := l.WithContext(ctx)
		entry.Info("with-trace-context")
	}
}

// BenchmarkWithContextFullFields measures WithContext with all correlation fields.
func BenchmarkWithContextFullFields(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-id-123")
	ctx = WithSpanID(ctx, "span-id-456")
	ctx = WithRequestID(ctx, "request-id-789")
	ctx = WithUserID(ctx, "user-abc")
	ctx = WithComponent(ctx, "apiserver")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry := l.WithContext(ctx)
		entry.Info("full-correlation-context")
	}
}

// ============================================================================
// Field Injection Benchmarks
// ============================================================================

// BenchmarkWithFieldString measures single string field injection cost.
func BenchmarkWithFieldString(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("key", fmt.Sprintf("value-%d", i)).Info("field-test")
	}
}

// BenchmarkWithFieldsMultiple measures multiple field injection overhead.
func BenchmarkWithFieldsMultiple(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithFields(logrus.Fields{
			"int_field":   i,
			"string_field": fmt.Sprintf("str-%d", i),
			"bool_field":  true,
			"float_field": float64(i) * 0.001,
		}).Info("multi-field")
	}
}

// BenchmarkWithMapLarge measures large map field injection.
func BenchmarkWithMapLarge(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	fields := make(map[string]interface{}, 20)
	for i := 0; i < 20; i++ {
		fields[fmt.Sprintf("field-%d", i)] = fmt.Sprintf("value-%d", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithFields(logrus.Fields(fields)).Info("large-map")
	}
}

// ============================================================================
// Structured Logging Benchmarks
// ============================================================================

// BenchmarkJSONMarshalSingleEntry measures JSON encoding latency for a structured log entry.
func BenchmarkJSONMarshalSingleEntry(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	type Payload struct {
		UserID   string `json:"user_id"`
		Action   string `json:"action"`
		Duration int64  `json:"duration_ms"`
	}

	payload := Payload{
		UserID:   "user-123",
		Action:   "create_resource",
		Duration: 42,
	}

	jsonBytes, _ := json.Marshal(payload)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("payload", json.RawMessage(jsonBytes)).Info("structured-log")
	}
}

// ============================================================================
// High-Level Logging Pattern Benchmarks
// ============================================================================

// BenchmarkInfopattern simulates production INFO-level pattern.
func BenchmarkInfoPattern(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "apiserver",
	})

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-abcd1234")
	ctx = WithRequestID(ctx, "req-efgh5678")
	ctx = WithUserID(ctx, "user-xyz")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithContext(ctx).Info("request-started", "method", "GET", "path", "/api/v1/clusters")
	}
}

// BenchmarkErrorPattern simulates production ERROR-level pattern.
func BenchmarkErrorPattern(b *testing.B) {
	l := New(Config{
		Level:     "error",
		Format:    "json",
		Output:    io.Discard,
		Component: "scheduler",
	})

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-error1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := fmt.Errorf("failed to schedule workload: %w", nil) // dummy err
		l.WithContext(ctx).WithError(err).Error("scheduling-failure")
	}
}

// ============================================================================
// Level Filter Benchmarks (Critical Performance Metric)
// ============================================================================

// BenchmarkLogDebugUnderInfoLevel tests that DEBUG logs are fast-pathed when level is INFO.
// This is critical: filtered logs should have near-zero cost.
func BenchmarkLogDebugUnderInfoLevel(b *testing.B) {
	l := New(Config{
		Level:     "info", // DEBUG messages are above threshold
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Debug("filtered-out-debug-message")
	}
}

// BenchmarkLogWarningUnderErrorLevel measures WARNING filtering under ERROR level.
func BenchmarkLogWarningUnderErrorLevel(b *testing.B) {
	l := New(Config{
		Level:     "error",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Warn("filtered-out-warning")
	}
}

// BenchmarkLogDisabledLevel measures completely disabled log level cost.
func BenchmarkLogDisabledLevel(b *testing.B) {
	l := New(Config{
		Level:     "fatal", // All below fatal are disabled
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Debug("disabled-debug")
		l.Info("disabled-info")
		l.Warn("disabled-warn")
		l.Error("disabled-error")
	}
}

// ============================================================================
// Sampling Benchmarks
// ============================================================================

// BenchmarkLogSamplerShouldLog measures sampling decision overhead per message.
func BenchmarkLogSamplerShouldLog(b *testing.B) {
	sampler := NewLogSampler(DefaultSamplerConfig())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sampler.ShouldLog(fmt.Sprintf("sample-key-%d", i%100))
	}
}

// BenchmarkSampledLogFrequentSameKey tests sampler behavior under frequent duplicate keys.
func BenchmarkSampledLogFrequentSameKey(b *testing.B) {
	l := New(Config{
		Level:           "info",
		Format:          "json",
		Output:          io.Discard,
		Component:       "bench",
		EnableSampling:  true,
		SamplerConfig:   func() *SamplerConfig { c := DefaultSamplerConfig(); return &c }(),
	})

	ctx := context.Background()
	sampleKey := "high-freq-alert"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.SampledLog(ctx, logrus.InfoLevel, sampleKey, "repeating alert")
	}
}

// ============================================================================
// Multi-Writer Sink Benchmark (MultiWriter)
// ============================================================================

// BenchmarkMultiWriterLog measures performance impact of additional output sinks.
func BenchmarkMultiWriterLog(b *testing.B) {
	var buf bytes.Buffer
	l := New(Config{
		Level:             "info",
		Format:            "json",
		Output:            io.Discard, // Primary discard
		AdditionalOutputs: []io.Writer{&buf}, // One additional writer
		Component:         "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("iteration", i).Info("multi-sink")
	}
}

// BenchmarkNoAdditionalSink benchmarks primary-only output.
func BenchmarkNoAdditionalSink(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("iteration", i).Info("primary-only")
	}
}

// ============================================================================
// Concurrency Patterns
// ============================================================================

// BenchmarkParallelLogSequential writes in parallel but sequentially within goroutines.
func BenchmarkParallelLogSequential(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			l.WithField("goroutine", i).Info("parallel-log")
			i++
		}
	})
}

// BenchmarkConcurrentWritesSync measures actual concurrent write performance.
func BenchmarkConcurrentWritesSync(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	var wg sync.WaitGroup
	b.ResetTimer()
	
	// Run 4 goroutines writing concurrently
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < b.N/4; i++ {
				l.WithField("gid", gid).Info(fmt.Sprintf("concurrent-write-%d", i))
			}
		}(g)
	}
	
	wg.Wait()
	b.StopTimer()
}

// ============================================================================
// Text Formatter Benchmark
// ============================================================================

// BenchmarkTextFormatter measures text formatter overhead vs JSON.
func BenchmarkTextFormatter(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "text",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("key", fmt.Sprintf("value-%d", i)).Info("text-formatter")
	}
}

// ============================================================================
// Log Field Encoding Benchmarks
// ============================================================================

// BenchmarkFieldEncodingInt measures integer field encoding overhead.
func BenchmarkFieldEncodingInt(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("counter", i).Info("counter-field")
	}
}

// BenchmarkFieldEncodingFloat64 measures float64 field encoding overhead.
func BenchmarkFieldEncodingFloat64(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("latency_sec", float64(i)*0.001).Info("float-field")
	}
}

// BenchmarkFieldEncodingBool measures boolean field encoding overhead.
func BenchmarkFieldEncodingBool(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	flag := false
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("enabled", i%2==0).Info("bool-field")
		flag = !flag
	}
}

// ============================================================================
// Dynamic Level Change Benchmark
// ============================================================================

// BenchmarkDynamicLevelChange measures SetLevel overhead.
func BenchmarkDynamicLevelChange(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.SetLevel(fmt.Sprintf("level-%d", i%4)) // switch between debug/info/warn/error
	}
}

// ============================================================================
// Allocations Benchmark
// ============================================================================

// BenchmarkLogAllocInfo measures allocations for basic info logging.
func BenchmarkLogAllocInfo(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithField("message", fmt.Sprintf("msg-%d", i)).Info("allocation-check")
	}
}

// BenchmarkLogAllocContext measures allocation cost of context-aware logging.
func BenchmarkLogAllocContext(b *testing.B) {
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})
	ctx := context.Background()
	ctx = WithTraceID(ctx, "test-trace-id")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithContext(ctx).WithField("i", i).Info("context-allocation")
	}
}

// ============================================================================
// Comparison Against Zap (Industry Standard) Numbers
// ============================================================================

// BenchmarkLoggingVsZap compares our structured logging overhead against
// Open-source industry numbers. According to zap's official benchmarks:
// - info level (structured): ~200ns/op
// - debug level (when filtered): <50ns/op (fast-path)
// 
// Our implementation should match or exceed these baselines.
func BenchmarkLoggingVsZap(b *testing.B) {
	// Test case 1: Info level with moderate fields (target: ~200ns/op like zap)
	l := New(Config{
		Level:     "info",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.WithFields(logrus.Fields{
			"ts": time.Now().UnixNano(),
			"level": "info",
			"msg": "operation",
			"op": "request-handled",
			"dur_ms": float64(i%1000),
		}).Info("operation-complete")
	}
}

// BenchmarkFilteredLogsZeroCost measures the critical fast-path where
// disabled log levels return immediately without marshalling.
func BenchmarkFilteredLogsZeroCost(b *testing.B) {
	// Setup logger at ERROR level only
	l := New(Config{
		Level:     "error",
		Format:    "json",
		Output:    io.Discard,
		Component: "bench",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// These should be fast-paths and not marshal anything
		l.Debug("debug-filtered")
		l.Info("info-filtered")
		l.Warn("warn-filtered")
	}
}
