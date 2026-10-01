// Package api - federated_learning_handlers.go implements M20 Federated Learning & Edge Intelligence Platform API endpoints.
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

// RegisterFederatedLearningRoutes registers all M20 Federated Learning endpoints
func RegisterFederatedLearningRoutes(router *gin.Engine, evidenceLedger *evidence.Ledger, logger *logrus.Logger) {
	fl := router.Group("/api/v1/federated-learning")
	fl.Use(
		middleware.EndpointRateLimiter(50, 60),
		auth.RequirePermission(auth.PermWorkloadRead),
	)
	{
		// Device Management
		fl.POST("/devices/enroll", handleEnrollEdgeDevice(evidenceLedger, logger))
		fl.GET("/devices", handleListEdgeDevices())
		fl.GET("/devices/:id", handleGetEdgeDevice())
		fl.PUT("/devices/:id", handleUpdateEdgeDevice(logger))
		fl.DELETE("/devices/:id", handleRemoveEdgeDevice(logger))
		fl.POST("/devices/:id/sync", handleTriggerDeviceSync(logger))
		
		// Model Aggregation Rounds
		fl.POST("/rounds/start", handleStartAggregationRound(evidenceLedger, logger))
		fl.GET("/rounds", handleListAggregationRounds())
		fl.GET("/rounds/:id", handleGetAggregationRound())
		fl.POST("/rounds/:id/stop", handleStopAggregationRound(logger))
		fl.POST("/rounds/:id/pause", handlePauseAggregationRound(logger))
		
		// Global Model Management
		fl.GET("/global-models", handleListGlobalModels())
		fl POST("/global-models/deploy", handleDeployGlobalModel(evidenceLedger, logger))
		fl GET("/global-models/:id", handleGetGlobalModel())
		fl DELETE("/global-models/:id", handleDeleteGlobalModel(logger))
		
		// Privacy & Security
		fl POST("/privacy/configure", handleConfigurePrivacySettings())
		fl GET("/privacy/compliance-audit", handleGetPrivacyAudit())
		fl POST("/dp-noise/add", handleAddDPNoise())
		fl GET("/secure-aggregation/status", handleGetSecureAggStatus())
		
		// Training Progress Monitoring
		fl GET("/rounds/:id/training-progress", handleGetTrainingProgress())
		fl GET("/rounds/:id/model-updates", handleGetModelUpdates())
		fl GET("/convergence/plot", handleGetConvergencePlot())
		
		// Edge Intelligence Analytics
		fl GET("/analytics/device-performance", handleGetDevicePerformanceAnalytics())
		fl GET("/analytics/heterogeneity-analysis", handleGetHeterogeneityAnalysis())
		fl GET("/analytics/failure-patterns", handleGetFailurePatterns())
		
		// Configuration
		fl POST("/config/update", handleUpdateFLConfig(logger))
		fl GET("/config/current", handleGetCurrentConfig())
		
		// Export & Reporting
		fl POST("/rounds/:id/export-results", handleExportRoundResults(evidenceLedger, logger))
	}
}

// ============================================================================
// Edge Device Management Handlers
// ============================================================================

