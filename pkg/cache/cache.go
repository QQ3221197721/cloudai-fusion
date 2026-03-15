// Package cache provides a distributed caching layer for CloudAI Fusion.
// Supports Redis Cluster as the primary backend with an in-memory LRU cache
// as L1 (local process cache) and Redis as L2 (distributed shared cache).
//
// Architecture:
//   L1 (in-memory) → L2 (Redis Cluster) → PostgreSQL (source of truth)
//
// Cache invalidation follows a write-through pattern:
//   - On write: invalidate L1 & L2, write to DB
//   - On read: check L1 → L2 → DB, populate back up
//
// ============================================================================
// Messaging/Locking Responsibility Matrix (Redis vs Kafka vs NATS)
// ============================================================================
//
// Redis:
//   ✓ L2 distributed cache (GET/SET with TTL)
//   ✓ Distributed locking (SET NX EX — scheduler leader election, queue CAS)
//   ✓ Lightweight pub/sub (cross-instance cache invalidation, node cache refresh)
//   ✗ NOT for durable event streams (use Kafka)
//   ✗ NOT for complex routing/fan-out (use NATS)
//
// Kafka:
//   ✓ Durable event sourcing (audit logs, workload lifecycle events)
//   ✓ High-throughput ordered streams (metrics ingestion)
//   ✓ Consumer group processing (multi-consumer workload dispatch)
//   ✗ NOT for low-latency pub/sub (use Redis or NATS)
//
// NATS:
//   ✓ Real-time lightweight pub/sub (event bus, controller triggers)
//   ✓ Request-reply RPC (agent ↔ control-plane)
//   ✓ Queue groups (work distribution to agents)
//   ✗ NOT for durable storage (use Kafka)
//   ✗ NOT for distributed locking (use Redis)
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Cache Interface — unified contract for all cache backends
// ============================================================================

// Cache provides typed key-value caching operations.
type Cache interface {
	// Get retrieves a value from the cache.
	// Returns (nil, nil) if the key is not found (cache miss).
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores a value in the cache with a TTL.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a key from the cache.
	Delete(ctx context.Context, key string) error

	// DeletePattern removes all keys matching a glob pattern.
	DeletePattern(ctx context.Context, pattern string) error

	// Exists checks if a key exists in the cache.
	Exists(ctx context.Context, key string) (bool, error)

	// Close releases cache resources.
	Close() error

	// Stats returns cache statistics.
	Stats() CacheStats
}

// CacheStats holds cache performance metrics.
type CacheStats struct {
	Backend    string `json:"backend"`
	Hits       int64  `json:"hits"`
	Misses     int64  `json:"misses"`
	Sets       int64  `json:"sets"`
	Deletes    int64  `json:"deletes"`
	Errors     int64  `json:"errors"`
	KeyCount   int64  `json:"key_count"`
	MemoryUsed int64  `json:"memory_used_bytes,omitempty"`
	HitRate    float64 `json:"hit_rate"`
}

// ============================================================================
// Config
// ============================================================================

// Config holds cache configuration.
type Config struct {
	// Backend: "memory" (default), "redis", or "multi" (L1+L2).
	Backend string `mapstructure:"backend"`

	// Redis configuration
	RedisAddr     string `mapstructure:"redis_addr"`
	RedisPassword string `mapstructure:"redis_password"` //nolint:gosec // G101: config field, not a hardcoded credential
	RedisDB       int    `mapstructure:"redis_db"`

	// Redis Cluster configuration
	RedisClusterAddrs []string `mapstructure:"redis_cluster_addrs"`

	// L1 (in-memory) settings
	L1MaxSize    int           `mapstructure:"l1_max_size"`    // max entries
	L1DefaultTTL time.Duration `mapstructure:"l1_default_ttl"` // default TTL

	// L2 (Redis) settings
	L2DefaultTTL time.Duration `mapstructure:"l2_default_ttl"`

	// Key prefix for namespace isolation
	KeyPrefix string `mapstructure:"key_prefix"`
}

// DefaultConfig returns default cache configuration.
func DefaultConfig() Config {
	return Config{
		Backend:      "memory",
		RedisAddr:    "localhost:6379",
		L1MaxSize:    10000,
		L1DefaultTTL: 5 * time.Minute,
		L2DefaultTTL: 30 * time.Minute,
		KeyPrefix:    "cloudai:",
	}
}

// ============================================================================
// In-Memory LRU Cache — L1 cache for single-process
// ============================================================================

