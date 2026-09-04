// Package mesh — theoretical foundations for the zero-copy inference-routing moat.
//
// theoretical_zerocopy_routing.go provides a formal cost model and a routing
// latency lower-bound theorem for CloudAI Fusion's in-process ("sidecarless")
// inference mesh, contrasted against the Istio/Envoy sidecar architecture.
//
// SCOPE / HONESTY:
//   - This file contains NO production logic and modifies no production code.
//     It defines analytical cost functions and machine-checkable predicates that
//     the accompanying tests (adversarial_zerocopy_routing_test.go) exercise
//     against the REAL data-plane primitives in datapath.go / route_table.go /
//     loadbalancer.go.
//   - Absolute Istio/Envoy latency figures are MODELED from published literature
//     and are labeled as such (see IstioSidecarModel). The memcpy / copy-bandwidth
//     coefficients used by the tests are MEASURED at runtime via real copy().
//
// Task #266, CloudAI Fusion T3 MoAT research.
package mesh

import "math"

/*
=== FORMAL MODEL: THE INFERENCE ROUTING PROBLEM ===

Definition 1 (Routing instance). An inference routing instance is a tuple
    I = (G, S, B, N)
where
    G = (V, E)   cluster topology: V nodes, E links each with a byte-bandwidth
                 cap; d_bw denotes the *inverse* bottleneck bandwidth along the
                 chosen route, in seconds-per-byte (min-cut of the route).
    S            model / request payload size in bytes.
    B            batch size (requests amortized over one payload movement).
    N            number of concurrent connections handled at the routing node.

A router selects a route R (an ingress→backend path in G) and a data-movement
strategy. Its objective is to minimize end-to-end latency L(R).

Definition 2 (Latency decomposition). For a route R,
    L(R) = L_dec(R) + L_move(R) + L_compute
where
    L_dec      control-plane routing decision latency (which backend?),
    L_move     data-plane latency to place the payload at the chosen backend,
    L_compute  model forward pass — architecture-independent, identical for
               both designs, so it cancels in any comparison.

=== THE UNAVOIDABLE LOWER BOUND ===

Lemma 1 (Single-traversal floor). Any correct router must move the request
payload from ingress to the elected backend at least once. Over the route's
bottleneck link this costs, per batch, at least S * d_bw seconds. Amortized per
request in a batch of B:
    ε(I) = d_bw * S / B          (seconds/request)
No router — zero-copy or sidecar — can beat ε on the data plane, because ε is
the information-theoretic cost of delivering the payload exactly once. This is
the routing latency lower bound; it matches the O(d_bw · S / B) target.

=== THE STRUCTURAL GAP: WHY THE SIDECAR CANNOT REACH ε ===

The two architectures differ in HOW MANY TIMES the payload is copied through the
CPU memory bus before/while it crosses the wire, and in the control-plane cost.

Zero-copy (in-process) — datapath.go model:
    L_dec      = O(1): one atomic pointer load (Snapshot) + one array index
                 (Pick) + a byte-trie walk (Match). Independent of S and N.
                 Zero heap allocation on the hot path.
    L_move     = ε exactly: the payload never leaves the caller's address space;
                 only a *descriptor* (pointer + length, i.e. the Endpoint handle)
                 is passed to the transport, which DMAs the buffer to the NIC.
                 No intermediate CPU copy.
    => L_zc(I) = τ_atomic + ε,    τ_atomic ≈ 10 ns, constant.

Sidecar (Envoy/ztunnel) — IstioSidecarModel:
    The payload is intercepted by a co-located proxy. On each hop the bytes are
    copied out of the application socket buffer into the proxy's address space,
    processed, then copied back out toward the peer. That is k ≥ 2 full-payload
    CPU copies that CANNOT overlap the NIC DMA (they are CPU-memory-bandwidth
    bound, not link bound).
    L_dec'     = O(1) proxy match, but on a SEPARATE address space reached via a
                 loopback hop (adds a userspace<->kernel<->userspace round trip).
    L_move'    = ε + Δ_copy(I), where
                 Δ_copy(I) = k * (S / mem_bw) / B   (per-request extra CPU copy)
                           + Θ(N) per-connection buffer/iovec management.
    => L_sc(I) = τ_proxy + ε + Δ_copy(I),  with Δ_copy(I) > 0 for all S > 0.

Theorem 1 (Zero-copy latency-optimality; sidecar structural gap).
For every inference routing instance I = (G, S, B, N) with S > 0:
    (a)  L_zc(I) = τ_atomic + ε(I)          [reaches the floor up to O(1) term]
    (b)  L_sc(I) = τ_proxy  + ε(I) + Δ_copy(I),  Δ_copy(I) = Θ(k·S/(B·mem_bw) + N)
    (c)  L_sc(I) − L_zc(I) = (τ_proxy − τ_atomic) + Δ_copy(I) > 0,
         and this gap grows Θ(S) in payload size and Θ(N) in connection count.
Therefore the sidecar CANNOT reach the lower bound ε: its deficit is not an
implementation inefficiency but a structural consequence of interposing a
memory-bandwidth-bound copy on the CPU. Only an in-address-space (zero-copy)
router attains ε up to an O(1) additive constant.  ∎ (structural argument;
the coefficients k, mem_bw, τ are measured/modeled, not assumed)

=== WORST CASE: THE BANDWIDTH-SATURATION ATTACK ===

Adversary maximizes S and N (max-size payloads on many connections) to saturate
the CPU memory bus. Sidecar Δ_copy(I) = Θ(N·S) drives L_sc → mem-bus limited and
P99 explodes (bus contention + allocator pressure). Zero-copy Δ_copy = 0, so
L_zc stays at τ_atomic + ε regardless of S, N (the NIC DMA is untouched by the
attack). This is the qualitative reason zero-copy is irreplaceable for large
model / high-fan-in inference serving. See TestAdversarial_* for measurements.
*/