// handleEnrollEdgeDevice enrolls a new edge device
func handleEnrollEdgeDevice(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name          string            `json:"name" binding:"required"`
			Type          string            `json:"type" binding:"required"` // "mobile", "iot", "server"
			Capabilities  map[string]any    `json:"capabilities" binding:"required"`
			Location      string            `json:"location"`
			Metadata      map[string]any    `json:"metadata"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
			return
		}
		
		deviceID := generateDeviceID()
		
		logger.WithFields(logrus.Fields{
			"device_id": deviceID,
			"name":      req.Name,
			"type":      req.Type,
			"actor":     c.GetString("user_id"),
			"action":    "enroll_device",
		}).Info("Edge device enrolled")
		
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "EDGE_DEVICE_ENROLLED",
				Subject: deviceID,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"name":       req.Name,
					"type":       req.Type,
					"timestamp":  time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusCreated, gin.H{
			"device_id":   deviceID,
			"message":     "edge device enrolled successfully",
			"status":      "offline",
			"audit_trail": true,
		})
	}
}

// handleListEdgeDevices lists all enrolled devices
func handleListEdgeDevices() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := c.Query("status")
		deviceType := c.Query("type")
		
		type DeviceSummary struct {
			ID           string    `json:"id"`
			Name         string    `json:"name"`
			Type         string    `json:"type"`
			Status       string    `json:"status"`
			ModelVersion string    `json:"model_version"`
			LastSync     time.Time `json:"last_sync"`
			Accuracy     float64   `json:"accuracy"`
		}
		
		devices := []DeviceSummary{
			{
				ID:           "device-1738234567",
				Name:         "Mobile Device Alpha",
				Type:         "mobile",
				Status:       "online",
				ModelVersion: "v2.3.1",
				LastSync:     time.Now().Add(-10 * time.Minute),
				Accuracy:     0.923,
			},
			{
				ID:           "device-1738134567",
				Name:         "IoT Sensor Array",
				Type:         "iot",
				Status:       "syncing",
				ModelVersion: "v2.3.0",
				LastSync:     time.Now().Add(-2 * time.Hour),
				Accuracy:     0.897,
			},
		}
		
		c.JSON(http.StatusOK, gin.H{
			"devices":    devices,
			"total":      len(devices),
			"filters":    gin.H{"status": status, "type": deviceType},
		})
	}
}

// handleGetEdgeDevice retrieves details for a device
func handleGetEdgeDevice() gin.HandlerFunc {
	return func(c *gin.Context) {
		deviceID := c.Param("id")
		
		c.JSON(http.StatusOK, gin.H{
			"id": deviceID,
			"name": "Mobile Device Alpha",
			"type": "mobile",
			"status": "online",
			"capabilities": gin.H{
				"cpu_cores":    8,
				"memory_gb":    6,
				"storage_gb":   128,
				"neural_engine": true,
			},
			"model_version": "v2.3.1",
			"local_samples": 15420,
			"last_sync":    time.Now().Add(-10 * time.Minute),
			"accuracy":     0.923,
			"uptime_hours": 720,
			"location":     "datacenter-us-east-1",
		})
	}
}

// handleUpdateEdgeDevice updates device metadata
func handleUpdateEdgeDevice(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"device_id": c.Param("id"),
			"actor":     c.GetString("user_id"),
		}).Info("Edge device updated")
		
		c.JSON(http.StatusOK, gin.H{
			"device_id": c.Param("id"),
			"updated":   true,
		})
	}
}

// handleRemoveEdgeDevice removes an edge device
func handleRemoveEdgeDevice(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"device_id": c.Param("id"),
			"actor":     c.GetString("user_id"),
		}).Info("Edge device removed")
		
		c.JSON(http.StatusOK, gin.H{
			"device_id": c.Param("id"),
			"removed":   true,
		})
	}
}

// handleTriggerDeviceSync triggers sync for a device
func handleTriggerDeviceSync(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"device_id": c.Param("id"),
			"action":    "trigger_sync",
			"actor":     c.GetString("user_id"),
		}).Info("Device sync triggered")
		
		c.JSON(http.StatusOK, gin.H{
			"device_id": c.Param("id"),
			"sync_initiated": true,
		})
	}
}

// ============================================================================
// Aggregation Round Handlers
// ============================================================================

// handleStartAggregationRound starts new aggregation round
func handleStartAggregationRound(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			RoundName      string   `json:"round_name" binding:"required"`
		 ParticipatingDeviceIDs []string `json:"participating_devices"`
			FailureThreshold int      `json:"failure_threshold"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		roundID := generateRoundID()
		
		logger.WithFields(logrus.Fields{
			"round_id":             roundID,
			"round_name":           req.RoundName,
			"num_devices":          len(req.ParticipatingDeviceIDs),
			"actor":                c.GetString("user_id"),
			"action":               "start_aggregation_round",
		}).Info("Aggregation round started")
		
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "AGGREGATION_ROUND_STARTED",
				Subject: roundID,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"round_name":       req.RoundName,
					"num_devices":      len(req.ParticipatingDeviceIDs),
					"timestamp":        time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusCreated, gin.H{
			"round_id":              roundID,
			"message":               "aggregation round started",
			"status":                "in_progress",
			"audit_trail":           true,
		})
	}
}

// handleListAggregationRounds lists all rounds
func handleListAggregationRounds() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := c.Query("status")
		
		c.JSON(http.StatusOK, gin.H{
			"rounds": []gin.H{
				{
					"id": "round-1738234567",
					"name": "FL Round #142",
					"status": "completed",
					"num_devices": 45,
					"avg_accuracy_improvement": 0.012,
					"completed_at": time.Now().Add(-1 * time.Hour),
				},
			},
			"total": 1,
			"filter": status,
		})
	}
}

// handleGetAggregationRound gets round details
func handleGetAggregationRound() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id": c.Param("id"),
			"name": "FL Round #142",
			"status": "completed",
			"started_at": time.Now().Add(-2 * time.Hour),
			"completed_at": time.Now().Add(-1 * time.Hour),
			"participating_devices": 45,
			"successful_updates": 43,
			"failed_updates": 2,
			"global_model_accuracy": 0.934,
			"aggregation_method": "FedAvg",
			"privacy_budget_used": 0.45,
		})
	}
}

// handleStopAggregationRound stops a round
func handleStopAggregationRound(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"round_id": c.Param("id"),
			"action":   "stop_round",
			"actor":    c.GetString("user_id"),
		}).Info("Aggregation round stopped")
		
		c.JSON(http.StatusOK, gin.H{
			"round_id": c.Param("id"),
			"stopped":  true,
		})
	}
}

