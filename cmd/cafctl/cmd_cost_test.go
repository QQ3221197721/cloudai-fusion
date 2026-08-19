// cmd_cost_test.go walks the real `cafctl cost` developer journey:
// config → estimate (two-node price comparison) → report (Monitor
// trade-off join) → optimize (0.7:0.3 mix recommendation).
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCostCmd wires and executes one cost subcommand with the shared buffer.
func runCostCmd(t *testing.T, cmd *cobra.Command, args ...string) string {
	t.Helper()
	buf := wireCmd(cmd)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute(), "cost command must succeed: %v", args)
	return buf.String()
}

// writeCostJob writes a flat key:value job spec file and returns its path.
func writeCostJob(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "job.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// costConfig configures one node through the real CLI command.
func costConfig(t *testing.T, store, node string, extra ...string) string {
	t.Helper()
	args := append([]string{node, "--store", store}, extra...)
	return runCostCmd(t, newCostConfigCmd(), args...)
}

// writeMonitorJSONL seeds the sibling Monitor store with a baseline record
// (latency p95 = baseMS) and the latest record (latestMS) for one version.
func writeMonitorJSONL(t *testing.T, store, version string, baseMS, latestMS float64) {
	t.Helper()
	dir := filepath.Join(store, "monitor")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	records := []string{
		`{"model_version":"` + version + `","timestamp":"2026-08-14T10:00:00Z","latency_p95_ms":` + jsonNum(baseMS) + `,"accuracy":0.92,"sample_count":100}`,
		`{"model_version":"` + version + `","timestamp":"2026-08-15T10:00:00Z","latency_p95_ms":` + jsonNum(latestMS) + `,"accuracy":0.91,"sample_count":120}`,
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "perf.jsonl"), []byte(strings.Join(records, "\n")+"\n"), 0o644))
}

