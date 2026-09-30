// Package api provides RESTful HTTP handlers for the Data Pipeline subsystem.
// Endpoints:
//   - GET /api/v1/pipelines - List all pipelines with filtering
//   - POST /api/v1/pipelines - Create new pipeline
//   - GET /api/v1/pipelines/:id - Get pipeline details
//   - PUT /api/v1/pipelines/:id - Update pipeline configuration
//   - DELETE /api/v1/pipelines/:id - Delete pipeline
//   - POST /api/v1/pipelines/:id/run - Trigger manual pipeline run
//   - GET /api/v1/pipelines/:id/runs - List run history
//   - GET /api/v1/pipelines/:id/runs/:run_id - Get run details
//   - POST /api/v1/runs/:run_id/retry - Retry failed run
//   - GET /api/v1/runs/:run_id/logs - Get execution logs
//   - PUT /api/v1/pipelines/:id/archive - Archive pipeline
//   - PUT /api/v1/pipelines/:id/activate - Activate archived pipeline
package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/pipeline"
	"github.com/gin-gonic/gin"
)

// PipelineHandler handles data pipeline HTTP requests.
type PipelineHandler struct {
	manager *pipeline.Manager
}

// NewPipelineHandler creates a new PipelineHandler instance.
func NewPipelineHandler(mgr *pipeline.Manager) *PipelineHandler {
	return &PipelineHandler{
		manager: mgr,
	}
}

// RegisterRoutes registers all pipeline routes in the gin router.
func (h *PipelineHandler) RegisterRoutes(router *gin.Engine) {
	pipes := router.Group("/pipelines")
	{
		pipes.GET("", h.listPipelines)
		pipes.POST("", h.createPipeline)
		pipes.GET("/:id", h.getPipeline)
		pipes.PUT("/:id", h.updatePipeline)
		pipes.DELETE("/:id", h.deletePipeline)
		
		pipes.POST("/:id/run", h.triggerRun)
		pipes.GET("/:id/runs", h.listRuns)
		pipes.GET("/:id/runs/:run_id", h.getRun)
		pipes.PUT("/:id/archive", h.archivePipeline)
		pipes.PUT("/:id/activate", h.activatePipeline)
	}
	
	runs := router.Group("/runs")
	{
		runs.GET("", h.listAllRuns)
		runs.POST("/:run_id/retry", h.retryRun)
		runs.GET("/:run_id/logs", h.getRunLogs)
		runs.POST("/:run_id/cancel", h.cancelRun)
	}
}

// ============================================================================
// Pipeline CRUD Operations
// ============================================================================

// listPipelines godoc
// @Summary List all pipelines
// @Description Get all data pipelines with optional filtering by status, type, etc.
// @Tags data-pipeline
// @Param status query string false "Filter by status: active, inactive, archived"
// @Param source_type query string false "Filter by source type: postgres, mysql, snowflake, kafka"
// @Param name query string false "Search by pipeline name"
// @Param sort_by query string false "Sort field: name, created_at, updated_at"
// @Param sort_order query string false "asc or desc"
// @Param limit query int false "Results per page (default: 20)"
// @Param offset query int false "Pagination offset (default: 0)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/pipelines [get]
func (h *PipelineHandler) listPipelines(c *gin.Context) {
	filter := &pipeline.PipelineFilter{}
	
	if statusStr := c.Query("status"); statusStr != "" {
		switch statusStr {
		case "active":
			filter.Status = []pipeline.PipelineStatus{pipeline.PipelineActive}
		case "inactive":
			filter.Status = []pipeline.PipelineStatus{pipeline.PipelineInactive}
		case "archived":
			filter.Status = []pipeline.PipelineStatus{pipeline.PipelineArchived}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status value"})
			return
		}
	}
	
	if sourceType := c.Query("source_type"); sourceType != "" {
		filter.Type = sourceType
	}
	
	if name := c.Query("name"); name != "" {
		filter.Name = name
	}
	
	if sortBy := c.Query("sort_by"); sortBy != "" {
		filter.SortBy = sortBy
	}
	
	if sortOrder := c.Query("sort_order"); sortOrder != "" {
		if sortOrder == "asc" || sortOrder == "desc" {
			filter.SortOrder = sortOrder
		} else {
			filter.SortOrder = "desc"
		}
	}
	
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	filter.Limit = limit
	filter.Offset = offset
	
	pipelines, total, err := h.manager.ListPipelines(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "failed to list pipelines",
			"details":  err.Error(),
		})
		return
	}
	
	response := gin.H{
		"pipelines": pipelines,
		"count":     len(pipelines),
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	}
	
	c.JSON(http.StatusOK, response)
}

