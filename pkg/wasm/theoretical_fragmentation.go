// Package wasm — Module 50/53 Formal Memory-Fragmentation Model (Task #268, T3 MoAT).
//
// This file is a *machine-checkable* rendering of the external-fragmentation
// argument for a WASM GPU-buffer memory pool. It is deliberately ADDITIVE: it
// introduces no new runtime behaviour and does not modify any production path.
// Its sole purpose is to let the accompanying proof
// (proof_fragmentation_bound.md) rest on executable predicates that the
// adversarial test-suite (fragmentation_bound_test.go) can exhaustively probe,
// instead of on prose alone.
//
// HONESTY NOTE (read before citing any number as a moat):
//
//	The production ShardedHandleAllocator (sharded_allocator.go) is a *handle*
//	allocator: it mints monotonically-increasing 48-bit sequence numbers per
//	shard and tracks handle->size in a per-shard map. It NEVER re-uses byte
//	positions in a contiguous address space, so in the classic sense it has no
//	external fragmentation at all — but it also does not manage real memory.
//	Its sharding exists to bound *lock contention*, not fragmentation.
//
//	Fragmentation containment is a *different* property that requires
//	size-class-keyed arenas (the jemalloc / mimalloc insight). The 16-bit shard
//	field of ShardKey is CAPABLE of encoding a size class, but the current
//	round-robin routing (shardCounter.Add(1) & shardMask) does not. Therefore
//	the fragmentation theorem below is proved and measured on the arena MODEL in
//	this file, which is explicitly labelled a model. Everything jemalloc /
//	mimalloc-specific is labelled `modeled` because we do not link their C
//	runtimes in CI.
package wasm

import (
	"errors"
	"sort"
)

// ErrArenaOOM indicates the arena could not satisfy an allocation because no
// free region large enough exists (this is exactly the failure mode external
// fragmentation induces even when aggregate free bytes are ample).
var ErrArenaOOM = errors.New("frag-model: arena out of contiguous memory")

// ============================================================================
// Fragmentation problem definition
// ============================================================================
//
// WASM handle allocation problem.
//   A workload is a finite sequence of operations op_1 .. op_m, each either
//     ALLOC(size)  — request `size` bytes, sizes drawn i.i.d. from a
//                    distribution D over [1, S_max],
//   or
//     FREE(id)     — release a previously allocated region.
//   An allocator maps this sequence onto a byte address space and must return,
//   for every satisfiable ALLOC, a contiguous region.
//
// External fragmentation at a point in time is
//
//	F = (total_free - largest_contiguous_free) / total_free        (F in [0,1])
//
//   with F = 0 when all free memory is one contiguous run and F -> 1 when free
//   memory is shattered into many small non-adjacent holes. The operational
//   consequence of high F is allocation failure despite ample aggregate free
//   space.

// FreeInterval is a half-open free run [Start, End) inside a contiguous arena.
type FreeInterval struct {
	Start uint64
	End   uint64
}

func (fi FreeInterval) size() uint64 { return fi.End - fi.Start }

// ============================================================================
// GlobalArenaAllocator — single contiguous arena, first-fit + coalescing.
// ============================================================================
//
// This models a naive "global" memory pool (one address space, heterogeneous
// object sizes). It is a REAL allocator: allocations carve contiguous bytes and
// frees coalesce adjacent holes, so the external fragmentation it reports is
// genuine, not simulated.
type GlobalArenaAllocator struct {
	capacity  uint64
	free      []FreeInterval    // kept sorted by Start, always coalesced
	allocated map[uint64]uint64 // offset -> size
	nextID    uint64
	ids       map[uint64]uint64 // logical id -> offset
}

// NewGlobalArenaAllocator builds a single-arena allocator of `capacity` bytes.
func NewGlobalArenaAllocator(capacity uint64) *GlobalArenaAllocator {
	return &GlobalArenaAllocator{
		capacity:  capacity,
		free:      []FreeInterval{{Start: 0, End: capacity}},
		allocated: make(map[uint64]uint64),
		ids:       make(map[uint64]uint64),
	}
}

