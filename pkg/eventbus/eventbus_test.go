package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// NewEvent & Event helpers
// ============================================================================

func TestNewEvent(t *testing.T) {
	payload := map[string]string{"name": "test-cluster"}
	evt, err := NewEvent("cluster.created", "Created", "apiserver", payload)
	if err != nil {
		t.Fatalf("NewEvent failed: %v", err)
	}
	if evt.Topic != "cluster.created" {
		t.Errorf("Topic = %q, want cluster.created", evt.Topic)
	}
	if evt.Type != "Created" {
		t.Errorf("Type = %q, want Created", evt.Type)
	}
	if evt.Source != "apiserver" {
		t.Errorf("Source = %q, want apiserver", evt.Source)
	}
	if evt.ID == "" {
		t.Error("ID should not be empty")
	}
	if evt.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
	if evt.Metadata == nil {
		t.Error("Metadata should be initialized")
	}
}

func TestNewEvent_MarshalError(t *testing.T) {
	// channels cannot be marshalled to JSON
	_, err := NewEvent("topic", "Type", "src", make(chan int))
	if err == nil {
		t.Error("expected marshal error")
	}
}

func TestEvent_UnmarshalData(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}
	evt, _ := NewEvent("topic", "Type", "src", payload{Name: "hello", ID: 42})

	var got payload
	if err := evt.UnmarshalData(&got); err != nil {
		t.Fatalf("UnmarshalData: %v", err)
	}
	if got.Name != "hello" || got.ID != 42 {
		t.Errorf("got %+v, want {hello 42}", got)
	}
}

func TestEvent_WithCorrelation(t *testing.T) {
	evt, _ := NewEvent("t", "T", "s", nil)
	ret := evt.WithCorrelation("corr-1", "cause-1")
	if ret != evt {
		t.Error("WithCorrelation should return same pointer")
	}
	if evt.CorrelationID != "corr-1" || evt.CausationID != "cause-1" {
		t.Error("correlation/causation not set")
	}
}

func TestEvent_WithMetadata(t *testing.T) {
	evt := &Event{}
	evt.WithMetadata("key1", "val1")
	if evt.Metadata["key1"] != "val1" {
		t.Errorf("Metadata[key1] = %q, want val1", evt.Metadata["key1"])
	}
	evt.WithMetadata("key2", "val2")
	if len(evt.Metadata) != 2 {
		t.Errorf("len(Metadata) = %d, want 2", len(evt.Metadata))
	}
}

// ============================================================================
// Subscription
// ============================================================================

