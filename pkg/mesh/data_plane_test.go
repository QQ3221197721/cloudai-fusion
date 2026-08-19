package mesh

import (
	"fmt"
	"testing"
	"time"
)

// splitmix64 is a tiny deterministic PRNG used only for statistical sampling in
// tests/benchmarks (distribution uniformity, split ratios). It is NOT used for
// any security-sensitive purpose — no tokens, keys, or secrets.
type splitmix64 struct{ s uint64 }

func (r *splitmix64) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// ============================================================================
// Service discovery tests — snapshot isolation and atomic ops
// ============================================================================

func TestEndpointSet_ReplacePublishesNewSnapshot(t *testing.T) {
	set := NewEndpointSet(NewEndpoint("e1", "1.1.1.1:8080", 1), NewEndpoint("e2", "2.2.2.2:8080", 2))
	if set.Len() != 2 {
		t.Fatalf("expected 2 endpoints, got %d", set.Len())
	}
	old := set.Snapshot()
	set.Replace(nil)
	if set.Len() != 0 {
		t.Fatalf("expected 0 after Replace(nil), got %d", set.Len())
	}
	// The previously held snapshot must be untouched (copy-on-write isolation).
	if len(old) != 2 {
		t.Fatalf("held snapshot must remain length 2, got %d", len(old))
	}
}

func TestEndpointSet_AddRemoveSetHealth(t *testing.T) {
	set := NewEndpointSet(NewEndpoint("a", "a:80", 1))
	set.Add(NewEndpoint("b", "b:80", 1))
	if set.Len() != 2 {
		t.Fatalf("expected 2 after Add, got %d", set.Len())
	}
	set.Add(NewEndpoint("a", "a:81", 3)) // replace existing by ID
	for _, e := range set.Snapshot() {
		if e.ID == "a" && e.Weight != 3 {
			t.Fatalf("expected replaced weight 3, got %d", e.Weight)
		}
	}
	set.SetHealth("b", false)
	for _, e := range set.Snapshot() {
		if e.ID == "b" && e.Healthy {
			t.Fatal("expected b unhealthy")
		}
	}
	set.Remove("a")
	if set.Len() != 1 {
		t.Fatalf("expected 1 after Remove, got %d", set.Len())
	}
}

// TestEndpointSet_ConcurrentReadWrite exercises the lock-free read path against
// concurrent writers to catch races under -race.
func TestEndpointSet_ConcurrentReadWrite(t *testing.T) {
	set := NewEndpointSet(NewEndpoint("a", "a:80", 1), NewEndpoint("b", "b:80", 1))
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20000; i++ {
			set.Replace([]*Endpoint{NewEndpoint("a", "a:80", 1)})
		}
		close(done)
	}()
	for i := 0; i < 20000; i++ {
		_ = set.Snapshot()
	}
	<-done
}

func TestRegistry_AtomicRegisterLookup(t *testing.T) {
	r := NewRegistry()
	r.Register("svc-a", NewEndpointSet(NewEndpoint("e1", "x:80", 1)))
	r.Register("svc-b", NewEndpointSet(NewEndpoint("e2", "y:80", 1)))
	if r.Services() != 2 {
		t.Fatalf("expected 2 services, got %d", r.Services())
	}
	if a := r.Lookup("svc-a"); a == nil || a.Len() != 1 {
		t.Fatal("svc-a lookup failed")
	}
	if r.Lookup("svc-missing") != nil {
		t.Fatal("expected nil for unknown service")
	}
}

// ============================================================================
// Load balancer correctness
// ============================================================================

func TestRoundRobin_EvenDistribution(t *testing.T) {
	eps := []*Endpoint{NewEndpoint("e1", "1:80", 1), NewEndpoint("e2", "2:80", 1), NewEndpoint("e3", "3:80", 1)}
	set := NewEndpointSet(eps...)
	b := NewRoundRobin()
	counts := map[string]int{}
	for i := 0; i < 3000; i++ {
		ep, ok := b.Pick(set.Snapshot(), 0)
		if !ok {
			t.Fatal("expected pick")
		}
		counts[ep.ID]++
	}
	for id, c := range counts {
		if c != 1000 {
			t.Errorf("round-robin uneven: %s got %d, want 1000", id, c)
		}
	}
}

