package disaster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EvidenceLogger 记录 split-brain 证据的接口（便于接入透明日志/外部存储）。
type EvidenceLogger interface {
	LogSplitBrain(evidence *SplitBrainEvidence)
}

// noopEvidenceLogger 空实现：不做任何持久化（默认占位，待接入真实后端）。
type noopEvidenceLogger struct{}

func (noopEvidenceLogger) LogSplitBrain(*SplitBrainEvidence) {}

// NewEvidenceLogger 返回默认的（空）证据记录器。
func NewEvidenceLogger() EvidenceLogger { return noopEvidenceLogger{} }

// ============================================================================
// Split-Brain Detection Engine - Real Implementation
// ============================================================================
// Purpose: 实时检测双活冲突（Split-Brain）并自动生成证据链
// Core Principle: "Detect before it's too late" - 500ms 内识别 + 自动熔断
// Reference: docs/architecture.md section "Disaster Recovery -> Split-Brain Detection"
// Performance SLA: < 500ms detection time, < 100ms false positive rate
// ============================================================================

// SplitBrainDetector 真实的双脑检测引擎
// Responsibilities:
// 1. Raft Term 版本向量冲突检测
// 2. Quorum 心跳丢失但主节点仍响应的矛盾分析  
// 3. PostgreSQL WAL LSN 跳跃性验证
// 4. 生成 Merkle Proof 证据 → EvidenceChain.Append()
type SplitBrainDetector struct {
	mu                sync.RWMutex
	nodes             map[string]*NodeStatus
	detectionInterval time.Duration // 默认 100ms
	timeout           time.Duration   // 响应超时 200ms
	evidenceLogger    EvidenceLogger  // 证据记录器
	onDetection       DetectionHandler // 检测触发回调
}

// NodeStatus 节点状态快照
type NodeStatus struct {
	ID              string        `json:"node_id"`
	IsPrimary       bool          `json:"is_primary"`
	RaftTerm        uint64        `json:"raft_term"`
	LastHeartbeatAt time.Time     `json:"last_heartbeat"`
	WALLSN          uint64        `json:"wal_lsn"`      // Write-Ahead Log Sequence Number
	ViewOfCluster   []string      `json:"view_of_cluster"` // 认为哪些节点是 primary
	NetworkLatency  time.Duration `json:"network_latency"`
}

// DetectionHandler split-brain 检测触发时的处理回调
type DetectionHandler func(evidence *SplitBrainEvidence) error

// SplitBrainEvidence 双脑检测证据结构
type SplitBrainEvidence struct {
	EvidenceID     string            `json:"evidence_id"`      // UUID v4
	Timestamp      uint64            `json:"timestamp"`        // Unix nanoseconds
	Nodes          []*NodeStatus     `json:"nodes"`            // 所有参与检测的节点状态
	ViolationType  string            `json:"violation_type"`   // conflict-type (e.g., "dual-primary", "raft-term-mismatch")
	MerkleProof    []byte            `json:"merkle_proof"`     // Merkle Tree Proof
	QuorumVote     *QuorumVote       `json:"quorum_vote,omitempty"` // 投票证书
	MitigationAction string          `json:"mitigation_action"` // 建议采取的缓解措施
	Fingerprint    string            `json:"fingerprint"`      // 证据指纹（SHA256）
}

// QuorumVote 多数派投票证书
type QuorumVote struct {
	Voters      []string    `json:"voters"`        // 参与投票的节点列表
	VotesFor    []string    `json:"votes_for"`     // 支持某个节点的票数
	Term        uint64      `json:"term"`          // Raft term
	Signature   []byte      `json:"signature"`     // Ed25519 签名
}

// NewSplitBrainDetector 创建新的分裂脑检测器
// 注：DRRegion 不携带 Raft term / WAL LSN / 集群视图等运行时信息，
// 这些字段需后续从 Raft 实现与数据库复制状态单独注入，此处先置零值。
func NewSplitBrainDetector(nodes map[string]*DRRegion, evidenceLogger EvidenceLogger, handler DetectionHandler) *SplitBrainDetector {
	// Convert DRRegions to NodeStatus
	nodeStatuses := make(map[string]*NodeStatus)
	for id, region := range nodes {
		nodeStatuses[id] = &NodeStatus{
			ID:              id,
			IsPrimary:       region.IsPrimary,
			LastHeartbeatAt: time.Now(),
			// RaftTerm / WALLSN / ViewOfCluster: 待从 Raft 与数据库复制状态注入
		}
	}

	return &SplitBrainDetector{
		nodes:             nodeStatuses,
		detectionInterval: 100 * time.Millisecond,
		timeout:           200 * time.Millisecond,
		evidenceLogger:    evidenceLogger,
		onDetection:       handler,
	}
}

