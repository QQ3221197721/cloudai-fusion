package cache

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// DefaultConfig
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", cfg.Backend)
	}
	if cfg.L1MaxSize != 10000 {
		t.Errorf("L1MaxSize = %d, want 10000", cfg.L1MaxSize)
	}
	if cfg.KeyPrefix != "cloudai:" {
		t.Errorf("KeyPrefix = %q, want cloudai:", cfg.KeyPrefix)
	}
}

// ============================================================================
// memoryCache — basic CRUD
// ============================================================================

func newTestCache() Cache {
	return NewMemoryCache(Config{L1MaxSize: 100, KeyPrefix: "test:", L1DefaultTTL: time.Minute}, logrus.StandardLogger())
}

func TestMemoryCache_SetGet(t *testing.T) {
	c := newTestCache()
	defer c.Close()
	ctx := context.Background()

	err := c.Set(ctx, "k1", []byte("hello"), time.Minute)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	val, err := c.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(val) != "hello" {
		t.Errorf("Get = %q, want hello", val)
	}
}

func TestMemoryCache_Get_Miss(t *testing.T) {
	c := newTestCache()
	defer c.Close()

	val, err := c.Get(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for miss, got %q", val)
	}
}

func TestMemoryCache_Get_Expired(t *testing.T) {
	c := newTestCache()
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "exp", []byte("data"), 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	val, err := c.Get(ctx, "exp")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if val != nil {
		t.Error("expired key should return nil")
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	c := newTestCache()
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "del", []byte("data"), time.Minute)
	c.Delete(ctx, "del")

	val, _ := c.Get(ctx, "del")
	if val != nil {
		t.Error("deleted key should return nil")
	}
}

func TestMemoryCache_Exists(t *testing.T) {
	c := newTestCache()
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "ex", []byte("data"), time.Minute)

	exists, err := c.Exists(ctx, "ex")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Error("key should exist")
	}

	exists, _ = c.Exists(ctx, "no")
	if exists {
		t.Error("nonexistent key should not exist")
	}
}

func TestMemoryCache_Exists_Expired(t *testing.T) {
	c := newTestCache()
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "exp2", []byte("data"), 1*time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	exists, _ := c.Exists(ctx, "exp2")
	if exists {
		t.Error("expired key should not exist")
	}
}

func TestMemoryCache_DeletePattern(t *testing.T) {
	c := newTestCache()
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "user:1", []byte("a"), time.Minute)
	c.Set(ctx, "user:2", []byte("b"), time.Minute)
	c.Set(ctx, "order:1", []byte("c"), time.Minute)

	c.DeletePattern(ctx, "user:*")

	v1, _ := c.Get(ctx, "user:1")
	v2, _ := c.Get(ctx, "user:2")
	v3, _ := c.Get(ctx, "order:1")

	if v1 != nil || v2 != nil {
		t.Error("user:* should be deleted")
	}
	if v3 == nil {
		t.Error("order:1 should still exist")
	}
}

func TestMemoryCache_Eviction(t *testing.T) {
	c := NewMemoryCache(Config{L1MaxSize: 2, KeyPrefix: ""}, logrus.StandardLogger())
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "a", []byte("1"), time.Minute)
	c.Set(ctx, "b", []byte("2"), time.Minute)
	c.Set(ctx, "c", []byte("3"), time.Minute) // should evict oldest

	stats := c.Stats()
	// After eviction, should have at most 2 keys
	if stats.KeyCount > 2 {
		t.Errorf("KeyCount = %d, want <= 2", stats.KeyCount)
	}
}

func TestMemoryCache_Stats(t *testing.T) {
	c := newTestCache()
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "s1", []byte("data"), time.Minute)
	c.Get(ctx, "s1") // hit
	c.Get(ctx, "s2") // miss

	stats := c.Stats()
	if stats.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", stats.Backend)
	}
	if stats.Sets != 1 {
		t.Errorf("Sets = %d, want 1", stats.Sets)
	}
	if stats.Hits != 1 {
		t.Errorf("Hits = %d, want 1", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses = %d, want 1", stats.Misses)
	}
	if stats.HitRate != 0.5 {
		t.Errorf("HitRate = %f, want 0.5", stats.HitRate)
	}
}

