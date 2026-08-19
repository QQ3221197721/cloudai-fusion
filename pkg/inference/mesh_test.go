// Package inference - unit tests for Inference Service Mesh Module 15.
// Every test wires a REAL ledger (MemoryStore + EphemeralSigner + NewLedger) —
// never a nil ledger — mirroring modelregistry_test.go's construction pattern,
// and asserts on-disk persistence under <tmpDir>/inference/ (the path contract
// Module 16 taught us to pin down precisely).
package inference

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestMesh creates a fresh inference mesh with a real ledger for testing.
func newTestMesh(t *testing.T, attest bool) (*FSMInferenceMesh, *evidence.Ledger, *evidence.MemoryStore, func()) {
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

	mesh, err := NewFSMInferenceMesh(tmp, ledger)
	require.NoError(t, err, "new FSMInferenceMesh")

	cleanup := func() {
		if attest && ledger != nil {
			count, _ := store.Count(context.Background())
			t.Logf("final ledger count: %d", count)
		}
	}
	return mesh, ledger, store, cleanup
}

// TestDeploy_PersistsAndAttests: Deploy persists services.json under
// <tmp>/inference/, initializes 100% routing to the deployed version, and
// writes an "inference.deploy" attestation with the given actor.
func TestDeploy_PersistsAndAttests(t *testing.T) {
	mesh, _, store, cleanup := newTestMesh(t, true)
	defer cleanup()

	ctx := context.Background()

	svc, err := mesh.Deploy(ctx, DeployInput{
		Name:     "my-service",
		ModelRef: "my-model@v3",
		Endpoint: "http://custom.endpoint:8080",
		Replicas: 2,
		Actor:    "alice",
	})
	require.NoError(t, err)
	assert.Equal(t, "my-service", svc.Name)
	assert.Equal(t, "my-model@v3", svc.ModelRef)
	assert.Equal(t, "http://custom.endpoint:8080", svc.Endpoint)
	assert.Equal(t, StatusServing, svc.Status)
	assert.Equal(t, 2, svc.Replicas)
	assert.True(t, strings.HasPrefix(svc.ID, "inf-"), "ID prefix must be inf-")
	assert.Regexp(t, `^inf-[0-9a-f]{16}$`, svc.ID, "ID must be inf-<hex16>")
	assert.Equal(t, map[string]int{"v3": 100}, svc.Routes, "initial route must be 100% to deployed version")
	assert.False(t, svc.CreatedAt.IsZero())

	// Path contract: store lives under <tmp>/inference/services.json.
	servicesPath := filepath.Join(mesh.Root(), "services.json")
	data, err := os.ReadFile(servicesPath)
	require.NoError(t, err, "services.json must exist on disk under the inference/ subdir")
	assert.Contains(t, string(data), "my-service")
	assert.Contains(t, string(data), "my-model@v3")

	// The receipt is real and points at the deployed service.
	last := mesh.LastAttestation()
	require.NotNil(t, last, "attestation must be written when ledger is wired")
	assert.Equal(t, "inference.deploy", last.Action)
	assert.Equal(t, svc.ID, last.Subject)
	assert.Equal(t, "alice", last.Actor)

	recs, err := store.All(ctx)
	require.NoError(t, err)
	require.Len(t, recs, 1, "exactly one ledger record after one deploy")
	assert.Equal(t, "inference.deploy", recs[0].Action)
}

// TestDeploy_AutoEndpoint: an empty Endpoint is auto-generated from the ID.
func TestDeploy_AutoEndpoint(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	svc, err := mesh.Deploy(context.Background(), DeployInput{
		Name: "auto-ep", ModelRef: "m@v1", Replicas: 1,
	})
	require.NoError(t, err)
	assert.Contains(t, svc.Endpoint, svc.ID, "auto endpoint embeds the service ID")
	assert.NotEmpty(t, svc.Endpoint)
}