type cacheEntry struct {
	value     []byte
	expiresAt time.Time
}

type memoryCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
	maxSize int
	stats   CacheStats
	statsMu sync.Mutex
	prefix  string
	logger  *logrus.Logger
}

// NewMemoryCache creates an in-memory LRU cache.
func NewMemoryCache(cfg Config, logger *logrus.Logger) Cache {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	maxSize := cfg.L1MaxSize
	if maxSize == 0 {
		maxSize = 10000
	}
	mc := &memoryCache{
		entries: make(map[string]*cacheEntry, maxSize),
		maxSize: maxSize,
		prefix:  cfg.KeyPrefix,
		logger:  logger,
		stats:   CacheStats{Backend: "memory"},
	}

	// Start eviction goroutine
	go mc.evictionLoop()

	return mc
}

func (c *memoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	fullKey := c.prefix + key

	c.mu.RLock()
	entry, ok := c.entries[fullKey]
	c.mu.RUnlock()

	if !ok {
		c.statsMu.Lock()
		c.stats.Misses++
		c.statsMu.Unlock()
		return nil, nil
	}

	if time.Now().After(entry.expiresAt) {
		// Expired — treat as miss and delete
		c.mu.Lock()
		delete(c.entries, fullKey)
		c.mu.Unlock()

		c.statsMu.Lock()
		c.stats.Misses++
		c.statsMu.Unlock()
		return nil, nil
	}

	c.statsMu.Lock()
	c.stats.Hits++
	c.statsMu.Unlock()

	return entry.value, nil
}

func (c *memoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	fullKey := c.prefix + key

	c.mu.Lock()
	// Simple eviction: if at capacity, remove oldest (simplified — not true LRU)
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}
	c.entries[fullKey] = &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	c.mu.Unlock()

	c.statsMu.Lock()
	c.stats.Sets++
	c.statsMu.Unlock()

	return nil
}

func (c *memoryCache) Delete(ctx context.Context, key string) error {
	fullKey := c.prefix + key

	c.mu.Lock()
	delete(c.entries, fullKey)
	c.mu.Unlock()

	c.statsMu.Lock()
	c.stats.Deletes++
	c.statsMu.Unlock()

	return nil
}

