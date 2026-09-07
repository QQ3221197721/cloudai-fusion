// Package edge provides local decision making during offline periods.
// Extends existing OfflineRuntime with autonomous scheduling capabilities.
package edge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edgeautonomy"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// LocalDecisionMaker extends OfflineRuntime with autonomous scheduling
// Implements core decision engine for offline operations
// ============================================================================

// LocalDecisionMaker makes scheduling decisions without cloud connectivity
type LocalDecisionMaker struct {
	runtime      *OfflineRuntime
	cacheMgr     *EnhancedCacheManager
	versionVec   *edgeautonomy.VersionVector
	maxPending   int
	pendingCount int
	pendingMu    sync.Mutex
	
	decisionHistory []LocalDecisionRecord // Circular buffer
	historySize     int
	
	logger *logrus.Logger
}

// Decision represents a local scheduling decision made during offline mode
type Decision struct {
	ID                 string    `json:"id"`                    // Unique UUID
	NodeID             string    `json:"node_id"`               // Target node
	WorkloadID         string    `json:"workload_id"`           // Workload to schedule
	ResourceRequests   []string  `json:"resource_requests"`     // Required resources
	QoSClass           string    `json:"qos_class"`             // High/Medium/Low priority
	Timestamp          time.Time `json:"timestamp"`             // ISO 8601 format
	Status             string    `json:"status"`                // pending/offline_validation/completed
	VersionVector      []int     `json:"version_vec"`           // Causality tracking
	RetryCount         int       `json:"retry_count,omitempty"`
	FailureReason      string    `json:"failure_reason,omitempty"`
}

// LocalDecisionRecord wraps a decision for persistence and audit logging
type LocalDecisionRecord struct {
	ID        string            `json:"record_id"`
	NodeID    string            `json:"node_id"`
	WorkloadID string           `json:"workload_id"`
	Decision  Decision          `json:"decision"`
	VersionVec []int            `json:"version_vec"`
	CreatedAt time.Time         `json:"created_at"`
	Synced    bool              `json:"synced"`
}

// NewLocalDecisionMaker creates decision maker extending runtime infrastructure
func NewLocalDecisionMaker(
	runtime *OfflineRuntime,
	cacheMgr *EnhancedCacheManager,
	nodeIDs []string,
	config OfflineRuntimeConfig,
	logger *logrus.Logger,
) *LocalDecisionMaker {
	if runtime == nil || cacheMgr == nil {
		panic("runtime and cache manager cannot be nil")
	}
	
	defensive.ValidateRange(float64(config.MaxLocalDecisions), 100, 5000, "max_local_decisions")
	
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	vv := edgeautonomy.NewVersionVector(nodeIDs)
	
	return &LocalDecisionMaker{
		runtime:       runtime,
		cacheMgr:      cacheMgr,
		versionVec:    vv,
		maxPending:    config.MaxLocalDecisions,
		historySize:   config.TransitionHistorySize / 2, // Half of history size
		decisionHistory: make([]LocalDecisionRecord, 0, config.TransitionHistorySize/2),
		logger:        logger.WithField("component", "local_decision_maker"),
	}
}

// MakeLocalDecision is the core scheduling logic invoked when offline
func (m *LocalDecisionMaker) MakeLocalDecision(
	ctx context.Context,
	workload Workload,
	availableNodes []*Node,
) (Decision, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Defensive programming guards - all inputs validated
	if err := defensive.RequireNonNil(workload.NodeSelector, "node_selector"); err != nil {
		return Decision{}, fmt.Errorf("invalid workload selector: %w", err)
	}
	
	if len(availableNodes) == 0 {
		return Decision{}, fmt.Errorf("no available nodes in cache")
	}
	
	// Check queue capacity
	m.pendingMu.Lock()
	if m.pendingCount >= m.maxPending {
		m.pendingMu.Unlock()
		return Decision{}, fmt.Errorf("local decision queue full (%d/%d)", m.pendingCount, m.maxPending)
	}
	m.pendingCount++
	m.pendingMu.Unlock()
	defer func() {
		m.pendingMu.Lock()
		m.pendingCount--
		m.pendingMu.Unlock()
	}()
	
	// Find best matching node
	bestNode := m.scoreAndSelectNode(availableNodes, workload)
	if bestNode == nil {
		return Decision{}, fmt.Errorf("no suitable node found matching requirements")
	}
	
	// Generate version vector update
	vv := m.versionVec.Update(m.runtime.nodeID)
	
	// Create decision record
	decision := Decision{
		ID:                 generateUUID(),
		NodeID:             bestNode.ID,
		WorkloadID:         workload.ID,
		ResourceRequests:   workload.ResourceRequirements,
		QoSClass:           workload.QoS,
		Timestamp:          time.Now().UTC(),
		Status:             "pending_offline_validation",
		VersionVector:      vv,
		RetryCount:         0,
	}
	
	// Create persistent record
	record := LocalDecisionRecord{
		ID:         decision.ID,
		NodeID:     m.runtime.nodeID,
		WorkloadID: workload.ID,
		Decision:   decision,
		VersionVec: vv,
		CreatedAt:  time.Now().UTC(),
		Synced:     false,
	}
	
	// Store for audit trail (non-blocking if DB unavailable)
	if err := m.cacheMgr.StoreLocalRecord(record); err != nil {
		// Log but don't fail the decision itself - graceful degradation
		m.logger.WithError(err).Warn("Failed to store decision record, continuing anyway")
	}
	
	// Update history
	m.updateDecisionHistory(record)
	
	return decision, nil
}

