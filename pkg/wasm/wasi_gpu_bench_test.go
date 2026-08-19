// Package wasm — Module 53: GPU WASI benchmark suite.
// Measures validation logic overhead (not real GPU compute latency).
// All benchmarks run against simulated GPU runtime — no physical GPU required.
package wasm

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// BenchmarkGPUCapabilityCheck measures the latency of capability gate checks
// for GPU access (Module 51 → Module 53 permission boundary).
func BenchmarkGPUCapabilityCheck(b *testing.B) {
	svc := NewMockGPUService(capability.ModeSimulated).(*mockGPUService)
	validGrant := &Grant{GPU: &GPURule{AllowedDevices: []int{0, 1}}}
	nilGrant := (*Grant)(nil)
	noGPUGrant := NewDefaultGrant()
	ctx := context.Background()

	b.Run("valid-grant", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = svc.withCapabilityCheck(ctx, validGrant)
		}
	})

	b.Run("nil-grant-denied", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = svc.withCapabilityCheck(ctx, nilGrant)
		}
	})

	b.Run("no-gpu-grant-denied", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = svc.withCapabilityCheck(ctx, noGPUGrant)
		}
	})
}

// BenchmarkKernelDispatchValidation measures the full path latency of:
// capability check → device lookup → simulated kernel dispatch readiness.
// This represents the validation overhead before a real kernel would be dispatched.
func BenchmarkKernelDispatchValidation(b *testing.B) {
	svc := NewMockGPUService(capability.ModeSimulated)
	ctx := context.Background()

	b.Run("device-info-lookup", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = svc.DeviceInfo(ctx, 0)
		}
	})

	b.Run("device-info-invalid-idx", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = svc.DeviceInfo(ctx, 999) // error path
		}
	})

	b.Run("nvlink-topology-query", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = svc.NVLinkTopology(ctx)
		}
	})

	b.Run("device-count", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = svc.DeviceCount(ctx)
		}
	})
}

// BenchmarkGPUMemoryAllocation measures alloc/free cycle latency
// for the simulated GPU VRAM handle tracker.
func BenchmarkGPUMemoryAllocation(b *testing.B) {
	svc := NewMockGPUService(capability.ModeSimulated)
	defer svc.Close()
	ctx := context.Background()

	b.Run("alloc-4KB", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			h, _ := svc.Alloc(ctx, 4096)
			_ = svc.Free(ctx, h)
		}
	})

	b.Run("alloc-1MB", func(b *testing.B) {
		svc2 := NewMockGPUService(capability.ModeSimulated)
		defer svc2.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			h, _ := svc2.Alloc(ctx, 1024*1024)
			_ = svc2.Free(ctx, h)
		}
	})

	b.Run("alloc-1GB", func(b *testing.B) {
		svc2 := NewMockGPUService(capability.ModeSimulated)
		defer svc2.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			h, _ := svc2.Alloc(ctx, 1024*1024*1024)
			_ = svc2.Free(ctx, h)
		}
	})

	b.Run("alloc-oversized-rejected", func(b *testing.B) {
		svc2 := NewMockGPUService(capability.ModeSimulated)
		defer svc2.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = svc2.Alloc(ctx, 100*1024*1024*1024) // error path
		}
	})

	b.Run("alloc-zero-rejected", func(b *testing.B) {
		svc2 := NewMockGPUService(capability.ModeSimulated)
		defer svc2.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = svc2.Alloc(ctx, 0) // error path
		}
	})

	b.Run("free-invalid-handle", func(b *testing.B) {
		svc2 := NewMockGPUService(capability.ModeSimulated)
		defer svc2.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = svc2.Free(ctx, 99999) // error path
		}
	})

	b.Run("batch-alloc-free-100", func(b *testing.B) {
		svc2 := NewMockGPUService(capability.ModeSimulated)
		defer svc2.Close()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			handles := make([]uint64, 100)
			for j := 0; j < 100; j++ {
				h, _ := svc2.Alloc(ctx, 4096)
				handles[j] = h
			}
			for _, h := range handles {
				_ = svc2.Free(ctx, h)
			}
		}
	})
}

// BenchmarkWASIGPUModuleLoad measures the cost of constructing the mock GPU service
// (simulating module initialization + device discovery path).
func BenchmarkWASIGPUModuleLoad(b *testing.B) {
	b.Run("simulated-mode-init", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			svc := NewMockGPUService(capability.ModeSimulated)
			svc.Close()
		}
	})

	b.Run("real-mode-init-fallback", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// ModeReal falls back to simulated (no driver) — measures fallback path
			svc := NewMockGPUService(capability.ModeReal)
			svc.Close()
		}
	})

	b.Run("full-lifecycle", func(b *testing.B) {
		b.ReportAllocs()
		ctx := context.Background()
		for i := 0; i < b.N; i++ {
			svc := NewMockGPUService(capability.ModeSimulated)
			_, _ = svc.DeviceCount(ctx)
			_, _ = svc.DeviceInfo(ctx, 0)
			_, _ = svc.NVLinkTopology(ctx)
			h, _ := svc.Alloc(ctx, 1024*1024)
			_ = svc.Free(ctx, h)
			svc.Close()
		}
	})
}
