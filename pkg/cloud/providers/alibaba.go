package providers

// AlibabaProvider is the unified-interface implementation for Alibaba Cloud.
//
// TODO: 接入阿里云 SDK v3 (https://www.alibabacloud.com/help/go/) 时：
//   - Compute : https://github.com/aliyun/alibabacloud-ecs-go-sdk -> DescribeInstances / CreateInstance / DeleteInstance
//   - Storage : ... oss-go-sdk -> GetBucket / PutObject
//   - Network : ... vpc-go-sdk -> DescribeVpcs / CreateSecurityGroup + AuthorizeSecurityGroupRule
//     Auth via config.Config with AccessKeyID+AccessKeySecret from cfg.AccessKey/SecretKey.
type AlibabaProvider struct {
	*genericProvider
}

var _ CloudProvider = (*AlibabaProvider)(nil)

// NewAlibaba constructs an Alibaba Cloud provider. cfg.Region defaults to cn-hangzhou when empty.
func NewAlibaba(cfg ProviderConfig) *AlibabaProvider {
	if cfg.Name == "" {
		cfg.Name = "alibaba"
	}
	if cfg.Region == "" {
		cfg.Region = "cn-hangzhou"
	}
	seedInstances := []Instance{
		{ID: "ali-i-seed-01", Name: "bastion", Type: "ecs.g6.small", Region: cfg.Region, State: "Running", PrivateIP: "172.20.0.10"},
		// g8y.8xlarge is a typical GPU instance family (compatible with SmartRouter cost heuristics).
		{ID: "ali-i-seed-02", Name: "gpu-worker", Type: "g8y.8xlarge", Region: cfg.Region, State: "Running", PublicIP: "47.100.0.5"},
	}
	seedBuckets := []Bucket{
		{Name: "caf-aliyun-artifacts", Region: cfg.Region, CreatedAt: "2026-01-08T08:00:00Z"},
	}
	seedVPCs := []VPC{
		{ID: "vpc-ali-0a1b", CIDR: "172.20.0.0/16", Region: cfg.Region, State: "Available"},
	}
	return &AlibabaProvider{genericProvider: newGenericProvider(cfg, seedInstances, seedBuckets, seedVPCs)}
}
