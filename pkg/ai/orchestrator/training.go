// Package orchestrator implements Modules 14-16: AI/ML Workload Management.
//
// Module 14 (training.go)  — training job orchestrator: DAG pipelines, gang scheduling,
//                            checkpoint management, and a strict job state machine.
// Module 15 (inference.go) — inference service mesh: endpoint autoscaling, GPU memory
//                            pooling with fragmentation diagnostics, cold-start warming,
//                            and model/version routing with canary weights.
// Module 16 (autoscale.go) — elastic scaling engine: threshold (HPA-compatible) policy,
//                            an RL policy seam, jitter suppression via cooldown windows,
//                            and cross-pool arbitration between training and inference.
//
// Relationship to sibling packages: pkg/training, pkg/inference and pkg/scaler already
// implement the *evidence-and-lifecycle* face of these modules (filesystem-persisted
// records, signed attestations via pkg/evidence, CLI-facing operations). This package is
// deliberately the *algorithmic scheduling* layer: it owns the graph, packing, retention
// and control-loop math that those packages do not implement. Nothing here duplicates
// their persistence or attestation logic, and this package does not import them.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Module 14 — Job state machine
// ============================================================================

// JobState is a training job lifecycle state.
type JobState string

const (
	// StatePending means the job is admitted but has no resources yet.
	StatePending JobState = "Pending"
	// StateScheduled means a gang of workers has been allocated but execution has not begun.
	StateScheduled JobState = "Scheduled"
	// StateRunning means the job's pipeline is executing.
	StateRunning JobState = "Running"
	// StateSucceeded is a terminal state: the whole pipeline completed.
	StateSucceeded JobState = "Succeeded"
	// StateFailed is a terminal state: admission or a pipeline stage failed.
	StateFailed JobState = "Failed"
	// StatePreempted means resources were reclaimed. It is not terminal: a preempted job
	// may return to Pending to be requeued and resumed from its latest checkpoint.
	StatePreempted JobState = "Preempted"
)

// ErrIllegalTransition is returned when a caller attempts a transition the state machine forbids.
var ErrIllegalTransition = errors.New("orchestrator: illegal state transition")

// legalTransitions is the adjacency list of the job state machine. Terminal states map to
// an empty set, so any transition out of them is rejected.
var legalTransitions = map[JobState][]JobState{
	StatePending:   {StateScheduled, StateFailed},
	StateScheduled: {StateRunning, StatePreempted, StateFailed},
	StateRunning:   {StateSucceeded, StateFailed, StatePreempted},
	StateSucceeded: {},
	StateFailed:    {},
	StatePreempted: {StatePending},
}

