package evidence

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Recorder is the narrow interface control-plane subsystems (scheduler, finops,
// API middleware) depend on to emit evidence. Keeping it small lets callers hold
// a NopRecorder when evidence is disabled without nil checks scattered around.
type Recorder interface {
	// Record appends a signed, chained receipt for one action and returns it.
	// A NopRecorder returns (nil, nil); callers must tolerate a nil result.
	Record(ctx context.Context, in RecordInput) (*Evidence, error)
}

// RecordInput describes one action to be receipted. Input/Output are hashed
// (never stored verbatim, so secrets stay out of the ledger); Payload is the
// domain-specific, verifiable record (e.g. a SchedulingDecision) stored as-is.
type RecordInput struct {
	Actor      string        // identity that caused the action
	Action     string        // e.g. "schedule.bind", "finops.reclaim"
	Subject    string        // resource id acted upon
	Input      any           // hashed into InputHash
	Output     any           // hashed into OutputHash
	Payload    any           // domain payload, JSON-encoded into Payload
	Backends   []BackendFact // explicit per-action backends (takes precedence)
	Components []string      // if Backends is nil, snapshot only these components
}

// CapabilitySource supplies the per-action real-vs-simulated backend snapshot and
// the active run mode. It is an interface (not a direct pkg/capability import) so
// the ledger core stays unit-testable; capability_adapter.go wires the default.
type CapabilitySource interface {
	Snapshot() []BackendFact
	RunMode() string
}

// Ledger is the append-only, hash-chained, signed evidence ledger. It serializes
// writes so Seq is monotonic and PrevHash forms an unbroken chain.
type Ledger struct {
	mu         sync.Mutex
	signerMu   sync.RWMutex
	store      Store
	signer     Signer
	keyHistory []PublicKeyEntry
	anchorer   Anchorer
	cap        CapabilitySource
}

// LedgerConfig configures a Ledger. Store and Signer are required; Anchorer
// defaults to the honest SimulatedAnchorer and Cap to an empty source.
type LedgerConfig struct {
	Store    Store
	Signer   Signer
	Anchorer Anchorer
	Cap      CapabilitySource
}

// NewLedger constructs a Ledger, filling in honest defaults.
func NewLedger(cfg LedgerConfig) (*Ledger, error) {
	if cfg.Store == nil {
		return nil, errors.New("evidence: ledger requires a Store")
	}
	if cfg.Signer == nil {
		return nil, errors.New("evidence: ledger requires a Signer")
	}
	if cfg.Anchorer == nil {
		cfg.Anchorer = NewSimulatedAnchorer()
	}
	if cfg.Cap == nil {
		cfg.Cap = emptyCapabilitySource{}
	}
	return &Ledger{
		store:      cfg.Store,
		signer:     cfg.Signer,
		keyHistory: initialKeyHistory(cfg.Signer),
		anchorer:   cfg.Anchorer,
		cap:        cfg.Cap,
	}, nil
}

// initialKeyHistory seeds the key history with the ledger's first signing key.
func initialKeyHistory(s Signer) []PublicKeyEntry {
	pemBytes, err := MarshalPublicKeyPEM(s.PublicKey())
	if err != nil {
		return nil
	}
	return []PublicKeyEntry{{KeyID: s.KeyID(), PEM: string(pemBytes)}}
}

// Record builds, hashes, signs, anchors, and durably appends a receipt. It is
// the single-record path; BatchRecord (batch_append.go) reuses the same
// prepare/append primitives to pipeline a batch. The per-record content hashing
// is factored into prepareRecordInput (pure) and the sign+chain+append critical
// section into appendPrepared, so both paths agree byte-for-byte.
func (l *Ledger) Record(ctx context.Context, in RecordInput) (*Evidence, error) {
	p, err := prepareRecordInput(in)
	if err != nil {
		return nil, err
	}
	return l.appendPrepared(ctx, p)
}

// snapshotBackends returns the capability snapshot, optionally filtered to the
// components a specific action touched.
func (l *Ledger) snapshotBackends(components []string) []BackendFact {
	all := l.cap.Snapshot()
	if len(components) == 0 {
		return all
	}
	want := make(map[string]bool, len(components))
	for _, c := range components {
		want[c] = true
	}
	out := make([]BackendFact, 0, len(components))
	for _, b := range all {
		if want[b.Component] {
			out = append(out, b)
		}
	}
	return out
}

// Store exposes the underlying store for read APIs (list/get/export).
func (l *Ledger) Store() Store { return l.store }

// Signer exposes the current signer for public-key export.
func (l *Ledger) Signer() Signer { return l.currentSigner() }

// currentSigner returns the active signer under a read lock (safe vs rotation).
func (l *Ledger) currentSigner() Signer {
	l.signerMu.RLock()
	defer l.signerMu.RUnlock()
	return l.signer
}

// KeyEntries returns every public key the ledger has signed with, oldest first —
// enough for a verifier to check a chain that spans key rotations.
func (l *Ledger) KeyEntries() []PublicKeyEntry {
	l.signerMu.RLock()
	defer l.signerMu.RUnlock()
	out := make([]PublicKeyEntry, len(l.keyHistory))
	copy(out, l.keyHistory)
	return out
}

// RotateSigner switches to a new signing key and records a signed "key.rotate"
// receipt (signed by the NEW key, referencing the old KeyID), so the rotation is
// itself part of the tamper-evident, verifiable log. Not safe to call
// concurrently with Record on a non-atomic store; quiesce writes first.
func (l *Ledger) RotateSigner(ctx context.Context, newSigner Signer, reason string) (*Evidence, error) {
	if newSigner == nil {
		return nil, errors.New("evidence: RotateSigner requires a signer")
	}
	l.signerMu.Lock()
	oldKeyID := l.signer.KeyID()
	l.signer = newSigner
	if pemBytes, err := MarshalPublicKeyPEM(newSigner.PublicKey()); err == nil {
		l.keyHistory = append(l.keyHistory, PublicKeyEntry{KeyID: newSigner.KeyID(), PEM: string(pemBytes)})
	}
	l.signerMu.Unlock()

	return l.Record(ctx, RecordInput{
		Actor:   "evidence",
		Action:  "key.rotate",
		Subject: newSigner.KeyID(),
		Payload: &KeyRotation{OldKeyID: oldKeyID, NewKeyID: newSigner.KeyID(), Reason: reason, RotatedAt: time.Now().UTC()},
	})
}

// Anchorer exposes the configured anchorer (for capability reporting).
func (l *Ledger) Anchorer() Anchorer { return l.anchorer }

// NopRecorder is a Recorder that records nothing. Used when evidence is disabled
// so emitters can call Record unconditionally.
type NopRecorder struct{}

// Record does nothing and returns (nil, nil).
func (NopRecorder) Record(_ context.Context, _ RecordInput) (*Evidence, error) { return nil, nil }

// emptyCapabilitySource is the default when none is supplied (tests, minimal wiring).
type emptyCapabilitySource struct{}

func (emptyCapabilitySource) Snapshot() []BackendFact { return nil }
func (emptyCapabilitySource) RunMode() string         { return "simulation" }
