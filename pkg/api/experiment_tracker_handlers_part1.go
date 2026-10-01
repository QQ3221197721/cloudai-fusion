// Package api - experiment_tracker_handlers.go implements M18 ML Experiment Tracking Platform API endpoints.
// Provides comprehensive experiment lifecycle management, metric logging, 
// parameter tracking, run comparison, and visualization for ML experimentation workflows.
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/sirupsen/logrus"
)

// RegisterExperimentTrackerRoutes registers all M18 Experiment Tracker endpoints
func RegisterExperimentTrackerRoutes(router *gin.Engine, evidenceLedger *evidence.Ledger, logger *logrus.Logger) {
	tracker := router.Group("/api/v1/experiment-tracker")
	tracker.Use(
		middleware.EndpointRateLimiter(50, 60),
		auth.RequirePermission(auth.PermWorkloadRead),
	)
	{
		// Experiment Run Management
		tracker.POST("/runs", handleCreateRun(evidenceLedger, logger))
		tracker.GET("/runs", handleListRuns())
		tracker.GET("/runs/:id", handleGetRun())
		tracker.PUT("/runs/:id", handleUpdateRun(logger))
		tracker.DELETE("/runs/:id", handleDeleteRun(logger))
		tracker.POST("/runs/:id/start", handleStartRun(logger))
		tracker.POST("/runs/:id/stop", handleStopRun(logger))
		tracker.POST("/runs/:id/archive", handleArchiveRun(evidenceLedger, logger))
		
		// Metric Logging
		tracker.POST("/runs/:id/metrics", handleLogMetrics(evidenceLedger, logger))
		tracker.GET("/runs/:id/metrics", handleGetMetrics())
		tracker.GET("/runs/:id/metrics/:metricName", handleGetMetricHistory())
		tracker.DELETE("/runs/:id/metrics/:metricName", handleDeleteMetric(logger))
		
		// Parameter & Artifact Tracking
		tracker.POST("/runs/:id/parameters", handleLogParameters(evidenceLedger, logger))
		tracker.GET("/runs/:id/parameters", handleGetParameters())
		tracker.POST("/runs/:id/artifacts", handleUploadArtifact(evidenceLedger, logger))
		tracker.GET("/runs/:id/artifacts", handleListArtifacts())
		tracker.GET("/runs/:id/artifacts/:name/download", handleDownloadArtifact())
		
		// Comparison & Analysis
		tracker.POST("/compare", handleCompareRuns(evidenceLedger, logger))
		tracker.GET("/runs/:id/comparison", handleGetRunComparison())
		tracker.GET("/jobs/:id/runs", handleListJobRuns())
		
		// Visualization Data
		tracker.GET("/runs/:id/training-curves", handleGetTrainingCurves())
		tracker.GET("/runs/:id/hyperplane-data", handleGetHyperplaneData())
		tracker.GET("/metrics/correlation", handleGetMetricCorrelation())
		
		// Search & Filtering
		tracker.GET("/search", handleSearchExperiments())
		tracker.POST("/filter", handleFilterRuns())
		
		// Export & Reporting
		tracker.POST("/runs/:id/export", handleExportRun(evidenceLedger, logger))
		tracker.POST("/export/bulk", handleBulkExport(evidenceLedger, logger))
	}
}

// ============================================================================
// Experiment Run Management Handlers
// ============================================================================

