// Package devenv provides development environment metrics collection benchmarks,
// comparing our simple collector against prometheus/client_golang and OpenTelemetry SDK.
package devenv

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sm "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	testMetricName    = "dev_env_test_metric"
	totalDataPoints   = 1000
	benchmarkN        = totalDataPoints
	testIterations    = 6
)

// ============================================================================
// Prometheus Client_Golang Benchmark Harness
// ============================================================================

type PrometheusHarness struct {
	reg          *prometheus.Registry
	counterVec   *prometheus.CounterVec
	gaugeVec     *prometheus.GaugeVec
	histogramVec *prometheus.HistogramVec
	pointsWritten int
	startTime    time.Time
}

func NewPrometheusHarness() *PrometheusHarness {
	reg := prometheus.NewRegistry()

	counterVec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "benchmark",
			Subsystem: "prometheus",
			Name:      "counter_total",
			Help:      "Benchmark counter metric",
		},
		[]string{"label_set"},
	)

	gaugeVec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "benchmark",
			Subsystem: "prometheus",
			Name:      "gauge_value",
			Help:      "Benchmark gauge metric",
		},
		[]string{"label_set"},
	)

	histogramVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "benchmark",
			Subsystem: "prometheus",
			Name:      "histogram_latency",
			Help:      "Benchmark histogram latency",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"label_set"},
	)

	reg.MustRegister(counterVec, gaugeVec, histogramVec)

	return &PrometheusHarness{
		reg:           reg,
		counterVec:    counterVec,
		gaugeVec:      gaugeVec,
		histogramVec:  histogramVec,
		startTime:     time.Now(),
	}
}

func (p *PrometheusHarness) WriteCounterPoint(label string, value float64) error {
	p.counterVec.WithLabelValues(label).Add(value)
	p.pointsWritten++
	return nil
}

func (p *PrometheusHarness) WriteGaugePoint(label string, value float64) error {
	p.gaugeVec.WithLabelValues(label).Set(value)
	p.pointsWritten++
	return nil
}

func (p *PrometheusHarness) WriteHistogramPoint(label string, value float64) error {
	p.histogramVec.WithLabelValues(label).Observe(value)
	p.pointsWritten++
	return nil
}

func (p *PrometheusHarness) QueryGaugeValue(label string) (float64, bool) {
	metricFamilies, err := p.reg.Gather()
	if err != nil {
		return 0, false
	}

	for _, mf := range metricFamilies {
		if *mf.Name == "benchmark_prometheus_gauge_value" {
			for _, m := range mf.Metric {
				labels := make(map[string]string)
				for _, lp := range m.Label {
					labels[*lp.Name] = *lp.Value
				}

				if ls, ok := labels["label_set"]; ok && ls == label {
					return *m.Gauge.Value, true
				}
			}
		}
	}

	return 0, false
}

func (p *PrometheusHarness) GetMetricsCount() int {
	return p.pointsWritten
}

func (p *PrometheusHarness) Reset() {
	p.pointsWritten = 0
	p.startTime = time.Now()

	// Unregister and re-register to reset all metrics
	p.reg = prometheus.NewRegistry()

	p.counterVec = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "benchmark",
			Subsystem: "prometheus",
			Name:      "counter_total",
			Help:      "Benchmark counter metric",
		},
		[]string{"label_set"},
	)
	p.gaugeVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "benchmark",
			Subsystem: "prometheus",
			Name:      "gauge_value",
			Help:      "Benchmark gauge metric",
		},
		[]string{"label_set"},
	)
	p.histogramVec = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "benchmark",
			Subsystem: "prometheus",
			Name:      "histogram_latency",
			Help:      "Benchmark histogram latency",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"label_set"},
	)

	p.reg.MustRegister(p.counterVec, p.gaugeVec, p.histogramVec)
}

// ============================================================================
// OpenTelemetry SDK Benchmark Harness
// ============================================================================

type OTelHarness struct {
	meterProvider      *sm.MeterProvider
	meter              metric.Meter
	counter            metric.Int64Counter
	updown             metric.Float64UpDownCounter
	histogram          metric.Float64Histogram
	manualReader       *sm.ManualReader
	dataChannel        chan metricdata.ResourceMetrics
	attributesPool     []attribute.KeyValue
	pointsWritten      int
	startTime          time.Time
	collectionComplete chan struct{}
}

