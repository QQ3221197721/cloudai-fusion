package cloud

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// fullConfig configures all six vendors for the unified client.
func fullConfig() map[string]ProviderConfig {
	return map[string]ProviderConfig{
		"aws":     {Region: "us-east-1"},
		"azure":   {Region: "eastus"},
		"gcp":     {Region: "us-central1"},
		"alibaba": {Region: "cn-hangzhou"},
		"huawei":  {Region: "cn-north-1"},
		"tencent": {Region: "ap-guangzhou"},
	}
}

func TestNewCloudClientRegistersAllVendors(t *testing.T) {
	c := NewCloudClient(fullConfig())
	got := c.Providers()
	want := []string{"alibaba", "aws", "azure", "gcp", "huawei", "tencent"}
	if len(got) != len(want) {
		t.Fatalf("Providers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Providers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Default is first in sorted order.
	if c.DefaultProvider() != "alibaba" {
		t.Errorf("DefaultProvider() = %q, want alibaba", c.DefaultProvider())
	}
}

func TestNewCloudClientSkipsUnknownVendor(t *testing.T) {
	c := NewCloudClient(map[string]ProviderConfig{
		"aws":       {Region: "us-east-1"},
		"oracle":    {Region: "ap"}, // unknown: must be skipped
		"digitalis": {},             // unknown: must be skipped
	})
	provs := c.Providers()
	if len(provs) != 1 || provs[0] != "aws" {
		t.Fatalf("expected only aws registered, got %v", provs)
	}
}

func TestComputeRouting(t *testing.T) {
	c := NewCloudClient(fullConfig())
	ctx := context.Background()
	for _, key := range c.Providers() {
		api := c.Compute(key)
		if api == nil {
			t.Fatalf("Compute(%q) returned nil", key)
		}
		if _, err := api.ListInstances(ctx); err != nil {
			t.Errorf("Compute(%q).ListInstances: %v", key, err)
		}
	}
}

func TestComputeDefaultProvider(t *testing.T) {
	c := NewCloudClient(fullConfig())
	// Empty string routes to default provider.
	if c.Compute("") == nil {
		t.Fatalf("Compute(\"\") should route to default provider")
	}
}

func TestComputeUnknownReturnsNil(t *testing.T) {
	c := NewCloudClient(fullConfig())
	if c.Compute("nope") != nil {
		t.Fatalf("Compute(unknown) should be nil")
	}
	if c.Storage("nope") != nil {
		t.Fatalf("Storage(unknown) should be nil")
	}
	if c.Network("nope") != nil {
		t.Fatalf("Network(unknown) should be nil")
	}
}

func TestComputeOrErr(t *testing.T) {
	c := NewCloudClient(fullConfig())
	if _, err := c.ComputeOrErr("aws"); err != nil {
		t.Fatalf("ComputeOrErr(aws): %v", err)
	}
	if _, err := c.ComputeOrErr("nope"); err == nil {
		t.Fatalf("ComputeOrErr(unknown) should error")
	}
}

func TestStorageAndNetworkRouting(t *testing.T) {
	c := NewCloudClient(fullConfig())
	ctx := context.Background()
	s := c.Storage("gcp")
	if s == nil {
		t.Fatalf("Storage(gcp) nil")
	}
	buckets, err := s.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(buckets) == 0 {
		t.Fatalf("expected seeded buckets")
	}
	if err := s.UploadObject(ctx, buckets[0].Name, "k/v.txt", strings.NewReader("data")); err != nil {
		t.Fatalf("UploadObject: %v", err)
	}

	n := c.Network("azure")
	if n == nil {
		t.Fatalf("Network(azure) nil")
	}
	if _, err := n.ListVPCs(ctx); err != nil {
		t.Fatalf("ListVPCs: %v", err)
	}
}

func TestProviderAccessor(t *testing.T) {
	c := NewCloudClient(fullConfig())
	p, err := c.Provider("huawei")
	if err != nil {
		t.Fatalf("Provider(huawei): %v", err)
	}
	if p.Name() != "huawei" {
		t.Errorf("Name() = %q, want huawei", p.Name())
	}
	if _, err := c.Provider("nope"); err == nil {
		t.Fatalf("Provider(unknown) should error")
	}
}

func TestSetDefault(t *testing.T) {
	c := NewCloudClient(fullConfig())
	if err := c.SetDefault("tencent"); err != nil {
		t.Fatalf("SetDefault(tencent): %v", err)
	}
	if c.DefaultProvider() != "tencent" {
		t.Errorf("DefaultProvider() = %q, want tencent", c.DefaultProvider())
	}
	if err := c.SetDefault("nope"); err == nil {
		t.Fatalf("SetDefault(unknown) should error")
	}
}

func TestListAllInstances(t *testing.T) {
	c := NewCloudClient(fullConfig())
	all, errs := c.ListAllInstances(context.Background())
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(all) == 0 {
		t.Fatalf("expected aggregated instances across providers")
	}
}

// TestCloudClientConcurrentDistinctProviders proves that concurrent calls into
// DIFFERENT providers are race-free (run with -race).
func TestCloudClientConcurrentDistinctProviders(t *testing.T) {
	c := NewCloudClient(fullConfig())
	ctx := context.Background()
	keys := c.Providers()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		key := keys[i%len(keys)]
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			api := c.Compute(key)
			if api == nil {
				t.Errorf("Compute(%q) nil", key)
				return
			}
			id, err := api.CreateInstance(ctx, InstanceReq{Name: "conc", Type: "t.small"})
			if err != nil {
				t.Errorf("CreateInstance: %v", err)
				return
			}
			_ = api.DeleteInstance(ctx, id)
		}(key)
	}
	wg.Wait()
}

// TestCloudClientConcurrentReadWriteDefault runs SetDefault concurrently with
// DefaultProvider reads to prove the RWMutex guards the default field.
func TestCloudClientConcurrentReadWriteDefault(t *testing.T) {
	c := NewCloudClient(fullConfig())
	keys := c.Providers()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = c.SetDefault(keys[i%len(keys)])
		}(i)
		go func() {
			defer wg.Done()
			_ = c.DefaultProvider()
		}()
	}
	wg.Wait()
}
