package gitops

import (
	"context"
	"testing"
)

// staticApp returns a representative application for scanning.
func staticApp() *Application {
	return &Application{
		ID: "app-1", Name: "payments", Engine: EngineArgoCD,
		Namespace: "prod", Environment: "production",
	}
}

// twoResourceProvider yields a desired/live pair with several field divergences
// across two resources in the same namespace (should cluster together) plus one
// resource in a different namespace (separate cluster).
func twoResourceProvider() *StaticStateProvider {
	return &StaticStateProvider{
		Desired: []ResourceState{
			{Kind: "Deployment", Name: "payments", Namespace: "prod", Fields: map[string]string{
				"spec.replicas":            "3",
				"spec.template.image":      "payments:v2",
				"resources.limits.memory":  "512Mi",
			}},
			{Kind: "Deployment", Name: "orders", Namespace: "prod", Fields: map[string]string{
				"spec.replicas": "2",
			}},
			{Kind: "ConfigMap", Name: "app-config", Namespace: "staging", Fields: map[string]string{
				"data.LOG_LEVEL": "info",
			}},
		},
		Live: []ResourceState{
			{Kind: "Deployment", Name: "payments", Namespace: "prod", Fields: map[string]string{
				"spec.replicas":            "1",       // drift (high)
				"spec.template.image":      "payments:v1", // drift (high)
				"resources.limits.memory":  "256Mi",   // drift (medium)
			}},
			{Kind: "Deployment", Name: "orders", Namespace: "prod", Fields: map[string]string{
				"spec.replicas": "5", // drift (high)
			}},
			{Kind: "ConfigMap", Name: "app-config", Namespace: "staging", Fields: map[string]string{
				"data.LOG_LEVEL": "debug", // drift (low)
			}},
		},
	}
}

// ============================================================================
// Diff
// ============================================================================

func TestDiffStates_DetectsFieldDivergence(t *testing.T) {
	p := twoResourceProvider()
	drifts := DiffStates(p.Desired, p.Live)
	// 3 (payments) + 1 (orders) + 1 (configmap) = 5 field drifts
	if len(drifts) != 5 {
		t.Fatalf("expected 5 drifts, got %d: %+v", len(drifts), drifts)
	}
}

func TestDiffStates_MissingAndExtra(t *testing.T) {
	desired := []ResourceState{
		{Kind: "Service", Name: "gone", Namespace: "prod", Fields: map[string]string{"port": "80"}},
	}
	live := []ResourceState{
		{Kind: "Service", Name: "rogue", Namespace: "prod", Fields: map[string]string{"port": "8080"}},
	}
	drifts := DiffStates(desired, live)
	// One missing (gone), one extra (rogue).
	var missing, extra bool
	for _, d := range drifts {
		if d.ResourceName == "gone" && d.Actual == "missing" {
			missing = true
		}
		if d.ResourceName == "rogue" && d.Actual == "present" {
			extra = true
		}
	}
	if !missing || !extra {
		t.Fatalf("expected both missing+extra drift, got missing=%v extra=%v (%+v)", missing, extra, drifts)
	}
}

func TestDiffStates_NoDrift(t *testing.T) {
	same := []ResourceState{{Kind: "Deployment", Name: "a", Namespace: "prod", Fields: map[string]string{"x": "1"}}}
	if d := DiffStates(same, same); len(d) != 0 {
		t.Fatalf("identical states must yield zero drift, got %d", len(d))
	}
}

// ============================================================================
// Clustering
// ============================================================================

func TestClusterDrifts_GroupsByStructure(t *testing.T) {
	p := twoResourceProvider()
	drifts := DiffStates(p.Desired, p.Live)
	clusters := ClusterDrifts(drifts, 0.35)

	// prod Deployment drifts (payments+orders, all spec.*) should cluster; the
	// staging ConfigMap is its own cluster (different ns + kind + field prefix).
	if len(clusters) < 2 {
		t.Fatalf("expected at least 2 clusters, got %d", len(clusters))
	}

	var stagingSeen bool
	for _, c := range clusters {
		if c.Namespace == "staging" {
			stagingSeen = true
			if len(c.Drifts) != 1 {
				t.Errorf("staging cluster should hold exactly the ConfigMap drift, got %d", len(c.Drifts))
			}
		}
	}
	if !stagingSeen {
		t.Error("expected a distinct staging cluster")
	}
}

