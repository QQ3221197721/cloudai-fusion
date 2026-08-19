// Package providers implements the Multi-Cloud Unified Interface (Module 2).
//
// It exposes a single, Docker-like abstraction (ComputeAPI / StorageAPI /
// NetworkAPI) over six cloud vendors (AWS, Azure, GCP, Alibaba, Huawei,
// Tencent). Every vendor implementation talks through a mock HTTP client so
// the module stays dependency-light: no real cloud SDK is vendored here.
//
// Each provider file annotates the exact place where a production integration
// would swap the mock transport for the vendor SDK (see the `// TODO: 接入 ...`
// comments). This keeps the seam explicit and the migration path obvious.
package providers

import (
	"context"
	"io"
)

// ============================================================================
// Unified data models
//
// These are intentionally vendor-neutral. Vendor-specific fields (e.g. AWS
// AvailabilityZone, Azure ResourceGroup) are carried opaquely in Metadata so
// the unified surface stays stable while providers keep their nuances.
// ============================================================================

// Instance is a vendor-neutral view of a compute instance / VM.
type Instance struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`      // e.g. "g5.2xlarge", "Standard_ND96..."
	Region    string            `json:"region"`    // vendor region id
	State     string            `json:"state"`     // running | stopped | pending | terminated
	PublicIP  string            `json:"public_ip"`
	PrivateIP string            `json:"private_ip"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// InstanceRequest describes a compute instance to create.
type InstanceRequest struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`     // required: instance/VM size
	Region  string            `json:"region"`   // optional: falls back to provider default
	ImageID string            `json:"image_id"` // AMI / image / machine image id
	GPU     bool              `json:"gpu"`
	Tags    map[string]string `json:"tags,omitempty"`
}

// Bucket is a vendor-neutral view of an object-storage bucket/container.
type Bucket struct {
	Name      string `json:"name"`
	Region    string `json:"region"`
	CreatedAt string `json:"created_at"` // RFC3339
}

// VPC is a vendor-neutral view of a virtual private cloud / network.
type VPC struct {
	ID     string `json:"id"`
	CIDR   string `json:"cidr"`
	Region string `json:"region"`
	State  string `json:"state"`
}

// SecurityRule is one ingress/egress rule inside a security group.
type SecurityRule struct {
	Direction string `json:"direction"` // ingress | egress
	Protocol  string `json:"protocol"`  // tcp | udp | icmp | -1 (all)
	FromPort  int    `json:"from_port"`
	ToPort    int    `json:"to_port"`
	CIDR      string `json:"cidr"`
	Note      string `json:"note,omitempty"`
}

// ============================================================================
// Unified capability interfaces
// ============================================================================

// ComputeAPI abstracts VM/instance lifecycle across clouds.
type ComputeAPI interface {
	ListInstances(ctx context.Context) ([]Instance, error)
	CreateInstance(ctx context.Context, req InstanceRequest) (string, error)
	DeleteInstance(ctx context.Context, id string) error
}

// StorageAPI abstracts object storage across clouds.
type StorageAPI interface {
	ListBuckets(ctx context.Context) ([]Bucket, error)
	UploadObject(ctx context.Context, bucket, obj string, reader io.Reader) error
}

// NetworkAPI abstracts virtual networking across clouds.
type NetworkAPI interface {
	ListVPCs(ctx context.Context) ([]VPC, error)
	CreateSecurityGroup(ctx context.Context, rules []SecurityRule) (string, error)
}

// CloudProvider is the full unified surface every vendor implements. It is the
// "one interface to rule them all" that lets callers treat six clouds like a
// single Docker-style engine.
type CloudProvider interface {
	ComputeAPI
	StorageAPI
	NetworkAPI

	// Name returns the canonical vendor key ("aws", "azure", ...).
	Name() string
	// DefaultRegion returns the region this provider was configured with.
	DefaultRegion() string
}

// ProviderConfig configures a single vendor provider.
type ProviderConfig struct {
	// Name is the canonical vendor key: aws | azure | gcp | alibaba | huawei | tencent.
	Name string
	// Region is the default region used when a request omits one.
	Region string
	// AccessKey / SecretKey are credentials. With the mock transport they are
	// only validated for presence; the real SDK would sign requests with them.
	AccessKey string
	SecretKey string
	// Endpoint optionally overrides the (mock) base URL. Empty uses the
	// vendor's synthetic mock endpoint.
	Endpoint string
	// Extra carries vendor-specific config (subscription_id, project_id, ...).
	Extra map[string]string
}

// Compile-time assertions that every constructed provider satisfies the full
// unified surface. Populated by each vendor file's init-time var check.
var (
	_ ComputeAPI = (*genericProvider)(nil)
	_ StorageAPI = (*genericProvider)(nil)
	_ NetworkAPI = (*genericProvider)(nil)
	_ CloudProvider = (*genericProvider)(nil)
)