// createPipeline godoc
// @Summary Create new pipeline
// @Description Create a new data pipeline with source, target, and transformation rules
// @Tags data-pipeline
// @Success 201 {object} pipeline.Pipeline
// @Router /api/v1/pipelines [post]
func (h *PipelineHandler) createPipeline(c *gin.Context) {
	var p pipeline.Pipeline
	
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}
	
	p.Status = pipeline.PipelineActive
	
	if err := h.manager.CreatePipeline(c.Request.Context(), &p); err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			c.JSON(http.StatusConflict, gin.H{
				"error": "pipeline with this name already exists",
			})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":    "failed to create pipeline",
				"details":  err.Error(),
			})
		}
		return
	}
	
	c.JSON(http.StatusCreated, p)
}

// getPipeline godoc
// @Summary Get pipeline details
// @Description Retrieve detailed information about a single pipeline
// @Tags data-pipeline
// @Param id path string true "Pipeline ID"
// @Success 200 {object} pipeline.Pipeline
// @Router /api/v1/pipelines/{id} [get]
func (h *PipelineHandler) getPipeline(c *gin.Context) {
	id := c.Param("id")
	
	p, err := h.manager.GetPipeline(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "pipeline not found",
			"details":  err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, p)
}

// updatePipeline godoc
// @Summary Update pipeline configuration
// @Description Update an existing pipeline's settings, transformations, schedule, etc.
// @Tags data-pipeline
// @Param id path string true "Pipeline ID"
// @Success 200 {object} pipeline.Pipeline
// @Router /api/v1/pipelines/{id} [put]
func (h *PipelineHandler) updatePipeline(c *gin.Context) {
	id := c.Param("id")
	
	var updates pipeline.Pipeline
	
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}
	
	existing, err := h.manager.GetPipeline(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "pipeline not found",
		})
		return
	}
	
	// Merge updates into existing pipeline
	updates.ID = existing.ID
	if updates.Name != "" {
		existing.Name = updates.Name
	}
	if updates.Description != "" {
		existing.Description = updates.Description
	}
	if updates.Source.Type != "" {
		existing.Source = updates.Source
	}
	if updates.Target.Type != "" {
		existing.Target = updates.Target
	}
	if len(updates.Transformations) > 0 {
		existing.Transformations = updates.Transformations
	}
	if updates.Schedule != nil {
		existing.Schedule = updates.Schedule
	}
	if updates.Alerts.Enabled != existing.Alerts.Enabled {
		existing.Alerts = updates.Alerts
	}
	if updates.Status != "" {
		existing.Status = updates.Status
	}
	
	if err := h.manager.UpdatePipeline(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "failed to update pipeline",
			"details":  err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, existing)
}

// deletePipeline godoc
// @Summary Delete pipeline
// @Description Permanently delete a pipeline from the system
// @Tags data-pipeline
// @Param id path string true "Pipeline ID"
// @Success 204
// @Router /api/v1/pipelines/{id} [delete]
func (h *PipelineHandler) deletePipeline(c *gin.Context) {
	id := c.Param("id")
	
	if err := h.manager.DeletePipeline(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "failed to delete pipeline",
			"details":  err.Error(),
		})
		return
	}
	
	c.Status(http.StatusNoContent)
}

