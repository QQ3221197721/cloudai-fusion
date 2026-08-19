package providers

// TencentProvider is the unified-interface implementation for Tencent Cloud.
//
// TODO: 接入腾讯云 SDK (https://cloud.tencent.com/document/product/436/68624) 时：
//   - Compute : https://github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm -> DescribeInstances / RunInstances / TerminateInstances
//   - Storage : ... cos-go-sdk -> GetBucket / PutObject
//   - Network : VPC client -> DescribeVpcs / CreateSecurityGroup + AuthorizeSecurityGroupRules
//     Auth via credential.Credential with SecretId/SecretToken from cfg.AccessKey/SecretKey.
type TencentProvider struct {
	*genericProvider
}

var _ CloudProvider = (*TencentProvider)(nil)

// NewTencent constructs a Tencent Cloud provider. cfg.Region defaults to ap-guangzhou when empty.
func NewTencent(cfg ProviderConfig) *TencentProvider {
	if cfg.Name == "" {
		cfg.Name = "tencent"
	}
	if cfg.Region == "" {
		cfg.Region = "ap-guangzhou"
	}
	seedInstances := []Instance{
		{ID: "tccm-i-seed-01", Name: "bastion", Type: "S2.SMALL1", Region: cfg.Region, State: "RUNNING", PrivateIP: "172.16.0.10"},
		// GN10(16XGIGABYTE) is Tencent's GPU instance SKU mapping to our SmartRouter expectations.
		{ID: "tccm-i-seed-02", Name: "gpu-worker", Type: "GN10(16XGIGABYTE)", Region: cfg.Region, State: "RUNNING", PublicIP: "118.24.0.6"},
	}
	seedBuckets := []Bucket{
		{Name: "caf-tencent-artifacts", Region: cfg.Region, CreatedAt: "2026-01-10T09:00:00Z"},
	}
	seedVPCs := []VPC{
		{ID: "vpc-tcp-default", CIDR: "172.16.0.0/16", Region: cfg.Region, State: "AVAILABLE"},
	}
	return &TencentProvider{genericProvider: newGenericProvider(cfg, seedInstances, seedBuckets, seedVPCs)}
}
