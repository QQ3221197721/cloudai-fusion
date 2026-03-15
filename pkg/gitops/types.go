// Package gitops provides GitOps workflow management for CloudAI Fusion,
// including ArgoCD/Flux integration, environment promotion automation,
// configuration drift detection, and Infrastructure as Code (Terraform) modules.
package gitops

import (
	"time"
)

// ============================================================================
// GitOps Engine Types
// ============================================================================

// EngineType identifies the GitOps engine backend.
type EngineType string

const (
	EngineArgoCD EngineType = "argocd"
	EngineFlux   EngineType = "flux"
)

// SyncStatus represents the synchronization status of a GitOps application.
type SyncStatus string

const (
	SyncStatusSynced     SyncStatus = "synced"
	SyncStatusOutOfSync  SyncStatus = "out-of-sync"
	SyncStatusUnknown    SyncStatus = "unknown"
	SyncStatusProgressing SyncStatus = "progressing"
	SyncStatusDegraded   SyncStatus = "degraded"
)

// HealthStatus represents the health status of a GitOps application.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusMissing   HealthStatus = "missing"
	HealthStatusUnknown   HealthStatus = "unknown"
	HealthStatusSuspended HealthStatus = "suspended"
)

// ============================================================================
// Application
// ============================================================================

// Application represents a GitOps-managed application.
type Application struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Engine         EngineType        `json:"engine"`
	RepoURL        string            `json:"repo_url"`
	RepoPath       string            `json:"repo_path"`
	TargetRevision string            `json:"target_revision"`
	Namespace      string            `json:"namespace"`
	ClusterID      string            `json:"cluster_id"`
	Environment    string            `json:"environment"`
	SyncStatus     SyncStatus        `json:"sync_status"`
	HealthStatus   HealthStatus      `json:"health_status"`
	SyncPolicy     *SyncPolicy       `json:"sync_policy,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	LastSyncedAt   *time.Time        `json:"last_synced_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// SyncPolicy controls automatic synchronization behavior.
type SyncPolicy struct {
	AutoSync   bool `json:"auto_sync"`
	SelfHeal   bool `json:"self_heal"`
	Prune      bool `json:"prune"`
	RetryLimit int  `json:"retry_limit"`
}

// SyncResult captures the outcome of a sync operation.
type SyncResult struct {
	ApplicationID string     `json:"application_id"`
	Revision      string     `json:"revision"`
	Status        SyncStatus `json:"status"`
	Message       string     `json:"message,omitempty"`
	Resources     int        `json:"resources_synced"`
	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   time.Time  `json:"completed_at"`
}

// ============================================================================
// Environment Promotion
// ============================================================================

// PromotionStage represents an environment in the promotion pipeline.
type PromotionStage struct {
	Name        string            `json:"name"`
	Environment string            `json:"environment"`
	ClusterID   string            `json:"cluster_id"`
	Namespace   string            `json:"namespace"`
	Order       int               `json:"order"`
	Gates       []PromotionGate   `json:"gates,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// PromotionGate defines a gate that must pass before promotion proceeds.
type PromotionGate struct {
	Type      GateType `json:"type"`
	Name      string   `json:"name"`
	Required  bool     `json:"required"`
	Timeout   string   `json:"timeout,omitempty"`
	Config    map[string]string `json:"config,omitempty"`
}

// GateType defines the type of promotion gate.
type GateType string

const (
	GateManualApproval GateType = "manual-approval"
	GateTestSuite      GateType = "test-suite"
	GateCanaryAnalysis GateType = "canary-analysis"
	GateSLOValidation  GateType = "slo-validation"
	GateSecurityScan   GateType = "security-scan"
)

// PromotionPipeline defines a multi-stage promotion workflow.
type PromotionPipeline struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	AppID       string           `json:"app_id"`
	Stages      []PromotionStage `json:"stages"`
	Status      PromotionStatus  `json:"status"`
	CurrentStage int             `json:"current_stage"`
	Revision    string           `json:"revision"`
	StartedAt   time.Time        `json:"started_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
}

// PromotionStatus represents the status of a promotion pipeline.
type PromotionStatus string

const (
	PromotionPending    PromotionStatus = "pending"
	PromotionInProgress PromotionStatus = "in-progress"
	PromotionGated      PromotionStatus = "gated"
	PromotionCompleted  PromotionStatus = "completed"
	PromotionFailed     PromotionStatus = "failed"
	PromotionRolledBack PromotionStatus = "rolled-back"
)

// ============================================================================
// Configuration Drift Detection
// ============================================================================

// DriftReport captures detected configuration drift.
type DriftReport struct {
	ID            string        `json:"id"`
	ApplicationID string        `json:"application_id"`
	DetectedAt    time.Time     `json:"detected_at"`
	Drifts        []DriftDetail `json:"drifts"`
	AutoRemediate bool          `json:"auto_remediate"`
	RemediatedAt  *time.Time    `json:"remediated_at,omitempty"`
	Status        DriftStatus   `json:"status"`
}

// DriftDetail describes a single drift item.
type DriftDetail struct {
	ResourceKind string `json:"resource_kind"`
	ResourceName string `json:"resource_name"`
	Namespace    string `json:"namespace"`
	Field        string `json:"field"`
	Expected     string `json:"expected"`
	Actual       string `json:"actual"`
	Severity     string `json:"severity"`
}

// DriftStatus represents drift remediation status.
type DriftStatus string

const (
	DriftDetected   DriftStatus = "detected"
	DriftRemediated DriftStatus = "remediated"
	DriftAccepted   DriftStatus = "accepted"
	DriftIgnored    DriftStatus = "ignored"
)

// ============================================================================
// Terraform IaC
// ============================================================================

// TerraformModule represents a Terraform infrastructure module.
type TerraformModule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Source      string            `json:"source"`
	Version     string            `json:"version"`
	Provider    string            `json:"provider"`
	Variables   map[string]string `json:"variables,omitempty"`
	Outputs     map[string]string `json:"outputs,omitempty"`
	State       TerraformState    `json:"state"`
	LastApplyAt *time.Time        `json:"last_apply_at,omitempty"`
	PlanOutput  string            `json:"plan_output,omitempty"`
}

// TerraformState represents the lifecycle state of a Terraform module.
type TerraformState string

const (
	TFStateUninitialized TerraformState = "uninitialized"
	TFStatePlanning      TerraformState = "planning"
	TFStatePlanned       TerraformState = "planned"
	TFStateApplying      TerraformState = "applying"
	TFStateApplied       TerraformState = "applied"
	TFStateDestroying    TerraformState = "destroying"
	TFStateDestroyed     TerraformState = "destroyed"
	TFStateError         TerraformState = "error"
)

// TerraformPlanResult captures the result of a terraform plan.
type TerraformPlanResult struct {
	ModuleID   string    `json:"module_id"`
	HasChanges bool      `json:"has_changes"`
	Add        int       `json:"resources_to_add"`
	Change     int       `json:"resources_to_change"`
	Destroy    int       `json:"resources_to_destroy"`
	PlanOutput string    `json:"plan_output"`
	PlannedAt  time.Time `json:"planned_at"`
}