func TestClusterDrifts_Empty(t *testing.T) {
	if c := ClusterDrifts(nil, 0.35); c != nil {
		t.Fatalf("empty input should yield nil clusters, got %d", len(c))
	}
}

func TestClusterDrifts_TightThresholdSplits(t *testing.T) {
	drifts := []DriftDetail{
		{ResourceKind: "Deployment", Namespace: "prod", Field: "spec.replicas", Severity: "high"},
		{ResourceKind: "ConfigMap", Namespace: "prod", Field: "data.X", Severity: "low"},
	}
	// A very tight threshold keeps dissimilar drifts apart.
	clusters := ClusterDrifts(drifts, 0.05)
	if len(clusters) != 2 {
		t.Fatalf("tight threshold should keep 2 separate clusters, got %d", len(clusters))
	}
}

func TestUnionFind_Basic(t *testing.T) {
	uf := newUnionFind(5)
	uf.union(0, 1)
	uf.union(1, 2)
	if uf.find(0) != uf.find(2) {
		t.Fatal("0 and 2 should share a root after transitive union")
	}
	if uf.find(0) == uf.find(3) {
		t.Fatal("0 and 3 should be in different sets")
	}
}

// ============================================================================
// Scanner (DriftScanner interface)
// ============================================================================

func TestClusterDriftScanner_ImplementsInterface(t *testing.T) {
	var _ DriftScanner = NewClusterDriftScanner(DriftDetectorConfig{Provider: twoResourceProvider()})
}

func TestClusterDriftScanner_ScanReturnsFlatDrifts(t *testing.T) {
	s := NewClusterDriftScanner(DriftDetectorConfig{Provider: twoResourceProvider()})
	drifts, err := s.Scan(context.Background(), staticApp())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(drifts) != 5 {
		t.Fatalf("expected 5 flat drifts, got %d", len(drifts))
	}
}

func TestClusterDriftScanner_NoProvider(t *testing.T) {
	s := NewClusterDriftScanner(DriftDetectorConfig{})
	if _, err := s.Scan(context.Background(), staticApp()); err == nil {
		t.Fatal("scan without a provider must error, not silently pass")
	}
}

