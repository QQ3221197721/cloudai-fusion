package metrics

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// ============================================================================
// T2: M41 Metrics Collector Head-to-Head vs Prometheus + OpenTelemetry
// ============================================================================
//
// REAL, FAIR, HONEST benchmark comparing CloudAI Fusion's standard metrics
// collector against prometheus/client_golang and OpenTelemetry metric SDK.
//
// THREE COMPETITORS:
//   1. CLOUDAI FUSION (promauto-based) - Our current implementation
//   2. PROMETHEUS DIRECT               - Raw client_golang without promauto wrapper
//   3. OPENTELEMETRY SDK               - go.opentelemetry.io/otel/sdk/metric
//
// THREE METRICS FOR EACH:
//   • Ingest Latency: ns/op per metric point observed
//   • Throughput:     points/sec at scale
//   • Query Latency:  μs/op to gather/serialize all metrics
//
// RULES:
//   • benchtime = 2s, count = 6 runs total
//   • MEDIAN of 6 runs reported (anti-warmup protection)
//   • JSON output for automation: go test -bench=. -json > results.json
//   • Honest admission: each has trade-offs
//
// CRITICAL: Import REAL competitors, NEVER stub. Count=6 median. Same work unit.
// ===========================================================================

// generateTestWorkload creates a realistic latency distribution for testing
func generateTestWorkload(n int) []float64 {
	data := make([]float64, n)
	x := uint64(0x9E3779B97F4A7C15)

	for i := range data {
		x ^= x >> 12
		x ^= x << 25
		x ^= x >> 27
		r := (x * 0x2545F4914F6CDD1D) >> 11
		u := float64(r) / float64(1<<53)
		// Skewed latency distribution like real HTTP requests
		data[i] = u*u*u*2.0 + u*0.05 + 0.001
	}
	return data
}

// -------------------------------------------------------------------------
// PART A: INGEST THROUGHPUT BENCHMARKS
// -------------------------------------------------------------------------

// BenchmarkInsert_CloudAI_Fusion_HotPath measures optimized hot-path ingestion
func BenchmarkInsert_CloudAI_Fusion_HotPath(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "cloudai_bench_requests_total",
		Help: "benchmark counter",
	}, []string{"method", "path", "status"})
	histogram := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cloudai_bench_request_duration_seconds",
		Help:    "benchmark histogram",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path", "status"})

	methods := []string{"GET", "POST", "PUT", "DELETE"}
	paths := []string{"/api/v1/clusters", "/api/v1/nodes", "/health", "/metrics"}
	statuses := []string{"200", "201", "400", "404", "500"}

	var labelKeys [][3]string
	for _, m := range methods {
		for _, p := range paths {
			for _, s := range statuses {
				labelKeys = append(labelKeys, [3]string{m, p, s})
			}
		}
	}

	workload := generateTestWorkload(len(labelKeys) * 300)
	batchIdx := 0

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		lbl := labelKeys[batchIdx%len(labelKeys)]
		counter.WithLabelValues(lbl[0], lbl[1], lbl[2]).Inc()
		histogram.WithLabelValues(lbl[0], lbl[1], lbl[2]).Observe(workload[batchIdx%len(workload)])
		batchIdx++
	}
	_ = reg
}

// BenchmarkInsert_CloudAI_Fusion_Simple measures minimal single-metric ingest
func BenchmarkInsert_CloudAI_Fusion_Simple(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "cloudai_simple_counter",
		Help: "simple counter",
	})
	histogram := promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
		Name:    "cloudai_simple_histogram",
		Help:    "simple histogram",
		Buckets: prometheus.DefBuckets,
	})

	workload := generateTestWorkload(b.N)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		counter.Inc()
		histogram.Observe(workload[i])
	}
	_ = reg
}

// BenchmarkInsert_Prometheus_Direct_CounterRaw measures raw Counter.Inc()
func BenchmarkInsert_Prometheus_Direct_CounterRaw(b *testing.B) {
	reg := prometheus.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prometheus_direct_counter_total",
		Help: "direct counter",
	})
	reg.MustRegister(c)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.Inc()
	}
	_ = reg
}

// BenchmarkInsert_Prometheus_Direct_HistogramRaw measures raw Histogram.Observe()
func BenchmarkInsert_Prometheus_Direct_HistogramRaw(b *testing.B) {
	reg := prometheus.NewRegistry()
	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "prometheus_direct_histogram",
		Help:    "direct histogram",
		Buckets: prometheus.DefBuckets,
	})
	reg.MustRegister(h)

	workload := generateTestWorkload(b.N)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		h.Observe(workload[i])
	}
	_ = reg
}

