// Package pipeline implements the core business logic for data pipeline management,
// including run execution, scheduling, and monitoring.
package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/sirupsen/logrus"
)

// Config holds pipeline system configuration.
type Config struct {
	MaxConcurrentRuns int           // maximum parallel pipeline runs
	HeartbeatInterval time.Duration // interval for sending heartbeat during runs
	LogLevel          string        // logging level
}

// Manager orchestrates pipeline creation, execution, and monitoring.
type Manager struct {
	pipelineStore common.PipelineStoreInterface
	runStore      common.PipelineStoreInterface // can be the same interface
	config        Config
	logger        *logrus.Logger
	activeRuns    map[string]*RunContext
	mu            sync.RWMutex
	executor      RunExecutor
	triggerManager *TriggerManager
}

// RunContext holds runtime state for an active pipeline execution.
type RunContext struct {
	RunID       string
	PipelineID  string
	CancelFunc  context.CancelFunc
	Context     context.Context
	StartTime   time.Time
	Stages      []RunStage
	Metrics     ResourceMetrics
	RecordsDone int64
}

// RunExecutor defines the interface for executing pipeline stages.
type RunExecutor interface {
	Execute(ctx context.Context, p *Pipeline, run *PipelineRun) error
	ReadSource(ctx context.Context, source DataSource, limit int) ([]map[string]interface{}, error)
	WriteTarget(ctx context.Context, target DataSink, records []map[string]interface{}) error
	ApplyTransformations(records []map[string]interface{}, rules []TransformationRule) ([]map[string]interface{}, error)
}

// TriggerManager handles scheduled pipeline triggers.
type TriggerManager struct {
	scheduledJobs map[string]time.Time
	mu            sync.RWMutex
}

// New creates a new Pipeline Manager with the given pipeline store and configuration.
func New(pipelineStore common.PipelineStoreInterface, cfg Config) *Manager {
	if cfg.MaxConcurrentRuns == 0 {
		cfg.MaxConcurrentRuns = 5
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}

	return &Manager{
		pipelineStore: pipelineStore,
		runStore:      pipelineStore, // For now, same interface; can be different implementation later
		config:        cfg,
		logger:        logrus.StandardLogger(),
		activeRuns:    make(map[string]*RunContext),
		triggerManager: &TriggerManager{
			scheduledJobs: make(map[string]time.Time),
		},
	}
}

// Start begins all background processes including scheduled triggers.
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting pipeline manager...")
	
	// Load all active pipelines and schedule them
	activePipelines, _, err := m.pipelineStore.ListPipelines(ctx, &PipelineFilter{
		Status: []PipelineStatus{PipelineActive},
	})
	if err != nil {
		return fmt.Errorf("failed to list active pipelines: %w", err)
	}

	// Schedule each pipeline
	for _, p := range activePipelines {
		if p.Schedule != nil {
			if err := m.schedulePipeline(p); err != nil {
				m.logger.WithError(err).WithField("pipeline_id", p.ID).Warn("Failed to schedule pipeline")
			}
		}
	}

	m.logger.Info("Pipeline manager started successfully")
	return nil
}

// Stop stops all background processes and cancels active runs.
func (m *Manager) Stop() {
	m.logger.Info("Stopping pipeline manager...")
	
	// Cancel all active runs
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for _, ctx := range m.activeRuns {
		if ctx.CancelFunc != nil {
			ctx.CancelFunc()
		}
	}
	m.activeRuns = make(map[string]*RunContext)
}

// CreatePipeline creates a new pipeline and saves it to the database.
func (m *Manager) CreatePipeline(ctx context.Context, p *Pipeline) error {
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	
	// Validate pipeline
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid pipeline: %w", err)
	}
	
	return m.pipelineStore.CreatePipeline(ctx, p)
}

// GetPipeline retrieves a pipeline by ID.
func (m *Manager) GetPipeline(ctx context.Context, id string) (*Pipeline, error) {
	return m.pipelineStore.GetPipelineByID(ctx, id)
}

// ListPipelines lists pipelines with optional filtering.
func (m *Manager) ListPipelines(ctx context.Context, filter *PipelineFilter) ([]*Pipeline, int64, error) {
	return m.pipelineStore.ListPipelines(ctx, filter)
}

