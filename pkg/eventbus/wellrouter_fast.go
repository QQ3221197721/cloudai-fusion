package eventbus

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
)

// wellrouter_fast.go is Module 6's performance moat: a zero-allocation,
// hop-bounded routing core for the 16-well AISecOps fabric.
//
// The existing WellRouter (deepwell.go / fabric.go) routes rich *Event values
// whose hop counter and target well live in a string-keyed metadata map and
// whose payload is JSON. That is convenient for interop but allocates a fresh
// map and event per hop and re-parses integers out of strings on the hot path.
// FastRouter keeps the identical routing *semantics* — hop-bounded TTL, loop
// prevention, deterministic fan-out along the same connectivity graph, and an
// L8 SOAR terminal hop — but expresses the envelope as a fixed-layout struct
// pooled through sync.Pool. The routing core therefore performs no heap
// allocation in steady state. Even on the signed path the benchmark measures
// 0 allocs/op: the signature slice ed25519.Sign returns does not escape sign()
// (it is copied into the fixed Sig array), so escape analysis keeps it on the
// stack. We deliberately reuse the stdlib crypto rather than reimplement it.
//
// The moat vs NATS/Kafka is not raw fan-out speed alone: every envelope that
// leaves the router is Ed25519-signed over a canonical header that binds the
// well, origin, hop, loop-prevention bitmask, sequence, and a SHA-256 digest of
// the payload. A broker forwards opaque bytes; this fabric forwards
// self-authenticating, hop-bounded, loop-free envelopes.

// WellSink receives each child envelope the router produces. The sink takes
// ownership of the envelope for the duration of the call and MUST return it to
// the router via Release once done (a hot-path sink typically releases it
// immediately). The sink must not retain the envelope past Release.
type WellSink func(*WellEnvelope)

// envHeaderLen is the size of the canonical signed header:
// well(1) + origin(1) + hop(1) + visited(4) + seq(8) + payloadDigest(32).
const envHeaderLen = 1 + 1 + 1 + 4 + 8 + sha256.Size

// ErrNilEnvelope is returned when a routing call is given a nil envelope.
var ErrNilEnvelope = errors.New("eventbus: nil well envelope")

// WellEnvelope is the fixed-layout message that travels the fast routing path.
// Unlike Event it carries the hop counter and loop-prevention state as machine
// words, so routing neither parses strings nor allocates maps. Instances are
// pooled: obtain them from Seed / via a Deliver sink and return them with
// Release.
type WellEnvelope struct {
	// Well is the well this envelope is currently routed to.
	Well DeepWell
	// Origin is the well that first raised the signal.
	Origin DeepWell
	// Hop is how many wells the envelope has traversed (seed = 0).
	Hop uint8
	// Signed reports whether Sig holds a valid Ed25519 signature.
	Signed bool
	// Visited is a bitmask of wells already on this envelope's path (bit
	// w-1 for well w). It makes loop prevention O(1) and allocation-free
	// even on the intentionally cyclic connectivity graph.
	Visited uint32
	// Seq is a monotonic per-router sequence number, giving every envelope a
	// deterministic identity that is bound into the signature.
	Seq uint64
	// Payload is the opaque event body. It is referenced, never copied, so
	// callers must not mutate it while the envelope is in flight.
	Payload []byte
	// Sig is the Ed25519 signature over the canonical header (see marshalHeader).
	Sig [ed25519.SignatureSize]byte
}

// marshalHeader writes the canonical signing header into buf (which must be at
// least envHeaderLen long) and returns the number of bytes written. pd is the
// SHA-256 digest of the payload, bound into the header so the signature covers
// payload integrity without signing the whole (possibly large) body.
func (env *WellEnvelope) marshalHeader(buf []byte, pd [32]byte) int {
	_ = buf[envHeaderLen-1] // bounds-check hint; panics early on a short buffer
	buf[0] = byte(env.Well)
	buf[1] = byte(env.Origin)
	buf[2] = env.Hop
	binary.LittleEndian.PutUint32(buf[3:7], env.Visited)
	binary.LittleEndian.PutUint64(buf[7:15], env.Seq)
	copy(buf[15:15+sha256.Size], pd[:])
	return envHeaderLen
}

