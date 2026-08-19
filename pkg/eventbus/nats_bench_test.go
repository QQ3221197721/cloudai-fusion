package eventbus

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// benchLogger returns a quiet logger so benchmark output isn't polluted by the
// expected "NATS unavailable — using in-memory fallback" warnings.
func benchLogger() *logrus.Logger {
	lg := logrus.New()
	lg.SetLevel(logrus.PanicLevel)
	return lg
}

// BenchmarkNATSBus_Publish_Fallback measures throughput of the graceful
// in-memory fallback path that engages when no NATS server is reachable.
// It points at a port that has no broker so NewNATSBus falls back deterministically.
// Target: >500K msg/sec in fallback mode.
func BenchmarkNATSBus_Publish_Fallback(b *testing.B) {
	cfg := Config{
		Backend:    "nats",
		NATSURL:    "nats://127.0.0.1:14222", // no server here -> fallback
		BufferSize: 4096,
		MaxRetries: 1,
	}
	bus, err := NewNATSBus(cfg, benchLogger())
	if err != nil {
		b.Fatalf("NewNATSBus: %v", err)
	}
	defer bus.Close()

	if got := bus.Stats().Backend; got != "nats" {
		b.Fatalf("Backend = %q, want nats", got)
	}

	var received atomic.Int64
	if _, err := bus.Subscribe("bench.fallback", func(ctx context.Context, e *Event) error {
		received.Add(1)
		return nil
	}); err != nil {
		b.Fatalf("Subscribe: %v", err)
	}

	evt, _ := NewEvent("bench.fallback", "Bench", "bench", map[string]int{"n": 1})
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bus.Publish(ctx, evt); err != nil {
			b.Fatalf("Publish: %v", err)
		}
	}
	b.StopTimer()

	if received.Load() == 0 {
		b.Fatal("fallback bus delivered no messages")
	}
}

// BenchmarkNATSBus_PublishSubscribe_Integration measures real JetStream
// publish/consume throughput against a running NATS server. It is skipped
// unless NATS_TEST_URL points at a reachable JetStream-enabled server, so the
// benchmark never fails merely because no broker is running.
func BenchmarkNATSBus_PublishSubscribe_Integration(b *testing.B) {
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		b.Skip("set NATS_TEST_URL to run the real NATS JetStream integration benchmark")
	}

	cfg := Config{Backend: "nats", NATSURL: url, MaxRetries: 3}
	bus, err := NewNATSBus(cfg, benchLogger())
	if err != nil {
		b.Fatalf("NewNATSBus: %v", err)
	}
	defer bus.Close()

	// Unique topic per run to avoid clashing with pre-existing durable state.
	topic := fmt.Sprintf("bench.integration.%d", time.Now().UnixNano())

	var received atomic.Int64
	if _, err := bus.Subscribe(topic, func(ctx context.Context, e *Event) error {
		received.Add(1)
		return nil
	}); err != nil {
		b.Fatalf("Subscribe: %v", err)
	}

	evt, _ := NewEvent(topic, "Bench", "bench", map[string]int{"n": 1})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bus.Publish(ctx, evt); err != nil {
			b.Fatalf("Publish: %v", err)
		}
	}

	// JetStream delivery is asynchronous; wait for consumers to drain.
	deadline := time.Now().Add(30 * time.Second)
	for received.Load() < int64(b.N) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	b.StopTimer()

	if received.Load() < int64(b.N) {
		b.Fatalf("delivered %d/%d messages before timeout", received.Load(), b.N)
	}
}
