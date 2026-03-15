package cluster

import (
	"context"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ClusterService defines the contract for Kubernetes cluster lifecycle management.
// The API layer depends on this interface rather than the concrete *Manager,
// enabling mock implementations for handler unit testing and pluggable backends.
type ClusterService interface {
	// ListClusters returns all managed clusters.
	ListClusters(ctx context.Context) ([]*Cluster, error)

	// GetCluster returns a single cluster by ID.
	GetCluster(ctx context.Context, clusterID string) (*Cluster, error)

	// ImportCluster registers an existing cluster into the platform.
	ImportCluster(ctx context.Context, req *ImportClusterRequest) (*Cluster, error)

	// DeleteCluster removes a cluster from management.
	DeleteCluster(ctx context.Context, clusterID string) error

	// GetClusterHealth returns health status for a cluster.
	GetClusterHealth(ctx context.Context, clusterID string) (*ClusterHealth, error)

	// GetClusterNodes returns all nodes in a cluster.
	GetClusterNodes(ctx context.Context, clusterID string) ([]*Node, error)

	// GetGPUTopology returns GPU topology information for a cluster.
	GetGPUTopology(ctx context.Context, clusterID string) ([]*common.GPUTopologyInfo, error)

	// GetResourceSummary returns aggregated resource capacity across all clusters.
	GetResourceSummary(ctx context.Context) (*common.ResourceCapacity, error)

	// StartHealthCheckLoop begins periodic health checking.
	StartHealthCheckLoop(ctx context.Context, interval time.Duration)
}

// Compile-time interface satisfaction check.
var _ ClusterService = (*Manager)(nil)
