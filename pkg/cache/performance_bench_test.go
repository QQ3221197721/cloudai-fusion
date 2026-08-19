package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Bloom Filter - hit/miss latency, escape vector defense counting
// ============================================================================

func BenchmarkBloomFilterAdd(b *testing.B) {
	bf := NewBloomFilter(100000, 0.01)

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(keys[i])
		_ = bf.Count()
	}
}

func BenchmarkBloomFilterMayContainHit(b *testing.B) {
	bf := NewBloomFilter(100000, 0.01)
	for i := 0; i < 50000; i++ {
		bf.Add(fmt.Sprintf("key-%d", i))
	}

	hits := make([]string, b.N)
	for i := range hits {
		hits[i] = fmt.Sprintf("key-%d", i%50000)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		found := bf.MayContain(hits[i])
		if !found {
			b.Fatal("expected hit")
		}
	}
}

func BenchmarkBloomFilterMayContainMiss(b *testing.B) {
	bf := NewBloomFilter(100000, 0.01)
	for i := 0; i < 50000; i++ {
		bf.Add(fmt.Sprintf("key-%d", i))
	}

	misses := make([]string, b.N)
	for i := range misses {
		misses[i] = fmt.Sprintf("missing-key-%d", i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		found := bf.MayContain(misses[i])
		if found {
			b.Fatal("expected miss")
		}
	}
}

func BenchmarkBloomFilterEstimatedFPRate(b *testing.B) {
	bf := NewBloomFilter(100000, 0.01)
	for i := 0; i < 70000; i++ {
		bf.Add(fmt.Sprintf("key-%d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rate := bf.EstimatedFPRate()
		_ = rate
	}
}

// ============================================================================
// Sharded LRU Cache - get/set performance, concurrent throughput
// ============================================================================

func BenchmarkShardedLRUGetHit(b *testing.B) {
	logger := logrus.New()
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg, logger)
	defer sc.Close()

	// Pre-warm cache
	keys := make([]string, b.N)
	for i := range keys {
		key := fmt.Sprintf("warm-key-%d", i%5000)
		keys[i] = key
		_ = sc.Set(context.Background(), key, []byte("value"), 5*time.Minute)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		val, err := sc.Get(context.Background(), keys[i])
		if err != nil {
			b.Fatal(err)
		}
		if len(val) == 0 {
			b.Fatal("expected hit")
		}
	}
	_ = sc.Stats()
	_ = sc.BloomStats()
	_ = sc.AdaptiveTTLStats()
}

func BenchmarkShardedLRUGetMiss(b *testing.B) {
	logger := logrus.New()
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg, logger)
	defer sc.Close()

	// Pre-populate some entries, leave most as misses
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("existing-%d", i)
		_ = sc.Set(context.Background(), key, []byte("value"), 5*time.Minute)
	}

	// Generate queries that will miss (mostly bloom-filtered)
	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("missing-%d", i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		val, err := sc.Get(context.Background(), keys[i])
		if err != nil {
			b.Fatal(err)
		}
		if val != nil {
			b.Fatal("expected miss")
		}
	}
	_ = sc.Stats()
}

