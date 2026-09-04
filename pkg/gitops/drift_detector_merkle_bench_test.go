package gitops

import (
	"context"
	"testing"
)

// ============================================================================
// Benchmark: Merkle vs Naive Full Diff for drift detection
// ============================================================================

// buildMerkleProviderForBenchmark constructs a provider that simulates
// Helm-release-time Merkle tree caching. The tree is built ONCE, then reused
// across benchmark iterations to measure pure incremental diff cost.
func buildMerkleProviderForBenchmark(charts, fields, kDrift int) (*StaticStateProvider, *DriftMerkleTree, *DriftMerkleTree) {
	desired := synthSnapshot(charts, fields)
	live := deepCopySnapshot(desired)
	for i := 0; i < kDrift; i++ {
		mutateField(live, i*(charts/(kDrift+1)), i%fields)
	}

	provider := &StaticStateProvider{Desired: desired, Live: live}
	dt, lt, _ := BuildDriftMerklePair(desired, live)
	return provider, dt, lt
}

// BenchmarkOldDiffStates_5000charts_10drift measures the Θ(n) baseline with
// the existing map-based DiffStates implementation. This is what production
// currently uses.
func BenchmarkOldDiffStates_5000charts_10drift(b *testing.B) {
	provider, _, _ := buildMerkleProviderForBenchmark(5000, 50, 10)
	app := &Application{Name: "svc", Engine: EngineArgoCD, Namespace: "prod"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewClusterDriftScanner(DriftDetectorConfig{Provider: provider})
		if _, err := s.Scan(ctx, app); err != nil {
			b.Fatalf("scan: %v", err)
		}
	}
}

// BenchmarkNewDiffMerkle_5000charts_10drift measures the Θ(k·log n) path
// using Merkle pruning. Tree is built once (amortized at Helm commit time).
func BenchmarkNewDiffMerkle_5000charts_10drift(b *testing.B) {
	provider, dt, lt := buildMerkleProviderForBenchmark(5000, 50, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := DiffMerkleOptimized(provider, dt, lt)
		if len(result.ChangedKeys) == 0 {
			b.Fatal("expected non-zero changed keys")
		}
	}
}

// BenchmarkOldDiffStates_WorstCaseSingleChange simulates an adversarial case
// where only one field drifted but full scan touches every leaf.
func BenchmarkOldDiffStates_WorstCaseSingleChange(b *testing.B) {
	provider, _, _ := buildMerkleProviderForBenchmark(4096, 15, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewClusterDriftScanner(DriftDetectorConfig{Provider: provider})
		if _, err := s.Scan(context.Background(), &Application{Name: "svc"}); err != nil {
			b.Fatalf("scan: %v", err)
		}
	}
}

// BenchmarkNewDiffMerkle_WorstCaseSingleChange shows Merkle's ability to
// localize a single change in ~2·log2(n) comparisons.
func BenchmarkNewDiffMerkle_WorstCaseSingleChange(b *testing.B) {
	provider, dt, lt := buildMerkleProviderForBenchmark(4096, 15, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := DiffMerkleOptimized(provider, dt, lt)
		if len(result.ChangedKeys) != 1 {
			b.Fatalf("expected exactly 1 changed key, got %d", len(result.ChangedKeys))
		}
	}
}

// BenchmarkOldDiffStates_NoDrift measures full scan overhead even when no
// drift exists (common steady-state case).
func BenchmarkOldDiffStates_NoDrift_WholeTreePrune(b *testing.B) {
	provider, _, _ := buildMerkleProviderForBenchmark(5000, 50, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewClusterDriftScanner(DriftDetectorConfig{Provider: provider})
		if _, err := s.Scan(context.Background(), &Application{Name: "svc"}); err != nil {
			b.Fatalf("scan: %v", err)
		}
	}
}

// BenchmarkNewDiffMerkle_NoDrift_WholeTreePrune proves the amortized advantage:
// one root comparison prunes everything when no drift exists.
func BenchmarkNewDiffMerkle_NoDrift_WholeTreePrune(b *testing.B) {
	provider, dt, lt := buildMerkleProviderForBenchmark(5000, 50, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := DiffMerkleOptimized(provider, dt, lt)
		if len(result.ChangedKeys) != 0 {
			b.Fatalf("expected zero changed keys in no-drift case, got %d", len(result.ChangedKeys))
		}
		if result.Comparisons != 1 {
			b.Fatalf("expected exactly 1 comparison in no-drift case, got %d", result.Comparisons)
		}
	}
}

// Real-world scenario: 120 Helm releases, 40 fields each, 5 drifts scattered
func BenchmarkOldDiffStates_RealWorld_HelmReleases(b *testing.B) {
	provider, _, _ := buildMerkleProviderForBenchmark(120, 40, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewClusterDriftScanner(DriftDetectorConfig{Provider: provider})
		if _, err := s.Scan(context.Background(), &Application{Name: "svc"}); err != nil {
			b.Fatalf("scan: %v", err)
		}
	}
}

func BenchmarkNewDiffMerkle_RealWorld_HelmReleases(b *testing.B) {
	provider, dt, lt := buildMerkleProviderForBenchmark(120, 40, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := DiffMerkleOptimized(provider, dt, lt)
		if len(result.ChangedKeys) != 5 {
			b.Fatalf("expected 5 changed keys, got %d", len(result.ChangedKeys))
		}
	}
}

// ============================================================================
// Memory allocation benchmarks
// ============================================================================

func BenchmarkOldDiffStates_Alloc_5000charts(b *testing.B) {
	provider, _, _ := buildMerkleProviderForBenchmark(5000, 50, 10)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := NewClusterDriftScanner(DriftDetectorConfig{Provider: provider})
		if _, err := s.Scan(context.Background(), &Application{Name: "svc"}); err != nil {
			b.Fatalf("scan: %v", err)
		}
	}
}

func BenchmarkNewDiffMerkle_Alloc_5000charts(b *testing.B) {
	provider, dt, lt := buildMerkleProviderForBenchmark(5000, 50, 10)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := DiffMerkleOptimized(provider, dt, lt)
		_ = result
	}
}