func TestClusterDriftScanner_IntegratesWithManager(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	app, err := mgr.CreateApplication(context.Background(), &Application{
		Name: "svc", Engine: EngineArgoCD, Namespace: "prod", Environment: "production",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	mgr.SetDriftScanner(NewClusterDriftScanner(DriftDetectorConfig{Provider: twoResourceProvider()}))

	report, err := mgr.DetectDrift(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("detect drift: %v", err)
	}
	if report.Status != DriftDetected {
		t.Fatalf("expected DriftDetected, got %s", report.Status)
	}
	if len(report.Drifts) != 5 {
		t.Fatalf("expected 5 drifts in report, got %d", len(report.Drifts))
	}
}

func TestClusterDriftScanner_ScoresCriticality(t *testing.T) {
	s := NewClusterDriftScanner(DriftDetectorConfig{Provider: twoResourceProvider()})
	s.SetCriticality("Deployment", 1.0)
	clusters, err := s.ScanClusters(context.Background(), staticApp())
	if err != nil {
		t.Fatalf("scan clusters: %v", err)
	}
	var scored bool
	for _, c := range clusters {
		if c.ClusterKind == "Deployment" {
			scored = true
			if c.SeverityScore <= 0 {
				t.Errorf("Deployment cluster should have positive severity score, got %f", c.SeverityScore)
			}
		}
	}
	if !scored {
		t.Error("expected at least one Deployment cluster")
	}
}

// ============================================================================
// Remediation planning
// ============================================================================

func TestPlanRemediation_ProgressiveOrdersSafestFirst(t *testing.T) {
	clusters := []ClusteredDrift{
		{ID: "c-prod-high", Namespace: "prod", ClusterKind: "Deployment", MaxSeverity: "high",
			Drifts: []DriftDetail{{ResourceKind: "Deployment", Namespace: "prod", ResourceName: "p"}}},
		{ID: "c-staging-low", Namespace: "staging", ClusterKind: "ConfigMap", MaxSeverity: "low",
			Drifts: []DriftDetail{{ResourceKind: "ConfigMap", Namespace: "staging", ResourceName: "c"}}},
	}
	plan := PlanRemediation(clusters, RemediationConfig{Strategy: StrategyProgressiveRollback})
	if plan.Strategy != StrategyProgressiveRollback {
		t.Fatalf("unexpected strategy %s", plan.Strategy)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	// staging (canary) must be first, and marked canary.
	if plan.Steps[0].Namespace != "staging" || !plan.Steps[0].Canary {
		t.Fatalf("expected canary staging step first, got %+v", plan.Steps[0])
	}
	if plan.Steps[0].Action != string(ActionRollback) {
		t.Errorf("progressive plan should rollback, got %s", plan.Steps[0].Action)
	}
}

func TestPlanRemediation_BatchedGroupsByNamespace(t *testing.T) {
	clusters := []ClusteredDrift{
		{ID: "a", Namespace: "ns1", ClusterKind: "Deployment", MaxSeverity: "low"},
		{ID: "b", Namespace: "ns1", ClusterKind: "Service", MaxSeverity: "medium"},
		{ID: "c", Namespace: "ns2", ClusterKind: "Deployment", MaxSeverity: "high"},
	}
	plan := PlanRemediation(clusters, RemediationConfig{Strategy: StrategyBatchedDeploy})
	if plan.TotalBatches != 2 {
		t.Fatalf("expected 2 namespace batches, got %d", plan.TotalBatches)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(plan.Steps))
	}
	// ns1 (peak medium) should be batched before ns2 (peak high).
	if plan.Steps[0].Namespace != "ns1" {
		t.Errorf("expected lower-severity namespace ns1 first, got %s", plan.Steps[0].Namespace)
	}
	if plan.Steps[len(plan.Steps)-1].Namespace != "ns2" {
		t.Errorf("expected higher-severity ns2 last, got %s", plan.Steps[len(plan.Steps)-1].Namespace)
	}
}

func TestPlanRemediation_BatchedRespectsMaxBatchSize(t *testing.T) {
	clusters := []ClusteredDrift{
		{ID: "a", Namespace: "ns1", MaxSeverity: "low"},
		{ID: "b", Namespace: "ns1", MaxSeverity: "low"},
		{ID: "c", Namespace: "ns1", MaxSeverity: "low"},
	}
	plan := PlanRemediation(clusters, RemediationConfig{Strategy: StrategyBatchedDeploy, MaxBatchSize: 2})
	if plan.TotalBatches != 2 {
		t.Fatalf("3 clusters with maxBatch=2 should form 2 batches, got %d", plan.TotalBatches)
	}
}

func TestPlanRemediation_EmptyClusters(t *testing.T) {
	plan := PlanRemediation(nil, RemediationConfig{})
	if len(plan.Steps) != 0 || plan.TotalBatches != 0 {
		t.Fatalf("empty clusters should yield empty plan, got %+v", plan)
	}
	if plan.Strategy != StrategyProgressiveRollback {
		t.Errorf("default strategy should be progressive-rollback, got %s", plan.Strategy)
	}
}

func TestResourceIDs_Dedup(t *testing.T) {
	c := ClusteredDrift{Drifts: []DriftDetail{
		{ResourceKind: "Deployment", Namespace: "prod", ResourceName: "x"},
		{ResourceKind: "Deployment", Namespace: "prod", ResourceName: "x"}, // dup
		{ResourceKind: "Deployment", Namespace: "prod", ResourceName: "y"},
	}}
	ids := resourceIDs(c)
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique resource ids, got %d: %v", len(ids), ids)
	}
}
