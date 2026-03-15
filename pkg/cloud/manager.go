// Package cloud provides multi-cloud provider abstraction layer.
// Supports Alibaba Cloud (ACK), AWS (EKS), Azure (AKS), GCP (GKE),
// Huawei Cloud (CCE), and Tencent Cloud (TKE).
package cloud

import (
	"context"
	"fmt"
	"sync"
	"time"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
)

// ============================================================================
// Provider Interface
// ============================================================================

// Provider defines the unified interface all cloud providers must implement
type Provider interface {
	// Identity
	Name() string
	Type() common.CloudProviderType
	Region() string

	// Cluster Operations
	ListClusters(ctx context.Context) ([]*ClusterInfo, error)
	GetCluster(ctx context.Context, clusterID string) (*ClusterInfo, error)
	CreateCluster(ctx context.Context, req *CreateClusterRequest) (*ClusterInfo, error)
	DeleteCluster(ctx context.Context, clusterID string) error
	ScaleCluster(ctx context.Context, clusterID string, nodeCount int) error
	GetKubeConfig(ctx context.Context, clusterID string) (string, error)

	// Node Operations
	ListNodes(ctx context.Context, clusterID string) ([]*NodeInfo, error)
	GetNodeMetrics(ctx context.Context, clusterID, nodeID string) (*NodeMetrics, error)

	// GPU/Accelerator Operations
	ListGPUInstances(ctx context.Context) ([]*GPUInstanceInfo, error)
	GetGPUPricing(ctx context.Context, gpuType string) (*GPUPricing, error)

	// Cost & Billing
	GetCostSummary(ctx context.Context, startTime, endTime string) (*CostSummary, error)

	// Health Check
	Ping(ctx context.Context) error
}

// ============================================================================
// Data Types
// ============================================================================

// ClusterInfo represents a Kubernetes cluster from any cloud provider
type ClusterInfo struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	Provider          common.CloudProviderType `json:"provider"`
	Region            string                `json:"region"`
	KubernetesVersion string               `json:"kubernetes_version"`
	Status            string                `json:"status"`
	Endpoint          string                `json:"endpoint"`
	NodeCount         int                   `json:"node_count"`
	GPUNodeCount      int                   `json:"gpu_node_count"`
	TotalCPU          int64                 `json:"total_cpu_millicores"`
	TotalMemory       int64                 `json:"total_memory_bytes"`
	TotalGPU          int                   `json:"total_gpu"`
	CreatedAt         string                `json:"created_at"`
	Tags              map[string]string     `json:"tags,omitempty"`
}

// CreateClusterRequest defines parameters for creating a new cluster
type CreateClusterRequest struct {
	Name              string            `json:"name" binding:"required"`
	KubernetesVersion string            `json:"kubernetes_version"`
	NodeCount         int               `json:"node_count" binding:"required,min=1"`
	NodeType          string            `json:"node_type" binding:"required"`
	GPUNodeCount      int               `json:"gpu_node_count"`
	GPUNodeType       string            `json:"gpu_node_type"`
	VPCConfig         *VPCConfig        `json:"vpc_config,omitempty"`
	Tags              map[string]string  `json:"tags,omitempty"`
}

// VPCConfig defines VPC/network configuration
type VPCConfig struct {
	VPCID    string   `json:"vpc_id,omitempty"`
	SubnetIDs []string `json:"subnet_ids,omitempty"`
	CIDR     string   `json:"cidr,omitempty"`
}

// NodeInfo represents a single node in a cluster
type NodeInfo struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Status           string            `json:"status"`
	Role             string            `json:"role"`
	InstanceType     string            `json:"instance_type"`
	IPAddress        string            `json:"ip_address"`
	CPUCapacity      int64             `json:"cpu_capacity_millicores"`
	MemoryCapacity   int64             `json:"memory_capacity_bytes"`
	GPUType          string            `json:"gpu_type,omitempty"`
	GPUCount         int               `json:"gpu_count"`
	GPUMemoryBytes   int64             `json:"gpu_memory_bytes"`
	OSImage          string            `json:"os_image"`
	KernelVersion    string            `json:"kernel_version"`
	ContainerRuntime string            `json:"container_runtime"`
	Labels           map[string]string `json:"labels,omitempty"`
	Conditions       []NodeCondition   `json:"conditions,omitempty"`
}