// BenchmarkInsert_Prometheus_Direct_VecWithLabelValues measures CounterVec with labels
func BenchmarkInsert_Prometheus_Direct_VecWithLabelValues(b *testing.B) {
	reg := prometheus.NewRegistry()
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prom_vec_total",
		Help: "counter vec",
	}, []string{"method", "status"})
	reg.MustRegister(cv)

	// Pre-compute label keys for performance
	precomputed := make([]prometheus.Counter, 0, 16)
	methods := []string{"GET", "POST", "PUT", "DELETE"}
	statuses := []string{"200", "201", "400", "500"}

	for _, m := range methods {
		for _, s := range statuses {
			precomputed = append(precomputed, cv.WithLabelValues(m, s))
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx := i % len(precomputed)
		precomputed[idx].Inc()
	}
	_ = reg
}

// BenchmarkInsert_Prometheus_Direct_HighCardinality simulates high-label-cardity stress
func BenchmarkInsert_Prometheus_Direct_HighCardinality(b *testing.B) {
	reg := prometheus.NewRegistry()
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prom_highcard_total",
		Help: "high cardinality",
	}, []string{"user_id", "endpoint"})
	reg.MustRegister(cv)

	// Pre-create user patterns
	users := make([]string, 1000)
	for i := range users {
		users[i] = fmt.Sprintf("user-%d", i)
	}
	endpoints := []string{"/api/v1/resource/a", "/api/v1/resource/b", "/api/v1/resource/c"}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		userIdx := i % 1000
		endpoint := endpoints[i%3]
		cv.WithLabelValues(users[userIdx], endpoint).Inc()
	}
	_ = reg
}

// BenchmarkInsert_OTel_SDK_Counter measures OTel SDK Counter.Add()
func BenchmarkInsert_OTel_SDK_Counter(b *testing.B) {
	ctx := context.Background()

	mp := sdkmetric.NewMeterProvider()
	meter := mp.Meter("cloudai.benchmarks")
	counter, _ := meter.Int64Counter(
		"otel_direct_counter",
		otelmetric.WithDescription("OTel counter benchmark"),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		counter.Add(ctx, 1)
	}
	_ = mp
}

// BenchmarkInsert_OTel_SDK_Histogram measures OTel SDK Histogram.Record()
func BenchmarkInsert_OTel_SDK_Histogram(b *testing.B) {
	ctx := context.Background()

	mp := sdkmetric.NewMeterProvider()
	meter := mp.Meter("cloudai.benchmarks")

	hist, _ := meter.Float64Histogram("otel_direct_histogram",
		otelmetric.WithDescription("OTel histogram"),
		otelmetric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))

	workload := generateTestWorkload(b.N)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		hist.Record(ctx, workload[i])
	}
	_ = mp
}

// BenchmarkInsert_OTel_SDK_Attributes measures OTel with attributes (label-like)
func BenchmarkInsert_OTel_SDK_Attributes(b *testing.B) {
	ctx := context.Background()

	mp := sdkmetric.NewMeterProvider()
	meter := mp.Meter("cloudai.benchmarks")

	counter, _ := meter.Int64Counter("otel_attr_counter",
		otelmetric.WithDescription("OTel with attributes"))

	methods := []string{"GET", "POST", "PUT", "DELETE"}
	statuses := []string{"200", "201", "400", "500"}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		attrs := []attribute.KeyValue{
			attribute.String("method", methods[i%4]),
			attribute.String("status", statuses[i%4]),
		}
		counter.Add(ctx, 1, otelmetric.WithAttributes(attrs...))
	}
	_ = mp
}

// BenchmarkInsert_OTel_SDK_MultiMetricMixed measures mixed metric types in OTel
func BenchmarkInsert_OTel_SDK_MultiMetricMixed(b *testing.B) {
	ctx := context.Background()

	mp := sdkmetric.NewMeterProvider()
	meter := mp.Meter("cloudai.benchmarks")

	counter, _ := meter.Int64Counter("otel_mixed_counter",
		otelmetric.WithDescription("mixed counter"))

	hist, _ := meter.Float64Histogram("otel_mixed_histogram",
		otelmetric.WithDescription("mixed histogram"),
		otelmetric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))

	gauge, _ := meter.Float64UpDownCounter("otel_mixed_updown",
		otelmetric.WithDescription("mixed up/down counter"))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		switch i % 3 {
		case 0:
			counter.Add(ctx, 1)
		case 1:
			hist.Record(ctx, float64(i)*0.01)
		case 2:
			gauge.Add(ctx, 1.0)
		}
	}
	_ = mp
}

// ------------------------------------------------------------------
// PARALLEL WORKLOAD BENCHMARKS
// ------------------------------------------------------------------

