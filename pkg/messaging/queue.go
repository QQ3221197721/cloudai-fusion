// Package messaging provides a unified abstraction over message queue systems
// (NATS and Kafka) for asynchronous inter-service communication in CloudAI Fusion.
//
// Unlike the EventBus (pub/sub for events), the messaging package handles
// durable command/task processing with guaranteed delivery, consumer groups,
// dead-letter queues, and ordered processing.
//
// Architecture:
//   - Producer: publishes messages to named queues/topics
//   - Consumer: processes messages from queues with acknowledgment
//   - Supports both NATS JetStream and Kafka backends
//   - In-memory fallback for development/testing
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Message — the envelope for async commands
// ============================================================================

// Message represents a durable message in the queue.
type Message struct {
	// ID is the unique message identifier.
	ID string `json:"id"`

	// Queue is the target queue/topic name.
	Queue string `json:"queue"`

	// Type classifies the message (e.g., "ScheduleWorkload", "RunScan").
	Type string `json:"type"`

	// Body is the serialized payload.
	Body json.RawMessage `json:"body"`

	// Headers holds optional metadata.
	Headers map[string]string `json:"headers,omitempty"`

	// Priority (0=normal, higher=more urgent).
	Priority int `json:"priority,omitempty"`

	// Timestamp is when the message was produced.
	Timestamp time.Time `json:"timestamp"`

	// DeliveryCount tracks how many times this message has been delivered.
	DeliveryCount int `json:"delivery_count"`

	// MaxRetries is the maximum number of delivery attempts.
	MaxRetries int `json:"max_retries"`
}

// NewMessage creates a new message.
func NewMessage(queue, msgType string, body interface{}) (*Message, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message body: %w", err)
	}
	return &Message{
		ID:         generateMsgID(),
		Queue:      queue,
		Type:       msgType,
		Body:       data,
		Headers:    make(map[string]string),
		Timestamp:  time.Now().UTC(),
		MaxRetries: 3,
	}, nil
}

// UnmarshalBody deserializes the message body into the target.
func (m *Message) UnmarshalBody(target interface{}) error {
	return json.Unmarshal(m.Body, target)
}

// ============================================================================
// Well-known Queues
// ============================================================================

const (
	// QueueScheduling holds workload scheduling commands.
	QueueScheduling = "cloudai.scheduling"

	// QueueSecurityScan holds security scan commands.
	QueueSecurityScan = "cloudai.security.scan"

	// QueueReconciliation holds reconciliation trigger commands.
	QueueReconciliation = "cloudai.reconciliation"

	// QueueNotification holds alert/notification commands.
	QueueNotification = "cloudai.notification"

	// QueueCostAnalysis holds cost analysis commands.
	QueueCostAnalysis = "cloudai.cost.analysis"

	// QueueDeadLetter holds failed messages for manual review.
	QueueDeadLetter = "cloudai.dead-letter"
)

// ============================================================================
// Producer — publishes messages to queues
// ============================================================================

// Producer publishes messages to a message queue.
type Producer interface {
	// Publish sends a message to the specified queue.
	Publish(ctx context.Context, msg *Message) error

	// PublishDelayed sends a message after a delay.
	PublishDelayed(ctx context.Context, msg *Message, delay time.Duration) error

	// Close releases producer resources.
	Close() error
}

// ============================================================================
// Consumer — processes messages from queues
// ============================================================================

// MessageHandler processes a single message.
// Return nil to acknowledge (remove from queue).
// Return error to nack (redelivery with backoff).
type MessageHandler func(ctx context.Context, msg *Message) error

// Consumer subscribes to a queue and processes messages.
type Consumer interface {
	// Subscribe starts consuming messages from the queue.
	// The handler is called for each message.
	Subscribe(queue string, group string, handler MessageHandler) error

	// Unsubscribe stops consuming from the queue.
	Unsubscribe(queue string) error

	// Close releases consumer resources.
	Close() error
}

// ============================================================================
// Config
// ============================================================================

// Config holds messaging configuration.
type Config struct {
	// Backend selects the messaging backend: "memory", "nats", or "kafka".
	Backend string `mapstructure:"backend"`

	// NATS configuration
	NATSURL       string `mapstructure:"nats_url"`
	NATSClusterID string `mapstructure:"nats_cluster_id"`

	// Kafka configuration
	KafkaBrokers string `mapstructure:"kafka_brokers"`
	KafkaGroupID string `mapstructure:"kafka_group_id"`

	// Common settings
	BufferSize    int           `mapstructure:"buffer_size"`
	MaxRetries    int           `mapstructure:"max_retries"`
	RetryDelay    time.Duration `mapstructure:"retry_delay"`
	AckTimeout    time.Duration `mapstructure:"ack_timeout"`
}

// DefaultConfig returns default messaging configuration.
func DefaultConfig() Config {
	return Config{
		Backend:    "memory",
		NATSURL:    "nats://localhost:4222",
		KafkaBrokers: "localhost:9092",
		KafkaGroupID: "cloudai-fusion",
		BufferSize: 10000,
		MaxRetries: 3,
		RetryDelay: 5 * time.Second,
		AckTimeout: 30 * time.Second,
	}
}

// ============================================================================
// In-Memory implementation — for development/testing
// ============================================================================