func TestRoundRobin_SkipsUnhealthy(t *testing.T) {
	set := NewEndpointSet(NewEndpoint("e1", "1:80", 1), NewEndpoint("e2", "2:80", 1))
	set.SetHealth("e1", false)
	b := NewRoundRobin()
	for i := 0; i < 100; i++ {
		ep, ok := b.Pick(set.Snapshot(), 0)
		if !ok || ep.ID != "e2" {
			t.Fatalf("expected only healthy e2, got ok=%v ep=%v", ok, ep)
		}
	}
	// All unhealthy → no pick.
	set.SetHealth("e2", false)
	if _, ok := b.Pick(set.Snapshot(), 0); ok {
		t.Fatal("expected no pick when all unhealthy")
	}
}

func TestLeastConn_PicksLowestLoad(t *testing.T) {
	e1 := NewEndpoint("e1", "1:80", 1)
	e2 := NewEndpoint("e2", "2:80", 1)
	set := NewEndpointSet(e1, e2)
	b := NewLeastConn()
	// Put two in-flight requests on e1 → least-conn must pick e2.
	rel1 := e1.Acquire()
	rel2 := e1.Acquire()
	ep, ok := b.Pick(set.Snapshot(), 0)
	if !ok || ep.ID != "e2" {
		t.Fatalf("expected e2 (lower load), got ok=%v ep=%v", ok, ep)
	}
	rel1()
	rel2()
}

func TestLeastConn_WeightNormalizes(t *testing.T) {
	// e1 weight 4 with 2 in-flight → score 0.5; e2 weight 1 with 1 in-flight → score 1.0.
	e1 := NewEndpoint("e1", "1:80", 4)
	e2 := NewEndpoint("e2", "2:80", 1)
	set := NewEndpointSet(e1, e2)
	b := NewLeastConn()
	e1.Acquire()
	e1.Acquire()
	e2.Acquire()
	ep, ok := b.Pick(set.Snapshot(), 0)
	if !ok || ep.ID != "e1" {
		t.Fatalf("expected e1 (lower weighted load 0.5 vs 1.0), got ep=%v", ep)
	}
}

func TestConsistentHash_RingSizeWithWeights(t *testing.T) {
	eps := []*Endpoint{NewEndpoint("e1", "1:80", 1), NewEndpoint("e2", "2:80", 2)}
	ring := NewConsistentHashRing(eps, 100)
	// e1: 100 vnodes, e2: 200 vnodes → 300 total.
	if ring.Size() != 300 {
		t.Fatalf("expected 300 vnodes (weighted), got %d", ring.Size())
	}
}

// TestConsistentHash_SameKeySameOwner verifies determinism.
func TestConsistentHash_SameKeySameOwner(t *testing.T) {
	eps := []*Endpoint{NewEndpoint("e1", "1:80", 1), NewEndpoint("e2", "2:80", 1), NewEndpoint("e3", "3:80", 1)}
	ring := NewConsistentHashRing(eps, 160)
	first, _ := ring.PickKey("session-abc-123")
	for i := 0; i < 1000; i++ {
		ep, ok := ring.PickKey("session-abc-123")
		if !ok || ep.ID != first.ID {
			t.Fatalf("consistent hash not deterministic: %v vs %v", first, ep)
		}
	}
}

// TestConsistentHash_DistributionUniformity measures how evenly 1M keys spread
// across 8 endpoints (should be within a few % of the 12.5% ideal).
func TestConsistentHash_DistributionUniformity(t *testing.T) {
	const n = 8
	eps := make([]*Endpoint, n)
	for i := range eps {
		eps[i] = NewEndpoint(fmt.Sprintf("e%d", i), fmt.Sprintf("10.0.0.%d:80", i), 1)
	}
	ring := NewConsistentHashRing(eps, 200)
	t.Logf("consistent-hash ring size: %d vnodes for %d real endpoints (%dx avg)", ring.Size(), n, ring.Size()/n)
	dist := make([]int, n)
	idx := map[string]int{}
	for i, e := range eps {
		idx[e.ID] = i
	}
	const total = 1_000_000
	rng := splitmix64{s: 42}
	// Diagnostic: first 3 keys should land on different owners.
	var firstKeys [3]int
	for i := 0; i < 3; i++ {
		k := rng.next()
		ep, ok := ring.Pick(eps, k)
		if !ok {
			t.Fatal("expected pick")
		}
		firstKeys[i] = idx[ep.ID]
	}
	t.Logf("first 3 keys owner indices: %v", firstKeys[:])
	for i := 0; i < total; i++ {
		ep, ok := ring.Pick(eps, rng.next())
		if !ok {
			t.Fatal("expected pick")
		}
		dist[idx[ep.ID]]++
	}
	ideal := float64(total) / float64(n)
	maxDev := 0.0
	for _, c := range dist {
		dev := (float64(c) - ideal) / ideal * 100
		if dev < 0 {
			dev = -dev
		}
		if dev > maxDev {
			maxDev = dev
		}
	}
	t.Logf("consistent-hash 8-way distribution: %v (ideal=%.0f/bucket, max deviation=%.2f%%)", dist, ideal, maxDev)
	if maxDev > 15 {
		t.Fatalf("distribution too skewed: max deviation %.2f%% > 15%%", maxDev)
	}
}

