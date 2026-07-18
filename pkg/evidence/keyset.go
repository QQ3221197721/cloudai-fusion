package evidence

import (
	"crypto/ed25519"
	"time"
)

// keyset.go adds support for KEY ROTATION over the lifetime of a long-lived log.
// A single signing key is the common case, but an append-only log that outlives a
// key must let a verifier resolve the correct public key PER RECORD (by KeyID).
// A KeySet is that resolver; VerifyChainWithKeySet is the rotation-aware verifier.

// PublicKeyEntry is a published verifying key with its KeyID, suitable for
// embedding in an export bundle so third parties can verify across rotations.
type PublicKeyEntry struct {
	KeyID string `json:"key_id"`
	PEM   string `json:"pem"`
}

// KeyRotation is the payload of a "key.rotate" receipt: a signed, verifiable
// record that the log's signing identity changed, linking old and new KeyIDs.
type KeyRotation struct {
	OldKeyID  string    `json:"old_key_id"`
	NewKeyID  string    `json:"new_key_id"`
	Reason    string    `json:"reason,omitempty"`
	RotatedAt time.Time `json:"rotated_at"`
}

// KeySet resolves a public key by KeyID for rotation-aware verification.
type KeySet map[string]ed25519.PublicKey

// NewKeySetFromEntries parses PEM entries into a KeySet, keyed by the derived
// KeyID (the entry's declared KeyID must match, guarding against mislabeling).
func NewKeySetFromEntries(entries []PublicKeyEntry) (KeySet, error) {
	ks := make(KeySet, len(entries))
	for _, e := range entries {
		pub, err := ParsePublicKeyPEM([]byte(e.PEM))
		if err != nil {
			return nil, err
		}
		ks[KeyIDFor(pub)] = pub
	}
	return ks, nil
}

// Add inserts a public key under its derived KeyID.
func (ks KeySet) Add(pub ed25519.PublicKey) { ks[KeyIDFor(pub)] = pub }

// VerifyChainWithKeySet verifies an ascending-Seq chain where records may be
// signed by different keys over time (rotation). Each record's signature is
// checked against the key named by its KeyID; a record whose KeyID is absent from
// the set fails verification (reported per-record, never panics).
func VerifyChainWithKeySet(records []*Evidence, keys KeySet) (*VerifyReport, error) {
	rep := &VerifyReport{Total: len(records), Valid: true, Records: make([]RecordResult, 0, len(records))}

	prevHash := GenesisPrevHash
	var prevSeq uint64
	for i, e := range records {
		res := RecordResult{Seq: e.Seq, ID: e.ID, Action: e.Action}

		recomputed, err := e.ComputeHash()
		if err != nil {
			res.Error = "hash: " + err.Error()
			rep.Records = append(rep.Records, res)
			rep.Failed++
			rep.Valid = false
			continue
		}
		res.HashOK = recomputed == e.Hash

		if i == 0 {
			res.ChainOK = e.PrevHash == GenesisPrevHash && e.Seq >= 1
		} else {
			res.ChainOK = e.PrevHash == prevHash && e.Seq == prevSeq+1
		}

		if pub, ok := keys[e.KeyID]; !ok {
			res.SignatureOK = false
			res.Error = "no public key for key_id " + e.KeyID
		} else if err := VerifyLeaf(pub, []byte(e.Hash), e.Signature); err != nil {
			res.SignatureOK = false
			if res.Error == "" {
				res.Error = err.Error()
			}
		} else {
			res.SignatureOK = true
		}

		if e.LogEntry != nil && e.LogEntry.Backend == "rekor" {
			res.AnchorReal = true
			rep.AnchoredReal++
			if e.LogEntry.Proof != nil && VerifyRekorInclusion(e.LogEntry) == nil {
				rep.RekorVerified++
			}
		}

		if res.OK() {
			rep.Verified++
		} else {
			rep.Failed++
			rep.Valid = false
		}
		rep.Records = append(rep.Records, res)
		prevHash = recomputed
		prevSeq = e.Seq
	}
	return rep, nil
}
