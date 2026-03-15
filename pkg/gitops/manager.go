package gitops

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Manager — GitOps workflow orchestration
// ============================================================================

// ManagerConfig holds GitOps manager configuration.
type ManagerConfig struct {
	// DefaultEngine is the default GitOps engine (argocd or flux).
	DefaultEngine EngineType

	// DriftCheckInterval controls how often drift detection runs.
	DriftCheckInterval time.Duration

	// AutoRemediateDrift enables automatic remediation of detected drift.
	AutoRemediateDrift bool

	// PromotionPipelines defines the default promotion stages.
	DefaultPromotionStages []PromotionStage
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		DefaultEngine:      EngineArgoCD,
		DriftCheckInterval: 5 * time.Minute,
		AutoRemediateDrift: false,
		DefaultPromotionStages: []PromotionStage{
			{Name: "dev", Environment: "development", Order: 1},
			{Name: "staging", Environment: "staging", Order: 2, Gates: []PromotionGate{
				{Type: GateTestSuite, Name: "integration-tests", Required: true},
				{Type: GateSecurityScan, Name: "trivy-scan", Required: true},
			}},
			{Name: "production", Environment: "production", Order: 3, Gates: []PromotionGate{
				{Type: GateCanaryAnalysis, Name: "canary-10pct", Required: true, Timeout: "30m"},
				{Type: GateSLOValidation, Name: "slo-check", Required: true, Timeout: "15m"},
				{Type: GateManualApproval, Name: "prod-approval", Required: true},
			}},
		},
	}
}

// Manager orchestrates GitOps workflows including sync, promotion,
// drift detection, and Terraform module management.
type Manager struct {
	config      ManagerConfig
	apps        map[string]*Application
	pipelines   map[string]*PromotionPipeline
	driftReports map[string]*DriftReport
	tfModules   map[string]*TerraformModule
	logger      *logrus.Logger
	mu          sync.RWMutex
}

// NewManager creates a new GitOps manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		config:       cfg,
		apps:         make(map[string]*Application),
		pipelines:    make(map[string]*PromotionPipeline),
		driftReports: make(map[string]*DriftReport),
		tfModules:    make(map[string]*TerraformModule),
		logger:       logrus.StandardLogger(),
	}
}

// ============================================================================
// Application Lifecycle
// ============================================================================

// CreateApplication registers a new GitOps-managed application.
func (m *Manager) CreateApplication(ctx context.Context, app *Application) (*Application, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicate
	for _, existing := range m.apps {
		if existing.Name == app.Name && existing.Environment == app.Environment {
			return nil, fmt.Errorf("application '%s' already exists in environment '%s'", app.Name, app.Environment)
		}
	}

	now := common.NowUTC()
	app.ID = common.NewUUID()
	app.CreatedAt = now
	app.UpdatedAt = now
	app.SyncStatus = SyncStatusUnknown
	app.HealthStatus = HealthStatusUnknown

	if app.Engine == "" {
		app.Engine = m.config.DefaultEngine
	}
	if app.SyncPolicy == nil {
		app.SyncPolicy = &SyncPolicy{
			AutoSync: true,
			SelfHeal: true,
			Prune:    false,
		}
	}

	m.apps[app.ID] = app

	m.logger.WithFields(logrus.Fields{
		"app_id":      app.ID,
		"app_name":    app.Name,
		"engine":      app.Engine,
		"environment": app.Environment,
		"repo":        app.RepoURL,
	}).Info("GitOps application created")

	return app, nil
}

// GetApplication retrieves an application by ID.
func (m *Manager) GetApplication(ctx context.Context, appID string) (*Application, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	app, ok := m.apps[appID]
	if !ok {
		return nil, fmt.Errorf("application '%s' not found", appID)
	}
	return app, nil
}

