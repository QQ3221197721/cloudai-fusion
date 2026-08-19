// Package mlops provides MLOps building blocks for the CloudAI Fusion
// control plane. It contains two independent capabilities:
//
//   - M19 Experiment Tracking: an in-memory (optionally file-persisted)
//     metadata store for experiments, runs, params, metrics and artifacts,
//     with Ed25519-signed run provenance for tamper-evident lineage.
//   - M20 Model Performance Monitor: statistical drift detection
//     (Population Stability Index and Kolmogorov-Smirnov) with configurable
//     SLO thresholds and a Prometheus-compatible metric exporter.
//
// The package has no external process dependencies; persistence is plain
// JSON on the local filesystem and all statistics are computed in pure Go.
package mlops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ============================================================================
// M19 Experiment Tracking — data model
// ============================================================================

// RunStatus enumerates the lifecycle states of an experiment run.
type RunStatus string

const (
	// RunRunning indicates a run that has started but not finished.
	RunRunning RunStatus = "RUNNING"
	// RunFinished indicates a run that completed successfully.
	RunFinished RunStatus = "FINISHED"
	// RunFailed indicates a run that terminated with an error.
	RunFailed RunStatus = "FAILED"
	// RunKilled indicates a run that was cancelled externally.
	RunKilled RunStatus = "KILLED"
)

// MetricPoint is a single scalar metric observation. Metrics form a time
// series keyed by name; each point records the value, the wall-clock time and
// an optional training step so learning curves can be reconstructed.
type MetricPoint struct {
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
	Step      int64     `json:"step"`
}

