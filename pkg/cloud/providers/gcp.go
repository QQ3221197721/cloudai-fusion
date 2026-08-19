package providers

// GCPProvider is the unified-interface implementation for Google Cloud Platform.
//
// TODO: 接入 Google Cloud SDK (https://pkg.go.dev/cloud.google.com/go) 时：
//   - Compute : cloud.google.com/go/compute/apiv1 -> InstancesClient (Insert / List / Delete)
//   - Storage : cloud.google.com/go/storage       -> Client.Buckets / ObjectHandle.NewWriter
//   - Network : cloud.google.com/go/compute/apiv1 -> NetworksClient / FirewallsClient
//     Auth via option.WithCredentialsJSON(cfg.Extra["service_account_json"]).
type GCPProvider struct {
	*genericProvider
}

var _ CloudProvider = (*GCPProvider)(nil)

// NewGCP constructs a GCP provider. cfg.Region defaults to us-central1 when empty.
func NewGCP(cfg ProviderConfig) *GCPProvider {
	if cfg.Name == "" {
		cfg.Name = "gcp"
	}
	if cfg.Region == "" {
		cfg.Region = "us-central1"
	}
	seedInstances := []Instance{
		{ID: "gcp-i-seed-01", Name: "jump", Type: "e2-small", Region: cfg.Region, State: "RUNNING", PrivateIP: "10.2.0.10"},
		// a2-highgpu-1g == "A2" family, the GPU reference SKU for SmartRouter.
		{ID: "gcp-i-seed-02", Name: "gpu-worker", Type: "a2-highgpu-1g", Region: cfg.Region, State: "RUNNING", PublicIP: "34.68.0.9"},
	}
	seedBuckets := []Bucket{
		{Name: "caf-gcp-artifacts", Region: cfg.Region, CreatedAt: "2026-01-07T11:00:00Z"},
	}
	seedVPCs := []VPC{
		{ID: "vpc-gcp-default", CIDR: "10.2.0.0/16", Region: cfg.Region, State: "READY"},
	}
	return &GCPProvider{genericProvider: newGenericProvider(cfg, seedInstances, seedBuckets, seedVPCs)}
}
