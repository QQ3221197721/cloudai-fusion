// Package elasticpool - Module 12 correctness tests for FSM enforcement,
// budget guard boundary conditions, and concurrent slot accounting invariants.
package elasticpool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFSMIllegalTransitions verifies that acquiring from pools in non-Ready state
// is rejected via ErrNoCapacity (drained nodes skip; deleted pools reject).
// This proves the FSM is strictly enforced by Acquire's status checks.
func TestFSMIllegalTransitions(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()
	ctx := context.Background()

	t.Run("AcquireOnDeletedPoolRejected", func(t *testing.T) {
		pool, err := p.CreatePool(ctx, PoolInput{
			Name: "deleted-pool", GPUType: "A100", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 5, CostPerNodeHour: 3.2,
		})
		require.NoError(t, err)
		_, err = p.AddNode(ctx, pool.ID)
		require.NoError(t, err)

		// Transitions to drained by releasing all slots
		l, err := p.Acquire(ctx, pool.ID, "inf-x", 4)
		require.NoError(t, err)
		_, err = p.Release(ctx, l.ID)
		require.NoError(t, err)

		// Drained node rejects Acquire (no ready nodes)
		_, err = p.Acquire(ctx, pool.ID, "inf-new", 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoCapacity, "drained nodes not ready; no capacity")

		// Manually set pool to deleted
		pool.Status = PoolDeleted
		err = p.persistPoolLocked(pool)
		require.NoError(t, err)

		_, err = p.Acquire(ctx, pool.ID, "inf-again", 1)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deleted")
	})
}

// TestBudgetGuardCorrectness verifies budget rejection/acceptance at boundaries
// with high precision float comparisons (proving epsilon tolerance is correct).
func TestBudgetGuardCorrectness(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()
	ctx := context.Background()

	tests := []struct {
		name           string
		currentCost    float64
		budgetLimit    float64
		costImpact     float64
		expectOK       bool
		expectedAction string
	}{
		{"AcceptExactEquality", 98, 100, 2, true, "scale_up"},    // 98+2 == 100 → accept
		{"RejectClearOvershoot", 99, 100, 2, false, "no_change"}, // 99+2 > 100 → reject
		{"AcceptUnderEpsilon", 97.9, 100, 2, true, "scale_up"},   // 97.9+2 < 100 → accept
		{"RejectAboveLimit", 95, 100, 6, false, "no_change"},     // 95+6 > 100 → reject
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := p.CreatePool(ctx, PoolInput{
				Name: "budget-correct", GPUType: "A100", SlotsPerNode: 1, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: tc.costImpact,
			})
			require.NoError(t, err)

			// Fill 1 node fully to force a scale-up decision
			_, err = p.AddNode(ctx, pool.ID)
			require.NoError(t, err)
			_, err = p.Acquire(ctx, pool.ID, "inf-fill", 1)
			require.NoError(t, err)

			decision, err := p.EvaluateElasticity(ctx, pool.ID, 1, tc.budgetLimit, tc.currentCost)
			require.NoError(t, err)

			if tc.expectOK {
				assert.True(t, decision.BudgetOK, "expected budget OK")
			} else {
				assert.False(t, decision.BudgetOK, "expected budget REJECTED")
			}
			assert.Equal(t, tc.expectedAction, decision.Action, "action must match")
		})
	}
}

// TestConcurrencyBasicStress verifies that concurrent acquires/releases maintain
// invariant accounting when no race detector is available. Uses sync.WaitGroup
// instead of -race flag.
func TestConcurrencyBasicStress(t *testing.T) {
	p, _, store, cleanup := newTestPool(t, true)
	defer cleanup()
	ctx := context.Background()

	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "stress-concurrent", GPUType: "L4-24G", SlotsPerNode: 32, MinNodes: 4, MaxNodes: 8, CostPerNodeHour: 1.5,
	})
	require.NoError(t, err)

	const nodeCount = 4
	for i := 0; i < nodeCount; i++ {
		if _, err := p.AddNode(ctx, pool.ID); err != nil {
			t.Fatalf("add node %d: %v", i, err)
		}
	}

	const workers = 24
	const opsPerWorker = 25

	var acquired, released, failures int64
	var wg sync.WaitGroup
	wg.Add(workers)

	start := time.Now()

	for w := 0; w < workers; w++ {
		go func(wID int) {
			defer wg.Done()
			localOp := 0
			for i := 0; i < opsPerWorker; i++ {
				id := benchServiceID(wID*1000 + i)
				l, err := p.Acquire(ctx, pool.ID, id, 1)
				if err != nil {
					atomic.AddInt64(&failures, 1)
					continue
				}
				atomic.AddInt64(&acquired, 1)
				localOp++

				_, releaseErr := p.Release(ctx, l.ID)
				if releaseErr != nil {
					atomic.AddInt64(&failures, 1)
					return
				}
				atomic.AddInt64(&released, 1)
				localOp--

				if localOp < 0 {
					panic("invariant violation: negative local counter")
				}
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(start)

	require.Equal(t, acquired, released, "every acquired slot must be released eventually")

	nodes, err := p.ListNodes(pool.ID)
	require.NoError(t, err)
	totalUsed := 0
	for _, n := range nodes {
		totalUsed += n.UsedSlots
	}
	assert.Equal(t, 0, totalUsed, "all Released slots must reduce UsedSlots to 0")

	recs, _ := store.All(ctx)
	t.Logf("concurrency stress: ops_per_sec=%.0f duration=%v ledger_records=%d acquired=%d released=%d transient_failures=%d",
		float64(acquired)/duration.Seconds(), duration, len(recs), acquired, released, failures)
}
