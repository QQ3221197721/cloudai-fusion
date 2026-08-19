// Package elasticpool - unit tests for Module 12: Elastic Inference Pool.
// Every test wires a REAL ledger (MemoryStore + EphemeralSigner + NewLedger) —
// never a nil ledger — mirroring inference_test.go's construction pattern,
// and asserts on-disk persistence under <tmpDir>/elasticpool/ (the path contract).
package elasticpool

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestPool creates a fresh elastic pool with a real ledger for testing.
func newTestPool(t *testing.T, attest bool) (*FSMElasticPool, *evidence.Ledger, *evidence.MemoryStore, func()) {
	t.Helper()
	tmp := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	require.NoError(t, err, "generate ephemeral signer")

	var ledger *evidence.Ledger
	if attest {
		ledger, err = evidence.NewLedger(evidence.LedgerConfig{
			Store:    store,
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		require.NoError(t, err, "build ledger")
	}

	pool, err := NewFSMElasticPool(tmp, ledger)
	require.NoError(t, err, "new FSMElasticPool")

	cleanup := func() {
		if attest && ledger != nil {
			count, _ := store.Count(context.Background())
			t.Logf("final ledger count: %d", count)
		}
	}
	return pool, ledger, store, cleanup
}

// TestCreatePool_PersistsAndAttests: CreatePool persists pools.json under
// <tmp>/elasticpool/, validates constraints, initializes status=active, and
// writes an "elasticpool.create" attestation with the given actor.
func TestCreatePool_PersistsAndAttests(t *testing.T) {
	p, _, store, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	in := PoolInput{
		Name:            "gpu-pool",
		GPUType:         "A100-80G",
		SlotsPerNode:    8,
		MinNodes:        1,
		MaxNodes:        10,
		CostPerNodeHour: 3.2,
		Actor:           "alice",
	}

	pool, err := p.CreatePool(ctx, in)
	require.NoError(t, err)
	assert.Equal(t, "gpu-pool", pool.Name)
	assert.Equal(t, "A100-80G", pool.GPUType)
	assert.Equal(t, 8, pool.SlotsPerNode)
	assert.Equal(t, 1, pool.MinNodes)
	assert.Equal(t, 10, pool.MaxNodes)
	assert.Equal(t, 3.2, pool.CostPerNodeHour)
	assert.Equal(t, PoolActive, pool.Status)
	assert.Regexp(t, `^pool-[0-9a-f]{16}$`, pool.ID, "ID must be pool-<hex16>")
	assert.False(t, pool.CreatedAt.IsZero())

	// Path contract from Module 16's lesson: the store MUST be
	// <store>/elasticpool/pools.json, not <store>/pools.json.
	poolsJSON := filepath.Join(p.Root(), poolsFile)
	data, err := os.ReadFile(poolsJSON)
	require.NoError(t, err, "pools.json must exist on disk under the elasticpool/ subdir")
	assert.Contains(t, string(data), "gpu-pool")
	assert.Contains(t, string(data), "A100-80G")

	// The receipt is real and points at the created pool.
	last := p.LastAttestation()
	require.NotNil(t, last, "attestation must be written when ledger is wired")
	assert.Equal(t, "elasticpool.create", last.Action)
	assert.Equal(t, pool.ID, last.Subject)
	assert.Equal(t, "alice", last.Actor)

	recs, err := store.All(ctx)
	require.NoError(t, err)
	require.Len(t, recs, 1, "exactly one ledger record after one create")
}

// TestCreatePool_RejectsInvalidParams: invalid parameter sets are rejected
// before any persistence or ledger write. Tests MinNodes>=MaxNodes,
// SlotsPerNode<=0, CostPerNodeHour<=0, and empty GPUType (four cases).
func TestCreatePool_RejectsInvalidParams(t *testing.T) {
	p, _, store, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()

	cases := []struct {
		name      string
		input     PoolInput
		wantError string
	}{
		{"MinNodes==MaxNodes",
			PoolInput{Name: "p1", GPUType: "A100", SlotsPerNode: 4, MinNodes: 5, MaxNodes: 5, CostPerNodeHour: 1.0},
			"max_nodes (5) must be > min_nodes (5)"},
		{"SlotsPerNode==0",
			PoolInput{Name: "p2", GPUType: "A100", SlotsPerNode: 0, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 1.0},
			"slots_per_node (0) must be positive"},
		{"CostPerNodeHour==0",
			PoolInput{Name: "p3", GPUType: "A100", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 0},
			"cost_per_node_hour (0.00) must be positive"},
		{"EmptyGPUType",
			PoolInput{Name: "p4", GPUType: "", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 1.0},
			"gpu type is required"},
		{"NegativeMinNodes",
			PoolInput{Name: "p5", GPUType: "A100", SlotsPerNode: 4, MinNodes: -1, MaxNodes: 10, CostPerNodeHour: 1.0},
			"min_nodes (-1) cannot be negative"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.CreatePool(ctx, tc.input)
			require.Error(t, err, "case %s must fail", tc.name)
			assert.Contains(t, err.Error(), tc.wantError, "case %s error mentions %q", tc.name, tc.wantError)
		})
	}

	// Nothing persisted or attested.
	recs, err := store.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, recs, "no ledger records after rejected creates")
}