func NewOTelHarness() *OTelHarness {
	reader := sm.NewManualReader()
	mp := sm.NewMeterProvider(sm.WithReader(reader))

	// Initialize attributes pool with common label combinations
	attrsPool := make([]attribute.KeyValue, 0, 10)
	for i := 0; i < 10; i++ {
		attrsPool = append(attrsPool, attribute.String("label_set", fmt.Sprintf("label_%d", i)))
	}

	ot := &OTelHarness{
		meterProvider:      mp,
		meter:              mp.Meter("benchmark.otel"),
		attributesPool:     attrsPool,
		manualReader:       reader,
		pointsWritten:      0,
		startTime:          time.Now(),
		collectionComplete: make(chan struct{}, 1),
	}

	// Create test instruments using meter methods
	var err error
	ot.counter, err = ot.meter.Int64Counter("benchmark_counter_total",
		metric.WithDescription("Benchmark counter metric"))
	if err != nil {
		panic(err)
	}

	// UpDownCounter can be used as gauge replacement
	ot.updown, _ = ot.meter.Float64UpDownCounter("proxy_gauge",
		metric.WithDescription("Benchmark up/down counter as gauge"))

	ot.histogram, err = ot.meter.Float64Histogram("benchmark_histogram_latency",
		metric.WithDescription("Benchmark histogram latency"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0))
	if err != nil {
		panic(err)
	}

	return ot
}

func (ot *OTelHarness) WriteCounterPoint(label string, value int64) error {
	attrs := attribute.String("label_set", label)
	ot.counter.Add(context.Background(), value, metric.WithAttributes(attrs))
	ot.pointsWritten++
	return nil
}

func (ot *OTelHarness) WriteGaugePoint(label string, value float64) error {
	// Use UpDownCounter as gauge replacement
	count := int64(value)
	ot.updown.Add(context.Background(), float64(count), metric.WithAttributes(attribute.String("label_set", label)))
	ot.pointsWritten++
	return nil
}

func (ot *OTelHarness) WriteHistogramPoint(label string, value float64) error {
	attrs := attribute.String("label_set", label)
	ot.histogram.Record(context.Background(), value, metric.WithAttributes(attrs))
	ot.pointsWritten++
	return nil
}

func (ot *OTelHarness) QueryGaugeValue(label string) (float64, bool) {
	result := metricdata.ResourceMetrics{}
	err := ot.manualReader.Collect(context.Background(), &result)
	if err != nil || len(result.ScopeMetrics) == 0 {
		return 0, false
	}

	for _, sm := range result.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "proxy_gauge" {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[float64]:
				for _, dp := range data.DataPoints {
					if matchLabel(dp.Attributes.ToSlice(), label) {
						return dp.Value, true
					}
				}
			case metricdata.Gauge[float64]:
				for _, dp := range data.DataPoints {
					if matchLabel(dp.Attributes.ToSlice(), label) {
						return dp.Value, true
					}
				}
			}
		}
	}

	return 0, false
}

// matchLabel checks if the attribute set contains label_set matching the target
func matchLabel(attrs []attribute.KeyValue, target string) bool {
	for _, kv := range attrs {
		if string(kv.Key) == "label_set" && kv.Value.AsString() == target {
			return true
		}
	}
	return false
}

func (ot *OTelHarness) GetMetricsCount() int {
	return ot.pointsWritten
}

func (ot *OTelHarness) Reset() {
	ot.pointsWritten = 0
	ot.startTime = time.Now()
}

// ============================================================================
// Our Simple Collector Harness for Benchmarking
// ============================================================================

type SimpleCollectorHarness struct {
	collector     *SimpleCollector
	pointsWritten int
}

func NewSimpleCollectorHarness(bufferSize int) *SimpleCollectorHarness {
	sc := NewSimpleCollector(bufferSize, 0)
	return &SimpleCollectorHarness{
		collector:     sc,
		pointsWritten: 0,
	}
}

func (h *SimpleCollectorHarness) WriteCounterPoint(label string, value float64) error {
	current := h.pointsWritten
	h.collector.CollectPoint(testMetricName+"_counter", float64(current), map[string]string{"label": label})
	h.pointsWritten++
	return nil
}

func (h *SimpleCollectorHarness) WriteGaugePoint(label string, value float64) error {
	// Store with full label set as part of metric name for easy retrieval
	metricName := fmt.Sprintf("%s_%s", testMetricName, label)
	h.collector.CollectPoint(metricName, value, map[string]string{"label": label})
	h.pointsWritten++
	return nil
}

func (h *SimpleCollectorHarness) WriteHistogramPoint(label string, value float64) error {
	h.collector.CollectPoint(testMetricName+"_histogram", value, map[string]string{"label": label})
	h.pointsWritten++
	return nil
}

func (h *SimpleCollectorHarness) QueryGaugeValue(label string) (float64, bool) {
	// Query using same naming scheme as WriteGaugePoint
	metricName := fmt.Sprintf("%s_%s", testMetricName, label)
	val, found := h.collector.QueryGaugeByName(metricName)
	return val, found
}

func (h *SimpleCollectorHarness) GetMetricsCount() int {
	return h.pointsWritten
}

func (h *SimpleCollectorHarness) Reset() {
	h.pointsWritten = 0
	h.collector = NewSimpleCollector(h.collector.bufferSize, 0)
}
