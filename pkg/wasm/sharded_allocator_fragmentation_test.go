package wasm

// Task #270 — Production integration proof for the size-class free-list moat.
//
// These tests are ADDITIVE and exercise the PRODUCTION ShardedHandleAllocator
// (sharded_allocator.go), not the arena MODEL in theoretical_fragmentation.go.
// They prove that AllocFast now recycles freed handles through per-shard,
// per-size-class free-lists — i.e. the allocatable fragmentation of freed
// handles is driven to 0, the property previously proved only on the model.

import (
	"context"
	"testing"
)

// TestProdAllocator_FreeListReuse proves that after freeing a batch of handles,
// the next same-size allocations RECYCLE those freed handle positions instead of
// bumping fresh ones — the direct evidence that the monotonic never-reuse
// behaviour is gone.
func TestProdAllocator_FreeListReuse(t *testing.T) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	ctx := context.Background()

	const batch = 4096
	const size = uint64(4096) // routes to a single size class

	// Phase 1: allocate a full batch (all fresh mints).
	handles := make([]uint64, 0, batch)
	for i := 0; i < batch; i++ {
		h, err := alloc.AllocFast(ctx, size)
		if err != nil {
			t.Fatalf("phase-1 alloc %d failed: %v", i, err)
		}
		handles = append(handles, h)
	}
	fresh1, reuse1, _ := alloc.ReuseStats()
	t.Logf("[prod-reuse] after phase-1: fresh=%d reuse=%d (expect fresh=%d reuse=0)", fresh1, reuse1, batch)
	if fresh1 != int64(batch) || reuse1 != 0 {
		t.Fatalf("phase-1 should be all fresh mints: fresh=%d reuse=%d", fresh1, reuse1)
	}

	// Phase 2: free the entire batch — every handle lands on its class free-list.
	for i, h := range handles {
		if err := alloc.FreeFast(h); err != nil {
			t.Fatalf("free %d failed: %v", i, err)
		}
	}
	if live := alloc.Count(); live != 0 {
		t.Fatalf("expected 0 live handles after freeing all, got %d", live)
	}

	// Phase 3: re-allocate the same batch/size. Because shard routing is the
	// same deterministic round-robin and each shard has exactly its freed slots
	// back, EVERY allocation must be a recycle — zero new fresh mints.
	for i := 0; i < batch; i++ {
		if _, err := alloc.AllocFast(ctx, size); err != nil {
			t.Fatalf("phase-3 realloc %d failed: %v", i, err)
		}
	}
	fresh2, reuse2, _ := alloc.ReuseStats()
	newFresh := fresh2 - fresh1
	newReuse := reuse2 - reuse1
	reuseRate := 100 * float64(newReuse) / float64(newFresh+newReuse)
	t.Logf("[prod-reuse] phase-3 realloc: newFresh=%d newReuse=%d reuseRate=%.2f%%", newFresh, newReuse, reuseRate)

	if newFresh != 0 {
		t.Fatalf("phase-3 must reuse ALL freed handles (F_alloc->0), but minted %d fresh handles", newFresh)
	}
	if newReuse != int64(batch) {
		t.Fatalf("expected %d recycled allocations, got %d", batch, newReuse)
	}
	t.Logf("[prod-reuse] PROVEN: production ShardedHandleAllocator reuse rate = 100%% (allocatable fragmentation = 0)")
}

// TestProdAllocator_SizeClassRouting proves that handles of DIFFERENT size
// classes do not cross-contaminate each other's free-lists: freeing a small
// handle must not let a large request recycle it (that would be a correctness
// bug that inflates internal fragmentation and breaks the moat argument).
func TestProdAllocator_SizeClassRouting(t *testing.T) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	ctx := context.Background()

	// Allocate + free a small handle (class of 512B).
	small, err := alloc.AllocFast(ctx, 512)
	if err != nil {
		t.Fatalf("small alloc failed: %v", err)
	}
	if err := alloc.FreeFast(small); err != nil {
		t.Fatalf("small free failed: %v", err)
	}

	freshBefore, reuseBefore, _ := alloc.ReuseStats()

	// A large request (class of 1MiB) must NOT recycle the small freed handle:
	// its class free-list is empty, so it must mint fresh.
	large, err := alloc.AllocFast(ctx, 1<<20)
	if err != nil {
		t.Fatalf("large alloc failed: %v", err)
	}
	freshAfter, reuseAfter, _ := alloc.ReuseStats()

	if reuseAfter != reuseBefore {
		t.Fatalf("large request wrongly recycled a different-size-class handle (reuse %d->%d)", reuseBefore, reuseAfter)
	}
	if freshAfter != freshBefore+1 {
		t.Fatalf("large request should have minted exactly 1 fresh handle (fresh %d->%d)", freshBefore, freshAfter)
	}

	// Sanity: the reported size for the large handle is the true requested size.
	if sz, ok := alloc.GetHandleSize(large); !ok || sz != (1<<20) {
		t.Fatalf("large handle size mismatch: got (%d,%v), want (%d,true)", sz, ok, 1<<20)
	}
	t.Logf("[size-class] confirmed: cross-class recycling prevented; large handle minted fresh with correct size")
}

// TestProdAllocator_BoundedFreshMints proves the operational fragmentation
// claim under a churn workload: over many alloc/free cycles that never exceed a
// working set of W live handles, the total fresh mints stays bounded by roughly
// W (peak concurrency), NOT by the total number of allocations. A monotonic
// never-reuse allocator would mint one fresh handle per allocation (unbounded).
func TestProdAllocator_BoundedFreshMints(t *testing.T) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	ctx := context.Background()

	const workingSet = 256
	const cycles = 200 // total allocations = workingSet * cycles = 51200

	live := make([]uint64, 0, workingSet)
	for c := 0; c < cycles; c++ {
		// Fill the working set.
		for i := 0; i < workingSet; i++ {
			h, err := alloc.AllocFast(ctx, 8192)
			if err != nil {
				t.Fatalf("cycle %d alloc %d failed: %v", c, i, err)
			}
			live = append(live, h)
		}
		// Drain it (all freed handles go back to free-lists).
		for _, h := range live {
			if err := alloc.FreeFast(h); err != nil {
				t.Fatalf("cycle %d free failed: %v", c, err)
			}
		}
		live = live[:0]
	}

	fresh, reuse, _ := alloc.ReuseStats()
	totalAllocs := int64(workingSet * cycles)
	t.Logf("[bounded-mints] totalAllocs=%d fresh=%d reuse=%d reuseRate=%.2f%%",
		totalAllocs, fresh, reuse, 100*float64(reuse)/float64(fresh+reuse))

	// Fresh mints must be bounded by the working set (+ shard-rounding slack),
	// NOT by totalAllocs. Allow generous slack of 2x working set for shard skew.
	if fresh > int64(2*workingSet) {
		t.Fatalf("fresh mints %d exceeded bounded working-set expectation (~%d) — reuse not working", fresh, workingSet)
	}
	if fresh >= totalAllocs {
		t.Fatalf("fresh mints %d ~= totalAllocs %d — allocator is still monotonic never-reuse", fresh, totalAllocs)
	}
	t.Logf("[bounded-mints] PROVEN: %d allocations serviced with only %d fresh handles (%.1fx reuse leverage)",
		totalAllocs, fresh, float64(totalAllocs)/float64(fresh))
}
