package providers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// allProviders returns one freshly-constructed provider per vendor, using the
// vendor constructors so their seed data / default regions are exercised.
func allProviders() []CloudProvider {
	return []CloudProvider{
		NewAWS(ProviderConfig{}),
		NewAzure(ProviderConfig{}),
		NewGCP(ProviderConfig{}),
		NewAlibaba(ProviderConfig{}),
		NewHuawei(ProviderConfig{}),
		NewTencent(ProviderConfig{}),
	}
}

// wantRegions documents the spec-defined default region per vendor.
var wantRegions = map[string]string{
	"aws":     "us-east-1",
	"azure":   "eastus",
	"gcp":     "us-central1",
	"alibaba": "cn-hangzhou",
	"huawei":  "cn-north-1",
	"tencent": "ap-guangzhou",
}

func TestProviderIdentityAndDefaults(t *testing.T) {
	for _, p := range allProviders() {
		name := p.Name()
		if name == "" {
			t.Fatalf("provider has empty Name()")
		}
		wantRegion, ok := wantRegions[name]
		if !ok {
			t.Fatalf("unexpected vendor key %q", name)
		}
		if got := p.DefaultRegion(); got != wantRegion {
			t.Errorf("%s DefaultRegion() = %q, want %q", name, got, wantRegion)
		}
	}
}

// TestComputeLifecycle exercises the full instance CRUD flow through the mock
// HTTP transport for every vendor.
func TestComputeLifecycle(t *testing.T) {
	ctx := context.Background()
	for _, p := range allProviders() {
		p := p
		t.Run(p.Name(), func(t *testing.T) {
			// Seeded instances must be listable.
			before, err := p.ListInstances(ctx)
			if err != nil {
				t.Fatalf("ListInstances: %v", err)
			}
			if len(before) == 0 {
				t.Fatalf("expected seeded instances, got 0")
			}

			// Create a new instance.
			id, err := p.CreateInstance(ctx, InstanceRequest{Name: "test-vm", Type: "gpu.large", GPU: true})
			if err != nil {
				t.Fatalf("CreateInstance: %v", err)
			}
			if id == "" {
				t.Fatalf("CreateInstance returned empty id")
			}

			after, err := p.ListInstances(ctx)
			if err != nil {
				t.Fatalf("ListInstances after create: %v", err)
			}
			if len(after) != len(before)+1 {
				t.Fatalf("instance count = %d, want %d", len(after), len(before)+1)
			}

			// Delete it again.
			if err := p.DeleteInstance(ctx, id); err != nil {
				t.Fatalf("DeleteInstance: %v", err)
			}
			final, err := p.ListInstances(ctx)
			if err != nil {
				t.Fatalf("ListInstances after delete: %v", err)
			}
			if len(final) != len(before) {
				t.Fatalf("instance count after delete = %d, want %d", len(final), len(before))
			}
		})
	}
}

func TestCreateInstanceValidation(t *testing.T) {
	p := NewAWS(ProviderConfig{})
	if _, err := p.CreateInstance(context.Background(), InstanceRequest{Name: "notype"}); err == nil {
		t.Fatalf("expected error when Type is empty")
	}
}

