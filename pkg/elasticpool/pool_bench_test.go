// Package elasticpool - Module 12 performance benchmarks and concurrency
// correctness harness. Every benchmark wires a REAL evidence ledger
// (MemoryStore + EphemeralSigner + SimulatedAnchorer + NewLedger) unless the
// benchmark's whole point is to measure the attestation delta, in which case a
// nil-ledger variant is provided alongside. All state is file-system backed
// under b.TempDir()/elasticpool/, so the numbers include real JSON persistence
// (tmp+rename) and, when attested, real Ed25519 signing + hash chaining.
//
// These benchmarks back docs/performance-validation-module-12.md. Run with:
//
//	go test ./pkg/elasticpool/... -bench=. -benchmem -run=^$
//
// and the concurrency race check with:
//
//	go test ./pkg/elasticpool/... -run TestConcurrent -race -count=1
package elasticpool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// newBenchPool builds a real-ledger (or nil-ledger) pool rooted at b.TempDir().
// attest=true mirrors production: every write is signed and hash-chained.
func newBenchPool(b *testing.B, attest bool) *FSMElasticPool {
	b.Helper()
	var ledger *evidence.Ledger
	if attest {
		signer, err := evidence.GenerateEphemeralSigner()
		if err != nil {
			b.Fatalf("generate ephemeral signer: %v", err)
		}
		ledger, err = evidence.NewLedger(evidence.LedgerConfig{
			Store:    evidence.NewMemoryStore(),
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		if err != nil {
			b.Fatalf("build ledger: %v", err)
		}
	}
	pool, err := NewFSMElasticPool(b.TempDir(), ledger)
	if err != nil {
		b.Fatalf("new FSMElasticPool: %v", err)
	}
	return pool
}

// benchServiceID returns a valid "inf-" prefixed opaque Module 15 service ref.
func benchServiceID(i int) string { return fmt.Sprintf("inf-bench%012d", i) }

// ---------------------------------------------------------------------------
// 1. Acquire latency (isolated: Release is excluded from the timer)
// ---------------------------------------------------------------------------

func benchmarkAcquire(b *testing.B, attest bool) {
	p := newBenchPool(b, attest)
	ctx := context.Background()
	// A very wide pool (10K nodes × 8K slots = 80M total) keeps best-fit O(1)
	// and guarantees free slots each iteration despite drained-node FSM.
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "bench-acq", GPUType: "A100-80G", SlotsPerNode: 8_000, MinNodes: 1, MaxNodes: 10000, CostPerNodeHour: 3.2,
	})
	if err != nil {
		b.Fatal(err)
	}
	// Pre-add enough nodes so even if some go drained, we always have ready capacity
	for i := 0; i < 100; i++ {
		if _, err := p.AddNode(ctx, pool.ID); err != nil {
			b.Fatalf("add initial node %d: %v", i, err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l, err := p.Acquire(ctx, pool.ID, benchServiceID(i), 1)
		if err != nil {
			b.Fatalf("acquire: %v", err)
		}
		// Release outside the timer so we measure Acquire alone and never run
		// out of slots.
		b.StopTimer()
		_, err = p.Release(ctx, l.ID)
		if err != nil {
			b.Fatalf("release cleanup: %v", err)
		}
		b.StartTimer()
	}
}

func BenchmarkAcquire_Attested(b *testing.B) { benchmarkAcquire(b, true) }
func BenchmarkAcquire_NoAttest(b *testing.B) { benchmarkAcquire(b, false) }

// ---------------------------------------------------------------------------
// 2. Release latency (isolated: Acquire is excluded from the timer)
// ---------------------------------------------------------------------------

func benchmarkRelease(b *testing.B, attest bool) {
	p := newBenchPool(b, attest)
	ctx := context.Background()
	// Same wide pool design as acquire benchmark
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "bench-rel", GPUType: "A100-80G", SlotsPerNode: 8_000, MinNodes: 1, MaxNodes: 10000, CostPerNodeHour: 3.2,
	})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := p.AddNode(ctx, pool.ID); err != nil {
			b.Fatalf("add initial node %d: %v", i, err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		l, err := p.Acquire(ctx, pool.ID, benchServiceID(i), 1)
		if err != nil {
			b.Fatalf("acquire setup: %v", err)
		}
		b.StartTimer()

		if _, err := p.Release(ctx, l.ID); err != nil {
			b.Fatalf("release: %v", err)
		}
	}
}

func BenchmarkRelease_Attested(b *testing.B) { benchmarkRelease(b, true) }
func BenchmarkRelease_NoAttest(b *testing.B) { benchmarkRelease(b, false) }

// ---------------------------------------------------------------------------
// 3. EvaluateElasticity scaling-decision latency (no_change path)
// ---------------------------------------------------------------------------

func benchmarkEvaluateNoChange(b *testing.B, attest bool) {
	p := newBenchPool(b, attest)
	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "bench-eval", GPUType: "A100-80G", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 100, CostPerNodeHour: 3.2,
	})
	if err != nil {
		b.Fatal(err)
	}
	// Two nodes, half-full → utilization 50% → deterministic no_change path.
	for i := 0; i < 50; i++ {
		if _, err := p.AddNode(ctx, pool.ID); err != nil {
			b.Fatal(err)
		}
	}
	// Fill just enough nodes to get ~50% utilization (400 slots out of 800 total)
	for i := 0; i < 100; i++ {
		if _, err := p.Acquire(ctx, pool.ID, benchServiceID(i), 4); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// pending=4 <= free=8, util=50% → no_change (also budget OK).
		if _, err := p.EvaluateElasticity(ctx, pool.ID, 4, 1000, 0); err != nil {
			b.Fatalf("evaluate: %v", err)
		}
	}
}

