package edge

import "context"

// EdgeService defines the contract for edge-cloud collaborative architecture.
// The API layer depends on this interface rather than the concrete *Manager,
// enabling mock implementations for handler unit testing and pluggable backends.
type EdgeService interface {
	// GetTopology returns the edge-cloud topology graph.
	GetTopology(ctx context.Context) (map[string]interface{}, error)

	// GetTopologySummary returns a summary of edge topology.
	GetTopologySummary(ctx context.Context) map[string]interface{}

	// ListNodes returns edge nodes filtered by tier.
	ListNodes(ctx context.Context, tier NodeTier) ([]*EdgeNode, error)

	// RegisterNode registers a new edge node.
	RegisterNode(ctx context.Context, node *EdgeNode) error

	// DeployModel deploys a model to edge nodes.
	DeployModel(ctx context.Context, req *EdgeDeployRequest) (*DeployedModel, error)

	// GetSyncPolicies returns edge-cloud synchronization policies.
	GetSyncPolicies(ctx context.Context) ([]*SyncPolicy, error)

	// Heartbeat processes a heartbeat from an edge node.
	Heartbeat(ctx context.Context, nodeID string, usage *EdgeResourceUsage) error

	// StartSyncLoop starts the background edge-cloud sync loop.
	StartSyncLoop(ctx context.Context)

	// Enhanced edge-cloud collaboration methods
	
	// GetOfflineStatus returns offline operation queue status for a node.
	GetOfflineStatus(ctx context.Context, nodeID string) (*SyncStatus, error)

	// DrainOfflineQueue processes all pending operations for a node.
	DrainOfflineQueue(ctx context.Context, nodeID string) (int, error)

	// OptimizeModelForEdge assesses and optimizes a model for edge deployment.
	OptimizeModelForEdge(ctx context.Context, req *EdgeDeployRequest) (*QuantizationType, *PowerBudgetResult, error)

	// RemoveNode removes an edge node and cleans up resources.
	RemoveNode(ctx context.Context, nodeID string) error

	// GetNodeHealth returns detailed health status of an edge node.
	GetNodeHealth(ctx context.Context, nodeID string) (map[string]interface{}, error)

	// Enhanced edge-cloud collaboration methods (Phase 2)

	// HandleEdgeCollaboration handles edge-cloud collaboration requests.
	HandleEdgeCollaboration(ctx context.Context, action string, params map[string]interface{}) (interface{}, error)

	// HandleOfflineCapabilities handles offline capability requests.
	HandleOfflineCapabilities(ctx context.Context, action string, params map[string]interface{}) (interface{}, error)

	// EdgeCollabHub returns the edge collaboration hub for direct access.
	EdgeCollabHub() *EdgeCollabHub

	// OfflineHub returns the offline capabilities hub for direct access.
	OfflineHub() *OfflineHub
}

// Compile-time interface satisfaction check.
var _ EdgeService = (*Manager)(nil)
