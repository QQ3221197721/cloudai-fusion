// Package modelregistry — tamper-detection tests and performance benchmarks for
// Module 13. These tests exercise the ONE capability that structurally separates
// this registry from MLflow/DVC: cryptographically verifiable, offline-checkable
// model provenance. MLflow and DVC record versions and lineage as ordinary
// database rows that can be edited with no cryptographic trace; here every
// version is content-addressed AND bound to a signed, hash-chained attestation,
// so any post-registration edit is provably detected.
package modelregistry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerChain registers root:1.0.0 then child:1.1.0 (parent=1.0.0) and returns
// the registry. Used by the tamper tests so there is real lineage to attack.
func registerChain(t *testing.T, attest bool) *FSRegistry {
	t.Helper()
	reg, _, cleanup := newTestRegistry(t, attest)
	t.Cleanup(cleanup)

	ctx := context.Background()
	_, err := reg.Register(ctx, RegisterInput{
		Name: "resnet50", Version: "1.0.0",
		ArtifactPath: writeArtifact(t, "root.pt", []byte("root weights")),
		DatasetRef:   "sha256:dataset-A", CodeRef: "git:commit-A",
		Metrics: map[string]float64{"accuracy": 0.80},
	})
	require.NoError(t, err)
	_, err = reg.Register(ctx, RegisterInput{
		Name: "resnet50", Version: "1.1.0",
		ArtifactPath:  writeArtifact(t, "tuned.pt", []byte("fine-tuned weights")),
		ParentVersion: "1.0.0",
		DatasetRef:    "sha256:dataset-B", CodeRef: "git:commit-B",
		Metrics: map[string]float64{"accuracy": 0.91},
	})
	require.NoError(t, err)
	return reg
}

// rewriteVersionRecord loads name:version.json, applies mutate, and writes it
// back — simulating an attacker (or careless operator) editing the provenance
// record on disk exactly as they could UPDATE a MLflow/DVC database row.
func rewriteVersionRecord(t *testing.T, reg *FSRegistry, name, version string, mutate func(*ModelArtifact)) {
	t.Helper()
	path := filepath.Join(reg.Root(), name, version+".json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var a ModelArtifact
	require.NoError(t, json.Unmarshal(data, &a))
	mutate(&a)
	out, err := json.MarshalIndent(&a, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, out, 0o644))
}

// TestVerify_Clean: a freshly registered version passes every integrity check.
func TestVerify_Clean(t *testing.T) {
	reg := registerChain(t, true)

	rep, err := reg.Verify(context.Background(), "resnet50", "1.1.0")
	require.NoError(t, err)
	assert.False(t, rep.Tampered, "clean record must not be flagged as tampered")
	assert.True(t, rep.BlobPresent)
	assert.True(t, rep.BlobHashOK, "content address must verify")
	assert.True(t, rep.AttestationFound, "signed attestation must be found")
	assert.True(t, rep.RecordDigestOK, "on-disk digest must match sealed digest")
	assert.True(t, rep.ChainVerified, "attestation chain must verify offline")
	t.Logf("clean verify checks:\n  %s", join(rep.Checks))
}

// TestVerify_LineageTamper is the headline test: silently rewrite the lineage
// record on disk (orphan the fine-tune parent + swap the dataset ref) and prove
// our registry detects it cryptographically, while a plain-DB baseline cannot.
func TestVerify_LineageTamper(t *testing.T) {
	reg := registerChain(t, true)
	ctx := context.Background()

	// Baseline: what a MLflow/DVC-style plain row would return before/after edit.
	// We model it as the value the registry would SERVE (Lineage walk) — no
	// cryptographic reference exists to compare it against.
	before, err := reg.Lineage(ctx, "resnet50", "1.1.0")
	require.NoError(t, err)
	require.Equal(t, 2, before.Depth, "before tamper: child links to parent (depth 2)")

	// Attack: rewrite the on-disk provenance record — drop the parent link and
	// forge the dataset reference. On a plain DB this is a single UPDATE.
	rewriteVersionRecord(t, reg, "resnet50", "1.1.0", func(a *ModelArtifact) {
		a.Lineage.ParentVersion = "" // orphan: hide that this was fine-tuned
		a.Lineage.DatasetRef = "sha256:FORGED-clean-dataset"
	})

	// Plain-DB baseline: the tampered row is served as if authentic. The lineage
	// walk now reports a root model (depth 1) with a forged dataset — undetected.
	after, err := reg.Lineage(ctx, "resnet50", "1.1.0")
	require.NoError(t, err)
	assert.Equal(t, 1, after.Depth, "plain-DB view: tampered record silently looks like a root model")
	assert.Equal(t, "sha256:FORGED-clean-dataset", after.Nodes[0].Lineage.DatasetRef,
		"plain-DB view: forged dataset ref is served with no error")

	// Our registry: Verify recomputes the record digest and compares it to the
	// signed, hash-chained attestation — the tamper is provably caught.
	rep, err := reg.Verify(ctx, "resnet50", "1.1.0")
	require.NoError(t, err)
	assert.True(t, rep.Tampered, "lineage tamper MUST be detected")
	assert.True(t, rep.AttestationFound, "the original signed attestation is still present")
	assert.False(t, rep.RecordDigestOK, "recomputed digest must diverge from the sealed digest")
	assert.True(t, rep.ChainVerified, "the attestation chain itself remains intact and signed")
	t.Logf("TAMPER DETECTED — checks:\n  %s", join(rep.Checks))
}

