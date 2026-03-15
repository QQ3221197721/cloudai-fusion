package wasm

import (
	"context"
	"testing"
)

func TestNewManager_Defaults(t *testing.T) {
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.config.DefaultRuntime != RuntimeSpin {
		t.Errorf("expected default runtime %q, got %q", RuntimeSpin, mgr.config.DefaultRuntime)
	}
	if mgr.config.MaxInstances != 1000 {
		t.Errorf("expected 1000 max instances, got %d", mgr.config.MaxInstances)
	}
	if mgr.config.MemoryLimitMB != 128 {
		t.Errorf("expected 128MB memory limit, got %d", mgr.config.MemoryLimitMB)
	}
}

func TestNewManager_CustomConfig(t *testing.T) {
	mgr, err := NewManager(Config{
		DefaultRuntime: RuntimeWasmtime,
		MaxInstances:   500,
		MemoryLimitMB:  256,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.config.DefaultRuntime != RuntimeWasmtime {
		t.Errorf("expected %q, got %q", RuntimeWasmtime, mgr.config.DefaultRuntime)
	}
	if mgr.config.MaxInstances != 500 {
		t.Errorf("expected 500, got %d", mgr.config.MaxInstances)
	}
}

func TestRegisterModule(t *testing.T) {
	mgr, _ := NewManager(Config{})
	module := &WasmModule{
		Name:         "hello-wasm",
		Version:      "1.0.0",
		Runtime:      RuntimeSpin,
		SourceURL:    "oci://registry/hello:v1",
		Size:         1024000,
		Capabilities: []string{"http"},
	}
	err := mgr.RegisterModule(context.Background(), module)
	if err != nil {
		t.Fatalf("RegisterModule failed: %v", err)
	}
	if module.ID == "" {
		t.Error("expected ID to be assigned")
	}
	if module.UploadedAt.IsZero() {
		t.Error("expected UploadedAt to be set")
	}
	if len(mgr.modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(mgr.modules))
	}
}

func TestDeploy(t *testing.T) {
	mgr, _ := NewManager(Config{})
	req := &WasmDeployRequest{
		ModuleID:  "mod-1",
		ClusterID: "cluster-1",
		Replicas:  3,
		Runtime:   RuntimeWasmEdge,
	}
	instances, err := mgr.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if len(instances) != 3 {
		t.Fatalf("expected 3 instances, got %d", len(instances))
	}
	for _, inst := range instances {
		if inst.ID == "" {
			t.Error("expected instance ID")
		}
		if inst.Runtime != RuntimeWasmEdge {
			t.Errorf("expected runtime %q, got %q", RuntimeWasmEdge, inst.Runtime)
		}
		if inst.Status != WasmStatusRunning {
			t.Errorf("expected status %q, got %q", WasmStatusRunning, inst.Status)
		}
	}
}

func TestDeployDefaultRuntime(t *testing.T) {
	mgr, _ := NewManager(Config{})
	req := &WasmDeployRequest{
		ModuleID:  "mod-1",
		ClusterID: "cluster-1",
		Replicas:  1,
	}
	instances, err := mgr.Deploy(context.Background(), req)
	if err != nil {
		t.Fatalf("Deploy failed: %v", err)
	}
	if instances[0].Runtime != RuntimeSpin {
		t.Errorf("expected default runtime %q, got %q", RuntimeSpin, instances[0].Runtime)
	}
}

func TestListInstances(t *testing.T) {
	mgr, _ := NewManager(Config{})
	mgr.Deploy(context.Background(), &WasmDeployRequest{
		ModuleID: "m1", ClusterID: "c1", Replicas: 2,
	})
	instances, err := mgr.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(instances))
	}
}

func TestGetMetrics_Empty(t *testing.T) {
	mgr, _ := NewManager(Config{})
	metrics, err := mgr.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}
	if metrics.TotalModules != 0 {
		t.Errorf("expected 0 modules, got %d", metrics.TotalModules)
	}
	if metrics.RunningInstances != 0 {
		t.Errorf("expected 0 instances, got %d", metrics.RunningInstances)
	}
}

func TestGetMetrics_WithInstances(t *testing.T) {
	mgr, _ := NewManager(Config{})
	mgr.RegisterModule(context.Background(), &WasmModule{Name: "mod1"})
	mgr.Deploy(context.Background(), &WasmDeployRequest{
		ModuleID: "m1", ClusterID: "c1", Replicas: 3,
	})
	metrics, err := mgr.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics failed: %v", err)
	}
	if metrics.TotalModules != 1 {
		t.Errorf("expected 1 module, got %d", metrics.TotalModules)
	}
	if metrics.RunningInstances != 3 {
		t.Errorf("expected 3 running, got %d", metrics.RunningInstances)
	}
	if metrics.AvgColdStartMs <= 0 {
		t.Error("expected positive avg cold start")
	}
	if metrics.AvgMemoryUsedKB <= 0 {
		t.Error("expected positive avg memory")
	}
}

func TestRuntimeTypeConstants(t *testing.T) {
	tests := map[RuntimeType]string{
		RuntimeWasmEdge:  "wasmedge",
		RuntimeWasmtime:  "wasmtime",
		RuntimeSpin:      "spin",
		RuntimeWasmCloud: "wasmcloud",
	}
	for rt, expected := range tests {
		if string(rt) != expected {
			t.Errorf("RuntimeType %q != %q", rt, expected)
		}
	}
}

func TestWorkloadStatusConstants(t *testing.T) {
	tests := map[WorkloadStatus]string{
		WasmStatusPending: "pending",
		WasmStatusRunning: "running",
		WasmStatusStopped: "stopped",
		WasmStatusFailed:  "failed",
	}
	for ws, expected := range tests {
		if string(ws) != expected {
			t.Errorf("WorkloadStatus %q != %q", ws, expected)
		}
	}
}
