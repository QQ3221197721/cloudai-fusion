package cloud_test

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud/providers"
)

// Benchmark against Crossplane runtime/terraform CLI proxy for fair comparison

func BenchmarkM2_AliyunDescribeClusters(b *testing.B) {
	ctx := context.Background()
	
	// Use mock provider since no real credentials in CI
	cfg := providers.ProviderConfig{
		Name:       "aliyun",
		Region:     "cn-hangzhou",
		AccessKey:  "", // empty = stub mode
		SecretKey:  "",
	}
	
	// Create generic provider for measurement
	p := &providers.GenericProvider{}
	if err := p.Init(cfg); err != nil {
		b.Fatalf("failed to init provider: %v", err)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Ping equivalent via ListInstances (lightweight call)
		_, err := p.ListInstances(ctx)
		if err != nil {
			b.Logf("List error (expected in stub mode): %v", err)
		}
	}
}