// TestConsistentHash_StabilityVsModulo is the core moat proof: when one node of
// N is removed, consistent hashing remaps ~1/N of keys, whereas modulo/random
// selection remaps ~(N-1)/N of keys.
func TestConsistentHash_StabilityVsModulo(t *testing.T) {
	const n = 10
	before := make([]*Endpoint, n)
	for i := range before {
		before[i] = NewEndpoint(fmt.Sprintf("e%d", i), fmt.Sprintf("10.0.1.%d:80", i), 1)
	}
	after := before[:n-1] // remove the last endpoint

	ringBefore := NewConsistentHashRing(before, 200)
	ringAfter := NewConsistentHashRing(after, 200)

	const total = 200_000
	chRemap := 0
	modRemap := 0
	rng := splitmix64{s: 7}
	for i := 0; i < total; i++ {
		k := rng.next()
		// Consistent hash.
		b0, _ := ringBefore.Pick(before, k)
		a0, _ := ringAfter.Pick(after, k)
		if b0.ID != a0.ID {
			chRemap++
		}
		// Modulo selection (the naive baseline).
		if int(k%uint64(n)) != int(k%uint64(n-1)) {
			modRemap++
		}
	}
	chPct := float64(chRemap) / total * 100
	modPct := float64(modRemap) / total * 100
	t.Logf("node removal 10→9: consistent-hash remapped %.2f%% of keys; modulo remapped %.2f%% of keys", chPct, modPct)
	// Consistent hash should move roughly 1/N (~10%); modulo moves the large majority.
	if chPct > 20 {
		t.Fatalf("consistent hash remapped too many keys: %.2f%%", chPct)
	}
	if modPct < chPct {
		t.Fatalf("modulo (%.2f%%) should remap more than consistent-hash (%.2f%%)", modPct, chPct)
	}
}

// ============================================================================
// Route table correctness (longest-prefix match semantics)
// ============================================================================

func TestRouteTable_LongestPrefixMatch(t *testing.T) {
	rt := NewRouteTable()
	rt.AddRule("users", "/api/users", "users-service")
	rt.AddRule("api", "/api", "api-gateway")
	rt.AddRule("health", "/health", "probe-service")

	cases := []struct {
		path     string
		wantID   string
		wantBind interface{}
		wantOk   bool
	}{
		{"/api/users/profile", "users", "users-service", true}, // longest prefix wins
		{"/api/orders", "api", "api-gateway", true},
		{"/api", "api", "api-gateway", true},
		{"/health", "health", "probe-service", true},
		{"/healthz", "health", "probe-service", true}, // prefix match (like Envoy prefix route)
		{"/metrics", "", nil, false},                  // no matching prefix
	}
	for _, tc := range cases {
		id, binder, ok := rt.Match(tc.path)
		if ok != tc.wantOk || id != tc.wantID || binder != tc.wantBind {
			t.Errorf("Match(%q)=(%q,%v,%v), want (%q,%v,%v)", tc.path, id, binder, ok, tc.wantID, tc.wantBind, tc.wantOk)
		}
	}
}

func TestRouteTable_CatchAllRoot(t *testing.T) {
	rt := NewRouteTable()
	rt.AddRule("default", "", "fallback") // empty prefix = catch-all
	rt.AddRule("api", "/api", "api-service")
	if id, b, ok := rt.Match("/anything/else"); !ok || id != "default" || b != "fallback" {
		t.Fatalf("catch-all failed: id=%q b=%v ok=%v", id, b, ok)
	}
	if id, _, ok := rt.Match("/api/x"); !ok || id != "api" {
		t.Fatalf("expected /api to override catch-all, got id=%q ok=%v", id, ok)
	}
}

func TestRouteTable_UpdateAndRemove(t *testing.T) {
	rt := NewRouteTable()
	rt.AddRule("r1", "/api/v1", "old-v1")
	rt.AddRule("r1", "/api/v1", "new-v1") // update in place
	if _, bind, ok := rt.Match("/api/v1"); !ok || bind != "new-v1" {
		t.Fatalf("update failed: bind=%v ok=%v", bind, ok)
	}
	old, ok := rt.RemoveRule("r1")
	if !ok || old != "new-v1" {
		t.Fatalf("remove failed: ok=%v old=%v", ok, old)
	}
	if _, _, ok := rt.Match("/api/v1"); ok {
		t.Fatal("rule still present after remove")
	}
	if _, ok := rt.RemoveRule("missing"); ok {
		t.Fatal("removing missing rule should return false")
	}
}