func BenchmarkShardedLRUSet(b *testing.B) {
	logger := logrus.New()
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg, logger)
	defer sc.Close()

	values := make([][]byte, b.N)
	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("set-key-%d", i)
		values[i] = []byte(fmt.Sprintf("value-%d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := sc.Set(context.Background(), keys[i], values[i], 5*time.Minute)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sc.Stats()
}

func BenchmarkShardedLRUDelete(b *testing.B) {
	logger := logrus.New()
	cfg := DefaultShardedCacheConfig()
	sc := NewShardedCache(cfg, logger)
	defer sc.Close()

	// Warm up with some data
	for i := 0; i < 2000; i++ {
		key := fmt.Sprintf("delete-key-%d", i)
		_ = sc.Set(context.Background(), key, []byte("value"), 5*time.Minute)
	}

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("delete-key-%d", i%2000)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := sc.Delete(context.Background(), keys[i])
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sc.Stats()
}

// ============================================================================
// Distributed Lock (in-memory implementation)
// (Real Redlock requires actual Redis connection — benchmark memory lock here)
// ============================================================================

func BenchmarkMemoryLockAcquireHit(b *testing.B) {
	lock := NewMemoryLock("owner-1")

	ctx := context.Background()
	key := "my-lock"

	// Acquire once to set up locked state
	ok, _ := lock.Acquire(ctx, key, 5*time.Second)
	if !ok {
		b.Fatal("initial acquire failed")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Second attempt should fail (held by another "owner" in real scenarios)
		// Here it will succeed because re-entrant allows self-renewal
		ok, err := lock.Acquire(ctx, key, 5*time.Second)
		if err != nil {
			b.Fatal(err)
		}
		_ = ok
	}
}

func BenchmarkMemoryLockRelease(b *testing.B) {
	lock := NewMemoryLock("owner-1")

	ctx := context.Background()
	key := "my-lock"

	// Setup locked state
	ok, _ := lock.Acquire(ctx, key, 5*time.Second)
	if !ok {
		b.Fatal("initial acquire failed")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := lock.Release(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
		// Re-acquire for next iteration
		ok, _ := lock.Acquire(ctx, key, 5*time.Second)
		if !ok {
			b.Fatal("re-acquire failed")
		}
	}
}

func BenchmarkMemoryLockRenew(b *testing.B) {
	lock := NewMemoryLock("owner-1")

	ctx := context.Background()
	key := "my-lock"

	// Setup locked state
	ok, _ := lock.Acquire(ctx, key, 1*time.Second)
	if !ok {
		b.Fatal("initial acquire failed")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := lock.Renew(ctx, key, 1*time.Second)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = lock.(*memoryLock).stats
}

func BenchmarkMemoryLockIsHeld(b *testing.B) {
	lock := NewMemoryLock("owner-1")

	ctx := context.Background()
	key := "my-lock"

	// Setup locked state
	ok, _ := lock.Acquire(ctx, key, 5*time.Second)
	if !ok {
		b.Fatal("initial acquire failed")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		holding, err := lock.IsHeld(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
		if !holding {
			b.Fatal("expected lock to be held")
		}
	}
}

// ============================================================================
// Adaptive TTL Manager
// ============================================================================

func BenchmarkAdaptiveTTLCompute(b *testing.B) {
	ttlMgr := NewAdaptiveTTLManager(DefaultAdaptiveTTLConfig())
	defer ttlMgr.Close()

	// Warm up with hot/cold keys
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i%10) // Some hot keys
		ttlMgr.RecordAccess(key)
		ttlMgr.RecordAccess(key)
		ttlMgr.RecordAccess(key)
	}

	keys := make([]string, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i%10)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ttl := ttlMgr.ComputeTTL(keys[i])
		_ = ttl
	}
	_ = ttlMgr.Stats()
}

// ============================================================================
// Multi-Level Cache (L1+L2 simulated)
// ============================================================================

func BenchmarkMultiLevelCacheGet(b *testing.B) {
	logger := logrus.New()
	cfg := DefaultConfig()
	cache := NewMultiLevelCache(cfg, logger)

	keys := make([]string, b.N)
	for i := range keys {
		key := fmt.Sprintf("ml-key-%d", i%1000)
		keys[i] = key
		_ = cache.Set(context.Background(), key, []byte("multi-level-value"), 5*time.Minute)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		val, err := cache.Get(context.Background(), keys[i])
		if err != nil {
			b.Fatal(err)
		}
		if len(val) == 0 {
			b.Fatal("expected hit")
		}
	}
	_ = cache.Stats()
}

func BenchmarkMultiLevelCacheSet(b *testing.B) {
	logger := logrus.New()
	cfg := DefaultConfig()
	cache := NewMultiLevelCache(cfg, logger)

	keys := make([]string, b.N)
	values := make([][]byte, b.N)
	for i := range keys {
		keys[i] = fmt.Sprintf("ml-set-%d", i)
		values[i] = []byte(fmt.Sprintf("value-%d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := cache.Set(context.Background(), keys[i], values[i], 5*time.Minute)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = cache.Stats()
}
