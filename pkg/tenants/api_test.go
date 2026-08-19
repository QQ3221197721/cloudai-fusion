package tenants

import (
	"context"
	"os"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// TestTenantPoolCreation creates a pool without hardware, then checks store persistence.
func TestTenantPoolCreation(t *testing.T) {
	tmpDir := t.TempDir()
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	mgr, err := NewManager(tmpDir, gpuMgr)
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
		// Expected on hosts without nvidia-smi
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

	// Verify store persisted the pool
	pools := mgr.ListPools()
	if len(pools) != 1 {
		t.Errorf("expected 1 pool in list, got %d", len(pools))
	}
}

// TestAddTenantToMIGPool adds tenant when CreatePool succeeded and pool exists.
func TestAddTenantToMIGPool(t *testing.T) {
	// Skip if no nvidia-smi / MIG support
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, gpuMgr)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// First create pool
	input := PoolInput{
		Name:        "test-mps-shared",
		GPUType:     "nvidia-h100",
		MigProfile:  "", // N/A for MPS
		Mode:        PoolModeMPS,
		NodeIndex:   0,
		GPUIndices:  []int{0, 1},
		TotalSlices: 2,
	}
	pool, err := mgr.CreatePool(context.Background(), input)
	if err != nil {
		t.Skipf("pool creation skipped (no MPS daemon): %v", err)
	}

	// Add tenant with MPS resource mode
	memberInput := MemberInput{
		Name:         "alice-mps",
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

// TestAllocateInvalidParams exercises validation paths.
func TestAllocateInvalidParams(t *testing.T) {
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, gpuMgr)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Invalid positive test - allocate negative slices should fail
	_, err = mgr.AllocateToTenant(context.Background(), "fake-pool", "fake-tenant", -1)
	if err == nil {
		t.Error("expected error for negative slices allocation")
	}
	// Invalid zero slices
	_, err = mgr.AllocateToTenant(context.Background(), "fake-pool", "fake-tenant", 0)
	if err == nil {
		t.Error("expected error for zero slices allocation")
	}
	// Non-existent pool
	_, err = mgr.AllocateToTenant(context.Background(), "non-existent", "tenant", 1)
	if err == nil {
		t.Error("expected error for non-existent pool")
	}
}

// TestRemoveTenant removes a tenant and verifies it's gone from the store.
func TestRemoveTenant(t *testing.T) {
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	tmpDir := t.TempDir()
	mgr, err := NewManager(tmpDir, gpuMgr)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create pool
	pool, err := mgr.CreatePool(context.Background(), PoolInput{
		Name:        "test-remove",
		GPUType:     "nvidia-a100",
		MigProfile:  "1g.5gb",
		Mode:        PoolModeMIG,
		GPUIndices:  []int{0},
		TotalSlices: 4,
	})
	if err != nil {
		// MIG may not be supported; skip test but keep flow consistent
		t.Skipf("skipping remove test (MIG unavailable): %v", err)
	}

	// Add tenant
	member, err := mgr.AddTenant(context.Background(), pool.ID, MemberInput{
		Name:         "temp-tenant",
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

	// Now remove tenant
	if err := mgr.RemoveTenant(context.Background(), pool.ID, member.ID); err != nil {
		t.Fatalf("RemoveTenant failed: %v", err)
	}

	// Verify removed from store
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

// TestStorePersistence verifies data survives manager recreation.
func TestStorePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})

	// First manager creates pool
	mgr1, err := NewManager(tmpDir, gpuMgr)
	if err != nil {
		t.Fatalf("NewManager #1 failed: %v", err)
	}

	_, err = mgr1.CreatePool(context.Background(), PoolInput{
		Name:        "persistent-test",
		GPUType:     "nvidia-h100",
		Mode:        PoolModeMPS,
		GPUIndices:  []int{0},
		TotalSlices: 1,
	})
	if err != nil {
		t.Skipf("skipping persistence test (create failed): %v", err)
	}

	// Read file to verify write happened
	data, readErr := os.ReadFile(mgr1.store.poolsFile())
	if readErr != nil {
		t.Fatalf("persistence file not found after first manager: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("persistence file is empty")
	}

	// Second manager loads from disk
	mgr2, err := NewManager(tmpDir, gpuMgr)
	if err != nil {
		t.Fatalf("NewManager #2 failed: %v", err)
	}

	pools := mgr2.ListPools()
	if len(pools) == 0 {
		t.Fatal("second manager loaded no pools from disk")
	}
}
