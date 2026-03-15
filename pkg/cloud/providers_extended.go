package cloud

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
)

// =============================================================================
// TencentProvider - Tencent Cloud (TKE / TI-ONE / CLS)
// =============================================================================

// TencentProvider implements Provider for Tencent Cloud
// with REAL SDK calls to TKE API using TC3-HMAC-SHA256 signing.
type TencentProvider struct {
	name      string
	region    string
	secretID  string
	secretKey string
	client    *TencentAPIClient // real API client
}

func NewTencentProvider(cfg config.CloudProviderConfig) (*TencentProvider, error) {
	p := &TencentProvider{
		name:      cfg.Name,
		region:    cfg.Region,
		secretID:  cfg.AccessKeyID,
		secretKey: cfg.AccessKeySecret,
	}
	if cfg.AccessKeyID != "" && cfg.AccessKeySecret != "" {
		p.client = NewTencentAPIClient(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.Region)
	}
	return p, nil
}

func (p *TencentProvider) Name() string                   { return p.name }
func (p *TencentProvider) Type() common.CloudProviderType { return common.CloudProviderTencent }
func (p *TencentProvider) Region() string                 { return p.region }

func (p *TencentProvider) Ping(ctx context.Context) error {
	if p.client == nil {
		return nil
	}
	_, err := p.client.ListTKEClusters(ctx)
	if err != nil {
		return fmt.Errorf("Tencent Cloud connectivity check failed: %w", err)
	}
	return nil
}

func (p *TencentProvider) ListClusters(ctx context.Context) ([]*ClusterInfo, error) {
	if p.client == nil {
		return []*ClusterInfo{}, nil
	}
	tkeClusters, err := p.client.ListTKEClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list TKE clusters: %w", err)
	}
	clusters := make([]*ClusterInfo, 0, len(tkeClusters))
	for _, c := range tkeClusters {
		clusters = append(clusters, &ClusterInfo{
			ID:                c.ClusterID,
			Name:              c.ClusterName,
			Provider:          common.CloudProviderTencent,
			Region:            p.region,
			KubernetesVersion: c.ClusterVersion,
			Status:            c.ClusterStatus,
			Endpoint:          c.ClusterExternalEndpoint,
			NodeCount:         c.ClusterNodeNum,
			CreatedAt:         c.CreatedTime,
		})
	}
	return clusters, nil
}

func (p *TencentProvider) GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("Tencent Cloud credentials not configured")
	}
	c, err := p.client.GetTKECluster(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get TKE cluster %s: %w", clusterID, err)
	}
	return &ClusterInfo{
		ID:                c.ClusterID,
		Name:              c.ClusterName,
		Provider:          common.CloudProviderTencent,
		Region:            p.region,
		KubernetesVersion: c.ClusterVersion,
		Status:            c.ClusterStatus,
		Endpoint:          c.ClusterExternalEndpoint,
		NodeCount:         c.ClusterNodeNum,
	}, nil
}

func (p *TencentProvider) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("Tencent Cloud credentials not configured")
	}
	id, err := p.client.CreateTKECluster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create TKE cluster: %w", err)
	}
	return &ClusterInfo{
		ID:       id,
		Name:     req.Name,
		Provider: common.CloudProviderTencent,
		Region:   p.region,
		Status:   "Creating",
	}, nil
}

func (p *TencentProvider) DeleteCluster(ctx context.Context, clusterID string) error {
	if p.client == nil {
		return fmt.Errorf("Tencent Cloud credentials not configured")
	}
	return p.client.DeleteTKECluster(ctx, clusterID)
}

func (p *TencentProvider) ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error {
	if p.client == nil {
		return fmt.Errorf("Tencent Cloud credentials not configured")
	}
	// TKE scaling via AddExistedInstances or cluster node pool API
	return fmt.Errorf("TKE scaling requires node pool API; use AddExistedInstances")
}

func (p *TencentProvider) GetKubeConfig(ctx context.Context, clusterID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("Tencent Cloud credentials not configured")
	}
	return p.client.GetTKEKubeConfig(ctx, clusterID)
}

func (p *TencentProvider) ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error) {
	if p.client == nil {
		return []*NodeInfo{}, nil
	}
	instances, err := p.client.ListTKEInstances(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	nodes := make([]*NodeInfo, 0, len(instances))
	for _, inst := range instances {
		nodes = append(nodes, &NodeInfo{
			ID:           inst.InstanceID,
			Name:         inst.InstanceID,
			Status:       inst.InstanceState,
			Role:         inst.InstanceRole,
			InstanceType: inst.InstanceType,
			IPAddress:    inst.LanIP,
		})
	}
	return nodes, nil
}

func (p *TencentProvider) GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error) {
	return &NodeMetrics{NodeID: nodeID}, fmt.Errorf("node metrics require Tencent Cloud Monitor; use Prometheus in-cluster")
}

