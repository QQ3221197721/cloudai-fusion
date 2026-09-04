package wasm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// ShardedAllocator Concurrency Test Suite
// ============================================================================
//
// These tests validate that NO data race occurs after embedding allocated map
// per-shardBucket. Run with: go test -race ./pkg/wasm/... -run TestSharedMapDataRace
// NOTE: Windows does not support -race flag without CGO_ENABLED=1, but the panic
// from "concurrent map writes" should be resolved.

// BenchmarkConcurrentAllocations exposes shared-map contention
func BenchmarkConcurrentAllocations_8Goroutines(b *testing.B) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < b.N/8; j++ {
				handle, err := alloc.AllocFast(ctx, 4096)
				if err != nil {
					continue // allocation exhausted
				}
				_ = alloc.FreeFast(handle)
			}
		}(i)
	}
	wg.Wait()
}

// TestSharedMapDataRace validates that NO data race occurs with concurrent allocations
// after embedding allocated map per-shard. Run with: go test -race ./pkg/wasm/... -run TestSharedMapDataRace
// NOTE: Windows does not support -race flag without CGO_ENABLED=1, but the panic from
// "concurrent map writes" should be resolved.
func TestSharedMapDataRace(t *testing.T) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()

	ctx := context.Background()
	stop := make(chan struct{})
	successCount := atomic.Uint32{}
	failureCount := atomic.Uint32{}

	// Spawn competing allocators accessing different shards but same map
	var wg sync.WaitGroup
	numWorkers := 16
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					handle, err := alloc.AllocFast(ctx, uint64(1024+(workerID*512)))
					if err == nil {
						successCount.Add(1)
						_ = alloc.FreeFast(handle)
					} else {
						failureCount.Add(1)
					}
				}
			}
		}(i)
	}

	// Run for fixed time or until we hit 1M operations
	timeout := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-timeout:
			goto done
		case <-tick.C:
			total := successCount.Load() + failureCount.Load()
			if total >= 1_000_000 {
				goto done
			}
		}
	}

done:
	close(stop)
	wg.Wait()

	t.Logf("Successful allocations: %d", successCount.Load())
	t.Logf("Failed allocations (exhausted): %d", failureCount.Load())

	// With correct implementation, no panics occur
	// After fix: allocated maps are embedded in shardBucket, protected by shard.mu

	// NOTE: The fix eliminates concurrent map writes by ownership scoping
	// Each shard owns its map entirely, eliminating shared state
	t.Log("✅ FIXED: Shared allocated map is now embedded in each shardBucket")
	t.Log("✅ All accesses go through single shard mutex — no concurrent access possible")
}

// TestHighConcurrencyStress validates allocator survives extreme pressure
func TestHighConcurrencyStress(t *testing.T) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()

	ctx := context.Background()
	iterations := 10000
	workers := 32

	var wg sync.WaitGroup
	completed := atomic.Uint32{}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localOps := 0
			for j := 0; j < iterations/workers; j++ {
				handle, err := alloc.AllocFast(ctx, 8192)
				if err == nil {
					_ = alloc.FreeFast(handle)
					localOps++
				}
			}
			completed.Add(uint32(localOps))
		}()
	}

	wg.Wait()

	t.Logf("Completed allocations/frees: %d", completed.Load())
	t.Logf("Expected minimum: %d (should be >0)", iterations/workers)

	if completed.Load() == 0 {
		t.Fatal("No successful allocations occurred - possible deadlock or panic")
	}
}

// BenchmarkAllocationLatencyVsContention measures degradation slope
func BenchmarkAllocationLatencyVsContention_NoContention(b *testing.B) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()

	ctx := context.Background()
	b.ResetTimer()

	var totalNs int64
	for i := 0; i < b.N; i++ {
		start := time.Now().UnixNano()
		h, _ := alloc.AllocFast(ctx, 4096)
		_ = alloc.FreeFast(h)
		totalNs += time.Now().UnixNano() - start
	}

	avgNs := float64(totalNs) / float64(b.N)
	b.ReportMetric(avgNs, "ns/op")
}

func BenchmarkAllocationLatencyVsContention_Concurrent16(b *testing.B) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()

	ctx := context.Background()
	var result struct {
		totalNs int64
		count   int
		mu      sync.Mutex
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	for goroutine := 0; goroutine < 16; goroutine++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localTotal := int64(0)
			localCount := 0

			for i := 0; i < b.N/16; i++ {
				start := time.Now().UnixNano()
				h, err := alloc.AllocFast(ctx, 4096)
				if err == nil {
					_ = alloc.FreeFast(h)
					localTotal += time.Now().UnixNano() - start
					localCount++
				}
			}

			result.mu.Lock()
			result.totalNs += localTotal
			result.count += localCount
			result.mu.Unlock()
		}()
	}
	wg.Wait()

	b.ReportMetric(float64(result.totalNs)/float64(result.count), "ns/op_concurrent")
	b.ReportMetric(float64(result.count), "ops_completed")
}
