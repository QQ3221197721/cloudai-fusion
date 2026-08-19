// Package training implements Module 14 — the Training Job Orchestrator, which forms the developer journey
// closure with Module 13 Model Registry. Every job transition runs through a real state machine
// (queued → scheduled → running → succeeded/failed/cancelled), writes a signed attestation via
// pkg/evidence.Ledger, and persists jobs to disk as JSON files in .caf/training/<job-id>.json.
//
// Execution is honest-simulated: the code documents clearly that K8s/GPU submission is a future integration point;
// today it tracks the lifecycle of hypothetical workloads and their resulting model versions. This means:
//   - No real container/image execution happens here; we simulate scheduling decisions.
//   - When Complete() is called with artifactPath+registry, it registers a new model version with lineage.
//   - Attestations are genuine cryptographic receipts (in-memory store + ephemeral signer when no backend).
//
// Lock-in thesis: after weeks/months of training jobs producing signed receipts and hash-chained attestations,
// every team owns an immutable history of "we trained X model on Y dataset using Z GPU resources" — migrating
// away means abandoning that provenance chain auditors already trust. The model registry deepens this lock-in
// because each training completion can register a new version with parent lineage, forming a recursive DAG.
//
// Storage layout:
//
//	<root>/training/<job-id>.json     job record for one training task
//
// Every Create/Transition writes a real attestation through pkg/evidence.Ledger; pass nil ledger to skip.
package training

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
)

// JobStatus defines the valid lifecycle states for a training job.
type JobStatus string

const (
	// StatusQueued indicates the job has been created and submitted but not yet scheduled for execution.
	StatusQueued JobStatus = "queued"
	// StatusScheduled indicates resources have been allocated and the job is ready to start.
	StatusScheduled JobStatus = "scheduled"
	// StatusRunning indicates the job is actively executing (simulated; real K8s/GPU submission is a future integration point).
	StatusRunning JobStatus = "running"
	// StatusSucceeded indicates the job completed successfully.
	StatusSucceeded JobStatus = "succeeded"
	// StatusFailed indicates the job failed during execution.
	StatusFailed JobStatus = "failed"
	// StatusCancelled indicates the job was explicitly cancelled by a user/operator.
	StatusCancelled JobStatus = "cancelled"
)

// validTransitions defines the legal state machine transitions for a training job.
// Keys are source states; values are lists of allowed target states. Empty lists mean terminal states.
var validTransitions = map[JobStatus][]JobStatus{
	StatusQueued:    {StatusScheduled, StatusCancelled},
	StatusScheduled: {StatusRunning, StatusCancelled},
	StatusRunning:   {StatusSucceeded, StatusFailed, StatusCancelled},
	StatusSucceeded: {},
	StatusFailed:    {},
	StatusCancelled: {},
}

// jobIDVerb maps a destination status to the corresponding attestation action verb.
var jobIDVerb = map[JobStatus]string{
	StatusScheduled: "schedule",
	StatusRunning:   "start",
	StatusSucceeded: "complete",
	StatusFailed:    "fail",
	StatusCancelled: "cancel",
}

// TrainingJob represents a single ML/DL training task. It tracks lifecycle, resource requirements, and provenance.
// NOTE: All fields below are documented as simulated execution in production docs. Real K8s/GPU submission is a future integration point.
type TrainingJob struct {
	ID          string        `json:"id"`                      // unique identifier (e.g., "job-a1b2c3d4")
	Name        string        `json:"name"`                    // human-readable job name
	Image       string        `json:"image"`                   // container image (e.g., "pytorch:2.0")
	GPUCount    int           `json:"gpu_count"`               // number of GPUs requested
	MemoryGB    int           `json:"memory_gb"`               // memory requirement in GB
	BaseModel   string        `json:"base_model,omitempty"`    // fine-tune parent reference ("resnet50:1.0.0"; empty for from-scratch training)
	DatasetRef  string        `json:"dataset_ref"`             // dataset SHA-256 or path reference
	Command     string        `json:"command,omitempty"`       // training command/script (e.g., "python train.py")
	Status      JobStatus     `json:"status"`                  // current state
	Events      []JobEvent    `json:"events"`                  // chronological event history (state transitions)
	CreatedAt   time.Time     `json:"created_at"`              // UTC timestamp when job was created
	ScheduledAt *time.Time    `json:"scheduled_at,omitempty"`  // when queued→scheduled occurred
	StartedAt   *time.Time    `json:"started_at,omitempty"`    // when scheduled→running occurred
	CompletedAt *time.Time    `json:"completed_at,omitempty"`  // when running→terminal occurred
	Actor       string        `json:"actor,omitempty"`         // who last acted on this job (default: cafctl)
	Hyperparams map[string]string `json:"hyperparams,omitempty"`  // optional training hyperparameters (e.g., {"lr": "0.001"})
	Tags        map[string]string `json:"tags,omitempty"`        // optional tags for organization
}

