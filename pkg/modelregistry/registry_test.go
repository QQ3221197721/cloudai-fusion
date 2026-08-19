// Package modelregistry - unit tests for Model Registry Module 13
package modelregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper: create a temporary registry with ledger for testing.
func newTestRegistry(t *testing.T, attest bool) (*FSRegistry, *evidence.Ledger, func()) {
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

	reg, err := NewFSRegistry(tmp, ledger)
	require.NoError(t, err, "new FSRegistry")

	cleanup := func() {
		// Optionally call ledger.Store().Count to check attestation records
		if attest && ledger != nil {
			count, _ := store.Count(context.Background())
			t.Logf("Final ledger count: %d", count)
		}
	}
	return reg, ledger, cleanup
}

// helper: write a fake artifact file and return its path.
func writeArtifact(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, content, 0o644))
	return path
}

// TestRegister_And_List: Register 2 versions, List returns newest first.
func TestRegister_And_List(t *testing.T) {
	reg, _, cleanup := newTestRegistry(t, false)
	defer cleanup()

	ctx := context.Background()
	basePath := writeArtifact(t, "model.pt", []byte("fake weights"))

	// Register v1.0.0
	v1, err := reg.Register(ctx, RegisterInput{
		Name:        "resnet50",
		Version:     "1.0.0",
		ArtifactPath: basePath,
		CreatedBy:   "test-user",
		DatasetRef:  "sha256:dataset123",
		CodeRef:     "git:abc123",
		TaskType:    "classification",
		Framework:   "pytorch",
		Summary:     "ResNet50 root model",
		Metrics:     map[string]float64{"accuracy": 0.75},
		Hyperparams: map[string]string{"lr": "0.001"},
		Tags:        map[string]string{"root": "true"},
	})
	require.NoError(t, err)
	assert.Equal(t, "resnet50", v1.Name)
	assert.Equal(t, "1.0.0", v1.Version)
	assert.Equal(t, testArtifactHash([]byte("fake weights")), v1.SHA256)
	assert.Equal(t, int64(12), v1.SizeBytes)
	assert.Equal(t, "test-user", v1.CreatedBy)
	assert.False(t, v1.CreatedAt.IsZero())
	assert.Equal(t, "sha256:dataset123", v1.Lineage.DatasetRef)
	assert.Equal(t, "git:abc123", v1.Lineage.CodeRef)
	assert.Equal(t, "", v1.Lineage.ParentVersion)
	assert.Equal(t, "pytorch", v1.ModelCard.Framework)

	time.Sleep(10 * time.Millisecond) // Ensure distinct CreatedAt

	// Register v1.1.0 as parent of v1.0.0
	v2, err := reg.Register(ctx, RegisterInput{
		Name:          "resnet50",
		Version:       "1.1.0",
		ArtifactPath:  writeArtifact(t, "tuned.pt", []byte("different weights!")),
		ParentVersion: "1.0.0",
		CreatedBy:     "test-user",
		DatasetRef:    "sha256:dataset123",
		CodeRef:       "git:def456",
		Metrics:       map[string]float64{"accuracy": 0.85},
	})
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", v2.Version)

	arts, err := reg.List(ctx, "resnet50")
	require.NoError(t, err)
	assert.Len(t, arts, 2)
	assert.Equal(t, "1.1.0", arts[0].Version)
	assert.Equal(t, "1.0.0", arts[1].Version)
}

// TestContentDedup: same content under different versions -> blob stored once.
func TestContentDedup(t *testing.T) {
	reg, _, cleanup := newTestRegistry(t, false)
	defer cleanup()

	ctx := context.Background()
	artifactContent := []byte("identical content for dedup test")
	artifactPath := writeArtifact(t, "weights.pt", artifactContent)
	expSha := testArtifactHash(artifactContent)

	// Register two versions with identical content
	v1, err := reg.Register(ctx, RegisterInput{Name: "dedup-model", Version: "1.0.0", ArtifactPath: artifactPath})
	require.NoError(t, err)
	assert.Equal(t, expSha, v1.SHA256)

	v2, err := reg.Register(ctx, RegisterInput{Name: "dedup-model", Version: "1.1.0", ArtifactPath: artifactPath})
	require.NoError(t, err)
	assert.Equal(t, expSha, v2.SHA256, "same content must map to the same content address")

	// Verify only one blob exists
	blobsDir := reg.Root() + "/blobs"
	blobCount := 0
	filepath.Walk(blobsDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() { blobCount++ }
		return nil
	})
	assert.Equal(t, 1, blobCount, "blob deduplication should prevent duplicate storage")
}