func TestMemoryCache_Close(t *testing.T) {
	c := newTestCache()
	ctx := context.Background()
	c.Set(ctx, "k", []byte("v"), time.Minute)

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After close, cache should be empty
	val, _ := c.Get(ctx, "k")
	if val != nil {
		t.Error("after close, keys should be cleared")
	}
}

// ============================================================================
// redisCache (fallback mode)
// ============================================================================

func TestRedisCache_FallbackMode(t *testing.T) {
	cfg := Config{Backend: "redis", RedisAddr: "localhost:6379", KeyPrefix: "test:", L1MaxSize: 100}
	c := NewRedisCache(cfg, logrus.StandardLogger())
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "rk", []byte("redis-data"), time.Minute)
	val, err := c.Get(ctx, "rk")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(val) != "redis-data" {
		t.Errorf("Get = %q, want redis-data", val)
	}

	stats := c.Stats()
	if stats.Backend != "redis" {
		t.Errorf("Backend = %q, want redis", stats.Backend)
	}
}

func TestRedisCache_Delete(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	c := NewRedisCache(cfg, logrus.StandardLogger())
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "rd", []byte("v"), time.Minute)
	c.Delete(ctx, "rd")
	val, _ := c.Get(ctx, "rd")
	if val != nil {
		t.Error("deleted key should be nil")
	}
}

func TestRedisCache_Exists(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	c := NewRedisCache(cfg, logrus.StandardLogger())
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "re", []byte("v"), time.Minute)
	exists, _ := c.Exists(ctx, "re")
	if !exists {
		t.Error("key should exist")
	}
}

// ============================================================================
// MultiLevelCache — L1 + L2
// ============================================================================

func TestMultiLevelCache_GetFromL1(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	c := NewMultiLevelCache(cfg, logrus.StandardLogger())
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "ml1", []byte("data"), time.Minute)
	val, err := c.Get(ctx, "ml1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(val) != "data" {
		t.Errorf("Get = %q, want data", val)
	}
}

func TestMultiLevelCache_L2Promotion(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	mlc := NewMultiLevelCache(cfg, logrus.StandardLogger()).(*MultiLevelCache)
	defer mlc.Close()
	ctx := context.Background()

	// Write only to L2
	mlc.l2.Set(ctx, "l2only", []byte("from-l2"), time.Minute)

	// Get should find in L2 and promote to L1
	val, err := mlc.Get(ctx, "l2only")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(val) != "from-l2" {
		t.Errorf("Get = %q, want from-l2", val)
	}

	// Should now be in L1
	val, _ = mlc.l1.Get(ctx, "l2only")
	if val == nil {
		t.Error("key should have been promoted to L1")
	}
}

func TestMultiLevelCache_Miss(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	c := NewMultiLevelCache(cfg, logrus.StandardLogger())
	defer c.Close()

	val, err := c.Get(context.Background(), "nothing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != nil {
		t.Error("expected nil for miss")
	}
}

func TestMultiLevelCache_Delete(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	c := NewMultiLevelCache(cfg, logrus.StandardLogger())
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "mld", []byte("v"), time.Minute)
	c.Delete(ctx, "mld")

	val, _ := c.Get(ctx, "mld")
	if val != nil {
		t.Error("deleted key should be nil")
	}
}

func TestMultiLevelCache_Exists(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	c := NewMultiLevelCache(cfg, logrus.StandardLogger())
	defer c.Close()
	ctx := context.Background()

	c.Set(ctx, "mle", []byte("v"), time.Minute)
	exists, _ := c.Exists(ctx, "mle")
	if !exists {
		t.Error("key should exist")
	}
}

