// Package wasm — Performance benchmarks for Module 53 moat algorithms.
// Run with: go test -bench=. -benchmem ./pkg/wasm/ -cpu=1,4,8 -benchtime=2s
package wasm

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// ============================================================================
// Host Function Dispatch Benchmarks (wazero integration)
// ============================================================================

func BenchmarkHostFunctionDispatch(c *testing.B) {
	ctx := context.Background()
	
	svc := NewMockGPUService(capability.ModeSimulated).(*mockGPUService)
	defer svc.Close()
	
	// grant := &Grant{GPU: &GPURule{AllowedDevices: []int{0}}} // NOT USED, cap check in DeviceCount doesn't need it
	
	c.ResetTimer()
	for i := 0; i < c.N; i++ {
		count, _ := svc.DeviceCount(ctx)
		_ = count
	}
}

// ============================================================================
// Zero-Copy vs memcpy Benchmarks
// ============================================================================

func BenchmarkZeroCopyVsMemcpy_1MB(c *testing.B) {
	ctx := context.Background()
	
	svc := NewMockGPUService(capability.ModeSimulated).(*mockGPUService)
	defer svc.Close()
	
	// Alloc 1MB buffer first
	handle, err := svc.Alloc(ctx, 1*1024*1024)
	if err != nil {
		c.Fatalf("Alloc failed: %v", err)
	}
	defer svc.Free(ctx, handle)
	
	// Valid grant
	grant := &Grant{GPU: &GPURule{AllowedDevices: []int{0}}}
	
	c.ResetTimer()
	for i := 0; i < c.N; i++ {
		// Measure zero-copy descriptor creation
		desc, err := GetZeroView(ctx, svc, grant, handle, 0, 1024*1024)
		if err != nil || desc == nil {
			c.Fatalf("GetZeroView failed: %v", err)
		}
		_ = desc.Length
	}
}

// ============================================================================
// Sharded Allocator Benchmarks (vs global mutex baseline)
// ============================================================================

func BenchmarkShardedAllocator_GlobalMutex_Comparison(c *testing.B) {
	c.Run("sharded-no-contention", func(c *testing.B) {
		alloc := NewShardedHandleAllocator()
		defer alloc.Close()
		
		ctx := context.Background()
		c.ResetTimer()
		for i := 0; i < c.N; i++ {
			h, err := alloc.AllocFast(ctx, 4096)
			if err != nil {
				c.Fatalf("Alloc failed: %v", err)
			}
			_ = alloc.FreeFast(h)
		}
	})
	
	c.Run("global-mutex-baseline", func(c *testing.B) {
		svc := NewMockGPUService(capability.ModeSimulated)
		defer svc.Close()
		
		ctx := context.Background()
		c.ResetTimer()
		for i := 0; i < c.N; i++ {
			h, err := svc.Alloc(ctx, 4096)
			if err != nil {
				c.Fatalf("Alloc failed: %v", err)
			}
			_ = svc.Free(ctx, h)
		}
	})
}

// ============================================================================
// Multi-threaded Sharded Allocator (concurrency scaling)
// ============================================================================

func BenchmarkShardedAllocator_ConcurrencyScaling(c *testing.B) {
	nCPU := 4 // test with 4 concurrent goroutines
	alloc := NewShardedHandleAllocator()
	defer alloc.Close()
	
	ctx := context.Background()
	sem := make(chan struct{}, nCPU)
	results := make(chan uint64, nCPU)
	
	c.ResetTimer()
	for i := 0; i < c.N; i++ {
		wg := new(sync.WaitGroup)
		for j := 0; j < nCPU; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				
				start := time.Now().UnixNano()
				h, err := alloc.AllocFast(ctx, 4096)
				if err == nil {
					_ = alloc.FreeFast(h)
				}
				elapsed := time.Now().UnixNano() - start
				
				results <- uint64(elapsed)
			}()
		}
		wg.Wait()
		close(results)
		results = make(chan uint64, nCPU)
	}
}

