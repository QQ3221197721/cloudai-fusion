// Package elasticpool implements cross-cluster GPU resource pooling mechanisms.
// It provides two architectures:
//   - Centralized: Single controller manages all clusters' elastic pools
//   - Federated: Distributed consensus among cluster nodes (CRDT-based)
//
// This package is designed for M12 Elastic Pool → Design phase (Alex's research).
// The goal is to enable future T2 benchmark comparison with AWS Inferentia / Google TPU Pod Pool.
package elasticpool

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Core Errors
// ============================================================================

var (
	// ErrInsufficientCapacity indicates global pool lacks required resources.
	ErrInsufficientCapacity = errors.New("elasticpool: insufficient global capacity")
	
	// ErrAllocationTimeout indicates gang allocation did not complete within deadline.
	ErrAllocationTimeout = errors.New("elasticpool: allocation timeout exceeded")
	
	// ErrClusterNotConnected indicates a cluster is unreachable.
	ErrClusterNotConnected = errors.New("elasticpool: cluster not connected")
	
	// ErrPartitionDetected indicates network partition during allocation.
	ErrPartitionDetected = errors.New("elasticpool: network partition detected")
)

// ============================================================================
// Configuration
// ============================================================================

// ElasticPoolConfig configures an elastic pool instance.
type ElasticPoolConfig struct {
	// HeartbeatInterval controls how frequently clusters report status.
	HeartbeatInterval time.Duration
	// AllocationTimeout defines maximum wait time for gang scheduling.
	AllocationTimeout time.Duration
	// EvidenceEnabled signs all allocation decisions for auditability.
	EvidenceEnabled bool
	// LeaderElection enables HA mode with Raft leader election (centralized only).
	LeaderElection bool
}

// DefaultElasticPoolConfig returns sensible defaults.
func DefaultElasticPoolConfig() ElasticPoolConfig {
	return ElasticPoolConfig{
		HeartbeatInterval:   5 * time.Second,
		AllocationTimeout:   30 * time.Second,
		EvidenceEnabled:     true,
		LeaderElection:      true,
	}
}

// ============================================================================
// Data Models
// ============================================================================

// NodeDescriptor represents a node's advertised capacity in the global pool.
type NodeDescriptor struct {
	NodeID          string
	ClusterID       string
	CPU             int           // millicores
	MemoryGB        float64       // gigabytes
	GPUs            int           // total devices
	GPUModel        string        // e.g., "A100-80GB", "H100"
	AllocatedCPU    int
	AllocatedMemory float64
	AllocatedGPU    int
	Labels          map[string]string
	Taints          []string
	Status          NodeStatus
	
	mu sync.RWMutex
}

// NodeStatus describes physical node state.
type NodeStatus string

const (
	NodeStatusReady     NodeStatus = "ready"
	NodeStatusNotReady  NodeStatus = "not-ready"
	NodeStatusOffline   NodeStatus = "offline"
	NodeStatusDraining  NodeStatus = "draining"
)

// Remaining returns unallocated resources.
func (n *NodeDescriptor) Remaining() *NodeDescriptor {
	n.mu.RLock()
	defer n.mu.RUnlock()
	
	return &NodeDescriptor{
		NodeID:          n.NodeID,
		ClusterID:       n.ClusterID,
		CPU:             n.CPU - n.AllocatedCPU,
		MemoryGB:        n.MemoryGB - n.AllocatedMemory,
		GPUs:            n.GPUs - n.AllocatedGPU,
		GPUModel:        n.GPUModel,
		AllocatedCPU:    0,
		AllocatedMemory: 0,
		AllocatedGPU:    0,
		Labels:          n.Labels,
		Taints:          n.Taints,
		Status:          n.Status,
	}
}

// WorkerSpec defines per-worker resource requirements.
type WorkerSpec struct {
	CPU    int   // millicores
	Memory float64 // GB
	GPU    int
	ModelConstraints []string // preferred GPU models (e.g., ["H100", "A100"])
}

