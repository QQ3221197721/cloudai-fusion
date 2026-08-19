// Package edge - Module 21: Edge Node Manager.
//
// Provides an explicit lifecycle state machine for edge nodes
// (provisioned -> active -> offline -> retired) that is independent of the
// edge-cloud Manager in manager.go.
//
// Naming note: the package already declares EdgeNode (manager.go), which models
// a registered edge-cloud topology member with flat hardware fields. This module
// deliberately introduces ManagedNode instead of redeclaring EdgeNode, so the
// lifecycle control plane can carry a structured HardwareSpec and a strict state
// machine without changing the existing topology contract. ToEdgeNode bridges the
// two representations.
package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Lifecycle states
//
// These extend the existing NodeStatus type declared in delta_sync.go, which
// already provides StatusOnline / StatusOffline / StatusSyncing / StatusDegraded.
// ============================================================================

const (
	// StatusProvisioned means the node record exists and credentials were issued,
	// but the node has not yet reported a heartbeat.
	StatusProvisioned NodeStatus = "provisioned"
	// StatusActive means the node has reported a heartbeat and accepts work.
	StatusActive NodeStatus = "active"
	// StatusRetired is a terminal state; the node is decommissioned.
	StatusRetired NodeStatus = "retired"
)

// HardwareSpec describes the compute capability of an edge node.
type HardwareSpec struct {
	CPUCores         int     `json:"cpu_cores"`
	CPUModel         string  `json:"cpu_model,omitempty"`
	MemoryGB         float64 `json:"memory_gb"`
	GPUType          string  `json:"gpu_type,omitempty"`
	GPUCount         int     `json:"gpu_count"`
	GPUMemoryGB      float64 `json:"gpu_memory_gb"`
	StorageGB        float64 `json:"storage_gb"`
	NetworkSpeedMbps float64 `json:"network_speed_mbps"`
	PowerLimitWatts  int     `json:"power_limit_watts"`
}

// Metrics captures a point-in-time resource utilization sample for a node.
type Metrics struct {
	NodeID        string    `json:"node_id"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryPercent float64   `json:"memory_percent"`
	GPUPercent    float64   `json:"gpu_percent"`
	DiskPercent   float64   `json:"disk_percent"`
	PowerWatts    float64   `json:"power_watts"`
	TemperatureC  float64   `json:"temperature_celsius"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	CollectedAt   time.Time `json:"collected_at"`
	// Stale is true when the sample predates the staleness threshold, meaning the
	// values are the last known reading rather than a live observation.
	Stale bool `json:"stale"`
}

// Clone returns a defensive copy so callers cannot mutate manager-owned state.
func (m *Metrics) Clone() *Metrics {
	if m == nil {
		return nil
	}
	cp := *m
	return &cp
}

// ManagedNode is an edge node tracked by the lifecycle control plane.
type ManagedNode struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Status       NodeStatus        `json:"status"`
	Hardware     HardwareSpec      `json:"hardware"`
	Region       string            `json:"region"`
	Location     *GeoLocation      `json:"location,omitempty"`
	LastSeen     time.Time         `json:"last_seen"`
	RegisteredAt time.Time         `json:"registered_at"`
	ActivatedAt  *time.Time        `json:"activated_at,omitempty"`
	RetiredAt    *time.Time        `json:"retired_at,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`

	// OfflineCapable marks nodes able to run the offline-first decision engine.
	OfflineCapable bool `json:"offline_capable"`

	lastMetrics *Metrics
	// transitions is an append-only audit trail of lifecycle changes.
	transitions []NodeTransition
}

// NodeTransition records one lifecycle state change.
type NodeTransition struct {
	From      NodeStatus `json:"from"`
	To        NodeStatus `json:"to"`
	Reason    string     `json:"reason"`
	Timestamp time.Time  `json:"timestamp"`
}

// Clone returns a deep copy of the node.
func (n *ManagedNode) Clone() *ManagedNode {
	if n == nil {
		return nil
	}
	cp := *n
	if n.Location != nil {
		loc := *n.Location
		cp.Location = &loc
	}
	if n.Labels != nil {
		cp.Labels = make(map[string]string, len(n.Labels))
		for k, v := range n.Labels {
			cp.Labels[k] = v
		}
	}
	if n.ActivatedAt != nil {
		t := *n.ActivatedAt
		cp.ActivatedAt = &t
	}
	if n.RetiredAt != nil {
		t := *n.RetiredAt
		cp.RetiredAt = &t
	}
	cp.lastMetrics = n.lastMetrics.Clone()
	cp.transitions = append([]NodeTransition(nil), n.transitions...)
	return &cp
}

