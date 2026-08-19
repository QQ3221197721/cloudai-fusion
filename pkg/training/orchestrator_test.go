// Package training - unit tests for Training Job Orchestrator Module 14
package training

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestOrchestrator creates a temporary orchestrator with a real (in-memory) ledger.
// Returns the orchestrator, the store (to count attestations), and the temp dir.
func newTestOrchestrator(t *testing.T, attest bool) (*FSOrchestrator, *evidence.MemoryStore) {
	t.Helper()
	tmp := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	require.NoError(t, err, "generate ephemeral signer")

	var ledger *evidence.Ledger
	if attest {
		ledger, err = evidence.NewLedger(evidence.LedgerConfig{
			Store:    store,
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		require.NoError(t, err, "build ledger")
	}

	orch, err := NewFSOrchestrator(tmp, ledger)
	require.NoError(t, err, "new FSOrchestrator")
	return orch, store
}

// newTestRegistry creates a temporary model registry for cross-package integration.
func newTestRegistry(t *testing.T) (modelregistry.Registry, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "models")
	reg, err := modelregistry.NewFSRegistry(dir, nil)
	require.NoError(t, err, "new FSRegistry")
	return reg, dir
}

// validSubmitInput returns a canonical SubmitInput for testing.
func validSubmitInput(name string) SubmitInput {
	return SubmitInput{
		Name:       name,
		Image:      "pytorch:2.0",
		GPUCount:   4,
		MemoryGB:   32,
		DatasetRef: "sha256:dataset-abc",
		Command:    "python train.py",
	}
}

// runJobToRunning pushes a job through queued → scheduled → running.
func runJobToRunning(t *testing.T, orch Orchestrator, jobID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, orch.Schedule(ctx, jobID), "schedule must succeed")
	require.NoError(t, orch.Start(ctx, jobID), "start must succeed")
}

// TestSubmit_Creates_Queued_With_Attestation verifies that Submit creates a job in queued
// status, persists it to disk as JSON, and writes a real attestation via the ledger.
func TestSubmit_Creates_Queued_With_Attestation(t *testing.T) {
	orch, store := newTestOrchestrator(t, true)
	ctx := context.Background()

	job, err := orch.Submit(ctx, validSubmitInput("test-job"))
	require.NoError(t, err, "submit must succeed")

	assert.NotEmpty(t, job.ID, "job ID must be generated")
	assert.Equal(t, StatusQueued, job.Status, "new job must be queued")
	assert.Equal(t, "test-job", job.Name)
	assert.Equal(t, "pytorch:2.0", job.Image)
	assert.Equal(t, 4, job.GPUCount)
	assert.Equal(t, 32, job.MemoryGB)
	assert.False(t, job.CreatedAt.IsZero(), "CreatedAt must be set")

	// Verify persisted to disk.
	jobFile := filepath.Join(orch.Root(), job.ID+".json")
	_, statErr := os.Stat(jobFile)
	assert.NoError(t, statErr, "job JSON must exist on disk")

	// Verify attestation count (>=1 means train.submit was recorded).
	count, err := store.Count(ctx)
	require.NoError(t, err, "count attestations")
	assert.GreaterOrEqual(t, count, int64(1), "submit must write at least one attestation")

	// LastAttestation must be a real receipt (non-empty hash).
	last := orch.LastAttestation()
	require.NotNil(t, last, "LastAttestation must return receipt")
	assert.Equal(t, "train.submit", last.Action)
	assert.NotEmpty(t, last.Hash, "receipt must have a content hash")
	assert.NotEmpty(t, last.Signature, "receipt must be signed")
}

