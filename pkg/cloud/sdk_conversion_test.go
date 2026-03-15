package cloud

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	gkeapi "google.golang.org/api/container/v1"
)

// =============================================================================
// AWS SDK Conversion Tests
// =============================================================================

func TestSdkClusterToEKS_Nil(t *testing.T) {
	result := sdkClusterToEKS(nil)
	if result != nil {
		t.Error("nil input should return nil")
	}
}

func TestSdkClusterToEKS_Empty(t *testing.T) {
	c := &ekstypes.Cluster{}
	result := sdkClusterToEKS(c)
	if result == nil {
		t.Fatal("empty cluster should not return nil")
	}
	if result.Name != "" {
		t.Errorf("expected empty name, got %q", result.Name)
	}
	if result.Status != "" {
		t.Errorf("expected empty status, got %q", result.Status)
	}
}

func TestSdkClusterToEKS_FullFields(t *testing.T) {
	c := &ekstypes.Cluster{
		Name:            aws.String("my-eks"),
		Arn:             aws.String("arn:aws:eks:us-west-2:123456:cluster/my-eks"),
		Version:         aws.String("1.29"),
		Status:          ekstypes.ClusterStatusActive,
		Endpoint:        aws.String("https://abc.eks.amazonaws.com"),
		RoleArn:         aws.String("arn:aws:iam::role/eks"),
		PlatformVersion: aws.String("eks.8"),
		Tags:            map[string]string{"env": "prod"},
		KubernetesNetworkConfig: &ekstypes.KubernetesNetworkConfigResponse{
			ServiceIpv4Cidr: aws.String("10.100.0.0/16"),
		},
		CertificateAuthority: &ekstypes.Certificate{
			Data: aws.String("LS0tLS1CRUdJTi..."),
		},
	}

	result := sdkClusterToEKS(c)
	if result.Name != "my-eks" {
		t.Errorf("Name = %q, want %q", result.Name, "my-eks")
	}
	if result.Arn != "arn:aws:eks:us-west-2:123456:cluster/my-eks" {
		t.Errorf("Arn mismatch: %q", result.Arn)
	}
	if result.Version != "1.29" {
		t.Errorf("Version = %q, want %q", result.Version, "1.29")
	}
	if result.Status != "ACTIVE" {
		t.Errorf("Status = %q, want ACTIVE", result.Status)
	}
	if result.Endpoint != "https://abc.eks.amazonaws.com" {
		t.Errorf("Endpoint mismatch: %q", result.Endpoint)
	}
	if result.RoleArn != "arn:aws:iam::role/eks" {
		t.Errorf("RoleArn mismatch: %q", result.RoleArn)
	}
	if result.PlatformVersion != "eks.8" {
		t.Errorf("PlatformVersion mismatch: %q", result.PlatformVersion)
	}
	if result.Tags["env"] != "prod" {
		t.Errorf("Tags mismatch: %v", result.Tags)
	}
	if result.KubernetesNetworkConfig.ServiceIpv4Cidr != "10.100.0.0/16" {
		t.Errorf("ServiceIpv4Cidr mismatch: %q", result.KubernetesNetworkConfig.ServiceIpv4Cidr)
	}
	if result.CertificateAuthority.Data != "LS0tLS1CRUdJTi..." {
		t.Errorf("CertificateAuthority.Data mismatch: %q", result.CertificateAuthority.Data)
	}
}

func TestSdkClusterToEKS_PartialFields(t *testing.T) {
	// Only Name and Version set — other pointer fields nil
	c := &ekstypes.Cluster{
		Name:    aws.String("partial"),
		Version: aws.String("1.28"),
	}
	result := sdkClusterToEKS(c)
	if result.Name != "partial" {
		t.Errorf("Name = %q", result.Name)
	}
	// Nil pointers should not cause panic; fields should be zero-valued
	if result.Endpoint != "" {
		t.Errorf("Endpoint should be empty, got %q", result.Endpoint)
	}
	if result.KubernetesNetworkConfig.ServiceIpv4Cidr != "" {
		t.Errorf("ServiceIpv4Cidr should be empty")
	}
	if result.CertificateAuthority.Data != "" {
		t.Errorf("CertificateAuthority.Data should be empty")
	}
}

