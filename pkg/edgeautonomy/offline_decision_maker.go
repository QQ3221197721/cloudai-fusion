// Package edgeautonomy - Offline decision maker for edge autonomy.
package edgeautonomy

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Offline Decision Maker Core Algorithm
// ============================================================================

// OfflineDecisionMaker makes decisions locally when network partition occurs
type OfflineDecisionMaker struct {
	mu            sync.RWMutex
	versionVector *VersionVector
	cacheMgr      *CacheManager
	dbStore       interface{} // Database for persistence
	logger        interface{} // Logger
	k8sClient     interface{} // K8s client for local execution
	config        *Config    // Configuration reference
}

// Config holds configuration for OfflineDecisionMaker
type Config struct {
	NodeID        string
	VersionVector *VersionVector
	CacheManager  *CacheManager
	DBStore       interface{}
	Logger        interface{}
	K8sClient     interface{}
}

// NodeID returns unique identifier for this node
func (o *OfflineDecisionMaker) NodeID() string {
	if o.config != nil && o.config.NodeID != "" {
		return o.config.NodeID
	}
	if o.cacheMgr != nil {
		return o.cacheMgr.NodeID()
	}
	return "edge-node-01"
}

// MakeLocalDecision creates a decision based on cached node data and offline policies
func (o *OfflineDecisionMaker) MakeLocalDecision(ctx context.Context, workload WorkloadRequest, availableNodes []*Node) (*DecisionResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Validate inputs
	if len(availableNodes) == 0 {
		return nil, fmt.Errorf("no available nodes in cache")
	}

	// Update version vector with this node's perspective
	_ = o.versionVector.Update(o.NodeID())

	// Find best matching node using scoring algorithm
	bestNode := o.scoreAndSelectNode(availableNodes, workload)
	if bestNode == nil {
		return nil, fmt.Errorf("no suitable node found matching requirements")
	}

	// Create decision record
	result := &DecisionResult{
		Action:    ActionScaleUp.Ptr(),
		Target:    DecisionTarget{Type: "workload", Name: workload.Name, Namespace: workload.Namespace},
		Confidence: 0.85,
		CreatedAt: time.Now(),
		IsOffline: true,
		Priority:  workload.Priority,
		Cause:     "Network partition detected, executing offline decision",
		Metrics: map[string]float64{
			"node_gpu_utilization": bestNode.GPUUtilization,
			"node_cpu_usage":       bestNode.CPUUsage,
			"memory_available_gb":  float64(bestNode.MemoryAvailableGB),
		},
	}

	return result, nil
}

// scoreAndSelectNode uses weighted scoring to pick the best available node
func (o *OfflineDecisionMaker) scoreAndSelectNode(nodes []*Node, workload WorkloadRequest) *Node {
	var bestScore float64 = -1.0
	var bestNode *Node

	for _, node := range nodes {
		score := o.calculateNodeScore(node, workload)

		if score > bestScore {
			bestScore = score
			bestNode = node
		}
	}

	return bestNode
}

// calculateNodeScore evaluates node suitability based on GPU topology, resources, and affinity
func (o *OfflineDecisionMaker) calculateNodeScore(node *Node, workload WorkloadRequest) float64 {
	score := 0.0

	// Primary criterion: GPU availability matches requirement
	gpuFree := node.GPUCount - node.UsedGPUCount
	gpuReq := workload.ResourceRequest.GPUMemoryMiB / 1024 // Convert MiB to GB

	if gpuFree >= int(gpuReq) {
		excess := float64(gpuFree - int(gpuReq))
		score += excess * 10.0
	} else {
		return -1.0 // Cannot satisfy requirement
	}

	// Secondary criterion: GPU topology requirements
	if workload.GPUTopologyReq != nil && workload.GPUTopologyReq.RequireNVLink {
		if node.HasNVLink && node.NVLinkBandwidthGB >= workload.GPUTopologyReq.MinNVLinkBandwidthGB {
			score += 20.0 // Bonus for meeting NVLink requirement
		} else {
			return -1.0 // Don't schedule if NVLink required but not available
		}
	}

	// Tertiary criterion: CPU compatibility
	cpuReq := parseCPU(workload.ResourceRequest.CPURequest)
	if cpuReq <= float64(node.CPUCount) {
		score += 5.0
	}

	// Quaternary criterion: Memory availability
	memReq := parseMemory(workload.ResourceRequest.MemoryRequest)
	if float64(memReq) <= node.MemoryAvailableGB * 1024.0 {
		score += 3.0
	}

	// Penalty for low utilization (prefer less loaded nodes)
	if node.GPUUtilization < 80.0 {
		linearBonus := float64(80.0 - node.GPUUtilization) * 0.1
		score += linearBonus
	}

	// Cost efficiency factor (if cost data available)
	if node.CostPerHour > 0 && gpuFree > 0 {
		costEfficiency := float64(gpuFree) / node.CostPerHour
		score += costEfficiency * 2.0
	}

	// Affinity scoring
	if workload.Affinity.NodeAffinity != nil {
		if o.evaluateNodeAffinity(node, workload.Affinity.NodeAffinity) {
			score += 15.0
		} else {
			return -1.0
		}
	}

	// Topology spread scoring
	if workload.Affinity.TopologySpread != nil {
		score += o.evaluateTopologySpread(node, workload.Affinity.TopologySpread)
	}

	return score
}

