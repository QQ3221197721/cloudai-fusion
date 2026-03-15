package mesh

import (
	"context"
	"testing"
)

func TestNewManager_DefaultAmbient(t *testing.T) {
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.config.Mode != MeshModeAmbient {
		t.Errorf("expected default mode %q, got %q", MeshModeAmbient, mgr.config.Mode)
	}
	if mgr.config.TraceSampleRate != 0.1 {
		t.Errorf("expected trace sample rate 0.1, got %f", mgr.config.TraceSampleRate)
	}
}

func TestNewManager_CiliumMode(t *testing.T) {
	mgr, err := NewManager(Config{Mode: MeshModeCilium, TraceSampleRate: 0.5})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.config.Mode != MeshModeCilium {
		t.Errorf("expected %q, got %q", MeshModeCilium, mgr.config.Mode)
	}
	if mgr.config.TraceSampleRate != 0.5 {
		t.Errorf("expected 0.5, got %f", mgr.config.TraceSampleRate)
	}
}

func TestGetStatus_Ambient(t *testing.T) {
	mgr, _ := NewManager(Config{Mode: MeshModeAmbient, EnableMTLS: true})
	status, err := mgr.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Mode != MeshModeAmbient {
		t.Errorf("expected ambient mode")
	}
	if !status.Healthy {
		t.Error("expected healthy")
	}
	if !status.MTLSEnabled {
		t.Error("expected mTLS enabled")
	}
	if status.ZTunnelStatus != "running" {
		t.Errorf("expected ztunnel running, got %q", status.ZTunnelStatus)
	}
	if status.WaypointStatus != "running" {
		t.Errorf("expected waypoint running, got %q", status.WaypointStatus)
	}
	if status.Version != "istio-1.27.0-ambient" {
		t.Errorf("unexpected version: %q", status.Version)
	}
}

func TestGetStatus_Cilium(t *testing.T) {
	mgr, _ := NewManager(Config{Mode: MeshModeCilium})
	status, err := mgr.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Version != "cilium-1.16.0" {
		t.Errorf("expected cilium version, got %q", status.Version)
	}
	if status.CiliumAgentCount != 8 {
		t.Errorf("expected 8 cilium agents, got %d", status.CiliumAgentCount)
	}
	if status.EBPFProgramCount != 156 {
		t.Errorf("expected 156 eBPF programs, got %d", status.EBPFProgramCount)
	}
}

func TestDefaultPolicies(t *testing.T) {
	mgr, _ := NewManager(Config{})
	policies, err := mgr.ListPolicies(context.Background())
	if err != nil {
		t.Fatalf("ListPolicies failed: %v", err)
	}
	if len(policies) != 3 {
		t.Fatalf("expected 3 default policies, got %d", len(policies))
	}
	names := map[string]bool{}
	for _, p := range policies {
		names[p.Name] = true
	}
	for _, expected := range []string{"default-deny-all", "allow-dns-egress", "allow-prometheus-scrape"} {
		if !names[expected] {
			t.Errorf("missing default policy: %s", expected)
		}
	}
}

func TestCreatePolicy(t *testing.T) {
	mgr, _ := NewManager(Config{})
	policy := &NetworkPolicy{
		Name:        "test-policy",
		Namespace:   "test-ns",
		Type:        "ingress",
		Enforcement: "enforce",
	}
	err := mgr.CreatePolicy(context.Background(), policy)
	if err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}
	if policy.ID == "" {
		t.Error("expected ID to be assigned")
	}
	if policy.Status != "active" {
		t.Errorf("expected status 'active', got %q", policy.Status)
	}
	policies, _ := mgr.ListPolicies(context.Background())
	if len(policies) != 4 {
		t.Fatalf("expected 4 policies, got %d", len(policies))
	}
}

func TestGetTrafficMetrics(t *testing.T) {
	mgr, _ := NewManager(Config{})
	metrics, err := mgr.GetTrafficMetrics(context.Background(), "cloudai-fusion")
	if err != nil {
		t.Fatalf("GetTrafficMetrics failed: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 traffic metrics, got %d", len(metrics))
	}
	if metrics[0].SourcePod != "apiserver-7b9f4c" {
		t.Errorf("unexpected source pod: %q", metrics[0].SourcePod)
	}
	if !metrics[0].Encrypted {
		t.Error("expected encrypted traffic")
	}
	if metrics[1].ErrorCount != 0 {
		t.Errorf("expected 0 errors, got %d", metrics[1].ErrorCount)
	}
}

func TestEnableMTLS(t *testing.T) {
	mgr, _ := NewManager(Config{})
	if mgr.config.EnableMTLS {
		t.Error("mTLS should be disabled initially")
	}
	err := mgr.EnableMTLS(context.Background(), true)
	if err != nil {
		t.Fatalf("EnableMTLS failed: %v", err)
	}
	if !mgr.config.EnableMTLS {
		t.Error("mTLS should be enabled after call")
	}
}

func TestMeshModeConstants(t *testing.T) {
	if string(MeshModeAmbient) != "ambient" {
		t.Error("MeshModeAmbient mismatch")
	}
	if string(MeshModeCilium) != "cilium" {
		t.Error("MeshModeCilium mismatch")
	}
	if string(MeshModeDisabled) != "disabled" {
		t.Error("MeshModeDisabled mismatch")
	}
}