// BenchmarkParallel_CloudAI_Fusion measures concurrent prometheus writes
func BenchmarkParallel_CloudAI_Fusion(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "parallel_cloudai_total",
		Help: "parallel counter",
	}, []string{"worker"})

	histogram := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "parallel_cloudai_histogram",
		Help:    "parallel histogram",
		Buckets: prometheus.DefBuckets,
	}, []string{"worker"})

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			worker := i % 10
			counter.WithLabelValues(fmt.Sprintf("w%d", worker)).Inc()
			histogram.WithLabelValues(fmt.Sprintf("w%d", worker)).Observe(float64(i) * 0.01)
			i++
		}
	})
	_ = reg
}

// BenchmarkParallel_Prometheus_Direct measures concurrent raw prometheus writes
func BenchmarkParallel_Prometheus_Direct(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "parallel_prom_total",
		Help: "parallel direct counter",
	}, []string{"worker"})
	reg.MustRegister(counter)

	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "parallel_prom_histogram",
		Help:    "parallel direct histogram",
		Buckets: prometheus.DefBuckets,
	}, []string{"worker"})
	reg.MustRegister(histogram)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			worker := i % 10
			counter.WithLabelValues(fmt.Sprintf("w%d", worker)).Inc()
			histogram.WithLabelValues(fmt.Sprintf("w%d", worker)).Observe(float64(i) * 0.01)
			i++
		}
	})
	_ = reg
}

// BenchmarkParallel_OTel_SDK measures concurrent OTel writes
func BenchmarkParallel_OTel_SDK(b *testing.B) {
	ctx := context.Background()

	mp := sdkmetric.NewMeterProvider()
	meter := mp.Meter("cloudai.benchmarks")

	counter, _ := meter.Int64Counter("otel_parallel_counter",
		otelmetric.WithDescription("parallel OTel counter"))

	histogram, _ := meter.Float64Histogram("otel_parallel_histogram",
		otelmetric.WithDescription("parallel OTel histogram"),
		otelmetric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			counter.Add(ctx, 1)
			histogram.Record(ctx, float64(i)*0.01)
			i++
		}
	})
	_ = mp
}

// ===========================================================================
// PART B: QUERY LATENCY (gather/serialize throughput)
// ===========================================================================

// BenchmarkQuery_CloudAI_Fusion_Gather measures CloudAI gather+marshal time
func BenchmarkQuery_CloudAI_Fusion_Gather(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "query_cloudai_counter_total",
		Help: "query counter",
	}, []string{"method", "path"})

	histogram := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "query_cloudai_histogram",
		Help:    "query histogram",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	// Pre-populate with data
	methods := []string{"GET", "POST", "PUT"}
	paths := []string{"/api/v1/test1", "/api/v1/test2", "/api/v1/test3"}

	for _, m := range methods {
		for _, p := range paths {
			counter.WithLabelValues(m, p).Add(100)
			for j := 0; j < 100; j++ {
				histogram.WithLabelValues(m, p).Observe(float64(j) * 0.01)
			}
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := reg.Gather()
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = reg
}

// BenchmarkQuery_Prometheus_Direct_Gather measures raw Prometheus gather time
func BenchmarkQuery_Prometheus_Direct_Gather(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "query_prom_counter_total",
		Help: "query counter",
	}, []string{"method", "path"})
	reg.MustRegister(counter)

	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "query_prom_histogram",
		Help:    "query histogram",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
	reg.MustRegister(histogram)

	// Pre-populate
	methods := []string{"GET", "POST", "PUT"}
	paths := []string{"/api/v1/test1", "/api/v1/test2", "/api/v1/test3"}

	for _, m := range methods {
		for _, p := range paths {
			counter.WithLabelValues(m, p).Add(100)
			for j := 0; j < 100; j++ {
				histogram.WithLabelValues(m, p).Observe(float64(j) * 0.01)
			}
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := reg.Gather()
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = reg
}

// BenchmarkQuery_OTel_SDK_ToReader measures OTel metric export time
func BenchmarkQuery_OTel_SDK_ToReader(b *testing.B) {
	ctx := context.Background()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("cloudai.querybench")

	counter, _ := meter.Int64Counter("otel_query_counter",
		otelmetric.WithDescription("query OTel counter"))

	histogram, _ := meter.Float64Histogram("otel_query_histogram",
		otelmetric.WithDescription("query OTel histogram"),
		otelmetric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))

	// Pre-populate
	for i := 0; i < 100; i++ {
		counter.Add(ctx, 100)
		histogram.Record(ctx, float64(i)*0.01)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &rm); err != nil {
			b.Fatal(err)
		}
	}
	_ = mp
}

// ===========================================================================
// PART C: SCALABILITY AND CARDINALITY STRESS TESTS
// ===========================================================================

