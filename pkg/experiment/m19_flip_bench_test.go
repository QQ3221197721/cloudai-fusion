package experiment_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// M19FlipBenchmark tests FLIP M19 H2H challenge: our in-memory tracker vs MLflow file-store
// on top-k query latency AND recall@k for HPO workloads.
func TestM19FlipBenchmark(t *testing.T) {
	t.Skip("Manual benchmark via: go test -run=^$ -bench=M19Flip -benchtime=1s -count=6")
}

// BenchmarkMLflowBaseline runs the MLflow file-store benchmark to establish baseline
func BenchmarkMLflowBaseline(b *testing.B) {
	dataDir := filepath.Join("testdata", "mlflow-baseline-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		b.Fatalf("create data dir: %v", err)
	}

	pythonScript := filepath.Join("testdata", "mlflow_bench.py")
	outputFile := filepath.Join(dataDir, "mlflow_baseline.json")

	// Run Python benchmark (N=1000 runs, count=6 median)
	cmd := exec.Command("python", "-u", pythonScript, "1000", "6")
	output, err := cmd.CombinedOutput()
	if err != nil {
		b.Fatalf("MLflow benchmark failed: %v\n%s", err, output)
	}

	// Write JSON output to file for later parsing
	if err := os.WriteFile(outputFile, output, 0o644); err != nil {
		b.Fatalf("write mlflow baseline JSON: %v", err)
	}

	var result struct {
		QueryMsMedian     float64 `json:"query_ms_median"`
		TopKLatencyMs     map[int]map[string]float64 `json:"topk_latency_ms"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		b.Fatalf("parse MLflow JSON: %v", err)
	}

	// Log results
	b.Logf("MLflow baseline query_ms_median: %.2f ms", result.QueryMsMedian)
	for k, v := range result.TopKLatencyMs {
		b.Logf("MLflow top-k% d: latency=%.2fms recall=%.4f", k, v["latency_ms_median"], v["recall_at_k"])
	}
}

// BenchmarkOurTracker tests our FSTracker's TopKByMetric implementation
func BenchmarkOurTracker(b *testing.B) {
	tracker, err := NewFSTracker(filepath.Join("testdata", "our-bench"), nil)
	if err != nil {
		b.Fatalf("create tracker: %v", err)
	}

	// Build 1000 experiments with hyperparams and metrics
	ctx := context.Background()
	const N = 1000
	params := []struct{ lr string; batch int }{
		{"0.001", 32}, {"0.001", 64}, {"0.01", 32}, {"0.01", 64},
	}
	for i := 0; i < N; i++ {
		_, err := tracker.Start(ctx, StartInput{
			Name:      "hpo-run",
			Hyperparams: map[string]string{
				"lr":    params[i%4].lr,
				"batch": fmt.Sprintf("%d", params[i%4].batch),
			},
		})
		if err != nil {
			b.Fatalf("start experiment %d: %v", i, err)
		}
	}

	// Warm up: build hot index once
	_, _ = tracker.TopKByMetric(ctx, map[string]string{"lr": "0.001", "batch": "32"}, "metric0", 10)

	// Reset timer for actual benchmark
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := tracker.TopKByMetric(ctx, map[string]string{"lr": "0.001", "batch": "32"}, "metric0", 10)
		if err != nil {
			b.Fatalf("TopKByMetric failed: %v", err)
		}
	}
}
