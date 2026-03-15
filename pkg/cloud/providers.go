package cloud

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
)

// AliyunProvider implements Provider for Alibaba Cloud (ACK/PAI/MaxCompute)
// with REAL SDK calls to ACK OpenAPI.
type AliyunProvider struct {
	name      string
	region    string
	accessID  string
	accessKey string
	client    *AliyunAPIClient // real API client
}

func NewAliyunProvider(cfg config.CloudProviderConfig) (*AliyunProvider, error) {
	p := &AliyunProvider{
		name:      cfg.Name,
		region:    cfg.Region,
		accessID:  cfg.AccessKeyID,
		accessKey: cfg.AccessKeySecret,
	}
	// Initialize real API client if credentials are provided
	if cfg.AccessKeyID != "" && cfg.AccessKeySecret != "" {
		p.client = NewAliyunAPIClient(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.Region)
	}
	return p, nil
}

func (p *AliyunProvider) Name() string                    { return p.name }
func (p *AliyunProvider) Type() common.CloudProviderType  { return common.CloudProviderAliyun }
func (p *AliyunProvider) Region() string                  { return p.region }

func (p *AliyunProvider) Ping(ctx context.Context) error {
	if p.client == nil {
		// No credentials configured – provider is registered but operates in stub mode
		return nil
	}
	// Verify connectivity by calling DescribeClusters (lightweight)
	_, err := p.client.DescribeClusters(ctx)
	if err != nil {
		return fmt.Errorf("alibaba cloud connectivity check failed: %w", err)
	}
	return nil
}

func (p *AliyunProvider) ListClusters(ctx context.Context) ([]*ClusterInfo, error) {
	if p.client == nil {
		return []*ClusterInfo{}, nil
	}
	ackClusters, err := p.client.DescribeClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list ACK clusters: %w", err)
	}

	clusters := make([]*ClusterInfo, 0, len(ackClusters))
	for _, c := range ackClusters {
		clusters = append(clusters, &ClusterInfo{
			ID:                c.ClusterID,
			Name:              c.Name,
			Provider:          common.CloudProviderAliyun,
			Region:            c.RegionID,
			KubernetesVersion: c.Version,
			Status:            c.State,
			Endpoint:          c.ExternalEndpoint,
			NodeCount:         c.Size,
			CreatedAt:         c.Created,
		})
	}
	return clusters, nil
}

func (p *AliyunProvider) GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("alibaba cloud credentials not configured")
	}
	c, err := p.client.DescribeClusterDetail(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ACK cluster %s: %w", clusterID, err)
	}
	return &ClusterInfo{
		ID:                c.ClusterID,
		Name:              c.Name,
		Provider:          common.CloudProviderAliyun,
		Region:            c.RegionID,
		KubernetesVersion: c.Version,
		Status:            c.State,
		Endpoint:          c.ExternalEndpoint,
		NodeCount:         c.Size,
		CreatedAt:         c.Created,
	}, nil
}

func (p *AliyunProvider) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("alibaba cloud credentials not configured")
	}
	c, err := p.client.CreateACKCluster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACK cluster: %w", err)
	}
	return &ClusterInfo{
		ID:       c.ClusterID,
		Name:     c.Name,
		Provider: common.CloudProviderAliyun,
		Region:   c.RegionID,
		Status:   c.State,
	}, nil
}

func (p *AliyunProvider) DeleteCluster(ctx context.Context, clusterID string) error {
	if p.client == nil {
		return fmt.Errorf("alibaba cloud credentials not configured")
	}
	return p.client.DeleteACKCluster(ctx, clusterID)
}

func (p *AliyunProvider) ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error {
	if p.client == nil {
		return fmt.Errorf("alibaba cloud credentials not configured")
	}
	return p.client.ScaleOutCluster(ctx, clusterID, nodeCount)
}

func (p *AliyunProvider) GetKubeConfig(ctx context.Context, clusterID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("alibaba cloud credentials not configured")
	}
	return p.client.DescribeClusterUserKubeconfig(ctx, clusterID)
}

