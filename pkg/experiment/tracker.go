// Package experiment implements Module 19 — the Experiment Tracking System, which
// completes the MLOps loop alongside Module 13 (model registry), Module 14 (training
// orchestrator), and Module 20 (performance monitor):
//
//	register → train → monitor → experiment compare → pick the winner to deploy.
//
// An experiment is a named hypothesis ("cifar-lr-sweep") carrying hyperparameters
// (lr, batch, epochs), a stream of logged metrics (accuracy, loss — appended as
// history, latest value exposed as a map), and a strict two-terminal lifecycle:
//
//	running → completed | failed
//
// Every operation writes a real signed attestation via pkg/evidence.Ledger and
// persists the experiment to <root>/experiments/<exp-id>.json atomically. IDs are
// crypto/rand hex in the same style as the training orchestrator's job-<hex>
// ("exp-<16 hex chars>"). Duplicate names are allowed — identity is the unique ID.
//
// Compare() is honest math, computed from the persisted records:
//   - HyperparamDiff lists only keys whose values differ between A and B
//     (keys present on one side only count as differing; the missing side is "").
//   - MetricCompare is the union of both metric sets; a missing metric reads as 0
//     and the CLI annotates it "missing".
//   - MetricDeltaPct = (B-A)/|A|*100 with a +Inf guard when A is 0 (and B is not).
//
// Lock-in thesis: the accumulated per-experiment receipts — hyperparams, metric
// curves, completion receipts linking to model versions — form the team's
// experimental memory. Walking away means losing the reproducibility trail that
// already explains every deployed model's origin.
//
// Storage layout:
//
//	<root>/experiments/<exp-id>.json   one experiment record with full metric history
//
// Pass a nil ledger to skip attestation (all other behavior unchanged).
package experiment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// Status defines the valid lifecycle states for an experiment.
type Status string

const (
	// StatusRunning indicates the experiment is active and accepts metric logs.
	StatusRunning Status = "running"
	// StatusCompleted indicates the experiment finished successfully (optionally linked to a model version).
	StatusCompleted Status = "completed"
	// StatusFailed indicates the experiment terminated with a failure reason.
	StatusFailed Status = "failed"
)

// validTransitions defines the legal state machine: running → completed | failed.
// completed and failed are terminal; LogMetric is rejected in both.
var validTransitions = map[Status][]Status{
	StatusRunning:   {StatusCompleted, StatusFailed},
	StatusCompleted: {},
	StatusFailed:    {},
}

// Experiment is one tracked ML experiment: hyperparameters, metric stream, and lifecycle.
type Experiment struct {
	ID              string             `json:"id"`                         // unique id "exp-<hex16>"
	Name            string             `json:"name"`                       // human-readable name (duplicates allowed)
	Hyperparams     map[string]string  `json:"hyperparams,omitempty"`      // e.g. {"lr":"0.001","batch":"32"}
	Metrics         map[string]float64 `json:"metrics,omitempty"`          // latest value per metric (LogMetric overwrites)
	MetricHistory   []MetricEntry      `json:"metric_history,omitempty"`   // full append-only (name, value, ts) history
	Status          Status             `json:"status"`                     // running | completed | failed
	TrainingJobRef  string             `json:"training_job_ref,omitempty"` // optional link to a training job (Module 14)
	ModelVersionRef string             `json:"model_version_ref,omitempty"`// optional "resnet50:1.1.0" filled at Complete
	FailReason      string             `json:"fail_reason,omitempty"`      // recorded when Status=failed
	CreatedAt       time.Time          `json:"created_at"`                 // UTC timestamp when started
	CompletedAt     time.Time          `json:"completed_at,omitempty"`     // zero until completed/failed
}

// MetricEntry is one point in an experiment's metric history.
type MetricEntry struct {
	Name  string    `json:"name"`
	Value float64   `json:"value"`
	At    time.Time `json:"at"`
}

// CompareResult is the honest head-to-head diff between two experiments.
type CompareResult struct {
	A, B           *Experiment
	HyperparamDiff map[string][2]string  // only differing keys: key → [aValue, bValue] (missing side "")
	MetricCompare  map[string][2]float64 // union of metrics: key → [a, b] (missing side 0, annotated by callers)
	MetricDeltaPct map[string]float64    // (b-a)/|a|*100; +Inf when a==0 && b!=0; 0 when both 0
}

