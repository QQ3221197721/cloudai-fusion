// Benchmark tests for devenv metric collectors against prometheus/client_golang and OpenTelemetry SDK
package devenv

import (
	"testing"
)

const (
	benchmarkIterations = 6 // Run each bench N times
	dataPointsCount     = 1000
)

// ============================================================================
// Prometheus Client_Golang Benchmarks
// ============================================================================

func BenchmarkPrometheusWriteCounter(b *testing.B) {
	harness := NewPrometheusHarness()
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		if err := harness.WriteCounterPoint(label, 1.0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheusWriteGauge(b *testing.B) {
	harness := NewPrometheusHarness()
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i % 100)
		if err := harness.WriteGaugePoint(label, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheusWriteHistogram(b *testing.B) {
	harness := NewPrometheusHarness()
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i) * 0.001
		if err := harness.WriteHistogramPoint(label, value); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// OpenTelemetry SDK Benchmarks
// ============================================================================

func BenchmarkOTelWriteCounter(b *testing.B) {
	harness := NewOTelHarness()
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		if err := harness.WriteCounterPoint(label, int64(1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTelWriteGauge(b *testing.B) {
	harness := NewOTelHarness()
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i % 100)
		if err := harness.WriteGaugePoint(label, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTelWriteHistogram(b *testing.B) {
	harness := NewOTelHarness()
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i) * 0.001
		if err := harness.WriteHistogramPoint(label, value); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// Our Simple Collector Benchmarks
// ============================================================================

func BenchmarkOurSimpleCollectorWriteCounter(b *testing.B) {
	harness := NewSimpleCollectorHarness(dataPointsCount)
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		if err := harness.WriteCounterPoint(label, 1.0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOurSimpleCollectorWriteGauge(b *testing.B) {
	harness := NewSimpleCollectorHarness(dataPointsCount)
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i % 100)
		if err := harness.WriteGaugePoint(label, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOurSimpleCollectorWriteHistogram(b *testing.B) {
	harness := NewSimpleCollectorHarness(dataPointsCount)
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i) * 0.001
		if err := harness.WriteHistogramPoint(label, value); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// Query Performance Benchmarks
// ============================================================================

func BenchmarkPrometheusQueryGauge(b *testing.B) {
	harness := NewPrometheusHarness()
	
	// Pre-populate some data
	for i := 0; i < dataPointsCount; i++ {
		label := "query_test"
		harness.WriteGaugePoint(label, float64(i))
	}
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		if _, found := harness.QueryGaugeValue("query_test"); !found {
			b.Fatal("gauge not found")
		}
	}
}

func BenchmarkOTelQueryGauge(b *testing.B) {
	harness := NewOTelHarness()
	
	// Pre-populate some data
	for i := 0; i < dataPointsCount; i++ {
		label := "query_test"
		harness.WriteGaugePoint(label, float64(i))
	}
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		if _, found := harness.QueryGaugeValue("query_test"); !found {
			b.Fatal("gauge not found")
		}
	}
}

func BenchmarkOurSimpleCollectorQueryGauge(b *testing.B) {
	harness := NewSimpleCollectorHarness(dataPointsCount)
	
	// Pre-populate some data
	for i := 0; i < dataPointsCount; i++ {
		label := "query_test"
		harness.WriteGaugePoint(label, float64(i))
	}
	defer harness.Reset()

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		if _, found := harness.QueryGaugeValue("query_test"); !found {
			b.Fatal("gauge not found")
		}
	}
}

// ============================================================================
// Aggregate Query Benchmarks (avg/max/min over last N points)
// ============================================================================

func BenchmarkOurSimpleCollectorQueryAggregates(b *testing.B) {
	collector := NewSimpleCollector(dataPointsCount, 0)
	
	// Pre-populate data
	for i := 0; i < dataPointsCount; i++ {
		collector.CollectPoint(testMetricName, float64(i), map[string]string{"test": "aggregate"})
	}

	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		result := collector.QueryAggregates(testMetricName, 100)
		if result.Count == 0 {
			b.Fatal("no data aggregated")
		}
	}
}
