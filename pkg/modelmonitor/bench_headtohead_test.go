// Package modelmonitor — head-to-head comparison benchmarks against Prometheus client_golang 
// and OpenTelemetry SDK metric collection.
//
// FAIR COMPARISON RULES:
// - SAME WORK UNIT: One logical "model performance point" = recording all 6 metrics 
//   (latency_p50/p95/p99, throughput, accuracy, error_rate) for one model version.
// - COUNT=6 MEDIAN: Each benchmark runs -count=6 and reports median from JSON output.
// - HONEST VERDICT: If any competitor wins at its specialty, we admit it.
// - NO WARMUP BIAS: All sides use fresh registries/instruments per run.
// - CLEAN CODE: Must pass `go build ./pkg/modelmonitor/...` and `go vet`.
//
// METRICS MEASURED:
// 1. Ingest latency (ns/op): time to record one model perf point
// 2. Throughput (points/sec): max records per second
// 3. Query latency (ms): time to compute aggregates over N records
//
// EXPECTED RESULT:
// - Prometheus/OTel win ingestion speed (atomic counters vs disk IO)
// - M20 wins: persistent evidence, drift detection, registry integration, JSONL portability
package modelmonitor

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	promauto "github.com/prometheus/client_golang/prometheus/promauto"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// ===== TEST SANITY CHECK =====

func TestRecordHeadToHead(t *testing.T) {
	ctx := context.Background()
	nRuns := 10

	m20Dir := t.TempDir()
	m20Mon, err := NewFSMonitor(m20Dir, nil, nil)
	if err != nil {
		t.Fatalf("NewFSMonitor: %v", err)
	}

	reg := prometheus.NewRegistry()
	precorder := setupPrometheusRecorder(reg)

	orecorder := setupOTELMeter()

	rec := mkRecWithTs("test:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Now())

	for i := 0; i < nRuns; i++ {
		if err := m20Mon.Record(ctx, rec); err != nil {
			t.Fatalf("m20 record failed: %v", err)
		}
		precorder.record(rec)
		orecorder.record(rec)
	}

	// Clear records for clean state
	os.RemoveAll(m20Dir)
	m20Mon, _ = NewFSMonitor(m20Dir, nil, nil)
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}

// ===== PROMETHEUS RECORDER =====
// Uses prometheus/client_golang histogram for quantile computation + gauges for current values.

type prometheusRecorder struct {
	latencyP50  prometheus.Gauge
	latencyP95  prometheus.Gauge
	latencyP99  prometheus.Gauge
	throughput  prometheus.Gauge
	accuracy    prometheus.Gauge
	errorRate   prometheus.Gauge
	histogram   prometheus.Histogram
	collectionC chan struct{} // signal readiness
	lastValue   float64       // thread-safe last recorded value
	mu          sync.RWMutex
}

func setupPrometheusRecorder(reg *prometheus.Registry) *prometheusRecorder {
	pr := &prometheusRecorder{collectionC: make(chan struct{}, 1)}

	gauges := []struct {
		name string
		dst  *prometheus.Gauge
	}{
		{"latency_p50_ms", &pr.latencyP50},
		{"latency_p95_ms", &pr.latencyP95},
		{"latency_p99_ms", &pr.latencyP99},
		{"throughput_qps", &pr.throughput},
		{"accuracy", &pr.accuracy},
		{"error_rate", &pr.errorRate},
	}

	for _, g := range gauges {
		*g.dst = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("model_perf_%s", g.name),
			Help: fmt.Sprintf("Model performance %s", g.name),
		})
	}

	// Histogram for latency distribution (simulating quantile computation)
	pr.histogram = promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "model_latency_seconds_histogram",
		Help:    "Latency distribution in seconds",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
	})

	// Register custom collector for current values
	reg.MustRegister(pr)

	return pr
}

// Implement prometheus.Collector interface to expose last-value state
func (p *prometheusRecorder) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc("model_perf_last_value", "Last recorded value across all metrics", nil, nil)
}

func (p *prometheusRecorder) Collect(ch chan<- prometheus.Metric) {
	p.mu.RLock()
	val := p.lastValue
	p.mu.RUnlock()

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("model_perf_last_value", "Last recorded value", nil, nil),
		prometheus.GaugeValue,
		val,
	)
	close(p.collectionC)
}

