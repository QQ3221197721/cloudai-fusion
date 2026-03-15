package scheduler

import (
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// ============================================================================
// HTTP Handlers
// ============================================================================

// HandleStatus returns the scheduler engine status.
func (e *Engine) HandleStatus(c *gin.Context) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"status":        "running",
		"policy":        e.policy,
		"queue_length":  len(e.queue),
		"running_count": len(e.running),
		"ready":         e.ready,
	})
}

// HandleQueue returns the current scheduling queue.
func (e *Engine) HandleQueue(c *gin.Context) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"queue": e.queue,
		"total": len(e.queue),
	})
}

// HandleScheduleRequest accepts a workload scheduling request.
func (e *Engine) HandleScheduleRequest(c *gin.Context) {
	var req ScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	e.mu.Lock()
	req.Workload.ID = common.NewUUID()
	req.Workload.Status = common.WorkloadStatusQueued
	req.Workload.QueuedAt = common.NowUTC()
	e.queue = append(e.queue, &req.Workload)
	e.mu.Unlock()

	e.persistQueuedWorkload(&req.Workload)

	c.JSON(http.StatusAccepted, gin.H{
		"message":     "workload queued for scheduling",
		"workload_id": req.Workload.ID,
	})
}

// persistQueuedWorkload saves a newly queued workload to the database.
func (e *Engine) persistQueuedWorkload(wl *Workload) {
	if e.store == nil {
		return
	}
	dbModel := &store.WorkloadModel{
		ID:               wl.ID,
		Name:             wl.Name,
		Namespace:        wl.Namespace,
		ClusterID:        wl.ClusterID,
		Type:             string(wl.Type),
		Status:           string(wl.Status),
		Priority:         wl.Priority,
		Framework:        wl.Framework,
		ModelName:        wl.ModelName,
		GPUCountRequired: wl.ResourceRequest.GPUCount,
		CreatedAt:        wl.QueuedAt,
		UpdatedAt:        wl.QueuedAt,
	}
	if err := e.store.CreateWorkload(dbModel); err != nil {
		e.logger.WithError(err).Warn("Failed to persist queued workload to database")
	}
}

// HandlePreemptRequest handles workload preemption.
func (e *Engine) HandlePreemptRequest(c *gin.Context) {
	var req struct {
		WorkloadID string `json:"workload_id" binding:"required"`
		Reason     string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	workload, ok := e.running[req.WorkloadID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "workload not found in running set"})
		return
	}

	workload.Status = common.WorkloadStatusPreempted
	delete(e.running, req.WorkloadID)

	c.JSON(http.StatusOK, gin.H{
		"message":  "workload preempted",
		"workload": workload,
	})
}

