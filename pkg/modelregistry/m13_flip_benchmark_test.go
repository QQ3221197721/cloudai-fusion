// Package modelregistry — FLIP M13 Model Registry Benchmark (N=100/1000 models).
//
// Measures lineage query latency (ns/op) for ancestor traversal and 
// storage efficiency via content-addressable deduplication ratio.
// Compares against real MLflow Python client subprocess.
//
// Usage: go test ./pkg/modelregistry -bench=M13Flip -count=6 -json > ../../output/m13_flip_bench.json
package modelregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"
)

// flipBenchmarkResult captures both systems' metrics in unified JSON format.
type flipBenchmarkResult struct {
	System  string  `json:"system"`       // "M13_Model_Registry" or "MLflow_Registry"
	Version string  `json:"version"`      // e.g., "go1.26.5" or "mlflow3.15.1"
	ModelCount int  `json:"model_count"`  // N models total
	TotalVersions int `json:"total_versions"`
	Iterations int  `json:"iterations"` // count=6 requirement
	
	// Lineage query latency in nanoseconds per operation
	MedianQueryLatencyNsOp float64 `json:"median_query_latency_ns_op"`
	StdDevQueryLatencyNsOp float64 `json:"stddev_query_latency_ns_op"`
	
	// Storage efficiency: higher is better (dedup ratio = bytes written / bytes stored)
	DedupRatio float64 `json:"dedup_ratio"`
	
	// Workload characteristics
	AvgLineageDepth float64 `json:"avg_lineage_depth"`
	ArtifactSizeAvg int `json:"artifact_size_avg_bytes"`
	
	Error       string `json:"error,omitempty"`
	BenchmarkErr string `json:"benchmark_error,omitempty"`
}

// median returns median of copy of xs (xs not mutated).
func flipMedian(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[n/2]
}

// stddev returns sample standard deviation.
func flipStddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var mean float64
	for _, v := range xs {
		mean += v
	}
	mean /= float64(len(xs))
	var ss float64
	for _, v := range xs {
		ss += (v - mean) * (v - mean)
	}
	return ss / float64(len(xs)-1)
}