// CanTransition reports whether from → to is a legal move.
func CanTransition(from, to JobState) bool {
	for _, allowed := range legalTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// AllowedTransitions returns the legal successor states of s, sorted for stable output.
func AllowedTransitions(s JobState) []JobState {
	out := append([]JobState(nil), legalTransitions[s]...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// IsTerminal reports whether s admits no further transitions.
func IsTerminal(s JobState) bool { return len(legalTransitions[s]) == 0 }

// StateChange is one recorded move through the state machine.
type StateChange struct {
	From   JobState
	To     JobState
	At     time.Time
	Reason string
}

// ============================================================================
// Module 14 — DAG pipeline
// ============================================================================

// StageFunc is the work a stage performs. A nil StageFunc is a no-op, which keeps
// scheduling benchmarks free of synthetic sleep time.
type StageFunc func(ctx context.Context) error

// Stage is one node of a training pipeline DAG.
type Stage struct {
	ID  string
	Run StageFunc
}

// Dep is a DAG edge: From must complete before To may start.
type Dep struct {
	From string
	To   string
}

// Pipeline is a DAG of training stages.
type Pipeline struct {
	Nodes []Stage
	Edges []Dep
}

// CycleError reports a dependency cycle, naming the members so the operator can fix the graph.
type CycleError struct {
	// Nodes are the stages that could never reach in-degree zero, sorted.
	Nodes []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("orchestrator: pipeline has a cyclic dependency involving stage(s) [%s]",
		strings.Join(e.Nodes, ", "))
}

// Validate checks structural integrity: non-empty unique stage IDs and edges that
// reference declared stages only.
func (p Pipeline) Validate() error {
	if len(p.Nodes) == 0 {
		return errors.New("orchestrator: pipeline has no stages")
	}
	seen := make(map[string]bool, len(p.Nodes))
	for _, n := range p.Nodes {
		if strings.TrimSpace(n.ID) == "" {
			return errors.New("orchestrator: pipeline stage has an empty ID")
		}
		if seen[n.ID] {
			return fmt.Errorf("orchestrator: duplicate pipeline stage %q", n.ID)
		}
		seen[n.ID] = true
	}
	for _, e := range p.Edges {
		if !seen[e.From] {
			return fmt.Errorf("orchestrator: edge %s->%s references unknown stage %q", e.From, e.To, e.From)
		}
		if !seen[e.To] {
			return fmt.Errorf("orchestrator: edge %s->%s references unknown stage %q", e.From, e.To, e.To)
		}
		if e.From == e.To {
			return &CycleError{Nodes: []string{e.From}}
		}
	}
	return nil
}

// Levels groups stages into dependency levels using Kahn's algorithm. Stages inside one
// level have no ordering constraint between them and may run concurrently; level i always
// completes before level i+1. Each level is sorted for deterministic execution order.
// A cyclic graph yields *CycleError.
func (p Pipeline) Levels() ([][]string, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	indegree := make(map[string]int, len(p.Nodes))
	successors := make(map[string][]string, len(p.Nodes))
	for _, n := range p.Nodes {
		indegree[n.ID] = 0
	}
	// Deduplicate edges so a repeated dependency cannot inflate in-degree and fake a cycle.
	type edgeKey struct{ from, to string }
	seenEdge := make(map[edgeKey]bool, len(p.Edges))
	for _, e := range p.Edges {
		k := edgeKey{e.From, e.To}
		if seenEdge[k] {
			continue
		}
		seenEdge[k] = true
		successors[e.From] = append(successors[e.From], e.To)
		indegree[e.To]++
	}

	ready := make([]string, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		if indegree[n.ID] == 0 {
			ready = append(ready, n.ID)
		}
	}
	sort.Strings(ready)

	levels := make([][]string, 0)
	settled := 0
	for len(ready) > 0 {
		levels = append(levels, ready)
		settled += len(ready)
		next := make([]string, 0)
		for _, id := range ready {
			for _, succ := range successors[id] {
				indegree[succ]--
				if indegree[succ] == 0 {
					next = append(next, succ)
				}
			}
		}
		sort.Strings(next)
		ready = next
	}

	if settled != len(p.Nodes) {
		stuck := make([]string, 0, len(p.Nodes)-settled)
		for id, deg := range indegree {
			if deg > 0 {
				stuck = append(stuck, id)
			}
		}
		sort.Strings(stuck)
		return nil, &CycleError{Nodes: stuck}
	}
	return levels, nil
}

// TopoOrder returns a flat topological ordering of stage IDs, or *CycleError.
func (p Pipeline) TopoOrder() ([]string, error) {
	levels, err := p.Levels()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(p.Nodes))
	for _, lvl := range levels {
		out = append(out, lvl...)
	}
	return out, nil
}

// stage returns the stage with the given ID.
func (p Pipeline) stage(id string) (Stage, bool) {
	for _, n := range p.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Stage{}, false
}

// ============================================================================
// Module 14 — Gang scheduling
// ============================================================================

// NodeCapacity is an immutable snapshot of one node's resources.
type NodeCapacity struct {
	NodeID    string
	TotalGPUs int
	FreeGPUs  int
	TotalMemMB int
	FreeMemMB  int
}

// GangRequest asks for N identical workers that must be placed all-or-nothing.
type GangRequest struct {
	JobID          string
	Workers        int
	GPUsPerWorker  int
	MemMBPerWorker int
}

// WorkerPlacement records where a single gang member landed.
type WorkerPlacement struct {
	Worker int
	NodeID string
	GPUs   int
	MemMB  int
}

// Placement is the accepted placement of an entire gang.
type Placement struct {
	JobID       string
	Workers     []WorkerPlacement
	AllocatedAt time.Time
}

// GangUnsatisfiableError explains exactly why a gang could not be placed. It is returned
// only after the pool has been left completely untouched.
type GangUnsatisfiableError struct {
	JobID          string
	Workers        int
	Placed         int
	GPUsPerWorker  int
	MemMBPerWorker int
	FreeGPUs       int
	FreeMemMB      int
}

func (e *GangUnsatisfiableError) Error() string {
	return fmt.Sprintf("orchestrator: gang for job %q unsatisfiable: placed %d/%d workers "+
		"(each needs %d GPU + %d MB); cluster free: %d GPU, %d MB; no resources were reserved",
		e.JobID, e.Placed, e.Workers, e.GPUsPerWorker, e.MemMBPerWorker, e.FreeGPUs, e.FreeMemMB)
}

// ErrGangAlreadyAllocated is returned when a job already holds a gang lease.
var ErrGangAlreadyAllocated = errors.New("orchestrator: gang already allocated for job")

// ErrNoSuchGang is returned when releasing an unknown gang lease.
var ErrNoSuchGang = errors.New("orchestrator: no gang lease for job")

type poolNode struct {
	id         string
	totalGPUs  int
	freeGPUs   int
	totalMemMB int
	freeMemMB  int
}

// ResourcePool tracks node resources and performs all-or-nothing gang allocation.
// It is safe for concurrent use.
type ResourcePool struct {
	mu     sync.Mutex
	nodes  map[string]*poolNode
	order  []string // node IDs in insertion-sorted order for deterministic packing
	leases map[string]*Placement
	now    func() time.Time
}

// NewResourcePool creates an empty pool.
func NewResourcePool() *ResourcePool {
	return &ResourcePool{
		nodes:  make(map[string]*poolNode),
		leases: make(map[string]*Placement),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// AddNode registers a node with the given capacity.
func (p *ResourcePool) AddNode(nodeID string, gpus, memMB int) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("orchestrator: node ID is required")
	}
	if gpus < 0 || memMB < 0 {
		return errors.New("orchestrator: node capacity cannot be negative")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.nodes[nodeID]; dup {
		return fmt.Errorf("orchestrator: node %q already registered", nodeID)
	}
	p.nodes[nodeID] = &poolNode{id: nodeID, totalGPUs: gpus, freeGPUs: gpus, totalMemMB: memMB, freeMemMB: memMB}
	p.order = append(p.order, nodeID)
	sort.Strings(p.order)
	return nil
}

// AllocateGang places all req.Workers workers or none of them (gang / all-or-nothing
// semantics). Placement is computed against a scratch copy of the free counters and is
// committed to the pool only after every worker has a home, so a rejected request
// provably leaves no residual reservation behind.
func (p *ResourcePool) AllocateGang(req GangRequest) (*Placement, error) {
	if strings.TrimSpace(req.JobID) == "" {
		return nil, errors.New("orchestrator: gang request needs a job ID")
	}
	if req.Workers <= 0 {
		return nil, errors.New("orchestrator: gang request needs at least one worker")
	}
	if req.GPUsPerWorker < 0 || req.MemMBPerWorker < 0 {
		return nil, errors.New("orchestrator: gang request resources cannot be negative")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.leases[req.JobID]; exists {
		return nil, fmt.Errorf("%w: %s", ErrGangAlreadyAllocated, req.JobID)
	}

	// Scratch counters: the real pool is not mutated until the gang is fully satisfied.
	scratchGPU := make(map[string]int, len(p.nodes))
	scratchMem := make(map[string]int, len(p.nodes))
	for id, n := range p.nodes {
		scratchGPU[id] = n.freeGPUs
		scratchMem[id] = n.freeMemMB
	}

	placements := make([]WorkerPlacement, 0, req.Workers)
	for w := 0; w < req.Workers; w++ {
		placed := false
		for _, id := range p.order {
			if scratchGPU[id] >= req.GPUsPerWorker && scratchMem[id] >= req.MemMBPerWorker {
				scratchGPU[id] -= req.GPUsPerWorker
				scratchMem[id] -= req.MemMBPerWorker
				placements = append(placements, WorkerPlacement{
					Worker: w, NodeID: id, GPUs: req.GPUsPerWorker, MemMB: req.MemMBPerWorker,
				})
				placed = true
				break
			}
		}
		if !placed {
			freeGPU, freeMem := 0, 0
			for _, n := range p.nodes {
				freeGPU += n.freeGPUs
				freeMem += n.freeMemMB
			}
			return nil, &GangUnsatisfiableError{
				JobID: req.JobID, Workers: req.Workers, Placed: len(placements),
				GPUsPerWorker: req.GPUsPerWorker, MemMBPerWorker: req.MemMBPerWorker,
				FreeGPUs: freeGPU, FreeMemMB: freeMem,
			}
		}
	}

	// Commit.
	for _, wp := range placements {
		n := p.nodes[wp.NodeID]
		n.freeGPUs -= wp.GPUs
		n.freeMemMB -= wp.MemMB
	}
	pl := &Placement{JobID: req.JobID, Workers: placements, AllocatedAt: p.now()}
	p.leases[req.JobID] = pl
	return pl, nil
}

// ReleaseGang returns a job's reserved resources to the pool.
func (p *ResourcePool) ReleaseGang(jobID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	pl, ok := p.leases[jobID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoSuchGang, jobID)
	}
	for _, wp := range pl.Workers {
		n, exists := p.nodes[wp.NodeID]
		if !exists {
			continue
		}
		n.freeGPUs += wp.GPUs
		if n.freeGPUs > n.totalGPUs {
			n.freeGPUs = n.totalGPUs
		}
		n.freeMemMB += wp.MemMB
		if n.freeMemMB > n.totalMemMB {
			n.freeMemMB = n.totalMemMB
		}
	}
	delete(p.leases, jobID)
	return nil
}

// Nodes returns a snapshot of every node's capacity, sorted by node ID.
func (p *ResourcePool) Nodes() []NodeCapacity {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]NodeCapacity, 0, len(p.nodes))
	for _, id := range p.order {
		n := p.nodes[id]
		out = append(out, NodeCapacity{
			NodeID: n.id, TotalGPUs: n.totalGPUs, FreeGPUs: n.freeGPUs,
			TotalMemMB: n.totalMemMB, FreeMemMB: n.freeMemMB,
		})
	}
	return out
}

// FreeGPUs reports cluster-wide unreserved GPUs.
func (p *ResourcePool) FreeGPUs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := 0
	for _, n := range p.nodes {
		total += n.freeGPUs
	}
	return total
}