// ============================================================================
// Tenant Accounting Benchmarks
// ============================================================================

func BenchmarkTenantAccounting_SingleTenant_vs_100Tenants(c *testing.B) {
	c.Run("single-tenant", func(c *testing.B) {
		r := NewTenantAccountRegistry(1_000_000.0)
		defer r.Close()
		
		c.ResetTimer()
		for i := 0; i < c.N; i++ {
			r.TryConsumeForTenant("tenant-1", 100.0, 1_000_000.0)
		}
	})
	
	c.Run("100tenants-concurrent-burst", func(c *testing.B) {
		r := NewTenantAccountRegistry(100_000_000.0) // ample budget
		defer r.Close()
		
		c.ResetTimer()
		for i := 0; i < c.N; i++ {
			for t := 0; t < 100; t++ {
				tenantID := fmt.Sprintf("tenant-%d", t%100)
				r.TryConsumeForTenant(tenantID, 10.0, 100_000_000.0)
			}
		}
	})
}

// ============================================================================
// Optimal Placement Benchmarks (NVLink topology-aware scheduling)
// ============================================================================

func BenchmarkOptimalPlacement_VaryingScale(c *testing.B) {
	scales := []int{8, 16, 32, 64}
	
	for _, scale := range scales {
		c.Run(fmt.Sprintf("%d-gpus", scale), func(c *testing.B) {
			topology := createMockTopology(scale)
			req := OptimalPlacementRequest{GPUCount: scale}
			
			c.ResetTimer()
			for i := 0; i < c.N; i++ {
				result := OptimalPlacement(context.Background(), topology, req)
				_ = len(result.SelectedGPUs)
			}
		})
	}
}

// ============================================================================
// End-to-End Full Lifecycle Benchmark
// ============================================================================

func BenchmarkEndToEndGPUWASI_FullLifecycle(c *testing.B) {
	ctx := context.Background()
	
	svc := NewMockGPUService(capability.ModeSimulated).(*mockGPUService)
	defer svc.Close()
	
	grant := &Grant{GPU: &GPURule{AllowedDevices: []int{0, 1}}}
	
	c.ResetTimer()
	for i := 0; i < c.N; i++ {
		// 1. Device count
		count, _ := svc.DeviceCount(ctx)
		_ = count
		
		// 2. Device info lookup
		dev, _ := svc.DeviceInfo(ctx, 0)
		if dev == nil {
			continue
		}
		
		// 3. Allocate buffer
		handle, _ := svc.Alloc(ctx, 1024*1024)
		if handle == 0 {
			continue
		}
		
		// 4. Zero-view access
		desc, _ := GetZeroView(ctx, svc, grant, handle, 0, 1024)
		if desc == nil {
			svc.Free(ctx, handle)
			continue
		}
		
		// 5. Free buffer
		_ = svc.Free(ctx, handle)
		
		// 6. NVLink topology query
		topo, _ := svc.NVLinkTopology(ctx)
		if topo == nil {
			continue
		}
		
		// 7. Optimal placement
		req := OptimalPlacementRequest{GPUCount: len(topo.GPUDevices)}
		placement := OptimalPlacement(ctx, topo, req)
		_ = len(placement.SelectedGPUs)
	}
}

// ============================================================================
// Baseline Comparison: Old Global Mutex vs New Sharded
// ============================================================================

func BenchmarkGlobalMutex_AllocationOverhead(c *testing.B) {
	svc := NewMockGPUService(capability.ModeSimulated)
	defer svc.Close()
	
	ctx := context.Background()
	
	// This measures the OLD implementation using global mutex
	// Before refactoring to sharded allocator
	c.ReportAllocs()
	for i := 0; i < c.N; i++ {
		h, _ := svc.Alloc(ctx, 4096)
		_ = svc.Free(ctx, h)
	}
}
