package metrics

import (
	"fmt"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ============================================================================
// Counter Benchmarks
// ============================================================================

// BenchmarkCounterInc measures the cost of a simple Counter.Inc() operation
// on our pre-registered counter (backed by prometheus/client_golang).
func BenchmarkCounterInc(b *testing.B) {
	counter := promauto.With(prometheus.NewRegistry()).NewCounter(prometheus.CounterOpts{
		Name: "bench_counter_total",
		Help: "benchmark counter",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.Inc()
	}
}

// BenchmarkCounterAdd measures Counter.Add() with a value.
func BenchmarkCounterAdd(b *testing.B) {
	counter := promauto.With(prometheus.NewRegistry()).NewCounter(prometheus.CounterOpts{
		Name: "bench_counter_add_total",
		Help: "benchmark counter add",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.Add(1.5)
	}
}

// BenchmarkCounterIncParallel measures concurrent Counter.Inc() throughput.
func BenchmarkCounterIncParallel(b *testing.B) {
	counter := promauto.With(prometheus.NewRegistry()).NewCounter(prometheus.CounterOpts{
		Name: "bench_counter_parallel_total",
		Help: "benchmark counter parallel",
	})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			counter.Inc()
		}
	})
}

// ============================================================================
// CounterVec (with labels) Benchmarks
// ============================================================================

// BenchmarkCounterVecWithLabelValues measures CounterVec.WithLabelValues().Inc().
func BenchmarkCounterVecWithLabelValues(b *testing.B) {
	cv := promauto.With(prometheus.NewRegistry()).NewCounterVec(prometheus.CounterOpts{
		Name: "bench_counter_vec_total",
		Help: "benchmark counter vec",
	}, []string{"method", "path", "status"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cv.WithLabelValues("GET", "/api/v1/health", "200").Inc()
	}
}

// BenchmarkCounterVecHighCardinality simulates high label cardinality stress.
func BenchmarkCounterVecHighCardinality(b *testing.B) {
	reg := prometheus.NewRegistry()
	cv := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "bench_counter_high_card_total",
		Help: "high cardinality benchmark",
	}, []string{"user_id", "endpoint"})

	// Pre-create 1000 label combinations
	for i := 0; i < 1000; i++ {
		cv.WithLabelValues(fmt.Sprintf("user-%d", i), fmt.Sprintf("/api/v1/resource/%d", i%50)).Inc()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uid := fmt.Sprintf("user-%d", i%1000)
		ep := fmt.Sprintf("/api/v1/resource/%d", i%50)
		cv.WithLabelValues(uid, ep).Inc()
	}
}

// BenchmarkCounterVecCurried benchmarks curried label access pattern.
func BenchmarkCounterVecCurried(b *testing.B) {
	cv := promauto.With(prometheus.NewRegistry()).NewCounterVec(prometheus.CounterOpts{
		Name: "bench_counter_curried_total",
		Help: "curried counter benchmark",
	}, []string{"method", "path", "status"})
	curried := cv.MustCurryWith(prometheus.Labels{"method": "GET"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		curried.WithLabelValues("/api/health", "200").Inc()
	}
}

// ============================================================================
// Gauge Benchmarks
// ============================================================================

// BenchmarkGaugeSet measures Gauge.Set() operation cost.
func BenchmarkGaugeSet(b *testing.B) {
	g := promauto.With(prometheus.NewRegistry()).NewGauge(prometheus.GaugeOpts{
		Name: "bench_gauge",
		Help: "benchmark gauge",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Set(float64(i))
	}
}

// BenchmarkGaugeInc measures Gauge.Inc() operation cost.
func BenchmarkGaugeInc(b *testing.B) {
	g := promauto.With(prometheus.NewRegistry()).NewGauge(prometheus.GaugeOpts{
		Name: "bench_gauge_inc",
		Help: "benchmark gauge inc",
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Inc()
	}
}

// BenchmarkGaugeSetParallel measures concurrent Gauge.Set() throughput.
func BenchmarkGaugeSetParallel(b *testing.B) {
	g := promauto.With(prometheus.NewRegistry()).NewGauge(prometheus.GaugeOpts{
		Name: "bench_gauge_parallel",
		Help: "benchmark gauge parallel",
	})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i float64
		for pb.Next() {
			g.Set(i)
			i++
		}
	})
}

// ============================================================================
// Histogram Benchmarks
// ============================================================================

// BenchmarkHistogramObserve measures Histogram.Observe() latency.
func BenchmarkHistogramObserve(b *testing.B) {
	h := promauto.With(prometheus.NewRegistry()).NewHistogram(prometheus.HistogramOpts{
		Name:    "bench_histogram",
		Help:    "benchmark histogram",
		Buckets: prometheus.DefBuckets,
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Observe(0.025)
	}
}

// BenchmarkHistogramObserveParallel measures concurrent Histogram.Observe().
func BenchmarkHistogramObserveParallel(b *testing.B) {
	h := promauto.With(prometheus.NewRegistry()).NewHistogram(prometheus.HistogramOpts{
		Name:    "bench_histogram_parallel",
		Help:    "benchmark histogram parallel",
		Buckets: prometheus.DefBuckets,
	})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h.Observe(0.05)
		}
	})
}

