// Package edge - Enhanced manager methods for edge-cloud collaboration
package edge

import (
	"context"
	"fmt"
	"io"
	"net/http"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/sirupsen/logrus"
)

// GetOfflineStatus returns offline operation queue status for a node
func (m *Manager) GetOfflineStatus(ctx context.Context, nodeID string) (*SyncStatus, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	queue, ok := m.offlineQueues[nodeID]
	if !ok {
		return &SyncStatus{NodeID: nodeID}, nil
	}

	status := queue.GetStatus()
	status.IsOnline = true
	status.LastSyncAt = m.bandwidthMonitor.lastMeasurement
	status.CurrentBandwidthMbps = m.bandwidthMonitor.currentBandwidthMbps
	status.AvgLatencyMs = m.bandwidthMonitor.avgLatencyMs
	status.SyncDirection = SyncBidirectional
	status.CompressionEnabled = m.bandwidthMonitor.ShouldCompress(10.0)

	return status, nil
}

// DrainOfflineQueue processes all pending operations for a node
func (m *Manager) DrainOfflineQueue(ctx context.Context, nodeID string) (int, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return 0, err
	}

	queue := m.getOrCreateOfflineQueue(nodeID)
	processed := 0

	for {
		op := queue.Dequeue()
		if op == nil {
			break
		}

		// Execute operation based on type
		var execErr error
		switch op.Type {
		case OpTypeDeployModel:
			execErr = m.executeDeployOp(ctx, nodeID, op)
		case OpTypeStopModel:
			execErr = m.executeStopOp(ctx, nodeID, op)
		case OpTypeUpdateConfig:
			execErr = m.executeConfigOp(ctx, nodeID, op)
		default:
			execErr = fmt.Errorf("unknown operation type: %s", op.Type)
		}

		if execErr != nil {
			queue.MarkFailed(op.ID, execErr)
		} else {
			queue.MarkCompleted(op.ID)
			processed++
		}
	}

	m.logger.WithFields(logrus.Fields{
		"node":      nodeID,
		"processed": processed,
	}).Info("Drained offline queue")

	return processed, nil
}

func (m *Manager) executeDeployOp(ctx context.Context, nodeID string, op *OfflineOperation) error {
	modelID, _ := op.Payload["model_id"].(string)
	modelName, _ := op.Payload["model_name"].(string)
	paramCount, _ := op.Payload["parameter_count"].(string)

	req := &EdgeDeployRequest{
		ModelID:        modelID,
		ModelName:      modelName,
		ParameterCount: paramCount,
		EdgeNodeID:     nodeID,
	}

	_, err := m.DeployModel(ctx, req)
	return err
}

func (m *Manager) executeStopOp(ctx context.Context, nodeID string, op *OfflineOperation) error {
	m.logger.WithField("op", op.ID).Debug("Executing stop operation")
	return nil
}

func (m *Manager) executeConfigOp(ctx context.Context, nodeID string, op *OfflineOperation) error {
	m.logger.WithField("op", op.ID).Debug("Executing config operation")
	return nil
}

// OptimizeModelForEdge assesses and optimizes a model for edge deployment
func (m *Manager) OptimizeModelForEdge(ctx context.Context, req *EdgeDeployRequest) (*QuantizationType, *PowerBudgetResult, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, nil, err
	}

	node, ok := m.nodes[req.EdgeNodeID]
	if !ok {
		return nil, nil, apperrors.NotFound("edge node", req.EdgeNodeID)
	}

	quant, powerResult, err := m.optEngine.AssessQuantization(
		req.ModelName,
		req.ParameterCount,
		node.PowerBudgetWatts,
		node.MemoryGB,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("optimization assessment failed: %w", err)
	}

	// Update request with recommended quantization
	if req.QuantizationType == "" {
		req.QuantizationType = string(*quant)
	}

	return quant, powerResult, nil
}