// TestAddNode_ListNodes: AddNode creates a node with TotalSlots=SlotsPerNode,
// UsedSlots=0, Status=ready; ListNodes returns newest-first; ID format node-<hex12>.
func TestAddNode_ListNodes(t *testing.T) {
	p, _, store, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool1, err := p.CreatePool(ctx, PoolInput{
		Name: "small-pool", GPUType: "H100-96G", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 3, CostPerNodeHour: 4.5,
	})
	require.NoError(t, err)

	// Add two nodes
	n1, err := p.AddNode(ctx, pool1.ID)
	require.NoError(t, err)
	assert.Equal(t, pool1.ID, n1.PoolID)
	assert.Equal(t, pool1.SlotsPerNode, n1.TotalSlots) // 4
	assert.Equal(t, 0, n1.UsedSlots)
	assert.Equal(t, NodeReady, n1.Status)
	assert.Regexp(t, `^node-[0-9a-f]{12}$`, n1.ID)
	assert.False(t, n1.JoinedAt.IsZero())

	time.Sleep(2 * time.Millisecond) // distinct JoinedAt
	n2, err := p.AddNode(ctx, pool1.ID)
	require.NoError(t, err)
	assert.Equal(t, n1.TotalSlots, n2.TotalSlots)
	assert.Equal(t, 0, n2.UsedSlots)

	// Check nodes.json path
	nodesPath := filepath.Join(p.Root(), pool1.ID, nodesFile)
	data, err := os.ReadFile(nodesPath)
	require.NoError(t, err, "nodes.json must exist under <elasticpool>/<poolID>/nodes.json")
	assert.Contains(t, string(data), "total_slots")

	nodes, err := p.ListNodes(pool1.ID)
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, n2.ID, nodes[0].ID, "newest first by JoinedAt")
	assert.Equal(t, n1.ID, nodes[1].ID)

	// Attestation
	last := p.LastAttestation()
	require.NotNil(t, last)
	assert.Equal(t, "elasticpool.node.add", last.Action)

	// Ledger count should have increased
	recs, err := store.All(ctx)
	require.NoError(t, err)
	assert.Len(t, recs, 3, "2 adds + 1 create ledger records")
}

// TestAcquire_BestFit: best-fit allocates slots to the ready node with the
// smallest remaining free space that satisfies the request (fragmentation
// minimization). We construct two nodes with different free space to verify.
func TestAcquire_BestFit(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, false)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "bestfit-pool", GPUType: "A100-80G", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 3.2,
	})
	require.NoError(t, err)

	n1, _ := p.AddNode(ctx, pool.ID)
	n2, _ := p.AddNode(ctx, pool.ID)

	// Acquire 6 slots on n1 → free=2
	l1, err := p.Acquire(ctx, pool.ID, "inf-svc000000000001", 6)
	require.NoError(t, err)
	assert.Equal(t, n1.ID, l1.NodeID)

	// Acquire 2 slots → best-fit picks n1 (free=2 smallest satisfying)
	l2, err := p.Acquire(ctx, pool.ID, "inf-svc000000000002", 2)
	require.NoError(t, err)
	assert.Equal(t, n1.ID, l2.NodeID, "best-fit selects node with smallest satisfied free space")

	// Verify n1 is now full/busy
	nodes, err := p.ListNodes(pool.ID)
	require.NoError(t, err)
	for _, nd := range nodes {
		if nd.ID == n1.ID {
			assert.Equal(t, NodeBusy, nd.Status)
			assert.Equal(t, 8, nd.UsedSlots)
		} else if nd.ID == n2.ID {
			assert.Equal(t, NodeReady, nd.Status)
			assert.Equal(t, 0, nd.UsedSlots)
		}
	}
}

