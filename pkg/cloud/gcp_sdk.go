// Package cloud - GCP GKE (Google Kubernetes Engine) SDK
// Uses the official Google Cloud Client Library (google.golang.org/api/container/v1).
// API Reference: https://cloud.google.com/kubernetes-engine/docs/reference/rest
package cloud

import (
	"context"
	"encoding/json"
	"fmt"

	gkeapi "google.golang.org/api/container/v1"
	"google.golang.org/api/option"
)

// ============================================================================
// GCP GKE Client (Official Google Cloud SDK)
// ============================================================================

// GCPAPIClient wraps the official Google Cloud Container (GKE) service client
type GCPAPIClient struct {
	svc       *gkeapi.Service
	projectID string
	location  string // e.g., "us-central1" or "-" for all
}

// GCPServiceAccountKey represents the JSON key file structure
type GCPServiceAccountKey struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	ClientID    string `json:"client_id"`
	AuthURI     string `json:"auth_uri"`
	TokenURI    string `json:"token_uri"`
}

// NewGCPAPIClient creates a new GCP GKE API client from service account JSON
func NewGCPAPIClient(credJSON, projectID, location string) (*GCPAPIClient, error) {
	ctx := context.Background()

	svc, err := gkeapi.NewService(ctx, option.WithCredentialsJSON([]byte(credJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to create GKE service client: %w", err)
	}

	// Extract project ID from credentials JSON if not provided
	if projectID == "" {
		var sa GCPServiceAccountKey
		if err := json.Unmarshal([]byte(credJSON), &sa); err == nil {
			projectID = sa.ProjectID
		}
	}

	return &GCPAPIClient{
		svc:       svc,
		projectID: projectID,
		location:  location,
	}, nil
}

// ============================================================================
// GKE API Types
// ============================================================================

// GKECluster represents a GKE cluster
type GKECluster struct {
	Name                 string        `json:"name"`
	Description          string        `json:"description"`
	InitialNodeCount     int           `json:"initialNodeCount"`
	CurrentMasterVersion string        `json:"currentMasterVersion"`
	CurrentNodeVersion   string        `json:"currentNodeVersion"`
	Status               string        `json:"status"`
	Endpoint             string        `json:"endpoint"`
	Location             string        `json:"location"`
	SelfLink             string        `json:"selfLink"`
	CreateTime           string        `json:"createTime"`
	NodePools            []GKENodePool `json:"nodePools"`
}

// GKENodePool represents a GKE node pool
type GKENodePool struct {
	Name             string `json:"name"`
	InitialNodeCount int    `json:"initialNodeCount"`
	Status           string `json:"status"`
	Config           struct {
		MachineType string `json:"machineType"`
		DiskSizeGb  int    `json:"diskSizeGb"`
	} `json:"config"`
	Autoscaling struct {
		Enabled      bool `json:"enabled"`
		MinNodeCount int  `json:"minNodeCount"`
		MaxNodeCount int  `json:"maxNodeCount"`
	} `json:"autoscaling"`
}

// ============================================================================
// GKE API Methods
// ============================================================================

func (c *GCPAPIClient) parentPath() string {
	return fmt.Sprintf("projects/%s/locations/%s", c.projectID, c.location)
}

// ListGKEClusters lists all GKE clusters
func (c *GCPAPIClient) ListGKEClusters(ctx context.Context) ([]GKECluster, error) {
	if c.svc == nil {
		return nil, fmt.Errorf("GCP GKE SDK client not initialized")
	}

	parent := c.parentPath()
	resp, err := c.svc.Projects.Locations.Clusters.List(parent).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("GKE ListClusters failed: %w", err)
	}

	clusters := make([]GKECluster, 0, len(resp.Clusters))
	for _, sc := range resp.Clusters {
		clusters = append(clusters, sdkClusterToGKE(sc))
	}
	return clusters, nil
}

// GetGKECluster retrieves a specific GKE cluster
func (c *GCPAPIClient) GetGKECluster(ctx context.Context, clusterName string) (*GKECluster, error) {
	if c.svc == nil {
		return nil, fmt.Errorf("GCP GKE SDK client not initialized")
	}

	name := fmt.Sprintf("%s/clusters/%s", c.parentPath(), clusterName)
	sc, err := c.svc.Projects.Locations.Clusters.Get(name).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("GKE GetCluster failed: %w", err)
	}

	cluster := sdkClusterToGKE(sc)
	return &cluster, nil
}

// CreateGKECluster creates a new GKE cluster
func (c *GCPAPIClient) CreateGKECluster(ctx context.Context, req *CreateClusterRequest) (*GKECluster, error) {
	if c.svc == nil {
		return nil, fmt.Errorf("GCP GKE SDK client not initialized")
	}

	parent := c.parentPath()
	createReq := &gkeapi.CreateClusterRequest{
		Cluster: &gkeapi.Cluster{
			Name:             req.Name,
			InitialNodeCount: int64(req.NodeCount),
			NodeConfig: &gkeapi.NodeConfig{
				MachineType: req.NodeType,
			},
		},
	}

	_, err := c.svc.Projects.Locations.Clusters.Create(parent, createReq).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("GKE CreateCluster failed: %w", err)
	}

	// Return a placeholder; real cluster info available after provisioning
	return &GKECluster{
		Name:   req.Name,
		Status: "PROVISIONING",
	}, nil
}

// DeleteGKECluster deletes a GKE cluster
func (c *GCPAPIClient) DeleteGKECluster(ctx context.Context, clusterName string) error {
	if c.svc == nil {
		return fmt.Errorf("GCP GKE SDK client not initialized")
	}

	name := fmt.Sprintf("%s/clusters/%s", c.parentPath(), clusterName)
	_, err := c.svc.Projects.Locations.Clusters.Delete(name).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("GKE DeleteCluster failed: %w", err)
	}
	return nil
}

// ============================================================================
// SDK type -> local type conversion
// ============================================================================

func sdkClusterToGKE(sc *gkeapi.Cluster) GKECluster {
	cluster := GKECluster{
		Name:                 sc.Name,
		Description:          sc.Description,
		InitialNodeCount:     int(sc.InitialNodeCount),
		CurrentMasterVersion: sc.CurrentMasterVersion,
		CurrentNodeVersion:   sc.CurrentNodeVersion,
		Status:               sc.Status,
		Endpoint:             sc.Endpoint,
		Location:             sc.Location,
		SelfLink:             sc.SelfLink,
		CreateTime:           sc.CreateTime,
	}

	for _, sp := range sc.NodePools {
		np := GKENodePool{
			Name:             sp.Name,
			InitialNodeCount: int(sp.InitialNodeCount),
			Status:           sp.Status,
		}
		if sp.Config != nil {
			np.Config.MachineType = sp.Config.MachineType
			np.Config.DiskSizeGb = int(sp.Config.DiskSizeGb)
		}
		if sp.Autoscaling != nil {
			np.Autoscaling.Enabled = sp.Autoscaling.Enabled
			np.Autoscaling.MinNodeCount = int(sp.Autoscaling.MinNodeCount)
			np.Autoscaling.MaxNodeCount = int(sp.Autoscaling.MaxNodeCount)
		}
		cluster.NodePools = append(cluster.NodePools, np)
	}

	return cluster
}