// GangAllocationRequest describes an ML training job submission.
type GangAllocationRequest struct {
	RequestID   string
	JobID       string
	WorkerCount int
	WorkerSpec  WorkerSpec
	MinQuorum   int // minimum workers needed (default = WorkerCount)
	Priority    int // higher = scheduled first (default = 0)
	CreatedAt   time.Time
}

// Assignment maps a worker to a physical node and cluster.
type Assignment struct {
	WorkerID  string
	NodeID    string
	ClusterID string
}

// AllocationDecision is the result of scheduling a gang.
type AllocationDecision struct {
	RequestID   string
	Decision    string // accepted | rejected | pending
	Assignments []Assignment
	Evidence    *evidence.Evidence
	Timestamp   time.Time
	Error       string // if decision = rejected
}

// ============================================================================
// Centralized Controller
// ============================================================================

// CentralizedController implements single-point-of-truth elastic pool management.
type CentralizedController struct {
	config    ElasticPoolConfig
	signer    evidence.Signer
	recorder  evidence.Recorder
	
	nodes    map[string]*NodeDescriptor // nodeID -> NodeDescriptor
	clusters map[string]bool            // clusterID -> exists
	mu       sync.RWMutex
	
	logger  interface{} // placeholder for actual logger
}

// NewCentralizedController creates a centralized elastic pool controller.
func NewCentralizedController(cfg ElasticPoolConfig) *CentralizedController {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = DefaultElasticPoolConfig().HeartbeatInterval
	}
	if cfg.AllocationTimeout == 0 {
		cfg.AllocationTimeout = DefaultElasticPoolConfig().AllocationTimeout
	}
	
	return &CentralizedController{
		config:   cfg,
		nodes:    make(map[string]*NodeDescriptor),
		clusters: make(map[string]bool),
	}
}

// SetEvidenceConfig wires up signing/recording for auditability.
func (c *CentralizedController) SetEvidenceConfig(signer evidence.Signer, recorder evidence.Recorder) {
	c.signer = signer
	c.recorder = recorder
}

// RegisterNode adds or updates a node in the global pool.
func (c *CentralizedController) RegisterNode(ctx context.Context, node *NodeDescriptor) error {
	if node == nil || node.NodeID == "" {
		return fmt.Errorf("elasticpool: invalid node descriptor")
	}
	
	if node.Status == "" {
		node.Status = NodeStatusReady
	}
	
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Register new cluster if needed
	if _, exists := c.clusters[node.ClusterID]; !exists {
		c.clusters[node.ClusterID] = true
	}
	
	// Merge existing allocations into new descriptor
	if existing, ok := c.nodes[node.NodeID]; ok {
		node.AllocatedCPU = existing.AllocatedCPU
		node.AllocatedMemory = existing.AllocatedMemory
		node.AllocatedGPU = existing.AllocatedGPU
	}
	
	c.nodes[node.NodeID] = node
	
	_ = capability.Report("elasticpool.node.register", node.NodeID, 
		capability.ModeProduction, fmt.Sprintf("Registered node %s in cluster %s", node.NodeID, node.ClusterID))
	
	return nil
}