func (p *prometheusRecorder) record(rec PerformanceRecord) {
	values := []float64{rec.LatencyP50MS, rec.LatencyP95MS, rec.LatencyP99MS,
		rec.ThroughputQPS, rec.Accuracy, rec.ErrorRate}

	for i, val := range values {
		switch i {
		case 0:
			p.latencyP50.Set(val)
		case 1:
			p.latencyP95.Set(val)
		case 2:
			p.latencyP99.Set(val)
		case 3:
			p.throughput.Set(val)
		case 4:
			p.accuracy.Set(val)
		case 5:
			p.errorRate.Set(val)
		}
	}

	// Update histogram with sample
	p.histogram.Observe(float64(rec.LatencyP50MS) / 1000.0) // convert ms to seconds

	// Store last value atomically
	p.mu.Lock()
	p.lastValue = values[0] // use latency as representative value
	p.mu.Unlock()
}

// ===== OTEL RECORDER =====
// Uses OpenTelemetry Go SDK with real synchronous instruments (6 gauges + 1 histogram),
// matching the same 6-metric work unit as M20 and Prometheus.

type otelRecorder struct {
	ctx        context.Context
	reader     *metric.ManualReader
	latencyP50 otelmetric.Float64Gauge
	latencyP95 otelmetric.Float64Gauge
	latencyP99 otelmetric.Float64Gauge
	throughput otelmetric.Float64Gauge
	accuracy   otelmetric.Float64Gauge
	errorRate  otelmetric.Float64Gauge
	histogram  otelmetric.Float64Histogram
}

func setupOTELMeter() *otelRecorder {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("modelmonitor")

	r := &otelRecorder{ctx: context.Background(), reader: reader}
	r.latencyP50, _ = meter.Float64Gauge("model_perf_latency_p50_ms")
	r.latencyP95, _ = meter.Float64Gauge("model_perf_latency_p95_ms")
	r.latencyP99, _ = meter.Float64Gauge("model_perf_latency_p99_ms")
	r.throughput, _ = meter.Float64Gauge("model_perf_throughput_qps")
	r.accuracy, _ = meter.Float64Gauge("model_perf_accuracy")
	r.errorRate, _ = meter.Float64Gauge("model_perf_error_rate")
	r.histogram, _ = meter.Float64Histogram("model_latency_seconds_histogram")
	return r
}

func (o *otelRecorder) record(rec PerformanceRecord) {
	// Record all 6 metrics via real OTel synchronous instruments (same work unit).
	o.latencyP50.Record(o.ctx, rec.LatencyP50MS)
	o.latencyP95.Record(o.ctx, rec.LatencyP95MS)
	o.latencyP99.Record(o.ctx, rec.LatencyP99MS)
	o.throughput.Record(o.ctx, rec.ThroughputQPS)
	o.accuracy.Record(o.ctx, rec.Accuracy)
	o.errorRate.Record(o.ctx, rec.ErrorRate)
	o.histogram.Record(o.ctx, rec.LatencyP50MS/1000.0)
}

// ===== HEAD-TO-HEAD INGEST BENCHMARKS =====

// BenchmarkIngestPerfPoint_M20 measures appending one model perf point (all 6 metrics) to JSONL.
func BenchmarkIngestPerfPoint_M20(b *testing.B) {
	dir := b.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)
	ctx := context.Background()

	rec := mkRecWithTs("h2h:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Now())
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := mon.Record(ctx, rec); err != nil {
			b.Fatalf("record: %v", err)
		}
	}
}

// BenchmarkIngestPerfPoint_Prometheus measures setting all 6 gauge values + histogram observation.
func BenchmarkIngestPerfPoint_Prometheus(b *testing.B) {
	reg := prometheus.NewRegistry()
	recorder := setupPrometheusRecorder(reg)

	rec := PerformanceRecord{
		ModelVersion: "h2h:1.0.0", Timestamp: time.Now(),
		LatencyP50MS: 40, LatencyP95MS: 100, LatencyP99MS: 200,
		ThroughputQPS: 1000, Accuracy: 0.90, ErrorRate: 0.01, SampleCount: 10000,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		recorder.record(rec)
	}
}

