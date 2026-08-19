// Package experiment - Tracker tests.
//
// Each test exercises the real state machine, persistence, and attestations.
// The Compare() math is precisely verified (test 5: A vs B with overlapping metrics).
package experiment

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestTracker opens a fresh tracker in a temp directory with attestation.
func createTestTracker(t *testing.T) (*FSTracker, string) {
	t.Helper()
	dir := t.TempDir()
	signer, serr := evidence.GenerateEphemeralSigner()
	require.NoError(t, serr, "generate signer must not fail")
	ledger, lerr := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	require.NoError(t, lerr, "build ledger must not fail")
	trk, terr := NewFSTracker(dir, ledger)
	require.NoError(t, terr, "create tracker must not fail")
	return trk, dir
}

func TestStart_Creates_Running_With_Attestation(t *testing.T) {
	trk, dir := createTestTracker(t)
	name := "cifar-lr-sweep"

	exp, err := trk.Start(context.Background(), StartInput{
		Name:      name,
		Hyperparams: map[string]string{"lr": "0.001", "batch": "32", "epochs": "50"},
		Actor:     "cafctl-experiment",
	})
	require.NoError(t, err, "start must succeed")

	// ID format exp-<hex16>.
	assert.Regexp(t, "^exp-[a-f0-9]{16}$", exp.ID, "ID must be exp-<16 hex chars>")
	assert.Equal(t, name, exp.Name, "name must match input")
	assert.Equal(t, StatusRunning, exp.Status, "status must be running")
	assert.Equal(t, map[string]string{"lr": "0.001", "batch": "32", "epochs": "50"}, exp.Hyperparams)
	assert.Empty(t, exp.Metrics, "metrics start empty")
	assert.Empty(t, exp.MetricHistory, "metric history starts empty")
	assert.True(t, exp.CompletedAt.IsZero(), "CompletedAt zero on start (running)")
	assert.False(t, exp.CreatedAt.IsZero(), "CreatedAt must be set")

	// Actually, we passed empty training job ref above. Fix input:
	exp2, err := trk.Start(context.Background(), StartInput{Name: name, Hyperparams: nil})
	require.NoError(t, err, "second start must succeed")
	assert.Empty(t, exp2.TrainingJobRef, "TrainingJobRef defaults to empty when not provided")

	// Attestation written.
	last := trk.LastAttestation()
	assert.NotNil(t, last, "last attestation must be non-nil")
	assert.Equal(t, "experiment.start", last.Action, "action must be experiment.start")
	
	// File persisted.
	file := filepath.Join(dir, "experiments", exp.ID+".json")
	data, err := os.ReadFile(file)
	require.NoError(t, err, "persisted file must exist")
	var loaded Experiment
	require.NoError(t, json.Unmarshal(data, &loaded), "persisted JSON must parse")
	assert.Equal(t, exp.ID, loaded.ID, "ID matches")
	assert.Equal(t, exp.Status, loaded.Status, "status matches")
	// Verify hyperparams roundtrip.
	b, jerr := json.Marshal(exp.Hyperparams)
	require.NoError(t, jerr)
	b2, jerr2 := json.Marshal(loaded.Hyperparams)
	require.NoError(t, jerr2)
	assert.JSONEq(t, string(b), string(b2), "hyperparams match after read/write")
}

func TestLogMetric_Appends_And_Allows_Overwrite(t *testing.T) {
	trk, _ := createTestTracker(t)
	
	// Start an experiment.
	exp, err := trk.Start(context.Background(), StartInput{Name: "acc-tracker"})
	require.NoError(t, err)

	// Log accuracy metric.
	err = trk.LogMetric(context.Background(), exp.ID, "accuracy", 0.90)
	require.NoError(t, err, "log first accuracy must succeed")

	expA, err := trk.Get(context.Background(), exp.ID)
	require.NoError(t, err)
	assert.Equal(t, float64(0.90), expA.Metrics["accuracy"], "Metrics['accuracy'] must be 0.90")
	assert.Len(t, expA.MetricHistory, 1, "metric history must have 1 entry")
	assert.Equal(t, "accuracy", expA.MetricHistory[0].Name)
	assert.InDelta(t, 0.90, expA.MetricHistory[0].Value, 1e-9)

	// Log same metric again — overwrites latest value but appends history.
	err = trk.LogMetric(context.Background(), exp.ID, "accuracy", 0.94)
	require.NoError(t, err, "log second accuracy must succeed")

	expB, err := trk.Get(context.Background(), exp.ID)
	require.NoError(t, err)
	assert.Equal(t, float64(0.94), expB.Metrics["accuracy"], "Metrics['accuracy'] overwritten to 0.94")
	assert.Len(t, expB.MetricHistory, 2, "history must now have 2 entries")
	assert.InDelta(t, 0.94, expB.Metrics["accuracy"], 1e-9)
}

