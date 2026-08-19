package security

// supply_chain_bench_test.go - Simplified version for Module 31/33 benchmarks

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkSupplyChainManager_GenerateSBOM measures the SIMULATED SBOM generator.
func BenchmarkSupplyChainManager_GenerateSBOM(b *testing.B) {
	mgr := NewSupplyChainManager(SupplyChainConfig{})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = mgr.GenerateSBOM("ghcr.io/cloudai-fusion/app:v1", "sha256:deadbeef")
	}
}

// BenchmarkSecurity_Edge_Composite validates core security paths.
func BenchmarkSecurity_Edge_Composite(b *testing.B) {
	ctx := context.Background()
	cfg := HardeningConfig{
		PSS:    DefaultPSSConfig(),
		Cosign: DefaultCosignConfig(),
	}
	hm := NewHardeningManager(cfg)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = hm.VerifyImage(ctx, fmt.Sprintf("ghcr.io/cloudai-fusion/app:v%d", i%50))
	}
}
