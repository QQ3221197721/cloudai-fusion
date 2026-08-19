// Package mesh — in-process route table (prefix matching) + resilience primitives.
//
// route_table.go provides L7 path-based route matching via a compiled byte-level
// prefix trie plus resilience primitives (circuit breaker, retry policy, traffic
// splitter/mirror). All hot paths are read-only pointer/integer/atomic ops with
// zero allocation. This is the sidecarless alternative to an Envoy route config:
// the decision is a handful of array indexes in the caller's own goroutine.
package mesh

import (
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// Route table — longest-prefix path matching via a byte trie
// ============================================================================

// RouteTable maps request paths to backends via longest-prefix match. Rule
// insertion/removal is serialized; Match is a single read-lock + byte walk with
// zero allocation. Binder is opaque user data (e.g. an *EndpointSet or Balancer).
type RouteTable struct {
	mu    sync.RWMutex
	root  *trieNode
	rules map[string]string // ruleID -> prefix (for removal / listing)
}

// NewRouteTable builds an empty route table.
func NewRouteTable() *RouteTable {
	return &RouteTable{root: &trieNode{}, rules: make(map[string]string)}
}

type trieNode struct {
	label    string        // compressed edge label (multi-char, not single byte)
	rule     *routeEntry   // non-nil if a route terminates at this node
	children *[256]*trieNode // sparse array of children, indexed by first byte of child label
}

type routeEntry struct {
	id     string
	prefix string
	binder interface{}
}

// AddRule inserts or updates a route (prefix → binder) under a stable ruleID.
// Hot path: insert walks labels with batch comparisons + one linear scan per node;
// zero allocation aside from the new node. For N paths of avg len L and D branches,
// complexity is roughly O(N×L×D) total across all inserts (vs O(N×L²) for naive).
func (rt *RouteTable) AddRule(id, prefix string, binder interface{}) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	node := rt.root
	i := 0
	for i < len(prefix) {
		c := prefix[i]
		var child *trieNode = nil
		if node.children != nil {
			child = (*node.children)[c]
		}
		if child == nil {
			// No edge starts with c → create chain of new nodes for remaining prefix.
			var parent *trieNode = node
			for j := i; j < len(prefix); j++ {
				newNode := &trieNode{label: string(prefix[j:j+1])}
				if parent.children == nil {
					parent.children = new([256]*trieNode)
				}
				(*parent.children)[prefix[j]] = newNode
				parent = newNode
			}
			parent.rule = &routeEntry{id: id, prefix: prefix, binder: binder}
			rt.rules[id] = prefix
			return
		}
		// Edge exists: consume common prefix length between child.label and remaining path.
		lcp := 0
		for lcp < len(child.label) && i+lcp < len(prefix) && child.label[lcp] == prefix[i+lcp] {
			lcp++
		}
		if lcp == len(child.label) {
			// Child label fully matches next segment: move down, reset label-boundary offset.
			node = child
			i += lcp
			continue
		}
		// Partial match inside child.edge: split child into two nodes.
		// Original child keeps suffix starting at lcp; new node takes prefix up to lcp.
		suffixLabel := child.label[lcp:]
		oldRule := child.rule
		suffixNode := &trieNode{label: suffixLabel, rule: oldRule}
		// Copy children pointers from child to suffixNode
		if child.children != nil {
			if suffixNode.children == nil {
				suffixNode.children = new([256]*trieNode)
			}
			for k := range *child.children {
				(*suffixNode.children)[k] = (*child.children)[k]
			}
		}
		(*node.children)[c].label = child.label[:lcp]
		(*node.children)[c].rule = nil
		(*(*node.children)[c].children)[child.label[lcp]] = suffixNode
		node = node.children[c]
		i += lcp
	}
	node.rule = &routeEntry{id: id, prefix: prefix, binder: binder}
	rt.rules[id] = prefix
}

// RemoveRule deletes a rule by ID. Returns the previous binder and whether found.
// Zero allocation except for map operations during walk.
func (rt *RouteTable) RemoveRule(id string) (interface{}, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	prefix, ok := rt.rules[id]
	if !ok {
		return nil, false
	}
	var foundOld interface{}
	node := rt.root
	remaining := prefix
	for len(remaining) > 0 {
		var child *trieNode = nil
		if node.children != nil {
			child = (*node.children)[remaining[0]]
		}
		if child == nil {
			delete(rt.rules, id)
			return nil, false
		}
		// Consume as much of remaining as child.label covers.
		consume := 0
		maxLen := len(child.label)
		for consume < maxLen && consume < len(remaining) && remaining[consume] == child.label[consume] {
			consume++
		}
		if consume != maxLen || consume == 0 {
			delete(rt.rules, id)
			return nil, false
		}
		remaining = remaining[consume:]
		if len(remaining) == 0 {
			// Arrived at target node.
			if child.rule != nil {
				foundOld = child.rule.binder
				child.rule = nil
			}
		}
		node = child
	}
	if foundOld != nil {
		delete(rt.rules, id)
	}
	return foundOld, foundOld != nil
}