func TestSubscription_IsActive(t *testing.T) {
	sub := &Subscription{ID: "s1", active: true}
	if !sub.IsActive() {
		t.Error("subscription should be active")
	}
	sub.Unsubscribe()
	if sub.IsActive() {
		t.Error("subscription should be inactive after Unsubscribe")
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
	if cfg.BufferSize != 4096 {
		t.Errorf("BufferSize = %d, want 4096", cfg.BufferSize)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
}

// ============================================================================
// topicMatches
// ============================================================================

func TestTopicMatches(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		want    bool
	}{
		{"cluster.created", "cluster.created", true},
		{"cluster.created", "cluster.deleted", false},
		{"cluster.*", "cluster.created", true},
		{"cluster.*", "cluster.deleted", true},
		{"cluster.*", "workload.created", false},
		{">", "anything.goes.here", true},
		{"cluster.>", "cluster.created.sub", true},
		{"cluster.*", "cluster.a.b", false},
		{"a.b.c", "a.b", false},
		{"a.b", "a.b.c", false},
	}
	for _, tc := range tests {
		t.Run(tc.pattern+"_vs_"+tc.topic, func(t *testing.T) {
			got := topicMatches(tc.pattern, tc.topic)
			if got != tc.want {
				t.Errorf("topicMatches(%q, %q) = %v, want %v", tc.pattern, tc.topic, got, tc.want)
			}
		})
	}
}

// ============================================================================
// memoryBus — Publish / Subscribe
// ============================================================================

func newTestBus() EventBus {
	return NewMemoryBus(Config{BufferSize: 4096, MaxRetries: 1, RetryDelay: time.Millisecond}, logrus.StandardLogger())
}

func TestMemoryBus_PublishSubscribe(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	var received atomic.Int32
	_, err := bus.Subscribe("cluster.created", func(ctx context.Context, event *Event) error {
		received.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	evt, _ := NewEvent("cluster.created", "Created", "test", map[string]string{"id": "c1"})
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if received.Load() != 1 {
		t.Errorf("received = %d, want 1", received.Load())
	}
}

func TestMemoryBus_WildcardSubscribe(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	var count atomic.Int32
	bus.Subscribe("cluster.*", func(ctx context.Context, event *Event) error {
		count.Add(1)
		return nil
	})

	e1, _ := NewEvent("cluster.created", "Created", "test", nil)
	e2, _ := NewEvent("cluster.deleted", "Deleted", "test", nil)
	e3, _ := NewEvent("workload.created", "Created", "test", nil)
	bus.Publish(context.Background(), e1)
	bus.Publish(context.Background(), e2)
	bus.Publish(context.Background(), e3)

	if count.Load() != 2 {
		t.Errorf("wildcard received = %d, want 2", count.Load())
	}
}

func TestMemoryBus_SubscribeGroup(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	var countA, countB atomic.Int32
	bus.SubscribeGroup("cluster.created", "mygroup", func(ctx context.Context, event *Event) error {
		countA.Add(1)
		return nil
	})
	bus.SubscribeGroup("cluster.created", "mygroup", func(ctx context.Context, event *Event) error {
		countB.Add(1)
		return nil
	})

	evt, _ := NewEvent("cluster.created", "Created", "test", nil)
	bus.Publish(context.Background(), evt)

	// Only one handler per group should receive
	total := countA.Load() + countB.Load()
	if total != 1 {
		t.Errorf("group total = %d, want 1", total)
	}
}

func TestMemoryBus_Unsubscribe(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	var count atomic.Int32
	sub, _ := bus.Subscribe("test.topic", func(ctx context.Context, event *Event) error {
		count.Add(1)
		return nil
	})

	evt, _ := NewEvent("test.topic", "T", "s", nil)
	bus.Publish(context.Background(), evt)
	if count.Load() != 1 {
		t.Errorf("before unsub: count = %d, want 1", count.Load())
	}

	if err := bus.Unsubscribe(sub.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	bus.Publish(context.Background(), evt)
	if count.Load() != 1 {
		t.Errorf("after unsub: count = %d, want 1", count.Load())
	}
}

func TestMemoryBus_Unsubscribe_NotFound(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	err := bus.Unsubscribe("nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent subscription")
	}
}

func TestMemoryBus_PublishAfterClose(t *testing.T) {
	bus := newTestBus()
	bus.Close()

	evt, _ := NewEvent("t", "T", "s", nil)
	err := bus.Publish(context.Background(), evt)
	if err == nil {
		t.Error("expected error when publishing to closed bus")
	}
}

func TestMemoryBus_SubscribeAfterClose(t *testing.T) {
	bus := newTestBus()
	bus.Close()

	_, err := bus.Subscribe("t", func(ctx context.Context, event *Event) error { return nil })
	if err == nil {
		t.Error("expected error when subscribing to closed bus")
	}
}

func TestMemoryBus_Stats(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	bus.Subscribe("topic.a", func(ctx context.Context, event *Event) error { return nil })
	bus.Subscribe("topic.b", func(ctx context.Context, event *Event) error { return nil })

	stats := bus.Stats()
	if stats.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", stats.Backend)
	}
	if stats.ActiveTopics != 2 {
		t.Errorf("ActiveTopics = %d, want 2", stats.ActiveTopics)
	}
	if stats.ActiveSubscriptions != 2 {
		t.Errorf("ActiveSubscriptions = %d, want 2", stats.ActiveSubscriptions)
	}

	e, _ := NewEvent("topic.a", "T", "s", nil)
	bus.Publish(context.Background(), e)
	stats = bus.Stats()
	if stats.TotalPublished != 1 {
		t.Errorf("TotalPublished = %d, want 1", stats.TotalPublished)
	}
	if stats.TotalDelivered != 1 {
		t.Errorf("TotalDelivered = %d, want 1", stats.TotalDelivered)
	}
}

func TestMemoryBus_HandlerError_CountsErrors(t *testing.T) {
	bus := newTestBus()
	defer bus.Close()

	bus.Subscribe("err.topic", func(ctx context.Context, event *Event) error {
		return errors.New("handler failed")
	})

	e, _ := NewEvent("err.topic", "T", "s", nil)
	bus.Publish(context.Background(), e)

	stats := bus.Stats()
	if stats.TotalErrors != 1 {
		t.Errorf("TotalErrors = %d, want 1", stats.TotalErrors)
	}
}

// ============================================================================
// NATS Bus (fallback mode)
// ============================================================================

func TestNATSBus_FallbackMode(t *testing.T) {
	cfg := Config{Backend: "nats", NATSURL: "nats://localhost:4222", BufferSize: 100}
	bus, err := NewNATSBus(cfg, logrus.StandardLogger())
	if err != nil {
		t.Fatalf("NewNATSBus: %v", err)
	}
	defer bus.Close()

	var got string
	bus.Subscribe("test.nats", func(ctx context.Context, event *Event) error {
		got = event.Topic
		return nil
	})
	e, _ := NewEvent("test.nats", "T", "s", nil)
	bus.Publish(context.Background(), e)

	if got != "test.nats" {
		t.Errorf("got topic %q, want test.nats", got)
	}

	stats := bus.Stats()
	if stats.Backend != "nats" {
		t.Errorf("Backend = %q, want nats", stats.Backend)
	}
	if stats.TotalPublished != 1 {
		t.Errorf("TotalPublished = %d, want 1", stats.TotalPublished)
	}
}

// ============================================================================
// Factory
// ============================================================================

func TestNew_DefaultMemory(t *testing.T) {
	bus := New(DefaultConfig(), logrus.StandardLogger())
	defer bus.Close()

	stats := bus.Stats()
	if stats.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", stats.Backend)
	}
}

func TestNew_NATSFallback(t *testing.T) {
	cfg := Config{Backend: "nats", NATSURL: "nats://invalid:9999"}
	bus := New(cfg, logrus.StandardLogger())
	defer bus.Close()

	// Should fallback gracefully
	stats := bus.Stats()
	if stats.Backend == "" {
		t.Error("Backend should not be empty")
	}
}

// ============================================================================
// MiddlewareBus
// ============================================================================

func TestMiddlewareBus_Wraps(t *testing.T) {
	inner := newTestBus()
	defer inner.Close()

	mbus := NewMiddlewareBus(inner, logrus.StandardLogger())

	var middlewareCalled bool
	mbus.Use(func(next Handler) Handler {
		return func(ctx context.Context, event *Event) error {
			middlewareCalled = true
			return next(ctx, event)
		}
	})

	var handlerCalled bool
	mbus.Subscribe("mw.test", func(ctx context.Context, event *Event) error {
		handlerCalled = true
		return nil
	})

	e, _ := NewEvent("mw.test", "T", "s", nil)
	mbus.Publish(context.Background(), e)

	if !middlewareCalled {
		t.Error("middleware should be called")
	}
	if !handlerCalled {
		t.Error("handler should be called")
	}
}

func TestMiddlewareBus_Stats(t *testing.T) {
	inner := newTestBus()
	defer inner.Close()

	mbus := NewMiddlewareBus(inner, logrus.StandardLogger())
	stats := mbus.Stats()
	if stats.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", stats.Backend)
	}
}