// JobEvent records a single state transition within a job's lifecycle.
type JobEvent struct {
	Timestamp time.Time `json:"timestamp"`
	From      JobStatus `json:"from"`
	To        JobStatus `json:"to"`
	Reason    string    `json:"reason,omitempty"` // optional reason/context for the transition
}

// SubmitInput specifies all required parameters for creating a new training job.
type SubmitInput struct {
	Name        string
	Image       string
	GPUCount    int
	MemoryGB    int
	BaseModel   string            // optional fine-tune parent; "" means from-scratch training
	DatasetRef  string
	Command     string            // command/script executed in container
	Actor       string            // defaults to "cafctl-train" if empty
	Hyperparams map[string]string // optional hyperparameters (e.g., {"lr": "0.001"})
	Tags        map[string]string // optional tags for organization
}

// Orchestrator manages the full lifecycle of training jobs: creation, scheduling, execution simulation,
// completion/failure handling, cancellation, and persistence. It integrates with modelregistry.Registry
// to automatically register new model versions upon successful training completion with artifacts.
type Orchestrator interface {
	// Submit creates a new training job in 'queued' status and records an attestation.
	Submit(ctx context.Context, input SubmitInput) (*TrainingJob, error)
	// Schedule transitions a job from 'queued' to 'scheduled', indicating resource allocation.
	Schedule(ctx context.Context, jobID string) error
	// Start transitions a job from 'scheduled' to 'running', beginning simulated execution.
	Start(ctx context.Context, jobID string) error
	// Complete transitions a job from 'running' to 'succeeded'. If artifactPath is non-empty and reg is non-nil,
	// it calculates a new model version number, registers the artifact via reg.Register, and includes the result
	// in the final attestation payload. The calculated version increments MINOR (e.g., base resnet50:1.0.0 → 1.1.0);
	// if that version exists, MINOR bumps again until a free slot is found.
	Complete(ctx context.Context, jobID, reason, artifactPath string, reg modelregistry.Registry) error
	// Fail transitions a job from 'running' to 'failed' with an optional reason.
	Fail(ctx context.Context, jobID, reason string) error
	// Cancel transitions a job to 'cancelled' if currently in a non-terminal state (queued/scheduled/running).
	Cancel(ctx context.Context, jobID, reason string) error
	// Get retrieves a single job by ID; returns nil if not found.
	Get(ctx context.Context, jobID string) (*TrainingJob, error)
	// List returns all jobs sorted newest-first by CreatedAt.
	List(ctx context.Context) ([]TrainingJob, error)
	// LastAttestation returns the most recent attestation evidence from the last completed operation.
	LastAttestation() *evidence.Evidence
}

// Compile-time proof that FSOrchestrator satisfies Orchestrator.
var _ Orchestrator = (*FSOrchestrator)(nil)

// FSOrchestrator is the filesystem-backed implementation of the Orchestrator interface. It stores job records
// as individual JSON files and uses pkg/evidence.Ledger for cryptographic attestation of every state change.
type FSOrchestrator struct {
	root   string
	ledger *evidence.Ledger

	mu      sync.Mutex // serializes Submit (ID generation + creation)
	lastMu  sync.Mutex // guards last & lastReg
	last    *evidence.Evidence          // most recent receipt from the ledger
	lastReg *modelregistry.ModelArtifact // model version registered by the most recent Complete
}