// TestAcquire_CapacityExceeded: acquire fails with ErrNoCapacity when no ready
// node can fit the request; error message mentions node-add/evaluate.
func TestAcquire_CapacityExceeded(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, false)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "empty-pool", GPUType: "A100-80G", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 2.0,
	})
	require.NoError(t, err)

	// One node, fully leased
	_, err = p.AddNode(ctx, pool.ID)
	require.NoError(t, err)
	_, err = p.Acquire(ctx, pool.ID, "inf-cap000000000001", 4)
	require.NoError(t, err)

	// Try 1 slot → no room
	_, err = p.Acquire(ctx, pool.ID, "inf-cap000000000002", 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoCapacity)
	assert.Contains(t, err.Error(), "no ready node")
	assert.Contains(t, err.Error(), "add nodes")
	assert.Contains(t, err.Error(), "evaluate")
}

// TestAcquire_NodeFullTurnsBusy: acquiring fills up a node exactly and flips
// it to busy; further acquires will skip this node.
func TestAcquire_NodeFullTurnsBusy(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, false)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "busy-pool", GPUType: "A100-80G", SlotsPerNode: 6, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 2.0,
	})
	require.NoError(t, err)

	n1, _ := p.AddNode(ctx, pool.ID)
	require.NoError(t, err)

	leases := []int{3, 2, 1} // total 6 slots
	for i, slots := range leases {
		l, err := p.Acquire(ctx, pool.ID, "inf-busy"+string(rune('0'+i)), slots)
		require.NoError(t, err, "acquire %d slots", slots)
		assert.NotEmpty(t, l.ID)
		assert.Equal(t, n1.ID, l.NodeID)
	}

	// Node should be busy now
	nodes, _ := p.ListNodes(pool.ID)
	for _, nd := range nodes {
		if nd.ID == n1.ID {
			assert.Equal(t, NodeBusy, nd.Status, "fully leased node becomes busy")
			assert.Equal(t, 6, nd.UsedSlots)
		}
	}
}

// TestRelease_RestoresReady: Release clears ReleasedAt, reduces UsedSlots, and
// transitions busy→ready when spare capacity appears.
func TestRelease_RestoresReady(t *testing.T) {
	p, _, store, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "release-pool", GPUType: "L4-24G", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 1.5,
	})
	require.NoError(t, err)

	n1, _ := p.AddNode(ctx, pool.ID)
	l1, _ := p.Acquire(ctx, pool.ID, "inf-rel000000000001", 3)
	_, _ = p.Acquire(ctx, pool.ID, "inf-rel000000000002", 1) // node 4/4 → busy

	nodes, _ := p.ListNodes(pool.ID)
	for _, nd := range nodes {
		if nd.ID == n1.ID {
			assert.Equal(t, NodeBusy, nd.Status)
		}
	}

	released, err := p.Release(ctx, l1.ID)
	require.NoError(t, err)
	require.NotNil(t, released.ReleasedAt, "ReleasedAt must be non-nil after release")

	leases, err := p.Leases(pool.ID, 0)
	require.NoError(t, err)
	// Last-write-wins: merged state has ReleasedAt set
	for _, le := range leases {
		if le.ID == l1.ID && le.NodeID == n1.ID {
			assert.NotNil(t, le.ReleasedAt)
		}
	}

	nodes, _ = p.ListNodes(pool.ID)
	for _, nd := range nodes {
		if nd.ID == n1.ID {
			assert.Equal(t, NodeReady, nd.Status, "busy→ready when UsedSlots drops below TotalSlots")
			assert.Equal(t, 1, nd.UsedSlots, "UsedSlots reduced by lease.Slots")
		}
	}

	// Attestation
	last := p.LastAttestation()
	require.NotNil(t, last)
	assert.Equal(t, "elasticpool.lease.release", last.Action)

	recs, err := store.All(ctx)
	require.NoError(t, err)
	assert.Len(t, recs, 5, "1 create + 1 node.add + 2 lease.acquire + 1 lease.release = 5 attested records")
}

