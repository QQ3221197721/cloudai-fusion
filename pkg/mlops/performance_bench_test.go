package mlops

import (
	"math/rand"
	"testing"
	"time"
)

// ============================================================================
// M20 Drift Detection - comprehensive throughput benchmarks
// (complements BenchmarkPSIScore1k/KSScore1k/PSIScore10k in monitor_test.go)
// ============================================================================

func BenchmarkDriftDetectionBatchPSI50k(b *testing.B) {
	m := NewMonitor()
	ref := makeNormal(rand.New(rand.NewSource(42)), 50000, 1.0, 1)
	if err := m.RegisterBaseline(FeatureSLO{Feature: "feature-x", Method: MethodPSI, Bins: 10}, ref); err != nil {
		b.Fatalf("Register baseline: %v", err)
	}
	live := makeNormal(rand.New(rand.NewSource(7)), 50000, 1.0, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := m.Score("feature-x", live)
		if err != nil {
			b.Fatal(err)
		}
		_ = res.Score
	}
}

func BenchmarkDriftDetectionBatchKS50k(b *testing.B) {
	m := NewMonitor()
	ref := makeNormal(rand.New(rand.NewSource(43)), 50000, 1.0, 1)
	if err := m.RegisterBaseline(FeatureSLO{Feature: "feature-y", Method: MethodKS}, ref); err != nil {
		b.Fatalf("Register baseline: %v", err)
	}
	live := makeNormal(rand.New(rand.NewSource(8)), 50000, 1.0, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := m.Score("feature-y", live)
		if err != nil {
			b.Fatal(err)
		}
		_ = res.Score
	}
}

func BenchmarkMultiFeatureDriftScan(b *testing.B) {
	m := NewMonitor()
	const nFeatures = 100
	names := make([]string, nFeatures)
	for f := 0; f < nFeatures; f++ {
		names[f] = "feat-" + string(rune('A'+f%26)) + string(rune('0'+f/26))
		ref := makeNormal(rand.New(rand.NewSource(int64(f))), 1000, 0.5, 1)
		if err := m.RegisterBaseline(FeatureSLO{Feature: names[f], Method: MethodPSI, Bins: 10}, ref); err != nil {
			b.Fatalf("Register baseline for %q: %v", names[f], err)
		}
	}
	live := makeNormal(rand.New(rand.NewSource(99)), 1000, 0.5, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for f := 0; f < nFeatures; f++ {
			if _, err := m.Score(names[f], live); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// ============================================================================
// M19 Experiment Tracking - Ed25519 provenance seal / verify latency
// (complements BenchmarkSealRun/VerifyRun in experiment_test.go)
// ============================================================================

func benchRun(nMetrics, nParams int) *Run {
	end := time.Now()
	run := &Run{
		ID:           "run-bench",
		ExperimentID: "exp-bench",
		Name:         "benchmark-run",
		Status:       RunFinished,
		Params:       make(map[string]string, nParams),
		Metrics:      make(map[string][]MetricPoint, nMetrics),
		Artifacts:    []Artifact{{Name: "model.pt", URI: "s3://b/m.pt", SHA256: "deadbeef"}},
		Tags:         map[string]string{"task": "classification"},
		StartTime:    end.Add(-time.Hour),
		EndTime:      &end,
	}
	for p := 0; p < nParams; p++ {
		run.Params["param-"+string(rune('a'+p%26))] = "v" + string(rune('0'+p%10))
	}
	for mm := 0; mm < nMetrics; mm++ {
		name := "metric-" + string(rune('a'+mm%26)) + string(rune('0'+mm/26%10))
		run.Metrics[name] = []MetricPoint{{Value: float64(mm) * 0.01, Timestamp: end, Step: int64(mm)}}
	}
	return run
}

func BenchmarkSealLargeRun(b *testing.B) {
	sealer, err := NewSealer()
	if err != nil {
		b.Fatalf("Create sealer: %v", err)
	}
	run := benchRun(200, 50)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sealer.Seal(run); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyLargeRun(b *testing.B) {
	sealer, err := NewSealer()
	if err != nil {
		b.Fatalf("Create sealer: %v", err)
	}
	run := benchRun(200, 50)
	if _, err := sealer.Seal(run); err != nil {
		b.Fatalf("Seal run: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Verify(run); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSealAndVerifyRoundTrip(b *testing.B) {
	sealer, err := NewSealer()
	if err != nil {
		b.Fatalf("Create sealer: %v", err)
	}
	run := benchRun(10, 5)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prov, err := sealer.Seal(run)
		if err != nil {
			b.Fatal(err)
		}
		_ = prov.Fingerprint
		if err := Verify(run); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// M19 Tracking Store — high-throughput logging operations
// ============================================================================

func BenchmarkLogMetricStream(b *testing.B) {
	store := NewTrackingStore("")
	exp := store.CreateExperiment("bench-exp", nil)
	run, err := store.StartRun(exp.ID, "bench-stream", nil)
	if err != nil {
		b.Fatalf("StartRun: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.LogMetric(run.ID, "score", float64(i)*0.001, int64(i)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStartRunThroughputStore(b *testing.B) {
	store := NewTrackingStore("")
	exp := store.CreateExperiment("bench-exp", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.StartRun(exp.ID, "run", map[string]string{"lr": "0.001"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLatestMetricLookup(b *testing.B) {
	store := NewTrackingStore("")
	exp := store.CreateExperiment("bench-exp", nil)
	run, err := store.StartRun(exp.ID, "bench-lookup", nil)
	if err != nil {
		b.Fatalf("StartRun: %v", err)
	}
	for i := 0; i < 1000; i++ {
		_ = store.LogMetric(run.ID, "loss", 1.0/float64(i+1), int64(i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, ok := store.LatestMetric(run.ID, "loss")
		if !ok {
			b.Fatal("metric not found")
		}
		_ = v
	}
}
