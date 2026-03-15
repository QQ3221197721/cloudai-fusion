// Package cloud - Huawei Cloud CCE (Cloud Container Engine) SDK
// Uses the official Huawei Cloud SDK for Go v3 (github.com/huaweicloud/huaweicloud-sdk-go-v3).
// API Reference: https://support.huaweicloud.com/api-cce/cce_02_0001.html
package cloud

import (
	"context"
	"fmt"

	hwauth "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	cce "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3"
	ccemodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/cce/v3/model"
)

// ============================================================================
// Huawei Cloud CCE Client (Official SDK v3)
// ============================================================================

// HuaweiAPIClient wraps the official Huawei Cloud CCE SDK v3 client
type HuaweiAPIClient struct {
	cceClient *cce.CceClient
	region    string
	projectID string
}

// NewHuaweiAPIClient creates a new Huawei Cloud CCE API client using the official SDK v3
func NewHuaweiAPIClient(accessKey, secretKey, region, projectID string) *HuaweiAPIClient {
	auth, err := hwauth.NewCredentialsBuilder().
		WithAk(accessKey).
		WithSk(secretKey).
		WithProjectId(projectID).
		SafeBuild()
	if err != nil {
		return &HuaweiAPIClient{region: region, projectID: projectID}
	}

	endpoint := fmt.Sprintf("https://cce.%s.myhuaweicloud.com", region)
	builder := cce.CceClientBuilder().
		WithEndpoint(endpoint).
		WithCredential(auth)

	client, err := builder.SafeBuild()
	if err != nil {
		return &HuaweiAPIClient{region: region, projectID: projectID}
	}

	cceClient := cce.NewCceClient(client)
	return &HuaweiAPIClient{
		cceClient: cceClient,
		region:    region,
		projectID: projectID,
	}
}

// ============================================================================
// CCE API Types
// ============================================================================

// CCECluster represents a Huawei Cloud CCE cluster
type CCECluster struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Metadata   struct {
		UID       string `json:"uid"`
		Name      string `json:"name"`
		CreatedAt string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Category string `json:"category"` // CCE, Turbo
		Type     string `json:"type"`     // VirtualMachine, ARM64
		Flavor   string `json:"flavor"`   // cce.s1.small, etc.
		Version  string `json:"version"`
	} `json:"spec"`
	Status struct {
		Phase     string `json:"phase"` // Available, Unavailable, Creating
		Endpoints []struct {
			URL  string `json:"url"`
			Type string `json:"type"` // Internal, External
		} `json:"endpoints"`
	} `json:"status"`
}

// CCENode represents a CCE cluster node
type CCENode struct {
	Metadata struct {
		UID  string `json:"uid"`
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Flavor   string `json:"flavor"`
		AZ       string `json:"az"`
		PublicIP string `json:"publicIP,omitempty"`
	} `json:"spec"`
	Status struct {
		Phase     string `json:"phase"` // Active, Abnormal
		PrivateIP string `json:"privateIP"`
	} `json:"status"`
}

// ============================================================================
// CCE API Methods
// ============================================================================

// ListCCEClusters lists all CCE clusters in the project
func (c *HuaweiAPIClient) ListCCEClusters(_ context.Context) ([]CCECluster, error) {
	if c.cceClient == nil {
		return nil, fmt.Errorf("Huawei Cloud CCE SDK client not initialized")
	}

	request := &ccemodel.ListClustersRequest{}
	response, err := c.cceClient.ListClusters(request)
	if err != nil {
		return nil, fmt.Errorf("CCE ListClusters failed: %w", err)
	}

	clusters := make([]CCECluster, 0)
	if response.Items != nil {
		for _, sc := range *response.Items {
			clusters = append(clusters, sdkClusterToCCE(&sc))
		}
	}
	return clusters, nil
}

// GetCCECluster retrieves a specific CCE cluster
func (c *HuaweiAPIClient) GetCCECluster(_ context.Context, clusterID string) (*CCECluster, error) {
	if c.cceClient == nil {
		return nil, fmt.Errorf("Huawei Cloud CCE SDK client not initialized")
	}

	request := &ccemodel.ShowClusterRequest{
		ClusterId: clusterID,
	}
	response, err := c.cceClient.ShowCluster(request)
	if err != nil {
		return nil, fmt.Errorf("CCE ShowCluster failed: %w", err)
	}

	cluster := sdkShowClusterToCCE(response)
	return &cluster, nil
}

