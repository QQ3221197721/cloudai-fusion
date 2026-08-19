// Package elasticpool implements Module 12 — the Elastic Inference Pool: a
// GPU-slot resource pool that tracks node membership, leases slots to Module 15
// inference services with best-fit placement (smallest satisfying free space, to
// minimize fragmentation), and evaluates elasticity under a hard budget guard
// whose math mirrors pkg/scaler exactly (currentCost + costImpact > budgetLimit
// → BUDGET REJECTED). Every write operation is accompanied by a signed,
// hash-chained attestation through the evidence ledger, so the entire capacity
// history — every node joined, every slot leased and released, every scale
// decision — is tamper-evident and offline-verifiable.
//
// Lock-in thesis: after months of leases, the pool holds a verified provenance
// record of "which service held which GPU slots, when, at what cost ceiling" —
// the placement history and budget discipline auditors trust. Migrating means
// abandoning the attested capacity ledger.
//
// Storage layout (content-addressed, file-system based):
//
//	<root>/elasticpool/pools.json                  pool list (JSON object keyed by poolID)
//	<root>/elasticpool/<poolID>/nodes.json         node members (JSON object keyed by nodeID)
//	<root>/elasticpool/<poolID>/leases.jsonl       append-only leases (last-write-wins per lease ID)
//	<root>/elasticpool/<poolID>/decisions.jsonl    append-only elasticity decisions
//
// Every CreatePool/AddNode/Acquire/Release/EvaluateElasticity writes a real
// attestation through pkg/evidence.Ledger; pass nil ledger to skip attestation.
package elasticpool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// PoolStatus defines the lifecycle states of an elastic pool.
type PoolStatus string

const (
	// PoolActive indicates the pool accepts nodes and leases.
	PoolActive PoolStatus = "active"
	// PoolDraining indicates the pool is winding down (releases still allowed).
	PoolDraining PoolStatus = "draining"
	// PoolDeleted marks a removed pool (terminal).
	PoolDeleted PoolStatus = "deleted"
)

// NodeStatus defines the lifecycle states of a pool node.
type NodeStatus string

const (
	// NodeReady means the node has free slots and accepts leases.
	NodeReady NodeStatus = "ready"
	// NodeBusy means every slot on the node is leased.
	NodeBusy NodeStatus = "busy"
	// NodeDrained means the node hosts no leases: resources are being emptied
	// and it temporarily does NOT accept new leases (Acquire skips it because
	// status != ready). A drained node is eligible for pool removal or
	// re-activation via a future node replace; it only re-enters rotation as a
	// freshly joined node.
	NodeDrained NodeStatus = "drained"
)

// DefaultActor is the attestation actor for all pool write operations.
const DefaultActor = "cafctl-pool"

// Sentinel errors callers can test with errors.Is.
var (
	// ErrNotFound is returned when a pool, node, or lease is absent.
	ErrNotFound = errors.New("elasticpool: not found")
	// ErrAlreadyReleased is returned when releasing an already-released lease.
	ErrAlreadyReleased = errors.New("elasticpool: lease already released")
	// ErrInvalidID is returned when a pool/node/lease ID fails validation.
	ErrInvalidID = errors.New("elasticpool: invalid ID")
	// ErrNoCapacity is returned when no ready node can fit an acquisition.
	ErrNoCapacity = errors.New("elasticpool: no capacity")
	// ErrPoolFull is returned when adding a node past MaxNodes.
	ErrPoolFull = errors.New("elasticpool: pool at max_nodes")
)

// Pool represents one elastic GPU pool.
type Pool struct {
	ID              string     `json:"id"`                  // "pool-<hex16>"
	Name            string     `json:"name"`                // human-readable name
	GPUType         string     `json:"gpu_type"`            // e.g. "A100-80G"
	SlotsPerNode    int        `json:"slots_per_node"`      // GPU slots each node contributes (>0)
	MinNodes        int        `json:"min_nodes"`           // floor constraint (>=0)
	MaxNodes        int        `json:"max_nodes"`           // ceiling constraint (> MinNodes)
	CostPerNodeHour float64    `json:"cost_per_node_hour"`  // USD per node-hour (>0)
	Status          PoolStatus `json:"status"`              // "active"|"draining"|"deleted"
	CreatedAt       time.Time  `json:"created_at"`
}

// Node is one member node of a pool, contributing SlotsPerNode slots.
type Node struct {
	ID         string     `json:"id"`          // "node-<hex12>"
	PoolID     string     `json:"pool_id"`
	TotalSlots int        `json:"total_slots"` // == pool.SlotsPerNode at join time
	UsedSlots  int        `json:"used_slots"`
	Status     NodeStatus `json:"status"` // "ready"|"busy"|"drained"
	JoinedAt   time.Time  `json:"joined_at"`
}

