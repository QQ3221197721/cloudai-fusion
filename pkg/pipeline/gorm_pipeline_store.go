// Package pipeline provides GORM-based database persistence for the data pipeline system.
package pipeline

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// PipelineStoreImpl implements the PipelineStore interface using GORM.
type PipelineStoreImpl struct {
	db *gorm.DB
}

// RunStoreImpl implements the RunStore interface using GORM.
type RunStoreImpl struct {
	db *gorm.DB
}

// NewPipelineStore creates a new PipelineStore instance.
func NewPipelineStore(db *gorm.DB) *PipelineStoreImpl {
	return &PipelineStoreImpl{db: db}
}

// NewRunStore creates a new RunStore instance.
func NewRunStore(db *gorm.DB) *RunStoreImpl {
	return &RunStoreImpl{db: db}
}

// ============================================================================
// Pipeline CRUD Operations
// ============================================================================

// CreatePipeline creates a new pipeline record in the database.
func (s *PipelineStoreImpl) CreatePipeline(ctx context.Context, p *Pipeline) error {
	return s.db.WithContext(ctx).Create(p).Error
}

// GetPipelineByID retrieves a pipeline by its unique identifier.
func (s *PipelineStoreImpl) GetPipelineByID(ctx context.Context, id string) (*Pipeline, error) {
	var p Pipeline
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPipelines lists all pipelines with optional filtering and pagination.
func (s *PipelineStoreImpl) ListPipelines(ctx context.Context, filter *pipeline.PipelineFilter) ([]*pipeline.Pipeline, int64, error) {
	var pipelines []*pipeline.Pipeline
	query := s.db.WithContext(ctx)

	if filter != nil {
		if len(filter.Status) > 0 {
			query = query.Where("status IN ?", filter.Status)
		}
		if filter.Type != "" {
			// Query source type from JSON column
			query = query.Where("source->>'type' = ?", filter.Type)
		}
		if filter.Name != "" {
			query = query.Where("name ILIKE ?", "%"+filter.Name+"%")
		}
		if filter.HasSchedule {
			// Check if schedule is not null
			query = query.Where("schedule IS NOT NULL")
		}
	}

	// Count total
	var count int64
	if err := query.Model(&pipeline.Pipeline{}).Count(&count).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	if filter.SortBy != "" {
		field := filter.SortBy
		if contains([]string{"created_at", "updated_at", "name"}, field) {
			query = query.Order(field + " " + filter.SortOrder)
		}
	}

	// Apply pagination
	if filter.Limit > 0 {
		offset := filter.Offset * filter.Limit
		query = query.Limit(filter.Limit).Offset(offset)
	}

	err := query.Find(&pipelines).Error
	return pipelines, count, err
}

// UpdatePipeline updates an existing pipeline's fields.
func (s *PipelineStoreImpl) UpdatePipeline(ctx context.Context, p *pipeline.Pipeline) error {
	p.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(p).Error
}

// DeletePipeline deletes a pipeline permanently.
func (s *PipelineStoreImpl) DeletePipeline(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&pipeline.Pipeline{}, id).Error
}

// ArchivePipeline sets the pipeline status to archived.
func (s *PipelineStoreImpl) ArchivePipeline(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Model(&pipeline.Pipeline{}).
		Where("id = ?", id).
		Update("status", pipeline.PipelineArchived).
		Error
}

// ActivatePipeline sets the pipeline status back to active.
func (s *PipelineStoreImpl) ActivatePipeline(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Model(&pipeline.Pipeline{}).
		Where("id = ?", id).
		Update("status", pipeline.PipelineActive).
		Error
}

// CountPipelines returns the total number of pipelines.
func (s *PipelineStoreImpl) CountPipelines(ctx context.Context) int64 {
	var count int64
	s.db.WithContext(ctx).Model(&pipeline.Pipeline{}).Count(&count)
	return count
}

// HealthCheck performs a health check on the pipeline store.
func (s *PipelineStoreImpl) HealthCheck(ctx context.Context) error {
	return s.db.WithContext(ctx).Exec("SELECT 1").Error
}

// ============================================================================
// Run CRUD Operations  
// ============================================================================

// CreateRun creates a new pipeline run record.
func (s *RunStoreImpl) CreateRun(ctx context.Context, r *PipelineRun) error {
	return s.db.WithContext(ctx).Create(r).Error
}

