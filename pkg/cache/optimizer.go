// Package cache - optimizer provides cache hit rate optimization techniques.
// Implements:
//   - Bloom filter for negative lookup avoidance (skip DB for known-absent keys)
//   - Adaptive TTL based on access frequency (hot keys live longer)
//   - Cache warmup for predictable key patterns
//   - Sharded LRU for reduced lock contention at high concurrency
//   - Access frequency tracking for intelligent eviction (LFU-aware)
//
// Combined, these techniques target >90% L1 hit rate under production traffic.
package cache

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Bloom Filter — negative lookup avoidance
// ============================================================================

// BloomFilter is a space-efficient probabilistic data structure that tests
// whether an element is a member of a set. False positives are possible,
// but false negatives are not. Used to avoid unnecessary DB lookups for
// keys that have never been cached.
type BloomFilter struct {
	bits    []uint64
	size    uint64
	hashes  int
	mu      sync.RWMutex
	added   int64
}

// NewBloomFilter creates a bloom filter optimized for n expected elements
// with a target false positive rate p.
func NewBloomFilter(n int, p float64) *BloomFilter {
	if n <= 0 {
		n = 100000
	}
	if p <= 0 || p >= 1 {
		p = 0.01
	}
	// Optimal size: m = -(n * ln(p)) / (ln(2)^2)
	m := uint64(-float64(n) * math.Log(p) / (math.Log(2) * math.Log(2)))
	// Optimal hash count: k = (m/n) * ln(2)
	k := int(float64(m) / float64(n) * math.Log(2))
	if k < 1 {
		k = 1
	}
	if k > 30 {
		k = 30
	}

	words := (m + 63) / 64
	return &BloomFilter{
		bits:   make([]uint64, words),
		size:   m,
		hashes: k,
	}
}

// Add adds a key to the bloom filter.
func (bf *BloomFilter) Add(key string) {
	bf.mu.Lock()
	h1, h2 := bf.hash(key)
	for i := 0; i < bf.hashes; i++ {
		pos := (h1 + uint64(i)*h2) % bf.size
		bf.bits[pos/64] |= 1 << (pos % 64)
	}
	atomic.AddInt64(&bf.added, 1)
	bf.mu.Unlock()
}

// MayContain returns true if the key might be in the set.
// False means definitely not in the set.
func (bf *BloomFilter) MayContain(key string) bool {
	bf.mu.RLock()
	h1, h2 := bf.hash(key)
	for i := 0; i < bf.hashes; i++ {
		pos := (h1 + uint64(i)*h2) % bf.size
		if bf.bits[pos/64]&(1<<(pos%64)) == 0 {
			bf.mu.RUnlock()
			return false
		}
	}
	bf.mu.RUnlock()
	return true
}

// Reset clears the bloom filter.
func (bf *BloomFilter) Reset() {
	bf.mu.Lock()
	for i := range bf.bits {
		bf.bits[i] = 0
	}
	atomic.StoreInt64(&bf.added, 0)
	bf.mu.Unlock()
}

// Count returns the number of elements added.
func (bf *BloomFilter) Count() int64 {
	return atomic.LoadInt64(&bf.added)
}

// EstimatedFPRate returns the estimated current false positive rate.
func (bf *BloomFilter) EstimatedFPRate() float64 {
	n := float64(atomic.LoadInt64(&bf.added))
	m := float64(bf.size)
	k := float64(bf.hashes)
	return math.Pow(1-math.Exp(-k*n/m), k)
}

func (bf *BloomFilter) hash(key string) (uint64, uint64) {
	h := fnv.New128a()
	h.Write([]byte(key))
	sum := h.Sum(nil)
	h1 := uint64(sum[0])<<56 | uint64(sum[1])<<48 | uint64(sum[2])<<40 | uint64(sum[3])<<32 |
		uint64(sum[4])<<24 | uint64(sum[5])<<16 | uint64(sum[6])<<8 | uint64(sum[7])
	h2 := uint64(sum[8])<<56 | uint64(sum[9])<<48 | uint64(sum[10])<<40 | uint64(sum[11])<<32 |
		uint64(sum[12])<<24 | uint64(sum[13])<<16 | uint64(sum[14])<<8 | uint64(sum[15])
	return h1, h2
}

// ============================================================================
// Adaptive TTL — hot keys live longer, cold keys expire faster
// ============================================================================