// Transitions returns a copy of the lifecycle audit trail.
func (n *ManagedNode) Transitions() []NodeTransition {
	return append([]NodeTransition(nil), n.transitions...)
}

// ToEdgeNode projects a ManagedNode onto the package's existing EdgeNode
// topology representation, so lifecycle-managed nodes can be handed to the
// edge-cloud Manager without duplicating hardware fields.
func (n *ManagedNode) ToEdgeNode() *EdgeNode {
	en := &EdgeNode{
		ID:                   n.ID,
		Name:                 n.Name,
		Region:               n.Region,
		Tier:                 TierEdge,
		CPUCores:             n.Hardware.CPUCores,
		MemoryGB:             n.Hardware.MemoryGB,
		GPUType:              n.Hardware.GPUType,
		GPUCount:             n.Hardware.GPUCount,
		GPUMemoryGB:          n.Hardware.GPUMemoryGB,
		StorageGB:            n.Hardware.StorageGB,
		NetworkBandwidthMbps: n.Hardware.NetworkSpeedMbps,
		PowerBudgetWatts:     n.Hardware.PowerLimitWatts,
		IsOfflineCapable:     n.OfflineCapable,
		LastHeartbeatAt:      n.LastSeen,
		RegisteredAt:         n.RegisteredAt,
		Labels:               n.Labels,
	}
	if n.Location != nil {
		// Note: GeoLocation has City/Country but no Region field.
		// If region info is needed, derive from Country + city heuristics or add a new field.
		// Current implementation leaves en.Region as-is from constructor.
	}
	switch n.Status {
	case StatusActive:
		en.Status = EdgeNodeOnline
	case StatusDegraded:
		en.Status = EdgeNodeDegraded
	case StatusProvisioned:
		en.Status = EdgeNodeMaintenance
	default:
		en.Status = EdgeNodeOffline
	}
	return en
}

// ============================================================================
// State machine
// ============================================================================

// allowedTransitions encodes the legal lifecycle edges. Retired is terminal.
var allowedTransitions = map[NodeStatus]map[NodeStatus]bool{
	StatusProvisioned: {StatusActive: true, StatusOffline: true, StatusRetired: true},
	StatusActive:      {StatusOffline: true, StatusDegraded: true, StatusRetired: true},
	StatusOffline:     {StatusActive: true, StatusDegraded: true, StatusRetired: true},
	StatusDegraded:    {StatusActive: true, StatusOffline: true, StatusRetired: true},
	StatusRetired:     {},
}

// CanTransition reports whether from -> to is a legal lifecycle edge.
func CanTransition(from, to NodeStatus) bool {
	if from == to {
		return true
	}
	next, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return next[to]
}

// ============================================================================
// NodeManager
// ============================================================================

// NodeManagerConfig tunes lifecycle behaviour.
type NodeManagerConfig struct {
	// OfflineAfter is the heartbeat age past which an active node is considered
	// offline by ReconcileLiveness.
	OfflineAfter time.Duration
	// MetricsStaleAfter is the sample age past which Monitor flags Stale.
	MetricsStaleAfter time.Duration
}

// DefaultNodeManagerConfig returns production-leaning defaults.
func DefaultNodeManagerConfig() NodeManagerConfig {
	return NodeManagerConfig{
		OfflineAfter:      90 * time.Second,
		MetricsStaleAfter: 60 * time.Second,
	}
}

// ProvisionHook is invoked during Provision to perform side effects such as
// issuing credentials or calling an infrastructure API.
type ProvisionHook func(ctx context.Context, nodeID string, spec HardwareSpec, region string) error

// RetireHook is invoked during Retire to release remote resources.
type RetireHook func(ctx context.Context, nodeID string) error

// NodeManager owns the lifecycle of edge nodes.
//
// All exported methods are safe for concurrent use. Accessors return deep copies
// so callers cannot mutate manager-owned state without going through the API.
type NodeManager struct {
	mu     sync.RWMutex
	nodes  map[string]*ManagedNode
	cfg    NodeManagerConfig
	logger *logrus.Logger

	provisionHook ProvisionHook
	retireHook    RetireHook

	// now is injectable so tests can control time deterministically.
	now func() time.Time
}

