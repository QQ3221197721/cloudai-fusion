// Package experiment — Module 19 M19 head-to-head benchmark vs MLflow FILE-store.
//
// This test runs REAL mlflow against our FSTracker using identical work units:
//   - Log N experiments with hyperparams + metrics
//   - Measure log throughput (exp/sec)
//   - Measure query latency (filter by hyperparams + retrieve)
//   - Compare storage overhead
//
// Uses MLflow's local file store (no server!) — same as m19.py but invoked via Go subprocess.
// Anti-fiasco rules:
//   - count=6 median, stderr captured so bench text isn't eaten
//   - HONEST verdict on where we win/lose
//   - Storage cost comparisons (JSON vs MLflow Parquet/Delta)
//
// Run with: go test -bench=M19_H2H_ -run=^$ -count=6 -json ./pkg/experiment
package experiment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// M19BenchmarkResult contains results from one sample run of the H2H comparison.
type M19BenchmarkResult struct {
	CAF        CAFResult      `json:"caf"`
	MLflow     MLflowResult   `json:"mlflow"`
	Margins    ComparisonMap  `json:"margins"`
	Verdict    string         `json:"verdict"`
	Evidence   EvidenceReport `json:"evidence"`
}

// CAFResult captures FSTracker performance metrics.
type CAFResult struct {
	LogThroughput float64 `json:"log_throughput_exp_per_sec"` // experiments per second
	QueryLatency  float64 `json:"query_latency_ms"`           // query latency in milliseconds
	StorageBytes  int64   `json:"storage_bytes"`              // total storage used
	NExperiments  int     `json:"n_experiments"`
}

// MLflowResult captures MLflow file-store performance metrics.
type MLflowResult struct {
	LogThroughput float64 `json:"log_throughput_exp_per_sec"`
	QueryLatency  float64 `json:"query_latency_ms"`
	StorageBytes  int64   `json:"storage_bytes"`
	NExperiments  int     `json:"n_experiments"`
	Version       string  `json:"mlflow_version"`
}

// ComparisonMap holds percentage differences (CAF wins positive).
type ComparisonMap map[string]string

// EvidenceReport provides defensible claims based on the numbers.
type EvidenceReport struct {
	WinningSide string   `json:"winning_side"` // "caf", "mlflow", or "comparable"
	KeyMetrics  []string `json:"key_metrics"`
	Caveats     []string `json:"caveats"`
}

// BenchmarkM19HeadToHead runs the full comparison and outputs JSON result.
func BenchmarkM19HeadToHead(b *testing.B) {
	b.Helper()

	// Work unit: 200 experiments each
	nExperiments := 200

	// Create temp directories
	cafRoot := b.TempDir() + "/caf"
	mlflowRoot := b.TempDir() + "/mlflow"

	if err := os.MkdirAll(cafRoot, 0o755); err != nil {
		b.Fatalf("create caf root: %v", err)
	}
	if err := os.MkdirAll(mlflowRoot, 0o755); err != nil {
		b.Fatalf("create mlflow root: %v", err)
	}

	// Setup CAF FSTracker
	trk, err := NewFSTracker(cafRoot, nil)
	if err != nil {
		b.Fatalf("new caf tracker: %v", err)
	}

	ctx := context.Background()

	// Pre-load CAF: 200 experiments with params + metrics
	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30, "f1": 0.88}
	var cafExpIDs []string

	for i := 0; i < nExperiments; i++ {
		exp, err := trk.Start(ctx, StartInput{Name: "m19-bench", Hyperparams: params})
		if err != nil {
			b.Fatalf("caf start: %v", err)
		}
		cafExpIDs = append(cafExpIDs, exp.ID)
		for k, v := range metrics {
			if err := trk.LogMetric(ctx, exp.ID, k, v); err != nil {
				b.Fatalf("caf log: %v", err)
			}
		}
		if err := trk.Complete(ctx, exp.ID, ""); err != nil {
			b.Fatalf("caf complete: %v", err)
		}
	}

	// CAF Log Throughput Test
	cafLogStart := make([]byte, 0, b.N*100)
	cafTimer := testing.T{...}
	b.ResetTimer()
	cafLogTime := b.N // placeholder

	// Use actual timing for throughput
	startTime := false
	var cafElapsed float64
	tmpB := &testing.T{}
	b.Run("caf_log_throughput", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			trkTmp, _ := NewFSTracker(b.TempDir(), nil)
			exp, _ := trkTmp.Start(ctx, StartInput{Name: "perf", Hyperparams: params})
			for k, v := range metrics {
				trkTmp.LogMetric(ctx, exp.ID, k, v)
			}
			trkTmp.Complete(ctx, exp.ID, "")
		}
	})

	// Record CAF throughput from the named sub-benchmark
	// We'll use a simpler approach: direct measurement outside -bench loop

	// Query Latency Test (filter by lr=0.001 AND batch=32)
	queryTimes := []float64{}
	for i := 0; i < 10; i++ {
		qStart := getNanoTime()
		all := trk.List(ctx)
		count := 0
		for _, e := range all {
			if e.Hyperparams["lr"] == "0.001" && e.Hyperparams["batch"] == "32" {
				count++
			}
		}
		_ = count
		qElasped := (getNanoTime() - qStart) / 1e6 // ms
		queryTimes = append(queryTimes, qElasped)
	}
	qMedian := medianFloat(queryTimes)
	cafQueryLatencyMs := qMedian

	// Store size
	cafSize, _ := directorySize(cafRoot)
	cafSize /= (nExperiments + 1) // approximate per-run storage

	// Now invoke Python MLflow benchmark
	mlflowScript := filepath.Join(filepath.Dir(__FILE__), "testdata", "mlflow_bench.py")
	cmd := exec.Command("python", "-u", mlflowScript, fmt.Sprintf("%d", nExperiments), "6")
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.Env = append(os.Environ(), fmt.Sprintf("TEMP_DIR=%s", mlflowRoot))

	if err := cmd.Run(); err != nil {
		b.Logf("MLflow benchmark stderr: %s", stderrBuf.String())
		b.Logf("MLflow benchmark failed (expected if python/mlflow unavailable): %v", err)
		b.Skipf("skipping MLflow comparison - no Python/MLflow: %v", err)
	}

	// Parse MLflow JSON output
	var mlflowOutput MLflowResult
	if err := json.Unmarshal(stdoutBuf.Bytes(), &mlflowOutput); err != nil {
		b.Fatalf("parse mlflow output: %v\nstdout: %s", err, stdoutBuf.String())
	}

	// Report results as a summary line
	result := &M19BenchmarkResult{
		CAF: CAFResult{
			LogThroughput: float64(nExperiments) / cafElapsed, // will be set properly below
			QueryLatency:  cafQueryLatencyMs,
			StorageBytes:  cafSize,
			NExperiments:  nExperiments,
		},
		MLflow:     mlflowOutput,
		Verdict:    determineVerdict(&CAFResult{}, &mlflowOutput),
		Evidence:   generateEvidence(&CAFResult{}, &mlflowOutput),
	}

	// Output simplified JSON for parsing
	result.CAF.LogThroughput = cafLogThroughput // TODO: calculate properly
	result.CAF.StorageBytes = cafSize

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	b.Logf("M19 H2H BENCHMARK RESULT:\n%s", string(resultJSON))
}

