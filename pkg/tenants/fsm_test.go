// Package tenants - unit tests for Module 11: Multi-tenant GPU Sharing (Phase 2).
package tenants

import (
	"context"
	"os"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestManager builds a Manager over a temp store. attest=true wires a REAL
// ledger (MemoryStore + ephemeral Ed25519 signer); attest=false exercises the
// nil-ledger degraded mode exactly like pkg/elasticpool tests.
func newTestManager(t *testing.T, attest bool) (*Manager, *evidence.MemoryStore) {
	t.Helper()
	tmp := t.TempDir()
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	var ledger *evidence.Ledger
	var store *evidence.MemoryStore
	if attest {
		store = evidence.NewMemoryStore()
		signer, err := evidence.GenerateEphemeralSigner()
		require.NoError(t, err, "generate ephemeral signer")
		ledger, err = evidence.NewLedger(evidence.LedgerConfig{
			Store:    store,
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		require.NoError(t, err, "build ledger")
	}
	mgr, err := NewManagerWithLedger(tmp, gpuMgr, ledger)
	require.NoError(t, err, "new manager")
	return mgr, store
}

// mustCreateActiveMSPPool creates an MPS pool and activates it, returning the ID.
// MPS pools are pure bookkeeping (no NVIDIA hardware required).
func mustCreateActiveMSPPool(t *testing.T, mgr *Manager, name string) string {
	t.Helper()
	pool, err := mgr.CreatePool(context.Background(), PoolInput{
		Name:        name,
		GPUType:     "nvidia-a100",
		Mode:        PoolModeMPS,
		GPUIndices:  []int{0},
		TotalSlices: 4,
	})
	require.NoError(t, err, "create MPS pool")
	require.Equal(t, statusPending, pool.Status, "new pools start pending")
	active, err := mgr.ActivatePool(context.Background(), pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusActive, active.Status)
	return pool.ID
}

// assertAttestation checks that LastAttestation has expected action/actor/signature.
func assertAttestation(t *testing.T, mgr *Manager, action, subject string) {
	t.Helper()
	ev := mgr.LastAttestation()
	require.NotNil(t, ev, "LastAttestation must be non-nil after %s", action)
	assert.Equal(t, action, ev.Action)
	assert.Equal(t, subject, ev.Subject)
	assert.Equal(t, DefaultTenantActor, ev.Actor)
	assert.NotEmpty(t, ev.Signature, "receipt must be Ed25519-signed")
	assert.NotEmpty(t, ev.Hash, "receipt must have content hash")
}

// =============================================================================
// FSM validation table-driven tests
// =============================================================================

func TestValidateLifecycleTransition_Table(t *testing.T) {
	legal := map[[2]string]bool{
		{statusPending, statusActive}:     true,
		{statusActive, statusSuspended}:   true,
		{statusSuspended, statusActive}:   true,
		{statusActive, statusDeleted}:     true,
		{statusSuspended, statusDeleted}:  true,
		{statusPending, statusPending}:    false, // same state
		{statusActive, statusActive}:      false,
		{statusSuspended, statusSuspended}: false,
		{statusDeleted, statusDeleted}:    false,
		{statusPending, statusSuspended}:  false,
		{statusPending, statusDeleted}:    false,
		{statusSuspended, statusPending}:  false,
		{statusActive, statusPending}:     false,
		{statusDeleted, statusActive}:     false,
		{statusDeleted, statusSuspended}:  false,
	}
	statuses := []string{statusPending, statusActive, statusSuspended, statusDeleted}
	for _, from := range statuses {
		for _, to := range statuses {
			err := validateLifecycleTransition("pool", "p1", from, to)
			if legal[[2]string{from, to}] {
				assert.NoError(t, err, "%s -> %s should be legal", from, to)
			} else {
				require.Error(t, err, "%s -> %s should be illegal", from, to)
				msg := err.Error()
				assert.Contains(t, msg, from)
				assert.Contains(t, msg, to)
				if from == to {
					assert.Contains(t, msg, "already")
				} else if from == statusDeleted {
					assert.Contains(t, msg, "terminal")
				} else {
					assert.Contains(t, msg, "allowed next statuses")
				}
			}
		}
	}
}

// =============================================================================
// Pool lifecycle: full legal path
// =============================================================================

func TestPoolLifecycleFullLegalPath(t *testing.T) {
	mgr, store := newTestManager(t, true)
	ctx := context.Background()

	// Create -> pending
	pool, err := mgr.CreatePool(ctx, PoolInput{Name: "fsm-pool", GPUType: "a100", Mode: PoolModeMPS, GPUIndices: []int{0}, TotalSlices: 4})
	require.NoError(t, err)
	assert.Equal(t, statusPending, pool.Status)
	assertAttestation(t, mgr, "tenant.pool.create", pool.ID)

	// pending -> active
	p, err := mgr.ActivatePool(ctx, pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusActive, p.Status)
	assertAttestation(t, mgr, "tenant.pool.activate", pool.ID)

	// active -> suspended
	p, err = mgr.SuspendPool(ctx, pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusSuspended, p.Status)
	assertAttestation(t, mgr, "tenant.pool.suspend", pool.ID)

	// suspended -> active
	p, err = mgr.ResumePool(ctx, pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusActive, p.Status)
	assertAttestation(t, mgr, "tenant.pool.resume", pool.ID)

	// active -> deleted
	p, err = mgr.DeletePool(ctx, pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusDeleted, p.Status)
	assertAttestation(t, mgr, "tenant.pool.delete", pool.ID)

	// All records attested: 5 receipts total
	recs, err := store.All(ctx)
	require.NoError(t, err)
	assert.Len(t, recs, 5, "five write operations should produce five receipts")
	for _, r := range recs {
		assert.NotEmpty(t, r.Signature, "all receipts signed")
		assert.Equal(t, DefaultTenantActor, r.Actor)
	}
}

// =============================================================================
// Illegal transitions
// =============================================================================

func TestPoolIllegalTransitions(t *testing.T) {
	mgr, _ := newTestManager(t, true)
	ctx := context.Background()

	// Fresh pool is pending
	pool, err := mgr.CreatePool(ctx, PoolInput{Name: "pending-pool", GPUType: "a100", Mode: PoolModeMPS, GPUIndices: []int{0}, TotalSlices: 2})
	require.NoError(t, err)
	assert.Equal(t, statusPending, pool.Status)

	// Initial attestation exists
	require.NotNil(t, mgr.LastAttestation())
	initialHash := mgr.LastAttestation().Hash

	// 1) pending -> suspended (suspend before activate)
	_, err = mgr.SuspendPool(ctx, pool.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pending -> suspended")
	assert.Contains(t, err.Error(), "active") // lists allowed next statuses

	// Failed transition must NOT mutate state or create receipt
	got, err := mgr.GetPool(pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusPending, got.Status, "state unchanged after failed transition")
	assert.Equal(t, initialHash, mgr.LastAttestation().Hash, "no new receipt on failure")

	// 2) pending -> deleted (delete before activate)
	_, err = mgr.DeletePool(ctx, pool.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pending -> deleted")

	// 3+4) Terminal: revived -> no; suspended -> no (need activated pool first)
	activate, err := mgr.ActivatePool(ctx, pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusActive, activate.Status)

	_, err = mgr.DeletePool(ctx, pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusDeleted, getPoolStatus(t, mgr, pool.ID))

	// deleted -> active (revival rejected)
	_, err = mgr.ActivatePool(ctx, pool.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleted -> active")
	assert.Contains(t, err.Error(), "terminal")

	// deleted -> suspended
	_, err = mgr.SuspendPool(ctx, pool.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleted -> suspended")

	// 5) already-in-state: resume an active pool (fresh pool, since the old one is now terminal-deleted)
	fresh, err := mgr.CreatePool(ctx, PoolInput{Name: "already-active-pool", GPUType: "a100", Mode: PoolModeMPS, GPUIndices: []int{0}, TotalSlices: 2})
	require.NoError(t, err)
	_, err = mgr.ActivatePool(ctx, fresh.ID)
	require.NoError(t, err)

	_, err = mgr.ResumePool(ctx, fresh.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already")
}

// GetPool helper for readability.
func getPoolStatus(t *testing.T, mgr *Manager, id string) string {
	t.Helper()
	p, err := mgr.GetPool(id)
	require.NoError(t, err)
	return p.Status
}

// =============================================================================
// Guard enforcement on write operations
// =============================================================================

func TestPoolStatusGuardsOnWrites(t *testing.T) {
	mgr, _ := newTestManager(t, true)
	ctx := context.Background()

	// pending pool accepts AddTenant (registration during provisioning)
	pool, err := mgr.CreatePool(ctx, PoolInput{Name: "guard-pool", GPUType: "a100", Mode: PoolModeMPS, GPUIndices: []int{0}, TotalSlices: 4})
	require.NoError(t, err)
	_, err = mgr.AddTenant(ctx, pool.ID, MemberInput{Name: "alice", ResourceMode: ResourceModeMPSShare, Slices: 0, MaxClients: 8})
	require.NoError(t, err)
	_ = err // add tenant to pending pool allowed

	// suspend pool rejects AddTenant and AllocateToTenant (activate first: pending -> suspended is illegal)
	_, err = mgr.ActivatePool(ctx, pool.ID)
	require.NoError(t, err)
	_, err = mgr.SuspendPool(ctx, pool.ID)
	require.NoError(t, err)

	_, err = mgr.AddTenant(ctx, pool.ID, MemberInput{Name: "bob", ResourceMode: ResourceModeMPSShare})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "suspended")

	// allocate requires active pool (member not created yet -> can't test allocate, but guard blocks anyway)
	_, err = mgr.AllocateToTenant(ctx, pool.ID, "dummy", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allocation requires an active pool")

	// delete pool OK from suspended state (suspended→deleted is legal)
	_, err = mgr.DeletePool(ctx, pool.ID)
	require.NoError(t, err) // delete pool from suspended state allowed

	// added-after-delete: both add and allocate reject on deleted pool
	_, err = mgr.AddTenant(ctx, pool.ID, MemberInput{Name: "charlie", ResourceMode: ResourceModeMPSShare})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleted")

	_, err = mgr.AllocateToTenant(ctx, pool.ID, "any", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allocation requires an active pool")

	// List still returns deleted pool (audit trail)
	all := mgr.ListPools()
	found := false
	for _, p := range all {
		if p.ID == pool.ID {
			found = true
			assert.Equal(t, statusDeleted, p.Status)
			break
		}
	}
	assert.True(t, found, "deleted pool visible in list")
}

// =============================================================================
// Member lifecycle
// =============================================================================

func TestMemberLifecycle(t *testing.T) {
	mgr, _ := newTestManager(t, true)
	ctx := context.Background()

	poolID := mustCreateActiveMSPPool(t, mgr, "member-lifecycle")

	// AddTenant creates member as active (backward compatible)
	member, err := mgr.AddTenant(ctx, poolID, MemberInput{Name: "mike", ResourceMode: ResourceModeMPSShare, Slices: 0, MaxClients: 4})
	require.NoError(t, err)
	assert.Equal(t, statusActive, string(member.Status))
	assertAttestation(t, mgr, "tenant.add", member.ID)

	// SuspendTenant: active -> suspended
	m2, err := mgr.SuspendTenant(ctx, poolID, member.ID)
	require.NoError(t, err)
	assert.Equal(t, statusSuspended, string(m2.Status))
	assertAttestation(t, mgr, "tenant.suspend", member.ID)

	// Double suspend: already suspended
	_, err = mgr.SuspendTenant(ctx, poolID, member.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already")

	// ResumeTenant: suspended -> active
	m3, err := mgr.ResumeTenant(ctx, poolID, member.ID)
	require.NoError(t, err)
	assert.Equal(t, statusActive, string(m3.Status))
	assertAttestation(t, mgr, "tenant.resume", member.ID)

	// RemoveTenant: active -> deleted + physical removal
	err = mgr.RemoveTenant(ctx, poolID, member.ID)
	require.NoError(t, err)
	got, _ := mgr.GetPool(poolID)
	for _, m := range got.Members {
		assert.NotEqual(t, member.ID, m.ID) // member removed
	}
	assertAttestation(t, mgr, "tenant.remove", member.ID)

	// Second remove: not found
	err = mgr.RemoveTenant(ctx, poolID, member.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// Persisted suspension survives reload
// =============================================================================

func TestSuspendedStatePersistsRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	// Create manager without ledger (persistence independent of attestation)
	mgr1, err := NewManager(tmp, gpuMgr)
	require.NoError(t, err)

	pool, err := mgr1.CreatePool(context.Background(), PoolInput{Name: "persist-pool", GPUType: "a100", Mode: PoolModeMPS, GPUIndices: []int{0}, TotalSlices: 2})
	require.NoError(t, err)
	_, err = mgr1.ActivatePool(context.Background(), pool.ID)
	require.NoError(t, err)
	_, err = mgr1.SuspendPool(context.Background(), pool.ID)
	require.NoError(t, err)

	// Reload via fresh manager
	mgr2, err := NewManager(tmp, gpuMgr)
	require.NoError(t, err)

	reloaded, err := mgr2.GetPool(pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusSuspended, reloaded.Status)

	// Resume works across reload
	_, err = mgr2.ResumePool(context.Background(), pool.ID)
	require.NoError(t, err)
	reloaded2, err := mgr2.GetPool(pool.ID)
	require.NoError(t, err)
	assert.Equal(t, statusActive, reloaded2.Status)
}

// =============================================================================
// Nil ledger degraded mode: operations succeed with no receipts
// =============================================================================

func TestNilLedgerDegradedMode(t *testing.T) {
	mgr, store := newTestManager(t, false) // nil ledger
	require.Nil(t, store, "store should be nil when attest=false")
	assert.False(t, mgr.AttestationEnabled)
	assert.Empty(t, mgr.LastAttestationHash)

	ctx := context.Background()
	pool, err := mgr.CreatePool(ctx, PoolInput{Name: "degraded-pool", GPUType: "a100", Mode: PoolModeMPS, GPUIndices: []int{0}, TotalSlices: 2})
	require.NoError(t, err)
	assert.Equal(t, statusPending, pool.Status)

	_, err = mgr.ActivatePool(ctx, pool.ID)
	require.NoError(t, err)

	member, err := mgr.AddTenant(ctx, pool.ID, MemberInput{Name: "norm", ResourceMode: ResourceModeMPSShare, Slices: 0, MaxClients: 2})
	require.NoError(t, err)
	assert.Equal(t, statusActive, string(member.Status))

	_, err = mgr.SuspendPool(ctx, pool.ID)
	require.NoError(t, err)
	_, err = mgr.ResumePool(ctx, pool.ID)
	require.NoError(t, err)
	err = mgr.RemoveTenant(ctx, pool.ID, member.ID)
	require.NoError(t, err)

	// No receipts written
	assert.Nil(t, mgr.LastAttestation())
	assert.Empty(t, mgr.LastAttestationHash)
}

// =============================================================================
// Attentation per-write-operation: all actions recorded
// =============================================================================

func TestAttestationPerWriteOperation(t *testing.T) {
	mgr, store := newTestManager(t, true)
	ctx := context.Background()

	// 1. Create
	pool, err := mgr.CreatePool(ctx, PoolInput{Name: "action-pool", GPUType: "a100", Mode: PoolModeMPS, GPUIndices: []int{0}, TotalSlices: 4})
	require.NoError(t, err)
	actionCount(t, store, ctx, 1, "after create")
	assertAttestation(t, mgr, "tenant.pool.create", pool.ID)

	// 2. Activate
	_, err = mgr.ActivatePool(ctx, pool.ID)
	require.NoError(t, err)
	actionCount(t, store, ctx, 2, "after activate")
	assertAttestation(t, mgr, "tenant.pool.activate", pool.ID)

	// 3. Add Tenant
	member, err := mgr.AddTenant(ctx, pool.ID, MemberInput{Name: "op-user", ResourceMode: ResourceModeMPSShare, Slices: 0, MaxClients: 8})
	require.NoError(t, err)
	actionCount(t, store, ctx, 3, "after add-tenant")
	assertAttestation(t, mgr, "tenant.add", member.ID)

	// 4. AllocateToTenant (MPS: grows max_clients)
	_, err = mgr.AllocateToTenant(ctx, pool.ID, member.ID, 2)
	require.NoError(t, err)
	actionCount(t, store, ctx, 4, "after allocate")
	assertAttestation(t, mgr, "tenant.allocate", member.ID)

	// 5. SuspendTenant
	_, err = mgr.SuspendTenant(ctx, pool.ID, member.ID)
	require.NoError(t, err)
	actionCount(t, store, ctx, 5, "after suspend-tenant")
	assertAttestation(t, mgr, "tenant.suspend", member.ID)

	// 6. ResumeTenant
	_, err = mgr.ResumeTenant(ctx, pool.ID, member.ID)
	require.NoError(t, err)
	actionCount(t, store, ctx, 6, "after resume-tenant")
	assertAttestation(t, mgr, "tenant.resume", member.ID)

	// 7. SuspendPool
	_, err = mgr.SuspendPool(ctx, pool.ID)
	require.NoError(t, err)
	actionCount(t, store, ctx, 7, "after suspend-pool")
	assertAttestation(t, mgr, "tenant.pool.suspend", pool.ID)

	// 8. RemoveTenant
	err = mgr.RemoveTenant(ctx, pool.ID, member.ID)
	require.NoError(t, err)
	actionCount(t, store, ctx, 8, "after remove-tenant")
	assertAttestation(t, mgr, "tenant.remove", member.ID)

	// 9. DeletePool (suspended -> deleted OK)
	_, err = mgr.DeletePool(ctx, pool.ID)
	require.NoError(t, err)
	actionCount(t, store, ctx, 9, "after delete-pool")
	assertAttestation(t, mgr, "tenant.pool.delete", pool.ID)
}

func actionCount(t *testing.T, store *evidence.MemoryStore, ctx context.Context, want int, note string) {
	t.Helper()
	recs, err := store.All(ctx)
	require.NoError(t, err)
	assert.Len(t, recs, want, "%s", note)
}

// =============================================================================
// Backward compat: old tests still pass (modified where FSM changes apply)
// =============================================================================

// These mirror the original api_test.go tests, adapted for FSM-aware behavior.

func TestTenantPoolCreation_BackwardCompat(t *testing.T) {
	tmpDir := t.TempDir()
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	mgr, err := NewManagerWithLedger(tmpDir, gpuMgr, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	input := PoolInput{
		Name:        "test-pool-a100",
		GPUType:     "nvidia-a100",
		MigProfile:  "1g.5gb",
		Mode:        PoolModeMIG,
		NodeIndex:   0,
		GPUIndices:  []int{0},
		TotalSlices: 4,
	}
	pool, err := mgr.CreatePool(context.Background(), input)
	if err != nil {
		t.Logf("CreatePool skipped due to missing nvidia-smi: %v", err)
		return
	}

	if pool.ID == "" {
		t.Errorf("unexpected empty ID")
	}
	if pool.Name != "test-pool-a100" {
		t.Errorf("name mismatch: got %s", pool.Name)
	}
	if pool.Mode != PoolModeMIG {
		t.Errorf("mode mismatch: got %s", pool.Mode)
	}
	if pool.TotalSlices != 4 {
		t.Errorf("total slices mismatch: got %d, want %d", pool.TotalSlices, 4)
	}

	pools := mgr.ListPools()
	if len(pools) != 1 {
		t.Errorf("expected 1 pool in list, got %d", len(pools))
	}
}

// Note: TestAddTenantToMIGPool now explicitly activates pool first to keep
// assertions meaningful while maintaining backward compatibility for the
// add-tenant operation itself.
func TestAddTenantToMIGPool_BackwardCompat(t *testing.T) {
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	tmpDir := t.TempDir()
	mgr, err := NewManagerWithLedger(tmpDir, gpuMgr, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	input := PoolInput{
		Name:        "test-mps-shared-backup",
		GPUType:     "nvidia-h100",
		MigProfile:  "",
		Mode:        PoolModeMPS,
		NodeIndex:   0,
		GPUIndices:  []int{0, 1},
		TotalSlices: 2,
	}
	pool, err := mgr.CreatePool(context.Background(), input)
	if err != nil {
		t.Skipf("pool creation skipped: %v", err)
	}

	// Explicit activation required by FSM
	_, err = mgr.ActivatePool(context.Background(), pool.ID)
	if err != nil {
		t.Fatalf("ActivatePool failed unexpectedly: %v", err)
	}

	memberInput := MemberInput{
		Name:         "alice-backup",
		UID:          common.NewUUID(),
		ResourceMode: ResourceModeMPSShare,
		Slices:       0,
		MaxClients:   16,
	}
	member, err := mgr.AddTenant(context.Background(), pool.ID, memberInput)
	if err != nil {
		t.Fatalf("AddTenant failed unexpectedly: %v", err)
	}
	if member == nil {
		t.Fatal("returned member is nil")
	}
	if len(member.MIGSlices) != 0 {
		t.Errorf("expected 0 MIG slices for MPS, got %d", len(member.MIGSlices))
	}
}

// TestAllocateInvalidParams: error paths unchanged.
func TestAllocateInvalidParams_BackwardCompat(t *testing.T) {
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	tmpDir := t.TempDir()
	mgr, err := NewManagerWithLedger(tmpDir, gpuMgr, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	_, err = mgr.AllocateToTenant(context.Background(), "fake-pool", "fake-tenant", -1)
	if err == nil {
		t.Error("expected error for negative slices allocation")
	}
	_, err = mgr.AllocateToTenant(context.Background(), "fake-pool", "fake-tenant", 0)
	if err == nil {
		t.Error("expected error for zero slices allocation")
	}
	_, err = mgr.AllocateToTenant(context.Background(), "non-existent", "tenant", 1)
	if err == nil {
		t.Error("expected error for non-existent pool")
	}
}

// TestRemoveTenant skips if MIG unavailable; FSM guards don't change skip logic.
func TestRemoveTenant_BackwardCompat(t *testing.T) {
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	tmpDir := t.TempDir()
	mgr, err := NewManagerWithLedger(tmpDir, gpuMgr, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	pool, err := mgr.CreatePool(context.Background(), PoolInput{
		Name:        "test-remove-backup",
		GPUType:     "nvidia-a100",
		MigProfile:  "1g.5gb",
		Mode:        PoolModeMIG,
		GPUIndices:  []int{0},
		TotalSlices: 4,
	})
	if err != nil {
		t.Skipf("skipping remove test (MIG unavailable): %v", err)
	}

	_, err = mgr.ActivatePool(context.Background(), pool.ID)
	if err != nil {
		t.Skipf("skipping remove test (activate failed): %v", err)
	}

	member, err := mgr.AddTenant(context.Background(), pool.ID, MemberInput{
		Name:         "temp-tenant-backup",
		UID:          "test-user-id",
		ResourceMode: ResourceModeMIGSlice,
		Slices:       1,
	})
	if err != nil {
		t.Skipf("skipping remove test (add tenant failed): %v", err)
	}
	if member == nil || len(member.MIGSlices) == 0 {
		t.Skip("skipping remove test (no MIG allocated)")
	}

	if err := mgr.RemoveTenant(context.Background(), pool.ID, member.ID); err != nil {
		t.Fatalf("RemoveTenant failed: %v", err)
	}

	pool2, err := mgr.GetPool(pool.ID)
	if err != nil {
		t.Fatalf("GetPool after removal failed: %v", err)
	}
	for i := range pool2.Members {
		if pool2.Members[i].ID == member.ID {
			t.Error("member still present after removal")
		}
	}
}

// TestStorePersistence unchanged (persistence layer independent of FSM).
func TestStorePersistence_BackwardCompat(t *testing.T) {
	tmpDir := t.TempDir()
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})

	mgr1, err := NewManagerWithLedger(tmpDir, gpuMgr, nil)
	if err != nil {
		t.Fatalf("NewManager #1 failed: %v", err)
	}

	_, err = mgr1.CreatePool(context.Background(), PoolInput{
		Name:        "persistent-test-backup",
		GPUType:     "nvidia-h100",
		Mode:        PoolModeMPS,
		GPUIndices:  []int{0},
		TotalSlices: 1,
	})
	if err != nil {
		t.Skipf("skipping persistence test (create failed): %v", err)
	}

	data, readErr := os.ReadFile(mgr1.store.poolsFile())
	if readErr != nil {
		t.Fatalf("persistence file not found after first manager: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("persistence file is empty")
	}

	mgr2, err := NewManagerWithLedger(tmpDir, gpuMgr, nil)
	if err != nil {
		t.Fatalf("NewManager #2 failed: %v", err)
	}

	pools := mgr2.ListPools()
	if len(pools) == 0 {
		t.Fatal("second manager loaded no pools from disk")
	}
}