// NewFSOrchestrator opens (and creates, if needed) an orchestrator rooted at dir. Jobs will be persisted
// to <root>/training/<job-id>.json. A nil ledger disables attestation (all other behavior unchanged).
func NewFSOrchestrator(dir string, ledger *evidence.Ledger) (*FSOrchestrator, error) {
	if dir == "" {
		return nil, errors.New("training: orchestrator root path is required")
	}
	trainingDir := filepath.Join(dir, "training")
	if err := os.MkdirAll(trainingDir, 0o755); err != nil {
		return nil, fmt.Errorf("training: create orchestrator root: %w", err)
	}
	return &FSOrchestrator{root: trainingDir, ledger: ledger}, nil
}

// Root returns the orchestrator root directory (read-only accessor).
func (o *FSOrchestrator) Root() string { return o.root }

// LastAttestation returns the receipt from the most recent operation (Submit/Schedule/Start/Complete/Fail/Cancel),
// or nil when none was written (nil ledger or no operations yet). This is a genuine, signed ledger receipt —
// never a synthesized one.
func (o *FSOrchestrator) LastAttestation() *evidence.Evidence {
	o.lastMu.Lock()
	defer o.lastMu.Unlock()
	return o.last
}

// LastRegisteredArtifact returns the model version registered by the most recent Complete(ctx, ..., reg)
// call, or nil when the last Complete did not register one. Lets the CLI report the lineage
// outcome without re-querying the registry.
func (o *FSOrchestrator) LastRegisteredArtifact() *modelregistry.ModelArtifact {
	o.lastMu.Lock()
	defer o.lastMu.Unlock()
	return o.lastReg
}

