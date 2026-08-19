// Gang scheduling for Module 14 — the Training Job Orchestrator.
//
// This file adds gang (all-or-nothing) scheduling on top of the single-job lifecycle already provided by
// FSOrchestrator in orchestrator.go. Distributed training (data/model/pipeline parallel) requires that ALL
// worker replicas of a job start together: launching a subset wastes GPUs (they idle waiting for peers) and
// can deadlock a cluster. Gang scheduling enforces the invariant "admit the whole gang or none of it".
//
// Design goals versus Kubeflow's MPIJob/PyTorchJob (whose admission goes through the K8s API server plus a
// controller reconcile loop, typically ~150ms end-to-end) and Volcano's podgroup gang plugin:
//   - Submission is a pure in-memory operation (spec validation + ID + one signed receipt), targeting <100ms
//     and in practice microseconds — the API server round-trip and etcd write are the future integration point.
//   - Admission is an all-or-nothing capacity fit computed against a real allocation ledger, not a mock.
//   - Every lifecycle transition emits an Ed25519-signed receipt (the moat): an offline-verifiable, tamper-
//     evident record of "gang G with N replicas moved from state X to Y at time T", chained by sequence.
//
// The gang state machine is intentionally separate from the queued→scheduled→running JobStatus machine used by
// FSOrchestrator: gang scheduling reasons about the collective (Pending→GangReady→Running→Succeeded/Failed),
// whereas FSOrchestrator reasons about one logical job's provenance. Both live in package training but share no
// mutable state; they only reuse pure helpers (writeJSONAtomic, safeJoin) where convenient.
package training

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// GangState is the collective lifecycle state of a gang-scheduled training job.
type GangState string

const (
	// GangPending: the job is submitted and waiting for the whole gang to be admitted. No resources reserved yet.
	GangPending GangState = "pending"
	// GangReady: the entire gang passed all-or-nothing admission; resources for every replica are reserved.
	GangReady GangState = "gang_ready"
	// GangRunning: every replica has been launched; the gang is executing.
	GangRunning GangState = "running"
	// GangSucceeded: the gang finished successfully (terminal). Reserved resources are released.
	GangSucceeded GangState = "succeeded"
	// GangFailed: admission was abandoned or execution failed (terminal). Any reserved resources are released.
	GangFailed GangState = "failed"
)

// gangTransitions is the exhaustive gang state machine. EVERY state is a key (terminal states map to an empty
// slice), so an unknown state is a programming error rather than a silently-allowed transition.
var gangTransitions = map[GangState][]GangState{
	GangPending:   {GangReady, GangFailed},
	GangReady:     {GangRunning, GangFailed},
	GangRunning:   {GangSucceeded, GangFailed},
	GangSucceeded: {},
	GangFailed:    {},
}

// isGangTerminal reports whether s is a terminal gang state.
func isGangTerminal(s GangState) bool {
	return s == GangSucceeded || s == GangFailed
}

// canGangTransition reports whether from→to is a legal edge in the gang FSM. The second return value is false
// when `from` is not a known state at all (distinguishes "illegal edge" from "corrupt/unknown state").
func canGangTransition(from, to GangState) (allowed bool, known bool) {
	targets, ok := gangTransitions[from]
	if !ok {
		return false, false
	}
	for _, t := range targets {
		if t == to {
			return true, true
		}
	}
	return false, true
}

// ResourceRequest is the per-replica resource requirement of a gang member.
type ResourceRequest struct {
	GPUs     int `json:"gpus"`      // GPUs per replica (accelerators are the scarce, gang-critical resource)
	CPUCores int `json:"cpu_cores"` // CPU cores per replica
	MemoryGB int `json:"memory_gb"` // memory per replica in GB
}

