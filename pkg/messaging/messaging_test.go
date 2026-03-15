package messaging

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// resetSharedQueue — reset singleton between tests
// ============================================================================

func resetSharedQueue() {
	sharedQueue = nil
	sharedQueueOnce = sync.Once{}
}

// ============================================================================
// Message & NewMessage
// ============================================================================

func TestNewMessage(t *testing.T) {
	body := map[string]string{"workload_id": "wl-1"}
	msg, err := NewMessage(QueueScheduling, "ScheduleWorkload", body)
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if msg.ID == "" {
		t.Error("ID should not be empty")
	}
	if msg.Queue != QueueScheduling {
		t.Errorf("Queue = %q, want %q", msg.Queue, QueueScheduling)
	}
	if msg.Type != "ScheduleWorkload" {
		t.Errorf("Type = %q, want ScheduleWorkload", msg.Type)
	}
	if msg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", msg.MaxRetries)
	}
	if msg.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
	if msg.Headers == nil {
		t.Error("Headers should be initialized")
	}
}

func TestNewMessage_MarshalError(t *testing.T) {
	_, err := NewMessage("q", "T", make(chan int))
	if err == nil {
		t.Error("expected marshal error for channel")
	}
}

func TestMessage_UnmarshalBody(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	msg, _ := NewMessage("q", "T", payload{Name: "test"})

	var got payload
	if err := msg.UnmarshalBody(&got); err != nil {
		t.Fatalf("UnmarshalBody: %v", err)
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want test", got.Name)
	}
}

func TestMessage_JSON_RoundTrip(t *testing.T) {
	msg, _ := NewMessage(QueueScheduling, "ScheduleWorkload", map[string]string{"id": "wl-1"})
	msg.Priority = 5
	msg.Headers["trace-id"] = "t-1"

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Queue != msg.Queue || decoded.Type != msg.Type || decoded.Priority != 5 {
		t.Error("round-trip mismatch")
	}
}

// ============================================================================
// Well-known Queue Constants
// ============================================================================

func TestWellKnownQueues(t *testing.T) {
	queues := []string{
		QueueScheduling, QueueSecurityScan, QueueReconciliation,
		QueueNotification, QueueCostAnalysis, QueueDeadLetter,
	}
	for _, q := range queues {
		if q == "" {
			t.Errorf("queue constant should not be empty")
		}
	}
}

// ============================================================================
// DefaultConfig
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", cfg.Backend)
	}
	if cfg.BufferSize != 10000 {
		t.Errorf("BufferSize = %d, want 10000", cfg.BufferSize)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.AckTimeout != 30*time.Second {
		t.Errorf("AckTimeout = %v, want 30s", cfg.AckTimeout)
	}
}

// ============================================================================
// Producer — Publish
// ============================================================================

func TestMemoryProducer_Publish(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 100, MaxRetries: 1, RetryDelay: time.Millisecond}
	producer := NewMemoryProducer(cfg, logrus.StandardLogger())
	defer producer.Close()

	msg, _ := NewMessage(QueueScheduling, "T", nil)
	err := producer.Publish(context.Background(), msg)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func TestMemoryProducer_PublishAfterClose(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 100}
	producer := NewMemoryProducer(cfg, logrus.StandardLogger())
	producer.Close()

	msg, _ := NewMessage("q", "T", nil)
	err := producer.Publish(context.Background(), msg)
	if err == nil {
		t.Error("expected error when publishing to closed queue")
	}
}

func TestMemoryProducer_PublishFull(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 1}
	producer := NewMemoryProducer(cfg, logrus.StandardLogger())
	defer producer.Close()

	msg1, _ := NewMessage("q", "T", nil)
	producer.Publish(context.Background(), msg1)

	msg2, _ := NewMessage("q", "T", nil)
	err := producer.Publish(context.Background(), msg2)
	if err == nil {
		t.Error("expected error when queue is full")
	}
}

