// Package cloud - Tencent Cloud TKE (Tencent Kubernetes Engine) SDK
// Uses the official Tencent Cloud SDK for Go (github.com/tencentcloud/tencentcloud-sdk-go).
// API Reference: https://cloud.tencent.com/document/product/457/31862
package cloud

import (
	"context"
	"fmt"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	tke "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/tke/v20180525"
)

// ============================================================================
// Tencent Cloud TKE Client (Official SDK)
// ============================================================================

// TencentAPIClient wraps the official Tencent Cloud TKE SDK client
type TencentAPIClient struct {
	tkeClient *tke.Client
	region    string
}

// NewTencentAPIClient creates a new Tencent Cloud TKE API client using the official SDK
func NewTencentAPIClient(secretID, secretKey, region string) *TencentAPIClient {
	credential := tccommon.NewCredential(secretID, secretKey)
	cpf := tcprofile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "tke.tencentcloudapi.com"

	client, err := tke.NewClient(credential, region, cpf)
	if err != nil {
		return &TencentAPIClient{region: region}
	}

	return &TencentAPIClient{
		tkeClient: client,
		region:    region,
	}
}

// ============================================================================
// TKE API Types
// ============================================================================

// TKECluster represents a Tencent Cloud TKE cluster
type TKECluster struct {
	ClusterID               string `json:"ClusterId"`
	ClusterName             string `json:"ClusterName"`
	ClusterDescription      string `json:"ClusterDescription"`
	ClusterVersion          string `json:"ClusterVersion"`
	ClusterOs               string `json:"ClusterOs"`
	ClusterType             string `json:"ClusterType"` // MANAGED_CLUSTER, INDEPENDENT_CLUSTER
	ClusterStatus           string `json:"ClusterStatus"`
	ClusterNodeNum          int    `json:"ClusterNodeNum"`
	ClusterExternalEndpoint string `json:"ClusterExternalEndpoint"`
	ProjectId               int    `json:"ProjectId"`
	CreatedTime             string `json:"CreatedTime"`
}

// TKEInstance represents a TKE cluster node instance
type TKEInstance struct {
	InstanceID    string `json:"InstanceId"`
	InstanceRole  string `json:"InstanceRole"`
	InstanceState string `json:"InstanceState"`
	LanIP         string `json:"LanIP"`
	InstanceType  string `json:"InstanceType"`
	CreatedTime   string `json:"CreatedTime"`
}

// ============================================================================
// TKE API Methods
// ============================================================================

// ListTKEClusters lists all TKE clusters in the region
func (c *TencentAPIClient) ListTKEClusters(_ context.Context) ([]TKECluster, error) {
	if c.tkeClient == nil {
		return nil, fmt.Errorf("Tencent Cloud TKE SDK client not initialized")
	}

	request := tke.NewDescribeClustersRequest()
	response, err := c.tkeClient.DescribeClusters(request)
	if err != nil {
		return nil, fmt.Errorf("TKE DescribeClusters failed: %w", err)
	}

	clusters := make([]TKECluster, 0)
	if response.Response != nil && response.Response.Clusters != nil {
		for _, sc := range response.Response.Clusters {
			clusters = append(clusters, sdkClusterToTKE(sc))
		}
	}
	return clusters, nil
}

// GetTKECluster retrieves a specific TKE cluster
func (c *TencentAPIClient) GetTKECluster(_ context.Context, clusterID string) (*TKECluster, error) {
	if c.tkeClient == nil {
		return nil, fmt.Errorf("Tencent Cloud TKE SDK client not initialized")
	}

	request := tke.NewDescribeClustersRequest()
	request.ClusterIds = []*string{&clusterID}

	response, err := c.tkeClient.DescribeClusters(request)
	if err != nil {
		return nil, fmt.Errorf("TKE DescribeClusters failed: %w", err)
	}

	if response.Response == nil || len(response.Response.Clusters) == 0 {
		return nil, fmt.Errorf("TKE cluster '%s' not found", clusterID)
	}

	cluster := sdkClusterToTKE(response.Response.Clusters[0])
	return &cluster, nil
}

// CreateTKECluster creates a new TKE cluster
func (c *TencentAPIClient) CreateTKECluster(_ context.Context, req *CreateClusterRequest) (string, error) {
	if c.tkeClient == nil {
		return "", fmt.Errorf("Tencent Cloud TKE SDK client not initialized")
	}

	request := tke.NewCreateClusterRequest()
	clusterType := "MANAGED_CLUSTER"
	clusterCIDR := "172.16.0.0/16"
	request.ClusterType = &clusterType
	request.ClusterCIDRSettings = &tke.ClusterCIDRSettings{
		ClusterCIDR: &clusterCIDR,
	}

	response, err := c.tkeClient.CreateCluster(request)
	if err != nil {
		return "", fmt.Errorf("TKE CreateCluster failed: %w", err)
	}

	if response.Response != nil && response.Response.ClusterId != nil {
		return *response.Response.ClusterId, nil
	}
	return "", fmt.Errorf("TKE CreateCluster returned no cluster ID")
}

