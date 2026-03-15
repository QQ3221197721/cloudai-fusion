package cloud

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
)

// =============================================================================
// Aliyun Provider Mock Tests (nil-client paths)
// =============================================================================

func TestAliyunProvider_NilClient_GetCluster(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	_, err := p.GetCluster(context.Background(), "c-123")
	if err == nil || !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error, got: %v", err)
	}
}

func TestAliyunProvider_NilClient_CreateCluster(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	_, err := p.CreateCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil || !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error, got: %v", err)
	}
}

func TestAliyunProvider_NilClient_DeleteCluster(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	err := p.DeleteCluster(context.Background(), "c-123")
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestAliyunProvider_NilClient_ScaleCluster(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	err := p.ScaleCluster(context.Background(), "c-123", 5)
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestAliyunProvider_NilClient_GetKubeConfig(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	_, err := p.GetKubeConfig(context.Background(), "c-123")
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestAliyunProvider_NilClient_ListNodes(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	nodes, err := p.ListNodes(context.Background(), "c-123")
	if err != nil {
		t.Errorf("ListNodes should return empty slice for nil client, got err: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestAliyunProvider_GetNodeMetrics(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	m, err := p.GetNodeMetrics(context.Background(), "c-123", "node-1")
	if err == nil {
		t.Error("GetNodeMetrics should return an error (requires CloudMonitor)")
	}
	if m == nil || m.NodeID != "node-1" {
		t.Error("should still return NodeMetrics with NodeID set")
	}
}

func TestAliyunProvider_ListGPUInstances(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	instances, err := p.ListGPUInstances(context.Background())
	if err != nil {
		t.Fatalf("ListGPUInstances should succeed: %v", err)
	}
	if len(instances) < 1 {
		t.Error("should return at least 1 GPU instance type")
	}
	foundA100 := false
	for _, inst := range instances {
		if inst.GPUType == "nvidia-a100" {
			foundA100 = true
			if inst.GPUMemoryGB != 80 {
				t.Errorf("A100 GPU memory = %f, want 80", inst.GPUMemoryGB)
			}
		}
	}
	if !foundA100 {
		t.Error("should include nvidia-a100 instance type")
	}
}

func TestAliyunProvider_GetGPUPricing(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	pricing, err := p.GetGPUPricing(context.Background(), "nvidia-a100")
	if err != nil {
		t.Fatalf("GetGPUPricing should succeed: %v", err)
	}
	if pricing.GPUType != "nvidia-a100" {
		t.Errorf("GPUType = %q", pricing.GPUType)
	}
	if pricing.Currency != "CNY" {
		t.Errorf("Currency = %q, want CNY", pricing.Currency)
	}
	if pricing.OnDemandPrice <= 0 {
		t.Error("OnDemandPrice should be positive")
	}
	if pricing.SpotPrice <= 0 {
		t.Error("SpotPrice should be positive")
	}
}

func TestAliyunProvider_GetCostSummary(t *testing.T) {
	p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "aliyun-mock", Region: "cn-hangzhou"})
	cost, err := p.GetCostSummary(context.Background(), "2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatalf("GetCostSummary should succeed: %v", err)
	}
	if cost.Currency != "CNY" {
		t.Errorf("Currency = %q, want CNY", cost.Currency)
	}
}

// =============================================================================
// AWS Provider Mock Tests (nil-client paths)
// =============================================================================

func TestAWSProvider_NilClient_GetCluster(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	_, err := p.GetCluster(context.Background(), "my-cluster")
	if err == nil || !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error, got: %v", err)
	}
}

func TestAWSProvider_NilClient_CreateCluster(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	_, err := p.CreateCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestAWSProvider_NilClient_DeleteCluster(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	err := p.DeleteCluster(context.Background(), "my-cluster")
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestAWSProvider_NilClient_ScaleCluster(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	err := p.ScaleCluster(context.Background(), "my-cluster", 5)
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestAWSProvider_NilClient_GetKubeConfig(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	_, err := p.GetKubeConfig(context.Background(), "my-cluster")
	if err == nil {
		t.Error("expected error for nil client")
	}
}

func TestAWSProvider_NilClient_ListNodes(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	nodes, err := p.ListNodes(context.Background(), "my-cluster")
	if err != nil {
		t.Errorf("ListNodes should return empty for nil client: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestAWSProvider_ListGPUInstances(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	instances, err := p.ListGPUInstances(context.Background())
	if err != nil {
		t.Fatalf("ListGPUInstances should succeed: %v", err)
	}
	if len(instances) < 2 {
		t.Error("should return at least 2 GPU instance types")
	}
	foundH100 := false
	for _, inst := range instances {
		if inst.GPUType == "nvidia-h100" {
			foundH100 = true
			if !inst.SpotAvailable {
				t.Error("H100 should have spot available")
			}
			if inst.GPUCount != 8 {
				t.Errorf("H100 GPU count = %d, want 8", inst.GPUCount)
			}
		}
	}
	if !foundH100 {
		t.Error("should include nvidia-h100")
	}
}

func TestAWSProvider_GetGPUPricing(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	pricing, err := p.GetGPUPricing(context.Background(), "nvidia-a100")
	if err != nil {
		t.Fatalf("GetGPUPricing should succeed: %v", err)
	}
	if pricing.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", pricing.Currency)
	}
	if pricing.SpotPrice >= pricing.OnDemandPrice {
		t.Error("spot price should be less than on-demand")
	}
}

func TestAWSProvider_GetNodeMetrics(t *testing.T) {
	p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "aws-mock", Region: "us-west-2"})
	m, err := p.GetNodeMetrics(context.Background(), "c-1", "node-1")
	if err == nil {
		t.Error("should return CloudWatch integration error")
	}
	if m == nil || m.NodeID != "node-1" {
		t.Error("should return NodeMetrics with NodeID")
	}
}

// =============================================================================
// Azure Provider Mock Tests (nil-client paths)
// =============================================================================

func TestAzureProvider_NilClient_GetCluster(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	_, err := p.GetCluster(context.Background(), "aks-1")
	if err == nil || !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error, got: %v", err)
	}
}

func TestAzureProvider_NilClient_CreateCluster(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	_, err := p.CreateCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestAzureProvider_NilClient_DeleteCluster(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	err := p.DeleteCluster(context.Background(), "aks-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAzureProvider_NilClient_ScaleCluster(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	err := p.ScaleCluster(context.Background(), "aks-1", 3)
	if err == nil {
		t.Error("expected error")
	}
}

func TestAzureProvider_NilClient_GetKubeConfig(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	_, err := p.GetKubeConfig(context.Background(), "aks-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAzureProvider_NilClient_ListNodes(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	nodes, err := p.ListNodes(context.Background(), "aks-1")
	if err != nil {
		t.Errorf("ListNodes should return empty for nil client: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestAzureProvider_DefaultResourceGroup(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	// When no resource_group in Extra, should default to "cloudai-fusion-rg"
	if p.resourceGroup != "cloudai-fusion-rg" {
		t.Errorf("resourceGroup = %q, want cloudai-fusion-rg", p.resourceGroup)
	}
}

func TestAzureProvider_CustomResourceGroup(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name:   "azure-mock",
		Region: "eastus",
		Extra:  map[string]string{"resource_group": "my-rg"},
	})
	if p.resourceGroup != "my-rg" {
		t.Errorf("resourceGroup = %q, want my-rg", p.resourceGroup)
	}
}

func TestAzureProvider_ListGPUInstances(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	instances, err := p.ListGPUInstances(context.Background())
	if err != nil {
		t.Fatalf("ListGPUInstances should succeed: %v", err)
	}
	if len(instances) < 1 {
		t.Error("should return at least 1 GPU instance type")
	}
}

func TestAzureProvider_GetCostSummary(t *testing.T) {
	p, _ := NewAzureProvider(config.CloudProviderConfig{
		Name: "azure-mock", Region: "eastus", Extra: map[string]string{},
	})
	cost, err := p.GetCostSummary(context.Background(), "2026-01-01", "2026-01-31")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cost.Currency != "USD" {
		t.Errorf("Currency = %q", cost.Currency)
	}
}

// =============================================================================
// GCP Provider Mock Tests (nil-client paths)
// =============================================================================

func TestGCPProvider_NilClient_GetCluster(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	_, err := p.GetCluster(context.Background(), "gke-1")
	if err == nil || !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error, got: %v", err)
	}
}

func TestGCPProvider_NilClient_CreateCluster(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	_, err := p.CreateCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestGCPProvider_NilClient_DeleteCluster(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	err := p.DeleteCluster(context.Background(), "gke-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestGCPProvider_NilClient_ScaleCluster(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	err := p.ScaleCluster(context.Background(), "gke-1", 5)
	if err == nil {
		t.Error("expected error")
	}
}

func TestGCPProvider_NilClient_GetKubeConfig(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	_, err := p.GetKubeConfig(context.Background(), "gke-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestGCPProvider_NilClient_ListNodes(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	nodes, err := p.ListNodes(context.Background(), "gke-1")
	if err != nil {
		t.Errorf("ListNodes should return empty: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestGCPProvider_ListGPUInstances(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	instances, err := p.ListGPUInstances(context.Background())
	if err != nil {
		t.Fatalf("ListGPUInstances should succeed: %v", err)
	}
	if len(instances) < 2 {
		t.Error("should return at least 2 GPU instance types (H100 + A100 + TPU)")
	}
	foundTPU := false
	for _, inst := range instances {
		if strings.Contains(inst.GPUType, "tpu") {
			foundTPU = true
		}
	}
	if !foundTPU {
		t.Error("GCP should include TPU instance type")
	}
}

func TestGCPProvider_GetGPUPricing(t *testing.T) {
	p, _ := NewGCPProvider(config.CloudProviderConfig{
		Name: "gcp-mock", Region: "us-central1", Extra: map[string]string{},
	})
	pricing, err := p.GetGPUPricing(context.Background(), "nvidia-h100")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pricing.GPUType != "nvidia-h100" {
		t.Errorf("GPUType = %q", pricing.GPUType)
	}
	if pricing.Currency != "USD" {
		t.Errorf("Currency = %q", pricing.Currency)
	}
}

func TestGCPProvider_ParentPath(t *testing.T) {
	client := &GCPAPIClient{projectID: "my-proj", location: "us-central1"}
	path := client.parentPath()
	expected := "projects/my-proj/locations/us-central1"
	if path != expected {
		t.Errorf("parentPath = %q, want %q", path, expected)
	}
}

// =============================================================================
// Tencent Provider Mock Tests (nil-client paths)
// =============================================================================

func TestTencentProvider_NilClient_GetCluster(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	_, err := p.GetCluster(context.Background(), "cls-1")
	if err == nil || !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error, got: %v", err)
	}
}

func TestTencentProvider_NilClient_CreateCluster(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	_, err := p.CreateCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestTencentProvider_NilClient_DeleteCluster(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	err := p.DeleteCluster(context.Background(), "cls-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestTencentProvider_NilClient_ScaleCluster(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	err := p.ScaleCluster(context.Background(), "cls-1", 5)
	if err == nil {
		t.Error("expected error")
	}
}

func TestTencentProvider_NilClient_GetKubeConfig(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	_, err := p.GetKubeConfig(context.Background(), "cls-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestTencentProvider_NilClient_ListNodes(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	nodes, err := p.ListNodes(context.Background(), "cls-1")
	if err != nil {
		t.Errorf("ListNodes should return empty: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestTencentProvider_ListGPUInstances(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	instances, err := p.ListGPUInstances(context.Background())
	if err != nil {
		t.Fatalf("ListGPUInstances should succeed: %v", err)
	}
	if len(instances) < 2 {
		t.Error("should return at least 2 GPU instance types")
	}
}

func TestTencentProvider_GetGPUPricing(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	pricing, err := p.GetGPUPricing(context.Background(), "nvidia-a100")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pricing.Currency != "CNY" {
		t.Errorf("Currency = %q, want CNY", pricing.Currency)
	}
}

func TestTencentProvider_GetNodeMetrics(t *testing.T) {
	p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "tc-mock", Region: "ap-guangzhou"})
	m, err := p.GetNodeMetrics(context.Background(), "c-1", "node-1")
	if err == nil {
		t.Error("should return Cloud Monitor error")
	}
	if m.NodeID != "node-1" {
		t.Errorf("NodeID = %q", m.NodeID)
	}
}

// =============================================================================
// Huawei Provider Mock Tests (nil-client paths)
// =============================================================================

func TestHuaweiProvider_NilClient_GetCluster(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	_, err := p.GetCluster(context.Background(), "cce-1")
	if err == nil || !strings.Contains(err.Error(), "credentials not configured") {
		t.Errorf("expected credentials error, got: %v", err)
	}
}

func TestHuaweiProvider_NilClient_CreateCluster(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	_, err := p.CreateCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}
}

func TestHuaweiProvider_NilClient_DeleteCluster(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	err := p.DeleteCluster(context.Background(), "cce-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestHuaweiProvider_NilClient_ScaleCluster(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	err := p.ScaleCluster(context.Background(), "cce-1", 5)
	if err == nil {
		t.Error("expected error")
	}
}

func TestHuaweiProvider_NilClient_GetKubeConfig(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	_, err := p.GetKubeConfig(context.Background(), "cce-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestHuaweiProvider_NilClient_ListNodes(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	nodes, err := p.ListNodes(context.Background(), "cce-1")
	if err != nil {
		t.Errorf("ListNodes should return empty: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestHuaweiProvider_ListGPUInstances(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	instances, err := p.ListGPUInstances(context.Background())
	if err != nil {
		t.Fatalf("ListGPUInstances should succeed: %v", err)
	}
	if len(instances) < 2 {
		t.Error("should return at least 2 GPU instance types")
	}
	foundAscend := false
	for _, inst := range instances {
		if strings.Contains(inst.GPUType, "ascend") {
			foundAscend = true
		}
	}
	if !foundAscend {
		t.Error("Huawei should include Ascend instance type")
	}
}

func TestHuaweiProvider_GetGPUPricing(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	pricing, err := p.GetGPUPricing(context.Background(), "huawei-ascend-910b")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if pricing.Currency != "CNY" {
		t.Errorf("Currency = %q, want CNY", pricing.Currency)
	}
}

func TestHuaweiProvider_GetNodeMetrics(t *testing.T) {
	p, _ := NewHuaweiProvider(config.CloudProviderConfig{
		Name: "hw-mock", Region: "cn-north-4", Extra: map[string]string{},
	})
	m, err := p.GetNodeMetrics(context.Background(), "c-1", "node-1")
	if err == nil {
		t.Error("should return Cloud Eye error")
	}
	if m.NodeID != "node-1" {
		t.Errorf("NodeID = %q", m.NodeID)
	}
}

// =============================================================================
// Provider Type & Identity Tests (all 6)
// =============================================================================

func TestAllProviderTypes(t *testing.T) {
	tests := []struct {
		providerType string
		expectedType common.CloudProviderType
		create       func() Provider
	}{
		{
			providerType: "aliyun",
			expectedType: common.CloudProviderAliyun,
			create: func() Provider {
				p, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "a", Region: "cn-hangzhou"})
				return p
			},
		},
		{
			providerType: "aws",
			expectedType: common.CloudProviderAWS,
			create: func() Provider {
				p, _ := NewAWSProvider(config.CloudProviderConfig{Name: "b", Region: "us-west-2"})
				return p
			},
		},
		{
			providerType: "azure",
			expectedType: common.CloudProviderAzure,
			create: func() Provider {
				p, _ := NewAzureProvider(config.CloudProviderConfig{Name: "c", Region: "eastus", Extra: map[string]string{}})
				return p
			},
		},
		{
			providerType: "gcp",
			expectedType: common.CloudProviderGCP,
			create: func() Provider {
				p, _ := NewGCPProvider(config.CloudProviderConfig{Name: "d", Region: "us-central1", Extra: map[string]string{}})
				return p
			},
		},
		{
			providerType: "huawei",
			expectedType: common.CloudProviderHuawei,
			create: func() Provider {
				p, _ := NewHuaweiProvider(config.CloudProviderConfig{Name: "e", Region: "cn-north-4", Extra: map[string]string{}})
				return p
			},
		},
		{
			providerType: "tencent",
			expectedType: common.CloudProviderTencent,
			create: func() Provider {
				p, _ := NewTencentProvider(config.CloudProviderConfig{Name: "f", Region: "ap-guangzhou"})
				return p
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.providerType, func(t *testing.T) {
			p := tt.create()
			if p.Type() != tt.expectedType {
				t.Errorf("Type() = %q, want %q", p.Type(), tt.expectedType)
			}
			if p.Name() == "" {
				t.Error("Name() should not be empty")
			}
			if p.Region() == "" {
				t.Error("Region() should not be empty")
			}
		})
	}
}

// =============================================================================
// Provider Ping Tests (all nil-client: should succeed silently)
// =============================================================================

func TestAllProviders_Ping_NilClient(t *testing.T) {
	providers := []Provider{}

	aliyun, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "a", Region: "cn-hangzhou"})
	providers = append(providers, aliyun)

	awsP, _ := NewAWSProvider(config.CloudProviderConfig{Name: "b", Region: "us-west-2"})
	providers = append(providers, awsP)

	azureP, _ := NewAzureProvider(config.CloudProviderConfig{Name: "c", Region: "eastus", Extra: map[string]string{}})
	providers = append(providers, azureP)

	gcpP, _ := NewGCPProvider(config.CloudProviderConfig{Name: "d", Region: "us-central1", Extra: map[string]string{}})
	providers = append(providers, gcpP)

	hwP, _ := NewHuaweiProvider(config.CloudProviderConfig{Name: "e", Region: "cn-north-4", Extra: map[string]string{}})
	providers = append(providers, hwP)

	tcP, _ := NewTencentProvider(config.CloudProviderConfig{Name: "f", Region: "ap-guangzhou"})
	providers = append(providers, tcP)

	for _, p := range providers {
		t.Run(string(p.Type()), func(t *testing.T) {
			err := p.Ping(context.Background())
			if err != nil {
				t.Errorf("Ping should succeed for nil-client (stub mode): %v", err)
			}
		})
	}
}

// =============================================================================
// Provider ListClusters Tests (all nil-client: should return empty slice)
// =============================================================================

func TestAllProviders_ListClusters_NilClient(t *testing.T) {
	providers := []Provider{}

	aliyun, _ := NewAliyunProvider(config.CloudProviderConfig{Name: "a", Region: "cn-hangzhou"})
	providers = append(providers, aliyun)

	awsP, _ := NewAWSProvider(config.CloudProviderConfig{Name: "b", Region: "us-west-2"})
	providers = append(providers, awsP)

	azureP, _ := NewAzureProvider(config.CloudProviderConfig{Name: "c", Region: "eastus", Extra: map[string]string{}})
	providers = append(providers, azureP)

	gcpP, _ := NewGCPProvider(config.CloudProviderConfig{Name: "d", Region: "us-central1", Extra: map[string]string{}})
	providers = append(providers, gcpP)

	hwP, _ := NewHuaweiProvider(config.CloudProviderConfig{Name: "e", Region: "cn-north-4", Extra: map[string]string{}})
	providers = append(providers, hwP)

	tcP, _ := NewTencentProvider(config.CloudProviderConfig{Name: "f", Region: "ap-guangzhou"})
	providers = append(providers, tcP)

	for _, p := range providers {
		t.Run(string(p.Type()), func(t *testing.T) {
			clusters, err := p.ListClusters(context.Background())
			if err != nil {
				t.Errorf("ListClusters should return empty for nil client: %v", err)
			}
			if clusters == nil {
				t.Error("ListClusters should return empty slice, not nil")
			}
			if len(clusters) != 0 {
				t.Errorf("expected 0 clusters, got %d", len(clusters))
			}
		})
	}
}

// =============================================================================
// Aliyun SDK nil-client tests
// =============================================================================

func TestAliyunAPIClient_NilSDKClient(t *testing.T) {
	client := &AliyunAPIClient{region: "cn-hangzhou", endpoint: "cs.cn-hangzhou.aliyuncs.com"}
	// doROARequest with nil sdkClient should error
	_, _, err := client.doROARequest(context.Background(), "GET", "/test", "")
	if err == nil {
		t.Error("expected error for nil sdkClient")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("error should mention not initialized: %v", err)
	}
}

func TestAliyunAPIClient_DescribeClusters_NilClient(t *testing.T) {
	client := &AliyunAPIClient{}
	_, err := client.DescribeClusters(context.Background())
	if err == nil {
		t.Error("expected error")
	}
}

func TestAliyunAPIClient_DescribeClusterDetail_NilClient(t *testing.T) {
	client := &AliyunAPIClient{}
	_, err := client.DescribeClusterDetail(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAliyunAPIClient_DescribeClusterNodes_NilClient(t *testing.T) {
	client := &AliyunAPIClient{}
	_, err := client.DescribeClusterNodes(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAliyunAPIClient_DescribeKubeconfig_NilClient(t *testing.T) {
	client := &AliyunAPIClient{}
	_, err := client.DescribeClusterUserKubeconfig(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAliyunAPIClient_DeleteCluster_NilClient(t *testing.T) {
	client := &AliyunAPIClient{}
	err := client.DeleteACKCluster(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}
}

func TestAliyunAPIClient_ScaleOut_NilClient(t *testing.T) {
	client := &AliyunAPIClient{}
	err := client.ScaleOutCluster(context.Background(), "c-1", 3)
	if err == nil {
		t.Error("expected error")
	}
}

func TestAliyunAPIClient_CreateCluster_NilClient(t *testing.T) {
	client := &AliyunAPIClient{}
	_, err := client.CreateACKCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// AWS SDK nil-client tests
// =============================================================================

func TestAWSAPIClient_NilEKSClient(t *testing.T) {
	client := &AWSAPIClient{region: "us-west-2"}

	_, err := client.ListEKSClusters(context.Background())
	if err == nil {
		t.Error("expected error for nil eksClient")
	}

	_, err = client.DescribeEKSCluster(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.CreateEKSCluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}

	err = client.DeleteEKSCluster(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.ListEKSNodegroups(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.DescribeEKSNodegroup(context.Background(), "c-1", "ng-1")
	if err == nil {
		t.Error("expected error")
	}

	err = client.UpdateEKSNodegroupScaling(context.Background(), "c-1", "ng-1", 5)
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// Azure SDK nil-client tests
// =============================================================================

func TestAzureAPIClient_NilAKSClient(t *testing.T) {
	client := &AzureAPIClient{subscriptionID: "sub-1"}

	_, err := client.ListAKSClusters(context.Background(), "rg")
	if err == nil {
		t.Error("expected error for nil aksClient")
	}

	_, err = client.GetAKSCluster(context.Background(), "rg", "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.CreateAKSCluster(context.Background(), "rg", &CreateClusterRequest{Name: "test", NodeCount: 1, NodeType: "Standard_DS2"}, "eastus")
	if err == nil {
		t.Error("expected error")
	}

	err = client.DeleteAKSCluster(context.Background(), "rg", "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.GetAKSCredentials(context.Background(), "rg", "c-1")
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// GCP SDK nil-client tests
// =============================================================================

func TestGCPAPIClient_NilService(t *testing.T) {
	client := &GCPAPIClient{projectID: "proj", location: "us-central1"}

	_, err := client.ListGKEClusters(context.Background())
	if err == nil {
		t.Error("expected error for nil svc")
	}

	_, err = client.GetGKECluster(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.CreateGKECluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}

	err = client.DeleteGKECluster(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// Huawei SDK nil-client tests
// =============================================================================

func TestHuaweiAPIClient_NilCCEClient(t *testing.T) {
	client := &HuaweiAPIClient{region: "cn-north-4", projectID: "proj"}

	_, err := client.ListCCEClusters(context.Background())
	if err == nil {
		t.Error("expected error for nil cceClient")
	}

	_, err = client.GetCCECluster(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.CreateCCECluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}

	err = client.DeleteCCECluster(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.ListCCENodes(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.GetCCEKubeConfig(context.Background(), "c-1")
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// Tencent SDK nil-client tests
// =============================================================================

func TestTencentAPIClient_NilTKEClient(t *testing.T) {
	client := &TencentAPIClient{region: "ap-guangzhou"}

	_, err := client.ListTKEClusters(context.Background())
	if err == nil {
		t.Error("expected error for nil tkeClient")
	}

	_, err = client.GetTKECluster(context.Background(), "cls-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.CreateTKECluster(context.Background(), &CreateClusterRequest{Name: "test"})
	if err == nil {
		t.Error("expected error")
	}

	err = client.DeleteTKECluster(context.Background(), "cls-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.ListTKEInstances(context.Background(), "cls-1")
	if err == nil {
		t.Error("expected error")
	}

	_, err = client.GetTKEKubeConfig(context.Background(), "cls-1")
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// createProvider factory tests
// =============================================================================

func TestCreateProvider_AllTypes(t *testing.T) {
	tests := []struct {
		cfgType string
		wantErr bool
	}{
		{"aliyun", false},
		{"aws", false},
		{"azure", false},
		{"gcp", false},
		{"huawei", false},
		{"tencent", false},
		{"oracle", true},   // unsupported
		{"unknown", true},  // unsupported
	}

	for _, tt := range tests {
		t.Run(tt.cfgType, func(t *testing.T) {
			cfg := config.CloudProviderConfig{
				Name:   "test-" + tt.cfgType,
				Type:   tt.cfgType,
				Region: "us-east-1",
				Extra:  map[string]string{},
			}
			_, err := createProvider(cfg)
			if tt.wantErr && err == nil {
				t.Errorf("expected error for type %q", tt.cfgType)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for type %q: %v", tt.cfgType, err)
			}
		})
	}
}