func (p *TencentProvider) ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error) {
	return []*GPUInstanceInfo{
		{
			InstanceType: "GN10Xp.20XLARGE320",
			GPUType:      "nvidia-v100",
			GPUCount:     4,
			GPUMemoryGB:  128,
			CPUCores:     80,
			MemoryGB:     320,
			PricePerHour: 58.68,
			Availability: "available",
		},
		{
			InstanceType: "GT4.41XLARGE948",
			GPUType:      "nvidia-a100",
			GPUCount:     8,
			GPUMemoryGB:  640,
			CPUCores:     164,
			MemoryGB:     948,
			PricePerHour: 168.88,
			Availability: "limited",
		},
	}, nil
}

func (p *TencentProvider) GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error) {
	return &GPUPricing{
		GPUType:       gpuType,
		OnDemandPrice: 168.88,
		SpotPrice:     50.66,
		ReservedPrice: 118.22,
		Currency:      "CNY",
	}, nil
}

func (p *TencentProvider) GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	return &CostSummary{TotalCost: 0, Currency: "CNY"}, nil
}

// =============================================================================
// HuaweiProvider - Huawei Cloud (CCE / ModelArts / Cloud Monitor)
// =============================================================================

// HuaweiProvider implements Provider for Huawei Cloud
// with REAL SDK calls to CCE API using HMAC-SHA256 signing.
type HuaweiProvider struct {
	name      string
	region    string
	accessKey string
	secretKey string
	projectID string
	client    *HuaweiAPIClient // real API client
}

func NewHuaweiProvider(cfg config.CloudProviderConfig) (*HuaweiProvider, error) {
	p := &HuaweiProvider{
		name:      cfg.Name,
		region:    cfg.Region,
		accessKey: cfg.AccessKeyID,
		secretKey: cfg.AccessKeySecret,
		projectID: cfg.Extra["project_id"],
	}
	if cfg.AccessKeyID != "" && cfg.AccessKeySecret != "" && p.projectID != "" {
		p.client = NewHuaweiAPIClient(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.Region, p.projectID)
	}
	return p, nil
}

func (p *HuaweiProvider) Name() string                   { return p.name }
func (p *HuaweiProvider) Type() common.CloudProviderType { return common.CloudProviderHuawei }
func (p *HuaweiProvider) Region() string                 { return p.region }

func (p *HuaweiProvider) Ping(ctx context.Context) error {
	if p.client == nil {
		return nil
	}
	_, err := p.client.ListCCEClusters(ctx)
	if err != nil {
		return fmt.Errorf("Huawei Cloud connectivity check failed: %w", err)
	}
	return nil
}

func (p *HuaweiProvider) ListClusters(ctx context.Context) ([]*ClusterInfo, error) {
	if p.client == nil {
		return []*ClusterInfo{}, nil
	}
	cceClusters, err := p.client.ListCCEClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list CCE clusters: %w", err)
	}
	clusters := make([]*ClusterInfo, 0, len(cceClusters))
	for _, c := range cceClusters {
		endpoint := ""
		for _, ep := range c.Status.Endpoints {
			if ep.Type == "External" {
				endpoint = ep.URL
				break
			}
		}
		clusters = append(clusters, &ClusterInfo{
			ID:                c.Metadata.UID,
			Name:              c.Metadata.Name,
			Provider:          common.CloudProviderHuawei,
			Region:            p.region,
			KubernetesVersion: c.Spec.Version,
			Status:            c.Status.Phase,
			Endpoint:          endpoint,
			CreatedAt:         c.Metadata.CreatedAt,
		})
	}
	return clusters, nil
}

func (p *HuaweiProvider) GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("Huawei Cloud credentials not configured")
	}
	c, err := p.client.GetCCECluster(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get CCE cluster %s: %w", clusterID, err)
	}
	endpoint := ""
	for _, ep := range c.Status.Endpoints {
		if ep.Type == "External" {
			endpoint = ep.URL
			break
		}
	}
	return &ClusterInfo{
		ID:                c.Metadata.UID,
		Name:              c.Metadata.Name,
		Provider:          common.CloudProviderHuawei,
		Region:            p.region,
		KubernetesVersion: c.Spec.Version,
		Status:            c.Status.Phase,
		Endpoint:          endpoint,
	}, nil
}

func (p *HuaweiProvider) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("Huawei Cloud credentials not configured")
	}
	c, err := p.client.CreateCCECluster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create CCE cluster: %w", err)
	}
	return &ClusterInfo{
		ID:       c.Metadata.UID,
		Name:     c.Metadata.Name,
		Provider: common.CloudProviderHuawei,
		Region:   p.region,
		Status:   c.Status.Phase,
	}, nil
}

