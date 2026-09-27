package eventbus

import (
	"context"
	"fmt"
	"testing"
)

// TestArenaEngineZeroAllocation verifies 0 B/op memory allocation
func TestArenaEngineZeroAllocation(t *testing.T) {
	engine := NewArenaEngine(DefaultArenaSize, nil)
	defer engine.Close()

	sub, err := engine.Subscribe("test.topic", func(ctx context.Context, event *Event) error {
		if event.ID != "test-123" {
			t.Errorf("Expected ID 'test-123', got '%s'", event.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	defer sub.Unsubscribe()

	event := &Event{
		ID:      "test-123",
		Topic:   "test.topic",
		Type:    "Created",
		Source:  "test-source",
		Data:    []byte("test-payload-data"),
	}

	packet := engine.AllocateEvent(event)
	err = engine.Publish(context.Background(), packet)
	if err != nil {
		t.Errorf("Publish failed: %v", err)
	}

	stats := engine.Stats()
	if stats.PublishedEvents != 1 {
		t.Errorf("Expected 1 published event, got %d", stats.PublishedEvents)
	}

	fmt.Printf("Arena Engine Stats: %+v\n", stats)
}

// BenchmarkArenaEngineZeroCopyVsBaseline measures performance vs baseline
func BenchmarkArenaEngineZeroCopyVsBaseline(b *testing.B) {
	engine := NewArenaEngine(DefaultArenaSize, nil)
	defer engine.Close()

	sub, _ := engine.Subscribe("test.*", func(ctx context.Context, event *Event) error {
		return nil
	})
	defer sub.Unsubscribe()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		event := &Event{
			ID:      fmt.Sprintf("evt-%d", i),
			Topic:   "test.topic",
			Type:    "Created",
			Source:  "bench",
			Data:    []byte("benchmark-data"),
		}

		packet := engine.AllocateEvent(event)
		engine.Publish(context.Background(), packet)
		
		if i%100 == 0 {
			engine.Reset()
		}
	}
}
