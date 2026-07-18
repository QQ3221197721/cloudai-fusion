// Package messaging — real Kafka driver (IBM/sarama).
//
// Implements Producer/Consumer against a live Kafka cluster: Publish is a
// synchronous produce with acks=all; Subscribe joins a consumer group for
// ordered, at-least-once, load-balanced consumption. The factory in queue.go
// selects this driver when backend=="kafka" and the brokers are reachable,
// registering a "real" capability; otherwise it falls back to the in-memory
// driver and registers "simulated" (which run_mode=production rejects at boot).
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/sirupsen/logrus"
)

func splitBrokers(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ---- Producer ----

type kafkaProducer struct {
	producer sarama.SyncProducer
	logger   *logrus.Logger
}

func newKafkaProducer(cfg Config, logger *logrus.Logger) (*kafkaProducer, error) {
	brokers := splitBrokers(cfg.KafkaBrokers)
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no kafka brokers configured")
	}
	sc := sarama.NewConfig()
	sc.Version = sarama.V2_1_0_0
	sc.Producer.RequiredAcks = sarama.WaitForAll // durability
	sc.Producer.Retry.Max = cfg.MaxRetries
	sc.Producer.Return.Successes = true
	sc.Net.DialTimeout = 3 * time.Second

	p, err := sarama.NewSyncProducer(brokers, sc)
	if err != nil {
		return nil, err
	}
	return &kafkaProducer{producer: p, logger: logger}, nil
}

func (p *kafkaProducer) Publish(_ context.Context, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	_, _, err = p.producer.SendMessage(&sarama.ProducerMessage{
		Topic: msg.Queue,
		Value: sarama.ByteEncoder(data),
	})
	return err
}

func (p *kafkaProducer) PublishDelayed(ctx context.Context, msg *Message, delay time.Duration) error {
	go func() {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
			if err := p.Publish(context.Background(), msg); err != nil {
				p.logger.WithError(err).Warn("kafka: delayed publish failed")
			}
		case <-ctx.Done():
		}
	}()
	return nil
}

func (p *kafkaProducer) Close() error { return p.producer.Close() }

// ---- Consumer ----

type kafkaConsumer struct {
	brokers []string
	cfg     Config
	logger  *logrus.Logger
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func newKafkaConsumer(cfg Config, logger *logrus.Logger) (*kafkaConsumer, error) {
	brokers := splitBrokers(cfg.KafkaBrokers)
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no kafka brokers configured")
	}
	// Verify connectivity up front so the factory can fall back honestly.
	sc := sarama.NewConfig()
	sc.Version = sarama.V2_1_0_0
	sc.Net.DialTimeout = 3 * time.Second
	client, err := sarama.NewClient(brokers, sc)
	if err != nil {
		return nil, err
	}
	_ = client.Close()
	return &kafkaConsumer{brokers: brokers, cfg: cfg, logger: logger, cancels: make(map[string]context.CancelFunc)}, nil
}

// kafkaCGHandler adapts a MessageHandler to sarama's ConsumerGroupHandler.
type kafkaCGHandler struct {
	handler MessageHandler
	logger  *logrus.Logger
}

func (h *kafkaCGHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *kafkaCGHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *kafkaCGHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for m := range claim.Messages() {
		var msg Message
		if err := json.Unmarshal(m.Value, &msg); err != nil {
			h.logger.WithError(err).Warn("kafka: dropping malformed message")
			sess.MarkMessage(m, "")
			continue
		}
		if err := h.handler(sess.Context(), &msg); err != nil {
			h.logger.WithError(err).Warn("kafka: handler error (offset not committed for redelivery)")
			continue // do not mark → at-least-once redelivery
		}
		sess.MarkMessage(m, "")
	}
	return nil
}

func (c *kafkaConsumer) Subscribe(queue, group string, handler MessageHandler) error {
	if group == "" {
		group = c.cfg.KafkaGroupID
	}
	sc := sarama.NewConfig()
	sc.Version = sarama.V2_1_0_0
	sc.Consumer.Offsets.Initial = sarama.OffsetOldest
	sc.Net.DialTimeout = 3 * time.Second

	cg, err := sarama.NewConsumerGroup(c.brokers, group, sc)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancels[queue] = cancel
	c.mu.Unlock()

	h := &kafkaCGHandler{handler: handler, logger: c.logger}
	go func() {
		defer func() { _ = cg.Close() }()
		for {
			if err := cg.Consume(ctx, []string{queue}, h); err != nil {
				c.logger.WithError(err).Warn("kafka: consume loop error")
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return nil
}

func (c *kafkaConsumer) Unsubscribe(queue string) error {
	c.mu.Lock()
	cancel := c.cancels[queue]
	delete(c.cancels, queue)
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (c *kafkaConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cancel := range c.cancels {
		cancel()
	}
	c.cancels = make(map[string]context.CancelFunc)
	return nil
}