// AdaptiveTTLConfig configures adaptive TTL behavior.
type AdaptiveTTLConfig struct {
	BaseTTL          time.Duration // base TTL for new keys
	MinTTL           time.Duration // minimum TTL
	MaxTTL           time.Duration // maximum TTL
	HotThreshold     int           // access count to be considered "hot"
	TTLMultiplier    float64       // multiply TTL by this per hot threshold
	DecayInterval    time.Duration // how often to decay access counters
}

// DefaultAdaptiveTTLConfig returns sensible defaults.
func DefaultAdaptiveTTLConfig() AdaptiveTTLConfig {
	return AdaptiveTTLConfig{
		BaseTTL:       5 * time.Minute,
		MinTTL:        30 * time.Second,
		MaxTTL:        30 * time.Minute,
		HotThreshold:  10,
		TTLMultiplier: 2.0,
		DecayInterval: 5 * time.Minute,
	}
}

// AdaptiveTTLManager tracks access patterns and computes optimal TTLs.
type AdaptiveTTLManager struct {
	config      AdaptiveTTLConfig
	accessCount map[string]*int64 // key → access count
	mu          sync.RWMutex
}

// NewAdaptiveTTLManager creates a new adaptive TTL manager.
func NewAdaptiveTTLManager(cfg AdaptiveTTLConfig) *AdaptiveTTLManager {
	m := &AdaptiveTTLManager{
		config:      cfg,
		accessCount: make(map[string]*int64),
	}
	go m.decayLoop()
	return m
}

// RecordAccess increments the access counter for a key.
func (m *AdaptiveTTLManager) RecordAccess(key string) {
	m.mu.RLock()
	counter, ok := m.accessCount[key]
	m.mu.RUnlock()

	if ok {
		atomic.AddInt64(counter, 1)
		return
	}

	m.mu.Lock()
	if counter, ok = m.accessCount[key]; ok {
		m.mu.Unlock()
		atomic.AddInt64(counter, 1)
		return
	}
	c := int64(1)
	m.accessCount[key] = &c
	m.mu.Unlock()
}

// ComputeTTL returns the adaptive TTL for a key based on its access frequency.
func (m *AdaptiveTTLManager) ComputeTTL(key string) time.Duration {
	m.mu.RLock()
	counter, ok := m.accessCount[key]
	m.mu.RUnlock()

	if !ok {
		return m.config.BaseTTL
	}

	count := atomic.LoadInt64(counter)
	ttl := m.config.BaseTTL

	// Scale TTL based on access frequency
	if count >= int64(m.config.HotThreshold) {
		multiplier := math.Log2(float64(count)/float64(m.config.HotThreshold) + 1)
		ttl = time.Duration(float64(ttl) * m.config.TTLMultiplier * multiplier)
	} else if count <= 1 {
		// Cold key: reduce TTL
		ttl = m.config.MinTTL + (m.config.BaseTTL-m.config.MinTTL)/2
	}

	// Clamp to bounds
	if ttl < m.config.MinTTL {
		ttl = m.config.MinTTL
	}
	if ttl > m.config.MaxTTL {
		ttl = m.config.MaxTTL
	}

	return ttl
}

// decayLoop periodically decays access counters to adapt to changing patterns.
func (m *AdaptiveTTLManager) decayLoop() {
	ticker := time.NewTicker(m.config.DecayInterval)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		for key, counter := range m.accessCount {
			newVal := atomic.LoadInt64(counter) / 2
			if newVal <= 0 {
				delete(m.accessCount, key)
			} else {
				atomic.StoreInt64(counter, newVal)
			}
		}
		m.mu.Unlock()
	}
}

// Stats returns access tracking statistics.
func (m *AdaptiveTTLManager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	hotCount := 0
	coldCount := 0
	for _, counter := range m.accessCount {
		if atomic.LoadInt64(counter) >= int64(m.config.HotThreshold) {
			hotCount++
		} else {
			coldCount++
		}
	}

	return map[string]interface{}{
		"tracked_keys": len(m.accessCount),
		"hot_keys":     hotCount,
		"cold_keys":    coldCount,
	}
}

// ============================================================================
// Sharded LRU Cache — reduced lock contention
// ============================================================================

const defaultShardCount = 16

// ShardedCache distributes keys across multiple independent cache shards
// to reduce lock contention under high-concurrency workloads.
type ShardedCache struct {
	shards     []Cache
	shardCount int
	bloom      *BloomFilter
	ttlManager *AdaptiveTTLManager
	logger     *logrus.Logger

	// Aggregated stats
	totalHits   int64
	totalMisses int64
	bloomSkips  int64
}

