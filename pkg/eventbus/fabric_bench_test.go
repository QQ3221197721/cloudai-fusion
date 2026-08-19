package eventbus

import (
	"context"
	"strconv"
	"testing"
)

// fabric_bench_test.go measures single-machine routing throughput and latency
// for the Event Message Fabric. Three variants isolate the cost of evidence
// signing (Ed25519), which dominates the consume path.

// benchWellEvent builds a reusable below-cap well event for forwarding
// benchmarks (hop 0 => forwards to downstream wells).
func benchWellEvent(b *testing.B, well DeepWell, hop int) *Event {
	b.Helper()
	ev, err := NewEvent(TopicWellEvent, "bench", well.String(), WellEvent{Well: well, Kind: "bench"})
	if err != nil {
		b.Fatalf("NewEvent: %v", err)
	}
	ev.WithMetadata(mdWell, strconv.Itoa(int(well))).
		WithMetadata(mdWellName, well.String()).
		WithMetadata(mdHop, strconv.Itoa(hop))
	return ev
}

// BenchmarkFabric_Forward measures the pure single-hop forwarding primitive
// (metadata + event allocation + Publish) with no subscribers and no evidence.
func BenchmarkFabric_Forward(b *testing.B) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())
	ev := benchWellEvent(b, WellIntel, 0)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Forward(ctx, ev, WellHunt); err != nil {
			b.Fatalf("Forward: %v", err)
		}
	}
	b.StopTimer()
	reportThroughput(b)
}

// BenchmarkFabric_RouteEvent_NoEvidence measures full RouteEvent (well parse +
// L8 branch + downstream fan-out) without evidence signing. The event sits at
// the terminal hop so it does not forward, isolating the routing decision.
func BenchmarkFabric_RouteEvent_NoEvidence(b *testing.B) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())
	r.SetL8Consumer(func(context.Context, *Event) error { return nil })
	ev := benchWellEvent(b, WellResponse, MaxWellHops)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.RouteEvent(ctx, ev); err != nil {
			b.Fatalf("RouteEvent: %v", err)
		}
	}
	b.StopTimer()
	reportThroughput(b)
}

// BenchmarkFabric_RouteEvent_WithEvidence measures the full consume path with
// Ed25519 receipt signing enabled — the honest end-to-end cost per consumed
// event on the fabric.
func BenchmarkFabric_RouteEvent_WithEvidence(b *testing.B) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())
	r.SetEvidence(testReceiptBuilder())
	r.SetL8Consumer(func(context.Context, *Event) error { return nil })
	ev := benchWellEvent(b, WellResponse, MaxWellHops)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.RouteEvent(ctx, ev); err != nil {
			b.Fatalf("RouteEvent: %v", err)
		}
	}
	b.StopTimer()
	reportThroughput(b)
}

// reportThroughput adds an events/sec metric derived from the benchmark's
// measured wall time so the report can quote a real throughput number.
func reportThroughput(b *testing.B) {
	sec := b.Elapsed().Seconds()
	if sec > 0 {
		b.ReportMetric(float64(b.N)/sec, "events/sec")
	}
}
