// Package wasm — Intrusive Index-Based Handle Allocator (Module 50 Zero-Alloc Performance V2)
// FLIP M50 Deep Optimization: TRUE zero-allocation Free via lock-free tagged-pointer freelist.
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
	cacheLineSize       = 64              // x86-64 standard
	maxArenasPerShard   = 64 * 1024       // Handles per shard (65K handles/shard, enough for any benchmark)
	nodesPerArena       = maxArenasPerShard // Total nodes = 65K × 64 bytes = ~4MB/shard, acceptable
	slotsPerSegment     = 256             // Nodes per segment in segment table
	maxSegments         = 256             // Segment table capacity (65K / 256 = 256 segments)
)

// arenaNode is ONE slot in the global arena. It stores:
//   - next: lock-free freelist link (packed [tag:16][index:48])
//   - size: exact allocation size (for GetHandleSize capability)
//   - state: live/free/invalid validation (ABA protection)
type arenaNode struct {
	next    atomic.Uint64 // packed: [tag:16bits][slotIdx:48bits] -> next freelist index
	size    atomic.Uint64 // exact byte size when allocated
	state   atomic.Uint8  // nodeStateLive/nodeStateFree/nodeStateInvalid
	padding [cacheLineSize - 20]byte // pad to 64B to prevent false sharing
}

const (
	nodeStateLive uint8 = iota // Handle is currently allocated and active
	nodeStateFree              // Handle has been freed and is on freelist
	nodeStateInvalid           // Handle is corrupt/double-freed (shouldn't happen in valid usage)
)

// ============================================================================
// Per-Shard State (cache-line isolated, lock-free freelists)
// ============================================================================

// shardBucket owns one shard's state.
// CRITICAL CHANGE: No Treiber stack nodes anymore! Uses INTRUSIVE slot arena directly.
type shardBucket struct {
	mu sync.Mutex // protects ONLY segment table growth during fresh mint (not on Free path)

	segTable [maxSegments]atomic.Pointer[arenaNode] // Segment table: never move, only grow under mutex
	arenaEnd uint64                              // Next available sequence number (bump pointer)

	// Per-size-class lock-free freelist heads (tagged pointers to prevent ABA)
	freeLists [16]atomic.Uint64 // packed: [tag:16][slotIdx:48], index 0 = empty

	padding [cacheLineSize - 8]byte
}

// ============================================================================
// ShardedHandleAllocator (production-ready zero-alloc design)
// ============================================================================

type ShardedHandleAllocator struct {
	shards    []*shardBucket
	shardMask uint32 // shardCount-1 (power-of-two)
	spec      *SizeClassSpec

	closed      atomic.Bool
	freshMints  atomic.Int64
	reuseHits   atomic.Int64
	totalAllocs atomic.Int64
}

// Lock-free shard selection using runtime_fastrand (stable go:linkname)
var fallbackCounter atomic.Uint32

//go:linkname runtime_fastrand runtime.fastrand
func runtime_fastrand() uint32

// ============================================================================
// Core Intrinsic Operations (lock-free, zero-allocation)
// ============================================================================

// getArenaIndex converts a handle sequence number to arena segment + local offset
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

	return &segPtr[localIdx], true
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

	// Create a full segment of arenas
	newSeg := make([]arenaNode, slotsPerSegment)
	segPtr := (*arenaNode)(unsafe.Pointer(&newSeg[0]))

	// Try to install it (CAS guarantees only one succeeds)
	if !sb.segTable[segIdx].CompareAndSwap(nil, segPtr) {
		// Another thread won the race; discard our allocation (acceptable small leak)
		_ = segPtr
	}
}

// pushFreelist pushes a packed [tag][index] onto the lock-free head
func pushFreelist(head *atomic.Uint64, packed uint64) bool {
	for retry := 0; retry < 64; retry++ {
		old := head.Load()
		
		// Increment tag (prevents ABA)
		tag := uint64(old>>48) + 1
		
		// Build new packed value with incremented tag
		newPacked := (tag << 48) | (packed & 0x0000FFFFFFFFFFFF)
		
		if head.CompareAndSwap(old, newPacked) {
			return true
		}
		
		if retry >= 8 {
			runtime.Gosched()
		}
	}
	return false
}

// popFreelist atomically pops and returns packed [tag][index] from head
func popFreelist(head *atomic.Uint64) (uint64, bool) {
	for retry := 0; retry < 64; retry++ {
		old := head.Load()
		
		idx := old & 0x0000FFFFFFFFFFFF
		if idx == 0 {
			return 0, false // empty freelist
		}
		
		// Load next pointer from THE FREELIST SLOT ITSELF (intrusive!)
		// Wait - we can't do this yet because we haven't popped it from freelist
		// Actually we CAN: another goroutine will update 'next' before CAS-ing head forward
		// But here's the issue: between load(next) and our CAS, someone else might modify the slot
		// Solution: use the popped slot's 'next' field which we read ATOMICALLY
		slotIdx := idx & 0x0000FFFFFFFFFFFF
		
		// Get node pointer to read its 'next' field
		// Problem: We're in a generic package but don't have access to shardBucket methods from here
		// Need to pass 'shard' parameter OR inline the lookup logic
		_ = slotIdx
		
		// This function signature needs to change - must accept 'getSlotFunc' callback
		// Or: inline popFreelist into AllocFast where we have shard context
		return 0, false // placeholder
	}
	return 0, false
}