// GetRunByID retrieves a run by its ID.
func (s *RunStoreImpl) GetRunByID(ctx context.Context, id string) (*PipelineRun, error) {
	var r PipelineRun
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&r).Error
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRuns lists runs for a specific pipeline with optional filters.
func (s *RunStoreImpl) ListRuns(ctx context.Context, pipelineID string, filter *RunFilter) ([]*PipelineRun, int64, error) {
	var runs []*PipelineRun
	query := s.db.WithContext(ctx).Where("pipeline_id = ?", pipelineID)

	if filter != nil {
		if len(filter.Status) > 0 {
			query = query.Where("status IN ?", filter.Status)
		}
		if filter.StartDate != nil {
			query = query.Where("start_time >= ?", *filter.StartDate)
		}
		if filter.EndDate != nil {
			query = query.Where("start_time <= ?", *filter.EndDate)
		}
		if filter.MinDuration > 0 {
			query = query.Where("duration_ms >= ?", filter.MinDuration)
		}
		if filter.MaxDuration > 0 {
			query = query.Where("duration_ms <= ? OR duration_ms IS NULL", filter.MaxDuration)
		}
	}

	// Count total
	var count int64
	if err := query.Model(&pipeline.PipelineRun{}).Count(&count).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	if filter.SortBy != "" {
		field := filter.SortBy
		if contains([]string{"start_time", "status", "records_processed"}, field) {
			query = query.Order(field + " " + filter.SortOrder)
		}
	}

	// Apply pagination
	if filter.Limit > 0 {
		offset := filter.Offset * filter.Limit
		query = query.Limit(filter.Limit).Offset(offset)
	}

	err := query.Find(&runs).Error
	return runs, count, err
}

// UpdateRunStatus updates the status of a pipeline run.
func (s *RunStoreImpl) UpdateRunStatus(ctx context.Context, id string, status pipeline.RunStatus, errorMsg string) error {
	updateMap := map[string]interface{}{
		"status": status,
	}
	if errorMsg != "" {
		updateMap["error_message"] = errorMsg
	}
	
	endTime := time.Now()
	updateMap["end_time"] = endTime
	
	query := s.db.WithContext(ctx).Model(&pipeline.PipelineRun{}).Where("id = ?", id)
	result := query.Update(updateMap)
	
	if result.Error == nil && status == pipeline.RunSuccess || status == pipeline.RunFailure {
		// Calculate duration if end_time changed
		var run pipeline.PipelineRun
		if err := s.db.WithContext(ctx).First(&run, id).Error; err == nil {
			duration := endTime.Sub(run.StartTime).Milliseconds()
			query.Update("duration_ms", duration)
		}
	}
	
	return result.Error
}

// UpdateRunStages updates the stages of a pipeline run.
func (s *RunStoreImpl) UpdateRunStages(ctx context.Context, runID string, stages []pipeline.RunStage) error {
	return s.db.WithContext(ctx).Model(&pipeline.PipelineRun{}).
		Where("id = ?", runID).
		Update("stages", stages).
		Error
}

// UpdateRunMetrics updates execution metrics and statistics for a run.
func (s *RunStoreImpl) UpdateRunMetrics(ctx context.Context, runID string, metrics *pipeline.ResourceMetrics, stats *pipeline.RunStatistics) error {
	updateMap := make(map[string]interface{})
	
	if metrics != nil {
		updateMap["resource_usage"] = metrics
	}
	if stats != nil {
		updateMap["records_processed"] = stats.TotalRecords
		if stats.Warnings > 0 {
			updateMap["output_stats"] = stats
		}
	}
	
	return s.db.WithContext(ctx).Model(&pipeline.PipelineRun{}).
		Where("id = ?", runID).
		UpdateColumns(updateMap).
		Error
}

// CancelRun cancels a pending or running run.
func (s *RunStoreImpl) CancelRun(ctx context.Context, id string) error {
	// Only cancel if currently pending or running
	return s.db.WithContext(ctx).Model(&pipeline.PipelineRun{}).
		Where("status IN ?", []pipeline.RunStatus{pipeline.RunPending, pipeline.RunRunning}).
		Where("id = ?", id).
		Update("status", pipeline.RunCancelled).
		Error
}

// GetFailedRuns retrieves all failed runs in the last N hours.
func (s *RunStoreImpl) GetFailedRuns(ctx context.Context, hoursAgo int) ([]*PipelineRun, error) {
	cutoff := time.Now().Add(-time.Duration(hoursAgo) * time.Hour)
	var runs []*PipelineRun
	
	err := s.db.WithContext(ctx).
		Where("status = ? AND start_time >= ?", RunFailure, cutoff).
		Order("start_time DESC").
		Find(&runs).Error
	
	return runs, err
}

// GetRecentRuns retrieves the most recent runs across all pipelines.
func (s *RunStoreImpl) GetRecentRuns(ctx context.Context, limit int) ([]*PipelineRun, error) {
	if limit <= 0 {
		limit = 50
	}
	
	var runs []*PipelineRun
	err := s.db.WithContext(ctx).
		Order("start_time DESC").
		Limit(limit).
		Find(&runs).Error
	
	return runs, err
}

// Helper function to check if string is in slice.
func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}