// Submit implements Orchestrator: creates a new training job in 'queued' status, persists it,
// and writes an attestation.
func (o *FSOrchestrator) Submit(ctx context.Context, in SubmitInput) (*TrainingJob, error) {
	if in.Name == "" {
		return nil, errors.New("training: job name is required")
	}
	if in.Image == "" {
		return nil, errors.New("training: container image is required")
	}
	if in.GPUCount <= 0 {
		return nil, errors.New("training: GPU count must be positive")
	}
	if in.MemoryGB <= 0 {
		return nil, errors.New("training: memory must be positive")
	}
	if in.DatasetRef == "" {
		return nil, errors.New("training: dataset reference is required")
	}
	// BaseModel must be "name:version" when present ("" means from-scratch training).
	if in.BaseModel != "" && !strings.Contains(in.BaseModel, ":") {
		return nil, fmt.Errorf("training: base model %q must be in \"name:version\" format or empty", in.BaseModel)
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	// Generate a deterministic-looking unique ID (hex-encoded random bytes).
	idBytes := make([]byte, 8)
	_, err := rand.Read(idBytes)
	if err != nil {
		return nil, fmt.Errorf("training: generate random bytes: %w", err)
	}
	jobID := fmt.Sprintf("job-%s", hex.EncodeToString(idBytes)[:16])

	createdAt := time.Now().UTC()
	actor := in.Actor
	if actor == "" {
		actor = "cafctl-train"
	}

	job := &TrainingJob{
		ID:          jobID,
		Name:        in.Name,
		Image:       in.Image,
		GPUCount:    in.GPUCount,
		MemoryGB:    in.MemoryGB,
		BaseModel:   in.BaseModel,
		DatasetRef:  in.DatasetRef,
		Command:     in.Command,
		Status:      StatusQueued,
		Events:      []JobEvent{{Timestamp: createdAt, From: "", To: StatusQueued, Reason: "submitted"}},
		CreatedAt:   createdAt,
		Actor:       actor,
		Hyperparams: in.Hyperparams,
		Tags:        in.Tags,
	}

	jobFile, err := safeJoin(o.root, jobID+".json")
	if err != nil {
		return nil, fmt.Errorf("training: job file path: %w", err)
	}
	if _, statErr := os.Stat(jobFile); statErr == nil {
		return nil, fmt.Errorf("training: job %q already exists", jobID)
	}
	if err := writeJSONAtomic(jobFile, job); err != nil {
		return nil, fmt.Errorf("training: persist job %q: %w", jobID, err)
	}

	// Write attestation: action="train.submit".
	if err := o.attestLocked(ctx, "train.submit", job.ID, actor,
		map[string]any{"name": in.Name, "image": in.Image, "gpu_count": in.GPUCount, "memory_gb": in.MemoryGB, "dataset_ref": in.DatasetRef, "base_model": in.BaseModel},
		map[string]any{"job_id": jobID, "status": "queued"},
		map[string]any{"execution": "simulated"}); err != nil {
		return nil, err
	}

	return job, nil
}

// Schedule implements Orchestrator: transitions from queued → scheduled (resources allocated).
func (o *FSOrchestrator) Schedule(ctx context.Context, jobID string) error {
	_, err := o.transition(ctx, jobID, StatusScheduled, "resources allocated")
	return err
}

// Start implements Orchestrator: transitions from scheduled → running (execution begins).
func (o *FSOrchestrator) Start(ctx context.Context, jobID string) error {
	_, err := o.transition(ctx, jobID, StatusRunning, "execution started")
	return err
}

// Complete implements Orchestrator: transitions from running → succeeded, optionally registering a new model version.
func (o *FSOrchestrator) Complete(ctx context.Context, jobID, reason, artifactPath string, reg modelregistry.Registry) error {
	job, err := o.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != StatusRunning {
		return fmt.Errorf("training: cannot complete job %q: status is %q, expected running", jobID, job.Status)
	}

	var registeredArtifact *modelregistry.ModelArtifact
	if artifactPath != "" && reg != nil {
		// Calculate new model version based on BaseModel.
		name, version := parseBaseModel(job.BaseModel)
		if name == "" {
			name = sanitizeModelName(job.Name)
		}
		newVer, err := nextVersion(ctx, reg, name, version)
		if err != nil {
			return fmt.Errorf("training: compute next version: %w", err)
		}
		artifactInput := o.buildRegisterInput(job, artifactPath, name, newVer)
		art, err := reg.Register(ctx, artifactInput)
		if err != nil {
			return fmt.Errorf("training: register model version %s:%s: %w", name, newVer, err)
		}
		registeredArtifact = art
	}

	transReason := fmt.Sprintf("completed%s%s", orDash(reason), transAttr(registeredArtifact))
	_, err = o.transition(ctx, jobID, StatusSucceeded, transReason)
	if err != nil {
		return err
	}

	o.lastMu.Lock()
	o.lastReg = registeredArtifact
	o.lastMu.Unlock()
	return nil
}

// Fail implements Orchestrator: transitions from running → failed.
func (o *FSOrchestrator) Fail(ctx context.Context, jobID, reason string) error {
	_, err := o.transition(ctx, jobID, StatusFailed, reason)
	return err
}

// Cancel implements Orchestrator: transitions any non-terminal state to cancelled.
func (o *FSOrchestrator) Cancel(ctx context.Context, jobID, reason string) error {
	job, err := o.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if isTerminalState(job.Status) {
		return fmt.Errorf("training: cannot cancel job %q: already in terminal state %q", jobID, job.Status)
	}
	target := StatusCancelled
	validForSource, ok := validTransitions[job.Status]
	if !ok {
		return fmt.Errorf("training: unknown source state %q", job.Status)
	}
	canReachTarget := false
	for _, s := range validForSource {
		if s == target {
			canReachTarget = true
			break
		}
	}
	if !canReachTarget {
		return fmt.Errorf("training: invalid transition from %q to %q", job.Status, target)
	}
	_, err = o.transition(ctx, jobID, StatusCancelled, reason)
	return err
}

// Get implements Orchestrator: retrieve a single job by ID.
func (o *FSOrchestrator) Get(ctx context.Context, jobID string) (*TrainingJob, error) {
	if jobID == "" {
		return nil, errors.New("training: job ID is required")
	}
	file := filepath.Join(o.root, jobID+".json")
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("training: job %q not found", jobID)
		}
		return nil, fmt.Errorf("training: read job %q: %w", jobID, err)
	}
	var job TrainingJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("training: parse job %q: %w", jobID, err)
	}
	return &job, nil
}