// StartInput specifies the parameters for starting a new experiment.
type StartInput struct {
	Name           string            // required
	Hyperparams    map[string]string // optional, e.g. {"lr": "0.001", "batch": "32"}
	TrainingJobRef string            // optional link to a training job id (Module 14)
	Actor          string            // defaults to "cafctl-experiment"
}

// Tracker manages the experiment lifecycle. All mutations persist to disk and
// (when a ledger is wired) write signed attestations.
type Tracker interface {
	// Start creates a new experiment in 'running' status and records an attestation.
	// Duplicate names are allowed; the unique ID is the identity.
	Start(ctx context.Context, in StartInput) (*Experiment, error)
	// LogMetric appends (name, value, now) to the history and overwrites the latest-value
	// map. Only legal while Status==running; completed/failed experiments reject it.
	LogMetric(ctx context.Context, expID, name string, value float64) error
	// Complete transitions running → completed, optionally linking a model version ref.
	Complete(ctx context.Context, expID, modelVersion string) error
	// Fail transitions running → failed with a reason.
	Fail(ctx context.Context, expID, reason string) error
	// Get retrieves one experiment by ID.
	Get(ctx context.Context, expID string) (*Experiment, error)
	// List returns all experiments sorted by CreatedAt descending (newest first).
	List(ctx context.Context) []Experiment
	// Compare computes the honest head-to-head diff of two experiments.
	Compare(ctx context.Context, idA, idB string) (*CompareResult, error)
}

// Compile-time proof that FSTracker satisfies Tracker.
var _ Tracker = (*FSTracker)(nil)

// FSTracker is the filesystem-backed Tracker: one JSON file per experiment under
// <dir>/experiments, with a real evidence ledger for attestations.
type FSTracker struct {
	root   string
	ledger *evidence.Ledger

	mu     sync.Mutex // serializes mutations (create/log/complete/fail)
	lastMu sync.Mutex // guards last
	last   *evidence.Evidence
}

// NewFSTracker opens (and creates, if needed) a tracker rooted at dir. Experiment
// records live in <dir>/experiments/<exp-id>.json. A nil ledger disables
// attestation (all other behavior unchanged).
func NewFSTracker(dir string, ledger *evidence.Ledger) (*FSTracker, error) {
	if dir == "" {
		return nil, errors.New("experiment: tracker root path is required")
	}
	expDir := filepath.Join(dir, "experiments")
	if err := os.MkdirAll(expDir, 0o755); err != nil {
		return nil, fmt.Errorf("experiment: create tracker root: %w", err)
	}
	return &FSTracker{root: expDir, ledger: ledger}, nil
}

// Root returns the tracker root directory (read-only accessor).
func (t *FSTracker) Root() string { return t.root }

// LastAttestation returns the receipt from the most recent attested operation, or
// nil when none was written (nil ledger or no operations yet). This is a genuine,
// signed ledger receipt — never a synthesized one.
func (t *FSTracker) LastAttestation() *evidence.Evidence {
	t.lastMu.Lock()
	defer t.lastMu.Unlock()
	return t.last
}

// Start implements Tracker: creates a running experiment, persists it, and attests.
func (t *FSTracker) Start(ctx context.Context, in StartInput) (*Experiment, error) {
	if in.Name == "" {
		return nil, errors.New("experiment: name is required")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("experiment: generate random bytes: %w", err)
	}
	expID := fmt.Sprintf("exp-%s", hex.EncodeToString(idBytes)[:16])

	actor := in.Actor
	if actor == "" {
		actor = "cafctl-experiment"
	}
	createdAt := time.Now().UTC()

	exp := &Experiment{
		ID:             expID,
		Name:           in.Name,
		Hyperparams:    in.Hyperparams,
		Metrics:        map[string]float64{},
		MetricHistory:  []MetricEntry{},
		Status:         StatusRunning,
		TrainingJobRef: in.TrainingJobRef,
		CreatedAt:      createdAt,
	}

	expFile, err := safeJoin(t.root, expID+".json")
	if err != nil {
		return nil, fmt.Errorf("experiment: experiment file path: %w", err)
	}
	if _, statErr := os.Stat(expFile); statErr == nil {
		return nil, fmt.Errorf("experiment: experiment %q already exists", expID)
	}
	if err := writeJSONAtomic(expFile, exp); err != nil {
		return nil, fmt.Errorf("experiment: persist experiment %q: %w", expID, err)
	}

	if err := t.attest(ctx, "experiment.start", exp.ID, actor,
		map[string]any{"name": in.Name, "hyperparams": in.Hyperparams, "training_job_ref": in.TrainingJobRef},
		map[string]any{"experiment_id": expID, "status": string(StatusRunning)},
		map[string]any{"created_at": createdAt}); err != nil {
		return nil, err
	}
	return exp, nil
}

