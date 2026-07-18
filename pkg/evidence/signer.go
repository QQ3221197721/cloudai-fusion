package evidence

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
)

// Signer produces detached signatures over a receipt's leaf hash and exposes the
// verifying public key. The default implementation is real Ed25519.
type Signer interface {
	// KeyID identifies the public key (first 16 hex chars of sha256(pubkey)).
	KeyID() string
	// PublicKey returns the raw Ed25519 public key.
	PublicKey() ed25519.PublicKey
	// Sign returns a base64 signature over the given leaf-hash bytes.
	Sign(leaf []byte) (string, error)
}

// Ed25519Signer is a real Ed25519 signer. There is no simulated signer: the
// cryptography is always genuine. What may be simulated (and honestly reported)
// is where the KEY comes from — an ephemeral in-memory key outside production
// vs. an operator-provisioned key in production.
type Ed25519Signer struct {
	priv      ed25519.PrivateKey
	pub       ed25519.PublicKey
	keyID     string
	ephemeral bool // true when the key was generated in-process (dev/sim only)
}

// KeyIDFor derives the canonical KeyID for a public key.
func KeyIDFor(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])[:16]
}

// GenerateEphemeralSigner creates a fresh in-process Ed25519 key. It is intended
// for simulation/degraded modes ONLY; callers must report it to pkg/capability
// as a simulated backend so a production boot refuses it.
func GenerateEphemeralSigner() (*Ed25519Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("evidence: generate ephemeral key: %w", err)
	}
	return &Ed25519Signer{priv: priv, pub: pub, keyID: KeyIDFor(pub), ephemeral: true}, nil
}

// NewSignerFromSeed builds a deterministic signer from a 32-byte seed. Useful for
// operator-provisioned keys and for reproducible tests/golden files.
func NewSignerFromSeed(seed []byte) (*Ed25519Signer, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("evidence: seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return &Ed25519Signer{priv: priv, pub: pub, keyID: KeyIDFor(pub)}, nil
}

// NewSignerFromPEM loads an Ed25519 private key from a PKCS#8 PEM block. This is
// the production path: the key is provisioned out-of-band (secret/HSM export).
func NewSignerFromPEM(pemBytes []byte) (*Ed25519Signer, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("evidence: no PEM block found in signing key")
	}
	key, err := parsePKCS8Ed25519(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub := key.Public().(ed25519.PublicKey)
	return &Ed25519Signer{priv: key, pub: pub, keyID: KeyIDFor(pub)}, nil
}

// KeyID returns the signer's key identifier.
func (s *Ed25519Signer) KeyID() string { return s.keyID }

// PublicKey returns the Ed25519 public key used to verify this signer's output.
func (s *Ed25519Signer) PublicKey() ed25519.PublicKey { return s.pub }

// Ephemeral reports whether this key was generated in-process (dev/sim only).
func (s *Ed25519Signer) Ephemeral() bool { return s.ephemeral }

// Sign returns a base64-encoded Ed25519 signature over leaf.
func (s *Ed25519Signer) Sign(leaf []byte) (string, error) {
	if len(s.priv) == 0 {
		return "", errors.New("evidence: signer has no private key")
	}
	sig := ed25519.Sign(s.priv, leaf)
	return base64.StdEncoding.EncodeToString(sig), nil
}

// PublicKeyPEM returns the signer's public key as a PKIX PEM block, suitable for
// publishing so third parties can verify the ledger offline.
func (s *Ed25519Signer) PublicKeyPEM() ([]byte, error) {
	return MarshalPublicKeyPEM(s.pub)
}

// VerifyLeaf checks a base64 signature over leaf against pub. It is the single
// verification primitive shared by the API and the offline Verifier.
func VerifyLeaf(pub ed25519.PublicKey, leaf []byte, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("evidence: decode signature: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("evidence: bad public key size %d", len(pub))
	}
	if !ed25519.Verify(pub, leaf, sig) {
		return errors.New("evidence: signature verification failed")
	}
	return nil
}
