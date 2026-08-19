// Package mesh — in-process load balancing algorithms.
//
// loadbalancer.go implements the three classic balancing strategies over an
// immutable endpoint snapshot. All Pick calls run on the caller's goroutine
// with no lock and (for round-robin and consistent-hash) zero allocation.
//
//   - RoundRobin      : one atomic counter increment + modulo.
//   - LeastConn       : single O(n) scan of live in-flight counters.
//   - ConsistentHash  : precomputed virtual-node ring + binary search.
package mesh

import (
	"sort"
	"sync/atomic"
)

// Balancer selects one healthy endpoint from an immutable snapshot. Pick must
// be safe for concurrent use and must not mutate the snapshot. The key is used
// only by hash-based balancers (ignored by round-robin / least-conn); it is
// typically a hash of the request's session/routing key.
type Balancer interface {
	Pick(eps []*Endpoint, key uint64) (*Endpoint, bool)
	Name() string
}

// RoundRobin distributes requests evenly using a single atomic counter. The hot
// path is one atomic add and a bounded scan that skips unhealthy endpoints.
type RoundRobin struct {
	ctr atomic.Uint64
}

// NewRoundRobin builds a round-robin balancer.
func NewRoundRobin() *RoundRobin { return &RoundRobin{} }

// Name implements Balancer.
func (*RoundRobin) Name() string { return "round-robin" }

// Pick returns the next endpoint in rotation, skipping unhealthy ones. Zero
// allocation. Returns false when no healthy endpoint exists.
func (r *RoundRobin) Pick(eps []*Endpoint, _ uint64) (*Endpoint, bool) {
	n := len(eps)
	if n == 0 {
		return nil, false
	}
	start := int(r.ctr.Add(1) - 1)
	for i := 0; i < n; i++ {
		ep := eps[(start+i)%n]
		if ep.Healthy {
			return ep, true
		}
	}
	return nil, false
}

// LeastConn selects the healthy endpoint with the fewest in-flight requests,
// weighted by endpoint weight (active/weight ratio). Zero allocation.
type LeastConn struct{}

// NewLeastConn builds a least-connection balancer.
func NewLeastConn() *LeastConn { return &LeastConn{} }

// Name implements Balancer.
func (*LeastConn) Name() string { return "least-conn" }

// Pick returns the endpoint minimizing active/weight. Ties break toward the
// earlier endpoint. Zero allocation.
func (*LeastConn) Pick(eps []*Endpoint, _ uint64) (*Endpoint, bool) {
	var best *Endpoint
	var bestScore float64
	for _, ep := range eps {
		if !ep.Healthy {
			continue
		}
		w := ep.Weight
		if w <= 0 {
			w = 1
		}
		score := float64(ep.active.Load()) / float64(w)
		if best == nil || score < bestScore {
			best = ep
			bestScore = score
		}
	}
	return best, best != nil
}

// ConsistentHashRing is a precomputed hash ring with virtual nodes. Building the
// ring is O(v·n·log) and happens only on topology change; the hot Pick path is a
// binary search over a sorted uint64 slice — O(log(v·n)), zero allocation. This
// gives minimal key remapping when endpoints join/leave (≈1/n of keys move),
// which random/modulo selection cannot offer.
type ConsistentHashRing struct {
	hashes []uint64    // sorted virtual-node hashes
	owners []int       // owners[i] indexes into eps for hashes[i]
	eps    []*Endpoint // endpoint snapshot the ring was built from
	vnodes int
}

// defaultVNodes is the number of virtual nodes per endpoint. More vnodes yield
// smoother key distribution at the cost of a larger ring.
const defaultVNodes = 160

// NewConsistentHashRing compiles a ring from an endpoint snapshot. vnodes<=0
// falls back to defaultVNodes. Weighted endpoints receive proportionally more
// virtual nodes (weight × vnodes).
func NewConsistentHashRing(eps []*Endpoint, vnodes int) *ConsistentHashRing {
	if vnodes <= 0 {
		vnodes = defaultVNodes
	}
	total := 0
	for _, e := range eps {
		w := e.Weight
		if w <= 0 {
			w = 1
		}
		total += vnodes * w
	}
	r := &ConsistentHashRing{
		hashes: make([]uint64, 0, total),
		owners: make([]int, 0, total),
		eps:    eps,
		vnodes: vnodes,
	}
	// Collect (hash, owner) pairs, then sort by hash.
	type vnode struct {
		h     uint64
		owner int
	}
	nodes := make([]vnode, 0, total)
	var buf [64]byte
	for idx, e := range eps {
		w := e.Weight
		if w <= 0 {
			w = 1
		}
		for v := 0; v < vnodes*w; v++ {
			// key = "<id>#<v>" hashed; build without fmt to avoid garbage.
			n := copy(buf[:], e.ID)
			buf[n] = '#'
			m := n + 1
			m += putUint(buf[m:], uint64(v))
			nodes = append(nodes, vnode{h: hashString(string(buf[:m])), owner: idx})
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].h < nodes[j].h })
	for _, vn := range nodes {
		r.hashes = append(r.hashes, vn.h)
		r.owners = append(r.owners, vn.owner)
	}
	return r
}

// Name implements Balancer.
func (*ConsistentHashRing) Name() string { return "consistent-hash" }

// Pick maps the key onto the ring and returns the owning endpoint, walking
// clockwise past unhealthy owners. Zero allocation.
func (r *ConsistentHashRing) Pick(_ []*Endpoint, key uint64) (*Endpoint, bool) {
	n := len(r.hashes)
	if n == 0 {
		return nil, false
	}
	// Binary search: first vnode hash >= key (wrap around when past the end).
	lo, hi := 0, n
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if r.hashes[mid] < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	// Walk clockwise up to n positions to find a healthy owner.
	for i := 0; i < n; i++ {
		pos := (lo + i) % n
		ep := r.eps[r.owners[pos]]
		if ep.Healthy {
			return ep, true
		}
	}
	return nil, false
}

// PickKey is a convenience that hashes a string key and returns the owner.
func (r *ConsistentHashRing) PickKey(key string) (*Endpoint, bool) {
	return r.Pick(nil, hashString(key))
}

// Size returns the number of virtual nodes on the ring.
func (r *ConsistentHashRing) Size() int { return len(r.hashes) }

// putUint writes the decimal representation of v into b and returns the number
// of bytes written. It allocates nothing (used during ring construction).
func putUint(b []byte, v uint64) int {
	if v == 0 {
		b[0] = '0'
		return 1
	}
	var tmp [20]byte
	i := len(tmp)
	for v > 0 {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
	}
	return copy(b, tmp[i:])
}

