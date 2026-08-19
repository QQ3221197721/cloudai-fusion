package config

// crdt.go implements the conflict-free replicated data types that let Module 8
// converge configuration across many nodes without a central coordinator and
// without losing writes. Two CRDTs are provided:
//
//  1. LWW-register (last-write-wins) — the merge primitive for a single config
//     key. Each write carries a Hybrid Logical Clock (HLC) timestamp; the merge
//     keeps the write with the greater timestamp. Ties are broken deterministically
//     by (logical counter, node id, value) so that every replica, regardless of
//     the order in which it receives updates, converges to the byte-identical
//     result. This is what "deterministic merge" means here.
//
//  2. OR-set (observed-remove set) — the merge primitive for set-valued config
//     (e.g. an allow-list of enabled features / cloud regions). Adds are tagged
//     with a unique dot so that a concurrent add always wins over a remove that
//     did not observe it — the standard OR-set semantics.
//
// Both types satisfy the CRDT laws (Merge is commutative, associative and
// idempotent), which is what guarantees eventual, deterministic convergence.

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Hybrid Logical Clock
// ---------------------------------------------------------------------------

// HLC is a Hybrid Logical Clock timestamp. It combines physical wall-clock time
// (nanoseconds) with a logical counter so that causally-related events keep a
// total order even when two nodes' wall clocks are identical or skewed. The
// Node field provides a final, stable tie-break so the ordering is total across
// the whole cluster.
type HLC struct {
	Wall    int64  `json:"wall"`    // unix nanoseconds
	Logical uint64 `json:"logical"` // monotonic counter within the same wall tick
	Node    string `json:"node"`    // originating node id (final tie-break)
}

// Compare returns -1, 0 or +1 giving a TOTAL order over HLC values. The order is
// (Wall, Logical, Node); Node guarantees no two distinct nodes ever compare
// equal, which is what makes LWW merges deterministic across replicas.
func (a HLC) Compare(b HLC) int {
	switch {
	case a.Wall != b.Wall:
		if a.Wall < b.Wall {
			return -1
		}
		return 1
	case a.Logical != b.Logical:
		if a.Logical < b.Logical {
			return -1
		}
		return 1
	case a.Node != b.Node:
		if a.Node < b.Node {
			return -1
		}
		return 1
	default:
		return 0
	}
}

// After reports whether a is strictly greater than b in the total order.
func (a HLC) After(b HLC) bool { return a.Compare(b) > 0 }

// Clock is a per-node HLC generator. Now() is safe for concurrent use and never
// returns the same timestamp twice for a given node.
type Clock struct {
	node    string
	wall    func() int64
	lastVal atomic.Uint64 // packed (wall<<0) is not enough; guarded by mu below
	mu      sync.Mutex
	last    HLC
}

// NewClock returns a clock stamped with the given node id, using the system
// wall clock. Each node in the cluster MUST use a unique node id.
func NewClock(node string) *Clock {
	return &Clock{node: node, wall: func() int64 { return time.Now().UnixNano() }}
}

// Now returns a fresh, strictly-monotonic HLC for this node.
func (c *Clock) Now() HLC {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.wall()
	if w > c.last.Wall {
		c.last = HLC{Wall: w, Logical: 0, Node: c.node}
	} else {
		// Wall clock did not advance (or went backwards): bump the logical part
		// so the timestamp is still strictly increasing.
		c.last = HLC{Wall: c.last.Wall, Logical: c.last.Logical + 1, Node: c.node}
	}
	return c.last
}

// Observe advances this clock past a remote timestamp so that any event we
// generate afterwards is ordered after the event we just saw (causal tracking).
func (c *Clock) Observe(remote HLC) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.wall()
	max := c.last
	if remote.Compare(max) > 0 {
		max = remote
	}
	if w > max.Wall {
		c.last = HLC{Wall: w, Logical: 0, Node: c.node}
	} else {
		c.last = HLC{Wall: max.Wall, Logical: max.Logical + 1, Node: c.node}
	}
}

// ---------------------------------------------------------------------------
// LWW-register
// ---------------------------------------------------------------------------

// LWWRegister is a last-write-wins register holding one config value plus the
// HLC timestamp of the write that produced it. The zero value is a valid empty
// register (TS is the minimum, so any real write wins over it).
type LWWRegister struct {
	Value string `json:"value"`
	TS    HLC    `json:"ts"`
	// Tombstone marks the key as deleted while retaining the timestamp, so a
	// delete can win/lose against a concurrent write by the same LWW rule.
	Tombstone bool `json:"tombstone,omitempty"`
}

// Merge returns the winning register between r and o. It is commutative,
// associative and idempotent. On a timestamp tie (which can only happen for the
// SAME node writing the same logical tick, i.e. a duplicate) the value with the
// greater byte ordering wins so the result is still deterministic.
func (r LWWRegister) Merge(o LWWRegister) LWWRegister {
	c := o.TS.Compare(r.TS)
	if c > 0 {
		return o
	}
	if c < 0 {
		return r
	}
	// Exact timestamp tie: deterministic value/tombstone tie-break.
	if o.Value > r.Value {
		return o
	}
	if o.Value == r.Value && o.Tombstone && !r.Tombstone {
		return o
	}
	return r
}

// ---------------------------------------------------------------------------
// OR-set (observed-remove set)
// ---------------------------------------------------------------------------

// dot is a unique add-tag: (node, counter). A distinct dot is minted for every
// add so that concurrent add/remove resolve in favour of the add (OR-set rule).
type dot struct {
	Node    string `json:"n"`
	Counter uint64 `json:"c"`
}