// CreateCCECluster creates a new CCE cluster
func (c *HuaweiAPIClient) CreateCCECluster(_ context.Context, req *CreateClusterRequest) (*CCECluster, error) {
	if c.cceClient == nil {
		return nil, fmt.Errorf("Huawei Cloud CCE SDK client not initialized")
	}

	kind := "Cluster"
	apiVersion := "v3"
	clusterName := req.Name
	category := ccemodel.GetClusterSpecCategoryEnum().CCE
	clusterType := ccemodel.GetClusterSpecTypeEnum().VIRTUAL_MACHINE
	flavorStr := "cce.s2.medium"
	version := req.KubernetesVersion

	request := &ccemodel.CreateClusterRequest{
		Body: &ccemodel.Cluster{
			Kind:       kind,
			ApiVersion: apiVersion,
			Metadata: &ccemodel.ClusterMetadata{
				Name: clusterName,
			},
			Spec: &ccemodel.ClusterSpec{
				Category: &category,
				Type:     &clusterType,
				Flavor:   flavorStr,
				Version:  &version,
				HostNetwork: &ccemodel.HostNetwork{
					Vpc:    "auto",
					Subnet: "auto",
				},
				ContainerNetwork: &ccemodel.ContainerNetwork{
					Mode: ccemodel.GetContainerNetworkModeEnum().OVERLAY_L2,
					Cidr: strPtrHelper("172.16.0.0/16"),
				},
			},
		},
	}

	response, err := c.cceClient.CreateCluster(request)
	if err != nil {
		return nil, fmt.Errorf("CCE CreateCluster failed: %w", err)
	}

	cluster := sdkCreateClusterToCCE(response)
	return &cluster, nil
}

// DeleteCCECluster deletes a CCE cluster
func (c *HuaweiAPIClient) DeleteCCECluster(_ context.Context, clusterID string) error {
	if c.cceClient == nil {
		return fmt.Errorf("Huawei Cloud CCE SDK client not initialized")
	}

	request := &ccemodel.DeleteClusterRequest{
		ClusterId: clusterID,
	}
	_, err := c.cceClient.DeleteCluster(request)
	if err != nil {
		return fmt.Errorf("CCE DeleteCluster failed: %w", err)
	}
	return nil
}

// ListCCENodes lists nodes in a CCE cluster
func (c *HuaweiAPIClient) ListCCENodes(_ context.Context, clusterID string) ([]CCENode, error) {
	if c.cceClient == nil {
		return nil, fmt.Errorf("Huawei Cloud CCE SDK client not initialized")
	}

	request := &ccemodel.ListNodesRequest{
		ClusterId: clusterID,
	}
	response, err := c.cceClient.ListNodes(request)
	if err != nil {
		return nil, fmt.Errorf("CCE ListNodes failed: %w", err)
	}

	nodes := make([]CCENode, 0)
	if response.Items != nil {
		for _, sn := range *response.Items {
			nodes = append(nodes, sdkNodeToCCE(&sn))
		}
	}
	return nodes, nil
}

// GetCCEKubeConfig retrieves kubeconfig for a CCE cluster
func (c *HuaweiAPIClient) GetCCEKubeConfig(_ context.Context, clusterID string) (string, error) {
	if c.cceClient == nil {
		return "", fmt.Errorf("Huawei Cloud CCE SDK client not initialized")
	}

	request := &ccemodel.CreateKubernetesClusterCertRequest{
		ClusterId: clusterID,
		Body: &ccemodel.ClusterCertDuration{
			Duration: int32Ptr(30), // 30 days validity
		},
	}
	response, err := c.cceClient.CreateKubernetesClusterCert(request)
	if err != nil {
		return "", fmt.Errorf("CCE CreateKubernetesClusterCert failed: %w", err)
	}

	// The response contains the kubeconfig content
	if response != nil {
		// Serialize to YAML/JSON for kubeconfig
		return fmt.Sprintf("%v", response), nil
	}
	return "", fmt.Errorf("CCE returned empty kubeconfig")
}

