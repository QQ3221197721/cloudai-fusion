package scheduler

import (
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Scheduling Models
// ============================================================================

// SchedulingPolicy defines how workloads are scheduled.
type SchedulingPolicy struct {
	Name                string  `json:"name"`
	PreemptionEnabled   bool    `json:"preemption_enabled"`
	GPUShareEnabled     bool    `json:"gpu_share_enabled"`
	TopologyAware       bool    `json:"topology_aware"`
	CostOptimize        bool    `json:"cost_optimize"`
	MaxGPUUtilization   float64 `json:"max_gpu_utilization"`
	SpotInstanceEnabled bool    `json:"spot_instance_enabled"`
	FairnessWeight      float64 `json:"fairness_weight"`
	EfficiencyWeight    float64 `json:"efficiency_weight"`
}

// Workload represents an AI workload to be scheduled.
type Workload struct {
	ID              string                  `json:"id"`
	Name            string                  `json:"name"`
	Namespace       string                  `json:"namespace"`
	ClusterID       string                  `json:"cluster_id"`
	Type            common.WorkloadType     `json:"type"`
	Status          common.WorkloadStatus   `json:"status"`
	Priority        int                     `json:"priority"`
	Framework       string                  `json:"framework"`
	ModelName       string                  `json:"model_name,omitempty"`
	ResourceRequest common.ResourceRequest  `json:"resource_request"`
	GPUTopologyReq  *GPUTopologyRequirement `json:"gpu_topology_req,omitempty"`
	SchedulingHint  *SchedulingHint         `json:"scheduling_hint,omitempty"`
	Assignment      *Assignment             `json:"assignment,omitempty"`
	QueuedAt        time.Time               `json:"queued_at"`
	StartedAt       *time.Time              `json:"started_at,omitempty"`
	CompletedAt     *time.Time              `json:"completed_at,omitempty"`
	Metrics         *WorkloadMetrics        `json:"metrics,omitempty"`
}

// GPUTopologyRequirement specifies GPU interconnection requirements.
type GPUTopologyRequirement struct {
	RequireNVLink      bool    `json:"require_nvlink"`
	MinNVLinkBandwidth float64 `json:"min_nvlink_bandwidth_gbps"`
	PreferSameNode     bool    `json:"prefer_same_node"`
	GPUAffinityGroup   string  `json:"gpu_affinity_group,omitempty"`
}

// SchedulingHint provides hints to the scheduler.
type SchedulingHint struct {
	PreferredNodes    []string         `json:"preferred_nodes,omitempty"`
	AvoidNodes        []string         `json:"avoid_nodes,omitempty"`
	PreferredGPUTypes []common.GPUType `json:"preferred_gpu_types,omitempty"`
	MaxCostPerHour    float64          `json:"max_cost_per_hour,omitempty"`
	PreferSpot        bool             `json:"prefer_spot"`
	Deadline          *time.Time       `json:"deadline,omitempty"`
}

// Assignment represents the scheduling decision.
type Assignment struct {
	NodeName      string    `json:"node_name"`
	GPUIndices    []int     `json:"gpu_indices"`
	GPUShareRatio float64   `json:"gpu_share_ratio,omitempty"`
	Score         float64   `json:"score"`
	Reason        string    `json:"reason"`
	AssignedAt    time.Time `json:"assigned_at"`
}

// WorkloadMetrics holds runtime metrics for a workload.
type WorkloadMetrics struct {
	GPUUtilization    float64 `json:"gpu_utilization_percent"`
	GPUMemoryUsage    float64 `json:"gpu_memory_usage_percent"`
	ThroughputSamples float64 `json:"throughput_samples_per_sec"`
	LossValue         float64 `json:"loss_value,omitempty"`
	Epoch             int     `json:"epoch,omitempty"`
	EstimatedETA      string  `json:"estimated_eta,omitempty"`
}

// ScheduleRequest is the API request to schedule a workload.
type ScheduleRequest struct {
	Workload Workload `json:"workload" binding:"required"`
}

// NodeScore represents a candidate node with its scheduling score.
type NodeScore struct {
	NodeName        string  `json:"node_name"`
	ClusterID       string  `json:"cluster_id"`
	Score           float64 `json:"score"`
	GPUFreeCount    int     `json:"gpu_free_count"`
	GPUType         string  `json:"gpu_type"`
	GPUUtilization  float64 `json:"gpu_utilization"`
	CostPerHour     float64 `json:"cost_per_hour"`
	TopologyScore   float64 `json:"topology_score"`
	AvailableMemory int64   `json:"available_memory_bytes"`
}