func (c *memoryCache) DeletePattern(ctx context.Context, pattern string) error {
	fullPattern := c.prefix + pattern

	c.mu.Lock()
	for key := range c.entries {
		if matchGlob(fullPattern, key) {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()

	return nil
}

func (c *memoryCache) Exists(ctx context.Context, key string) (bool, error) {
	fullKey := c.prefix + key

	c.mu.RLock()
	entry, ok := c.entries[fullKey]
	c.mu.RUnlock()

	if !ok || time.Now().After(entry.expiresAt) {
		return false, nil
	}
	return true, nil
}

func (c *memoryCache) Close() error {
	c.mu.Lock()
	c.entries = make(map[string]*cacheEntry)
	c.mu.Unlock()
	return nil
}

func (c *memoryCache) Stats() CacheStats {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()

	c.mu.RLock()
	c.stats.KeyCount = int64(len(c.entries))
	c.mu.RUnlock()

	total := c.stats.Hits + c.stats.Misses
	if total > 0 {
		c.stats.HitRate = float64(c.stats.Hits) / float64(total)
	}
	return c.stats
}

func (c *memoryCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.entries {
		if oldestKey == "" || entry.expiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.expiresAt
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func (c *memoryCache) evictionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		c.mu.Lock()
		for key, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}

// ============================================================================
// Redis Cache — L2 distributed cache (stub — add go-redis dependency)
// ============================================================================

type redisCache struct {
	addr    string
	prefix  string
	logger  *logrus.Logger
	stats   CacheStats
	statsMu sync.Mutex

	// In-memory fallback for when Redis is unavailable
	fallback *memoryCache
}

// NewRedisCache creates a Redis-backed cache.
// Falls back to in-memory if Redis is unavailable.
func NewRedisCache(cfg Config, logger *logrus.Logger) Cache {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	rc := &redisCache{
		addr:   cfg.RedisAddr,
		prefix: cfg.KeyPrefix,
		logger: logger,
		stats:  CacheStats{Backend: "redis"},
		fallback: NewMemoryCache(cfg, logger).(*memoryCache),
	}

	// NOTE: In production, initialize with:
	//   rc.client = redis.NewClusterClient(&redis.ClusterOptions{
	//       Addrs: cfg.RedisClusterAddrs,
	//   })
	// For now, use in-memory fallback.
	logger.WithField("addr", cfg.RedisAddr).Info("Redis cache initialized (fallback mode — add go-redis dependency)")

	return rc
}

func (c *redisCache) Get(ctx context.Context, key string) ([]byte, error) {
	// NOTE: In production: c.client.Get(ctx, c.prefix+key).Bytes()
	return c.fallback.Get(ctx, key)
}

func (c *redisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	// NOTE: In production: c.client.Set(ctx, c.prefix+key, value, ttl).Err()
	return c.fallback.Set(ctx, key, value, ttl)
}

func (c *redisCache) Delete(ctx context.Context, key string) error {
	return c.fallback.Delete(ctx, key)
}

func (c *redisCache) DeletePattern(ctx context.Context, pattern string) error {
	return c.fallback.DeletePattern(ctx, pattern)
}

func (c *redisCache) Exists(ctx context.Context, key string) (bool, error) {
	return c.fallback.Exists(ctx, key)
}

func (c *redisCache) Close() error {
	// NOTE: In production: c.client.Close()
	return c.fallback.Close()
}

func (c *redisCache) Stats() CacheStats {
	stats := c.fallback.Stats()
	stats.Backend = "redis"
	return stats
}

// ============================================================================
// Multi-Level Cache — L1 (in-memory) + L2 (Redis)
// ============================================================================

// MultiLevelCache combines L1 (in-memory) and L2 (Redis) caches.
type MultiLevelCache struct {
	l1     Cache // in-memory
	l2     Cache // Redis
	logger *logrus.Logger
}

// NewMultiLevelCache creates a two-tier cache (L1=memory, L2=Redis).
func NewMultiLevelCache(cfg Config, logger *logrus.Logger) Cache {
	l1 := NewMemoryCache(cfg, logger)
	l2 := NewRedisCache(cfg, logger)

	return &MultiLevelCache{
		l1:     l1,
		l2:     l2,
		logger: logger,
	}
}

func (c *MultiLevelCache) Get(ctx context.Context, key string) ([]byte, error) {
	// Try L1 first
	if val, err := c.l1.Get(ctx, key); err == nil && val != nil {
		return val, nil
	}

	// Try L2
	val, err := c.l2.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, nil // cache miss at all levels
	}

	// Promote to L1
	_ = c.l1.Set(ctx, key, val, 5*time.Minute)

	return val, nil
}

func (c *MultiLevelCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	// Write to both levels
	_ = c.l1.Set(ctx, key, value, ttl)
	return c.l2.Set(ctx, key, value, ttl)
}

func (c *MultiLevelCache) Delete(ctx context.Context, key string) error {
	_ = c.l1.Delete(ctx, key)
	return c.l2.Delete(ctx, key)
}

func (c *MultiLevelCache) DeletePattern(ctx context.Context, pattern string) error {
	_ = c.l1.DeletePattern(ctx, pattern)
	return c.l2.DeletePattern(ctx, pattern)
}

func (c *MultiLevelCache) Exists(ctx context.Context, key string) (bool, error) {
	exists, _ := c.l1.Exists(ctx, key)
	if exists {
		return true, nil
	}
	return c.l2.Exists(ctx, key)
}

func (c *MultiLevelCache) Close() error {
	_ = c.l1.Close()
	return c.l2.Close()
}

func (c *MultiLevelCache) Stats() CacheStats {
	l1Stats := c.l1.Stats()
	l2Stats := c.l2.Stats()

	return CacheStats{
		Backend:  "multi-level",
		Hits:     l1Stats.Hits + l2Stats.Hits,
		Misses:   l2Stats.Misses, // L2 miss = true miss
		Sets:     l2Stats.Sets,
		Deletes:  l2Stats.Deletes,
		KeyCount: l2Stats.KeyCount,
		HitRate:  l1Stats.HitRate, // L1 hit rate is most meaningful
	}
}

// ============================================================================
// Typed Cache Helpers — domain-specific caching
// ============================================================================

// TypedCache wraps Cache with JSON serialization for typed objects.
type TypedCache[T any] struct {
	cache  Cache
	prefix string
}

// NewTypedCache creates a typed cache wrapper.
func NewTypedCache[T any](cache Cache, typePrefix string) *TypedCache[T] {
	return &TypedCache[T]{
		cache:  cache,
		prefix: typePrefix + ":",
	}
}

// Get retrieves a typed object from the cache.
func (tc *TypedCache[T]) Get(ctx context.Context, key string) (*T, error) {
	data, err := tc.cache.Get(ctx, tc.prefix+key)
	if err != nil || data == nil {
		return nil, err
	}

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("cache unmarshal error: %w", err)
	}
	return &result, nil
}

// Set stores a typed object in the cache.
func (tc *TypedCache[T]) Set(ctx context.Context, key string, value *T, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache marshal error: %w", err)
	}
	return tc.cache.Set(ctx, tc.prefix+key, data, ttl)
}