// BenchmarkHistogramVecObserve measures HistogramVec.WithLabelValues().Observe().
func BenchmarkHistogramVecObserve(b *testing.B) {
	hv := promauto.With(prometheus.NewRegistry()).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bench_histogram_vec",
		Help:    "benchmark histogram vec",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path", "status_code"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hv.WithLabelValues("GET", "/api/v1/clusters", "200").Observe(0.042)
	}
}

// BenchmarkHistogramVecHighBuckets measures histogram with many buckets.
func BenchmarkHistogramVecHighBuckets(b *testing.B) {
	// 50 buckets simulates fine-grained latency tracking
	buckets := prometheus.LinearBuckets(0.001, 0.005, 50)
	hv := promauto.With(prometheus.NewRegistry()).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bench_histogram_many_buckets",
		Help:    "benchmark histogram with 50 buckets",
		Buckets: buckets,
	}, []string{"service"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hv.WithLabelValues("scheduler").Observe(0.042)
	}
}

// ============================================================================
// Registry Operations
// ============================================================================

// BenchmarkRegistryRegister measures the cost of registering a new collector.
func BenchmarkRegistryRegister(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		reg := NewRegistry()
		c := prometheus.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("bench_reg_%d", i),
			Help: "benchmark",
		})
		b.StartTimer()
		_ = reg.Register(fmt.Sprintf("bench_%d", i), c)
	}
}

// BenchmarkRegistryGet measures thread-safe Get() lookup cost.
func BenchmarkRegistryGet(b *testing.B) {
	reg := NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bench_get_counter",
		Help: "benchmark",
	})
	_ = reg.Register("test-metric", c)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.Get("test-metric")
	}
}

// BenchmarkRegistryGetParallel measures concurrent Get() reads.
func BenchmarkRegistryGetParallel(b *testing.B) {
	reg := NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bench_get_parallel_counter",
		Help: "benchmark",
	})
	_ = reg.Register("test-metric", c)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reg.Get("test-metric")
		}
	})
}

// ============================================================================
// Gather/Serialize Benchmark (export throughput)
// ============================================================================

// BenchmarkGather measures the cost of gathering all metrics for export.
func BenchmarkGather(b *testing.B) {
	reg := prometheus.NewRegistry()
	// Register several metric types
	counter := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "bench_gather_counter",
		Help: "gather benchmark counter",
	}, []string{"method"})
	hist := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bench_gather_histogram",
		Help:    "gather benchmark histogram",
		Buckets: prometheus.DefBuckets,
	}, []string{"path"})
	gauge := promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Name: "bench_gather_gauge",
		Help: "gather benchmark gauge",
	}, []string{"node"})

	// Populate metrics
	for i := 0; i < 100; i++ {
		counter.WithLabelValues(fmt.Sprintf("method-%d", i%5)).Inc()
		hist.WithLabelValues(fmt.Sprintf("/path/%d", i%20)).Observe(float64(i) * 0.01)
		gauge.WithLabelValues(fmt.Sprintf("node-%d", i%10)).Set(float64(i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.Gather()
	}
}

// ============================================================================
// Allocation-focused Benchmarks
// ============================================================================

// BenchmarkCounterIncAlloc verifies zero-allocation hot path for Counter.Inc().
func BenchmarkCounterIncAlloc(b *testing.B) {
	counter := promauto.With(prometheus.NewRegistry()).NewCounter(prometheus.CounterOpts{
		Name: "bench_counter_alloc",
		Help: "alloc check",
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.Inc()
	}
}

// BenchmarkHistogramObserveAlloc verifies allocation behavior for Histogram.Observe().
func BenchmarkHistogramObserveAlloc(b *testing.B) {
	h := promauto.With(prometheus.NewRegistry()).NewHistogram(prometheus.HistogramOpts{
		Name:    "bench_hist_alloc",
		Help:    "alloc check",
		Buckets: prometheus.DefBuckets,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Observe(0.05)
	}
}

// BenchmarkCounterVecWithLabelValuesAlloc checks label lookup allocations.
func BenchmarkCounterVecWithLabelValuesAlloc(b *testing.B) {
	cv := promauto.With(prometheus.NewRegistry()).NewCounterVec(prometheus.CounterOpts{
		Name: "bench_cv_alloc",
		Help: "alloc check",
	}, []string{"method", "status"})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cv.WithLabelValues("GET", "200").Inc()
	}
}

// ============================================================================
// Real-world Scenario: HTTPRequestDuration recording pattern
// ============================================================================

// BenchmarkHTTPRequestRecording simulates recording a full HTTP request metric set.
func BenchmarkHTTPRequestRecording(b *testing.B) {
	// Use local registry to avoid polluting global state
	reg := prometheus.NewRegistry()
	duration := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cloudai",
		Subsystem: "http",
		Name:      "bench_request_duration_seconds",
		Help:      "bench",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path", "status_code"})
	total := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Namespace: "cloudai",
		Subsystem: "http",
		Name:      "bench_requests_total",
		Help:      "bench",
	}, []string{"method", "path", "status_code"})
	inflight := promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Namespace: "cloudai",
		Subsystem: "http",
		Name:      "bench_in_flight",
		Help:      "bench",
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inflight.Inc()
		duration.WithLabelValues("GET", "/api/v1/clusters", "200").Observe(0.042)
		total.WithLabelValues("GET", "/api/v1/clusters", "200").Inc()
		inflight.Dec()
	}
}

