// Package mesh — in-process (sidecarless) data-plane primitives.
//
// datapath.go provides the read-optimized foundation for the CloudAI Fusion
// in-process service mesh: an endpoint model, a lock-free copy-on-write
// EndpointSet, and a service Registry. Unlike a sidecar proxy (Envoy/ztunnel),
// routing and discovery happen *inside the caller's address space*, so the hot
// path is a pointer load and an array index — nanoseconds, zero allocations,
// and zero extra network hops.
//
// Design invariants:
//   - Read path (Snapshot / Lookup) never takes a mutex and never allocates.
//   - Writers serialize on a mutex, build a fresh immutable slice/map, then
//     publish it with a single atomic pointer swap (copy-on-write).
//   - Published slices/maps are treated as immutable; readers may hold a
//     snapshot for as long as they like without tearing.
package mesh

import (
	"sync"
	"sync/atomic"
)

// hashString computes a 64-bit hash of s without allocating. It is FNV-1a
// followed by a splitmix64 avalanche finalizer. The finalizer is essential:
// raw FNV-1a has weak diffusion, so near-identical inputs (e.g. consistent-hash
// virtual-node keys "e0#0", "e0#1", ...) produce correlated, clustered hashes
// and wreck ring uniformity. The finalizer decorrelates them. It is used for
// consistent-hash keys, ring construction, and split/mirror bucketing — never
// for anything security-sensitive.
func hashString(s string) uint64 {
	const (
		offset64 = 1469598103934665603
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return mix64(h)
}

// mix64 is the splitmix64 finalizer: a bijective bit-mixer with strong
// avalanche. It maps correlated inputs to well-spread outputs. Zero allocation.
func mix64(z uint64) uint64 {
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// mix32 is a 32-bit variant of the same mixer for bucket selection when mixing
// an already-64-bit mixed value down to 32 bits. Zero allocation.
func mix32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x88eddddd
	x ^= x >> 16
	return x
}

// Endpoint is a single routable backend instance. Once published into an
// EndpointSet snapshot the immutable topology fields (ID/Address/Weight) must
// not be mutated. The active counter is a shared pointer so least-connection
// balancing survives copy-on-write snapshots.
type Endpoint struct {
	ID      string
	Address string
	Weight  int
	Healthy bool

	// active tracks in-flight requests for least-connection load balancing.
	// It is a pointer to shared state, intentionally excluded from the
	// immutable topology so snapshots share the same live counter.
	active *atomic.Int64
}

// NewEndpoint builds an endpoint with an initialized in-flight counter.
func NewEndpoint(id, address string, weight int) *Endpoint {
	if weight <= 0 {
		weight = 1
	}
	return &Endpoint{
		ID:      id,
		Address: address,
		Weight:  weight,
		Healthy: true,
		active:  new(atomic.Int64),
	}
}

// Acquire increments the in-flight counter and returns a release function. It
// is used by least-connection balancing to reflect live load.
func (e *Endpoint) Acquire() func() {
	e.active.Add(1)
	return func() { e.active.Add(-1) }
}

// Active returns the current in-flight request count for this endpoint.
func (e *Endpoint) Active() int64 { return e.active.Load() }

// EndpointSet is a lock-free, copy-on-write set of endpoints for a single
// service. Reads load an atomic pointer (zero allocation); writes build a new
// slice and swap the pointer under a writer mutex.
type EndpointSet struct {
	ptr atomic.Pointer[[]*Endpoint]
	mu  sync.Mutex // serializes writers only
}

// NewEndpointSet builds a set from an initial list. The list is copied so the
// caller's slice cannot mutate published state.
func NewEndpointSet(eps ...*Endpoint) *EndpointSet {
	s := &EndpointSet{}
	cp := make([]*Endpoint, len(eps))
	copy(cp, eps)
	s.ptr.Store(&cp)
	return s
}

// Snapshot returns the current immutable endpoint slice. The hot read path:
// a single atomic pointer load, no allocation, no lock. Callers MUST treat the
// returned slice as read-only.
func (s *EndpointSet) Snapshot() []*Endpoint {
	p := s.ptr.Load()
	if p == nil {
		return nil
	}
	return *p
}

// Len returns the number of endpoints currently published.
func (s *EndpointSet) Len() int { return len(s.Snapshot()) }

// Replace atomically swaps the entire endpoint list (copy-on-write).
func (s *EndpointSet) Replace(eps []*Endpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]*Endpoint, len(eps))
	copy(cp, eps)
	s.ptr.Store(&cp)
}

// Add appends an endpoint via copy-on-write. If an endpoint with the same ID
// already exists it is replaced in place (new topology).
func (s *EndpointSet) Add(ep *Endpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.Snapshot()
	next := make([]*Endpoint, 0, len(cur)+1)
	replaced := false
	for _, e := range cur {
		if e.ID == ep.ID {
			next = append(next, ep)
			replaced = true
		} else {
			next = append(next, e)
		}
	}
	if !replaced {
		next = append(next, ep)
	}
	s.ptr.Store(&next)
}

// Remove drops the endpoint with the given ID via copy-on-write.
func (s *EndpointSet) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.Snapshot()
	next := make([]*Endpoint, 0, len(cur))
	for _, e := range cur {
		if e.ID != id {
			next = append(next, e)
		}
	}
	s.ptr.Store(&next)
}

// SetHealth flips the health flag for an endpoint via copy-on-write. Because
// endpoints are immutable once published, a fresh Endpoint value is created so
// concurrent readers holding the old snapshot are unaffected.
func (s *EndpointSet) SetHealth(id string, healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.Snapshot()
	next := make([]*Endpoint, len(cur))
	for i, e := range cur {
		if e.ID == id {
			clone := *e // copy topology; keep shared active counter
			clone.Healthy = healthy
			next[i] = &clone
		} else {
			next[i] = e
		}
	}
	s.ptr.Store(&next)
}

// Registry is a lock-free, copy-on-write service→EndpointSet map. It is the
// in-process analogue of a service-discovery cache: Lookup is a single map read
// with no lock and no allocation, so discovery at any scale (100 / 1k / 10k
// endpoints across services) stays in the nanosecond range.
type Registry struct {
	ptr atomic.Pointer[map[string]*EndpointSet]
	mu  sync.Mutex // serializes writers only
}

// NewRegistry builds an empty service registry.
func NewRegistry() *Registry {
	r := &Registry{}
	m := make(map[string]*EndpointSet)
	r.ptr.Store(&m)
	return r
}

// Lookup returns the EndpointSet for a service, or nil if unknown. Hot path:
// one atomic load + one map read, zero allocation.
func (r *Registry) Lookup(service string) *EndpointSet {
	p := r.ptr.Load()
	if p == nil {
		return nil
	}
	return (*p)[service]
}

// Register publishes (or replaces) the EndpointSet for a service via
// copy-on-write of the top-level map.
func (r *Registry) Register(service string, set *EndpointSet) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur := r.ptr.Load()
	next := make(map[string]*EndpointSet, len(*cur)+1)
	for k, v := range *cur {
		next[k] = v
	}
	next[service] = set
	r.ptr.Store(&next)
}

// Services returns the number of registered services.
func (r *Registry) Services() int {
	p := r.ptr.Load()
	if p == nil {
		return 0
	}
	return len(*p)
}
