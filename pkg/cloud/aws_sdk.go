// Package cloud - AWS EKS (Elastic Kubernetes Service) SDK
// Uses the official AWS SDK for Go v2 (github.com/aws/aws-sdk-go-v2).
// API Reference: https://docs.aws.amazon.com/eks/latest/APIReference/
package cloud

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// ============================================================================
// AWS EKS Client (Official SDK v2)
// ============================================================================

// AWSAPIClient wraps the official AWS SDK v2 EKS client
type AWSAPIClient struct {
	eksClient *eks.Client
	region    string
}

// NewAWSAPIClient creates a new AWS EKS API client using the official AWS SDK v2
func NewAWSAPIClient(accessKeyID, secretAccessKey, region string) *AWSAPIClient {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		),
	)
	if err != nil {
		return &AWSAPIClient{region: region}
	}
	return &AWSAPIClient{
		eksClient: eks.NewFromConfig(cfg),
		region:    region,
	}
}

// ============================================================================
// EKS API Types
// ============================================================================

// EKSCluster represents an EKS cluster from the AWS API
type EKSCluster struct {
	Name               string            `json:"name"`
	Arn                string            `json:"arn"`
	Version            string            `json:"version"`
	Status             string            `json:"status"`
	Endpoint           string            `json:"endpoint"`
	RoleArn            string            `json:"roleArn"`
	PlatformVersion    string            `json:"platformVersion"`
	Tags               map[string]string `json:"tags,omitempty"`
	CreatedAt          float64           `json:"createdAt"`
	KubernetesNetworkConfig struct {
		ServiceIpv4Cidr string `json:"serviceIpv4Cidr"`
	} `json:"kubernetesNetworkConfig"`
	CertificateAuthority struct {
		Data string `json:"data"`
	} `json:"certificateAuthority"`
}

// EKSNodegroup represents an EKS managed node group
type EKSNodegroup struct {
	NodegroupName string   `json:"nodegroupName"`
	ClusterName   string   `json:"clusterName"`
	Status        string   `json:"status"`
	InstanceTypes []string `json:"instanceTypes"`
	ScalingConfig struct {
		DesiredSize int `json:"desiredSize"`
		MinSize     int `json:"minSize"`
		MaxSize     int `json:"maxSize"`
	} `json:"scalingConfig"`
}

// ============================================================================
// EKS API Methods
// ============================================================================

// ListEKSClusters lists all EKS cluster names in the region
func (c *AWSAPIClient) ListEKSClusters(ctx context.Context) ([]string, error) {
	if c.eksClient == nil {
		return nil, fmt.Errorf("AWS EKS SDK client not initialized")
	}
	output, err := c.eksClient.ListClusters(ctx, &eks.ListClustersInput{})
	if err != nil {
		return nil, fmt.Errorf("EKS ListClusters failed: %w", err)
	}
	return output.Clusters, nil
}

// DescribeEKSCluster gets detailed info for a specific cluster
func (c *AWSAPIClient) DescribeEKSCluster(ctx context.Context, clusterName string) (*EKSCluster, error) {
	if c.eksClient == nil {
		return nil, fmt.Errorf("AWS EKS SDK client not initialized")
	}
	output, err := c.eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("EKS DescribeCluster failed: %w", err)
	}
	return sdkClusterToEKS(output.Cluster), nil
}