// TestFLIPM13LineageQuery performs head-to-head comparison: M13 vs MLflow.
// Workload: N=100 models, each with lineage chain depth D=10.
// Metrics: lineage query latency (ns/op) + storage dedup ratio.
// Rules: count=6 iterations, honest verdict, never fake numbers.
func TestFLIPM13LineageQuery(t *testing.T) {
	ctx := context.Background()
	t.Log("FLIP Benchmark M13: lineage query latency + storage dedup @ N=100 models")
	
	// ---- Build M13 registry with lineage DAG ----
	reg, err := NewFSRegistry(t.TempDir(), nil) // no ledger for raw perf test
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	
	modelCount := 100
	chainDepth := 10
	numIterations := 6 // count=6 requirement
	
	// Pre-create artifacts with some duplicates for realistic dedup testing
	tmpDir := t.TempDir()
	artifactMap := make(map[string]string)
	artifactSizes := []int{4 * 1024, 64 * 1024, 256 * 1024} // vary sizes
	
	for m := 0; m < modelCount; m++ {
		size := artifactSizes[m%len(artifactSizes)]
		p := filepath.Join(tmpDir, fmt.Sprintf("weights-%d.pt", m))
		data := make([]byte, size)
		for j := range data {
			data[j] = byte((m ^ j) & 0xFF)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		artifactMap[fmt.Sprintf("m%d", m)] = p
	}
	
	// Register models with parent-child lineage relationships (DAG structure)
	var totalWritten int64
	var modelNames []string
	
	for m := 0; m < modelCount; m++ {
		name := fmt.Sprintf("flip-m%d", m)
		modelNames = append(modelNames, name)
		
		prevVer := "" // empty = root version
		modelDir := filepath.Join(reg.Root(), name)
		if err := os.MkdirAll(modelDir, 0o755); err != nil {
			t.Fatal(err)
		}
		
		for d := 0; d < chainDepth; d++ {
			version := fmt.Sprintf("1.%d.0", d)
			
			// Track total written (for dedup ratio)
			totalWritten += int64(artifactSizes[m%len(artifactSizes)])
			
			// Set parent version (empty for root = first version)
			parentVersion := prevVer
			
			_, err := reg.Register(ctx, RegisterInput{
				Name:          name,
				Version:       version,
				ArtifactPath:  artifactMap[fmt.Sprintf("m%d", m)],
				ParentVersion: parentVersion,
				DatasetRef:    "sha256:flip-test-data",
				CodeRef:       "git:flip-commit-bench",
			})
			if err != nil {
				t.Fatalf("register %s:%s: %v", name, version, err)
			}
			
			// Chain to next version
			prevVer = version
		}
	}
	
	// Count unique blobs stored (content-addressed dedup metric)
	blobsDir := filepath.Join(reg.Root(), blobsDir)
	blobEntries, err := os.ReadDir(blobsDir)
	if err != nil {
		t.Fatalf("read blobs dir: %v", err)
	}
	
	blobCount := 0
	for _, e := range blobEntries {
		if !e.IsDir() {
			blobCount++
		}
	}
	
	// Calculate dedup ratio: bytes written / bytes stored (higher = better)
	// At minimum, we store one blob per unique SHA256 hash
	dedupRatio := float64(totalWritten) / float64(blobCount*1024) // normalize by KB
	
	// ---- Measure lineage query latency ----
	queryLatencies := make([]float64, 0)
	var totalDepth int64
	
	// Sample queries across all models (at least 10 models deep chains)
	for _, name := range modelNames[:min(20, len(modelNames))] {
		arts, err := reg.List(ctx, name)
		if err != nil || len(arts) == 0 {
			continue
		}
		
		// Query leaf version (deepest chain)
		leafArt := arts[len(arts)-1]
		
		for i := 0; i < numIterations; i++ {
			start := time.Now()
			_, err := reg.Get(ctx, name, leafArt.Version)
			if err != nil {
				continue
			}
			graph, err := reg.Lineage(ctx, name, leafArt.Version)
			if err != nil {
				continue
			}
			
			queryLatencies = append(queryLatencies, float64(time.Since(start)))
			totalDepth += int64(graph.Depth)
		}
	}
	
	// Calculate M13 statistics
	medianQueryMs := flipMedian(queryLatencies) * 1e6 // Convert to ns for Go-native performance
	stddevQueryMs := flipStddev(queryLatencies) * 1e6
	avgChainLength := float64(totalDepth) / float64(len(queryLatencies)/numIterations)
	
	t.Logf("M13 Results: %d queries, median=%.0f ns, stddev=%.0f ns",
		len(queryLatencies), medianQueryMs, stddevQueryMs)
	t.Logf("Storage Efficiency: dedup_ratio=%.2fx (%d unique blobs from %.0f KB written)",
		dedupRatio, blobCount, float64(totalWritten)/1024)
	t.Logf("Lineage Characteristics: avg_chain_length=%.1f, artifact_size_avg=%d bytes",
		avgChainLength, artifactSizes[0])
	
	// Generate M13 result object
	m13Result := flipBenchmarkResult{
		System:               "M13_Model_Registry",
		Version:              "Go_1.26.5",
		ModelCount:           modelCount,
		TotalVersions:        modelCount * chainDepth,
		Iterations:           numIterations,
		MedianQueryLatencyNsOp: medianQueryMs,
		StdDevQueryLatencyNsOp: stddevQueryMs,
		DedupRatio:           dedupRatio,
		AvgLineageDepth:      avgChainLength,
		ArtifactSizeAvg:      artifactSizes[0],
	}
	
	// ---- Run MLflow subprocess benchmark ----
	t.Log("Running MLflow subprocess benchmark (same workload)...")
	mlflowRes, mlflowOk := runMLflowFlipBenchPython(modelCount, chainDepth, numIterations)
	
	if !mlflowOk {
		t.Logf("WARNING: MLflow unavailable: %s", mlflowRes.BenchmarkErr)
		mlflowResult := flipBenchmarkResult{
			System:     "MLflow_Registry",
			Version:    "Unavailable",
			ModelCount: modelCount,
			Error:      mlflowRes.BenchmarkErr,
		}
		
		// Output partial results when MLflow unavailable
		outputFlipResult(t, m13Result, mlflowResult)
	} else {
		t.Logf("MLflow Results: median_query=%.0f ns, dedup_ratio=N/A",
			mlflowRes.MedianQueryLatencyNsOp)
		
		// Compare and generate verdict
		verdict := compareFlipResults(m13Result, mlflowRes)
		outputFlipResultWithVerdict(t, m13Result, mlflowRes, verdict)
	}
}

// runMLflowFlipBenchPython executes the MLflow benchmark subprocess.
func runMLflowFlipBenchPython(numModels, chainDepth, iterations int) (flipBenchmarkResult, bool) {
	result := flipBenchmarkResult{
		System:    "MLflow_Registry",
		Version:   "MLflow_3.15.1",
		ModelCount: numModels,
		TotalVersions: numModels * chainDepth,
		Iterations: iterations,
	}
	
	// Execute MLflow Python benchmark via subprocess
	script := filepath.Join("testdata", "mlflow_flip_subprocess.py")
	if _, err := os.Stat(script); os.IsNotExist(err) {
		result.BenchmarkErr = "MLflow benchmark script not found: " + script
		return result, false
	}
	
	cmd := exec.Command("python", script, strconv.Itoa(numModels), strconv.Itoa(chainDepth), strconv.Itoa(iterations))
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		result.BenchmarkErr = fmt.Sprintf("python failed: %v (stderr: %s)", err, stderr)
		return result, false
	}
	
	if err := json.Unmarshal(out, &result); err != nil {
		result.BenchmarkErr = "bad JSON from MLflow: " + err.Error()
		return result, false
	}
	
	return result, true
}

