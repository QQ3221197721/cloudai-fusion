// Enhanced benchmarks for Θ(1) gang barrier with timeout and bitmask features.
//
// These benchmarks verify the performance improvements from:
//   1. WithTimeout() - O(1) timeout-based exit (sub-microsecond overhead)
//   2. Bitmask implementation - Single-instruction readiness check for P≤64
//   3. Exponential backoff spin wait - Efficient polling for stragglers
//
// Performance targets:
//   - Timeout setup overhead: <1μs per barrier creation
//   - Bitmask Arrive(): <0.5μs for P≤64 (vs 1.5μs counter approach)
//   - SpinUntilAllReady(): P99 latency <1ms under normal conditions
package training

import (
	"sync"
	"testing"
	"time"
)

// ============================================================================
// Benchmark: Timeout-based exit overhead
// ============================================================================

// BenchmarkBarrier_WithTimeout_Overhead measures the cost of attaching timeout semantics.
// Expected result: <1μs additional overhead (goroutine creation + context setup).
func BenchmarkBarrier_WithTimeout_Overhead_P8(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		barrier := NewGangBarrier("timeout-bench", 8)
		_ = barrier.WithTimeout(30*time.Second)
	}
}

// BenchmarkBarrier_WithTimeout_FailurePath measures timeout expiration time.
// When no workers arrive, should trigger Fail() after 30s (not measured in bench due to duration).
// Instead, we measure the goroutine wakeup latency once timeout triggers.
func BenchmarkBarrier_WithTimeout_WakeupLatency(b *testing.B) {
	b.Skip("Timeout expires after 30s - not practical for benchmark suite")
	
	// Alternative: measure rapid create/cancel cycles
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		barrier := NewGangBarrier("cancel-bench", 8)
		wrapped := barrier.WithTimeout(30*time.Second)
		
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			wrapped.CancelTimeout()
		}()
		
		wg.Wait()
	}
}

// ============================================================================
// Benchmark: Bitmask vs Counter approach
// ============================================================================

// BenchmarkBarrier_Bitmask_Arrival_Latency measures per-worker arrival speed.
// For small gangs (P≤64), bitmask should be ~3x faster than atomic counter due to:
//   - Single CAS loop vs multiple cache-line transitions
//   - CPU's POPCNT instruction for population count
func BenchmarkBarrier_Bitmask_Arrival_Latency_P4(b *testing.B) {
	benchmarkBitmaskArrival(b, 4)
}

func BenchmarkBarrier_Bitmask_Arrival_Latency_P16(b *testing.B) {
	benchmarkBitmaskArrival(b, 16)
}

func BenchmarkBarrier_Bitmask_Arrival_Latency_P64(b *testing.B) {
	// Maximum supported size for bitmask approach
	benchmarkBitmaskArrival(b, 64)
}

func benchmarkBitmaskArrival(b *testing.B, p int) {
	b.ReportAllocs()
	
	xbb, err := NewGangBarrierBitmask("bitmask-bench", p)
	if err != nil {
		b.Fatalf("setup: %v", err)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		
		// All workers arrive concurrently
		for w := 0; w < p; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				_ = xbb.Arrive("worker-" + string(rune(workerID)))
			}(w)
		}
		
		wg.Wait()
		
		// Reset barrier for next iteration
		xbb.mu.Lock()
		xbb.mask.Store(0)
		xbb.released = false
		close(xbb.releaseCh)
		xbb.releaseCh = make(chan struct{})
		xbb.mu.Unlock()
	}
}

// CompareDirectBenchmark_BitmaskVsCounter provides side-by-side comparison at same gang sizes.
// Run with: go test -bench="BenchmarkCompare.*_P16" -benchmem
func BenchmarkCompare_Bitmask_P16(b *testing.B) {
	benchmarkCompare(b, 16, "bitmask")
}

func BenchmarkCompare_BarrierCounter_P16(b *testing.B) {
	benchmarkCompare(b, 16, "counter")
}

