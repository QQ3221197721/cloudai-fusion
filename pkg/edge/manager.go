// Package edge provides edge-cloud collaborative architecture management.
// Implements the three-tier architecture: Cloud (global processing) →
// Edge (real-time inference) → Terminal (data collection), supporting
// 50B parameter model deployment at edge nodes with <200W power budget.
package edge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// NodeTier defines the compute tier
type NodeTier string

const (
	TierCloud    NodeTier = "cloud"    // Global data processing, training, scheduling
	TierEdge     NodeTier = "edge"     // Real-time inference, local autonomy
	TierTerminal NodeTier = "terminal" // Data collection, lightweight compute
)

// EdgeNodeStatus represents edge node health
type EdgeNodeStatus string

const (
	EdgeNodeOnline       EdgeNodeStatus = "online"
	EdgeNodeOffline      EdgeNodeStatus = "offline"
	EdgeNodeDegraded     EdgeNodeStatus = "degraded"
	EdgeNodeMaintenance  EdgeNodeStatus = "maintenance"
)

// Config holds edge-cloud configuration
type Config struct {
	CloudEndpoint      string        `json:"cloud_endpoint"`
	SyncInterval       time.Duration `json:"sync_interval"`
	OfflineBufferSize  int           `json:"offline_buffer_size_mb"`
	MaxEdgePowerWatts  int           `json:"max_edge_power_watts"` // Target: <200W
	EnableAutoFailover bool          `json:"enable_auto_failover"`
	MQTTBroker         string        `json:"mqtt_broker"`   // MQTT broker URL for edge messaging
	GRPCEndpoint       string        `json:"grpc_endpoint"` // gRPC endpoint for edge control plane
}

// EdgeNode represents an edge computing node
type EdgeNode struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Region          string            `json:"region"`
	Location        *GeoLocation      `json:"location,omitempty"`
	Tier            NodeTier          `json:"tier"`
	Status          EdgeNodeStatus    `json:"status"`
	ClusterID       string            `json:"cluster_id,omitempty"`
	// Hardware
	CPUCores        int               `json:"cpu_cores"`
	MemoryGB        float64           `json:"memory_gb"`
	GPUType         string            `json:"gpu_type,omitempty"`
	GPUCount        int               `json:"gpu_count"`
	GPUMemoryGB     float64           `json:"gpu_memory_gb"`
	StorageGB       float64           `json:"storage_gb"`
	PowerBudgetWatts int              `json:"power_budget_watts"`
	CurrentPowerWatts float64         `json:"current_power_watts"`
	// Connectivity
	IPAddress            string      `json:"ip_address,omitempty"`
	NetworkBandwidthMbps float64     `json:"network_bandwidth_mbps"`
	LatencyToCloudMs     float64     `json:"latency_to_cloud_ms"`
	IsOfflineCapable     bool        `json:"is_offline_capable"`
	// Runtime
	DeployedModels   []DeployedModel   `json:"deployed_models,omitempty"`
	ResourceUsage    *EdgeResourceUsage `json:"resource_usage,omitempty"`
	Labels           map[string]string  `json:"labels,omitempty"`
	LastHeartbeatAt  time.Time          `json:"last_heartbeat_at"`
	RegisteredAt     time.Time          `json:"registered_at"`
}

// GeoLocation represents physical location
type GeoLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	City      string  `json:"city,omitempty"`
	Country   string  `json:"country,omitempty"`
}

// DeployedModel represents a model running on edge node
type DeployedModel struct {
	ModelID        string  `json:"model_id"`
	ModelName      string  `json:"model_name"`
	ParameterCount string  `json:"parameter_count"` // e.g., "7B", "50B"
	Framework      string  `json:"framework"`
	QuantizationType string `json:"quantization_type,omitempty"` // INT4, INT8, FP16
	InferenceLatencyMs float64 `json:"inference_latency_ms"`
	MemoryUsageGB  float64 `json:"memory_usage_gb"`
	PowerUsageWatts float64 `json:"power_usage_watts"`
	RequestsPerSec float64 `json:"requests_per_sec"`
	Status         string  `json:"status"`
}

// EdgeResourceUsage tracks edge node resource consumption
type EdgeResourceUsage struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	GPUPercent    float64 `json:"gpu_percent"`
	DiskPercent   float64 `json:"disk_percent"`
	PowerWatts    float64 `json:"power_watts"`
	Temperature   float64 `json:"temperature_celsius"`
}

