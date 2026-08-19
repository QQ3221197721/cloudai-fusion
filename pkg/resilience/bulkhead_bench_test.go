package resilience

import (
	"context"
	"testing"
)

// BenchmarkTokenBucketLimiter_Allow measures the rate-limiter admission hot path.
func BenchmarkTokenBucketLimiter_Allow(b *testing.B) {
	limiter := NewTokenBucketLimiter(float64(b.N)+1, 1e9)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = limiter.Allow()
	}
}

// BenchmarkBulkhead_Execute measures the overhead of acquiring/releasing a slot.
func BenchmarkBulkhead_Execute(b *testing.B) {
	bh := NewBulkhead(16)
	ctx := context.Background()
	noop := func() error { return nil }
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bh.Execute(ctx, noop)
	}
}
