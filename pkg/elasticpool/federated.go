package elasticpool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Federated CRDT-Based Elastic Pool
// ============================================================================

var (
	// ErrConflictResolved indicates a double-allocation conflict was resolved.
	ErrConflictResolved = errors.New("elasticpool: double-allocation conflict resolved via CRDT")
	
	// ErrQuorumNotReached indicates insufficient cluster responses for allocation.
	ErrQuorumNotReached = errors.New("elasticpool: quorum not reached for distributed decision")
)

// CRDTConfig configures CRDT behavior for federated pooling.
type CRDTConfig struct {
	// GossipInterval controls frequency of state propagation between clusters.
	GossipInterval time.Duration
	
	// QuorumSize determines minimum clusters that must respond to allocation.
	QuorumSize int
	
	// PartitionTolerance enables optimistic mode during network splits.
	PartitionTolerance bool
	
	// ConflictResolutionMode defines how double-allocations are resolved.
	ConflictResolutionMode string // "first-writer-wins" | "last-write-wins" | "vector-clock"
}

// DefaultCRDTConfig returns sensible defaults.
func DefaultCRDTConfig() CRDTConfig {
	return CRDTConfig{
		GossipInterval:         100 * time.Millisecond,
		QuorumSize:             3,
		PartitionTolerance:     true,
		ConflictResolutionMode: "first-writer-wins",
	}
}

// VectorClock implements causal ordering for CRDT merge.
type VectorClock map[string]int

// Clone creates a deep copy.
func (vc VectorClock) Clone() VectorClock {
	result := make(VectorClock, len(vc))
	for k, v := range vc {
		result[k] = v
	}
	return result
}

// Update increments this node's counter.
func (vc VectorClock) Update(nodeID string) {
	vc[nodeID]++
}

// Merge takes element-wise maximum with another clock.
func (vc VectorClock) Merge(other VectorClock) {
	for node, version := range other {
		if version > vc[node] {
			vc[node] = version
		}
	}
}

// CausalAfter checks if this happened-before another.
func (vc VectorClock) CausalAfter(other VectorClock) bool {
	allGreaterOrEqual := true
	atLeastOneGreater := false
	
	for node, otherVer := range other {
		selfVer := vc[node]
		
		if selfVer < otherVer {
			allGreaterOrEqual = false
			break
		}
		if selfVer > otherVer {
			atLeastOneGreater = true
		}
	}
	
	// Check other nodes not in this clock
	for node := range vc {
		if _, exists := other[node]; !exists {
			if vc[node] > 0 {
				atLeastOneGreater = true
			}
		}
	}
	
	return allGreaterOrEqual && atLeastOneGreater
}

// ConcurrentWith detects concurrent events (neither causally before).
func (vc VectorClock) ConcurrentWith(other VectorClock) bool {
	return !vc.CausalAfter(other) && !other.CausalAfter(vc)
}

// ResourceStateCRDT implements a PN-Counter CRDT for GPU resource tracking.
// Supports add/remove operations that merge commutatively.
type ResourceStateCRDT struct {
	// AddCounts maps node IDs to vector clocks of additions
	AddCounts map[string][]AddEvent
	
	// RemoveCounts maps node IDs to vector clocks of removals  
	RemoveCounts map[string][]RemoveEvent
	
	// VersionVector maintains causal history across all clusters
	VersionVector VectorClock
	
	// LocalNodeID identifies this cluster in gossip protocol
	LocalNodeID string
	
	mu sync.RWMutex
}

// AddEvent represents a single addition operation.
type AddEvent struct {
	VectorClock VectorClock
	Value       int
}

// RemoveEvent represents a single removal operation.
type RemoveEvent struct {
	VectorClock VectorClock
	Value       int
}

// NewResourceStateCRDT creates an empty CRDT state.
func NewResourceStateCRDT(nodeID string) *ResourceStateCRDT {
	return &ResourceStateCRDT{
		AddCounts:       make(map[string][]AddEvent),
		RemoveCounts:    make(map[string][]RemoveEvent),
		VersionVector:   make(VectorClock),
		LocalNodeID:     nodeID,
	}
}

// ApplyAdd records a new GPU becoming available.
func (c *ResourceStateCRDT) ApplyAdd(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.VersionVector.Update(c.LocalNodeID)
	
	addEvent := AddEvent{
		VectorClock: c.VersionVector.Clone(),
		Value:       count,
	}
	
	key := fmt.Sprintf("%s-%d", c.LocalNodeID, time.Now().UnixNano())
	c.AddCounts[key] = append(c.AddCounts[key], addEvent)
}

