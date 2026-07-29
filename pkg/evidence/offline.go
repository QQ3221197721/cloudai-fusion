package evidence

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// offline.go extends the Verifiable Control Plane (L13) to air-gapped operation.
//
// A connected node exports its signed chain to a portable bundle file; the file
// is carried across an air gap (USB / one-way diode) to another node or an
// auditor, who verifies it OFFLINE against a pinned public key and — when
// reconciling — merges the verified records into a local store. No network, no
// trust in the transport: the Ed25519 signatures, hash chain, and signed Merkle
// checkpoint are the only trust anchors.
//
// This file is purely additive over export.go/verifier.go: it adds file I/O and
// merge helpers and changes none of the existing signing/verification logic.

// ExportToFile exports the full chain and atomically writes it as a JSON bundle
// to path (temp file + rename, mode 0600). The written file is self-describing
// and verifiable offline via VerifyBundleFile.
func (l *Ledger) ExportToFile(ctx context.Context, path string) error {
	bundle, err := l.Export(ctx)
	if err != nil {
		return fmt.Errorf("evidence: export chain: %w", err)
	}
	return WriteBundleFile(path, bundle)
}

// WriteBundleFile marshals b to indented JSON and writes it atomically to path.
// The parent directory must already exist. Writing is atomic (temp + rename) so
// a partially written bundle is never observed by a concurrent reader.
func WriteBundleFile(path string, b *ExportBundle) error {
	if b == nil {
		return fmt.Errorf("evidence: nil bundle")
	}
	clean := filepath.Clean(path)
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("evidence: marshal bundle: %w", err)
	}
	tmp := clean + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("evidence: write temp bundle: %w", err)
	}
	if err := os.Rename(tmp, clean); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("evidence: finalize bundle: %w", err)
	}
	return nil
}

// ReadBundleFile reads and JSON-decodes an export bundle from path. It performs
// no verification; call VerifyBundle / VerifyBundleWithKey (or VerifyBundleFile)
// on the result.
func ReadBundleFile(path string) (*ExportBundle, error) {
	clean := filepath.Clean(path)
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, fmt.Errorf("evidence: read bundle file: %w", err)
	}
	var b ExportBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("evidence: decode bundle: %w", err)
	}
	return &b, nil
}

// VerifyBundleFile reads a bundle from bundlePath and verifies it OFFLINE against
// the Ed25519 public key pinned in pubKeyPath (PEM). This is the air-gapped
// auditor's one-shot check: it proves the transported chain is intact and signed
// by exactly the expected key, with no network access.
func VerifyBundleFile(bundlePath, pubKeyPath string) (*VerifyReport, error) {
	b, err := ReadBundleFile(bundlePath)
	if err != nil {
		return nil, err
	}
	pemBytes, err := os.ReadFile(filepath.Clean(pubKeyPath))
	if err != nil {
		return nil, fmt.Errorf("evidence: read pinned public key: %w", err)
	}
	pub, err := ParsePublicKeyPEM(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("evidence: parse pinned public key: %w", err)
	}
	return VerifyBundleWithKey(b, pub)
}

// MergeResult reports the outcome of importing an offline bundle into a store.
type MergeResult struct {
	Verified  bool `json:"verified"`   // the bundle passed offline verification
	Imported  int  `json:"imported"`   // records newly appended
	Skipped   int  `json:"skipped"`    // records already present (by ID)
	TotalSeen int  `json:"total_seen"` // records in the bundle
}

// ImportBundleToStore verifies b against the pinned key, then appends any records
// not already present (deduplicated by record ID) into store in ascending Seq
// order. Verification is mandatory: an invalid bundle imports nothing and returns
// an error, so a tampered transport can never poison the local chain.
//
// Records are appended verbatim (their Seq/PrevHash/signatures are preserved), so
// this reconciles an independently-signed air-gapped chain into a durable store
// for querying — it does not re-sign or re-chain.
func ImportBundleToStore(ctx context.Context, store Store, b *ExportBundle, pinned ed25519.PublicKey) (*MergeResult, error) {
	if store == nil {
		return nil, fmt.Errorf("evidence: nil store")
	}
	if b == nil {
		return nil, fmt.Errorf("evidence: nil bundle")
	}
	rep, err := VerifyBundleWithKey(b, pinned)
	if err != nil {
		return nil, fmt.Errorf("evidence: verify bundle: %w", err)
	}
	res := &MergeResult{Verified: rep.Valid, TotalSeen: len(b.Records)}
	if !rep.Valid {
		return res, fmt.Errorf("evidence: refusing to import an invalid bundle")
	}
	for _, e := range b.Records {
		if e == nil {
			continue
		}
		if existing, _ := store.Get(ctx, e.ID); existing != nil {
			res.Skipped++
			continue
		}
		if err := store.Append(ctx, e); err != nil {
			return res, fmt.Errorf("evidence: append imported record %s: %w", e.ID, err)
		}
		res.Imported++
	}
	return res, nil
}