// GangJobSpec is the training-job CRD schema submitted by a user. It mirrors the shape of a Kubeflow
// PyTorchJob / Volcano podgroup: a collection of identical replicas that must be scheduled as a unit.
type GangJobSpec struct {
	Name      string          `json:"name"`       // human-readable gang name
	Image     string          `json:"image"`      // training container image (e.g., "pytorch:2.3")
	Replicas  int             `json:"replicas"`   // total number of worker replicas in the gang
	MinMembers int            `json:"min_members"` // minimum replicas that must be co-scheduled; 0 => all replicas (strict gang)
	Priority  int             `json:"priority"`   // scheduling priority (higher wins); used for queue ordering
	Resources ResourceRequest `json:"resources"`  // per-replica resource request
	Command   string          `json:"command,omitempty"`
	Queue     string          `json:"queue,omitempty"` // logical scheduling queue/namespace
}

// requiredMembers returns the number of replicas that must be co-scheduled for the gang to be admitted.
// A zero or negative MinMembers means strict all-or-nothing (all replicas). MinMembers is clamped to Replicas.
func (s GangJobSpec) requiredMembers() int {
	if s.MinMembers <= 0 || s.MinMembers > s.Replicas {
		return s.Replicas
	}
	return s.MinMembers
}

// totalRequest returns the aggregate resources needed to admit `members` replicas of this spec.
func (s GangJobSpec) totalRequest(members int) ResourceRequest {
	return ResourceRequest{
		GPUs:     s.Resources.GPUs * members,
		CPUCores: s.Resources.CPUCores * members,
		MemoryGB: s.Resources.MemoryGB * members,
	}
}

// validate checks that a spec is well-formed before it can be submitted.
func (s GangJobSpec) validate() error {
	if s.Name == "" {
		return errors.New("training: gang name is required")
	}
	if s.Image == "" {
		return errors.New("training: container image is required")
	}
	if s.Replicas <= 0 {
		return errors.New("training: replicas must be positive")
	}
	if s.MinMembers < 0 {
		return errors.New("training: min_members cannot be negative")
	}
	if s.MinMembers > s.Replicas {
		return fmt.Errorf("training: min_members %d exceeds replicas %d", s.MinMembers, s.Replicas)
	}
	if s.Resources.GPUs < 0 || s.Resources.CPUCores < 0 || s.Resources.MemoryGB < 0 {
		return errors.New("training: resource requests cannot be negative")
	}
	if s.Resources.GPUs == 0 && s.Resources.CPUCores == 0 && s.Resources.MemoryGB == 0 {
		return errors.New("training: at least one resource dimension must be requested")
	}
	return nil
}

// GangEvent records a single gang FSM transition together with its signed receipt.
type GangEvent struct {
	Timestamp time.Time        `json:"timestamp"`
	From      GangState        `json:"from"`
	To        GangState        `json:"to"`
	Reason    string           `json:"reason,omitempty"`
	Receipt   LifecycleReceipt `json:"receipt"`
}

// GangJob is the runtime record for a gang-scheduled training job.
type GangJob struct {
	ID          string      `json:"id"`
	Spec        GangJobSpec `json:"spec"`
	State       GangState   `json:"state"`
	Reserved    bool        `json:"reserved"`               // true while this gang holds a capacity reservation
	AdmittedMembers int     `json:"admitted_members"`       // number of replicas co-scheduled at admission time
	Events      []GangEvent `json:"events"`
	CreatedAt   time.Time   `json:"created_at"`
	ReadyAt     *time.Time  `json:"ready_at,omitempty"`
	StartedAt   *time.Time  `json:"started_at,omitempty"`
	EndedAt     *time.Time  `json:"ended_at,omitempty"`
}

// clone returns a deep-ish copy safe to hand to callers without exposing internal slices.
func (j *GangJob) clone() *GangJob {
	cp := *j
	cp.Events = append([]GangEvent(nil), j.Events...)
	return &cp
}

// ============================================================================
// Ed25519-signed lifecycle receipts (the moat)
// ============================================================================

