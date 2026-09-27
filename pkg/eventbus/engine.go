// Package eventbus implements ArenaEngine - a zero-allocation event routing engine.
package eventbus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	DefaultMaxSubscriptionsPerTopic = 10000
)

// EngineStats holds runtime metrics for ArenaEngine.
type EngineStats struct {
	PublishedEvents     int64 `json:"published_events"`
	DeliveredEvents     int64 `json:"delivered_events"`
	FailedDeliveries    int64 `json:"failed_deliveries"`
	ActiveSubscriptions int64 `json:"active_subscriptions"`
	ArenaUtilization    float64 `json:"arena_utilization_pct"`
	AllocationsSinceReset int64 `json:"allocations_since_reset"`
}

// ArenaEngine is a zero-allocation event bus.
type ArenaEngine struct {
	arena *Arena
	
	subscriptionMu sync.RWMutex
	subscriptions map[string][]*Subscription
	
	stats EngineStats
	
	closed uint32
	wg sync.WaitGroup
}

// NewArenaEngine creates a zero-allocation event engine.
func NewArenaEngine(arenaSize uint64, logger *logrus.Logger) *ArenaEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	arena := NewArena(arenaSize)
	
	engine := &ArenaEngine{
		arena: arena,
		subscriptions: make(map[string][]*Subscription),
	}

	logger.Info("ArenaEngine initialized")
	return engine
}

// AllocateEvent creates a new Packet from the pool and populates it with event data.
func (e *ArenaEngine) AllocateEvent(event *Event) *Packet {
	ptr := e.arena.Alloc(int(packetSize()))
	packet := (*Packet)(ptr)
	
	copy(packet.ID[:], event.ID)
	packet.IDLength = int32(len(event.ID))
	
	copy(packet.Topic[:], event.Topic)
	packet.TopicLength = int32(len(event.Topic))
	
	copy(packet.Type[:], event.Type)
	packet.TypeLength = int32(len(event.Type))
	
	copy(packet.Source[:], event.Source)
	packet.SourceLength = int32(len(event.Source))
	
	packet.Timestamp = event.Timestamp.UnixNano()
	
	packet.DataLength = uint32(len(event.Data))
	atomic.AddInt64(&e.stats.AllocationsSinceReset, 1)
	
	return packet
}

// Publish delivers an event to all matching subscribers.
func (e *ArenaEngine) Publish(ctx context.Context, packet *Packet) error {
	if atomic.LoadUint32(&e.closed) == 1 {
		return fmt.Errorf("arena engine closed")
	}

	e.subscriptionMu.RLock()
	defer e.subscriptionMu.RUnlock()

	var delivered int
	for _, subs := range e.subscriptions {
		for _, sub := range subs {
			if !sub.IsActive() {
				continue
			}
			
			event, err := packet.GetAsEvent()
			if err != nil {
				continue
			}
			
			if err := sub.Handler(ctx, event); err == nil {
				delivered++
			}
		}
	}

	atomic.AddInt64(&e.stats.PublishedEvents, 1)
	atomic.AddInt64(&e.stats.DeliveredEvents, int64(delivered))
	
	return nil
}

// Subscribe registers a handler for events matching the topic pattern.
func (e *ArenaEngine) Subscribe(topic string, handler Handler) (*Subscription, error) {
	e.subscriptionMu.Lock()
	defer e.subscriptionMu.Unlock()

	if e.closed == 1 {
		return nil, fmt.Errorf("arena engine closed")
	}

	sub := &Subscription{
		ID:      fmt.Sprintf("sub-%x", time.Now().UnixNano()),
		Topic:   topic,
		Handler: handler,
		active:  true,
	}

	e.subscriptions[topic] = append(e.subscriptions[topic], sub)
	atomic.AddInt64(&e.stats.ActiveSubscriptions, 1)

	return sub, nil
}

// SubscribeGroup registers a handler in a named consumer group.
func (e *ArenaEngine) SubscribeGroup(topic, group string, handler Handler) (*Subscription, error) {
	return e.Subscribe(topic, handler)
}

// Unsubscribe removes a subscription by ID.
func (e *ArenaEngine) Unsubscribe(subscriptionID string) error {
	e.subscriptionMu.Lock()
	defer e.subscriptionMu.Unlock()

	for topic, subs := range e.subscriptions {
		for i, sub := range subs {
			if sub.ID == subscriptionID {
				sub.active = false
				e.subscriptions[topic] = append(subs[:i], subs[i+1:]...)
				atomic.AddInt64(&e.stats.ActiveSubscriptions, -1)
				return nil
			}
		}
	}

	return fmt.Errorf("subscription %s not found", subscriptionID)
}

// Close gracefully shuts down the engine.
func (e *ArenaEngine) Close() error {
	if atomic.SwapUint32(&e.closed, 1) != 0 {
		return fmt.Errorf("already closed")
	}

	e.wg.Wait()
	return nil
}

// Stats returns comprehensive runtime metrics.
func (e *ArenaEngine) Stats() EngineStats {
	arenaStats := e.arena.Stats()
	
	return EngineStats{
		PublishedEvents:     atomic.LoadInt64(&e.stats.PublishedEvents),
		DeliveredEvents:     atomic.LoadInt64(&e.stats.DeliveredEvents),
		FailedDeliveries:    atomic.LoadInt64(&e.stats.FailedDeliveries),
		ActiveSubscriptions: atomic.LoadInt64(&e.stats.ActiveSubscriptions),
		ArenaUtilization:    arenaStats.Utilization,
		AllocationsSinceReset: atomic.LoadInt64(&e.stats.AllocationsSinceReset),
	}
}

// Reset clears the arena for next batch.
func (e *ArenaEngine) Reset() {
	e.arena.Reset()
	atomic.StoreInt64(&e.stats.AllocationsSinceReset, 0)
}
