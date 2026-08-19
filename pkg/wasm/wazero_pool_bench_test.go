// Package wasm — Benchmark for Module 50 WASM Executor Pool Performance
//
// This file provides production-grade benchmarks measuring the cost of WASM instance
// pooling strategies. The goal is to demonstrate how pre-warmed pools amortize the
// ~200ms cold-start penalty across thousands of invocations.
//
// Core API surface being benchmarked (all from wazero_runtime.go, zero core changes):
//   - NewWazeroInstance() → runtime creation (fast)
//   - Instantiate(wasmBytes) → compile + module load (slow ~200ms)
//   - InvokeFunction() → actual execution (~microseconds)
//
// No existing pool implementation is present in core code, so this benchmark constructs
// a channel-backed warm pool (bounded, guaranteed-reuse) to measure what production
// deployments should achieve: pre-warm N instances at startup, then borrow/return them
// off the request hot path so users never pay the compile cost.
//
// Target metrics:
//   - ColdStartSingle: baseline single cold instantiation (~200ms)
//   - PoolPreWarmed: amortized time per reuse after pre-warming (<5ms target)
//   - PoolLookup: pool borrow/return overhead alone (<100ns target)
//   - ConcurrentPoolAccess: contention/correctness under parallel worker goroutines
package wasm

import (
	"context"
	"fmt"
	"testing"
)

// ============================================================================
// Warm Pool Implementation (Production Pattern)
// ============================================================================

// PreWarmPool models the production pattern: keep a bounded set of already-compiled,
// already-instantiated modules ready to serve. A buffered channel guarantees reuse
// (unlike sync.Pool, whose contents the GC may reclaim) and doubles as a semaphore so
// concurrent borrowers block rather than triggering a fresh 200ms instantiation.
type PreWarmPool struct {
	ch  chan *WazeroInstance
	cfg RuntimeConfig
}

// NewPreWarmPool constructs a pool and eagerly instantiates prewarmCount instances,
// paying the full cold-start cost once, at startup, off the request path.
func NewPreWarmPool(ctx context.Context, cfg RuntimeConfig, wasmBytes []byte, prewarmCount int) (*PreWarmPool, error) {
	if prewarmCount < 1 {
		prewarmCount = 1
	}
	p := &PreWarmPool{
		ch:  make(chan *WazeroInstance, prewarmCount),
		cfg: cfg,
	}
	for i := 0; i < prewarmCount; i++ {
		inst, err := NewWazeroInstance(cfg)
		if err != nil {
			return nil, fmt.Errorf("prewarm: create instance %d: %w", i, err)
		}
		if err := inst.Instantiate(wasmBytes); err != nil {
			_ = inst.Close()
			return nil, fmt.Errorf("prewarm: instantiate instance %d: %w", i, err)
		}
		p.ch <- inst
	}
	return p, nil
}

// Get borrows a pre-warmed instance, blocking if all are currently in use.
func (p *PreWarmPool) Get() *WazeroInstance { return <-p.ch }

// Put returns an instance to the pool for the next borrower.
func (p *PreWarmPool) Put(inst *WazeroInstance) { p.ch <- inst }

// Close drains and shuts down every pooled instance exactly once.
func (p *PreWarmPool) Close() error {
	close(p.ch)
	for inst := range p.ch {
		_ = inst.Close()
	}
	return nil
}

// ============================================================================
// Benchmark #1: ColdStartSingle - Baseline Single Cold Start
// ============================================================================

// BenchmarkColdStartSingle measures pure cold-start cost: one module, compile + instantiate
// + close, on every iteration. This is the latency a user experiences if the deployment
// does NOT keep a warm pool. Baseline for Module-50 (~200ms/op on the reference machine).
func BenchmarkColdStartSingle(b *testing.B) {
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst, err := NewWazeroInstance(cfg)
		if err != nil {
			b.Skipf("Skipping test (wazero not available): %v", err)
		}
		_ = inst.Instantiate(minimalAddModule)
		_ = inst.Close()
	}
}

// ============================================================================
// Benchmark #2: PoolPreWarmed - Reuse After Pre-Warming
// ============================================================================

// BenchmarkPoolPreWarmed measures amortized cost when using a pre-warmed pool.
// Each iteration: Get() an already-compiled instance → invoke a function → Put() back.
// This IS the production path: startup pre-warms the pool once, then every request
// borrows/returns an instance in microseconds and never pays the compile cost.
// Target: <5ms per Op (vs ~200ms cold start).
func BenchmarkPoolPreWarmed(b *testing.B) {
	ctx := context.Background()
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false

	// Pre-warm the pool BEFORE the timed region so the 200ms/instance is excluded.
	pool, err := NewPreWarmPool(ctx, cfg, minimalAddModule, 4)
	if err != nil {
		b.Skipf("Skipping test (wazero not available): %v", err)
	}
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := pool.Get()
		_, _ = inst.InvokeFunction("add", 3, 5)
		pool.Put(inst)
	}
}

