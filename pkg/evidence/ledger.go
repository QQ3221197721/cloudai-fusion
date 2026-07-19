package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
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

// Record builds, hashes, signs, anchors, and durably appends a receipt.
func (l *Ledger) Record(ctx context.Context, in RecordInput) (*Evidence, error) {
	inputHash, err := HashAny(in.Input)
	if err != nil {
		return nil, err
	}
	outputHash, err := HashAny(in.Output)
	if err != nil {
		return nil, err
	}
	var payload json.RawMessage
	if in.Payload != nil {
		b, err := marshalCanonical(in.Payload)
		if err != nil {
			return nil, err
		}
		payload = b
	}

	backends := in.Backends
	if backends == nil {
		backends = l.snapshotBackends(in.Components)
	}

	// build assembles, hashes, signs, and anchors a record given the current
	// chain head. It runs either under the ledger mutex or inside the store's
	// atomic append, so Seq/PrevHash assignment is race-free.
	sgn := l.currentSigner()
	build := func(last *Evidence) (*Evidence, error) {
		prev := GenesisPrevHash
		var seq uint64 = 1
		if last != nil {
			prev = last.Hash
			seq = last.Seq + 1
		}
		e := &Evidence{
			ID:         common.NewUUID(),
			Seq:        seq,
			PrevHash:   prev,
			Timestamp:  time.Now().UTC(),
			Actor:      in.Actor,
			Action:     in.Action,
			Subject:    in.Subject,
			RunMode:    l.cap.RunMode(),
			Backends:   backends,
			InputHash:  inputHash,
			OutputHash: outputHash,
			Payload:    payload,
		}
		hash, herr := e.ComputeHash()
		if herr != nil {
			return nil, herr
		}
		e.Hash = hash
		// Sign over the hex leaf hash; the Verifier signs/verifies the same bytes.
		sig, serr := sgn.Sign([]byte(hash))
		if serr != nil {
			return nil, serr
		}
		e.Signature = sig
		e.KeyID = sgn.KeyID()
		// Anchor is best-effort but always truthful. NOTE: the simulated anchor is
		// a pure struct assignment (safe inside a DB tx); Phase 5's real Rekor
		// client must anchor post-commit and update LogEntry, which is excluded
		// from the signed content by design.
		if ref, aerr := l.anchorer.Anchor(ctx, AnchorRequest{LeafHex: hash, SignatureB64: sig, PublicKey: sgn.PublicKey()}); aerr == nil {
			e.LogEntry = ref
		} else {
			e.LogEntry = &TransparencyRef{Backend: "simulated", Detail: aerr.Error(), IntegratedAt: time.Now().UTC()}
		}
		return e, nil
	}

	// Prefer the store's atomic append (race-free across concurrent writers, e.g.
	// multiple processes sharing a DB); otherwise serialize within this process.
	if as, ok := l.store.(AtomicStore); ok {
		e, aerr := as.AppendChained(ctx, build)
		if aerr != nil {
			return nil, aerr
		}
		observeRecord(e)
		return e, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	last, err := l.store.Last(ctx)
	if err != nil {
		return nil, err
	}
	e, err := build(last)
	if err != nil {
		return nil, err
	}
	if err := l.store.Append(ctx, e); err != nil {
		return nil, err
	}
	observeRecord(e)
	return e, nil
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
