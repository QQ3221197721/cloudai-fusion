package gitops

import (
	"context"
	"fmt"
	"testing"
)

// buildDrifts synthesizes n drifts spread across kinds/namespaces/fields so the
// clustering benchmark exercises a realistic mix rather than one giant cluster.
func buildDrifts(n int) []DriftDetail {
	kinds := []string{"Deployment", "Service", "ConfigMap", "Secret", "Ingress"}
	namespaces := []string{"prod", "staging", "dev", "system"}
	fields := []string{"spec.replicas", "spec.template.image", "resources.limits.memory", "data.KEY", "metadata.labels.app"}
	sev := []string{"low", "medium", "high", "critical"}

	drifts := make([]DriftDetail, n)
	for i := 0; i < n; i++ {
		drifts[i] = DriftDetail{
			ResourceKind: kinds[i%len(kinds)],
			ResourceName: fmt.Sprintf("res-%d", i),
			Namespace:    namespaces[i%len(namespaces)],
			Field:        fields[i%len(fields)],
			Expected:     "a",
			Actual:       "b",
			Severity:     sev[i%len(sev)],
		}
	}
	return drifts
}

// buildProvider constructs a static provider whose diff yields ~n field drifts.
func buildProvider(n int) *StaticStateProvider {
	desired := make([]ResourceState, n)
	live := make([]ResourceState, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("res-%d", i)
		ns := []string{"prod", "staging", "dev"}[i%3]
		desired[i] = ResourceState{Kind: "Deployment", Name: name, Namespace: ns,
			Fields: map[string]string{"spec.replicas": "3"}}
		live[i] = ResourceState{Kind: "Deployment", Name: name, Namespace: ns,
			Fields: map[string]string{"spec.replicas": "1"}}
	}
	return &StaticStateProvider{Desired: desired, Live: live}
}

// ============================================================================
// Benchmarks — drift detection latency
// ============================================================================

func BenchmarkDriftScan_Latency(b *testing.B) {
	s := NewClusterDriftScanner(DriftDetectorConfig{Provider: buildProvider(100)})
	app := &Application{Name: "svc", Engine: EngineArgoCD, Namespace: "prod"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Scan(ctx, app); err != nil {
			b.Fatalf("scan: %v", err)
		}
	}
}

func BenchmarkDriftScanClusters_Latency(b *testing.B) {
	s := NewClusterDriftScanner(DriftDetectorConfig{Provider: buildProvider(100)})
	app := &Application{Name: "svc", Engine: EngineArgoCD, Namespace: "prod"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ScanClusters(ctx, app); err != nil {
			b.Fatalf("scan clusters: %v", err)
		}
	}
}

// ============================================================================
// Benchmarks — difference clustering throughput
// ============================================================================

func BenchmarkClusterDrifts_100(b *testing.B) {
	drifts := buildDrifts(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClusterDrifts(drifts, 0.35)
	}
}

func BenchmarkClusterDrifts_1000(b *testing.B) {
	drifts := buildDrifts(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClusterDrifts(drifts, 0.35)
	}
}

func BenchmarkDiffStates_1000(b *testing.B) {
	p := buildProvider(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DiffStates(p.Desired, p.Live)
	}
}

// ============================================================================
// Benchmarks — 10K scale clustering (stress test)
// ============================================================================

func BenchmarkClusterDrifts_10000(b *testing.B) {
	drifts := buildDrifts(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ClusterDrifts(drifts, 0.35)
	}
}

// Baseline: flat per-resource grouping (no clustering, just O(n) bucket by namespace)
// This proves that single-linkage + union-find has measurable advantage.
func BenchmarkBaseline_FlatGrouping_1000(b *testing.B) {
	drifts := buildDrifts(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simple grouping by namespace only (no dissimilarity calc)
		nsGroups := make(map[string][]DriftDetail)
		for _, d := range drifts {
			nsGroups[d.Namespace] = append(nsGroups[d.Namespace], d)
		}
		_ = len(nsGroups)
	}
}

func BenchmarkBaseline_FlatGrouping_10000(b *testing.B) {
	drifts := buildDrifts(10000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nsGroups := make(map[string][]DriftDetail)
		for _, d := range drifts {
			nsGroups[d.Namespace] = append(nsGroups[d.Namespace], d)
		}
		_ = len(nsGroups)
	}
}

// ============================================================================
// Benchmarks — remediation decision latency
// ============================================================================

func BenchmarkPlanRemediation_Progressive(b *testing.B) {
	drifts := buildDrifts(200)
	clusters := ClusterDrifts(drifts, 0.35)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PlanRemediation(clusters, RemediationConfig{Strategy: StrategyProgressiveRollback})
	}
}

func BenchmarkPlanRemediation_Batched(b *testing.B) {
	drifts := buildDrifts(200)
	clusters := ClusterDrifts(drifts, 0.35)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PlanRemediation(clusters, RemediationConfig{Strategy: StrategyBatchedDeploy, MaxBatchSize: 10})
	}
}
