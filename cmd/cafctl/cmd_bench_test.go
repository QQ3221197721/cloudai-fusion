// Package main - cafctl bench subcommand tests.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBenchOutput simulates realistic go test -bench output.
const fakeBenchOutput = `goos: linux
goarch: amd64
pkg: github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler
cpu: 13th Gen Intel(R) Core(TM) i9-13900K
BenchmarkConstraintSolver-24    	    5000	      2345.0 ns/op	     128 B/op	       4 allocs/op
BenchmarkTopologyAware-24       	    3000	      5678.0 ns/op	     256 B/op	       8 allocs/op
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler	0.234s
`

func TestParseBenchOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []BenchResult
	}{
		{
			name:  "standard output with two benchmarks",
			input: fakeBenchOutput,
			expected: []BenchResult{
				{Name: "BenchmarkConstraintSolver-24", NsPerOp: 2345.0, BytesPerOp: 128, AllocsPerOp: 4},
				{Name: "BenchmarkTopologyAware-24", NsPerOp: 5678.0, BytesPerOp: 256, AllocsPerOp: 8},
			},
		},
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name:     "no matching lines",
			input:    "PASS\nok\n",
			expected: nil,
		},
		{
			name:  "ns/op only without mem stats",
			input: "BenchmarkFoo-8   100   999.5 ns/op\n",
			expected: []BenchResult{
				{Name: "BenchmarkFoo-8", NsPerOp: 999.5, BytesPerOp: 0, AllocsPerOp: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBenchOutput(tt.input)
			if tt.expected == nil {
				assert.Nil(t, got)
			} else {
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestNewBenchCmd_SubcommandRegistration(t *testing.T) {
	cmd := newBenchCmd()
	names := make([]string, 0, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		names = append(names, c.Use)
	}
	assert.Contains(t, names, "scheduler")
	assert.Contains(t, names, "reporting")
	assert.Contains(t, names, "messaging")
	assert.Contains(t, names, "runmode")
}

func TestBenchSubCmd_HelpContainsBenchmark(t *testing.T) {
	subcmds := []string{"scheduler", "reporting", "messaging", "runmode"}
	benchCmd := newBenchCmd()
	for _, name := range subcmds {
		t.Run(name, func(t *testing.T) {
			sub, _, err := benchCmd.Find([]string{name})
			require.NoError(t, err)
			output := strings.ToLower(sub.Short + " " + sub.Long)
			assert.Contains(t, output, "benchmark", "Help text should contain 'benchmark': Short=%q Long=%q", sub.Short, sub.Long)
		})
	}
}

func TestBenchSubCmd_ArgsRejected(t *testing.T) {
	subcmds := []string{"scheduler", "reporting", "messaging", "runmode"}
	for _, name := range subcmds {
		t.Run(name, func(t *testing.T) {
			cmd := newBenchSubCmd(name, "./pkg/"+name+"/...", ".")
			wireCmd(cmd)
			cmd.SetArgs([]string{"unexpected-arg"})
			err := cmd.Execute()
			assert.Error(t, err)
		})
	}
}

func TestBenchSubCmd_JSONOutput_Success(t *testing.T) {
	// Override benchRunner to return fake output
	origRunner := benchRunner
	defer func() { benchRunner = origRunner }()

	benchRunner = func(pkg string, pattern string) (string, error) {
		return fakeBenchOutput, nil
	}

	subcmds := []string{"scheduler", "reporting", "messaging", "runmode"}
	for _, name := range subcmds {
		t.Run(name, func(t *testing.T) {
			cmd := newBenchSubCmd(name, "./pkg/"+name+"/...", ".")
			buf := wireCmd(cmd)
			require.NoError(t, cmd.Execute())

			var report BenchReport
			err := json.Unmarshal(buf.Bytes(), &report)
			require.NoError(t, err, "output must be valid JSON: %s", buf.String())
			assert.Equal(t, name, report.Package)
			assert.Empty(t, report.Error)
			assert.NotEmpty(t, report.Benchmarks)
			assert.Equal(t, "BenchmarkConstraintSolver-24", report.Benchmarks[0].Name)
			assert.InDelta(t, 2345.0, report.Benchmarks[0].NsPerOp, 0.01)
			assert.Equal(t, int64(128), report.Benchmarks[0].BytesPerOp)
			assert.Equal(t, int64(4), report.Benchmarks[0].AllocsPerOp)
		})
	}
}

func TestBenchSubCmd_JSONOutput_Error(t *testing.T) {
	origRunner := benchRunner
	defer func() { benchRunner = origRunner }()

	benchRunner = func(pkg string, pattern string) (string, error) {
		return "FAIL	./pkg/scheduler 0.001s", fmt.Errorf("exit status 1")
	}

	cmd := newBenchSubCmd("scheduler", "./pkg/scheduler/...", ".")
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute()) // command itself should not error

	var report BenchReport
	err := json.Unmarshal(buf.Bytes(), &report)
	require.NoError(t, err, "output must be valid JSON: %s", buf.String())
	assert.Equal(t, "scheduler", report.Package)
	assert.NotEmpty(t, report.Error)
	assert.Contains(t, report.Error, "exit status 1")
	assert.Nil(t, report.Benchmarks)
}

func TestBenchSubCmd_PatternFlag(t *testing.T) {
	origRunner := benchRunner
	defer func() { benchRunner = origRunner }()

	var capturedPattern string
	benchRunner = func(pkg string, pattern string) (string, error) {
		capturedPattern = pattern
		return "", nil
	}

	cmd := newBenchSubCmd("scheduler", "./pkg/scheduler/...", ".")
	wireCmd(cmd)
	cmd.SetArgs([]string{"--pattern", "BenchmarkTopology"})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "BenchmarkTopology", capturedPattern)
}

func TestBenchSubCmd_Deterministic(t *testing.T) {
	origRunner := benchRunner
	defer func() { benchRunner = origRunner }()

	benchRunner = func(pkg string, pattern string) (string, error) {
		return fakeBenchOutput, nil
	}

	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newBenchSubCmd("scheduler", "./pkg/scheduler/...", ".")
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must produce identical output")
}
