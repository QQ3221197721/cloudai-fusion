// Package api - inference_pool_handlers.go implements M12 Elastic Inference Pool API endpoints.
// Provides lifecycle management for GPU resource pools, auto-scaling operations,
// cost tracking, and health monitoring for elastic inference workloads.
package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/elasticpool"
	"github.com/sirupsen/logrus"
)

// RegisterInferencePoolRoutes registers all M12 Elastic Inference Pool endpoints
func RegisterInferencePoolRoutes(router *gin.Engine, poolMgr interface{}, ledger *evidence.Ledger, logger *logrus.Logger) {
	pools := router.Group("/api/v1/inference/pools")
	{
		pools.GET("", handleListPools(poolMgr))
		pools.POST("", handleCreatePool(poolMgr, ledger, logger))
		pools.GET("/:id", handleGetPool(poolMgr))
		pools.PUT("/:id", handleUpdatePool(poolMgr, ledger, logger))
		pools.DELETE("/:id", handleDeletePool(poolMgr, ledger, logger))
		
		pools.POST("/:id/scale", handleScalePool(poolMgr, ledger, logger))
		pools.GET("/:id/scaling-history", handleGetScalingHistory(poolMgr))
		
		pools.GET("/:id/health", handleGetPoolHealth(poolMgr))
		pools.POST("/:id/failover-test", handleFailoverTest(poolMgr, ledger, logger))
		
		pools.GET("/:id/cost-report", handleGetCostReport(poolMgr))
	}
	
	endpoints := router.Group("/api/v1/inference/endpoints")
	{
		endpoints.GET("", handleListEndpoints())
		endpoints.GET("/:id/metrics", handleGetEndpointMetrics())
	}
}

// handleListPools returns all inference pools with filtering support
func handleListPools(mgr interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		statusFilter := c.Query("status")
		typeFilter := c.Query("type")
		
		// Type assertion for actual manager type
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		pools, err := poolMgr.ListPools(statusFilter, typeFilter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"pools":   pools,
			"total":   len(pools),
			"filters": gin.H{"status": statusFilter, "type": typeFilter},
		})
	}
}

// handleCreatePool creates a new inference pool
func handleCreatePool(mgr interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name             string                              `json:"name" binding:"required"`
			Description      string                              `json:"description"`
			MinInstances     int                                 `json:"min_instances" binding:"required,min=1"`
			MaxInstances     int                                 `json:"max_instances" binding:"required,gtefield=min_instances"`
			InstanceType     string                              `json:"instance_type"`
			AutoScaling      elasticpool.AutoScalingConfig       `json:"auto_scaling"`
			SLARequirements  map[string]interface{}              `json:"sla_requirements"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
			return
		}
		
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		pool, err := poolMgr.CreatePool(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create pool", "details": err.Error()})
			return
		}
		
		// Record evidence receipt (T1 requirement)
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "INFERENCE_POOL_CREATED",
				Subject:   pool.ID,
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
				InputHash: sha256Hash(fmt.Sprintf("%s:%s", req.Name, req.InstanceType)),
				Metadata: gin.H{
					"name":            req.Name,
					"description":     req.Description,
					"min_instances":   req.MinInstances,
					"max_instances":   req.MaxInstances,
					"instance_type":   req.InstanceType,
				},
			}
			
			if attestErr := ledger.RecordReceipt(receipt); attestErr != nil {
				logger.WithError(attestErr).Warn("Failed to record pool creation evidence (non-critical)")
			}
		}
		
		c.JSON(http.StatusCreated, gin.H{
			"pool":      pool,
			"message":   "pool created successfully",
			"created_at": time.Now().UTC(),
		})
	}
}

// handleGetPool retrieves a specific inference pool by ID
func handleGetPool(mgr interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		pool, err := poolMgr.GetPool(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("pool %s not found", c.Param("id"))})
			return
		}
		
		c.JSON(http.StatusOK, pool)
	}
}

// handleUpdatePool updates an existing inference pool configuration
func handleUpdatePool(mgr interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			MinInstances int `json:"min_instances"`
			MaxInstances int `json:"max_instances"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		pool, err := poolMgr.UpdatePool(c.Param("id"), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update pool"})
			return
		}
		
		// Record evidence receipt (T1 requirement)
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "INFERENCE_POOL_UPDATED",
				Subject:   c.Param("id"),
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
				Metadata: gin.H{
					"min_instances": req.MinInstances,
					"max_instances": req.MaxInstances,
				},
			}
			
			if attestErr := ledger.RecordReceipt(receipt); attestErr != nil {
				logger.WithError(attestErr).Warn("Failed to record pool update evidence (non-critical)")
			}
		}
		
		c.JSON(http.StatusOK, gin.H{
			"pool":      pool,
			"updated_at": time.Now().UTC(),
		})
	}
}

// handleDeletePool deletes an inference pool
func handleDeletePool(mgr interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		poolID := c.Param("id")
		if err := poolMgr.DeletePool(poolID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete pool"})
			return
		}
		
		// Record evidence receipt (T1 requirement)
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "INFERENCE_POOL_DELETED",
				Subject:   poolID,
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
			}
			
			if attestErr := ledger.RecordReceipt(receipt); attestErr != nil {
				logger.WithError(attestErr).Warn("Failed to record pool deletion evidence (non-critical)")
			}
		}
		
		c.JSON(http.StatusOK, gin.H{
			"message":   "pool deleted successfully",
			"pool_id":   poolID,
		})
	}
}

