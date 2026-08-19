// Package wasm — Tests for Module 53: GPU WASI extensions.
package wasm

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

func TestMockGPUService_DeviceCount(t *testing.T) {
	svc := NewMockGPUService(capability.ModeSimulated)
	defer svc.Close()

	count, err := svc.DeviceCount(context.Background())
	if err != nil {
		t.Fatalf("DeviceCount failed: %v", err)
	}
	if count == 0 {
		t.Error("Expected non-zero device count in mock mode")
	}
}

func TestMockGPUService_DeviceInfo(t *testing.T) {
	svc := NewMockGPUService(capability.ModeSimulated)
	defer svc.Close()

	info, err := svc.DeviceInfo(context.Background(), 0)
	if err != nil {
		t.Fatalf("DeviceInfo(0) failed: %v", err)
	}
	if info == nil {
		t.Fatal("DeviceInfo returned nil")
	}

	// HONESTY REQUIREMENT: mock mode MUST report Simulated=true
	if !info.Simulated {
		t.Error("Mock GPU device MUST have Simulated=true to avoid faking real hardware")
	}

	// Test invalid index
	_, err = svc.DeviceInfo(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for out-of-range device index")
	}
}

func TestMockGPUService_NVLinkTopology(t *testing.T) {
	svc := NewMockGPUService(capability.ModeSimulated)
	defer svc.Close()

	graph, err := svc.NVLinkTopology(context.Background())
	if err != nil {
		t.Fatalf("NVLinkTopology failed: %v", err)
	}
	if graph == nil {
		t.Fatal("Topology returned nil")
	}

	// HONESTY: mock mode topology MUST be marked simulated
	if !graph.Simulated {
		t.Error("Mock topology MUST have Simulated=true")
	}

	if graph.Totals.TotalGPUs == 0 {
		t.Error("Expected non-zero GPU count in topology")
	}
}

func TestMockGPUService_AllocFree(t *testing.T) {
	svc := NewMockGPUService(capability.ModeSimulated)
	defer svc.Close()

	// Alloc valid size
	handle, err := svc.Alloc(context.Background(), 1024*1024) // 1MB
	if err != nil {
		t.Fatalf("Alloc failed: %v", err)
	}
	if handle == 0 {
		t.Error("Alloc returned zero handle")
	}

	// Free it
	err = svc.Free(context.Background(), handle)
	if err != nil {
		t.Errorf("Free failed: %v", err)
	}

	// Double-free should fail
	err = svc.Free(context.Background(), handle)
	if err == nil {
		t.Error("Expected double-free error")
	}

	// Invalid alloc size (too big)
	_, err = svc.Alloc(context.Background(), 100*1024*1024*1024) // 100GB
	if err == nil {
		t.Error("Expected error for oversized allocation")
	}

	// Zero-byte alloc should fail
	_, err = svc.Alloc(context.Background(), 0)
	if err == nil {
		t.Error("Expected error for zero-byte allocation")
	}
}

func TestGPUCapabilityGate(t *testing.T) {
	svc := NewMockGPUService(capability.ModeSimulated).(*mockGPUService)

	// nil grant → denied
	err := svc.withCapabilityCheck(context.Background(), nil)
	if err == nil {
		t.Error("Expected denial for nil grant")
	}

	// grant without GPU rule → denied
	emptyGrant := NewDefaultGrant()
	err = svc.withCapabilityCheck(context.Background(), emptyGrant)
	if err == nil {
		t.Error("Expected denial for grant without GPU access")
	}

	// grant WITH GPU rule → allowed
	gpuGrant := &Grant{GPU: &GPURule{AllowedDevices: []int{0}}}
	err = svc.withCapabilityCheck(context.Background(), gpuGrant)
	if err != nil {
		t.Errorf("Expected GPU access with valid grant, got: %v", err)
	}
}

func TestMapToSchedulerGPUNode(t *testing.T) {
	svc := NewMockGPUService(capability.ModeSimulated).(*mockGPUService)
	node := svc.MapToSchedulerGPUNode("node-test")

	if node.Name != "node-test" {
		t.Errorf("Node name mismatch: got %q", node.Name)
	}
}

func BenchmarkGPUAlloc(b *testing.B) {
	svc := NewMockGPUService(capability.ModeSimulated)
	defer svc.Close()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h, _ := svc.Alloc(ctx, 4096)
		_ = svc.Free(ctx, h)
	}
}
