package sdk

import (
	"context"
	"net/http"
	"time"
)

// GPUClient provides access to GPU scheduling — submitting workloads, listing
// available accelerators, and inspecting the interconnect topology.
//
// Obtain it from a Client via the GPU field; do not construct it directly.
type GPUClient struct {
	client *Client
}

// GPUJob describes a GPU workload to submit for scheduling.
type GPUJob struct {
	// Name is a human-readable identifier for the job.
	Name string `json:"name"`
	// GPUCount is the number of GPUs the job requires.
	GPUCount int `json:"gpuCount"`
	// Image is the container image to run, e.g. "nvcr.io/nvidia/pytorch:24.01".
	Image string `json:"image"`
	// Command optionally overrides the image's default entrypoint.
	Command []string `json:"command,omitempty"`
	// Namespace optionally scopes the job to a tenant namespace.
	Namespace string `json:"namespace,omitempty"`
	// Priority influences scheduling order; higher runs sooner. Zero is normal.
	Priority int `json:"priority,omitempty"`
}

// JobResult reports the scheduling outcome for a submitted GPUJob.
type JobResult struct {
	// ID is the server-assigned identifier for the scheduled job.
	ID string `json:"id"`
	// Status is the current lifecycle state, e.g. "pending" or "running".
	Status string `json:"status"`
	// AssignedGPUs lists the GPU UUIDs allocated to the job, when placed.
	AssignedGPUs []string `json:"assignedGpus,omitempty"`
	// SubmittedAt is when the scheduler accepted the job.
	SubmittedAt time.Time `json:"submittedAt"`
}

// GPUInfo describes a single GPU resource known to the scheduler.
type GPUInfo struct {
	// UUID uniquely identifies the physical or virtual GPU.
	UUID string `json:"uuid"`
	// Model is the device model, e.g. "NVIDIA H100 80GB".
	Model string `json:"model"`
	// MemoryMB is the total device memory in megabytes.
	MemoryMB int `json:"memoryMb"`
	// Node is the cluster node hosting the GPU.
	Node string `json:"node"`
	// Allocated is true when the GPU is currently assigned to a job.
	Allocated bool `json:"allocated"`
}

// Topology describes how GPUs are interconnected, enabling topology-aware
// placement decisions.
type Topology struct {
	// GPUs is the set of GPUs included in the topology.
	GPUs []GPUInfo `json:"gpus"`
	// Links describes pairwise interconnects between GPUs.
	Links []TopologyLink `json:"links"`
}

// TopologyLink describes the interconnect between two GPUs.
type TopologyLink struct {
	// Source is the UUID of the originating GPU.
	Source string `json:"source"`
	// Target is the UUID of the destination GPU.
	Target string `json:"target"`
	// Type is the link technology, e.g. "nvlink" or "pcie".
	Type string `json:"type"`
	// BandwidthGBps is the link bandwidth in gigabytes per second.
	BandwidthGBps float64 `json:"bandwidthGBps"`
}

// SubmitJob submits a GPU workload for scheduling and returns its placement.
func (g *GPUClient) SubmitJob(ctx context.Context, job *GPUJob) (*JobResult, error) {
	var out JobResult
	if err := g.client.do(ctx, http.MethodPost, "/api/v1/gpu/jobs", job, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListGPUs returns the GPU resources currently visible to the scheduler.
func (g *GPUClient) ListGPUs(ctx context.Context) ([]*GPUInfo, error) {
	var out []*GPUInfo
	if err := g.client.do(ctx, http.MethodGet, "/api/v1/gpu/devices", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetTopology returns the GPU topology map used for interconnect-aware placement.
func (g *GPUClient) GetTopology(ctx context.Context) (*Topology, error) {
	var out Topology
	if err := g.client.do(ctx, http.MethodGet, "/api/v1/gpu/topology", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