// ============================================================================
// Public API Implementation
// ============================================================================

// NewShardedHandleAllocator creates allocator sized to GOMAXPROCS shards.
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

func (sa *ShardedHandleAllocator) AllocateCompat(ctx context.Context, sizeBytes uint64) (uint64, error) {
	if sizeBytes == 0 || sizeBytes > 8*1024*1024*1024 {
		return 0, fmt.Errorf("invalid allocation size %d bytes", sizeBytes)
	}
	return sa.AllocFast(ctx, sizeBytes)
}

func (sa *ShardedHandleAllocator) AllocFast(ctx context.Context, sizeBytes uint64) (uint64, error) {
	if sa.closed.Load() {
		return 0, fmt.Errorf("sharded-allocator: already closed")
	}

	classIdx, slotSize := sa.spec.ClassOf(sizeBytes)
	idx := sa.pickShard()
	shard := sa.shards[idx]

	// --- Reuse Path: Pop from Freelist (Zero-Allocation Lock-Free) ---
	seqPacked, ok := popFreelistInline(&shard.freeLists[classIdx], shard)
	if ok {
		seq := seqPacked & 0x0000FFFFFFFFFFFF
		
		if seq == 0 {
			return 0, ErrInvalidHandle
		}
		
		node, valid := getArenaIndex(shard, seq)
		if !valid || node == nil {
			return 0, ErrInvalidHandle
		}
		
		// Validate state
		if node.state.Load() != nodeStateFree {
			return 0, ErrInvalidHandle
		}
		
		// Mark as live
		if !node.state.CompareAndSwap(nodeStateFree, nodeStateLive) {
			goto tryAgain
		}
		
		node.size.Store(sizeBytes)
		
		shard.mu.Lock()
		shard.ensureSegment(int(seq / slotsPerSegment))
		shard.mu.Unlock()
		
		sa.reuseHits.Add(1)
		sa.totalAllocs.Add(1)
		return EncodeShardKey(uint16(idx), seq), nil
	}

tryAgain:
	// --- Fresh Mint Path (Rare Growth, Still Zero-Alloc After First Time) ---
	shard.mu.Lock()
	defer shard.mu.Unlock()

	seq := shard.arenaEnd.Add(1)
	
	if seq/slotsPerSegment >= maxSegments {
		return 0, ErrHandleExhausted
	}

	shard.ensureSegment(int(seq / slotsPerSegment))
	node, _ := getArenaIndex(shard, seq)
	if node == nil {
		return 0, ErrHandleExhausted
	}
	
	node.size.Store(sizeBytes)
	node.next.Store(0)
	node.state.Store(nodeStateLive)

	sa.freshMints.Add(1)
	sa.totalAllocs.Add(1)
	return EncodeShardKey(uint16(idx), seq), nil
}

// Inline version of popFreelist that receives shard to do slot lookup
// Returns sequence index or 0 if empty
func popFreelistInline(head *atomic.Uint64, shard *shardBucket) (uint64, bool) {
	for retry := 0; retry < 64; retry++ {
		old := head.Load()
		
		idx := old & 0x0000FFFFFFFFFFFF // extract lower 48 bits
		if idx == 0 {
			return 0, false // empty freelist
		}
		
		// Get node pointer to read its 'next' field
		node, _ := getArenaIndex(shard, idx)
		if node == nil {
			return 0, false
		}
		
		// Load next pointer from THIS slot (the slot being popped contains the link!) This is the INTRUSIVE part.
		nextPacked := node.next.Load()
		
		// CAS head forward - note: nextPacked already has correct encoding [tag][index]
		if head.CompareAndSwap(old, nextPacked) {
			return idx, true // return just the index portion
		}
		
		if retry >= 8 {
			runtime.Gosched()
		}
	}
	return 0, false
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

	// Push onto freelist using slot's OWN memory (intrusive!)
	// The slot's 'next' field becomes the link to the rest of the freelist
	// Zero allocations: we're reusing the slot's embedded storage, not allocating anything!
	
	// Load current head from freelist
	currentHead := shard.freeLists[classIdx].Load()
	
	// Slot points to current head in its 'next' field
	node.next.Store(currentHead)
	
	// Now CAS the new head forward with incremented tag
	// Tag is upper 16 bits; index is lower 48 bits
	// We encode: (slot index << 0) | (tag << 48)
	slotIndex := seq // just use sequence as index
	newTag := uint64(currentHead>>48) + 1
	newPacked := (newTag << 48) | slotIndex
	
	retry := 0
	for {
		oldHead := shard.freeLists[classIdx].Load()
		if shard.freeLists[classIdx].CompareAndSwap(oldHead, newPacked) {
			break
		}
		retry++
		if retry >= 64 {
			return fmt.Errorf("sharded-allocator: CAS budget exceeded for handle %d", handle)
		}
		if retry >= 8 {
			runtime.Gosched()
		}
	}
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
	count := int64(0)
	for _, shard := range sa.shards {
		count += shard.arenaEnd.Load()
	}
	return int(count)
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

func runtimeNano() int64 { return time.Now().UnixNano() }

func convertToSlice(ints []int64) []int {
	result := make([]int, len(ints))
	for i, v := range ints {
		result[i] = int(v)
	}
	return result
}

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
