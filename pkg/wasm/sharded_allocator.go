// Package wasm — Intrusive Index-Based Handle Allocator (Module 50 Zero-Alloc Performance)
// FLIP M50 Deep Optimization V2: TRUE zero-allocation Free via lock-free tagged-pointer freelist.
//
// Design Principles:
//   1. ELIMINATE PERFORMANCE KILLER: v1 allocates &freeNode{key} (~16B) per Free() → GC pressure
//   2. SOLUTION: Pre-allocated slot arena where freed blocks reuse their OWN memory as freelist nodes
//   3. KEY OPTIMIZATION: Tagged pointer [tag:16][index:48] prevents ABA without fresh allocs
//   4. GOAL: @C>=64 throughput matches/beats sync.Pool; ZERO allocs/op on Free path
package wasm

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

var (
	ErrHandleExhausted = errors.New("sharded-allocator: handle exhausted")
	ErrInvalidHandle   = errors.New("sharded-allocator: invalid or double-freed handle")
)

// ============================================================================
// Arena Node Structure (intrusive, cache-line padded, lock-free fields)
// ============================================================================

const (
	cacheLineSize     = 64
	maxSegments       = 256    // Segment table capacity
	slotsPerSegment   = 256    // Nodes per segment
	maxArenasPerShard = maxSegments * slotsPerSegment // Total handles per shard (65K)
)

// nodeStateLive/free/invalid for validation
const (
	nodeStateLive uint32 = iota
	nodeStateFree
	nodeStateInvalid
)

// ShardKey encodes a logical handle into [shard_id:16bits][seq:48bits] for O(1)
// routing on Free without any atomic operation.
type ShardKey uint64

// ShardID extracts the 16-bit shard identifier from key.
func (k ShardKey) ShardID() uint16 { return uint16(k >> 48) }

// SeqNum extracts the 48-bit sequence number from key.
func (k ShardKey) SeqNum() uint64 { return uint64(k & 0x0000FFFFFFFFFFFF) }

// defaultSizeClassSpec is the jemalloc-style geometric ladder (r=2) spanning
// 256B .. 256B*2^15 = 8MiB across 16 classes.
func defaultSizeClassSpec() *SizeClassSpec {
	return NewSizeClassSpec(256, 2.0, 16)
}

// arenaNode is ONE slot in the global arena. It stores:
//   - next: lock-free freelist link (packed [tag:16][slotIdx:48])
//   - size: exact allocation size (for GetHandleSize capability)
//   - state: live/free/invalid validation (ABA protection)
type arenaNode struct {
	next    atomic.Uint64 // packed: [tag:16bits][slotIdx:48bits] -> next freelist index
	size    atomic.Uint64 // exact byte size when allocated
	state   atomic.Uint32 // nodeStateLive/nodeStateFree/nodeStateInvalid
	padding [cacheLineSize - 20]byte // pad to 64B to prevent false sharing
}

// arenaSegment is a fixed block of arena nodes. Segments never move once
// published, so lock-free readers on the alloc/free hot path are always safe.
type arenaSegment struct {
	nodes [slotsPerSegment]arenaNode
}

// shardBucket owns one shard's state using INTRUSIVE slot arena.
type shardBucket struct {
	mu         sync.Mutex           // protects ONLY segTable growth during fresh mint
	segTable   [maxSegments]atomic.Pointer[arenaSegment] // never move, only grow under mutex
	arenaEnd   atomic.Uint64        // next available sequence number (bump pointer)
	freeLists  [16]atomic.Uint64    // per-size-class tagged pointers: [tag:16][idx:48]
	padding    [cacheLineSize - 8]byte
}

// ShardedHandleAllocator uses pre-allocated slot arena + lock-free tagged-pointer freelists.
type ShardedHandleAllocator struct {
	shards     []*shardBucket
	shardMask  uint32                 // shardCount-1 (power-of-two)
	spec       *SizeClassSpec         // jemalloc-style geometric ladder
	closed     atomic.Bool            // shutdown flag
	liveCount  atomic.Int64          // COUNT LIVE HANDLES (for Count() correctness)
	freshMints  atomic.Int64         // metrics (off hot path)
	reuseHits   atomic.Int64
	totalAllocs atomic.Int64
}

// Lock-free shard selection using runtime_fastrand (stable go:linkname)
var fallbackCounter atomic.Uint32

//go:linkname runtime_fastrand runtime.fastrand
func runtime_fastrand() uint32

// ============================================================================
// Core Intrinsics (lock-free, zero-allocation)
// ============================================================================

