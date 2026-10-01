// Package api provides HTTP handlers for M52 Hot-Swap State Migration Engine
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Hot-Swap State Migration Types & Interfaces
// ============================================================================

// MigrationPhase represents current state in migration lifecycle
type MigrationPhase string

const (
	PhasePreparing   MigrationPhase = "preparing"   // Initial setup
	PhaseSnapshot    MigrationPhase = "snapshot"    // Capture state
	PhaseLoadNew     MigrationPhase = "load_new"    // Load new version
	PhaseMigrate     MigrationPhase = "migrate"     // Transform state
	PhaseActivate    MigrationPhase = "activate"    // Swap instances
	PhaseCompleted   MigrationPhase = "completed"   // Success
	PhaseRollingBack MigrationPhase = "rolling_back" // Rollback needed
	PhaseRolledBack  MigrationPhase = "rolled_back"  // Rollback complete
	PhaseFailed      MigrationPhase = "failed"      // Fatal error
)

// MigrationStatus defines overall outcome
type MigrationStatus string

const (
	StatusPreparing    MigrationStatus = "preparing"
	StatusInProgress   MigrationStatus = "in_progress"
	StatusSwapping     MigrationStatus = "swapping"
	StatusCompleted    MigrationStatus = "completed"
	StatusRolledBack   MigrationStatus = "rolled_back"
	StatusFailed       MigrationStatus = "failed"
	StatusCancelled    MigrationStatus = "cancelled"
)

// SnapshotFormat specifies serialization format
type SnapshotFormat string

const (
	FormatJSON SnapshotFormat = "json"
	FormatBinary SnapshotFormat = "binary"
)

// PluginVersionPair tracks old→new versions
type PluginVersionPair struct {
	PluginName  string `json:"plugin_name"`
	VersionFrom string `json:"version_from"`
	VersionTo   string `json:"version_to"`
}

// StateTransformer defines migration function signature
type StateTransformer func(oldState interface{}) (newState interface{}, err error)

// HotSwapMigrationTask represents live migration job
type HotSwapMigrationTask struct {
	ID                 string                  `json:"id"`
	PluginInfo         PluginVersionPair       `json:"plugin_info"`
	Snapshot           map[string]interface{}  `json:"snapshot,omitempty"`
	SnapshotPath       string                  `json:"snapshot_path,omitempty"`
	SnapshotSizeBytes  int64                   `json:"snapshot_size_bytes"`
	MigrationStatus    MigrationStatus         `json:"migration_status"`
	CurrentPhase       MigrationPhase          `json:"current_phase"`
	StartTime          time.Time               `json:"start_time"`
	EndTime            *time.Time              `json:"end_time,omitempty"`
	DurationMs         int64                   `json:"duration_ms"`
	ErrorMsg           string                  `json:"error_msg,omitempty"`
	RollbackAttempted  bool                    `json:"rollback_attempted"`
	RollbackSuccess    bool                    `json:"rollback_success,omitempty"`
	PhasesHistory      []PhaseTimeline         `json:"phases_history"`
	SuccessRate        float64                 `json:"success_rate"`
	EvidenceChain      map[string]string       `json:"evidence_chain,omitempty"`
	PerformanceMetrics map[string]any          `json:"performance_metrics,omitempty"`
	Metadata           map[string]string       `json:"metadata,omitempty"`
	Tags               []string                `json:"tags,omitempty"`
}

