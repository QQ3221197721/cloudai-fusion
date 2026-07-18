package evidence

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// keys.go centralizes Ed25519 key (de)serialization so the signer, the API
// public-key export, and the offline Verifier all agree on the wire format:
// PKCS#8 for private keys, PKIX (SubjectPublicKeyInfo) for public keys, both in
// standard PEM. This is what third parties consume to verify the ledger.

// parsePKCS8Ed25519 parses a PKCS#8-encoded Ed25519 private key.
func parsePKCS8Ed25519(der []byte) (ed25519.PrivateKey, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("evidence: parse PKCS#8 key: %w", err)
	}
	edKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("evidence: signing key is not Ed25519")
	}
	return edKey, nil
}

// MarshalPublicKeyPEM encodes an Ed25519 public key as a PKIX PEM block.
func MarshalPublicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("evidence: marshal public key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// ParsePublicKeyPEM decodes an Ed25519 public key from a PKIX PEM block. Used by
// the offline verifier (cmd/cafctl verify) to load the published key.
func ParsePublicKeyPEM(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("evidence: no PEM block found in public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("evidence: parse PKIX public key: %w", err)
	}
	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("evidence: public key is not Ed25519")
	}
	return edPub, nil
}