func (p *AliyunProvider) ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error) {
	if p.client == nil {
		return []*NodeInfo{}, nil
	}
	ackNodes, err := p.client.DescribeClusterNodes(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to list ACK nodes: %w", err)
	}

	nodes := make([]*NodeInfo, 0, len(ackNodes))
	for _, n := range ackNodes {
		ip := ""
		if len(n.IPAddress) > 0 {
			ip = n.IPAddress[0]
		}
		nodes = append(nodes, &NodeInfo{
			ID:           n.InstanceID,
			Name:         n.InstanceName,
			Status:       n.State,
			Role:         n.InstanceRole,
			InstanceType: n.InstanceType,
			IPAddress:    ip,
		})
	}
	return nodes, nil
}

func (p *AliyunProvider) GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error) {
	// Node metrics require CloudMonitor integration (separate API)
	// ACK does not expose node-level metrics directly via CS API.
	// Use Prometheus + node_exporter in real deployments.
	return &NodeMetrics{
		NodeID: nodeID,
	}, fmt.Errorf("node metrics require CloudMonitor integration; use Prometheus in-cluster")
}

func (p *AliyunProvider) ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error) {
	// Alibaba Cloud GPU instance types (GN7i, GN7, etc.)
	return []*GPUInstanceInfo{
		{
			InstanceType: "ecs.gn7i-c8g1.2xlarge",
			GPUType:      "nvidia-a10",
			GPUCount:     1,
			GPUMemoryGB:  24,
			CPUCores:     8,
			MemoryGB:     30,
			PricePerHour: 2.85,
			Availability: "available",
		},
		{
			InstanceType: "ecs.gn7-c12g1.3xlarge",
			GPUType:      "nvidia-a100",
			GPUCount:     1,
			GPUMemoryGB:  80,
			CPUCores:     12,
			MemoryGB:     94,
			PricePerHour: 8.50,
			Availability: "available",
		},
	}, nil
}

func (p *AliyunProvider) GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error) {
	return &GPUPricing{
		GPUType:       gpuType,
		OnDemandPrice: 8.50,
		SpotPrice:     3.40,
		ReservedPrice: 5.95,
		Currency:      "CNY",
	}, nil
}

func (p *AliyunProvider) GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	return &CostSummary{
		TotalCost: 0,
		Currency:  "CNY",
	}, nil
}

// =============================================================================

// AWSProvider implements Provider for Amazon Web Services (EKS/SageMaker)
// with REAL SDK calls to AWS EKS API using SigV4 signing.
type AWSProvider struct {
	name      string
	region    string
	accessID  string
	secretKey string
	client    *AWSAPIClient // real API client
}

func NewAWSProvider(cfg config.CloudProviderConfig) (*AWSProvider, error) {
	p := &AWSProvider{
		name:      cfg.Name,
		region:    cfg.Region,
		accessID:  cfg.AccessKeyID,
		secretKey: cfg.AccessKeySecret,
	}
	if cfg.AccessKeyID != "" && cfg.AccessKeySecret != "" {
		p.client = NewAWSAPIClient(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.Region)
	}
	return p, nil
}

func (p *AWSProvider) Name() string                    { return p.name }
func (p *AWSProvider) Type() common.CloudProviderType  { return common.CloudProviderAWS }
func (p *AWSProvider) Region() string                  { return p.region }

func (p *AWSProvider) Ping(ctx context.Context) error {
	if p.client == nil {
		return nil
	}
	_, err := p.client.ListEKSClusters(ctx)
	if err != nil {
		return fmt.Errorf("AWS connectivity check failed: %w", err)
	}
	return nil
}

func (p *AWSProvider) ListClusters(ctx context.Context) ([]*ClusterInfo, error) {
	if p.client == nil {
		return []*ClusterInfo{}, nil
	}
	names, err := p.client.ListEKSClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list EKS clusters: %w", err)
	}
	clusters := make([]*ClusterInfo, 0, len(names))
	for _, name := range names {
		c, err := p.client.DescribeEKSCluster(ctx, name)
		if err != nil {
			continue
		}
		clusters = append(clusters, &ClusterInfo{
			ID:                c.Name,
			Name:              c.Name,
			Provider:          common.CloudProviderAWS,
			Region:            p.region,
			KubernetesVersion: c.Version,
			Status:            c.Status,
			Endpoint:          c.Endpoint,
		})
	}
	return clusters, nil
}