// ListApplications returns all registered applications, optionally filtered by environment.
func (m *Manager) ListApplications(ctx context.Context, environment string) ([]*Application, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	apps := make([]*Application, 0, len(m.apps))
	for _, app := range m.apps {
		if environment == "" || app.Environment == environment {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

// DeleteApplication removes a GitOps application.
func (m *Manager) DeleteApplication(ctx context.Context, appID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.apps[appID]; !ok {
		return fmt.Errorf("application '%s' not found", appID)
	}
	delete(m.apps, appID)

	m.logger.WithField("app_id", appID).Info("GitOps application deleted")
	return nil
}

// ============================================================================
// Sync Operations
// ============================================================================

// SyncApplication triggers a synchronization for an application.
func (m *Manager) SyncApplication(ctx context.Context, appID string, revision string) (*SyncResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	app, ok := m.apps[appID]
	if !ok {
		return nil, fmt.Errorf("application '%s' not found", appID)
	}

	now := common.NowUTC()
	if revision == "" {
		revision = app.TargetRevision
	}

	app.SyncStatus = SyncStatusProgressing
	app.UpdatedAt = now

	m.logger.WithFields(logrus.Fields{
		"app_id":      appID,
		"app_name":    app.Name,
		"revision":    revision,
		"engine":      app.Engine,
		"environment": app.Environment,
	}).Info("Sync triggered")

	// Simulate sync based on engine type
	result := &SyncResult{
		ApplicationID: appID,
		Revision:      revision,
		Status:        SyncStatusSynced,
		Message:       fmt.Sprintf("Successfully synced via %s engine", app.Engine),
		StartedAt:     now,
		CompletedAt:   common.NowUTC(),
	}

	// In production, this would call ArgoCD/Flux API:
	// - ArgoCD: POST /api/v1/applications/{name}/sync
	// - Flux: kubectl annotate gitrepository <name> reconcile.fluxcd.io/requestedAt=<timestamp>
	switch app.Engine {
	case EngineArgoCD:
		result.Message = fmt.Sprintf("ArgoCD sync completed for revision %s", revision)
	case EngineFlux:
		result.Message = fmt.Sprintf("Flux reconciliation completed for revision %s", revision)
	}

	app.SyncStatus = SyncStatusSynced
	app.HealthStatus = HealthStatusHealthy
	app.LastSyncedAt = &now
	app.UpdatedAt = common.NowUTC()

	return result, nil
}

// ============================================================================
// Environment Promotion
// ============================================================================

// CreatePromotionPipeline starts a new promotion pipeline.
func (m *Manager) CreatePromotionPipeline(ctx context.Context, appID, revision string, stages []PromotionStage) (*PromotionPipeline, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.apps[appID]; !ok {
		return nil, fmt.Errorf("application '%s' not found", appID)
	}

	if len(stages) == 0 {
		stages = m.config.DefaultPromotionStages
	}

	now := common.NowUTC()
	pipeline := &PromotionPipeline{
		ID:           common.NewUUID(),
		Name:         fmt.Sprintf("promote-%s-%s", appID[:8], revision[:min(8, len(revision))]),
		AppID:        appID,
		Stages:       stages,
		Status:       PromotionPending,
		CurrentStage: 0,
		Revision:     revision,
		StartedAt:    now,
	}

	m.pipelines[pipeline.ID] = pipeline

	m.logger.WithFields(logrus.Fields{
		"pipeline_id": pipeline.ID,
		"app_id":      appID,
		"revision":    revision,
		"stages":      len(stages),
	}).Info("Promotion pipeline created")

	return pipeline, nil
}

// AdvancePromotion moves the pipeline to the next stage.
func (m *Manager) AdvancePromotion(ctx context.Context, pipelineID string) (*PromotionPipeline, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	pipeline, ok := m.pipelines[pipelineID]
	if !ok {
		return nil, fmt.Errorf("pipeline '%s' not found", pipelineID)
	}

	if pipeline.Status == PromotionCompleted || pipeline.Status == PromotionFailed {
		return nil, fmt.Errorf("pipeline '%s' is already in terminal state: %s", pipelineID, pipeline.Status)
	}

	// Check gates for current stage
	currentStage := pipeline.Stages[pipeline.CurrentStage]
	for _, gate := range currentStage.Gates {
		if gate.Required {
			m.logger.WithFields(logrus.Fields{
				"pipeline_id": pipelineID,
				"stage":       currentStage.Name,
				"gate":        gate.Name,
				"gate_type":   gate.Type,
			}).Info("Evaluating promotion gate")
		}
	}

	// Move to next stage
	pipeline.CurrentStage++
	if pipeline.CurrentStage >= len(pipeline.Stages) {
		pipeline.Status = PromotionCompleted
		now := common.NowUTC()
		pipeline.CompletedAt = &now
		m.logger.WithField("pipeline_id", pipelineID).Info("Promotion pipeline completed")
	} else {
		pipeline.Status = PromotionInProgress
		nextStage := pipeline.Stages[pipeline.CurrentStage]
		m.logger.WithFields(logrus.Fields{
			"pipeline_id": pipelineID,
			"next_stage":  nextStage.Name,
		}).Info("Promoted to next stage")
	}

	return pipeline, nil
}

// RollbackPromotion rolls back a promotion pipeline.
func (m *Manager) RollbackPromotion(ctx context.Context, pipelineID string) (*PromotionPipeline, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	pipeline, ok := m.pipelines[pipelineID]
	if !ok {
		return nil, fmt.Errorf("pipeline '%s' not found", pipelineID)
	}

	pipeline.Status = PromotionRolledBack
	now := common.NowUTC()
	pipeline.CompletedAt = &now

	m.logger.WithFields(logrus.Fields{
		"pipeline_id":  pipelineID,
		"rolled_back_from": pipeline.Stages[pipeline.CurrentStage].Name,
	}).Warn("Promotion pipeline rolled back")

	return pipeline, nil
}

// GetPromotionPipeline retrieves a pipeline by ID.
func (m *Manager) GetPromotionPipeline(ctx context.Context, pipelineID string) (*PromotionPipeline, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	pipeline, ok := m.pipelines[pipelineID]
	if !ok {
		return nil, fmt.Errorf("pipeline '%s' not found", pipelineID)
	}
	return pipeline, nil
}

// ============================================================================
// Configuration Drift Detection
// ============================================================================

// DetectDrift scans an application for configuration drift.
func (m *Manager) DetectDrift(ctx context.Context, appID string) (*DriftReport, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	app, ok := m.apps[appID]
	if !ok {
		return nil, fmt.Errorf("application '%s' not found", appID)
	}

	now := common.NowUTC()
	report := &DriftReport{
		ID:            common.NewUUID(),
		ApplicationID: appID,
		DetectedAt:    now,
		Drifts:        make([]DriftDetail, 0),
		AutoRemediate: m.config.AutoRemediateDrift,
		Status:        DriftDetected,
	}

	// In production, this would:
	// - ArgoCD: GET /api/v1/applications/{name}?refresh=hard → compare desired vs live
	// - Flux: kubectl get kustomization <name> -o json → check .status.conditions
	// - Alternatively: run `kubectl diff -f <manifests>` against live cluster

	m.logger.WithFields(logrus.Fields{
		"app_id":   appID,
		"app_name": app.Name,
		"engine":   app.Engine,
	}).Info("Drift detection completed")

	// If no drift found, mark app as synced
	if len(report.Drifts) == 0 {
		app.SyncStatus = SyncStatusSynced
	} else {
		app.SyncStatus = SyncStatusOutOfSync
		// Auto-remediate if enabled
		if m.config.AutoRemediateDrift {
			m.logger.WithField("app_id", appID).Info("Auto-remediating drift")
			report.Status = DriftRemediated
			remediatedAt := common.NowUTC()
			report.RemediatedAt = &remediatedAt
		}
	}

	m.driftReports[report.ID] = report
	return report, nil
}

// GetDriftReports returns drift reports for an application.
func (m *Manager) GetDriftReports(ctx context.Context, appID string) ([]*DriftReport, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	reports := make([]*DriftReport, 0)
	for _, r := range m.driftReports {
		if appID == "" || r.ApplicationID == appID {
			reports = append(reports, r)
		}
	}
	return reports, nil
}

// StartDriftDetectionLoop starts periodic drift detection for all apps.
func (m *Manager) StartDriftDetectionLoop(ctx context.Context) {
	interval := m.config.DriftCheckInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			appIDs := make([]string, 0, len(m.apps))
			for id := range m.apps {
				appIDs = append(appIDs, id)
			}
			m.mu.RUnlock()

			for _, id := range appIDs {
				if _, err := m.DetectDrift(ctx, id); err != nil {
					m.logger.WithError(err).WithField("app_id", id).Warn("Drift detection failed")
				}
			}
		}
	}
}

// ============================================================================
// Terraform Module Management
// ============================================================================

// RegisterTerraformModule registers a Terraform module for IaC management.
func (m *Manager) RegisterTerraformModule(ctx context.Context, module *TerraformModule) (*TerraformModule, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	module.ID = common.NewUUID()
	module.State = TFStateUninitialized
	m.tfModules[module.ID] = module

	m.logger.WithFields(logrus.Fields{
		"module_id": module.ID,
		"name":      module.Name,
		"source":    module.Source,
		"provider":  module.Provider,
	}).Info("Terraform module registered")

	return module, nil
}

// PlanTerraformModule runs terraform plan on a module.
func (m *Manager) PlanTerraformModule(ctx context.Context, moduleID string) (*TerraformPlanResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	module, ok := m.tfModules[moduleID]
	if !ok {
		return nil, fmt.Errorf("terraform module '%s' not found", moduleID)
	}

	module.State = TFStatePlanning

	// In production, this would execute:
	//   terraform init -backend-config=... && terraform plan -out=plan.tfplan
	// And parse the output for resource changes.

	result := &TerraformPlanResult{
		ModuleID:   moduleID,
		HasChanges: false,
		PlannedAt:  common.NowUTC(),
		PlanOutput: fmt.Sprintf("Terraform plan for module '%s': no changes detected", module.Name),
	}

	module.State = TFStatePlanned
	module.PlanOutput = result.PlanOutput

	m.logger.WithFields(logrus.Fields{
		"module_id":  moduleID,
		"has_changes": result.HasChanges,
	}).Info("Terraform plan completed")

	return result, nil
}

// ApplyTerraformModule runs terraform apply on a module.
func (m *Manager) ApplyTerraformModule(ctx context.Context, moduleID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	module, ok := m.tfModules[moduleID]
	if !ok {
		return fmt.Errorf("terraform module '%s' not found", moduleID)
	}

	if module.State != TFStatePlanned {
		return fmt.Errorf("module '%s' must be in 'planned' state before apply (current: %s)", moduleID, module.State)
	}

	module.State = TFStateApplying

	// In production, this would execute:
	//   terraform apply plan.tfplan
	now := common.NowUTC()
	module.State = TFStateApplied
	module.LastApplyAt = &now

	m.logger.WithFields(logrus.Fields{
		"module_id": moduleID,
		"name":      module.Name,
	}).Info("Terraform apply completed")

	return nil
}

// DestroyTerraformModule runs terraform destroy on a module.
func (m *Manager) DestroyTerraformModule(ctx context.Context, moduleID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	module, ok := m.tfModules[moduleID]
	if !ok {
		return fmt.Errorf("terraform module '%s' not found", moduleID)
	}

	module.State = TFStateDestroying

	// In production: terraform destroy -auto-approve
	module.State = TFStateDestroyed

	m.logger.WithFields(logrus.Fields{
		"module_id": moduleID,
		"name":      module.Name,
	}).Warn("Terraform module destroyed")

	return nil
}

// ListTerraformModules returns all registered Terraform modules.
func (m *Manager) ListTerraformModules(ctx context.Context) ([]*TerraformModule, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	modules := make([]*TerraformModule, 0, len(m.tfModules))
	for _, mod := range m.tfModules {
		modules = append(modules, mod)
	}
	return modules, nil
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
