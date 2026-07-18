// Package evidence implements the Verifiable Control Plane: a signed,
// hash-chained, independently verifiable ledger of consequential control-plane
// actions. It is the platform's answer to "did this action really run right
// now, or was it degraded/simulated?".
//
// Where mature platforms are structurally "trust us", this ledger is "prove it":
// every recorded action produces a receipt that states which real-vs-simulated
// backend executed it (sourced from pkg/capability), hashes its input and
// output, is chained to the previous receipt (tamper-evident), and is signed
// with Ed25519. Any third party can verify the chain and signatures offline
// against the published public key — see Verifier and cmd/cafctl verify.
//
// Design principle mirrors the rest of CloudAI Fusion: the cryptography is
// REAL (crypto/ed25519 + crypto/sha256, no simulation), while external anchors
// (Rekor) and persistence degrade to honestly-reported fallbacks outside
// production rather than pretending to be real.
package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// GenesisPrevHash is the PrevHash of the first record in a chain. Using a fixed,
// non-empty sentinel (rather than "") lets a verifier distinguish a genuine
// chain head from a truncated/forged one.
const GenesisPrevHash = "genesis"

// BackendFact is a per-action snapshot of one subsystem's real-vs-simulated
// backing at the moment the action executed. It is the runtime, per-action
// upgrade of the static pkg/capability registry: instead of only knowing "the
// cache is simulated" at boot, each receipt records "this specific bind ran on
// a real k8s backend".
type BackendFact struct {
	Component string `json:"component"` // e.g. "scheduler.nodes", "cache", "finops.pricing"
	Mode      string `json:"mode"`      // real | simulated | disabled
	Driver    string `json:"driver"`    // e.g. "k8s", "memory", "static-table"
	Detail    string `json:"detail,omitempty"`
}

// Evidence is a single tamper-evident, signed receipt for one control-plane
// action. Hash is computed over every content field (everything except Hash,
// Signature, KeyID and LogEntry); Signature is Ed25519(Hash). PrevHash links to
// the preceding record's Hash to form the chain.
type Evidence struct {
	ID         string          `json:"id"`
	Seq        uint64          `json:"seq"`       // monotonic, starts at 1
	PrevHash   string          `json:"prev_hash"` // Hash of Seq-1 (GenesisPrevHash for Seq 1)
	Timestamp  time.Time       `json:"timestamp"`
	Actor      string          `json:"actor"`   // user/service identity that caused the action
	Action     string          `json:"action"`  // e.g. "schedule.bind", "finops.reclaim"
	Subject    string          `json:"subject"` // resource id the action acted upon
	RunMode    string          `json:"run_mode"`
	Backends   []BackendFact   `json:"backends"`
	InputHash  string          `json:"input_hash"`  // sha256(canonical(input))
	OutputHash string          `json:"output_hash"` // sha256(canonical(output))
	Payload    json.RawMessage `json:"payload,omitempty"`
	Hash       string          `json:"hash"`      // sha256(content) — the signed leaf
	Signature  string          `json:"signature"` // base64 Ed25519 over Hash bytes
	KeyID      string          `json:"key_id"`    // identifies the signing public key
	LogEntry   *TransparencyRef `json:"log_entry,omitempty"`
}

// TransparencyRef references an external transparency-log inclusion (e.g. Rekor).
// When no real log is configured the Anchorer records Backend="simulated" so the
// receipt never pretends to be externally anchored.
type TransparencyRef struct {
	Backend    string `json:"backend"`               // "rekor" | "simulated"
	LogURL     string `json:"log_url,omitempty"`     // transparency log base URL
	LogIndex   int64  `json:"log_index,omitempty"`   // entry index in the log
	EntryUUID  string `json:"entry_uuid,omitempty"`  // Rekor entry UUID
	InclusionProof string `json:"inclusion_proof,omitempty"` // raw proof JSON when available
	Proof          *RekorProof `json:"proof,omitempty"`     // structured, offline-verifiable proof
	Detail         string      `json:"detail,omitempty"`    // e.g. why anchoring was simulated
	IntegratedAt   time.Time   `json:"integrated_at,omitempty"`
}

