package config

// sealed.go preserves and extends the existing Ed25519 config moat. The existing
// EvidenceConfigEngine (evidence_config.go) seals each individual key change into
// a signed receipt. This file adds a complementary primitive for the hot-reload
// path: a SealedBundle seals a WHOLE snapshot version, so a node can prove
// offline that "version V of the config was signed by node key K", and any peer
// or auditor can verify it without contacting us.
//
// Both barriers use real crypto/ed25519 — no placeholders. A SealedBundle is
// self-contained: it carries the public key, the canonical payload it signed,
// and the signature, so verification needs nothing but the bundle itself.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// SealedBundle is an offline-verifiable Ed25519 seal over one config version.
type SealedBundle struct {
	Version   string            `json:"version"`    // Snapshot.Version this seals
	Payload   []byte            `json:"payload"`    // canonical JSON that was signed
	PublicKey ed25519.PublicKey `json:"public_key"` // signer identity
	Signature []byte            `json:"signature"`  // Ed25519 signature over Payload
}

// BundleSigner holds a node's Ed25519 signing key and produces SealedBundles.
// Keep one per node; the public key identifies the origin of every sealed
// version it emits.
type BundleSigner struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// NewBundleSigner generates a fresh Ed25519 key pair for a node.
func NewBundleSigner() (*BundleSigner, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &BundleSigner{priv: priv, pub: pub}, nil
}

// NewBundleSignerFromSeed builds a signer from a 32-byte seed. Useful when the
// signing key comes from EvidenceKeyPath / a KMS-derived seed rather than being
// generated in-process.
func NewBundleSignerFromSeed(seed []byte) (*BundleSigner, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("config: ed25519 seed must be 32 bytes")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return &BundleSigner{priv: priv, pub: priv.Public().(ed25519.PublicKey)}, nil
}

// PublicKey returns the signer's public key (the node's config-origin identity).
func (s *BundleSigner) PublicKey() ed25519.PublicKey { return s.pub }

// Seal produces an offline-verifiable bundle binding version to the canonical
// JSON of values. The payload is canonicalised (sorted keys) so two nodes that
// converge on the same config produce byte-identical, mutually-verifiable seals.
func (s *BundleSigner) Seal(version string, values map[string]string) (*SealedBundle, error) {
	payload := canonicalPayload(version, values)
	sig := ed25519.Sign(s.priv, payload)
	return &SealedBundle{
		Version:   version,
		Payload:   payload,
		PublicKey: s.pub,
		Signature: sig,
	}, nil
}

// Verify checks the seal is internally consistent and cryptographically valid:
//  1. sizes are correct,
//  2. the signature verifies under the embedded public key,
//  3. the embedded version matches the SHA-256 recomputed from the payload's
//     values (so a mismatched Version cannot be smuggled in).
// Returns nil when the bundle is authentic and untampered.
func (b *SealedBundle) Verify() error {
	if len(b.PublicKey) != ed25519.PublicKeySize {
		return errors.New("config: sealed bundle has wrong public-key size")
	}
	if len(b.Signature) != ed25519.SignatureSize {
		return errors.New("config: sealed bundle has wrong signature size")
	}
	if !ed25519.Verify(b.PublicKey, b.Payload, b.Signature) {
		return errors.New("config: sealed bundle signature verification failed")
	}
	// Re-derive the version from the payload to reject a spoofed Version field.
	version, values, err := decodePayload(b.Payload)
	if err != nil {
		return err
	}
	if version != b.Version {
		return errors.New("config: sealed bundle version does not match payload")
	}
	if ComputeVersion(values) != version {
		return errors.New("config: sealed bundle version is not the digest of its values")
	}
	return nil
}

// sealedPayload is the canonical wire form that gets signed and hashed.
type sealedPayload struct {
	Version string            `json:"version"`
	Values  map[string]string `json:"values"`
}

// canonicalPayload marshals (version, values) deterministically. encoding/json
// already sorts map keys, so the output is stable across nodes.
func canonicalPayload(version string, values map[string]string) []byte {
	// Copy into a fresh map so the caller's map cannot race with marshalling.
	cp := make(map[string]string, len(values))
	for k, v := range values {
		cp[k] = v
	}
	b, _ := json.Marshal(sealedPayload{Version: version, Values: cp})
	return b
}

func decodePayload(b []byte) (string, map[string]string, error) {
	var p sealedPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return "", nil, errors.New("config: sealed bundle payload is not valid JSON")
	}
	return p.Version, p.Values, nil
}

// bundleDigest returns a short hex digest of a bundle's payload, handy for logs
// and metrics without leaking values.
func bundleDigest(b *SealedBundle) string {
	sum := sha256.Sum256(b.Payload)
	return hex.EncodeToString(sum[:8])
}

// sortedKeys is a small shared helper (kept here so both crdt and sealed paths
// can produce stable orderings without duplicating logic).
func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