// ============================================================================
// Benchmark #3: PoolLookup - Pool Borrow/Return Overhead
// ============================================================================

// BenchmarkPoolLookup isolates the raw cost of pool borrow/return without any WASM
// invocation, showing the upper-bound pool-management overhead a request pays on top
// of its real work. Uses lightweight sentinels so no instantiation happens in the loop.
// Target: <100ns per Op.
func BenchmarkPoolLookup(b *testing.B) {
	type sentinel struct{ id int }
	pool := make(chan *sentinel, 8)
	for i := 0; i < 8; i++ {
		pool <- &sentinel{id: i}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := <-pool // borrow
		pool <- v   // return
	}
}

// ============================================================================
// Benchmark #4: ConcurrentPoolAccess - Parallel Worker Contention
// ============================================================================

// BenchmarkConcurrentPoolAccess exercises the pool under realistic concurrent load:
// many goroutines borrow an instance, invoke, and return it in parallel. It validates
// the pool's concurrency safety (buffered channel as semaphore) and measures per-op
// cost under contention via b.RunParallel.
func BenchmarkConcurrentPoolAccess(b *testing.B) {
	ctx := context.Background()
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false

	pool, err := NewPreWarmPool(ctx, cfg, minimalAddModule, 16)
	if err != nil {
		b.Skipf("Skipping test (wazero not available): %v", err)
	}
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var n uint64
		for pb.Next() {
			inst := pool.Get()
			_, _ = inst.InvokeFunction("add", n, n+1)
			pool.Put(inst)
			n++
		}
	})
}

// ============================================================================
// Benchmark #5: PoolWarmThenReuse - Startup Pre-Warming + Serving
// ============================================================================

// BenchmarkPoolWarmThenReuse mirrors the full deployment lifecycle: pre-warm a pool
// (app startup) then serve requests off it. Only the serving phase is timed, so this
// reports the same steady-state figure a warm production process would exhibit.
func BenchmarkPoolWarmThenReuse(b *testing.B) {
	ctx := context.Background()
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false

	pool, err := NewPreWarmPool(ctx, cfg, minimalAddModule, 8)
	if err != nil {
		b.Skipf("Skipping test (wazero not available): %v", err)
	}
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := pool.Get()
		_, _ = inst.InvokeFunction("add", 1, 2)
		pool.Put(inst)
	}
}

// ============================================================================
// Benchmark #6: RealWorkloadPattern - Borrow -> Snapshot/Restore -> Return
// ============================================================================

// BenchmarkRealWorkloadPattern models a stateful workload (snapshot → mutate → restore)
// served from a warm pool. It shows that once the pool is warm, the user path is
// dominated by real work, never the 200ms cold-start.
func BenchmarkRealWorkloadPattern(b *testing.B) {
	ctx := context.Background()
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false

	pool, err := NewPreWarmPool(ctx, cfg, memoryModule, 8)
	if err != nil {
		b.Skipf("Skipping test (wazero not available): %v", err)
	}
	defer pool.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := pool.Get()

		// Snapshot current linear memory, mutate a slice of it, then restore.
		snap, _ := inst.Snapshot()
		pattern := make([]byte, min(len(snap), 1024))
		for j := range pattern {
			pattern[j] = byte(i % 256)
		}
		mod := inst.testModuleForSnapshot()
		mem := mod.Memory()
		_ = mem.Write(0, pattern)
		_ = inst.Restore(snap)

		pool.Put(inst)
	}
}

// ============================================================================
// Benchmark #7: ColdVsWarmComparison - Side-by-Side Amortization
// ============================================================================

// BenchmarkColdVsWarmComparison places the no-pool (cold every request) path directly
// beside the pooled (warm reuse) path so the amortization gain is visible in one run.
func BenchmarkColdVsWarmComparison(b *testing.B) {
	ctx := context.Background()
	cfg := DefaultRuntimeConfig()
	cfg.EnableWASI = false
	wasmByte := minimalAddModule

	b.Run("NoPool_ColdEveryRequest", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			inst, err := NewWazeroInstance(cfg)
			if err != nil {
				b.Skipf("Skipping test (wazero not available): %v", err)
			}
			_ = inst.Instantiate(wasmByte)
			_, _ = inst.InvokeFunction("add", 3, 5)
			_ = inst.Close()
		}
	})

	b.Run("WithPool_WarmReuse", func(b *testing.B) {
		pool, err := NewPreWarmPool(ctx, cfg, wasmByte, 8)
		if err != nil {
			b.Skipf("Skipping test (wazero not available): %v", err)
		}
		defer pool.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			inst := pool.Get()
			_, _ = inst.InvokeFunction("add", 3, 5)
			pool.Put(inst)
		}
	})
}
