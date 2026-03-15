package mesh

import "context"

// MeshService defines the contract for service mesh management.
// The API layer depends on this interface rather than the concrete *Manager,
// enabling mock implementations for handler unit testing and pluggable backends.
type MeshService interface {
	// GetStatus returns the current service mesh status.
	GetStatus(ctx context.Context) (*MeshStatus, error)

	// ListPolicies returns all network policies.
	ListPolicies(ctx context.Context) ([]*NetworkPolicy, error)

	// CreatePolicy creates a new network policy and applies the CRD to the cluster.
	CreatePolicy(ctx context.Context, policy *NetworkPolicy) error

	// DeletePolicy removes a network policy from the cluster and DB.
	DeletePolicy(ctx context.Context, namespace, name string) error

	// EnableMTLS enables or disables mutual TLS via Istio PeerAuthentication CRD.
	EnableMTLS(ctx context.Context, strict bool) error

	// GetTrafficMetrics returns eBPF-collected traffic metrics via Hubble Relay.
	GetTrafficMetrics(ctx context.Context, namespace string) ([]TrafficMetrics, error)
}

// Compile-time interface satisfaction check.
var _ MeshService = (*Manager)(nil)