// HandleGPUTopology returns GPU topology information.
func (e *Engine) HandleGPUTopology(c *gin.Context) {
	topo, err := e.topology.DiscoverTopology(c.Request.Context(), "local")
	if err == nil && topo.TotalGPUs > 0 {
		commonTopo := topo.ConvertToCommonTopology()
		c.JSON(http.StatusOK, gin.H{"topology": []common.GPUTopologyInfo{commonTopo}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"topology": simulatedTopology()})
}

// HandleUtilization returns cluster GPU utilization from the best available source.
func (e *Engine) HandleUtilization(c *gin.Context) {
	e.mu.RLock()
	runCount := len(e.running)
	queueCount := len(e.queue)
	e.mu.RUnlock()

	if data, ok := e.utilizationFromTopology(c); ok {
		data["running_workloads"] = runCount
		data["queued_workloads"] = queueCount
		c.JSON(http.StatusOK, data)
		return
	}

	if data, ok := e.utilizationFromK8s(c); ok {
		data["running_workloads"] = runCount
		data["queued_workloads"] = queueCount
		c.JSON(http.StatusOK, data)
		return
	}

	// Fallback: simulated data
	c.JSON(http.StatusOK, gin.H{
		"cluster_utilization": gin.H{
			"total_gpu": 16, "used_gpu": 11, "gpu_util_pct": 68.75,
			"total_cpu": 384000, "used_cpu": 245000, "cpu_util_pct": 63.8,
			"total_mem_gb": 3072, "used_mem_gb": 2150, "mem_util_pct": 69.9,
		},
		"running_workloads": runCount,
		"queued_workloads":  queueCount,
		"source":            "simulated",
	})
}

// utilizationFromTopology attempts to get GPU utilization via nvidia-smi topology.
func (e *Engine) utilizationFromTopology(c *gin.Context) (gin.H, bool) {
	topo, err := e.topology.DiscoverTopology(c.Request.Context(), "local")
	if err != nil || topo.TotalGPUs == 0 {
		return nil, false
	}

	totalGPU := topo.TotalGPUs
	usedGPU := 0
	totalUtil := 0.0
	for _, g := range topo.GPUs {
		totalUtil += g.Utilization
		if g.Utilization > 5 {
			usedGPU++
		}
	}
	avgUtil := 0.0
	if totalGPU > 0 {
		avgUtil = totalUtil / float64(totalGPU)
	}

	return gin.H{
		"cluster_utilization": gin.H{
			"total_gpu":    totalGPU,
			"used_gpu":     usedGPU,
			"gpu_util_pct": math.Round(avgUtil*100) / 100,
		},
		"gpu_details":      topo.GPUs,
		"nvlink_available": topo.HasNVLink,
		"source":           "nvidia-smi",
	}, true
}

// utilizationFromK8s attempts to get GPU utilization via K8s API.
func (e *Engine) utilizationFromK8s(c *gin.Context) (gin.H, bool) {
	if e.k8sClient == nil {
		return nil, false
	}
	nodes, err := e.k8sClient.GetNodeResources(c.Request.Context())
	if err != nil || len(nodes) == 0 {
		return nil, false
	}

	totalGPU, usedGPU := 0, 0
	for _, n := range nodes {
		alloc, _ := strconv.Atoi(n.GPUAllocatable)
		cap, _ := strconv.Atoi(n.GPUCapacity)
		totalGPU += cap
		usedGPU += cap - alloc
	}
	gpuUtil := 0.0
	if totalGPU > 0 {
		gpuUtil = float64(usedGPU) / float64(totalGPU) * 100
	}

	return gin.H{
		"cluster_utilization": gin.H{
			"total_gpu":    totalGPU,
			"used_gpu":     usedGPU,
			"gpu_util_pct": math.Round(gpuUtil*100) / 100,
		},
		"source": "k8s-api",
	}, true
}

// HandleUpdatePolicy updates the scheduling policy.
func (e *Engine) HandleUpdatePolicy(c *gin.Context) {
	var policy SchedulingPolicy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	e.mu.Lock()
	e.policy = &policy
	e.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "scheduling policy updated",
		"policy":  policy,
	})
}

// HandleRLStats returns the RL optimizer statistics.
func (e *Engine) HandleRLStats(c *gin.Context) {
	stats := e.rlOptimizer.GetStatistics()
	c.JSON(http.StatusOK, gin.H{"rl_optimizer": stats})
}

// HandleGPUSharing returns GPU sharing state and available MIG profiles.
func (e *Engine) HandleGPUSharing(c *gin.Context) {
	states := e.gpuSharing.GetGPUSharingStates()
	c.JSON(http.StatusOK, gin.H{
		"gpu_sharing": states,
		"mig_profiles": gin.H{
			"a100-40gb": SupportedMIGProfiles("a100"),
			"a100-80gb": SupportedMIGProfiles("a100-80"),
			"h100":      SupportedMIGProfiles("h100"),
		},
		"memory_oversell": e.gpuSharing.GetGPUMemoryStates(),
	})
}

// HandleElasticInference returns elastic inference endpoints and scaling status.
func (e *Engine) HandleElasticInference(c *gin.Context) {
	endpoints := e.elasticInference.GetEndpoints()
	c.JSON(http.StatusOK, gin.H{
		"endpoints": endpoints,
		"total":     len(endpoints),
		"config":    e.elasticInference.config,
	})
}

