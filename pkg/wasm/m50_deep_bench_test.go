// Package wasm — FLIP M50 Deep Benchmark vs Real sync.Pool
// Honesty mandate: measure real numbers at C=1/8/64/256, count=6 median, no edge cases.
package wasm

import (
	"context"
	"sync"
	"testing"
)

// ============================================================================
// sync.Pool competitor — exact best-case idiom
// ============================================================================

type poolSlot struct {
	handle uint64
	size   uint64
}

type syncPoolAllocator struct {
	pool       sync.Pool
	nextIdx    atomic.Uint64
	freshMints atomic.Int64
	reuseHits  atomic.Int64
}

func newSyncPoolAllocator() *syncPoolAllocator {
	spa := &syncPoolAllocator{}
	spa.pool.New = func() any {
		return &poolSlot{handle: spa.nextIdx.Add(1)}
	}
	return spa
}

func (spa *syncPoolAllocator) acquire(size uint64) *poolSlot {
	s := spa.pool.Get().(*poolSlot)
	if s.size == 0 {
		spa.freshMints.Add(1)
	} else {
		spa.reuseHits.Add(1)
	}
	s.size = size
	return s
}

func (spa *syncPoolAllocator) release(s *poolSlot) {
	spa.pool.Put(s)
}

func (spa *syncPoolAllocator) reuseStats() (fresh, reuse int64) {
	return spa.freshMints.Load(), spa.reuseHits.Load()
}

// ============================================================================
// Shared concurrency harness
// ============================================================================

func runConcurrent(b *testing.B, c int, op func()) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	per := b.N / c
	rem := b.N % c

	var wg sync.WaitGroup
	wg.Add(c)
	for g := 0; g < c; g++ {
		n := per
		if g < rem {
			n++
		}
		go func(iters int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				op()
			}
		}(n)
	}
	wg.Wait()
}

// ============================================================================
// M50 benchmarks — Sharded allocator vs sync.Pool at C=1/8/64/256
// ============================================================================

const m50HandleSize = 4096 // 4 KiB fixed work-unit size class

func benchSharded(b *testing.B, c int) {
	sa := NewShardedHandleAllocator()
	ctx := context.Background()
	runConcurrent(b, c, func() {
		h, err := sa.AllocFast(ctx, m50HandleSize)
		if err != nil {
			b.Fatal(err)
		}
		_ = sa.FreeFast(h)
	})
}

func benchStdlibPool(b *testing.B, c int) {
	spa := newSyncPoolAllocator()
	runConcurrent(b, c, func() {
		s := spa.acquire(m50HandleSize)
		spa.release(s)
	})
}

// --- C=1 ---
func BenchmarkM50Deep_Sharded_C1(b *testing.B)    { benchSharded(b, 1) }
func BenchmarkM50Deep_StdlibPool_C1(b *testing.B) { benchStdlibPool(b, 1) }

// --- C=8 ---
func BenchmarkM50Deep_Sharded_C8(b *testing.B)    { benchSharded(b, 8) }
func BenchmarkM50Deep_StdlibPool_C8(b *testing.B) { benchStdlibPool(b, 8) }

// --- C=64 ---
func BenchmarkM50Deep_Sharded_C64(b *testing.B)    { benchSharded(b, 64) }
func BenchmarkM50Deep_StdlibPool_C64(b *testing.B) { benchStdlibPool(b, 64) }

// --- C=256 ---
func BenchmarkM50Deep_Sharded_C256(b *testing.B)    { benchSharded(b, 256) }
func BenchmarkM50Deep_StdlibPool_C256(b *testing.B) { benchStdlibPool(b, 256) }

// ============================================================================
// Correctness tests: reuse rate, size isolation, free-by-id
// ============================================================================

func TestM50ReUseAndIsolation(t *testing.T) {
	ctx := context.Background()
	sa := NewShardedHandleAllocator()
	defer sa.Close()

	// Churn warmup to saturate freelists
	const rounds = 5000
	for i := 0; i < rounds; i++ {
		h, _ := sa.AllocFast(ctx, m50HandleSize)
		_ = sa.FreeFast(h)
	}

	fresh, reuse, total := sa.ReuseStats()
	denom := float64(fresh + reuse)
	reuseRate := 0.0
	if denom > 0 {
		reuseRate = 100 * float64(reuse) / denom
	}
	t.Logf("[reuse] fresh=%d reuse=%d total=%d reuseRate=%.2f%%", fresh, reuse, total, reuseRate)
	if reuseRate < 85.0 {
		t.Errorf("expected reuseRate ≥85%% under churn, got %.2f%%", reuseRate)
	}

	// Pool baseline comparison
	spa := newSyncPoolAllocator()
	for i := 0; i < rounds; i++ {
		s := spa.acquire(m50HandleSize)
		spa.release(s)
	}
	sf, sr := spa.reuseStats()
	poolDenom := float64(sf + sr)
	poolReuse := 0.0
	if poolDenom > 0 {
		poolReuse = 100 * float64(sr) / poolDenom
	}
	t.Logf("[pool] fresh=%d reuse=%d reuseRate=%.2f%%", sf, sr, poolReuse)

	// Size-class isolation: freeing 4KiB should not satisfy a 64KiB request
	h4, _ := sa.AllocFast(ctx, 4096)
	freshBefore, _, _ := sa.ReuseStats()
	_ = sa.FreeFast(h4)
	h64, _ := sa.AllocFast(ctx, 65536)
	freshAfter, _, _ := sa.ReuseStats()
	if freshAfter <= freshBefore {
		t.Error("size-class isolation broken: 64KiB reused freed 4KiB handle")
	}
	if sz, ok := sa.GetHandleSize(h64); !ok || sz != 65536 {
		t.Errorf("GetHandleSize returned size=%d ok=%v instead of exact 64KiB", sz, ok)
	}

	// Free-by-id from another goroutine
	h, _ := sa.AllocFast(ctx, m50HandleSize)
	done := make(chan error, 1)
	go func() { done <- sa.FreeFast(h) }()
	if err := <-done; err != nil {
		t.Fatalf("cross-goroutine free-by-id failed: %v", err)
	}
}