// ActiveGangs reports how many gang leases are currently held.
func (p *ResourcePool) ActiveGangs() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.leases)
}

// ============================================================================
// Module 14 — Checkpoint management
// ============================================================================

// Checkpoint is a restartable snapshot of a training job's progress.
type Checkpoint struct {
	JobID           string
	Step            int
	CreatedAt       time.Time
	CompletedStages []string
	Metadata        map[string]string
}

// RetentionPolicy governs checkpoint pruning: keep the newest KeepLast checkpoints, and
// among those additionally evict any older than MaxAge. The single newest checkpoint is
// never pruned, so crash recovery always has something to resume from.
type RetentionPolicy struct {
	KeepLast int
	MaxAge   time.Duration
}

// CheckpointStore persists checkpoints for crash recovery.
type CheckpointStore interface {
	// Save stores a checkpoint, overwriting any existing checkpoint with the same step.
	Save(ctx context.Context, cp Checkpoint) error
	// Load returns the checkpoint at the given step; a negative step means "latest".
	Load(ctx context.Context, jobID string, step int) (*Checkpoint, error)
	// List returns a job's checkpoints ordered by step ascending.
	List(ctx context.Context, jobID string) ([]Checkpoint, error)
	// Prune applies the retention policy and returns the evicted checkpoints.
	Prune(ctx context.Context, jobID string, policy RetentionPolicy) ([]Checkpoint, error)
}

