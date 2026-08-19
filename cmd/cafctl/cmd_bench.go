// Package main - cafctl bench subcommand: run package benchmarks and emit JSON reports.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// BenchResult holds a single parsed benchmark result line.
type BenchResult struct {
	Name        string `json:"name"`
	NsPerOp     float64 `json:"ns_per_op"`
	BytesPerOp  int64   `json:"bytes_per_op"`
	AllocsPerOp int64   `json:"allocs_per_op"`
}

// BenchReport is the JSON output envelope for a bench subcommand.
type BenchReport struct {
	Package    string        `json:"package"`
	Benchmarks []BenchResult `json:"benchmarks,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// benchLineRe parses a standard Go benchmark output line:
//
//	BenchmarkXxx-8   1000   1234 ns/op   56 B/op   7 allocs/op
var benchLineRe = regexp.MustCompile(
	`^(Benchmark\S+)\s+\d+\s+([\d.]+)\s+ns/op(?:\s+([\d]+)\s+B/op)?(?:\s+([\d]+)\s+allocs/op)?`,
)

// parseBenchOutput extracts BenchResult entries from raw go test -bench output.
func parseBenchOutput(output string) []BenchResult {
	var results []BenchResult
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		m := benchLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ns, _ := strconv.ParseFloat(m[2], 64)
		var bytesOp, allocsOp int64
		if m[3] != "" {
			bytesOp, _ = strconv.ParseInt(m[3], 10, 64)
		}
		if m[4] != "" {
			allocsOp, _ = strconv.ParseInt(m[4], 10, 64)
		}
		results = append(results, BenchResult{
			Name:        m[1],
			NsPerOp:     ns,
			BytesPerOp:  bytesOp,
			AllocsPerOp: allocsOp,
		})
	}
	return results
}

// benchRunner is the function that actually runs the benchmark subprocess.
// It is a package-level var so tests can override it.
var benchRunner = runBenchProcess

// runBenchProcess executes `go test -bench=... -benchmem -count=3 -benchtime=5x -run=^$`
// against the given package path and returns the combined output.
func runBenchProcess(pkg string, benchPattern string) (string, error) {
	args := []string{
		"test", pkg,
		"-bench=" + benchPattern,
		"-benchmem",
		"-count=3",
		"-benchtime=5x",
		"-run=^$",
	}
	cmd := exec.Command("go", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// newBenchCmd builds the top-level `cafctl bench` command with subcommands.
func newBenchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Run performance benchmarks for core packages and emit JSON reports",
		Long: `Run Go benchmarks for CloudAI Fusion core packages.

Each subcommand invokes 'go test -bench=...' against a specific package and
prints a JSON report with per-benchmark ns/op, B/op, and allocs/op.`,
	}
	cmd.AddCommand(
		newBenchSubCmd("scheduler", "./pkg/scheduler/...", "."),
		newBenchSubCmd("reporting", "./pkg/reporting/...", "."),
		newBenchSubCmd("messaging", "./pkg/messaging/...", "."),
		newBenchSubCmd("runmode", "./pkg/runmode/...", "."),
	)
	return cmd
}

// newBenchSubCmd creates one bench subcommand for a given package.
func newBenchSubCmd(name, pkgPath, benchPattern string) *cobra.Command {
	var pattern string
	cmd := &cobra.Command{
		Use:           name,
		Short:         fmt.Sprintf("Run benchmark for %s package", name),
		Long:          fmt.Sprintf("Execute Go benchmarks in %s and output a JSON performance report.", pkgPath),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bp := benchPattern
			if pattern != "" {
				bp = pattern
			}
			output, err := benchRunner(pkgPath, bp)
			report := BenchReport{Package: name}
			if err != nil {
				report.Error = fmt.Sprintf("go test failed: %v\n%s", err, output)
			} else {
				report.Benchmarks = parseBenchOutput(output)
			}
			b, _ := json.MarshalIndent(report, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
	cmd.Flags().StringVar(&pattern, "pattern", "", "Override benchmark regex pattern (default: all)")
	return cmd
}