// LogMetric implements Tracker: appends to the history and overwrites the latest
// value. Only legal while running — completed/failed experiments reject metrics.
func (t *FSTracker) LogMetric(ctx context.Context, expID, name string, value float64) error {
	if name == "" {
		return errors.New("experiment: metric name is required")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	exp, err := t.load(expID)
	if err != nil {
		return err
	}
	if exp.Status != StatusRunning {
		return fmt.Errorf("experiment: cannot log metric %q on %q: status is %q, expected running (terminal experiments are immutable)", name, expID, exp.Status)
	}

	now := time.Now().UTC()
	if exp.Metrics == nil {
		exp.Metrics = map[string]float64{}
	}
	exp.Metrics[name] = value // latest value overwrites…
	exp.MetricHistory = append(exp.MetricHistory, MetricEntry{Name: name, Value: value, At: now}) // …history appends

	file, err := safeJoin(t.root, expID+".json")
	if err != nil {
		return err
	}
	if err := writeJSONAtomic(file, exp); err != nil {
		return fmt.Errorf("experiment: persist experiment %q after metric log: %w", expID, err)
	}

	return t.attest(ctx, "experiment.metric", expID, "cafctl-experiment",
		map[string]any{"metric": name, "value": value},
		map[string]any{"experiment_id": expID, "status": string(StatusRunning), "points_logged": len(exp.MetricHistory)},
		map[string]any{"logged_at": now, "latest": value})
}

// Complete implements Tracker: running → completed, optionally linking a model version.
func (t *FSTracker) Complete(ctx context.Context, expID, modelVersion string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	exp, err := t.load(expID)
	if err != nil {
		return err
	}
	if exp.Status != StatusRunning {
		return fmt.Errorf("experiment: cannot complete %q: status is %q, expected running", expID, exp.Status)
	}

	now := time.Now().UTC()
	exp.Status = StatusCompleted
	exp.ModelVersionRef = modelVersion // may be empty
	exp.CompletedAt = now

	file, err := safeJoin(t.root, expID+".json")
	if err != nil {
		return err
	}
	if err := writeJSONAtomic(file, exp); err != nil {
		return fmt.Errorf("experiment: persist experiment %q after complete: %w", expID, err)
	}

	return t.attest(ctx, "experiment.complete", expID, "cafctl-experiment",
		map[string]any{"from": string(StatusRunning), "to": string(StatusCompleted), "model_version": modelVersion},
		map[string]any{"experiment_id": expID, "status": string(StatusCompleted)},
		map[string]any{"metrics": exp.Metrics, "model_version_ref": modelVersion, "completed_at": now})
}

// Fail implements Tracker: running → failed with a recorded reason.
func (t *FSTracker) Fail(ctx context.Context, expID, reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	exp, err := t.load(expID)
	if err != nil {
		return err
	}
	if exp.Status != StatusRunning {
		return fmt.Errorf("experiment: cannot fail %q: status is %q, expected running", expID, exp.Status)
	}
	if reason == "" {
		reason = "unspecified"
	}

	now := time.Now().UTC()
	exp.Status = StatusFailed
	exp.FailReason = reason
	exp.CompletedAt = now

	file, err := safeJoin(t.root, expID+".json")
	if err != nil {
		return err
	}
	if err := writeJSONAtomic(file, exp); err != nil {
		return fmt.Errorf("experiment: persist experiment %q after fail: %w", expID, err)
	}

	return t.attest(ctx, "experiment.fail", expID, "cafctl-experiment",
		map[string]any{"from": string(StatusRunning), "to": string(StatusFailed), "reason": reason},
		map[string]any{"experiment_id": expID, "status": string(StatusFailed)},
		map[string]any{"fail_reason": reason, "failed_at": now})
}

// Get implements Tracker: load one experiment by ID. The ID is treated as
// untrusted input — the resolved path is verified to stay inside the tracker root
// (defense in depth against path traversal such as "../../etc/passwd").
func (t *FSTracker) Get(ctx context.Context, expID string) (*Experiment, error) {
	if expID == "" {
		return nil, errors.New("experiment: experiment ID is required")
	}
	file, err := safeJoin(t.root, expID+".json")
	if err != nil {
		return nil, fmt.Errorf("experiment: invalid experiment ID %q: %w", expID, err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("experiment: experiment %q not found", expID)
		}
		return nil, fmt.Errorf("experiment: read experiment %q: %w", expID, err)
	}
	var exp Experiment
	if err := json.Unmarshal(data, &exp); err != nil {
		return nil, fmt.Errorf("experiment: parse experiment %q: %w", expID, err)
	}
	return &exp, nil
}

// List implements Tracker: all experiments sorted newest-first by CreatedAt
// (ties broken by ID descending, mirroring the training orchestrator).
func (t *FSTracker) List(ctx context.Context) []Experiment {
	entries, err := os.ReadDir(t.root)
	if err != nil {
		return []Experiment{}
	}
	exps := []Experiment{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(t.root, e.Name()))
		if rerr != nil {
			continue
		}
		var exp Experiment
		if err := json.Unmarshal(data, &exp); err != nil {
			continue
		}
		exps = append(exps, exp)
	}
	sort.SliceStable(exps, func(i, j int) bool {
		if !exps[i].CreatedAt.Equal(exps[j].CreatedAt) {
			return exps[i].CreatedAt.After(exps[j].CreatedAt)
		}
		return exps[i].ID > exps[j].ID
	})
	return exps
}