func (p *AWSProvider) GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("AWS credentials not configured")
	}
	c, err := p.client.DescribeEKSCluster(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get EKS cluster %s: %w", clusterID, err)
	}
	return &ClusterInfo{
		ID:                c.Name,
		Name:              c.Name,
		Provider:          common.CloudProviderAWS,
		Region:            p.region,
		KubernetesVersion: c.Version,
		Status:            c.Status,
		Endpoint:          c.Endpoint,
	}, nil
}

func (p *AWSProvider) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("AWS credentials not configured")
	}
	c, err := p.client.CreateEKSCluster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create EKS cluster: %w", err)
	}
	return &ClusterInfo{
		ID:       c.Name,
		Name:     c.Name,
		Provider: common.CloudProviderAWS,
		Region:   p.region,
		Status:   c.Status,
	}, nil
}

func (p *AWSProvider) DeleteCluster(ctx context.Context, clusterID string) error {
	if p.client == nil {
		return fmt.Errorf("AWS credentials not configured")
	}
	return p.client.DeleteEKSCluster(ctx, clusterID)
}

func (p *AWSProvider) ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error {
	if p.client == nil {
		return fmt.Errorf("AWS credentials not configured")
	}
	nodegroups, err := p.client.ListEKSNodegroups(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("failed to list nodegroups: %w", err)
	}
	if len(nodegroups) == 0 {
		return fmt.Errorf("no node groups found for cluster %s", clusterID)
	}
	return p.client.UpdateEKSNodegroupScaling(ctx, clusterID, nodegroups[0], nodeCount)
}

func (p *AWSProvider) GetKubeConfig(ctx context.Context, clusterID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("AWS credentials not configured")
	}
	// AWS does not provide kubeconfig directly via EKS API; use aws eks get-token
	c, err := p.client.DescribeEKSCluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("apiVersion: v1\nclusters:\n- cluster:\n    server: %s\n    certificate-authority-data: %s\n  name: %s",
		c.Endpoint, c.CertificateAuthority.Data, c.Name), nil
}

func (p *AWSProvider) ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error) {
	if p.client == nil {
		return []*NodeInfo{}, nil
	}
	nodegroups, err := p.client.ListEKSNodegroups(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	var nodes []*NodeInfo
	for _, ngName := range nodegroups {
		ng, err := p.client.DescribeEKSNodegroup(ctx, clusterID, ngName)
		if err != nil {
			continue
		}
		instType := ""
		if len(ng.InstanceTypes) > 0 {
			instType = ng.InstanceTypes[0]
		}
		for i := 0; i < ng.ScalingConfig.DesiredSize; i++ {
			nodes = append(nodes, &NodeInfo{
				ID:           fmt.Sprintf("%s-%d", ngName, i),
				Name:         fmt.Sprintf("%s-node-%d", ngName, i),
				Status:       ng.Status,
				Role:         "worker",
				InstanceType: instType,
			})
		}
	}
	return nodes, nil
}

func (p *AWSProvider) GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error) {
	return &NodeMetrics{NodeID: nodeID}, fmt.Errorf("node metrics require CloudWatch integration; use Prometheus in-cluster")
}

func (p *AWSProvider) ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error) {
	return []*GPUInstanceInfo{
		{
			InstanceType:  "p5.48xlarge",
			GPUType:       "nvidia-h100",
			GPUCount:      8,
			GPUMemoryGB:   640,
			CPUCores:      192,
			MemoryGB:      2048,
			PricePerHour:  98.32,
			SpotAvailable: true,
			SpotPrice:     39.33,
			Availability:  "limited",
		},
		{
			InstanceType:  "p4d.24xlarge",
			GPUType:       "nvidia-a100",
			GPUCount:      8,
			GPUMemoryGB:   320,
			CPUCores:      96,
			MemoryGB:      1152,
			PricePerHour:  32.77,
			SpotAvailable: true,
			SpotPrice:     13.11,
			Availability:  "available",
		},
	}, nil
}