// Artifact references a file or object produced by a run. The store records
// metadata only; the payload is expected to live in blob/object storage.
type Artifact struct {
	Name      string    `json:"name"`
	URI       string    `json:"uri"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Run is a single execution of an experiment. Params are immutable inputs,
// Metrics are append-only time series, and Artifacts are produced outputs.
type Run struct {
	ID           string                   `json:"id"`
	ExperimentID string                   `json:"experiment_id"`
	Name         string                   `json:"name"`
	Status       RunStatus                `json:"status"`
	Params       map[string]string        `json:"params"`
	Metrics      map[string][]MetricPoint `json:"metrics"`
	Artifacts    []Artifact               `json:"artifacts"`
	Tags         map[string]string        `json:"tags"`
	StartTime    time.Time                `json:"start_time"`
	EndTime      *time.Time               `json:"end_time,omitempty"`

	// Provenance holds the Ed25519 signature over the run's canonical
	// fingerprint. It is populated by Sealer.Seal and verified by Verify.
	Provenance *Provenance `json:"provenance,omitempty"`
}

// Experiment groups related runs under a stable name.
type Experiment struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Tags      map[string]string `json:"tags"`
	CreatedAt time.Time         `json:"created_at"`
}

// ============================================================================
// Tracking store
// ============================================================================

// TrackingStore is a concurrency-safe metadata store for experiments and runs.
// It keeps everything in memory and can optionally snapshot to a JSON file for
// durability across restarts.
type TrackingStore struct {
	mu          sync.RWMutex
	experiments map[string]*Experiment
	runs        map[string]*Run
	// runsByExp indexes run IDs by experiment ID for fast listing.
	runsByExp map[string][]string
	// persistPath, when non-empty, is the file used by Save/Load.
	persistPath string
	// seq drives deterministic, monotonic ID generation.
	seq uint64
	now func() time.Time
}

// NewTrackingStore returns an in-memory store. If persistPath is non-empty the
// store can be persisted with Save and rehydrated with Load; the directory is
// created lazily on first Save.
func NewTrackingStore(persistPath string) *TrackingStore {
	return &TrackingStore{
		experiments: make(map[string]*Experiment),
		runs:        make(map[string]*Run),
		runsByExp:   make(map[string][]string),
		persistPath: persistPath,
		now:         time.Now,
	}
}

func (s *TrackingStore) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%08x-%d", prefix, s.seq, s.now().UnixNano())
}

// CreateExperiment registers a new experiment and returns it. Names are not
// required to be unique; callers that need uniqueness should check first.
func (s *TrackingStore) CreateExperiment(name string, tags map[string]string) *Experiment {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp := &Experiment{
		ID:        s.nextID("exp"),
		Name:      name,
		Tags:      cloneStringMap(tags),
		CreatedAt: s.now(),
	}
	s.experiments[exp.ID] = exp
	return exp
}

// GetExperiment returns the experiment by ID, or false if absent.
func (s *TrackingStore) GetExperiment(id string) (*Experiment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.experiments[id]
	return exp, ok
}

// StartRun creates a RUNNING run under the given experiment.
func (s *TrackingStore) StartRun(experimentID, name string, params map[string]string) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.experiments[experimentID]; !ok {
		return nil, fmt.Errorf("mlops: experiment %q not found", experimentID)
	}
	run := &Run{
		ID:           s.nextID("run"),
		ExperimentID: experimentID,
		Name:         name,
		Status:       RunRunning,
		Params:       cloneStringMap(params),
		Metrics:      make(map[string][]MetricPoint),
		Tags:         make(map[string]string),
		StartTime:    s.now(),
	}
	s.runs[run.ID] = run
	s.runsByExp[experimentID] = append(s.runsByExp[experimentID], run.ID)
	return run, nil
}

// LogParam records an immutable input parameter. Re-logging an existing key
// overwrites it, matching MLflow's last-write-wins semantics for params set
// before a run completes.
func (s *TrackingStore) LogParam(runID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("mlops: run %q not found", runID)
	}
	if run.Params == nil {
		run.Params = make(map[string]string)
	}
	run.Params[key] = value
	return nil
}

// LogMetric appends a metric observation to the run's time series.
func (s *TrackingStore) LogMetric(runID, name string, value float64, step int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("mlops: run %q not found", runID)
	}
	run.Metrics[name] = append(run.Metrics[name], MetricPoint{
		Value:     value,
		Timestamp: s.now(),
		Step:      step,
	})
	return nil
}

// LogArtifact attaches artifact metadata to the run.
func (s *TrackingStore) LogArtifact(runID string, a Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("mlops: run %q not found", runID)
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = s.now()
	}
	run.Artifacts = append(run.Artifacts, a)
	return nil
}

// SetTag sets or overwrites a run tag.
func (s *TrackingStore) SetTag(runID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("mlops: run %q not found", runID)
	}
	if run.Tags == nil {
		run.Tags = make(map[string]string)
	}
	run.Tags[key] = value
	return nil
}

// FinishRun transitions a run to a terminal state and stamps the end time.
func (s *TrackingStore) FinishRun(runID string, status RunStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("mlops: run %q not found", runID)
	}
	run.Status = status
	end := s.now()
	run.EndTime = &end
	return nil
}

// GetRun returns a deep copy of the run so callers cannot mutate store state.
func (s *TrackingStore) GetRun(runID string) (*Run, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[runID]
	if !ok {
		return nil, false
	}
	return cloneRun(run), true
}

// LatestMetric returns the most recent value for a metric on a run.
func (s *TrackingStore) LatestMetric(runID, name string) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[runID]
	if !ok {
		return 0, false
	}
	pts := run.Metrics[name]
	if len(pts) == 0 {
		return 0, false
	}
	return pts[len(pts)-1].Value, true
}

// RunQuery filters runs during ListRuns.
type RunQuery struct {
	ExperimentID string
	Status       RunStatus // empty matches any status
	// MetricFilter, when set, keeps runs whose latest value of MetricName
	// satisfies the comparison against MetricValue.
	MetricName  string
	MetricValue float64
	MetricOp    string // one of ">", ">=", "<", "<=", "==" ; empty disables
}

// ListRuns returns copies of runs matching the query, sorted by start time
// descending (newest first).
func (s *TrackingStore) ListRuns(q RunQuery) []*Run {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var candidateIDs []string
	if q.ExperimentID != "" {
		candidateIDs = s.runsByExp[q.ExperimentID]
	} else {
		candidateIDs = make([]string, 0, len(s.runs))
		for id := range s.runs {
			candidateIDs = append(candidateIDs, id)
		}
	}

	out := make([]*Run, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		run := s.runs[id]
		if run == nil {
			continue
		}
		if q.Status != "" && run.Status != q.Status {
			continue
		}
		if q.MetricName != "" && q.MetricOp != "" {
			pts := run.Metrics[q.MetricName]
			if len(pts) == 0 {
				continue
			}
			if !compareFloat(pts[len(pts)-1].Value, q.MetricOp, q.MetricValue) {
				continue
			}
		}
		out = append(out, cloneRun(run))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartTime.After(out[j].StartTime)
	})
	return out
}

// ============================================================================
// Persistence
// ============================================================================

// snapshot is the on-disk representation of the store.
type snapshot struct {
	Version     int                    `json:"version"`
	SavedAt     time.Time              `json:"saved_at"`
	Seq         uint64                 `json:"seq"`
	Experiments map[string]*Experiment `json:"experiments"`
	Runs        map[string]*Run        `json:"runs"`
	RunsByExp   map[string][]string    `json:"runs_by_exp"`
}

// Save writes a JSON snapshot to persistPath. It is an error to call Save on a
// store created without a persist path.
func (s *TrackingStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.persistPath == "" {
		return fmt.Errorf("mlops: store has no persist path configured")
	}
	snap := snapshot{
		Version:     1,
		SavedAt:     s.now(),
		Seq:         s.seq,
		Experiments: s.experiments,
		Runs:        s.runs,
		RunsByExp:   s.runsByExp,
	}
	data, err := json.MarshalIndent(&snap, "", "  ")
	if err != nil {
		return fmt.Errorf("mlops: marshal snapshot: %w", err)
	}
	if dir := filepath.Dir(s.persistPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mlops: create persist dir: %w", err)
		}
	}
	tmp := s.persistPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("mlops: write snapshot: %w", err)
	}
	if err := os.Rename(tmp, s.persistPath); err != nil {
		return fmt.Errorf("mlops: commit snapshot: %w", err)
	}
	return nil
}

// Load replaces the store contents with the snapshot at persistPath.
func (s *TrackingStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persistPath == "" {
		return fmt.Errorf("mlops: store has no persist path configured")
	}
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		return fmt.Errorf("mlops: read snapshot: %w", err)
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("mlops: unmarshal snapshot: %w", err)
	}
	s.experiments = snap.Experiments
	s.runs = snap.Runs
	s.runsByExp = snap.RunsByExp
	s.seq = snap.Seq
	if s.experiments == nil {
		s.experiments = make(map[string]*Experiment)
	}
	if s.runs == nil {
		s.runs = make(map[string]*Run)
	}
	if s.runsByExp == nil {
		s.runsByExp = make(map[string][]string)
	}
	return nil
}

// ============================================================================
// Helpers
// ============================================================================

func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneRun(r *Run) *Run {
	cp := *r
	cp.Params = cloneStringMap(r.Params)
	cp.Tags = cloneStringMap(r.Tags)
	cp.Metrics = make(map[string][]MetricPoint, len(r.Metrics))
	for k, v := range r.Metrics {
		pts := make([]MetricPoint, len(v))
		copy(pts, v)
		cp.Metrics[k] = pts
	}
	if len(r.Artifacts) > 0 {
		cp.Artifacts = make([]Artifact, len(r.Artifacts))
		copy(cp.Artifacts, r.Artifacts)
	}
	if r.EndTime != nil {
		t := *r.EndTime
		cp.EndTime = &t
	}
	if r.Provenance != nil {
		p := *r.Provenance
		cp.Provenance = &p
	}
	return &cp
}

func compareFloat(a float64, op string, b float64) bool {
	switch op {
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "<":
		return a < b
	case "<=":
		return a <= b
	case "==":
		return a == b
	default:
		return false
	}
}