// ErrNoCheckpoint is returned when no checkpoint satisfies a Load.
var ErrNoCheckpoint = errors.New("orchestrator: no checkpoint found")

// MemCheckpointStore is an in-memory CheckpointStore, safe for concurrent use.
type MemCheckpointStore struct {
	mu  sync.Mutex
	cps map[string][]Checkpoint
	now func() time.Time
}

var _ CheckpointStore = (*MemCheckpointStore)(nil)

// NewMemCheckpointStore creates an empty in-memory checkpoint store.
func NewMemCheckpointStore() *MemCheckpointStore {
	return &MemCheckpointStore{
		cps: make(map[string][]Checkpoint),
		now: func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the store's time source; used by tests to exercise age-based pruning.
func (s *MemCheckpointStore) SetClock(fn func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn != nil {
		s.now = fn
	}
}

func cloneCheckpoint(cp Checkpoint) Checkpoint {
	out := cp
	out.CompletedStages = append([]string(nil), cp.CompletedStages...)
	if cp.Metadata != nil {
		out.Metadata = make(map[string]string, len(cp.Metadata))
		for k, v := range cp.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

// Save implements CheckpointStore.
func (s *MemCheckpointStore) Save(ctx context.Context, cp Checkpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(cp.JobID) == "" {
		return errors.New("orchestrator: checkpoint needs a job ID")
	}
	if cp.Step < 0 {
		return errors.New("orchestrator: checkpoint step cannot be negative")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = s.now()
	}
	stored := cloneCheckpoint(cp)
	list := s.cps[cp.JobID]
	for i := range list {
		if list[i].Step == cp.Step {
			list[i] = stored
			s.cps[cp.JobID] = list
			return nil
		}
	}
	list = append(list, stored)
	sort.Slice(list, func(i, j int) bool { return list[i].Step < list[j].Step })
	s.cps[cp.JobID] = list
	return nil
}

// Load implements CheckpointStore. A negative step selects the highest-step checkpoint.
func (s *MemCheckpointStore) Load(ctx context.Context, jobID string, step int) (*Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.cps[jobID]
	if len(list) == 0 {
		return nil, fmt.Errorf("%w for job %q", ErrNoCheckpoint, jobID)
	}
	if step < 0 {
		out := cloneCheckpoint(list[len(list)-1])
		return &out, nil
	}
	for _, cp := range list {
		if cp.Step == step {
			out := cloneCheckpoint(cp)
			return &out, nil
		}
	}
	return nil, fmt.Errorf("%w for job %q step %d", ErrNoCheckpoint, jobID, step)
}

// List implements CheckpointStore.
func (s *MemCheckpointStore) List(ctx context.Context, jobID string) ([]Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Checkpoint, 0, len(s.cps[jobID]))
	for _, cp := range s.cps[jobID] {
		out = append(out, cloneCheckpoint(cp))
	}
	return out, nil
}

// Prune implements CheckpointStore: retain the newest KeepLast checkpoints, then drop any
// of those older than MaxAge, always keeping the single newest one.
func (s *MemCheckpointStore) Prune(ctx context.Context, jobID string, policy RetentionPolicy) ([]Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if policy.KeepLast < 1 {
		return nil, errors.New("orchestrator: retention KeepLast must be at least 1")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.cps[jobID]
	if len(list) == 0 {
		return nil, nil
	}

	evicted := make([]Checkpoint, 0)
	kept := make([]Checkpoint, 0, len(list))

	// Count-based retention: everything before the last KeepLast entries goes.
	cut := len(list) - policy.KeepLast
	if cut < 0 {
		cut = 0
	}
	for i, cp := range list {
		if i < cut {
			evicted = append(evicted, cp)
			continue
		}
		kept = append(kept, cp)
	}

	// Age-based retention over the survivors, never dropping the newest.
	if policy.MaxAge > 0 && len(kept) > 1 {
		cutoff := s.now().Add(-policy.MaxAge)
		survivors := make([]Checkpoint, 0, len(kept))
		for i, cp := range kept {
			isNewest := i == len(kept)-1
			if !isNewest && cp.CreatedAt.Before(cutoff) {
				evicted = append(evicted, cp)
				continue
			}
			survivors = append(survivors, cp)
		}
		kept = survivors
	}

	s.cps[jobID] = kept
	return evicted, nil
}

// ============================================================================
// Module 14 — Job manager
// ============================================================================

// JobSpec describes a training job submission.
type JobSpec struct {
	ID             string
	Name           string
	Workers        int
	GPUsPerWorker  int
	MemMBPerWorker int
	Priority       int
	Pipeline       Pipeline
}

// Job is the tracked state of a submitted training job.
type Job struct {
	Spec        JobSpec
	State       JobState
	SubmittedAt time.Time
	Placement   *Placement
	History     []StateChange
	LastStep    int
}

func (j *Job) clone() Job {
	out := *j
	out.History = append([]StateChange(nil), j.History...)
	if j.Placement != nil {
		p := *j.Placement
		p.Workers = append([]WorkerPlacement(nil), j.Placement.Workers...)
		out.Placement = &p
	}
	return out
}

// ErrJobNotFound is returned for an unknown job ID.
var ErrJobNotFound = errors.New("orchestrator: job not found")

// JobManager owns job lifecycle: submission, gang scheduling, DAG execution,
// checkpointing and state transitions. It is safe for concurrent use.
type JobManager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	pool *ResourcePool
	ckpt CheckpointStore
	now  func() time.Time
}

// NewJobManager builds a manager over a resource pool and checkpoint store.
// Both may be nil: a nil pool disables gang scheduling (Schedule then errors) and a nil
// store disables checkpointing (pipeline execution still works, without recovery points).
func NewJobManager(pool *ResourcePool, ckpt CheckpointStore) *JobManager {
	return &JobManager{
		jobs: make(map[string]*Job),
		pool: pool,
		ckpt: ckpt,
		now:  func() time.Time { return time.Now().UTC() },
	}
}

// Submit admits a job in Pending state. The pipeline is validated up front so a cyclic
// graph is rejected at submission rather than at execution.
func (m *JobManager) Submit(ctx context.Context, spec JobSpec) (*Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.ID) == "" {
		return nil, errors.New("orchestrator: job ID is required")
	}
	if spec.Workers <= 0 {
		return nil, errors.New("orchestrator: job needs at least one worker")
	}
	if len(spec.Pipeline.Nodes) > 0 {
		if _, err := spec.Pipeline.Levels(); err != nil {
			return nil, fmt.Errorf("orchestrator: job %q pipeline invalid: %w", spec.ID, err)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.jobs[spec.ID]; dup {
		return nil, fmt.Errorf("orchestrator: job %q already submitted", spec.ID)
	}
	now := m.now()
	job := &Job{
		Spec:        spec,
		State:       StatePending,
		SubmittedAt: now,
		History:     []StateChange{{From: "", To: StatePending, At: now, Reason: "submitted"}},
		LastStep:    -1,
	}
	m.jobs[spec.ID] = job
	return ptr(job.clone()), nil
}

// transitionLocked applies a guarded state change. Precondition: m.mu held for writing.
func (m *JobManager) transitionLocked(job *Job, to JobState, reason string) error {
	if !CanTransition(job.State, to) {
		return fmt.Errorf("%w: %s -> %s for job %q (allowed from %s: %v)",
			ErrIllegalTransition, job.State, to, job.Spec.ID, job.State, AllowedTransitions(job.State))
	}
	from := job.State
	job.State = to
	job.History = append(job.History, StateChange{From: from, To: to, At: m.now(), Reason: reason})
	return nil
}

// Transition moves a job to an explicit state, rejecting illegal moves with
// ErrIllegalTransition. Resource-bearing exits (Succeeded/Failed/Preempted) release the gang.
func (m *JobManager) Transition(ctx context.Context, jobID string, to JobState, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	if err := m.transitionLocked(job, to, reason); err != nil {
		return err
	}
	if to == StateSucceeded || to == StateFailed || to == StatePreempted {
		m.releasePlacementLocked(job)
	}
	return nil
}

// releasePlacementLocked frees a job's gang if it holds one. Precondition: m.mu held.
func (m *JobManager) releasePlacementLocked(job *Job) {
	if job.Placement == nil || m.pool == nil {
		return
	}
	// A missing lease is benign here (already released); the job's view is what we reset.
	_ = m.pool.ReleaseGang(job.Spec.ID)
	job.Placement = nil
}

// Schedule allocates a gang for a Pending job and moves it to Scheduled. If the gang
// cannot be satisfied the job stays Pending and no resources are reserved.
func (m *JobManager) Schedule(ctx context.Context, jobID string) (*Placement, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	if m.pool == nil {
		return nil, errors.New("orchestrator: job manager has no resource pool")
	}
	if !CanTransition(job.State, StateScheduled) {
		return nil, fmt.Errorf("%w: %s -> %s for job %q",
			ErrIllegalTransition, job.State, StateScheduled, jobID)
	}
	pl, err := m.pool.AllocateGang(GangRequest{
		JobID:          job.Spec.ID,
		Workers:        job.Spec.Workers,
		GPUsPerWorker:  job.Spec.GPUsPerWorker,
		MemMBPerWorker: job.Spec.MemMBPerWorker,
	})
	if err != nil {
		return nil, err
	}
	if err := m.transitionLocked(job, StateScheduled, "gang allocated"); err != nil {
		_ = m.pool.ReleaseGang(job.Spec.ID)
		return nil, err
	}
	job.Placement = pl
	out := *pl
	out.Workers = append([]WorkerPlacement(nil), pl.Workers...)
	return &out, nil
}

// Start moves a Scheduled job to Running.
func (m *JobManager) Start(ctx context.Context, jobID string) error {
	return m.Transition(ctx, jobID, StateRunning, "execution started")
}

// Preempt reclaims a job's resources and marks it Preempted.
func (m *JobManager) Preempt(ctx context.Context, jobID, reason string) error {
	return m.Transition(ctx, jobID, StatePreempted, reason)
}

// Requeue returns a Preempted job to Pending so it can be rescheduled.
func (m *JobManager) Requeue(ctx context.Context, jobID string) error {
	return m.Transition(ctx, jobID, StatePending, "requeued after preemption")
}

// Get returns a copy of a job's current state.
func (m *JobManager) Get(jobID string) (*Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	return ptr(job.clone()), nil
}

// List returns copies of all jobs, ordered by submission time then ID.
func (m *JobManager) List() []Job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].SubmittedAt.Equal(out[j].SubmittedAt) {
			return out[i].SubmittedAt.Before(out[j].SubmittedAt)
		}
		return out[i].Spec.ID < out[j].Spec.ID
	})
	return out
}