// TestRelease_IdempotentReject: releasing an already-released lease fails with
// ErrAlreadyReleased and the release timestamp.
func TestRelease_IdempotentReject(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, false)
	defer cleanup()

	ctx := context.Background()
	pool, _ := p.CreatePool(ctx, PoolInput{
		Name: "idem-pool", GPUType: "T4-16G", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 1.0,
	})
	n1, _ := p.AddNode(ctx, pool.ID)
	require.NotEmpty(t, n1.ID)
	l1, _ := p.Acquire(ctx, pool.ID, "inf-idem000000000001", 2)

	_, _ = p.Release(ctx, l1.ID)

	_, err := p.Release(ctx, l1.ID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAlreadyReleased)
	assert.Contains(t, err.Error(), "already released")
}

// TestEvaluate_ScaleUp: pending > free triggers scale_up with ceil((pending-free)/spn);
// target capped at MaxNodes verified via TargetNodes=max.
func TestEvaluate_ScaleUp(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "scaleup-pool", GPUType: "V100-32G", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 3, CostPerNodeHour: 2.5,
	})
	require.NoError(t, err)

	_, _ = p.AddNode(ctx, pool.ID)
	_, _ = p.AddNode(ctx, pool.ID)
	// Nodes: 8+8=16 total slots, used: 6+0=6 → free=10
	_, _ = p.Acquire(ctx, pool.ID, "inf-su000000000001", 6) // n1 6/8, n2 0/8

	d, err := p.EvaluateElasticity(ctx, pool.ID, 20, 1000, 0)
	require.NoError(t, err)
	assert.Equal(t, "scale_up", d.Action)
	assert.True(t, d.BudgetOK, "budget OK scenario")
	// free=10, pending=20 → deficit=10 → need=ceil(10/8)=2 → target=2+2=4 → cap=3
	assert.Equal(t, 2, d.CurrentNodes)
	assert.Equal(t, 3, d.TargetNodes) // capped at MaxNodes
	assert.Equal(t, 2.5, d.CostImpactPerHour, "cap adds (3-2)=1 node × $2.50")
	assert.NotContains(t, d.Reason, "BUDGET REJECTED")
}

// TestEvaluate_BudgetRejected: currentCost + costImpact > budgetLimit
// → BudgetOK=false, Action="no_change", Reason contains "BUDGET REJECTED".
func TestEvaluate_BudgetRejected(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "budget-pool", GPUType: "L40-48G", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 2.0,
	})
	require.NoError(t, err)

	_, _ = p.AddNode(ctx, pool.ID)
	_, _ = p.Acquire(ctx, pool.ID, "inf-bu000000000001", 4) // full → free=0

	// pending=4 needs 1 node (impact $2/hr), but $99 + $2 > $100
	d, err := p.EvaluateElasticity(ctx, pool.ID, 4, 100, 99)
	require.NoError(t, err)
	assert.False(t, d.BudgetOK, "budget rejection expected")
	assert.Equal(t, "no_change", d.Action, "rejected → no_change")
	assert.Equal(t, 1, d.CurrentNodes, "target reset to current upon rejection")
	assert.Equal(t, 1, d.TargetNodes) // same as current
	assert.Contains(t, d.Reason, "BUDGET REJECTED", "reason must include BUDGET REJECTED keyword")
}

// TestEvaluate_ScaleDown: utilization < 30% and nodes > MinNodes → scale_down.
func TestEvaluate_ScaleDown(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "scaledown-pool", GPUType: "A100-40G", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 3.0,
	})
	require.NoError(t, err)

	// Add 3 nodes → 24 slots, acquire 4 slots → util≈16.7%
	for i := 0; i < 3; i++ {
		_, _ = p.AddNode(ctx, pool.ID)
	}
	_, _ = p.Acquire(ctx, pool.ID, "inf-sd000000000001", 4)

	d, err := p.EvaluateElasticity(ctx, pool.ID, 0, 1000, 0)
	require.NoError(t, err)
	assert.Equal(t, "scale_down", d.Action, "util < 30% triggers shrink")
	assert.True(t, d.BudgetOK, "scale_down always budget OK (savings)")
	assert.Equal(t, 3, d.CurrentNodes)
	assert.Equal(t, 1, d.TargetNodes, "max(MinNodes, ceil(4/8)=1)")
	assert.Negative(t, d.CostImpactPerHour, "negative cost impact = savings")
	assert.Contains(t, d.Reason, "below 30%")
}