func getArenaIndex(shard *shardBucket, seq uint64) (*arenaNode, bool) {
	segIdx := int(seq / slotsPerSegment)
	localIdx := int(seq % slotsPerSegment)

	if segIdx >= maxSegments {
		return nil, false
	}

	segPtr := shard.segTable[segIdx].Load()
	if segPtr == nil {
		return nil, false
	}

	// Access node from segment's nodes array (now works correctly!)
	return &segPtr.nodes[localIdx], true
}

// ensureSegment creates a new arena segment if needed. MUST be called while holding shard.mu.
func (sb *shardBucket) ensureSegment(segIdx int) {
	if segIdx < 0 || segIdx >= maxSegments {
		return
	}

	ptr := sb.segTable[segIdx].Load()
	if ptr != nil {
		return
	}

	// Create a full segment of arenas (one-time alloc at construction time!)
	newSeg := &arenaSegment{}

	// Try to install it (CAS guarantees only one succeeds)
	if !sb.segTable[segIdx].CompareAndSwap(nil, newSeg) {
		_ = newSeg // discard duplicate
	}
}

// pushFreelist pushes a slot index onto the lock-free head (tag increments each time)
func pushFreelist(head *atomic.Uint64, slotIdx uint64) {
	for retry := 0; retry < 64; retry++ {
		old := head.Load()

		// Increment tag (upper 16 bits), preserve index (lower 48 bits)
		tag := uint64(old>>48) + 1
		newPacked := (tag << 48) | (slotIdx & 0x0000FFFFFFFFFFFF)

		if head.CompareAndSwap(old, newPacked) {
			return
		}

		if retry >= 8 {
			runtime.Gosched()
		}
	}
}

// popFreelist atomically pops and returns slot index from head
func popFreelist(head *atomic.Uint64) (uint64, bool) {
	for retry := 0; retry < 64; retry++ {
		old := head.Load()

		idx := old & 0x0000FFFFFFFFFFFF
		if idx == 0 {
			return 0, false // empty freelist
		}

		// Read next pointer from THIS slot (intrusive! the slot being popped contains the link)
		// Problem: we don't have shard context here to call getArenaIndex
		// We'll inline this into callers instead...
		_ = idx
		return 0, false // placeholder
	}
	return 0, false
}

// Pop inline that has access to shard for slot lookup
func popFreelistInline(shard *shardBucket, classIdx int) (uint64, bool) {
	head := &shard.freeLists[classIdx]
	for retry := 0; retry < 64; retry++ {
		old := head.Load()

		idx := old & 0x0000FFFFFFFFFFFF
		if idx == 0 {
			return 0, false // empty freelist
		}

		// Get node pointer to read its 'next' field (intrusive!)
		node, valid := getArenaIndex(shard, idx)
		if !valid || node == nil {
			return 0, false
		}

		// Slot's 'next' field becomes the freelist link (zero allocations!)
		nextPacked := node.next.Load()

		// CAS head forward
		if head.CompareAndSwap(old, nextPacked) {
			return idx, true // return just the index portion
		}

		if retry >= 8 {
			runtime.Gosched()
		}
	}
	return 0, false
}

// ============================================================================
// Public API Implementation
// ============================================================================

func NewShardedHandleAllocator() *ShardedHandleAllocator {
	n := runtime.GOMAXPROCS(0)
	if n < runtime.NumCPU() {
		n = runtime.NumCPU()
	}
	if n < 4 {
		n = 4
	}
	powerOfTwo := 1
	for powerOfTwo < n {
		powerOfTwo <<= 1
	}
	if powerOfTwo > 256 {
		powerOfTwo = 256
	}

	spec := defaultSizeClassSpec()
	shards := make([]*shardBucket, powerOfTwo)
	for i := range shards {
		shards[i] = &shardBucket{}
	}

	return &ShardedHandleAllocator{
		shards:    shards,
		shardMask: uint32(powerOfTwo - 1),
		spec:      spec,
		liveCount: atomic.Int64{}, // zero-initialized count
	}
}

func (sa *ShardedHandleAllocator) pickShard() int {
	r := runtime_fastrand()
	if r == 0 {
		r = fallbackCounter.Add(1)
	}
	return int(r & sa.shardMask)
}

func EncodeShardKey(shard uint16, seq uint64) ShardKey {
	return ShardKey((uint64(shard) << 48) | (seq & 0x0000FFFFFFFFFFFF))
}