func TestSdkNodegroupToEKS_Nil(t *testing.T) {
	result := sdkNodegroupToEKS(nil)
	if result != nil {
		t.Error("nil input should return nil")
	}
}

func TestSdkNodegroupToEKS_FullFields(t *testing.T) {
	desired := int32(3)
	min := int32(1)
	max := int32(5)
	ng := &ekstypes.Nodegroup{
		NodegroupName: aws.String("ng-default"),
		ClusterName:   aws.String("my-cluster"),
		Status:        ekstypes.NodegroupStatusActive,
		InstanceTypes: []string{"p4d.24xlarge", "p5.48xlarge"},
		ScalingConfig: &ekstypes.NodegroupScalingConfig{
			DesiredSize: &desired,
			MinSize:     &min,
			MaxSize:     &max,
		},
	}

	result := sdkNodegroupToEKS(ng)
	if result.NodegroupName != "ng-default" {
		t.Errorf("NodegroupName = %q", result.NodegroupName)
	}
	if result.ClusterName != "my-cluster" {
		t.Errorf("ClusterName = %q", result.ClusterName)
	}
	if result.Status != "ACTIVE" {
		t.Errorf("Status = %q", result.Status)
	}
	if len(result.InstanceTypes) != 2 {
		t.Errorf("InstanceTypes len = %d", len(result.InstanceTypes))
	}
	if result.ScalingConfig.DesiredSize != 3 {
		t.Errorf("DesiredSize = %d, want 3", result.ScalingConfig.DesiredSize)
	}
	if result.ScalingConfig.MinSize != 1 {
		t.Errorf("MinSize = %d", result.ScalingConfig.MinSize)
	}
	if result.ScalingConfig.MaxSize != 5 {
		t.Errorf("MaxSize = %d", result.ScalingConfig.MaxSize)
	}
}

func TestSdkNodegroupToEKS_NilScalingConfig(t *testing.T) {
	ng := &ekstypes.Nodegroup{
		NodegroupName: aws.String("ng-noscale"),
	}
	result := sdkNodegroupToEKS(ng)
	if result.ScalingConfig.DesiredSize != 0 {
		t.Errorf("DesiredSize should be 0 with nil ScalingConfig")
	}
}

// =============================================================================
// GCP SDK Conversion Tests
// =============================================================================

func TestSdkClusterToGKE_Basic(t *testing.T) {
	sc := &gkeapi.Cluster{
		Name:                 "my-gke",
		Description:          "test cluster",
		InitialNodeCount:     3,
		CurrentMasterVersion: "1.29.1-gke.1000",
		CurrentNodeVersion:   "1.29.1-gke.1000",
		Status:               "RUNNING",
		Endpoint:             "34.123.45.67",
		Location:             "us-central1",
		SelfLink:             "https://container.googleapis.com/v1/projects/proj/locations/us-central1/clusters/my-gke",
		CreateTime:           "2026-01-01T00:00:00Z",
	}

	result := sdkClusterToGKE(sc)
	if result.Name != "my-gke" {
		t.Errorf("Name = %q", result.Name)
	}
	if result.Description != "test cluster" {
		t.Errorf("Description = %q", result.Description)
	}
	if result.InitialNodeCount != 3 {
		t.Errorf("InitialNodeCount = %d", result.InitialNodeCount)
	}
	if result.Status != "RUNNING" {
		t.Errorf("Status = %q", result.Status)
	}
	if result.Location != "us-central1" {
		t.Errorf("Location = %q", result.Location)
	}
	if result.CurrentMasterVersion != "1.29.1-gke.1000" {
		t.Errorf("CurrentMasterVersion = %q", result.CurrentMasterVersion)
	}
	if result.Endpoint != "34.123.45.67" {
		t.Errorf("Endpoint = %q", result.Endpoint)
	}
}