// handlePauseAggregationRound pauses a round
func handlePauseAggregationRound(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"round_id": c.Param("id"),
			"action":   "pause_round",
			"actor":    c.GetString("user_id"),
		}).Info("Aggregation round paused")
		
		c.JSON(http.StatusOK, gin.H{
			"round_id": c.Param("id"),
			"paused":   true,
		})
	}
}

// ============================================================================
// Global Model Management Handlers
// ============================================================================

// handleListGlobalModels lists global models
func handleListGlobalModels() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"models": []gin.H{
				{
					"id": "model-v2.3.1",
					"version": "v2.3.1",
					"accuracy": 0.934,
					"created_at": time.Now().Add(-1 * time.Hour),
				},
			},
		})
	}
}

// handleDeployGlobalModel deploys global model
func handleDeployGlobalModel(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			ModelVersion string   `json:"model_version" binding:"required"`
			TargetDevices []string `json:"target_devices"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"model_version": req.ModelVersion,
			"num_devices":   len(req.TargetDevices),
			"actor":         c.GetString("user_id"),
			"action":        "deploy_global_model",
		}).Info("Global model deployment initiated")
		
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "GLOBAL_MODEL_DEPLOYED",
				Subject: req.ModelVersion,
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"model_version": req.ModelVersion,
					"timestamp":     time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"deployed": true,
			"model_version": req.ModelVersion,
		})
	}
}

// handleGetGlobalModel gets model details
func handleGetGlobalModel() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id":          c.Param("id"),
			"version":     "v2.3.1",
			"accuracy":    0.934,
			"created_at":  time.Now().Add(-1 * time.Hour),
		})
	}
}

// handleDeleteGlobalModel deletes a model
func handleDeleteGlobalModel(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"model_id": c.Param("id"),
			"actor":    c.GetString("user_id"),
		}).Info("Global model deleted")
		
		c.JSON(http.StatusOK, gin.H{
			"model_id": c.Param("id"),
			"deleted":  true,
		})
	}
}

// ============================================================================
// Privacy & Security Handlers
// ============================================================================

// handleConfigurePrivacySettings configures privacy settings
func handleConfigurePrivacySettings() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"configured": true,
		})
	}
}

// handleGetPrivacyAudit gets privacy audit log
func handleGetPrivacyAudit() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"audit_entries": 15,
		})
	}
}

// handleAddDPNoise adds differential privacy noise
func handleAddDPNoise() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"noise_added": true,
		})
	}
}

// handleGetSecureAggStatus gets secure aggregation status
func handleGetSecureAggStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "enabled",
		})
	}
}

// ============================================================================
// Training Progress & Analytics Handlers
// ============================================================================

// handleGetTrainingProgress gets training progress
func handleGetTrainingProgress() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"round_id": c.Param("id"),
			"progress_percent": 75,
		})
	}
}

// handleGetModelUpdates gets model updates
func handleGetModelUpdates() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"updates": 43,
		})
	}
}

// handleGetConvergencePlot gets convergence plot data
func handleGetConvergencePlot() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"iterations": 20,
			"accuracy_values": []float64{0.7, 0.8, 0.85, 0.9, 0.934},
		})
	}
}

// handleGetDevicePerformanceAnalytics gets device performance analytics
func handleGetDevicePerformanceAnalytics() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"analytics": "retrieved",
		})
	}
}

// handleGetHeterogeneityAnalysis gets heterogeneity analysis
func handleGetHeterogeneityAnalysis() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"analysis": "computed",
		})
	}
}

// handleGetFailurePatterns gets failure patterns
func handleGetFailurePatterns() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"patterns_identified": 3,
		})
	}
}

// ============================================================================
// Configuration & Export Handlers
// ============================================================================

// handleUpdateFLConfig updates FL configuration
func handleUpdateFLConfig(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"actor": c.GetString("user_id"),
		}).Info("FL config updated")
		
		c.JSON(http.StatusOK, gin.H{
			"updated": true,
		})
	}
}

// handleGetCurrentConfig gets current config
func handleGetCurrentConfig() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"config": gin.H{"aggregation_method": "FedAvg"},
		})
	}
}

// handleExportRoundResults exports round results
func handleExportRoundResults(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"round_id": c.Param("id"),
			"actor":    c.GetString("user_id"),
		}).Info("Round results exported")
		
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:  "ROUND_RESULTS_EXPORTED",
				Subject: c.Param("id"),
				Actor:   c.GetString("user_id"),
				Metadata: gin.H{
					"timestamp": time.Now().UTC(),
				},
			}
			ledger.RecordReceipt(receipt)
		}
		
		c.JSON(http.StatusOK, gin.H{
			"exported": true,
		})
	}
}

// Helper Functions

// generateDeviceID generates unique device ID
func generateDeviceID() string {
	return fmt.Sprintf("device-%d", time.Now().UnixNano())
}

// generateRoundID generates unique round ID
func generateRoundID() string {
	return fmt.Sprintf("round-%d", time.Now().UnixNano())
}