// wellBit returns the loop-prevention bit for a well (well w -> bit w-1).
func wellBit(w DeepWell) uint32 { return 1 << (uint(w) - 1) }

// FastRouter is the zero-allocation, hop-bounded core router. It is safe for
// concurrent use: it holds no per-event mutable state outside pooled envelopes
// and atomic counters.
type FastRouter struct {
	maxHop uint8
	signer ed25519.PrivateKey // nil => envelopes are not signed
	pubKey ed25519.PublicKey
	seq    atomic.Uint64
	pool   sync.Pool // *WellEnvelope

	routed    atomic.Int64 // envelopes fed into Deliver
	delivered atomic.Int64 // child envelopes handed to a sink
	dropped   atomic.Int64 // fan-out edges skipped by loop prevention
	l8Count   atomic.Int64 // terminal-hop (L8 SOAR) consumptions
}

// NewFastRouter builds a router with the given hop cap. maxHop<=0 (or an
// out-of-range value) defaults to MaxWellHops. A valid Ed25519 private key
// enables signed envelopes (the moat); a nil/invalid key produces unsigned
// envelopes, which is useful only for measuring the routing core in isolation.
func NewFastRouter(maxHop int, signer ed25519.PrivateKey) *FastRouter {
	mh := maxHop
	if mh <= 0 || mh > 255 {
		mh = MaxWellHops
	}
	fr := &FastRouter{maxHop: uint8(mh)}
	if len(signer) == ed25519.PrivateKeySize {
		fr.signer = signer
		fr.pubKey = signer.Public().(ed25519.PublicKey)
	}
	fr.pool.New = func() any { return new(WellEnvelope) }
	return fr
}

// MaxHop returns the configured hop cap.
func (fr *FastRouter) MaxHop() uint8 { return fr.maxHop }

// Signed reports whether the router signs envelopes.
func (fr *FastRouter) Signed() bool { return fr.signer != nil }

// RoutedCount, DeliveredCount, DroppedCount and L8Count expose routing metrics.
func (fr *FastRouter) RoutedCount() int64    { return fr.routed.Load() }
func (fr *FastRouter) DeliveredCount() int64 { return fr.delivered.Load() }
func (fr *FastRouter) DroppedCount() int64   { return fr.dropped.Load() }
func (fr *FastRouter) L8Count() int64        { return fr.l8Count.Load() }

// acquire pulls a reset-on-release envelope from the pool.
func (fr *FastRouter) acquire() *WellEnvelope { return fr.pool.Get().(*WellEnvelope) }

// Release returns an envelope to the pool. It clears the payload reference so a
// pooled envelope never pins a stale buffer. Safe to call with nil.
func (fr *FastRouter) Release(env *WellEnvelope) {
	if env == nil {
		return
	}
	env.Payload = nil
	env.Signed = false
	fr.pool.Put(env)
}

// Seed produces the origin envelope for a signal raised in origin. The returned
// envelope is owned by the caller and must eventually be returned with Release.
func (fr *FastRouter) Seed(origin DeepWell, payload []byte) (*WellEnvelope, error) {
	if !origin.Valid() {
		return nil, ErrNoSourceWell
	}
	env := fr.acquire()
	env.Well = origin
	env.Origin = origin
	env.Hop = 0
	env.Visited = wellBit(origin)
	env.Seq = fr.seq.Add(1)
	env.Payload = payload
	env.Signed = false
	if fr.signer != nil {
		fr.sign(env, sha256.Sum256(payload))
	}
	return env, nil
}