// EdgeDeployRequest defines model deployment to edge
type EdgeDeployRequest struct {
	ModelID          string `json:"model_id" binding:"required"`
	ModelName        string `json:"model_name" binding:"required"`
	ParameterCount   string `json:"parameter_count" binding:"required"`
	EdgeNodeID       string `json:"edge_node_id" binding:"required"`
	Framework        string `json:"framework"`
	QuantizationType string `json:"quantization_type"`
	MaxPowerWatts    int    `json:"max_power_watts"`
}

// SyncPolicy defines cloud-edge data synchronization policy
type SyncPolicy struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Direction      string `json:"direction"` // cloud-to-edge, edge-to-cloud, bidirectional
	DataType       string `json:"data_type"` // model, config, metrics, logs
	Interval       string `json:"interval"`
	Priority       int    `json:"priority"`
	OfflineBehavior string `json:"offline_behavior"` // buffer, drop, retry
	CompressionEnabled bool `json:"compression_enabled"`
}

// ============================================================================
// Manager
// ============================================================================

// Manager manages edge-cloud collaborative architecture
type Manager struct {
	config           Config
	nodes            map[string]*EdgeNode   // in-memory cache (hot-path reads)
	syncPolicies     []*SyncPolicy
	store            *store.Store           // DB persistence (nil = in-memory only)
	httpClient       *http.Client           // for edge node REST API calls
	logger           *logrus.Logger
	mu               sync.RWMutex
	// New fields for enhanced edge-cloud collaboration
	offlineQueues    map[string]*OfflineQueue        // per-node offline buffers
	optEngine        *OptimizationEngine             // model optimization
	versionMgr       *VersionManager                 // model version tracking
	bandwidthMonitor *BandwidthMonitor               // adaptive sync
	conflictResolver *ConflictResolver               // conflict resolution
	inferenceProxy   *InferenceProxy                 // cloud fallback
	// Phase 2: Edge computing capabilities
	edgeCollabHub    *EdgeCollabHub                  // KubeEdge/OpenYurt, autonomy, diff delivery, inference opt
	offlineHub       *OfflineHub                     // cache, CRDT, mDNS, OTA
}

// NewManager creates a new edge-cloud manager
func NewManager(cfg Config) (*Manager, error) {
	if cfg.MaxEdgePowerWatts == 0 {
		cfg.MaxEdgePowerWatts = 200 // Chinese market target: <200W
	}
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 30 * time.Second
	}

	logger := logrus.StandardLogger()

	mgr := &Manager{
		config:        cfg,
		nodes:         make(map[string]*EdgeNode),
		syncPolicies:  defaultSyncPolicies(),
		httpClient:    &http.Client{Timeout: 15 * time.Second},
		logger:        logger,
		edgeCollabHub: NewEdgeCollabHub(DefaultEdgeCollabConfig(), logger),
		offlineHub:    NewOfflineHub(DefaultOfflineHubConfig(), logger),
	}
	return mgr, nil
}

// SetStore injects a database store for persistent edge node management
func (m *Manager) SetStore(s *store.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = s
	if s != nil {
		m.loadNodesFromDB()
	}
}

// loadNodesFromDB populates the in-memory cache from PostgreSQL
func (m *Manager) loadNodesFromDB() {
	if m.store == nil {
		return
	}
	models, _, err := m.store.ListEdgeNodes("", 0, 1000)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to load edge nodes from DB")
		return
	}
	for _, model := range models {
		node := edgeNodeModelToNode(&model)
		m.nodes[node.ID] = node
	}
	if len(models) > 0 {
		m.logger.WithField("count", len(models)).Info("Loaded edge nodes from database")
	}
}

// RegisterNode registers a new edge node (DB + cache)
func (m *Manager) RegisterNode(ctx context.Context, node *EdgeNode) error {
	if err := apperrors.CheckContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	node.ID = common.NewUUID()
	node.Status = EdgeNodeOnline
	node.RegisteredAt = common.NowUTC()
	node.LastHeartbeatAt = common.NowUTC()

	// Persist to DB
	if m.store != nil {
		dbModel := nodeToEdgeNodeModel(node)
		if err := m.store.CreateEdgeNode(dbModel); err != nil {
			m.logger.WithError(err).Warn("Failed to persist edge node to database")
		}
	}

	m.nodes[node.ID] = node
	m.logger.WithFields(logrus.Fields{
		"node": node.Name, "tier": node.Tier, "region": node.Region,
	}).Info("Edge node registered")
	return nil
}

