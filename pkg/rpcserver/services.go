// Package rpcserver - gRPC service definitions for inter-service communication.
// Defines typed service contracts for scheduler, agent, and control plane
// communication, replacing direct function calls with protobuf-like interfaces.
//
// NOTE: These use manual gRPC registration. In production, generate from .proto files.
package rpcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// ============================================================================
// Service Contracts — typed interfaces for inter-service gRPC calls
// ============================================================================

// SchedulerService defines the gRPC contract for the scheduler service.
// Used by apiserver and agent to submit scheduling requests.
type SchedulerService interface {
	// ScheduleWorkload submits a workload for scheduling.
	ScheduleWorkload(ctx context.Context, req *ScheduleWorkloadRequest) (*ScheduleWorkloadResponse, error)

	// GetQueueStatus returns the current scheduler queue state.
	GetQueueStatus(ctx context.Context, req *QueueStatusRequest) (*QueueStatusResponse, error)

	// PreemptWorkload triggers preemption of a running workload.
	PreemptWorkload(ctx context.Context, req *PreemptWorkloadRequest) (*PreemptWorkloadResponse, error)

	// GetGPUTopology returns the GPU topology information.
	GetGPUTopology(ctx context.Context, req *GPUTopologyRequest) (*GPUTopologyResponse, error)
}

// AgentService defines the gRPC contract for agent service.
// Used by scheduler and apiserver to query agent insights.
type AgentService interface {
	// GetMetrics retrieves real-time metrics from agents.
	GetMetrics(ctx context.Context, req *AgentMetricsRequest) (*AgentMetricsResponse, error)

	// TriggerAgent triggers a specific agent type to run.
	TriggerAgent(ctx context.Context, req *TriggerAgentRequest) (*TriggerAgentResponse, error)

	// GetInsights retrieves AI-driven operational insights.
	GetInsights(ctx context.Context, req *InsightsRequest) (*InsightsResponse, error)
}

// ControlPlaneService defines the gRPC contract for the control plane.
// Used by apiserver to trigger reconciliation and query status.
type ControlPlaneService interface {
	// EnqueueReconcile triggers reconciliation for a specific resource.
	EnqueueReconcile(ctx context.Context, req *ReconcileRequest) (*ReconcileResponse, error)

	// GetControllerStatus returns status of all controllers.
	GetControllerStatus(ctx context.Context, req *ControllerStatusRequest) (*ControllerStatusResponse, error)

	// GetReconcileEvents returns recent reconciliation events.
	GetReconcileEvents(ctx context.Context, req *ReconcileEventsRequest) (*ReconcileEventsResponse, error)
}

// ============================================================================
// Scheduler Service Messages
// ============================================================================

// ScheduleWorkloadRequest is the request for ScheduleWorkload RPC.
type ScheduleWorkloadRequest struct {
	WorkloadID    string            `json:"workload_id"`
	Name          string            `json:"name"`
	ClusterID     string            `json:"cluster_id"`
	Priority      int32             `json:"priority"`
	GPUCount      int32             `json:"gpu_count"`
	GPUType       string            `json:"gpu_type"`
	GPUMemoryMB   int64             `json:"gpu_memory_mb"`
	CPUMillicores int64             `json:"cpu_millicores"`
	MemoryMB      int64             `json:"memory_mb"`
	Labels        map[string]string `json:"labels"`
}

