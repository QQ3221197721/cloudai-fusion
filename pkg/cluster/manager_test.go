package cluster

import (
	"context"
	"testing"
)

func TestNewManager(t *testing.T) {
	mgr, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}
}

func TestImportAndListClusters(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	ctx := context.Background()

	// Initially empty
	clusters, err := mgr.ListClusters(ctx)
	if err != nil {
		t.Fatalf("ListClusters failed: %v", err)
	}
	if len(clusters) != 0 {
		t.Error("should have no clusters initially")
	}

	// Import cluster
	cl, err := mgr.ImportCluster(ctx, &ImportClusterRequest{
		Name:       "test-cluster",
		Provider:   "aws",
		Region:     "us-east-1",
		KubeConfig: "fake-kubeconfig",
		Labels:     map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("ImportCluster failed: %v", err)
	}
	if cl.ID == "" {
		t.Error("cluster ID should not be empty")
	}
	if cl.Name != "test-cluster" {
		t.Errorf("expected name 'test-cluster', got '%s'", cl.Name)
	}

	// List again
	clusters, _ = mgr.ListClusters(ctx)
	if len(clusters) != 1 {
		t.Errorf("expected 1 cluster, got %d", len(clusters))
	}
}

func TestGetCluster(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	ctx := context.Background()

	cl, _ := mgr.ImportCluster(ctx, &ImportClusterRequest{
		Name: "lookup-test", Provider: "aliyun", Region: "cn-hangzhou", KubeConfig: "fake",
	})

	found, err := mgr.GetCluster(ctx, cl.ID)
	if err != nil {
		t.Fatalf("GetCluster failed: %v", err)
	}
	if found.Name != "lookup-test" {
		t.Errorf("expected name 'lookup-test', got '%s'", found.Name)
	}

	_, err = mgr.GetCluster(ctx, "nonexistent-id")
	if err == nil {
		t.Error("GetCluster should fail for nonexistent ID")
	}
}

func TestDeleteCluster(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	ctx := context.Background()

	cl, _ := mgr.ImportCluster(ctx, &ImportClusterRequest{
		Name: "delete-test", Provider: "azure", Region: "westus2", KubeConfig: "fake",
	})

	err := mgr.DeleteCluster(ctx, cl.ID)
	if err != nil {
		t.Fatalf("DeleteCluster failed: %v", err)
	}

	_, err = mgr.GetCluster(ctx, cl.ID)
	if err == nil {
		t.Error("cluster should not be found after deletion")
	}

	err = mgr.DeleteCluster(ctx, "nonexistent")
	if err == nil {
		t.Error("DeleteCluster should fail for nonexistent ID")
	}
}

func TestGetResourceSummary(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	ctx := context.Background()

	summary, err := mgr.GetResourceSummary(ctx)
	if err != nil {
		t.Fatalf("GetResourceSummary failed: %v", err)
	}
	if summary == nil {
		t.Fatal("summary should not be nil")
	}
}

func TestGetClusterHealth(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	ctx := context.Background()

	cl, _ := mgr.ImportCluster(ctx, &ImportClusterRequest{
		Name: "health-test", Provider: "gcp", Region: "us-central1", KubeConfig: "fake",
	})

	health, err := mgr.GetClusterHealth(ctx, cl.ID)
	if err != nil {
		t.Fatalf("GetClusterHealth failed: %v", err)
	}
	if health == nil {
		t.Fatal("health should not be nil")
	}
}