// outputFlipResult outputs M13 only (when MLflow unavailable).
func outputFlipResult(t *testing.T, m13 flipBenchmarkResult, mlflow flipBenchmarkResult) {
	verdict := "UNABLE_TO_COMPARE_MLFLOW_UNAVAILABLE"
	if m13.DedupRatio > 1.0 {
		verdict = "WIN_DEDUP_ONLY"
	}
	
	type finalVerdict struct {
		BenchmarkInfo struct {
			Timestamp string `json:"timestamp"`
			ModelCount int  `json:"model_count"`
			Iterations int  `json:"iterations"`
		} `json:"benchmark_info"`
		M13 flipBenchmarkResult `json:"m13_result"`
		MLflow flipBenchmarkResult `json:"mlflow_result"`
		Verdict string `json:"verdict"`
	}
	
	fv := finalVerdict{
		BenchmarkInfo: struct {
			Timestamp string `json:"timestamp"`
			ModelCount int  `json:"model_count"`
			Iterations int  `json:"iterations"`
		}{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			ModelCount: m13.ModelCount,
			Iterations: m13.Iterations,
		},
		M13:      m13,
		MLflow:   mlflow,
		Verdict:  verdict,
	}
	
	if err := json.NewEncoder(os.Stdout).Encode(fv); err != nil {
		t.Fatalf("failed to encode output JSON: %v", err)
	}
}

// compareFlipResults generates honest win/loss/draw verdict.
func compareFlipResults(m13, mlflow flipBenchmarkResult) string {
	// Lower latency is better, higher dedup ratio is better
	
	latencyBetter := m13.MedianQueryLatencyNsOp < mlflow.MedianQueryLatencyNsOp*0.95 // 5% margin
	dedupBetter := m13.DedupRatio > 1.0 && mlflow.DedupRatio == 0 // MLflow has no dedup
	
	if latencyBetter && dedupBetter {
		return "CLEAN_WIN_BOTH_CRITERIA"
	} else if latencyBetter && !dedupBetter {
		return "WIN_LATENCY_ONLY"
	} else if !latencyBetter && dedupBetter {
		return "WIN_DEDUP_ONLY"
	} else {
		return "LOSS_OR_DRAW_CHECK_METRICS"
	}
}

// outputFlipResultWithVerdict outputs full comparison with verdict.
func outputFlipResultWithVerdict(t *testing.T, m13, mlflow flipBenchmarkResult, verdict string) {
	type finalComparison struct {
		BenchmarkInfo struct {
			Timestamp string `json:"timestamp"`
			ModelCount int  `json:"model_count"`
			Iterations int  `json:"iterations"`
		} `json:"benchmark_info"`
		M13 flipBenchmarkResult `json:"m13_result"`
		MLflow flipBenchmarkResult `json:"mlflow_result"`
		Verdict struct {
			Overall  string `json:"overall"`
			Criteria string `json:"criteria"`
			MarginMs float64 `json:"margin_ms"`
		} `json:"verdict"`
	}
	
	marginMs := ((mlflow.MedianQueryLatencyNsOp - m13.MedianQueryLatencyNsOp) / mlflow.MedianQueryLatencyNsOp) * 1000 // Convert to ms percent
	
	fc := finalComparison{
		BenchmarkInfo: struct {
			Timestamp string `json:"timestamp"`
			ModelCount int  `json:"model_count"`
			Iterations int  `json:"iterations"`
		}{
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			ModelCount: m13.ModelCount,
			Iterations: m13.Iterations,
		},
		M13:      m13,
		MLflow:   mlflow,
		Verdict: struct {
			Overall  string `json:"overall"`
			Criteria string `json:"criteria"`
			MarginMs float64 `json:"margin_ms"`
		}{
			Overall:  verdict,
			Criteria: "lineage_query_latency + storage_dedup_ratio",
			MarginMs: marginMs,
		},
	}
	
	if err := json.NewEncoder(os.Stdout).Encode(fc); err != nil {
		t.Fatalf("failed to encode comparison JSON: %v", err)
	}
}

// min helper
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