// ============================================================================
// Concurrent Mixed Workload
// ============================================================================

// BenchmarkMixedWorkloadParallel simulates a production-like mixed metric workload.
func BenchmarkMixedWorkloadParallel(b *testing.B) {
	reg := prometheus.NewRegistry()
	counter := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "bench_mixed_counter",
		Help: "mixed",
	}, []string{"op"})
	hist := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bench_mixed_hist",
		Help:    "mixed",
		Buckets: prometheus.DefBuckets,
	}, []string{"op"})
	gauge := promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "bench_mixed_gauge",
		Help: "mixed",
	})

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			switch i % 3 {
			case 0:
				counter.WithLabelValues("read").Inc()
			case 1:
				hist.WithLabelValues("write").Observe(0.01 * float64(i%100))
			case 2:
				gauge.Set(float64(i))
			}
			i++
		}
	})
}

// ============================================================================
// CollectDBPoolStats Benchmark
// ============================================================================

// BenchmarkCollectDBPoolStats measures the overhead of updating pool metrics.
func BenchmarkCollectDBPoolStats(b *testing.B) {
	stats := DBPoolStats{
		MaxOpen:   100,
		Open:      50,
		InUse:     30,
		Idle:      20,
		WaitCount: 5,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CollectDBPoolStats("primary", stats)
	}
}

// ============================================================================
// Prometheus client_golang Direct Comparison
// ============================================================================

// BenchmarkPrometheusCounterInc_Direct is a direct comparison against
// prometheus/client_golang v1.19.0 Counter.Inc() to establish baseline.
func BenchmarkPrometheusCounterInc_Direct(b *testing.B) {
	reg := prometheus.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prom_direct_counter",
		Help: "direct prometheus counter benchmark",
	})
	reg.MustRegister(c)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

// BenchmarkPrometheusHistogramObserve_Direct is a direct comparison against
// prometheus/client_golang v1.19.0 Histogram.Observe().
func BenchmarkPrometheusHistogramObserve_Direct(b *testing.B) {
	reg := prometheus.NewRegistry()
	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "prom_direct_histogram",
		Help:    "direct prometheus histogram benchmark",
		Buckets: prometheus.DefBuckets,
	})
	reg.MustRegister(h)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Observe(0.042)
	}
}

// BenchmarkPrometheusCounterVec_Direct is a direct comparison against
// prometheus/client_golang v1.19.0 CounterVec.WithLabelValues().Inc().
func BenchmarkPrometheusCounterVec_Direct(b *testing.B) {
	reg := prometheus.NewRegistry()
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prom_direct_counter_vec",
		Help: "direct prometheus counter vec benchmark",
	}, []string{"method", "status"})
	reg.MustRegister(cv)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cv.WithLabelValues("GET", "200").Inc()
	}
}

// BenchmarkPrometheusCounterVecParallel_Direct concurrent CounterVec benchmark.
func BenchmarkPrometheusCounterVecParallel_Direct(b *testing.B) {
	reg := prometheus.NewRegistry()
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "prom_direct_counter_vec_parallel",
		Help: "direct prometheus counter vec parallel benchmark",
	}, []string{"method", "status"})
	reg.MustRegister(cv)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cv.WithLabelValues("POST", "201").Inc()
		}
	})
}

// ============================================================================
// Mutex Contention Test: Registry under parallel writes and reads
// ============================================================================

// BenchmarkRegistryMixedContention simulates write + read contention on Registry.
func BenchmarkRegistryMixedContention(b *testing.B) {
	reg := NewRegistry()
	// Pre-populate
	for i := 0; i < 50; i++ {
		c := prometheus.NewCounter(prometheus.CounterOpts{
			Name: fmt.Sprintf("contention_%d", i),
			Help: "contention",
		})
		_ = reg.Register(fmt.Sprintf("m-%d", i), c)
	}

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Add(2)

	// Readers
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			reg.Get(fmt.Sprintf("m-%d", i%50))
		}
	}()

	// Writers (register/unregister)
	go func() {
		defer wg.Done()
		for i := 0; i < b.N/10; i++ {
			name := fmt.Sprintf("contention-w-%d", i)
			c := prometheus.NewCounter(prometheus.CounterOpts{
				Name: name,
				Help: "contention write",
			})
			_ = reg.Register(name, c)
			reg.Unregister(name)
		}
	}()

	wg.Wait()
}