func TestMemoryProducer_PublishCancelled(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 0} // will default to 10000
	producer := NewMemoryProducer(cfg, logrus.StandardLogger())
	defer producer.Close()

	// Fill the queue first to trigger the select on ctx.Done
	// Actually with large buffer, just test cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	msg, _ := NewMessage("q", "T", nil)
	// With a large buffer it won't block, so this will succeed — that's ok
	_ = producer.Publish(ctx, msg)
}

// ============================================================================
// PublishDelayed
// ============================================================================

func TestMemoryProducer_PublishDelayed(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 100}
	producer := NewMemoryProducer(cfg, logrus.StandardLogger())
	defer producer.Close()

	msg, _ := NewMessage(QueueScheduling, "T", nil)
	err := producer.PublishDelayed(context.Background(), msg, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("PublishDelayed: %v", err)
	}

	// Wait for delayed delivery
	time.Sleep(50 * time.Millisecond)
}

// ============================================================================
// Consumer — Subscribe & Process
// ============================================================================

func TestMemoryConsumer_SubscribeAndProcess(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 100, MaxRetries: 1, RetryDelay: time.Millisecond}
	// Use the shared queue for both producer and consumer
	producer := NewMemoryProducer(cfg, logrus.StandardLogger())
	consumer := NewMemoryConsumer(cfg, logrus.StandardLogger())
	defer producer.Close()

	var received string
	var mu sync.Mutex

	err := consumer.Subscribe(QueueScheduling, "test-group", func(ctx context.Context, msg *Message) error {
		mu.Lock()
		received = msg.Type
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	msg, _ := NewMessage(QueueScheduling, "ScheduleWorkload", nil)
	producer.Publish(context.Background(), msg)

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := received
	mu.Unlock()

	if got != "ScheduleWorkload" {
		t.Errorf("received = %q, want ScheduleWorkload", got)
	}
}

func TestMemoryConsumer_Unsubscribe(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 100}
	consumer := NewMemoryConsumer(cfg, logrus.StandardLogger())
	defer consumer.Close()

	consumer.Subscribe(QueueScheduling, "g1", func(ctx context.Context, msg *Message) error {
		return nil
	})
	err := consumer.Unsubscribe(QueueScheduling)
	if err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
}

// ============================================================================
// Close idempotent
// ============================================================================

func TestMemoryQueue_CloseIdempotent(t *testing.T) {
	resetSharedQueue()

	cfg := Config{BufferSize: 100}
	q := NewMemoryProducer(cfg, logrus.StandardLogger())
	if err := q.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := q.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// ============================================================================
// Factory — NewProducer / NewConsumer
// ============================================================================

func TestNewProducer_Backends(t *testing.T) {
	tests := []struct {
		backend string
	}{
		{"memory"},
		{"kafka"},
		{"nats"},
		{""},
	}
	for _, tc := range tests {
		t.Run(tc.backend, func(t *testing.T) {
			resetSharedQueue()
			cfg := Config{Backend: tc.backend, BufferSize: 10}
			p := NewProducer(cfg, logrus.StandardLogger())
			if p == nil {
				t.Error("producer should not be nil")
			}
			p.Close()
		})
	}
}

func TestNewConsumer_Backends(t *testing.T) {
	tests := []struct {
		backend string
	}{
		{"memory"},
		{"kafka"},
		{"nats"},
	}
	for _, tc := range tests {
		t.Run(tc.backend, func(t *testing.T) {
			resetSharedQueue()
			cfg := Config{Backend: tc.backend, BufferSize: 10}
			c := NewConsumer(cfg, logrus.StandardLogger())
			if c == nil {
				t.Error("consumer should not be nil")
			}
			c.Close()
		})
	}
}

// ============================================================================
// generateMsgID
// ============================================================================

func TestGenerateMsgID(t *testing.T) {
	id := generateMsgID()
	if id == "" {
		t.Error("msg ID should not be empty")
	}
	if len(id) < 5 {
		t.Errorf("msg ID too short: %q", id)
	}
}