// evaluateNodeAffinity checks if node satisfies affinity constraints
func (o *OfflineDecisionMaker) evaluateNodeAffinity(node *Node, affinity *NodeAffinity) bool {
	if affinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		required := affinity.RequiredDuringSchedulingIgnoredDuringExecution
		for _, expr := range required.MatchExpressions {
			value, exists := node.Labels[expr.Key]
			switch expr.Operator {
			case "In":
				if !containsString(expr.Values, value) {
					return false
				}
			case "NotIn":
				if containsString(expr.Values, value) {
					return false
				}
			case "Exists":
				if !exists {
					return false
				}
			case "DoesNotExist":
				if exists {
					return false
				}
			}
		}
	}

	return true
}

// evaluateTopologySpread calculates spread satisfaction score
func (o *OfflineDecisionMaker) evaluateTopologySpread(node *Node, topology *TopologySpread) float64 {
	// Simplified: return score based on current distribution
	// In production, would query all pods across topology domains
	return float64(topology.MaxSkew) * 10.0
}

// ExecuteLocally executes an action directly via local K8s API
func (o *OfflineDecisionMaker) ExecuteLocally(ctx context.Context, action *DecisionAction, target DecisionTarget) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.k8sClient == nil {
		return fmt.Errorf("K8s client not initialized")
	}

	switch *action {
	case ActionScaleUp:
		return o.executeScaling(ctx, target, 1)
	case ActionScaleDown:
		return o.executeScaling(ctx, target, -1)
	case ActionRestart:
		return o.executePodRestart(ctx, target)
	case ActionMigrate:
		return o.executeWorkloadMigration(ctx, target, "")
	default:
		return fmt.Errorf("unsupported action: %s", *action)
	}
}

// executeScaling scales deployment up/down
func (o *OfflineDecisionMaker) executeScaling(ctx context.Context, target DecisionTarget, delta int32) error {
	// Real K8s API call via client-go
	// This is where we'd actually call: deployment.Spec.Replicas += delta
	fmt.Printf("[MOCK K8s API] Scaling %s/%s by +%d\n", target.Namespace, target.Name, delta)
	return nil
}

// executePodRestart restarts pods by triggering deployment rollout
func (o *OfflineDecisionMaker) executePodRestart(ctx context.Context, target DecisionTarget) error {
	// Delete pods to trigger rolling restart
	fmt.Printf("[MOCK K8s API] Restarting pods in %s/%s\n", target.Namespace, target.Name)
	return nil
}

// executeWorkloadMigration migrates workload to different node
func (o *OfflineDecisionMaker) executeWorkloadMigration(ctx context.Context, target DecisionTarget, newNode string) error {
	// Apply new scheduling constraints to force reschedule
	fmt.Printf("[MOCK K8s API] Migrating %s to node %s\n", target.Name, newNode)
	return nil
}

// StoreLocalRecord persists a decision for audit trail
func (o *OfflineDecisionMaker) StoreLocalRecord(ctx context.Context, entry *DecisionLogEntry) error {
	// Persist to database
	if o.dbStore != nil {
		// TODO: Implement actual DB storage
		entry.Version++
	}
	return nil
}

// GetPreviousState loads previous state from persistent storage
func (o *OfflineDecisionMaker) getPreviousState(ctx context.Context, version int64) ([]byte, error) {
	// Load from database or file system
	if o.dbStore != nil {
		// TODO: Implement actual DB query
		return nil, nil
	}
	return nil, fmt.Errorf("state not available")
}

// Ptr returns pointer to action
func (a DecisionAction) Ptr() *DecisionAction {
	return &a
}

// Helper functions
func parseCPU(cpuStr string) float64 {
	// Parse CPU string like "2" or "2000m"
	if strings.HasSuffix(cpuStr, "m") {
		val := strings.TrimSuffix(cpuStr, "m")
		f, _ := strconv.ParseFloat(val, 64)
		return f / 1000.0
	}
	f, _ := strconv.ParseFloat(cpuStr, 64)
	return f
}

func parseMemory(memStr string) int64 {
	// Parse memory string like "4Gi" or "4096Mi"
	if strings.HasSuffix(memStr, "Gi") {
		val := strings.TrimSuffix(memStr, "Gi")
		f, _ := strconv.ParseFloat(val, 64)
		return int64(f * 1024 * 1024 * 1024)
	}
	if strings.HasSuffix(memStr, "Mi") {
		val := strings.TrimSuffix(memStr, "Mi")
		f, _ := strconv.ParseFloat(val, 64)
		return int64(f * 1024 * 1024)
	}
	val, _ := strconv.ParseInt(memStr, 10, 64)
	return val
}

func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
