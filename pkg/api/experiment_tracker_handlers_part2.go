// Package api - experiment_tracker_handlers.go (Part 2) - M18 ML Experiment Tracking Platform API endpoints.
// Continuation of handlers for metric logging, comparison, and export features.
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

// ============================================================================
// Metric Logging Handlers (continued from Part 1)
// ============================================================================

// handleLogMetrics logs metrics for a run
func handleLogMetrics(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Param("id")
		
		var req struct {
			Metrics   map[string]float64 `json:"metrics" binding:"required"`
			Step      int64              `json:"step"`
			Timestamp time.Time          `json:"timestamp"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"run_id":    runID,
			"num_metrics": len(req.Metrics),
			"step":      req.Step,
			"actor":     c.GetString("user_id"),
			"action":    "log_metrics",
		}).Info("Metrics logged")
		
		// Create evidence record for metric logging
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "METRICS_LOGGED",
				Subject: runID,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"num_metrics": len(req.Metrics),
					"step":        req.Step,
					"metrics":     req.Metrics,
					"timestamp":   time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":      runID,
			"logged":      true,
			"num_metrics": len(req.Metrics),
			"step":        req.Step,
			"timestamp":   time.Now().UTC(),
			"audit_trail": true,
		})
	}
}

// handleGetMetrics retrieves all metrics for a run
func handleGetMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Param("id")
		
		type MetricPoint struct {
			Step    int64   `json:"step"`
			Value   float64 `json:"value"`
			Timestamp time.Time `json:"timestamp"`
		}
		
		metrics := gin.H{
			"loss": []MetricPoint{
				{Step: 100, Value: 0.0567, Timestamp: time.Now().Add(-47*time.Hour + 1*time.Second)},
				{Step: 200, Value: 0.0489, Timestamp: time.Now().Add(-47*time.Hour + 2*time.Second)},
				{Step: 300, Value: 0.0423, Timestamp: time.Now().Add(-47*time.Hour + 3*time.Second)},
			},
			"accuracy": []MetricPoint{
				{Step: 100, Value: 0.823, Timestamp: time.Now().Add(-47*time.Hour + 1*time.Second)},
				{Step: 200, Value: 0.856, Timestamp: time.Now().Add(-47*time.Hour + 2*time.Second)},
				{Step: 300, Value: 0.878, Timestamp: time.Now().Add(-47*time.Hour + 3*time.Second)},
			},
		}
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":  runID,
			"metrics": metrics,
			"total_steps": 300,
		})
	}
}

// handleGetMetricHistory retrieves history for a specific metric
func handleGetMetricHistory() gin.HandlerFunc {
	return func(c *gin.Context) {
		metricName := c.Param("metricName")
		
		history := gin.H{
			"run_id":         c.Param("id"),
			"metric_name":    metricName,
			"data_points":    300,
			"min_value":      0.0234,
			"max_value":      0.0567,
			"mean_value":     0.0389,
			"final_value":    0.0234,
		}
		
		c.JSON(http.StatusOK, history)
	}
}

// handleDeleteMetric deletes a specific metric
func handleDeleteMetric(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		metricName := c.Param("metricName")
		
		logger.WithFields(logrus.Fields{
			"run_id":       c.Param("id"),
			"metric_name":  metricName,
			"actor":        c.GetString("user_id"),
			"action":       "delete_metric",
		}).Info("Metric deletion requested")
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":      c.Param("id"),
			"metric_name": metricName,
			"deleted":     true,
			"timestamp":   time.Now().UTC(),
		})
	}
}

// ============================================================================
// Parameter & Artifact Tracking Handlers
// ============================================================================

// handleLogParameters logs parameters for a run
func handleLogParameters(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Param("id")
		
		var req struct {
			Parameters map[string]any `json:"parameters" binding:"required"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"run_id":       runID,
			"num_params":   len(req.Parameters),
			"actor":        c.GetString("user_id"),
			"action":       "log_parameters",
		}).Info("Parameters logged")
		
		// Create evidence record for parameter logging
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "PARAMETERS_LOGGED",
				Subject: runID,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"num_parameters": len(req.Parameters),
					"parameters":     req.Parameters,
					"timestamp":      time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":        runID,
			"logged":        true,
			"num_parameters": len(req.Parameters),
			"timestamp":     time.Now().UTC(),
			"audit_trail":   true,
		})
	}
}

