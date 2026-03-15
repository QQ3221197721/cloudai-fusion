// Package cloud - Azure AKS (Azure Kubernetes Service) SDK
// Uses the official Azure SDK for Go (Track 2).
// API Reference: https://learn.microsoft.com/en-us/rest/api/aks/
package cloud

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v4"
)

// ============================================================================
// Azure AKS Client (Official Azure SDK Track 2)
// ============================================================================

// AzureAPIClient wraps the official Azure SDK AKS managed clusters client
type AzureAPIClient struct {
	aksClient      *armcontainerservice.ManagedClustersClient
	subscriptionID string
}

// NewAzureAPIClient creates a new Azure ARM API client using the official Azure SDK
func NewAzureAPIClient(subscriptionID, tenantID, clientID, clientSecret string) *AzureAPIClient {
	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		return &AzureAPIClient{subscriptionID: subscriptionID}
	}

	client, err := armcontainerservice.NewManagedClustersClient(subscriptionID, cred, nil)
	if err != nil {
		return &AzureAPIClient{subscriptionID: subscriptionID}
	}

	return &AzureAPIClient{
		aksClient:      client,
		subscriptionID: subscriptionID,
	}
}

// ============================================================================
// AKS API Types
// ============================================================================

// AKSCluster represents an AKS managed cluster from the ARM API
type AKSCluster struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags,omitempty"`
	Properties struct {
		ProvisioningState string        `json:"provisioningState"`
		KubernetesVersion string        `json:"kubernetesVersion"`
		Fqdn              string        `json:"fqdn"`
		PowerState        struct {
			Code string `json:"code"`
		} `json:"powerState"`
		AgentPoolProfiles []AKSNodePool `json:"agentPoolProfiles"`
	} `json:"properties"`
}

// AKSNodePool represents an AKS agent pool (node group)
type AKSNodePool struct {
	Name              string `json:"name"`
	Count             int    `json:"count"`
	VMSize            string `json:"vmSize"`
	OsType            string `json:"osType"`
	Mode              string `json:"mode"`
	ProvisioningState string `json:"provisioningState"`
}

// AKSCredentials holds the kubeconfig credential block
type AKSCredentials struct {
	Kubeconfigs []struct {
		Name  string `json:"name"`
		Value string `json:"value"` // base64-encoded kubeconfig
	} `json:"kubeconfigs"`
}

// ============================================================================
// AKS API Methods
// ============================================================================

// ListAKSClusters lists all AKS clusters in a resource group
func (c *AzureAPIClient) ListAKSClusters(ctx context.Context, resourceGroup string) ([]AKSCluster, error) {
	if c.aksClient == nil {
		return nil, fmt.Errorf("Azure AKS SDK client not initialized")
	}

	var clusters []AKSCluster
	pager := c.aksClient.NewListByResourceGroupPager(resourceGroup, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("AKS ListByResourceGroup failed: %w", err)
		}
		for _, mc := range page.Value {
			clusters = append(clusters, sdkManagedClusterToAKS(mc))
		}
	}
	return clusters, nil
}

// GetAKSCluster retrieves a specific AKS cluster
func (c *AzureAPIClient) GetAKSCluster(ctx context.Context, resourceGroup, clusterName string) (*AKSCluster, error) {
	if c.aksClient == nil {
		return nil, fmt.Errorf("Azure AKS SDK client not initialized")
	}

	resp, err := c.aksClient.Get(ctx, resourceGroup, clusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("AKS Get failed: %w", err)
	}

	cluster := sdkManagedClusterToAKS(&resp.ManagedCluster)
	return &cluster, nil
}

// CreateAKSCluster creates a new AKS cluster
func (c *AzureAPIClient) CreateAKSCluster(ctx context.Context, resourceGroup string, req *CreateClusterRequest, location string) (*AKSCluster, error) {
	if c.aksClient == nil {
		return nil, fmt.Errorf("Azure AKS SDK client not initialized")
	}

	count := int32(req.NodeCount)
	poolName := "nodepool1"
	osType := armcontainerservice.OSTypeLinux
	mode := armcontainerservice.AgentPoolModeSystem

	params := armcontainerservice.ManagedCluster{
		Location: &location,
		Identity: &armcontainerservice.ManagedClusterIdentity{
			Type: ptrIdentityType(armcontainerservice.ResourceIdentityTypeSystemAssigned),
		},
		Properties: &armcontainerservice.ManagedClusterProperties{
			KubernetesVersion: &req.KubernetesVersion,
			DNSPrefix:         strPtr(req.Name + "-dns"),
			AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{
				{
					Name:   &poolName,
					Count:  &count,
					VMSize: &req.NodeType,
					OSType: &osType,
					Mode:   &mode,
				},
			},
		},
	}

	poller, err := c.aksClient.BeginCreateOrUpdate(ctx, resourceGroup, req.Name, params, nil)
	if err != nil {
		return nil, fmt.Errorf("AKS CreateOrUpdate failed: %w", err)
	}

	// Poll until done (blocking) or return early with status
	result, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		// Return partial info even if polling fails
		return &AKSCluster{Name: req.Name}, fmt.Errorf("AKS CreateOrUpdate polling: %w", err)
	}

	cluster := sdkManagedClusterToAKS(&result.ManagedCluster)
	return &cluster, nil
}

