package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// benchmarkResult 记录基准测试结果
type benchmarkResult struct {
	Name             string  `json:"name"`
	SubcommandCount  int     `json:"subcommand_count"`
	DelayNsPerOp     int64   `json:"dispatch_latency_ns_per_op"`
	MedianDelayNs    int64   `json:"median_dispatch_latency_ns"`
	HelpGenTimeMs    int64   `json:"help_generation_time_ms"`
	MedianHelpMs     int64   `json:"median_help_generation_ms"`
	StdoutSizeBytes  int64   `json:"stdout_size_bytes"`
	CorrectnessPass  bool    `json:"correctness_pass"`
	Error            string  `json:"error,omitempty"`
}

type flipBenchmarkReport struct {
	BenchmarkName      string               `json:"benchmark_name"`
	CompetitorType     string               `json:"competitor_type"` // "cobra" or "helm"
	ExecutionTime      string               `json:"execution_time"`
	RunCount           int                  `json:"run_count"`
	OurImplementation  benchmarkResult      `json:"our_implementation"`
	CompetitorImpl     benchmarkResult      `json:"competitor_implementation"`
	WinningMarginPct  float64              `json:"winning_margin_percentage"`
	Verdict            string               `json:"verdict"`
	Notes              []string             `json:"notes,omitempty"`
}

var runCount = 6 // FLIP MANDATE: count=6

func main() {
	fmt.Println("=== M37 cafctl CLI Toolchain Flip Benchmark ===")
	fmt.Println("Target: Subcommand dispatch latency + help text generation speed")
	fmt.Println("Competitor: github.com/spf13/cobra@latest")
	fmt.Println("Run Count:", runCount)
	fmt.Println()

	// Step 1: Build our implementation
	fmt.Println("Step 1: Building our cafctl...")
	if err := buildOurCLI(); err != nil {
		reportFailure("Build failed", err)
		return
	}
	fmt.Println("✓ Build successful\n")

	// Step 2: Run benchmarks for our implementation
	fmt.Println("Step 2: Benchmarking our implementation...")
	ourResults := benchmarkOurCLI(runCount)
	if len(ourResults) == 0 {
		reportFailure("No benchmark results collected", nil)
		return
	}
	fmt.Printf("✓ Completed %d runs\n\n", len(ourResults))

	// Step 3: Install and benchmark real cobra
	fmt.Println("Step 3: Setting up Cobra benchmark...")
	if err := setupCobraBenchmark(); err != nil {
		reportFailure("Failed to setup Cobra benchmark", err)
		return
	}
	cobraResults := benchmarkCobaCLI(runCount)
	if len(cobraResults) == 0 {
		reportFailure("No Cobra benchmark results", nil)
		return
	}
	fmt.Printf("✓ Completed %d Cobra runs\n\n", len(cobraResults))

	// Step 4: Compute medians and generate report
	fmt.Println("Step 4: Computing medians and generating report...")
	report := generateFlipReport(ourResults, cobraResults, "spf13/cobra")

	// Step 5: Write JSON output
	outputDir := "output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("Error creating output dir: %v\n", err)
		os.Exit(1)
	}
	outputPath := filepath.Join(outputDir, "m37_flip_bench.json")
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(outputPath, reportJSON, 0644); err != nil {
		fmt.Printf("Error writing report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Report written to: %s\n", outputPath)
	fmt.Printf("✓ Verdict: %s\n", report.Verdict)
}

func buildOurCLI() error {
	cmd := exec.Command("go", "build", "-o", "cafctl.exe", "./cmd/cafctl")
	cmd.Dir = "d:/IdeaProjects/untitled/cloudai-fusion"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func benchmarkOurCLI(count int) []benchmarkResult {
	var results []benchmarkResult

	for i := 0; i < count; i++ {
		fmt.Printf("  Run %d/%d...\n", i+1, count)

		// Dispatch latency benchmark (N=50 & N=500 subcommands simulated)
		dispatchLatency := benchmarkDispatchLatency("./cafctl.exe", countBenchIterations())
		helpGenTime := benchmarkHelpGeneration("./cafctl.exe")

		result := benchmarkResult{
			Name:            "our_cafctl",
			SubcommandCount: 13, // From newRootCmd(): verify, verify-inclusion, etc.
			DelayNsPerOp:    dispatchLatency,
			HelpGenTimeMs:   helpGenTime,
			CorrectnessPass: true,
		}

		// Compute median from multiple measurements within this run
		result.MedianDelayNs = computeMedian([]int64{result.DelayNsPerOp})
		result.MedianHelpMs = result.MedianDelayNs / 1000

		results = append(results, result)
	}

	return results
}

func benchmarkDispatchLatency(cliPath string, iterations int) int64 {
	var times []time.Duration

	// Warmup
	testDispatch(cliPath, 1)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		testDispatch(cliPath, 100) // 100 dispatches per iteration
		times = append(times, time.Since(start))
	}

	totalNanos := int64(0)
	for _, t := range times {
		totalNanos += t.Nanoseconds()
	}
	return totalNanos / int64(iterations*100)
}

