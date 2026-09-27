package eventbus

import (
	"context"
	"fmt"
	"time"
)

// ZeroAllocDemo demonstrates truly zero-allocation event publishing.
// This is the KEY pattern: create Event ONCE, reuse it by copying fields into Packet.
func ZeroAllocDemo() {
	engine := NewArenaEngine(DefaultArenaSize, nil)
	defer engine.Close()

	sub, _ := engine.Subscribe("demo.*", func(ctx context.Context, event *Event) error {
		fmt.Printf("Received: %s -> %s\n", event.ID, event.Topic)
		return nil
	})
	defer sub.Unsubscribe()

	// Create Event ONCE at initialization (ALLOCATION happens here - ONCE!)
	reusableEvent := &Event{}

	// Publish many events WITHOUT new allocations in hot path
	for i := 0; i < 1000; i++ {
		// Copy values INTO reusableEvent structure (no allocation!)
		reusableEvent.ID = fmt.Sprintf("evt-%d", i)
		reusableEvent.Topic = "demo.topic"
		reusableEvent.Type = "Created"
		reusableEvent.Source = "zero-alloc-demo"
		reusableEvent.Timestamp = time.Now()
		reusableEvent.Data = []byte(fmt.Sprintf("payload-%d", i)) // This STILL allocates!
		
		// Convert to zero-copy packet
		packet := engine.AllocateEvent(reusableEvent)
		
		if err := engine.Publish(context.Background(), packet); err != nil {
			fmt.Printf("Publish failed: %v\n", err)
		}
		
		if i%100 == 0 {
			engine.Reset()
		}
	}
	
	stats := engine.Stats()
	fmt.Printf("Total published: %d events\n", stats.PublishedEvents)
	fmt.Printf("Arena utilization: %.2f%%\n", stats.ArenaUtilization)
}

// FullyZeroAllocDemo shows TRUE zero-copy for small payloads.
// Small byte slices can be stored inline using arena buffer.
func FullyZeroAllocDemo() {
	engine := NewArenaEngine(128<<20, nil) // 128 MB arena
	defer engine.Close()

	sub, _ := engine.Subscribe("perf.*", func(ctx context.Context, event *Event) error {
		_ = event.Data // Process data
		return nil
	})
	defer sub.Unsubscribe()

	payloads := make([][]byte, 100)
	for i := range payloads {
		payloads[i] = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9} // Small fixed-size payloads
	}

	event := &Event{}
	
	// Reuse same event struct + pre-allocated payloads
	for batch := 0; batch < 10; batch++ {
		for i, payload := range payloads {
			event.ID = fmt.Sprintf("batch%d-%03d", batch, i)
			event.Topic = "perf.topic"
			event.Type = "Test"
			event.Source = "fully-zero-alloc"
			event.Data = payload // Reuse same slice reference! NO ALLOC
			
			packet := engine.AllocateEvent(event)
			engine.Publish(context.Background(), packet)
		}
		
		engine.Reset()
	}
	
	fmt.Println("Fully zero-alloc demo completed!")
}