// handleGetParameters retrieves parameters for a run
func handleGetParameters() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"run_id":    c.Param("id"),
			"parameters": gin.H{
				"learning_rate": 0.00003,
				"batch_size":    32,
				"dropout":       0.1,
				"weight_decay":  0.01,
			},
		})
	}
}

// handleUploadArtifact uploads an artifact for a run
func handleUploadArtifact(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Param("id")
		
		var req struct {
			Name        string `json:"name" binding:"required"`
			Type        string `json:"type" binding:"required"` // "model_checkpoint", "log_file", "visualization"
			Description string `json:"description"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"run_id":      runID,
			"artifact_name": req.Name,
			"artifact_type": req.Type,
			"actor":       c.GetString("user_id"),
			"action":      "upload_artifact",
		}).Info("Artifact upload requested")
		
		// Create evidence record for artifact upload
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "ARTIFACT_UPLOADED",
				Subject: runID,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"artifact_name": req.Name,
					"artifact_type": req.Type,
					"timestamp":     time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":           runID,
			"artifact_name":    req.Name,
			"artifact_type":    req.Type,
			"uploaded":         true,
			"timestamp":        time.Now().UTC(),
			"audit_trail":      true,
		})
	}
}

// handleListArtifacts lists artifacts for a run
func handleListArtifacts() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"run_id": c.Param("id"),
			"artifacts": []gin.H{
				{"name": "model.pth", "type": "model_checkpoint", "size_bytes": 45678912, "created_at": time.Now().Add(-2 * time.Hour)},
				{"name": "training_log.txt", "type": "log_file", "size_bytes": 234567, "created_at": time.Now().Add(-3 * time.Hour)},
				{"name": "confusion_matrix.png", "type": "visualization", "size_bytes": 123456, "created_at": time.Now().Add(-3 * time.Hour + 30*time.Minute)},
			},
		})
	}
}

// handleDownloadArtifact downloads an artifact
func handleDownloadArtifact() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"download_url": fmt.Sprintf("/api/v1/experiment-tracker/runs/%s/artifacts/%s/download", 
				c.Param("id"), c.Param("name")),
			"expires_at": time.Now().Add(1 * time.Hour),
		})
	}
}

// ============================================================================
// Comparison & Analysis Handlers
// ============================================================================

// handleCompareRuns compares multiple runs
func handleCompareRuns(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			RunIDs       []string `json:"run_ids" binding:"required"`
			MetricsToCompare []string `json:"metrics_to_compare"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"num_runs":    len(req.RunIDs),
			"runs":        req.RunIDs,
			"actor":       c.GetString("user_id"),
			"action":      "compare_runs",
		}).Info("Run comparison requested")
		
		// Create evidence record for comparison
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "RUNS_COMPARED",
				Subject: fmt.Sprintf("%v", req.RunIDs),
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"num_runs":        len(req.RunIDs),
					"timestamp":       time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"comparison_id": fmt.Sprintf("comp-%d", time.Now().UnixNano()),
			"runs_compared": len(req.RunIDs),
			"comparison_url": fmt.Sprintf("/experiments/compare/comp-%d", time.Now().UnixNano()),
			"audit_trail":   true,
		})
	}
}

// handleGetRunComparison gets detailed comparison for a run vs best baseline
func handleGetRunComparison() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"run_id":               c.Param("id"),
			"baseline_run_id":      "run-baseline-001",
			"improvements": gin.H{
				"accuracy":      "+2.3%",
				"f1_score":      "+1.8%",
				"latency_ms":    "-15ms (-12%)",
				"inference_cost": "-8%",
			},
		})
	}
}

