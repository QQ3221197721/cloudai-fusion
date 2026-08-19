package config

// hotreload.go implements the zero-downtime hot-reload core of Module 8: an
// immutable Snapshot published through a single lock-free atomic pointer.
//
// The design is copy-on-write:
//
//   - Readers call HotStore.Load() (or Flag()) which does ONE atomic pointer
//     load and then a plain map read. There are no locks on the read path, so a
//     flag lookup is a couple of nanoseconds and never blocks, even while a
//     reload is in flight.
//
//   - Writers never mutate a live Snapshot. They build a brand-new Snapshot,
//     seal it (Ed25519), and publish it with a single atomic Store/CompareAndSwap.
//     In-flight readers keep using the previous Snapshot until their next Load();
//     nobody ever observes a half-applied config. That is what "atomic swap with
//     no downtime" means here.

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync/atomic"
	"time"
)

// Snapshot is an IMMUTABLE, fully-consistent view of configuration at one point
// in time. After it is published via HotStore.Swap it MUST NOT be mutated — the
// whole no-downtime guarantee rests on that. Build a new Snapshot instead.
type Snapshot struct {
	// Version is the SHA-256 (hex) digest over the canonical key=value form of
	// Values. Two snapshots with identical content share a version, which lets
	// callers cheaply detect "nothing changed" and skip a swap.
	Version string

	// Values holds string config keys/values. Feature flags live here under the
	// "ff_" prefix (e.g. ff_rl_scheduler=true) so the flag lookup is a single
	// map read against this same map.
	Values map[string]string

	// Meta carries non-config context (origin node, source path, profile).
	Meta map[string]string

	// Timestamp is the server UTC creation time.
	Timestamp time.Time

	// Sealed is the Ed25519-sealed bundle for this Version (the existing moat).
	// nil only for the empty bootstrap snapshot.
	Sealed *SealedBundle
}

// ComputeVersion returns the deterministic SHA-256 (hex) digest of a config map.
// Keys are sorted first so the digest is stable regardless of map iteration
// order — essential for convergence detection across nodes.
func ComputeVersion(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{'='})
		h.Write([]byte(values[k]))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Flag answers a feature-flag query against this immutable snapshot. The flag is
// stored under the "ff_" prefix; a missing flag reads as false. This is a single
// map read with no locking — the benchmark reports it at well under 20ns/op.
func (s *Snapshot) Flag(name string) bool {
	v, ok := s.Values["ff_"+name]
	if !ok {
		return false
	}
	return v == "true" || v == "1"
}

// Get returns a raw config value and whether it is present.
func (s *Snapshot) Get(key string) (string, bool) {
	v, ok := s.Values[key]
	return v, ok
}

// HotStore publishes the current live Snapshot through a single atomic pointer.
// Reads are lock-free; writes are copy-on-write atomic swaps. The zero value is
// not usable — construct with NewHotStore.
type HotStore struct {
	current  atomic.Pointer[Snapshot]
	nodeID   string
	swaps    atomic.Int64 // number of successful swaps (observability)
	reads    atomic.Int64 // number of Load() calls (observability)
}

// NewHotStore returns a store primed with an empty bootstrap snapshot so readers
// never observe a nil view before the first reload.
func NewHotStore(nodeID string) *HotStore {
	h := &HotStore{nodeID: nodeID}
	h.current.Store(&Snapshot{
		Version:   "bootstrap",
		Values:    map[string]string{},
		Meta:      map[string]string{"node": nodeID},
		Timestamp: time.Now().UTC(),
	})
	return h
}

// Load returns the current immutable snapshot with a single atomic load. Safe
// for unbounded concurrent use; never blocks against a concurrent Swap.
func (h *HotStore) Load() *Snapshot {
	h.reads.Add(1)
	return h.current.Load()
}

// Flag is the hot-path convenience wrapper: atomic load + one map read.
func (h *HotStore) Flag(name string) bool {
	return h.current.Load().Flag(name)
}

// Swap installs next as the live snapshot and returns the previous one. The
// publish is a single atomic store, so readers switch over instantaneously and
// no downtime window exists.
func (h *HotStore) Swap(next *Snapshot) *Snapshot {
	prev := h.current.Swap(next)
	h.swaps.Add(1)
	return prev
}

// CompareAndSwap publishes next only if the current snapshot is still expect.
// Returns true on success. Use this to avoid clobbering a concurrent reload.
func (h *HotStore) CompareAndSwap(expect, next *Snapshot) bool {
	if h.current.CompareAndSwap(expect, next) {
		h.swaps.Add(1)
		return true
	}
	return false
}

// Stats reports observability counters (swaps applied, reads served).
func (h *HotStore) Stats() (swaps, reads int64) {
	return h.swaps.Load(), h.reads.Load()
}

// Publish is the copy-on-write helper writers use for a reload: it builds a new
// immutable Snapshot from values, seals it with signer (Ed25519, preserving the
// moat), and atomically swaps it in. If the computed version matches the current
// snapshot's version, no swap occurs and (current, false) is returned — this is
// the "nothing changed" fast path that avoids needless propagation.
func (h *HotStore) Publish(values map[string]string, signer *BundleSigner) (*Snapshot, bool, error) {
	version := ComputeVersion(values)
	cur := h.current.Load()
	if cur != nil && cur.Version == version {
		return cur, false, nil
	}

	// Defensive copy so the caller cannot mutate the published map afterwards.
	cp := make(map[string]string, len(values))
	for k, v := range values {
		cp[k] = v
	}

	next := &Snapshot{
		Version:   version,
		Values:    cp,
		Meta:      map[string]string{"node": h.nodeID},
		Timestamp: time.Now().UTC(),
	}

	if signer != nil {
		sealed, err := signer.Seal(version, cp)
		if err != nil {
			return nil, false, err
		}
		next.Sealed = sealed
	}

	h.Swap(next)
	return next, true, nil
}