// ORSet is an observed-remove set of strings. It supports concurrent add/remove
// with the guarantee that an add not observed by a remove survives the merge.
type ORSet struct {
	// element -> set of live add-dots
	adds map[string]map[dot]struct{}
	// tombstoned dots (removed adds we have observed)
	removed map[dot]struct{}
}

// NewORSet returns an empty observed-remove set.
func NewORSet() *ORSet {
	return &ORSet{adds: map[string]map[dot]struct{}{}, removed: map[dot]struct{}{}}
}

// Add inserts elem, minting a fresh dot from the supplied clock. Safe to call
// repeatedly; each call records a new observed add.
func (s *ORSet) Add(elem string, clk *Clock) {
	ts := clk.Now()
	d := dot{Node: ts.Node, Counter: ts.Wall2Counter()}
	if s.adds[elem] == nil {
		s.adds[elem] = map[dot]struct{}{}
	}
	s.adds[elem][d] = struct{}{}
}

// Remove deletes elem by tombstoning every add-dot currently observed for it.
// Adds that arrive later (with new dots) are unaffected — OR-set semantics.
func (s *ORSet) Remove(elem string) {
	for d := range s.adds[elem] {
		s.removed[d] = struct{}{}
	}
	delete(s.adds, elem)
}

// Contains reports whether elem has at least one live (non-tombstoned) add-dot.
func (s *ORSet) Contains(elem string) bool {
	for d := range s.adds[elem] {
		if _, gone := s.removed[d]; !gone {
			return true
		}
	}
	return false
}

// Elements returns the live members in sorted order (deterministic).
func (s *ORSet) Elements() []string {
	out := make([]string, 0, len(s.adds))
	for e := range s.adds {
		if s.Contains(e) {
			out = append(out, e)
		}
	}
	sort.Strings(out)
	return out
}

// Merge folds other into s. Commutative, associative, idempotent: the union of
// live add-dots minus the union of observed tombstones.
func (s *ORSet) Merge(other *ORSet) {
	for d := range other.removed {
		s.removed[d] = struct{}{}
	}
	for elem, dots := range other.adds {
		if s.adds[elem] == nil {
			s.adds[elem] = map[dot]struct{}{}
		}
		for d := range dots {
			s.adds[elem][d] = struct{}{}
		}
	}
	// Garbage-collect any add-dot now covered by a tombstone.
	for elem, dots := range s.adds {
		for d := range dots {
			if _, gone := s.removed[d]; gone {
				delete(dots, d)
			}
		}
		if len(dots) == 0 {
			delete(s.adds, elem)
		}
	}
}

// Wall2Counter derives a per-node-unique counter for an add-dot from an HLC.
// It mixes the wall tick and logical part so repeated Adds within one tick still
// produce distinct dots.
func (a HLC) Wall2Counter() uint64 {
	return uint64(a.Wall)<<12 ^ a.Logical
}

// ---------------------------------------------------------------------------
// ConfigState — the replicated config document
// ---------------------------------------------------------------------------

// ConfigState is the per-node replicated view of the whole configuration: a map
// of key -> LWW-register. Merging two ConfigStates converges every key by the
// LWW rule, so all replicas that have seen the same set of writes hold the
// byte-identical document regardless of delivery order.
type ConfigState struct {
	mu    sync.RWMutex
	node  string
	clock *Clock
	regs  map[string]LWWRegister
}

// NewConfigState returns an empty replicated config document for the given node.
func NewConfigState(node string) *ConfigState {
	return &ConfigState{
		node:  node,
		clock: NewClock(node),
		regs:  map[string]LWWRegister{},
	}
}

// Set writes key=value locally, stamping it with a fresh HLC. The write becomes
// the winner unless a strictly-later write for the same key is merged in.
func (c *ConfigState) Set(key, value string) HLC {
	c.mu.Lock()
	defer c.mu.Unlock()
	ts := c.clock.Now()
	c.regs[key] = LWWRegister{Value: value, TS: ts}
	return ts
}

// Delete tombstones key locally with a fresh HLC.
func (c *ConfigState) Delete(key string) HLC {
	c.mu.Lock()
	defer c.mu.Unlock()
	ts := c.clock.Now()
	c.regs[key] = LWWRegister{TS: ts, Tombstone: true}
	return ts
}

// Get returns the current value for key and whether it is live (present and not
// tombstoned).
func (c *ConfigState) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.regs[key]
	if !ok || r.Tombstone {
		return "", false
	}
	return r.Value, true
}

// Registers returns a copy of the raw register map, suitable for shipping to a
// peer during reconciliation.
func (c *ConfigState) Registers() map[string]LWWRegister {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]LWWRegister, len(c.regs))
	for k, v := range c.regs {
		out[k] = v
	}
	return out
}

// Merge folds a peer's registers into this state using the LWW rule and advances
// the local clock past every observed timestamp. Returns the number of keys
// whose winning value changed as a result — a useful convergence signal.
func (c *ConfigState) Merge(peer map[string]LWWRegister) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := 0
	for k, incoming := range peer {
		c.clock.Observe(incoming.TS)
		cur, ok := c.regs[k]
		if !ok {
			c.regs[k] = incoming
			changed++
			continue
		}
		merged := cur.Merge(incoming)
		if merged != cur {
			c.regs[k] = merged
			changed++
		}
	}
	return changed
}

// Snapshot returns the live (non-tombstoned) key/value view, sorted-key stable.
func (c *ConfigState) Snapshot() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.regs))
	for k, r := range c.regs {
		if !r.Tombstone {
			out[k] = r.Value
		}
	}
	return out
}
