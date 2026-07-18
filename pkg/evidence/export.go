package evidence

import (
	"context"
	"crypto/ed25519"
	"time"
)

// export.go defines the portable bundle an auditor takes offline to verify the
// ledger, and the helpers that verify it. The bundle carries the full chain, the
// verifying public key(s) (including any used across rotations), and a signed
// Merkle checkpoint. Verification covers four layers: per-record signatures
// (rotation-aware), the hash chain, the recomputed Merkle root, and the signed
// checkpoint that commits to it.

// ExportBundle is a self-describing, portable snapshot of the evidence chain.
type ExportBundle struct {
	KeyID        string           `json:"key_id"`
	PublicKeyPEM string           `json:"public_key_pem"`
	Keys         []PublicKeyEntry `json:"keys,omitempty"` // all keys used (rotation history)
	RunMode      string           `json:"run_mode,omitempty"`
	ExportedAt   time.Time        `json:"exported_at"`
	Count        int              `json:"count"`
	Checkpoint   *Checkpoint      `json:"checkpoint,omitempty"` // signed tree head over Records
	Records      []*Evidence      `json:"records"`
}

// Export produces a bundle of the entire chain, the verifying public key(s), and
// a signed checkpoint committing to exactly those records.
func (l *Ledger) Export(ctx context.Context) (*ExportBundle, error) {
	all, err := l.store.All(ctx)
	if err != nil {
		return nil, err
	}
	pemBytes, err := MarshalPublicKeyPEM(l.Signer().PublicKey())
	if err != nil {
		return nil, err
	}
	leaves, err := leafHashesFromRecords(all)
	if err != nil {
		return nil, err
	}
	cp, err := l.signCheckpoint(len(all), merkleRoot(leaves))
	if err != nil {
		return nil, err
	}
	return &ExportBundle{
		KeyID:        l.Signer().KeyID(),
		PublicKeyPEM: string(pemBytes),
		Keys:         l.KeyEntries(),
		RunMode:      l.cap.RunMode(),
		ExportedAt:   time.Now().UTC(),
		Count:        len(all),
		Checkpoint:   cp,
		Records:      all,
	}, nil
}

// VerifyBundle verifies a bundle against its OWN embedded key(s). It is
// rotation-aware: each record is checked against the key named by its KeyID from
// the bundle's key set. It proves internal consistency but not that the keys are
// the ones you expected — pin them out-of-band for trust.
func VerifyBundle(b *ExportBundle) (*VerifyReport, error) {
	keys, err := bundleKeySet(b)
	if err != nil {
		return nil, err
	}
	rep, err := VerifyChainWithKeySet(b.Records, keys)
	if err != nil {
		return nil, err
	}
	applyMerkleCheck(b, rep, func(cp *Checkpoint) bool {
		pub, ok := keys[cp.KeyID]
		return ok && VerifyCheckpoint(cp, pub) == nil
	})
	return rep, nil
}

// VerifyBundleWithKey verifies a bundle against a single caller-supplied (pinned)
// key. Suitable when no key rotation occurred; records signed by other keys will
// be reported as signature failures.
func VerifyBundleWithKey(b *ExportBundle, pub ed25519.PublicKey) (*VerifyReport, error) {
	rep, err := VerifyChain(b.Records, pub)
	if err != nil {
		return nil, err
	}
	applyMerkleCheck(b, rep, func(cp *Checkpoint) bool {
		return VerifyCheckpoint(cp, pub) == nil
	})
	return rep, nil
}

// bundleKeySet builds the verifying key set from the bundle: the explicit Keys
// list when present, otherwise the single embedded public key.
func bundleKeySet(b *ExportBundle) (KeySet, error) {
	if len(b.Keys) > 0 {
		return NewKeySetFromEntries(b.Keys)
	}
	pub, err := ParsePublicKeyPEM([]byte(b.PublicKeyPEM))
	if err != nil {
		return nil, err
	}
	ks := KeySet{}
	ks.Add(pub)
	return ks, nil
}

// applyMerkleCheck recomputes the Merkle root over the records and, if a
// checkpoint is present, requires it to verify (via checkpointVerify) AND to
// commit to exactly these records; failures set rep.Valid = false.
func applyMerkleCheck(b *ExportBundle, rep *VerifyReport, checkpointVerify func(*Checkpoint) bool) {
	rootHex, err := MerkleRootHexOfRecords(b.Records)
	if err != nil {
		rep.Valid = false
		return
	}
	rep.MerkleRoot = rootHex
	if b.Checkpoint != nil {
		rep.CheckpointPresent = true
		rep.CheckpointVerified = checkpointVerify(b.Checkpoint)
		rep.CheckpointRootMatch = b.Checkpoint.RootHash == rootHex && b.Checkpoint.TreeSize == len(b.Records)
		if !rep.CheckpointVerified || !rep.CheckpointRootMatch {
			rep.Valid = false
		}
	}
}