func (p *AWSProvider) GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error) {
	return &GPUPricing{
		GPUType:       gpuType,
		OnDemandPrice: 32.77,
		SpotPrice:     13.11,
		ReservedPrice: 19.66,
		Currency:      "USD",
	}, nil
}

func (p *AWSProvider) GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	return &CostSummary{TotalCost: 0, Currency: "USD"}, nil
}

// =============================================================================

// AzureProvider implements Provider for Microsoft Azure (AKS/Azure ML)
// with REAL SDK calls to Azure ARM API using OAuth2 Client Credentials.
type AzureProvider struct {
	name           string
	region         string
	subscriptionID string
	tenantID       string
	clientID       string
	clientSecret   string
	resourceGroup  string
	client         *AzureAPIClient // real API client
}

func NewAzureProvider(cfg config.CloudProviderConfig) (*AzureProvider, error) {
	p := &AzureProvider{
		name:           cfg.Name,
		region:         cfg.Region,
		subscriptionID: cfg.Extra["subscription_id"],
		tenantID:       cfg.Extra["tenant_id"],
		clientID:       cfg.AccessKeyID,
		clientSecret:   cfg.AccessKeySecret,
		resourceGroup:  cfg.Extra["resource_group"],
	}
	if cfg.AccessKeyID != "" && cfg.AccessKeySecret != "" && p.tenantID != "" && p.subscriptionID != "" {
		p.client = NewAzureAPIClient(p.subscriptionID, p.tenantID, cfg.AccessKeyID, cfg.AccessKeySecret)
	}
	if p.resourceGroup == "" {
		p.resourceGroup = "cloudai-fusion-rg"
	}
	return p, nil
}

func (p *AzureProvider) Name() string                    { return p.name }
func (p *AzureProvider) Type() common.CloudProviderType  { return common.CloudProviderAzure }
func (p *AzureProvider) Region() string                  { return p.region }

func (p *AzureProvider) Ping(ctx context.Context) error {
	if p.client == nil {
		return nil
	}
	_, err := p.client.ListAKSClusters(ctx, p.resourceGroup)
	if err != nil {
		return fmt.Errorf("Azure connectivity check failed: %w", err)
	}
	return nil
}

func (p *AzureProvider) ListClusters(ctx context.Context) ([]*ClusterInfo, error) {
	if p.client == nil {
		return []*ClusterInfo{}, nil
	}
	aksClusters, err := p.client.ListAKSClusters(ctx, p.resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("failed to list AKS clusters: %w", err)
	}
	clusters := make([]*ClusterInfo, 0, len(aksClusters))
	for _, c := range aksClusters {
		nodeCount := 0
		for _, pool := range c.Properties.AgentPoolProfiles {
			nodeCount += pool.Count
		}
		clusters = append(clusters, &ClusterInfo{
			ID:                c.Name,
			Name:              c.Name,
			Provider:          common.CloudProviderAzure,
			Region:            c.Location,
			KubernetesVersion: c.Properties.KubernetesVersion,
			Status:            c.Properties.ProvisioningState,
			Endpoint:          c.Properties.Fqdn,
			NodeCount:         nodeCount,
		})
	}
	return clusters, nil
}

func (p *AzureProvider) GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("Azure credentials not configured")
	}
	c, err := p.client.GetAKSCluster(ctx, p.resourceGroup, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get AKS cluster %s: %w", clusterID, err)
	}
	nodeCount := 0
	for _, pool := range c.Properties.AgentPoolProfiles {
		nodeCount += pool.Count
	}
	return &ClusterInfo{
		ID:                c.Name,
		Name:              c.Name,
		Provider:          common.CloudProviderAzure,
		Region:            c.Location,
		KubernetesVersion: c.Properties.KubernetesVersion,
		Status:            c.Properties.ProvisioningState,
		Endpoint:          c.Properties.Fqdn,
		NodeCount:         nodeCount,
	}, nil
}