// ShardedCacheConfig configures the sharded cache.
type ShardedCacheConfig struct {
	ShardCount       int
	MaxSizePerShard  int
	DefaultTTL       time.Duration
	KeyPrefix        string
	EnableBloom      bool
	BloomExpectedN   int
	BloomFPRate      float64
	EnableAdaptiveTTL bool
	AdaptiveTTL      AdaptiveTTLConfig
}

// DefaultShardedCacheConfig returns sensible defaults.
func DefaultShardedCacheConfig() ShardedCacheConfig {
	return ShardedCacheConfig{
		ShardCount:        defaultShardCount,
		MaxSizePerShard:   5000,
		DefaultTTL:        5 * time.Minute,
		KeyPrefix:         "cloudai:",
		EnableBloom:       true,
		BloomExpectedN:    100000,
		BloomFPRate:       0.01,
		EnableAdaptiveTTL: true,
		AdaptiveTTL:       DefaultAdaptiveTTLConfig(),
	}
}

// NewShardedCache creates a high-performance sharded cache.
func NewShardedCache(cfg ShardedCacheConfig, logger *logrus.Logger) *ShardedCache {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if cfg.ShardCount <= 0 {
		cfg.ShardCount = defaultShardCount
	}

	shards := make([]Cache, cfg.ShardCount)
	for i := 0; i < cfg.ShardCount; i++ {
		shards[i] = NewMemoryCache(Config{
			L1MaxSize: cfg.MaxSizePerShard,
			KeyPrefix: cfg.KeyPrefix,
		}, logger)
	}

	sc := &ShardedCache{
		shards:     shards,
		shardCount: cfg.ShardCount,
		logger:     logger,
	}

	if cfg.EnableBloom {
		sc.bloom = NewBloomFilter(cfg.BloomExpectedN, cfg.BloomFPRate)
	}
	if cfg.EnableAdaptiveTTL {
		sc.ttlManager = NewAdaptiveTTLManager(cfg.AdaptiveTTL)
	}

	return sc
}

func (sc *ShardedCache) shardIndex(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32()) % sc.shardCount
}

// Get retrieves a value, using bloom filter to skip known-absent keys.
func (sc *ShardedCache) Get(ctx context.Context, key string) ([]byte, error) {
	// Bloom filter check: if definitely not in cache, skip lookup
	if sc.bloom != nil && !sc.bloom.MayContain(key) {
		atomic.AddInt64(&sc.bloomSkips, 1)
		atomic.AddInt64(&sc.totalMisses, 1)
		return nil, nil
	}

	val, err := sc.shards[sc.shardIndex(key)].Get(ctx, key)
	if err != nil {
		return nil, err
	}

	if val != nil {
		atomic.AddInt64(&sc.totalHits, 1)
		if sc.ttlManager != nil {
			sc.ttlManager.RecordAccess(key)
		}
	} else {
		atomic.AddInt64(&sc.totalMisses, 1)
	}

	return val, nil
}

// Set stores a value, registering in bloom filter and using adaptive TTL.
func (sc *ShardedCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if sc.bloom != nil {
		sc.bloom.Add(key)
	}
	if sc.ttlManager != nil {
		ttl = sc.ttlManager.ComputeTTL(key)
	}
	return sc.shards[sc.shardIndex(key)].Set(ctx, key, value, ttl)
}

// Delete removes a key from the cache.
func (sc *ShardedCache) Delete(ctx context.Context, key string) error {
	return sc.shards[sc.shardIndex(key)].Delete(ctx, key)
}

// DeletePattern removes all keys matching a glob pattern across all shards.
func (sc *ShardedCache) DeletePattern(ctx context.Context, pattern string) error {
	for _, shard := range sc.shards {
		if err := shard.DeletePattern(ctx, pattern); err != nil {
			return err
		}
	}
	return nil
}

// Exists checks if a key exists in the cache.
func (sc *ShardedCache) Exists(ctx context.Context, key string) (bool, error) {
	if sc.bloom != nil && !sc.bloom.MayContain(key) {
		return false, nil
	}
	return sc.shards[sc.shardIndex(key)].Exists(ctx, key)
}

// Close releases all shard resources.
func (sc *ShardedCache) Close() error {
	for _, shard := range sc.shards {
		shard.Close()
	}
	return nil
}

