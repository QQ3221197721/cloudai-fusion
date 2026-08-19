package cloudprovider

import (
	"time"
)

// ProviderKind is the canonical key identifying a backend implementation.
type ProviderKind string

const (
	// ProviderLocalMock is the zero-credential, in-memory backend.
	ProviderLocalMock ProviderKind = "localmock"
	// ProviderAWS is the Amazon Web Services adapter (EC2-style compute).
	ProviderAWS ProviderKind = "aws"
	// ProviderAzure is the Microsoft Azure adapter (Virtual Machines).
	ProviderAzure ProviderKind = "azure"
	// ProviderGCP is the Google Cloud Platform adapter (Compute Engine).
	ProviderGCP ProviderKind = "gcp"
)

// InstanceState is the lifecycle state of a compute instance.
type InstanceState string

const (
	// StatePending is assigned immediately after a create request.
	StatePending InstanceState = "pending"
	// StateRunning indicates the instance is booted and serving.
	StateRunning InstanceState = "running"
	// StateStopped indicates a halted-but-not-deleted instance.
	StateStopped InstanceState = "stopped"
	// StateTerminated indicates the instance has been deleted.
	StateTerminated InstanceState = "terminated"
)

// CredentialStatus reports whether a provider has usable credentials.
type CredentialStatus string

const (
	// CredentialsSatisfied means the provider was configured with the
	// credential fields it needs to reach its live backend.
	CredentialsSatisfied CredentialStatus = "credentials-satisfied"
	// CredentialsRequired means the provider is running in offline mode and
	// cannot perform live operations until credentials are supplied.
	CredentialsRequired CredentialStatus = "credentials-required"
)

// Instance is a vendor-neutral view of a compute instance / virtual machine.
//
// Vendor-specific attributes that have no neutral analogue are carried opaquely
// in Tags so the unified surface stays stable across backends.
type Instance struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`   // e.g. "t3.medium", "Standard_D2s_v5"
	Region    string            `json:"region"` // backend region id
	State     InstanceState     `json:"state"`
	PublicIP  string            `json:"public_ip,omitempty"`
	PrivateIP string            `json:"private_ip,omitempty"`
	Provider  ProviderKind      `json:"provider"`
	CreatedAt time.Time         `json:"created_at"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// CreateInstanceRequest describes a compute instance to create. Type is the
// only required field; Region falls back to the provider's default when empty.
type CreateInstanceRequest struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Region  string            `json:"region,omitempty"`
	ImageID string            `json:"image_id,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
}

// Pricing is a vendor-neutral hourly/monthly price quote for an instance type.
type Pricing struct {
	Provider     ProviderKind `json:"provider"`
	InstanceType string       `json:"instance_type"`
	Region       string       `json:"region"`
	Currency     string       `json:"currency"` // ISO 4217, e.g. "USD"
	HourlyUSD    float64      `json:"hourly_usd"`
	MonthlyUSD   float64      `json:"monthly_usd"` // derived: HourlyUSD * 730
	// Source describes provenance: "catalog" for the local deterministic
	// price book, "live-api" for a real cloud pricing API response.
	Source string `json:"source"`
}

// Capabilities is a truthful self-report of what a provider can do in its
// current configuration. Callers use it to decide, at runtime, whether a
// backend is live or degraded to credentials-required mode.
type Capabilities struct {
	Provider         ProviderKind     `json:"provider"`
	CredentialStatus CredentialStatus `json:"credential_status"`
	// Online is true only when the provider can actually service live
	// operations right now (credentials present AND a live backend linked).
	Online bool `json:"online"`
	// SupportedRegions lists the regions the adapter knows about.
	SupportedRegions []string `json:"supported_regions"`
	// SupportsPricing indicates whether GetPricing can return a real answer
	// in the current mode.
	SupportsPricing bool `json:"supports_pricing"`
	// Notes is a human-readable explanation of the current mode. For a
	// degraded adapter it states plainly that credentials are required.
	Notes string `json:"notes"`
}
