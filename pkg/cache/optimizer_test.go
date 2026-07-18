package cache

import (
	"fmt"
	"testing"
	"time"
)

// TestBloomFilter_NoFalseNegatives verifies the fundamental Bloom guarantee and
// that the empirical false-positive rate tracks the theoretical estimate.
// FNV hashing is deterministic, so these results are stable (not flaky).
func TestBloomFilter_NoFalseNegatives(t *testing.T) {
	const n = 10000
	bf := NewBloomFilter(n, 0.01)

	for i := 0; i < n; i++ {
		bf.Add(fmt.Sprintf("key-%d", i))
	}

	// No false negatives: every added key must be reported present.
	for i := 0; i < n; i++ {
		if !bf.MayContain(fmt.Sprintf("key-%d", i)) {
			t.Fatalf("false negative for key-%d (Bloom filters must never have false negatives)", i)
		}
	}

	// Empirical FPR on unseen keys should track the theoretical estimate.
	falsePos := 0
	for i := 0; i < n; i++ {
		if bf.MayContain(fmt.Sprintf("absent-%d", i)) {
			falsePos++
		}
	}
	empirical := float64(falsePos) / float64(n)
	theoretical := bf.EstimatedFPRate()

	if theoretical <= 0 || theoretical > 0.05 {
		t.Errorf("theoretical FPR should be ~0.01 for target 0.01, got %.4f", theoretical)
	}
	if empirical > theoretical+0.02 {
		t.Errorf("empirical FPR %.4f far exceeds theoretical %.4f", empirical, theoretical)
	}
	t.Logf("Bloom FPR: theoretical=%.4f empirical=%.4f (n=%d, size=%d, hashes=%d)",
		theoretical, empirical, n, bf.size, bf.hashes)
}

// TestBloomFilter_FPRateGrowsWithLoad proves EstimatedFPRate is a real function
// of load (not a constant): more elements => higher false-positive rate.
func TestBloomFilter_FPRateGrowsWithLoad(t *testing.T) {
	bf := NewBloomFilter(10000, 0.01)
	for i := 0; i < 200; i++ {
		bf.Add(fmt.Sprintf("k-%d", i))
	}
	low := bf.EstimatedFPRate()
	for i := 200; i < 8000; i++ {
		bf.Add(fmt.Sprintf("k-%d", i))
	}
	high := bf.EstimatedFPRate()
	if high <= low {
		t.Errorf("FPR should increase with load: low(200)=%.5f high(8000)=%.5f", low, high)
	}
}

// TestAdaptiveTTL_HotColdScaling verifies TTL adapts to real access frequency:
// unknown -> base, cold -> shorter, hot -> longer (monotonic), extreme -> clamped.
func TestAdaptiveTTL_HotColdScaling(t *testing.T) {
	cfg := DefaultAdaptiveTTLConfig()
	m := NewAdaptiveTTLManager(cfg)

	// Unknown key => base TTL.
	if got := m.ComputeTTL("unknown"); got != cfg.BaseTTL {
		t.Errorf("unknown key should get BaseTTL %v, got %v", cfg.BaseTTL, got)
	}

	// Cold key (single access) => shorter than base.
	m.RecordAccess("cold")
	if got := m.ComputeTTL("cold"); got >= cfg.BaseTTL {
		t.Errorf("cold key TTL %v should be < BaseTTL %v", got, cfg.BaseTTL)
	}

	// Hot key => longer than base.
	for i := 0; i < 15; i++ {
		m.RecordAccess("hot")
	}
	hot15 := m.ComputeTTL("hot")
	if hot15 <= cfg.BaseTTL {
		t.Errorf("hot key TTL %v should be > BaseTTL %v", hot15, cfg.BaseTTL)
	}

	// Hotter key => even longer (TTL is monotonic in access count).
	for i := 0; i < 40; i++ {
		m.RecordAccess("hotter")
	}
	if hotter40 := m.ComputeTTL("hotter"); hotter40 < hot15 {
		t.Errorf("more accesses should not reduce TTL: hot(15)=%v hotter(40)=%v", hot15, hotter40)
	}

	// Extremely hot => clamped to MaxTTL.
	for i := 0; i < 5000; i++ {
		m.RecordAccess("blazing")
	}
	if got := m.ComputeTTL("blazing"); got != cfg.MaxTTL {
		t.Errorf("blazing key TTL should clamp to MaxTTL %v, got %v", cfg.MaxTTL, got)
	}

	// All TTLs must respect the configured bounds.
	for _, k := range []string{"cold", "hot", "hotter", "blazing"} {
		ttl := m.ComputeTTL(k)
		if ttl < cfg.MinTTL || ttl > cfg.MaxTTL {
			t.Errorf("key %q TTL %v out of bounds [%v,%v]", k, ttl, cfg.MinTTL, cfg.MaxTTL)
		}
	}
}

// TestAdaptiveTTLManager_CloseStopsGoroutine verifies the background decay
// goroutine is stopped by Close (no leak, graceful shutdown).
func TestAdaptiveTTLManager_CloseStopsGoroutine(t *testing.T) {
	m := NewAdaptiveTTLManager(DefaultAdaptiveTTLConfig())

	// Close must stop the background decay goroutine and return promptly.
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return: decay goroutine leaked")
	}

	// Close is idempotent.
	m.Close()
}

// TestMemoryCache_CloseStopsEviction verifies Close stops the background
// eviction goroutine (no leak, graceful shutdown).
func TestMemoryCache_CloseStopsEviction(t *testing.T) {
	c := NewMemoryCache(Config{}, nil)

	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return: eviction goroutine leaked")
	}

	// Close is idempotent.
	_ = c.Close()
}
