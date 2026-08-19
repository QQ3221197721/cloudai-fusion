package providers

// AzureProvider is the unified-interface implementation for Microsoft Azure.
//
// TODO: 接入 Azure SDK for Go (https://learn.microsoft.com/azure/developer/go/) 时：
//   - Compute : github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute -> VirtualMachinesClient
//   - Storage : github.com/Azure/azure-sdk-for-go/sdk/storage/azblob -> ContainerClient / BlockBlobClient
//   - Network : .../armnetwork -> VirtualNetworksClient / SecurityGroupsClient
//     Auth via azidentity.NewClientSecretCredential using cfg.Extra["tenant_id"] + AccessKey/SecretKey.
type AzureProvider struct {
	*genericProvider
}

var _ CloudProvider = (*AzureProvider)(nil)

// NewAzure constructs an Azure provider. cfg.Region defaults to eastus when empty.
func NewAzure(cfg ProviderConfig) *AzureProvider {
	if cfg.Name == "" {
		cfg.Name = "azure"
	}
	if cfg.Region == "" {
		cfg.Region = "eastus"
	}
	seedInstances := []Instance{
		{ID: "azure-i-seed-01", Name: "jump", Type: "Standard_B1s", Region: cfg.Region, State: "running", PrivateIP: "10.1.0.10"},
		// Standard_ND96amsr_A100_v4 == "NDv4" family, the GPU reference SKU for SmartRouter.
		{ID: "azure-i-seed-02", Name: "gpu-worker", Type: "Standard_ND96amsr_A100_v4", Region: cfg.Region, State: "running", PublicIP: "20.42.0.7"},
	}
	seedBuckets := []Bucket{
		{Name: "cafazureartifacts", Region: cfg.Region, CreatedAt: "2026-01-06T10:00:00Z"},
	}
	seedVPCs := []VPC{
		{ID: "vnet-azure-1", CIDR: "10.1.0.0/16", Region: cfg.Region, State: "Succeeded"},
	}
	return &AzureProvider{genericProvider: newGenericProvider(cfg, seedInstances, seedBuckets, seedVPCs)}
}