// NewNodeManager creates a NodeManager.
func NewNodeManager(cfg NodeManagerConfig, logger *logrus.Logger) *NodeManager {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if cfg.OfflineAfter <= 0 {
		cfg.OfflineAfter = DefaultNodeManagerConfig().OfflineAfter
	}
	if cfg.MetricsStaleAfter <= 0 {
		cfg.MetricsStaleAfter = DefaultNodeManagerConfig().MetricsStaleAfter
	}
	return &NodeManager{
		nodes:  make(map[string]*ManagedNode),
		cfg:    cfg,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the time source. Intended for deterministic tests.
func (m *NodeManager) SetClock(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now != nil {
		m.now = now
	}
}

// SetProvisionHook registers a side-effect hook invoked by Provision.
func (m *NodeManager) SetProvisionHook(h ProvisionHook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provisionHook = h
}

// SetRetireHook registers a side-effect hook invoked by Retire.
func (m *NodeManager) SetRetireHook(h RetireHook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retireHook = h
}

// Provision registers a new edge node in the provisioned state and returns its ID.
//
// The node ID is derived deterministically from (region, name) so that a
// re-provision attempt for the same logical node is detected as a duplicate
// rather than silently creating a second record.
func (m *NodeManager) Provision(ctx context.Context, name, region string, spec HardwareSpec) (string, error) {
	if name == "" {
		return "", fmt.Errorf("edge: node name is required")
	}
	if region == "" {
		return "", fmt.Errorf("edge: region is required")
	}
	if spec.CPUCores <= 0 {
		return "", fmt.Errorf("edge: hardware spec requires cpu_cores > 0")
	}
	if spec.MemoryGB <= 0 {
		return "", fmt.Errorf("edge: hardware spec requires memory_gb > 0")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	nodeID := NodeID(region, name)

	m.mu.Lock()
	if _, exists := m.nodes[nodeID]; exists {
		m.mu.Unlock()
		return "", fmt.Errorf("edge: node %s already provisioned", nodeID)
	}
	now := m.now()
	node := &ManagedNode{
		ID:             nodeID,
		Name:           name,
		Status:         StatusProvisioned,
		Hardware:       spec,
		Region:         region,
		LastSeen:       now,
		RegisteredAt:   now,
		OfflineCapable: true,
		Labels:         make(map[string]string),
		transitions: []NodeTransition{{
			From: "", To: StatusProvisioned, Reason: "provision", Timestamp: now,
		}},
	}
	m.nodes[nodeID] = node
	hook := m.provisionHook
	m.mu.Unlock()

	// The hook runs outside the lock so a slow infrastructure call cannot block
	// heartbeat processing for other nodes.
	if hook != nil {
		if err := hook(ctx, nodeID, spec, region); err != nil {
			m.mu.Lock()
			delete(m.nodes, nodeID)
			m.mu.Unlock()
			return "", fmt.Errorf("edge: provision hook failed for %s: %w", nodeID, err)
		}
	}

	m.logger.WithFields(logrus.Fields{
		"node_id": nodeID,
		"region":  region,
		"cores":   spec.CPUCores,
		"gpu":     spec.GPUType,
	}).Info("edge node provisioned")

	return nodeID, nil
}

// Monitor returns the latest metrics sample for a node.
//
// Monitor never fabricates a reading: if the node has never reported metrics it
// returns an error, and if the last sample is older than MetricsStaleAfter the
// returned sample is flagged Stale so callers do not mistake it for live data.
func (m *NodeManager) Monitor(ctx context.Context, nodeID string) (*Metrics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	node, ok := m.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("edge: node %s not found", nodeID)
	}
	if node.lastMetrics == nil {
		return nil, fmt.Errorf("edge: node %s has not reported metrics", nodeID)
	}

	sample := node.lastMetrics.Clone()
	now := m.now()
	sample.Stale = now.Sub(sample.CollectedAt) > m.cfg.MetricsStaleAfter
	if node.ActivatedAt != nil {
		sample.UptimeSeconds = int64(sample.CollectedAt.Sub(*node.ActivatedAt).Seconds())
		if sample.UptimeSeconds < 0 {
			sample.UptimeSeconds = 0
		}
	}
	return sample, nil
}

// Retire moves a node to the terminal retired state and releases its resources.
func (m *NodeManager) Retire(ctx context.Context, nodeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	node, ok := m.nodes[nodeID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("edge: node %s not found", nodeID)
	}
	if node.Status == StatusRetired {
		m.mu.Unlock()
		return fmt.Errorf("edge: node %s is already retired", nodeID)
	}
	hook := m.retireHook
	m.mu.Unlock()

	// Release remote resources before flipping to the terminal state, so a hook
	// failure leaves the node in its previous state and the caller can retry.
	if hook != nil {
		if err := hook(ctx, nodeID); err != nil {
			return fmt.Errorf("edge: retire hook failed for %s: %w", nodeID, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	node, ok = m.nodes[nodeID]
	if !ok {
		return fmt.Errorf("edge: node %s not found", nodeID)
	}
	now := m.now()
	m.transitionLocked(node, StatusRetired, "retire", now)
	node.RetiredAt = &now

	m.logger.WithField("node_id", nodeID).Info("edge node retired")
	return nil
}

// Heartbeat records liveness for a node and promotes it to active.
// Metrics may be nil when the agent reports liveness without a resource sample.
func (m *NodeManager) Heartbeat(ctx context.Context, nodeID string, metrics *Metrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	node, ok := m.nodes[nodeID]
	if !ok {
		return fmt.Errorf("edge: node %s not found", nodeID)
	}
	if node.Status == StatusRetired {
		return fmt.Errorf("edge: node %s is retired and cannot heartbeat", nodeID)
	}

	now := m.now()
	node.LastSeen = now

	if metrics != nil {
		sample := metrics.Clone()
		sample.NodeID = nodeID
		if sample.CollectedAt.IsZero() {
			sample.CollectedAt = now
		}
		sample.Stale = false
		node.lastMetrics = sample
	}

	target := StatusActive
	// A node reporting power above its envelope is degraded, not healthy.
	if metrics != nil && node.Hardware.PowerLimitWatts > 0 &&
		metrics.PowerWatts > float64(node.Hardware.PowerLimitWatts) {
		target = StatusDegraded
	}

	if node.Status != target {
		m.transitionLocked(node, target, "heartbeat", now)
	}
	if node.ActivatedAt == nil && (target == StatusActive || target == StatusDegraded) {
		activated := now
		node.ActivatedAt = &activated
	}
	return nil
}

// ReconcileLiveness marks nodes offline when their heartbeat has aged past
// OfflineAfter. It returns the IDs that transitioned, sorted for determinism.
func (m *NodeManager) ReconcileLiveness(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	var flipped []string
	for id, node := range m.nodes {
		if node.Status != StatusActive && node.Status != StatusDegraded {
			continue
		}
		if now.Sub(node.LastSeen) <= m.cfg.OfflineAfter {
			continue
		}
		m.transitionLocked(node, StatusOffline, "heartbeat timeout", now)
		flipped = append(flipped, id)
	}
	sort.Strings(flipped)
	return flipped, nil
}

// GetNode returns a deep copy of a node record.
func (m *NodeManager) GetNode(nodeID string) (*ManagedNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	node, ok := m.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("edge: node %s not found", nodeID)
	}
	return node.Clone(), nil
}

// ListNodes returns deep copies of nodes, optionally filtered by status,
// sorted by ID for deterministic output.
func (m *NodeManager) ListNodes(status *NodeStatus) []*ManagedNode {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var nodes []*ManagedNode
	m.mu.RLock()
	for _, node := range m.nodes {
		if status != nil && node.Status != *status {
			continue
		}
		nodes = append(nodes, node.Clone())
	}
	m.mu.RUnlock()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

// Stats summarizes the fleet by lifecycle state.
func (m *NodeManager) Stats() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]int{"total": len(m.nodes)}
	for _, node := range m.nodes {
		stats[string(node.Status)]++
	}
	return stats
}

// transitionLocked applies a state change and appends to the audit trail.
// Caller must hold m.mu for writing. Illegal edges are rejected and logged
// rather than silently applied, so the audit trail stays trustworthy.
func (m *NodeManager) transitionLocked(node *ManagedNode, to NodeStatus, reason string, at time.Time) {
	from := node.Status
	if from == to {
		return
	}
	if !CanTransition(from, to) {
		m.logger.WithFields(logrus.Fields{
			"node_id": node.ID,
			"from":    from,
			"to":      to,
		}).Warn("edge: rejected illegal node lifecycle transition")
		return
	}
	node.Status = to
	node.transitions = append(node.transitions, NodeTransition{
		From: from, To: to, Reason: reason, Timestamp: at,
	})
}

// NodeID derives a stable node identifier from region and name.
func NodeID(region, name string) string {
	sum := sha256.Sum256([]byte(region + "\x00" + name))
	return "node-" + hex.EncodeToString(sum[:8])
}