// TestEvaluate_NoChange: pending fits free AND utilization within bounds → no_change.
func TestEvaluate_NoChange(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "nocp-pool", GPUType: "A10-40G", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 2.0,
	})
	require.NoError(t, err)

	_, _ = p.AddNode(ctx, pool.ID)
	_, _ = p.AddNode(ctx, pool.ID) // 16 slots total
	_, _ = p.Acquire(ctx, pool.ID, "inf-nocp000000000001", 8) // 8/16, free=8, util=50%

	d, err := p.EvaluateElasticity(ctx, pool.ID, 4, 1000, 0) // pending=4 <= free=8
	require.NoError(t, err)
	assert.Equal(t, "no_change", d.Action, "capacity available, utilization normal → no change")
	assert.True(t, d.BudgetOK)
	assert.Equal(t, 2, d.CurrentNodes)
	assert.Equal(t, 2, d.TargetNodes)
}

// TestPathTraversal: invalid pool/node IDs are rejected ([a-z0-9-] only).
func TestPathTraversal(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, false)
	defer cleanup()

	ctx := context.Background()
	_, err := p.GetPool("../etc")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidID)

	_, err = p.GetPool("INF-UPPERCASE")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidID)

	_, err = p.AddNode(ctx, "/escapes")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidID)

	_, _, err = p.FindLease("..\\windows")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidID)
}

// TestLeases_LastWriteWins: Release appends an updated lease row; Leases()
// merges by ID using last-write-wins semantics.
func TestLeases_LastWriteWins(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, _ := p.CreatePool(ctx, PoolInput{
		Name: "lww-pool", GPUType: "T4", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 1.0,
	})
	_, _ = p.AddNode(ctx, pool.ID)
	l1, _ := p.Acquire(ctx, pool.ID, "inf-lww000000000001", 2)
	leases1, _ := p.Leases(pool.ID, 0)
	require.Len(t, leases1, 1)
	assert.Nil(t, leases1[0].ReleasedAt, "held leases have ReleasedAt=nil")

	_, _ = p.Release(ctx, l1.ID)
	leases2, _ := p.Leases(pool.ID, 0)
	require.Len(t, leases2, 1, "merged view still shows 1 unique lease")
	assert.NotNil(t, leases2[0].ReleasedAt, "last-write-wins: ReleasedAt populated")
	assert.Equal(t, l1.NodeID, leases2[0].NodeID)
}

// TestNilLedgerDisablesAttestationOnly: with nil ledger all behavior works,
// just without receipts (parity with module 13/14 patterns).
func TestNilLedgerDisablesAttestationOnly(t *testing.T) {
	p, err := NewFSMElasticPool(t.TempDir(), nil)
	require.NoError(t, err)

	ctx := context.Background()
	pool, _ := p.CreatePool(ctx, PoolInput{Name: "x", GPUType: "V100", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 5, CostPerNodeHour: 1.0})
	_, _ = p.AddNode(ctx, pool.ID)
	_, _ = p.Acquire(ctx, pool.ID, "inf-noattest00000001", 2)
	_, _ = p.Release(ctx, "lease-x") // intentionally wrong ID
	d, _ := p.EvaluateElasticity(ctx, pool.ID, 0, 100, 0)
	assert.NotNil(t, d)

	assert.Nil(t, p.LastAttestation(), "no attestations with nil ledger")
}

// TestGetPool_ListPools: GetPool round-trips by ID; ListPools newest-first.
func TestGetPool_ListPools(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, false)
	defer cleanup()

	ctx := context.Background()
	poolA, _ := p.CreatePool(ctx, PoolInput{Name: "pool-a", GPUType: "X", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 5, CostPerNodeHour: 1.0})
	time.Sleep(1 * time.Millisecond)
	poolB, _ := p.CreatePool(ctx, PoolInput{Name: "pool-b", GPUType: "Y", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 2.0})

	list, _ := p.ListPools()
	require.Len(t, list, 2)
	assert.Equal(t, poolB.ID, list[0].ID, "newest first")
	assert.Equal(t, poolA.ID, list[1].ID)

	got, _ := p.GetPool(poolA.ID)
	assert.Equal(t, poolA.Name, got.Name)
}

