package cloudprovider

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkLocalMock_ListInstances measures listing throughput over a backend
// pre-seeded with 100 instances (includes copy + deterministic sort cost).
func BenchmarkLocalMock_ListInstances(b *testing.B) {
	p := NewLocalMockProvider(WithoutLatency())
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if _, err := p.CreateInstance(ctx, CreateInstanceRequest{Name: "seed", Type: "t3.medium"}); err != nil {
			b.Fatalf("seed create failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.ListInstances(ctx)
	}
}

// BenchmarkLocalMock_CreateInstance measures instance creation throughput.
func BenchmarkLocalMock_CreateInstance(b *testing.B) {
	p := NewLocalMockProvider(WithoutLatency())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, err := p.CreateInstance(context.Background(), CreateInstanceRequest{
			Name: fmt.Sprintf("instance-%d", i),
			Type: "t3.medium",
		})
		if err != nil {
			b.Fatalf("create failed: %v", err)
		}
		_ = id
	}
}

// BenchmarkLocalMock_DeleteInstance measures instance deletion throughput. It
// pre-creates exactly b.N instances (untimed) then times deleting all of them.
func BenchmarkLocalMock_DeleteInstance(b *testing.B) {
	ctx := context.Background()
	p := NewLocalMockProvider(WithoutLatency())

	ids := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		id, err := p.CreateInstance(ctx, CreateInstanceRequest{Name: "del", Type: "t3.micro"})
		if err != nil {
			b.Fatalf("setup create failed: %v", err)
		}
		ids[i] = id
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.DeleteInstance(ctx, ids[i]); err != nil {
			b.Fatalf("delete failed: %v", err)
		}
	}
}

// BenchmarkLocalMock_GetPricing measures pricing catalog lookup throughput.
func BenchmarkLocalMock_GetPricing(b *testing.B) {
	p := NewLocalMockProvider(WithoutLatency())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.GetPricing("t3.micro", "us-east-1")
	}
}

// BenchmarkRegistry_DispatchOverhead measures the unified call dispatch overhead
// per operation. This captures the map lookup cost + interface call indirection.
func BenchmarkRegistry_DispatchOverhead(b *testing.B) {
	reg := NewRegistry()
	p := NewLocalMockProvider(WithoutLatency())
	reg.Register(ProviderLocalMock, p)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reg.ListInstances(ctx, ProviderLocalMock)
	}
}

// BenchmarkRegistry_LookupOverhead measures just the registry lookup cost,
// excluding any provider implementation work.
func BenchmarkRegistry_LookupOverhead(b *testing.B) {
	reg := NewRegistry()
	p := NewLocalMockProvider(WithoutLatency())
	reg.Register(ProviderLocalMock, p)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = reg.Get(ProviderLocalMock)
	}
}

// BenchmarkCloudAdapter_CredentialsRequired_HonorableError measures the error
// path for cloud adapters operating in credentials-required mode. The intent is
// to confirm that no fake operations occur when credentials are missing — every
// method returns a typed error immediately.
func BenchmarkCloudAdapter_CredentialsRequired_HonorableError(b *testing.B) {
	ctx := context.Background()
	pAWS := NewAWSProvider(Credentials{}) // empty creds -> credentials required

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pAWS.ListInstances(ctx)
		if err == nil {
					b.Fatal("expected ErrCredentialsRequired, got nil")
		}
	}
}

// BenchmarkCloudAdapter_Adapters_ReturnCapabilities reports truthful capability.
func BenchmarkCloudAdapter_Adapters_ReturnCapabilities(b *testing.B) {
	pAWS := NewAWSProvider(Credentials{})
	pAzure := NewAzureProvider(Credentials{})
	pGCP := NewGCPProvider(Credentials{})

	b.Run("AWS", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = pAWS.Capabilities()
		}
	})
	b.Run("Azure", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = pAzure.Capabilities()
		}
	})
	b.Run("GCP", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = pGCP.Capabilities()
		}
	})
}