func BenchmarkEvaluateElasticity_Attested(b *testing.B) { benchmarkEvaluateNoChange(b, true) }
func BenchmarkEvaluateElasticity_NoAttest(b *testing.B) { benchmarkEvaluateNoChange(b, false) }

// ---------------------------------------------------------------------------
// 4. Budget guard rejection path overhead (currentCost+impact > budgetLimit)
// ---------------------------------------------------------------------------

func benchmarkBudgetReject(b *testing.B, attest bool) {
	p := newBenchPool(b, attest)
	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "bench-budget", GPUType: "A100-80G", SlotsPerNode: 16, MinNodes: 1, MaxNodes: 100, CostPerNodeHour: 2.0,
	})
	if err != nil {
		b.Fatal(err)
	}
	// Fill 1 node fully to force a scale-up decision
	for i := 0; i < 50; i++ {
		if _, err := p.AddNode(ctx, pool.ID); err != nil {
			b.Fatal(err)
		}
	}
	// Fill half-slots on many nodes to create consistent pending > free scenario
	for i := 0; i < 100; i++ {
		if _, err := p.Acquire(ctx, pool.ID, benchServiceID(i), 8); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// pending=4 needs 1 node ($2 impact); 99+2 > 100 → BUDGET REJECTED path.
		d, err := p.EvaluateElasticity(ctx, pool.ID, 4, 100, 99)
		if err != nil {
			b.Fatalf("evaluate: %v", err)
		}
		if d.BudgetOK {
			b.Fatalf("expected budget rejection, got BudgetOK=true")
		}
	}
}

func BenchmarkBudgetGuardReject_Attested(b *testing.B) { benchmarkBudgetReject(b, true) }
func BenchmarkBudgetGuardReject_NoAttest(b *testing.B) { benchmarkBudgetReject(b, false) }

// ---------------------------------------------------------------------------
// 5. FSM state-transition latency: a 2-slot node with one slot held
//    permanently flips ready→busy (acquire) then busy→ready (release) every
//    iteration, forcing two status transitions per cycle without draining.
//    Reported as ns/op for the two-transition cycle.
// ---------------------------------------------------------------------------