// archivePipeline godoc
// @Summary Archive pipeline
// @Description Soft-delete a pipeline by setting its status to archived
// @Tags data-pipeline
// @Param id path string true "Pipeline ID"
// @Success 200 {object} pipeline.Pipeline
// @Router /api/v1/pipelines/{id}/archive [put]
func (h *PipelineHandler) archivePipeline(c *gin.Context) {
	id := c.Param("id")
	
	if err := h.manager.ArchivePipeline(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "failed to archive pipeline",
			"details":  err.Error(),
		})
		return
	}
	
	existing, _ := h.manager.GetPipeline(c.Request.Context(), id)
	c.JSON(http.StatusOK, existing)
}

// activatePipeline godoc
// @Summary Activate archived pipeline
// @Description Change pipeline status from archived back to active
// @Tags data-pipeline
// @Param id path string true "Pipeline ID"
// @Success 200 {object} pipeline.Pipeline
// @Router /api/v1/pipelines/{id}/activate [put]
func (h *PipelineHandler) activatePipeline(c *gin.Context) {
	id := c.Param("id")
	
	if err := h.manager.ActivatePipeline(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "failed to activate pipeline",
			"details":  err.Error(),
		})
		return
	}
	
	existing, _ := h.manager.GetPipeline(c.Request.Context(), id)
	c.JSON(http.StatusOK, existing)
}

// ============================================================================
// Run Operations
// ============================================================================

// triggerRun godoc
// @Summary Trigger manual pipeline run
// @Description Start a new execution of the pipeline manually
// @Tags data-pipeline
// @Param id path string true "Pipeline ID"
// @Success 202 {object} pipeline.PipelineRun
// @Router /api/v1/pipelines/{id}/run [post]
func (h *PipelineHandler) triggerRun(c *gin.Context) {
	id := c.Param("id")
	
	run, err := h.manager.TriggerRun(
		c.Request.Context(), 
		id, 
		"user_manual", // In production would extract user ID from auth context
		"manual",
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "failed to trigger run",
			"details":  err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusAccepted, run)
}

// listRuns godoc
// @Summary List run history for a pipeline
// @Description Get all executions of a specific pipeline with filtering options
// @Tags data-pipeline
// @Param id path string true "Pipeline ID"
// @Param status query string false "Filter by status: pending, running, success, failure"
// @Param limit query int false "Results per page (default: 50)"
// @Param offset query int false "Pagination offset (default: 0)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/pipelines/{id}/runs [get]
func (h *PipelineHandler) listRuns(c *gin.Context) {
	pipelineID := c.Param("id")
	
	filter := &pipeline.RunFilter{}
	
	if statusStr := c.Query("status"); statusStr != "" {
		status := parseRunStatus(statusStr)
		if status == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status value"})
			return
		}
		filter.Status = []pipeline.RunStatus{status}
	}
	
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	filter.Limit = limit
	filter.Offset = offset
	
	runs, total, err := h.manager.GetRunStore().ListRuns(c.Request.Context(), pipelineID, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "failed to list runs",
			"details":  err.Error(),
		})
		return
	}
	
	response := gin.H{
		"runs":    runs,
		"count":   len(runs),
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	}
	
	c.JSON(http.StatusOK, response)
}

// getRun godoc
// @Summary Get run details
// @Description Retrieve detailed information about a specific pipeline run
// @Tags data-pipeline
// @Param run_id path string true "Run ID"
// @Success 200 {object} pipeline.PipelineRun
// @Router /api/v1/runs/{run_id} [get]
func (h *PipelineHandler) getRun(c *gin.Context) {
	runID := c.Param("run_id")
	
	run, err := h.manager.GetRunStore().GetRunByID(c.Request.Context(), runID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "run not found",
			"details":  err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, run)
}

