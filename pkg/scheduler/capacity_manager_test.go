package scheduler

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestCapacityManager_TryAllocateAtomic verifies that TryAllocate performs the
// quota check and the reservation atomically: under heavy concurrency a tenant
// can never be granted more than its quota (no check-then-act overshoot).
func TestCapacityManager_TryAllocateAtomic(t *testing.T) {
	cm := NewCapacityManager(1000)
	cm.SetQuota(&TenantQuota{TenantID: "t1", GPUQuota: 50}) // hard cap, borrowing off

	const workers = 200
	var granted int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if ok, _ := cm.TryAllocate("t1", 1, 0, 0); ok {
				atomic.AddInt64(&granted, 1)
			}
		}()
	}
	wg.Wait()

	// Exactly GPUQuota grants may succeed and usage must never exceed the cap.
	if granted != 50 {
		t.Errorf("granted = %d, want exactly 50 (quota cap)", granted)
	}
	if got := cm.GetUsage()["t1"].GPUsAllocated; got != 50 {
		t.Errorf("GPUsAllocated = %d, want 50 (no overshoot)", got)
	}
}

// TestCapacityManager_TryAllocateRejectsOverQuota verifies a single request
// larger than the remaining quota is rejected and leaves usage unchanged.
func TestCapacityManager_TryAllocateRejectsOverQuota(t *testing.T) {
	cm := NewCapacityManager(1000)
	cm.SetQuota(&TenantQuota{TenantID: "t1", GPUQuota: 8})

	if ok, _ := cm.TryAllocate("t1", 6, 0, 0); !ok {
		t.Fatal("first request of 6/8 should be granted")
	}
	if ok, _ := cm.TryAllocate("t1", 4, 0, 0); ok {
		t.Error("second request of 4 (6+4>8) should be rejected")
	}
	if got := cm.GetUsage()["t1"].GPUsAllocated; got != 6 {
		t.Errorf("GPUsAllocated = %d, want 6 (rejected request must not deduct)", got)
	}
}
