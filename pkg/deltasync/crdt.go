package deltasync

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"sync/atomic"
)

// crdt.go implements a block-level state-based CRDT (CvRDT): an LWW-Element-Map
// keyed by block index whose values carry a monotonic version and a replica id.
//
// The merge operation Join is a least-upper-bound on a join-semilattice and is
// therefore commutative, associative, and idempotent. Consequently every replica
// that has observed the same set of updates converges to a byte-identical state
// regardless of the ORDER in which merges are applied — the property the
// property-based test in crdt_test.go validates empirically.
//
// Deletions use tombstones (Deleted=true carried at a version) so that a delete
// and a concurrent update are ordered by (Version, Replica) exactly like updates,
// keeping the lattice well defined.

// LogicalClock is a per-replica Lamport-style counter. Timestamps produced by
// Next() are strictly increasing within a replica; cross-replica ties are broken
// by the replica id embedded in the LWWRegister.
type LogicalClock struct {
	counter uint64
}

// NewClock returns a fresh clock starting at zero.
func NewClock() *LogicalClock { return &LogicalClock{} }

// Next returns the next monotonic tick.
func (c *LogicalClock) Next() uint64 { return atomic.AddUint64(&c.counter, 1) }

// Observe advances the clock past a peer's timestamp (Lamport merge rule), so
// subsequent local ticks causally dominate everything seen so far.
func (c *LogicalClock) Observe(peer uint64) {
	for {
		cur := atomic.LoadUint64(&c.counter)
		next := cur
		if peer > next {
			next = peer
		}
		if atomic.CompareAndSwapUint64(&c.counter, cur, next) {
			return
		}
	}
}

// LWWRegister is the lattice element stored per block index.
type LWWRegister struct {
	CID     [32]byte `json:"cid"`     // content address of the block
	Size    int      `json:"size"`    // block length in bytes
	Version uint64   `json:"version"` // logical clock tick of this write
	Replica uint32   `json:"replica"` // replica id (tie-break)
	Deleted bool     `json:"deleted"` // tombstone flag
}

// dominates reports whether r is the lattice winner over o. Ordering key is
// (Version, Replica, CID) lexicographically — total and deterministic, so Join
// is well defined and commutative.
func (r LWWRegister) dominates(o LWWRegister) bool {
	if r.Version != o.Version {
		return r.Version > o.Version
	}
	if r.Replica != o.Replica {
		return r.Replica > o.Replica
	}
	return byteCompareGreater(r.CID, o.CID)
}

func byteCompareGreater(a, b [32]byte) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// LWWMap is the CvRDT: a map from block index to its winning register.
type LWWMap struct {
	data map[int]LWWRegister
}

// NewLWWMap creates an empty map.
func NewLWWMap() *LWWMap { return &LWWMap{data: make(map[int]LWWRegister)} }

// Put writes (or overwrites) the register at idx. Callers pass the version from
// their LogicalClock so concurrent writes are totally ordered on merge.
func (m *LWWMap) Put(idx int, cid [32]byte, size int, version uint64, replica uint32) {
	if m.data == nil {
		m.data = make(map[int]LWWRegister)
	}
	incoming := LWWRegister{CID: cid, Size: size, Version: version, Replica: replica}
	if cur, ok := m.data[idx]; !ok || incoming.dominates(cur) {
		m.data[idx] = incoming
	}
}

// Delete places a tombstone at idx at the given version.
func (m *LWWMap) Delete(idx int, version uint64, replica uint32) {
	if m.data == nil {
		m.data = make(map[int]LWWRegister)
	}
	incoming := LWWRegister{Version: version, Replica: replica, Deleted: true}
	if cur, ok := m.data[idx]; !ok || incoming.dominates(cur) {
		m.data[idx] = incoming
	}
}

// Get returns the live register at idx (tombstones report absent).
func (m *LWWMap) Get(idx int) (LWWRegister, bool) {
	if m.data == nil {
		return LWWRegister{}, false
	}
	r, ok := m.data[idx]
	if !ok || r.Deleted {
		return LWWRegister{}, false
	}
	return r, true
}

// Size returns the number of live (non-tombstoned) entries.
func (m *LWWMap) Size() int {
	n := 0
	for _, r := range m.data {
		if !r.Deleted {
			n++
		}
	}
	return n
}

// Join merges other into m in place: for each key keep the dominating register.
// This is the semilattice least-upper-bound — commutative, associative, idempotent.
func (m *LWWMap) Join(other *LWWMap) {
	if other == nil {
		return
	}
	if m.data == nil {
		m.data = make(map[int]LWWRegister)
	}
	for idx, in := range other.data {
		if cur, ok := m.data[idx]; !ok || in.dominates(cur) {
			m.data[idx] = in
		}
	}
}

// Clone returns a deep copy so merges of one replica do not mutate another.
func (m *LWWMap) Clone() *LWWMap {
	cp := &LWWMap{data: make(map[int]LWWRegister, len(m.data))}
	for k, v := range m.data {
		cp.data[k] = v
	}
	return cp
}

// Digest returns a deterministic 256-bit fingerprint of the FULL state
// (including tombstones), so two replicas are byte-identical iff their digests
// match. Entries are hashed in ascending index order to remove map nondeterminism.
func (m *LWWMap) Digest() [32]byte {
	idxs := make([]int, 0, len(m.data))
	for k := range m.data {
		idxs = append(idxs, k)
	}
	sort.Ints(idxs)
	h := sha256.New()
	var scratch [8]byte
	for _, k := range idxs {
		r := m.data[k]
		binary.BigEndian.PutUint64(scratch[:], uint64(k))
		h.Write(scratch[:])
		binary.BigEndian.PutUint64(scratch[:], r.Version)
		h.Write(scratch[:])
		binary.BigEndian.PutUint32(scratch[:4], r.Replica)
		h.Write(scratch[:4])
		if r.Deleted {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		h.Write(r.CID[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Equal reports byte-identical convergence.
func (m *LWWMap) Equal(other *LWWMap) bool { return m.Digest() == other.Digest() }

// NaiveCRDTFullState models the "naive CRDT" sync baseline: to merge, a replica
// ships its ENTIRE state map. RetransBytes reports the serialized full-state size
// (versus deltasync which only transmits changed registers).
func NaiveCRDTFullState(m *LWWMap) int64 {
	// Each register serialized as: idx(8) + version(8) + replica(4) + flag(1) + cid(32) = 53 bytes.
	const bytesPerRegister = 8 + 8 + 4 + 1 + 32
	return int64(len(m.data) * bytesPerRegister)
}

