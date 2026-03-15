package cloud

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
)

func TestNewManager_Empty(t *testing.T) {
	mgr, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatalf("NewManager with empty config should succeed: %v", err)
	}
	if len(mgr.ListProviders()) != 0 {
		t.Error("empty manager should have no providers")
	}
}

func TestNewManager_AllProviders(t *testing.T) {
	providerConfigs := []config.CloudProviderConfig{
		{Name: "aliyun-test", Type: "aliyun", Region: "cn-hangzhou"},
		{Name: "aws-test", Type: "aws", Region: "us-west-2"},
		{Name: "azure-test", Type: "azure", Region: "eastus", Extra: map[string]string{"subscription_id": "sub-1", "tenant_id": "t-1"}},
		{Name: "gcp-test", Type: "gcp", Region: "us-central1", Extra: map[string]string{"project_id": "proj-1"}},
		{Name: "huawei-test", Type: "huawei", Region: "cn-north-4", Extra: map[string]string{"project_id": "proj-2"}},
		{Name: "tencent-test", Type: "tencent", Region: "ap-guangzhou"},
	}

	mgr, err := NewManager(ManagerConfig{Providers: providerConfigs})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	providers := mgr.ListProviders()
	if len(providers) != 6 {
		t.Errorf("expected 6 providers, got %d", len(providers))
	}
}

func TestNewManager_UnsupportedType(t *testing.T) {
	_, err := NewManager(ManagerConfig{
		Providers: []config.CloudProviderConfig{
			{Name: "unknown", Type: "oracle", Region: "us-1"},
		},
	})
	if err == nil {
		t.Error("should fail with unsupported provider type")
	}
}

func TestGetProvider(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{
		Providers: []config.CloudProviderConfig{
			{Name: "my-aws", Type: "aws", Region: "us-east-1"},
		},
	})

	p, err := mgr.GetProvider("my-aws")
	if err != nil {
		t.Fatalf("GetProvider failed: %v", err)
	}
	if p.Name() != "my-aws" {
		t.Errorf("expected name 'my-aws', got '%s'", p.Name())
	}
	if p.Type() != common.CloudProviderAWS {
		t.Errorf("expected type 'aws', got '%s'", p.Type())
	}

	_, err = mgr.GetProvider("nonexistent")
	if err == nil {
		t.Error("should fail for nonexistent provider")
	}
}

func TestRegisterProvider(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})

	aliyunCfg := config.CloudProviderConfig{Name: "dynamic-aliyun", Type: "aliyun", Region: "cn-shanghai"}
	p, _ := NewAliyunProvider(aliyunCfg)
	mgr.RegisterProvider("dynamic-aliyun", p)

	provider, err := mgr.GetProvider("dynamic-aliyun")
	if err != nil {
		t.Fatalf("GetProvider after register failed: %v", err)
	}
	if provider.Region() != "cn-shanghai" {
		t.Errorf("expected region 'cn-shanghai', got '%s'", provider.Region())
	}
}

func TestProviderInterface_Aliyun(t *testing.T) {
	testProviderBasics(t, "aliyun", func() (Provider, error) {
		return NewAliyunProvider(config.CloudProviderConfig{Name: "test-aliyun", Region: "cn-hangzhou"})
	})
}

func TestProviderInterface_AWS(t *testing.T) {
	testProviderBasics(t, "aws", func() (Provider, error) {
		return NewAWSProvider(config.CloudProviderConfig{Name: "test-aws", Region: "us-west-2"})
	})
}

func TestProviderInterface_Azure(t *testing.T) {
	testProviderBasics(t, "azure", func() (Provider, error) {
		return NewAzureProvider(config.CloudProviderConfig{Name: "test-azure", Region: "eastus", Extra: map[string]string{}})
	})
}

func TestProviderInterface_GCP(t *testing.T) {
	testProviderBasics(t, "gcp", func() (Provider, error) {
		return NewGCPProvider(config.CloudProviderConfig{Name: "test-gcp", Region: "us-central1", Extra: map[string]string{}})
	})
}

func TestProviderInterface_Huawei(t *testing.T) {
	testProviderBasics(t, "huawei", func() (Provider, error) {
		return NewHuaweiProvider(config.CloudProviderConfig{Name: "test-huawei", Region: "cn-north-4", Extra: map[string]string{}})
	})
}

func TestProviderInterface_Tencent(t *testing.T) {
	testProviderBasics(t, "tencent", func() (Provider, error) {
		return NewTencentProvider(config.CloudProviderConfig{Name: "test-tencent", Region: "ap-guangzhou"})
	})
}

func testProviderBasics(t *testing.T, providerType string, create func() (Provider, error)) {
	t.Helper()
	p, err := create()
	if err != nil {
		t.Fatalf("[%s] create failed: %v", providerType, err)
	}

	if p.Name() == "" {
		t.Errorf("[%s] Name() should not be empty", providerType)
	}
	if p.Region() == "" {
		t.Errorf("[%s] Region() should not be empty", providerType)
	}

	ctx := context.Background()

	// Ping should succeed
	if err := p.Ping(ctx); err != nil {
		t.Errorf("[%s] Ping failed: %v", providerType, err)
	}

	// ListClusters should not error
	clusters, err := p.ListClusters(ctx)
	if err != nil {
		t.Errorf("[%s] ListClusters failed: %v", providerType, err)
	}
	if clusters == nil {
		t.Errorf("[%s] ListClusters returned nil (should be empty slice)", providerType)
	}

	// ListGPUInstances
	gpuInstances, err := p.ListGPUInstances(ctx)
	if err != nil {
		t.Errorf("[%s] ListGPUInstances failed: %v", providerType, err)
	}
	if len(gpuInstances) == 0 {
		t.Errorf("[%s] ListGPUInstances should return at least one instance type", providerType)
	}

	// GetGPUPricing
	pricing, err := p.GetGPUPricing(ctx, "nvidia-a100")
	if err != nil {
		t.Errorf("[%s] GetGPUPricing failed: %v", providerType, err)
	}
	if pricing.Currency == "" {
		t.Errorf("[%s] pricing currency should not be empty", providerType)
	}

	// GetCostSummary
	cost, err := p.GetCostSummary(ctx, "2026-03-01", "2026-03-31")
	if err != nil {
		t.Errorf("[%s] GetCostSummary failed: %v", providerType, err)
	}
	if cost.Currency == "" {
		t.Errorf("[%s] cost currency should not be empty", providerType)
	}
}

func TestGetTotalCost(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{
		Providers: []config.CloudProviderConfig{
			{Name: "aws", Type: "aws", Region: "us-east-1"},
			{Name: "aliyun", Type: "aliyun", Region: "cn-hangzhou"},
		},
	})

	ctx := context.Background()
	cost, err := mgr.GetTotalCost(ctx, "2026-03-01", "2026-03-31")
	if err != nil {
		t.Fatalf("GetTotalCost failed: %v", err)
	}
	if cost.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", cost.Currency)
	}
}