// Delete removes a typed object from the cache.
func (tc *TypedCache[T]) Delete(ctx context.Context, key string) error {
	return tc.cache.Delete(ctx, tc.prefix+key)
}

// InvalidateAll removes all entries for this type.
func (tc *TypedCache[T]) InvalidateAll(ctx context.Context) error {
	return tc.cache.DeletePattern(ctx, tc.prefix+"*")
}

// ============================================================================
// Factory
// ============================================================================

// New creates a Cache based on configuration.
func New(cfg Config, logger *logrus.Logger) Cache {
	switch cfg.Backend {
	case "redis":
		return NewRedisCache(cfg, logger)
	case "multi":
		return NewMultiLevelCache(cfg, logger)
	default:
		return NewMemoryCache(cfg, logger)
	}
}

// ============================================================================
// Utility
// ============================================================================

// matchGlob performs simple glob matching (* = any chars).
func matchGlob(pattern, str string) bool {
	if pattern == "*" {
		return true
	}
	// Simple implementation: just check prefix before first *
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' {
			prefix := pattern[:i]
			if len(str) >= len(prefix) && str[:len(prefix)] == prefix {
				return true
			}
			return false
		}
	}
	return pattern == str
}

// ============================================================================
// Distributed Lock — Redis-backed mutual exclusion
// ============================================================================
// Used by the scheduler for:
//   - Leader election (only one scheduler instance writes to the queue)
//   - CAS operations on shared state (e.g., workload claim)
//   - Preventing split-brain in multi-replica HA deployments
//
// Implementation uses the Redis SET NX EX pattern (Redlock-lite):
//   SET lock_key owner_id NX EX ttl_seconds
//   DEL lock_key (only if owner matches — Lua script for atomicity)
//
// For production multi-master Redis (Redis Cluster), consider the full
// Redlock algorithm with quorum across N independent masters.

// DistributedLock provides distributed mutual exclusion.
type DistributedLock interface {
	// Acquire attempts to acquire a lock. Returns true if successful.
	// The lock expires after ttl to prevent deadlocks.
	Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error)

	// Release releases a previously acquired lock.
	// Only the lock owner can release it (prevents accidental release).
	Release(ctx context.Context, key string) error

	// Renew extends the TTL of a held lock (heartbeat for long operations).
	Renew(ctx context.Context, key string, ttl time.Duration) error

	// IsHeld checks if the lock is currently held by this instance.
	IsHeld(ctx context.Context, key string) (bool, error)
}

// LockStats holds distributed lock metrics.
type LockStats struct {
	Acquired int64 `json:"acquired"`
	Released int64 `json:"released"`
	Renewed  int64 `json:"renewed"`
	Failed   int64 `json:"failed"`  // acquire failures (contention)
	Expired  int64 `json:"expired"` // locks that expired without release
}

// memoryLock implements DistributedLock with an in-memory map.
// Suitable for single-instance deployments and testing.
type memoryLock struct {
	locks   map[string]*lockEntry
	mu      sync.Mutex
	ownerID string
	stats   LockStats
	statsMu sync.Mutex
}

type lockEntry struct {
	owner     string
	expiresAt time.Time
}

// NewMemoryLock creates an in-memory distributed lock (single-instance fallback).
func NewMemoryLock(ownerID string) DistributedLock {
	if ownerID == "" {
		ownerID = fmt.Sprintf("mem-%d", time.Now().UnixNano())
	}
	ml := &memoryLock{
		locks:   make(map[string]*lockEntry),
		ownerID: ownerID,
	}
	go ml.evictionLoop()
	return ml
}

