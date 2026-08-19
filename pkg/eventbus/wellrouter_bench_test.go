package eventbus

import (
	"crypto/ed25519"
	"testing"
)

// wellrouter_bench_test.go measures single-machine FastRouter throughput,
// latency, and allocation behaviour across the hot routing path: unsigned
// zero-allocation hop-bounded routing versus the same path with Ed25519 signing
// enabled (the Module 6 moat vs opaque NATS/Kafka forwarding).
//
// Metrics reported under `go test -bench=. -benchmem`:
//   - ns/op      wall time per envelope (routing + optional crypto)
//   - allocs/op  heap allocations per op — expected 0 unsigned, 1 signed
//                (the 64-byte slice ed25519.Sign returns; stdlib has no
//                 in-place variant, and we reuse stdlib rather than reimplement)
//   - B/op       heap bytes per op
//   - events/sec derived throughput on this machine (ReportMetric)
//
// The benchmarks isolate the routing core rather than end-to-end bus delivery,
// but exercise the real fabric semantics: hop-bounded TTL, loop prevention via
// the visited bitmask, deterministic fan-out along the connectivity graph, and
// per-envelope Ed25519 signing.

// benchSigner returns a deterministic Ed25519 key for signed-router benchmarks.
func benchSigner() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i*7 + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// releaseSink returns child envelopes to the router's pool immediately, keeping
// the measured op allocation-free in steady state.
func releaseSink(fr *FastRouter) WellSink {
	return func(env *WellEnvelope) { fr.Release(env) }
}

// reportRouterThroughput adds an events/sec metric from the benchmark's measured
// wall time so the validation doc can quote a real throughput number. n is the
// number of envelopes routed per iteration (fan-out width for a single-hop
// Deliver, or the full propagation size).
func reportRouterThroughput(b *testing.B, perOp int) {
	sec := b.Elapsed().Seconds()
	if sec > 0 {
		b.ReportMetric(float64(b.N*perOp)/sec, "events/sec")
	}
}

// BenchmarkFastRouter_Unsigned_SingleHop measures the pure routing core: one
// hop-bounded, loop-checked, deterministic fan-out from L1 (4 downstream wells),
// with no signing. Target: 0 allocs/op (envelopes recycled through sync.Pool).
func BenchmarkFastRouter_Unsigned_SingleHop(b *testing.B) {
	fr := NewFastRouter(MaxWellHops, nil)
	seed, err := fr.Seed(WellIntel, []byte("cve_ingested"))
	if err != nil {
		b.Fatalf("Seed: %v", err)
	}
	defer fr.Release(seed)
	sink := releaseSink(fr)
	fanout := len(connectivity[WellIntel])

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if n, err := fr.Deliver(seed, sink); n != fanout || err != nil {
			b.Fatalf("Deliver: n=%d err=%v", n, err)
		}
	}
	b.StopTimer()
	reportRouterThroughput(b, fanout)
}

// BenchmarkFastRouter_Signed_SingleHop adds Ed25519 signing to the identical
// fan-out. This is the honest end-to-end cost of a self-authenticating
// envelope — the moat. Expected: ~1 alloc/op per signed child (from
// ed25519.Sign), i.e. fanout allocs/op.
func BenchmarkFastRouter_Signed_SingleHop(b *testing.B) {
	fr := NewFastRouter(MaxWellHops, benchSigner())
	seed, err := fr.Seed(WellIntel, []byte("cve_ingested"))
	if err != nil {
		b.Fatalf("Seed: %v", err)
	}
	defer fr.Release(seed)
	sink := releaseSink(fr)
	fanout := len(connectivity[WellIntel])

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if n, err := fr.Deliver(seed, sink); n != fanout || err != nil {
			b.Fatalf("Deliver: n=%d err=%v", n, err)
		}
	}
	b.StopTimer()
	reportRouterThroughput(b, fanout)
}

// BenchmarkFastRouter_Unsigned_FullPropagation measures a complete hop-bounded,
// loop-free BFS across the fabric from L1 with signing disabled. The BFS
// work-queue allocates, so this is not the zero-alloc path; it reports the
// realistic cost and event count of a full 16-well propagation wave.
func BenchmarkFastRouter_Unsigned_FullPropagation(b *testing.B) {
	fr := NewFastRouter(MaxWellHops, nil)
	payload := []byte("finding")

	// Measure the propagation size once for the throughput metric.
	size, err := fr.Propagate(WellIntel, payload, nil)
	if err != nil {
		b.Fatalf("Propagate: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fr.Propagate(WellIntel, payload, nil); err != nil {
			b.Fatalf("Propagate: %v", err)
		}
	}
	b.StopTimer()
	reportRouterThroughput(b, size)
}

// BenchmarkFastRouter_Signed_FullPropagation repeats the full propagation with
// Ed25519 signing on every envelope — the full-fabric moat cost.
func BenchmarkFastRouter_Signed_FullPropagation(b *testing.B) {
	fr := NewFastRouter(MaxWellHops, benchSigner())
	payload := []byte("finding")

	size, err := fr.Propagate(WellIntel, payload, nil)
	if err != nil {
		b.Fatalf("Propagate: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fr.Propagate(WellIntel, payload, nil); err != nil {
			b.Fatalf("Propagate: %v", err)
		}
	}
	b.StopTimer()
	reportRouterThroughput(b, size)
}

// BenchmarkFastRouter_Verify measures signature verification throughput — the
// receiver-side cost a subscriber pays to trust an envelope without contacting
// the sender, which opaque brokers cannot offer.
func BenchmarkFastRouter_Verify(b *testing.B) {
	fr := NewFastRouter(MaxWellHops, benchSigner())
	seed, err := fr.Seed(WellIntel, []byte("cve_ingested"))
	if err != nil {
		b.Fatalf("Seed: %v", err)
	}
	defer fr.Release(seed)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !fr.Verify(seed) {
			b.Fatal("Verify returned false")
		}
	}
	b.StopTimer()
	reportRouterThroughput(b, 1)
}

// BenchmarkFastRouter_Unsigned_SingleHop_Parallel measures routing under
// contention across all CPUs. Each goroutine keeps its own seed envelope; the
// shared router state is only the pool and atomic counters, so throughput
// should scale with cores while allocs/op stays at 0.
func BenchmarkFastRouter_Unsigned_SingleHop_Parallel(b *testing.B) {
	fr := NewFastRouter(MaxWellHops, nil)
	sink := releaseSink(fr)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		seed, err := fr.Seed(WellIntel, []byte("cve_ingested"))
		if err != nil {
			b.Fatalf("Seed: %v", err)
		}
		defer fr.Release(seed)
		for pb.Next() {
			if _, err := fr.Deliver(seed, sink); err != nil {
				b.Fatalf("Deliver: %v", err)
			}
		}
	})
}