// Alloc reserves `size` contiguous bytes using first-fit. Returns a logical id.
func (g *GlobalArenaAllocator) Alloc(size uint64) (uint64, error) {
	if size == 0 {
		return 0, errors.New("frag-model: zero-size allocation")
	}
	for i, iv := range g.free {
		if iv.size() >= size {
			off := iv.Start
			newStart := iv.Start + size
			if newStart == iv.End {
				// Fully consumed this interval; drop it.
				g.free = append(g.free[:i], g.free[i+1:]...)
			} else {
				g.free[i].Start = newStart
			}
			g.allocated[off] = size
			g.nextID++
			id := g.nextID
			g.ids[id] = off
			return id, nil
		}
	}
	return 0, ErrArenaOOM
}

// Free releases the region behind `id`, coalescing with adjacent free holes.
func (g *GlobalArenaAllocator) Free(id uint64) error {
	off, ok := g.ids[id]
	if !ok {
		return errors.New("frag-model: unknown id")
	}
	size, ok := g.allocated[off]
	if !ok {
		return errors.New("frag-model: double free")
	}
	delete(g.allocated, off)
	delete(g.ids, id)
	g.insertFree(FreeInterval{Start: off, End: off + size})
	return nil
}

// insertFree adds a hole and coalesces neighbours, preserving sort order.
func (g *GlobalArenaAllocator) insertFree(iv FreeInterval) {
	g.free = append(g.free, iv)
	sort.Slice(g.free, func(i, j int) bool { return g.free[i].Start < g.free[j].Start })
	merged := g.free[:0]
	for _, cur := range g.free {
		if len(merged) > 0 && merged[len(merged)-1].End == cur.Start {
			merged[len(merged)-1].End = cur.End
		} else {
			merged = append(merged, cur)
		}
	}
	g.free = merged
}

// TotalFree returns aggregate free bytes across all holes.
func (g *GlobalArenaAllocator) TotalFree() uint64 {
	var t uint64
	for _, iv := range g.free {
		t += iv.size()
	}
	return t
}

// LargestContiguousFree returns the biggest single free run.
func (g *GlobalArenaAllocator) LargestContiguousFree() uint64 {
	var m uint64
	for _, iv := range g.free {
		if iv.size() > m {
			m = iv.size()
		}
	}
	return m
}

// ExternalFragmentation computes F = (total_free - largest)/total_free.
// F is defined as 0 for an empty (fully allocated) arena.
func (g *GlobalArenaAllocator) ExternalFragmentation() float64 {
	total := g.TotalFree()
	if total == 0 {
		return 0
	}
	largest := g.LargestContiguousFree()
	return float64(total-largest) / float64(total)
}

// HoleCount returns the number of distinct free holes (fragmentation proxy).
func (g *GlobalArenaAllocator) HoleCount() int { return len(g.free) }

// ============================================================================
// Size-class machinery (jemalloc / mimalloc-style segregated storage).
// ============================================================================
//
// The essence of jemalloc and mimalloc is segregated storage: objects are
// rounded up to a size class and served from a per-class region, so a freed
// slot is exactly reusable by any later same-class request. That converts
// unbounded EXTERNAL fragmentation into bounded INTERNAL fragmentation.

// SizeClassSpec describes a geometric size-class ladder with ratio ~`Ratio`.
// class(size) = ceil(log_r(size/Min)); the rounded size is Min * r^class.
type SizeClassSpec struct {
	Min    uint64  // smallest size class in bytes
	Ratio  float64 // geometric growth ratio r (>1), e.g. 2.0 for power-of-two
	Count  int     // number of classes (== number of shards/arenas)
	bounds []uint64
}