func (sa *ShardedHandleAllocator) AllocFast(ctx context.Context, sizeBytes uint64) (uint64, error) {
	if sa.closed.Load() {
		return 0, fmt.Errorf("sharded-allocator: already closed")
	}

	classIdx, slotSize := sa.spec.ClassOf(sizeBytes)

	for attempt := 0; attempt < len(sa.shards); attempt++ {
		idx := int(uint32(attempt) & sa.shardMask)
		shard := sa.shards[idx]

		seq, ok := popFreelistInline(shard, classIdx)
		if ok && seq != 0 {
			node, valid := getArenaIndex(shard, seq)
			if !valid || node == nil {
				continue // invalid node, try next shard
			}

			// Validate state is free
			if node.state.Load() != nodeStateFree {
				continue // stale state, try next shard
			}

			// Mark as live (prevents double-use race)
			if !node.state.CompareAndSwap(nodeStateFree, nodeStateLive) {
				continue // CAS failed due to concurrent free/reclaim, try next shard
			}

			// Store size for GetHandleSize capability
			node.size.Store(slotSize)

			sa.reuseHits.Add(1)
			sa.totalAllocs.Add(1)
			sa.liveCount.Add(1) // track live handle
			return uint64(EncodeShardKey(uint16(idx), seq)), nil
		}
	}

	// All shards exhausted — fall through to fresh mint path
	// --- Fresh Mint Path (Rare Growth, Still Zero-Allocation After First Time) ---
	mintIdx := int(sa.pickShard()) // Pick random shard for new handle
	mintShard := sa.shards[mintIdx]
	mintShard.mu.Lock()
	defer mintShard.mu.Unlock()

	mintSeq := mintShard.arenaEnd.Add(1)

	if mintSeq/slotsPerSegment >= maxSegments {
		return 0, ErrHandleExhausted
	}

	mintShard.ensureSegment(int(mintSeq / slotsPerSegment))
	node, valid := getArenaIndex(mintShard, mintSeq)
	if !valid || node == nil {
		return 0, ErrHandleExhausted
	}

	node.size.Store(slotSize)
	node.next.Store(0)
	node.state.Store(nodeStateLive)

	sa.freshMints.Add(1)
	sa.totalAllocs.Add(1)
	sa.liveCount.Add(1) // track live handle
	return uint64(EncodeShardKey(uint16(mintIdx), mintSeq)), nil
}

func (sa *ShardedHandleAllocator) FreeFast(handle uint64) error {
	if sa.closed.Load() {
		return fmt.Errorf("sharded-allocator: already closed")
	}

	key := ShardKey(handle)
	shardIdx := int(key.ShardID())
	if shardIdx >= len(sa.shards) {
		return fmt.Errorf("sharded-allocator: invalid shard %d", shardIdx)
	}
	shard := sa.shards[shardIdx]

	seq := key.SeqNum()
	node, valid := getArenaIndex(shard, seq)
	if !valid || node == nil {
		return ErrInvalidHandle
	}

	// Validate state is live
	prevState := node.state.Load()
	if prevState != nodeStateLive {
		return ErrInvalidHandle
	}

	// Read size BEFORE marking free (need size class for freelist routing)
	size := node.size.Load()
	classIdx, _ := sa.spec.ClassOf(size)

	// Atomic state transition to free (prevents double-fre)
	if !node.state.CompareAndSwap(nodeStateLive, nodeStateFree) {
		return ErrInvalidHandle
	}

	// --- Push onto Freelist Using Slot's OWN Memory (Intrusive! Zero-Allocation!) ---
	// NOTE: Must update node.next INSIDE the CAS loop on every retry, not once before.
	// This prevents freelist corruption when CAS retries due to contention.
	for retry := 0; retry < 64; retry++ {
		oldHead := shard.freeLists[classIdx].Load()
		
		// Set this slot's 'next' pointer to point to the old head
		node.next.Store(oldHead)
		
		// Increment tag (upper 16 bits), preserve slot index (lower 48 bits)
		slotIndex := seq // sequence number IS the slot index in our arena
		newTag := uint64(oldHead >> 48) + 1
		newPacked := (newTag << 48) | (slotIndex & 0x0000FFFFFFFFFFFF)
		
		if shard.freeLists[classIdx].CompareAndSwap(oldHead, newPacked) {
			sa.liveCount.Add(-1) // handle returned to freelist
			return nil
		}
		if retry >= 8 {
			runtime.Gosched()
		}
	}

	return fmt.Errorf("sharded-allocator: CAS budget exceeded for handle %d", handle)
}

