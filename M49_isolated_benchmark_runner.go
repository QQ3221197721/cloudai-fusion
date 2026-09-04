// M49 Isolated Benchmark Runner
// Runs three isolated benchmarks with -count=6, computes median ns/op per path,
// and writes output/m49_isolated_bench.json with honest speedup verdict.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// M49Results is the JSON schema written to output/m49_isolated_bench.json
type M49Results struct {
	Timestamp           string  `json:"timestamp"`
	PureDetection       int64   `json:"pure_detection_ns_per_op"`
	PureWorkqueue       int64   `json:"pure_workqueue_reconcile_ns_per_op"`
	HybridAsync         int64   `json:"hybrid_async_ns_per_op"`
	RatioDetToWQ        float64 `json:"detection_to_workqueue_ratio"`
	RatioAsyncToDet     float64 `json:"async_to_detection_ratio"`
	SpeedupWQoverDet    float64 `json:"speedup_workqueue_over_detection"`
	FaultsPerIter       int     `json:"faults_detected_per_iteration"`
	SamplesCount        int     `json:"samples_count"`
	WorkloadSize        int     `json:"workload_objects"`
	FaultRatePct        float64 `json:"fault_rate_percentage"`
	Verdict             string  `json:"verdict"`
	RawBenchOutput      string  `json:"raw_bench_output"`
}

func median(vals []int64) int64 {
	if len(vals) == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	n := len(vals)
	if n%2 == 0 {
		return (vals[n/2-1] + vals[n/2]) / 2
	}
	return vals[n/2]
}

func main() {
	cmd := exec.Command("go", "test",
		"-run", "^$",
		"-bench", "^(BenchmarkM49_PureDetection|BenchmarkM49_PureWorkqueueReconcile|BenchmarkM49_HybridAsync)$",
		"-benchmem",
		"-count", "6",
		"-benchtime", "100x",
		"./pkg/aiops/",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: go test exited with error: %v\nStderr: %s\n", err, stderr.String())
	}

	output := stdout.String()

	lineRe := regexp.MustCompile(`^(BenchmarkM49_\w+)-\d+\s+\d+\s+([0-9.]+)\s+ns/op`)

	samples := map[string][]int64{
		"BenchmarkM49_PureDetection":          {},
		"BenchmarkM49_PureWorkqueueReconcile": {},
		"BenchmarkM49_HybridAsync":            {},
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		m := lineRe.FindStringSubmatch(line)
		if len(m) == 3 {
			name := m[1]
			nsFloat, _ := strconv.ParseFloat(m[2], 64)
			if _, ok := samples[name]; ok {
				samples[name] = append(samples[name], int64(nsFloat))
			}
		}
	}

	for name, s := range samples {
		if len(s) == 0 {
			fmt.Fprintf(os.Stderr, "ERROR: no samples captured for %s\nRaw output:\n%s\n", name, output)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "INFO: %s captured %d samples\n", name, len(s))
	}

	pureDetection := median(samples["BenchmarkM49_PureDetection"])
	pureWorkqueue := median(samples["BenchmarkM49_PureWorkqueueReconcile"])
	hybridAsync := median(samples["BenchmarkM49_HybridAsync"])

	if pureDetection == 0 || pureWorkqueue == 0 || hybridAsync == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: zero median detected (det=%d wq=%d hyb=%d)\n", pureDetection, pureWorkqueue, hybridAsync)
		os.Exit(1)
	}

	ratioDetToWQ := float64(pureDetection) / float64(pureWorkqueue)
	ratioAsyncToDet := float64(hybridAsync) / float64(pureDetection)
	speedupWQoverDet := float64(pureWorkqueue) / float64(pureDetection)

	var verdict string
	if speedupWQoverDet >= 2.0 {
		verdict = fmt.Sprintf("REAL SPEEDUP: Pure Detection is %.2fx faster than Workqueue Reconcile (lock-free category-bucket correlation payoff confirmed)", speedupWQoverDet)
	} else if speedupWQoverDet >= 1.15 {
		verdict = fmt.Sprintf("PARTIAL WIN: Detection is %.2fx faster than Workqueue Reconcile; measurable but below the 2-3x target.", speedupWQoverDet)
	} else if speedupWQoverDet >= 0.85 {
		verdict = fmt.Sprintf("NO CLEAR WIN: Detection and Workqueue Reconcile are within noise (ratio=%.2f).", speedupWQoverDet)
	} else {
		verdict = fmt.Sprintf("REGRESSION: Detection is slower than Workqueue Reconcile (%.2fx slower).", 1.0/speedupWQoverDet)
	}

	finalResults := M49Results{
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		PureDetection:     pureDetection,
		PureWorkqueue:     pureWorkqueue,
		HybridAsync:       hybridAsync,
		RatioDetToWQ:      ratioDetToWQ,
		RatioAsyncToDet:   ratioAsyncToDet,
		SpeedupWQoverDet:  speedupWQoverDet,
		FaultsPerIter:     40,
		SamplesCount:      6,
		WorkloadSize:      100,
		FaultRatePct:      40.0,
		Verdict:           verdict,
		RawBenchOutput:    output,
	}

	jsonOut, err := json.MarshalIndent(finalResults, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON marshal error: %v\n", err)
		os.Exit(1)
	}

	outputPath := "./output/m49_isolated_bench.json"
	if err := os.WriteFile(outputPath, jsonOut, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "File write error (%s): %v\n", outputPath, err)
		os.Exit(1)
	}

	fmt.Println("=======================================================")
	fmt.Println("    M49 ISOLATED BENCHMARK RESULTS (count=6 median)")
	fmt.Println("=======================================================")
	fmt.Printf("Timestamp: %s\n", finalResults.Timestamp)
	fmt.Printf("\nWorkload: %d objects, %d breach thresholds (%.0f%% fault rate)\n",
		finalResults.WorkloadSize, finalResults.FaultsPerIter, finalResults.FaultRatePct)
	fmt.Printf("\nIsolated latencies (ns/op, median of 6):\n")
	fmt.Printf("  [1] Pure Detection            : %d ns/op\n", finalResults.PureDetection)
	fmt.Printf("  [2] Pure Workqueue Reconcile  : %d ns/op\n", finalResults.PureWorkqueue)
	fmt.Printf("  [3] Hybrid Async Fast Path    : %d ns/op\n", finalResults.HybridAsync)
	fmt.Printf("\nSpeedup factors:\n")
	fmt.Printf("  detection/workqueue ratio  : %.3f\n", finalResults.RatioDetToWQ)
	fmt.Printf("  async/detection ratio      : %.3f\n", finalResults.RatioAsyncToDet)
	fmt.Printf("  workqueue/detection (speedup): %.2fx\n", finalResults.SpeedupWQoverDet)
	fmt.Printf("\nVERDICT: %s\n", finalResults.Verdict)
	fmt.Printf("\nJSON: %s\n", outputPath)
	fmt.Println("=======================================================")
}
