// Package messaging — real NATS driver (nats.go).
//
// Implements Producer/Consumer against a live NATS server: Publish maps to a
// subject publish, Subscribe uses queue groups for work distribution across
// consumer instances. The factory in queue.go selects this driver when
// backend=="nats" and the server is reachable, registering a "real" capability;
// otherwise it falls back to the in-memory driver and registers "simulated"
// (which run_mode=production rejects at boot).
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

// natsConnect dials NATS with a bounded initial-connect timeout so an
// unreachable server fails fast (letting the factory fall back honestly).
func natsConnect(url, name string) (*nats.Conn, error) {
	if url == "" {
		return nil, fmt.Errorf("nats url is empty")
	}
	return nats.Connect(url,
		nats.Name(name),
		nats.Timeout(2*time.Second),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
}

// ---- Producer ----

type natsProducer struct {
	nc     *nats.Conn
	logger *logrus.Logger
}

func newNATSProducer(cfg Config, logger *logrus.Logger) (*natsProducer, error) {
	nc, err := natsConnect(cfg.NATSURL, "cloudai-fusion-producer")
	if err != nil {
		return nil, err
	}
	return &natsProducer{nc: nc, logger: logger}, nil
}

func (p *natsProducer) Publish(ctx context.Context, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	if err := p.nc.Publish(msg.Queue, data); err != nil {
		return err
	}
	return p.nc.FlushWithContext(ctx)
}

func (p *natsProducer) PublishDelayed(ctx context.Context, msg *Message, delay time.Duration) error {
	// Core NATS has no native delayed delivery; schedule client-side.
	go func() {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-t.C:
			if err := p.Publish(context.Background(), msg); err != nil {
				p.logger.WithError(err).Warn("nats: delayed publish failed")
			}
		case <-ctx.Done():
		}
	}()
	return nil
}

func (p *natsProducer) Close() error {
	if p.nc != nil {
		p.nc.Close()
	}
	return nil
}

// ---- Consumer ----

type natsConsumer struct {
	nc     *nats.Conn
	logger *logrus.Logger
	mu     sync.Mutex
	subs   map[string]*nats.Subscription
}

func newNATSConsumer(cfg Config, logger *logrus.Logger) (*natsConsumer, error) {
	nc, err := natsConnect(cfg.NATSURL, "cloudai-fusion-consumer")
	if err != nil {
		return nil, err
	}
	return &natsConsumer{nc: nc, logger: logger, subs: make(map[string]*nats.Subscription)}, nil
}

func (c *natsConsumer) Subscribe(queue, group string, handler MessageHandler) error {
	cb := func(m *nats.Msg) {
		var msg Message
		if err := json.Unmarshal(m.Data, &msg); err != nil {
			c.logger.WithError(err).Warn("nats: dropping malformed message")
			return
		}
		if err := handler(context.Background(), &msg); err != nil {
			c.logger.WithError(err).Warn("nats: handler returned error (message not acknowledged)")
		}
	}

	var (
		sub *nats.Subscription
		err error
	)
	if group != "" {
		sub, err = c.nc.QueueSubscribe(queue, group, cb) // work distribution across group members
	} else {
		sub, err = c.nc.Subscribe(queue, cb)
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.subs[queue] = sub
	c.mu.Unlock()
	return nil
}

func (c *natsConsumer) Unsubscribe(queue string) error {
	c.mu.Lock()
	sub := c.subs[queue]
	delete(c.subs, queue)
	c.mu.Unlock()
	if sub != nil {
		return sub.Unsubscribe()
	}
	return nil
}

func (c *natsConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.subs {
		_ = s.Unsubscribe()
	}
	if c.nc != nil {
		c.nc.Close()
	}
	return nil
}