// TestDeploy_RejectsInvalidModelRef: malformed refs are rejected before any
// persistence or ledger write.
func TestDeploy_RejectsInvalidModelRef(t *testing.T) {
	mesh, _, store, cleanup := newTestMesh(t, true)
	defer cleanup()

	ctx := context.Background()

	cases := []struct {
		ref       string
		name      string
		wantError string
	}{
		{"no-at-sign", "missing @", `expected "name@version"`},
		{"@v1", "empty name", "invalid model ref"},
		{"foo@", "empty version", "invalid model ref"},
		{"foo@bar@baz", "double @ (version 'bar@baz' fails charset)", "invalid model ref"},
	}
	for _, tc := range cases {
		_, err := mesh.Deploy(ctx, DeployInput{Name: "bad", ModelRef: tc.ref, Replicas: 1})
		require.Error(t, err, "case %s must fail", tc.name)
		assert.Contains(t, err.Error(), tc.wantError, "case %s error mentions %q", tc.name, tc.wantError)
	}

	// Nothing was persisted or attested.
	recs, err := store.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, recs, "no ledger records after rejected deploys")
	svcList, err := mesh.ListServices()
	require.NoError(t, err)
	assert.Empty(t, svcList)
}

// TestDeploy_RejectsBadInput: name/replicas guards.
func TestDeploy_RejectsBadInput(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	_, err := mesh.Deploy(ctx, DeployInput{Name: "", ModelRef: "m@v1", Replicas: 1})
	assert.ErrorContains(t, err, "service name is required")

	_, err = mesh.Deploy(ctx, DeployInput{Name: "x", ModelRef: "m@v1", Replicas: 0})
	assert.ErrorContains(t, err, "replica count must be positive")
}

// TestDeploy_ValidatorInjection: a ValidateModelFunc rejection aborts Deploy;
// acceptance passes through (Module 13 integration seam stays decoupled).
func TestDeploy_ValidatorInjection(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()

	mesh.SetModelValidator(func(modelName, version string) error {
		return errors.New("registry: model \"ghost\" has no version \"v9\"")
	})

	_, err := mesh.Deploy(ctx, DeployInput{Name: "blocked", ModelRef: "ghost@v9", Replicas: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model validation failed")
	assert.Contains(t, err.Error(), "registry: model")

	// Swap in an accepting validator: deploy now succeeds and the receipt
	// records that the model was validated.
	mesh.SetModelValidator(func(string, string) error { return nil })
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "allowed", ModelRef: "ghost@v9", Replicas: 1})
	require.NoError(t, err)
	assert.Equal(t, "allowed", svc.Name)
}

// TestSetRoute_Success: valid weights replace the route table and attest.
func TestSetRoute_Success(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, true)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "routable", ModelRef: "base@v3", Replicas: 3})
	require.NoError(t, err)

	err = mesh.SetRoute(ctx, svc.ID, map[string]int{"v3": 70, "v4": 30})
	require.NoError(t, err)

	got, err := mesh.GetService(svc.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"v3": 70, "v4": 30}, got.Routes)

	last := mesh.LastAttestation()
	require.NotNil(t, last)
	assert.Equal(t, "inference.route", last.Action)
	assert.Equal(t, svc.ID, last.Subject)
}

// TestSetRoute_WeightSumNot100: any total != 100 is rejected.
func TestSetRoute_WeightSumNot100(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "guarded", ModelRef: "test@v1", Replicas: 1})
	require.NoError(t, err)

	err = mesh.SetRoute(ctx, svc.ID, map[string]int{"v1": 90})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must sum to 100")
	assert.Contains(t, err.Error(), "90")

	// Route table unchanged after the rejection.
	got, err := mesh.GetService(svc.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"v1": 100}, got.Routes, "routes must be untouched after a rejected SetRoute")
}

