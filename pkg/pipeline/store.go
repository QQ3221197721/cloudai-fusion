// Package pipeline defines the data store interface for pipelines.
package pipeline

import "context"

// PipelineStore defines the CRUD operations for Pipeline management.
type PipelineStore interface {
	// CreatePipeline creates a new pipeline.
	CreatePipeline(ctx context.Context, pipeline *Pipeline) error
	
	// GetPipelineByID retrieves a pipeline by its ID.
	GetPipelineByID(ctx context.Context, id string) (*Pipeline, error)
	
	// ListPipelines lists all pipelines with optional filters.
	ListPipelines(ctx context.Context, filter *PipelineFilter) ([]*Pipeline, int64, error)
	
	// UpdatePipeline updates an existing pipeline.
	UpdatePipeline(ctx context.Context, pipeline *Pipeline) error
	
	// DeletePipeline deletes a pipeline by ID.
	DeletePipeline(ctx context.Context, id string) error
	
	// ArchivePipeline archives a pipeline (soft delete).
	ArchivePipeline(ctx context.Context, id string) error
	
	// ActivatePipeline activates an archived pipeline.
	ActivatePipeline(ctx context.Context, id string) error
	
	// CountPipelines returns the total number of pipelines.
	CountPipelines(ctx context.Context) int64
	
	// HealthCheck performs a health check on the store.
	HealthCheck(ctx context.Context) error
}

// PipelineFilter defines query parameters for listing pipelines.
type PipelineFilter struct {
	Status      []PipelineStatus // filter by status (active, inactive, archived)
	HasSchedule bool             // filter pipelines with schedule
	Type        string           // filter by source type (postgres, mysql, etc.)
	Name        string           // search by name
	SortBy      string           // sort field: name, created_at, updated_at
	SortOrder   string           // asc or desc
	Limit       int              // max results per page
	Offset      int              // pagination offset
}

// RunStore defines operations for pipeline run history.
type RunStore interface {
	// CreateRun creates a new pipeline run record.
	CreateRun(ctx context.Context, run *PipelineRun) error
	
	// GetRunByID retrieves a run by its ID.
	GetRunByID(ctx context.Context, id string) (*PipelineRun, error)
	
	// ListRuns lists runs for a specific pipeline.
	ListRuns(ctx context.Context, pipelineID string, filter *RunFilter) ([]*PipelineRun, int64, error)
	
	// UpdateRunStatus updates the status of a run.
	UpdateRunStatus(ctx context.Context, id string, status RunStatus, errorMsg string) error
	
	// UpdateRunStages updates the stages of a run.
	UpdateRunStages(ctx context.Context, runID string, stages []RunStage) error
	
	// UpdateRunMetrics updates execution metrics for a run.
	UpdateRunMetrics(ctx context.Context, runID string, metrics *ResourceMetrics, stats *RunStatistics) error
	
	// CancelRun cancels a pending or running run.
	CancelRun(ctx context.Context, id string) error
	
	// GetFailedRuns retrieves all failed runs in a time range.
	GetFailedRuns(ctx context.Context, hoursAgo int) ([]*PipelineRun, error)
	
	// GetRecentRuns retrieves the most recent runs across all pipelines.
	GetRecentRuns(ctx context.Context, limit int) ([]*PipelineRun, error)
}

// RunFilter defines query parameters for listing runs.
type RunFilter struct {
	PipelineIDs []string   // filter by multiple pipeline IDs
	Status      []RunStatus // filter by status
	StartDate   *time.Time // start date filter
	EndDate     *time.Time // end date filter
	MinDuration int64      // minimum duration in milliseconds
	MaxDuration int64      // maximum duration in milliseconds
	SortBy      string     // sort field: start_time, status, records_processed
	SortOrder   string     // asc or desc
	Limit       int        // max results per page
	Offset      int        // pagination offset
}
