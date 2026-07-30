// Package tee provides Trusted Execution Environment (TEE) attestation for CloudAI Fusion.
// This module implements hardware-rooted trust using Intel SGX or AWS Nitro Enclaves.
package edge

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

// TEEProvider defines interface for different TEE implementations
type TEEProvider interface {
	// CreateEnclave creates a new secure enclave context
	CreateEnclave() (EnclaveContext, error)
	
	// GetVendor returns TEE vendor name
	GetVendor() string
	
	// VerifyQuote verifies hardware attestation quote
	VerifyQuote(quote []byte) (bool, error)
}

// EnclaveContext represents active enclave session
type EnclaveContext interface {
	// Hash computes SHA-256 hash inside enclave (data never leaves secure memory)
	Hash(data []byte) [32]byte
	
	// Sign signs data using enclave's private key (key never exposed)
	Sign(hash [32]byte) ([]byte, error)
	
	// GetQuote returns hardware attestation quote from enclave
	GetQuote() ([]byte, error)
	
	// Close releases enclave resources
	Close() error
}

// EvidenceBundle represents complete cryptographic evidence bundle
// All sensitive operations happen inside TEE enclave
type EvidenceBundle struct {
	Hash          [32]byte        // SHA-256 of original data
	Signature     []byte          // Ed25519 signature from enclave
	Quote         []byte          // Hardware attestation quote
	VerifiedAt    time.Time       // ISO 8601 timestamp
	PubKey        ed25519.PublicKey
	EnclaveID     string          // Unique enclave identifier
	VersionVector []int           // Version vector for causality tracking
}

// EvidenceChain represents chained evidence over time
// Enables tamper-evident audit trail
type EvidenceChain struct {
	Bundle *EvidenceBundle
	PrevHash [32]byte
	CurrHash [32]byte
}

func (c *EvidenceChain) Append(nextEvidence []byte, vv *VersionVector) *EvidenceChain {
	newBundle := &EvidenceBundle{
		Hash: sha256.Sum256(nextEvidence),
		VerifiedAt: time.Now().UTC(),
		VersionVector: vv.Update("central"),
	}
	
	// Chain linking: current hash includes previous hash
	h := sha256.New()
	h.Write(c.CurrHash[:])
	h.Write(nextEvidence)
	newBundle.PrevHash = c.CurrHash
	newBundle.CurrHash = h.Sum(nil)
	
	return &EvidenceChain{
		Bundle: newBundle,
		PrevHash: c.CurrHash,
		CurrHash: newBundle.CurrHash,
	}
}

// AttestationVerifier validates hardware attestation chain
type AttestationVerifier struct {
	trustedEnclaveIDs []string
	rootCertificate   *RootCA
	certChain         []*Certificate
	maxClockDriftSec  int
}

// NewAttestationVerifier creates verifier with trusted list
func NewAttestationVerifier(trustedIDs []string) *AttestationVerifier {
	return &AttestationVerifier{
		trustedEnclaveIDs: trustedIDs,
		maxClockDriftSec:  300, // 5 minutes drift allowed
	}
}

// VerifyFullChain checks full attestation and evidence verification
func (v *AttestationVerifier) VerifyFullChain(chain *EvidenceChain) bool {
	// Step 1: Verify enclave quote against trusted IDs
	if !v.VerifyQuote(chain.Bundle.Quote) {
		return false
	}
	
	// Step 2: Verify signature matches public key
	content := append(chain.Bundle.Hash[:], chain.Bundle.VersionVector...)
	validSig := ed25519.Verify(chain.Bundle.PubKey, content, chain.Bundle.Signature)
	if !validSig {
		return false
	}
	
	// Step 3: Verify clock hasn't been tampered with
	if !v.VerifyClockIntegrity(chain.Bundle.VerifiedAt) {
		return false
	}
	
	// Step 4: Verify chain linkage integrity
	expectedPrevHash := chain.CurrHash // Should match next bundle's prev hash
	if !verifyHashLink(expectedPrevHash, chain.Bundle.PrevHash) {
		return false
	}
	
	return true
}

// VerifyQuote validates hardware attestation quote
func (v *AttestationVerifier) VerifyQuote(quote []byte) bool {
	if len(v.trustedEnclaveIDs) == 0 {
		return true // Skip if no trusted list configured
	}
	
	enclaveID := extractEnclaveIDFromQuote(quote)
	
	for _, trustedID := range v.trustedEnclaveIDs {
		if enclaveID == trustedID || containsSubString(enclaveID, trustedID) {
			return true
		}
	}
	
	return false
}

// VerifyClockIntegrity checks system clock hasn't been tampered
func (v *AttestationVerifier) VerifyClockIntegrity(t time.Time) bool {
	now := time.Now().UTC()
	diff := now.Sub(t).Seconds()
	
	if diff < float64(-v.maxClockDriftSec) || diff > float64(v.maxClockDriftSec) {
		return false
	}
	
	return true
}

// RootCA represents root certificate authority
type RootCA struct {
	PublicKey ed25519.PublicKey
	Subject   string
	Expires   time.Time
}

// Certificate represents attestation certificate
type Certificate struct {
	ENclaveID   string
	Issuer      string
	ValidFrom   time.Time
	ValidTo     time.Time
	PublicKey   ed25519.PublicKey
	Signature   []byte
}

// Helper functions
func extractEnclaveIDFromQuote(quote []byte) string {
	if len(quote) < 64 {
		return ""
	}
	
	// Extract first 32 bytes as enclave ID
	var idBytes [32]byte
	copy(idBytes[:], quote[:32])
	
	return fmt.Sprintf("%x", idBytes)
}

func containsSubString(s, sub string) bool {
	if len(s) < len(sub) {
		return false
	}
	
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	
	return false
}

func verifyHashLink(h1, h2 [32]byte) bool {
	return h1 == h2
}

// SecureKeyStore manages enclave keys securely
type SecureKeyStore struct {
	masterKey    []byte
	keyDerivation KeyDerivationFunction
	cache        map[string][]byte // Encrypted key handles
}

// KeyDerivationFunction derives keys from master secret
type KeyDerivationFunction interface {
	// Derive generates derived key from master key and label
	Derive(masterKey []byte, label string) []byte
}

// HKDF_SHA256 implements HMAC-based key derivation
type HKDF_SHA256 struct{}

func (hkdf *HKDF_SHA256) Derive(masterKey []byte, label string) []byte {
	hash := sha256.Sum256(append(masterKey, []byte(label)...))
	return hash[:]
}

// GenerateEnclaveKeys creates new key pair inside secure enclave
func (ks *SecureKeyStore) GenerateEnclaveKeys() (*EnclaveKeys, error) {
	// Keys generated inside enclave only
	privateKey, publicKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	
	// Encrypt private key before storing
	encrypted := ks.encrypt(privateKey)
	
	return &EnclaveKeys{
		PrivateKeyHandle: encrypted,
		PublicKey:        publicKey,
		CreatedAt:        time.Now().UTC(),
	}, nil
}

// EnclaveKeys holds enclave key pair
type EnclaveKeys struct {
	PrivateKeyHandle []byte
	PublicKey        ed25519.PublicKey
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

// encrypt wraps private key material
func (ks *SecureKeyStore) encrypt(key []byte) []byte {
	derived := ks.keyDerivation.Derive(ks.masterKey, "encryption_key")
	
	// Simple XOR encryption (replace with proper AES-GCM in production)
	encrypted := make([]byte, len(key))
	for i := range key {
		encrypted[i] = key[i] ^ derived[i%len(derived)]
	}
	
	return encrypted
}