// RekorProof is a structured, offline-verifiable Merkle inclusion proof for a
// Rekor log entry. Verifying it (RFC 6962 path recompute) proves the receipt is
// included in a tree with RootHash; establishing full trust additionally requires
// pinning Rekor's own log key to check the signed Checkpoint.
type RekorProof struct {
	LogIndex   int64    `json:"log_index"`  // leaf index within the tree
	TreeSize   int64    `json:"tree_size"`
	RootHash   string   `json:"root_hash"`  // hex Merkle root at TreeSize
	LeafHash   string   `json:"leaf_hash"`  // hex RFC 6962 leaf hash of the entry body
	Hashes     []string `json:"hashes"`     // hex audit path (leaf -> root)
	Checkpoint string   `json:"checkpoint,omitempty"` // Rekor signed note (verified with Rekor's key)
}

// content returns the canonical, deterministic representation over which Hash is
// computed. Field order is fixed; time is formatted as RFC3339Nano UTC so the
// hash is stable across (un)marshal round-trips. Hash/Signature/KeyID/LogEntry
// are intentionally excluded — they are derived from or attached after Hash.
func (e *Evidence) content() ([]byte, error) {
	core := struct {
		ID         string          `json:"id"`
		Seq        uint64          `json:"seq"`
		PrevHash   string          `json:"prev_hash"`
		Timestamp  string          `json:"timestamp"`
		Actor      string          `json:"actor"`
		Action     string          `json:"action"`
		Subject    string          `json:"subject"`
		RunMode    string          `json:"run_mode"`
		Backends   []BackendFact   `json:"backends"`
		InputHash  string          `json:"input_hash"`
		OutputHash string          `json:"output_hash"`
		Payload    json.RawMessage `json:"payload,omitempty"`
	}{
		ID:         e.ID,
		Seq:        e.Seq,
		PrevHash:   e.PrevHash,
		Timestamp:  e.Timestamp.UTC().Format(time.RFC3339Nano),
		Actor:      e.Actor,
		Action:     e.Action,
		Subject:    e.Subject,
		RunMode:    e.RunMode,
		Backends:   e.Backends,
		InputHash:  e.InputHash,
		OutputHash: e.OutputHash,
		Payload:    e.Payload,
	}
	return marshalCanonical(core)
}

// marshalCanonical produces the platform's canonical JSON encoding used for all
// hashing: compact, with object keys sorted (Go marshals struct fields in order
// and map keys sorted), and — crucially — WITHOUT Go's default HTML escaping of
// <, > and &. Standard JSON encoders in other languages do not HTML-escape, so
// disabling it here makes the hashed bytes byte-for-byte reproducible by an
// independent verifier written in any language.
func marshalCanonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// json.Encoder.Encode appends a trailing newline; trim it so the canonical
	// form is exactly the value's encoding with no framing.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// ComputeHash returns the hex-encoded sha256 over the record's canonical content.
// It is pure (no mutation) so both the Ledger and the Verifier can call it and
// must agree byte-for-byte.
func (e *Evidence) ComputeHash() (string, error) {
	b, err := e.content()
	if err != nil {
		return "", fmt.Errorf("evidence: marshal content: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// HashAny returns the hex-encoded sha256 over the canonical JSON of v. nil marshals
// to the JSON literal "null", yielding a stable hash for "no input/output".
func HashAny(v any) (string, error) {
	b, err := marshalCanonical(v)
	if err != nil {
		return "", fmt.Errorf("evidence: marshal value: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Filter narrows a ledger query.
type Filter struct {
	Action  string
	Subject string
	Actor   string
	Limit   int
}