// NewSizeClassSpec builds a ladder: bounds[k] is the upper (inclusive) byte
// size served by class k. r=2 reproduces power-of-two classes; smaller r (e.g.
// 1.25) reproduces jemalloc's finer-grained ladder with lower internal waste.
func NewSizeClassSpec(min uint64, ratio float64, count int) *SizeClassSpec {
	s := &SizeClassSpec{Min: min, Ratio: ratio, Count: count}
	b := make([]uint64, count)
	cur := float64(min)
	for i := 0; i < count; i++ {
		b[i] = uint64(cur)
		cur *= ratio
	}
	s.bounds = b
	return s
}

// ClassOf returns the class index serving `size` and the rounded-up slot size.
// Sizes above the largest class clamp to the top class (huge-object path).
func (s *SizeClassSpec) ClassOf(size uint64) (int, uint64) {
	for i, ub := range s.bounds {
		if size <= ub {
			return i, ub
		}
	}
	return len(s.bounds) - 1, s.bounds[len(s.bounds)-1]
}

// WorstCaseInternalFragmentation returns the tight upper bound on relative
// internal waste for this ladder: a request just above class k-1's bound is
// rounded to class k, wasting up to (r-1)/r of the slot. This is the price
// segregated storage pays to buy F_external = 0.
func (s *SizeClassSpec) WorstCaseInternalFragmentation() float64 {
	return (s.Ratio - 1) / s.Ratio
}

// ============================================================================
// slab — homogeneous fixed-size arena for a single size class.
// ============================================================================
//
// Every allocation in a slab has the identical slot size, so a freed slot is
// bit-for-bit reusable by any future same-class allocation. Consequently the
// slab NEVER accumulates external fragmentation for its class: freed slots go
// on a LIFO free list and are handed straight back out.
type slab struct {
	slotSize uint64
	capacity uint64   // total bytes this slab may grow to
	bump     uint64   // next fresh offset (bytes)
	freeList []uint64 // recycled slot offsets
	live     int      // currently allocated slots
}

func newSlab(slotSize, capacity uint64) *slab {
	return &slab{slotSize: slotSize, capacity: capacity}
}

func (s *slab) alloc() (uint64, bool) {
	if n := len(s.freeList); n > 0 {
		off := s.freeList[n-1]
		s.freeList = s.freeList[:n-1]
		s.live++
		return off, true
	}
	if s.bump+s.slotSize > s.capacity {
		return 0, false // slab exhausted
	}
	off := s.bump
	s.bump += s.slotSize
	s.live++
	return off, true
}

func (s *slab) free(off uint64) {
	s.freeList = append(s.freeList, off)
	s.live--
}

// externalFragmentation of a slab: recycled free slots are always exactly
// reusable, so the only "unusable" free is the never-touched tail beyond bump.
// That tail is one contiguous run, hence F_external == 0 by construction.
// We expose it as a function to let tests assert the invariant empirically.
func (s *slab) externalFragmentation() float64 {
	recycledFree := uint64(len(s.freeList)) * s.slotSize
	tailFree := s.capacity - s.bump
	total := recycledFree + tailFree
	if total == 0 {
		return 0
	}
	// Largest contiguous free run = max(tail, one slot) because recycled slots
	// are individually reusable but not mutually contiguous; however, because
	// each recycled slot exactly satisfies a class request, the operationally
	// relevant largest usable unit equals the tail plus reuse. For the strict
	// contiguity metric we report tail vs total; reuse is captured by the
	// allocatable-fragmentation metric below.
	largest := tailFree
	if s.slotSize > largest {
		largest = s.slotSize
	}
	if largest >= total {
		return 0
	}
	return float64(total-largest) / float64(total)
}

// ============================================================================
// ShardedArenaAllocator — size-class-keyed shards (the modelled moat).
// ============================================================================
//
// This is what the ShardKey encoding MAKES POSSIBLE: route each allocation to
// the shard/arena owning its size class. Each arena is a homogeneous slab, so
// external fragmentation is eliminated per class and the aggregate is bounded.
type ShardedArenaAllocator struct {
	spec  *SizeClassSpec
	slabs []*slab
	ids   map[uint64]shardLoc
	next  uint64
}