// Match returns the longest registered prefix that is a prefix of path. It is
// the zero-allocation hot path: lock + walk down radix tree via indexed lookups
// and batch label comparisons; no heap allocations on read path.
func (rt *RouteTable) Match(path string) (id string, binder interface{}, ok bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	node := rt.root
	var best *routeEntry
	if node.rule != nil { // root ("") rule = catch-all default
		best = node.rule
	}
	remaining := path
	for len(remaining) > 0 {
		var child *trieNode = nil
		if node.children != nil {
			child = (*node.children)[remaining[0]]
		}
		if child == nil {
			break
		}
		// Compute how much of remaining we can consume along this edge.
		consume := 0
		maxConsume := len(child.label)
		for consume < maxConsume && consume < len(remaining) && remaining[consume] == child.label[consume] {
			consume++
		}
		if consume != maxConsume {
			// Label mismatch inside edge.
			break
		}
		// Edge matched fully: advance, record rule if present.
		if child.rule != nil {
			best = child.rule
		}
		node = child
		remaining = remaining[consume:]
	}
	if best == nil {
		return "", nil, false
	}
	return best.id, best.binder, true
}

// PrefixMatchResult is the ergonomic (allocating) form of Match.
type PrefixMatchResult struct {
	RouteID string
	Prefix  string
	Binder  interface{}
}

// Lookup is the ergonomic wrapper around Match; it allocates one result struct.
func (rt *RouteTable) Lookup(path string) *PrefixMatchResult {
	id, binder, ok := rt.Match(path)
	if !ok {
		return nil
	}
	rt.mu.RLock()
	prefix := rt.rules[id]
	rt.mu.RUnlock()
	return &PrefixMatchResult{RouteID: id, Prefix: prefix, Binder: binder}
}

// RuleCount returns the number of active route rules.
func (rt *RouteTable) RuleCount() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return len(rt.rules)
}

// ============================================================================
// Circuit breaker — lock-free three-state machine
// ============================================================================

// Circuit breaker states.
const (
	cbClosed   uint32 = 0
	cbOpen     uint32 = 1
	cbHalfOpen uint32 = 2
)

// CircuitBreaker enforces fail-fast when failures exceed a threshold. State is a
// single atomic word; the closed-state Allow() hot path is one atomic load with
// zero allocation. Transitions: closed →(failLimit)→ open →(cooldown)→ half-open
// →(successLimit)→ closed.
type CircuitBreaker struct {
	state         atomic.Uint32
	failures      atomic.Int64
	successes     atomic.Int64
	openedAtNanos atomic.Int64
	failureLimit  int64
	successLimit  int64
	cooldown      time.Duration
	// nowNanos is injectable for deterministic tests; nil → time.Now.
	nowNanos func() int64
}