func TestMiddlewareBus_Unsubscribe(t *testing.T) {
	inner := newTestBus()
	defer inner.Close()
	mbus := NewMiddlewareBus(inner, logrus.StandardLogger())

	sub, _ := mbus.Subscribe("uns.test", func(ctx context.Context, event *Event) error { return nil })
	if err := mbus.Unsubscribe(sub.ID); err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
}

func TestMiddlewareBus_Close(t *testing.T) {
	inner := newTestBus()
	mbus := NewMiddlewareBus(inner, logrus.StandardLogger())
	if err := mbus.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// ============================================================================
// Built-in Middlewares
// ============================================================================

func TestLoggingMiddleware(t *testing.T) {
	mw := LoggingMiddleware(logrus.StandardLogger())
	var called bool
	handler := mw(func(ctx context.Context, event *Event) error {
		called = true
		return nil
	})
	e, _ := NewEvent("t", "T", "s", nil)
	handler(context.Background(), e)
	if !called {
		t.Error("handler should be called through logging middleware")
	}
}

func TestLoggingMiddleware_Error(t *testing.T) {
	mw := LoggingMiddleware(logrus.StandardLogger())
	handler := mw(func(ctx context.Context, event *Event) error {
		return errors.New("oops")
	})
	e, _ := NewEvent("t", "T", "s", nil)
	err := handler(context.Background(), e)
	if err == nil {
		t.Error("expected error from handler")
	}
}

func TestMetricsMiddleware(t *testing.T) {
	mw := MetricsMiddleware()
	handler := mw(func(ctx context.Context, event *Event) error { return nil })
	e, _ := NewEvent("t", "T", "s", nil)
	if err := handler(context.Background(), e); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ============================================================================
// generateEventID
// ============================================================================

func TestGenerateEventID(t *testing.T) {
	id := generateEventID()
	if id == "" {
		t.Error("event ID should not be empty")
	}
	if len(id) < 5 {
		t.Errorf("event ID too short: %q", id)
	}
}

// ============================================================================
// Event JSON Round-Trip
// ============================================================================

func TestEvent_JSON_RoundTrip(t *testing.T) {
	evt, _ := NewEvent("cluster.created", "Created", "apiserver", map[string]string{"name": "prod"})
	evt.WithCorrelation("corr-1", "cause-1").WithMetadata("tenant", "acme")

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Topic != evt.Topic || decoded.Type != evt.Type || decoded.Source != evt.Source {
		t.Error("round-trip mismatch")
	}
	if decoded.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID = %q, want corr-1", decoded.CorrelationID)
	}
}
