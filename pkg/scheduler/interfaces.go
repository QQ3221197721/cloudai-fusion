package scheduler

import (
	"context"

	"github.com/gin-gonic/gin"
)

// Scheduler defines the contract for the AI resource scheduling engine.
// Consumers depend on this interface rather than the concrete *Engine,
// enabling mock implementations for unit testing and pluggable scheduling strategies.
type Scheduler interface {
	// Lifecycle
	Run(ctx context.Context)
	Stop()
	IsReady() bool

	// HTTP Handlers (Gin)
	HandleScheduleRequest(c *gin.Context)
	HandleStatus(c *gin.Context)
	HandleQueue(c *gin.Context)
	HandleUtilization(c *gin.Context)
	HandlePreemptRequest(c *gin.Context)
	HandleUpdatePolicy(c *gin.Context)
	HandleGPUTopology(c *gin.Context)
	HandleGPUSharing(c *gin.Context)
	HandleRLStats(c *gin.Context)
	HandlePluginStatus(c *gin.Context)

	// Advanced Scheduling Handlers
	HandleElasticInference(c *gin.Context)
	HandlePredictiveScaling(c *gin.Context)
	HandleCostOptimization(c *gin.Context)
	HandleFederation(c *gin.Context)
	HandleQueueManagement(c *gin.Context)
}

// Compile-time interface satisfaction check.
var _ Scheduler = (*Engine)(nil)