// TestVerify_BlobTamper: substituting the content-addressed weights blob with
// attacker bytes breaks the content address and is detected even without any
// lineage edit.
func TestVerify_BlobTamper(t *testing.T) {
	reg := registerChain(t, true)
	ctx := context.Background()

	art, err := reg.Get(ctx, "resnet50", "1.0.0")
	require.NoError(t, err)
	blobPath := filepath.Join(reg.Root(), blobsDir, art.SHA256)
	require.NoError(t, os.WriteFile(blobPath, []byte("malicious backdoored weights"), 0o644))

	rep, err := reg.Verify(ctx, "resnet50", "1.0.0")
	require.NoError(t, err)
	assert.True(t, rep.Tampered, "blob substitution MUST be detected")
	assert.True(t, rep.BlobPresent)
	assert.False(t, rep.BlobHashOK, "recomputed sha256 must no longer match the content address")
	t.Logf("BLOB TAMPER DETECTED — checks:\n  %s", join(rep.Checks))
}

// TestVerify_ChainTamper: editing a receipt in the attestation ledger breaks the
// hash chain, so the offline chain verification fails.
func TestVerify_ChainTamper(t *testing.T) {
	reg := registerChain(t, true)
	ctx := context.Background()

	// Tamper the ledger: flip a field on the first receipt. MemoryStore hands out
	// the live pointers, so this mutates the stored record — recomputing its leaf
	// hash will no longer match the stored/signed hash.
	all, err := reg.ledger.Store().All(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, all)
	all[0].Actor = all[0].Actor + "-IMPERSONATED"

	rep, err := reg.Verify(ctx, "resnet50", "1.1.0")
	require.NoError(t, err)
	assert.False(t, rep.ChainVerified, "a broken attestation chain must fail verification")
	assert.True(t, rep.Tampered, "ledger tampering MUST be detected")
	t.Logf("CHAIN TAMPER DETECTED — checks:\n  %s", join(rep.Checks))
}

// TestVerify_NoLedger: with no ledger, the signed cross-check is honestly
// skipped, but content-addressing still catches blob substitution.
func TestVerify_NoLedger(t *testing.T) {
	reg, _, cleanup := newTestRegistry(t, false) // attest=false => nil ledger
	defer cleanup()
	ctx := context.Background()

	_, err := reg.Register(ctx, RegisterInput{
		Name: "noledger", Version: "1.0.0",
		ArtifactPath: writeArtifact(t, "w.pt", []byte("weights")),
	})
	require.NoError(t, err)

	clean, err := reg.Verify(ctx, "noledger", "1.0.0")
	require.NoError(t, err)
	assert.False(t, clean.Tampered)
	assert.True(t, clean.BlobHashOK)
	assert.False(t, clean.AttestationFound, "no ledger => no attestation cross-check")

	// Content-address still guards the blob.
	art, err := reg.Get(ctx, "noledger", "1.0.0")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(reg.Root(), blobsDir, art.SHA256), []byte("tampered"), 0o644))
	bad, err := reg.Verify(ctx, "noledger", "1.0.0")
	require.NoError(t, err)
	assert.True(t, bad.Tampered, "blob tamper is caught by content-addressing even without a ledger")
}

// join renders check lines for readable test logs.
func join(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n  "
		}
		out += l
	}
	return out
}
