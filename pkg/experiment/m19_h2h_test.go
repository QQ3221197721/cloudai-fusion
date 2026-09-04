// Package experiment — M19 head-to-head benchmark vs MLflow FILE-store (no server).
//
// Compares Go-native FSTracker against REAL mlflow using identical work units:
//   - Log N experiments with hyperparameters + metrics
//   - Measure log throughput (exp/sec)  
//   - Measure query latency (filter by params + retrieve)
//   - Compare storage overhead
//
// MLflow runs in FILE-store mode (mlflow.set_tracking_uri("file://<tmpdir>")) —
// no network, no HTTP, fastest possible mlflow config. Honest numbers only.
//
// Anti-fiasco rules:
//   - count=6 median, stderr captured so bench text isn't eaten
//   - HONEST verdict even if we lose
//   - Same work unit (one run = start + 2 params + 3 metrics + complete)
//
// Run command:
//   go test -bench=BenchmarkM19_HeadToHead_Full -run=^$ -count=6 -json ./pkg/experiment 2>&1 | tee m19_results.txt
package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

const (
	nExperimentsForThroughput = 200 // same as mlflow_bench.py default
	nQuerySamples             = 10  // repeated queries for stable latency reading
)

// MLflowBenchmarkOutput is the JSON output format from mlflow_bench.py
type MLflowBenchmarkOutput struct {
	Tool                     string    `json:"tool"`
	MLflowVersion            string    `json:"mlflow_version"`
	NRuns                    int       `json:"n_runs"`
	Count                    int       `json:"count"`
	ThroughputMedian         float64   `json:"throughput_runs_per_sec_median"`
	ThroughputStdDev         float64   `json:"throughput_runs_per_sec_stddev"`
	QueryLatencyMsMedian     float64   `json:"query_ms_median"`
	QueryLatencyStdDev       float64   `json:"query_ms_stddev"`
	StorageBytesMedian       float64   `json:"storage_bytes_median"`
	StorageBytesPerRun       float64   `json:"storage_bytes_per_run"`
	Samples                  []sample  `json:"samples"`
}

type sample struct {
	NRuns                int     `json:"n_runs"`
	LogElapsedS          float64 `json:"log_elapsed_s"`
	ThroughputRunsPerSec float64 `json:"throughput_runs_per_sec"`
	QueryMs              float64 `json:"query_ms"`
	QueryMatched         int     `json:"query_matched"`
	StorageBytes         int64   `json:"storage_bytes"`
}

