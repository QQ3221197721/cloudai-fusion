// Package cache — real Redis driver (go-redis v9).
//
// This file provides the REAL distributed-cache, distributed-lock and pub/sub
// implementations backed by Redis. The factories in cache.go attempt to build a
// live client via newRedisClient; on success they return these real types and
// register a "real" capability, otherwise they fall back to the in-memory
// implementations and register a "simulated" capability (which run_mode=production
// rejects at boot). This replaces the previous "stub — add go-redis" behavior.
package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// newRedisClient builds a go-redis client and verifies connectivity with a
// short ping. It returns an error when Redis is unreachable so the caller can
// decide, per run-mode policy, whether to fall back or fail fast.
func newRedisClient(addr, password string, db int) (*redis.Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("redis address is empty")
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// ============================================================================
// Real Redis cache
// ============================================================================

type redisRealCache struct {
	client *redis.Client
	prefix string
	logger *logrus.Logger
	mu     sync.Mutex
	stats  CacheStats
}

func (c *redisRealCache) key(k string) string { return c.prefix + k }

func (c *redisRealCache) bump(f func(*CacheStats)) {
	c.mu.Lock()
	f(&c.stats)
	c.mu.Unlock()
}

func (c *redisRealCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, c.key(key)).Bytes()
	if err == redis.Nil {
		c.bump(func(s *CacheStats) { s.Misses++ })
		return nil, nil
	}
	if err != nil {
		c.bump(func(s *CacheStats) { s.Errors++ })
		return nil, err
	}
	c.bump(func(s *CacheStats) { s.Hits++ })
	return val, nil
}

func (c *redisRealCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := c.client.Set(ctx, c.key(key), value, ttl).Err(); err != nil {
		c.bump(func(s *CacheStats) { s.Errors++ })
		return err
	}
	c.bump(func(s *CacheStats) { s.Sets++ })
	return nil
}

func (c *redisRealCache) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, c.key(key)).Err(); err != nil {
		return err
	}
	c.bump(func(s *CacheStats) { s.Deletes++ })
	return nil
}

func (c *redisRealCache) DeletePattern(ctx context.Context, pattern string) error {
	iter := c.client.Scan(ctx, 0, c.key(pattern), 100).Iterator()
	for iter.Next(ctx) {
		if err := c.client.Del(ctx, iter.Val()).Err(); err != nil {
			return err
		}
	}
	return iter.Err()
}

func (c *redisRealCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, c.key(key)).Result()
	return n > 0, err
}

func (c *redisRealCache) Close() error { return c.client.Close() }

func (c *redisRealCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	s.Backend = "redis"
	if total := s.Hits + s.Misses; total > 0 {
		s.HitRate = float64(s.Hits) / float64(total)
	}
	return s
}

// ============================================================================
// Real Redis distributed lock (SET NX + owner-checked Lua release/renew)
// ============================================================================

var redisUnlockScript = redis.NewScript(
	`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)

var redisRenewScript = redis.NewScript(
	`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`)

type redisRealLock struct {
	client  *redis.Client
	prefix  string
	ownerID string
}

func (l *redisRealLock) key(k string) string { return l.prefix + k }

func (l *redisRealLock) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return l.client.SetNX(ctx, l.key(key), l.ownerID, ttl).Result()
}

func (l *redisRealLock) Release(ctx context.Context, key string) error {
	return redisUnlockScript.Run(ctx, l.client, []string{l.key(key)}, l.ownerID).Err()
}

func (l *redisRealLock) Renew(ctx context.Context, key string, ttl time.Duration) error {
	res, err := redisRenewScript.Run(ctx, l.client, []string{l.key(key)}, l.ownerID, ttl.Milliseconds()).Result()
	if err != nil {
		return err
	}
	if n, ok := res.(int64); ok && n == 0 {
		return fmt.Errorf("cannot renew lock %q: not owner or expired", key)
	}
	return nil
}

func (l *redisRealLock) IsHeld(ctx context.Context, key string) (bool, error) {
	v, err := l.client.Get(ctx, l.key(key)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == l.ownerID, nil
}

// ============================================================================
// Real Redis pub/sub
// ============================================================================

type redisRealPubSub struct {
	client *redis.Client
	prefix string
	mu     sync.Mutex
	subs   map[string]*redis.PubSub
}

func (p *redisRealPubSub) channel(c string) string { return p.prefix + c }

func (p *redisRealPubSub) Publish(ctx context.Context, channel string, message []byte) error {
	return p.client.Publish(ctx, p.channel(channel), message).Err()
}

func (p *redisRealPubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, error) {
	ps := p.client.Subscribe(ctx, p.channel(channel))
	out := make(chan []byte, 256)

	p.mu.Lock()
	if p.subs == nil {
		p.subs = make(map[string]*redis.PubSub)
	}
	p.subs[channel] = ps
	p.mu.Unlock()

	go func() {
		defer close(out)
		rc := ps.Channel()
		for {
			select {
			case <-ctx.Done():
				_ = ps.Close()
				return
			case msg, ok := <-rc:
				if !ok {
					return
				}
				select {
				case out <- []byte(msg.Payload):
				default: // drop if subscriber is slow (fire-and-forget)
				}
			}
		}
	}()
	return out, nil
}

func (p *redisRealPubSub) Unsubscribe(_ context.Context, channel string) error {
	p.mu.Lock()
	ps := p.subs[channel]
	delete(p.subs, channel)
	p.mu.Unlock()
	if ps != nil {
		return ps.Close()
	}
	return nil
}

func (p *redisRealPubSub) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ps := range p.subs {
		_ = ps.Close()
	}
	p.subs = make(map[string]*redis.PubSub)
	return nil
}
