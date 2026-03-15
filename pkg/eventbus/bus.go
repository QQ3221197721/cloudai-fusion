package eventbus

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// EventBus Interface — the contract all backends must implement
// ============================================================================

// EventBus is the core abstraction for event-driven communication.
// Services publish events and subscribe to topics without direct coupling.
type EventBus interface {
	// Publish sends an event to all subscribers of the event's topic.
	Publish(ctx context.Context, event *Event) error

	// Subscribe registers a handler for events matching the topic pattern.
	// Topic patterns support wildcards: "cluster.*" matches "cluster.created", etc.
	// Returns a Subscription handle that can be used to unsubscribe.
	Subscribe(topic string, handler Handler) (*Subscription, error)

	// SubscribeGroup registers a handler in a named consumer group.
	// Within a group, each event is delivered to only one handler (load-balanced).
	SubscribeGroup(topic, group string, handler Handler) (*Subscription, error)

	// Unsubscribe removes a subscription by ID.
	Unsubscribe(subscriptionID string) error

	// Close shuts down the event bus and releases resources.
	Close() error

	// Stats returns runtime statistics about the event bus.
	Stats() BusStats
}

// BusStats holds runtime statistics for the event bus.
type BusStats struct {
	Backend         string `json:"backend"`
	TotalPublished  int64  `json:"total_published"`
	TotalDelivered  int64  `json:"total_delivered"`
	TotalErrors     int64  `json:"total_errors"`
	ActiveTopics    int    `json:"active_topics"`
	ActiveSubscriptions int `json:"active_subscriptions"`
}

// ============================================================================
// In-Memory EventBus — default backend for single-process or testing
// ============================================================================

// memoryBus is an in-memory event bus implementation using Go channels.
// Suitable for single-process deployments, development, and testing.
type memoryBus struct {
	subscriptions map[string][]*Subscription // topic -> subscriptions
	groups        map[string]map[string][]*Subscription // topic -> group -> subscriptions
	mu            sync.RWMutex
	config        Config
	logger        *logrus.Logger
	stats         BusStats
	statsMu       sync.Mutex
	closed        bool
}

// NewMemoryBus creates an in-memory event bus.
func NewMemoryBus(cfg Config, logger *logrus.Logger) EventBus {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 4096
	}
	return &memoryBus{
		subscriptions: make(map[string][]*Subscription),
		groups:        make(map[string]map[string][]*Subscription),
		config:        cfg,
		logger:        logger,
		stats:         BusStats{Backend: "memory"},
	}
}

// Publish sends an event to all matching subscribers.
func (b *memoryBus) Publish(ctx context.Context, event *Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return fmt.Errorf("event bus is closed")
	}

	b.statsMu.Lock()
	b.stats.TotalPublished++
	b.statsMu.Unlock()

	delivered := 0

	// Find all matching subscriptions (exact match + wildcard)
	for topic, subs := range b.subscriptions {
		if !topicMatches(topic, event.Topic) {
			continue
		}
		for _, sub := range subs {
			if !sub.IsActive() {
				continue
			}
			if err := b.deliverWithRetry(ctx, sub, event); err != nil {
				b.logger.WithError(err).WithFields(logrus.Fields{
					"event_id": event.ID,
					"topic":    event.Topic,
					"sub_id":   sub.ID,
				}).Warn("Event delivery failed after retries")
				b.statsMu.Lock()
				b.stats.TotalErrors++
				b.statsMu.Unlock()
			} else {
				delivered++
			}
		}
	}

	// Deliver to consumer groups (one handler per group)
	for topic, groups := range b.groups {
		if !topicMatches(topic, event.Topic) {
			continue
		}
		for _, groupSubs := range groups {
			// Round-robin within group: pick first active handler
			for _, sub := range groupSubs {
				if !sub.IsActive() {
					continue
				}
				if err := b.deliverWithRetry(ctx, sub, event); err != nil {
					b.statsMu.Lock()
					b.stats.TotalErrors++
					b.statsMu.Unlock()
				} else {
					delivered++
				}
				break // only one handler per group
			}
		}
	}

	b.statsMu.Lock()
	b.stats.TotalDelivered += int64(delivered)
	b.statsMu.Unlock()

	return nil
}

// Subscribe registers a handler for events matching the topic pattern.
func (b *memoryBus) Subscribe(topic string, handler Handler) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, fmt.Errorf("event bus is closed")
	}

	sub := &Subscription{
		ID:      fmt.Sprintf("sub-%x", time.Now().UnixNano()),
		Topic:   topic,
		Handler: handler,
		active:  true,
	}

	b.subscriptions[topic] = append(b.subscriptions[topic], sub)
	b.updateTopicCount()

	b.logger.WithFields(logrus.Fields{
		"sub_id": sub.ID,
		"topic":  topic,
	}).Debug("Subscription created")

	return sub, nil
}

