package providers

// HuaweiProvider is the unified-interface implementation for Huawei Cloud.
//
// TODO: 接入华为云 SDK v3 (https://sdk.huaweicloud.com/go/sdk.html) 时：
//   - Compute : github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs -> ListInstances / CreateInstance / DeleteInstance
//   - Storage : ... OBS -> ListBuckets / PutObjectInput
//   - Network : VPC client -> ListVpcs / CreateSecurityGroup / CreateRule
//     Auth via hc_config.Auth with AK/SK from cfg.AccessKey/SecretKey.
type HuaweiProvider struct {
	*genericProvider
}

var _ CloudProvider = (*HuaweiProvider)(nil)

// NewHuawei constructs a Huawei Cloud provider. cfg.Region defaults to cn-north-1 when empty.
func NewHuawei(cfg ProviderConfig) *HuaweiProvider {
	if cfg.Name == "" {
		cfg.Name = "huawei"
	}
	if cfg.Region == "" {
		cfg.Region = "cn-north-1"
	}
	seedInstances := []Instance{
		{ID: "hw-i-seed-01", Name: "jump", Type: "s6.medium", Region: cfg.Region, State: "RUNNING", PrivateIP: "192.168.0.10"},
		// s6.6xlarge-gpu is a Huawei GPU instance that we'll map to SmartRouter costs.
		{ID: "hw-i-seed-02", Name: "gpu-worker", Type: "s6.6xlarge-gpu", Region: cfg.Region, State: "RUNNING", PublicIP: "121.36.0.8"},
	}
	seedBuckets := []Bucket{
		{Name: "caf-huawei-artifacts", Region: cfg.Region, CreatedAt: "2026-01-09T06:00:00Z"},
	}
	seedVPCs := []VPC{
		{ID: "vpc-hw-default", CIDR: "192.168.0.0/16", Region: cfg.Region, State: "INSERVING"},
	}
	return &HuaweiProvider{genericProvider: newGenericProvider(cfg, seedInstances, seedBuckets, seedVPCs)}
}
