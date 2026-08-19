// Package main - `cafctl train` CLI tests.
//
// Each test builds fresh, parent-less command instances via the newXxxCmd()
// constructors (the run/model/verify-* pattern) so Execute runs the command
// directly. The full developer journey (model register -> train submit with
// --base-model -> run-once --artifact -> train status -> model list showing the
// new version's parent chain) runs against real temp stores.
package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustSubmitTrainJob submits a training job through the real CLI command and
// returns the created job ID (parsed from --output json).
func mustSubmitTrainJob(t *testing.T, store, name string, extra ...string) string {
	t.Helper()
	cmd := newTrainSubmitCmd()
	buf := wireCmd(cmd)
	args := append([]string{
		name, "--image", "pytorch:2.0", "--gpu", "4", "--memory", "32",
		"--dataset", "sha256:ds-cli", "--command", "python train.py",
		"--store", store, "--output", "json",
	}, extra...)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute(), "submit %s must succeed", name)
	var result struct {
		JobID           string `json:"job_id"`
		Status          string `json:"status"`
		AttestationHash string `json:"attestation_hash"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "submit output must be valid JSON: %s", buf.String())
	require.NotEmpty(t, result.JobID, "job_id must be in submit output")
	require.Equal(t, "queued", result.Status)
	assert.NotEmpty(t, result.AttestationHash, "submit must be attested")
	return result.JobID
}

// runOnce executes `cafctl train run-once` through the real command.
func runOnce(t *testing.T, store, jobID, artifact, registry string, wantErr bool) string {
	t.Helper()
	cmd := newTrainRunOnceCmd()
	buf := wireCmd(cmd)
	args := []string{jobID, "--store", store}
	if artifact != "" {
		args = append(args, "--artifact", artifact)
	}
	if registry != "" {
		args = append(args, "--registry", registry)
	}
	cmd.SetArgs(args)
	err := cmd.Execute()
	if wantErr {
		require.Error(t, err, "run-once must fail")
		return buf.String()
	}
	require.NoError(t, err, "run-once must succeed")
	return buf.String()
}

// TestTrainCmd_FullJourney walks the complete developer journey across
// Modules 13+14: register a base model, submit a fine-tuning job pointing at
// it, run the job to completion with an artifact, then verify the registry
// holds the new version with the correct parent lineage.
func TestTrainCmd_FullJourney(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, ".caf")
	reg := filepath.Join(dir, "models")

	// Step 1: register base model resnet50:1.0.0 (Module 13).
	baseArt := writeModelWeights(t, "base weights")
	mustRegister(t, reg, "resnet50", "1.0.0", baseArt)

	// Step 2: submit a fine-tuning job based on resnet50:1.0.0.
	jobID := mustSubmitTrainJob(t, store, "fine-tune-resnet", "--base-model", "resnet50:1.0.0")

	// Step 3: run-once with an artifact registers the new model version.
	tunedArt := writeModelWeights(t, "fine-tuned weights")
	out := runOnce(t, store, jobID, tunedArt, reg, false)
	assert.Contains(t, out, "queued → scheduled")
	assert.Contains(t, out, "scheduled → running")
	assert.Contains(t, out, "running → succeeded")
	assert.Contains(t, out, "resnet50:1.1.0", "new minor version must be registered")
	assert.Contains(t, out, "parent 1.0.0", "parent lineage must be reported")
	assert.Contains(t, out, "Attestation:")

	// Step 4: status shows the terminal state and 4-event timeline.
	st := newTrainStatusCmd()
	stBuf := wireCmd(st)
	st.SetArgs([]string{jobID, "--store", store})
	require.NoError(t, st.Execute())
	s := stBuf.String()
	assert.Contains(t, s, "succeeded")
	assert.Contains(t, s, "Timeline:")
	assert.Contains(t, s, "∅ → queued")
	assert.Contains(t, s, "queued → scheduled")
	assert.Contains(t, s, "scheduled → running")
	assert.Contains(t, s, "running → succeeded")

	// Step 5: model list shows the new version with its parent (closure).
	ml := newModelListCmd()
	mlBuf := wireCmd(ml)
	ml.SetArgs([]string{"--registry", reg})
	require.NoError(t, ml.Execute())
	ls := mlBuf.String()
	assert.Contains(t, ls, "resnet50")
	assert.Contains(t, ls, "1.1.0")
	assert.Contains(t, ls, "1.0.0")
}

// TestTrainCmd_ListShowsJobs verifies the table output.
func TestTrainCmd_ListShowsJobs(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")
	id1 := mustSubmitTrainJob(t, store, "job-alpha")
	id2 := mustSubmitTrainJob(t, store, "job-beta")

	cmd := newTrainListCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{"--store", store})
	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "JOB ID")
	assert.Contains(t, out, id1)
	assert.Contains(t, out, id2)
	assert.Contains(t, out, "job-alpha")
	assert.Contains(t, out, "job-beta")
	assert.Contains(t, out, "queued")
}

// TestTrainCmd_IllegalOperationRejected proves state-machine enforcement at the
// CLI: running run-once on an already-succeeded job fails with a clear error.
func TestTrainCmd_IllegalOperationRejected(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")
	jobID := mustSubmitTrainJob(t, store, "one-shot")
	runOnce(t, store, jobID, "", "", false) // walk to succeeded

	// Second run-once must fail: succeeded → scheduled is illegal.
	out := runOnce(t, store, jobID, "", "", true)
	assert.Contains(t, out, "invalid transition", "error output must explain the rejected transition")

	// Cancelling a succeeded job must also fail.
	cancel := newTrainCancelCmd()
	cancelBuf := wireCmd(cancel)
	cancel.SetArgs([]string{jobID, "--reason", "too late", "--store", store})
	err := cancel.Execute()
	require.Error(t, err, "cancel on succeeded job must fail")
	assert.Contains(t, cancelBuf.String(), "terminal")
}

// TestTrainCmd_CancelJourney covers cancel from queued plus list reflecting it.
func TestTrainCmd_CancelJourney(t *testing.T) {
	store := filepath.Join(t.TempDir(), ".caf")
	jobID := mustSubmitTrainJob(t, store, "doomed")

	cmd := newTrainCancelCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{jobID, "--reason", "wrong dataset", "--store", store})
	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "cancelled")
	assert.Contains(t, out, "Attestation:")

	// The cancelled job cannot be run.
	runOut := runOnce(t, store, jobID, "", "", true)
	assert.Contains(t, runOut, "invalid transition")
}

// TestTrainCmd_MissingJobFailsCleanly verifies the not-found error path.
func TestTrainCmd_MissingJobFailsCleanly(t *testing.T) {
	cmd := newTrainStatusCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{"job-0000000000000000", "--store", t.TempDir()})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "not found")
}

// TestTrainCmd_SubmitValidation guards required-flag enforcement end to end.
func TestTrainCmd_SubmitValidation(t *testing.T) {
	cmd := newTrainSubmitCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{"bad-job", "--image", "", "--dataset", "ds", "--store", t.TempDir()})
	err := cmd.Execute()
	require.Error(t, err, "submit without image must fail")
	assert.Contains(t, buf.String(), "image")
}