var cafLogThroughput float64

func init() {
	// Calculate CAF log throughput separately for fair comparison
	tmpRoot := os.TempDir() + "/caf-m19-init"
	defer os.RemoveAll(tmpRoot)
	os.MkdirAll(tmpRoot, 0o755)

	trk, _ := NewFSTracker(tmpRoot, nil)
	ctx := context.Background()
	nExps := 200
	params := map[string]string{"lr": "0.001", "batch": "32"}
	metrics := map[string]float64{"accuracy": 0.90, "loss": 0.30, "f1": 0.88}

	startTime := getTimeMS()
	for i := 0; i < nExps; i++ {
		exp, _ := trk.Start(ctx, StartInput{Name: "perf", Hyperparams: params})
		for k, v := range metrics {
			trk.LogMetric(ctx, exp.ID, k, v)
		}
		trk.Complete(ctx, exp.ID, "")
	}
	elapsedSec := (getTimeMS() - startTime) / 1000.0

	cafLogThroughput = float64(nExps) / elapsedSec
}

// determineVerdict compares CAF vs MLflow and declares winner.
func determineVerdict(caf *CAFResult, mlflow *MLflowResult) string {
	if mlflow.LogThroughput == 0 || caf.LogThroughput == 0 {
		return "N/A"
	}

	thrustMargin := (caf.LogThroughput - mlflow.LogThroughput) / mlflow.LogThroughput * 100
	queryMargin := (mlflow.QueryLatency - caf.QueryLatency) / caf.QueryLatency * 100

	if thrustMargin > 20 || queryMargin > 20 {
		return "CAF_WINS"
	} else if mlflow.LogThroughput > caf.LogThroughput*1.2 {
		return "MLFLOW_WINS_LOG_THROUGHPUT"
	} else if caf.QueryLatency > mlflow.QueryLatency*1.2 {
		return "MLFLOW_WINS_QUERY_LATENCY"
	}
	return "COMPARABLE"
}

// generateEvidence creates defensible claims about performance differences.
func generateEvidence(caf *CAFResult, mlflow *MLflowResult) EvidenceReport {
	report := EvidenceReport{
		WinningSide: determineVerdict(caf, mlflow),
		KeyMetrics:  []string{},
		Caveats:     []string{},
	}

	if caf.LogThroughput > 0 && mlflow.LogThroughput > 0 {
		diff := caf.LogThroughput - mlflow.LogThroughput
		report.KeyMetrics = append(report.KeyMetrics,
			fmt.Sprintf("Log Throughput: CAF %.2f exp/s vs MLflow %.2f exp/s (diff: %.2f)",
				caf.LogThroughput, mlflow.LogThroughput, diff))
	}

	if caf.QueryLatency > 0 && mlflow.QueryLatency > 0 {
		report.KeyMetrics = append(report.KeyMetrics,
			fmt.Sprintf("Query Latency: CAF %.2fms vs MLflow %.2fms",
				caf.QueryLatency, mlflow.QueryLatency))
	}

	report.Caveats = append(report.Caveats,
		"MLflow uses file store mode (no network/server overhead)")
	report.Caveats = append(report.Caveats,
		"CAF includes JSON atomic persistence (write tmp + rename)")
	report.Caveats = append(report.Caveits,
		"CAF has optional attestation layer (disabled for this benchmark)")

	return report
}

func getTimeMS() int64 {
	return time.Now().UnixNano() / 1e6
}

func getNanoTime() int64 {
	return time.Now().UnixNano()
}

func medianFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sortFloat64(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func sortFloat64(vals []float64) {
	for i := 0; i < len(vals)-1; i++ {
		for j := i + 1; j < len(vals); j++ {
			if vals[i] > vals[j] {
				vals[i], vals[j] = vals[j], vals[i]
			}
		}
	}
}
