// Package messaging — evidence_envelope.go provides a zero-copy evidence
// envelope that binds message payloads to an HMAC-SHA256 digest, a nanosecond
// timestamp, and a monotonic sequence number.
//
// Wire format (all fields little-endian):
//
//	[ HMAC-SHA256 (32 B) | timestamp (8 B) | seqNo (8 B) | payload (N B) ]
//	  Total envelope = 48 + len(payload)
//
// Zero-allocation design:
//   - Seal writes directly into a caller-provided dst buffer.
//   - An internal sync.Pool of hmac.Hash instances avoids re-keying on every call.
//   - The sequence counter uses atomic increment (no mutex).
package messaging

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash"
	"sync"
	"sync/atomic"
	"time"
)

// Envelope header sizes.
const (
	hmacSize      = 32 // SHA-256 output
	timestampSize = 8  // int64 UnixNano
	seqSize       = 8  // uint64 monotonic
	headerSize    = hmacSize + timestampSize + seqSize // 48
)

// EvidenceEnvelope seals payloads with HMAC-SHA256 + timestamp + sequence.
type EvidenceEnvelope struct {
	pool sync.Pool
	seq  atomic.Uint64
	key  []byte // retained for Verify path
}

// NewEvidenceEnvelope creates an envelope sealer keyed with the given HMAC key.
func NewEvidenceEnvelope(key []byte) *EvidenceEnvelope {
	// Keep a copy of the key for verify-side pool creation.
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	e := &EvidenceEnvelope{key: keyCopy}
	e.pool.New = func() interface{} {
		return hmac.New(sha256.New, keyCopy)
	}
	// Pre-warm pool.
	for i := 0; i < 4; i++ {
		e.pool.Put(hmac.New(sha256.New, keyCopy))
	}
	return e
}

// EnvelopeSize returns the total bytes required to seal a payload of payloadLen.
func EnvelopeSize(payloadLen int) int {
	return headerSize + payloadLen
}

// Seal writes the evidence envelope into dst and returns bytes written.
// dst must be at least EnvelopeSize(len(payload)) bytes.
// Designed for 0 alloc/op when dst is pre-allocated.
func (e *EvidenceEnvelope) Seal(payload []byte, dst []byte) (int, error) {
	needed := headerSize + len(payload)
	if len(dst) < needed {
		return 0, ErrBufferTooSmall
	}

	// Write timestamp (bytes 32-39).
	ts := time.Now().UnixNano()
	binary.LittleEndian.PutUint64(dst[hmacSize:hmacSize+timestampSize], uint64(ts))

	// Write sequence number (bytes 40-47).
	seq := e.seq.Add(1)
	binary.LittleEndian.PutUint64(dst[hmacSize+timestampSize:headerSize], seq)

	// Write payload (bytes 48+).
	copy(dst[headerSize:], payload)

	// Compute HMAC over (timestamp | seqNo | payload) → write into bytes 0-31.
	mac := e.pool.Get().(hash.Hash)
	mac.Reset()
	mac.Write(dst[hmacSize:needed]) // timestamp + seq + payload
	mac.Sum(dst[:0])                // writes 32 bytes at dst[0:32]
	e.pool.Put(mac)

	return needed, nil
}

// Verify checks the envelope integrity and returns the payload slice (zero-copy
// sub-slice of envelope). Returns error on HMAC mismatch or malformed data.
func Verify(envelope []byte, key []byte) ([]byte, error) {
	if len(envelope) < headerSize {
		return nil, ErrMalformedEnvelope
	}

	// Recompute HMAC.
	mac := hmac.New(sha256.New, key)
	mac.Write(envelope[hmacSize:]) // timestamp + seq + payload
	expected := mac.Sum(nil)

	if !hmac.Equal(envelope[:hmacSize], expected) {
		return nil, ErrHMACMismatch
	}

	return envelope[headerSize:], nil
}

// VerifyWithInstance uses a pooled hasher for repeated verify calls (0 alloc
// on the hot path when the caller reuses the EvidenceEnvelope).
func (e *EvidenceEnvelope) Verify(envelope []byte) ([]byte, error) {
	if len(envelope) < headerSize {
		return nil, ErrMalformedEnvelope
	}

	mac := e.pool.Get().(hash.Hash)
	mac.Reset()
	mac.Write(envelope[hmacSize:])
	// Sum appends to a nil slice — this WILL allocate 32 bytes.
	// To keep verify zero-alloc we use a stack buffer.
	var buf [hmacSize]byte
	computed := mac.Sum(buf[:0])
	e.pool.Put(mac)

	if !hmac.Equal(envelope[:hmacSize], computed) {
		return nil, ErrHMACMismatch
	}
	return envelope[headerSize:], nil
}

// Seq returns the current monotonic sequence number.
func (e *EvidenceEnvelope) Seq() uint64 {
	return e.seq.Load()
}

// Sentinel errors.
var (
	ErrBufferTooSmall   = errors.New("messaging: dst buffer too small for envelope")
	ErrMalformedEnvelope = errors.New("messaging: envelope too short")
	ErrHMACMismatch     = errors.New("messaging: HMAC verification failed")
)