func jsonNum(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestCostEstimateCmd_TwoNodeComparison configures an a100 node and an h100
// node, then estimates the same job on both: prices must differ exactly
// ($68.00 vs $96.00) and the cheaper node must be called out.
func TestCostEstimateCmd_TwoNodeComparison(t *testing.T) {
	store := t.TempDir()
	costConfig(t, store, "gpu-a", "--gpu-type", "a100", "--gpu-cost", "8.5")
	costConfig(t, store, "gpu-b", "--gpu-type", "h100", "--gpu-cost", "12.0")

	job := writeCostJob(t, "name: train-resnet\ngpus: 4\nduration: 2h\nbudget: 75\n")
	out := runCostCmd(t, newCostEstimateCmd(),
		"--job", job, "--nodes", "gpu-a,gpu-b", "--store", store, "--no-attest")

	assert.Contains(t, out, "gpu-a")
	assert.Contains(t, out, "gpu-b")
	assert.Contains(t, out, "$68.00", "gpu-a: 4 × $8.50 × 2h must price exactly $68.00")
	assert.Contains(t, out, "$96.00", "gpu-b: 4 × $12.00 × 2h must price exactly $96.00")
	assert.Contains(t, out, "cheapest plan: gpu-a")
	assert.Contains(t, out, "$7.00 left", "gpu-a fits the $75 budget")
	assert.Contains(t, out, "over $21.00", "gpu-b must be flagged over the $75 budget")
	assert.Contains(t, out, "EXCEEDED")

	// Both decisions were appended to the history store.
	st := openCostStore(store)
	hist, err := st.LoadCostHistory()
	require.NoError(t, err)
	require.Len(t, hist, 2, "one history row per candidate node")
}

// TestCostEstimateCmd_JSONOutput verifies the machine-readable projection.
func TestCostEstimateCmd_JSONOutput(t *testing.T) {
	store := t.TempDir()
	costConfig(t, store, "gpu-a", "--gpu-type", "a100", "--gpu-cost", "8.5")

	job := writeCostJob(t, "name: j\ngpus: 4\nduration: 2h\n")
	out := runCostCmd(t, newCostEstimateCmd(),
		"--job", job, "--nodes", "gpu-a", "--store", store, "--no-attest", "-o", "json")

	var doc struct {
		Job struct {
			Name string `json:"name"`
		} `json:"job"`
		Nodes   []string `json:"nodes"`
		Results []struct {
			NodeID       string `json:"node_id"`
			DefaultRules bool   `json:"default_rules"`
			Estimate     struct {
				TotalCost float64 `json:"total_cost"`
			} `json:"estimate"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.Equal(t, []string{"gpu-a"}, doc.Nodes)
	require.Len(t, doc.Results, 1)
	assert.Equal(t, "gpu-a", doc.Results[0].NodeID)
	assert.False(t, doc.Results[0].DefaultRules, "configured node must not fall back to default rules")
	assert.Equal(t, 68.0, doc.Results[0].Estimate.TotalCost)
}

// TestCostConfigCmd_PersistsModel verifies config hits the disk as valid
// JSONL and prints the attestation receipt.
func TestCostConfigCmd_PersistsModel(t *testing.T) {
	store := t.TempDir()
	out := costConfig(t, store, "gpu-a",
		"--gpu-type", "a100", "--gpu-cost", "9.5", "--spot-discount", "0.4", "--cpu-cost", "0.1")

	assert.Contains(t, out, "$9.50/hr")
	assert.Contains(t, out, "40% of on-demand")
	assert.Contains(t, out, "$0.10/core-hr")
	assert.Contains(t, out, "model persisted to")
	assert.Contains(t, out, "Attestation:", "config must produce a signed receipt by default")

	raw, err := os.ReadFile(filepath.Join(store, "scheduler", "cost_models.json"))
	require.NoError(t, err, "model must really hit the disk")
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 1)
	assert.True(t, json.Valid([]byte(lines[0])), "persisted line must be valid JSON: %s", lines[0])
	var m struct {
		NodeID         string             `json:"node_id"`
		GPUCostPerHour map[string]float64 `json:"gpu_cost_per_hour"`
		SpotDiscount   float64            `json:"spot_discount"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &m))
	assert.Equal(t, "gpu-a", m.NodeID)
	assert.Equal(t, 9.5, m.GPUCostPerHour["a100"])
	assert.Equal(t, 0.4, m.SpotDiscount)
}

// TestCostReportCmd_TradeOffWithMonitor joins the recorded estimate with
// sibling Monitor observations: spend per model version next to p95 latency
// and baseline drift.
func TestCostReportCmd_TradeOffWithMonitor(t *testing.T) {
	store := t.TempDir()
	costConfig(t, store, "gpu-a", "--gpu-type", "a100")
	writeMonitorJSONL(t, store, "resnet50:1.1.0", 100, 130) // +30% p95 drift

	// Record one decision for a job named after the monitored model version.
	job := writeCostJob(t, "name: resnet50:1.1.0\ngpus: 4\nduration: 2h\nbudget: 100\n")
	runCostCmd(t, newCostEstimateCmd(),
		"--job", job, "--nodes", "gpu-a", "--store", store, "--no-attest")

	out := runCostCmd(t, newCostReportCmd(), "--store", store)
	assert.Contains(t, out, "cafctl cost report")
	assert.Contains(t, out, "resnet50:1.1.0")
	assert.Contains(t, out, "Total estimated spend: $68.00 across 1 decisions")
	assert.Contains(t, out, "Cost ↔ quality trade-off")
	assert.Contains(t, out, "130.0 ms", "latest Monitor p95 latency must be shown")
	assert.Contains(t, out, "+30.0%", "p95 drift vs baseline must be shown")
	assert.Contains(t, out, "0.9100", "latest accuracy must be shown")
}

// TestCostReportCmd_EmptyStore prints guidance instead of an error.
func TestCostReportCmd_EmptyStore(t *testing.T) {
	out := runCostCmd(t, newCostReportCmd(), "--store", t.TempDir())
	assert.Contains(t, out, "No cost decisions recorded yet.")
}

// TestCostOptimizeCmd_RecommendsCheapestMixedScore: with equal RL scores the
// 0.7:0.3 mix must pick the cheaper node and print the exact blend.
func TestCostOptimizeCmd_RecommendsCheapestMixedScore(t *testing.T) {
	store := t.TempDir()
	costConfig(t, store, "gpu-a", "--gpu-type", "a100", "--gpu-cost", "8.5")   // $68
	costConfig(t, store, "gpu-cheap", "--gpu-type", "a10g", "--gpu-cost", "2.85") // $22.80

	job := writeCostJob(t, "name: train-opt\ngpus: 4\nduration: 2h\n")
	out := runCostCmd(t, newCostOptimizeCmd(), "--job", job, "--store", store, "--no-attest")

	assert.Contains(t, out, "recommended node: gpu-cheap")
	assert.Contains(t, out, "$22.80")
	// mix = rl 0.8 × 0.7 + cost 1.0 × 0.3 = 0.86 (fixed-point blend).
	assert.Contains(t, out, "0.8600", "mix score must render the exact 0.86 blend")
	assert.Contains(t, out, "rl 0.800 × 0.7 + cost 1.000 × 0.3")
	assert.Contains(t, out, "gpu-a", "the rejected alternative must be visible")
}
