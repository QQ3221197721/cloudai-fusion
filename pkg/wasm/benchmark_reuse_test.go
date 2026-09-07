package wasm

import (
	"context"
	"testing"
)

func BenchmarkFragmentationReuseRate(b *testing.B) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	ctx := context.Background()

	const batch = 4096
	
	// Warmup: allocate and free to seed free-lists
	handles := make([]uint64, batch)
	for i := 0; i < batch; i++ {
		h, err := alloc.AllocFast(ctx, 4096)
		if err != nil {
			b.Fatalf("warmup alloc failed: %v", err)
		}
		handles[i] = h
		_ = alloc.FreeFast(h)
	}

	freshBefore, _, _ := alloc.ReuseStats()
	b.ResetTimer()
	
	// All allocations should hit recycled slots
	for i := 0; i < b.N; i++ {
		h, err := alloc.AllocFast(ctx, 4096)
		if err != nil {
			b.Fatalf("alloc %d failed: %v", i, err)
		}
		_ = alloc.FreeFast(h)
	}
	
	freshAfter, reuseAfter, _ := alloc.ReuseStats()
	newFresh := freshAfter - freshBefore
	newReuse := reuseAfter
	
	reuseRate := 100.0 * float64(newReuse) / float64(newFresh+newReuse)
	b.Logf("freshMints=%d reuseHits=%d reuseRate=%.2f%%", newFresh, newReuse, reuseRate)
}

func BenchmarkBoundedFreshMints(b *testing.B) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	ctx := context.Background()

	const workingSet = 256

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		live := make([]uint64, 0, workingSet)
		for j := 0; j < workingSet; j++ {
			h, err := alloc.AllocFast(ctx, 8192)
			if err != nil {
				b.Fatalf("alloc failed: %v", err)
			}
			live = append(live, h)
		}
		for _, h := range live {
			_ = alloc.FreeFast(h)
		}
		live = live[:0]
	}
	
	fresh, reuse, total := alloc.ReuseStats()
	reuseRate := 100.0 * float64(reuse) / float64(total)
	b.Logf("total=%d fresh=%d reuse=%d reuseRate=%.2f%%", total, fresh, reuse, reuseRate)
}