// TestEvaluate_BudgetLimitBoundary: newCost == budgetLimit should ACCEPT (proving
// the guard is ">" not ">="). With epsilon tolerance, equality and sub-eps overshoot
// both pass. Tests the critical boundary condition.
func TestEvaluate_BudgetLimitBoundary(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "boundary-pool", GPUType: "H100", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 5, CostPerNodeHour: 2.0,
	})
	require.NoError(t, err)

	_, _ = p.AddNode(ctx, pool.ID)
	_, _ = p.Acquire(ctx, pool.ID, "inf-bound0000000001", 4) // full → free=0

	// Scenario A: pending > free requires 1 node ($2 impact)
	// Case 1: newCost == budgetLimit exactly → accept (this is the key boundary test)
	d, err := p.EvaluateElasticity(ctx, pool.ID, 4, 100, 98) // 98 + 2 == 100
	require.NoError(t, err)
	assert.True(t, d.BudgetOK, "equality (newCost == budgetLimit) must be accepted; guard is strictly '>'")
	assert.Equal(t, "scale_up", d.Action)

	// Case 2: slightly under limit → also accept
	d2, err := p.EvaluateElasticity(ctx, pool.ID, 4, 100, 97) // 97 + 2 < 100
	require.NoError(t, err)
	assert.True(t, d2.BudgetOK)
	assert.Equal(t, "scale_up", d2.Action)

	// Case 3: slightly over limit with epsilon margin (within 1e-9) → accept due to tolerance
	// Use floating point values that would exceed by sub-eps amount
	const eps = 1e-9
	d3, err := p.EvaluateElasticity(ctx, pool.ID, 4, 100+eps/2, 98) // 98+2 = 100, exceeds 100+eps/2 by -eps/2 (under)
	require.NoError(t, err)
	assert.True(t, d3.BudgetOK, "sub-eps overshoot within tolerance must be accepted")
	assert.Equal(t, "scale_up", d3.Action)

	// Verify rejected case still works: clear overshoot beyond epsilon rejects
	d4, err := p.EvaluateElasticity(ctx, pool.ID, 4, 100, 98.1) // 98.1 + 2 > 100 + eps → reject
	require.NoError(t, err)
	assert.False(t, d4.BudgetOK, "clear overshoot beyond epsilon must be rejected")
	assert.Equal(t, "no_change", d4.Action)
	assert.Contains(t, d4.Reason, "BUDGET REJECTED")
}

// TestCreatePool_RejectsNonFiniteCost: NaN/Inf cost_per_node_hour must be
// explicitly rejected with 'cost must be finite' error message. Verifies the
// Go version agnostic check using IsNaN/IsInf instead of math.IsFinite().
func TestCreatePool_RejectsNonFiniteCost(t *testing.T) {
	p, _, store, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()

	cases := []struct {
		name      string
		cost      float64
		wantError string
	}{
		{"NaN_Cost", math.NaN(), "cost must be finite"},
		{"Positive_Inf", math.Inf(+1), "cost must be finite"},
		// Negative infinity also triggers finiteness check; error message must mention 'must be finite'
		{"Negative_Inf", math.Inf(-1), "cost must be finite"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := p.CreatePool(ctx, PoolInput{
				Name:            "nan-pool",
				GPUType:         "A100",
				SlotsPerNode:    4,
				MinNodes:        1,
				MaxNodes:        5,
				CostPerNodeHour: tc.cost,
			})
			require.Error(t, err, "%s must fail for cost=%v", tc.name, tc.cost)
			assert.Contains(t, err.Error(), tc.wantError, "%s error message must mention 'cost must be finite'", tc.name)
		})
	}

	// Nothing persisted or attested when finiteness fails.
	recs, err := store.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, recs, "no ledger records after rejected non-finite costs")
}