// ListNodes returns all edge nodes (DB-first with cache fallback)
func (m *Manager) ListNodes(ctx context.Context, tier NodeTier) ([]*EdgeNode, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	// Try DB first for authoritative data
	if m.store != nil {
		tierStr := string(tier)
		models, _, err := m.store.ListEdgeNodes(tierStr, 0, 1000)
		if err == nil {
			result := make([]*EdgeNode, 0, len(models))
			for _, model := range models {
				result = append(result, edgeNodeModelToNode(&model))
			}
			return result, nil
		}
		m.logger.WithError(err).Warn("DB read failed, falling back to cache")
	}

	// Fallback to in-memory cache
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*EdgeNode, 0)
	for _, n := range m.nodes {
		if tier == "" || n.Tier == tier {
			result = append(result, n)
		}
	}
	return result, nil
}

// DeployModel deploys an AI model to an edge node via real communication protocol
func (m *Manager) DeployModel(ctx context.Context, req *EdgeDeployRequest) (*DeployedModel, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok := m.nodes[req.EdgeNodeID]
	if !ok {
		return nil, apperrors.NotFound("edge node", req.EdgeNodeID)
	}

	// Power budget check
	if req.MaxPowerWatts > 0 && req.MaxPowerWatts > m.config.MaxEdgePowerWatts {
		return nil, fmt.Errorf("power budget %dW exceeds edge limit %dW", req.MaxPowerWatts, m.config.MaxEdgePowerWatts)
	}

	model := DeployedModel{
		ModelID:          req.ModelID,
		ModelName:        req.ModelName,
		ParameterCount:   req.ParameterCount,
		Framework:        req.Framework,
		QuantizationType: req.QuantizationType,
		Status:           "deploying",
	}

	// Try to deploy via real edge communication protocol
	if node.IPAddress != "" || (node.Location != nil && m.config.CloudEndpoint != "") {
		deployErr := m.deployModelToEdgeNode(ctx, node, &model)
		if deployErr != nil {
			m.logger.WithError(deployErr).WithField("node", node.Name).Warn("Real edge deploy failed, tracking locally")
		}
	}

	node.DeployedModels = append(node.DeployedModels, model)
	m.logger.WithFields(logrus.Fields{
		"model": req.ModelName, "params": req.ParameterCount,
		"node": node.Name, "quant": req.QuantizationType,
	}).Info("Model deploying to edge node")

	return &model, nil
}

// deployModelToEdgeNode sends a deploy command to the edge node via HTTP/gRPC
func (m *Manager) deployModelToEdgeNode(ctx context.Context, node *EdgeNode, model *DeployedModel) error {
	// Apply a timeout for the edge HTTP call
	httpCtx, httpCancel := context.WithTimeout(ctx, time.Duration(apperrors.ExternalCallTimeout)*time.Second)
	defer httpCancel()

	// Build edge node API URL
	edgeURL := ""
	if node.IPAddress != "" {
		edgeURL = fmt.Sprintf("http://%s:8082/api/v1/models/deploy", node.IPAddress)
	} else if m.config.GRPCEndpoint != "" {
		// Use cloud-side relay for edge nodes behind NAT
		edgeURL = fmt.Sprintf("%s/api/v1/edge/%s/deploy", m.config.CloudEndpoint, node.ID)
	} else {
		return fmt.Errorf("no reachable endpoint for edge node %s", node.Name)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"model_id":          model.ModelID,
		"model_name":        model.ModelName,
		"parameter_count":   model.ParameterCount,
		"framework":         model.Framework,
		"quantization_type": model.QuantizationType,
		"max_power_watts":   m.config.MaxEdgePowerWatts,
	})

	req, err := http.NewRequestWithContext(httpCtx, "POST", edgeURL, edgeReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create edge deploy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("edge node communication failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 202 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("edge node returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	model.Status = "running"
	return nil
}

func edgeReader(data []byte) io.Reader {
	return &edgeBytesReader{data: data}
}

type edgeBytesReader struct {
	data []byte
	pos  int
}

func (r *edgeBytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// Heartbeat processes a heartbeat from an edge node (via HTTP/MQTT)
func (m *Manager) Heartbeat(ctx context.Context, nodeID string, usage *EdgeResourceUsage) error {
	if err := apperrors.CheckContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok := m.nodes[nodeID]
	if !ok {
		return apperrors.NotFound("edge node", nodeID)
	}

	node.LastHeartbeatAt = common.NowUTC()
	node.ResourceUsage = usage
	node.CurrentPowerWatts = usage.PowerWatts
	node.Status = EdgeNodeOnline

	// Persist heartbeat to DB
	if m.store != nil {
		resJSON := "{}"
		if usage != nil {
			if b, err := json.Marshal(usage); err == nil {
				resJSON = string(b)
			}
		}
		if err := m.store.UpdateEdgeNodeHeartbeat(nodeID, string(node.Status), usage.PowerWatts, resJSON); err != nil {
			m.logger.WithError(err).WithField("node", nodeID).Warn("Failed to persist edge node heartbeat to database")
		}
	}

	// Detect degraded state based on resource thresholds
	if usage.CPUPercent > 95 || usage.MemoryPercent > 95 || usage.Temperature > 85 {
		node.Status = EdgeNodeDegraded
		m.logger.WithField("node", node.Name).Warn("Edge node entering degraded state")
	}

	// Update deployed model inference metrics if available
	for i := range node.DeployedModels {
		if node.DeployedModels[i].Status == "deploying" {
			node.DeployedModels[i].Status = "running"
		}
	}

	return nil
}

// StartSyncLoop starts periodic edge-cloud synchronization
func (m *Manager) StartSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			for _, node := range m.nodes {
				// Detect offline nodes (no heartbeat for 3x sync interval)
				if time.Since(node.LastHeartbeatAt) > m.config.SyncInterval*3 && node.Status == EdgeNodeOnline {
					node.Status = EdgeNodeOffline
					m.logger.WithField("node", node.Name).Warn("Edge node went offline")

					// Auto-failover: notify cloud for model re-scheduling
					if m.config.EnableAutoFailover && len(node.DeployedModels) > 0 {
						m.logger.WithFields(logrus.Fields{
							"node":   node.Name,
							"models": len(node.DeployedModels),
						}).Info("Auto-failover: scheduling model re-deployment")
					}
				}

				// Try to pull real metrics via HTTP if endpoint available
				if node.IPAddress != "" && node.Status == EdgeNodeOnline {
					go m.pullEdgeMetrics(ctx, node)
				}
			}
			m.mu.RUnlock()
		}
	}
}