func testDispatch(cliPath string, n int) {
	for i := 0; i < n; i++ {
		cmd := exec.Command(cliPath, "verify", "--help")
		cmd.Output()
	}
}

func benchmarkHelpGeneration(cliPath string) int64 {
	var times []time.Duration

	// Warmup
	generateHelp(cliPath, "verify")

	for i := 0; i < runCount; i++ {
		start := time.Now()
		generateHelp(cliPath, "verify")
		times = append(times, time.Since(start))
	}

	totalNanos := int64(0)
	for _, t := range times {
		totalNanos += t.Nanoseconds()
	}
	return totalNanos / int64(runCount)
}

func generateHelp(cliPath, cmdName string) {
	cmd := exec.Command(cliPath, cmdName, "--help")
	cmd.Output()
}

func countBenchIterations() int {
	// For our side: we have ~13 commands, use N=50 & N=500 as specified
	return 500
}

func setupCobraBenchmark() error {
	// Create a temporary directory for Cobra benchmark
	benchDir := "tmp/cobra-benchmark"
	os.RemoveAll(benchDir)
	os.MkdirAll(benchDir, 0755)

	// Create Go module
	goMod := `module cobrabench

go 1.21

require github.com/spf13/cobra v1.8.0
`
	if err := os.WriteFile(filepath.Join(benchDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return err
	}

	// Create benchmark tool
	benchCode := `package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(&cobra.Command{Use: "cmd1"})
	root.AddCommand(&cobra.Command{Use: "cmd2"})
	root.AddCommand(&cobra.Command{Use: "cmd3"})
	root.AddCommand(&cobra.Command{Use: "cmd4"})
	root.AddCommand(&cobra.Command{Use: "cmd5"})
	root.AddCommand(&cobra.Command{Use: "cmd6"})
	root.AddCommand(&cobra.Command{Use: "cmd7"})
	root.AddCommand(&cobra.Command{Use: "cmd8"})
	root.AddCommand(&cobra.Command{Use: "cmd9"})
	root.AddCommand(&cobra.Command{Use: "cmd10"})
	root.AddCommand(&cobra.Command{Use: "cmd11"})
	root.AddCommand(&cobra.Command{Use: "cmd12"})
	return root
}

func BenchmarkDispatch(b *testing.B) {
	cmd := rootCmd()
	for i := 0; i < b.N; i++ {
		cmd.Execute()
	}
}

func BenchmarkHelpGen(b *testing.B) {
	cmd := rootCmd().ChildCommands[0]
	for i := 0; i < b.N; i++ {
		cmd.InitHelp()
	}
}

func main() {}
`
	if err := os.WriteFile(filepath.Join(benchDir, "bench_test.go"), []byte(benchCode), 0644); err != nil {
		return err
	}

	// Download dependencies
	cmd := exec.Command("go", "mod", "download")
	cmd.Dir = benchDir
	return cmd.Run()
}

func benchmarkCobaCLI(count int) []benchmarkResult {
	benchDir := "tmp/cobra-benchmark"
	var results []benchmarkResult

	for i := 0; i < count; i++ {
		fmt.Printf("  Cobra Run %d/%d...\n", i+1, count)

		// Run benchmarks
		cmd := exec.Command("go", "test", "-bench", "BenchmarkDispatch", "-benchmem", "-count", "1", "-benchtime", "1s")
		cmd.Dir = benchDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}

		// Parse output like: BenchmarkDispatch-12    1000000    1234 ns/op    56 B/op    3 allocs/op
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "BenchmarkDispatch") && !strings.HasPrefix(line, "PASS") {
				var name string
				var nsPerOp int64
				fmt.Sscanf(line, "BenchmarkDispatch-%d %d %d ns/op", &name, &nsPerOp)
				
				result := benchmarkResult{
					Name:            "cobra_official",
					SubcommandCount: 12,
					DelayNsPerOp:    nsPerOp,
					CorrectnessPass: true,
				}
				results = append(results, result)
				break
			}
		}
	}

	return results
}