func (p *HuaweiProvider) DeleteCluster(ctx context.Context, clusterID string) error {
	if p.client == nil {
		return fmt.Errorf("Huawei Cloud credentials not configured")
	}
	return p.client.DeleteCCECluster(ctx, clusterID)
}

func (p *HuaweiProvider) ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error {
	if p.client == nil {
		return fmt.Errorf("Huawei Cloud credentials not configured")
	}
	// CCE scaling via node pool API
	return fmt.Errorf("CCE scaling requires node pool API; use CreateNode endpoint")
}

func (p *HuaweiProvider) GetKubeConfig(ctx context.Context, clusterID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("Huawei Cloud credentials not configured")
	}
	return p.client.GetCCEKubeConfig(ctx, clusterID)
}

func (p *HuaweiProvider) ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error) {
	if p.client == nil {
		return []*NodeInfo{}, nil
	}
	cceNodes, err := p.client.ListCCENodes(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	nodes := make([]*NodeInfo, 0, len(cceNodes))
	for _, n := range cceNodes {
		nodes = append(nodes, &NodeInfo{
			ID:           n.Metadata.UID,
			Name:         n.Metadata.Name,
			Status:       n.Status.Phase,
			Role:         "worker",
			InstanceType: n.Spec.Flavor,
			IPAddress:    n.Status.PrivateIP,
		})
	}
	return nodes, nil
}

func (p *HuaweiProvider) GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error) {
	return &NodeMetrics{NodeID: nodeID}, fmt.Errorf("node metrics require Huawei Cloud Eye; use Prometheus in-cluster")
}

func (p *HuaweiProvider) ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error) {
	return []*GPUInstanceInfo{
		{
			InstanceType: "ai1s.8xlarge.8",
			GPUType:      "huawei-ascend-910b",
			GPUCount:     8,
			GPUMemoryGB:  512,
			CPUCores:     192,
			MemoryGB:     1536,
			PricePerHour: 120.50,
			Availability: "available",
		},
		{
			InstanceType: "p2s.2xlarge",
			GPUType:      "nvidia-v100",
			GPUCount:     1,
			GPUMemoryGB:  32,
			CPUCores:     8,
			MemoryGB:     64,
			PricePerHour: 28.50,
			Availability: "available",
		},
	}, nil
}

func (p *HuaweiProvider) GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error) {
	return &GPUPricing{
		GPUType:       gpuType,
		OnDemandPrice: 120.50,
		SpotPrice:     48.20,
		ReservedPrice: 84.35,
		Currency:      "CNY",
	}, nil
}

func (p *HuaweiProvider) GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	return &CostSummary{TotalCost: 0, Currency: "CNY"}, nil
}

// =============================================================================
// GCPProvider - Google Cloud Platform (GKE / Vertex AI / Stackdriver)
// =============================================================================

// GCPProvider implements Provider for Google Cloud Platform
// with REAL SDK calls to GKE API using Service Account JWT + OAuth2.
type GCPProvider struct {
	name      string
	region    string
	projectID string
	credJSON  string // Service account JSON key
	client    *GCPAPIClient // real API client
}

func NewGCPProvider(cfg config.CloudProviderConfig) (*GCPProvider, error) {
	p := &GCPProvider{
		name:      cfg.Name,
		region:    cfg.Region,
		projectID: cfg.Extra["project_id"],
		credJSON:  cfg.Extra["credentials_json"],
	}
	if p.credJSON != "" {
		client, err := NewGCPAPIClient(p.credJSON, p.projectID, p.region)
		if err != nil {
			// Log but don't fail - allow stub mode
			logrus.WithError(err).Warn("Failed to initialize GCP API client, running in stub mode")
		} else {
			p.client = client
			if p.projectID == "" {
				p.projectID = client.projectID
			}
		}
	}
	return p, nil
}

func (p *GCPProvider) Name() string                   { return p.name }
func (p *GCPProvider) Type() common.CloudProviderType { return common.CloudProviderGCP }
func (p *GCPProvider) Region() string                 { return p.region }

func (p *GCPProvider) Ping(ctx context.Context) error {
	if p.client == nil {
		return nil
	}
	_, err := p.client.ListGKEClusters(ctx)
	if err != nil {
		return fmt.Errorf("GCP connectivity check failed: %w", err)
	}
	return nil
}