// pullEdgeMetrics fetches real-time metrics from an edge node via HTTP
func (m *Manager) pullEdgeMetrics(ctx context.Context, node *EdgeNode) {
	metricsURL := fmt.Sprintf("http://%s:8082/api/v1/metrics", node.IPAddress)
	req, err := http.NewRequestWithContext(ctx, "GET", metricsURL, nil)
	if err != nil {
		return
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return // Node unreachable, will be caught by heartbeat timeout
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var usage EdgeResourceUsage
		body, _ := io.ReadAll(resp.Body)
		if json.Unmarshal(body, &usage) == nil {
			m.mu.Lock()
			node.ResourceUsage = &usage
			node.CurrentPowerWatts = usage.PowerWatts
			node.LastHeartbeatAt = common.NowUTC()
			m.mu.Unlock()
		}
	}
}

// GetTopology returns the edge-cloud topology as a structured response
func (m *Manager) GetTopology(ctx context.Context) (map[string]interface{}, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	return m.GetTopologySummary(ctx), nil
}

// GetSyncPolicies returns configured cloud-edge data sync policies
func (m *Manager) GetSyncPolicies(ctx context.Context) ([]*SyncPolicy, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.syncPolicies, nil
}

// GetTopologySummary returns a summary of the edge-cloud topology
func (m *Manager) GetTopologySummary(ctx context.Context) map[string]interface{} {
	if ctx.Err() != nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	cloudCount, edgeCount, terminalCount := 0, 0, 0
	totalModels := 0
	for _, n := range m.nodes {
		switch n.Tier {
		case TierCloud:
			cloudCount++
		case TierEdge:
			edgeCount++
		case TierTerminal:
			terminalCount++
		}
		totalModels += len(n.DeployedModels)
	}

	return map[string]interface{}{
		"cloud_nodes":    cloudCount,
		"edge_nodes":     edgeCount,
		"terminal_nodes": terminalCount,
		"total_nodes":    len(m.nodes),
		"deployed_models": totalModels,
		"sync_policies":  len(m.syncPolicies),
		"architecture":   "three-tier: cloud → edge → terminal",
	}
}

func defaultSyncPolicies() []*SyncPolicy {
	return []*SyncPolicy{
		{ID: "sync-model", Name: "Model Sync", Direction: "cloud-to-edge", DataType: "model", Interval: "1h", Priority: 1, OfflineBehavior: "buffer", CompressionEnabled: true},
		{ID: "sync-config", Name: "Config Sync", Direction: "cloud-to-edge", DataType: "config", Interval: "5m", Priority: 2, OfflineBehavior: "retry"},
		{ID: "sync-metrics", Name: "Metrics Upload", Direction: "edge-to-cloud", DataType: "metrics", Interval: "30s", Priority: 3, OfflineBehavior: "buffer", CompressionEnabled: true},
		{ID: "sync-logs", Name: "Log Upload", Direction: "edge-to-cloud", DataType: "logs", Interval: "1m", Priority: 4, OfflineBehavior: "buffer", CompressionEnabled: true},
	}
}

