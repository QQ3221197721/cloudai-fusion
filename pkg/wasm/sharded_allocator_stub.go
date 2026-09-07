// Package wasm — Minimal ShardedHandleAllocator stub for compilation

package wasm

import (
	"context"
	"fmt"
	"sync"
)

type ShardKey uint64

func (k ShardKey) ShardID() uint16 {
	return uint16(k >> 48)
}

func (k ShardKey) SeqNum() uint64 {
	return uint64(k & 0x0000FFFFFFFFFFFF)
}

func EncodeShardKey(shard uint16, seq uint64) ShardKey {
	return ShardKey((uint64(shard) << 48) | (seq & 0x0000FFFFFFFFFFFF))
}

// Minimal sharded allocator stub to allow compilation without full M50 implementation
type ShardedHandleAllocator struct {
	mu      sync.Mutex
	nextIdx uint64
}

func NewShardedHandleAllocator() *ShardedHandleAllocator {
	return &ShardedHandleAllocator{}
}

func (sa *ShardedHandleAllocator) AllocFast(ctx context.Context, sizeBytes uint64) (uint64, error) {
	if sizeBytes == 0 || sizeBytes > 8*1024*1024*1024 {
		return 0, fmt.Errorf("invalid allocation size %d bytes", sizeBytes)
	}
	sa.mu.Lock()
	idx := sa.nextIdx
	sa.nextIdx++
	sa.mu.Unlock()
	return idx, nil
}

func (sa *ShardedHandleAllocator) FreeFast(handle uint64) error {
	return nil // no-op
}

func (sa *ShardedHandleAllocator) AllocateCompat(ctx context.Context, sizeBytes uint64) (uint64, error) {
	return sa.AllocFast(ctx, sizeBytes)
}

func (sa *ShardedHandleAllocator) FreeCompat(ctx context.Context, handle uint64) error {
	return sa.FreeFast(handle)
}

func (sa *ShardedHandleAllocator) Count() int {
	return 0
}

func (sa *ShardedHandleAllocator) ReuseStats() (freshMints, reuseHits, totalAllocs int64) {
	return 0, 0, 0
}

func (sa *ShardedHandleAllocator) GetHandleSize(handle uint64) (uint64, bool) {
	return 0, false
}

func (sa *ShardedHandleAllocator) Close() {
}

// Benchmarks for testing
func BenchmarkLatencyNoContention() (allocNs uint64, freeNs uint64) {
	return 100, 50 // mock values
}
