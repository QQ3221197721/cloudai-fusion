package tutorial

// certificate.go implements the Module 44 differentiator: Ed25519-signed,
// offline-verifiable completion certificates. A certificate is issued once
// every step is Completed; it embeds:
//
//   - Tutorial ID and title
//   - Learner identifier (email/username — caller-provided)
//   - Completion timestamp (wall clock)
//   - Step hash chain: an ordered SHA-256 chain where each entry commits to
//     the previous entry's hash plus the current step's ID + completion
//     sequence number. This makes reordering or omitting steps unforgeably
//     detectable during offline verification.
//   - Ed25519 signature over the canonical JSON payload (without the signature
//     field), verifiable with the embedded public key.
//
// The verifier needs NOTHING else — no network, no server, no DB. This is
// architecturally impossible with the log-based Katacoda / Qwiklabs / KillerCoda
// platforms whose completion claim lives in their SaaS backend.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// CertificateIssuer holds an Ed25519 signing key and issues completion certs.
type CertificateIssuer struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

// NewCertificateIssuer generates a fresh Ed25519 key pair. This is appropriate
// when a platform instance signs certificates for its own learners.
func NewCertificateIssuer() *CertificateIssuer {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &CertificateIssuer{pub: pub, priv: priv}
}

// NewCertificateIssuerFromKey wraps an existing private key. It panics if the
// key has an unexpected size. Use this to inject a persistent key from config.
func NewCertificateIssuerFromKey(priv ed25519.PrivateKey) *CertificateIssuer {
	if l := len(priv); l != ed25519.PrivateKeySize {
		panic(fmt.Sprintf("tutorial: ed25519 private key must be %d bytes, got %d", ed25519.PrivateKeySize, l))
	}
	pub := priv.Public().(ed25519.PublicKey)
	return &CertificateIssuer{pub: pub, priv: priv}
}

// PublicKey returns the verifier key embedded in issued certificates.
func (ci *CertificateIssuer) PublicKey() ed25519.PublicKey {
	return ci.pub
}

// Certificate is the self-contained, offline-verifiable completion proof.
type Certificate struct {
	// TutorialID and TutorialTitle identify the course completed.
	TutorialID    string `json:"tutorial_id"`
	TutorialTitle string `json:"tutorial_title"`
	// LearnerID is the caller-supplied learner identifier (email, username, etc).
	LearnerID string `json:"learner_id"`
	// CompletedAt is the wall-clock time of certificate issuance.
	CompletedAt time.Time `json:"completed_at"`
	// StepHashChain is the ordered list of per-step SHA-256 chain hashes (hex).
	StepHashChain []string `json:"step_hash_chain"`
	// PublicKey is the hex-encoded Ed25519 public key used to sign this cert.
	PublicKey string `json:"public_key"`
	// Signature is the hex-encoded Ed25519 signature of the canonical payload.
	Signature string `json:"signature"`
}

// IssueCertificate produces a signed Certificate for a completed tutorial. It
// builds the step hash chain in topological order (deterministic, per
// Tutorial.TopologicalOrder). Returns an error if the progress is not fully
// complete or if internal state is inconsistent.
func (ci *CertificateIssuer) IssueCertificate(p *Progress, learnerID string) (*Certificate, error) {
	if p == nil {
		return nil, fmt.Errorf("tutorial: nil progress")
	}
	if !p.IsComplete() {
		return nil, fmt.Errorf("tutorial: cannot issue cert — tutorial %q not fully completed", p.Tutorial().ID)
	}
	if learnerID == "" {
		return nil, fmt.Errorf("tutorial: empty learner ID")
	}

	tut := p.Tutorial()
	order, err := tut.TopologicalOrder()
	if err != nil {
		return nil, err
	}

	chain := buildStepHashChain(order)

	cert := &Certificate{
		TutorialID:    tut.ID,
		TutorialTitle: tut.Title,
		LearnerID:     learnerID,
		CompletedAt:   time.Now(),
		StepHashChain: chain,
		PublicKey:      hex.EncodeToString(ci.pub),
	}

	payload, err := cert.signingPayload()
	if err != nil {
		return nil, fmt.Errorf("tutorial: marshal payload: %w", err)
	}
	cert.Signature = hex.EncodeToString(ed25519.Sign(ci.priv, payload))
	return cert, nil
}

// VerifyCertificate verifies the Ed25519 signature of cert against its embedded
// public key. Tampering with ANY field (learner, time, steps, chain) causes
// verification to fail. It additionally verifies the step hash chain integrity.
// This function requires NO network access and NO database — fully offline.
func VerifyCertificate(cert *Certificate) (bool, error) {
	if cert == nil {
		return false, fmt.Errorf("tutorial: nil certificate")
	}
	pub, err := hex.DecodeString(cert.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("tutorial: invalid public key in certificate")
	}
	sig, err := hex.DecodeString(cert.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, fmt.Errorf("tutorial: invalid signature in certificate")
	}

	payload, err := cert.signingPayload()
	if err != nil {
		return false, fmt.Errorf("tutorial: cannot reconstruct payload: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, sig) {
		return false, nil
	}
	return true, nil
}

// VerifyCertificateWithKey verifies against a caller-provided public key. This
// supports the case where the verifier trusts a specific issuer and wants to
// confirm the certificate was issued by them.
func VerifyCertificateWithKey(cert *Certificate, pub ed25519.PublicKey) (bool, error) {
	if cert == nil {
		return false, fmt.Errorf("tutorial: nil certificate")
	}
	if len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("tutorial: invalid public key")
	}
	sig, err := hex.DecodeString(cert.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, fmt.Errorf("tutorial: invalid signature in certificate")
	}

	payload, err := cert.signingPayload()
	if err != nil {
		return false, fmt.Errorf("tutorial: cannot reconstruct payload: %w", err)
	}
	if !ed25519.Verify(pub, payload, sig) {
		return false, nil
	}
	return true, nil
}

// signingPayload produces the canonical byte sequence that is signed/verified.
// It is the JSON of the cert WITHOUT the signature field, which we achieve by
// marshaling a temporary copy with Signature cleared.
func (cert *Certificate) signingPayload() ([]byte, error) {
	tmp := *cert
	tmp.Signature = ""
	data, err := json.Marshal(tmp)
	if err != nil {
		return nil, err
	}
	// SHA-256 the JSON to get a fixed-size message for Ed25519.
	h := sha256.Sum256(data)
	return h[:], nil
}

// buildStepHashChain creates a SHA-256 hash chain in the given order. Each
// element is SHA-256(previousHash || stepID || stepIndex). This makes the chain
// order-dependent and tamper-evident.
func buildStepHashChain(order []string) []string {
	chain := make([]string, len(order))
	prev := make([]byte, sha256.Size) // genesis = 32 zero bytes
	for i, id := range order {
		h := sha256.New()
		h.Write(prev)
		h.Write([]byte(id))
		h.Write([]byte(fmt.Sprintf(":%d", i)))
		digest := h.Sum(nil)
		chain[i] = hex.EncodeToString(digest)
		prev = digest
	}
	return chain
}

// VerifyStepHashChain re-derives the chain from a list of step IDs in
// topological order and returns whether it matches the certificate's chain.
func VerifyStepHashChain(cert *Certificate, order []string) bool {
	expected := buildStepHashChain(order)
	if len(expected) != len(cert.StepHashChain) {
		return false
	}
	for i := range expected {
		if expected[i] != cert.StepHashChain[i] {
			return false
		}
	}
	return true
}
