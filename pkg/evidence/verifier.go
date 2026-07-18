package evidence

import (
	"crypto/ed25519"
	"fmt"
)

// verifier.go is the heart of "prove, don't trust": given an exported chain and
// the published Ed25519 public key, it re-derives every leaf hash, checks the
// unbroken hash chain, and verifies every signature — with ZERO dependency on
// the running platform. cmd/cafctl verify and the /api/v1/evidence/verify
// endpoint both call VerifyChain, so an auditor can reproduce the platform's
// claims byte-for-byte and offline.

// RecordResult is the per-record verification outcome.
type RecordResult struct {
	Seq          uint64 `json:"seq"`
	ID           string `json:"id"`
	Action       string `json:"action"`
	HashOK       bool   `json:"hash_ok"`       // recomputed hash matches stored hash
	ChainOK      bool   `json:"chain_ok"`      // prev_hash + seq link the previous record
	SignatureOK  bool   `json:"signature_ok"`  // signature verifies against the public key
	AnchorReal   bool   `json:"anchor_real"`   // externally anchored (e.g. Rekor) vs simulated
	Error        string `json:"error,omitempty"`
}

// OK reports whether this record passed hash, chain, and signature checks.
func (r RecordResult) OK() bool { return r.HashOK && r.ChainOK && r.SignatureOK }

// VerifyRecord checks a SINGLE receipt's hash integrity and signature against
// pub, without chain-linkage checks. Use it to verify a filtered subset (e.g.
// one workload's decisions) where records are not contiguous in the chain and
// so PrevHash linkage is not expected to hold. ChainOK is reported true (not
// applicable); full-chain linkage is checked by VerifyChain / the /verify API.
func VerifyRecord(e *Evidence, pub ed25519.PublicKey) RecordResult {
	res := RecordResult{Seq: e.Seq, ID: e.ID, Action: e.Action, ChainOK: true}
	recomputed, err := e.ComputeHash()
	if err != nil {
		res.Error = "hash: " + err.Error()
		return res
	}
	res.HashOK = recomputed == e.Hash
	if verr := VerifyLeaf(pub, []byte(e.Hash), e.Signature); verr != nil {
		res.Error = verr.Error()
	} else {
		res.SignatureOK = true
	}
	if e.LogEntry != nil && e.LogEntry.Backend == "rekor" {
		res.AnchorReal = true
	}
	return res
}

// VerifyReport is the aggregate result of verifying a chain.
type VerifyReport struct {
	Total       int            `json:"total"`
	Verified    int            `json:"verified"`     // records that passed all checks
	Failed      int            `json:"failed"`
	AnchoredReal int           `json:"anchored_real"` // records with a real transparency anchor
	RekorVerified int          `json:"rekor_verified"` // records whose Rekor inclusion proof verified offline
	KeyID       string         `json:"key_id"`
	Valid       bool           `json:"valid"` // true iff every record passed
	Records     []RecordResult `json:"records"`

	// Merkle transparency-log verification (populated by VerifyBundleWithKey).
	MerkleRoot          string `json:"merkle_root,omitempty"`          // recomputed RFC 6962 root over records
	CheckpointPresent   bool   `json:"checkpoint_present"`             // bundle carried a signed tree head
	CheckpointVerified  bool   `json:"checkpoint_verified"`            // checkpoint signature valid
	CheckpointRootMatch bool   `json:"checkpoint_root_match"`          // signed root == recomputed root & size
}

// VerifyChain verifies an ascending-Seq chain against pub. It never returns an
// error for verification FAILURES (those are captured per-record in the report);
// it only returns an error for malformed inputs. records must be ordered by Seq
// ascending (Store.All / the export endpoint provide this).
func VerifyChain(records []*Evidence, pub ed25519.PublicKey) (*VerifyReport, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("evidence: bad public key size %d (want %d)", len(pub), ed25519.PublicKeySize)
	}
	rep := &VerifyReport{Total: len(records), Valid: true, Records: make([]RecordResult, 0, len(records))}
	if len(records) > 0 {
		rep.KeyID = KeyIDFor(pub)
	}

	var prevHash string = GenesisPrevHash
	var prevSeq uint64
	for i, e := range records {
		res := RecordResult{Seq: e.Seq, ID: e.ID, Action: e.Action}

		// 1) Recompute the leaf hash from canonical content.
		recomputed, err := e.ComputeHash()
		if err != nil {
			res.Error = "hash: " + err.Error()
			rep.Records = append(rep.Records, res)
			rep.Failed++
			rep.Valid = false
			continue
		}
		res.HashOK = recomputed == e.Hash

		// 2) Verify the chain link on the RECOMPUTED hash (not the stored one):
		//    prev_hash must commit to the previous record's ACTUAL content. This
		//    way a single-byte edit anywhere propagates a chain break to every
		//    later record, even if the attacker leaves the stored hash untouched.
		if i == 0 {
			res.ChainOK = e.PrevHash == GenesisPrevHash && e.Seq >= 1
		} else {
			res.ChainOK = e.PrevHash == prevHash && e.Seq == prevSeq+1
		}

		// 3) Verify the signature over the (stored) leaf hash. If the hash was
		//    tampered, HashOK already fails; we still verify the signature over
		//    the stored hash so the report distinguishes hash vs signature breaks.
		if err := VerifyLeaf(pub, []byte(e.Hash), e.Signature); err != nil {
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