// Stats returns aggregated statistics across all shards.
func (sc *ShardedCache) Stats() CacheStats {
	var totalHits, totalMisses, totalSets, totalDeletes, totalKeys int64
	for _, shard := range sc.shards {
		s := shard.Stats()
		totalHits += s.Hits
		totalMisses += s.Misses
		totalSets += s.Sets
		totalDeletes += s.Deletes
		totalKeys += s.KeyCount
	}

	hitRate := float64(0)
	total := totalHits + totalMisses
	if total > 0 {
		hitRate = float64(totalHits) / float64(total)
	}

	return CacheStats{
		Backend:  fmt.Sprintf("sharded-%d", sc.shardCount),
		Hits:     totalHits,
		Misses:   totalMisses,
		Sets:     totalSets,
		Deletes:  totalDeletes,
		KeyCount: totalKeys,
		HitRate:  hitRate,
	}
}

// BloomStats returns bloom filter statistics.
func (sc *ShardedCache) BloomStats() map[string]interface{} {
	if sc.bloom == nil {
		return nil
	}
	return map[string]interface{}{
		"elements":  sc.bloom.Count(),
		"fp_rate":   sc.bloom.EstimatedFPRate(),
		"skips":     atomic.LoadInt64(&sc.bloomSkips),
	}
}

// AdaptiveTTLStats returns adaptive TTL tracking statistics.
func (sc *ShardedCache) AdaptiveTTLStats() map[string]interface{} {
	if sc.ttlManager == nil {
		return nil
	}
	return sc.ttlManager.Stats()
}

// ============================================================================
// Cache Warmup — pre-populate hot keys at startup
// ============================================================================

// WarmupConfig configures cache warmup behavior.
type WarmupConfig struct {
	Concurrency int           // parallel warmup goroutines
	Timeout     time.Duration // overall warmup timeout
	BatchSize   int           // keys per batch
}

// DefaultWarmupConfig returns default warmup configuration.
func DefaultWarmupConfig() WarmupConfig {
	return WarmupConfig{
		Concurrency: 4,
		Timeout:     30 * time.Second,
		BatchSize:   100,
	}
}

// WarmupResult holds the result of a cache warmup operation.
type WarmupResult struct {
	TotalKeys    int           `json:"total_keys"`
	LoadedKeys   int64         `json:"loaded_keys"`
	FailedKeys   int64         `json:"failed_keys"`
	Duration     time.Duration `json:"duration"`
	KeysPerSec   float64       `json:"keys_per_sec"`
}

// WarmupSource provides keys and values for cache warming.
type WarmupSource interface {
	// FetchBatch returns a batch of key-value pairs for cache warming.
	FetchBatch(ctx context.Context, offset, limit int) (map[string][]byte, error)
	// TotalCount returns the total number of keys to warm.
	TotalCount(ctx context.Context) (int, error)
}

// Warmup pre-populates the cache from a data source.
func Warmup(ctx context.Context, cache Cache, source WarmupSource, cfg WarmupConfig, logger *logrus.Logger) (*WarmupResult, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	start := time.Now()
	total, err := source.TotalCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("warmup source count failed: %w", err)
	}

	result := &WarmupResult{TotalKeys: total}

	// Parallel batch loading
	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	var loaded, failed int64

	for offset := 0; offset < total; offset += cfg.BatchSize {
		wg.Add(1)
		sem <- struct{}{}
		go func(off int) {
			defer wg.Done()
			defer func() { <-sem }()

			batch, err := source.FetchBatch(ctx, off, cfg.BatchSize)
			if err != nil {
				atomic.AddInt64(&failed, int64(cfg.BatchSize))
				logger.WithError(err).WithField("offset", off).Debug("Warmup batch failed")
				return
			}

			for key, val := range batch {
				if err := cache.Set(ctx, key, val, 10*time.Minute); err != nil {
					atomic.AddInt64(&failed, 1)
				} else {
					atomic.AddInt64(&loaded, 1)
				}
			}
		}(offset)
	}

	wg.Wait()

	result.LoadedKeys = atomic.LoadInt64(&loaded)
	result.FailedKeys = atomic.LoadInt64(&failed)
	result.Duration = time.Since(start)
	if result.Duration > 0 {
		result.KeysPerSec = float64(result.LoadedKeys) / result.Duration.Seconds()
	}

	logger.WithFields(logrus.Fields{
		"total":       total,
		"loaded":      result.LoadedKeys,
		"failed":      result.FailedKeys,
		"duration":    result.Duration,
		"keys_per_sec": result.KeysPerSec,
	}).Info("Cache warmup completed")

	return result, nil
}
