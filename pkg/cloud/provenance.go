package cloud

import (
	"context"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// provenance.go makes the multi-cloud layer HONEST and evidence-backed: each
// provider reports whether it runs against a real cloud SDK (credentials
// configured) or in stub mode, and every price lookup carries a provenance tag
// (real billing API vs static table) plus an optional signed receipt. This is
// what lets the FinOps moat state "these savings are computed from prices sourced
// from X" instead of silently trusting hardcoded numbers.

// CredentialAware is optionally implemented by providers that can report whether
// real credentials are configured (real SDK path) versus stub mode.
type CredentialAware interface {
	CredentialsConfigured() bool
}

// ProviderIsReal reports whether p has real credentials wired (real SDK path).
// Providers that do not implement CredentialAware are treated as stub.
func ProviderIsReal(p Provider) bool {
	if ca, ok := p.(CredentialAware); ok {
		return ca.CredentialsConfigured()
	}
	return false
}

// reportProviderCapability honestly registers whether a provider talks to a real
// cloud SDK or runs in stub mode (no credentials).
func reportProviderCapability(p Provider) {
	comp := "cloud.provider." + p.Name()
	if ProviderIsReal(p) {
		_ = capability.Report(comp, string(p.Type()), capability.ModeReal,
			"real cloud SDK (credentials configured)")
		return
	}
	_ = capability.Report(comp, string(p.Type()), capability.ModeSimulated,
		"stub mode: no credentials configured")
}

// PriceProvenance describes where a price value came from.
type PriceProvenance struct {
	Source string `json:"source"` // "cloud-billing-api" | "static-table"
	Real   bool   `json:"real"`
}

// CredentialsConfigured reports whether each provider has a live SDK client
// (created only when access credentials were supplied).
func (p *AliyunProvider) CredentialsConfigured() bool  { return p.client != nil }
func (p *AWSProvider) CredentialsConfigured() bool     { return p.client != nil }
func (p *AzureProvider) CredentialsConfigured() bool   { return p.client != nil }
func (p *GCPProvider) CredentialsConfigured() bool     { return p.client != nil }
func (p *HuaweiProvider) CredentialsConfigured() bool  { return p.client != nil }
func (p *TencentProvider) CredentialsConfigured() bool { return p.client != nil }

// GetGPUPricingWithProvenance returns a provider's GPU price together with an
// honest provenance tag and (when a recorder is attached) a signed cloud.pricing
// receipt. GPU prices are presently sourced from a static table; the provenance
// says so plainly. When a real billing adapter is configured for a real provider,
// this flips to a live source without changing callers.
func (m *Manager) GetGPUPricingWithProvenance(ctx context.Context, providerName, gpuType string) (*GPUPricing, *PriceProvenance, error) {
	p, err := m.GetProvider(providerName)
	if err != nil {
		return nil, nil, err
	}
	price, err := p.GetGPUPricing(ctx, gpuType)
	if err != nil {
		return nil, nil, err
	}

	// Honest: the current price path is a static table, not a live billing API.
	prov := &PriceProvenance{Source: "static-table", Real: false}
	_ = capability.Report("cloud.pricing", "static-table", capability.ModeSimulated,
		"GPU prices from a static table, not a live billing API")

	if m.recorder != nil {
		_, _ = m.recorder.Record(ctx, evidence.RecordInput{
			Actor:   "cloud",
			Action:  "cloud.pricing",
			Subject: providerName + "/" + gpuType,
			Input:   map[string]any{"provider": providerName, "gpu_type": gpuType},
			Output:  map[string]any{"on_demand": price.OnDemandPrice, "spot": price.SpotPrice, "currency": price.Currency},
			Payload: map[string]any{
				"pricing":       price,
				"provenance":    prov,
				"provider_real": ProviderIsReal(p),
			},
			Backends: []evidence.BackendFact{{Component: "cloud.pricing", Mode: "simulated", Driver: "static-table"}},
		})
	}
	return price, prov, nil
}