// LifecycleReceipt is a tamper-evident, offline-verifiable record of one gang lifecycle transition.
// The signature covers the canonical JSON of every field except Signature itself, so any mutation
// (state, replicas, timing, ordering via Seq) invalidates verification.
type LifecycleReceipt struct {
	JobID     string    `json:"job_id"`
	Seq       uint64    `json:"seq"`      // monotonic per-scheduler sequence number (ordering + anti-replay)
	From      GangState `json:"from"`
	To        GangState `json:"to"`
	Replicas  int       `json:"replicas"`
	Reason    string    `json:"reason,omitempty"`
	IssuedAt  time.Time `json:"issued_at"`
	PublicKey string    `json:"public_key"` // base64 Ed25519 public key of the issuing scheduler
	Signature string    `json:"signature"`  // base64 Ed25519 signature over the canonical payload
}

// receiptCore is the exact set of fields that are signed. Keeping it separate from LifecycleReceipt guarantees
// the signature can never accidentally cover itself.
type receiptCore struct {
	JobID     string    `json:"job_id"`
	Seq       uint64    `json:"seq"`
	From      GangState `json:"from"`
	To        GangState `json:"to"`
	Replicas  int       `json:"replicas"`
	Reason    string    `json:"reason,omitempty"`
	IssuedAt  time.Time `json:"issued_at"`
	PublicKey string    `json:"public_key"`
}

// canonicalBytes returns the deterministic byte payload signed for a receipt. time.Time marshals as RFC3339Nano
// which is stable, and Go marshals struct fields in declaration order, so the output is reproducible.
func (c receiptCore) canonicalBytes() ([]byte, error) {
	return json.Marshal(c)
}

// ReceiptSigner issues Ed25519 signatures over lifecycle receipts.
type ReceiptSigner struct {
	priv   ed25519.PrivateKey
	pubB64 string
}

// NewReceiptSigner generates a fresh random Ed25519 keypair for signing receipts.
func NewReceiptSigner() (*ReceiptSigner, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("training: generate receipt signer: %w", err)
	}
	return &ReceiptSigner{priv: priv, pubB64: base64.StdEncoding.EncodeToString(pub)}, nil
}