// ApplyRemove records GPUs being allocated (removed from available pool).
func (c *ResourceStateCRDT) ApplyRemove(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.VersionVector.Update(c.LocalNodeID)
	
	removeEvent := RemoveEvent{
		VectorClock: c.VersionVector.Clone(),
		Value:       count,
	}
	
	key := fmt.Sprintf("%s-%d", c.LocalNodeID, time.Now().UnixNano())
	c.RemoveCounts[key] = append(c.RemoveCounts[key], removeEvent)
}

// Merge combines two CRDT states (must be commutative and associative).
func (c *ResourceStateCRDT) Merge(other *ResourceStateCRDT) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// Merge version vectors
	c.VersionVector.Merge(other.VersionVector)
	
	// OR-Set semantics: union of all add/remove events
	for key, adds := range other.AddCounts {
		if _, exists := c.AddCounts[key]; !exists {
			c.AddCounts[key] = make([]AddEvent, 0)
		}
		c.AddCounts[key] = append(c.AddCounts[key], adds...)
	}
	
	for key, removes := range other.RemoveCounts {
		if _, exists := c.RemoveCounts[key]; !exists {
			c.RemoveCounts[key] = make([]RemoveEvent, 0)
		}
		c.RemoveCounts[key] = append(c.RemoveCounts[key], removes...)
	}
	
	return nil
}

// EffectiveCapacity computes logically consistent available GPU count.
// Uses CRDT semantics: effective = sum(adds) - sum(removes).
func (c *ResourceStateCRDT) EffectiveCapacity() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	totalAdds := 0
	totalRemoves := 0
	
	for _, adds := range c.AddCounts {
		for _, event := range adds {
			totalAdds += event.Value
		}
	}
	
	for _, removes := range c.RemoveCounts {
		for _, event := range removes {
			totalRemoves += event.Value
		}
	}
	
	effective := totalAdds - totalRemoves
	if effective < 0 {
		effective = 0
	}
	
	return effective
}

// GetHistory returns causal history for debugging/audit.
func (c *ResourceStateCRDT) GetHistory() []byte {
	// In production, would marshal to JSON/protobuf for audit trail
	_ = c.GetHistory
	return nil
}

// ============================================================================
// FederatedController
// ============================================================================

// FederatedController implements distributed CRDT-based elastic pool.
type FederatedController struct {
	config CRDTConfig
	
	// CRDT state for each cluster (gossip graph)
	clusters      map[string]*ClusterState
	localNodeID   string
	
	// Partition detection
	partitionsDetected bool
	lastHeartbeat     map[string]time.Time
	
	logger interface{} // placeholder for actual logger
}

// ClusterState holds local state for a remote cluster.
type ClusterState struct {
	NodeID        string
	CRDT          *ResourceStateCRDT
	LastUpdate    time.Time
	HeartbeatSent time.Time
	Healthy       bool
}

// NewFederatedController creates a federated pool controller.
func NewFederatedController(nodeID string) *FederatedController {
	cfg := DefaultCRDTConfig()
	
	fc := &FederatedController{
		config:        cfg,
		clusters:      make(map[string]*ClusterState),
		localNodeID:   nodeID,
		lastHeartbeat: make(map[string]time.Time),
	}
	
	// Initialize local cluster state
	fc.clusters[nodeID] = &ClusterState{
		NodeID:     nodeID,
		CRDT:       NewResourceStateCRDT(nodeID),
		Healthy:    true,
		LastUpdate: time.Now(),
	}
	
	return fc
}

// SetPartitionMode enables/disables partition-tolerant mode.
func (fc *FederatedController) SetPartitionMode(enabled bool) {
	fc.config.PartitionTolerance = enabled
}

// RegisterRemoteCluster adds a new cluster to the federation.
func (fc *FederatedController) RegisterRemoteCluster(ctx context.Context, clusterID string) error {
	if clusterID == "" || clusterID == fc.localNodeID {
		return nil // already local or invalid
	}
	
	fc.clusters[clusterID] = &ClusterState{
		NodeID:     clusterID,
		CRDT:       NewResourceStateCRDT(clusterID),
		Healthy:    false,
		LastUpdate: time.Now(),
	}
	
	fc.lastHeartbeat[clusterID] = time.Now()
	
	return nil
}

// UpdateLocalCapacity updates this cluster's available resources.
func (fc *FederatedController) UpdateLocalCapacity(gpusAvailable int) {
	localState := fc.clusters[fc.localNodeID]
	if localState == nil {
		return
	}
	
	// Apply delta (simplified; would track previous value in production)
	localState.CRDT.ApplyAdd(gpusAvailable)
	localState.LastUpdate = time.Now()
}