// DeleteAKSCluster deletes an AKS cluster
func (c *AzureAPIClient) DeleteAKSCluster(ctx context.Context, resourceGroup, clusterName string) error {
	if c.aksClient == nil {
		return fmt.Errorf("Azure AKS SDK client not initialized")
	}

	poller, err := c.aksClient.BeginDelete(ctx, resourceGroup, clusterName, nil)
	if err != nil {
		return fmt.Errorf("AKS Delete failed: %w", err)
	}
	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("AKS Delete polling: %w", err)
	}
	return nil
}

// GetAKSCredentials retrieves kubeconfig for an AKS cluster
func (c *AzureAPIClient) GetAKSCredentials(ctx context.Context, resourceGroup, clusterName string) (*AKSCredentials, error) {
	if c.aksClient == nil {
		return nil, fmt.Errorf("Azure AKS SDK client not initialized")
	}

	resp, err := c.aksClient.ListClusterAdminCredentials(ctx, resourceGroup, clusterName, nil)
	if err != nil {
		return nil, fmt.Errorf("AKS ListClusterAdminCredentials failed: %w", err)
	}

	creds := &AKSCredentials{}
	for _, kc := range resp.Kubeconfigs {
		name := ""
		if kc.Name != nil {
			name = *kc.Name
		}
		creds.Kubeconfigs = append(creds.Kubeconfigs, struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{
			Name:  name,
			Value: string(kc.Value),
		})
	}
	return creds, nil
}

// ============================================================================
// SDK type -> local type conversion + helpers
// ============================================================================

func sdkManagedClusterToAKS(mc *armcontainerservice.ManagedCluster) AKSCluster {
	cluster := AKSCluster{}

	if mc.ID != nil {
		cluster.ID = *mc.ID
	}
	if mc.Name != nil {
		cluster.Name = *mc.Name
	}
	if mc.Location != nil {
		cluster.Location = *mc.Location
	}
	if mc.Tags != nil {
		cluster.Tags = make(map[string]string)
		for k, v := range mc.Tags {
			if v != nil {
				cluster.Tags[k] = *v
			}
		}
	}
	if mc.Properties != nil {
		if mc.Properties.ProvisioningState != nil {
			cluster.Properties.ProvisioningState = *mc.Properties.ProvisioningState
		}
		if mc.Properties.KubernetesVersion != nil {
			cluster.Properties.KubernetesVersion = *mc.Properties.KubernetesVersion
		}
		if mc.Properties.Fqdn != nil {
			cluster.Properties.Fqdn = *mc.Properties.Fqdn
		}
		if mc.Properties.PowerState != nil && mc.Properties.PowerState.Code != nil {
			cluster.Properties.PowerState.Code = string(*mc.Properties.PowerState.Code)
		}
		for _, ap := range mc.Properties.AgentPoolProfiles {
			pool := AKSNodePool{}
			if ap.Name != nil {
				pool.Name = *ap.Name
			}
			if ap.Count != nil {
				pool.Count = int(*ap.Count)
			}
			if ap.VMSize != nil {
				pool.VMSize = *ap.VMSize
			}
			if ap.OSType != nil {
				pool.OsType = string(*ap.OSType)
			}
			if ap.Mode != nil {
				pool.Mode = string(*ap.Mode)
			}
			if ap.ProvisioningState != nil {
				pool.ProvisioningState = *ap.ProvisioningState
			}
			cluster.Properties.AgentPoolProfiles = append(cluster.Properties.AgentPoolProfiles, pool)
		}
	}
	return cluster
}

func strPtr(s string) *string {
	return &s
}

func ptrIdentityType(t armcontainerservice.ResourceIdentityType) *armcontainerservice.ResourceIdentityType {
	return &t
}
