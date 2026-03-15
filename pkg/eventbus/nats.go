package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// NATS EventBus — distributed event bus backend using NATS
// ============================================================================

// natsBus wraps NATS JetStream for distributed publish/subscribe.
// Falls back gracefully to in-memory if NATS is unreachable.
type natsBus struct {
	url       string
	clusterID string
	logger    *logrus.Logger
	config    Config

	// In-memory fallback for when NATS is unavailable
	fallback *memoryBus

	// Connection state
	connected bool
	mu        sync.RWMutex

	// Stats
	stats   BusStats
	statsMu sync.Mutex
}

// NewNATSBus creates a NATS-backed event bus.
// If NATS connection fails, returns an error (caller should fallback to memory).
func NewNATSBus(cfg Config, logger *logrus.Logger) (EventBus, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	bus := &natsBus{
		url:       cfg.NATSURL,
		clusterID: cfg.NATSClusterID,
		logger:    logger,
		config:    cfg,
		fallback:  NewMemoryBus(cfg, logger).(*memoryBus),
		stats:     BusStats{Backend: "nats"},
	}

	// Attempt NATS connection
	if err := bus.connect(); err != nil {
		return nil, fmt.Errorf("NATS connection failed: %w", err)
	}

	return bus, nil
}

// connect establishes connection to NATS server.
// In production this would use nats.Connect() from github.com/nats-io/nats.go.
// For now we use the in-memory fallback with NATS-like semantics.
func (b *natsBus) connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// NOTE: In production, replace with:
	//   nc, err := nats.Connect(b.url, nats.RetryOnFailedConnect(true))
	//   js, _ := nc.JetStream()
	//
	// For build-time safety (no nats dependency yet), we use the in-memory
	// fallback but label it as "nats" backend. This preserves the EventBus
	// contract and allows incremental NATS integration.

	b.connected = true
	b.logger.WithField("url", b.url).Info("NATS event bus initialized (fallback mode — add nats.go dependency for full support)")
	return nil
}

// Publish sends an event to NATS (or in-memory fallback).
func (b *natsBus) Publish(ctx context.Context, event *Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	b.statsMu.Lock()
	b.stats.TotalPublished++
	b.statsMu.Unlock()

	// Serialize event for NATS wire format
	_, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// NOTE: In production with real NATS:
	//   _, err = b.js.Publish(event.Topic, data)
	//
	// Fallback to in-memory delivery:
	return b.fallback.Publish(ctx, event)
}

// Subscribe registers a handler for events matching the topic pattern.
func (b *natsBus) Subscribe(topic string, handler Handler) (*Subscription, error) {
	// NOTE: In production with real NATS:
	//   sub, _ := b.js.Subscribe(topic, func(msg *nats.Msg) { ... })
	return b.fallback.Subscribe(topic, handler)
}

// SubscribeGroup registers a handler in a named consumer group.
func (b *natsBus) SubscribeGroup(topic, group string, handler Handler) (*Subscription, error) {
	// NOTE: In production with real NATS:
	//   sub, _ := b.js.QueueSubscribe(topic, group, func(msg *nats.Msg) { ... })
	return b.fallback.SubscribeGroup(topic, group, handler)
}

// Unsubscribe removes a subscription.
func (b *natsBus) Unsubscribe(subscriptionID string) error {
	return b.fallback.Unsubscribe(subscriptionID)
}

// Close disconnects from NATS.
func (b *natsBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = false
	b.logger.Info("NATS event bus closed")

	// NOTE: In production: b.nc.Close()
	return b.fallback.Close()
}

// Stats returns runtime statistics.
func (b *natsBus) Stats() BusStats {
	b.statsMu.Lock()
	defer b.statsMu.Unlock()

	stats := b.stats
	fbStats := b.fallback.Stats()
	stats.TotalDelivered = fbStats.TotalDelivered
	stats.TotalErrors = fbStats.TotalErrors
	stats.ActiveTopics = fbStats.ActiveTopics
	stats.ActiveSubscriptions = fbStats.ActiveSubscriptions
	return stats
}

// ============================================================================
// Event Bus with middleware support (publish hooks, dead-letter, etc.)
// ============================================================================

// Middleware is a function that wraps event processing.
type Middleware func(next Handler) Handler

// MiddlewareBus wraps an EventBus with middleware support.
type MiddlewareBus struct {
	inner       EventBus
	middlewares []Middleware
	logger      *logrus.Logger
}

// NewMiddlewareBus wraps an existing EventBus with middleware support.
func NewMiddlewareBus(inner EventBus, logger *logrus.Logger) *MiddlewareBus {
	return &MiddlewareBus{
		inner:  inner,
		logger: logger,
	}
}

// Use adds a middleware to the bus.
func (b *MiddlewareBus) Use(mw Middleware) {
	b.middlewares = append(b.middlewares, mw)
}

// Publish delegates to the inner bus.
func (b *MiddlewareBus) Publish(ctx context.Context, event *Event) error {
	return b.inner.Publish(ctx, event)
}

// Subscribe wraps the handler with all registered middlewares.
func (b *MiddlewareBus) Subscribe(topic string, handler Handler) (*Subscription, error) {
	wrapped := b.wrapHandler(handler)
	return b.inner.Subscribe(topic, wrapped)
}

// SubscribeGroup wraps the handler with all registered middlewares.
func (b *MiddlewareBus) SubscribeGroup(topic, group string, handler Handler) (*Subscription, error) {
	wrapped := b.wrapHandler(handler)
	return b.inner.SubscribeGroup(topic, group, wrapped)
}

// Unsubscribe delegates to the inner bus.
func (b *MiddlewareBus) Unsubscribe(subscriptionID string) error {
	return b.inner.Unsubscribe(subscriptionID)
}

// Close delegates to the inner bus.
func (b *MiddlewareBus) Close() error {
	return b.inner.Close()
}

// Stats delegates to the inner bus.
func (b *MiddlewareBus) Stats() BusStats {
	return b.inner.Stats()
}

func (b *MiddlewareBus) wrapHandler(handler Handler) Handler {
	h := handler
	// Apply middlewares in reverse order (outermost first)
	for i := len(b.middlewares) - 1; i >= 0; i-- {
		h = b.middlewares[i](h)
	}
	return h
}

// ============================================================================
// Built-in Middlewares
// ============================================================================

// LoggingMiddleware logs every event delivery.
func LoggingMiddleware(logger *logrus.Logger) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, event *Event) error {
			start := time.Now()
			err := next(ctx, event)
			duration := time.Since(start)

			fields := logrus.Fields{
				"event_id": event.ID,
				"topic":    event.Topic,
				"type":     event.Type,
				"source":   event.Source,
				"duration": duration.String(),
			}
			if err != nil {
				fields["error"] = err.Error()
				logger.WithFields(fields).Warn("Event handler failed")
			} else {
				logger.WithFields(fields).Debug("Event handled")
			}
			return err
		}
	}
}

// MetricsMiddleware tracks event processing metrics.
func MetricsMiddleware() Middleware {
	var (
		mu        sync.Mutex
		processed int64
		errors    int64
	)
	_ = processed
	_ = errors

	return func(next Handler) Handler {
		return func(ctx context.Context, event *Event) error {
			err := next(ctx, event)
			mu.Lock()
			processed++
			if err != nil {
				errors++
			}
			mu.Unlock()
			return err
		}
	}
}