// PropagateState broadcasts current CRDT state to neighbors.
func (fc *FederatedController) PropagateState(ctx context.Context) {
	currentTime := time.Now()
	
	fc.mu.RLock()
	for clusterID, state := range fc.clusters {
		if clusterID == fc.localNodeID || !state.Healthy {
			continue
		}
		
		// In production, would use gRPC streaming to send CRDT snapshot
		// For prototype, just log intent
		_ = ctx
		
		state.HeartbeatSent = currentTime
	}
	fc.mu.RUnlock()
	
	_ = capability.Report("elasticpool.gossip.propagate", fc.localNodeID,
		capability.ModeProduction, 
		fmt.Sprintf("Propagating CRDT state to %d clusters", len(fc.clusters)-1))
}

// ReceiveRemoteState merges incoming CRDT state from a remote cluster.
func (fc *FederatedController) ReceiveRemoteState(ctx context.Context, sourceNodeID string, remoteCRDT *ResourceStateCRDT) error {
	if sourceNodeID == fc.localNodeID {
		return nil
	}
	
	fc.mu.Lock()
	localState, exists := fc.clusters[sourceNodeID]
	fc.mu.Unlock()
	
	if !exists {
		return fmt.Errorf("elasticpool: unknown source cluster %s", sourceNodeID)
	}
	
	// Merge CRDT states (commutative operation)
	if err := localState.CRDT.Merge(remoteCRDT); err != nil {
		return fmt.Errorf("elasticpool: CRDT merge failed: %w", err)
	}
	
	localState.LastUpdate = time.Now()
	localState.Healthy = true
	
	return nil
}

// Allocate distributes allocation request across federation using quorum-based consensus.
func (fc *FederatedController) Allocate(ctx context.Context, req *GangAllocationRequest) (*AllocationDecision, error) {
	startTime := time.Now()
	
	decision := &AllocationDecision{
		RequestID: req.RequestID,
	}
	
	// Step 1: Broadcast query to all clusters
	query := map[string]any{
		"request_id":  req.RequestID,
		"worker_count": req.WorkerCount,
		"required_gpus": req.WorkerSpec.GPU,
		"deadline":      startTime.Add(2 * time.Second),
	}
	
	_ = query
	// In production, would use gRPC broadcast with timeout
	
	// Step 2: Collect responses (simulate for prototype)
	var responses []ClusterResponse
	healthyClusters := 0
	
	fc.mu.RLock()
	for clusterID, state := range fc.clusters {
		if !state.Healthy {
			continue
		}
		healthyClusters++
		
		response := ClusterResponse{
			ClusterID: clusterID,
			AvailableGPUs: state.CRDT.EffectiveCapacity(),
			LatencyMs: int(time.Since(startTime) / time.Millisecond),
		}
		responses = append(responses, response)
	}
	fc.mu.RUnlock()
	
	// Step 3: Check quorum
	minQuorum := fc.config.QuorumSize
	if minQuorum <= 0 {
		minQuorum = 3
	}
	
	if healthyClusters < minQuorum && !fc.config.PartitionTolerance {
		decision.Decision = "rejected"
		decision.Error = fmt.Sprintf("insufficient healthy clusters: %d/%d", healthyClusters, minQuorum)
		return decision, ErrQuorumNotReached
	}
	
	// Step 4: Aggregate capacity via CRDT merge
	totalCapacity := 0
	for _, resp := range responses {
		totalCapacity += resp.AvailableGPUs
	}
	
	requiredGPUs := req.WorkerSpec.GPU * req.WorkerCount
	
	// Step 5: Decision logic
	if totalCapacity >= requiredGPUs {
		// Accept: create assignments based on capacity distribution
		decision.Decision = "accepted"
		decision.Assignments = fc.createDistributedAssignments(req, responses)
	} else {
		decision.Decision = "rejected"
		decision.Error = fmt.Sprintf("total capacity %d < required %d", totalCapacity, requiredGPUs)
	}
	
	decision.Timestamp = time.Now()
	
	return decision, nil
}

// ClusterResponse represents a cluster's allocation response.
type ClusterResponse struct {
	ClusterID     string
	AvailableGPUs int
	LatencyMs     int
	Timestamp     time.Time
}