// Allocate submits a gang allocation request and attempts placement.
// Implements strict all-or-nothing semantics.
func (c *CentralizedController) Allocate(ctx context.Context, req *GangAllocationRequest) (*AllocationDecision, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	
	if req.WorkerCount <= 0 {
		return nil, fmt.Errorf("elasticpool: worker count must be positive")
	}
	
	startTime := time.Now()
	
	// Validate quorum
	minQuorum := req.MinQuorum
	if minQuorum <= 0 {
		minQuorum = req.WorkerCount
	}
	if minQuorum > req.WorkerCount {
		minQuorum = req.WorkerCount
	}
	
	// Attempt greedy placement with rollback
	decision := &AllocationDecision{
		RequestID: req.RequestID,
		Timestamp: time.Now(),
	}
	
	// Lock-free read for initial capacity check
	c.mu.RLock()
	nodeIDs := make([]string, 0, len(c.nodes))
	for nid := range c.nodes {
		nodeIDs = append(nodeIDs, nid)
	}
	c.mu.RUnlock()
	
	// Sort for deterministic placement
	sort.Strings(nodeIDs)
	
	// Tentative placements
	var tentativePlacements []Assignment
	nodeResources := make(map[string]*NodeDescriptor)
	
	for nid := range c.nodes {
		c.mu.RLock()
		if n, ok := c.nodes[nid]; ok {
			nodeCopy := *n
			nodeResources[nid] = &nodeCopy
		}
		c.mu.RUnlock()
	}
	
	// Try to place all workers
	for i := 0; i < req.WorkerCount; i++ {
		placed := false
		
		for _, nid := range nodeIDs {
			res := nodeResources[nid]
			if res == nil {
				continue
			}
			
			// Check resource constraints
			canAllocate := res.CPU-res.AllocatedCPU >= req.WorkerSpec.CPU &&
				res.MemoryGB-res.AllocatedMemory >= req.WorkerSpec.Memory &&
				res.GPUs-res.AllocatedGPU >= req.WorkerSpec.GPU
			
			if canAllocate {
				workerID := fmt.Sprintf("%s-worker-%d", req.JobID, i)
				
				tentativePlacements = append(tentativePlacements, Assignment{
					WorkerID:  workerID,
					NodeID:    nid,
					ClusterID: res.ClusterID,
				})
				
				// Update residual capacity
				res.AllocatedCPU += req.WorkerSpec.CPU
				res.AllocatedMemory += req.WorkerSpec.Memory
				res.AllocatedGPU += req.WorkerSpec.GPU
				
				placed = true
				break
			}
		}
		
		if !placed {
			// Rollback: release all tentative assignments
			decision.Decision = "rejected"
			decision.Error = fmt.Sprintf("cannot place worker %d: no suitable node found", i)
			decision.Timestamp = time.Now()
			decision.Assignments = nil
			
			// Record rejection evidence
			if c.recorder != nil {
				_ = c.rejectEvidence(ctx, req, decision.Error)
			}
			
			return decision, nil
		}
	}
	
	// All workers placed successfully
	decision.Decision = "accepted"
	decision.Assignments = tentativePlacements
	
	// Atomic commit: update global state
	c.mu.Lock()
	for _, assignment := range tentativePlacements {
		if node, ok := c.nodes[assignment.NodeID]; ok {
			node.mu.Lock()
			node.AllocatedCPU += req.WorkerSpec.CPU
			node.AllocatedMemory += req.WorkerSpec.Memory
			node.AllocatedGPU += req.WorkerSpec.GPU
			node.mu.Unlock()
		}
	}
	c.mu.Unlock()
	
	decision.Timestamp = time.Now()
	
	// Generate acceptance evidence
	if c.signer != nil || c.recorder != nil {
		evidence, err := c.acceptEvidence(ctx, req, tentativePlacements)
		if err == nil && evidence != nil {
			decision.Evidence = evidence
		}
	}
	
	_ = capability.Report("elasticpool.allocate", decision.Decision, 
		capability.ModeProduction, 
		fmt.Sprintf("Accepted gang %s with %d assignments in %v", req.JobID, len(tentativePlacements), decision.Timestamp.Sub(startTime)))
	
	return decision, nil
}

// ListNodes returns all registered nodes (thread-safe copy).
func (c *CentralizedController) ListNodes() []*NodeDescriptor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make([]*NodeDescriptor, 0, len(c.nodes))
	for _, node := range c.nodes {
		node.mu.RLock()
		copy := *node
		node.mu.RUnlock()
		result = append(result, &copy)
	}
	
	return result
}

// ListClusters returns all registered cluster IDs.
func (c *CentralizedController) ListClusters() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make([]string, 0, len(c.clusters))
	for cid := range c.clusters {
		result = append(result, cid)
	}
	
	return result
}