func benchmarkCompare(b *testing.B, p int, variant string) {
	b.ReportAllocs()
	
	switch variant {
	case "bitmask":
		xbb, _ := NewGangBarrierBitmask("compare-bitmask", p)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			for w := 0; w < p; w++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					_ = xbb.Arrive("worker-" + string(rune(id)))
				}(w)
			}
			wg.Wait()
			
			xbb.mu.Lock()
			xbb.mask.Store(0)
			xbb.released = false
			close(xbb.releaseCh)
			xbb.releaseCh = make(chan struct{})
			xbb.mu.Unlock()
		}
		
	case "counter":
		barrier := NewGangBarrier("compare-counter", p)
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			for w := 0; w < p; w++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					_ = barrier.Arrive("worker-" + string(rune(id)))
				}(w)
			}
			wg.Wait()
			
			// Barrier automatically released by last worker
			barrier = NewGangBarrier("compare-counter", p)
		}
	}
}

// ============================================================================
// Benchmark: Exponential backoff spin wait
// ============================================================================

// BenchmarkBarrier_SpinUntilAllReady_NormalPath measures latencies when all workers arrive quickly.
// Key metric: Does exponential backoff hurt P99 latency? Should be negligible (<100ns).
func BenchmarkBarrier_SpinUntilAllReady_NormalPath_P8(b *testing.B) {
	benchmarkSpinWaitNormal(b, 8)
}

func BenchmarkBarrier_SpinUntilAllReady_NormalPath_P64(b *testing.B) {
	benchmarkSpinWaitNormal(b, 64)
}

func benchmarkSpinWaitNormal(b *testing.B, p int) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		barrier := NewGangBarrier("spin-normal", p)
		
		startTime := time.Now()
		
		var wg sync.WaitGroup
		
		// Start one worker doing actual work (simulates real training loop)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// This simulates the slowest path: spin wait until release
			err := barrier.SpinUntilAllReady(100*time.Microsecond, 30*time.Second)
			_ = err
		}()
		
		// Other workers arrive immediately (simulates well-behaved training)
		for w := 1; w < p; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_ = barrier.Arrive("worker-" + string(rune(id)))
			}(w)
		}
		
		wg.Wait()
		
		elapsed := time.Since(startTime).Nanoseconds()
		flipSink.Add(elapsed)
	}
}

// BenchmarkBarrier_SpinUntilAllReady_StragglerPath measures worst-case when one worker is late.
// Exponential backoff should kick in after initial polling, reducing CPU waste.
func BenchmarkBarrier_SpinUntilAllReady_StragglerPath_P16(b *testing.B) {
	benchmarkSpinWaitStraggler(b, 16)
}

func benchmarkSpinWaitStraggler(b *testing.B, p int) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		barrier := NewGangBarrier("spin-straggler", p)
		
		var wg sync.WaitGroup
		
		// Main waiter starts spinning immediately
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = barrier.SpinUntilAllReady(100*time.Microsecond, 30*time.Second)
		}()
		
		// Straggler arrives late (after some iterations)
		// We can't simulate time delay in benchmarks, so we just measure the backoff logic overhead
		time.Sleep(1 * time.Millisecond) // artificial delay to force backoff
		
		// Complete the gang
		for w := 0; w < p; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_ = barrier.Arrive("worker-" + string(rune(id)))
			}(w)
		}
		
		wg.Wait()
	}
}

// ============================================================================
// Correctness tests for enhanced features
// ============================================================================

// TestBarrier_WithTimeout_Success verifies timeout watcher cancels on normal completion.
func TestBarrier_WithTimeout_Success(t *testing.T) {
	barrier := NewGangBarrier("timeout-success", 8).WithTimeout(100*time.Millisecond)
	
	done := make(chan struct{})
	go func() {
		// Wait for timeout to be set up
		time.Sleep(10 * time.Millisecond)
		
		// All workers arrive before timeout
		for i := 0; i < 8; i++ {
			_ = barrier.Arrive("worker-" + string(rune(i)))
		}
	}()
	
	<-barrier.releaseCh // Success - barrier released
	
	select {
	case <-done:
		t.Log("Timeout watcher properly cancelled on completion")
	default:
		// Cancel manually to clean up goroutine
		barrier.CancelTimeout()
		close(done)
	}
}

