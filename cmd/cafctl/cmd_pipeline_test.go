// Package main - cafctl pipeline CLI tests.
// Verifies the full developer journey: create → publish → run → status → list,
// with JSON output validation and attestation checks throughout. Tests follow the
// constructor pattern used by other modules.
package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustCreatePipeline creates a pipeline via CLI and returns the ID.
func mustCreatePipeline(t *testing.T, store, name, stages string, params map[string]string) string {
	t.Helper()
	cmd := newPipelineCreateCmd()
	buf := wireCmd(cmd)
	args := []string{
		name, "--store", store, "--stages", stages,
	}
	if len(params) > 0 {
		paramStrs := make([]string, 0, len(params))
		for k, v := range params {
			paramStrs = append(paramStrs, k+"="+v)
		}
		args = append(args, "--params", joinKV(paramStrs))
	}
	args = append(args, "--trigger", "manual")
	args = append(args, "--output", "json")

	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute(), "create must succeed")

	var result struct {
		PipelineID string `json:"pipeline_id"`
		Status     string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "create output must be valid JSON")
	assert.Equal(t, "draft", result.Status)
	return result.PipelineID
}

// joinKV joins key=value pairs with commas for test helpers.
func joinKV(pairs []string) string {
	out := ""
	for i, p := range pairs {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

// TestPipelineCmd_Journey walks the full developer journey: create draft, publish, run, check status.
func TestPipelineCmd_Journey(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")

	// Step 1: Create
	id := mustCreatePipeline(t, store, "journey-test", "train,experiment",
		map[string]string{"epochs": "50", "batch": "32"})
	assert.NotEmpty(t, id, "create must return ID")
	assert.True(t, len(id) == 21 && id[:5] == "pipe-", "ID format pipe-<16hex>")

	// Step 2: Publish
	publishCmd := newPipelinePublishCmd()
	wireCmd(publishCmd)
	publishCmd.SetArgs([]string{id, "--store", store, "--output", "json"})
	require.NoError(t, publishCmd.Execute(), "publish must succeed")

	// Step 3: Run
	runCmd := newPipelineRunCmd()
	wireCmd(runCmd)
	runBuf := wireCmd(runCmd)
	runCmd.SetArgs([]string{id, "--store", store, "--output", "json"})
	require.NoError(t, runCmd.Execute(), "run must succeed")
	var runResult struct {
		ID        string        `json:"id"`
		Name      string        `json:"name"`
		Status    string        `json:"status"`
		StageRuns []stageSummary `json:"stage_runs"`
	}
	require.NoError(t, json.Unmarshal(runBuf.Bytes(), &runResult), "run output must be valid JSON")
	assert.Equal(t, "completed", runResult.Status, "pipeline completed successfully")
	require.Len(t, runResult.StageRuns, 2, "two stages executed")
	for _, sr := range runResult.StageRuns {
		assert.Equal(t, "succeeded", sr.Status, "all stages succeeded")
	}

	// Step 4: Status
	statusCmd := newPipelineStatusCmd()
	wireCmd(statusCmd)
	statusBuf := wireCmd(statusCmd)
	statusCmd.SetArgs([]string{id, "--store", store})
	require.NoError(t, statusCmd.Execute())
	statusOut := statusBuf.String()
	assert.Contains(t, statusOut, id, "status shows pipeline ID")
	assert.Contains(t, statusOut, "completed", "status shows completed")
	assert.Contains(t, statusOut, "train", "stage names shown")

	// Step 5: List
	listCmd := newPipelineListCmd()
	listBuf := wireCmd(listCmd)
	listCmd.SetArgs([]string{"--store", store})
	require.NoError(t, listCmd.Execute())
	listOut := listBuf.String()
	assert.Contains(t, listOut, "ID", "list header present")
	assert.Contains(t, listOut, id, "pipeline appears in list")
}

// TestPipelineCmd_ListShowsAllPipelines verifies table output contains all pipelines.
func TestPipelineCmd_ListShowsAllPipelines(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")

	// Create multiple pipelines
	idA := mustCreatePipeline(t, store, "list-pipeline-a", "train", nil)
	idB := mustCreatePipeline(t, store, "list-pipeline-b", "experiment", nil)

	// List them
	listCmd := newPipelineListCmd()
	buf := wireCmd(listCmd)
	listCmd.SetArgs([]string{"--store", store})
	require.NoError(t, listCmd.Execute())
	out := buf.String()

	assert.Contains(t, out, "ID", "table header ID present")
	assert.Contains(t, out, "NAME", "table header NAME present")
	assert.Contains(t, out, "STATUS", "table header STATUS present")
	assert.Contains(t, out, "STAGES", "table header STAGES present")
	assert.Contains(t, out, idA, "first pipeline appears")
	assert.Contains(t, out, idB, "second pipeline appears")
}

// TestPipelineCmd_InvalidInputs validates CLI rejects malformed input.
func TestPipelineCmd_InvalidInputs(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")

	// Missing stages flag required? No -- but empty stages would fail at execution
	// Just verify basic command accepts args
	createCmd := newPipelineCreateCmd()
	_ = wireCmd(createCmd)
	createCmd.SetArgs([]string{"test-missing-stages", "--store", store})
	err := createCmd.Execute()
	require.Error(t, err, "empty stages must fail")
	assert.Contains(t, err.Error(), "stages cannot be empty", "error explains requirement")

	// Non-existent ID for status
	statusCmd := newPipelineStatusCmd()
	buf2 := wireCmd(statusCmd)
	statusCmd.SetArgs([]string{"nonexistent-id", "--store", store})
	err = statusCmd.Execute()
	require.Error(t, err, "invalid ID must fail")
	assert.Contains(t, buf2.String(), "not found", "error explains not found")
}

type stageSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// TestPipelineCmd_JSONOutput ensures all commands produce parseable JSON when --output json.
func TestPipelineCmd_JSONOutput(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")

	// Create produces clean JSON
	createCmd := newPipelineCreateCmd()
	createBuf := wireCmd(createCmd)
	createCmd.SetArgs([]string{"json-test", "--store", store, "--stages", "train", "--output", "json"})
	require.NoError(t, createCmd.Execute())

	var createRes struct {
		PipelineID string `json:"pipeline_id"`
		Status     string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(createBuf.Bytes(), &createRes), "create JSON must parse")
	assert.Equal(t, "draft", createRes.Status)
	assert.NotEmpty(t, createRes.PipelineID)

	// Publish produces JSON
	publishCmd := newPipelinePublishCmd()
	publishBuf := wireCmd(publishCmd)
	publishCmd.SetArgs([]string{createRes.PipelineID, "--store", store, "--output", "json"})
	require.NoError(t, publishCmd.Execute())

	var publishRes map[string]string
	require.NoError(t, json.Unmarshal(publishBuf.Bytes(), &publishRes), "publish JSON must parse")
	assert.Equal(t, "published", publishRes["status"])
}

// TestPipelineCmd_AttestationPresence verifies signed receipts appear in JSON output.
func TestPipelineCmd_AttestationPresence(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")

	// Create with attestations enabled (default)
	createCmd := newPipelineCreateCmd()
	createBuf := wireCmd(createCmd)
	createCmd.SetArgs([]string{"attest-test", "--store", store, "--stages", "train", "--output", "json"})
	require.NoError(t, createCmd.Execute())

	var res struct {
		PipelineID  string `json:"pipeline_id"`
		Attestation string `json:"attestation_hash,omitempty"`
	}
	require.NoError(t, json.Unmarshal(createBuf.Bytes(), &res))
	// Attestation should be present unless explicitly disabled
	assert.NotEmpty(t, res.Attestation, "create should include attestation hash")
}
