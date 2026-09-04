package eventbus

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// ============================================================================
// M6 EventBus T2 Head-to-Head vs embedded NATS Server (in-process)
// ============================================================================
// Purpose: Honest benchmark comparing our memory-based EventBus against an
// embedded nats-server using TRUE IN-PROCESS transport (no loopback TCP on either side).
//
// Rules:
//   - Same work unit: publish N identical-size messages; subscriber must receive all N
//   - Transport: Both in-process (memoryBus uses Go channels; NATS uses InProcessServer pipe)
//   - Run 6 iterations: go test ./pkg/eventbus/... -bench=BenchmarkM6_T2_ -count=6 -json > t2.json
//   - Report median + stddev of events/sec, p50/p99 latencies, allocs/op
//   - If we WIN: explain why (zero-allocation ring buffer, synchronous inline fan-out)
//   - If we LOSE: report gap margin honestly + optimization attempt
// ============================================================================

// startEmbeddedInProcessNATS creates an in-process NATS server (zero TCP) and returns a
// client connected over the in-memory pipe (nats.InProcessServer), NOT loopback TCP.
// This is the fairest transport to compare against our in-process memoryBus: neither
// side crosses a socket. Caller must Close() the connection and Shutdown() the server.
func startEmbeddedInProcessNATS(tb testing.TB) (*server.Server, *nats.Conn) {
	tb.Helper()
	opts := &server.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		NoLog:     true,
		NoSigs:    true,
		JetStream: false, // core NATS only: no persistence overhead
	}
	srv, err := server.NewServer(opts)
	if err != nil {
		tb.Fatalf("create embedded NATS server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		tb.Fatal("embedded NATS did not start within 10s")
	}
	// InProcessServer bypasses TCP entirely: client<->server over an in-memory net.Pipe.
	nc, err := nats.Connect("", nats.InProcessServer(srv))
	if err != nil {
		srv.Shutdown()
		tb.Fatalf("connect in-process NATS: %v", err)
	}
	return srv, nc
}

// BenchmarkM6_T2_Throughput_MemoryBus measures pipeline throughput: memory-based EventBus delivers
// messages synchronously inline. Subscribers receive each message as it's published. The work unit is
// "publish N and have subscriber receive all N" — wall time stops only after all N delivered.
func BenchmarkM6_T2_Throughput_MemoryBus(b *testing.B) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()

	const topic = "m6.t2.throughput.mem"
	var receivedCount atomic.Int32
	_, err := bus.Subscribe(topic, func(ctx context.Context, e *Event) error {
		receivedCount.Add(1)
		return nil
	})
	if err != nil {
		b.Fatalf("subscribe memory bus: %v", err)
	}

	evt, _ := NewEvent(topic, "M6Benchmark", "t2-throughput", map[string]string{"batch": "yes"})
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bus.Publish(ctx, evt); err != nil {
			b.Errorf("publish: %v", err)
		}
	}
	// For synchronous bus, all N should be delivered by now — verify.
	target := int32(b.N)
	if actual := receivedCount.Load(); actual != target {
		b.Fatalf("delivered %d/%d events before timeout", actual, target)
	}
	b.StopTimer()
}

// BenchmarkM6_T2_Throughput_InProcessNATS measures pipeline throughput: embedded NATS using
// true in-process transport (zero TCP). Messages are delivered asynchronously via goroutines. The work unit
// is "publish N and have subscriber receive all N" — we wait for all deliveries before stopping the timer,
// ensuring fair comparison against our synchronous in-process memoreBus.
func BenchmarkM6_T2_Throughput_InProcessNATS(b *testing.B) {
	srv, nc := startEmbeddedInProcessNATS(b)
	defer srv.Shutdown()
	defer nc.Close()

	const topic = "m6.t2.throughput.nats"
	var receivedCount atomic.Int32
	_, err := nc.Subscribe(topic, func(msg *nats.Msg) {
		receivedCount.Add(1)
	})
	if err != nil {
		b.Fatalf("subscribe NATS: %v", err)
	}

	payload, _ := json.Marshal(map[string]string{"batch": "yes"})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := nc.Publish(topic, payload); err != nil {
			b.Errorf("publish: %v", err)
		}
	}
	// Wait until all N are delivered (NATS is async).
	target := int32(b.N)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()
		for {
			if receivedCount.Load() == target {
				close(done)
				return
			}
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	select {
	case <-done:
		// All received.
	case <-time.After(60 * time.Second):
		b.Fatalf("subscriber did not receive all %d messages within 60s", target)
	}
	b.StopTimer()
	nc.Flush()
}

// ============================================================================
// Latency Benchmark (ping-pong): measure per-message round-trip latency
// This is the most unfair comparison to memoryBus and shows architectural advantage
// ============================================================================

// BenchmarkM6_T2_Latency_MemoryBus measures per-message latency via one-shot ping-pong.
func BenchmarkM6_T2_Latency_MemoryBus(b *testing.B) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()

	const topic = "m6.t2.latency.mem"
	sig := make(chan struct{}, 1)
	_, err := bus.Subscribe(topic, func(ctx context.Context, e *Event) error {
		select {
		case sig <- struct{}{}:
		default:
		}
		return nil
	})
	if err != nil {
		b.Fatalf("subscribe memory bus: %v", err)
	}

	evt, _ := NewEvent(topic, "M6Latency", "ping-pong", map[string]int{"seq": 0})

	var latencies atomic.Int32 // nanoseconds in microseconds for fitment
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := bus.Publish(context.Background(), evt); err != nil {
			b.Fatalf("publish: %v", err)
		}
		select {
		case <-sig:
		case <-time.After(5 * time.Second):
			b.Fatal("subscriber did not receive message before timeout")
		}
		micros := time.Since(start).Microseconds()
		latencies.Add(int32(micros))
	}
	b.StopTimer()
	avgLatency := float64(latencies.Load()) / float64(b.N)
	b.ReportMetric(avgLatency, "avg-lat/us")
}

// BenchmarkM6_T2_Latency_InProcessNATS measures per-message latency via ping-pong.
func BenchmarkM6_T2_Latency_InProcessNATS(b *testing.B) {
	srv, nc := startEmbeddedInProcessNATS(b)
	defer srv.Shutdown()
	defer nc.Close()

	const topic = "m6.t2.latency.nats"
	sig := make(chan struct{}, 1)
	_, err := nc.Subscribe(topic, func(msg *nats.Msg) {
		select {
		case sig <- struct{}{}:
		default:
		}
	})
	if err != nil {
		b.Fatalf("subscribe NATS: %v", err)
	}
	if err := nc.Flush(); err != nil {
		b.Fatalf("flush NATS: %v", err)
	}

	payload, _ := json.Marshal(map[string]int{"seq": 0})

	var latencies atomic.Int32
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if err := nc.Publish(topic, payload); err != nil {
			b.Fatalf("publish: %v", err)
		}
		select {
		case <-sig:
		case <-time.After(5 * time.Second):
			b.Fatal("subscriber did not receive message before timeout")
		}
		micros := time.Since(start).Microseconds()
		latencies.Add(int32(micros))
	}
	b.StopTimer()
	avgLatency := float64(latencies.Load()) / float64(b.N)
	b.ReportMetric(avgLatency, "avg-lat/us")
}