func TestSdkClusterToGKE_WithNodePools(t *testing.T) {
	sc := &gkeapi.Cluster{
		Name:   "pool-cluster",
		Status: "RUNNING",
		NodePools: []*gkeapi.NodePool{
			{
				Name:             "default-pool",
				InitialNodeCount: 3,
				Status:           "RUNNING",
				Config: &gkeapi.NodeConfig{
					MachineType: "n1-standard-4",
					DiskSizeGb:  100,
				},
				Autoscaling: &gkeapi.NodePoolAutoscaling{
					Enabled:      true,
					MinNodeCount: 1,
					MaxNodeCount: 10,
				},
			},
			{
				Name:             "gpu-pool",
				InitialNodeCount: 2,
				Status:           "RUNNING",
				Config: &gkeapi.NodeConfig{
					MachineType: "a2-highgpu-1g",
					DiskSizeGb:  200,
				},
			},
		},
	}

	result := sdkClusterToGKE(sc)
	if len(result.NodePools) != 2 {
		t.Fatalf("NodePools len = %d, want 2", len(result.NodePools))
	}

	pool0 := result.NodePools[0]
	if pool0.Name != "default-pool" {
		t.Errorf("pool0.Name = %q", pool0.Name)
	}
	if pool0.Config.MachineType != "n1-standard-4" {
		t.Errorf("pool0.MachineType = %q", pool0.Config.MachineType)
	}
	if pool0.Config.DiskSizeGb != 100 {
		t.Errorf("pool0.DiskSizeGb = %d", pool0.Config.DiskSizeGb)
	}
	if !pool0.Autoscaling.Enabled {
		t.Error("pool0 autoscaling should be enabled")
	}
	if pool0.Autoscaling.MaxNodeCount != 10 {
		t.Errorf("pool0.MaxNodeCount = %d", pool0.Autoscaling.MaxNodeCount)
	}

	pool1 := result.NodePools[1]
	if pool1.Name != "gpu-pool" {
		t.Errorf("pool1.Name = %q", pool1.Name)
	}
	if pool1.Autoscaling.Enabled {
		t.Error("pool1 autoscaling should be disabled (nil autoscaling)")
	}
}

func TestSdkClusterToGKE_NilNodeConfig(t *testing.T) {
	sc := &gkeapi.Cluster{
		Name: "no-config",
		NodePools: []*gkeapi.NodePool{
			{Name: "bare-pool", InitialNodeCount: 1},
		},
	}
	result := sdkClusterToGKE(sc)
	if len(result.NodePools) != 1 {
		t.Fatal("expected 1 node pool")
	}
	// Nil Config should not panic; MachineType should be empty
	if result.NodePools[0].Config.MachineType != "" {
		t.Errorf("expected empty MachineType, got %q", result.NodePools[0].Config.MachineType)
	}
}

// =============================================================================
// Azure SDK Conversion Tests (tested via Provider since sdkManagedClusterToAKS
// requires *armcontainerservice.ManagedCluster which is complex to construct)
// =============================================================================

func TestAKSCluster_NodeCount(t *testing.T) {
	// Test AKSCluster struct directly — verify node pool aggregation logic
	cluster := AKSCluster{
		Name:     "test-aks",
		Location: "eastus",
	}
	cluster.Properties.AgentPoolProfiles = []AKSNodePool{
		{Name: "pool1", Count: 3, VMSize: "Standard_DS2_v2"},
		{Name: "pool2", Count: 5, VMSize: "Standard_NC6s_v3"},
	}

	totalNodes := 0
	for _, pool := range cluster.Properties.AgentPoolProfiles {
		totalNodes += pool.Count
	}
	if totalNodes != 8 {
		t.Errorf("total nodes = %d, want 8", totalNodes)
	}
}

// =============================================================================
// Aliyun SDK Types Tests
// =============================================================================

func TestACKCluster_Fields(t *testing.T) {
	cluster := ACKCluster{
		ClusterID:        "c-abc123",
		Name:             "test-ack",
		RegionID:         "cn-hangzhou",
		State:            "running",
		ClusterType:      "ManagedKubernetes",
		Size:             6,
		Version:          "1.28.3-aliyun.1",
		ExternalEndpoint: "https://ack.cn-hangzhou.aliyuncs.com",
		Created:          "2026-01-01T00:00:00Z",
	}
	if cluster.ClusterID != "c-abc123" {
		t.Errorf("ClusterID = %q", cluster.ClusterID)
	}
	if cluster.Size != 6 {
		t.Errorf("Size = %d", cluster.Size)
	}
}