// GetClusterCapacity returns total capacity across all nodes in a cluster.
func (c *CentralizedController) GetClusterCapacity(clusterID string) *NodeDescriptor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	total := &NodeDescriptor{
		ClusterID: clusterID,
		Labels:    make(map[string]string),
	}
	
	for _, node := range c.nodes {
		if node.ClusterID == clusterID {
			total.CPU += node.CPU - node.AllocatedCPU
			total.MemoryGB += node.MemoryGB - node.AllocatedMemory
			total.GPUs += node.GPUs - node.AllocatedGPU
			total.AllocatedCPU += node.AllocatedCPU
			total.AllocatedMemory += node.AllocatedMemory
			total.AllocatedGPU += node.AllocatedGPU
		}
	}
	
	return total
}

// Helper: reject evidence generation
func (c *CentralizedController) rejectEvidence(ctx context.Context, req *GangAllocationRequest, reason string) error {
	if c.recorder == nil {
		return nil
	}
	
	inputHash, _ := evidence.HashAny(req)
	outputHash, _ := evidence.HashAny(map[string]string{"decision": "rejected", "reason": reason})
	
	e := &evidence.Evidence{
		ID:        common.NewUUID(),
		Seq:       1,
		PrevHash:  evidence.GenesisPrevHash,
		Timestamp: time.Now().UTC(),
		Actor:     "centralized_controller",
		Action:    "gang.reject",
		Subject:   req.JobID,
		RunMode:   "elastic_pool_centralized",
		InputHash: inputHash,
		OutputHash: outputHash,
	}
	
	hash, err := e.ComputeHash()
	if err != nil {
		return err
	}
	e.Hash = hash
	
	if c.signer != nil {
		sig, err := c.signer.Sign([]byte(hash))
		if err == nil {
			e.Signature = sig
			e.KeyID = c.signer.KeyID()
		}
	}
	
	if ledger, ok := c.recorder.(*evidence.Ledger); ok {
		recorded, rerr := ledger.Record(ctx, evidence.RecordInput{
			Actor:   "centralized_controller",
			Action:  "gang.reject",
			Subject: req.JobID,
			Input:   req,
			Output:  map[string]string{"reason": reason},
			Payload: e,
		})
		if rerr == nil && recorded != nil {
			e = recorded
		}
	}
	
	return nil
}

// Helper: acceptance evidence generation
func (c *CentralizedController) acceptEvidence(ctx context.Context, req *GangAllocationRequest, assignments []Assignment) (*evidence.Evidence, error) {
	if c.recorder == nil {
		return nil, nil
	}
	
	inputHash, _ := evidence.HashAny(req)
	outputHash, _ := evidence.HashAny(map[string]int{"assignments": len(assignments)})
	
	e := &evidence.Evidence{
		ID:        common.NewUUID(),
		Seq:       1,
		PrevHash:  evidence.GenesisPrevHash,
		Timestamp: time.Now().UTC(),
		Actor:     "centralized_controller",
		Action:    "gang.accept",
		Subject:   req.JobID,
		RunMode:   "elastic_pool_centralized",
		InputHash: inputHash,
		OutputHash: outputHash,
	}
	
	hash, err := e.ComputeHash()
	if err != nil {
		return nil, err
	}
	e.Hash = hash
	
	if c.signer != nil {
		sig, err := c.signer.Sign([]byte(hash))
		if err == nil {
			e.Signature = sig
			e.KeyID = c.signer.KeyID()
		}
	}
	
	if ledger, ok := c.recorder.(*evidence.Ledger); ok {
		recorded, rerr := ledger.Record(ctx, evidence.RecordInput{
			Actor:   "centralized_controller",
			Action:  "gang.accept",
			Subject: req.JobID,
			Input:   req,
			Output:  map[string]int{"assignments": len(assignments)},
			Payload: e,
		})
		if rerr == nil && recorded != nil {
			e = recorded
		}
	}
	
	return e, nil
}