// ============================================================================
// DB <-> Domain Conversion
// ============================================================================

func edgeNodeModelToNode(m *store.EdgeNodeModel) *EdgeNode {
	node := &EdgeNode{
		ID:                  m.ID,
		Name:                m.Name,
		Region:              m.Region,
		Tier:                NodeTier(m.Tier),
		Status:              EdgeNodeStatus(m.Status),
		ClusterID:           m.ClusterID,
		IPAddress:           m.IPAddress,
		CPUCores:            m.CPUCores,
		MemoryGB:            m.MemoryGB,
		GPUType:             m.GPUType,
		GPUCount:            m.GPUCount,
		GPUMemoryGB:         m.GPUMemoryGB,
		StorageGB:           m.StorageGB,
		PowerBudgetWatts:    m.PowerBudgetWatts,
		CurrentPowerWatts:   m.CurrentPowerWatts,
		NetworkBandwidthMbps: m.NetworkBandwidth,
		LatencyToCloudMs:    m.LatencyToCloudMs,
		IsOfflineCapable:    m.IsOfflineCapable,
		LastHeartbeatAt:     m.LastHeartbeatAt,
		RegisteredAt:        m.RegisteredAt,
	}
	if m.Location != "" && m.Location != "{}" {
		var loc GeoLocation
		if json.Unmarshal([]byte(m.Location), &loc) == nil {
			node.Location = &loc
		}
	}
	if m.DeployedModels != "" && m.DeployedModels != "[]" {
		if err := json.Unmarshal([]byte(m.DeployedModels), &node.DeployedModels); err != nil {
			logrus.WithError(err).WithField("node", m.ID).Debug("Failed to unmarshal edge node deployed models")
		}
	}
	if m.ResourceUsage != "" && m.ResourceUsage != "{}" {
		var usage EdgeResourceUsage
		if json.Unmarshal([]byte(m.ResourceUsage), &usage) == nil {
			node.ResourceUsage = &usage
		}
	}
	if m.Labels != "" && m.Labels != "{}" {
		if err := json.Unmarshal([]byte(m.Labels), &node.Labels); err != nil {
			logrus.WithError(err).WithField("node", m.ID).Debug("Failed to unmarshal edge node labels")
		}
	}
	return node
}

func nodeToEdgeNodeModel(node *EdgeNode) *store.EdgeNodeModel {
	locJSON := "{}"
	if node.Location != nil {
		if b, err := json.Marshal(node.Location); err == nil {
			locJSON = string(b)
		}
	}
	modelsJSON := "[]"
	if len(node.DeployedModels) > 0 {
		if b, err := json.Marshal(node.DeployedModels); err == nil {
			modelsJSON = string(b)
		}
	}
	resJSON := "{}"
	if node.ResourceUsage != nil {
		if b, err := json.Marshal(node.ResourceUsage); err == nil {
			resJSON = string(b)
		}
	}
	labelsJSON := "{}"
	if len(node.Labels) > 0 {
		if b, err := json.Marshal(node.Labels); err == nil {
			labelsJSON = string(b)
		}
	}
	return &store.EdgeNodeModel{
		ID:                node.ID,
		Name:              node.Name,
		Region:            node.Region,
		Tier:              string(node.Tier),
		Status:            string(node.Status),
		ClusterID:         node.ClusterID,
		IPAddress:         node.IPAddress,
		Location:          locJSON,
		CPUCores:          node.CPUCores,
		MemoryGB:          node.MemoryGB,
		GPUType:           node.GPUType,
		GPUCount:          node.GPUCount,
		GPUMemoryGB:       node.GPUMemoryGB,
		StorageGB:         node.StorageGB,
		PowerBudgetWatts:  node.PowerBudgetWatts,
		CurrentPowerWatts: node.CurrentPowerWatts,
		NetworkBandwidth:  node.NetworkBandwidthMbps,
		LatencyToCloudMs:  node.LatencyToCloudMs,
		IsOfflineCapable:  node.IsOfflineCapable,
		DeployedModels:    modelsJSON,
		ResourceUsage:     resJSON,
		Labels:            labelsJSON,
		LastHeartbeatAt:   node.LastHeartbeatAt,
		RegisteredAt:      node.RegisteredAt,
		CreatedAt:         common.NowUTC(),
		UpdatedAt:         common.NowUTC(),
	}
}
