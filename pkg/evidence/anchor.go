package evidence

import (
	"context"
	"crypto/ed25519"
	"time"
)

// AnchorRequest carries everything a transparency log needs to record a
// verifiable entry for a receipt: the leaf hash (the "artifact"), the detached
// signature over it, and the public key that verifies the signature. A Rekor
// hashedrekord entry is built from exactly these three.
type AnchorRequest struct {
	LeafHex      string            // the receipt Hash (hex sha256)
	SignatureB64 string            // base64 Ed25519 signature over []byte(LeafHex)
	PublicKey    ed25519.PublicKey // verifying key
}

// Anchorer optionally anchors a receipt into an external transparency log (e.g.
// Rekor) so inclusion can be proven independently of this platform.
//
// Honesty rule: an Anchorer must ALWAYS return a TransparencyRef whose Backend
// truthfully states what happened. A real Rekor client returns Backend="rekor"
// with a log index and inclusion proof; when no log is configured the
// SimulatedAnchorer returns Backend="simulated" so a receipt never pretends to
// be externally anchored. In production, capability.Enforce() blocks boot if the
// anchor is only simulated (see Ledger wiring).
type Anchorer interface {
	// Anchor submits the receipt and returns a truthful reference.
	Anchor(ctx context.Context, req AnchorRequest) (*TransparencyRef, error)
	// Real reports whether this anchorer talks to a real transparency log.
	Real() bool
	// Backend names the anchor backend ("rekor" | "simulated").
	Backend() string
}

// SimulatedAnchorer is the honest no-op anchor used when no transparency log is
// configured. It records that anchoring was simulated rather than fabricating a
// log index or proof.
type SimulatedAnchorer struct{}

// NewSimulatedAnchorer returns the honest no-op anchorer.
func NewSimulatedAnchorer() *SimulatedAnchorer { return &SimulatedAnchorer{} }

// Anchor returns a ref that plainly states anchoring was simulated.
func (a *SimulatedAnchorer) Anchor(_ context.Context, _ AnchorRequest) (*TransparencyRef, error) {
	return &TransparencyRef{Backend: "simulated", IntegratedAt: time.Now().UTC()}, nil
}

// Real always reports false: this is the fallback, not a real log.
func (a *SimulatedAnchorer) Real() bool { return false }

// Backend identifies this anchorer as simulated.
func (a *SimulatedAnchorer) Backend() string { return "simulated" }
