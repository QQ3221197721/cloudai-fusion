package cloud

import "context"

// CloudService defines the contract for multi-cloud provider management.
// The API layer depends on this interface rather than the concrete *Manager,
// enabling mock implementations for handler unit testing.
type CloudService interface {
	// ListProviders returns all registered cloud providers.
	ListProviders() []Provider

	// GetProvider returns a named cloud provider.
	GetProvider(name string) (Provider, error)

	// RegisterProvider registers a cloud provider implementation.
	RegisterProvider(name string, provider Provider)

	// ListAllClusters aggregates clusters from all providers.
	ListAllClusters(ctx context.Context) ([]*ClusterInfo, error)

	// GetTotalCost returns cost summary across all providers.
	GetTotalCost(ctx context.Context, startTime, endTime string) (*CostSummary, error)
}

// Compile-time interface satisfaction check.
var _ CloudService = (*Manager)(nil)