// DeleteTKECluster deletes a TKE cluster
func (c *TencentAPIClient) DeleteTKECluster(_ context.Context, clusterID string) error {
	if c.tkeClient == nil {
		return fmt.Errorf("Tencent Cloud TKE SDK client not initialized")
	}

	request := tke.NewDeleteClusterRequest()
	request.ClusterId = &clusterID
	instanceDeleteMode := "terminate"
	request.InstanceDeleteMode = &instanceDeleteMode

	_, err := c.tkeClient.DeleteCluster(request)
	if err != nil {
		return fmt.Errorf("TKE DeleteCluster failed: %w", err)
	}
	return nil
}

// ListTKEInstances lists nodes in a TKE cluster
func (c *TencentAPIClient) ListTKEInstances(_ context.Context, clusterID string) ([]TKEInstance, error) {
	if c.tkeClient == nil {
		return nil, fmt.Errorf("Tencent Cloud TKE SDK client not initialized")
	}

	request := tke.NewDescribeClusterInstancesRequest()
	request.ClusterId = &clusterID
	instanceRole := "WORKER"
	request.InstanceRole = &instanceRole

	response, err := c.tkeClient.DescribeClusterInstances(request)
	if err != nil {
		return nil, fmt.Errorf("TKE DescribeClusterInstances failed: %w", err)
	}

	instances := make([]TKEInstance, 0)
	if response.Response != nil && response.Response.InstanceSet != nil {
		for _, si := range response.Response.InstanceSet {
			instances = append(instances, sdkInstanceToTKE(si))
		}
	}
	return instances, nil
}

// GetTKEKubeConfig retrieves kubeconfig for a TKE cluster
func (c *TencentAPIClient) GetTKEKubeConfig(_ context.Context, clusterID string) (string, error) {
	if c.tkeClient == nil {
		return "", fmt.Errorf("Tencent Cloud TKE SDK client not initialized")
	}

	request := tke.NewDescribeClusterKubeconfigRequest()
	request.ClusterId = &clusterID

	response, err := c.tkeClient.DescribeClusterKubeconfig(request)
	if err != nil {
		return "", fmt.Errorf("TKE DescribeClusterKubeconfig failed: %w", err)
	}

	if response.Response != nil && response.Response.Kubeconfig != nil {
		return *response.Response.Kubeconfig, nil
	}
	return "", fmt.Errorf("TKE returned empty kubeconfig")
}

// ============================================================================
// SDK type -> local type conversion
// ============================================================================

func sdkClusterToTKE(sc *tke.Cluster) TKECluster {
	cluster := TKECluster{}
	if sc.ClusterId != nil {
		cluster.ClusterID = *sc.ClusterId
	}
	if sc.ClusterName != nil {
		cluster.ClusterName = *sc.ClusterName
	}
	if sc.ClusterDescription != nil {
		cluster.ClusterDescription = *sc.ClusterDescription
	}
	if sc.ClusterVersion != nil {
		cluster.ClusterVersion = *sc.ClusterVersion
	}
	if sc.ClusterOs != nil {
		cluster.ClusterOs = *sc.ClusterOs
	}
	if sc.ClusterType != nil {
		cluster.ClusterType = *sc.ClusterType
	}
	if sc.ClusterStatus != nil {
		cluster.ClusterStatus = *sc.ClusterStatus
	}
	if sc.ClusterNodeNum != nil {
		cluster.ClusterNodeNum = int(*sc.ClusterNodeNum)
	}
	if sc.ProjectId != nil {
		cluster.ProjectId = int(*sc.ProjectId)
	}
	if sc.CreatedTime != nil {
		cluster.CreatedTime = *sc.CreatedTime
	}
	return cluster
}

func sdkInstanceToTKE(si *tke.Instance) TKEInstance {
	inst := TKEInstance{}
	if si.InstanceId != nil {
		inst.InstanceID = *si.InstanceId
	}
	if si.InstanceRole != nil {
		inst.InstanceRole = *si.InstanceRole
	}
	if si.InstanceState != nil {
		inst.InstanceState = *si.InstanceState
	}
	if si.LanIP != nil {
		inst.LanIP = *si.LanIP
	}
	if si.CreatedTime != nil {
		inst.CreatedTime = *si.CreatedTime
	}
	return inst
}