// RemoveNode removes an edge node and cleans up resources
func (m *Manager) RemoveNode(ctx context.Context, nodeID string) error {
	if err := apperrors.CheckContext(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok := m.nodes[nodeID]
	if !ok {
		return apperrors.NotFound("edge node", nodeID)
	}

	// Delete from DB if store is available
	if m.store != nil {
		if err := m.store.DeleteEdgeNode(nodeID); err != nil {
			m.logger.WithError(err).Warn("Failed to delete edge node from database")
		}
	}

	// Clear offline queue
	if queue, ok := m.offlineQueues[nodeID]; ok {
		queue.Clear()
		delete(m.offlineQueues, nodeID)
	}

	// Remove from cache
	delete(m.nodes, nodeID)

	m.logger.WithField("node", node.Name).Info("Removed edge node")
	return nil
}

// GetNodeHealth returns detailed health status of an edge node
func (m *Manager) GetNodeHealth(ctx context.Context, nodeID string) (map[string]interface{}, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	node, ok := m.nodes[nodeID]
	if !ok {
		return nil, apperrors.NotFound("edge node", nodeID)
	}

	health := map[string]interface{}{
		"node_id":         nodeID,
		"name":            node.Name,
		"status":          node.Status,
		"is_online":       node.Status == EdgeNodeOnline || node.Status == EdgeNodeDegraded,
		"last_heartbeat":  node.LastHeartbeatAt,
		"power_watts":     node.CurrentPowerWatts,
		"power_budget":    node.PowerBudgetWatts,
		"deployed_models": len(node.DeployedModels),
	}

	if node.ResourceUsage != nil {
		health["cpu_percent"] = node.ResourceUsage.CPUPercent
		health["memory_percent"] = node.ResourceUsage.MemoryPercent
		health["gpu_percent"] = node.ResourceUsage.GPUPercent
		health["temperature"] = node.ResourceUsage.Temperature
	}

	// Add offline queue status if exists
	if queue, ok := m.offlineQueues[nodeID]; ok {
		queueStatus := queue.GetStatus()
		health["pending_operations"] = queueStatus.PendingOperations
		health["failed_operations"] = queueStatus.FailedOperations
		health["buffer_utilization"] = queueStatus.BufferUtilization
	}

	return health, nil
}

func (m *Manager) getOrCreateOfflineQueue(nodeID string) *OfflineQueue {
	m.mu.Lock()
	defer m.mu.Unlock()

	if queue, ok := m.offlineQueues[nodeID]; ok {
		return queue
	}

	queue := NewOfflineQueue(nodeID, m.config.OfflineBufferSize, m.logger)
	m.offlineQueues[nodeID] = queue
	return queue
}

// Helper function to read HTTP response body
func readResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return body, nil
}

// ============================================================================
// Phase 2: Edge Collaboration & Offline Capability Handlers
// ============================================================================

// HandleEdgeCollaboration handles edge-cloud collaboration requests
func (m *Manager) HandleEdgeCollaboration(ctx context.Context, action string, params map[string]interface{}) (interface{}, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}

	switch action {
	case "platform_status":
		return m.edgeCollabHub.Platform.GetStatus(), nil

	case "platform_connect":
		err := m.edgeCollabHub.Platform.Connect(ctx)
		if err != nil {
			return nil, err
		}
		return m.edgeCollabHub.Platform.GetStatus(), nil

	case "autonomy_status":
		nodeID, _ := params["node_id"].(string)
		if nodeID == "" {
			return nil, fmt.Errorf("node_id required")
		}
		return m.edgeCollabHub.Autonomy.GetNodeAutonomyState(nodeID), nil

	case "autonomy_disconnect":
		nodeID, _ := params["node_id"].(string)
		if nodeID == "" {
			return nil, fmt.Errorf("node_id required")
		}
		m.edgeCollabHub.Autonomy.NotifyDisconnect(nodeID)
		return m.edgeCollabHub.Autonomy.GetNodeAutonomyState(nodeID), nil

	case "autonomy_reconnect":
		nodeID, _ := params["node_id"].(string)
		if nodeID == "" {
			return nil, fmt.Errorf("node_id required")
		}
		msgs, err := m.edgeCollabHub.Autonomy.NotifyReconnect(nodeID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"forwarded_messages": len(msgs),
			"state":             m.edgeCollabHub.Autonomy.GetNodeAutonomyState(nodeID),
		}, nil

	case "diff_status":
		nodeID, _ := params["node_id"].(string)
		modelID, _ := params["model_id"].(string)
		return m.edgeCollabHub.DiffDelivery.GetDeliveryStatus(nodeID, modelID), nil

	case "optimize_model":
		modelID, _ := params["model_id"].(string)
		gpuType, _ := params["gpu_type"].(string)
		sizeBytes := int64(0)
		if s, ok := params["size_bytes"].(float64); ok {
			sizeBytes = int64(s)
		}
		return m.edgeCollabHub.InferenceOpt.OptimizeModel(ctx, modelID, sizeBytes, gpuType)

	case "list_optimized":
		return m.edgeCollabHub.InferenceOpt.ListOptimizedModels(), nil

	case "select_runtime":
		gpuType, _ := params["gpu_type"].(string)
		cpuArch, _ := params["cpu_arch"].(string)
		runtime, profile, err := m.edgeCollabHub.InferenceOpt.SelectRuntime(gpuType, cpuArch)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"runtime": runtime,
			"profile": profile,
		}, nil

	case "status":
		return m.edgeCollabHub.GetStatus(), nil

	default:
		return nil, fmt.Errorf("unknown edge collaboration action: %s", action)
	}
}