// CountByState returns how many jobs sit in each state.
func (m *JobManager) CountByState() map[JobState]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[JobState]int, len(legalTransitions))
	for _, j := range m.jobs {
		out[j.State]++
	}
	return out
}

// PendingCount reports the training backlog; Module 16 consumes this for scale-up pressure.
func (m *JobManager) PendingCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, j := range m.jobs {
		if j.State == StatePending {
			n++
		}
	}
	return n
}

// RunPipeline executes a Running job's DAG level by level; stages within a level run
// concurrently. After each level the manager writes a checkpoint recording the stages
// completed so far, which is what ResumeFrom uses to skip finished work after a crash.
// The first stage error fails the job and is returned.
func (m *JobManager) RunPipeline(ctx context.Context, jobID string) error {
	m.mu.RLock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	state := job.State
	pipeline := job.Spec.Pipeline
	m.mu.RUnlock()

	if state != StateRunning {
		return fmt.Errorf("orchestrator: job %q must be %s to run its pipeline, got %s",
			jobID, StateRunning, state)
	}

	levels, err := pipeline.Levels()
	if err != nil {
		_ = m.Transition(ctx, jobID, StateFailed, err.Error())
		return err
	}

	// Stages already completed in a previous attempt are skipped on resume.
	done := make(map[string]bool)
	if cp, lerr := m.loadLatest(ctx, jobID); lerr == nil && cp != nil {
		for _, id := range cp.CompletedStages {
			done[id] = true
		}
	}

	completed := make([]string, 0, len(pipeline.Nodes))
	for id := range done {
		completed = append(completed, id)
	}
	sort.Strings(completed)

	step := 0
	for _, level := range levels {
		var (
			wg      sync.WaitGroup
			errMu   sync.Mutex
			firstErr error
			doneMu  sync.Mutex
		)
		for _, id := range level {
			if done[id] {
				continue
			}
			st, found := pipeline.stage(id)
			if !found {
				continue
			}
			wg.Add(1)
			go func(s Stage) {
				defer wg.Done()
				if err := ctx.Err(); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					return
				}
				if s.Run != nil {
					if err := s.Run(ctx); err != nil {
						errMu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("orchestrator: stage %q failed: %w", s.ID, err)
						}
						errMu.Unlock()
						return
					}
				}
				doneMu.Lock()
				completed = append(completed, s.ID)
				doneMu.Unlock()
			}(st)
		}
		wg.Wait()

		if firstErr != nil {
			_ = m.Transition(ctx, jobID, StateFailed, firstErr.Error())
			return firstErr
		}

		step++
		sort.Strings(completed)
		if err := m.saveCheckpoint(ctx, jobID, step, completed); err != nil {
			return err
		}
	}

	return m.Transition(ctx, jobID, StateSucceeded, "pipeline completed")
}