// TestStateMachine_Valid_Path walks the full happy path:
// queued → scheduled → running → succeeded, verifying event history integrity.
func TestStateMachine_Valid_Path(t *testing.T) {
	orch, store := newTestOrchestrator(t, true)
	ctx := context.Background()

	job, err := orch.Submit(ctx, validSubmitInput("happy-path"))
	require.NoError(t, err)

	require.NoError(t, orch.Schedule(ctx, job.ID))
	require.NoError(t, orch.Start(ctx, job.ID))
	require.NoError(t, orch.Complete(ctx, job.ID, "training finished", "", nil))

	// Reload from disk and verify full event history.
	final, err := orch.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, final.Status)

	require.Len(t, final.Events, 4, "expected 4 transitions: submit, schedule, start, complete")
	// First event: "" → queued
	assert.Equal(t, JobStatus(""), final.Events[0].From)
	assert.Equal(t, StatusQueued, final.Events[0].To)
	// queued → scheduled
	assert.Equal(t, StatusQueued, final.Events[1].From)
	assert.Equal(t, StatusScheduled, final.Events[1].To)
	// scheduled → running
	assert.Equal(t, StatusScheduled, final.Events[2].From)
	assert.Equal(t, StatusRunning, final.Events[2].To)
	// running → succeeded
	assert.Equal(t, StatusRunning, final.Events[3].From)
	assert.Equal(t, StatusSucceeded, final.Events[3].To)

	// Timestamps must be set.
	require.NotNil(t, final.ScheduledAt, "ScheduledAt must be set")
	require.NotNil(t, final.StartedAt, "StartedAt must be set")
	require.NotNil(t, final.CompletedAt, "CompletedAt must be set")

	// 4 attestations: submit + 3 transitions.
	count, err := store.Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(4), count, "submit + 3 transitions = 4 attestations")
}

// TestStateMachine_Illegal_Transition_Rejected verifies that illegal transitions
// (e.g., succeeded→running) return errors and leave the job state unchanged.
func TestStateMachine_Illegal_Transition_Rejected(t *testing.T) {
	orch, _ := newTestOrchestrator(t, false)
	ctx := context.Background()

	job, err := orch.Submit(ctx, validSubmitInput("illegal"))
	require.NoError(t, err)
	runJobToRunning(t, orch, job.ID)
	require.NoError(t, orch.Complete(ctx, job.ID, "done", "", nil))

	// Attempt illegal: succeeded → running (via Start).
	err = orch.Start(ctx, job.ID)
	require.Error(t, err, "succeeded→running must be rejected")
	assert.Contains(t, err.Error(), "invalid transition")

	// Attempt illegal: succeeded → scheduled.
	err = orch.Schedule(ctx, job.ID)
	require.Error(t, err, "succeeded→scheduled must be rejected")

	// Attempt illegal: succeeded → failed.
	err = orch.Fail(ctx, job.ID, "should not work")
	require.Error(t, err, "succeeded→failed must be rejected")

	// State must remain succeeded with no extra events.
	final, err := orch.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, final.Status, "state must remain unchanged after rejected transitions")
	assert.Len(t, final.Events, 4, "no new events after rejected transitions")
}

