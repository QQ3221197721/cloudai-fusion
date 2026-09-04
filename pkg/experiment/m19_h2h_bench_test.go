package experiment

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// M19 Head-to-Head Benchmark vs MLflow (Go-native side only, no Python)
//
// Mirrors mlflow_bench.py's exact work unit:
//   - Start an experiment
//   - Log 2 params (lr="0.001", batch="32")
//   - Log 3 metrics (accuracy=0.90, loss=0.30, f1=0.88)
//   - Complete the experiment
//
// Then measures:
//   - log throughput: N exp/sec for 200 experiments
//   - query latency: filter by hyperparameters + retrieve
//   - storage overhead (bytes per experiment)
//
// MLflow baseline numbers from mlflow_bench.py (file-store mode, 6 runs median):
//   - throughput: ~16 runs/sec
//   - query latency: ~9.8 ms
//   - storage: varies
//
// Run:
//	go test -bench=BenchmarkM19_Throughput -benchtime=1s -count=3 ./pkg/experiment/ 2>&1
//

const (
	nH2HExperiments = 200 // same as mlflow_bench.py default
)

// Reuse nQuerySamples from m19_h2h_test.go (declared there, can be referenced here)

func BenchmarkM19_Throughput(b *testing.B) {
	ctx := context.Background()
	cafRoot := b.TempDir()
	trk, err := NewFSTracker(cafRoot, nil)
	if err != nil {
		b.Fatalf("new tracker: %v", err)
	}

	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30, "f1": 0.88}

	// Warmup
	_, _ = trk.Start(ctx, StartInput{Name: "warmup", Hyperparams: params})
	_ = trk.Complete(ctx, "", "")

	startTime := time.Now()
	for i := 0; i < nH2HExperiments; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "h2h-exp", Hyperparams: params})
		for k, v := range metrics {
			_ = trk.LogMetric(ctx, exp.ID, k, v)
		}
		_ = trk.Complete(ctx, exp.ID, "")
	}
	elapsedSec := time.Since(startTime).Seconds()
	throughput := float64(nH2HExperiments) / elapsedSec
	totalBytes, _ := directorySize(cafRoot)
	bytesPerExp := float64(totalBytes) / float64(nH2HExperiments)

	b.Logf("CAF: %.2f exp/sec (took %.2fs) → %.2f bytes/exp",
		throughput, elapsedSec, float64(bytesPerExp))

	fmt.Printf("\n--- M19 H2H RESULTS ---\n")
	fmt.Printf("| side    | log throughput (runs/sec) | query latency (ms) | storage (bytes/exp) |\n")
	fmt.Printf("|---------|---------------------------|--------------------|---------------------|\n")
	// MLflow baseline numbers
	mlflowThroughput := 16.0
	mlflowQueryLatency := 9.8
	fmt.Printf("| OURS    | %-35.2f | %-17s | %-19s |\n", throughput, "-", fmt.Sprintf("%.0f", float64(bytesPerExp)))
	fmt.Printf("| MLflow  | %-35.2f | %-17.1f | %-19s |\n", mlflowThroughput, mlflowQueryLatency, "-")
	fmt.Printf("---\n\n")

	// Verdict based on throughput comparison (use mlflowThroughput from earlier)
	marginPct := ((throughput - mlflowThroughput) / mlflowThroughput) * 100
	if throughput > mlflowThroughput*1.2 {
		fmt.Printf("WIN: Our tracker is faster than MLflow (%+.1f%%)\n", marginPct)
	} else if throughput < mlflowThroughput*0.8 {
		fmt.Printf("LOSS: MLflow is faster than ours (%+.1f%%)\n", marginPct)
	} else {
		fmt.Printf("COMPARABLE: Throughput within ±20%% of MLflow (%+.1f%%)\n", marginPct)
	}
}

func BenchmarkM19_QueryLatency(b *testing.B) {
	ctx := context.Background()
	cafRoot := b.TempDir()
	trk, _ := NewFSTracker(cafRoot, nil)

	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30}

	for i := 0; i < nH2HExperiments; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "query-test", Hyperparams: params})
		for k, v := range metrics {
			_ = trk.LogMetric(ctx, exp.ID, k, v)
		}
		_ = trk.Complete(ctx, exp.ID, "")
	}

	queryLatencies := []float64{}
	for i := 0; i < nQuerySamples; i++ {
		t0 := time.Now()
		filters := map[string]string{"lr": "0.001", "batch": "32"}
		filtered := trk.SearchByParams(ctx, filters) // Production query path with inverted index
		count := len(filtered)
		_ = count
		queryLatencies = append(queryLatencies, time.Since(t0).Seconds()*1000)
	}
	medianQueryMs := medianFloat(queryLatencies)

	b.Logf("CAF Query latency: %.2fms median (N=%d samples)", medianQueryMs, len(queryLatencies))

	// MLflow baseline numbers
	mlflowQueryLatency := 9.8
	fmt.Printf("\n--- M19 H2H QUERY RESULT ---\n")
	fmt.Printf("| side    | log throughput (runs/sec) | query latency (ms) | storage (bytes/exp) |\n")
	fmt.Printf("|---------|---------------------------|--------------------|---------------------|\n")
	fmt.Printf("| OURS    | %-35s | %-17.1f | %-19s |\n", "-", medianQueryMs, "-")
	fmt.Printf("| MLflow  | %-35s | %-17.1f | %-19s |\n", "-", mlflowQueryLatency, "-")
	fmt.Printf("---\n\n")

	if medianQueryMs < mlflowQueryLatency*0.8 {
		fmt.Printf("WIN: Our query is faster than MLflow (%.1f%% quicker)\n",
			((mlflowQueryLatency-medianQueryMs)/mlflowQueryLatency)*100)
	} else if medianQueryMs > mlflowQueryLatency*1.2 {
		fmt.Printf("LOSS: MLflow query is faster than ours (%.1f%% slower)\n",
			((medianQueryMs-mlflowQueryLatency)/mlflowQueryLatency)*100)
	} else {
		fmt.Printf("COMPARABLE: Query latency within ±20%% of MLflow (%.1f%% diff)\n",
			abs((medianQueryMs-mlflowQueryLatency)/mlflowQueryLatency*100))
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func BenchmarkM19_StorageCost(b *testing.B) {
	ctx := context.Background()
	cafRoot := b.TempDir()
	trk, _ := NewFSTracker(cafRoot, nil)

	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30, "f1": 0.88, "precision": 0.85, "recall": 0.82}
	for i := 0; i < nH2HExperiments; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "storage-test", Hyperparams: params})
		for k, v := range metrics {
			_ = trk.LogMetric(ctx, exp.ID, k, v)
		}
		_ = trk.Complete(ctx, exp.ID, "")
	}

	totalBytes, _ := directorySize(cafRoot)
	bytesPerExp := float64(totalBytes) / float64(nH2HExperiments)

	b.Logf("CAF storage: %d KB (%.2f MB) total → %.2f bytes/exp",
		totalBytes/1024, float64(totalBytes)/1024/1024, float64(bytesPerExp))

	fmt.Printf("\n--- M19 STORAGE RESULT ---\n")
	fmt.Printf("| side    | log throughput (runs/sec) | query latency (ms) | storage (bytes/exp) |\n")
	fmt.Printf("|---------|---------------------------|--------------------|---------------------|\n")
	fmt.Printf("| OURS    | %-35s | %-17s | %-19.0f |\n", "-", "-", bytesPerExp)
	fmt.Printf("| MLflow  | %-35s | %-17s | %-19s |\n", "-", "-", "-")
	fmt.Printf("---\n\n")
}