func (m *memoryLock) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if lock exists and is still valid
	if existing, ok := m.locks[key]; ok {
		if time.Now().Before(existing.expiresAt) {
			// Lock is held by someone
			if existing.owner == m.ownerID {
				// Re-entrant: already held by us, renew
				existing.expiresAt = time.Now().Add(ttl)
				return true, nil
			}
			m.statsMu.Lock()
			m.stats.Failed++
			m.statsMu.Unlock()
			return false, nil // held by another
		}
		// Lock expired, take it
		m.statsMu.Lock()
		m.stats.Expired++
		m.statsMu.Unlock()
	}

	m.locks[key] = &lockEntry{
		owner:     m.ownerID,
		expiresAt: time.Now().Add(ttl),
	}
	m.statsMu.Lock()
	m.stats.Acquired++
	m.statsMu.Unlock()
	return true, nil
}

func (m *memoryLock) Release(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.locks[key]
	if !ok {
		return nil // already released
	}
	if existing.owner != m.ownerID {
		return fmt.Errorf("cannot release lock %q: owned by %s, not %s", key, existing.owner, m.ownerID)
	}

	delete(m.locks, key)
	m.statsMu.Lock()
	m.stats.Released++
	m.statsMu.Unlock()
	return nil
}

func (m *memoryLock) Renew(ctx context.Context, key string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.locks[key]
	if !ok {
		return fmt.Errorf("lock %q not found", key)
	}
	if existing.owner != m.ownerID {
		return fmt.Errorf("cannot renew lock %q: owned by %s", key, existing.owner)
	}

	existing.expiresAt = time.Now().Add(ttl)
	m.statsMu.Lock()
	m.stats.Renewed++
	m.statsMu.Unlock()
	return nil
}

func (m *memoryLock) IsHeld(ctx context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.locks[key]
	if !ok {
		return false, nil
	}
	if time.Now().After(existing.expiresAt) {
		delete(m.locks, key)
		return false, nil
	}
	return existing.owner == m.ownerID, nil
}

func (m *memoryLock) evictionLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for key, entry := range m.locks {
			if now.After(entry.expiresAt) {
				delete(m.locks, key)
			}
		}
		m.mu.Unlock()
	}
}

// redisLock implements DistributedLock backed by Redis SET NX EX.
// Falls back to memoryLock when Redis is unavailable.
type redisLock struct {
	addr     string
	prefix   string
	ownerID  string
	fallback DistributedLock
	logger   *logrus.Logger
}

// NewRedisLock creates a Redis-backed distributed lock.
// In production, replace the fallback with a real Redis client:
//
//	client := redis.NewClient(&redis.Options{Addr: addr})
//	// Acquire: client.SetNX(ctx, prefix+key, ownerID, ttl)
//	// Release: Lua script — if redis.call("get",KEYS[1])==ARGV[1] then redis.call("del",KEYS[1])
//	// Renew:   Lua script — if redis.call("get",KEYS[1])==ARGV[1] then redis.call("pexpire",KEYS[1],ARGV[2])
func NewRedisLock(addr, prefix, ownerID string, logger *logrus.Logger) DistributedLock {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	rl := &redisLock{
		addr:     addr,
		prefix:   prefix + "lock:",
		ownerID:  ownerID,
		fallback: NewMemoryLock(ownerID),
		logger:   logger,
	}
	// NOTE: In production, initialize Redis client here.
	// For now, use in-memory fallback.
	logger.WithField("addr", addr).Info("Redis lock initialized (fallback mode — add go-redis dependency)")
	return rl
}

func (r *redisLock) Acquire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	// NOTE: Production: r.client.SetNX(ctx, r.prefix+key, r.ownerID, ttl).Result()
	return r.fallback.Acquire(ctx, key, ttl)
}

func (r *redisLock) Release(ctx context.Context, key string) error {
	return r.fallback.Release(ctx, key)
}

func (r *redisLock) Renew(ctx context.Context, key string, ttl time.Duration) error {
	return r.fallback.Renew(ctx, key, ttl)
}

func (r *redisLock) IsHeld(ctx context.Context, key string) (bool, error) {
	return r.fallback.IsHeld(ctx, key)
}

// ============================================================================
// PubSub — Redis-backed publish/subscribe for cross-instance coordination
// ============================================================================
// Used for:
//   - Cache invalidation broadcast ("key X was updated, drop your L1 copy")
//   - Node cache refresh signals ("K8s node list changed, re-sync")
//   - Scheduler queue change notifications
//
// This is NOT a replacement for Kafka/NATS:
//   - Redis PubSub is fire-and-forget (no message persistence)
//   - If a subscriber is offline, it misses the message
//   - Use Kafka for durable event streams, NATS for request-reply

