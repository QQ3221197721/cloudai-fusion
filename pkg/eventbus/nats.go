package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// NATS EventBus — distributed event bus backed by real NATS JetStream
// ============================================================================
//
// This backend connects to a real NATS server using github.com/nats-io/nats.go
// and drives publish/subscribe through JetStream (persistent, at-least-once).
// Failed deliveries are retried up to MaxRetries and then routed to a Dead
// Letter Queue (DLQ) stream. If the NATS server is unavailable at startup the
// bus degrades gracefully to the in-memory backend so callers (and tests) keep
// working without a running broker.
// ============================================================================

// natsBus wraps NATS JetStream for distributed publish/subscribe.
type natsBus struct {
	url       string
	clusterID string
	logger    *logrus.Logger
	config    Config

	// Real NATS/JetStream handles (nil when running in fallback mode).
	conn       *nats.Conn
	js         jetstream.JetStream
	dlqStream  string
	maxRetries int
	natsUp     bool

	// Active JetStream consume contexts keyed by subscription ID.
	subs   map[string]jetstream.ConsumeContext
	subsMu sync.Mutex

	// In-memory fallback for when NATS is unavailable.
	fallback *memoryBus

	// Connection state.
	connected bool
	mu        sync.RWMutex

	// Stats.
	stats   BusStats
	statsMu sync.Mutex
}

// NewNATSBus creates a NATS-backed event bus.
// It attempts a real connection to the configured NATS server; if the server
// is unreachable it falls back to an in-memory bus (labelled "nats") so the
// process still functions. It only returns an error for unrecoverable setup
// problems, never merely because the broker is down.
func NewNATSBus(cfg Config, logger *logrus.Logger) (EventBus, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	bus := &natsBus{
		url:        cfg.NATSURL,
		clusterID:  cfg.NATSClusterID,
		logger:     logger,
		config:     cfg,
		maxRetries: maxRetries,
		subs:       make(map[string]jetstream.ConsumeContext),
		fallback:   NewMemoryBus(cfg, logger).(*memoryBus),
		stats:      BusStats{Backend: "nats"},
	}

	if err := bus.connect(); err != nil {
		return nil, fmt.Errorf("NATS event bus init failed: %w", err)
	}

	return bus, nil
}

// connect establishes a real connection to the NATS server and initializes
// JetStream plus the DLQ stream. On any connectivity failure it flips the bus
// into in-memory fallback mode instead of returning an error.
func (b *natsBus) connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	url := b.url
	if url == "" {
		url = nats.DefaultURL
	}

	// Real NATS connection. RetryOnFailedConnect makes Connect return a
	// (reconnecting) handle instead of an error when the server is down, so we
	// explicitly verify connectivity via IsConnected() below.
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(2*time.Second),
	)
	if err != nil || nc == nil || !nc.IsConnected() {
		if nc != nil {
			nc.Close()
		}
		b.natsUp = false
		b.connected = true // usable via in-memory fallback
		b.logger.WithField("url", url).Warn("NATS server unavailable — using in-memory fallback")
		return nil
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		b.natsUp = false
		b.connected = true
		b.logger.WithError(err).Warn("JetStream init failed — using in-memory fallback")
		return nil
	}

	// Create the Dead Letter Queue stream (in-memory, 7-day retention).
	b.dlqStream = "EVENTS_DLQ"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     b.dlqStream,
		Subjects: []string{"dlq.>"},
		Storage:  jetstream.MemoryStorage,
		MaxAge:   7 * 24 * time.Hour,
	}); err != nil {
		b.logger.WithError(err).Warn("failed to create DLQ stream")
	}

	b.conn = nc
	b.js = js
	b.natsUp = true
	b.connected = true
	b.logger.WithField("url", url).Info("Connected to NATS JetStream")
	return nil
}

// Publish sends an event to NATS JetStream (or the in-memory fallback).
func (b *natsBus) Publish(ctx context.Context, event *Event) error {
	b.statsMu.Lock()
	b.stats.TotalPublished++
	b.statsMu.Unlock()

	b.mu.RLock()
	up := b.natsUp
	js := b.js
	b.mu.RUnlock()

	if !up {
		return b.fallback.Publish(ctx, event)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	if _, err := js.Publish(ctx, event.Topic, data); err != nil {
		b.statsMu.Lock()
		b.stats.TotalErrors++
		b.statsMu.Unlock()
		return fmt.Errorf("nats publish failed: %w", err)
	}

	b.statsMu.Lock()
	b.stats.TotalDelivered++
	b.statsMu.Unlock()
	return nil
}

// Subscribe registers a handler for events on the given topic.
func (b *natsBus) Subscribe(topic string, handler Handler) (*Subscription, error) {
	b.mu.RLock()
	up := b.natsUp
	b.mu.RUnlock()

	if !up {
		return b.fallback.Subscribe(topic, handler)
	}
	return b.subscribeJetStream(topic, "", handler)
}

// SubscribeGroup registers a handler in a named consumer group. In JetStream
// this maps to a shared durable consumer so events are load-balanced across
// group members.
func (b *natsBus) SubscribeGroup(topic, group string, handler Handler) (*Subscription, error) {
	b.mu.RLock()
	up := b.natsUp
	b.mu.RUnlock()

	if !up {
		return b.fallback.SubscribeGroup(topic, group, handler)
	}
	return b.subscribeJetStream(topic, group, handler)
}

// subscribeJetStream provisions a stream + durable consumer for the topic and
// starts consuming messages with retry/DLQ semantics.
func (b *natsBus) subscribeJetStream(topic, group string, handler Handler) (*Subscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := b.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     sanitizeStreamName(topic),
		Subjects: []string{topic},
	})
	if err != nil {
		return nil, fmt.Errorf("create stream for %q: %w", topic, err)
	}

	durable := "consumer-" + sanitizeStreamName(topic)
	if group != "" {
		durable = "group-" + sanitizeStreamName(group) + "-" + sanitizeStreamName(topic)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:    durable,
		AckPolicy:  jetstream.AckExplicitPolicy,
		MaxDeliver: b.maxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer for %q: %w", topic, err)
	}

	sub := &Subscription{
		ID:      fmt.Sprintf("nats-sub-%x", time.Now().UnixNano()),
		Topic:   topic,
		Group:   group,
		Handler: handler,
		active:  true,
	}

	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		b.handleNATSMessage(context.Background(), topic, msg, handler)
	})
	if err != nil {
		return nil, fmt.Errorf("start consume for %q: %w", topic, err)
	}

	b.subsMu.Lock()
	b.subs[sub.ID] = cc
	b.subsMu.Unlock()

	b.logger.WithFields(logrus.Fields{
		"sub_id": sub.ID,
		"topic":  topic,
		"group":  group,
	}).Debug("JetStream subscription created")

	return sub, nil
}

