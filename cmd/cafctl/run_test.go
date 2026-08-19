// Package main - cmd_run unit tests
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRunTest builds a fresh `run` command wired to a single capture buffer for
// both stdout and stderr. Using a constructor per test (matching the verify-*
// command tests) gives each case isolated flag state and lets cmd.Execute() run
// the command directly instead of delegating to the shared root command.
func newRunTest(args ...string) (*cobra.Command, *bytes.Buffer) {
	cmd := newRunCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	return cmd, buf
}

// writeWasm writes a minimal valid WASM binary (\0asm + version 1) and returns
// its path.
func writeWasm(t *testing.T, name string, extra int) string {
	t.Helper()
	magic := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if extra > 0 {
		buf := make([]byte, len(magic)+extra)
		copy(buf, magic)
		magic = buf
	}
	tmp := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(tmp, magic, 0o644))
	return tmp
}

// TestRunHelp verifies the command surfaces the 'docker run' framing that makes
// it instantly recognizable to developers.
func TestRunHelp(t *testing.T) {
	cmd := newRunCmd()
	assert.Equal(t, "run <module-path-or-name>", cmd.Use)
	assert.Contains(t, cmd.Short, "docker run")

	// The rendered --help output must also mention it (acceptance criterion 3).
	help, _ := newRunTest("--help")
	var out bytes.Buffer
	help.SetOut(&out)
	help.SetErr(&out)
	require.NoError(t, help.Execute())
	assert.Contains(t, out.String(), "docker run")
}

// TestRunWasmModule validates a WASM binary and reports cold-start time.
func TestRunWasmModule(t *testing.T) {
	tmp := writeWasm(t, "test.wasm", 0)

	cmd, out := newRunTest(tmp)
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "completed in")
	assert.Contains(t, out.String(), "cold start")
}

// TestRunWithJSONOutput emits machine-readable JSON with an attestation hash.
func TestRunWithJSONOutput(t *testing.T) {
	tmp := writeWasm(t, "test.wasm", 0)

	cmd, out := newRunTest(tmp, "--output", "json")
	require.NoError(t, cmd.Execute())

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result), "JSON output should be valid")
	assert.Equal(t, "wasm", result["mode"])
	assert.Contains(t, result, "attestation_hash")
	assert.NotEmpty(t, result["attestation_hash"])
}

// TestRunNoAttest skips attestation when requested.
func TestRunNoAttest(t *testing.T) {
	tmp := writeWasm(t, "test.wasm", 0)

	cmd, out := newRunTest(tmp, "--no-attest", "--output", "json")
	require.NoError(t, cmd.Execute())

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	_, hasAttest := result["attestation_hash"]
	assert.False(t, hasAttest, "attestation_hash should be absent when --no-attest is set")
}

// TestRunGPUWorkload submits a simple GPU job YAML spec.
func TestRunGPUWorkload(t *testing.T) {
	spec := `name: training-job
gpus: 4
memory: 32GB
image: pytorch:2.0
command: python train.py`
	tmp := filepath.Join(t.TempDir(), "job.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte(spec), 0o644))

	cmd, out := newRunTest(tmp)
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "GPU")
	assert.Contains(t, out.String(), "submitted to topology-aware scheduler")
}

// TestRunGPUWorkloadWithFlag uses --gpu to override the spec request.
func TestRunGPUWorkloadWithFlag(t *testing.T) {
	spec := "name: inference-job\ngpus: 1"
	tmp := filepath.Join(t.TempDir(), "inference.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte(spec), 0o644))

	cmd, out := newRunTest(tmp, "--gpu", "8")
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Requested:    8 GPU(s)")
}

// TestRunInvalidWasmError rejects malformed binary files.
func TestRunInvalidWasmError(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "invalid.wasm")
	require.NoError(t, os.WriteFile(tmp, []byte{0xff, 0xfe, 0x61, 0x73, 0x01, 0x00, 0x00, 0x00}, 0o644))

	cmd, out := newRunTest(tmp)
	require.Error(t, cmd.Execute(), "expected error on invalid WASM binary")
	assert.Contains(t, out.String(), "not a valid WASM module")
}

// TestRunUnsupportedType fails gracefully for unknown extensions without --gpu.
func TestRunUnsupportedType(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "data.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("hello world"), 0o644))

	cmd, _ := newRunTest(tmp)
	require.Error(t, cmd.Execute(), "expected unsupported type error")
}

// TestRunUnsupportedTypeWithGPUGoThrough allows any extension when --gpu is set.
func TestRunUnsupportedTypeWithGPUGoThrough(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(tmp, []byte("notes"), 0o644))

	cmd, out := newRunTest(tmp, "--gpu", "2")
	require.NoError(t, cmd.Execute(), "should accept any extension when --gpu is provided")
	assert.Contains(t, out.String(), "GPU workload submitted")
}

// TestRunWasmModuleMetrics reports size, SHA-256, and cold-start figures.
func TestRunWasmModuleMetrics(t *testing.T) {
	tmp := writeWasm(t, "metrics.wasm", 1016) // 8 + 1016 = 1024 bytes

	cmd, out := newRunTest(tmp)
	require.NoError(t, cmd.Execute())
	s := out.String()
	assert.Contains(t, s, "Size:         ")
	assert.Contains(t, s, "SHA-256:")
	assert.Contains(t, s, "cold start")
}

// TestRunReproducibleGPUPlacement places the same workload on the same node.
func TestRunReproducibleGPUPlacement(t *testing.T) {
	spec := "name: consistent-workload"
	tmp := filepath.Join(t.TempDir(), "consistent.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte(spec), 0o644))

	cmd1, out1 := newRunTest(tmp, "--output", "json")
	require.NoError(t, cmd1.Execute())
	cmd2, out2 := newRunTest(tmp, "--output", "json")
	require.NoError(t, cmd2.Execute())

	var r1, r2 map[string]interface{}
	require.NoError(t, json.Unmarshal(out1.Bytes(), &r1))
	require.NoError(t, json.Unmarshal(out2.Bytes(), &r2))
	assert.Equal(t, r1["node"], r2["node"], "placement must be deterministic")
	assert.NotEmpty(t, r1["node"])
}

// TestRunWasmColdStartFast ensures the cold-start metric is genuinely fast.
func TestRunWasmColdStartFast(t *testing.T) {
	tmp := writeWasm(t, "fast.wasm", 0)

	cmd, out := newRunTest(tmp, "--output", "json")
	require.NoError(t, cmd.Execute())

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	coldStartMS, ok := result["cold_start_ms"].(float64)
	assert.True(t, ok, "cold_start_ms should be numeric")
	assert.Less(t, coldStartMS, 10.0, "cold start should be under 10ms for tiny files")
}

// TestRunNamespaceLabelsAttestation includes the namespace in the receipt line.
func TestRunNamespaceLabelsAttestation(t *testing.T) {
	tmp := writeWasm(t, "namespace.wasm", 0)

	cmd, out := newRunTest(tmp, "--namespace", "prod/deployment")
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "signed & hash-chained into namespace \"prod/deployment\"")
}