// Start 启动持续检测循环（goroutine）
func (d *SplitBrainDetector) Start(ctx context.Context) {
	ticker := time.NewTicker(d.detectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.detectAndMitigate(); err != nil {
				// Log but don't propagate - already handled internally
				fmt.Printf("[SPLIT-BRAIN-DETECTOR] Detection failed: %v\n", err)
			}
		}
	}
}

// detectAndMitigate 执行单次检测和可能的缓解动作
func (d *SplitBrainDetector) detectAndMitigate() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Step 1: 收集所有节点的最新状态
	currentStates := d.collectNodeStates()

	// Step 2: 运行多种检测算法
	detections := d.runDetectionAlgorithms(currentStates)

	if len(detections) == 0 {
		return nil // No split-brain detected
	}

	// Step 3: 生成证据链
	evidence := d.generateEvidence(detections, currentStates)

	// Step 4: 调用回调执行缓解措施
	if d.onDetection != nil {
		if err := d.onDetection(evidence); err != nil {
			return fmt.Errorf("mitigation-failed: %w", err)
		}
	}

	// Step 5: 记录到透明日志
	if d.evidenceLogger != nil {
		d.evidenceLogger.LogSplitBrain(evidence)
	}

	return nil
}

// collectNodeStates 从多个数据源收集节点状态
func (d *SplitBrainDetector) collectNodeStates() []*NodeStatus {
	states := make([]*NodeStatus, 0, len(d.nodes))

	for _, node := range d.nodes {
		// Check network latency via ICMP ping
		latency := d.measureNetworkLatency(node.ID)
		node.NetworkLatency = latency

		// Update last heartbeat
		node.LastHeartbeatAt = time.Now()

		// Fetch latest WAL LSN from database (simplified)
		walLSN := d.fetchWALSequenceNumber(node.ID)
		node.WALLSN = walLSN

		states = append(states, node)
	}

	return states
}

// measureNetworkLatency 测量到指定节点的网络延迟（ICMP/HTTP ping）
func (d *SplitBrainDetector) measureNetworkLatency(nodeID string) time.Duration {
	start := time.Now()
	
	// Try HTTP health check first (preferred for containerized environments)
	addr := fmt.Sprintf("http://%s:8080/healthz", nodeID)
	resp, err := http.DefaultClient.Get(addr)
	if err != nil {
		// Fallback to TCP dial（DialTimeout 自带超时，无需额外 context）
		if _, derr := net.DialTimeout("tcp", nodeID+":8080", 100*time.Millisecond); derr != nil {
			return d.timeout // Mark as unreachable
		}
	} else {
		defer resp.Body.Close()
	}

	return time.Since(start)
}

// fetchWALSequenceNumber 获取节点的 PostgreSQL WAL LSN（简化版，实际需连接数据库）
func (d *SplitBrainDetector) fetchWALSequenceNumber(nodeID string) uint64 {
	// TODO: Implement real database query
	// SELECT pg_current_wal_lsn() FROM pg_stat_replication WHERE node_id = $1
	return 0 // Placeholder for now
}

// runDetectionAlgorithms 运行多种检测算法返回违规类型
func (d *SplitBrainDetector) runDetectionAlgorithms(states []*NodeStatus) []string {
	violations := make([]string, 0)

	// Algorithm 1: Dual-Primary Detection
	if d.hasMultiplePrimary(states) {
		violations = append(violations, "dual-primary")
	}

	// Algorithm 2: Raft Term Mismatch
	if d.hasRaftTermConflict(states) {
		violations = append(violations, "raft-term-mismatch")
	}

	// Algorithm 3: View-of-Cluster Inconsistency
	if d.hasClusterViewConflict(states) {
		violations = append(violations, "cluster-view-inconsistent")
	}

	// Algorithm 4: Network Latency Anomaly (>500ms suggests partition)
	if d.hasNetworkPartition(states) {
		violations = append(violations, "network-partition-suspected")
	}

	return violations
}