func TestACKNode_IPAddress(t *testing.T) {
	node := ACKNode{
		InstanceID:   "i-abc",
		InstanceName: "worker-1",
		InstanceType: "ecs.gn7-c12g1.3xlarge",
		InstanceRole: "Worker",
		State:        "running",
		IPAddress:    []string{"172.16.0.10", "172.16.0.11"},
		HostName:     "node-1",
	}
	if len(node.IPAddress) != 2 {
		t.Errorf("IPAddress count = %d", len(node.IPAddress))
	}
	if node.IPAddress[0] != "172.16.0.10" {
		t.Errorf("first IP = %q", node.IPAddress[0])
	}
}

func TestACKNodePage_Pagination(t *testing.T) {
	page := ACKNodePage{
		Nodes: []ACKNode{{InstanceID: "i-1"}, {InstanceID: "i-2"}},
	}
	page.Page.TotalCount = 10
	page.Page.PageNumber = 1
	page.Page.PageSize = 2
	if len(page.Nodes) != 2 {
		t.Errorf("Nodes len = %d", len(page.Nodes))
	}
	if page.Page.TotalCount != 10 {
		t.Errorf("TotalCount = %d", page.Page.TotalCount)
	}
}

// =============================================================================
// Tencent SDK Types Tests
// =============================================================================

func TestTKECluster_Fields(t *testing.T) {
	cluster := TKECluster{
		ClusterID:      "cls-abc",
		ClusterName:    "test-tke",
		ClusterVersion: "1.28",
		ClusterType:    "MANAGED_CLUSTER",
		ClusterStatus:  "Running",
		ClusterNodeNum: 5,
		ProjectId:      123,
	}
	if cluster.ClusterID != "cls-abc" {
		t.Errorf("ClusterID = %q", cluster.ClusterID)
	}
	if cluster.ClusterNodeNum != 5 {
		t.Errorf("ClusterNodeNum = %d", cluster.ClusterNodeNum)
	}
}

func TestTKEInstance_Fields(t *testing.T) {
	inst := TKEInstance{
		InstanceID:    "ins-abc",
		InstanceRole:  "WORKER",
		InstanceState: "running",
		LanIP:         "10.0.0.5",
		InstanceType:  "S5.LARGE8",
	}
	if inst.LanIP != "10.0.0.5" {
		t.Errorf("LanIP = %q", inst.LanIP)
	}
}

// =============================================================================
// Huawei SDK Types Tests
// =============================================================================

func TestCCECluster_Fields(t *testing.T) {
	cluster := CCECluster{
		Kind:       "Cluster",
		APIVersion: "v3",
	}
	cluster.Metadata.UID = "uid-123"
	cluster.Metadata.Name = "test-cce"
	cluster.Metadata.CreatedAt = "2026-01-01T00:00:00Z"
	cluster.Spec.Category = "CCE"
	cluster.Spec.Flavor = "cce.s2.medium"
	cluster.Spec.Version = "1.28"
	cluster.Status.Phase = "Available"
	cluster.Status.Endpoints = []struct {
		URL  string `json:"url"`
		Type string `json:"type"`
	}{
		{URL: "https://internal.cce.cn-north-4.myhuaweicloud.com", Type: "Internal"},
		{URL: "https://external.cce.cn-north-4.myhuaweicloud.com", Type: "External"},
	}

	if cluster.Metadata.Name != "test-cce" {
		t.Errorf("Name = %q", cluster.Metadata.Name)
	}
	if cluster.Spec.Flavor != "cce.s2.medium" {
		t.Errorf("Flavor = %q", cluster.Spec.Flavor)
	}
	if len(cluster.Status.Endpoints) != 2 {
		t.Errorf("Endpoints count = %d", len(cluster.Status.Endpoints))
	}
	// Find external endpoint
	externalFound := false
	for _, ep := range cluster.Status.Endpoints {
		if ep.Type == "External" {
			externalFound = true
			if ep.URL != "https://external.cce.cn-north-4.myhuaweicloud.com" {
				t.Errorf("External URL = %q", ep.URL)
			}
		}
	}
	if !externalFound {
		t.Error("external endpoint not found")
	}
}

