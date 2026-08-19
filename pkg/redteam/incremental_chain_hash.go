package redteam

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// incremental_chain_hash.go implements an O(1)-per-append incremental Merkle-ish
// chain over an append-only set of evidence records (ledger rows). The user's
// moat spec demands that we never recompute the entire chain digest when appending
// a new record; instead we maintain a running state that can be updated in time
// linear with just the new record's content. A verifier can check the final
// digest without reconstructing all intermediate states.
//
// This is different from a full Merkle tree: we only need fast-forward updates
// because the ledger is strictly append-only (no deletions/updates), and each
// record references the previous record by its own digest. The incremental hash
// exposes exactly what the tests require: BuildChain(), Append(record) returns
// the new chain hash in O(len(record)), and Verify() recomputes naively to prove
// equivalence. Benchmarks compare both before/after to show O(n) vs O(n^2) growth.

// IncrementalChainHasher maintains a running SHA256 hash of an append-only chain
// of JSON-record digests. It supports O(len(record)) per Append and O(n) naive
// verification for honest proof.
type IncrementalChainHasher struct {
	hash        [32]byte // current H(chain_state)
	recordCount int
}

// NewIncrementalChainHasher creates a fresh hasher initialized to zero hash.
func NewIncrementalChainHasher() *IncrementalChainHasher {
	return &IncrementalChainHasher{hash: [32]byte{}, recordCount: 0}
}

// Digest returns the current cumulative hash as a hex string.
func (h *IncrementalChainHasher) Digest() string {
	return fmt.Sprintf("%x", h.hash[:])
}

// BuildChain hashes a slice of records and returns the cumulative chain digest.
// Each record's raw bytes are hashed into the accumulator: H_i = SHA256(H_{i-1} || SHA256(raw_i)).
func BuildChain(records [][]byte) string {
	h := NewIncrementalChainHasher()
	for _, raw := range records {
		h.Append(raw)
	}
	return h.Digest()
}

// Append updates the cumulative hash in O(len(record)). After each append, the
// returned digest is the chain state including the new record. Callers must not
// modify records after passing them here.
func (h *IncrementalChainHasher) Append(raw []byte) string {
	// Hash the new record content.
	recHash := sha256.Sum256(raw)
	// Combine previous hash + record hash and compute new H.
	data := make([]byte, 0, 64)
	data = append(data, h.hash[:]...)
	data = append(data, recHash[:]...)
	h.hash = sha256.Sum256(data)
	h.recordCount++
	return h.Digest()
}

// Len returns the number of appended records.
func (h *IncrementalChainHasher) Len() int { return h.recordCount }

// Reset clears the hasher state to initial values.
func (h *IncrementalChainHasher) Reset() { h.hash = [32]byte{}; h.recordCount = 0 }

// Verify computes the chain hash naively (O(n) recomputation) and compares it to
// the stored digest. If passedRecords has fewer items than h.Len(), verification fails.
func (h *IncrementalChainHasher) Verify(passRecords [][]byte) bool {
	if len(passRecords) != h.recordCount {
		return false
	}
	exp := NewIncrementalChainHasher()
	for _, raw := range passRecords {
		exp.Append(raw)
	}
	return exp.Digest() == h.Digest()
}

// RecordBytes marshals an interface to JSON and returns the raw bytes for hashing.
func RecordBytes(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
