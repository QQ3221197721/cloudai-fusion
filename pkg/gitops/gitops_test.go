package gitops

import (
	"context"
	"testing"
	"time"
)

func TestCreateApplication(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	app, err := mgr.CreateApplication(ctx, &Application{
		Name:           "cloudai-apiserver",
		RepoURL:        "https://github.com/cloudai-fusion/cloudai-fusion",
		RepoPath:       "deploy/helm/cloudai-fusion",
		TargetRevision: "main",
		Namespace:      "cloudai-system",
		Environment:    "development",
	})
	if err != nil {
		t.Fatalf("CreateApplication failed: %v", err)
	}
	if app.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if app.Engine != EngineArgoCD {
		t.Errorf("expected default engine argocd, got %s", app.Engine)
	}
	if app.SyncPolicy == nil || !app.SyncPolicy.AutoSync {
		t.Error("expected default sync policy with auto-sync enabled")
	}
}

func TestCreateDuplicateApplication(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	_, err := mgr.CreateApplication(ctx, &Application{
		Name: "test-app", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = mgr.CreateApplication(ctx, &Application{
		Name: "test-app", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})
	if err == nil {
		t.Fatal("expected error for duplicate application")
	}
}

func TestGetApplication(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	app, _ := mgr.CreateApplication(ctx, &Application{
		Name: "get-test", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})

	got, err := mgr.GetApplication(ctx, app.ID)
	if err != nil {
		t.Fatalf("GetApplication failed: %v", err)
	}
	if got.Name != "get-test" {
		t.Errorf("expected name 'get-test', got '%s'", got.Name)
	}

	_, err = mgr.GetApplication(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
}

func TestListApplications(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	mgr.CreateApplication(ctx, &Application{
		Name: "app-1", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})
	mgr.CreateApplication(ctx, &Application{
		Name: "app-2", Environment: "staging",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})

	all, _ := mgr.ListApplications(ctx, "")
	if len(all) != 2 {
		t.Errorf("expected 2 apps, got %d", len(all))
	}

	devOnly, _ := mgr.ListApplications(ctx, "dev")
	if len(devOnly) != 1 {
		t.Errorf("expected 1 dev app, got %d", len(devOnly))
	}
}

func TestDeleteApplication(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	app, _ := mgr.CreateApplication(ctx, &Application{
		Name: "del-test", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})

	err := mgr.DeleteApplication(ctx, app.ID)
	if err != nil {
		t.Fatalf("DeleteApplication failed: %v", err)
	}

	_, err = mgr.GetApplication(ctx, app.ID)
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

func TestSyncApplication(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	app, _ := mgr.CreateApplication(ctx, &Application{
		Name: "sync-test", Environment: "dev", Engine: EngineFlux,
		RepoURL: "https://example.com/repo", TargetRevision: "v1.0.0",
	})

	result, err := mgr.SyncApplication(ctx, app.ID, "abc123")
	if err != nil {
		t.Fatalf("SyncApplication failed: %v", err)
	}
	if result.Status != SyncStatusSynced {
		t.Errorf("expected synced status, got %s", result.Status)
	}
	if result.Revision != "abc123" {
		t.Errorf("expected revision 'abc123', got '%s'", result.Revision)
	}
}

func TestPromotionPipeline(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	app, _ := mgr.CreateApplication(ctx, &Application{
		Name: "promote-test", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})

	pipeline, err := mgr.CreatePromotionPipeline(ctx, app.ID, "v1.2.0", nil)
	if err != nil {
		t.Fatalf("CreatePromotionPipeline failed: %v", err)
	}
	if pipeline.Status != PromotionPending {
		t.Errorf("expected pending status, got %s", pipeline.Status)
	}
	if len(pipeline.Stages) != 3 {
		t.Errorf("expected 3 default stages, got %d", len(pipeline.Stages))
	}

	// Advance through stages
	p, err := mgr.AdvancePromotion(ctx, pipeline.ID)
	if err != nil {
		t.Fatalf("AdvancePromotion failed: %v", err)
	}
	if p.CurrentStage != 1 {
		t.Errorf("expected current stage 1, got %d", p.CurrentStage)
	}
	if p.Status != PromotionInProgress {
		t.Errorf("expected in-progress status, got %s", p.Status)
	}

	// Advance to stage 2
	p, _ = mgr.AdvancePromotion(ctx, pipeline.ID)
	if p.CurrentStage != 2 {
		t.Errorf("expected current stage 2, got %d", p.CurrentStage)
	}

	// Final advance → completed
	p, _ = mgr.AdvancePromotion(ctx, pipeline.ID)
	if p.Status != PromotionCompleted {
		t.Errorf("expected completed status, got %s", p.Status)
	}

	// Cannot advance after completion
	_, err = mgr.AdvancePromotion(ctx, pipeline.ID)
	if err == nil {
		t.Fatal("expected error when advancing completed pipeline")
	}
}

func TestRollbackPromotion(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	app, _ := mgr.CreateApplication(ctx, &Application{
		Name: "rollback-test", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})

	pipeline, _ := mgr.CreatePromotionPipeline(ctx, app.ID, "v2.0.0", nil)
	mgr.AdvancePromotion(ctx, pipeline.ID)

	p, err := mgr.RollbackPromotion(ctx, pipeline.ID)
	if err != nil {
		t.Fatalf("RollbackPromotion failed: %v", err)
	}
	if p.Status != PromotionRolledBack {
		t.Errorf("expected rolled-back status, got %s", p.Status)
	}
}

func TestDriftDetection(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	app, _ := mgr.CreateApplication(ctx, &Application{
		Name: "drift-test", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})

	report, err := mgr.DetectDrift(ctx, app.ID)
	if err != nil {
		t.Fatalf("DetectDrift failed: %v", err)
	}
	if report.ID == "" {
		t.Fatal("expected non-empty report ID")
	}

	reports, _ := mgr.GetDriftReports(ctx, app.ID)
	if len(reports) != 1 {
		t.Errorf("expected 1 drift report, got %d", len(reports))
	}
}

func TestTerraformModuleLifecycle(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	module, err := mgr.RegisterTerraformModule(ctx, &TerraformModule{
		Name:     "eks-cluster",
		Source:   "terraform-aws-modules/eks/aws",
		Version:  "20.0.0",
		Provider: "aws",
		Variables: map[string]string{
			"cluster_name": "cloudai-prod",
			"region":       "us-east-1",
		},
	})
	if err != nil {
		t.Fatalf("RegisterTerraformModule failed: %v", err)
	}
	if module.State != TFStateUninitialized {
		t.Errorf("expected uninitialized state, got %s", module.State)
	}

	// Plan
	plan, err := mgr.PlanTerraformModule(ctx, module.ID)
	if err != nil {
		t.Fatalf("PlanTerraformModule failed: %v", err)
	}
	if plan.ModuleID != module.ID {
		t.Errorf("expected module ID %s, got %s", module.ID, plan.ModuleID)
	}

	// Apply
	err = mgr.ApplyTerraformModule(ctx, module.ID)
	if err != nil {
		t.Fatalf("ApplyTerraformModule failed: %v", err)
	}

	// Destroy
	err = mgr.DestroyTerraformModule(ctx, module.ID)
	if err != nil {
		t.Fatalf("DestroyTerraformModule failed: %v", err)
	}

	// List
	modules, _ := mgr.ListTerraformModules(ctx)
	if len(modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(modules))
	}
}

func TestApplyWithoutPlan(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	module, _ := mgr.RegisterTerraformModule(ctx, &TerraformModule{
		Name: "test-module", Source: "local", Provider: "aws",
	})

	err := mgr.ApplyTerraformModule(ctx, module.ID)
	if err == nil {
		t.Fatal("expected error when applying without plan")
	}
}

func TestCancelledContext(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.CreateApplication(ctx, &Application{Name: "test"})
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}

	_, err = mgr.SyncApplication(ctx, "any-id", "rev")
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

func TestMinHelper(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) should be 3")
	}
	if min(7, 2) != 2 {
		t.Error("min(7,2) should be 2")
	}
}

func TestDriftDetectionLoop(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.DriftCheckInterval = 50 * time.Millisecond
	mgr := NewManager(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	mgr.CreateApplication(ctx, &Application{
		Name: "loop-test", Environment: "dev",
		RepoURL: "https://example.com/repo", TargetRevision: "main",
	})

	mgr.StartDriftDetectionLoop(ctx)
	// Loop should exit cleanly after context timeout
}
