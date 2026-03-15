// Package common provides shared types, utilities, and interfaces
// used across all CloudAI Fusion components.
package common

import (
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// Core Domain Models
// ============================================================================

// ResourceType defines cloud resource categories
type ResourceType string

const (
	ResourceTypeCPU    ResourceType = "cpu"
	ResourceTypeMemory ResourceType = "memory"
	ResourceTypeGPU    ResourceType = "gpu"
	ResourceTypeTPU    ResourceType = "tpu"
	ResourceTypeNPU    ResourceType = "npu"
	ResourceTypeDisk   ResourceType = "disk"
	ResourceTypeNet    ResourceType = "network"
)

// WorkloadType defines AI workload categories
type WorkloadType string

const (
	WorkloadTypeTraining   WorkloadType = "training"
	WorkloadTypeInference  WorkloadType = "inference"
	WorkloadTypeFineTuning WorkloadType = "fine-tuning"
	WorkloadTypeBatch      WorkloadType = "batch"
	WorkloadTypeServing    WorkloadType = "serving"
)

// WorkloadStatus represents the lifecycle state of a workload
type WorkloadStatus string

const (
	WorkloadStatusPending   WorkloadStatus = "pending"
	WorkloadStatusQueued    WorkloadStatus = "queued"
	WorkloadStatusScheduled WorkloadStatus = "scheduled"
	WorkloadStatusRunning   WorkloadStatus = "running"
	WorkloadStatusSucceeded WorkloadStatus = "succeeded"
	WorkloadStatusFailed    WorkloadStatus = "failed"
	WorkloadStatusCancelled WorkloadStatus = "cancelled"
	WorkloadStatusPreempted WorkloadStatus = "preempted"
)

// GPUType defines supported GPU accelerator types
type GPUType string

const (
	GPUTypeNvidiaA100  GPUType = "nvidia-a100"
	GPUTypeNvidiaH100  GPUType = "nvidia-h100"
	GPUTypeNvidiaH200  GPUType = "nvidia-h200"
	GPUTypeNvidiaL40S  GPUType = "nvidia-l40s"
	GPUTypeAMDMI300X   GPUType = "amd-mi300x"
	GPUTypeHuaweiAscend GPUType = "huawei-ascend-910b"
)

// CloudProviderType defines supported cloud providers
type CloudProviderType string

const (
	CloudProviderAliyun  CloudProviderType = "aliyun"
	CloudProviderAWS     CloudProviderType = "aws"
	CloudProviderAzure   CloudProviderType = "azure"
	CloudProviderGCP     CloudProviderType = "gcp"
	CloudProviderHuawei  CloudProviderType = "huawei"
	CloudProviderTencent CloudProviderType = "tencent"
)

// ClusterStatus represents cluster health state
type ClusterStatus string

const (
	ClusterStatusHealthy     ClusterStatus = "healthy"
	ClusterStatusDegraded    ClusterStatus = "degraded"
	ClusterStatusUnreachable ClusterStatus = "unreachable"
	ClusterStatusProvisioning ClusterStatus = "provisioning"
	ClusterStatusDeleting    ClusterStatus = "deleting"
)

// ============================================================================
// Resource Specifications
// ============================================================================

// ResourceRequest specifies resource requirements for a workload
type ResourceRequest struct {
	CPUMillicores   int64  `json:"cpu_millicores"`
	MemoryBytes     int64  `json:"memory_bytes"`
	GPUCount        int    `json:"gpu_count"`
	GPUType         GPUType `json:"gpu_type,omitempty"`
	GPUMemoryBytes  int64  `json:"gpu_memory_bytes,omitempty"`
	StorageBytes    int64  `json:"storage_bytes,omitempty"`
	NetworkBandwidth int64  `json:"network_bandwidth_mbps,omitempty"`
}

// ResourceCapacity represents total available resources
type ResourceCapacity struct {
	TotalCPUMillicores   int64 `json:"total_cpu_millicores"`
	UsedCPUMillicores    int64 `json:"used_cpu_millicores"`
	TotalMemoryBytes     int64 `json:"total_memory_bytes"`
	UsedMemoryBytes      int64 `json:"used_memory_bytes"`
	TotalGPUCount        int   `json:"total_gpu_count"`
	UsedGPUCount         int   `json:"used_gpu_count"`
	TotalGPUMemoryBytes  int64 `json:"total_gpu_memory_bytes"`
	UsedGPUMemoryBytes   int64 `json:"used_gpu_memory_bytes"`
}

// GPUTopologyInfo describes GPU interconnection topology
type GPUTopologyInfo struct {
	NodeName    string            `json:"node_name"`
	GPUDevices  []GPUDevice       `json:"gpu_devices"`
	NVLinkPairs []NVLinkPair      `json:"nvlink_pairs,omitempty"`
	Topology    map[string]string `json:"topology,omitempty"`
}

// GPUDevice represents a single GPU device
type GPUDevice struct {
	Index          int     `json:"index"`
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	Type           GPUType `json:"type"`
	MemoryBytes    int64   `json:"memory_bytes"`
	UsedMemoryBytes int64  `json:"used_memory_bytes"`
	Utilization    float64 `json:"utilization_percent"`
	Temperature    int     `json:"temperature_celsius"`
	PowerUsageWatts float64 `json:"power_usage_watts"`
}

// NVLinkPair describes NVLink connection between GPUs
type NVLinkPair struct {
	GPU1Index   int     `json:"gpu1_index"`
	GPU2Index   int     `json:"gpu2_index"`
	Bandwidth   float64 `json:"bandwidth_gbps"`
	LinkVersion int     `json:"link_version"`
}

// ============================================================================
// API Response Types
// ============================================================================

// APIResponse is the standard API response wrapper
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	TraceID string      `json:"trace_id,omitempty"`
}

// PaginatedResponse wraps paginated list results
type PaginatedResponse struct {
	Items      interface{} `json:"items"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// ============================================================================
// Utility Functions
// ============================================================================

// NewUUID generates a new UUID string
func NewUUID() string {
	return uuid.New().String()
}

// NowUTC returns current time in UTC
func NowUTC() time.Time {
	return time.Now().UTC()
}

// TimePtr returns a pointer to a time.Time
func TimePtr(t time.Time) *time.Time {
	return &t
}

// StringPtr returns a pointer to a string
func StringPtr(s string) *string {
	return &s
}

// IntPtr returns a pointer to an int
func IntPtr(i int) *int {
	return &i
}

// CalculateUtilization returns usage percentage
func CalculateUtilization(used, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}