func TestLogMetric_Rejected_After_Complete(t *testing.T) {
	trk, _ := createTestTracker(t)

	// Start + complete.
	exp, err := trk.Start(context.Background(), StartInput{Name: "terminal-test"})
	require.NoError(t, err)
	err = trk.Complete(context.Background(), exp.ID, "resnet50:1.1.0")
	require.NoError(t, err, "complete must succeed")

	// Try log metric after complete — must be rejected.
	err = trk.LogMetric(context.Background(), exp.ID, "accuracy", 0.99)
	require.Error(t, err, "log after complete must be rejected")
	assert.Contains(t, err.Error(), "terminal", "error must mention terminal state")
	assert.Contains(t, err.Error(), "immutable", "error must explain immutability")

	// Status stays completed.
	expAfter, err := trk.Get(context.Background(), exp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, expAfter.Status, "status must stay completed")
}

func TestComplete_Fills_ModelRef_And_Status(t *testing.T) {
	trk, _ := createTestTracker(t)

	// Start without model ref.
	exp, err := trk.Start(context.Background(), StartInput{Name: "ref-test"})
	require.NoError(t, err)
	assert.Empty(t, exp.ModelVersionRef, "ModelVersionRef empty before complete")
	assert.Empty(t, exp.FailReason, "FailReason empty before complete")
	assert.True(t, exp.CompletedAt.IsZero(), "CompletedAt zero before complete")

	// Complete with model version.
	const expectedVer = "resnet50:1.1.0"
	err = trk.Complete(context.Background(), exp.ID, expectedVer)
	require.NoError(t, err)

	expAfter, err := trk.Get(context.Background(), exp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, expAfter.Status, "status must be completed")
	assert.Equal(t, expectedVer, expAfter.ModelVersionRef, "ModelVersionRef filled")
	assert.False(t, expAfter.CompletedAt.IsZero(), "CompletedAt must be set")
	assert.LessOrEqual(t, exp.CreatedAt.Unix(), expAfter.CompletedAt.Unix(), "CompletedAt >= CreatedAt")

	// Verify state-machine enforcement: complete → complete rejected.
	err = trk.Complete(context.Background(), exp.ID, "v2")
	require.Error(t, err, "complete on completed experiment must be rejected")
	assert.Contains(t, err.Error(), "expected running", "error explains wrong state")
}

func TestFail_Logs_Reason_and_Rejects_Metric(t *testing.T) {
	trk, _ := createTestTracker(t)

	// Start + fail.
	exp, err := trk.Start(context.Background(), StartInput{Name: "oom-test"})
	require.NoError(t, err)
	reason := "CUDA out of memory (GPU 0)"
	err = trk.Fail(context.Background(), exp.ID, reason)
	require.NoError(t, err)

	expAfter, err := trk.Get(context.Background(), exp.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, expAfter.Status, "status must be failed")
	assert.Equal(t, reason, expAfter.FailReason, "FailReason recorded")
	assert.False(t, expAfter.CompletedAt.IsZero(), "CompletedAt set")

	// Metric logging rejected on failed experiment too.
	err = trk.LogMetric(context.Background(), exp.ID, "loss", 1e-4)
	require.Error(t, err, "log on failed must be rejected")
}

func TestCompare_Diff_Correctness(t *testing.T) {
	trk, _ := createTestTracker(t)

	// Create expA: lr=0.001,batch=32; acc=0.90,loss=0.30
	a, err := trk.Start(context.Background(), StartInput{
		Name:          "exp-a",
		Hyperparams: map[string]string{"lr": "0.001", "batch": "32"},
	})
	require.NoError(t, err)
	trk.LogMetric(context.Background(), a.ID, "accuracy", 0.90)
	trk.LogMetric(context.Background(), a.ID, "loss", 0.30)
	trk.Complete(context.Background(), a.ID, "")

	// Create expB: lr=0.01,batch=32; acc=0.94,loss=0.30,f1=0.88
	b, err := trk.Start(context.Background(), StartInput{
		Name:          "exp-b",
		Hyperparams: map[string]string{"lr": "0.01", "batch": "32"},
	})
	require.NoError(t, err)
	trk.LogMetric(context.Background(), b.ID, "accuracy", 0.94)
	trk.LogMetric(context.Background(), b.ID, "loss", 0.30)
	trk.LogMetric(context.Background(), b.ID, "f1", 0.88)
	trk.Complete(context.Background(), b.ID, "resnet50:1.2.0")

	// Perform comparison.
	res, err := trk.Compare(context.Background(), a.ID, b.ID)
	require.NoError(t, err)

	// HyperparamDiff: only keys with differing values (lr differs; batch equal not listed).
	assert.Len(t, res.HyperparamDiff, 1, "HyperparamDiff must contain exactly one key")
	pair, ok := res.HyperparamDiff["lr"]
	assert.True(t, ok, "lr must be present in diff")
	assert.Equal(t, [2]string{"0.001", "0.01"}, pair, "lr values correct")
	_, batchOk := res.HyperparamDiff["batch"]
	assert.False(t, batchOk, "batch not listed (same value)")

	// MetricCompare: union of metrics (acc, loss, f1).
	assert.Len(t, res.MetricCompare, 3, "MetricCompare must have union size 3")
	assert.Equal(t, [2]float64{0.90, 0.94}, res.MetricCompare["accuracy"], "accuracy [a,b]")
	assert.Equal(t, [2]float64{0.30, 0.30}, res.MetricCompare["loss"], "loss [a,b]")
	assert.Equal(t, [2]float64{0.0, 0.88}, res.MetricCompare["f1"], "f1 [a,b]: a missing reads as 0")
	_, accMissingInA := res.MetricCompare["accuracy_missing"]
	assert.False(t, accMissingInA, "no phantom missing-in-A key")

	// MetricDeltaPct verification:
	//   accuracy: (0.94 - 0.90) / |0.90| * 100 = 4.444...%
	//   loss: (0.30 - 0.30) / |0.30| * 100 = 0.0%
	//   f1: (0.88 - 0) / |0| * 100 = +Inf (guard: a==0 && b!=0 → +Inf)
	deltaAcc := res.MetricDeltaPct["accuracy"]
	deltaLoss := res.MetricDeltaPct["loss"]
	deltaF1 := res.MetricDeltaPct["f1"]

	assert.InDelta(t, 4.4444444, deltaAcc, 0.01, "accuracy Δ%% ≈ +4.44%%")
	assert.InDelta(t, 0.0, deltaLoss, 1e-9, "loss Δ%% = 0.0%%")
	assert.True(t, math.IsInf(deltaF1, 1), "f1 Δ%% must be +Inf (a==0 guard)")

	// Verify fields are correctly set on A and B pointers.
	assert.NotNil(t, res.A, "A pointer must be non-nil")
	assert.NotNil(t, res.B, "B pointer must be non-nil")
	assert.Equal(t, a.ID, res.A.ID, "A.ID matches start result")
	assert.Equal(t, b.ID, res.B.ID, "B.ID matches start result")
}