// ============================================================================
// SDK type -> local type conversion
// ============================================================================

func sdkClusterToCCE(sc *ccemodel.Cluster) CCECluster {
	cluster := CCECluster{
		Kind: sc.Kind,
	}
	if sc.Metadata != nil {
		if sc.Metadata.Uid != nil {
			cluster.Metadata.UID = *sc.Metadata.Uid
		}
		cluster.Metadata.Name = sc.Metadata.Name
		if sc.Metadata.CreationTimestamp != nil {
			cluster.Metadata.CreatedAt = *sc.Metadata.CreationTimestamp
		}
	}
	if sc.Spec != nil {
		cluster.Spec.Flavor = sc.Spec.Flavor
		if sc.Spec.Version != nil {
			cluster.Spec.Version = *sc.Spec.Version
		}
	}
	if sc.Status != nil {
		if sc.Status.Phase != nil {
			cluster.Status.Phase = string(*sc.Status.Phase)
		}
		if sc.Status.Endpoints != nil {
			for _, ep := range *sc.Status.Endpoints {
				epEntry := struct {
					URL  string `json:"url"`
					Type string `json:"type"`
				}{}
				if ep.Url != nil {
					epEntry.URL = *ep.Url
				}
				if ep.Type != nil {
					epEntry.Type = *ep.Type
				}
				cluster.Status.Endpoints = append(cluster.Status.Endpoints, epEntry)
			}
		}
	}
	return cluster
}

func sdkShowClusterToCCE(resp *ccemodel.ShowClusterResponse) CCECluster {
	cluster := CCECluster{}
	if resp.Kind != nil {
		cluster.Kind = *resp.Kind
	}
	if resp.Metadata != nil {
		if resp.Metadata.Uid != nil {
			cluster.Metadata.UID = *resp.Metadata.Uid
		}
		cluster.Metadata.Name = resp.Metadata.Name
		if resp.Metadata.CreationTimestamp != nil {
			cluster.Metadata.CreatedAt = *resp.Metadata.CreationTimestamp
		}
	}
	if resp.Spec != nil {
		cluster.Spec.Flavor = resp.Spec.Flavor
		if resp.Spec.Version != nil {
			cluster.Spec.Version = *resp.Spec.Version
		}
	}
	if resp.Status != nil {
		if resp.Status.Phase != nil {
			cluster.Status.Phase = string(*resp.Status.Phase)
		}
	}
	return cluster
}

func sdkCreateClusterToCCE(resp *ccemodel.CreateClusterResponse) CCECluster {
	cluster := CCECluster{}
	if resp.Kind != nil {
		cluster.Kind = *resp.Kind
	}
	if resp.Metadata != nil {
		if resp.Metadata.Uid != nil {
			cluster.Metadata.UID = *resp.Metadata.Uid
		}
		cluster.Metadata.Name = resp.Metadata.Name
	}
	if resp.Status != nil {
		if resp.Status.Phase != nil {
			cluster.Status.Phase = string(*resp.Status.Phase)
		}
	}
	return cluster
}

func sdkNodeToCCE(sn *ccemodel.Node) CCENode {
	node := CCENode{}
	if sn.Metadata != nil {
		if sn.Metadata.Uid != nil {
			node.Metadata.UID = *sn.Metadata.Uid
		}
		if sn.Metadata.Name != nil {
			node.Metadata.Name = *sn.Metadata.Name
		}
	}
	if sn.Spec != nil {
		node.Spec.Flavor = sn.Spec.Flavor
		node.Spec.AZ = sn.Spec.Az
	}
	if sn.Status != nil {
		if sn.Status.Phase != nil {
			node.Status.Phase = sn.Status.Phase.Value()
		}
		if sn.Status.PrivateIP != nil {
			node.Status.PrivateIP = *sn.Status.PrivateIP
		}
	}
	return node
}

func strPtrHelper(s string) *string {
	return &s
}

func int32Ptr(v int32) *int32 {
	return &v
}
