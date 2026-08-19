package gitops

// drift_audit.go materializes the Module 39 differentiator: a cryptographically
// chained, append-only audit trail of configuration-drift events.
//
// ArgoCD / Flux both DETECT drift and surface it in their UI / API, but the
// record of a drift event is an ordinary log line (or an in-cluster status
// field) that a privileged actor can edit or delete without a trace. This trail
// binds every drift observation to an Ed25519-signed, hash-chained receipt, so
// three distinct tampering vectors are all detectable OFFLINE with only the
// public key:
//
//  1. editing a stored drift event body      -> event/hash mismatch
//  2. forging or altering a receipt field     -> signature verification fails
//  3. deleting or reordering trail entries     -> receipt chain linkage breaks
//
// It is built on pkg/evidence's Receipt + ReceiptBuilder + VerifyChainOfReceipts
// (read-only dependency); no new crypto is invented here.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// DriftEvent is one recorded configuration-drift observation. It captures the
// desired-vs-live divergence for an application at a point in time. The exact
// diff (the []DriftDetail) is produced upstream by the DriftScanner backend
// (ArgoCD / Flux / kubectl diff); this type only records and attests it.
type DriftEvent struct {
	Application    string        `json:"application"`
	Namespace      string        `json:"namespace"`
	Engine         EngineType    `json:"engine"`
	DesiredSHA     string        `json:"desired_sha"`
	LiveSHA        string        `json:"live_sha"`
	Drifted        bool          `json:"drifted"`
	Drifts         []DriftDetail `json:"drifts,omitempty"`
	DetectedAt     time.Time     `json:"detected_at"`
}

// DriftAuditEntry pairs a drift event with the signed receipt that commits to it.
type DriftAuditEntry struct {
	Event   DriftEvent        `json:"event"`
	Receipt *evidence.Receipt `json:"receipt"`
}

// DriftAuditTrail is an append-only, cryptographically chained log of drift
// events. Every entry is Ed25519-signed and hash-chained to its predecessor,
// making the trail unforgeable and offline-verifiable — the Module 39 moat over
// plaintext GitOps logs.
type DriftAuditTrail struct {
	mu      sync.Mutex
	builder *evidence.ReceiptBuilder
	pub     ed25519.PublicKey
	entries []*DriftAuditEntry
}

// NewDriftAuditTrail creates a trail backed by a fresh Ed25519 key.
func NewDriftAuditTrail() *DriftAuditTrail {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &DriftAuditTrail{
		builder: evidence.NewReceiptBuilder("gitops.drift-audit", priv),
		pub:     pub,
	}
}

// PublicKey returns the verifier key required to check the trail offline. A
// third party needs nothing else — no server, no database — to audit it.
func (t *DriftAuditTrail) PublicKey() ed25519.PublicKey { return t.pub }

// Record appends a signed, chained drift event to the trail. The receipt's
// OutputHash is SHA-256(event JSON), so a later edit of the stored event is
// detectable by re-hashing it during Verify.
func (t *DriftAuditTrail) Record(ev DriftEvent) (*DriftAuditEntry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.DetectedAt.IsZero() {
		ev.DetectedAt = time.Now().UTC()
	}
	receipt, err := t.builder.Build("gitops.drift", ev, ev)
	if err != nil {
		return nil, err
	}
	entry := &DriftAuditEntry{Event: ev, Receipt: receipt}
	t.entries = append(t.entries, entry)
	return entry, nil
}

// Entries returns a snapshot copy of the audit trail in insertion order.
func (t *DriftAuditTrail) Entries() []*DriftAuditEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*DriftAuditEntry, len(t.entries))
	copy(out, t.entries)
	return out
}

// eventHash recomputes the SHA-256 that a receipt committed to for an event.
// It must mirror ReceiptBuilder.Build's output hashing exactly.
func eventHash(ev DriftEvent) ([32]byte, error) {
	b, err := json.Marshal(ev)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// Verify checks the entire trail offline and returns the index of the first
// tampered entry (or -1 when intact) plus an explanatory error. It enforces,
// per entry: (1) a valid Ed25519 signature, and (2) that the stored event still
// hashes to the value the signature committed to; then (3) that the receipt
// chain linkage is unbroken across the whole trail.
func VerifyDriftAuditEntries(entries []*DriftAuditEntry, pub ed25519.PublicKey) (int, error) {
	receipts := make([]*evidence.Receipt, 0, len(entries))
	for i, e := range entries {
		if e == nil || e.Receipt == nil {
			return i, fmt.Errorf("gitops: audit entry %d has no receipt", i)
		}
		// The signer identity must be the expected trail key.
		if !e.Receipt.SignerPublicKey.Equal(pub) {
			return i, fmt.Errorf("gitops: entry %d signed by an unexpected key", i)
		}
		// 1. Signature: catches mutation of any signed field (hashes, timestamp,
		//    operation, prev-id).
		if !e.Receipt.Verify() {
			return i, fmt.Errorf("gitops: receipt signature invalid at entry %d (tampered)", i)
		}
		// 2. Content binding: re-hash the stored event and compare to the hash
		//    the signature committed to. Catches edits to the drift body itself.
		h, err := eventHash(e.Event)
		if err != nil {
			return i, err
		}
		if h != e.Receipt.OutputHash {
			return i, fmt.Errorf("gitops: event/hash mismatch at entry %d (drift record tampered)", i)
		}
		receipts = append(receipts, e.Receipt)
	}
	// 3. Chain linkage: catches deletion / reordering of entries.
	if err := evidence.VerifyChainOfReceipts(receipts); err != nil {
		return -1, err
	}
	return -1, nil
}

// Verify is the method form of VerifyDriftAuditEntries over this trail's own
// entries and public key.
func (t *DriftAuditTrail) Verify() (int, error) {
	return VerifyDriftAuditEntries(t.Entries(), t.pub)
}