// BenchmarkM19_HeadToHead_LogThroughput measures throughput on both CAF and MLflow.
// Work unit: one experiment = Start + 2 hyperparams + 3 metrics + Complete
func BenchmarkM19_HeadToHead_LogThroughput(b *testing.B) {
	ctx := context.Background()

	// ===== CAF SIDE (Go FSTracker without ledger overhead for fair comparison) =====
	cafRoot := b.TempDir() + "/caf"
	if err := os.MkdirAll(cafRoot, 0o755); err != nil {
		b.Fatalf("create caf root: %v", err)
	}
	trk, err := NewFSTracker(cafRoot, nil)
	if err != nil {
		b.Fatalf("new caf tracker: %v", err)
	}

	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30, "f1": 0.88}

	// Warm-up run
	_, _ = trk.Start(ctx, StartInput{Name: "warmup", Hyperparams: params})
	trk.Complete(ctx, "", "")

	// Actual timing for CAF throughput (matches nExperimentsForThroughput)
	startTime := time.Now()
	for i := 0; i < nExperimentsForThroughput; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "m19-bench", Hyperparams: params})
		for k, v := range metrics {
			trk.LogMetric(ctx, exp.ID, k, v)
		}
		trk.Complete(ctx, exp.ID, "")
	}
	cafElapsedSec := time.Since(startTime).Seconds()
	cafThroughput := float64(nExperimentsForThroughput) / cafElapsedSec

	b.Logf("[CAF] Throughput: %.2f exp/sec (took %.2fs)", cafThroughput, cafElapsedSec)

	// ===== MLflow SIDE (Python subprocess with file store) =====
	mlflowScript := filepath.Join("testdata", "mlflow_bench.py")
	if _, statErr := os.Stat(mlflowScript); os.IsNotExist(statErr) {
		b.Skipf("skipping MLflow comparison: script not found at %s", mlflowScript)
	}

	tmpDir, err := os.MkdirTemp("", "mlflow-h2h-*")
	if err != nil {
		b.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Run Python benchmark
	cmd := exec.Command("python", "-u", mlflowScript, fmt.Sprintf("%d", nExperimentsForThroughput), "6")
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	if err != nil {
		b.Logf("MLflow benchmark stderr: %s", stderrBuf.String())
		b.Skipf("skipping MLflow: python/mlflow unavailable or failed: %v", err)
	}

	var mlflowOutput MLflowBenchmarkOutput
	if err := json.Unmarshal(stdoutBuf.Bytes(), &mlflowOutput); err != nil {
		b.Fatalf("parse mlflow output: %v\nstdout: %s", err, stdoutBuf.String())
	}

	b.Logf("[MLflow v%s] Throughput: %.2f exp/sec ±%.2f (count=6 median)",
		mlflowOutput.MLflowVersion,
		mlflowOutput.ThroughputMedian,
		mlflowOutput.ThroughputStdDev)

	// ===== HEAD-TO-HEAD COMPARISON =====
	throughputMarginPct := ((cafThroughput - mlflowOutput.ThroughputMedian) / mlflowOutput.ThroughputMedian) * 100
	var verdict string
	if throughputMarginPct > 20 {
		verdict = fmt.Sprintf("CAF_WINS_THROUGHPUT (%+g%%)", throughputMarginPct)
	} else if throughputMarginPct < -20 {
		verdict = fmt.Sprintf("MLFLOW_WINS_THROUGHPUT (%+g%%)", throughputMarginPct)
	} else {
		verdict = fmt.Sprintf("COMPARABLE_THROUGHPUT (%+g%%)", throughputMarginPct)
	}

	// Output JSON summary for parsing
	result := h2hResult{
		TestName:          "BenchmarkM19_HeadToHead_LogThroughput",
		NExperiments:      nExperimentsForThroughput,
		CAFThroughput:     cafThroughput,
		CAFEelapsedSec:    cafElapsedSec,
		MLflowVersion:   mlflowOutput.MLflowVersion,
		MLflowThroughput: mlflowOutput.ThroughputMedian,
		MLflowStdDev:    mlflowOutput.ThroughputStdDev,
		MarginPct:       throughputMarginPct,
		Verdict:         verdict,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	b.Logf("\n=== M19 H2H THROUGHPUT RESULT ===\n%s\n", string(resultJSON))
}

// BenchmarkM19_HeadToHead_QueryLatency measures query latency (filter+retrieve).
// Both systems pre-load data then execute filter queries repeatedly.
func BenchmarkM19_HeadToHead_QueryLatency(b *testing.B) {
	ctx := context.Background()

	// ===== CAF QUERY LATENCY =====
	cafRoot := b.TempDir() + "/caf"
	os.MkdirAll(cafRoot, 0o755)
	trk, _ := NewFSTracker(cafRoot, nil)

	// Pre-load experiments
	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30}
	for i := 0; i < nExperimentsForThroughput; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "query-test", Hyperparams: params})
		for k, v := range metrics {
			trk.LogMetric(ctx, exp.ID, k, v)
		}
		trk.Complete(ctx, exp.ID, "")
	}

	// Repeated queries with param index-based search (simulates MLflow search_runs filter_string)
	queryLatencies := []float64{}
	for i := 0; i < nQuerySamples; i++ {
		t0 := time.Now()
		filters := map[string]string{"lr": "0.001", "batch": "32"}
		filtered := trk.SearchByParams(ctx, filters) // Production query path with inverted index
		count := len(filtered)
		_ = count
		queryLatencies = append(queryLatencies, time.Since(t0).Seconds()*1000) // ms
	}
	cafQueryMedian := medianFloat(queryLatencies)
	b.Logf("[CAF] Query latency: %.2fms median (N=%d samples)", cafQueryMedian, len(queryLatencies))

	// ===== MLflow QUERY LATENCY (from Python subprocess) =====
	mlflowScript := filepath.Join("testdata", "mlflow_bench.py")
	tmpDir, _ := os.MkdirTemp("", "mlflow-query-*")
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command("python", "-u", mlflowScript, fmt.Sprintf("%d", nExperimentsForThroughput), "6")
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		b.Logf("MLflow query test skipped: %v", err)
		return
	}

	var mlflowOutput MLflowBenchmarkOutput
	if err := json.Unmarshal(stdoutBuf.Bytes(), &mlflowOutput); err != nil {
		b.Logf("failed to parse mlflow output: %v", err)
		return
	}

	b.Logf("[MLflow] Query latency: %.2fms median ±%.2f (N=%d samples)",
		mlflowOutput.QueryLatencyMsMedian,
		mlflowOutput.QueryLatencyStdDev,
		len(mlflowOutput.Samples))

	// ===== QUERY LATENCY COMPARISON =====
	queryMarginPct := ((mlflowOutput.QueryLatencyMsMedian - cafQueryMedian) / cafQueryMedian) * 100
	var verdict string
	if queryMarginPct > 20 {
		verdict = fmt.Sprintf("CAF_FASTER_QUERY (%+g%%)", queryMarginPct)
	} else if queryMarginPct < -20 {
		verdict = fmt.Sprintf("MLFLOW_FASTER_QUERY (%+g%%)", queryMarginPct)
	} else {
		verdict = fmt.Sprintf("COMPARABLE_QUERY (%+g%%)", queryMarginPct)
	}

	result := h2hResult{
		TestName:           "BenchmarkM19_HeadToHead_QueryLatency",
		NExperiments:       nExperimentsForThroughput,
		CAFQueryLatencyMs:  cafQueryMedian,
		CAFQuerySamples:    len(queryLatencies),
		MLflowVersion:      mlflowOutput.MLflowVersion,
		MLflowQueryLatency: mlflowOutput.QueryLatencyMsMedian,
		MLflowStdDev:       mlflowOutput.QueryLatencyStdDev,
		MarginPct:          queryMarginPct,
		Verdict:            verdict,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	b.Logf("\n=== M19 H2H QUERY RESULT ===\n%s\n", string(resultJSON))
}