func (p *AzureProvider) CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error) {
	if p.client == nil {
		return nil, fmt.Errorf("Azure credentials not configured")
	}
	c, err := p.client.CreateAKSCluster(ctx, p.resourceGroup, req, p.region)
	if err != nil {
		return nil, fmt.Errorf("failed to create AKS cluster: %w", err)
	}
	return &ClusterInfo{
		ID:       c.Name,
		Name:     c.Name,
		Provider: common.CloudProviderAzure,
		Region:   c.Location,
		Status:   c.Properties.ProvisioningState,
	}, nil
}

func (p *AzureProvider) DeleteCluster(ctx context.Context, clusterID string) error {
	if p.client == nil {
		return fmt.Errorf("Azure credentials not configured")
	}
	return p.client.DeleteAKSCluster(ctx, p.resourceGroup, clusterID)
}

func (p *AzureProvider) ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error {
	if p.client == nil {
		return fmt.Errorf("Azure credentials not configured")
	}
	// AKS scaling is done via PUT on the cluster with updated agentPoolProfiles count
	c, err := p.client.GetAKSCluster(ctx, p.resourceGroup, clusterID)
	if err != nil {
		return err
	}
	if len(c.Properties.AgentPoolProfiles) > 0 {
		req := &CreateClusterRequest{
			Name:              clusterID,
			KubernetesVersion: c.Properties.KubernetesVersion,
			NodeCount:         nodeCount,
			NodeType:          c.Properties.AgentPoolProfiles[0].VMSize,
		}
		_, err = p.client.CreateAKSCluster(ctx, p.resourceGroup, req, c.Location)
		return err
	}
	return fmt.Errorf("no agent pool found for cluster %s", clusterID)
}

func (p *AzureProvider) GetKubeConfig(ctx context.Context, clusterID string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("Azure credentials not configured")
	}
	creds, err := p.client.GetAKSCredentials(ctx, p.resourceGroup, clusterID)
	if err != nil {
		return "", err
	}
	if len(creds.Kubeconfigs) > 0 {
		return creds.Kubeconfigs[0].Value, nil
	}
	return "", fmt.Errorf("no kubeconfig returned for cluster %s", clusterID)
}

func (p *AzureProvider) ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error) {
	if p.client == nil {
		return []*NodeInfo{}, nil
	}
	c, err := p.client.GetAKSCluster(ctx, p.resourceGroup, clusterID)
	if err != nil {
		return nil, err
	}
	var nodes []*NodeInfo
	for _, pool := range c.Properties.AgentPoolProfiles {
		for i := 0; i < pool.Count; i++ {
			nodes = append(nodes, &NodeInfo{
				ID:           fmt.Sprintf("%s-%d", pool.Name, i),
				Name:         fmt.Sprintf("%s-%s-%d", clusterID, pool.Name, i),
				Status:       pool.ProvisioningState,
				Role:         pool.Mode,
				InstanceType: pool.VMSize,
			})
		}
	}
	return nodes, nil
}

func (p *AzureProvider) GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error) {
	return &NodeMetrics{NodeID: nodeID}, fmt.Errorf("node metrics require Azure Monitor integration; use Prometheus in-cluster")
}

func (p *AzureProvider) ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error) {
	return []*GPUInstanceInfo{
		{
			InstanceType:  "Standard_ND96amsr_A100_v4",
			GPUType:       "nvidia-a100",
			GPUCount:      8,
			GPUMemoryGB:   640,
			CPUCores:      96,
			MemoryGB:      1900,
			PricePerHour:  27.20,
			SpotAvailable: true,
			SpotPrice:     8.16,
			Availability:  "available",
		},
	}, nil
}

func (p *AzureProvider) GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error) {
	return &GPUPricing{
		GPUType:       gpuType,
		OnDemandPrice: 27.20,
		SpotPrice:     8.16,
		ReservedPrice: 16.32,
		Currency:      "USD",
	}, nil
}

func (p *AzureProvider) GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	return &CostSummary{TotalCost: 0, Currency: "USD"}, nil
}
