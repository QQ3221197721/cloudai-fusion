// Package cloud - Alibaba Cloud ACK (Container Service for Kubernetes) SDK
// Uses the official Alibaba Cloud SDK for Go (github.com/aliyun/alibaba-cloud-sdk-go).
// API Reference: https://help.aliyun.com/document_detail/87984.html
package cloud

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk"
	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
)

// ============================================================================
// Alibaba Cloud ACK Client (Official SDK with CommonRequest for ROA APIs)
// ============================================================================

// AliyunAPIClient wraps the official Alibaba Cloud SDK CommonRequest client
// for ACK ROA-style API calls.
type AliyunAPIClient struct {
	sdkClient *sdk.Client
	region    string
	endpoint  string
}

// NewAliyunAPIClient creates a new Alibaba Cloud API client using the official SDK
func NewAliyunAPIClient(accessKeyID, accessKeySecret, region string) *AliyunAPIClient {
	endpoint := fmt.Sprintf("cs.%s.aliyuncs.com", region)

	client, err := sdk.NewClientWithAccessKey(region, accessKeyID, accessKeySecret)
	if err != nil {
		return &AliyunAPIClient{region: region, endpoint: endpoint}
	}

	return &AliyunAPIClient{
		sdkClient: client,
		region:    region,
		endpoint:  endpoint,
	}
}

// doROARequest performs a signed ROA-style API request via the official SDK
func (c *AliyunAPIClient) doROARequest(_ context.Context, method, pathPattern, body string) ([]byte, int, error) {
	if c.sdkClient == nil {
		return nil, 0, fmt.Errorf("alibaba cloud SDK client not initialized (set CLOUDAI_CLOUD_PROVIDERS)")
	}

	request := requests.NewCommonRequest()
	request.Method = method
	request.Scheme = "https"
	request.Domain = c.endpoint
	request.Version = "2015-12-15"
	request.PathPattern = pathPattern
	request.Headers["Content-Type"] = "application/json"
	request.Headers["Accept"] = "application/json"

	if body != "" {
		request.SetContent([]byte(body))
	}

	response, err := c.sdkClient.ProcessCommonRequest(request)
	if err != nil {
		return nil, 0, fmt.Errorf("ACK API request failed: %w", err)
	}

	return response.GetHttpContentBytes(), response.GetHttpStatus(), nil
}

// ============================================================================
// ACK API Types
// ============================================================================

// ACKCluster represents an ACK cluster from the DescribeClusters API
type ACKCluster struct {
	ClusterID        string `json:"cluster_id"`
	Name             string `json:"name"`
	RegionID         string `json:"region_id"`
	State            string `json:"state"`
	ClusterType      string `json:"cluster_type"`
	Size             int    `json:"size"`
	Version          string `json:"current_version"`
	ExternalEndpoint string `json:"external_endpoint"`
	Created          string `json:"created"`
}

// ACKNode represents a node from DescribeClusterNodes API
type ACKNode struct {
	InstanceID   string   `json:"instance_id"`
	InstanceName string   `json:"instance_name"`
	InstanceType string   `json:"instance_type"`
	InstanceRole string   `json:"instance_role"`
	State        string   `json:"state"`
	IPAddress    []string `json:"ip_address"`
	HostName     string   `json:"host_name"`
}

// ACKNodePage wraps the paginated node list response
type ACKNodePage struct {
	Nodes []ACKNode `json:"nodes"`
	Page  struct {
		TotalCount int `json:"total_count"`
		PageNumber int `json:"page_number"`
		PageSize   int `json:"page_size"`
	} `json:"page"`
}

// ============================================================================
// ACK API Methods
// ============================================================================

// DescribeClusters lists all ACK clusters in the region
func (c *AliyunAPIClient) DescribeClusters(ctx context.Context) ([]ACKCluster, error) {
	body, statusCode, err := c.doROARequest(ctx, "GET", "/api/v1/clusters", "")
	if err != nil {
		return nil, err
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("ACK API returned HTTP %d: %s", statusCode, string(body))
	}

	var clusters []ACKCluster
	if err := json.Unmarshal(body, &clusters); err != nil {
		return nil, fmt.Errorf("failed to parse cluster list: %w", err)
	}
	return clusters, nil
}