// handleNATSMessage decodes and dispatches a JetStream message, applying the
// retry-then-DLQ policy on handler failure.
func (b *natsBus) handleNATSMessage(ctx context.Context, topic string, msg jetstream.Msg, handler Handler) {
	var event Event
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		// Poison message: cannot be decoded, so route straight to the DLQ and
		// terminate to avoid pointless redelivery.
		b.routeToDLQ(ctx, topic, msg)
		_ = msg.Term()
		b.statsMu.Lock()
		b.stats.TotalErrors++
		b.statsMu.Unlock()
		return
	}

	if err := safeInvoke(ctx, handler, &event); err != nil {
		b.statsMu.Lock()
		b.stats.TotalErrors++
		b.statsMu.Unlock()

		numDelivered := uint64(1)
		if md, mderr := msg.Metadata(); mderr == nil {
			numDelivered = md.NumDelivered
		}

		if numDelivered >= uint64(b.maxRetries) {
			// Exhausted retries: move to DLQ and terminate redelivery.
			b.routeToDLQ(ctx, topic, msg)
			_ = msg.Term()
			return
		}

		// Negative ack triggers redelivery per the consumer's MaxDeliver.
		_ = msg.Nak()
		return
	}

	_ = msg.Ack()
	b.statsMu.Lock()
	b.stats.TotalDelivered++
	b.statsMu.Unlock()
}

// routeToDLQ republishes a failed message onto the Dead Letter Queue stream
// under the "dlq.<originalTopic>" subject.
func (b *natsBus) routeToDLQ(ctx context.Context, originalTopic string, msg jetstream.Msg) {
	if b.js == nil {
		return
	}
	dlqSubject := fmt.Sprintf("dlq.%s", originalTopic)
	msgID := fmt.Sprintf("dlq-%s-%x", sanitizeStreamName(originalTopic), time.Now().UnixNano())

	if _, err := b.js.Publish(ctx, dlqSubject, msg.Data(), jetstream.WithMsgID(msgID)); err != nil {
		b.logger.WithError(err).WithField("subject", dlqSubject).Warn("failed to route message to DLQ")
		return
	}
	b.logger.WithFields(logrus.Fields{
		"dlq_subject":    dlqSubject,
		"original_topic": originalTopic,
	}).Warn("message routed to dead letter queue")
}

// Unsubscribe stops the JetStream consumer for the subscription ID.
func (b *natsBus) Unsubscribe(subscriptionID string) error {
	b.mu.RLock()
	up := b.natsUp
	b.mu.RUnlock()

	if !up {
		return b.fallback.Unsubscribe(subscriptionID)
	}

	b.subsMu.Lock()
	cc, ok := b.subs[subscriptionID]
	if ok {
		cc.Stop()
		delete(b.subs, subscriptionID)
	}
	b.subsMu.Unlock()

	if !ok {
		return fmt.Errorf("subscription %s not found", subscriptionID)
	}
	return nil
}

// Close stops all consumers and drains the NATS connection.
func (b *natsBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.subsMu.Lock()
	for _, cc := range b.subs {
		cc.Stop()
	}
	b.subs = make(map[string]jetstream.ConsumeContext)
	b.subsMu.Unlock()

	if b.conn != nil {
		_ = b.conn.Drain()
		b.conn.Close()
		b.conn = nil
	}

	b.natsUp = false
	b.connected = false
	b.logger.Info("NATS event bus closed")

	return b.fallback.Close()
}

// Stats returns runtime statistics. In fallback mode delivery counters are
// merged from the in-memory bus; in NATS mode they are tracked directly.
func (b *natsBus) Stats() BusStats {
	b.mu.RLock()
	up := b.natsUp
	b.mu.RUnlock()

	b.statsMu.Lock()
	stats := b.stats
	b.statsMu.Unlock()

	if !up {
		fb := b.fallback.Stats()
		stats.TotalDelivered = fb.TotalDelivered
		stats.TotalErrors = fb.TotalErrors
		stats.ActiveTopics = fb.ActiveTopics
		stats.ActiveSubscriptions = fb.ActiveSubscriptions
	} else {
		b.subsMu.Lock()
		stats.ActiveSubscriptions = len(b.subs)
		stats.ActiveTopics = len(b.subs)
		b.subsMu.Unlock()
	}
	return stats
}

// sanitizeStreamName converts a topic into a valid JetStream stream/consumer
// name. NATS names cannot contain whitespace, '.', '*', '>', or path
// separators, so any disallowed rune is replaced with '_'.
func sanitizeStreamName(topic string) string {
	var sb strings.Builder
	for _, r := range topic {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	name := sb.String()
	if name == "" {
		name = "stream"
	}
	return name
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