// TestSetRoute_RejectsBadWeights: non-positive weights and empty maps are errors.
func TestSetRoute_RejectsBadWeights(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "w", ModelRef: "test@v1", Replicas: 1})
	require.NoError(t, err)

	err = mesh.SetRoute(ctx, svc.ID, map[string]int{"v1": 50, "v2": -50})
	assert.ErrorContains(t, err, "must be positive")

	err = mesh.SetRoute(ctx, svc.ID, map[string]int{"v1": 100, "v2": 0})
	assert.ErrorContains(t, err, "must be positive")

	err = mesh.SetRoute(ctx, svc.ID, map[string]int{})
	assert.ErrorContains(t, err, "cannot be empty")
}

// TestSetRoute_Rejected_StoppedService: stopped services reject route updates.
func TestSetRoute_Rejected_StoppedService(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "stopped", ModelRef: "x@v1", Replicas: 1})
	require.NoError(t, err)
	require.NoError(t, mesh.Stop(ctx, svc.ID))

	err = mesh.SetRoute(ctx, svc.ID, map[string]int{"v1": 100})
	assert.ErrorIs(t, err, ErrStopped)
}

// TestSetRoute_UnknownServiceAndBadID: not-found + path-traversal guard.
func TestSetRoute_UnknownServiceAndBadID(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()

	err := mesh.SetRoute(ctx, "inf-doesnotexist99", map[string]int{"v1": 100})
	assert.ErrorIs(t, err, ErrNotFound)

	err = mesh.SetRoute(ctx, "../escape", map[string]int{"v1": 100})
	assert.ErrorIs(t, err, ErrInvalidServiceID)

	err = mesh.SetRoute(ctx, "INF-UPPER", map[string]int{"v1": 100})
	assert.ErrorIs(t, err, ErrInvalidServiceID)
}

// TestRecordStat_RejectsBadLatency: p95 < p50 violates the monotonic triple.
func TestRecordStat_RejectsBadLatency(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "stats", ModelRef: "model@v1", Replicas: 1})
	require.NoError(t, err)

	err = mesh.RecordStat(ctx, svc.ID, LoadStat{
		LatencyP50Ms: 100, LatencyP95Ms: 50, LatencyP99Ms: 150,
		Requests: 100, Errors: 5, ThroughputRPS: 10,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "p50<=p95<=p99")

	// p99 < p95 also rejected.
	err = mesh.RecordStat(ctx, svc.ID, LoadStat{
		LatencyP50Ms: 50, LatencyP95Ms: 150, LatencyP99Ms: 100,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "p50<=p95<=p99")

	// Negative counters rejected.
	err = mesh.RecordStat(ctx, svc.ID, LoadStat{Requests: -1})
	assert.ErrorContains(t, err, "non-negative")
}

// TestRecordStat_AppendsJSONLAndReads: valid stats append one JSONL line each
// under <tmp>/inference/<id>/stats.jsonl and Stats() reads them back newest-first.
func TestRecordStat_AppendsJSONLAndReads(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, true)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "stats", ModelRef: "model@v1", Replicas: 1})
	require.NoError(t, err)

	first := LoadStat{
		LatencyP50Ms: 10, LatencyP95Ms: 20, LatencyP99Ms: 30,
		Requests: 100, Errors: 1, ThroughputRPS: 12.5,
		Timestamp: time.Now().UTC().Add(-time.Minute),
	}
	require.NoError(t, mesh.RecordStat(ctx, svc.ID, first))

	second := LoadStat{ // zero Timestamp → filled with now
		LatencyP50Ms: 12, LatencyP95Ms: 25, LatencyP99Ms: 40,
		Requests: 200, Errors: 2, ThroughputRPS: 20,
	}
	require.NoError(t, mesh.RecordStat(ctx, svc.ID, second))

	// Path contract: stats live at <root>/<serviceID>/stats.jsonl.
	statsPath := filepath.Join(mesh.Root(), svc.ID, "stats.jsonl")
	raw, err := os.ReadFile(statsPath)
	require.NoError(t, err, "stats.jsonl must exist on disk")
	lines := strings.Count(strings.TrimSpace(string(raw)), "\n") + 1
	assert.Equal(t, 2, lines, "two appended JSONL lines")

	all, err := mesh.Stats(svc.ID, 0)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, int64(200), all[0].Requests, "newest first")
	assert.Equal(t, int64(100), all[1].Requests)
	assert.Equal(t, svc.ID, all[0].ServiceID, "ServiceID is normalized on append")
	assert.False(t, all[0].Timestamp.IsZero(), "zero Timestamp is filled with now")

	// limit=1 returns only the newest.
	one, err := mesh.Stats(svc.ID, 1)
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, int64(200), one[0].Requests)

	// The receipt is an inference.stat record.
	last := mesh.LastAttestation()
	require.NotNil(t, last)
	assert.Equal(t, "inference.stat", last.Action)
}