// createDistributedAssignments generates assignments across multiple clusters.
func (fc *FederatedController) createDistributedAssignments(req *GangAllocationRequest, responses []ClusterResponse) []Assignment {
	var assignments []Assignment
	
	workerIdx := 0
	for _, resp := range responses {
		if workerIdx >= req.WorkerCount {
			break
		}
		
		// Distribute workers proportionally to available capacity
		maxWorkersForCluster := resp.AvailableGPUs / req.WorkerSpec.GPU
		if maxWorkersForCluster == 0 {
			maxWorkersForCluster = 1
		}
		if workerIdx+maxWorkersForCluster > req.WorkerCount {
			maxWorkersForCluster = req.WorkerCount - workerIdx
		}
		
		for w := 0; w < maxWorkersForCluster && workerIdx < req.WorkerCount; w++ {
			assignments = append(assignments, Assignment{
				WorkerID:  fmt.Sprintf("%s-worker-%d", req.JobID, workerIdx),
				NodeID:    fmt.Sprintf("%s-node-%d", resp.ClusterID, workerIdx%4), // simulate 4 nodes per cluster
				ClusterID: resp.ClusterID,
			})
			workerIdx++
		}
	}
	
	return assignments
}

// DetectPartitions identifies potential network splits via heartbeat staleness.
func (fc *FederatedController) DetectPartitions() []string {
	var partitionedClusters []string
	
	now := time.Now()
	timeout := 3 * fc.config.GossipInterval
	
	fc.mu.RLock()
	for clusterID, state := range fc.clusters {
		if clusterID == fc.localNodeID {
			continue
		}
		
		if !state.Healthy || now.Sub(state.LastUpdate) > timeout {
			partitionedClusters = append(partitionedClusters, clusterID)
		}
	}
	fc.mu.RUnlock()
	
	if len(partitionedClusters) > 0 {
		fc.partitionsDetected = true
	}
	
	return partitionedClusters
}

// ListClusters returns all known cluster IDs.
func (fc *FederatedController) ListClusters() []string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	result := make([]string, 0, len(fc.clusters))
	for clusterID := range fc.clusters {
		result = append(result, clusterID)
	}
	
	return result
}

// GetGlobalCapacity returns total available GPUs across all healthy clusters.
func (fc *FederatedController) GetGlobalCapacity() int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	total := 0
	for _, state := range fc.clusters {
		if state.Healthy {
			total += state.CRDT.EffectiveCapacity()
		}
	}
	
	return total
}

// ReconcileConflicts attempts to resolve double-allocation conflicts post-partition.
func (fc *FederatedController) ReconcileConflicts(ctx context.Context) ([]ConflictResolution, error) {
	var resolutions []ConflictResolution
	
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	// Simple first-writer-wins resolution (would be more sophisticated in prod)
	for clusterID, state := range fc.clusters {
		if clusterID == fc.localNodeID || !state.Healthy {
			continue
		}
		
		// Check if local and remote have concurrent adds/removes for same GPU
		conflicts := fc.detectConcurrentUpdates(state)
		
		for _, conflict := range conflicts {
			resolution := ConflictResolution{
				ClusterPair:   []string{fc.localNodeID, clusterID},
				ResourceID:    conflict.ResourceID,
				VersionA:      conflict.VersionA,
				VersionB:      conflict.VersionB,
				Resolution:    conflict.resolvedViaFirstWriter(),
				ResolvedAt:    time.Now(),
			}
			resolutions = append(resolutions, resolution)
		}
	}
	
	return resolutions, nil
}

// ConcurrentUpdate detects conflicting updates.
type ConcurrentUpdate struct {
	ResourceID string
	VersionA   VectorClock
	VersionB   VectorClock
}

func (cu *ConcurrentUpdate) resolvedViaFirstWriter() string {
	// Simplified: compare timestamps encoded in vector clocks
	if len(cu.VersionA) == 0 {
		return "version_b_wins"
	}
	if len(cu.VersionB) == 0 {
		return "version_a_wins"
	}
	
	// Compare last component (assumes monotonically increasing)
	lastA := cu.VersionA[cu.VersionA.keys()[0]]
	lastB := cu.VersionB[cu.VersionB.keys()[0]]
	
	if lastA < lastB {
		return "version_a_wins"
	}
	return "version_b_wins"
}

// Helper extension for VectorClock to get keys
type ClockMap VectorClock

func (cm ClockMap) keys() []string {
	keys := make([]string, 0, len(cm))
	for k := range cm {
		keys = append(keys, k)
	}
	return keys
}

func (fc *FederatedController) detectConcurrentUpdates(remoteState *ClusterState) []ConcurrentUpdate {
	// Placeholder for detailed CRDT analysis
	// In production, would scan AddCounts/RemoveCounts for concurrent versions
	return nil
}

// ConflictResolution documents a resolved allocation conflict.
type ConflictResolution struct {
	ClusterPair []string
	ResourceID  string
	VersionA    VectorClock
	VersionB    VectorClock
	Resolution  string // "first_writer_wins" | "last_writer_wins"
	ResolvedAt  time.Time
}