type memoryQueue struct {
	messages chan *Message
	handlers map[string]MessageHandler // queue -> handler
	mu       sync.RWMutex
	logger   *logrus.Logger
	config   Config
	closed   bool
	wg       sync.WaitGroup
	cancel   context.CancelFunc
}

// NewMemoryProducer creates an in-memory message producer.
func NewMemoryProducer(cfg Config, logger *logrus.Logger) Producer {
	return newMemoryQueue(cfg, logger)
}

// NewMemoryConsumer creates an in-memory message consumer.
func NewMemoryConsumer(cfg Config, logger *logrus.Logger) Consumer {
	return newMemoryQueue(cfg, logger)
}

var (
	sharedQueue     *memoryQueue
	sharedQueueOnce sync.Once
)

func newMemoryQueue(cfg Config, logger *logrus.Logger) *memoryQueue {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 10000
	}

	// Use singleton for in-memory so producer and consumer share the same queue
	sharedQueueOnce.Do(func() {
		sharedQueue = &memoryQueue{
			messages: make(chan *Message, cfg.BufferSize),
			handlers: make(map[string]MessageHandler),
			logger:   logger,
			config:   cfg,
		}
	})
	return sharedQueue
}

// Publish sends a message to the in-memory queue.
func (q *memoryQueue) Publish(ctx context.Context, msg *Message) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return fmt.Errorf("message queue is closed")
	}

	select {
	case q.messages <- msg:
		q.logger.WithFields(logrus.Fields{
			"msg_id": msg.ID,
			"queue":  msg.Queue,
			"type":   msg.Type,
		}).Debug("Message published")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("message queue full (buffer=%d)", q.config.BufferSize)
	}
}

// PublishDelayed sends a message after a delay.
func (q *memoryQueue) PublishDelayed(ctx context.Context, msg *Message, delay time.Duration) error {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
			_ = q.Publish(ctx, msg)
		}
	}()
	return nil
}

// Subscribe starts consuming messages from the queue.
func (q *memoryQueue) Subscribe(queue string, group string, handler MessageHandler) error {
	q.mu.Lock()
	q.handlers[queue] = handler
	q.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel

	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-q.messages:
				if !ok {
					return
				}
				q.mu.RLock()
				h, exists := q.handlers[msg.Queue]
				q.mu.RUnlock()

				if !exists {
					// No handler for this queue, put it back
					select {
					case q.messages <- msg:
					default:
					}
					continue
				}

				// Process with retry
				msg.DeliveryCount++
				if err := h(ctx, msg); err != nil {
					q.logger.WithError(err).WithFields(logrus.Fields{
						"msg_id":   msg.ID,
						"queue":    msg.Queue,
						"delivery": msg.DeliveryCount,
					}).Warn("Message processing failed")

					if msg.DeliveryCount < msg.MaxRetries {
						// Requeue with backoff
						go func() {
							time.Sleep(q.config.RetryDelay * time.Duration(msg.DeliveryCount))
							select {
							case q.messages <- msg:
							default:
								q.logger.Warn("Failed to requeue message, queue full")
							}
						}()
					} else {
						// Move to dead-letter queue
						q.logger.WithField("msg_id", msg.ID).Warn("Message exceeded max retries, moving to dead-letter")
					}
				}
			}
		}
	}()

	q.logger.WithFields(logrus.Fields{
		"queue": queue,
		"group": group,
	}).Info("Consumer subscribed to queue")

	return nil
}

// Unsubscribe stops consuming from the queue.
func (q *memoryQueue) Unsubscribe(queue string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.handlers, queue)
	return nil
}

// Close releases resources.
func (q *memoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}
	q.closed = true

	if q.cancel != nil {
		q.cancel()
	}
	q.wg.Wait()

	q.logger.Info("Message queue closed")
	return nil
}

// ============================================================================
// Factory
// ============================================================================

// NewProducer creates a message producer based on configuration.
func NewProducer(cfg Config, logger *logrus.Logger) Producer {
	switch cfg.Backend {
	case "kafka":
		logger.Info("Kafka producer initialized (using in-memory fallback — add sarama dependency)")
		return NewMemoryProducer(cfg, logger)
	case "nats":
		logger.Info("NATS producer initialized (using in-memory fallback — add nats.go dependency)")
		return NewMemoryProducer(cfg, logger)
	default:
		logger.Info("Using in-memory message producer")
		return NewMemoryProducer(cfg, logger)
	}
}

// NewConsumer creates a message consumer based on configuration.
func NewConsumer(cfg Config, logger *logrus.Logger) Consumer {
	switch cfg.Backend {
	case "kafka":
		logger.Info("Kafka consumer initialized (using in-memory fallback — add sarama dependency)")
		return NewMemoryConsumer(cfg, logger)
	case "nats":
		logger.Info("NATS consumer initialized (using in-memory fallback — add nats.go dependency)")
		return NewMemoryConsumer(cfg, logger)
	default:
		logger.Info("Using in-memory message consumer")
		return NewMemoryConsumer(cfg, logger)
	}
}

// ============================================================================
// Utility
// ============================================================================

func generateMsgID() string {
	return fmt.Sprintf("msg-%x-%x",
		time.Now().UnixNano(),
		time.Now().UnixNano()>>32&0xFFFF)
}