// TestRecordStat_UnknownService: stats for an unknown service are rejected.
func TestRecordStat_UnknownService(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	err := mesh.RecordStat(context.Background(), "inf-nosuchservice1", LoadStat{})
	assert.ErrorIs(t, err, ErrNotFound)

	// Path traversal guard.
	err = mesh.RecordStat(context.Background(), "..%2Fetc", LoadStat{})
	assert.ErrorIs(t, err, ErrInvalidServiceID)
}

// TestStats_NoStatsYet: a deployed service with no recorded stats returns a
// clear not-found error.
func TestStats_NoStatsYet(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	svc, err := mesh.Deploy(context.Background(), DeployInput{Name: "quiet", ModelRef: "m@v1", Replicas: 1})
	require.NoError(t, err)

	_, err = mesh.Stats(svc.ID, 10)
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestListAndGet: ListServices is newest-first; GetService round-trips; unknown
// IDs are ErrNotFound.
func TestListAndGet(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	a, err := mesh.Deploy(ctx, DeployInput{Name: "svc-a", ModelRef: "modelA@v1", Replicas: 1})
	require.NoError(t, err)
	time.Sleep(5 * time.Millisecond) // distinct CreatedAt
	b, err := mesh.Deploy(ctx, DeployInput{Name: "svc-b", ModelRef: "modelB@v2", Replicas: 2})
	require.NoError(t, err)

	list, err := mesh.ListServices()
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, b.ID, list[0].ID, "newest first")
	assert.Equal(t, a.ID, list[1].ID)

	got, err := mesh.GetService(a.ID)
	require.NoError(t, err)
	assert.Equal(t, "svc-a", got.Name)
	assert.Equal(t, "modelA@v1", got.ModelRef)

	_, err = mesh.GetService("inf-unknown0000000")
	assert.ErrorIs(t, err, ErrNotFound)

	// Persistence contract: a fresh mesh over the same dir sees both services.
	mesh2, err := NewFSMInferenceMesh(filepath.Dir(mesh.Root()), nil)
	require.NoError(t, err)
	reloaded, err := mesh2.ListServices()
	require.NoError(t, err)
	assert.Len(t, reloaded, 2, "services.json reloads across instances")
}