func TestCreateInstanceDefaultsRegion(t *testing.T) {
	p := NewGCP(ProviderConfig{})
	id, err := p.CreateInstance(context.Background(), InstanceRequest{Type: "a2"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	insts, err := p.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	for _, in := range insts {
		if in.ID == id {
			if in.Region != "us-central1" {
				t.Errorf("region = %q, want us-central1 (provider default)", in.Region)
			}
			return
		}
	}
	t.Fatalf("created instance %q not found", id)
}

func TestDeleteInstanceErrors(t *testing.T) {
	p := NewAzure(ProviderConfig{})
	if err := p.DeleteInstance(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty id")
	}
	if err := p.DeleteInstance(context.Background(), "does-not-exist"); err == nil {
		t.Fatalf("expected error deleting nonexistent instance")
	}
}

func TestStorageFlow(t *testing.T) {
	ctx := context.Background()
	for _, p := range allProviders() {
		p := p
		t.Run(p.Name(), func(t *testing.T) {
			buckets, err := p.ListBuckets(ctx)
			if err != nil {
				t.Fatalf("ListBuckets: %v", err)
			}
			if len(buckets) == 0 {
				t.Fatalf("expected at least one seeded bucket")
			}
			bucket := buckets[0].Name
			if err := p.UploadObject(ctx, bucket, "path/to/obj.bin", strings.NewReader("hello world")); err != nil {
				t.Fatalf("UploadObject: %v", err)
			}
		})
	}
}

func TestUploadObjectErrors(t *testing.T) {
	p := NewHuawei(ProviderConfig{})
	ctx := context.Background()
	if err := p.UploadObject(ctx, "", "obj", strings.NewReader("x")); err == nil {
		t.Fatalf("expected error for empty bucket")
	}
	if err := p.UploadObject(ctx, "bucket", "", strings.NewReader("x")); err == nil {
		t.Fatalf("expected error for empty object name")
	}
	if err := p.UploadObject(ctx, "no-such-bucket", "obj", strings.NewReader("x")); err == nil {
		t.Fatalf("expected error uploading to nonexistent bucket")
	}
}

func TestNetworkFlow(t *testing.T) {
	ctx := context.Background()
	for _, p := range allProviders() {
		p := p
		t.Run(p.Name(), func(t *testing.T) {
			vpcs, err := p.ListVPCs(ctx)
			if err != nil {
				t.Fatalf("ListVPCs: %v", err)
			}
			if len(vpcs) == 0 {
				t.Fatalf("expected at least one seeded VPC")
			}
			rules := []SecurityRule{
				{Direction: "ingress", Protocol: "tcp", FromPort: 443, ToPort: 443, CIDR: "0.0.0.0/0", Note: "https"},
				{Direction: "egress", Protocol: "-1", FromPort: 0, ToPort: 0, CIDR: "0.0.0.0/0"},
			}
			sgID, err := p.CreateSecurityGroup(ctx, rules)
			if err != nil {
				t.Fatalf("CreateSecurityGroup: %v", err)
			}
			if sgID == "" {
				t.Fatalf("empty security group id")
			}
		})
	}
}

func TestCreateSecurityGroupValidation(t *testing.T) {
	p := NewTencent(ProviderConfig{})
	ctx := context.Background()
	if _, err := p.CreateSecurityGroup(ctx, nil); err == nil {
		t.Fatalf("expected error for empty rules")
	}
	bad := []SecurityRule{{Direction: "sideways", Protocol: "tcp"}}
	if _, err := p.CreateSecurityGroup(ctx, bad); err == nil {
		t.Fatalf("expected error for invalid direction")
	}
}

func TestContextCancellation(t *testing.T) {
	p := NewAWS(ProviderConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	if _, err := p.ListInstances(ctx); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestCustomEndpointAndName(t *testing.T) {
	p := NewAlibaba(ProviderConfig{Name: "alibaba", Region: "cn-shanghai", Endpoint: "https://mock.alibaba.example"})
	if p.DefaultRegion() != "cn-shanghai" {
		t.Errorf("region override not applied: %q", p.DefaultRegion())
	}
	if _, err := p.ListInstances(context.Background()); err != nil {
		t.Fatalf("ListInstances with custom endpoint: %v", err)
	}
}

// TestConcurrentAcrossProviders spins many goroutines against DISTINCT providers
// simultaneously; run with -race this proves cross-provider isolation.
func TestConcurrentAcrossProviders(t *testing.T) {
	ctx := context.Background()
	provs := allProviders()
	var wg sync.WaitGroup
	for _, p := range provs {
		for g := 0; g < 20; g++ {
			wg.Add(1)
			go func(p CloudProvider, g int) {
				defer wg.Done()
				if _, err := p.ListInstances(ctx); err != nil {
					t.Errorf("%s ListInstances: %v", p.Name(), err)
				}
				id, err := p.CreateInstance(ctx, InstanceRequest{Name: fmt.Sprintf("c-%d", g), Type: "t.small"})
				if err != nil {
					t.Errorf("%s CreateInstance: %v", p.Name(), err)
					return
				}
				if err := p.DeleteInstance(ctx, id); err != nil {
					t.Errorf("%s DeleteInstance: %v", p.Name(), err)
				}
			}(p, g)
		}
	}
	wg.Wait()
}

// TestConcurrentSameProvider hammers a single provider from many goroutines to
// prove the mock transport's internal locking keeps shared state consistent.
func TestConcurrentSameProvider(t *testing.T) {
	ctx := context.Background()
	p := NewAWS(ProviderConfig{})
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			id, err := p.CreateInstance(ctx, InstanceRequest{Name: fmt.Sprintf("x-%d", g), Type: "t.small"})
			if err != nil {
				t.Errorf("CreateInstance: %v", err)
				return
			}
			if _, err := p.ListInstances(ctx); err != nil {
				t.Errorf("ListInstances: %v", err)
			}
			if err := p.DeleteInstance(ctx, id); err != nil {
				t.Errorf("DeleteInstance: %v", err)
			}
		}(g)
	}
	wg.Wait()
}