// List implements Orchestrator: return all jobs sorted newest-first.
func (o *FSOrchestrator) List(ctx context.Context) ([]TrainingJob, error) {
	entries, err := os.ReadDir(o.root)
	if err != nil {
		if os.IsNotExist(err) {
			return []TrainingJob{}, nil
		}
		return nil, fmt.Errorf("training: list jobs: %w", err)
	}
	var jobs []TrainingJob
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var job TrainingJob
		path, jerr := safeJoin(o.root, e.Name())
		if jerr != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &job); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	sort.SliceStable(jobs, func(i, j int) bool {
		if !jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
		}
		return jobs[i].ID > jobs[j].ID
	})
	return jobs, nil
}

// ============================================================================
// Internal helpers
// ============================================================================

// transition performs a single state transition: it re-reads the job, checks legality against
// validTransitions, commits the new status, appends a JobEvent, persists the new record atomically,
// and attests through the ledger. It is a self-contained atomic operation; callers do NOT need to hold o.mu.
// Every transition is recorded in the job's event history — the audit trail IS the product.
func (o *FSOrchestrator) transition(ctx context.Context, jobID string, to JobStatus, reason string) (*TrainingJob, error) {
	job, err := o.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}

	source := job.Status
	validForSource, ok := validTransitions[source]
	if !ok {
		return nil, fmt.Errorf("training: unknown source state %q", source)
	}
	canReachTarget := false
	for _, s := range validForSource {
		if s == to {
			canReachTarget = true
			break
		}
	}
	if !canReachTarget {
		return nil, fmt.Errorf("training: invalid transition from %q to %q (allowed: %v)", source, to, validForSource)
	}

	// Commit the new status on the in-memory copy.
	job.Status = to

	now := time.Now().UTC()
	verb := jobIDVerb[to]
	action := fmt.Sprintf("train.%s", verb)

	// Update timestamps.
	if to == StatusScheduled && job.ScheduledAt == nil {
		job.ScheduledAt = &now
	} else if to == StatusRunning && job.StartedAt == nil {
		job.StartedAt = &now
	} else if isTerminalState(to) && job.CompletedAt == nil {
		job.CompletedAt = &now
	}

	// Append event to history (the audit trail is always recorded).
	event := JobEvent{Timestamp: now, From: source, To: to, Reason: reason}
	job.Events = append(job.Events, event)

	// Persist updated job.
	filename := filepath.Join(o.root, jobID+".json")
	if err := writeJSONAtomic(filename, job); err != nil {
		return nil, fmt.Errorf("training: persist job %q after %s: %w", jobID, action, err)
	}

	// Attest the transition.
	payload := map[string]any{
		"old_status": source,
		"new_status": to,
		"reason":     reason,
		"job_name":   job.Name,
		"job_image":  job.Image,
		"execution":  "simulated",
	}
	if serr := o.attestLocked(ctx, action, jobID, job.Actor, map[string]any{"from": source, "to": to, "reason": reason}, map[string]any{"job_id": jobID, "status": to}, payload); serr != nil {
		return nil, fmt.Errorf("training: attestation for %s failed: %w", action, serr)
	}

	return job, nil
}

// buildRegisterInput constructs a RegisterInput from a completed TrainingJob for integration with modelregistry.
func (o *FSOrchestrator) buildRegisterInput(job *TrainingJob, artifactPath, modelName, version string) modelregistry.RegisterInput {
	createdBy := "cafctl-train"
	if job.Actor != "" {
		createdBy = job.Actor
	}

	framework := detectFramework(job.Image)
	hyperparams := make(map[string]string)
	for k, v := range job.Hyperparams {
		hyperparams[k] = v
	}
	hyperparams["gpu_count"] = fmt.Sprintf("%d", job.GPUCount)
	hyperparams["memory_gb"] = fmt.Sprintf("%d", job.MemoryGB)
	hyperparams["command"] = job.Command
	tags := make(map[string]string)
	for k, v := range job.Tags {
		hyperparams["tag:"+k] = v
		tags[k] = v
	}

	return modelregistry.RegisterInput{
		Name:          modelName,
		Version:       version,
		ArtifactPath:  artifactPath,
		DatasetRef:    job.DatasetRef,
		CodeRef:       job.Command,
		ParentVersion: func() string { _, v := parseBaseModel(job.BaseModel); return v }(),
		Hyperparams:   hyperparams,
		Tags:          tags,
		TaskType:      "fine-tuning",
		Framework:     framework,
		Summary:       fmt.Sprintf("Training job %s completed", job.ID),
		CreatedBy:     createdBy,
	}
}