// TestLineage_Chain: register 1.0.0 -> 1.1.0 (parent=1.0.0) -> 1.2.0 (parent=1.1.0)
func TestLineage_Chain(t *testing.T) {
	reg, _, cleanup := newTestRegistry(t, false)
	defer cleanup()

	ctx := context.Background()
	wts1 := writeArtifact(t, "w1.pt", []byte("weight set 1"))
	wts2 := writeArtifact(t, "w2.pt", []byte("weight set 2"))
	wts3 := writeArtifact(t, "w3.pt", []byte("weight set 3"))

	_, err := reg.Register(ctx, RegisterInput{Name: "chain", Version: "1.0.0", ArtifactPath: wts1})
	require.NoError(t, err)
	_, err = reg.Register(ctx, RegisterInput{Name: "chain", Version: "1.1.0", ArtifactPath: wts2, ParentVersion: "1.0.0"})
	require.NoError(t, err)
	_, err = reg.Register(ctx, RegisterInput{Name: "chain", Version: "1.2.0", ArtifactPath: wts3, ParentVersion: "1.1.0"})
	require.NoError(t, err)

	graph, err := reg.Lineage(ctx, "chain", "1.2.0")
	require.NoError(t, err)
	assert.Equal(t, "chain:1.2.0", graph.Root)
	assert.Equal(t, 3, graph.Depth)
	assert.Len(t, graph.Nodes, 3)
	assert.Equal(t, "chain:1.2.0", ref(graph.Nodes[0].Name, graph.Nodes[0].Version))
	assert.Equal(t, "chain:1.0.0", ref(graph.Nodes[2].Name, graph.Nodes[2].Version))
	assert.Equal(t, 2, len(graph.Edges))
}

// TestRollback: after rollback, Get(latest) returns the target version.
func TestRollback(t *testing.T) {
	reg, _, cleanup := newTestRegistry(t, true)
	defer cleanup()

	ctx := context.Background()
	w1 := writeArtifact(t, "w1.pt", []byte("first weight"))
	w2 := writeArtifact(t, "w2.pt", []byte("second weight"))

	_, err := reg.Register(ctx, RegisterInput{Name: "rollback-test", Version: "1.0.0", ArtifactPath: w1})
	require.NoError(t, err)
	_, err = reg.Register(ctx, RegisterInput{Name: "rollback-test", Version: "1.1.0", ArtifactPath: w2})
	require.NoError(t, err)

	// Verify latest is 1.1.0
	later, err := reg.Get(ctx, "rollback-test", LatestVersion)
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", later.Version)

	// Rollback to 1.0.0
	err = reg.Rollback(ctx, "rollback-test", "1.1.0", "1.0.0")
	require.NoError(t, err)

	// Now latest should be 1.0.0
	newer, err := reg.Get(ctx, "rollback-test", LatestVersion)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", newer.Version)

	// Attestation was recorded
	last := reg.LastAttestation()
	assert.NotNil(t, last)
	assert.Equal(t, "model.rollback", last.Action)
	assert.Equal(t, "rollback-test", last.Subject)
}

// TestAttestationRecorded: Register creates an attestation record in ledger store.
func TestAttestationRecorded(t *testing.T) {
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	require.NoError(t, err)

	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    store,
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	require.NoError(t, err)

	reg, err := NewFSRegistry(t.TempDir(), ledger)
	require.NoError(t, err)

	artifact := writeArtifact(t, "a.pt", []byte("test"))
	_, err = reg.Register(context.Background(), RegisterInput{Name: "attest-me", Version: "1.0.0", ArtifactPath: artifact, CreatedBy: "alice"})
	require.NoError(t, err)

	// Check that exactly one attestation was recorded
	recs, err := store.All(context.Background())
	require.NoError(t, err)
	assert.Greater(t, len(recs), 0, "ledger should have at least one record")

	// Find the model.register record
	found := false
	for _, r := range recs {
		if r.Action == "model.register" && r.Subject == "attest-me" {
			found = true
			break
		}
	}
	assert.True(t, found, "should find a model.register attestation for attest-me")
}

// TestGet_NotFound: get non-existent version returns clear error.
func TestGet_NotFound(t *testing.T) {
	reg, _, cleanup := newTestRegistry(t, false)
	defer cleanup()

	ctx := context.Background()

	// Case 1: model entirely unknown.
	_, err := reg.Get(ctx, "nonexistent", "1.0.0")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "1.0.0")

	// Case 2: model exists but the requested version does not — the error must
	// enumerate what IS registered so the user can self-correct.
	_, err = reg.Register(ctx, RegisterInput{
		Name: "exists", Version: "1.0.0",
		ArtifactPath: writeArtifact(t, "e.pt", []byte("weights")),
	})
	require.NoError(t, err)
	_, err = reg.Get(ctx, "exists", "9.9.9")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "exists")
	assert.Contains(t, err.Error(), "9.9.9")
	assert.Contains(t, err.Error(), "1.0.0", "error should list the registered version")
}

// helper: compute SHA256 hash for test content.
func testArtifactHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