func TestMultiLevelCache_Stats(t *testing.T) {
	cfg := Config{KeyPrefix: "", L1MaxSize: 100}
	c := NewMultiLevelCache(cfg, logrus.StandardLogger())
	defer c.Close()

	stats := c.Stats()
	if stats.Backend != "multi-level" {
		t.Errorf("Backend = %q, want multi-level", stats.Backend)
	}
}

// ============================================================================
// TypedCache
// ============================================================================

type testItem struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestTypedCache_SetGet(t *testing.T) {
	inner := newTestCache()
	defer inner.Close()

	tc := NewTypedCache[testItem](inner, "item")
	ctx := context.Background()

	item := &testItem{Name: "widget", Value: 42}
	if err := tc.Set(ctx, "w1", item, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := tc.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("got nil")
	}
	if got.Name != "widget" || got.Value != 42 {
		t.Errorf("got %+v, want {widget 42}", got)
	}
}

func TestTypedCache_GetMiss(t *testing.T) {
	inner := newTestCache()
	defer inner.Close()

	tc := NewTypedCache[testItem](inner, "item")
	got, err := tc.Get(context.Background(), "nothing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Error("expected nil for miss")
	}
}

func TestTypedCache_Delete(t *testing.T) {
	inner := newTestCache()
	defer inner.Close()

	tc := NewTypedCache[testItem](inner, "item")
	ctx := context.Background()

	tc.Set(ctx, "d1", &testItem{Name: "x"}, time.Minute)
	tc.Delete(ctx, "d1")

	got, _ := tc.Get(ctx, "d1")
	if got != nil {
		t.Error("deleted key should be nil")
	}
}

func TestTypedCache_InvalidateAll(t *testing.T) {
	inner := newTestCache()
	defer inner.Close()

	tc := NewTypedCache[testItem](inner, "item")
	ctx := context.Background()

	tc.Set(ctx, "i1", &testItem{Name: "a"}, time.Minute)
	tc.Set(ctx, "i2", &testItem{Name: "b"}, time.Minute)

	tc.InvalidateAll(ctx)

	g1, _ := tc.Get(ctx, "i1")
	g2, _ := tc.Get(ctx, "i2")
	if g1 != nil || g2 != nil {
		t.Error("all items should be invalidated")
	}
}

// ============================================================================
// Factory
// ============================================================================

func TestNew_Memory(t *testing.T) {
	c := New(Config{Backend: "memory", L1MaxSize: 10}, logrus.StandardLogger())
	defer c.Close()
	stats := c.Stats()
	if stats.Backend != "memory" {
		t.Errorf("Backend = %q, want memory", stats.Backend)
	}
}

func TestNew_Redis(t *testing.T) {
	c := New(Config{Backend: "redis", L1MaxSize: 10}, logrus.StandardLogger())
	defer c.Close()
	stats := c.Stats()
	if stats.Backend != "redis" {
		t.Errorf("Backend = %q, want redis", stats.Backend)
	}
}

func TestNew_Multi(t *testing.T) {
	c := New(Config{Backend: "multi", L1MaxSize: 10}, logrus.StandardLogger())
	defer c.Close()
	stats := c.Stats()
	if stats.Backend != "multi-level" {
		t.Errorf("Backend = %q, want multi-level", stats.Backend)
	}
}

// ============================================================================
// matchGlob
// ============================================================================

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		str     string
		want    bool
	}{
		{"*", "anything", true},
		{"user:*", "user:123", true},
		{"user:*", "order:1", false},
		{"exact", "exact", true},
		{"exact", "diff", false},
	}
	for _, tc := range tests {
		t.Run(tc.pattern+"_"+tc.str, func(t *testing.T) {
			got := matchGlob(tc.pattern, tc.str)
			if got != tc.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tc.pattern, tc.str, got, tc.want)
			}
		})
	}
}