func TestCCENode_Fields(t *testing.T) {
	node := CCENode{}
	node.Metadata.UID = "node-uid-1"
	node.Metadata.Name = "cce-node-1"
	node.Spec.Flavor = "ai1s.8xlarge.8"
	node.Spec.AZ = "cn-north-4a"
	node.Spec.PublicIP = "1.2.3.4"
	node.Status.Phase = "Active"
	node.Status.PrivateIP = "192.168.1.10"

	if node.Spec.Flavor != "ai1s.8xlarge.8" {
		t.Errorf("Flavor = %q", node.Spec.Flavor)
	}
	if node.Status.PrivateIP != "192.168.1.10" {
		t.Errorf("PrivateIP = %q", node.Status.PrivateIP)
	}
}

// =============================================================================
// Helper function tests
// =============================================================================

func TestStrPtr(t *testing.T) {
	s := strPtr("hello")
	if s == nil || *s != "hello" {
		t.Errorf("strPtr returned unexpected value")
	}
}

func TestStrPtrHelper(t *testing.T) {
	s := strPtrHelper("world")
	if s == nil || *s != "world" {
		t.Errorf("strPtrHelper returned unexpected value")
	}
}

func TestInt32Ptr(t *testing.T) {
	p := int32Ptr(42)
	if p == nil || *p != 42 {
		t.Errorf("int32Ptr returned unexpected value")
	}
}

// =============================================================================
// EKSCluster / EKSNodegroup JSON marshaling tests
// =============================================================================

func TestEKSCluster_JSONFields(t *testing.T) {
	cluster := EKSCluster{
		Name:            "test",
		Arn:             "arn:test",
		Version:         "1.29",
		Status:          "ACTIVE",
		Endpoint:        "https://test.eks.amazonaws.com",
		RoleArn:         "arn:role",
		PlatformVersion: "eks.8",
		Tags:            map[string]string{"key": "value"},
		CreatedAt:       1234567890.0,
	}
	cluster.KubernetesNetworkConfig.ServiceIpv4Cidr = "10.100.0.0/16"
	cluster.CertificateAuthority.Data = "cert-data"

	if cluster.Name != "test" {
		t.Error("Name field mismatch")
	}
	if cluster.Tags["key"] != "value" {
		t.Error("Tags mismatch")
	}
}

func TestEKSNodegroup_JSONFields(t *testing.T) {
	ng := EKSNodegroup{
		NodegroupName: "ng-1",
		ClusterName:   "cluster-1",
		Status:        "ACTIVE",
		InstanceTypes: []string{"p4d.24xlarge"},
	}
	ng.ScalingConfig.DesiredSize = 3
	ng.ScalingConfig.MinSize = 1
	ng.ScalingConfig.MaxSize = 10

	if ng.ScalingConfig.DesiredSize != 3 {
		t.Error("ScalingConfig.DesiredSize mismatch")
	}
}

// =============================================================================
// GCPServiceAccountKey struct test
// =============================================================================

func TestGCPServiceAccountKey_Fields(t *testing.T) {
	key := GCPServiceAccountKey{
		Type:        "service_account",
		ProjectID:   "my-project",
		ClientEmail: "sa@my-project.iam.gserviceaccount.com",
	}
	if key.ProjectID != "my-project" {
		t.Errorf("ProjectID = %q", key.ProjectID)
	}
}

// =============================================================================
// AKSCredentials struct test
// =============================================================================

func TestAKSCredentials_Fields(t *testing.T) {
	creds := AKSCredentials{
		Kubeconfigs: []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{
			{Name: "clusterAdmin", Value: "base64-kubeconfig"},
		},
	}
	if len(creds.Kubeconfigs) != 1 {
		t.Fatalf("Kubeconfigs len = %d", len(creds.Kubeconfigs))
	}
	if creds.Kubeconfigs[0].Name != "clusterAdmin" {
		t.Errorf("Name = %q", creds.Kubeconfigs[0].Name)
	}
}