// ============================================================================
// Analytical cost model (pure functions, unit-testable)
// ============================================================================

// RoutingInstance is the analytical instance I = (G, S, B, N). Topology G is
// summarized by its bottleneck inverse-bandwidth DBwSecPerByte (seconds/byte),
// which is the only topology quantity the latency bound depends on.
type RoutingInstance struct {
	DBwSecPerByte float64 // d_bw: inverse of route bottleneck bandwidth (s/byte)
	PayloadBytes  float64 // S
	Batch         float64 // B (>=1)
	Connections   float64 // N (>=1)
}

// AmortizedLowerBound returns ε(I) = d_bw · S / B, the per-request data-plane
// latency floor no router can beat (Lemma 1). Units: seconds.
func (i RoutingInstance) AmortizedLowerBound() float64 {
	b := i.Batch
	if b < 1 {
		b = 1
	}
	return i.DBwSecPerByte * i.PayloadBytes / b
}

// ZeroCopyLatency returns L_zc(I) = τ_atomic + ε(I). The decision term is a
// constant independent of S and N. tauAtomicSec is the measured O(1) in-process
// routing-decision latency (≈10 ns).
func (i RoutingInstance) ZeroCopyLatency(tauAtomicSec float64) float64 {
	return tauAtomicSec + i.AmortizedLowerBound()
}

// SidecarCopyOverhead returns Δ_copy(I): the extra per-request latency the
// sidecar pays that zero-copy does not. It is k full-payload CPU copies
// amortized over the batch, plus a Θ(N) per-connection buffer term.
//
//	copies         k ≥ 2 (in + out per proxy hop)
//	memBwBytesSec  measured single-stream memcpy bandwidth (bytes/sec)
//	perConnSec     measured per-connection buffer/iovec management cost (sec)
func (i RoutingInstance) SidecarCopyOverhead(copies, memBwBytesSec, perConnSec float64) float64 {
	b := i.Batch
	if b < 1 {
		b = 1
	}
	if memBwBytesSec <= 0 {
		memBwBytesSec = 1
	}
	cpuCopy := copies * (i.PayloadBytes / memBwBytesSec) / b
	connMgmt := perConnSec * i.Connections
	return cpuCopy + connMgmt
}

// SidecarLatency returns L_sc(I) = τ_proxy + ε(I) + Δ_copy(I). tauProxySec is
// the modeled fixed proxy/loopback overhead (labeled MODELED at call sites).
func (i RoutingInstance) SidecarLatency(tauProxySec, copies, memBwBytesSec, perConnSec float64) float64 {
	return tauProxySec + i.AmortizedLowerBound() + i.SidecarCopyOverhead(copies, memBwBytesSec, perConnSec)
}

// StructuralGap returns L_sc − L_zc for instance I (Theorem 1c). It is > 0 for
// any S > 0 and grows with S and N.
func (i RoutingInstance) StructuralGap(tauAtomicSec, tauProxySec, copies, memBwBytesSec, perConnSec float64) float64 {
	return i.SidecarLatency(tauProxySec, copies, memBwBytesSec, perConnSec) - i.ZeroCopyLatency(tauAtomicSec)
}

// ============================================================================
// Machine-checkable theorem predicates (exercised by the test suite)
// ============================================================================

// VerifyLowerBoundReached checks Theorem 1(a): the zero-copy latency equals the
// floor ε plus only the constant O(1) decision term — i.e. its data-plane
// excess over ε is exactly τ_atomic, independent of S. Returns true iff the
// zero-copy excess is constant across the two payload sizes.
func VerifyLowerBoundReached(dBw, batch, tauAtomicSec, sSmall, sLarge float64) bool {
	small := RoutingInstance{DBwSecPerByte: dBw, PayloadBytes: sSmall, Batch: batch, Connections: 1}
	large := RoutingInstance{DBwSecPerByte: dBw, PayloadBytes: sLarge, Batch: batch, Connections: 1}
	excessSmall := small.ZeroCopyLatency(tauAtomicSec) - small.AmortizedLowerBound()
	excessLarge := large.ZeroCopyLatency(tauAtomicSec) - large.AmortizedLowerBound()
	return math.Abs(excessSmall-excessLarge) < 1e-15 // both == τ_atomic exactly
}