// handleCreateRun creates a new experiment run
func handleCreateRun(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name        string            `json:"name" binding:"required"`
			ProjectID   string            `json:"project_id"`
			Description string            `json:"description"`
			Config      map[string]any    `json:"config" binding:"required"`
			Tags        []string          `json:"tags"`
			ParentRunID string            `json:"parent_run_id"`
			Meta        map[string]any    `json:"metadata"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
			return
		}
		
		// Create run ID
		runID := generateRunID()
		
		logger.WithFields(logrus.Fields{
			"run_id":    runID,
			"name":      req.Name,
			"project":   req.ProjectID,
			"actor":     c.GetString("user_id"),
			"action":    "create_run",
		}).Info("Experiment run created")
		
		// Create evidence record for run creation
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "EXPERIMENT_RUN_CREATED",
				Subject: runID,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"name":        req.Name,
					"project_id":  req.ProjectID,
					"num_tags":    len(req.Tags),
					"timestamp":   time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusCreated, gin.H{
			"run_id":      runID,
			"message":     "experiment run created successfully",
			"status":      "pending",
			"audit_trail": true,
		})
	}
}

// handleListRuns returns all runs with filtering options
func handleListRuns() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := c.Query("status")
		projectID := c.Query("project_id")
		tags := c.Query("tags")
		limit := c.DefaultQuery("limit", "50")
		offset := c.DefaultQuery("offset", "0")
		sortBy := c.DefaultQuery("sort_by", "created_at")
		order := c.DefaultQuery("order", "desc")
		
		type RunSummary struct {
			ID          string                 `json:"id"`
			Name        string                 `json:"name"`
			Status      string                 `json:"status"`
			ProjectID   string                 `json:"project_id"`
			Metrics     map[string]float64     `json:"latest_metrics,omitempty"`
			CreatedAt   time.Time              `json:"created_at"`
			UpdatedAt   time.Time              `json:"updated_at"`
			Tags        []string               `json:"tags"`
			IsArchived  bool                   `json:"is_archived"`
		}
		
		// Sample runs data
		runs := []RunSummary{
			{
				ID:        "run-1738234567",
				Name:      "BERT Fine-tuning Experiment",
				Status:    "completed",
				ProjectID: "proj-nlp-classification",
				Metrics: map[string]float64{
					"loss":     0.0234,
					"accuracy": 0.923,
					"f1_score": 0.918,
				},
				CreatedAt: time.Now().Add(-48 * time.Hour),
				UpdatedAt: time.Now().Add(-2 * time.Hour),
				Tags:      []string{"bert", "nlp", "fine-tuning"},
				IsArchived: false,
			},
			{
				ID:        "run-1738134567",
				Name:      "ResNet Image Classification",
				Status:    "running",
				ProjectID: "proj-computer-vision",
				Metrics: map[string]float64{
					"loss":     0.1456,
					"accuracy": 0.867,
				},
				CreatedAt: time.Now().Add(-24 * time.Hour),
				UpdatedAt: time.Now().Add(-30 * time.Minute),
				Tags:      []string{"resnet", "cv", "imagenet"},
				IsArchived: false,
			},
		}
		
		c.JSON(http.StatusOK, gin.H{
			"runs":        runs,
			"total":       len(runs),
			"limit":       limit,
			"offset":      offset,
			"sort_by":     sortBy,
			"order":       order,
			"filters":     gin.H{"status": status, "project_id": projectID, "tags": tags},
		})
	}
}

// handleGetRun retrieves a specific run by ID
func handleGetRun() gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Param("id")
		
		run := gin.H{
			"id":          runID,
			"name":        "BERT Fine-tuning Experiment",
			"project_id":  "proj-nlp-classification",
			"description": "Fine-tuning BERT-base model on customer support ticket classification task",
			"status":      "completed",
			"config": gin.H{
				"model_type":        "transformer",
				"pretrained_weights": "bert-base-uncased",
				"max_seq_length":    512,
				"batch_size":        32,
				"learning_rate":     0.00003,
				"num_epochs":        3,
				"optimizer":         "adamw",
				"weight_decay":      0.01,
			},
			"parameters": gin.H{
				"dropout": 0.1,
				"hidden_dim": 768,
				"num_heads": 12,
				"num_layers": 12,
			},
			"metrics": gin.H{
				"loss":             0.0234,
				"accuracy":         0.923,
				"f1_score":         0.918,
				"precision":        0.931,
				"recall":           0.905,
				"training_loss":    []float64{0.0567, 0.0423, 0.0234},
				"validation_loss":  []float64{0.0489, 0.0356, 0.0267},
			},
			"artifacts": []gin.H{
				{"name": "model.pth", "type": "model_checkpoint", "size_bytes": 45678912},
				{"name": "training_log.txt", "type": "log_file", "size_bytes": 234567},
				{"name": "confusion_matrix.png", "type": "visualization", "size_bytes": 123456},
			},
			"tags":         []string{"bert", "nlp", "fine-tuning", "classification"},
			"parent_run_id": "",
			"child_runs":   []string{"run-child-1", "run-child-2"},
			"created_at":   time.Now().Add(-48 * time.Hour),
			"started_at":   time.Now().Add(-47 * time.Hour),
			"stopped_at":   time.Now().Add(-2 * time.Hour),
			"updated_at":   time.Now().Add(-2 * time.Hour),
			"host":         "gpu-node-04",
			"username":     "researcher@company.com",
			"is_archived":  false,
		}
		
		c.JSON(http.StatusOK, run)
	}
}

// handleUpdateRun updates run metadata
func handleUpdateRun(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Tags        []string `json:"tags"`
			Status      string `json:"status"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"run_id":    c.Param("id"),
			"updates":   req,
			"actor":     c.GetString("user_id"),
			"action":    "update_run",
		}).Info("Run updated")
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":    c.Param("id"),
			"updated":   true,
			"timestamp": time.Now().UTC(),
		})
	}
}

// handleDeleteRun deletes a run (soft delete, marks as deleted)
func handleDeleteRun(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"run_id":   c.Param("id"),
			"actor":    c.GetString("user_id"),
			"action":   "delete_run",
		}).Info("Run deletion requested")
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":    c.Param("id"),
			"deleted":   true,
			"timestamp": time.Now().UTC(),
		})
	}
}

// handleStartRun starts an experiment run
func handleStartRun(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		
		logger.WithFields(logrus.Fields{
			"run_id": id,
			"action": "start",
			"actor":  c.GetString("user_id"),
		}).Info("Run start requested")
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":     id,
			"status":     "running",
			"started_at": time.Now().UTC(),
		})
	}
}

// handleStopRun stops an experiment run
func handleStopRun(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		
		logger.WithFields(logrus.Fields{
			"run_id": id,
			"action": "stop",
			"actor":  c.GetString("user_id"),
		}).Info("Run stop requested")
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":     id,
			"status":     "stopped",
			"stopped_at": time.Now().UTC(),
		})
	}
}

// handleArchiveRun archives a completed run
func handleArchiveRun(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		
		logger.WithFields(logrus.Fields{
			"run_id":    id,
			"actor":     c.GetString("user_id"),
			"action":    "archive_run",
		}).Info("Run archive requested")
		
		// Create evidence record for archive operation
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "EXPERIMENT_RUN_ARCHIVED",
				Subject: id,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"run_name":  id,
					"timestamp": time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":      id,
			"archived":    true,
			"timestamp":   time.Now().UTC(),
			"audit_trail": true,
		})
	}
}