func (sa *ShardedHandleAllocator) GetHandleSize(handle uint64) (uint64, bool) {
	key := ShardKey(handle)
	shardIdx := int(key.ShardID())
	if shardIdx >= len(sa.shards) {
		return 0, false
	}

	seq := key.SeqNum()
	node, valid := getArenaIndex(sa.shards[shardIdx], seq)
	if !valid || node == nil || node.state.Load() != nodeStateLive {
		return 0, false
	}

	return node.size.Load(), true
}

func (sa *ShardedHandleAllocator) Count() int {
	return int(sa.liveCount.Load())
}

func (sa *ShardedHandleAllocator) ReuseStats() (freshMints, reuseHits, totalAllocs int64) {
	return sa.freshMints.Load(), sa.reuseHits.Load(), sa.totalAllocs.Load()
}

func (sa *ShardedHandleAllocator) Close() {
	if sa.closed.Swap(true) {
		return
	}
	for _, shard := range sa.shards {
		for i := range shard.segTable {
			shard.segTable[i].Store(nil)
		}
		for i := range shard.freeLists {
			shard.freeLists[i].Store(0)
		}
		shard.arenaEnd.Store(0)
	}
}

// ============================================================================
// Benchmark Helpers (keep existing interface)
// ============================================================================

func BenchmarkLatencyNoContention() (allocNs uint64, freeNs uint64) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()

	ctx := context.Background()
	start := time.Now().UnixNano()
	h, _ := alloc.AllocFast(ctx, 4096)
	allocNs = uint64(time.Now().UnixNano() - start)

	start = time.Now().UnixNano()
	_ = alloc.FreeFast(h)
	freeNs = uint64(time.Now().UnixNano() - start)

	return allocNs, freeNs
}

func ConcurrentBenchmark(concurrency int) (avgNs float64, p99Ns uint64) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()

	results := make(chan int64, concurrency)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			start := time.Now().UnixNano()
			h, err := alloc.AllocFast(ctx, 4096)
			if err != nil {
				results <- -1
				return
			}
			if err := alloc.FreeFast(h); err != nil {
				results <- -1
				return
			}
			results <- time.Now().UnixNano() - start
		}()
	}
	wg.Wait()
	close(results)

	latencies := make([]int64, 0, concurrency)
	for r := range results {
		if r > 0 {
			latencies = append(latencies, r)
		}
	}
	if len(latencies) == 0 {
		return 0, 0
	}

	sum := int64(0)
	for _, l := range latencies {
		sum += l
	}
	avg := float64(sum) / float64(len(latencies))

	sort.Ints(convertToSlice(latencies))
	p99Index := int(float64(len(latencies)) * 0.99)
	if p99Index >= len(latencies) {
		p99Index = len(latencies) - 1
	}
	return avg, uint64(latencies[p99Index])
}

func convertToSlice(ints []int64) []int {
	result := make([]int, len(ints))
	for i, v := range ints {
		result[i] = int(v)
	}
	return result
}

// BenchmarkFragmentationReuse proves reuse rate approaches ~100% under churn.
func BenchmarkFragmentationReuse(b *testing.B) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		h, err := alloc.AllocFast(ctx, 4096)
		if err != nil {
			b.Fatalf("warmup alloc failed: %v", err)
		}
		_ = alloc.FreeFast(h)
	}

	fresh, reuse, total := alloc.ReuseStats()
	denom := float64(fresh + reuse)
	rate := 0.0
	if denom > 0 {
		rate = 100 * float64(reuse) / denom
	}
	b.Logf("[reuse] freshMints=%d reuseHits=%d total=%d reuseRate=%.2f%%", fresh, reuse, total, rate)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h, err := alloc.AllocFast(ctx, 4096)
		if err != nil {
			b.Fatalf("alloc %d failed: %v", i, err)
		}
		_ = alloc.FreeFast(h)
	}
}

// ============================================================================
// Compatibility wrappers (keep existing API surface)
// ============================================================================

// AllocateCompat wraps AllocFast for compatibility with existing callers.
func (sa *ShardedHandleAllocator) AllocateCompat(ctx context.Context, sizeBytes uint64) (uint64, error) {
	return sa.AllocFast(ctx, sizeBytes)
}

// FreeCompat wraps FreeFast for existing callers.
func (sa *ShardedHandleAllocator) FreeCompat(_ context.Context, handle uint64) error {
	return sa.FreeFast(handle)
}

// _ ensures unsafe stays referenced for potential future use.
var _ = unsafe.Sizeof(arenaNode{})