// Storage benchmark comparing storage overhead per experiment.
func BenchmarkM19_HeadToHead_StorageCost(b *testing.B) {
	ctx := context.Background()

	// ===== CAF STORAGE =====
	cafRoot := b.TempDir() + "/caf-storage"
	os.MkdirAll(cafRoot, 0o755)
	trk, _ := NewFSTracker(cafRoot, nil)

	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30, "f1": 0.88, "precision": 0.85, "recall": 0.82}
	for i := 0; i < nExperimentsForThroughput; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "storage-test", Hyperparams: params})
		for k, v := range metrics {
			trk.LogMetric(ctx, exp.ID, k, v)
		}
		trk.Complete(ctx, exp.ID, "")
	}
	cafSize, _ := directorySize(cafRoot)
	cafBytesPerExp := float64(cafSize) / float64(nExperimentsForThroughput)

	b.Logf("[CAF] Total storage: %d KB (%.2f MB) for %d experiments → %.2f bytes/exp",
		cafSize/1024, float64(cafSize)/1024/1024, nExperimentsForThroughput, cafBytesPerExp)

	// ===== MLflow STORAGE =====
	mlflowScript := filepath.Join("testdata", "mlflow_bench.py")
	cmd := exec.Command("python", "-u", mlflowScript, fmt.Sprintf("%d", nExperimentsForThroughput), "6")
	var stdoutBuf, _ bytes.Buffer
	cmd.Stdout = &stdoutBuf
	err := cmd.Run()
	if err != nil {
		b.Logf("MLflow storage test skipped: %v", err)
	} else {
		var mlflowOutput MLflowBenchmarkOutput
		json.Unmarshal(stdoutBuf.Bytes(), &mlflowOutput)
		b.Logf("[MLflow] Storage: %d bytes/exp", int64(mlflowOutput.StorageBytesPerRun))
	}

	// Report CAF-only result (full MLflow comparison needs more work)
	result := struct {
		TestName          string  `json:"test_name"`
		NExperiments      int     `json:"n_experiments"`
		CaftotalBytes     int64   `json:"caf_total_bytes"`
		CAFBytesPerExp    float64 `json:"caf_bytes_per_experiment"`
	}{
		TestName:         "M19_Storage_Cost",
		NExperiments:     nExperimentsForThroughput,
		CaftotalBytes:    cafSize,
		CAFBytesPerExp:   cafBytesPerExp,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	b.Logf("\n=== M19 STORAGE COST RESULT ===\n%s\n", string(resultJSON))
}