// SlotLease reserves slots on one node for one inference service. ServiceID is
// an opaque Module 15 reference validated only by the "inf-" prefix — this
// package deliberately does not depend on pkg/inference.
type SlotLease struct {
	ID         string     `json:"id"`          // "lease-<hex12>"
	PoolID     string     `json:"pool_id"`
	NodeID     string     `json:"node_id"`
	ServiceID  string     `json:"service_id"`  // "inf-..." (Module 15 mesh service)
	Slots      int        `json:"slots"`
	AcquiredAt time.Time  `json:"acquired_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"` // nil = held
}

// ElasticDecision is one budget-guarded elasticity recommendation. Its shape
// mirrors scaler.ScaleDecision (ID/Action/Reason/CurrentNodes/TargetNodes/
// CostImpactPerHour/BudgetOK/CreatedAt) so the two decision streams compose.
type ElasticDecision struct {
	ID                string    `json:"id"`                  // "el-<hex16>"
	Action            string    `json:"action"`              // "scale_up" | "scale_down" | "no_change"
	Reason            string    `json:"reason"`              // detailed rationale with metrics
	CurrentNodes      int       `json:"current_nodes"`
	TargetNodes       int       `json:"target_nodes"`
	CostImpactPerHour float64   `json:"cost_impact_per_hour"` // USD change per hour (negative on scale_down)
	BudgetOK          bool      `json:"budget_ok"`
	CreatedAt         time.Time `json:"created_at"`
}

// PoolInput describes a new elastic pool (consumed by CreatePool).
type PoolInput struct {
	Name            string
	GPUType         string // e.g. "A100-80G"
	SlotsPerNode    int
	MinNodes        int
	MaxNodes        int
	CostPerNodeHour float64
	Actor           string // defaults to "cafctl-pool"; also the attestation actor
}

// FSMElasticPool is the file-system backed elastic pool manager. It stores
// pools in <dir>/elasticpool/, per-pool nodes in <poolID>/nodes.json, and
// append-only leases/decisions in JSONL. A nil ledger disables attestation
// (all other behavior unchanged).
type FSMElasticPool struct {
	root   string
	ledger *evidence.Ledger

	mu   sync.Mutex
	last *evidence.Evidence // most recent receipt, for CLI display
}

// NewFSMElasticPool opens (and creates, if needed) a pool store rooted at dir.
// All state lives under <dir>/elasticpool/.
func NewFSMElasticPool(dir string, ledger *evidence.Ledger) (*FSMElasticPool, error) {
	if dir == "" {
		return nil, errors.New("elasticpool: root path is required")
	}
	poolDir := filepath.Join(dir, "elasticpool")
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		return nil, fmt.Errorf("elasticpool: create root: %w", err)
	}
	return &FSMElasticPool{root: poolDir, ledger: ledger}, nil
}

// Root returns the elastic pool root directory (read-only accessor).
func (f *FSMElasticPool) Root() string { return f.root }

