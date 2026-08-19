package providers

// AWSProvider is the unified-interface implementation for Amazon Web Services.
// It satisfies ComputeAPI + StorageAPI + NetworkAPI (via the embedded
// genericProvider) and, in this module, is backed by a mock HTTP transport.
//
// TODO: 接入 AWS SDK v2 (https://docs.aws.amazon.com/sdk-for-go/) 时：
//   - Compute : github.com/aws/aws-sdk-go-v2/service/ec2  -> RunInstances / DescribeInstances / TerminateInstances
//   - Storage : github.com/aws/aws-sdk-go-v2/service/s3   -> ListBuckets / PutObject
//   - Network : github.com/aws/aws-sdk-go-v2/service/ec2  -> DescribeVpcs / CreateSecurityGroup + AuthorizeSecurityGroupIngress
//     Replace newGenericProvider's mock transport with a signed *http.Client
//     produced by config.LoadDefaultConfig + the ec2/s3 clients above.
type AWSProvider struct {
	*genericProvider
}

var _ CloudProvider = (*AWSProvider)(nil)

// NewAWS constructs an AWS provider. cfg.Region defaults to us-east-1 when empty.
func NewAWS(cfg ProviderConfig) *AWSProvider {
	if cfg.Name == "" {
		cfg.Name = "aws"
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	seedInstances := []Instance{
		{ID: "aws-i-seed-01", Name: "bastion", Type: "t3.micro", Region: cfg.Region, State: "running", PrivateIP: "10.0.0.10"},
		// g5.2xlarge is the GPU reference SKU used by SmartRouter (see smart_router.go).
		{ID: "aws-i-seed-02", Name: "gpu-worker", Type: "g5.2xlarge", Region: cfg.Region, State: "running", PublicIP: "54.12.0.5"},
	}
	seedBuckets := []Bucket{
		{Name: "caf-aws-artifacts", Region: cfg.Region, CreatedAt: "2026-01-05T09:00:00Z"},
	}
	seedVPCs := []VPC{
		{ID: "vpc-aws-0a1b", CIDR: "10.0.0.0/16", Region: cfg.Region, State: "available"},
	}
	return &AWSProvider{genericProvider: newGenericProvider(cfg, seedInstances, seedBuckets, seedVPCs)}
}