func TestList_Sorted_Desc(t *testing.T) {
	trk, root := createTestTracker(t)

	// Create three experiments with different creation times. (We'll adjust CreatedAt
	// manually to ensure deterministic ordering.)

	// e1: earliest
	e1, err := trk.Start(context.Background(), StartInput{Name: "first"})
	require.NoError(t, err)

	// e2: middle
	e2, err := trk.Start(context.Background(), StartInput{Name: "second"})
	require.NoError(t, err)

	// e3: latest
	e3, err := trk.Start(context.Background(), StartInput{Name: "third"})
	require.NoError(t, err)

	// Persist genuinely distinct CreatedAt values on disk to ensure deterministic ordering.
	// e1: oldest (base minus 2 hours), e2: middle (base minus 1 hour), e3: latest (base).
	e1File := filepath.Join(root, "experiments", e1.ID+".json")
	e2File := filepath.Join(root, "experiments", e2.ID+".json")

	// Marshal e1 with adjusted time (oldest)
	data1, err := os.ReadFile(e1File)
	require.NoError(t, err)
	var exp1 Experiment
	require.NoError(t, json.Unmarshal(data1, &exp1))
	exp1.CreatedAt = exp1.CreatedAt.Add(-2 * time.Hour)
	data1Out, err := json.MarshalIndent(exp1, "", "  ")
	require.NoError(t, err)
	tmp1 := e1File + ".tmp"
	require.NoError(t, os.WriteFile(tmp1, data1Out, 0o644))
	require.NoError(t, os.Rename(tmp1, e1File))

	// Marshal e2 with adjusted time (middle)
	data2, err := os.ReadFile(e2File)
	require.NoError(t, err)
	var exp2 Experiment
	require.NoError(t, json.Unmarshal(data2, &exp2))
	exp2.CreatedAt = exp2.CreatedAt.Add(-1 * time.Hour)
	data2Out, err := json.MarshalIndent(exp2, "", "  ")
	require.NoError(t, err)
	tmp2 := e2File + ".tmp"
	require.NoError(t, os.WriteFile(tmp2, data2Out, 0o644))
	require.NoError(t, os.Rename(tmp2, e2File))

	// e3 already exists at base time (latest). No sleep needed — timestamps are now truly distinct.

	// Now List should return [e3, e2, e1].
	list := trk.List(context.Background())
	require.Len(t, list, 3, "List must return 3 experiments")
	
	assert.Equal(t, e3.ID, list[0].ID, "newest-first: first element == e3")
	assert.Equal(t, e2.ID, list[1].ID, "middle element == e2")
	assert.Equal(t, e1.ID, list[2].ID, "oldest == e1")
}

func TestConcurrentAccess_Safety(t *testing.T) {
	trk, _ := createTestTracker(t)

	// Start one experiment concurrently from multiple goroutines (not recommended,
	// but ensures mutex safety).
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			_, err := trk.Start(context.Background(), StartInput{Name: "concurrent-exp"})
			done <- err
		}(i)
	}

	// All starts will succeed (duplicate names allowed, IDs unique), but only one
	// will win the mu lock at Submit time. We expect no panics or data races.
	for i := 0; i < 10; i++ {
		err := <-done
		require.NoError(t, err, "concurrent start #%d must not panic", i)
	}
}