// LastAttestation returns the receipt from the most recent operation, or nil
// when none was written (nil ledger or no operations yet). This is a genuine,
// signed ledger receipt.
func (f *FSMElasticPool) LastAttestation() *evidence.Evidence {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// ----------------------------------------------------------------------------
// Pool lifecycle
// ----------------------------------------------------------------------------

// CreatePool validates and registers a new pool. Constraints: GPUType
// non-empty, SlotsPerNode > 0, MaxNodes > MinNodes (MinNodes >= 0),
// CostPerNodeHour > 0. Writes attestation action "elasticpool.create".
func (f *FSMElasticPool) CreatePool(ctx context.Context, in PoolInput) (*Pool, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("elasticpool: pool name is required")
	}
	if strings.TrimSpace(in.GPUType) == "" {
		return nil, errors.New("elasticpool: gpu type is required")
	}
	if in.SlotsPerNode <= 0 {
		return nil, fmt.Errorf("elasticpool: slots_per_node (%d) must be positive", in.SlotsPerNode)
	}
	if in.MinNodes < 0 {
		return nil, fmt.Errorf("elasticpool: min_nodes (%d) cannot be negative", in.MinNodes)
	}
	if in.MaxNodes <= in.MinNodes {
		return nil, fmt.Errorf("elasticpool: max_nodes (%d) must be > min_nodes (%d)", in.MaxNodes, in.MinNodes)
	}
	// NaN would silently bypass the <=0 check above (NaN <= 0 is false), so
	// finiteness is enforced explicitly using the same pattern as mesh.go:!
	// IsNaN && !IsInf. Order matters: finite check BEFORE positivity check,
	// because -Inf triggers <=0 but we still want the 'must be finite' message.
	if math.IsNaN(in.CostPerNodeHour) || math.IsInf(in.CostPerNodeHour, 0) {
		return nil, fmt.Errorf("elasticpool: cost must be finite (cost_per_node_hour=%v rejected; NaN/+Inf/-Inf are not valid pricing)", in.CostPerNodeHour)
	}
	if in.CostPerNodeHour <= 0 {
		return nil, fmt.Errorf("elasticpool: cost_per_node_hour (%.2f) must be positive", in.CostPerNodeHour)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	poolID, err := genID("pool-", 16)
	if err != nil {
		return nil, err
	}

	p := &Pool{
		ID:              poolID,
		Name:            in.Name,
		GPUType:         in.GPUType,
		SlotsPerNode:    in.SlotsPerNode,
		MinNodes:        in.MinNodes,
		MaxNodes:        in.MaxNodes,
		CostPerNodeHour: in.CostPerNodeHour,
		Status:          PoolActive,
		CreatedAt:       time.Now().UTC(),
	}

	if err := f.persistPoolLocked(p); err != nil {
		return nil, err
	}

	actor := in.Actor
	if actor == "" {
		actor = DefaultActor
	}
	if err := f.attestLocked(ctx, "elasticpool.create", poolID, actor,
		map[string]any{"name": in.Name, "gpu_type": in.GPUType, "slots_per_node": in.SlotsPerNode,
			"min_nodes": in.MinNodes, "max_nodes": in.MaxNodes, "cost_per_node_hour": in.CostPerNodeHour},
		map[string]any{"pool_id": poolID, "status": string(p.Status)},
		map[string]any{}); err != nil {
		return nil, err
	}
	return p, nil
}

// GetPool retrieves one pool by ID.
func (f *FSMElasticPool) GetPool(id string) (*Pool, error) {
	if err := validateID(id, "pool ID"); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, id, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	p, err := f.getPoolLocked(id)
	if err != nil {
		return nil, err
	}
	copyPool := *p
	return &copyPool, nil
}

// ListPools returns all pools, newest first by CreatedAt (ID breaks ties).
func (f *FSMElasticPool) ListPools() ([]Pool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	pools, err := f.loadPoolsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Pool, 0, len(pools))
	for _, p := range pools {
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// ----------------------------------------------------------------------------
// Node membership
// ----------------------------------------------------------------------------

// AddNode joins a fresh node to an active pool: TotalSlots = SlotsPerNode,
// UsedSlots = 0, Status = ready. Rejects when the pool is already at MaxNodes.
// Writes attestation action "elasticpool.node.add".
func (f *FSMElasticPool) AddNode(ctx context.Context, poolID string) (*Node, error) {
	if err := validateID(poolID, "pool ID"); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, poolID, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	p, err := f.getPoolLocked(poolID)
	if err != nil {
		return nil, err
	}
	if p.Status != PoolActive {
		return nil, fmt.Errorf("elasticpool: pool %q is %q; nodes join only active pools", poolID, p.Status)
	}

	nodes, err := f.loadNodesLocked(poolID)
	if err != nil {
		return nil, err
	}
	if len(nodes) >= p.MaxNodes {
		return nil, fmt.Errorf("%w: pool %q already has %d/%d nodes; evaluate elasticity first", ErrPoolFull, poolID, len(nodes), p.MaxNodes)
	}

	nodeID, err := genID("node-", 12)
	if err != nil {
		return nil, err
	}

	n := &Node{
		ID:         nodeID,
		PoolID:     poolID,
		TotalSlots: p.SlotsPerNode,
		UsedSlots:  0,
		Status:     NodeReady,
		JoinedAt:   time.Now().UTC(),
	}
	nodes[nodeID] = *n
	if err := f.persistNodesLocked(poolID, nodes); err != nil {
		return nil, err
	}

	if err := f.attestLocked(ctx, "elasticpool.node.add", poolID, DefaultActor,
		map[string]any{"pool_id": poolID},
		map[string]any{"node_id": nodeID, "total_slots": n.TotalSlots, "status": string(n.Status)},
		map[string]any{"nodes": len(nodes), "max_nodes": p.MaxNodes}); err != nil {
		return nil, err
	}
	return n, nil
}

// ListNodes returns the pool's nodes, newest first by JoinedAt (ID breaks ties).
func (f *FSMElasticPool) ListNodes(poolID string) ([]Node, error) {
	if err := validateID(poolID, "pool ID"); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, poolID, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, err := f.getPoolLocked(poolID); err != nil {
		return nil, err
	}
	nodes, err := f.loadNodesLocked(poolID)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].JoinedAt.Equal(out[j].JoinedAt) {
			return out[i].JoinedAt.After(out[j].JoinedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// ----------------------------------------------------------------------------
// Slot leasing
// ----------------------------------------------------------------------------

// Acquire leases slots on one node for a Module 15 inference service using
// best-fit placement: among ready nodes with UsedSlots+slots <= TotalSlots it
// picks the one with the smallest remaining space, minimizing fragmentation.
// When the chosen node fills up (UsedSlots == TotalSlots) it flips to busy.
// ServiceID must carry the "inf-" prefix (opaque; no pkg/inference dependency).
// Writes attestation action "elasticpool.lease.acquire".
func (f *FSMElasticPool) Acquire(ctx context.Context, poolID, serviceID string, slots int) (*SlotLease, error) {
	if err := validateID(poolID, "pool ID"); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, poolID, err)
	}
	if !strings.HasPrefix(serviceID, "inf-") || len(serviceID) <= len("inf-") {
		return nil, fmt.Errorf("elasticpool: service ID %q must carry the \"inf-\" prefix (Module 15 mesh service)", serviceID)
	}
	if slots <= 0 {
		return nil, fmt.Errorf("elasticpool: slot count (%d) must be positive", slots)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	p, err := f.getPoolLocked(poolID)
	if err != nil {
		return nil, err
	}
	if p.Status == PoolDeleted {
		return nil, fmt.Errorf("elasticpool: pool %q is deleted; leases rejected", poolID)
	}

	nodes, err := f.loadNodesLocked(poolID)
	if err != nil {
		return nil, err
	}

	// Best-fit: smallest free space that still satisfies the request.
	ordered := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		ordered = append(ordered, n)
	}
	sort.SliceStable(ordered, func(i, j int) bool { // deterministic: JoinedAt, then ID
		if !ordered[i].JoinedAt.Equal(ordered[j].JoinedAt) {
			return ordered[i].JoinedAt.Before(ordered[j].JoinedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	var chosen *Node
	for i := range ordered {
		n := &ordered[i]
		if n.Status != NodeReady {
			continue // busy nodes are full; drained nodes host nothing
		}
		if n.UsedSlots+slots <= n.TotalSlots {
			if chosen == nil || (n.TotalSlots-n.UsedSlots) < (chosen.TotalSlots-chosen.UsedSlots) {
				chosen = n
			}
		}
	}
	if chosen == nil {
		free := 0
		for _, n := range ordered {
			if n.Status == NodeReady && n.TotalSlots-n.UsedSlots > free {
				free = n.TotalSlots - n.UsedSlots
			}
		}
		return nil, fmt.Errorf("%w: no ready node in pool %q can fit %d slot(s) (largest free: %d); add nodes via node-add or evaluate elasticity to scale up",
			ErrNoCapacity, poolID, slots, free)
	}

	leaseID, err := genID("lease-", 12)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	lease := &SlotLease{
		ID:         leaseID,
		PoolID:     poolID,
		NodeID:     chosen.ID,
		ServiceID:  serviceID,
		Slots:      slots,
		AcquiredAt: now,
	}

	chosen.UsedSlots += slots
	if chosen.UsedSlots == chosen.TotalSlots {
		chosen.Status = NodeBusy
	}
	nodes[chosen.ID] = *chosen
	if err := f.persistNodesLocked(poolID, nodes); err != nil {
		return nil, err
	}
	if err := f.appendLeaseLocked(poolID, lease); err != nil {
		return nil, err
	}

	if err := f.attestLocked(ctx, "elasticpool.lease.acquire", leaseID, DefaultActor,
		map[string]any{"pool_id": poolID, "service_id": serviceID, "slots": slots},
		map[string]any{"lease_id": leaseID, "node_id": chosen.ID, "node_status": string(chosen.Status)},
		map[string]any{"best_fit_free_after": chosen.TotalSlots - chosen.UsedSlots}); err != nil {
		return nil, err
	}
	return lease, nil
}

// Leases returns the pool's leases, newest first by AcquiredAt (ID breaks
// ties). limit <= 0 returns all entries. The leases.jsonl log is append-only;
// per lease ID the last written record wins (release appends the updated row).
func (f *FSMElasticPool) Leases(poolID string, limit int) ([]SlotLease, error) {
	if err := validateID(poolID, "pool ID"); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, poolID, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, err := f.getPoolLocked(poolID); err != nil {
		return nil, err
	}
	all, err := f.loadLeasesLocked(poolID)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].AcquiredAt.Equal(all[j].AcquiredAt) {
			return all[i].AcquiredAt.After(all[j].AcquiredAt)
		}
		return all[i].ID > all[j].ID
	})

	n := len(all)
	if limit > 0 && limit < n {
		all = all[:limit]
	}
	if all == nil {
		all = []SlotLease{}
	}
	return all, nil
}

// Release frees a held lease by lease ID (located by scanning pool lease logs;
// use FindLease to resolve the owning pool explicitly). Releasing an
// already-released lease is an explicit error (idempotent reject). The owning
// node's UsedSlots drops by the leased amount (floored at 0) and a busy node
// with spare capacity returns to ready. When the node ends up with
// UsedSlots == 0 it transitions to drained — resources are being emptied and
// the node temporarily does NOT accept new leases (Acquire only places on
// ready nodes); it is now eligible for pool removal.
// Writes attestation action "elasticpool.lease.release".
func (f *FSMElasticPool) Release(ctx context.Context, leaseID string) (*SlotLease, error) {
	if err := validateID(leaseID, "lease ID"); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, leaseID, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	poolID, lease, err := f.findLeaseLocked(leaseID)
	if err != nil {
		return nil, err
	}
	if lease.ReleasedAt != nil {
		return nil, fmt.Errorf("%w: lease %q was already released at %s", ErrAlreadyReleased, leaseID, lease.ReleasedAt.Format(time.RFC3339))
	}

	now := time.Now().UTC()
	lease.ReleasedAt = &now

	nodes, err := f.loadNodesLocked(poolID)
	if err != nil {
		return nil, err
	}
	if node, ok := nodes[lease.NodeID]; ok {
		node.UsedSlots -= lease.Slots
		if node.UsedSlots < 0 {
			node.UsedSlots = 0 // defensive floor
		}
		if node.Status == NodeBusy && node.UsedSlots < node.TotalSlots {
			node.Status = NodeReady
		}
		if node.UsedSlots == 0 && node.Status == NodeReady {
			node.Status = NodeDrained
		}
		nodes[lease.NodeID] = node
		if err := f.persistNodesLocked(poolID, nodes); err != nil {
			return nil, err
		}
	}

	// Append-only log: the updated lease row supersedes the acquire row.
	if err := f.appendLeaseLocked(poolID, lease); err != nil {
		return nil, err
	}

	if err := f.attestLocked(ctx, "elasticpool.lease.release", leaseID, DefaultActor,
		map[string]any{"lease_id": leaseID},
		map[string]any{"released_at": now.Format(time.RFC3339)},
		map[string]any{"pool_id": poolID, "node_id": lease.NodeID, "slots_freed": lease.Slots}); err != nil {
		return nil, err
	}
	return lease, nil
}

// FindLease resolves a lease ID to its owning pool and latest state. It scans
// every pool's leases.jsonl — the simplest index for `cafctl pool release`.
func (f *FSMElasticPool) FindLease(leaseID string) (string, *SlotLease, error) {
	if err := validateID(leaseID, "lease ID"); err != nil {
		return "", nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, leaseID, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.findLeaseLocked(leaseID)
}

// findLeaseLocked is the lock-held core of FindLease.
func (f *FSMElasticPool) findLeaseLocked(leaseID string) (string, *SlotLease, error) {
	pools, err := f.loadPoolsLocked()
	if err != nil {
		return "", nil, err
	}
	ids := make([]string, 0, len(pools))
	for id := range pools {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic scan order
	for _, poolID := range ids {
		leases, lerr := f.loadLeasesLocked(poolID)
		if lerr != nil {
			return "", nil, lerr
		}
		for i := range leases {
			if leases[i].ID == leaseID {
				found := leases[i]
				return poolID, &found, nil
			}
		}
	}
	return "", nil, fmt.Errorf("%w: lease %q", ErrNotFound, leaseID)
}

// ----------------------------------------------------------------------------
// Elasticity evaluation
// ----------------------------------------------------------------------------

// EvaluateElasticity weighs pending slot demand against free capacity under a
// hard budget, mirroring pkg/scaler's budget math exactly. Rules:
//
//   - pending > free: needs ceil((pending-free)/SlotsPerNode) new nodes,
//     target capped at MaxNodes, costImpact = added × CostPerNodeHour; when
//     currentCost + costImpact > budgetLimit the decision is BUDGET REJECTED
//     (BudgetOK=false, Action="no_change"), otherwise Action="scale_up".
//   - utilization < 30% and nodes > MinNodes: Action="scale_down" to
//     max(MinNodes, ceil(used/SlotsPerNode)); costImpact is the savings
//     (negative).
//   - otherwise Action="no_change".
//
// The decision is appended to <poolID>/decisions.jsonl and attested under
// action "elasticpool.evaluate".
func (f *FSMElasticPool) EvaluateElasticity(ctx context.Context, poolID string, pendingDemandSlots int, budgetLimit, currentCost float64) (*ElasticDecision, error) {
	if err := validateID(poolID, "pool ID"); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidID, poolID, err)
	}
	if pendingDemandSlots < 0 {
		return nil, fmt.Errorf("elasticpool: pending demand (%d) cannot be negative", pendingDemandSlots)
	}
	// Non-finite money values would poison every comparison below (NaN > x is
	// always false, so a NaN budget would never reject anything).
	if math.IsNaN(budgetLimit) || math.IsInf(budgetLimit, 0) {
		return nil, fmt.Errorf("elasticpool: budget limit must be finite (got %v)", budgetLimit)
	}
	if math.IsNaN(currentCost) || math.IsInf(currentCost, 0) {
		return nil, fmt.Errorf("elasticpool: current cost must be finite (got %v)", currentCost)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	p, err := f.getPoolLocked(poolID)
	if err != nil {
		return nil, err
	}
	nodes, err := f.loadNodesLocked(poolID)
	if err != nil {
		return nil, err
	}

	currentNodes := len(nodes)
	totalSlots, usedSlots := 0, 0
	for _, n := range nodes {
		totalSlots += n.TotalSlots
		usedSlots += n.UsedSlots
	}
	freeSlots := totalSlots - usedSlots
	if freeSlots < 0 {
		freeSlots = 0
	}

	decisionID, err := genID("el-", 16)
	if err != nil {
		return nil, err
	}

	action := "no_change"
	targetNodes := currentNodes
	costImpact := 0.0
	budgetOK := true
	var reasonParts []string

	switch {
	case pendingDemandSlots > freeSlots:
		deficit := pendingDemandSlots - freeSlots
		needNodes := (deficit + p.SlotsPerNode - 1) / p.SlotsPerNode // integer ceil
		targetNodes = currentNodes + needNodes
		if targetNodes > p.MaxNodes {
			targetNodes = p.MaxNodes
			reasonParts = append(reasonParts, fmt.Sprintf("target capped at max_nodes=%d", p.MaxNodes))
		}
		addNodes := targetNodes - currentNodes
		costImpact = float64(addNodes) * p.CostPerNodeHour
		reasonParts = append(reasonParts, fmt.Sprintf("pending demand %d slots > free %d slots; deficit %d needs %d node(s) at %d slots/node",
			pendingDemandSlots, freeSlots, deficit, needNodes, p.SlotsPerNode))

		// Budget math — identical shape to pkg/scaler: reject when
		// currentCost + costImpact exceeds budgetLimit. The epsilon tolerates
		// float rounding so equality (newCost == budgetLimit) accepts — the
		// guard is strictly ">", not ">=".
		newCost := currentCost + costImpact
		const budgetEps = 1e-9
		if newCost > budgetLimit+budgetEps {
			budgetOK = false
			action = "no_change"
			targetNodes = currentNodes
			reasonParts = append(reasonParts, fmt.Sprintf("BUDGET REJECTED: $%.2f+%.2f > $%.2f (over by $%.2f)",
				currentCost, costImpact, budgetLimit, newCost-budgetLimit))
		} else {
			action = "scale_up"
			reasonParts = append(reasonParts, fmt.Sprintf("within budget: $%.2f+%.2f ≤ $%.2f", currentCost, costImpact, budgetLimit))
		}

	default:
		// Capacity covers demand; consider shrinking when under-utilized.
		utilization := 0.0
		if totalSlots > 0 {
			utilization = float64(usedSlots) / float64(totalSlots) * 100.0
		}
		if utilization < 30.0 && currentNodes > p.MinNodes {
			neededForUsed := (usedSlots + p.SlotsPerNode - 1) / p.SlotsPerNode
			targetNodes = maxInt(p.MinNodes, neededForUsed)
			if targetNodes < currentNodes {
				action = "scale_down"
				costImpact = float64(targetNodes-currentNodes) * p.CostPerNodeHour // negative: savings
				reasonParts = append(reasonParts, fmt.Sprintf("utilization %.1f%% (%d/%d slots) below 30%% threshold; shrink %d → %d nodes, saving $%.2f/node-hour",
					utilization, usedSlots, totalSlots, currentNodes, targetNodes, -costImpact))
			} else {
				targetNodes = currentNodes
				reasonParts = append(reasonParts, fmt.Sprintf("utilization %.1f%% (%d/%d slots) low but workload already fits in %d node(s)",
					utilization, usedSlots, totalSlots, currentNodes))
			}
		} else {
			reasonParts = append(reasonParts, fmt.Sprintf("pending demand %d slots fits in free %d slots", pendingDemandSlots, freeSlots))
			if currentNodes <= p.MinNodes {
				reasonParts = append(reasonParts, fmt.Sprintf("utilization %.1f%% but already at min_nodes=%d", utilization, p.MinNodes))
			} else {
				reasonParts = append(reasonParts, fmt.Sprintf("utilization %.1f%% (%d/%d slots) within bounds", utilization, usedSlots, totalSlots))
			}
		}
	}

	d := &ElasticDecision{
		ID:                decisionID,
		Action:            action,
		Reason:            strings.Join(reasonParts, "; "),
		CurrentNodes:      currentNodes,
		TargetNodes:       targetNodes,
		CostImpactPerHour: costImpact,
		BudgetOK:          budgetOK,
		CreatedAt:         time.Now().UTC(),
	}

	if err := f.appendDecisionLocked(poolID, d); err != nil {
		return nil, err
	}

	if err := f.attestLocked(ctx, "elasticpool.evaluate", decisionID, DefaultActor,
		map[string]any{"pool_id": poolID, "pending_slots": pendingDemandSlots, "budget_limit": budgetLimit, "current_cost": currentCost},
		map[string]any{"action": action, "target_nodes": targetNodes, "budget_ok": budgetOK},
		map[string]any{"free_slots": freeSlots, "used_slots": usedSlots, "cost_impact_per_hour": costImpact}); err != nil {
		return nil, err
	}
	return d, nil
}

// ----------------------------------------------------------------------------
// Internal helpers (caller holds f.mu unless stated otherwise)
// ----------------------------------------------------------------------------

const (
	poolsFile     = "pools.json"
	nodesFile     = "nodes.json"
	leasesFile    = "leases.jsonl"
	decisionsFile = "decisions.jsonl"
)

var idRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// validateID enforces a filesystem-safe ID ([a-z0-9-] only) — the primary
// path-traversal guard for per-pool directories.
func validateID(id, what string) error {
	if id == "" {
		return fmt.Errorf("%s is required", what)
	}
	if !idRe.MatchString(id) {
		return fmt.Errorf("only [a-z0-9-] allowed (path-traversal protection)")
	}
	return nil
}

// genID returns "<prefix><hex>" with hexLen hex characters (crypto/rand).
func genID(prefix string, hexLen int) (string, error) {
	b := make([]byte, hexLen/2)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("elasticpool: generate random ID: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// safeJoin joins base with segments and verifies the resolved path stays
// inside base — defense in depth against path traversal (same pattern as
// inference/modelregistry).
func safeJoin(base string, segs ...string) (string, error) {
	p := base
	for _, s := range segs {
		p = filepath.Join(p, s)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %q", p)
	}
	return p, nil
}

func (f *FSMElasticPool) getPoolLocked(poolID string) (*Pool, error) {
	pools, err := f.loadPoolsLocked()
	if err != nil {
		return nil, err
	}
	p, ok := pools[poolID]
	if !ok {
		return nil, fmt.Errorf("%w: pool %q", ErrNotFound, poolID)
	}
	return &p, nil
}

// persistPoolLocked merges p into pools.json with an atomic tmp+rename.
func (f *FSMElasticPool) persistPoolLocked(p *Pool) error {
	pools, err := f.loadPoolsLocked()
	if err != nil {
		return err
	}
	pools[p.ID] = *p
	data, err := json.MarshalIndent(pools, "", "  ")
	if err != nil {
		return fmt.Errorf("elasticpool: marshal pools: %w", err)
	}
	path, err := safeJoin(f.root, poolsFile)
	if err != nil {
		return fmt.Errorf("elasticpool: pools path: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("elasticpool: write pools tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("elasticpool: commit pools.json: %w", err)
	}
	return nil
}

func (f *FSMElasticPool) loadPoolsLocked() (map[string]Pool, error) {
	path, err := safeJoin(f.root, poolsFile)
	if err != nil {
		return nil, fmt.Errorf("elasticpool: pools path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Pool{}, nil
		}
		return nil, fmt.Errorf("elasticpool: read pools.json: %w", err)
	}
	pools := map[string]Pool{}
	if err := json.Unmarshal(data, &pools); err != nil {
		return nil, fmt.Errorf("elasticpool: parse pools.json: %w", err)
	}
	return pools, nil
}

// persistNodesLocked replaces <poolID>/nodes.json atomically.
func (f *FSMElasticPool) persistNodesLocked(poolID string, nodes map[string]Node) error {
	data, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return fmt.Errorf("elasticpool: marshal nodes for %q: %w", poolID, err)
	}
	path, err := safeJoin(f.root, poolID, nodesFile)
	if err != nil {
		return fmt.Errorf("elasticpool: nodes path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("elasticpool: create pool dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("elasticpool: write nodes tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("elasticpool: commit nodes.json: %w", err)
	}
	return nil
}

func (f *FSMElasticPool) loadNodesLocked(poolID string) (map[string]Node, error) {
	path, err := safeJoin(f.root, poolID, nodesFile)
	if err != nil {
		return nil, fmt.Errorf("elasticpool: nodes path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Node{}, nil
		}
		return nil, fmt.Errorf("elasticpool: read nodes.json for %q: %w", poolID, err)
	}
	nodes := map[string]Node{}
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("elasticpool: parse nodes.json for %q: %w", poolID, err)
	}
	return nodes, nil
}

// appendLeaseLocked appends one lease row to <poolID>/leases.jsonl.
func (f *FSMElasticPool) appendLeaseLocked(poolID string, lease *SlotLease) error {
	path, err := safeJoin(f.root, poolID, leasesFile)
	if err != nil {
		return fmt.Errorf("elasticpool: leases path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("elasticpool: create pool dir: %w", err)
	}
	line, err := json.Marshal(lease)
	if err != nil {
		return fmt.Errorf("elasticpool: marshal lease: %w", err)
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("elasticpool: open leases file: %w", err)
	}
	// Record the pre-append size so a failed/partial write can be rolled back:
	// a torn JSONL line would poison every future Leases() read of this file.
	info, err := fh.Stat()
	if err != nil {
		if cerr := fh.Close(); cerr != nil {
			return fmt.Errorf("elasticpool: stat leases file: %w; also failed to close: %v", err, cerr)
		}
		return fmt.Errorf("elasticpool: stat leases file: %w", err)
	}
	offset := info.Size()
	n, werr := fh.Write(append(line, '\n'))
	if werr != nil || n != len(line)+1 {
		// Roll back any partial bytes so the JSONL log never carries a torn
		// line that would poison future Leases() reads. Best effort rollback;
		// report the original write failure with additional context.
		if cerr := fh.Close(); cerr != nil {
			_ = os.Truncate(path, offset) // best effort
			return fmt.Errorf("elasticpool: append lease (wrote %d of %d bytes): %w; also failed to close: %v", n, len(line)+1, werr, cerr)
		}
		if terr := os.Truncate(path, offset); terr != nil {
			return fmt.Errorf("elasticpool: append lease (wrote %d of %d bytes): %w; rollback failed: %v", n, len(line)+1, werr, terr)
		}
		if werr == nil {
			werr = io.ErrShortWrite
		}
		return fmt.Errorf("elasticpool: append lease (wrote %d of %d bytes): %w", n, len(line)+1, werr)
	}
	if err := fh.Close(); err != nil {
		return fmt.Errorf("elasticpool: close leases file: %w", err)
	}
	return nil
}

// loadLeasesLocked reads <poolID>/leases.jsonl and merges rows per lease ID
// (last write wins — Release appends the updated row), preserving first-seen
// order for stability.
func (f *FSMElasticPool) loadLeasesLocked(poolID string) ([]SlotLease, error) {
	path, err := safeJoin(f.root, poolID, leasesFile)
	if err != nil {
		return nil, fmt.Errorf("elasticpool: leases path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []SlotLease{}, nil
		}
		return nil, fmt.Errorf("elasticpool: read leases for %q: %w", poolID, err)
	}

	var order []string
	byID := map[string]SlotLease{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var l SlotLease
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			return nil, fmt.Errorf("elasticpool: parse lease line %d for %q: %w", i+1, poolID, err)
		}
		if _, seen := byID[l.ID]; !seen {
			order = append(order, l.ID)
		}
		byID[l.ID] = l
	}
	out := make([]SlotLease, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}

// appendDecisionLocked appends one decision row to <poolID>/decisions.jsonl.
func (f *FSMElasticPool) appendDecisionLocked(poolID string, d *ElasticDecision) error {
	path, err := safeJoin(f.root, poolID, decisionsFile)
	if err != nil {
		return fmt.Errorf("elasticpool: decisions path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("elasticpool: create pool dir: %w", err)
	}
	line, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("elasticpool: marshal decision: %w", err)
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("elasticpool: open decisions file: %w", err)
	}
	// Record the pre-append size so a failed/partial write can be rolled back:
	// a torn JSONL line would poison every future elasticity reads of this file.
	info, err := fh.Stat()
	if err != nil {
		if cerr := fh.Close(); cerr != nil {
			return fmt.Errorf("elasticpool: stat decisions file: %w; also failed to close: %v", err, cerr)
		}
		return fmt.Errorf("elasticpool: stat decisions file: %w", err)
	}
	offset := info.Size()
	n, werr := fh.Write(append(line, '\n'))
	if werr != nil || n != len(line)+1 {
		// Roll back any partial bytes so the JSONL log never carries a torn
		// line that would poison future elasticity reads. Best effort rollback;
		// report the original write failure with additional context.
		if cerr := fh.Close(); cerr != nil {
			_ = os.Truncate(path, offset) // best effort
			return fmt.Errorf("elasticpool: append decision (wrote %d of %d bytes): %w; also failed to close: %v", n, len(line)+1, werr, cerr)
		}
		if terr := os.Truncate(path, offset); terr != nil {
			return fmt.Errorf("elasticpool: append decision (wrote %d of %d bytes): %w; rollback failed: %v", n, len(line)+1, werr, terr)
		}
		if werr == nil {
			werr = io.ErrShortWrite
		}
		return fmt.Errorf("elasticpool: append decision (wrote %d of %d bytes): %w", n, len(line)+1, werr)
	}
	if err := fh.Close(); err != nil {
		return fmt.Errorf("elasticpool: close decisions file: %w", err)
	}
	return nil
}

// attestLocked writes one receipt through the evidence ledger (real signing
// and hash chaining; the backing store depends on the injected ledger).
// Caller holds f.mu.
func (f *FSMElasticPool) attestLocked(ctx context.Context, action, subject, actor string, input, output, payload map[string]any) error {
	if f.ledger == nil {
		return nil
	}
	ev, err := f.ledger.Record(ctx, evidence.RecordInput{
		Actor:   actor,
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("elasticpool: attestation %s failed: %w", action, err)
	}
	f.last = ev
	return nil
}