// Compare implements Tracker: honest head-to-head math, computed from the persisted records.
//   - HyperparamDiff: only keys with differing values (one-sided keys included, missing side "")
//   - MetricCompare: union of both metric sets, missing side 0
//   - MetricDeltaPct: (b-a)/|a|*100, +Inf when a==0 && b!=0, 0 when both are 0
func (t *FSTracker) Compare(ctx context.Context, idA, idB string) (*CompareResult, error) {
	a, err := t.Get(ctx, idA)
	if err != nil {
		return nil, err
	}
	b, err := t.Get(ctx, idB)
	if err != nil {
		return nil, err
	}

	res := &CompareResult{
		A:              a,
		B:              b,
		HyperparamDiff: map[string][2]string{},
		MetricCompare:  map[string][2]float64{},
		MetricDeltaPct: map[string]float64{},
	}

	// Hyperparameters: union of keys; equal key+value on both sides is NOT listed.
	for _, k := range unionKeys(a.Hyperparams, b.Hyperparams) {
		va, okA := a.Hyperparams[k]
		vb, okB := b.Hyperparams[k]
		if okA && okB && va == vb {
			continue // same key, same value — not a difference
		}
		res.HyperparamDiff[k] = [2]string{va, vb} // missing side stays ""
	}

	// Metrics: union of keys; missing side reads 0 (callers annotate "missing").
	for _, k := range unionKeys(a.Metrics, b.Metrics) {
		va := a.Metrics[k] // absent key → zero value
		vb := b.Metrics[k]
		res.MetricCompare[k] = [2]float64{va, vb}
		res.MetricDeltaPct[k] = deltaPct(va, vb)
	}
	return res, nil
}

// ============================================================================
// Internal helpers
// ============================================================================

// load reads one experiment without the state-machine guard (callers under mu apply their own).
func (t *FSTracker) load(expID string) (*Experiment, error) {
	return t.Get(context.Background(), expID)
}

// deltaPct computes (b-a)/|a|*100 with the +Inf guard: when a==0 the relative
// change is undefined; we return +Inf if b!=0 and 0 if both are 0.
func deltaPct(a, b float64) float64 {
	if a == 0 {
		if b == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return (b - a) / math.Abs(a) * 100
}

// unionKeys returns the sorted union of both maps' keys (deterministic iteration).
func unionKeys[M map[string]V, V any](a, b M) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// attest writes a signed receipt through the evidence ledger (real signing and
// hash chaining; skipped entirely when no ledger is wired).
func (t *FSTracker) attest(ctx context.Context, action, subject, actor string, input, output, payload map[string]any) error {
	if t.ledger == nil {
		return nil
	}
	ev, err := t.ledger.Record(ctx, evidence.RecordInput{
		Actor:   actor,
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("experiment: attestation %s failed: %w", action, err)
	}
	t.lastMu.Lock()
	t.last = ev
	t.lastMu.Unlock()
	return nil
}

// safeJoin joins base with segments and verifies the resolved path stays inside
// base — defense in depth against path traversal (same pattern as pkg/training).
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
		return "", fmt.Errorf("path escapes tracker root: %q", p)
	}
	return p, nil
}

// writeJSONAtomic writes v as indented JSON atomically (tmp file + rename).
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