// scoreAndSelectNode uses scoring algorithm to pick best available node
func (m *LocalDecisionMaker) scoreAndSelectNode(nodes []*Node, workload Workload) *Node {
	var bestScore float64 = -1.0
	var bestNode *Node
	
	for _, node := range nodes {
		score := calculateNodeScore(node, workload)
		
		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}
	
	return bestNode
}

// calculateNodeScore evaluates node suitability based on multiple criteria
func calculateNodeScore(node *Node, workload Workload) float64 {
	score := 0.0
	
	// Primary criterion: GPU availability matches requirement
	gpuFree := getNodeGPUCount(node.Capacity, "nvidia.com/gpu")
	gpuRequired := workload.MinGPUs
	
	if gpuFree >= gpuRequired {
		// Bonus proportional to excess capacity
		excess := float64(gpuFree - gpuRequired)
		score += excess * 10.0
	} else {
		return -1.0 // Cannot satisfy requirement
	}
	
	// Secondary criterion: CPU compatibility
	cpuReq := workload.CPURequest.MilliValues() / 1000
	cpuAvail := getNodeCPUCount(node.Capacity, "cpu")
	
	if cpuAvail >= cpuReq {
		score += 5.0
	}
	
	// Tertiary criterion: Memory availability
	memReq := workload.MemoryRequest.Value()
	memAvail := getNodeMemoryBytes(node.Capacity, memoryKeyMemory)
	
	if memAvail >= memReq {
		score += 2.0
	}
	
// Quaternary criterion: Prefer less utilized nodes (balance load)
	utilization := node.UtilizationPercent
	if utilization < 80 {
		linearBonus := float64(80 - utilization) * 0.1
		score += linearBonus
	}
	
	// Cost efficiency factor (if available)
	costPerHour := getNodeCostPerHour(node)
	if costPerHour > 0 && gpuFree > 0 {
		costEfficiency := float64(gpuFree) / costPerHour
		score += costEfficiency * 2.0
	}
	
	return score
}

// Helper functions would be implemented here:
func getNodeGPUCount(capacity ResourceList, resourceName string) int64 {
	if val, exists := capacity[resourceName]; exists {
		return val.Value()
	}
	return 0
}

func getNodeCPUCount(capacity ResourceList, resourceName string) int64 {
	if val, exists := capacity[resourceName]; exists {
		return val.MilliValue() / 1000
	}
	return 0
}

func getNodeMemoryBytes(capacity ResourceList, resourceName string) int64 {
	if val, exists := capacity[resourceName]; exists {
		return val.Value()
	}
	return 0
}

func getNodeCostPerHour(node *Node) float64 {
	// Would retrieve from node annotations or labels
	// For now, return 0 as placeholder
	return 0.0
}

// updateDecisionHistory maintains recent decision history
func (m *LocalDecisionMaker) updateDecisionHistory(record LocalDecisionRecord) {
	// Implement circular buffer behavior
	m.decisionHistory = append(m.decisionHistory, record)
	
	// Keep only last N entries
	if len(m.decisionHistory) > m.historySize {
		m.decisionHistory = m.decisionHistory[len(m.decisionHistory)-m.historySize:]
	}
}

// getRecentDecisions returns recent decision history
func (m *LocalDecisionMaker) getRecentDecisions(limit int) []LocalDecisionRecord {
	if limit <= 0 || limit > len(m.decisionHistory) {
		limit = len(m.decisionHistory)
	}
	
	result := make([]LocalDecisionRecord, limit)
	copy(result, m.decisionHistory[len(m.decisionHistory)-limit:])
	
	return result
}