// PhaseTimeline records phase execution details
type PhaseTimeline struct {
	Phase      MigrationPhase `json:"phase"`
	StartedAt  time.Time      `json:"started_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	DurationMs int64          `json:"duration_ms"`
	Success    bool           `json:"success"`
	Error      string         `json:"error,omitempty"`
}

// MigrationConfig configures hot-swap behavior
type MigrationConfig struct {
	MaxSwapTimeoutSec      int           `json:"max_swap_timeout_sec"`
	HistoryRetention       int           `json:"history_retention"`
	EnableRollbackGuard    bool          `json:"enable_rollback_guard"`
	StateSnapshotFormat    SnapshotFormat `json:"state_snapshot_format"`
	MigrationTimeoutMs     int64         `json:"migration_timeout_ms"`
	ParallelSwapsPermitted int           `json:"parallel_swaps_permitted"`
	CustomTransformers     []string      `json:"custom_transformers,omitempty"`
}

// SwapStats aggregates performance metrics
type SwapStats struct {
	AvgDurationMs        float64  `json:"avg_duration_ms"`
	P95DurationMs        int64    `json:"p95_duration_ms"`
	P99DurationMs        int64    `json:"p99_duration_ms"`
	MinDurationMs        int64    `json:"min_duration_ms"`
	MaxDurationMs        int64    `json:"max_duration_ms"`
	TotalSwaps           int      `json:"total_swaps"`
	SuccessfulSwaps      int      `json:"successful_swaps"`
	FailedSwaps          int      `json:"failed_swaps"`
	RolledBackSwaps      int      `json:"rolled_back_swaps"`
	AverageSuccessRate   float64  `json:"average_success_rate"`
	AvailableTransformers []string `json:"available_transformers"`
}

// TransformerRegistry holds state transformation functions
type TransformerRegistry struct {
	transformers map[string]StateTransformer
}

// ============================================================================
// Hot-Swap Store Interface
// ============================================================================

// HotSwapStore interface for persistence
type HotSwapStore interface {
	CreateMigrationTask(task *HotSwapMigrationTask) error
	GetMigrationTask(id string) (*HotSwapMigrationTask, error)
	UpdateMigrationTask(id string, updates map[string]any) error
	DeleteMigrationTask(id string) error
	ListMigrationTasks(filters map[string]any, limit, offset int) ([]HotSwapMigrationTask, error)
	
	RegisterTransformer(pluginName string, transformer StateTransformer) error
	GetTransformer(pluginName string) (StateTransformer, bool)
	ListTransformers() []string
	
	GetStats() (*SwapStats, error)
}

// ============================================================================
// Hot-Swap Handler
// ============================================================================

// HotSwapHandler manages zero-downtime plugin upgrades
type HotSwapHandler struct {
	store           HotSwapStore
	hotSwapEngine   *plugin.HotSwapEngine
	registry        *TransformerRegistry
	ledger          *evidence.Ledger
	logger          *logrus.Logger
	config          MigrationConfig
}

// NewHotSwapHandler creates new hot-swap handler
func NewHotSwapHandler(
	store HotSwapStore,
	engine *plugin.HotSwapEngine,
	registry *TransformerRegistry,
	ledger *evidence.Ledger,
	logger *logrus.Logger,
	config MigrationConfig,
) *HotSwapHandler {
	return &HotSwapHandler{
		store:         store,
		hotSwapEngine: engine,
		registry:      registry,
		ledger:        ledger,
		logger:        logger.WithField("handler", "hot_swap"),
		config:        config,
	}
}

// RegisterHotSwapRoutes registers REST endpoints for M52
func RegisterHotSwapRoutes(router *echo.Echo, handler *HotSwapHandler) {
	migration := router.Group("/api/v1/hot-swap")

	// Migration tasks
	migration.POST("", handler.handleStartMigration)
	migration.GET("", handler.handleListMigrations)
	migration.GET("/:id", handler.handleGetMigration)
	migration.DELETE("/:id", handler.handleCancelMigration)
	
	// Phase monitoring
	migration.GET("/:id/phases", handler.handleGetPhaseHistory)
	migration.GET("/:id/progress", handler.handleGetProgress)
	
	// Rollback operations
	migration.POST("/:id/rollback", handler.handleRollback)
	
	// Transformers
	registry := migration.Group("/transformers")
	registry.POST("", handler.handleRegisterTransformer)
	registry.GET("", handler.handleListTransformers)
	registry.DELETE("/:pluginName", handler.handleUnregisterTransformer)
	
	// Statistics
	stats := migration.Group("/stats")
	stats.GET("", handler.handleGetStats)
	stats.GET("/:pluginName", handler.handleGetPluginStats)
	
	// Evidence attestation
	evidencePath := migration.Group("/:id/evidence")
	evidencePath.POST("/attest", handler.handleAttestEvidence)
	evidencePath.GET("/chain", handler.handleGetEvidenceChain)
}

// ============================================================================
// Migration Task Handlers
// ============================================================================

// handleStartMigration initiates hot-swap process
// POST /api/v1/hot-swap
func (h *HotSwapHandler) handleStartMigration(c echo.Context) error {
	var request struct {
		PluginName  string `json:"plugin_name" binding:"required"`
		VersionFrom string `json:"version_from" binding:"required"`
		VersionTo   string `json:"version_to" binding:"required"`
		WasmPath    string `json:"wasm_path" binding:"required"`
		Config      map[string]any `json:"config,omitempty"`
	}
	
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	
	// Create migration task
	task := &HotSwapMigrationTask{
		ID:               uuid.New().String(),
		PluginInfo:       PluginVersionPair{
			PluginName:  request.PluginName,
			VersionFrom: request.VersionFrom,
			VersionTo:   request.VersionTo,
		},
		MigrationStatus: StatusPreparing,
		CurrentPhase:    PhasePreparing,
		StartTime:       time.Now(),
		SuccessRate:     1.0,
		PhasesHistory:   []PhaseTimeline{},
		EvidenceChain:   make(map[string]string),
	}
	
	// Sign evidence for migration start
	evidenceData := map[string]interface{}{
		"action": "start_migration",
		"task_id": task.ID,
		"plugin": request.PluginName,
		"from": request.VersionFrom,
		"to": request.VersionTo,
		"timestamp": time.Now().UTC(),
	}
	
	hash, signErr := h.signEvidence(evidenceData)
	if signErr == nil {
		task.EvidenceChain["migration_start"] = hash
	}
	
	// Persist task
	if err := h.store.CreateMigrationTask(task); err != nil {
		h.logger.WithError(err).Error("Failed to create migration task")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create migration task"})
	}
	
	// Start migration asynchronously
	go h.executeMigration(task, request.WasmPath, request.Config)
	
	h.logger.WithFields(logrus.Fields{
		"task_id": task.ID,
		"plugin": request.PluginName,
		"version_from": request.VersionFrom,
		"version_to": request.VersionTo,
	}).Info("Hot-swap migration initiated")
	
	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"task_id": task.ID,
		"status": StatusPreparing,
		"message": "Migration started asynchronously",
	})
}

// executeMigration runs the actual hot-swap process
func (h *HotSwapHandler) executeMigration(task *HotSwapMigrationTask, wasmPath string, config map[string]any) {
	ctx := context.Background()
	startTime := task.StartTime
	
	// Update status to in-progress
	h.updatePhase(task.ID, PhaseSnapshot, true, "", startTime)
	
	// Phase 1: Capture state snapshot (<10ms target)
	snapshot, snapErr := h.captureStateSnapshot(ctx, task.PluginInfo.PluginName)
	if snapErr != nil {
		h.updatePhase(task.ID, PhaseSnapshot, false, snapErr.Error(), startTime)
		h.failMigration(task.ID, "snapshot_capture_failed", snapErr.Error())
		return
	}
	
	task.Snapshot = snapshot
	task.SnapshotSizeBytes = int64(len(task.Snapshot))
	h.updatePhase(task.ID, PhaseSnapshot, true, "snapshot_captured", startTime)
	
	// Phase 2: Load new version (<50ms target)
	h.updatePhase(task.ID, PhaseLoadNew, true, "", startTime)
	loadErr := h.hotSwapEngine.LoadPlugin(ctx, wasmPath)
	if loadErr != nil {
		h.updatePhase(task.ID, PhaseLoadNew, false, loadErr.Error(), startTime)
		h.failMigration(task.ID, "load_new_version_failed", loadErr.Error())
		return
	}
	h.updatePhase(task.ID, PhaseLoadNew, true, "new_version_loaded", startTime)
	
	// Phase 3: Migrate state atomically (<300ms target)
	h.updatePhase(task.ID, PhaseMigrate, true, "", startTime)
	migrateErr := h.migrateState(ctx, task.PluginInfo.PluginName, snapshot, task.PluginInfo.VersionTo)
	if migrateErr != nil {
		h.updatePhase(task.ID, PhaseMigrate, false, migrateErr.Error(), startTime)
		
		// Attempt rollback
		h.updatePhase(task.ID, PhaseRollingBack, true, "", startTime)
		rollbackErr := h.hotSwapEngine.RollbackSwap(ctx, task.PluginInfo.PluginName, task.PluginInfo.VersionFrom)
		rollbackSuccess := rollbackErr == nil
		
		if rollbackSuccess {
			h.updatePhase(task.ID, PhaseRollingBack, true, "rollback_completed", startTime)
			task.MigrationStatus = StatusRolledBack
			task.RollbackSuccess = true
		}
		
		h.failMigration(task.ID, "state_migration_failed", migrateErr.Error())
		return
	}
	h.updatePhase(task.ID, PhaseMigrate, true, "state_migrated", startTime)
	
	// Phase 4: Activate new version (<100ms target)
	h.updatePhase(task.ID, PhaseActivate, true, "", startTime)
	activateErr := h.hotSwapEngine.ActivatePlugin(ctx, task.PluginInfo.PluginName)
	if activateErr != nil {
		h.updatePhase(task.ID, PhaseActivate, false, activateErr.Error(), startTime)
		h.failMigration(task.ID, "activation_failed", activateErr.Error())
		return
	}
	h.updatePhase(task.ID, PhaseActivate, true, "activated", startTime)
	
	// Phase 5: Mark completed
	endTime := time.Now()
	durationMs := int64(endTime.Sub(startTime) / time.Millisecond)
	
	task.MigrationStatus = StatusCompleted
	task.CurrentPhase = PhaseCompleted
	task.EndTime = &endTime
	task.DurationMs = durationMs
	task.SuccessRate = 1.0
	
	// Sign completion evidence
	evidenceData := map[string]interface{}{
		"action": "migration_completed",
		"task_id": task.ID,
		"duration_ms": durationMs,
		"timestamp": endTime.UTC(),
	}
	
	hash, signErr := h.signEvidence(evidenceData)
	if signErr == nil {
		task.EvidenceChain["completion"] = hash
	}
	
	// Update task record
	h.store.UpdateMigrationTask(task.ID, map[string]interface{}{
		"migration_status": StatusCompleted,
		"current_phase": PhaseCompleted,
		"end_time": endTime,
		"duration_ms": durationMs,
		"success_rate": 1.0,
		"phases_history": task.PhasesHistory,
	})
	
	h.logger.WithFields(logrus.Fields{
		"task_id": task.ID,
		"plugin": task.PluginInfo.PluginName,
		"duration_ms": durationMs,
	}).Info("Hot-swap migration completed successfully")
}

// captureStateSnapshot captures current plugin state
func (h *HotSwapHandler) captureStateSnapshot(ctx context.Context, pluginName string) (map[string]interface{}, error) {
	// Would query WASM export function get_state
	// Simplified for now
	snapshot := map[string]interface{}{
		"version": "v1.0",
		"data": map[string]interface{}{
			"initialized": true,
			"timestamp": time.Now().UTC(),
		},
	}
	
	return snapshot, nil
}

// migrateState transforms state from old to new version
func (h *HotSwapHandler) migrateState(ctx context.Context, pluginName string, oldState map[string]interface{}, newVersion string) error {
	// Get registered transformers
	transformer, exists := h.registry.GetTransformer(pluginName)
	if !exists {
		// Use identity transform if no custom transformer
		oldState["version"] = newVersion
		return nil
	}
	
	newState, err := transformer(oldState)
	if err != nil {
		return fmt.Errorf("state transformation failed: %w", err)
	}
	
	// In production: call set_state export on new instance
	_ = newState
	
	return nil
}

// failMigration handles migration failure
func (h *HotSwapHandler) failMigration(taskID, reason, errorMsg string) {
	task, err := h.store.GetMigrationTask(taskID)
	if err != nil {
		h.logger.WithError(err).Warn("Cannot retrieve task for failure handling")
		return
	}
	
	endTime := time.Now()
	task.MigrationStatus = StatusFailed
	task.CurrentPhase = PhaseFailed
	task.EndTime = &endTime
	task.ErrorMsg = errorMsg
	task.DurationMs = int64(endTime.Sub(task.StartTime) / time.Millisecond)
	
	h.store.UpdateMigrationTask(taskID, map[string]interface{}{
		"migration_status": StatusFailed,
		"current_phase": PhaseFailed,
		"end_time": endTime,
		"error_msg": errorMsg,
		"duration_ms": task.DurationMs,
	})
}

// updatePhase records phase timeline
func (h *HotSwapHandler) updatePhase(taskID string, phase MigrationPhase, success bool, error string, startTime time.Time) {
	timeline := PhaseTimeline{
		Phase:      phase,
		StartedAt:  time.Now(),
		Success:    success,
	}
	
	task, err := h.store.GetMigrationTask(taskID)
	if err != nil {
		return
	}
	
	task.PhasesHistory = append(task.PhasesHistory, timeline)
	
	if success {
		completedAt := time.Now()
		timeline.CompletedAt = &completedAt
		timeline.DurationMs = int64(completedAt.Sub(timeline.StartedAt) / time.Millisecond)
		
		// Update current phase
		task.CurrentPhase = phase
		task.MigrationStatus = StatusInProgress
	} else {
		task.CurrentPhase = phase
		if error != "" {
			timeline.Error = error
		}
	}
	
	h.store.UpdateMigrationTask(taskID, map[string]interface{}{
		"phases_history": task.PhasesHistory,
		"current_phase": task.CurrentPhase,
		"migration_status": task.MigrationStatus,
	})
}

// ============================================================================
// List & Query Handlers
// ============================================================================

// handleListMigrations lists migration tasks
// GET /api/v1/hot-swap
func (h *HotSwapHandler) handleListMigrations(c echo.Context) error {
	limit, _ := parseIntParam(c.QueryParam("limit"), 50)
	offset, _ := parseIntParam(c.QueryParam("offset"), 0)
	
	filters := make(map[string]any)
	if status := c.QueryParam("status"); status != "" {
		filters["migration_status"] = status
	}
	if pluginName := c.QueryParam("plugin_name"); pluginName != "" {
		filters["plugin_name"] = pluginName
	}
	
	tasks, err := h.store.ListMigrationTasks(filters, limit, offset)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list migrations")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to list migrations"})
	}
	
	totalCount := len(tasks)
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"migrations": tasks,
		"total": totalCount,
		"limit": limit,
		"offset": offset,
	})
}

// handleGetMigration retrieves specific migration task
// GET /api/v1/hot-swap/:id
func (h *HotSwapHandler) handleGetMigration(c echo.Context) error {
	id := c.Param("id")
	
	task, err := h.store.GetMigrationTask(id)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get migration")
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Migration task not found"})
	}
	
	return c.JSON(http.StatusOK, task)
}

// handleCancelMigration cancels an in-progress migration
// DELETE /api/v1/hot-swap/:id
func (h *HotSwapHandler) handleCancelMigration(c echo.Context) error {
	id := c.Param("id")
	
	task, err := h.store.GetMigrationTask(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Migration task not found"})
	}
	
	if task.MigrationStatus != StatusInProgress && task.MigrationStatus != StatusPreparing {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("Cannot cancel migration in status %s", task.MigrationStatus),
		})
	}
	
	// Update status to cancelled
	h.store.UpdateMigrationTask(id, map[string]interface{}{
		"migration_status": StatusCancelled,
	})
	
	h.logger.WithField("task", id).Info("Migration cancelled")
	return c.JSON(http.StatusOK, map[string]string{"message": "Migration cancelled"})
}

// handleGetPhaseHistory gets detailed phase history
// GET /api/v1/hot-swap/:id/phases
func (h *HotSwapHandler) handleGetPhaseHistory(c echo.Context) error {
	id := c.Param("id")
	
	task, err := h.store.GetMigrationTask(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Migration task not found"})
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"task_id": id,
		"phases_history": task.PhasesHistory,
		"count": len(task.PhasesHistory),
	})
}

// handleGetProgress gets real-time migration progress
// GET /api/v1/hot-swap/:id/progress
func (h *HotSwapHandler) handleGetProgress(c echo.Context) error {
	id := c.Param("id")
	
	task, err := h.store.GetMigrationTask(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Migration task not found"})
	}
	
	phasesCompleted := 0
	totalPhases := len(task.PhasesHistory)
	
	for _, phase := range task.PhasesHistory {
		if phase.Success {
			phasesCompleted++
		}
	}
	
	progressPercent := float64(0)
	if totalPhases > 0 {
		progressPercent = float64(phasesCompleted) / float64(totalPhases) * 100
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"task_id": id,
		"status": task.MigrationStatus,
		"current_phase": task.CurrentPhase,
		"progress_percent": progressPercent,
		"phases_completed": phasesCompleted,
		"total_phases": totalPhases,
		"duration_ms": task.DurationMs,
		"started_at": task.StartTime,
		"estimated_completion": time.Now().Add(time.Duration(500-task.DurationMs) * time.Millisecond),
	})
}

// ============================================================================
// Rollback Operations
// ============================================================================

// handleRollback manually triggers rollback
// POST /api/v1/hot-swap/:id/rollback
func (h *HotSwapHandler) handleRollback(c echo.Context) error {
	id := c.Param("id")
	
	task, err := h.store.GetMigrationTask(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Migration task not found"})
	}
	
	if task.RollbackAttempted {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Rollback already attempted"})
	}
	
	ctx := context.Background()
	startTime := time.Now()
	
	h.updatePhase(id, PhaseRollingBack, true, "", startTime)
	
	err = h.hotSwapEngine.RollbackSwap(ctx, task.PluginInfo.PluginName, task.PluginInfo.VersionFrom)
	
	endTime := time.Now()
	durationMs := int64(endTime.Sub(startTime) / time.Millisecond)
	
	rollbackSuccess := err == nil
	
	if rollbackSuccess {
		h.updatePhase(id, PhaseRollingBack, true, "rollback_completed", startTime)
		
		h.store.UpdateMigrationTask(id, map[string]interface{}{
			"rollback_attempted": true,
			"rollback_success": true,
			"current_phase": PhaseRolledBack,
		})
		
		h.logger.WithFields(logrus.Fields{
			"task": id,
			"duration_ms": durationMs,
		}).Info("Manual rollback completed successfully")
		
		return c.JSON(http.StatusOK, map[string]interface{}{
			"success": true,
			"duration_ms": durationMs,
			"message": "Rollback completed",
		})
	}
	
	h.updatePhase(id, PhaseRollingBack, false, err.Error(), startTime)
	
	h.store.UpdateMigrationTask(id, map[string]interface{}{
		"rollback_attempted": true,
		"rollback_success": false,
		"error_msg": err.Error(),
	})
	
	h.logger.WithFields(logrus.Fields{
		"task": id,
		"error": err,
	}).Warn("Manual rollback failed")
	
	return c.JSON(http.StatusInternalServerError, map[string]string{
		"success": false,
		"error": "Rollback failed",
	})
}

// ============================================================================
// Transformer Registry Handlers
// ============================================================================

// handleRegisterTransformer registers new state transformation function
// POST /api/v1/hot-swap/transformers
func (h *HotSwapHandler) handleRegisterTransformer(c echo.Context) error {
	var request struct {
		PluginName   string `json:"plugin_name" binding:"required"`
		TransformerConfig map[string]any `json:"config"`
	}
	
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	
	// In production: compile/load transformer from code
	// For now, register placeholder
	err := h.store.RegisterTransformer(request.PluginName, func(old interface{}) (newInterface{}, error) {
		return old, nil // Identity transform as default
	})
	
	if err != nil {
		h.logger.WithError(err).Error("Failed to register transformer")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to register transformer"})
	}
	
	h.logger.WithField("plugin", request.PluginName).Info("Transformer registered")
	return c.JSON(http.StatusCreated, map[string]string{
		"plugin_name": request.PluginName,
		"status": "registered",
	})
}

// handleListTransformers lists available transformers
// GET /api/v1/hot-swap/transformers
func (h *HotSwapHandler) handleListTransformers(c echo.Context) error {
	transformers := h.registry.Transformers()
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"transformers": transformers,
		"count": len(transformers),
	})
}

// handleUnregisterTransformer removes transformer registration
// DELETE /api/v1/hot-swap/transformers/:pluginName
func (h *HotSwapHandler) handleUnregisterTransformer(c echo.Context) error {
	pluginName := c.Param("pluginName")
	
	// Remove transformer registration
	h.registry.Unregister(pluginName)
	
	h.logger.WithField("plugin", pluginName).Info("Transformer unregistered")
	return c.NoContent(http.StatusNoContent)
}

// ============================================================================
// Statistics Handlers
// ============================================================================

// handleGetStats gets overall migration statistics
// GET /api/v1/hot-swap/stats
func (h *HotSwapHandler) handleGetStats(c echo.Context) error {
	stats, err := h.store.GetStats()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get stats")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to get statistics"})
	}
	
	return c.JSON(http.StatusOK, stats)
}

// handleGetPluginStats gets per-plugin statistics
// GET /api/v1/hot-swap/stats/:pluginName
func (h *HotSwapHandler) handleGetPluginStats(c echo.Context) error {
	pluginName := c.Param("pluginName")
	
	// Get filtered stats for specific plugin
	filters := map[string]any{"plugin_name": pluginName}
	tasks, err := h.store.ListMigrationTasks(filters, 1000, 0)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get plugin stats")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to get plugin statistics"})
	}
	
	var durations []int64
	successful := 0
	total := len(tasks)
	
	for _, task := range tasks {
		durations = append(durations, task.DurationMs)
		if task.SuccessRate > 0.9 {
			successful++
		}
	}
	
	avgDuration := float64(0)
	if len(durations) > 0 {
		var sum int64
		for _, d := range durations {
			sum += d
		}
		avgDuration = float64(sum) / float64(len(durations))
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"plugin_name": pluginName,
		"total_migrations": total,
		"successful_migrations": successful,
		"success_rate": float64(successful) / float64(total),
		"average_duration_ms": avgDuration,
		"distributions": durations,
	})
}

// ============================================================================
// Evidence Handlers
// ============================================================================

// handleAttestEvidence signs evidence for migration operation
// POST /api/v1/hot-swap/:id/evidence/attest
func (h *HotSwapHandler) handleAttestEvidence(c echo.Context) error {
	id := c.Param("id")
	
	attestation := struct {
		Action    string                 `json:"action" binding:"required"`
		Metadata  map[string]interface{} `json:"metadata"`
	}{}
	
	if err := c.Bind(&attestation); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid attestation request"})
	}
	
	hash, err := h.signEvidence(map[string]interface{}{
		"task_id": id,
		"action":  attestation.Action,
		"metadata": attestation.Metadata,
		"timestamp": time.Now().UTC(),
	})
	
	if err != nil {
		h.logger.WithError(err).Error("Failed to attest evidence")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Evidence attestation failed"})
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"task_id": id,
		"hash": hash,
		"algorithm": "Ed25519",
		"timestamp": time.Now().UTC(),
	})
}

// handleGetEvidenceChain retrieves evidence chain for migration
// GET /api/v1/hot-swap/:id/evidence/chain
func (h *HotSwapHandler) handleGetEvidenceChain(c echo.Context) error {
	id := c.Param("id")
	
	task, err := h.store.GetMigrationTask(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Migration task not found"})
	}
	
	chain := task.EvidenceChain
	if chain == nil {
		chain = make(map[string]string)
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"task_id": id,
		"evidence_chain": chain,
		"entry_count": len(chain),
	})
}

// ============================================================================
// Helper Functions
// ============================================================================

func (h *HotSwapHandler) signEvidence(data map[string]interface{}) (string, error) {
	if h.ledger == nil {
		return "", fmt.Errorf("evidence ledger not configured")
	}
	
	ctx := context.Background()
	signature, err := h.ledger.Attest(ctx, "hot_swap_migration", "m52_module", data)
	if err != nil {
		return "", err
	}
	
	return signature.Hash, nil
}