// UpdatePipeline updates an existing pipeline.
func (m *Manager) UpdatePipeline(ctx context.Context, p *Pipeline) error {
	p.UpdatedAt = time.Now()
	if err := p.Validate(); err != nil {
		return fmt.Errorf("invalid pipeline: %w", err)
	}
	return m.pipelineStore.UpdatePipeline(ctx, p)
}

// DeletePipeline deletes a pipeline.
func (m *Manager) DeletePipeline(ctx context.Context, id string) error {
	return m.pipelineStore.DeletePipeline(ctx, id)
}

// ArchivePipeline archives a pipeline.
func (m *Manager) ArchivePipeline(ctx context.Context, id string) error {
	return m.pipelineStore.ArchivePipeline(ctx, id)
}

// ActivatePipeline activates an archived pipeline.
func (m *Manager) ActivatePipeline(ctx context.Context, id string) error {
	return m.pipelineStore.ActivatePipeline(ctx, id)
}

// TriggerRun manually triggers a pipeline execution.
func (m *Manager) TriggerRun(ctx context.Context, pipelineID, triggerBy, triggerType string) (*PipelineRun, error) {
	p, err := m.pipelineStore.GetPipelineByID(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline: %w", err)
	}

	return m.executePipeline(ctx, p, triggerBy, triggerType)
}

// executePipeline is the internal method that actually executes a pipeline.
func (m *Manager) executePipeline(ctx context.Context, p *Pipeline, triggerBy, triggerType string) (*PipelineRun, error) {
	// Check concurrency limit
	m.mu.RLock()
	activeCount := len(m.activeRuns)
	m.mu.RUnlock()
	
	if activeCount >= m.config.MaxConcurrentRuns {
		return nil, fmt.Errorf("maximum concurrent runs (%d) reached", m.config.MaxConcurrentRuns)
	}

	// Create run record
	run := &PipelineRun{
		ID:           GenerateRunID(),
		PipelineID:   p.ID,
		Status:       RunPending,
		StartTime:    time.Now(),
		TriggeredBy:  triggerBy,
		TriggerType:  triggerType,
		Stages:       m.buildExecutionStages(p),
		InputStats:   RunStatistics{},
		OutputStats:  RunStatistics{},
		ResourceUsage: ResourceMetrics{},
	}

	// Create context with cancellation
	runCtx, cancel := context.WithCancel(ctx)
	
	// Save initial run
	if err := m.runStore.CreateRun(runCtx, run); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create run record: %w", err)
	}

	// Store active run context
	m.mu.Lock()
	m.activeRuns[run.ID] = &RunContext{
		RunID:      run.ID,
		PipelineID: p.ID,
		CancelFunc: cancel,
		Context:    runCtx,
		StartTime:  run.StartTime,
	}
	m.mu.Unlock()

	// Execute pipeline in goroutine
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.activeRuns, run.ID)
			m.mu.Unlock()
		}()

		err := m.runPipeline(ctx, p, run)
		
		if err != nil {
			m.runStore.UpdateRunStatus(ctx, run.ID, RunFailure, err.Error())
		} else {
			m.runStore.UpdateRunStatus(ctx, run.ID, RunSuccess, "")
		}
	}()

	return run, nil
}

// runPipeline is the actual execution logic.
func (m *Manager) runPipeline(ctx context.Context, p *Pipeline, run *PipelineRun) error {
	// Update status to running
	m.runStore.UpdateRunStatus(ctx, run.ID, RunRunning, "")

	// Initialize metrics
	var totalBytes int64
	var bytesRead int64
	
	// Read from source
	sourceRecords, err := m.readFromSource(ctx, p.Source)
	if err != nil {
		return fmt.Errorf("failed to read from source: %w", err)
	}
	
	inputStats := RunStatistics{
		TotalRecords: int64(len(sourceRecords)),
		Errors:       0,
	}

	// Apply transformations
	transformedRecords := sourceRecords
	if len(p.Transformations) > 0 {
		transformedRecords, err = m.applyTransformations(transformedRecords, p.Transformations)
		if err != nil {
			return fmt.Errorf("transformation failed: %w", err)
		}
	}

	outputStats := RunStatistics{
		TotalRecords: int64(len(transformedRecords)),
		Errors:       0,
	}

	// Write to target
	if err := m.writeToTarget(ctx, p.Target, transformedRecords); err != nil {
		return fmt.Errorf("failed to write to target: %w", err)
	}

	// Update final run metrics
	durationMs := time.Since(run.StartTime).Milliseconds()
	run.DurationMs = durationMs
	run.RecordsProcessed = outputStats.TotalRecords
	run.EndTime = &[]time.Time{time.Now()}[0]
	
	m.runStore.UpdateRunMetrics(ctx, run.ID, &ResourceMetrics{
		CPUPercent: 15.5, // Placeholder - would be measured
		MemoryBytes: int64(len(transformedRecords) * 1024),
	}, &outputStats)

	return nil
}