// ============================================================================
// Circuit breaker correctness (deterministic clock)
// ============================================================================

func TestCircuitBreaker_TripAndRecover(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 5*time.Second)
	var clock int64
	cb.nowNanos = func() int64 { return clock }

	if !cb.Allow() || cb.State() != "closed" {
		t.Fatal("expected initial closed/allow")
	}
	// Three failures trips the breaker.
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != "closed" {
		t.Fatalf("should still be closed after 2 failures, got %s", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatalf("expected open after 3 failures, got %s", cb.State())
	}
	if cb.Allow() {
		t.Fatal("open breaker must block before cooldown")
	}
	// Advance past cooldown → half-open probe allowed.
	clock += int64(5 * time.Second)
	if !cb.Allow() {
		t.Fatal("expected half-open probe to be allowed after cooldown")
	}
	if cb.State() != "half-open" {
		t.Fatalf("expected half-open, got %s", cb.State())
	}
	// Two successes → closed.
	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.State() != "closed" {
		t.Fatalf("expected closed after successLimit, got %s", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(1, 2, time.Second)
	var clock int64
	cb.nowNanos = func() int64 { return clock }
	cb.RecordFailure() // trips (failLimit=1)
	clock += int64(time.Second)
	cb.Allow() // → half-open
	cb.RecordFailure()
	if cb.State() != "open" {
		t.Fatalf("half-open failure must reopen, got %s", cb.State())
	}
}

// ============================================================================
// Retry policy correctness
// ============================================================================

func TestRetryPolicy_ShouldRetryAndBackoff(t *testing.T) {
	p := NewRetryPolicy(3, 10*time.Millisecond, 500*time.Millisecond, 2)
	if !p.ShouldRetry(0) || !p.ShouldRetry(1) {
		t.Fatal("attempts 0,1 should permit retry (max=3)")
	}
	if p.ShouldRetry(2) {
		t.Fatal("attempt 2 is the last try; no further retry")
	}
	if got := p.Backoff(0); got != 10*time.Millisecond {
		t.Fatalf("Backoff(0)=%v want 10ms", got)
	}
	if got := p.Backoff(1); got != 20*time.Millisecond {
		t.Fatalf("Backoff(1)=%v want 20ms", got)
	}
	if got := p.Backoff(10); got != 500*time.Millisecond {
		t.Fatalf("Backoff(10)=%v want clamp 500ms", got)
	}
}

// ============================================================================
// Traffic splitter / mirror correctness
// ============================================================================

func TestTrafficSplitter_SplitRatio(t *testing.T) {
	s := NewTrafficSplitter(30, 0) // 30% to secondary
	var primary, secondary int
	rng := splitmix64{s: 99}
	const total = 500_000
	for i := 0; i < total; i++ {
		if s.RouteToPrimary(rng.next()) {
			primary++
		} else {
			secondary++
		}
	}
	pct := float64(secondary) / total * 100
	t.Logf("traffic split: primary=%.2f%% secondary=%.2f%% (target secondary=30%%)", float64(primary)/total*100, pct)
	if pct < 27 || pct > 33 {
		t.Fatalf("secondary split %.2f%% outside 27-33%% tolerance", pct)
	}
}

func TestTrafficSplitter_MirrorRatioAndEdges(t *testing.T) {
	s := NewTrafficSplitter(0, 10) // 10% mirror
	var mirrored int
	rng := splitmix64{s: 123}
	const total = 500_000
	for i := 0; i < total; i++ {
		if s.ShouldMirror(rng.next()) {
			mirrored++
		}
	}
	pct := float64(mirrored) / total * 100
	t.Logf("mirror ratio: %.2f%% (target 10%%)", pct)
	if pct < 8 || pct > 12 {
		t.Fatalf("mirror %.2f%% outside 8-12%% tolerance", pct)
	}
	// Edge cases.
	if !NewTrafficSplitter(0, 0).RouteToPrimary(12345) {
		t.Fatal("0% secondary must always route primary")
	}
	if NewTrafficSplitter(100, 0).RouteToPrimary(12345) {
		t.Fatal("100% secondary must never route primary")
	}
	if NewTrafficSplitter(0, 100).ShouldMirror(12345) != true {
		t.Fatal("100% mirror must always mirror")
	}
}