// PubSub provides publish-subscribe messaging.
type PubSub interface {
	// Publish sends a message to all subscribers of a channel.
	Publish(ctx context.Context, channel string, message []byte) error

	// Subscribe returns a channel that receives messages for the given topic.
	// The returned channel is closed when Unsubscribe is called or context is cancelled.
	Subscribe(ctx context.Context, channel string) (<-chan []byte, error)

	// Unsubscribe removes a subscription.
	Unsubscribe(ctx context.Context, channel string) error

	// Close releases all PubSub resources.
	Close() error
}

// memoryPubSub implements PubSub with in-memory channels.
// Suitable for single-instance deployments and testing.
type memoryPubSub struct {
	subscribers map[string][]chan []byte
	mu          sync.RWMutex
}

// NewMemoryPubSub creates an in-memory pub/sub (single-instance fallback).
func NewMemoryPubSub() PubSub {
	return &memoryPubSub{
		subscribers: make(map[string][]chan []byte),
	}
}

func (m *memoryPubSub) Publish(ctx context.Context, channel string, message []byte) error {
	m.mu.RLock()
	subs := m.subscribers[channel]
	m.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- message:
		default:
			// subscriber too slow, drop message (fire-and-forget semantics)
		}
	}
	return nil
}

func (m *memoryPubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, error) {
	ch := make(chan []byte, 256)

	m.mu.Lock()
	m.subscribers[channel] = append(m.subscribers[channel], ch)
	m.mu.Unlock()

	// Auto-unsubscribe when context is cancelled
	go func() {
		<-ctx.Done()
		_ = m.Unsubscribe(context.Background(), channel)
	}()

	return ch, nil
}

func (m *memoryPubSub) Unsubscribe(ctx context.Context, channel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if subs, ok := m.subscribers[channel]; ok {
		for _, ch := range subs {
			close(ch)
		}
		delete(m.subscribers, channel)
	}
	return nil
}

func (m *memoryPubSub) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for channel, subs := range m.subscribers {
		for _, ch := range subs {
			close(ch)
		}
		delete(m.subscribers, channel)
	}
	return nil
}

// redisPubSub implements PubSub backed by Redis PUBLISH/SUBSCRIBE.
// Falls back to memoryPubSub when Redis is unavailable.
type redisPubSub struct {
	addr     string
	prefix   string
	fallback PubSub
	logger   *logrus.Logger
}

// NewRedisPubSub creates a Redis-backed pub/sub.
// In production, replace the fallback with a real Redis client:
//
//	client := redis.NewClient(&redis.Options{Addr: addr})
//	// Publish: client.Publish(ctx, prefix+channel, message)
//	// Subscribe: client.Subscribe(ctx, prefix+channel)
func NewRedisPubSub(addr, prefix string, logger *logrus.Logger) PubSub {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	rps := &redisPubSub{
		addr:     addr,
		prefix:   prefix + "pubsub:",
		fallback: NewMemoryPubSub(),
		logger:   logger,
	}
	logger.WithField("addr", addr).Info("Redis PubSub initialized (fallback mode — add go-redis dependency)")
	return rps
}

func (r *redisPubSub) Publish(ctx context.Context, channel string, message []byte) error {
	// NOTE: Production: r.client.Publish(ctx, r.prefix+channel, message).Err()
	return r.fallback.Publish(ctx, channel, message)
}

func (r *redisPubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, error) {
	return r.fallback.Subscribe(ctx, channel)
}

func (r *redisPubSub) Unsubscribe(ctx context.Context, channel string) error {
	return r.fallback.Unsubscribe(ctx, channel)
}

func (r *redisPubSub) Close() error {
	return r.fallback.Close()
}

// ============================================================================
// Lock + PubSub Factory
// ============================================================================

// NewDistributedLock creates a DistributedLock based on configuration.
func NewDistributedLock(cfg Config, ownerID string, logger *logrus.Logger) DistributedLock {
	switch cfg.Backend {
	case "redis", "multi":
		return NewRedisLock(cfg.RedisAddr, cfg.KeyPrefix, ownerID, logger)
	default:
		return NewMemoryLock(ownerID)
	}
}

// NewPubSub creates a PubSub based on configuration.
func NewPubSub(cfg Config, logger *logrus.Logger) PubSub {
	switch cfg.Backend {
	case "redis", "multi":
		return NewRedisPubSub(cfg.RedisAddr, cfg.KeyPrefix, logger)
	default:
		return NewMemoryPubSub()
	}
}
