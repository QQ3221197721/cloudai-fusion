// Package common provides shared interfaces and types for CloudAI Fusion.
// These interfaces break circular dependencies between packages by providing
// abstraction layers that concrete implementations can satisfy.
package common

import (
	"context"
	"time"
)

// =============================================================================
// Store Abstraction - Breaks the cycle pkg/scheduler -> pkg/store
// =============================================================================

// StoreInterface abstracts persistence operations used across modules.
type StoreInterface interface {
	Save(ctx context.Context, key string, data interface{}) error
	Load(ctx context.Context, key string) (interface{}, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
	BeginTransaction(ctx context.Context) (TxInterface, error)
	UpdateWorkloadStatus(workloadID, oldStatus, newStatus, reason string) error
	SaveSchedulerSnapshot(data string) error
	LoadSchedulerSnapshot() (string, error)
}

// TxInterface represents a database transaction.
type TxInterface interface {
	Commit() error
	Rollback() error
	Save(key string, data interface{}) error
	Load(key string) (interface{}, error)
}

// =============================================================================
// Pipeline Abstraction - Breaks the cycle pkg/pipeline -> pkg/scheduler  
// =============================================================================

// PipelineStatus defines valid pipeline states.
type PipelineStatus string

const (
	PipelineDraft       PipelineStatus = "draft"
	PipelineActive      PipelineStatus = "active"
	PipelineInactive    PipelineStatus = "inactive"
	PipelineCompleted   PipelineStatus = "completed"
	PipelineFailed      PipelineStatus = "failed"
	PipelineCancelled   PipelineStatus = "cancelled"
)

// PipelineRunStatus defines run execution states.
type PipelineRunStatus string

const (
	RunPending   PipelineRunStatus = "pending"
	RunRunning   PipelineRunStatus = "running"
	RunSucceeded PipelineRunStatus = "succeeded"
	RunFailed    PipelineRunStatus = "failed"
	RunSkipped   PipelineRunStatus = "skipped"
)

// DataSource represents a data source configuration.
type DataSource struct {
	Type        string            `json:"type"`
	Endpoint    string            `json:"endpoint"`
	Credentials map[string]string `json:"credentials,omitempty"`
	Query       string            `json:"query,omitempty"`
}

// DataSink represents a data sink configuration.
type DataSink struct {
	Type        string            `json:"type"`
	Endpoint    string            `json:"endpoint"`
	Credentials map[string]string `json:"credentials,omitempty"`
	Table       string            `json:"table,omitempty"`
}

// TransformationRule defines a data transformation rule.
type TransformationRule struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// CronExpression defines a scheduled cron expression.
type CronExpression struct {
	Expression string
}

// PipelineConfig holds complete pipeline specification.
type PipelineConfig struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Source              DataSource        `json:"source"`
	Target              DataSink          `json:"target"`
	Transformations     []TransformationRule `json:"transformations,omitempty"`
	Schedule            *CronExpression   `json:"schedule,omitempty"`
	Status              PipelineStatus    `json:"status"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at,omitempty"`
}

// PipelineFilter defines query filters for listing pipelines.
type PipelineFilter struct {
	Status []PipelineStatus `json:"status,omitempty"`
	Creator string           `json:"creator,omitempty"`
	FromTime time.Time        `json:"from_time,omitempty"`
	ToTime   time.Time        `json:"to_time,omitempty"`
}

// PipelineRun captures runtime execution information.
type PipelineRun struct {
	ID            string             `json:"id"`
	PipelineID    string             `json:"pipeline_id"`
	StartTime     time.Time          `json:"start_time"`
	EndTime       time.Time          `json:"end_time,omitempty"`
	Status        PipelineRunStatus  `json:"status"`
	RecordsIn     int64              `json:"records_in,omitempty"`
	RecordsOut    int64              `json:"records_out,omitempty"`
	ErrorMessage  string             `json:"error_message,omitempty"`
}

// PipelineFull contains complete pipeline information with metadata.
type PipelineFull struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Config          PipelineConfig `json:"config"`
	LastRun         *PipelineRun  `json:"last_run,omitempty"`
	SuccessRate     float64       `json:"success_rate,omitempty"`
	AvgDurationSecs float64       `json:"avg_duration_secs,omitempty"`
}

// PipelineStoreInterface manages pipeline lifecycle and runs.
type PipelineStoreInterface interface {
	CreatePipeline(ctx context.Context, config PipelineConfig) (*PipelineFull, error)
	GetPipeline(ctx context.Context, id string) (*PipelineFull, error)
	UpdatePipeline(ctx context.Context, id string, updates map[string]interface{}) error
	DeletePipeline(ctx context.Context, id string) error
	ListPipelines(ctx context.Context, filters PipelineFilter) ([]*PipelineFull, error)
	TriggerRun(ctx context.Context, pipelineID string) (*PipelineRun, error)
	GetRunStatus(ctx context.Context, runID string) (PipelineRunStatus, error)
	CancelRun(ctx context.Context, runID string) error
	StartRun(ctx context.Context, run *PipelineRun) error
	CompleteRun(ctx context.Context, run *PipelineRun, success bool, message string) error
	ListRuns(ctx context.Context, pipelineID string, limit int) ([]*PipelineRun, error)
}

// =============================================================================
// Scheduler Abstraction - Used by pkg/pipeline for cost estimation
// =============================================================================

// JobSpec defines a scheduled job specification.
type JobSpec struct {
	Name         string            `json:"name"`
	ResourceType string            `json:"resource_type"`
	Quantity     int               `json:"quantity"`
	Priority     int               `json:"priority"`
	DurationHours float64          `json:"duration_hours"`
	Budget       float64           `json:"budget"`
	GPUCount     int               `json:"gpu_count"`
	GPUType      string            `json:"gpu_type"`
}

// CostEstimate captures cost estimation results.
type CostEstimate struct {
	NodeID         string                 `json:"node_id"`
	TotalCost      float64                `json:"total_cost"` // in cents
	BudgetExceeded bool                   `json:"budget_exceeded"`
	Message        string                 `json:"message,omitempty"`
	Breakdown      []CostBreakdownItem    `json:"breakdown"`
}

// CostBreakdownItem details a cost component.
type CostBreakdownItem struct {
	Component string    `json:"component"`
	Amount    float64   `json:"amount"` // in cents
	Duration  time.Duration `json:"duration"`
}

// CostEstimator is a simplified interface for cost estimation only.
type CostEstimator interface {
	Estimate(ctx context.Context, job JobSpec, node string) (*CostEstimate, error)
}
