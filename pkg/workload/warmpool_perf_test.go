package workload

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// Workload Performance Benchmarks
//
// Validates two performance barriers:
// 1. WarmPool: O(1) instance acquisition vs 15-30s cold start
// 2. IncrementalCheckpoint: O(changed_params) I/O vs O(all_params)
//
// Run: go test -bench=Benchmark -benchmem -v ./pkg/workload/
// ============================================================================

// BenchmarkWarmPool_Acquire measures warm instance acquisition.
// This is the hot path: channel receive = ~50-100ns.
func BenchmarkWarmPool_Acquire(b *testing.B) {
	pool := NewWarmPool("gpt2-large", 100)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, ok := pool.Acquire(ctx)
		if ok {
			pool.Release(inst) // return to pool for next iteration
		}
	}
}

// BenchmarkWarmPool_AcquireRelease measures full cycle (acquire + use + release).
func BenchmarkWarmPool_AcquireRelease(b *testing.B) {
	pool := NewWarmPool("bert-base", 50)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, ok := pool.Acquire(ctx)
		if ok {
			// Simulate minimal inference work (just the routing overhead)
			_ = inst.ModelID
			pool.Release(inst)
		}
	}
}

// BenchmarkColdStart_Simulated simulates cold start overhead (model load from disk).
// Real cold start: 5-30s. We simulate 5ms as representative minimum.
func BenchmarkColdStart_Simulated(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate: container ready (1ms) + model load (4ms) = 5ms minimum
		time.Sleep(5 * time.Millisecond)
	}
}

// BenchmarkCheckpoint_Full measures full checkpoint save (all params written).
func BenchmarkCheckpoint_Full(b *testing.B) {
	// Simulate a model with 100 parameters, each 1KB
	params := make(map[string][]byte, 100)
	for i := 0; i < 100; i++ {
		data := make([]byte, 1024)
		rand.Read(data)
		params[fmt.Sprintf("layer.%d.weight", i)] = data
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Full save: write ALL params (100KB total)
		totalBytes := 0
		for _, v := range params {
			totalBytes += len(v)
		}
		_ = totalBytes
	}
}

// BenchmarkCheckpoint_Incremental measures incremental checkpoint (only diffs).
// With 5% parameter change rate, saves 95% I/O.
func BenchmarkCheckpoint_Incremental(b *testing.B) {
	// Initial state: 100 params, each 1KB
	params := make(map[string][]byte, 100)
	for i := 0; i < 100; i++ {
		data := make([]byte, 1024)
		rand.Read(data)
		params[fmt.Sprintf("layer.%d.weight", i)] = data
	}

	ckpt := NewIncrementalCheckpointer()
	// First save establishes base
	ckpt.ComputeDiff(params)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Change only 5 out of 100 params (5% change rate)
		for j := 0; j < 5; j++ {
			key := fmt.Sprintf("layer.%d.weight", (i*5+j)%100)
			data := make([]byte, 1024)
			rand.Read(data)
			params[key] = data
		}
		diff := ckpt.ComputeDiff(params)
		_ = diff
	}
}

// TestWarmPool_HitRate validates pool serves requests from warm instances.
func TestWarmPool_HitRate(t *testing.T) {
	pool := NewWarmPool("test-model", 10)
	ctx := context.Background()

	// 10 requests should all hit warm pool
	for i := 0; i < 10; i++ {
		inst, ok := pool.Acquire(ctx)
		if !ok {
			t.Fatalf("request %d: expected warm hit", i)
		}
		pool.Release(inst)
	}

	stats := pool.Stats()
	t.Logf("Hits: %d, Misses: %d, HitRate: %.2f%%, AvgLatency: %.2f us",
		stats.Hits, stats.Misses, stats.HitRate*100, stats.AvgWarmLatUs)

	if stats.HitRate < 1.0 {
		t.Errorf("expected 100%% hit rate, got %.2f%%", stats.HitRate*100)
	}
	if stats.AvgWarmLatUs > 100 { // should be well under 100us
		t.Errorf("warm latency too high: %.2f us", stats.AvgWarmLatUs)
	}
}

// TestWarmPool_Exhaustion validates graceful degradation when pool is empty.
func TestWarmPool_Exhaustion(t *testing.T) {
	pool := NewWarmPool("test-model", 3)
	ctx := context.Background()

	// Drain pool
	acquired := make([]*WarmInstance, 0, 3)
	for i := 0; i < 3; i++ {
		inst, ok := pool.Acquire(ctx)
		if !ok {
			t.Fatalf("should get 3 warm instances")
		}
		acquired = append(acquired, inst)
	}

	// Next acquire should miss (pool empty)
	_, ok := pool.Acquire(ctx)
	if ok {
		t.Error("expected miss when pool exhausted")
	}

	// Return instances
	for _, inst := range acquired {
		pool.Release(inst)
	}

	stats := pool.Stats()
	t.Logf("After exhaustion: Hits=%d Misses=%d", stats.Hits, stats.Misses)
}

// TestCheckpoint_Incremental_Savings validates I/O savings.
func TestCheckpoint_Incremental_Savings(t *testing.T) {
	// 100 params, 1KB each = 100KB total
	params := make(map[string][]byte, 100)
	for i := 0; i < 100; i++ {
		data := make([]byte, 1024)
		rand.Read(data)
		params[fmt.Sprintf("layer.%d.weight", i)] = data
	}

	ckpt := NewIncrementalCheckpointer()

	// First save: all params are "new" (100% written)
	diff1 := ckpt.ComputeDiff(params)
	t.Logf("First save: full=%d diff=%d ratio=%.2f%%",
		diff1.FullSizeBytes, diff1.DiffSizeBytes, diff1.CompressionRatio()*100)

	// Second save: change only 5 params (5%)
	for i := 0; i < 5; i++ {
		data := make([]byte, 1024)
		rand.Read(data)
		params[fmt.Sprintf("layer.%d.weight", i)] = data
	}
	diff2 := ckpt.ComputeDiff(params)
	t.Logf("Second save: full=%d diff=%d ratio=%.2f%% savings=%.0f%%",
		diff2.FullSizeBytes, diff2.DiffSizeBytes, diff2.CompressionRatio()*100,
		(1-diff2.CompressionRatio())*100)

	if diff2.CompressionRatio() > 0.10 {
		t.Errorf("expected <10%% ratio for 5%% change, got %.2f%%", diff2.CompressionRatio()*100)
	}

	t.Logf("Total I/O saved: %d bytes", ckpt.SavedBytes())
}