// SubscribeGroup registers a handler in a named consumer group.
func (b *memoryBus) SubscribeGroup(topic, group string, handler Handler) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, fmt.Errorf("event bus is closed")
	}

	sub := &Subscription{
		ID:      fmt.Sprintf("sub-%x-%s", time.Now().UnixNano(), group),
		Topic:   topic,
		Group:   group,
		Handler: handler,
		active:  true,
	}

	if b.groups[topic] == nil {
		b.groups[topic] = make(map[string][]*Subscription)
	}
	b.groups[topic][group] = append(b.groups[topic][group], sub)
	b.updateTopicCount()

	return sub, nil
}

// Unsubscribe removes a subscription by ID.
func (b *memoryBus) Unsubscribe(subscriptionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for topic, subs := range b.subscriptions {
		for i, sub := range subs {
			if sub.ID == subscriptionID {
				sub.active = false
				b.subscriptions[topic] = append(subs[:i], subs[i+1:]...)
				b.updateTopicCount()
				return nil
			}
		}
	}

	for topic, groups := range b.groups {
		for group, subs := range groups {
			for i, sub := range subs {
				if sub.ID == subscriptionID {
					sub.active = false
					b.groups[topic][group] = append(subs[:i], subs[i+1:]...)
					b.updateTopicCount()
					return nil
				}
			}
		}
	}

	return fmt.Errorf("subscription %s not found", subscriptionID)
}

// Close shuts down the event bus.
func (b *memoryBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true

	// Deactivate all subscriptions
	for _, subs := range b.subscriptions {
		for _, sub := range subs {
			sub.active = false
		}
	}
	for _, groups := range b.groups {
		for _, subs := range groups {
			for _, sub := range subs {
				sub.active = false
			}
		}
	}

	b.logger.Info("Event bus closed")
	return nil
}

// Stats returns runtime statistics.
func (b *memoryBus) Stats() BusStats {
	b.statsMu.Lock()
	defer b.statsMu.Unlock()
	stats := b.stats
	return stats
}

// deliverWithRetry delivers an event to a handler with retry support.
func (b *memoryBus) deliverWithRetry(ctx context.Context, sub *Subscription, event *Event) error {
	maxRetries := b.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := sub.Handler(ctx, event); err != nil {
			lastErr = err
			if attempt < maxRetries-1 {
				delay := b.config.RetryDelay * time.Duration(1<<uint(attempt))
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
			continue
		}
		return nil
	}
	return lastErr
}

func (b *memoryBus) updateTopicCount() {
	topics := make(map[string]struct{})
	totalSubs := 0
	for t, subs := range b.subscriptions {
		if len(subs) > 0 {
			topics[t] = struct{}{}
			totalSubs += len(subs)
		}
	}
	for t, groups := range b.groups {
		for _, subs := range groups {
			if len(subs) > 0 {
				topics[t] = struct{}{}
				totalSubs += len(subs)
			}
		}
	}
	b.statsMu.Lock()
	b.stats.ActiveTopics = len(topics)
	b.stats.ActiveSubscriptions = totalSubs
	b.statsMu.Unlock()
}

// topicMatches checks if a subscription topic pattern matches an event topic.
// Supports wildcards: "cluster.*" matches "cluster.created".
// "*" matches any single segment; ">" matches all remaining segments.
func topicMatches(pattern, topic string) bool {
	if pattern == topic {
		return true
	}

	patParts := strings.Split(pattern, ".")
	topParts := strings.Split(topic, ".")

	for i, pat := range patParts {
		if pat == ">" {
			return true // match all remaining
		}
		if i >= len(topParts) {
			return false
		}
		if pat != "*" && pat != topParts[i] {
			return false
		}
	}

	return len(patParts) == len(topParts)
}

// ============================================================================
// Factory — creates event bus from configuration
// ============================================================================

// New creates an EventBus based on the provided configuration.
// Falls back to in-memory if NATS is unavailable.
func New(cfg Config, logger *logrus.Logger) EventBus {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	switch cfg.Backend {
	case "nats":
		bus, err := NewNATSBus(cfg, logger)
		if err != nil {
			logger.WithError(err).Warn("Failed to connect to NATS, falling back to in-memory event bus")
			return NewMemoryBus(cfg, logger)
		}
		return bus
	default:
		logger.Info("Using in-memory event bus")
		return NewMemoryBus(cfg, logger)
	}
}