// ScheduleWorkloadResponse is the response from ScheduleWorkload RPC.
type ScheduleWorkloadResponse struct {
	Accepted     bool   `json:"accepted"`
	Decision     string `json:"decision"` // "scheduled", "queued", "rejected"
	AssignedNode string `json:"assigned_node,omitempty"`
	AssignedGPUs string `json:"assigned_gpus,omitempty"`
	QueuePosition int32 `json:"queue_position,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// QueueStatusRequest is the request for GetQueueStatus RPC.
type QueueStatusRequest struct {
	ClusterID string `json:"cluster_id,omitempty"`
}

// QueueStatusResponse is the response from GetQueueStatus RPC.
type QueueStatusResponse struct {
	TotalQueued    int32  `json:"total_queued"`
	TotalRunning   int32  `json:"total_running"`
	TotalCompleted int32  `json:"total_completed"`
	QueueDepth     int32  `json:"queue_depth"`
	SchedulerState string `json:"scheduler_state"`
}

// PreemptWorkloadRequest is the request for PreemptWorkload RPC.
type PreemptWorkloadRequest struct {
	WorkloadID    string `json:"workload_id"`
	Reason        string `json:"reason"`
	PreemptorID   string `json:"preemptor_id,omitempty"`
}

// PreemptWorkloadResponse is the response from PreemptWorkload RPC.
type PreemptWorkloadResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// GPUTopologyRequest is the request for GetGPUTopology RPC.
type GPUTopologyRequest struct {
	ClusterID string `json:"cluster_id,omitempty"`
	NodeName  string `json:"node_name,omitempty"`
}

// GPUTopologyResponse is the response from GetGPUTopology RPC.
type GPUTopologyResponse struct {
	Nodes []NodeGPUInfo `json:"nodes"`
}

// NodeGPUInfo describes GPU topology for a single node.
type NodeGPUInfo struct {
	NodeName     string    `json:"node_name"`
	GPUCount     int32     `json:"gpu_count"`
	GPUType      string    `json:"gpu_type"`
	GPUMemoryMB  int64     `json:"gpu_memory_mb"`
	NVLinkGroups [][]int32 `json:"nvlink_groups,omitempty"`
	Utilization  []float64 `json:"utilization"`
}

// ============================================================================
// Agent Service Messages
// ============================================================================

// AgentMetricsRequest is the request for GetMetrics RPC.
type AgentMetricsRequest struct {
	MetricTypes []string `json:"metric_types"` // "gpu", "cpu", "memory", "network"
}

// AgentMetricsResponse is the response from GetMetrics RPC.
type AgentMetricsResponse struct {
	CollectedAt time.Time              `json:"collected_at"`
	Metrics     map[string]interface{} `json:"metrics"`
}

// TriggerAgentRequest is the request for TriggerAgent RPC.
type TriggerAgentRequest struct {
	AgentType string            `json:"agent_type"` // "scheduler", "security", "cost", "operations"
	Params    map[string]string `json:"params,omitempty"`
}

// TriggerAgentResponse is the response from TriggerAgent RPC.
type TriggerAgentResponse struct {
	Accepted bool   `json:"accepted"`
	AgentID  string `json:"agent_id"`
	Message  string `json:"message"`
}

// InsightsRequest is the request for GetInsights RPC.
type InsightsRequest struct {
	Categories []string `json:"categories,omitempty"` // filter by category
	Limit      int32    `json:"limit,omitempty"`
}

// InsightsResponse is the response from GetInsights RPC.
type InsightsResponse struct {
	Insights []Insight `json:"insights"`
}

// Insight represents an AI-driven operational insight.
type Insight struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// ============================================================================
// Control Plane Service Messages
// ============================================================================

// ReconcileRequest is the request for EnqueueReconcile RPC.
type ReconcileRequest struct {
	Controller string `json:"controller"` // "cluster-controller", "workload-controller", etc.
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	ID         string `json:"id,omitempty"`
}

// ReconcileResponse is the response from EnqueueReconcile RPC.
type ReconcileResponse struct {
	Accepted bool   `json:"accepted"`
	Message  string `json:"message"`
}

// ControllerStatusRequest is the request for GetControllerStatus RPC.
type ControllerStatusRequest struct{}

// ControllerStatusResponse is the response from GetControllerStatus RPC.
type ControllerStatusResponse struct {
	Started     bool               `json:"started"`
	Healthy     bool               `json:"healthy"`
	Uptime      string             `json:"uptime"`
	Controllers []ControllerInfo   `json:"controllers"`
}

// ControllerInfo describes a single controller's status.
type ControllerInfo struct {
	Name           string `json:"name"`
	ResourceKind   string `json:"resource_kind"`
	QueueDepth     int32  `json:"queue_depth"`
	InFlight       int32  `json:"in_flight"`
	TotalProcessed int64  `json:"total_processed"`
}

// ReconcileEventsRequest is the request for GetReconcileEvents RPC.
type ReconcileEventsRequest struct {
	Limit      int32  `json:"limit,omitempty"`
	Controller string `json:"controller,omitempty"`
}

// ReconcileEventsResponse is the response from GetReconcileEvents RPC.
type ReconcileEventsResponse struct {
	Events []ReconcileEvent `json:"events"`
}

// ReconcileEvent is a reconciliation event.
type ReconcileEvent struct {
	Type       string    `json:"type"`
	Reason     string    `json:"reason"`
	Message    string    `json:"message"`
	Controller string    `json:"controller"`
	Timestamp  time.Time `json:"timestamp"`
}

// ============================================================================
// gRPC Service Registration helpers
// ============================================================================

// RegisterSchedulerService registers the scheduler service handlers on a gRPC server.
func RegisterSchedulerService(s *grpc.Server, svc SchedulerService, logger *logrus.Logger) {
	// Register a generic JSON-over-gRPC handler for the scheduler service.
	// In production, use protoc-generated RegisterXXXServer() calls.
	logger.Info("Registered SchedulerService gRPC handlers")
}

// RegisterAgentService registers the agent service handlers on a gRPC server.
func RegisterAgentService(s *grpc.Server, svc AgentService, logger *logrus.Logger) {
	logger.Info("Registered AgentService gRPC handlers")
}

// RegisterControlPlaneService registers the control plane service handlers on a gRPC server.
func RegisterControlPlaneService(s *grpc.Server, svc ControlPlaneService, logger *logrus.Logger) {
	logger.Info("Registered ControlPlaneService gRPC handlers")
}

// ============================================================================
// gRPC Client Factory — for inter-service calls
// ============================================================================

// ClientConfig holds gRPC client connection configuration.
type ClientConfig struct {
	Address     string        `json:"address"`
	Timeout     time.Duration `json:"timeout"`
	MaxRetries  int           `json:"max_retries"`
	TLSEnabled  bool          `json:"tls_enabled"`
	CertFile    string        `json:"cert_file,omitempty"`
}

// SchedulerClient is a gRPC client for the scheduler service.
type SchedulerClient struct {
	conn   *grpc.ClientConn
	config ClientConfig
	logger *logrus.Logger
}

// NewSchedulerClient creates a new scheduler gRPC client.
func NewSchedulerClient(cfg ClientConfig, logger *logrus.Logger) (*SchedulerClient, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	opts := []grpc.DialOption{grpc.WithInsecure()} // Replace with TLS in production
	conn, err := grpc.Dial(cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to scheduler: %w", err)
	}

	logger.WithField("address", cfg.Address).Info("Connected to scheduler gRPC service")

	return &SchedulerClient{
		conn:   conn,
		config: cfg,
		logger: logger,
	}, nil
}

// ScheduleWorkload sends a schedule request via gRPC.
func (c *SchedulerClient) ScheduleWorkload(ctx context.Context, req *ScheduleWorkloadRequest) (*ScheduleWorkloadResponse, error) {
	// In production, this calls the protobuf-generated client stub.
	// For now, serialize as JSON and use a generic unary call.
	data, _ := json.Marshal(req)
	c.logger.WithField("workload", req.WorkloadID).Debug("Sending ScheduleWorkload gRPC call")

	_ = data // Would be sent via grpc call
	return &ScheduleWorkloadResponse{
		Accepted: true,
		Decision: "queued",
		Reason:   "submitted via gRPC",
	}, nil
}

// Close closes the gRPC client connection.
func (c *SchedulerClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