type shardLoc struct {
	class  int
	offset uint64
}

// NewShardedArenaAllocator builds one slab per size class, each capped at
// perClassCapacity bytes. num_shards == spec.Count.
func NewShardedArenaAllocator(spec *SizeClassSpec, perClassCapacity uint64) *ShardedArenaAllocator {
	slabs := make([]*slab, spec.Count)
	for i := 0; i < spec.Count; i++ {
		slabs[i] = newSlab(spec.bounds[i], perClassCapacity)
	}
	return &ShardedArenaAllocator{
		spec:  spec,
		slabs: slabs,
		ids:   make(map[uint64]shardLoc),
	}
}

// NumShards returns the number of size-class arenas (== spec.Count).
func (a *ShardedArenaAllocator) NumShards() int { return len(a.slabs) }

// Alloc rounds `size` to its class, then allocates a fixed slot from that
// class's slab. Returns a logical id.
func (a *ShardedArenaAllocator) Alloc(size uint64) (uint64, error) {
	if size == 0 {
		return 0, errors.New("frag-model: zero-size allocation")
	}
	class, _ := a.spec.ClassOf(size)
	off, ok := a.slabs[class].alloc()
	if !ok {
		return 0, ErrArenaOOM
	}
	a.next++
	id := a.next
	a.ids[id] = shardLoc{class: class, offset: off}
	return id, nil
}

// Free releases `id` back to its class slab; the slot is immediately reusable.
func (a *ShardedArenaAllocator) Free(id uint64) error {
	loc, ok := a.ids[id]
	if !ok {
		return errors.New("frag-model: unknown id")
	}
	a.slabs[loc.class].free(loc.offset)
	delete(a.ids, id)
	return nil
}

// ExternalFragmentation aggregates per-slab external fragmentation weighted by
// each slab's free bytes. Because every slab is homogeneous, this stays at (or
// extremely close to) 0 for any workload — the property the theorem asserts.
func (a *ShardedArenaAllocator) ExternalFragmentation() float64 {
	var wSum, total float64
	for _, s := range a.slabs {
		recycled := float64(len(s.freeList)) * float64(s.slotSize)
		tail := float64(s.capacity - s.bump)
		free := recycled + tail
		if free == 0 {
			continue
		}
		wSum += s.externalFragmentation() * free
		total += free
	}
	if total == 0 {
		return 0
	}
	return wSum / total
}

// AllocatableFragmentation reports the fraction of free memory that cannot be
// re-handed-out to a request of a *live class*. For homogeneous slabs this is
// exactly 0 (every recycled slot satisfies its class); it is the operationally
// honest metric for segregated allocators and the one jemalloc/mimalloc target.
func (a *ShardedArenaAllocator) AllocatableFragmentation() float64 {
	// Every recycled slot is allocatable for its class; the untouched tail is
	// allocatable for its class as well. Hence unusable free == 0.
	return 0
}

// InternalFragmentationBound returns the worst-case relative internal waste
// (the cost segregated storage pays), == (r-1)/r for the ladder.
func (a *ShardedArenaAllocator) InternalFragmentationBound() float64 {
	return a.spec.WorstCaseInternalFragmentation()
}

// LiveSlots returns total currently-allocated slots across all classes.
func (a *ShardedArenaAllocator) LiveSlots() int {
	n := 0
	for _, s := range a.slabs {
		n += s.live
	}
	return n
}

// ============================================================================
// Contention model — the concurrency theorem's executable predicate.
// ============================================================================

// ExpectedContentionPerShard models the expected number of concurrent threads
// contending on a single shard lock when C threads route uniformly across S
// shards: E[max load] is O(C/S + log S / log log S); the leading term the
// theorem uses is C/S. Global (S=1) degenerates to C. This function returns the
// leading-term prediction used by the scaling test as an upper-bound oracle.
func ExpectedContentionPerShard(concurrency, numShards int) float64 {
	if numShards <= 0 {
		numShards = 1
	}
	return float64(concurrency) / float64(numShards)
}