// saveCheckpoint records progress and advances the job's LastStep.
func (m *JobManager) saveCheckpoint(ctx context.Context, jobID string, step int, completed []string) error {
	if m.ckpt == nil {
		return nil
	}
	cp := Checkpoint{
		JobID:           jobID,
		Step:            step,
		CreatedAt:       m.now(),
		CompletedStages: append([]string(nil), completed...),
	}
	if err := m.ckpt.Save(ctx, cp); err != nil {
		return fmt.Errorf("orchestrator: save checkpoint for job %q step %d: %w", jobID, step, err)
	}
	m.mu.Lock()
	if job, ok := m.jobs[jobID]; ok {
		job.LastStep = step
	}
	m.mu.Unlock()
	return nil
}

// loadLatest fetches a job's newest checkpoint, or (nil, nil) when checkpointing is off.
func (m *JobManager) loadLatest(ctx context.Context, jobID string) (*Checkpoint, error) {
	if m.ckpt == nil {
		return nil, nil
	}
	cp, err := m.ckpt.Load(ctx, jobID, -1)
	if err != nil {
		return nil, err
	}
	return cp, nil
}

// ResumeFrom prepares a crashed or preempted job to continue from its latest checkpoint.
// It requeues a Preempted job, reschedules a gang, and reports the checkpoint that
// execution will resume from. Stages recorded in that checkpoint are skipped by RunPipeline.
func (m *JobManager) ResumeFrom(ctx context.Context, jobID string) (*Checkpoint, error) {
	cp, err := m.loadLatest(ctx, jobID)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.RUnlock()
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	state := job.State
	m.mu.RUnlock()

	if state == StatePreempted {
		if err := m.Requeue(ctx, jobID); err != nil {
			return nil, err
		}
		state = StatePending
	}
	if state == StatePending {
		if _, err := m.Schedule(ctx, jobID); err != nil {
			return nil, err
		}
		state = StateScheduled
	}
	if state == StateScheduled {
		if err := m.Start(ctx, jobID); err != nil {
			return nil, err
		}
	}
	return cp, nil
}

// ptr returns a pointer to v; a small helper to avoid repeated temporaries.
func ptr[T any](v T) *T { return &v }
