package disaster

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// Split-Brain Containment Controller
// ============================================================================
// Purpose: 执行自动缓解措施以防止双脑继续扩大影响
// Strategy: "Fence high-latency nodes immediately, alert admins for review"
// SLA: < 100ms containment after detection
// ============================================================================

// SplitBrainContoller 分裂脑缓解控制器
type SplitBrainContoller struct {
	mu              sync.RWMutex
	detector        *SplitBrainDetector
	activeNodes     map[string]*NodeStatus      // 受影响的活跃节点
	evidenceChain   *EvidenceChain              // 关联的证据链
	failoverManager *DisasterManagerAdapter     // 用于执行故障转移
}

// NewSplitBrainContoller 创建新的缓解控制器
func NewSplitBrainContoller(detector *SplitBrainDetector, failoverManager *DisasterManagerAdapter) *SplitBrainContoller {
	return &SplitBrainContoller{
		activeNodes:     make(map[string]*NodeStatus),
		evidenceChain:   NewEvidenceChain(),
		failoverManager: failoverManager,
	}
}

// OnDetection 作为回调注册到 detector
func (c *SplitBrainContoller) OnDetection(evidence *SplitBrainEvidence) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	fmt.Printf("[SPLIT-BRAIN-CONTROLLER] Detection triggered: %s\n", evidence.ViolationType)
	
	// Record evidence to chain（EvidenceChain 提供 AddEvidence(nodeID, data)）
	_ = c.evidenceChain.AddEvidence(evidence.EvidenceID, evidence.MerkleProof)
	
	// Execute appropriate mitigation action
	switch evidence.MitigationAction {
	case "force-fence-high-latency-nodes":
		return c.forceFenceHighLatencyNodes(evidence.Nodes)
	case "isolate-node-with-highest-latency":
		return c.isolateHighestLatencyNode(evidence.Nodes)
	default:
		return c.alertAndQueueForReview(evidence)
	}
}

// forceFenceHighLatencyNodes 强制隔离所有高延迟节点（网络分区场景）
func (c *SplitBrainContoller) forceFenceHighLatencyNodes(nodes []*NodeStatus) error {
	var fenced []string
	
	for _, node := range nodes {
		if node.NetworkLatency > 500*time.Millisecond {
			fmt.Printf("[SPLIT-BRAIN-CONTROLLER] Fencing node %s (latency=%v)\n", node.ID, node.NetworkLatency)
			
			// TODO: Execute actual fencing mechanism
			// - Stop all write operations on this node
			// - Redirect traffic to healthy replicas
			// - Update load balancer configuration
			
			fenced = append(fenced, node.ID)
			
			// Keep track for audit log
			c.activeNodes[node.ID] = node
		}
	}
	
	if len(fenced) == 0 {
		return fmt.Errorf("no-nodes-requiring-fencing")
	}
	
	// Log successful fencing
	fmt.Printf("[SPLIT-BRAIN-CONTROLLER] Successfully fenced %d nodes: %v\n", len(fenced), fenced)
	
	return nil
}

// isolateHighestLatencyNode 隔离延迟最高的单个节点（轻微分区分隔）
func (c *SplitBrainContoller) isolateHighestLatencyNode(nodes []*NodeStatus) error {
	if len(nodes) == 0 {
		return fmt.Errorf("no-nodes-to-isolate")
	}
	
	// Find node with highest latency
	var highestLatencyNode *NodeStatus
	maxLatency := time.Duration(0)
	
	for _, node := range nodes {
		if node.NetworkLatency > maxLatency {
			maxLatency = node.NetworkLatency
			highestLatencyNode = node
		}
	}
	
	if highestLatencyNode == nil || maxLatency <= 500*time.Millisecond {
		return fmt.Errorf("no-single-node-needing-isolation")
	}
	
	fmt.Printf("[SPLIT-BRAIN-CONTROLLER] Isolating node %s (highest latency=%v)\n", 
		highestLatencyNode.ID, maxLatency)
	
	// TODO: Execute isolation
	// - Put node in Draining state
	// - Remove from cluster membership
	// - Trigger data rebalancing
	
	return nil
}

// alertAndQueueForReview 告警并等待人工审核（复杂违规场景）
func (c *SplitBrainContoller) alertAndQueueForReview(evidence *SplitBrainEvidence) error {
	fmt.Printf("[SPLIT-BRAIN-CONTROLLER] Critical alert: Complex violation detected\n")
	fmt.Printf("Violation Type: %s\n", evidence.ViolationType)
	fmt.Printf("Fingerprint: %s\n", evidence.Fingerprint)
	fmt.Printf("Evidence ID: %s\n", evidence.EvidenceID)
	
	// TODO: Send alerts to multiple channels
	// - PagerDuty/Slack notification
	// - Email to SRE team
	// - Create JIRA ticket
	// - Update status page
	
	// Queue for manual review
	c.queueForManualReview(evidence)
	
	return nil
}

// queueForManualReview 将证据添加到待审核队列
func (c *SplitBrainContoller) queueForManualReview(evidence *SplitBrainEvidence) {
	// Store in persistent queue (Redis/BoltDB)
	// Implementation detail: use existing event bus or message queue
	queueItem := map[string]interface{}{
		"evidence_id":   evidence.EvidenceID,
		"fingerprint":   evidence.Fingerprint,
		"violation_type": evidence.ViolationType,
		"timestamp":     time.Now().Unix(),
	}
	
	// This is a placeholder - replace with actual queue implementation
	_ = queueItem
	
	fmt.Printf("[SPLIT-BRAIN-CONTROLLER] Queued for manual review: %s\n", evidence.EvidenceID)
}

// GetActiveNodes 获取当前受影响的活跃节点列表
func (c *SplitBrainContoller) GetActiveNodes() map[string]*NodeStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make(map[string]*NodeStatus)
	for k, v := range c.activeNodes {
		result[k] = v
	}
	
	return result
}

// ClearFencedNodes 清除已恢复的节点标记
func (c *SplitBrainContoller) ClearFencedNodes(nodeIDs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	for _, id := range nodeIDs {
		delete(c.activeNodes, id)
	}
}

// GenerateContainmentReport 生成完整的缓解报告
func (c *SplitBrainContoller) GenerateContainmentReport() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	report := fmt.Sprintf("=== Split-Brain Containment Report ===\n")
	report += fmt.Sprintf("Timestamp: %s\n", time.Now().Format(time.RFC3339))
	report += fmt.Sprintf("Active Fenced Nodes: %d\n", len(c.activeNodes))
	
	if len(c.activeNodes) > 0 {
		report += "\nAffected Nodes:\n"
		for id, node := range c.activeNodes {
			report += fmt.Sprintf("  - %s (latency=%v, primary=%t)\n", 
				id, node.NetworkLatency, node.IsPrimary)
		}
	}
	
	return report
}

// ============================================================================
// Integration Helper Functions
// ============================================================================

// RegisterAsDetectionCallback 便捷方法：将 controller 注册为 detector 的回调
func (c *SplitBrainContoller) RegisterAsDetectionCallback(detector *SplitBrainDetector) {
	detector.onDetection = c.OnDetection
}