// Deliver performs one hop-bounded, loop-free, deterministic fan-out step for
// in. For each downstream well (in the fabric's fixed connectivity order) that
// the envelope has not already visited, it produces a signed child envelope at
// hop+1 and hands it to sink. When in has reached the hop cap it is instead
// consumed by the L8 SOAR terminal (counted via L8Count) and nothing is fanned
// out. It returns the number of children delivered.
//
// The hot path is allocation-free: children are drawn from the pool, the loop
// check is a bitmask test, the signing header is built on the stack, and the
// signature ed25519.Sign returns is copied into the fixed Sig array without
// escaping (measured 0 allocs/op even with signing enabled).
func (fr *FastRouter) Deliver(in *WellEnvelope, sink WellSink) (int, error) {
	if in == nil {
		return 0, ErrNilEnvelope
	}
	if !in.Well.Valid() {
		return 0, ErrNoSourceWell
	}
	fr.routed.Add(1)

	// Terminal hop: the L8 SOAR well consumes the signal; no further routing.
	if in.Hop >= fr.maxHop {
		fr.l8Count.Add(1)
		return 0, nil
	}

	// The payload is shared by every child, so its digest is computed once.
	var pd [32]byte
	signing := fr.signer != nil
	if signing {
		pd = sha256.Sum256(in.Payload)
	}

	fanout := 0
	childHop := in.Hop + 1
	for _, dst := range connectivity[in.Well] { // fixed order => deterministic
		bit := wellBit(dst)
		if in.Visited&bit != 0 {
			fr.dropped.Add(1) // loop prevention: already on this path
			continue
		}
		child := fr.acquire()
		child.Well = dst
		child.Origin = in.Origin
		child.Hop = childHop
		child.Visited = in.Visited | bit
		child.Seq = fr.seq.Add(1)
		child.Payload = in.Payload
		child.Signed = false
		if signing {
			fr.sign(child, pd)
		}
		sink(child)
		fr.delivered.Add(1)
		fanout++
	}
	return fanout, nil
}

// Propagate runs a full hop-bounded, loop-free breadth-first propagation from
// origin, invoking observe (if non-nil) for every envelope visited, and returns
// the number of envelopes visited. It is a convenience built on Deliver for
// callers that want the whole fabric traversal rather than a single hop; the
// per-hop work stays allocation-free, though the BFS work-queue itself grows on
// the heap. Termination is guaranteed by the visited bitmask and hop cap.
func (fr *FastRouter) Propagate(origin DeepWell, payload []byte, observe func(*WellEnvelope)) (int, error) {
	seed, err := fr.Seed(origin, payload)
	if err != nil {
		return 0, err
	}
	queue := []*WellEnvelope{seed}
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if observe != nil {
			observe(cur)
		}
		if _, derr := fr.Deliver(cur, func(child *WellEnvelope) {
			queue = append(queue, child)
		}); derr != nil {
			fr.Release(cur)
			return visited, derr
		}
		visited++
		fr.Release(cur)
	}
	return visited, nil
}

// sign writes an Ed25519 signature over env's canonical header into env.Sig.
// The header buffer is stack-allocated; pd is the payload digest bound into it.
func (fr *FastRouter) sign(env *WellEnvelope, pd [32]byte) {
	var msg [envHeaderLen]byte
	n := env.marshalHeader(msg[:], pd)
	sig := ed25519.Sign(fr.signer, msg[:n])
	copy(env.Sig[:], sig)
	env.Signed = true
}

// Verify checks an envelope's Ed25519 signature against the router's public
// key, recomputing the payload digest so payload tampering is detected. It
// returns false for an unsigned envelope or a router without a key.
func (fr *FastRouter) Verify(env *WellEnvelope) bool {
	if env == nil || !env.Signed || len(fr.pubKey) != ed25519.PublicKeySize {
		return false
	}
	pd := sha256.Sum256(env.Payload)
	var msg [envHeaderLen]byte
	n := env.marshalHeader(msg[:], pd)
	return ed25519.Verify(fr.pubKey, msg[:n], env.Sig[:])
}