// DescribeClusterDetail gets detailed info for a specific cluster
func (c *AliyunAPIClient) DescribeClusterDetail(ctx context.Context, clusterID string) (*ACKCluster, error) {
	path := fmt.Sprintf("/api/v1/clusters/%s", clusterID)
	body, statusCode, err := c.doROARequest(ctx, "GET", path, "")
	if err != nil {
		return nil, err
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("ACK API returned HTTP %d: %s", statusCode, string(body))
	}

	var cluster ACKCluster
	if err := json.Unmarshal(body, &cluster); err != nil {
		return nil, fmt.Errorf("failed to parse cluster detail: %w", err)
	}
	return &cluster, nil
}

// DescribeClusterNodes lists nodes in a cluster
func (c *AliyunAPIClient) DescribeClusterNodes(ctx context.Context, clusterID string) ([]ACKNode, error) {
	path := fmt.Sprintf("/api/v1/clusters/%s/nodes", clusterID)
	body, statusCode, err := c.doROARequest(ctx, "GET", path, "")
	if err != nil {
		return nil, err
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("ACK API returned HTTP %d: %s", statusCode, string(body))
	}

	var page ACKNodePage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("failed to parse node list: %w", err)
	}
	return page.Nodes, nil
}

// DescribeClusterUserKubeconfig retrieves the kubeconfig for a cluster
func (c *AliyunAPIClient) DescribeClusterUserKubeconfig(ctx context.Context, clusterID string) (string, error) {
	path := fmt.Sprintf("/api/v1/clusters/%s/kubeconfig", clusterID)
	body, statusCode, err := c.doROARequest(ctx, "GET", path, "")
	if err != nil {
		return "", err
	}
	if statusCode != 200 {
		return "", fmt.Errorf("ACK API returned HTTP %d: %s", statusCode, string(body))
	}

	var result struct {
		Config string `json:"config"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	return result.Config, nil
}

// DeleteACKCluster deletes an ACK cluster
func (c *AliyunAPIClient) DeleteACKCluster(ctx context.Context, clusterID string) error {
	path := fmt.Sprintf("/api/v1/clusters/%s", clusterID)
	body, statusCode, err := c.doROARequest(ctx, "DELETE", path, "")
	if err != nil {
		return err
	}
	if statusCode != 200 && statusCode != 202 {
		return fmt.Errorf("ACK API returned HTTP %d: %s", statusCode, string(body))
	}
	return nil
}

// ScaleOutCluster adds nodes to a cluster
func (c *AliyunAPIClient) ScaleOutCluster(ctx context.Context, clusterID string, count int) error {
	path := fmt.Sprintf("/api/v1/clusters/%s/nodes", clusterID)
	payload := fmt.Sprintf(`{"count":%d}`, count)
	body, statusCode, err := c.doROARequest(ctx, "POST", path, payload)
	if err != nil {
		return err
	}
	if statusCode != 200 && statusCode != 202 {
		return fmt.Errorf("ACK API returned HTTP %d: %s", statusCode, string(body))
	}
	return nil
}

// CreateACKCluster creates a new ACK cluster
func (c *AliyunAPIClient) CreateACKCluster(ctx context.Context, req *CreateClusterRequest) (*ACKCluster, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":                  req.Name,
		"cluster_type":          "ManagedKubernetes",
		"kubernetes_version":    req.KubernetesVersion,
		"region_id":             c.region,
		"num_of_nodes":          req.NodeCount,
		"worker_instance_types": []string{req.NodeType},
	})

	body, statusCode, err := c.doROARequest(ctx, "POST", "/api/v1/clusters", string(payload))
	if err != nil {
		return nil, err
	}
	if statusCode != 200 && statusCode != 202 {
		return nil, fmt.Errorf("ACK API returned HTTP %d: %s", statusCode, string(body))
	}

	var cluster ACKCluster
	if err := json.Unmarshal(body, &cluster); err != nil {
		return nil, fmt.Errorf("failed to parse created cluster: %w", err)
	}
	return &cluster, nil
}