// listAllRuns godoc
// @Summary List recent runs across all pipelines
// @Description Get the most recent pipeline runs across all pipelines
// @Tags data-pipeline
// @Param limit query int false "Results per page (default: 30)"
// @Param offset query int false "Pagination offset (default: 0)"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/runs [get]
func (h *PipelineHandler) listAllRuns(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	
	runs, err := h.manager.GetRunStore().GetRecentRuns(
		c.Request.Context(),
		limit+offset,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "failed to list runs",
			"details":  err.Error(),
		})
		return
	}
	
	// Apply pagination
	total := int64(len(runs))
	start := offset
	if start > total {
		start = 0
	}
	end := start + int64(limit)
	if end > total {
		end = total
	}
	
	paginatedRuns := runs[start:end]
	
	c.JSON(http.StatusOK, gin.H{
		"runs":    paginatedRuns,
		"count":   len(paginatedRuns),
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// retryRun godoc
// @Summary Retry failed run
// @Description Re-execute a failed run with the same configuration
// @Tags data-pipeline
// @Param run_id path string true "Run ID"
// @Success 202 {object} pipeline.PipelineRun
// @Router /api/v1/runs/{run_id}/retry [post]
func (h *PipelineHandler) retryRun(c *gin.Context) {
	runID := c.Param("run_id")
	
	run, err := h.manager.GetRunStore().GetRunByID(c.Request.Context(), runID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "run not found",
		})
		return
	}
	
	if run.Status != pipeline.RunFailure {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "can only retry failed runs",
		})
		return
	}
	
	// Trigger a new run with the same pipeline
	newRun, err := h.manager.TriggerRun(
		c.Request.Context(),
		run.PipelineID,
		"user_retry",
		"retry",
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    "failed to initiate retry",
			"details":  err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusAccepted, newRun)
}

// cancelRun godoc
// @Summary Cancel running run
// @Description Cancel a pending or currently running pipeline execution
// @Tags data-pipeline
// @Param run_id path string true "Run ID"
// @Success 200 {object} map[string]string
// @Router /api/v1/runs/{run_id}/cancel [post]
func (h *PipelineHandler) cancelRun(c *gin.Context) {
	runID := c.Param("run_id")
	
	if err := h.manager.GetRunStore().CancelRun(c.Request.Context(), runID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":    "run not found or cannot be cancelled",
			"details":  err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message": "cancellation initiated",
		"run_id":  runID,
	})
}

// getRunLogs godoc
// @Summary Get run logs
// @Description Retrieve execution logs for a pipeline run
// @Tags data-pipeline
// @Param run_id path string true "Run ID"
// @Param level query string false "Log level: info, warn, error (default: info)"
// @Success 200 {array} string
// @Router /api/v1/runs/{run_id}/logs [get]
func (h *PipelineHandler) getRunLogs(c *gin.Context) {
	runID := c.Param("run_id")
	level := c.DefaultQuery("level", "info")
	
	// In production, would load logs from storage
	logs := []string{
		"[INFO] " + time.Now().Format(time.RFC3339) + " - Run started",
		"[INFO] " + time.Now().Format(time.RFC3339) + " - Reading from source...",
		"[INFO] " + time.Now().Format(time.RFC3339) + " - Transforming data...",
		"[INFO] " + time.Now().Format(time.RFC3339) + " - Writing to target...",
		"[INFO] " + time.Now().Format(time.RFC3339) + " - Run completed successfully",
	}
	
	// Filter by log level if needed
	if level != "all" && level != "info" {
		// Simplified filtering - in production would filter properly
		if level == "warn" {
			// Would return only warnings
		} else if level == "error" {
			// Would return only errors
		}
	}
	
	c.JSON(http.StatusOK, gin.H{
		"logs":       logs,
		"run_id":     runID,
		"log_level":  level,
		"line_count": len(logs),
	})
}

// Helper functions

func parseRunStatus(statusStr string) pipeline.RunStatus {
	switch statusStr {
	case "pending":
		return pipeline.RunPending
	case "running":
		return pipeline.RunRunning
	case "success":
		return pipeline.RunSuccess
	case "failure":
		return pipeline.RunFailure
	case "cancelled":
		return pipeline.RunCancelled
	default:
		return ""
	}
}