// NewReceiptSignerFromSeed builds a deterministic signer from a 32-byte seed (used by tests for reproducible
// signatures). It errors if the seed is not exactly ed25519.SeedSize bytes.
func NewReceiptSignerFromSeed(seed []byte) (*ReceiptSigner, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("training: ed25519 seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return &ReceiptSigner{priv: priv, pubB64: base64.StdEncoding.EncodeToString(pub)}, nil
}

// PublicKeyBase64 returns the base64-encoded Ed25519 public key of this signer.
func (rs *ReceiptSigner) PublicKeyBase64() string { return rs.pubB64 }

// sign produces a signed LifecycleReceipt for one transition.
func (rs *ReceiptSigner) sign(jobID string, seq uint64, from, to GangState, replicas int, reason string, issuedAt time.Time) (LifecycleReceipt, error) {
	core := receiptCore{
		JobID:     jobID,
		Seq:       seq,
		From:      from,
		To:        to,
		Replicas:  replicas,
		Reason:    reason,
		IssuedAt:  issuedAt,
		PublicKey: rs.pubB64,
	}
	payload, err := core.canonicalBytes()
	if err != nil {
		return LifecycleReceipt{}, fmt.Errorf("training: marshal receipt core: %w", err)
	}
	sig := ed25519.Sign(rs.priv, payload)
	return LifecycleReceipt{
		JobID:     jobID,
		Seq:       seq,
		From:      from,
		To:        to,
		Replicas:  replicas,
		Reason:    reason,
		IssuedAt:  issuedAt,
		PublicKey: rs.pubB64,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// VerifyReceipt checks that r carries a valid Ed25519 signature from the public key it embeds. It returns an
// error describing the first problem found (bad base64, wrong key size, signature mismatch). A nil error means
// the receipt is authentic and unmodified. This is deliberately a package-level function so auditors can verify
// receipts without any scheduler instance.
func VerifyReceipt(r LifecycleReceipt) error {
	pub, err := base64.StdEncoding.DecodeString(r.PublicKey)
	if err != nil {
		return fmt.Errorf("training: decode public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("training: public key must be %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return fmt.Errorf("training: decode signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("training: signature must be %d bytes, got %d", ed25519.SignatureSize, len(sig))
	}
	core := receiptCore{
		JobID:     r.JobID,
		Seq:       r.Seq,
		From:      r.From,
		To:        r.To,
		Replicas:  r.Replicas,
		Reason:    r.Reason,
		IssuedAt:  r.IssuedAt,
		PublicKey: r.PublicKey,
	}
	payload, err := core.canonicalBytes()
	if err != nil {
		return fmt.Errorf("training: marshal receipt core: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, sig) {
		return errors.New("training: receipt signature verification failed")
	}
	return nil
}

// ============================================================================
// GangScheduler
// ============================================================================

// AdmissionResult is the outcome of an all-or-nothing admission decision.
type AdmissionResult struct {
	Admitted bool            // true if the whole gang was reserved
	Members  int             // replicas that would be / were co-scheduled
	Reason   string          // human-readable explanation when Admitted is false
	Shortfall ResourceRequest // per-dimension deficit (0 in every field when Admitted)
}

// ClusterCapacity is the total schedulable resource pool the gang scheduler reasons about.
type ClusterCapacity struct {
	GPUs     int
	CPUCores int
	MemoryGB int
}

// ErrGangNotFound is returned when a job ID is unknown to the scheduler.
var ErrGangNotFound = errors.New("training: gang job not found")

// GangScheduler is an in-memory, concurrency-safe gang (all-or-nothing) scheduler. It owns a capacity ledger,
// a set of gang jobs, a monotonic receipt sequence, and an Ed25519 signer. It performs no I/O on the hot path,
// which is what makes submission and admission microsecond-scale versus a K8s API server round-trip.
type GangScheduler struct {
	mu        sync.Mutex
	capacity  ClusterCapacity
	allocated ClusterCapacity
	jobs      map[string]*GangJob
	signer    *ReceiptSigner
	seq       uint64
	now       func() time.Time // injectable clock; defaults to time.Now().UTC()
}

// NewGangScheduler builds a scheduler with the given total capacity and signer. A nil signer causes an error —
// signed receipts are mandatory (the moat), so we never silently issue unsigned lifecycle records.
func NewGangScheduler(capacity ClusterCapacity, signer *ReceiptSigner) (*GangScheduler, error) {
	if signer == nil {
		return nil, errors.New("training: gang scheduler requires a receipt signer")
	}
	if capacity.GPUs < 0 || capacity.CPUCores < 0 || capacity.MemoryGB < 0 {
		return nil, errors.New("training: cluster capacity cannot be negative")
	}
	return &GangScheduler{
		capacity: capacity,
		jobs:     make(map[string]*GangJob),
		signer:   signer,
		now:      func() time.Time { return time.Now().UTC() },
	}, nil
}

// SetClock overrides the scheduler clock (test-only determinism helper). Safe to call before use.
func (s *GangScheduler) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now != nil {
		s.now = now
	}
}

// Available returns the currently unreserved capacity (capacity - allocated).
func (s *GangScheduler) Available() ClusterCapacity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ClusterCapacity{
		GPUs:     s.capacity.GPUs - s.allocated.GPUs,
		CPUCores: s.capacity.CPUCores - s.allocated.CPUCores,
		MemoryGB: s.capacity.MemoryGB - s.allocated.MemoryGB,
	}
}

// Submit validates a spec, creates a GangJob in Pending, and issues the first signed receipt (""→pending).
// It is the sub-100ms hot path: pure in-memory work plus one Ed25519 signature. Returns a defensive copy.
func (s *GangScheduler) Submit(spec GangJobSpec) (*GangJob, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("training: generate gang id: %w", err)
	}
	jobID := "gang-" + hex.EncodeToString(idBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	receipt, err := s.signLocked(jobID, "", GangPending, spec.Replicas, "submitted", now)
	if err != nil {
		return nil, err
	}
	job := &GangJob{
		ID:        jobID,
		Spec:      spec,
		State:     GangPending,
		CreatedAt: now,
		Events:    []GangEvent{{Timestamp: now, From: "", To: GangPending, Reason: "submitted", Receipt: receipt}},
	}
	s.jobs[jobID] = job
	return job.clone(), nil
}

// TryAdmit computes an all-or-nothing admission decision WITHOUT mutating scheduler state. It is the pure
// gang-scheduling kernel benchmarked as "admission decisions/sec". It reports whether requiredMembers replicas
// fit into the currently-available capacity and, if not, the per-dimension shortfall.
func (s *GangScheduler) TryAdmit(spec GangJobSpec) AdmissionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decideLocked(spec)
}

// decideLocked is the lock-held core of admission. Caller must hold s.mu.
func (s *GangScheduler) decideLocked(spec GangJobSpec) AdmissionResult {
	members := spec.requiredMembers()
	req := spec.totalRequest(members)
	freeGPU := s.capacity.GPUs - s.allocated.GPUs
	freeCPU := s.capacity.CPUCores - s.allocated.CPUCores
	freeMem := s.capacity.MemoryGB - s.allocated.MemoryGB

	shortfall := ResourceRequest{}
	if req.GPUs > freeGPU {
		shortfall.GPUs = req.GPUs - freeGPU
	}
	if req.CPUCores > freeCPU {
		shortfall.CPUCores = req.CPUCores - freeCPU
	}
	if req.MemoryGB > freeMem {
		shortfall.MemoryGB = req.MemoryGB - freeMem
	}
	if shortfall.GPUs == 0 && shortfall.CPUCores == 0 && shortfall.MemoryGB == 0 {
		return AdmissionResult{Admitted: true, Members: members}
	}
	return AdmissionResult{
		Admitted:  false,
		Members:   members,
		Reason:    fmt.Sprintf("insufficient capacity for %d replicas (need gpu=%d cpu=%d mem=%dGB)", members, req.GPUs, req.CPUCores, req.MemoryGB),
		Shortfall: shortfall,
	}
}

// Admit runs all-or-nothing admission for a Pending job. On success it reserves the gang's resources, moves
// the job Pending→GangReady, and issues a signed receipt. On capacity shortfall it leaves the job Pending and
// returns a non-admitted AdmissionResult (a legitimate decision, not an error). An error is returned only for
// unknown jobs or illegal state.
func (s *GangScheduler) Admit(jobID string) (AdmissionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return AdmissionResult{}, ErrGangNotFound
	}
	if job.State != GangPending {
		return AdmissionResult{}, fmt.Errorf("training: cannot admit gang %q in state %q (want %q)", jobID, job.State, GangPending)
	}

	decision := s.decideLocked(job.Spec)
	if !decision.Admitted {
		return decision, nil
	}

	req := job.Spec.totalRequest(decision.Members)
	s.allocated.GPUs += req.GPUs
	s.allocated.CPUCores += req.CPUCores
	s.allocated.MemoryGB += req.MemoryGB

	now := s.now()
	job.Reserved = true
	job.AdmittedMembers = decision.Members
	job.ReadyAt = &now
	if err := s.transitionLocked(job, GangReady, fmt.Sprintf("gang admitted: %d replicas reserved", decision.Members), now); err != nil {
		// Roll back the reservation if the (should-be-legal) transition ever fails.
		s.allocated.GPUs -= req.GPUs
		s.allocated.CPUCores -= req.CPUCores
		s.allocated.MemoryGB -= req.MemoryGB
		job.Reserved = false
		job.ReadyAt = nil
		return AdmissionResult{}, err
	}
	return decision, nil
}

// Start moves an admitted gang GangReady→Running (every replica launched).
func (s *GangScheduler) Start(jobID string) error {
	return s.mutate(jobID, GangRunning, "all replicas launched", func(job *GangJob, now time.Time) {
		job.StartedAt = &now
	})
}

// Succeed moves a running gang Running→Succeeded and releases its reservation.
func (s *GangScheduler) Succeed(jobID string) error {
	return s.mutate(jobID, GangSucceeded, "gang completed", func(job *GangJob, now time.Time) {
		job.EndedAt = &now
		s.releaseLocked(job)
	})
}

// Fail moves a gang to Failed from any non-terminal state and releases any reservation it held. This covers
// admission abandonment (Pending→Failed), pre-launch abort (GangReady→Failed), and execution failure
// (Running→Failed) — the exhaustive set of failure edges in the FSM.
func (s *GangScheduler) Fail(jobID, reason string) error {
	if reason == "" {
		reason = "gang failed"
	}
	return s.mutate(jobID, GangFailed, reason, func(job *GangJob, now time.Time) {
		job.EndedAt = &now
		s.releaseLocked(job)
	})
}

// Get returns a defensive copy of a gang job.
func (s *GangScheduler) Get(jobID string) (*GangJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil, ErrGangNotFound
	}
	return job.clone(), nil
}

// List returns all gang jobs sorted by priority (desc) then creation time (asc) — the order a real gang
// scheduler would consider its queue.
func (s *GangScheduler) List() []GangJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GangJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j.clone())
	}
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].Spec.Priority != out[k].Spec.Priority {
			return out[i].Spec.Priority > out[k].Spec.Priority
		}
		return out[i].CreatedAt.Before(out[k].CreatedAt)
	})
	return out
}