// CreateEKSCluster creates a new EKS cluster
func (c *AWSAPIClient) CreateEKSCluster(ctx context.Context, req *CreateClusterRequest) (*EKSCluster, error) {
	if c.eksClient == nil {
		return nil, fmt.Errorf("AWS EKS SDK client not initialized")
	}
	output, err := c.eksClient.CreateCluster(ctx, &eks.CreateClusterInput{
		Name:    aws.String(req.Name),
		Version: aws.String(req.KubernetesVersion),
		RoleArn: aws.String("arn:aws:iam::role/eks-cluster-role"),
		ResourcesVpcConfig: &ekstypes.VpcConfigRequest{
			SubnetIds: []string{"subnet-placeholder"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("EKS CreateCluster failed: %w", err)
	}
	return sdkClusterToEKS(output.Cluster), nil
}

// DeleteEKSCluster deletes an EKS cluster
func (c *AWSAPIClient) DeleteEKSCluster(ctx context.Context, clusterName string) error {
	if c.eksClient == nil {
		return fmt.Errorf("AWS EKS SDK client not initialized")
	}
	_, err := c.eksClient.DeleteCluster(ctx, &eks.DeleteClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		return fmt.Errorf("EKS DeleteCluster failed: %w", err)
	}
	return nil
}

// ListEKSNodegroups lists node groups for a cluster
func (c *AWSAPIClient) ListEKSNodegroups(ctx context.Context, clusterName string) ([]string, error) {
	if c.eksClient == nil {
		return nil, fmt.Errorf("AWS EKS SDK client not initialized")
	}
	output, err := c.eksClient.ListNodegroups(ctx, &eks.ListNodegroupsInput{
		ClusterName: aws.String(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("EKS ListNodegroups failed: %w", err)
	}
	return output.Nodegroups, nil
}

// DescribeEKSNodegroup gets detailed nodegroup info
func (c *AWSAPIClient) DescribeEKSNodegroup(ctx context.Context, clusterName, nodegroupName string) (*EKSNodegroup, error) {
	if c.eksClient == nil {
		return nil, fmt.Errorf("AWS EKS SDK client not initialized")
	}
	output, err := c.eksClient.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(nodegroupName),
	})
	if err != nil {
		return nil, fmt.Errorf("EKS DescribeNodegroup failed: %w", err)
	}
	return sdkNodegroupToEKS(output.Nodegroup), nil
}

// UpdateEKSNodegroupScaling updates the scaling config (effectively scales the cluster)
func (c *AWSAPIClient) UpdateEKSNodegroupScaling(ctx context.Context, clusterName, nodegroupName string, desiredSize int) error {
	if c.eksClient == nil {
		return fmt.Errorf("AWS EKS SDK client not initialized")
	}
	ds := int32(desiredSize)
	_, err := c.eksClient.UpdateNodegroupConfig(ctx, &eks.UpdateNodegroupConfigInput{
		ClusterName:   aws.String(clusterName),
		NodegroupName: aws.String(nodegroupName),
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			DesiredSize: &ds,
		},
	})
	if err != nil {
		return fmt.Errorf("EKS UpdateNodegroupConfig failed: %w", err)
	}
	return nil
}

// ============================================================================
// SDK type -> local type conversion
// ============================================================================

func sdkClusterToEKS(c *ekstypes.Cluster) *EKSCluster {
	if c == nil {
		return nil
	}
	result := &EKSCluster{
		Tags: c.Tags,
	}
	if c.Name != nil {
		result.Name = *c.Name
	}
	if c.Arn != nil {
		result.Arn = *c.Arn
	}
	if c.Version != nil {
		result.Version = *c.Version
	}
	result.Status = string(c.Status)
	if c.Endpoint != nil {
		result.Endpoint = *c.Endpoint
	}
	if c.RoleArn != nil {
		result.RoleArn = *c.RoleArn
	}
	if c.PlatformVersion != nil {
		result.PlatformVersion = *c.PlatformVersion
	}
	if c.CreatedAt != nil {
		result.CreatedAt = float64(c.CreatedAt.Unix())
	}
	if c.KubernetesNetworkConfig != nil && c.KubernetesNetworkConfig.ServiceIpv4Cidr != nil {
		result.KubernetesNetworkConfig.ServiceIpv4Cidr = *c.KubernetesNetworkConfig.ServiceIpv4Cidr
	}
	if c.CertificateAuthority != nil && c.CertificateAuthority.Data != nil {
		result.CertificateAuthority.Data = *c.CertificateAuthority.Data
	}
	return result
}

func sdkNodegroupToEKS(ng *ekstypes.Nodegroup) *EKSNodegroup {
	if ng == nil {
		return nil
	}
	result := &EKSNodegroup{
		InstanceTypes: ng.InstanceTypes,
	}
	if ng.NodegroupName != nil {
		result.NodegroupName = *ng.NodegroupName
	}
	if ng.ClusterName != nil {
		result.ClusterName = *ng.ClusterName
	}
	result.Status = string(ng.Status)
	if ng.ScalingConfig != nil {
		if ng.ScalingConfig.DesiredSize != nil {
			result.ScalingConfig.DesiredSize = int(*ng.ScalingConfig.DesiredSize)
		}
		if ng.ScalingConfig.MinSize != nil {
			result.ScalingConfig.MinSize = int(*ng.ScalingConfig.MinSize)
		}
		if ng.ScalingConfig.MaxSize != nil {
			result.ScalingConfig.MaxSize = int(*ng.ScalingConfig.MaxSize)
		}
	}
	return result
}