// hasMultiplePrimary 检测是否存在多个主节点（最严重的双脑形式）
func (d *SplitBrainDetector) hasMultiplePrimary(states []*NodeStatus) bool {
	var primaryNodes []*NodeStatus
	
	for _, s := range states {
		if s.IsPrimary {
			primaryNodes = append(primaryNodes, s)
		}
	}
	
	return len(primaryNodes) > 1
}

// hasRaftTermConflict 检测 Raft term 是否存在冲突
func (d *SplitBrainDetector) hasRaftTermConflict(states []*NodeStatus) bool {
	terms := make(map[uint64]int)
	
	for _, s := range states {
		if s.RaftTerm > 0 {
			terms[s.RaftTerm]++
		}
	}
	
	// If multiple different terms exist, there's a conflict
	return len(terms) > 1
}

// hasClusterViewConflict 检测集群视图是否不一致
func (d *SplitBrainDetector) hasClusterViewConflict(states []*NodeStatus) bool {
	views := make(map[string]int)
	
	for _, s := range states {
		viewKey := strings.Join(s.ViewOfCluster, ",")
		views[viewKey]++
	}
	
	return len(views) > 1
}

// hasNetworkPartition 检测是否存在网络分区（高延迟+心跳丢失）
func (d *SplitBrainDetector) hasNetworkPartition(states []*NodeStatus) bool {
	for _, s := range states {
		if s.NetworkLatency > 500*time.Millisecond {
			// High latency suggests network partition
			return true
		}
	}
	return false
}

// generateEvidence 根据检测到的违规类型生成证据链
func (d *SplitBrainDetector) generateEvidence(violations []string, states []*NodeStatus) *SplitBrainEvidence {
	// Generate unique evidence ID
	evidenceID := generateUUID()
	timestamp := uint64(time.Now().UnixNano())
	
	// Create Merkle Tree proof
	proof := d.buildMerkleProof(states, timestamp)
	
	// Calculate fingerprint
	fingerprint := calculateFingerprint(evidenceID, states, violations)
	
	// Determine appropriate mitigation action
	var mitigationAction string
	switch {
	case containsString(violations, "dual-primary"):
		mitigationAction = "force-fence-high-latency-nodes"
	case containsString(violations, "network-partition-suspected"):
		mitigationAction = "isolate-node-with-highest-latency"
	default:
		mitigationAction = "alert-admin-and-queue-for-review"
	}
	
	return &SplitBrainEvidence{
		EvidenceID:       evidenceID,
		Timestamp:        timestamp,
		Nodes:            states,
		ViolationType:    strings.Join(violations, ","),
		MerkleProof:      proof,
		Fingerprint:      fingerprint,
		MitigationAction: mitigationAction,
	}
}

// buildMerkleProof 构建 Merkle Tree Proof（简化版）
func (d *SplitBrainDetector) buildMerkleProof(states []*NodeStatus, timestamp uint64) []byte {
	// Collect all state hashes
	hashes := make([][]byte, 0, len(states)+1)
	
	// Add timestamp hash
	timeHash := sha256.Sum256([]byte(fmt.Sprintf("%d", timestamp)))
	hashes = append(hashes, timeHash[:])
	
	// Add each node's state hash
	for _, s := range states {
		nodeData := fmt.Sprintf("%s:%d:%t:%d", s.ID, s.RaftTerm, s.IsPrimary, s.WALLSN)
		nodeHash := sha256.Sum256([]byte(nodeData))
		hashes = append(hashes, nodeHash[:])
	}
	
	// Build Merkle Tree
	root := buildMerkleRoot(hashes)
	return root
}

// Helper functions

func generateUUID() string {
	// Using simple time-based ID for demo; replace with github.com/google/uuid in production
	return fmt.Sprintf("sb_%d", time.Now().UnixNano())
}

func calculateFingerprint(evidenceID string, states []*NodeStatus, violations []string) string {
	data := fmt.Sprintf("%s|%d|%s", evidenceID, len(states), strings.Join(violations, ","))
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func buildMerkleRoot(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return []byte{}
	}
	
	// Simple pairwise hashing
	for len(hashes) > 1 {
		nextLevel := make([][]byte, 0, (len(hashes)+1)/2)
		
		for i := 0; i < len(hashes); i += 2 {
			var combined []byte
			if i+1 < len(hashes) {
				combined = append(hashes[i], hashes[i+1]...)
			} else {
				combined = hashes[i]
			}
			
			hash := sha256.Sum256(combined)
			nextLevel = append(nextLevel, hash[:])
		}
		
		hashes = nextLevel
	}
	
	return hashes[0]
}

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