// HandleOfflineCapabilities handles offline capability requests
func (m *Manager) HandleOfflineCapabilities(ctx context.Context, action string, params map[string]interface{}) (interface{}, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}

	switch action {
	case "cache_stats":
		return m.offlineHub.Cache.GetStats(), nil

	case "cache_get":
		key, _ := params["key"].(string)
		entry, ok := m.offlineHub.Cache.Get(key)
		if !ok {
			return nil, fmt.Errorf("cache miss for key: %s", key)
		}
		return entry, nil

	case "cache_prefetch_predictions":
		return m.offlineHub.Cache.GeneratePrefetchPredictions(), nil

	case "crdt_counter":
		key, _ := params["key"].(string)
		return m.offlineHub.CRDTSync.GetCounterValue(key), nil

	case "crdt_register":
		key, _ := params["key"].(string)
		val, ok := m.offlineHub.CRDTSync.GetRegisterValue(key)
		if !ok {
			return nil, fmt.Errorf("register not found: %s", key)
		}
		return val, nil

	case "crdt_drain":
		return m.offlineHub.CRDTSync.DrainPendingOps(), nil

	case "devices_list":
		aliveOnly := true
		if v, ok := params["alive_only"].(bool); ok {
			aliveOnly = v
		}
		return m.offlineHub.DeviceDiscovery.ListDevices(aliveOnly), nil

	case "devices_prune":
		return m.offlineHub.DeviceDiscovery.PruneStale(), nil

	case "ota_releases":
		return m.offlineHub.OTA.ListReleases(), nil

	case "ota_create_rollout":
		releaseID, _ := params["release_id"].(string)
		totalNodes := 0
		if n, ok := params["total_nodes"].(float64); ok {
			totalNodes = int(n)
		}
		return m.offlineHub.OTA.CreateRollout(ctx, releaseID, totalNodes)

	case "ota_advance_stage":
		rolloutID, _ := params["rollout_id"].(string)
		return m.offlineHub.OTA.AdvanceStage(rolloutID)

	case "ota_rollout_status":
		rolloutID, _ := params["rollout_id"].(string)
		return m.offlineHub.OTA.GetRolloutStatus(rolloutID), nil

	case "ota_node_status":
		nodeID, _ := params["node_id"].(string)
		return m.offlineHub.OTA.GetNodeUpgradeStatus(nodeID), nil

	case "ota_rollback_node":
		nodeID, _ := params["node_id"].(string)
		err := m.offlineHub.OTA.RollbackNode(nodeID)
		if err != nil {
			return nil, err
		}
		return m.offlineHub.OTA.GetNodeUpgradeStatus(nodeID), nil

	case "status":
		return m.offlineHub.GetStatus(), nil

	default:
		return nil, fmt.Errorf("unknown offline capability action: %s", action)
	}
}

// EdgeCollabHub returns the edge collaboration hub for direct access
func (m *Manager) EdgeCollabHub() *EdgeCollabHub {
	return m.edgeCollabHub
}

// OfflineHub returns the offline capabilities hub for direct access
func (m *Manager) OfflineHub() *OfflineHub {
	return m.offlineHub
}