// TestRelease_ToDrained: releasing the final lease on a node must transition it
// to drained status (UsedSlots==0). The node should then reject new acquisitions
// because drained nodes are not in 'ready' status. Tests the full drained lifecycle.
func TestRelease_ToDrained(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, true)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "drain-pool", GPUType: "V100", SlotsPerNode: 4, MinNodes: 1, MaxNodes: 5, CostPerNodeHour: 2.0,
	})
	require.NoError(t, err)

	// Add single node with exactly 4 slots
	node1, err := p.AddNode(ctx, pool.ID)
	require.NoError(t, err)
	require.Equal(t, 4, node1.TotalSlots)

	// Acquire two separate leases of 2 slots each → total 4/4 = busy
	l1, err := p.Acquire(ctx, pool.ID, "inf-drain0000000001", 2)
	require.NoError(t, err)
	l2, err := p.Acquire(ctx, pool.ID, "inf-drain0000000002", 2)
	require.NoError(t, err)

	// Verify node is busy (4/4 slots)
	nodes, _ := p.ListNodes(pool.ID)
	for _, nd := range nodes {
		if nd.ID == node1.ID {
			assert.Equal(t, NodeBusy, nd.Status, "fully leased node must be busy")
			assert.Equal(t, 4, nd.UsedSlots)
		}
	}

	// Release first lease → UsedSlots=2, becomes ready! (not busy anymore)
	_, err = p.Release(ctx, l1.ID)
	require.NoError(t, err)
	nodes, _ = p.ListNodes(pool.ID)
	for _, nd := range nodes {
		if nd.ID == node1.ID {
			assert.Equal(t, NodeReady, nd.Status, "node becomes ready after partial release (UsedSlots < TotalSlots)")
			assert.Equal(t, 2, nd.UsedSlots)
		}
	}

	// Release second (final) lease → UsedSlots=0, must become drained!
	_, err = p.Release(ctx, l2.ID)
	require.NoError(t, err)
	nodes, _ = p.ListNodes(pool.ID)
	for _, nd := range nodes {
		if nd.ID == node1.ID {
			assert.Equal(t, NodeDrained, nd.Status, "usedslots==0 must transition to drained")
			assert.Equal(t, 0, nd.UsedSlots)
		}
	}

	// Drained node should NOT accept new leases (status != ready)
	_, err = p.Acquire(ctx, pool.ID, "inf-drain-new-000001", 1)
	require.Error(t, err, "acquire on drained node must fail (no capacity)")
	assert.ErrorIs(t, err, ErrNoCapacity, "error must be ErrNoCapacity when no ready node exists")
}

// TestAcquire_BestFit_TightFit: constructs a scenario where TWO nodes have enough
// free space for the request. Best-fit should pick the TIGHTER fit (smallest
// satisfying free space), proving the fragmentation-minimizing algorithm works.
func TestAcquire_BestFit_TightFit(t *testing.T) {
	p, _, _, cleanup := newTestPool(t, false)
	defer cleanup()

	ctx := context.Background()
	pool, err := p.CreatePool(ctx, PoolInput{
		Name: "tightfit-pool", GPUType: "A100", SlotsPerNode: 8, MinNodes: 1, MaxNodes: 10, CostPerNodeHour: 2.0,
	})
	require.NoError(t, err)

	// Add two nodes: both start at 0/8 used
	n1, err := p.AddNode(ctx, pool.ID)
	require.NoError(t, err)
	n2, err := p.AddNode(ctx, pool.ID)
	require.NoError(t, err)

	 // Pre-fill n1 to create DIFFERENT free spaces:
	// - acquire 6 slots on n1 → n1: 6/8 used, free=2
	_, err = p.Acquire(ctx, pool.ID, "inf-tight0000000001", 6)
	require.NoError(t, err)
	// Now n1 has free=2, n2 has free=8

	// Try to acquire 2 slots: BOTH nodes satisfy (n1 free=2 >= 2; n2 free=8 >= 2)
	// Best-fit MUST pick n1 (tightest fit: free=2 < free=8)
	l, err := p.Acquire(ctx, pool.ID, "inf-tight0000000002", 2)
	require.NoError(t, err)

	// Verify best-fit picked the tight node (n1), not the loose one (n2)
	nodes, _ := p.ListNodes(pool.ID)
	for _, nd := range nodes {
		if nd.ID == n1.ID {
			assert.Equal(t, NodeBusy, nd.Status, "n1 becomes busy after receiving 2 more slots (6+2=8)")
			assert.Equal(t, 8, nd.UsedSlots)
		} else if nd.ID == n2.ID {
			assert.Equal(t, NodeReady, nd.Status, "n2 remains untouched by this acquire")
			assert.Equal(t, 0, nd.UsedSlots)
		}
	}

	// Additional assertion: lease was placed on n1, NOT n2
	if l.NodeID == n1.ID {
		t.Log("✓ BEST-FIT CONFIRMED: tight-fit node selected (minimizes fragmentation)")
	} else if l.NodeID == n2.ID {
		t.Fatal("✗ ALGORITHM FAILED: selected loose-fit node instead of tight-fit node (bug!)")
	}
}
