package edge

import (
	"context"
	"testing"
	"time"
)

func TestNewManager_Defaults(t *testing.T) {
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.config.MaxEdgePowerWatts != 200 {
		t.Errorf("expected 200W default, got %d", mgr.config.MaxEdgePowerWatts)
	}
	if mgr.config.SyncInterval != 30*time.Second {
		t.Errorf("expected 30s sync interval, got %v", mgr.config.SyncInterval)
	}
}

func TestNewManager_CustomConfig(t *testing.T) {
	mgr, err := NewManager(Config{MaxEdgePowerWatts: 300, SyncInterval: time.Minute})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.config.MaxEdgePowerWatts != 300 {
		t.Errorf("expected 300W, got %d", mgr.config.MaxEdgePowerWatts)
	}
	if mgr.config.SyncInterval != time.Minute {
		t.Errorf("expected 1m, got %v", mgr.config.SyncInterval)
	}
}

func TestRegisterNode(t *testing.T) {
	mgr, _ := NewManager(Config{})
	node := &EdgeNode{
		Name:     "edge-beijing-01",
		Region:   "cn-beijing",
		Tier:     TierEdge,
		CPUCores: 16,
		MemoryGB: 64,
		GPUType:  "nvidia-l40s",
		GPUCount: 1,
	}
	err := mgr.RegisterNode(context.Background(), node)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}
	if node.ID == "" {
		t.Error("expected ID to be assigned")
	}
	if node.Status != EdgeNodeOnline {
		t.Errorf("expected status %q, got %q", EdgeNodeOnline, node.Status)
	}
	if node.RegisteredAt.IsZero() {
		t.Error("expected RegisteredAt to be set")
	}
}

func TestListNodes(t *testing.T) {
	mgr, _ := NewManager(Config{})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "cloud-1", Tier: TierCloud})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "edge-1", Tier: TierEdge})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "edge-2", Tier: TierEdge})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "terminal-1", Tier: TierTerminal})

	all, _ := mgr.ListNodes(context.Background(), "")
	if len(all) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(all))
	}

	edges, _ := mgr.ListNodes(context.Background(), TierEdge)
	if len(edges) != 2 {
		t.Fatalf("expected 2 edge nodes, got %d", len(edges))
	}

	clouds, _ := mgr.ListNodes(context.Background(), TierCloud)
	if len(clouds) != 1 {
		t.Fatalf("expected 1 cloud node, got %d", len(clouds))
	}
}

func TestDeployModel(t *testing.T) {
	mgr, _ := NewManager(Config{})
	node := &EdgeNode{Name: "edge-1", Tier: TierEdge}
	mgr.RegisterNode(context.Background(), node)

	model, err := mgr.DeployModel(context.Background(), &EdgeDeployRequest{
		ModelID:          "m-1",
		ModelName:        "llama-7b",
		ParameterCount:   "7B",
		EdgeNodeID:       node.ID,
		Framework:        "pytorch",
		QuantizationType: "INT4",
	})
	if err != nil {
		t.Fatalf("DeployModel failed: %v", err)
	}
	if model.ModelName != "llama-7b" {
		t.Errorf("expected llama-7b, got %q", model.ModelName)
	}
	if model.Status != "deploying" {
		t.Errorf("expected 'deploying', got %q", model.Status)
	}
}

func TestDeployModel_NodeNotFound(t *testing.T) {
	mgr, _ := NewManager(Config{})
	_, err := mgr.DeployModel(context.Background(), &EdgeDeployRequest{
		EdgeNodeID: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestDeployModel_PowerBudgetExceeded(t *testing.T) {
	mgr, _ := NewManager(Config{MaxEdgePowerWatts: 200})
	node := &EdgeNode{Name: "edge-1", Tier: TierEdge}
	mgr.RegisterNode(context.Background(), node)

	_, err := mgr.DeployModel(context.Background(), &EdgeDeployRequest{
		ModelID:       "m-1",
		ModelName:     "big-model",
		ParameterCount: "50B",
		EdgeNodeID:    node.ID,
		MaxPowerWatts: 350,
	})
	if err == nil {
		t.Fatal("expected power budget error")
	}
}

func TestHeartbeat(t *testing.T) {
	mgr, _ := NewManager(Config{})
	node := &EdgeNode{Name: "edge-1", Tier: TierEdge}
	mgr.RegisterNode(context.Background(), node)

	usage := &EdgeResourceUsage{
		CPUPercent: 45.0, MemoryPercent: 60.0,
		GPUPercent: 80.0, PowerWatts: 150.0,
		Temperature: 65.0,
	}
	err := mgr.Heartbeat(context.Background(), node.ID, usage)
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
	if node.ResourceUsage == nil {
		t.Fatal("expected resource usage to be set")
	}
	if node.CurrentPowerWatts != 150.0 {
		t.Errorf("expected 150W, got %f", node.CurrentPowerWatts)
	}
}

func TestHeartbeat_NodeNotFound(t *testing.T) {
	mgr, _ := NewManager(Config{})
	err := mgr.Heartbeat(context.Background(), "nonexistent", &EdgeResourceUsage{})
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestGetTopologySummary(t *testing.T) {
	mgr, _ := NewManager(Config{})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "c1", Tier: TierCloud})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "e1", Tier: TierEdge})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "e2", Tier: TierEdge})
	mgr.RegisterNode(context.Background(), &EdgeNode{Name: "t1", Tier: TierTerminal})

	summary := mgr.GetTopologySummary(context.Background())
	if summary["cloud_nodes"] != 1 {
		t.Errorf("expected 1 cloud node, got %v", summary["cloud_nodes"])
	}
	if summary["edge_nodes"] != 2 {
		t.Errorf("expected 2 edge nodes, got %v", summary["edge_nodes"])
	}
	if summary["terminal_nodes"] != 1 {
		t.Errorf("expected 1 terminal node, got %v", summary["terminal_nodes"])
	}
	if summary["total_nodes"] != 4 {
		t.Errorf("expected 4 total, got %v", summary["total_nodes"])
	}
}

func TestDefaultSyncPolicies(t *testing.T) {
	mgr, _ := NewManager(Config{})
	if len(mgr.syncPolicies) != 4 {
		t.Fatalf("expected 4 default sync policies, got %d", len(mgr.syncPolicies))
	}
}

func TestNodeTierConstants(t *testing.T) {
	if string(TierCloud) != "cloud" {
		t.Error("TierCloud mismatch")
	}
	if string(TierEdge) != "edge" {
		t.Error("TierEdge mismatch")
	}
	if string(TierTerminal) != "terminal" {
		t.Error("TierTerminal mismatch")
	}
}

func TestEdgeNodeStatusConstants(t *testing.T) {
	if string(EdgeNodeOnline) != "online" {
		t.Error("EdgeNodeOnline mismatch")
	}
	if string(EdgeNodeOffline) != "offline" {
		t.Error("EdgeNodeOffline mismatch")
	}
	if string(EdgeNodeDegraded) != "degraded" {
		t.Error("EdgeNodeDegraded mismatch")
	}
	if string(EdgeNodeMaintenance) != "maintenance" {
		t.Error("EdgeNodeMaintenance mismatch")
	}
}
