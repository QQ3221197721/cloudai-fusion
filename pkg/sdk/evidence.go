package sdk

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// EvidenceClient provides access to the Evidence Chain API — the tamper-evident,
// hash-chained ledger that records verifiable control-plane events.
//
// Obtain it from a Client via the Evidence field; do not construct it directly.
type EvidenceClient struct {
	client *Client
}

// VerifyResult reports the outcome of verifying an evidence chain.
type VerifyResult struct {
	// Valid is true when the chain's hash links and signatures are intact.
	Valid bool `json:"valid"`
	// EntryCount is the number of entries inspected during verification.
	EntryCount int `json:"entryCount"`
	// Namespace is the namespace that was verified.
	Namespace string `json:"namespace"`
	// RootHash is the current Merkle root of the chain, in hex.
	RootHash string `json:"rootHash,omitempty"`
	// BrokenAt, when non-empty, identifies the first entry ID where the chain
	// integrity check failed.
	BrokenAt string `json:"brokenAt,omitempty"`
}

// AttestResult describes an attestation appended to the evidence chain.
type AttestResult struct {
	// ID is the unique identifier assigned to the new attestation entry.
	ID string `json:"id"`
	// Hash is the entry's content hash linking it into the chain.
	Hash string `json:"hash"`
	// Signature is the detached signature over the entry.
	Signature string `json:"signature"`
	// Timestamp is when the attestation was recorded.
	Timestamp time.Time `json:"timestamp"`
}

// EvidenceEntry is a single record in the evidence chain.
type EvidenceEntry struct {
	// ID uniquely identifies the entry.
	ID string `json:"id"`
	// Namespace is the logical scope the entry belongs to.
	Namespace string `json:"namespace"`
	// Statement is the attested payload for the entry.
	Statement string `json:"statement"`
	// Hash is the entry's content hash.
	Hash string `json:"hash"`
	// PrevHash links this entry to its predecessor in the chain.
	PrevHash string `json:"prevHash"`
	// Timestamp is when the entry was recorded.
	Timestamp time.Time `json:"timestamp"`
}

// Verify checks the integrity of an evidence chain for the given namespace.
//
// A successful call means the request was answered; inspect VerifyResult.Valid
// to learn whether the chain itself passed verification.
func (e *EvidenceClient) Verify(ctx context.Context, namespace string) (*VerifyResult, error) {
	var out VerifyResult
	path := "/api/v1/evidence/verify?namespace=" + url.QueryEscape(namespace)
	if err := e.client.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Attest adds a signed attestation to the chain and returns the resulting entry.
func (e *EvidenceClient) Attest(ctx context.Context, statement string) (*AttestResult, error) {
	body := map[string]string{"statement": statement}
	var out AttestResult
	if err := e.client.do(ctx, http.MethodPost, "/api/v1/evidence/attest", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns recent evidence entries, honoring pagination and namespace
// filtering from opts. A nil opts requests the server defaults.
func (e *EvidenceClient) List(ctx context.Context, opts *ListOptions) ([]*EvidenceEntry, error) {
	path := "/api/v1/evidence"
	if q := opts.query(); len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out []*EvidenceEntry
	if err := e.client.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