// readFromSource reads data from the configured source.
func (m *Manager) readFromSource(ctx context.Context, source DataSource) ([]map[string]interface{}, error) {
	// In production, this would connect to the actual data source
	// For now, return placeholder data
	return []map[string]interface{}{
		{"id": "1", "name": "test_record_1"},
		{"id": "2", "name": "test_record_2"},
	}, nil
}

// writeToTarget writes data to the configured sink.
func (m *Manager) writeToTarget(ctx context.Context, target DataSink, records []map[string]interface{}) error {
	// In production, this would insert into the actual target
	// For now, just log
	m.logger.WithFields(logrus.Fields{
		"target_type": target.Type,
		"records":     len(records),
	}).Info("Writing data to target")
	return nil
}

// applyTransformations applies transformation rules to records.
func (m *Manager) applyTransformations(records []map[string]interface{}, rules []TransformationRule) ([]map[string]interface{}, error) {
	result := records
	for i, rule := range rules {
		if !rule.Enabled {
			continue
		}
		
		var err error
		switch rule.Type {
		case TransformFilter:
			result = m.filterRecords(result, rule.Config.Filter)
		case TransformProject:
			result = m.projectRecords(result, rule.Config.Project)
		case TransformDerive:
			result = m.deriveRecords(result, rule.Config.Derive)
		default:
			m.logger.WithField("rule_id", rule.ID).Debug("Unsupported transformation type")
		}
		
		if err != nil {
			return nil, fmt.Errorf("rule %d (%s) failed: %w", i, rule.Name, err)
		}
	}
	return result, nil
}

// filterRecords filters records based on filter config.
func (m *Manager) filterRecords(records []map[string]interface{}, cfg *FilterConfig) []map[string]interface{} {
	if cfg == nil || cfg.Expression == "" {
		return records
	}
	// Placeholder - in production would parse and evaluate expression
	return records
}

// projectRecords projects only the specified columns from each record.
func (m *Manager) projectRecords(records []map[string]interface{}, cfg *ProjectConfig) []map[string]interface{} {
	if cfg == nil {
		return records
	}
	
	result := make([]map[string]interface{}, 0, len(records))
	for _, record := range records {
		newRecord := make(map[string]interface{})
		for _, col := range cfg.Columns {
			if val, ok := record[col]; ok {
				if rename, found := cfg.Rename[col]; found {
					newRecord[rename] = val
				} else {
					newRecord[col] = val
				}
			}
		}
		result = append(result, newRecord)
	}
	return result
}

// deriveRecords adds derived columns based on expressions.
func (m *Manager) deriveRecords(records []map[string]interface{}, cfg *DeriveConfig) []map[string]interface{} {
	if cfg == nil || cfg.Column == "" || cfg.Expr == "" {
		return records
	}
	
	result := make([]map[string]interface{}, len(records))
	copy(result, records)
	
	// Placeholder - in production would evaluate expression against each record
	for i := range result {
		result[i][cfg.Column] = "derived_value"
	}
	
	return result
}


// buildExecutionStages constructs the stage list for a pipeline run.
func (m *Manager) buildExecutionStages(p *Pipeline) []RunStage {
	stages := []RunStage{
		{Name: "read_source", StageOrder: 1},
		{Name: "transform", StageOrder: 2},
		{Name: "write_target", StageOrder: 3},
	}
	return stages
}

// GenerateRunID generates a unique run ID.
func GenerateRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UnixNano())
}

// HealthCheck performs a health check on the pipeline manager.
func (m *Manager) HealthCheck(ctx context.Context) error {
	if err := m.pipelineStore.HealthCheck(ctx); err != nil {
		return fmt.Errorf("pipeline store unhealthy: %w", err)
	}
	return nil
}
