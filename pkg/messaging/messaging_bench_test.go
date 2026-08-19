package messaging

// Benchmarks for the messaging package.
//
// Scope (Task 132 / T2): publish throughput, end-to-end pub/sub throughput,
// dead-letter-queue enqueue latency, message serialization overhead, and
// concurrent publish throughput (b.RunParallel).
//
// The in-memory driver is exercised directly (not the singleton factory) so each
// benchmark owns an isolated queue with a buffer >= b.N, avoiding both the
// package-level sync.Once and any "queue full" back-pressure that would distort
// the measured cost. A discarding logger removes logging I/O from the hot path.

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
)

// benchLogger returns a logger whose output is discarded and whose level is set
// to Panic so Debug/Info calls on the hot path cost effectively nothing.
func benchLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	l.SetLevel(logrus.PanicLevel)
	return l
}

// newIsolatedQueue builds a memoryQueue with a buffer large enough to hold the
// whole run, bypassing the shared singleton for deterministic measurements.
func newIsolatedQueue(bufSize int) *memoryQueue {
	return &memoryQueue{
		messages: make(chan *Message, bufSize),
		handlers: make(map[string]MessageHandler),
		logger:   benchLogger(),
		config:   Config{BufferSize: bufSize, MaxRetries: 3},
	}
}

// samplePayload is a representative scheduling command body.
type samplePayload struct {
	WorkloadID string            `json:"workload_id"`
	Region     string            `json:"region"`
	Replicas   int               `json:"replicas"`
	Labels     map[string]string `json:"labels"`
}

func sampleBody() samplePayload {
	return samplePayload{
		WorkloadID: "wl-000123",
		Region:     "cn-hangzhou",
		Replicas:   8,
		Labels:     map[string]string{"team": "platform", "tier": "gold"},
	}
}

// ---------------------------------------------------------------------------
// Publish throughput — cost of enqueuing a prebuilt message into the queue.
// ---------------------------------------------------------------------------

func BenchmarkMemoryPublish(b *testing.B) {
	q := newIsolatedQueue(b.N + 16)
	msg, err := NewMessage(QueueScheduling, "ScheduleWorkload", sampleBody())
	if err != nil {
		b.Fatalf("NewMessage: %v", err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := q.Publish(ctx, msg); err != nil {
			b.Fatalf("Publish: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end pub/sub throughput — publish + consumer handler dispatch.
// ---------------------------------------------------------------------------

func BenchmarkMemoryPubSubThroughput(b *testing.B) {
	q := newIsolatedQueue(b.N + 16)
	msg, err := NewMessage(QueueScheduling, "ScheduleWorkload", sampleBody())
	if err != nil {
		b.Fatalf("NewMessage: %v", err)
	}

	done := make(chan struct{}, b.N)
	if err := q.Subscribe(QueueScheduling, "bench-group", func(_ context.Context, _ *Message) error {
		done <- struct{}{}
		return nil
	}); err != nil {
		b.Fatalf("Subscribe: %v", err)
	}
	defer q.Close()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := q.Publish(ctx, msg); err != nil {
			b.Fatalf("Publish: %v", err)
		}
	}
	for i := 0; i < b.N; i++ {
		<-done
	}
}

// ---------------------------------------------------------------------------
// Dead-letter-queue enqueue latency — cost of routing a failed message to DLQ.
// ---------------------------------------------------------------------------

func BenchmarkDLQEnqueue(b *testing.B) {
	q := newIsolatedQueue(b.N + 16)
	msg, err := NewMessage(QueueScheduling, "ScheduleWorkload", sampleBody())
	if err != nil {
		b.Fatalf("NewMessage: %v", err)
	}
	// Simulate a message that exhausted retries and must be dead-lettered.
	msg.DeliveryCount = msg.MaxRetries
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dlq := *msg
		dlq.Queue = QueueDeadLetter
		if err := q.Publish(ctx, &dlq); err != nil {
			b.Fatalf("DLQ Publish: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Serialization overhead — the message envelope is the marshaling unit.
// ---------------------------------------------------------------------------

func BenchmarkNewMessage(b *testing.B) {
	body := sampleBody()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := NewMessage(QueueScheduling, "ScheduleWorkload", body); err != nil {
			b.Fatalf("NewMessage: %v", err)
		}
	}
}

func BenchmarkMessageMarshal(b *testing.B) {
	msg, err := NewMessage(QueueScheduling, "ScheduleWorkload", sampleBody())
	if err != nil {
		b.Fatalf("NewMessage: %v", err)
	}
	msg.Headers["trace-id"] = "trace-abc-123"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(msg); err != nil {
			b.Fatalf("Marshal: %v", err)
		}
	}
}

func BenchmarkMessageUnmarshalBody(b *testing.B) {
	msg, err := NewMessage(QueueScheduling, "ScheduleWorkload", sampleBody())
	if err != nil {
		b.Fatalf("NewMessage: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out samplePayload
		if err := msg.UnmarshalBody(&out); err != nil {
			b.Fatalf("UnmarshalBody: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent publish throughput — contention on the queue under many producers.
// ---------------------------------------------------------------------------

func BenchmarkMemoryPublishParallel(b *testing.B) {
	q := newIsolatedQueue(b.N + 128)
	msg, err := NewMessage(QueueScheduling, "ScheduleWorkload", sampleBody())
	if err != nil {
		b.Fatalf("NewMessage: %v", err)
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := q.Publish(ctx, msg); err != nil {
				b.Fatalf("Publish: %v", err)
			}
		}
	})
}
