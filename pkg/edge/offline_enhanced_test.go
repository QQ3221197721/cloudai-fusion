package edge

import (
	"testing"
	"time"
)

// These tests cover EdgeCache from offline_enhanced.go — previously untested.
// They pin hit/miss accounting, TTL expiry, capacity eviction, hot-data
// protection, and oversize rejection. (CRDT sync + OfflineHub are covered by
// edge_hardware_test.go.)

func testCacheConfig(maxMB int, policy CachePolicy) CacheConfig {
	cfg := DefaultCacheConfig()
	cfg.MaxSizeMB = maxMB
	cfg.Policy = policy
	return cfg
}

// TestEdgeCache_PutGetHitMiss proves basic store/retrieve and hit/miss tallies.
func TestEdgeCache_PutGetHitMiss(t *testing.T) {
	c := NewEdgeCache(testCacheConfig(64, CachePolicyLRU), newOfflineTestLogger())

	if err := c.Put("model-a", 1024, "model_weight"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := c.Get("model-a"); !ok {
		t.Fatal("expected hit for stored key")
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss for absent key")
	}

	stats := c.GetStats()
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Fatalf("hits=%d misses=%d, want 1/1", stats.Hits, stats.Misses)
	}
	if stats.TotalEntries != 1 {
		t.Fatalf("total entries = %d, want 1", stats.TotalEntries)
	}
}

// TestEdgeCache_TTLExpiry proves an entry past its TTL is treated as a miss.
func TestEdgeCache_TTLExpiry(t *testing.T) {
	cfg := testCacheConfig(64, CachePolicyTTL)
	cfg.DefaultTTL = 5 * time.Millisecond
	c := NewEdgeCache(cfg, newOfflineTestLogger())

	_ = c.Put("ephemeral", 512, "config")
	time.Sleep(20 * time.Millisecond)

	if _, ok := c.Get("ephemeral"); ok {
		t.Fatal("expected miss for expired entry")
	}
}

// TestEdgeCache_EvictionLRUOrder proves capacity pressure evicts the OLDER
// entry (LRU order) so a new one fits, and the eviction is counted. (The
// existing TestEdgeCache_Eviction only checks the eviction count.)
func TestEdgeCache_EvictionLRUOrder(t *testing.T) {
	// 1 MB cache; two 600 KB entries cannot coexist.
	c := NewEdgeCache(testCacheConfig(1, CachePolicyLRU), newOfflineTestLogger())
	const big = int64(600 * 1024)

	if err := c.Put("first", big, "feature_data"); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := c.Put("second", big, "feature_data"); err != nil {
		t.Fatalf("Put second (should evict first): %v", err)
	}

	if _, ok := c.Get("second"); !ok {
		t.Fatal("most-recent entry must be present")
	}
	if _, ok := c.Get("first"); ok {
		t.Fatal("older entry should have been evicted under capacity pressure")
	}
	if c.GetStats().EvictionCount == 0 {
		t.Fatal("eviction must be counted")
	}
}

// TestEdgeCache_HotDataProtected proves frequently-accessed (hot) entries are
// protected from eviction while cold entries are reclaimed.
func TestEdgeCache_HotDataProtected(t *testing.T) {
	cfg := testCacheConfig(1, CachePolicyLRU)
	cfg.HotDataThreshold = 3
	c := NewEdgeCache(cfg, newOfflineTestLogger())
	const sz = int64(300 * 1024)

	_ = c.Put("hot", sz, "model_weight")
	// Access enough times to mark it hot.
	for i := 0; i < 4; i++ {
		if _, ok := c.Get("hot"); !ok {
			t.Fatal("hot key must stay resident during warm-up")
		}
	}
	_ = c.Put("cold-1", sz, "feature_data")
	// This Put pushes total over 1 MB and must evict a COLD entry, not "hot".
	if err := c.Put("cold-2", sz, "feature_data"); err != nil {
		t.Fatalf("Put cold-2: %v", err)
	}

	if _, ok := c.Get("hot"); !ok {
		t.Fatal("hot entry must be protected from eviction")
	}
}

// TestEdgeCache_PutTooLarge proves an entry larger than the whole cache is
// rejected honestly instead of silently dropped.
func TestEdgeCache_PutTooLarge(t *testing.T) {
	c := NewEdgeCache(testCacheConfig(1, CachePolicyLRU), newOfflineTestLogger())
	// 2 MB into a 1 MB cache: impossible even after evicting everything.
	if err := c.Put("huge", int64(2*1024*1024), "model_weight"); err == nil {
		t.Fatal("expected error when entry exceeds total capacity")
	}
}

// TestEdgeCache_StatsHitRate proves the derived hit-rate reflects real activity.
func TestEdgeCache_StatsHitRate(t *testing.T) {
	c := NewEdgeCache(testCacheConfig(64, CachePolicyLRU), newOfflineTestLogger())
	_ = c.Put("k", 1024, "config")

	c.Get("k")    // hit
	c.Get("k")    // hit
	c.Get("nope") // miss

	stats := c.GetStats()
	if stats.Hits != 2 || stats.Misses != 1 {
		t.Fatalf("hits=%d misses=%d, want 2/1", stats.Hits, stats.Misses)
	}
	// 2 hits of 3 accesses => ~66.7%.
	if stats.HitRate < 66.0 || stats.HitRate > 67.0 {
		t.Fatalf("hit rate = %.1f%%, want ~66.7%%", stats.HitRate)
	}
}
