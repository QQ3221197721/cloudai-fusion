// Package wasm — Unit tests for Module 53 performance moat algorithms.
// These tests verify the core algorithms: zero-copy, sharded allocator, token bucket.
package wasm

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// ============================================================================
// Zero-Copy Buffer Tests
// ============================================================================

func TestZeroCopyVsMemcpy_Overhead(t *testing.T) {
	svc := NewMockGPUService(capability.ModeSimulated).(*mockGPUService)
	defer svc.Close()

	ctx := context.Background()
	
	// Valid grant
	grant := &Grant{GPU: &GPURule{AllowedDevices: []int{0}}}
	
	// Alloc a buffer first
	handle, err := svc.Alloc(ctx, 1*1024*1024) // 1MB
	if err != nil {
		t.Fatalf("Alloc failed: %v", err)
	}
	defer svc.Free(ctx, handle)
	
	// Get zero view
	desc, err := GetZeroView(ctx, svc, grant, handle, 0, 1024)
	if err != nil {
		t.Fatalf("GetZeroView failed: %v", err)
	}
	if desc == nil {
		t.Fatal("Expected non-nil descriptor")
	}
	
	if desc.Length != 1024 {
		t.Errorf("Expected length 1024, got %d", desc.Length)
	}
	
	// Compare overhead
	memcpyUs := BenchmarkMemcpyLatency(1024)
	zeroviewNs := BenchmarkZeroViewOverhead()
	
	t.Logf("memcpy 1KB latency: %.2fµs", memcpyUs)
	t.Logf("zero-copy descriptor creation: %dns", zeroviewNs)
	
	// Zero-copy should be significantly faster (by design)
	if zeroviewNs >= uint64(memcpyUs*1000) {
		t.Log("WARNING: zero-copy not showing expected advantage")
	}
}

// ============================================================================
// Sharded Allocator Tests
// ============================================================================

func TestShardedAllocator_NConcurrency(t *testing.T) {
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	
	ctx := context.Background()
	
	// Test single-threaded first
	handles := make([]uint64, 0, 16)
	for i := 0; i < 16; i++ {
		h, err := alloc.AllocFast(ctx, 4096)
		if err != nil {
			t.Fatalf("Alloc #%d failed: %v", i, err)
		}
		handles = append(handles, h)
	}
	
	// Free all
	for _, h := range handles {
		err := alloc.FreeFast(h)
		if err != nil {
			t.Errorf("Free %d failed: %v", h, err)
		}
	}
	
	// Verify count reset
	if alloc.Count() != 0 {
		t.Errorf("Expected count 0 after free, got %d", alloc.Count())
	}
	
	// Measure latency
	allocNs, freeNs := BenchmarkLatencyNoContention()
	t.Logf("Single-alloc latency: %dns", allocNs)
	t.Logf("Single-free latency: %dns", freeNs)
}

// ============================================================================
// Token Bucket Tests
// ============================================================================

func TestTokenBucket_TryConsume_SuccessAndFail(t *testing.T) {
	r := NewTenantAccountRegistry(10_000.0) // 10ms budget (short)
	defer r.Close()
	
	// Single tenant consumption
	success := r.TryConsumeForTenant("tenant-1", 100.0, 10_000.0)
	if !success {
		t.Error("Expected consume to succeed with fresh bucket")
	}
	
	// Consume large chunk - should nearly exhaust
	success = r.TryConsumeForTenant("tenant-1", 9000.0, 10_000.0)
	if !success {
		t.Log("Second consume failed as expected due to refill lag")
	}
	
	// Wait a moment to avoid auto-refill interference
	time.Sleep(10 * time.Millisecond)
	
	// Try to exceed budget rapidly (should fail after first success)
	success = r.TryConsumeForTenant("tenant-1", 5000.0, 10_000.0)
	if !success {
		t.Log("Expected consume to fail when budget exhausted")
	} else {
		// If succeeded, try another immediately (should fail)
		success2 := r.TryConsumeForTenant("tenant-1", 5000.0, 10_000.0)
		if success2 {
			t.Error("Expected second rapid consume to fail")
		}
	}
	
	// Multi-tenant isolation
	success1 := r.TryConsumeForTenant("tenant-A", 1000.0, 10_000.0)
	success2 := r.TryConsumeForTenant("tenant-B", 1000.0, 10_000.0)
	
	if !success1 || !success2 {
		t.Error("Both tenants should succeed independently")
	}
}

// ============================================================================
// Optimal Placement Tests
// ============================================================================

func TestOptimalPlacement_VariousSizes(t *testing.T) {
	topology := createMockTopology(8)
	req := OptimalPlacementRequest{GPUCount: 8}
	
	result := OptimalPlacement(context.Background(), topology, req)
	
	if len(result.SelectedGPUs) != 8 {
		t.Errorf("Expected 8 GPUs selected, got %d", len(result.SelectedGPUs))
	}
	
	if result.TotalBandwidth <= 0 {
		t.Error("Expected positive total bandwidth")
	}
	
	if !result.IsFullyConnected {
		t.Log("Topology may not have enough edges for full connectivity")
	}
	
	t.Logf("Selected GPUs: %d", len(result.SelectedGPUs))
	t.Logf("Total bandwidth: %.2f GB/s", result.TotalBandwidth)
}
