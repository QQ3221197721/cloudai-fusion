package evidence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestOfflineBundleFileRoundTrip covers the air-gapped happy path: export a
// signed chain to a file, read it back, and verify it offline against the pinned
// public key — with no network and no live ledger.
func TestOfflineBundleFileRoundTrip(t *testing.T) {
	ctx := context.Background()
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 5)

	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "chain.bundle.json")
	if err := l.ExportToFile(ctx, bundlePath); err != nil {
		t.Fatalf("ExportToFile: %v", err)
	}

	// Write the pinned public key next to the bundle (as an operator would).
	keyPath := filepath.Join(dir, "pub.pem")
	pemBytes, err := MarshalPublicKeyPEM(l.Signer().PublicKey())
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	writeTestFile(t, keyPath, pemBytes)

	rep, err := VerifyBundleFile(bundlePath, keyPath)
	if err != nil {
		t.Fatalf("VerifyBundleFile: %v", err)
	}
	if !rep.Valid {
		t.Fatalf("offline verification must pass for an untampered bundle: %+v", rep)
	}
	if !rep.CheckpointPresent || !rep.CheckpointVerified || !rep.CheckpointRootMatch {
		t.Fatalf("checkpoint checks must all pass: %+v", rep)
	}
}

// TestOfflineBundleFileTamperDetected ensures a modified record is caught by
// offline verification (the whole point of the signed chain across an air gap).
func TestOfflineBundleFileTamperDetected(t *testing.T) {
	ctx := context.Background()
	l := newTestLedger(t, NewMemoryStore())
	recordN(t, l, 4)

	b, err := l.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// Tamper: mutate a record's subject after signing.
	b.Records[1].Subject = "tampered"

	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "tampered.bundle.json")
	if err := WriteBundleFile(bundlePath, b); err != nil {
		t.Fatalf("WriteBundleFile: %v", err)
	}
	keyPath := filepath.Join(dir, "pub.pem")
	pemBytes, _ := MarshalPublicKeyPEM(l.Signer().PublicKey())
	writeTestFile(t, keyPath, pemBytes)

	rep, err := VerifyBundleFile(bundlePath, keyPath)
	if err != nil {
		t.Fatalf("VerifyBundleFile returned error: %v", err)
	}
	if rep.Valid {
		t.Fatalf("tampered bundle must fail verification")
	}
}

// TestImportBundleToStore verifies the reconcile path: a verified air-gapped
// bundle is merged into a fresh store, is idempotent (re-import skips), and an
// invalid bundle imports nothing.
func TestImportBundleToStore(t *testing.T) {
	ctx := context.Background()
	src := newTestLedger(t, NewMemoryStore())
	recordN(t, src, 6)

	bundle, err := src.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	pinned := src.Signer().PublicKey()

	dst := NewMemoryStore()
	res, err := ImportBundleToStore(ctx, dst, bundle, pinned)
	if err != nil {
		t.Fatalf("ImportBundleToStore: %v", err)
	}
	if !res.Verified || res.Imported != 6 || res.Skipped != 0 || res.TotalSeen != 6 {
		t.Fatalf("unexpected merge result: %+v", res)
	}
	if n, _ := dst.Count(ctx); n != 6 {
		t.Fatalf("store should hold 6 records, got %d", n)
	}

	// Idempotent re-import: everything is skipped.
	res2, err := ImportBundleToStore(ctx, dst, bundle, pinned)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res2.Imported != 0 || res2.Skipped != 6 {
		t.Fatalf("re-import should skip all: %+v", res2)
	}

	// Invalid bundle (wrong pinned key) imports nothing and errors.
	other, _ := GenerateEphemeralSigner()
	fresh := NewMemoryStore()
	if _, err := ImportBundleToStore(ctx, fresh, bundle, other.PublicKey()); err == nil {
		t.Fatalf("import must fail when the pinned key does not match")
	}
	if n, _ := fresh.Count(ctx); n != 0 {
		t.Fatalf("no records may be imported from an unverified bundle, got %d", n)
	}
}

func TestReadBundleFile_Errors(t *testing.T) {
	if _, err := ReadBundleFile(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatalf("ReadBundleFile must error on a missing file")
	}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