// detectFramework extracts framework hint from container image name (simulated inference).
func detectFramework(image string) string {
	lower := strings.ToLower(image)
	switch {
	case strings.Contains(lower, "pytorch"):
		return "pytorch"
	case strings.Contains(lower, "tensorflow"), strings.Contains(lower, "tf"):
		return "tensorflow"
	case strings.Contains(lower, "jax"):
		return "jax"
	default:
		return "unknown"
	}
}

// transAttr formats an optional suffix for completion reason based on whether model was registered.
func transAttr(art *modelregistry.ModelArtifact) string {
	if art == nil {
		return ""
	}
	return fmt.Sprintf(" (model %s:%s registered)", art.Name, art.Version)
}

// orDash renders empty strings as an em dash for tidy output.
func orDash(s string) string {
	if s == "" {
		return " —"
	}
	return fmt.Sprintf(" (%s)", s)
}

// parseBaseModel parses a BaseModel string like "resnet50:1.0.0" into (name, version).
// Returns ("", "") if input is empty; if no colon found, returns ("", "") with caller treating it as from-scratch.
func parseBaseModel(base string) (name, version string) {
	if base == "" {
		return "", ""
	}
	idx := strings.LastIndex(base, ":")
	if idx < 0 {
		return "", ""
	}
	return base[:idx], base[idx+1:]
}

// sanitizeModelName cleans up a job name for use as a model name.
func sanitizeModelName(name string) string {
	out := strings.ReplaceAll(strings.ToLower(name), "-", "_")
	out = strings.ReplaceAll(out, ".", "_")
	return out
}

// nextVersion computes the next available MINOR version for a given model, bumping from parentVersion.
func nextVersion(ctx context.Context, reg modelregistry.Registry, name, parentVersion string) (string, error) {
	existing, err := reg.List(ctx, name)
	if err != nil && !errors.Is(err, modelregistry.ErrNotFound) {
		return "", err
	}
	taken := make(map[string]bool, len(existing))
	for _, a := range existing {
		taken[a.Version] = true
	}

	var base string
	if parentVersion == "" {
		base = "1.0.0"
	} else {
		parts := strings.Split(parentVersion, ".")
		if len(parts) != 3 {
			return "", fmt.Errorf("invalid semver %q for parent", parentVersion)
		}
		minor, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", fmt.Errorf("invalid minor %q: %w", parts[1], err)
		}
		base = fmt.Sprintf("%s.%d.0", parts[0], minor+1)
	}

	version := base
	attempts := 0
	for taken[version] && attempts < 1000 {
		parts := strings.Split(version, ".")
		minor, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", fmt.Errorf("compute next version: %w", err)
		}
		version = fmt.Sprintf("%s.%d.0", parts[0], minor+1)
		attempts++
	}
	if attempts >= 1000 {
		return "", errors.New("nextVersion: exhausted search space for version bump")
	}
	return version, nil
}

// isTerminalState returns true if the state is SUCCEEDED, FAILED, or CANCELLED.
func isTerminalState(status JobStatus) bool {
	return status == StatusSucceeded || status == StatusFailed || status == StatusCancelled
}

// safeJoin joins base with segments and verifies the resolved path stays inside base — defense in depth against path traversal.
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
		return "", fmt.Errorf("path escapes orchestrator root: %q", p)
	}
	return p, nil
}

// writeJSONAtomic writes v as indented JSON atomically (write to tmp, then rename).
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// attestLocked writes a signed attestation receipt through the evidence ledger (real signing and
// hash chaining; the backing store depends on the injected ledger).
func (o *FSOrchestrator) attestLocked(ctx context.Context, action, subject, actor string, input, output, payload map[string]any) error {
	if o.ledger == nil {
		return nil
	}
	ev, err := o.ledger.Record(ctx, evidence.RecordInput{
		Actor:   actor,
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("training: attestation %s failed: %w", action, err)
	}
	o.lastMu.Lock()
	o.last = ev
	o.lastMu.Unlock()
	return nil
}