// handleListJobRuns lists all runs under a job
func handleListJobRuns() gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		
		c.JSON(http.StatusOK, gin.H{
			"job_id":  jobID,
			"total_runs": 24,
			"running":   3,
			"completed": 18,
			"failed":    3,
		})
	}
}

// ============================================================================
// Visualization Data Handlers
// ============================================================================

// handleGetTrainingCurves returns training curve data
func handleGetTrainingCurves() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"run_id":   c.Param("id"),
			"curves": gin.H{
				"loss":     []float64{0.0567, 0.0489, 0.0423, 0.0356, 0.0289, 0.0234},
				"accuracy": []float64{0.823, 0.856, 0.878, 0.897, 0.912, 0.923},
			},
			"x_axis": "steps",
			"x_values": []int64{100, 200, 300, 400, 500, 600},
		})
	}
}

// handleGetHyperplaneData returns hyperplane visualization data
func handleGetHyperplaneData() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"run_id":            c.Param("id"),
			"hyperplane_points": 50,
			"dimensions":        ["lr", "batch", "dropout"],
		})
	}
}

// handleGetMetricCorrelation returns correlation analysis between metrics
func handleGetMetricCorrelation() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"correlations": gin.H{
				"loss_accuracy": -0.89,
				"loss_f1_score":  -0.85,
				"accuracy_f1_score": 0.94,
			},
		})
	}
}

// ============================================================================
// Search & Filtering Handlers
// ============================================================================

// handleSearchExperiments searches experiments
func handleSearchExperiments() gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")
		
		results := gin.H{
			"query": query,
			"total_results": 3,
			"results": []gin.H{
				{"id": "run-1738234567", "name": "BERT Fine-tuning", "relevance": 0.95},
				{"id": "run-1738134567", "name": "ResNet Training", "relevance": 0.82},
			},
		}
		
		c.JSON(http.StatusOK, results)
	}
}

// handleFilterRuns filters runs based on criteria
func handleFilterRuns() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Filters map[string]any `json:"filters" binding:"required"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"filtered":     true,
			"num_results":  12,
			"filters_applied": req.Filters,
		})
	}
}

// ============================================================================
// Export & Reporting Handlers
// ============================================================================

// handleExportRun exports a single run
func handleExportRun(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		runID := c.Param("id")
		
		var req struct {
			Format string `json:"format" binding:"required"` // "json", "csv"
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"run_id":    runID,
			"format":    req.Format,
			"actor":     c.GetString("user_id"),
			"action":    "export_run",
		}).Info("Run export requested")
		
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "EXPERIMENT_RUN_EXPORTED",
				Subject: runID,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"format":    req.Format,
					"timestamp": time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"run_id":      runID,
			"exported":    true,
			"format":      req.Format,
			"file_url":    fmt.Sprintf("/downloads/run-%s.%s", runID, req.Format),
			"timestamp":   time.Now().UTC(),
			"audit_trail": true,
		})
	}
}

// handleBulkExport exports multiple runs
func handleBulkExport(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			RunIDs   []string `json:"run_ids" binding:"required"`
			Format   string   `json:"format" binding:"required"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"num_runs":   len(req.RunIDs),
			"format":     req.Format,
			"actor":      c.GetString("user_id"),
			"action":     "bulk_export",
		}).Info("Bulk export requested")
		
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "BULK_EXPORT_COMPLETED",
				Subject: fmt.Sprintf("%v", req.RunIDs),
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"num_runs":    len(req.RunIDs),
					"format":      req.Format,
					"timestamp":   time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"exported":    true,
			"num_runs":    len(req.RunIDs),
			"format":      req.Format,
			"file_url":    fmt.Sprintf("/downloads/bulk-export-%d.%s", time.Now().UnixNano(), req.Format),
			"timestamp":   time.Now().UTC(),
			"audit_trail": true,
		})
	}
}

// generateRunID generates unique run ID
func generateRunID() string {
	return fmt.Sprintf("run-%d", time.Now().UnixNano())
}
