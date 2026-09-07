package devenv_test

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/devenv"
)

// ============================================================================
// M41 DevEnv T2 FLIP Benchmark: Real Prometheus vs OpenTelemetry vs Our SimpleCollector
// 
// Competitors: Real prometheus/client_golang v1.19.0 + go.opentelemetry.io v1.28.0
// Our Implementation: pkg/devenv.SimpleCollector (zero-allocation, memory-bounded)
// 
// Goal: Measure ingest latency and query performance for dev environment metrics
// Expected: Our SimpleCollector beats both by being simpler + zero allocations
// ============================================================================

func BenchmarkPrometheus_WriteCounter(b *testing.B) {
	harness := devenv.NewPrometheusHarness()
	defer harness.Reset()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		label := "label_0"
		err := harness.WriteCounterPoint(label, 1.0)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheus_WriteGauge(b *testing.B) {
	harness := devenv.NewPrometheusHarness()
	defer harness.Reset()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i % 100)
		err := harness.WriteGaugePoint(label, value)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheus_Query(b *testing.B) {
	harness := devenv.NewPrometheusHarness()
	
	// Pre-populate data
	for i := 0; i < 1000; i++ {
		harness.WriteGaugePoint("pre_populated", float64(i))
	}
	defer harness.Reset()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, found := harness.QueryGaugeValue("pre_populated")
		if !found {
			b.Fatal("gauge not found")
		}
	}
}

func BenchmarkOTel_WriteCounter(b *testing.B) {
	harness := devenv.NewOTelHarness()
	defer harness.Reset()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		label := "label_0"
		err := harness.WriteCounterPoint(label, 1)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTel_WriteGauge(b *testing.B) {
	harness := devenv.NewOTelHarness()
	defer harness.Reset()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		label := "label_0"
		value := float64(i % 100)
		err := harness.WriteGaugePoint(label, value)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOTel_Query(b *testing.B) {
	harness := devenv.NewOTelHarness()
	
	// Pre-populate data
	for i := 0; i < 1000; i++ {
		harness.WriteGaugePoint("pre_populated", float64(i))
	}
	defer harness.Reset()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, found := harness.QueryGaugeValue("pre_populated")
		if !found {
			b.Fatal("gauge not found")
		}
	}
}

func BenchmarkSimpleCollector_WriteGauge(b *testing.B) {
	harness := devenv.NewSimpleCollectorHarness(1000)
	defer harness.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := harness.CollectDataPoint(ctx, map[string]interface{}{
			"cpu_usage": 0.75,
			"memory_mb": 512,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSimpleCollector_QueryAggregates(b *testing.B) {
	harness := devenv.NewSimpleCollectorHarness(1000)
	
	// Pre-populate data
	for i := 0; i < 1000; i++ {
		harness.CollectDataPoint(ctx, map[string]interface{}{"test_val": float64(i)})
	}
	defer harness.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := harness.AggregateMetrics("test_val", []string{"avg", "max", "min"})
		if len(result) == 0 {
			b.Fatal("no aggregates computed")
		}
	}
}
