package cloudprovider

import (
	"context"
)

// Provider is a vendor-neutral abstraction over compute instance lifecycle.
//
// Every backend (localmock, aws, azure, gcp) must implement this interface.
type Provider interface {
	// ListInstances returns all instances in the current scope. For backends
	// that support regions, list across regions or fall back to the default.
	// Results MUST be deterministic: sorted by ID and stable ordering.
	ListInstances(ctx context.Context) ([]Instance, error)

	// CreateInstance creates a new instance with the specified attributes. It
	// returns the new instance's ID on success. State immediately becomes
	// Pending; an asynchronous transition to Running occurs naturally over
	// the simulated or real boot process.
	CreateInstance(ctx context.Context, req CreateInstanceRequest) (string, error)

	// DeleteInstance removes an existing instance given its ID. NotFound is
	// reported via ErrInstanceNotFound when applicable. This call has no side
	// effect on other instances and must not panic or corrupt state.
	DeleteInstance(ctx context.Context, id string) error

	// Capabilities returns a truthful self-report of whether this provider can
	// execute live operations right now. When credentials are absent, it MUST
	// report CredentialsRequired with Online=false and explain clearly in Notes.
	Capabilities() Capabilities

	// GetPricing returns a price quote for the requested instance type and
	// region. It returns ErrUnknownInstanceType when the requested configuration
	// is absent from the catalog (or a live pricing API failure when offline).
	GetPricing(instanceType, region string) (*Pricing, error)
}

// Registry manages the registration and lookup of providers by kind.
//
// Usage:
//   - Register(ProviderLocalMock, NewLocalMockProvider(...))
//   - Get(kind) -> Provider
// The Registry is thread-safe.
type Registry struct {
	m map[ProviderKind]Provider
}

// NewRegistry constructs a fresh empty registry. Callers populate it before
// using Get. For zero-credential operation, at minimum register LocalMock.
func NewRegistry() *Registry {
	return &Registry{m: make(map[ProviderKind]Provider)}
}

// Register binds a provider to a kind key for future lookups. It panics if
// the same kind is registered twice (defensive: caller should ensure uniqueness).
func (r *Registry) Register(kind ProviderKind, p Provider) {
	r.m[kind] = p
}

// Get returns the provider for a given kind, nil otherwise. No panic.
func (r *Registry) Get(kind ProviderKind) Provider {
	return r.m[kind]
}

// Kinds returns the registered provider kinds (unsorted).
func (r *Registry) Kinds() []ProviderKind {
	out := make([]ProviderKind, 0, len(r.m))
	for k := range r.m {
		out = append(out, k)
	}
	return out
}

// The methods below form the "unified call" surface: a caller names a provider
// kind and the registry dispatches to the matching backend. This is the single
// entry point that lets callers treat every cloud (and the local mock) the same
// way. They return ErrProviderNotRegistered when the kind is unknown.

// ListInstances dispatches to the named provider.
func (r *Registry) ListInstances(ctx context.Context, kind ProviderKind) ([]Instance, error) {
	p := r.m[kind]
	if p == nil {
		return nil, ErrProviderNotRegistered
	}
	return p.ListInstances(ctx)
}

// CreateInstance dispatches to the named provider.
func (r *Registry) CreateInstance(ctx context.Context, kind ProviderKind, req CreateInstanceRequest) (string, error) {
	p := r.m[kind]
	if p == nil {
		return "", ErrProviderNotRegistered
	}
	return p.CreateInstance(ctx, req)
}

// DeleteInstance dispatches to the named provider.
func (r *Registry) DeleteInstance(ctx context.Context, kind ProviderKind, id string) error {
	p := r.m[kind]
	if p == nil {
		return ErrProviderNotRegistered
	}
	return p.DeleteInstance(ctx, id)
}

// GetPricing dispatches to the named provider.
func (r *Registry) GetPricing(kind ProviderKind, instanceType, region string) (*Pricing, error) {
	p := r.m[kind]
	if p == nil {
		return nil, ErrProviderNotRegistered
	}
	return p.GetPricing(instanceType, region)
}

// Capabilities dispatches to the named provider, returning a truthful report.
func (r *Registry) Capabilities(kind ProviderKind) (Capabilities, error) {
	p := r.m[kind]
	if p == nil {
		return Capabilities{}, ErrProviderNotRegistered
	}
	return p.Capabilities(), nil
}
