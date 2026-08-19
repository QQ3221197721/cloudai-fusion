package evidence

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// receipt.go defines the universal, self-contained Receipt returned by every
// module in CloudAI Fusion. Unlike the ledger/Merkle machinery in this package
// (which builds a durable, anchored chain), a Receipt is a lightweight,
// individually verifiable attestation: a module signs (module, operation, input
// hash, output hash, timestamp) with a real Ed25519 key. The result is an
// unforgeable, offline-verifiable proof that a specific operation happened —
// competitors can only produce logs, we produce proofs.

// Receipt is a cryptographically signed proof that an operation occurred.
// Every module in CloudAI Fusion returns Receipts for its core operations.
// This creates an unforgeable audit trail — competitors only have logs, we have proofs.
type Receipt struct {
	// ID is a unique identifier for this receipt.
	ID string `json:"id"`

	// Module identifies which module produced this receipt.
	Module string `json:"module"`

	// Operation is what operation was performed.
	Operation string `json:"operation"`

	// Timestamp records when the operation occurred (nanosecond precision).
	Timestamp time.Time `json:"timestamp"`

	// InputHash is the SHA-256 hash of the input parameters.
	InputHash [32]byte `json:"input_hash"`

	// OutputHash is the SHA-256 hash of the output/result.
	OutputHash [32]byte `json:"output_hash"`

	// SignerPublicKey is the signing key's public key (identifies WHO made this attestation).
	SignerPublicKey ed25519.PublicKey `json:"signer_public_key"`

	// Signature is the Ed25519 signature over the deterministic signable payload.
	Signature []byte `json:"signature"`

	// PreviousReceiptID optionally references the previous receipt in the chain
	// (creates causal ordering).
	PreviousReceiptID string `json:"previous_receipt_id,omitempty"`

	// Metadata carries optional additional key/value context.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Verify checks the Ed25519 signature of this receipt.
// Returns true only if the signature is valid for the stated content.
func (r *Receipt) Verify() bool {
	if len(r.SignerPublicKey) != ed25519.PublicKeySize || len(r.Signature) != ed25519.SignatureSize {
		return false
	}
	payload := r.signablePayload()
	return ed25519.Verify(r.SignerPublicKey, payload, r.Signature)
}

// signablePayload returns the deterministic byte sequence that was signed.
// Deterministic encoding: module|operation|<timestamp_unix_nano BE u64>|input_hash|output_hash|prev_id
func (r *Receipt) signablePayload() []byte {
	prefix := []byte(r.Module + "|" + r.Operation + "|")
	// prefix + 8 (timestamp) + 32 (input) + 32 (output) + len(prev)
	data := make([]byte, 0, len(prefix)+8+sha256.Size+sha256.Size+len(r.PreviousReceiptID))
	data = append(data, prefix...)
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(r.Timestamp.UnixNano()))
	data = append(data, ts...)
	data = append(data, r.InputHash[:]...)
	data = append(data, r.OutputHash[:]...)
	data = append(data, []byte(r.PreviousReceiptID)...)
	return data
}

// ReceiptBuilder helps modules create receipts with minimal boilerplate.
// It is safe for concurrent use: the chaining state is guarded by a mutex so a
// single builder can back a concurrent server (e.g. the Gin middleware).
type ReceiptBuilder struct {
	module     string
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey

	mu     sync.Mutex
	lastID string // for chaining
}

// NewReceiptBuilder creates a builder for a specific module.
// Each module creates one at startup.
func NewReceiptBuilder(module string, privateKey ed25519.PrivateKey) *ReceiptBuilder {
	return &ReceiptBuilder{
		module:     module,
		privateKey: privateKey,
		publicKey:  privateKey.Public().(ed25519.PublicKey),
	}
}

// Build creates and signs a new receipt.
// input/output are any serializable values — their SHA-256 hashes are stored.
func (rb *ReceiptBuilder) Build(operation string, input, output interface{}) (*Receipt, error) {
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("evidence: marshal input: %w", err)
	}
	outputBytes, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("evidence: marshal output: %w", err)
	}
	return rb.BuildRaw(operation, sha256.Sum256(inputBytes), sha256.Sum256(outputBytes))
}

// BuildRaw creates and signs a receipt from already-computed input/output
// hashes. This is the low-boilerplate path used by the HTTP middleware, which
// hashes raw request/response bytes itself.
func (rb *ReceiptBuilder) BuildRaw(operation string, inputHash, outputHash [32]byte) (*Receipt, error) {
	if len(rb.privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("evidence: receipt builder has no valid private key")
	}

	rb.mu.Lock()
	prev := rb.lastID
	rb.mu.Unlock()

	receipt := &Receipt{
		ID:                generateReceiptID(),
		Module:            rb.module,
		Operation:         operation,
		Timestamp:         time.Now(),
		InputHash:         inputHash,
		OutputHash:        outputHash,
		SignerPublicKey:   rb.publicKey,
		PreviousReceiptID: prev,
		Metadata:          make(map[string]string),
	}

	// Sign the deterministic payload with a real Ed25519 key.
	receipt.Signature = ed25519.Sign(rb.privateKey, receipt.signablePayload())

	// Advance the chain head.
	rb.mu.Lock()
	rb.lastID = receipt.ID
	rb.mu.Unlock()

	return receipt, nil
}

// generateReceiptID returns a compact, collision-resistant identifier derived
// from 20 bytes of cryptographic randomness, encoded as unpadded base32.
func generateReceiptID() string {
	var buf [20]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failure is catastrophic; fall back to a timestamp-seeded
		// value so the caller still gets a unique-enough, non-empty ID.
		binary.BigEndian.PutUint64(buf[:8], uint64(time.Now().UnixNano()))
	}
	return "rcpt_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf[:])
}

// VerifyChainOfReceipts verifies both the causal ordering and every signature in
// a slice of receipts. The slice must be in chain order: each receipt's
// PreviousReceiptID must equal the ID of the receipt before it, and the first
// receipt must have no predecessor. Every signature must be valid.
func VerifyChainOfReceipts(receipts []*Receipt) error {
	for i, r := range receipts {
		if r == nil {
			return fmt.Errorf("evidence: receipt at index %d is nil", i)
		}
		if !r.Verify() {
			return fmt.Errorf("evidence: invalid signature on receipt %q (index %d)", r.ID, i)
		}
		if i == 0 {
			if r.PreviousReceiptID != "" {
				return fmt.Errorf("evidence: first receipt %q references a predecessor %q", r.ID, r.PreviousReceiptID)
			}
			continue
		}
		if r.PreviousReceiptID != receipts[i-1].ID {
			return fmt.Errorf("evidence: broken chain at index %d: receipt %q references %q but previous receipt is %q",
				i, r.ID, r.PreviousReceiptID, receipts[i-1].ID)
		}
	}
	return nil
}