// TestComplete_Registers_Model_Version verifies the cross-package integration:
// Complete with artifactPath+registry registers a new model version with
// ParentVersion=base version and DatasetRef carried over (lineage closure).
func TestComplete_Registers_Model_Version(t *testing.T) {
	orch, _ := newTestOrchestrator(t, true)
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	// Register base model resnet50:1.0.0.
	baseArtPath := filepath.Join(t.TempDir(), "base.pt")
	require.NoError(t, os.WriteFile(baseArtPath, []byte("base weights"), 0o644))
	_, err := reg.Register(ctx, modelregistry.RegisterInput{
		Name:         "resnet50",
		Version:      "1.0.0",
		ArtifactPath: baseArtPath,
	})
	require.NoError(t, err, "register base model")

	// Submit a fine-tuning job based on resnet50:1.0.0.
	in := validSubmitInput("fine-tune-resnet")
	in.BaseModel = "resnet50:1.0.0"
	job, err := orch.Submit(ctx, in)
	require.NoError(t, err)
	runJobToRunning(t, orch, job.ID)

	// Complete with artifact: should register resnet50:1.1.0 (minor bump).
	artPath := filepath.Join(t.TempDir(), "tuned.pt")
	require.NoError(t, os.WriteFile(artPath, []byte("fine-tuned weights"), 0o644))
	require.NoError(t, orch.Complete(ctx, job.ID, "fine-tune done", artPath, reg))

	// Verify new version in registry.
	arts, err := reg.List(ctx, "resnet50")
	require.NoError(t, err)
	require.Len(t, arts, 2, "base + fine-tuned version")

	// Find the new version.
	var newArt *modelregistry.ModelArtifact
	for i := range arts {
		if arts[i].Version == "1.1.0" {
			newArt = &arts[i]
		}
	}
	require.NotNil(t, newArt, "new version 1.1.0 must exist (minor bump from 1.0.0)")
	assert.Equal(t, "1.0.0", newArt.Lineage.ParentVersion, "ParentVersion must point to base")
	assert.Equal(t, "sha256:dataset-abc", newArt.Lineage.DatasetRef, "DatasetRef must be carried over")

	// LastRegisteredArtifact must return the registered artifact.
	lastReg := orch.LastRegisteredArtifact()
	require.NotNil(t, lastReg)
	assert.Equal(t, "1.1.0", lastReg.Version)

	// Run a second fine-tune job: version must bump to 1.2.0 (1.1.0 already taken).
	job2, err := orch.Submit(ctx, in)
	require.NoError(t, err)
	runJobToRunning(t, orch, job2.ID)
	artPath2 := filepath.Join(t.TempDir(), "tuned2.pt")
	require.NoError(t, os.WriteFile(artPath2, []byte("second fine-tuned weights"), 0o644))
	require.NoError(t, orch.Complete(ctx, job2.ID, "second fine-tune", artPath2, reg))

	arts2, err := reg.List(ctx, "resnet50")
	require.NoError(t, err)
	require.Len(t, arts2, 3, "base + 2 fine-tuned versions")
	assert.Equal(t, "1.2.0", arts2[0].Version, "second job must register 1.2.0 (1.1.0 taken, minor+1)")
	assert.Equal(t, "1.0.0", arts2[0].Lineage.ParentVersion, "second job parent is still 1.0.0")
}

// TestCancel_From_Queued verifies cancelling a job that has not started.
func TestCancel_From_Queued(t *testing.T) {
	orch, _ := newTestOrchestrator(t, true)
	ctx := context.Background()

	job, err := orch.Submit(ctx, validSubmitInput("cancel-me"))
	require.NoError(t, err)

	err = orch.Cancel(ctx, job.ID, "user requested")
	require.NoError(t, err, "cancel from queued must succeed")

	final, err := orch.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, final.Status)
	require.Len(t, final.Events, 2, "submit + cancel events")

	// Cancel again: must fail (terminal state).
	err = orch.Cancel(ctx, job.ID, "cancel again")
	require.Error(t, err, "cancelling a cancelled job must fail")
	assert.Contains(t, err.Error(), "terminal")
}

// TestList_And_Get verifies listing multiple jobs and querying by ID.
func TestList_And_Get(t *testing.T) {
	orch, _ := newTestOrchestrator(t, false)
	ctx := context.Background()

	// Empty list.
	jobs, err := orch.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, jobs, "list must be empty initially")

	// Create 3 jobs.
	job1, err := orch.Submit(ctx, validSubmitInput("job-one"))
	require.NoError(t, err)
	job2, err := orch.Submit(ctx, validSubmitInput("job-two"))
	require.NoError(t, err)
	job3, err := orch.Submit(ctx, validSubmitInput("job-three"))
	require.NoError(t, err)

	jobs, err = orch.List(ctx)
	require.NoError(t, err)
	assert.Len(t, jobs, 3, "all 3 jobs must be listed")

	// Get by ID returns the correct job.
	fetched, err := orch.Get(ctx, job2.ID)
	require.NoError(t, err)
	assert.Equal(t, job2.ID, fetched.ID)
	assert.Equal(t, "job-two", fetched.Name)
	assert.Equal(t, StatusQueued, fetched.Status)

	// Get with unknown ID returns error.
	_, err = orch.Get(ctx, "job-doesnotexist")
	require.Error(t, err, "unknown ID must return error")
	assert.Contains(t, err.Error(), "not found")

	// List entries must all be queued.
	for _, j := range jobs {
		assert.Equal(t, StatusQueued, j.Status)
	}

	// Verify all 3 IDs are present in the list.
	ids := map[string]bool{}
	for _, j := range jobs {
		ids[j.ID] = true
	}
	assert.True(t, ids[job1.ID] && ids[job2.ID] && ids[job3.ID], "all job IDs must appear in list")
}