// TestStop_IdempotentReject: first Stop succeeds and attests; a second Stop is
// an explicit error; the persisted status is stopped.
func TestStop_IdempotentReject(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, true)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "stop-me", ModelRef: "x@v1", Replicas: 1})
	require.NoError(t, err)

	require.NoError(t, mesh.Stop(ctx, svc.ID))

	got, err := mesh.GetService(svc.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusStopped, got.Status)

	err = mesh.Stop(ctx, svc.ID)
	require.Error(t, err, "stopping a stopped service must be rejected")
	assert.ErrorIs(t, err, ErrStopped)

	last := mesh.LastAttestation()
	require.NotNil(t, last)
	assert.Equal(t, "inference.stop", last.Action)

	err = mesh.Stop(ctx, "inf-neverdeployed0")
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestMarkDegraded: serving → degraded with reason; repeated or non-serving
// sources are rejected.
func TestMarkDegraded(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, true)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "flaky", ModelRef: "m@v1", Replicas: 2})
	require.NoError(t, err)

	require.NoError(t, mesh.MarkDegraded(ctx, svc.ID, "p95 above SLO"))

	got, err := mesh.GetService(svc.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDegraded, got.Status)

	// degraded → degraded rejected.
	err = mesh.MarkDegraded(ctx, svc.ID, "again")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot mark")

	// degraded → stop → mark degraded also rejected.
	require.NoError(t, mesh.Stop(ctx, svc.ID))
	err = mesh.MarkDegraded(ctx, svc.ID, "while stopped")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot mark")

	last := mesh.LastAttestation()
	require.NotNil(t, last)
	assert.Equal(t, "inference.stop", last.Action, "last successful op was the stop")
}

// TestNilLedgerDisablesAttestationOnly: with a nil ledger all behavior works,
// just without receipts (parity with Module 13/14 semantics).
func TestNilLedgerDisablesAttestationOnly(t *testing.T) {
	mesh, err := NewFSMInferenceMesh(t.TempDir(), nil)
	require.NoError(t, err)

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "noattest", ModelRef: "m@v1", Replicas: 1})
	require.NoError(t, err)
	require.NoError(t, mesh.SetRoute(ctx, svc.ID, map[string]int{"v1": 60, "v2": 40}))
	require.NoError(t, mesh.RecordStat(ctx, svc.ID, LoadStat{LatencyP50Ms: 1, LatencyP95Ms: 2, LatencyP99Ms: 3, Requests: 5}))
	require.NoError(t, mesh.Stop(ctx, svc.ID))

	assert.Nil(t, mesh.LastAttestation())

	got, err := mesh.GetService(svc.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusStopped, got.Status)
	assert.Equal(t, map[string]int{"v1": 60, "v2": 40}, got.Routes)

	stats, err := mesh.Stats(svc.ID, 0)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, int64(5), stats[0].Requests)
}

// TestRecordStat_RejectsNonFiniteMetrics: NaN/Inf in any float field are rejected
// with a precise error message before any write (stats file never created).
func TestRecordStat_RejectsNonFiniteMetrics(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, true)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "finite-svc", ModelRef: "m@v1", Replicas: 1})
	require.NoError(t, err)

	cases := []struct {
		name     string
		stat     LoadStat
		wantMsg  string
	}{
		{"p50 nan", LoadStat{LatencyP50Ms: math.NaN(), LatencyP95Ms: 10, LatencyP99Ms: 20, Requests: 1}, "latency_p50_ms"},
		{"p95 inf", LoadStat{LatencyP50Ms: 10, LatencyP95Ms: math.Inf(1), LatencyP99Ms: 20, Requests: 2}, "latency_p95_ms"},
		{"p99 ninf", LoadStat{LatencyP50Ms: 10, LatencyP95Ms: 20, LatencyP99Ms: math.Inf(-1), Requests: 3}, "latency_p99_ms"},
		{"throughput nan", LoadStat{LatencyP50Ms: 10, LatencyP95Ms: 20, LatencyP99Ms: 30, Requests: 4, ThroughputRPS: math.NaN()}, "throughput_rps"},
	}
	for _, tc := range cases {
		err = mesh.RecordStat(ctx, svc.ID, tc.stat)
		require.Error(t, err, "case %s must fail", tc.name)
		assert.Contains(t, err.Error(), tc.wantMsg, "case %s error mentions field %q", tc.name, tc.wantMsg)
		assert.Contains(t, err.Error(), "finite", "case %s error mentions finite", tc.name)

		// File should NOT exist after the rejection.
		statsPath := filepath.Join(mesh.Root(), svc.ID, "stats.jsonl")
		_, serr := os.Stat(statsPath)
		assert.True(t, os.IsNotExist(serr), "case %s stats.jsonl must not be created", tc.name)
	}
}