// NodeCondition represents a node health condition
type NodeCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// NodeMetrics holds real-time node resource metrics
type NodeMetrics struct {
	NodeID           string  `json:"node_id"`
	CPUUsagePercent  float64 `json:"cpu_usage_percent"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	GPUUtilization   float64 `json:"gpu_utilization_percent"`
	GPUMemoryUsage   float64 `json:"gpu_memory_usage_percent"`
	DiskUsagePercent float64 `json:"disk_usage_percent"`
	NetworkRxBytes   int64   `json:"network_rx_bytes_per_sec"`
	NetworkTxBytes   int64   `json:"network_tx_bytes_per_sec"`
}

// GPUInstanceInfo describes an available GPU instance type
type GPUInstanceInfo struct {
	InstanceType  string  `json:"instance_type"`
	GPUType       string  `json:"gpu_type"`
	GPUCount      int     `json:"gpu_count"`
	GPUMemoryGB   float64 `json:"gpu_memory_gb"`
	CPUCores      int     `json:"cpu_cores"`
	MemoryGB      float64 `json:"memory_gb"`
	PricePerHour  float64 `json:"price_per_hour"`
	SpotAvailable bool    `json:"spot_available"`
	SpotPrice     float64 `json:"spot_price_per_hour,omitempty"`
	Availability  string  `json:"availability"`
}

// GPUPricing represents GPU pricing information
type GPUPricing struct {
	GPUType       string  `json:"gpu_type"`
	OnDemandPrice float64 `json:"on_demand_price_per_hour"`
	SpotPrice     float64 `json:"spot_price_per_hour"`
	ReservedPrice float64 `json:"reserved_price_per_hour"`
	Currency      string  `json:"currency"`
}

// CostSummary provides cost breakdown for a time period
type CostSummary struct {
	TotalCost     float64           `json:"total_cost"`
	Currency      string            `json:"currency"`
	Period        string            `json:"period"`
	ByService     map[string]float64 `json:"by_service"`
	ByResourceType map[string]float64 `json:"by_resource_type"`
	GPUCost       float64           `json:"gpu_cost"`
	ComputeCost   float64           `json:"compute_cost"`
	StorageCost   float64           `json:"storage_cost"`
	NetworkCost   float64           `json:"network_cost"`
}

// ============================================================================
// Manager
// ============================================================================

// ManagerConfig holds cloud manager configuration
type ManagerConfig struct {
	Providers []config.CloudProviderConfig
}

// Manager manages multiple cloud providers
type Manager struct {
	providers map[string]Provider
	mu        sync.RWMutex
}

// NewManager creates a new cloud provider manager
func NewManager(cfg ManagerConfig) (*Manager, error) {
	m := &Manager{
		providers: make(map[string]Provider),
	}

	for _, providerCfg := range cfg.Providers {
		provider, err := createProvider(providerCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider %s: %w", providerCfg.Name, err)
		}
		m.providers[providerCfg.Name] = provider
	}

	return m, nil
}

// RegisterProvider adds a new cloud provider at runtime
func (m *Manager) RegisterProvider(name string, provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[name] = provider
}

// GetProvider returns a specific provider by name
func (m *Manager) GetProvider(name string) (Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[name]
	if !ok {
		return nil, apperrors.NotFound("cloud provider", name)
	}
	return p, nil
}

// ListProviders returns all registered providers
func (m *Manager) ListProviders() []Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		result = append(result, p)
	}
	return result
}

// ListAllClusters queries all providers and aggregates cluster info
func (m *Manager) ListAllClusters(ctx context.Context) ([]*ClusterInfo, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allClusters []*ClusterInfo
	var errs []error

	for name, provider := range m.providers {
		// Apply per-provider timeout
		pCtx, pCancel := context.WithTimeout(ctx, time.Duration(apperrors.ExternalCallTimeout)*time.Second)
		clusters, err := provider.ListClusters(pCtx)
		pCancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("provider %s: %w", name, err))
			continue
		}
		allClusters = append(allClusters, clusters...)
	}

	if len(errs) > 0 && len(allClusters) == 0 {
		return nil, fmt.Errorf("all providers failed: %v", errs)
	}

	return allClusters, nil
}

// GetTotalCost aggregates cost across all providers
func (m *Manager) GetTotalCost(ctx context.Context, startTime, endTime string) (*CostSummary, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := &CostSummary{
		Currency:       "USD",
		ByService:      make(map[string]float64),
		ByResourceType: make(map[string]float64),
	}

	for _, provider := range m.providers {
		pCtx, pCancel := context.WithTimeout(ctx, time.Duration(apperrors.ExternalCallTimeout)*time.Second)
		summary, err := provider.GetCostSummary(pCtx, startTime, endTime)
		pCancel()
		if err != nil {
			continue
		}
		total.TotalCost += summary.TotalCost
		total.GPUCost += summary.GPUCost
		total.ComputeCost += summary.ComputeCost
		total.StorageCost += summary.StorageCost
		total.NetworkCost += summary.NetworkCost
	}

	return total, nil
}

// createProvider is a factory function that creates the appropriate provider
func createProvider(cfg config.CloudProviderConfig) (Provider, error) {
	switch common.CloudProviderType(cfg.Type) {
	case common.CloudProviderAliyun:
		return NewAliyunProvider(cfg)
	case common.CloudProviderAWS:
		return NewAWSProvider(cfg)
	case common.CloudProviderAzure:
		return NewAzureProvider(cfg)
	case common.CloudProviderGCP:
		return NewGCPProvider(cfg)
	case common.CloudProviderHuawei:
		return NewHuaweiProvider(cfg)
	case common.CloudProviderTencent:
		return NewTencentProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported cloud provider type: %s", cfg.Type)
	}
}