// handleScalePool scales an existing pool up or down
func handleScalePool(mgr interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		
		var req struct {
			TargetInstances int    `json:"target_instances" binding:"required,min=1"`
			Reason          string `json:"reason,omitempty"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		scaleResult, err := poolMgr.ScalePool(id, req.TargetInstances)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "scaling failed", "details": err.Error()})
			return
		}
		
		action := "scaled_down"
		if req.TargetInstances > scaleResult.PreviousReplicas {
			action = "scaled_up"
		}
		
		// Record evidence receipt (T1 requirement)
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "INFERENCE_POOL_SCALING_TRIGGERED",
				Subject:   id,
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
				InputHash: sha256Hash(fmt.Sprintf("%s:%d", id, req.TargetInstances)),
				Metadata: gin.H{
					"previous_replicas": scaleResult.PreviousReplicas,
					"current_replicas":  scaleResult.CurrentReplicas,
					"action":            action,
					"reason":            req.Reason,
				},
			}
			
			if attestErr := ledger.RecordReceipt(receipt); attestErr != nil {
				logger.WithError(attestErr).Warn("Failed to record scaling event evidence (non-critical)")
			}
		}
		
		c.JSON(http.StatusOK, gin.H{
			"pool_id":           id,
			"previous_replicas": scaleResult.PreviousReplicas,
			"current_replicas":  scaleResult.CurrentReplicas,
			"action":            action,
			"reason":            req.Reason,
		})
	}
}

// handleGetScalingHistory retrieves scaling history for a pool
func handleGetScalingHistory(mgr interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		limit := 50
		if l := c.Query("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		
		history, err := poolMgr.GetScalingHistory(c.Param("id"), limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve history"})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"pool_id":   c.Param("id"),
			"history":   history,
			"count":     len(history),
			"limit":     limit,
		})
	}
}

// handleGetPoolHealth checks health status of a pool
func handleGetPoolHealth(mgr interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		health, err := poolMgr.GetPoolHealth(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "pool not found"})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"pool_id":      c.Param("id"),
			"healthy":      health.IsHealthy,
			"status":       health.Status,
			"details":      health.Details,
			"last_check":   time.Now().UTC(),
		})
	}
}

// handleFailoverTest triggers a failover test on a pool
func handleFailoverTest(mgr interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		result, err := poolMgr.FailoverTest(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failover test failed", "details": err.Error()})
			return
		}
		
		// Record evidence receipt (T1 requirement)
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "INFERENCE_POOL_FAILOVER_TEST_TRIGGERED",
				Subject:   c.Param("id"),
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
				Metadata: gin.H{
					"test_id":       result.TestID,
					"status":        result.Status,
					"result":        result.Result,
					"duration_ms":   result.Duration,
				},
			}
			
			if attestErr := ledger.RecordReceipt(receipt); attestErr != nil {
				logger.WithError(attestErr).Warn("Failed to record failover test evidence (non-critical)")
			}
		}
		
		c.JSON(http.StatusOK, gin.H{
			"test_id":         result.TestID,
			"status":          result.Status,
			"result":          result.Result,
			"test_duration":   result.Duration,
			"timestamp":       time.Now().UTC(),
		})
	}
}

// handleGetCostReport calculates cost report for a pool
func handleGetCostReport(mgr interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		poolMgr, ok := mgr.(*elasticpool.Manager)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid pool manager"})
			return
		}
		
		startDate := c.DefaultQuery("start_date", "2026-09-01")
		endDate := c.DefaultQuery("end_date", time.Now().Format("2006-01-02"))
		
		report, err := poolMgr.GetCostReport(c.Param("id"), startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate cost report"})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"pool_id":         c.Param("id"),
			"period":          gin.H{"start": startDate, "end": endDate},
			"cost_report":     report,
			"generated_at":    time.Now().UTC(),
		})
	}
}

// handleListEndpoints returns all inference endpoints
func handleListEndpoints() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Placeholder - would integrate with endpoint registry
		endpoints := []gin.H{
			{"id": "ep-001", "name": "production-inference", "status": "active", "pool_id": "pool-001"},
			{"id": "ep-002", "name": "staging-inference", "status": "active", "pool_id": "pool-002"},
		}
		
		c.JSON(http.StatusOK, gin.H{
			"endpoints": endpoints,
			"total":     len(endpoints),
		})
	}
}

// handleGetEndpointMetrics retrieves metrics for a specific endpoint
func handleGetEndpointMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Placeholder - would integrate with metrics collection
		metrics := gin.H{
			"request_count":     15234,
			"avg_latency_ms":    45.2,
			"p99_latency_ms":    128.5,
			"error_rate":        0.02,
			"throughput_rps":    342.5,
			"gpu_utilization":   67.8,
		}
		
		c.JSON(http.StatusOK, gin.H{
			"endpoint_id": c.Param("id"),
			"metrics":     metrics,
			"collection_period": "last_24h",
		})
	}
}