// TestRecordStat_StatsDirConflict verifies the create-stats-dir error path when
// root/<serviceID> exists as a non-directory (MkdirAll fails).
func TestRecordStat_StatsDirConflict(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "conflict", ModelRef: "m@v1", Replicas: 1})
	require.NoError(t, err)

	// Overwrite the service dir with a regular file (simulate conflict).
	dirPath := filepath.Join(mesh.Root(), svc.ID)
	statsPath := filepath.Join(dirPath, "stats.jsonl")
	// Remove the directory and recreate it as a file
	_ = os.RemoveAll(dirPath)
	err = os.WriteFile(dirPath, []byte("garbage"), 0o644)
	require.NoError(t, err)

	// RecordStat should fail with "create stats dir" error.
	err = mesh.RecordStat(ctx, svc.ID, LoadStat{
		LatencyP50Ms: 10, LatencyP95Ms: 20, LatencyP99Ms: 30,
		Requests: 1, Errors: 0,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create stats dir", "error must mention MkdirAll failure")

	// No JSONL file should have been created anywhere near this ID.
	_, serr := os.Stat(statsPath)
	assert.True(t, os.IsNotExist(serr), "stats.jsonl must not exist after conflict")
}

// TestRecordStat_WriteFailureNoTornLine exercises the write-failure path by
// replacing stats.jsonl with a directory (OpenFile fails) and verifying that
// prior JSONL records remain intact (no torn line).
func TestRecordStat_WriteFailureNoTornLine(t *testing.T) {
	mesh, _, _, cleanup := newTestMesh(t, false)
	defer cleanup()

	ctx := context.Background()
	svc, err := mesh.Deploy(ctx, DeployInput{Name: "torn-test", ModelRef: "m@v1", Replicas: 1})
	require.NoError(t, err)

	// Write one valid record first.
	statsPath := filepath.Join(mesh.Root(), svc.ID, "stats.jsonl")
	err = mesh.RecordStat(ctx, svc.ID, LoadStat{
		LatencyP50Ms: 5, LatencyP95Ms: 10, LatencyP99Ms: 15,
		Requests: 10, Errors: 0,
		Timestamp: time.Now().UTC(),
	})
	require.NoError(t, err, "first record must succeed")

	// Snapshot the valid line bytes before corruption.
	initialBytes, err := os.ReadFile(statsPath)
	require.NoError(t, err, "read initial stats.jsonl")

	// Replace stats.jsonl with a directory to simulate an open-failure scenario.
	_ = os.Remove(statsPath)
	err = os.Mkdir(statsPath, 0o755)
	require.NoError(t, err, "replace file with directory for next record")

	// Second record must fail with an open error.
	err = mesh.RecordStat(ctx, svc.ID, LoadStat{
		LatencyP50Ms: 20, LatencyP95Ms: 30, LatencyP99Ms: 40,
		Requests: 20, Errors: 1,
	})
	require.Error(t, err, "second record must fail due to directory conflict")
	assert.Contains(t, err.Error(), "open stats file", "error must come from OpenFile on a dir")

	// Verify prior record is untouched: restore the snapshot, re-open mesh, read back.
	_ = os.Remove(statsPath) // remove the directory
	err = os.WriteFile(statsPath, initialBytes, 0o644)
	require.NoError(t, err, "restore stats.jsonl")

	// Re-open mesh over the same root to verify persistence integrity.
	mesh2, err := NewFSMInferenceMesh(filepath.Dir(mesh.Root()), nil)
	require.NoError(t, err)

	all, err := mesh2.Stats(svc.ID, 0)
	require.NoError(t, err, "Stats must still work after corrupted attempt")
	require.Len(t, all, 1, "only the original record should remain")
	assert.Equal(t, int64(10), all[0].Requests)
}