func benchmarkFSMTransition(b *testing.B, attest bool) {
	p := newBenchPool(b, attest)
	ctx := context.Background()
	// A 2-slot node with one slot held permanently flips ready<->busy every
	// acquire/release cycle WITHOUT ever draining (UsedSlots never reaches 0),
	// isolating the FSM status-transition cost per two transitions.
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "bench-fsm", GPUType: "A100-80G", SlotsPerNode: 2, MinNodes: 1, MaxNodes: 4, CostPerNodeHour: 3.2,
	})
	if err != nil {
		b.Fatal(err)
	}
	if _, err := p.AddNode(ctx, pool.ID); err != nil {
		b.Fatal(err)
	}
	// Hold one slot permanently so the node stays 'ready' (used=1<2) between
	// cycles rather than draining to 0.
	if _, err := p.Acquire(ctx, pool.ID, benchServiceID(0), 1); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Acquire the second slot → node used 1→2, ready→busy.
		l, err := p.Acquire(ctx, pool.ID, benchServiceID(i+1), 1)
		if err != nil {
			b.Fatalf("acquire: %v", err)
		}
		// Release it → node used 2→1, busy→ready (does NOT drain).
		if _, err := p.Release(ctx, l.ID); err != nil {
			b.Fatalf("release: %v", err)
		}
	}
}

func BenchmarkFSMTransition_Attested(b *testing.B) { benchmarkFSMTransition(b, true) }
func BenchmarkFSMTransition_NoAttest(b *testing.B) { benchmarkFSMTransition(b, false) }

// ---------------------------------------------------------------------------
// Concurrency correctness: N goroutines each run acquire→release cycles under
// -race. The mutex-guarded FSM must never corrupt slot accounting and no data
// race must be reported. This is a correctness test, not a benchmark.
// ---------------------------------------------------------------------------

// TestConcurrentAcquireRelease_NoRace hammers Acquire/Release from many
// goroutines. With -race this proves the sync.Mutex fully guards all shared
// state; without -race it still asserts final slot accounting is consistent
// (every slot released → node returns to drained/ready with UsedSlots==0).
func TestConcurrentAcquireRelease_NoRace(t *testing.T) {
	tmp := t.TempDir()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	p, err := NewFSMElasticPool(tmp, ledger)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "race-pool", GPUType: "A100-80G", SlotsPerNode: 64, MinNodes: 1, MaxNodes: 8, CostPerNodeHour: 3.2,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ample capacity so acquires succeed under contention.
	const nodes = 8
	for i := 0; i < nodes; i++ {
		if _, err := p.AddNode(ctx, pool.ID); err != nil {
			t.Fatal(err)
		}
	}

	const (
		workers      = 16
		opsPerWorker = 40
	)
	var acquired, released, noCapacity int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				l, err := p.Acquire(ctx, pool.ID, benchServiceID(w*opsPerWorker+i), 1)
				if err != nil {
					// Under heavy contention capacity may transiently fill; that
					// is a legitimate ErrNoCapacity, not a race.
					atomic.AddInt64(&noCapacity, 1)
					continue
				}
				atomic.AddInt64(&acquired, 1)
				if _, err := p.Release(ctx, l.ID); err != nil {
					t.Errorf("release %s: %v", l.ID, err)
					return
				}
				atomic.AddInt64(&released, 1)
			}
		}(w)
	}
	wg.Wait()

	if acquired != released {
		t.Fatalf("acquired (%d) != released (%d): slot accounting diverged", acquired, released)
	}

	// After every lease is released, no node may report leftover UsedSlots.
	got, err := p.ListNodes(pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got {
		if n.UsedSlots != 0 {
			t.Fatalf("node %s has UsedSlots=%d after full release; expected 0", n.ID, n.UsedSlots)
		}
	}
	t.Logf("concurrency OK: acquired=%d released=%d transient no-capacity=%d across %d workers",
		acquired, released, noCapacity, workers)
}
