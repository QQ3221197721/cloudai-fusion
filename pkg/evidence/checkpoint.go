package evidence

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"
)

// checkpoint.go bridges the evidence records to the RFC 6962 Merkle tree and
// produces the artifacts a third party actually consumes:
//
//   - Checkpoint: a signed tree head (size + root) committing to the WHOLE log.
//   - InclusionProofResponse: proof that one receipt is in the log.
//   - ConsistencyProofResponse: proof the log grew append-only between two sizes.
//
// A Merkle leaf commits to a receipt's canonical content (the same bytes that
// back its Hash), domain-separated per RFC 6962.

// checkpointOrigin domain-separates our signed tree heads from any other use of
// the signing key, and identifies the log to a verifier.
const checkpointOrigin = "cloudai-fusion/evidence"

// Checkpoint is a signed tree head: a compact, signed commitment to the entire
// ledger state at a point in time. Pin it out-of-band and later prove any
// receipt's inclusion or the log's append-only growth against it.
type Checkpoint struct {
	Origin    string    `json:"origin"`
	TreeSize  int       `json:"tree_size"`
	RootHash  string    `json:"root_hash"` // hex sha256 Merkle root
	Timestamp time.Time `json:"timestamp"`
	KeyID     string    `json:"key_id"`
	Signature string    `json:"signature"` // base64 Ed25519 over checkpointSigningBytes
}

// checkpointSigningBytes is the canonical, domain-separated message signed for a
// checkpoint. Keeping it a stable, simple format lets other languages reproduce it.
func checkpointSigningBytes(size int, rootHex string) []byte {
	return []byte(fmt.Sprintf("%s\n%d\n%s\n", checkpointOrigin, size, rootHex))
}

// InclusionProofResponse proves a specific receipt is committed by a checkpoint.
type InclusionProofResponse struct {
	RecordID   string      `json:"record_id"`
	LeafIndex  int         `json:"leaf_index"`
	TreeSize   int         `json:"tree_size"`
	LeafHash   string      `json:"leaf_hash"`  // hex RFC 6962 leaf hash
	AuditPath  []string    `json:"audit_path"` // hex sibling hashes, leaf->root
	Checkpoint *Checkpoint `json:"checkpoint"`
}

// ConsistencyProofResponse proves the size-First tree is an append-only prefix of
// the size-Second tree — i.e. no historical receipt was altered or removed.
type ConsistencyProofResponse struct {
	First      int         `json:"first"`
	Second     int         `json:"second"`
	FirstRoot  string      `json:"first_root"`
	SecondRoot string      `json:"second_root"`
	Proof      []string    `json:"proof"` // hex nodes
	Checkpoint *Checkpoint `json:"checkpoint"`
}

// merkleLeafHash returns the RFC 6962 leaf hash committing to a receipt's
// canonical content (same bytes that Hash covers), so the tree commits to
// content, not merely to the stored hash string.
func merkleLeafHash(e *Evidence) ([]byte, error) {
	data, err := e.content()
	if err != nil {
		return nil, err
	}
	return mLeafHash(data), nil
}

// leafHashesFromRecords maps an ascending-Seq record slice to Merkle leaf hashes.
func leafHashesFromRecords(records []*Evidence) ([][]byte, error) {
	out := make([][]byte, len(records))
	for i, e := range records {
		h, err := merkleLeafHash(e)
		if err != nil {
			return nil, err
		}
		out[i] = h
	}
	return out, nil
}

// signCheckpoint signs a tree head for the given size and root.
func (l *Ledger) signCheckpoint(size int, root []byte) (*Checkpoint, error) {
	rootHex := hex.EncodeToString(root)
	cp := &Checkpoint{
		Origin:    checkpointOrigin,
		TreeSize:  size,
		RootHash:  rootHex,
		Timestamp: time.Now().UTC(),
		KeyID:     l.signer.KeyID(),
	}
	sig, err := l.signer.Sign(checkpointSigningBytes(size, rootHex))
	if err != nil {
		return nil, err
	}
	cp.Signature = sig
	return cp, nil
}

// Checkpoint returns a freshly signed tree head over the whole current ledger.
func (l *Ledger) Checkpoint(ctx context.Context) (*Checkpoint, error) {
	all, err := l.store.All(ctx)
	if err != nil {
		return nil, err
	}
	leaves, err := leafHashesFromRecords(all)
	if err != nil {
		return nil, err
	}
	return l.signCheckpoint(len(all), merkleRoot(leaves))
}

// InclusionProofByID returns an inclusion proof for the receipt with the given
// ID, plus a signed checkpoint over the same tree snapshot. Returns (nil, nil)
// when the ID is unknown.
func (l *Ledger) InclusionProofByID(ctx context.Context, id string) (*InclusionProofResponse, error) {
	all, err := l.store.All(ctx)
	if err != nil {
		return nil, err
	}
	idx := -1
	for i, e := range all {
		if e.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil
	}
	leaves, err := leafHashesFromRecords(all)
	if err != nil {
		return nil, err
	}
	cp, err := l.signCheckpoint(len(all), merkleRoot(leaves))
	if err != nil {
		return nil, err
	}
	return &InclusionProofResponse{
		RecordID:   id,
		LeafIndex:  idx,
		TreeSize:   len(all),
		LeafHash:   hex.EncodeToString(leaves[idx]),
		AuditPath:  hexNodes(inclusionProof(idx, leaves)),
		Checkpoint: cp,
	}, nil
}

// ConsistencyProof returns a proof that the size-`from` tree is an append-only
// prefix of the size-`to` tree. `to` <= 0 or beyond the ledger size means "the
// whole current log".
func (l *Ledger) ConsistencyProof(ctx context.Context, from, to int) (*ConsistencyProofResponse, error) {
	all, err := l.store.All(ctx)
	if err != nil {
		return nil, err
	}
	n := len(all)
	if to <= 0 || to > n {
		to = n
	}
	if from < 0 || from > to {
		return nil, fmt.Errorf("evidence: invalid consistency range from=%d to=%d (size=%d)", from, to, n)
	}
	leaves, err := leafHashesFromRecords(all)
	if err != nil {
		return nil, err
	}
	firstRoot := merkleRoot(leaves[:from])
	secondRoot := merkleRoot(leaves[:to])
	cp, err := l.signCheckpoint(to, secondRoot)
	if err != nil {
		return nil, err
	}
	return &ConsistencyProofResponse{
		First:      from,
		Second:     to,
		FirstRoot:  hex.EncodeToString(firstRoot),
		SecondRoot: hex.EncodeToString(secondRoot),
		Proof:      hexNodes(consistencyProof(from, leaves[:to])),
		Checkpoint: cp,
	}, nil
}

// hexNodes hex-encodes a list of proof node hashes.
func hexNodes(nodes [][]byte) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = hex.EncodeToString(n)
	}
	return out
}