// mutate applies a legal FSM transition to `to` under lock, running `apply` for state-specific side effects
// (timestamps, resource release) before the receipt is signed so the receipt reflects the committed state.
func (s *GangScheduler) mutate(jobID string, to GangState, reason string, apply func(job *GangJob, now time.Time)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return ErrGangNotFound
	}
	now := s.now()
	if apply != nil {
		// Pre-validate the edge before mutating side effects so a rejected transition leaves no partial state.
		if allowed, known := canGangTransition(job.State, to); !known {
			return fmt.Errorf("training: gang %q in unknown state %q", jobID, job.State)
		} else if !allowed {
			return fmt.Errorf("training: illegal gang transition %q→%q for job %q (allowed: %v)", job.State, to, jobID, gangTransitions[job.State])
		}
		apply(job, now)
	}
	return s.transitionLocked(job, to, reason, now)
}

// transitionLocked validates from→to against the exhaustive FSM, appends a signed event, and updates state.
// Caller must hold s.mu. It is safe to call after side effects because it re-checks the edge.
func (s *GangScheduler) transitionLocked(job *GangJob, to GangState, reason string, now time.Time) error {
	from := job.State
	allowed, known := canGangTransition(from, to)
	if !known {
		return fmt.Errorf("training: gang %q in unknown state %q", job.ID, from)
	}
	if !allowed {
		return fmt.Errorf("training: illegal gang transition %q→%q for job %q (allowed: %v)", from, to, job.ID, gangTransitions[from])
	}
	receipt, err := s.signLocked(job.ID, from, to, job.Spec.Replicas, reason, now)
	if err != nil {
		return err
	}
	job.State = to
	job.Events = append(job.Events, GangEvent{Timestamp: now, From: from, To: to, Reason: reason, Receipt: receipt})
	return nil
}

// releaseLocked returns a gang's reserved resources to the pool exactly once. Caller must hold s.mu.
func (s *GangScheduler) releaseLocked(job *GangJob) {
	if !job.Reserved {
		return
	}
	req := job.Spec.totalRequest(job.AdmittedMembers)
	s.allocated.GPUs -= req.GPUs
	s.allocated.CPUCores -= req.CPUCores
	s.allocated.MemoryGB -= req.MemoryGB
	job.Reserved = false
}

// signLocked issues the next sequenced, signed receipt. Caller must hold s.mu (it advances s.seq).
func (s *GangScheduler) signLocked(jobID string, from, to GangState, replicas int, reason string, now time.Time) (LifecycleReceipt, error) {
	s.seq++
	receipt, err := s.signer.sign(jobID, s.seq, from, to, replicas, reason, now)
	if err != nil {
		s.seq-- // keep the sequence gap-free if signing fails
		return LifecycleReceipt{}, err
	}
	return receipt, nil
}