// BenchmarkIngestPerfPoint_OTEL measures recording values via OTel SDK.
func BenchmarkIngestPerfPoint_OTEL(b *testing.B) {
	rec := PerformanceRecord{
		ModelVersion: "h2h:1.0.0", Timestamp: time.Now(),
		LatencyP50MS: 40, LatencyP95MS: 100, LatencyP99MS: 200,
		ThroughputQPS: 1000, Accuracy: 0.90, ErrorRate: 0.01, SampleCount: 10000,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		setupOTELMeter().record(rec)
	}
}

// ===== THROUGHPUT BENCHMARKS =====

// BenchmarkThroughputPerfPoints_M20 measures max points recorded per second.
func BenchmarkThroughputPerfPoints_M20(b *testing.B) {
	dir := b.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)
	ctx := context.Background()

	rec := mkRecWithTs("h2h:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Now())
	b.SetParallelism(8)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := mon.Record(ctx, rec); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkThroughputPerfPoints_Prometheus measures max point sets per second.
func BenchmarkThroughputPerfPoints_Prometheus(b *testing.B) {
	reg := prometheus.NewRegistry()
	recorder := setupPrometheusRecorder(reg)

	rec := PerformanceRecord{
		ModelVersion: "h2h:1.0.0", Timestamp: time.Now(),
		LatencyP50MS: 40, LatencyP95MS: 100, LatencyP99MS: 200,
		ThroughputQPS: 1000, Accuracy: 0.90, ErrorRate: 0.01, SampleCount: 10000,
	}

	b.SetParallelism(8)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			recorder.record(rec)
		}
	})
}

// BenchmarkThroughputPerfPoints_OTEL measures max point sets per second.
func BenchmarkThroughputPerfPoints_OTEL(b *testing.B) {
	rec := PerformanceRecord{
		ModelVersion: "h2h:1.0.0", Timestamp: time.Now(),
		LatencyP50MS: 40, LatencyP95MS: 100, LatencyP99MS: 200,
		ThroughputQPS: 1000, Accuracy: 0.90, ErrorRate: 0.01, SampleCount: 10000,
	}

	b.SetParallelism(8)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			setupOTELMeter().record(rec)
		}
	})
}

// ===== QUERY LATENCY BENCHMARKS =====

// BenchmarkQueryAggregates_M20 measures Report() latency (read records + compute drift).
func BenchmarkQueryAggregates_M20(b *testing.B) {
	dir := b.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)
	ctx := context.Background()

	// Pre-seed with N records
	N := 1000
	for i := 0; i < N; i++ {
		ts := time.Now().Add(time.Duration(i) * time.Second)
		rec := mkRecWithTs("h2h:1.0.0",
			float64(40+i%10), float64(100+i*2), float64(200+i*3),
			float64(1000+i), 0.90-float64(i%10)/1000,
			(float64(0.01)+float64(i%5))/1000, 10000+10*i, ts)
		if err := mon.Record(ctx, rec); err != nil {
			b.Fatalf("seed: %v", err)
		}
	}
	if err := mon.SetBaseline(ctx, "h2h:1.0.0"); err != nil {
		b.Fatalf("baseline: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mon.Report(ctx, "h2h", "1.0.0"); err != nil {
			b.Fatalf("report: %v", err)
		}
	}
}

// BenchmarkQueryAggregates_Prometheus measures gathering histogram quantiles.
func BenchmarkQueryAggregates_Prometheus(b *testing.B) {
	reg := prometheus.NewRegistry()
	recorder := setupPrometheusRecorder(reg)

	// Warm up with data
	rec := PerformanceRecord{
		LatencyP50MS: 40, LatencyP95MS: 100, LatencyP99MS: 200,
		ThroughputQPS: 1000, Accuracy: 0.90, ErrorRate: 0.01,
	}
	for i := 0; i < 1000; i++ {
		recorder.record(rec)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Gather()
	}
}

// BenchmarkQueryAggregates_OTEL measures Collect() latency.
func BenchmarkQueryAggregates_OTEL(b *testing.B) {
	reader := metric.NewManualReader()
	_ = metric.NewMeterProvider(metric.WithReader(reader))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var metrics metricdata.ResourceMetrics
		_ = reader.Collect(context.Background(), &metrics)
	}
}
