package cloud

// Multi-Cloud Unified Client (Module 2).
//
// CloudClient is the Docker-like entrypoint developers use: configure once,
// then route Compute/Storage/Network calls to any of the six vendors through a
// single, uniform surface. It composes the vendor implementations that live in
// pkg/cloud/providers.

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud/providers"
)

// Re-export the unified provider types at the package level so callers can use
// them as `cloud.ComputeAPI`, `cloud.ProviderConfig`, etc. without importing
// the providers subpackage directly. (The Module 2 spec's snippets reference
// these unqualified.)
type (
	ComputeAPI        = providers.ComputeAPI
	StorageAPI        = providers.StorageAPI
	NetworkAPI        = providers.NetworkAPI
	CloudProvider     = providers.CloudProvider
	ProviderConfig    = providers.ProviderConfig
	Instance          = providers.Instance
	InstanceReq       = providers.InstanceRequest
	Bucket            = providers.Bucket
	VPC               = providers.VPC
	SecurityRule      = providers.SecurityRule
)

// Re-export auth types for convenience.
type (
	TokenExchangeRequest     = auth.TokenExchangeRequest
	TokenExchangeResponse    = auth.TokenExchangeResponse
	GCPIdentityToken         = auth.GCPIdentityToken
	AzureCredentialRequest   = auth.AzureCredentialRequest
)

// CloudClient holds one provider per configured vendor and routes calls to the
// requested provider, defaulting to defaultProvider when none is specified.
//
// It is safe for concurrent use: the provider map is built once in
// NewCloudClient and never mutated afterwards, so concurrent reads of it (and
// concurrent calls into distinct providers) are race-free. Each provider owns
// its own internally-synchronized state.
type CloudClient struct {
	defaultProvider string
	providers       map[string]providers.CloudProvider
	mu              sync.RWMutex // guards defaultProvider (SetDefault) only
}

// providerFactory maps a canonical vendor key to its constructor.
var providerFactory = map[string]func(providers.ProviderConfig) providers.CloudProvider{
	"aws":     func(c providers.ProviderConfig) providers.CloudProvider { return providers.NewAWS(c) },
	"azure":   func(c providers.ProviderConfig) providers.CloudProvider { return providers.NewAzure(c) },
	"gcp":     func(c providers.ProviderConfig) providers.CloudProvider { return providers.NewGCP(c) },
	"alibaba": func(c providers.ProviderConfig) providers.CloudProvider { return providers.NewAlibaba(c) },
	"huawei":  func(c providers.ProviderConfig) providers.CloudProvider { return providers.NewHuawei(c) },
	"tencent": func(c providers.ProviderConfig) providers.CloudProvider { return providers.NewTencent(c) },
}

// NewCloudClient builds a client from a per-vendor config map. Unknown vendor
// keys are ignored (they simply won't be registered). The first vendor in
// sorted key order becomes the default provider, giving deterministic behavior.
func NewCloudClient(config map[string]ProviderConfig) *CloudClient {
	c := &CloudClient{
		providers: make(map[string]providers.CloudProvider, len(config)),
	}

	// Deterministic ordering so the default is stable across runs.
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		cfg := config[key]
		if cfg.Name == "" {
			cfg.Name = key
		}
		factory, ok := providerFactory[key]
		if !ok {
			continue // unknown vendor key: skip rather than panic
		}
		c.providers[key] = factory(cfg)
		if c.defaultProvider == "" {
			c.defaultProvider = key
		}
	}
	return c
}

// resolve returns the provider for p, or the default provider when p is empty.
func (c *CloudClient) resolve(p string) providers.CloudProvider {
	if p == "" {
		c.mu.RLock()
		p = c.defaultProvider
		c.mu.RUnlock()
	}
	if prov, ok := c.providers[p]; ok {
		return prov
	}
	return nil
}

// Compute returns the ComputeAPI for provider p (or the default when empty).
// Returns nil when the provider is not registered; callers should nil-check or
// use ComputeOrErr for an explicit error.
func (c *CloudClient) Compute(p string) ComputeAPI {
	prov := c.resolve(p)
	if prov == nil {
		return nil
	}
	return prov
}

// Storage returns the StorageAPI for provider p (or the default when empty).
func (c *CloudClient) Storage(p string) StorageAPI {
	prov := c.resolve(p)
	if prov == nil {
		return nil
	}
	return prov
}

// Network returns the NetworkAPI for provider p (or the default when empty).
func (c *CloudClient) Network(p string) NetworkAPI {
	prov := c.resolve(p)
	if prov == nil {
		return nil
	}
	return prov
}

// ComputeOrErr is the explicit-error variant of Compute.
func (c *CloudClient) ComputeOrErr(p string) (ComputeAPI, error) {
	prov := c.resolve(p)
	if prov == nil {
		return nil, fmt.Errorf("cloud: provider %q not registered", p)
	}
	return prov, nil
}

// Provider returns the full unified provider (all three APIs) for p.
func (c *CloudClient) Provider(p string) (CloudProvider, error) {
	prov := c.resolve(p)
	if prov == nil {
		return nil, fmt.Errorf("cloud: provider %q not registered", p)
	}
	return prov, nil
}

// DefaultProvider returns the current default vendor key.
func (c *CloudClient) DefaultProvider() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.defaultProvider
}

// SetDefault changes the default provider. Returns an error if p is unknown.
func (c *CloudClient) SetDefault(p string) error {
	if _, ok := c.providers[p]; !ok {
		return fmt.Errorf("cloud: cannot set default to unregistered provider %q", p)
	}
	c.mu.Lock()
	c.defaultProvider = p
	c.mu.Unlock()
	return nil
}

// Providers returns the sorted list of registered vendor keys.
func (c *CloudClient) Providers() []string {
	keys := make([]string, 0, len(c.providers))
	for k := range c.providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ListAllInstances fans out ListInstances across every registered provider and
// aggregates the results. Errors from individual providers are collected and
// returned together; partial results are still returned.
func (c *CloudClient) ListAllInstances(ctx context.Context) ([]Instance, []error) {
	var (
		all  []Instance
		errs []error
	)
	for _, key := range c.Providers() {
		prov := c.providers[key]
		insts, err := prov.ListInstances(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		all = append(all, insts...)
	}
	return all, errs
}