// HandlePredictiveScaling returns load forecasts and scaling recommendations.
func (e *Engine) HandlePredictiveScaling(c *gin.Context) {
	forecasts := e.predictiveScaler.GetLastForecast()
	history := e.predictiveScaler.GetHistory(48)

	var recommendation *ScalingRecommendation
	if rec, err := e.predictiveScaler.GetScalingRecommendation(len(e.running), 64); err == nil {
		recommendation = rec
	}

	c.JSON(http.StatusOK, gin.H{
		"forecasts":      forecasts,
		"history_points": len(history),
		"recommendation": recommendation,
		"config":         e.predictiveScaler.config,
	})
}

// HandleCostOptimization returns cost analysis and optimization recommendations.
func (e *Engine) HandleCostOptimization(c *gin.Context) {
	report := e.costOptimizer.GenerateCostReport()
	pricing := e.costOptimizer.GetPricingDB()
	c.JSON(http.StatusOK, gin.H{
		"cost_report":       report,
		"instance_pricing":  pricing,
		"allocations_count": len(e.costOptimizer.GetAllocations()),
		"config":            e.costOptimizer.config,
	})
}

// HandleFederation returns multi-cluster federation status.
func (e *Engine) HandleFederation(c *gin.Context) {
	status := e.federation.GetFederationStatus()
	clusters := e.federation.GetClusters()
	placements := e.federation.GetRecentPlacements(20)
	c.JSON(http.StatusOK, gin.H{
		"federation_status": status,
		"clusters":          clusters,
		"recent_placements": placements,
		"config":            e.federation.config,
	})
}

// HandleQueueManagement returns advanced queue status.
func (e *Engine) HandleQueueManagement(c *gin.Context) {
	queueStatus := e.queueManager.GetQueueStatus()
	c.JSON(http.StatusOK, gin.H{
		"queue_status": queueStatus,
		"config":       e.queueManager.config,
	})
}

// HandlePluginStatus returns the status of all registered scheduler plugins.
func (e *Engine) HandlePluginStatus(c *gin.Context) {
	if e.pluginRegistry == nil {
		c.JSON(http.StatusOK, gin.H{"plugins": []interface{}{}})
		return
	}
	plugins := e.pluginRegistry.ListAll()
	statuses := make([]gin.H, 0, len(plugins))
	for _, p := range plugins {
		meta := p.Metadata()
		statuses = append(statuses, gin.H{
			"name":            meta.Name,
			"version":         meta.Version,
			"description":     meta.Description,
			"extensionPoints": meta.ExtensionPoints,
			"priority":        meta.Priority,
			"dependencies":    meta.Dependencies,
			"tags":            meta.Tags,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"plugins":         statuses,
		"total":           len(statuses),
		"extensionPoints": e.pluginRegistry.ExtensionPoints(),
	})
}

// ============================================================================
// Simulated Data (fallbacks for development)
// ============================================================================

func simulatedTopology() []common.GPUTopologyInfo {
	return []common.GPUTopologyInfo{
		{
			NodeName: "gpu-node-01",
			GPUDevices: []common.GPUDevice{
				{Index: 0, Name: "NVIDIA A100-SXM4-80GB", Type: common.GPUTypeNvidiaA100, MemoryBytes: 85899345920, Utilization: 45.2},
				{Index: 1, Name: "NVIDIA A100-SXM4-80GB", Type: common.GPUTypeNvidiaA100, MemoryBytes: 85899345920, Utilization: 38.7},
				{Index: 2, Name: "NVIDIA A100-SXM4-80GB", Type: common.GPUTypeNvidiaA100, MemoryBytes: 85899345920, Utilization: 52.1},
				{Index: 3, Name: "NVIDIA A100-SXM4-80GB", Type: common.GPUTypeNvidiaA100, MemoryBytes: 85899345920, Utilization: 0},
			},
			NVLinkPairs: []common.NVLinkPair{
				{GPU1Index: 0, GPU2Index: 1, Bandwidth: 600, LinkVersion: 3},
				{GPU1Index: 2, GPU2Index: 3, Bandwidth: 600, LinkVersion: 3},
			},
		},
	}
}

// ============================================================================
// Unused import guard — logrus is used by persistQueuedWorkload via e.logger
// ============================================================================

var _ *logrus.Logger