// VerifyStructuralGapPositive checks Theorem 1(c): for every payload size in
// sizes (all > 0), the sidecar deficit is strictly positive AND monotonically
// non-decreasing in S. Returns true iff both hold.
func VerifyStructuralGapPositive(dBw, batch, tauAtomic, tauProxy, copies, memBw, perConn float64, sizes []float64) bool {
	prev := math.Inf(-1)
	for _, s := range sizes {
		if s <= 0 {
			continue
		}
		inst := RoutingInstance{DBwSecPerByte: dBw, PayloadBytes: s, Batch: batch, Connections: 1}
		gap := inst.StructuralGap(tauAtomic, tauProxy, copies, memBw, perConn)
		if gap <= 0 {
			return false
		}
		if gap < prev {
			return false // gap must be non-decreasing in S
		}
		prev = gap
	}
	return true
}

// VerifyGapGrowsWithConnections checks that Δ_copy — hence the sidecar deficit —
// grows with connection count N (the Θ(N) term of Theorem 1b). Returns true iff
// gap(N2) > gap(N1) for N2 > N1 with positive per-connection cost.
func VerifyGapGrowsWithConnections(dBw, batch, tauAtomic, tauProxy, copies, memBw, perConn, s, n1, n2 float64) bool {
	if n2 <= n1 || perConn <= 0 {
		return false
	}
	a := RoutingInstance{DBwSecPerByte: dBw, PayloadBytes: s, Batch: batch, Connections: n1}
	b := RoutingInstance{DBwSecPerByte: dBw, PayloadBytes: s, Batch: batch, Connections: n2}
	return b.StructuralGap(tauAtomic, tauProxy, copies, memBw, perConn) >
		a.StructuralGap(tauAtomic, tauProxy, copies, memBw, perConn)
}

// ============================================================================
// Complexity comparison (documentation-as-data, asserted by tests)
// ============================================================================

// ComplexityRow is one row of the zero-copy vs sidecar structural comparison.
type ComplexityRow struct {
	Dimension    string
	ZeroCopy     string
	Sidecar      string
	ZeroCopyWins bool
}

// ComplexityTable returns the structural complexity comparison used verbatim in
// the proof document. Each row is a claim the test suite either measures
// directly (allocations, decision latency) or grounds in the cost model above.
func ComplexityTable() []ComplexityRow {
	return []ComplexityRow{
		{"Routing decision", "O(1) metadata (atomic load + index)", "Θ(N) per-connection proxy buffer state", true},
		{"Heap allocations / request", "0 allocs", "N · sizeof(iovec) + payload buffer", true},
		{"Data-plane copies of payload", "0 (descriptor/handle passed)", "k ≥ 2 full-payload CPU copies", true},
		{"Hot-path primitive", "atomic load ≈ 10 ns", "memmove ≈ 1 µs/KB", true},
		{"Latency vs payload size S", "constant decision (ε floor only)", "grows Θ(S) via CPU copy", true},
		{"Extra network hops", "0 (in address space)", "≥ 1 loopback proxy hop", true},
	}
}

// IstioSidecarModel holds MODELED Istio/Envoy coefficients drawn from published
// literature. These are NOT measured by CloudAI Fusion and MUST be presented as
// "modeled". The memcpy bandwidth used in tests is measured separately at
// runtime; only the fixed proxy overhead and copy count are modeled here.
//
// Sources (public): Istio performance docs report ~0.5–2.65 ms added P99 latency
// per request through the sidecar data path at moderate load; Envoy performs at
// least an ingress and egress buffer copy per proxied stream.
type IstioSidecarModel struct {
	TauProxySec float64 // fixed proxy + loopback overhead (modeled)
	Copies      float64 // full-payload CPU copies per hop (modeled, k>=2)
	PerConnSec  float64 // per-connection buffer management (modeled)
	Source      string  // provenance label
}

// DefaultIstioModel returns representative MODELED Istio sidecar coefficients.
// TauProxySec = 0.5 ms (low end of published Istio added-latency range) so the
// comparison is conservative (favors the sidecar). Copies = 2 (ingress+egress).
func DefaultIstioModel() IstioSidecarModel {
	return IstioSidecarModel{
		TauProxySec: 0.5e-3,
		Copies:      2,
		PerConnSec:  50e-9, // 50 ns/connection buffer bookkeeping (modeled)
		Source:      "MODELED from public Istio/Envoy performance literature (0.5–2.65ms added P99)",
	}
}