// NewCircuitBreaker configures thresholds and the open→half-open cooldown.
// Defaults: failLimit=5, successLimit=2, cooldown=30s.
func NewCircuitBreaker(failLimit, successLimit int, cooldown time.Duration) *CircuitBreaker {
	if failLimit <= 0 {
		failLimit = 5
	}
	if successLimit <= 0 {
		successLimit = 2
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CircuitBreaker{
		failureLimit: int64(failLimit),
		successLimit: int64(successLimit),
		cooldown:     cooldown,
	}
}

func (cb *CircuitBreaker) now() int64 {
	if cb.nowNanos != nil {
		return cb.nowNanos()
	}
	return time.Now().UnixNano()
}

// Allow reports whether a request may proceed. Closed → always true (single
// atomic load). Open → true only after the cooldown elapses (then transitions
// to half-open via CAS). Half-open → true (probe traffic). Zero allocation.
func (cb *CircuitBreaker) Allow() bool {
	switch cb.state.Load() {
	case cbClosed:
		return true
	case cbHalfOpen:
		return true
	default: // open
		openedAt := cb.openedAtNanos.Load()
		if cb.now()-openedAt >= int64(cb.cooldown) {
			// Attempt open → half-open transition exactly once.
			if cb.state.CompareAndSwap(cbOpen, cbHalfOpen) {
				cb.successes.Store(0)
			}
			return true
		}
		return false
	}
}

// RecordSuccess reports a successful call.
func (cb *CircuitBreaker) RecordSuccess() {
	switch cb.state.Load() {
	case cbHalfOpen:
		if cb.successes.Add(1) >= cb.successLimit {
			cb.state.Store(cbClosed)
			cb.failures.Store(0)
		}
	case cbClosed:
		cb.failures.Store(0)
	}
}

// RecordFailure reports a failed call, tripping the breaker at the threshold.
func (cb *CircuitBreaker) RecordFailure() {
	switch cb.state.Load() {
	case cbClosed:
		if cb.failures.Add(1) >= cb.failureLimit {
			cb.state.Store(cbOpen)
			cb.openedAtNanos.Store(cb.now())
		}
	case cbHalfOpen:
		// Any failure while probing re-opens the circuit.
		cb.state.Store(cbOpen)
		cb.openedAtNanos.Store(cb.now())
	}
}

// State returns "closed", "open", or "half-open".
func (cb *CircuitBreaker) State() string {
	switch cb.state.Load() {
	case cbClosed:
		return "closed"
	case cbOpen:
		return "open"
	case cbHalfOpen:
		return "half-open"
	}
	return "unknown"
}

// ============================================================================
// Retry policy — exponential backoff decision (zero allocation)
// ============================================================================

// RetryPolicy computes retry decisions and backoff without allocating. Attempt
// numbering starts at 0 for the first try.
type RetryPolicy struct {
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	multiplier  uint // integer backoff multiplier (>=2)
}

// NewRetryPolicy configures attempts and exponential backoff bounds.
func NewRetryPolicy(maxAttempts int, base, max time.Duration, multiplier uint) *RetryPolicy {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if multiplier < 2 {
		multiplier = 2
	}
	if base <= 0 {
		base = 10 * time.Millisecond
	}
	if max <= 0 {
		max = time.Second
	}
	return &RetryPolicy{maxAttempts: maxAttempts, baseDelay: base, maxDelay: max, multiplier: multiplier}
}

// ShouldRetry reports whether another attempt is permitted. attempt is the index
// of the try that just failed (0-based). Zero allocation.
func (p *RetryPolicy) ShouldRetry(attempt int) bool {
	return attempt+1 < p.maxAttempts
}

// Backoff returns the delay before the given attempt index using integer
// exponential backoff, clamped to maxDelay. Zero allocation.
func (p *RetryPolicy) Backoff(attempt int) time.Duration {
	d := p.baseDelay
	for i := 0; i < attempt; i++ {
		d *= time.Duration(p.multiplier)
		if d >= p.maxDelay {
			return p.maxDelay
		}
	}
	if d > p.maxDelay {
		return p.maxDelay
	}
	return d
}

// ============================================================================
// Traffic splitter / mirror — deterministic weighted routing (zero allocation)
// ============================================================================

// TrafficSplitter performs deterministic weighted splitting and shadow mirroring
// keyed by a per-request hash. Weights are atomic so they can be updated live
// without locking readers. Decisions are pure integer math — zero allocation.
type TrafficSplitter struct {
	secondaryPct atomic.Uint32 // 0..100 of traffic sent to the canary/secondary
	mirrorPct    atomic.Uint32 // 0..100 of traffic mirrored (shadow) to a sink
}

// NewTrafficSplitter builds a splitter with the given secondary (canary) and
// mirror percentages (0..100).
func NewTrafficSplitter(secondaryPct, mirrorPct uint32) *TrafficSplitter {
	s := &TrafficSplitter{}
	s.SetSecondary(secondaryPct)
	s.SetMirror(mirrorPct)
	return s
}

// SetSecondary updates the canary percentage (clamped to 0..100).
func (s *TrafficSplitter) SetSecondary(pct uint32) {
	if pct > 100 {
		pct = 100
	}
	s.secondaryPct.Store(pct)
}

// SetMirror updates the mirror percentage (clamped to 0..100).
func (s *TrafficSplitter) SetMirror(pct uint32) {
	if pct > 100 {
		pct = 100
	}
	s.mirrorPct.Store(pct)
}

// RouteToPrimary reports whether the keyed request goes to the primary backend
// (true) or the secondary/canary (false). Deterministic in key. Zero allocation.
func (s *TrafficSplitter) RouteToPrimary(key uint64) bool {
	sec := s.secondaryPct.Load()
	if sec == 0 {
		return true
	}
	if sec >= 100 {
		return false
	}
	// Use a well-mixed bucket with strong avalanche (hashString already has finalizer).
	bucket := mix32(uint32(key)) % 100
	return bucket >= sec
}

// ShouldMirror reports whether the keyed request is shadow-mirrored. It uses a
// different fold of the key than RouteToPrimary so mirror and split decisions
// are independent. Zero allocation.
func (s *TrafficSplitter) ShouldMirror(key uint64) bool {
	m := s.mirrorPct.Load()
	if m == 0 {
		return false
	}
	if m >= 100 {
		return true
	}
	// Use a distinct mixed bucket: apply mix64 then mod 100.
	bucket := uint32(mix64(key)%100)
	return bucket < m
}