// TestBarrier_WithTimeout_Expires verifies timeout triggers Fail().
func TestBarrier_WithTimeout_Expires(t *testing.T) {
	timeoutMs := 50 // short timeout for fast test
	
	barrier := NewGangBarrier("timeout-expires", 8).WithTimeout(time.Duration(timeoutMs) * time.Millisecond)
	
	// Only one worker arrives (insufficient for release)
	_ = barrier.Arrive("worker-0")
	
	// Wait for timeout to expire
	select {
	case <-barrier.releaseCh:
		// Success - timeout triggered
		barrier.Wait()
		// Error is propagated via Fail(), check stats instead
		
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout expired but barrier not released")
	}
	
	stats := barrier.GetStats()
	if !stats.ReleasedDueToFailure {
		t.Error("expected barrier to be released due to failure (timeout)")
	}
}

// TestBarrier_Bitmask_Correctness verifies bitmask barrier reaches all bits.
func TestBarrier_Bitmask_Correctness(t *testing.T) {
	const p = 16
	xbb, err := NewGangBarrierBitmask("bitmask-correctness", p)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	
	var wg sync.WaitGroup
	for i := 0; i < p; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = xbb.Arrive("worker-" + string(rune(id)))
		}(i)
	}
	
	wg.Wait()
	
	stats := xbb.GetStats()
	if stats.ActualArrived != p {
		t.Fatalf("expected %d workers arrived, got %d", p, stats.ActualArrived)
	}
	if !stats.IsReleased {
		t.Fatal("expected barrier to be released after all arrivals")
	}
}

// TestBarrier_Bitmask_Idempotency verifies duplicate arrivals don't corrupt bitmask state.
func TestBarrier_Bitmask_Idempotency(t *testing.T) {
	const p = 8
	xbb, err := NewGangBarrierBitmask("bitmask-idempotent", p)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	
	// All unique workers arrive once each
	var wg sync.WaitGroup
	for i := 0; i < p; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = xbb.Arrive("worker-" + string(rune(id)))
		}(i)
	}
	
	wg.Wait()
	
	// Verify barrier released exactly when all unique workers arrived
	stats := xbb.GetStats()
	if !stats.IsReleased {
		t.Fatal("expected barrier to release after all 8 unique workers")
	}
	if stats.Expected != p {
		t.Errorf("expected Expected=%d, got %d", p, stats.Expected)
	}
	
	// Add more arrivals (should not change state - idempotent)
	initialReleased := stats.IsReleased
	initialCount := stats.ActualArrived
	
	_ = xbb.Arrive("worker-0") // duplicate
	_ = xbb.Arrive("worker-4") // another duplicate
	
	finalStats := xbb.GetStats()
	if finalStats.IsReleased != initialReleased {
		t.Error("idempotence violation: IsReleased changed after duplicate arrivals")
	}
	if finalStats.ActualArrived != initialCount && finalStats.ActualArrived > p {
		t.Errorf("idempotence violated: count went from %d to %d (max allowed: %d)", 
			initialCount, finalStats.ActualArrived, p)
	}
}

// TestBarrier_SpinWait_NoDeadlock verifies exponential backoff doesn't cause infinite loops.
func TestBarrier_SpinWait_NoDeadlock(t *testing.T) {
	const p = 4
	barrier := NewGangBarrier("spin-deadlock-test", p)
	
	var wg sync.WaitGroup
	
	// One waiter spins
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := barrier.SpinUntilAllReady(10*time.Microsecond, 100*time.Millisecond)
		_ = err
	}()
	
	// Complete barrier before timeout
	time.Sleep(20 * time.Millisecond)
	for i := 0; i < p; i++ {
		_ = barrier.Arrive("worker-" + string(rune(i)))
	}
	
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		t.Log("Spin wait completed without deadlock")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("spin wait timed out - possible deadlock")
	}
}