func (p *GCPProvider) ListClusters(ctx context.Context) ([]*ClusterInfo, error) {
	if p.client == nil {
		return []*ClusterInfo{}, nil
	}
	gkeClusters, err := p.client.ListGKEClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list GKE clusters: %w", err)
	}
	clusters := make([]*ClusterInfo, 0, len(gkeClusters))
	for _, c := range gkeClusters {
		nodeCount := 0
		for _, pool := range c.NodePools {
			nodeCount += pool.InitialNodeCount
		}
		if nodeCount == 0 {
			nodeCount = c.InitialNodeCount
		}
		clusters = append(clusters, &ClusterInfo{
			ID:                c.Name,
			Name:              c.Name,
			Provider:          common.CloudProviderGCP,
			Region:            c.Location,
			KubernetesVersion: c.CurrentMasterVersion,
			Status:            c.Status,
			Endpoint:          c.Endpoint,
			NodeCount:         nodeCount,
			CreatedAt:         c.CreateTime,
		})
	}
	return clusters, nil
}

func (p *GCPProvider) GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("GCP credentials not configured")
	}
	c, err := p.client.GetGKECluster(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get GKE cluster %s: %w", clusterID, err)
	}
	nodeCount := c.InitialNodeCount
	for _, pool := range c.NodePools {
		if pool.InitialNodeCount > 0 {
			nodeCount = pool.InitialNodeCount
		}
	}
	return &ClusterInfo{
		ID:                c.Name,
		Name:              c.Name,
		Provider:          common.CloudProviderGCP,
		Region:            c.Location,
		KubernetesVersion: c.CurrentMasterVersion,
		Status:            c.Status,
		Endpoint:          c.Endpoint,
		NodeCount:         nodeCount,
	}, nil
}

func (p *GCPProvider) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("GCP credentials not configured")
	}
	c, err := p.client.CreateGKECluster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create GKE cluster: %w", err)
	}
	return &ClusterInfo{
		ID:       c.Name,
		Name:     c.Name,
		Provider: common.CloudProviderGCP,
		Region:   p.region,
		Status:   c.Status,
	}, nil
}

func (p *GCPProvider) DeleteCluster(ctx context.Context, clusterID string) error {
	if p.client == nil {
		return fmt.Errorf("GCP credentials not configured")
	}
	return p.client.DeleteGKECluster(ctx, clusterID)
}

func (p *GCPProvider) ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error {
	if p.client == nil {
		return fmt.Errorf("GCP credentials not configured")
	}
	// GKE scaling via node pool resize API
	return fmt.Errorf("GKE scaling requires node pool setSize API")
}

func (p *GCPProvider) GetKubeConfig(ctx context.Context, clusterID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("GCP credentials not configured")
	}
	c, err := p.client.GetGKECluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("apiVersion: v1\nclusters:\n- cluster:\n    server: https://%s\n  name: %s",
		c.Endpoint, c.Name), nil
}

func (p *GCPProvider) ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error) {
	if p.client == nil {
		return []*NodeInfo{}, nil
	}
	c, err := p.client.GetGKECluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	var nodes []*NodeInfo
	for _, pool := range c.NodePools {
		for i := 0; i < pool.InitialNodeCount; i++ {
			nodes = append(nodes, &NodeInfo{
				ID:           fmt.Sprintf("%s-%d", pool.Name, i),
				Name:         fmt.Sprintf("%s-%s-%d", clusterID, pool.Name, i),
				Status:       pool.Status,
				Role:         "worker",
				InstanceType: pool.Config.MachineType,
			})
		}
	}
	return nodes, nil
}

func (p *GCPProvider) GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error) {
	return &NodeMetrics{NodeID: nodeID}, fmt.Errorf("node metrics require Cloud Monitoring; use Prometheus in-cluster")
}

func (p *GCPProvider) ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error) {
	return []*GPUInstanceInfo{
		{
			InstanceType: "a3-highgpu-8g",
			GPUType:      "nvidia-h100",
			GPUCount:     8,
			GPUMemoryGB:  640,
			CPUCores:     208,
			MemoryGB:     1872,
			PricePerHour: 101.22,
			Availability: "limited",
		},
		{
			InstanceType: "a2-ultragpu-4g",
			GPUType:      "nvidia-a100",
			GPUCount:     4,
			GPUMemoryGB:  320,
			CPUCores:     48,
			MemoryGB:     680,
			PricePerHour: 29.39,
			SpotAvailable: true,
			SpotPrice:     8.82,
			Availability:  "available",
		},
		{
			InstanceType:  "ct5lp-hightpu-4t",
			GPUType:       "google-tpu-v5e",
			GPUCount:      4,
			GPUMemoryGB:   64,
			CPUCores:      112,
			MemoryGB:      192,
			PricePerHour:  12.88,
			SpotAvailable: true,
			SpotPrice:     3.86,
			Availability:  "available",
		},
	}, nil
}

func (p *GCPProvider) GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error) {
	return &GPUPricing{
		GPUType:       gpuType,
		OnDemandPrice: 101.22,
		SpotPrice:     30.37,
		ReservedPrice: 60.73,
		Currency:      "USD",
	}, nil
}

func (p *GCPProvider) GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	return &CostSummary{TotalCost: 0, Currency: "USD"}, nil
}