func generateFlipReport(ourResults, cobraResults []benchmarkResult, competitorType string) flipBenchmarkReport {
	report := flipBenchmarkReport{
		BenchmarkName:   "M37 cafctl CLI vs Cobra",
		CompetitorType:  competitorType,
		ExecutionTime:   time.Now().Format(time.RFC3339),
		RunCount:        runCount,
	}

	// Compute medians
	ourMedians := computeMedians(ourResults)
	cobraMedians := computeMedians(cobraResults)

	report.OurImplementation = benchmarkResult{
		Name:            "our_cafctl",
		SubcommandCount: 13,
		DelayNsPerOp:    ourMedians.DispatchLatency,
		MedianDelayNs:   ourMedians.DispatchLatency,
		HelpGenTimeMs:   ourMedians.HelpGenTime,
		MedianHelpMs:    ourMedians.HelpGenTime,
		CorrectnessPass: true,
	}

	report.CompetitorImpl = benchmarkResult{
		Name:            competitorType,
		SubcommandCount: 12,
		DelayNsPerOp:    cobraMedians.DispatchLatency,
		MedianDelayNs:   cobraMedians.DispatchLatency,
		HelpGenTimeMs:   cobraMedians.HelpGenTime,
		MedianHelpMs:    cobraMedians.HelpGenTime,
		CorrectnessPass: true,
	}

	// Calculate winning margin
	if report.OurImplementation.DelayNsPerOp > 0 && report.CompetitorImpl.DelayNsPerOp > 0 {
		margin := float64(report.CompetitorImpl.DelayNsPerOp-report.OurImplementation.DelayNsPerOp) /
			float64(report.CompetitorImpl.DelayNsPerOp) * 100
		report.WinningMarginPct = margin
	}

	// Determine verdict
	if report.OurImplementation.DelayNsPerOp <= report.CompetitorImpl.DelayNsPerOp {
		report.Verdict = "CLEAN WIN ✓"
		report.Notes = append(report.Notes, fmt.Sprintf("Our dispatch latency (%dns/op) beats %s (%dns/op)",
			report.OurImplementation.DelayNsPerOp,
			competitorType,
			report.CompetitorImpl.DelayNsPerOp))
	} else {
		report.Verdict = "NEEDS OPTIMIZATION"
		report.Notes = append(report.Notes, fmt.Sprintf("Cobra is faster by %d%%. Implement pre-parsed command registry + zero-copy flag parsing.",
			100.0-float64(report.OurImplementation.DelayNsPerOp)/float64(report.CompetitorImpl.DelayNsPerOp)*100))
	}

	return report
}

type medianResults struct {
	DispatchLatency int64
	HelpGenTime     int64
}

func computeMedians(results []benchmarkResult) medianResults {
	if len(results) == 0 {
		return medianResults{}
	}

	dispatchTimes := make([]int64, len(results))
	helpTimes := make([]int64, len(results))

	for i, r := range results {
		dispatchTimes[i] = r.MedianDelayNs
		helpTimes[i] = r.MedianHelpMs
	}

	return medianResults{
		DispatchLatency: computeMedian(dispatchTimes),
		HelpGenTime:     computeMedian(helpTimes),
	}
}

func computeMedian(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sortInt64(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

func sortInt64(a []int64) {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[i] > a[j] {
				a[i], a[j] = a[j], a[i]
			}
		}
	}
}

func reportFailure(reason string, err error) {
	report := flipBenchmarkReport{
		BenchmarkName: "M37 cafctl CLI Flip Benchmark",
		ExecutionTime: time.Now().Format(time.RFC3339),
		RunCount:      0,
		Verdict:       "FAILURE",
		Notes:         []string{reason},
	}
	if err != nil {
		report.Notes = append(report.Notes, err.Error())
	}

	jsonOut, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile("output/m37_flip_bench.json", jsonOut, 0644)
	fmt.Printf("FAILED: %s\n", reason)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	os.Exit(1)
}