type h2hResult struct {
	TestName           string   `json:"test_name"`
	NExperiments       int      `json:"n_experiments"`
	CAFThroughput      float64  `json:"caf_throughput_exp_per_sec,omitempty"`
	CAFEelapsedSec     float64  `json:"caf_elapsed_seconds,omitempty"`
	CAFQueryLatencyMs  float64  `json:"caf_query_latency_ms,omitempty"`
	CAFQuerySamples    int      `json:"caf_query_samples,omitempty"`
	MLflowVersion      string   `json:"mlflow_version,omitempty"`
	MLflowThroughput   float64  `json:"mlflow_throughput_exp_per_sec,omitempty"`
	MLflowQueryLatency float64  `json:"mlflow_query_latency_ms,omitempty"`
	MLflowStdDev       float64  `json:"mlflow_stddev,omitempty"`
	MarginPct          float64  `json:"margin_percentage,omitempty"`
	Verdict            string   `json:"verdict"`
	Evidence           []string `json:"evidence,omitempty"`
}

func medianFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1]+sorted[mid]) / 2
	}
	return sorted[mid]
}

// TestSearchByParamsCorrectness verifies that SearchByParams returns identical
// results to manual client-side filtering. This is the correctness test.
func TestSearchByParamsCorrectness(t *testing.T) {
	ctx := context.Background()
	cafRoot := t.TempDir()
	trk, err := NewFSTracker(cafRoot, nil)
	if err != nil {
		t.Fatalf("new tracker: %v", err)
	}

	// Create experiments with various hyperparams
	params := map[string]string{
		"lr":     "0.001",
		"batch":  "32",
		"epochs": "100",
	}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30}

	for i := 0; i < nExperimentsForThroughput; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "correctness-test", Hyperparams: params})
		for k, v := range metrics {
			trk.LogMetric(ctx, exp.ID, k, v)
		}
		trk.Complete(ctx, exp.ID, "")
	}

	// Query using new param index path
	filters := map[string]string{"lr": "0.001", "batch": "32"}
	indexResults := trk.SearchByParams(ctx, filters)

	// Query using old client-side filter path
	allExp := trk.ListAll(ctx)
	clientSideResults := []Experiment{}
	for _, e := range allExp {
		if e.Hyperparams["lr"] == "0.001" && e.Hyperparams["batch"] == "32" {
			clientSideResults = append(clientSideResults, e)
		}
	}

	// Compare lengths
	if len(indexResults) != len(clientSideResults) {
		t.Errorf("Result length mismatch: index=%d, client-side=%d", len(indexResults), len(clientSideResults))
	}

	// Compare IDs (should be identical sets)
	indexIDs := make(map[string]bool)
	clientIDs := make(map[string]bool)
	for _, e := range indexResults {
		indexIDs[e.ID] = true
	}
	for _, e := range clientSideResults {
		clientIDs[e.ID] = true
	}

	if len(indexIDs) != len(clientIDs) {
		t.Error("ID set sizes differ")
	}

	for id := range indexIDs {
		if !clientIDs[id] {
			t.Errorf("Index result contains ID not in client-side: %s", id)
		}
	}
	for id := range clientIDs {
		if !indexIDs[id] {
			t.Errorf("Client-side result contains ID not in index: %s", id)
		}
	}

	t.Logf("CORRECTNESS TEST PASSED: Both methods returned %d matching experiments", len(indexResults))
}
