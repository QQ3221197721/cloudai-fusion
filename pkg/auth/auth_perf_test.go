package auth

import (
	"fmt"
	"testing"
)

// ============================================================================
// Permission Cache Performance Benchmarks
//
// Validates: O(1) cached auth vs O(policy_tree) full evaluation.
//
// Run: go test -bench=BenchmarkPermCache -benchmem ./pkg/auth/
// ============================================================================

// BenchmarkPermCache_L1Hit measures LRU cache hit (positive decision cached).
func BenchmarkPermCache_L1Hit(b *testing.B) {
	cache := NewPermissionCache(10000, 5000)
	// Pre-populate with allowed decisions
	for i := 0; i < 1000; i++ {
		cache.RecordAllow(fmt.Sprintf("user-%d:read:resource-%d", i%100, i%50))
	}

	key := "user-42:read:resource-7" // known to be cached
	cache.RecordAllow(key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Check(key)
	}
}

// BenchmarkPermCache_L2Hit measures Bloom filter negative hit (denial cached).
func BenchmarkPermCache_L2Hit(b *testing.B) {
	cache := NewPermissionCache(10000, 5000)
	// Record denials in bloom filter
	for i := 0; i < 5000; i++ {
		cache.RecordDeny(fmt.Sprintf("user-%d:admin:secret-%d", i, i))
	}

	key := "user-42:admin:secret-42" // known to be denied
	cache.RecordDeny(key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Check(key)
	}
}

// BenchmarkPermCache_Miss measures cache miss (must hit policy engine).
func BenchmarkPermCache_Miss(b *testing.B) {
	cache := NewPermissionCache(10000, 5000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Unknown keys always miss both caches
		cache.Check(fmt.Sprintf("unknown-user-%d:action-%d:resource-%d", i, i, i))
	}
}

// BenchmarkPermCache_FullPolicyEval simulates full policy evaluation (baseline).
// Represents OPA/Casbin evaluating 100 policy rules.
func BenchmarkPermCache_FullPolicyEval(b *testing.B) {
	// Simulate: iterate 100 policies, check conditions
	policies := make([]string, 100)
	for i := range policies {
		policies[i] = fmt.Sprintf("policy-rule-%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate evaluating 100 policies (string comparisons)
		key := fmt.Sprintf("user-42:read:resource-%d", i%50)
		for _, p := range policies {
			if key == p {
				break
			}
		}
	}
}

// BenchmarkPermCache_Concurrent measures thread-safety under parallel load.
func BenchmarkPermCache_Concurrent(b *testing.B) {
	cache := NewPermissionCache(10000, 5000)
	// Pre-populate
	for i := 0; i < 1000; i++ {
		cache.RecordAllow(fmt.Sprintf("user-%d:read:res-%d", i%100, i%50))
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("user-%d:read:res-%d", i%100, i%50)
			cache.Check(key)
			i++
		}
	})
}

// TestPermCache_HitRate validates overall cache effectiveness.
func TestPermCache_HitRate(t *testing.T) {
	cache := NewPermissionCache(1000, 500)

	// Record 100 allow decisions and 50 deny patterns
	for i := 0; i < 100; i++ {
		cache.RecordAllow(fmt.Sprintf("user-%d:read:doc-%d", i, i))
	}
	for i := 0; i < 50; i++ {
		cache.RecordDeny(fmt.Sprintf("user-%d:admin:secret-%d", i, i))
	}

	// Simulate 1000 requests: 600 allowed (cached), 200 denied (bloom), 200 unknown
	for i := 0; i < 600; i++ {
		cache.Check(fmt.Sprintf("user-%d:read:doc-%d", i%100, i%100))
	}
	for i := 0; i < 200; i++ {
		cache.Check(fmt.Sprintf("user-%d:admin:secret-%d", i%50, i%50))
	}
	for i := 0; i < 200; i++ {
		cache.Check(fmt.Sprintf("random-user-%d:random-action-%d:random-res-%d", i, i, i))
	}

	stats := cache.Stats()
	t.Logf("L1 Hits (allow cache): %d", stats.L1Hits)
	t.Logf("L2 Hits (deny bloom):  %d", stats.L2Hits)
	t.Logf("Misses (policy eval):  %d", stats.Misses)
	t.Logf("Overall hit rate:      %.1f%%", stats.OverallHitRate*100)

	if stats.OverallHitRate < 0.70 {
		t.Errorf("expected >70%% hit rate, got %.1f%%", stats.OverallHitRate*100)
	}
}