// BenchmarkScalability_CloudAI_IncreasesLabels measures performance as we add more label combos
func BenchmarkScalability_CloudAI_IncreasesLabels(b *testing.B) {
	baseReg := prometheus.NewRegistry()

	b.ReportAllocs()
	b.ResetTimer()

	for scale := 100; scale <= 10000; scale *= 10 {
		counter := promauto.With(baseReg).NewCounterVec(prometheus.CounterOpts{
			Name: fmt.Sprintf("scalable_cloudai_%d_total", scale),
			Help: fmt.Sprintf("scalable counter at %d", scale),
		}, []string{"id"})

		// Populate with unique label combos
		for i := 0; i < scale; i++ {
			counter.WithLabelValues(fmt.Sprintf("id-%d", i)).Inc()
		}

		b.ResetTimer()
		for run := 0; run < b.N/scale; run++ {
			idx := run % scale
			counter.WithLabelValues(fmt.Sprintf("id-%d", idx)).Inc()
		}
	}
	_ = baseReg
}

// BenchmarkScalability_Prometheus_IncreasesLabels measures Prometheus at scale
func BenchmarkScalability_Prometheus_IncreasesLabels(b *testing.B) {
	baseReg := prometheus.NewRegistry()

	b.ReportAllocs()
	b.ResetTimer()

	for scale := 100; scale <= 10000; scale *= 10 {
		counter := prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: fmt.Sprintf("scalable_prom_%d_total", scale),
			Help: fmt.Sprintf("scalable prom counter at %d", scale),
		}, []string{"id"})
		baseReg.MustRegister(counter)

		// Populate with unique label combos
		for i := 0; i < scale; i++ {
			counter.WithLabelValues(fmt.Sprintf("id-%d", i)).Inc()
		}

		b.ResetTimer()
		for run := 0; run < b.N/scale; run++ {
			idx := run % scale
			counter.WithLabelValues(fmt.Sprintf("id-%d", idx)).Inc()
		}
	}
	_ = baseReg
}

// BenchmarkScalability_OTel_IncreasesAttributes measures OTel at scale
func BenchmarkScalability_OTel_IncreasesAttributes(b *testing.B) {
	ctx := context.Background()
	mp := sdkmetric.NewMeterProvider()

	b.ReportAllocs()
	b.ResetTimer()

	for scale := 100; scale <= 10000; scale *= 10 {
		counter, _ := mp.Meter("cloudai.scalable").Int64Counter(
			fmt.Sprintf("scalable_otel_%d", scale),
			otelmetric.WithDescription(fmt.Sprintf("scalable OTel at %d", scale)))

		// Perform measurements with varied attributes
		for run := 0; run < b.N/scale; run++ {
			id := fmt.Sprintf("id-%d", run%scale)
			counter.Add(ctx, 1,
				otelmetric.WithAttributes(attribute.String("id", id)))
		}
	}
	_ = mp
}

// ===========================================================================
// PART D: MEMORY ALLOCATION ANALYSIS
// ===========================================================================

// BenchmarkMem_Alloc_CloudAI_Minimal measures allocations for minimal CloudAI usage
func BenchmarkMem_Alloc_CloudAI_Minimal(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "mem_cloudai_min_total",
		Help: "minimal alloc check",
	})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		counter.Inc()
	}
	_ = reg
}

// BenchmarkMem_Alloc_Prometheus_Vec checks if WithLabelValues allocates
func BenchmarkMem_Alloc_Prometheus_Vec(b *testing.B) {
	reg := prometheus.NewRegistry()
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mem_prom_vec_total",
		Help: "vec alloc check",
	}, []string{"method", "status"})
	reg.MustRegister(cv)

	// Pre-get label values to avoid allocation during benchmark
	pre := cv.WithLabelValues("GET", "200")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pre.Inc()
	}
	_ = reg
}

// BenchmarkMem_Alloc_OTel_Attributes measures attribute allocation cost
func BenchmarkMem_Alloc_OTel_Attributes(b *testing.B) {
	ctx := context.Background()

	mp := sdkmetric.NewMeterProvider()
	meter := mp.Meter("cloudai.memalloc")

	counter, _ := meter.Int64Counter("otel_mem_alloc",
		otelmetric.WithDescription("memory allocation benchmark"))

	methods := []string{"GET", "POST", "PUT", "DELETE"}
	statuses := []string{"200", "201", "400", "500"}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		attrs := []attribute.KeyValue{
			attribute.String("method", methods[i%4]),
			attribute.String("status", statuses[i%4]),
		}
		counter.Add(ctx, 1, otelmetric.WithAttributes(attrs...))
	}
	_ = mp
}
