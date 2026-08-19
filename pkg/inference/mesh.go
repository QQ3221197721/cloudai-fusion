// Package inference implements Module 15 — the Inference Service Mesh, which provides
// deployment management, weighted routing, load statistics recording, and a full
// lifecycle state machine (creating → serving → degraded/stopped). Every write operation
// is accompanied by a signed, hash-chained attestation through the evidence ledger,
// ensuring that all inference deployments are tamper-evident and verifiable.
//
// Lock-in thesis: after deploying many services with different traffic routing policies
// and recorded load patterns, teams own a verified history of "we serve X model with Y%
// to version Z on platform P" — migrating means abandoning the provenance auditors trust.
// The mesh routing format (map[version]weight) is the second ecosystem lock-in anchor.
//
// Storage layout (content-addressed, file-system based):
//
//	<root>/inference/services.json    service list (JSON object keyed by serviceID)
//	<root>/inference/<serviceID>/stats.jsonl     load stats (JSON lines, appended)
//
// Every Deploy/SetRoute/RecordStat/Stop/MarkDegraded writes a real attestation through
// pkg/evidence.Ledger; pass nil ledger to skip attestation.
package inference

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

// ServiceStatus defines the valid lifecycle states for an inference service.
type ServiceStatus string

const (
	// StatusCreating indicates the service has been submitted but not yet ready (reserved for future async deploy workflows).
	StatusCreating ServiceStatus = "creating"
	// StatusServing indicates the service is actively serving traffic according to its routes.
	StatusServing ServiceStatus = "serving"
	// StatusDegraded indicates the service is experiencing issues (manual mark only).
	StatusDegraded ServiceStatus = "degraded"
	// StatusStopped indicates the service has been stopped and serves no traffic.
	StatusStopped ServiceStatus = "stopped"
)

// Sentinel errors callers can test with errors.Is.
var (
	// ErrNotFound is returned when a service is absent.
	ErrNotFound = errors.New("inference: service not found")
	// ErrStopped is returned when operating on an already-stopped service.
	ErrStopped = errors.New("inference: service already stopped")
	// ErrInvalidServiceID is returned when service ID fails validation.
	ErrInvalidServiceID = errors.New("inference: invalid service ID")
)

// ValidateModelFunc injects optional model registry validation (dependency-free from package).
// Called during Deploy if set. Used to gate deployments behind model existence checks.
type ValidateModelFunc func(modelName, version string) error

// Service represents one inference deployment within the mesh.
type Service struct {
	ID        string         `json:"id"`        // "inf-<hex16>"
	Name      string         `json:"name"`      // human-readable name
	ModelRef  string         `json:"model_ref"` // "name@version" like "my-model@v3"
	Endpoint  string         `json:"endpoint"`  // target URL or empty (auto-generated)
	Status    ServiceStatus  `json:"status"`
	Replicas  int            `json:"replicas"`
	Routes    map[string]int `json:"routes"` // map[version]weight, must sum to 100 when serving
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// LoadStat records a single load measurement for one inference service.
type LoadStat struct {
	ServiceID     string    `json:"service_id"`
	Timestamp     time.Time `json:"timestamp"`
	Requests      int64     `json:"requests"`
	Errors        int64     `json:"errors"`
	LatencyP50Ms  float64   `json:"latency_p50_ms"` // milliseconds
	LatencyP95Ms  float64   `json:"latency_p95_ms"`
	LatencyP99Ms  float64   `json:"latency_p99_ms"`
	ThroughputRPS float64   `json:"throughput_rps"`
}

// DeployInput describes a new inference deployment.
type DeployInput struct {
	Name     string
	ModelRef string // "name@version"
	Endpoint string // optional; auto-generated when empty
	Replicas int
	Actor    string // defaults to "cafctl-infer"; also the attestation actor
}

// FSMInferenceMesh is the file-system backed inference mesh manager. It stores
// services in <dir>/inference/ and load stats per-service in append-only JSONL.
// A nil ledger disables attestation (all other behavior unchanged).
type FSMInferenceMesh struct {
	root      string
	ledger    *evidence.Ledger
	validator ValidateModelFunc

	mu   sync.Mutex
	last *evidence.Evidence // most recent receipt, for CLI display
}

// NewFSMInferenceMesh opens (and creates, if needed) a mesh rooted at dir. Services
// live in <dir>/inference/, stats in <dir>/inference/<serviceID>/stats.jsonl.
// The directory path is normalized to absolute to ensure stable paths immune to
// later cwd shifts.
func NewFSMInferenceMesh(dir string, ledger *evidence.Ledger) (*FSMInferenceMesh, error) {
	if dir == "" {
		return nil, errors.New("inference: root path is required")
	}
	// Normalize to an absolute path once so every safeJoin prefix check and
	// on-disk write is anchored to a stable location, immune to later cwd
	// shifts in the process.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("inference: resolve root path: %w", err)
	}
	inferenceDir := filepath.Join(absDir, "inference")
	if err := os.MkdirAll(inferenceDir, 0o755); err != nil {
		return nil, fmt.Errorf("inference: create root: %w", err)
	}
	return &FSMInferenceMesh{root: inferenceDir, ledger: ledger}, nil
}

// SetModelValidator injects a model validator function used during Deploy.
// This allows optional integration with Module 13 (modelregistry) without a
// direct package dependency: callers wrap registry lookups in this func.
func (m *FSMInferenceMesh) SetModelValidator(fn ValidateModelFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validator = fn
}

// Root returns the inference mesh root directory (read-only accessor).
func (m *FSMInferenceMesh) Root() string { return m.root }

// LastAttestation returns the receipt from the most recent operation
// (Deploy/SetRoute/RecordStat/Stop/MarkDegraded), or nil when none was written
// (nil ledger or no operations yet). This is a genuine, signed ledger receipt.
func (m *FSMInferenceMesh) LastAttestation() *evidence.Evidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}

// Deploy creates a new inference service. The ModelRef must be "name@version";
// when a validator is injected (SetModelValidator) the model must resolve there
// first. Deploy completes synchronously to StatusServing with the initial route
// table {deployedVersion: 100}; "creating" is reserved for a future async flow.
// Writes attestation action "inference.deploy".
func (m *FSMInferenceMesh) Deploy(ctx context.Context, in DeployInput) (*Service, error) {
	if in.Name == "" {
		return nil, errors.New("inference: service name is required")
	}
	if in.Replicas <= 0 {
		return nil, errors.New("inference: replica count must be positive")
	}

	modelName, version, err := parseModelRef(in.ModelRef)
	if err != nil {
		return nil, err
	}

	validator := m.currentValidator()
	if validator != nil {
		if verr := validator(modelName, version); verr != nil {
			return nil, fmt.Errorf("inference: model validation failed: %w", verr)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("inference: generate random bytes: %w", err)
	}
	serviceID := fmt.Sprintf("inf-%s", hex.EncodeToString(idBytes)[:16])

	now := time.Now().UTC()
	endpoint := in.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("http://%s.infer.mesh.local:8080", serviceID)
	}

	svc := &Service{
		ID:        serviceID,
		Name:      in.Name,
		ModelRef:  in.ModelRef,
		Endpoint:  endpoint,
		Status:    StatusServing,
		Replicas:  in.Replicas,
		Routes:    map[string]int{version: 100},
		CreatedAt: now,
		UpdatedAt: now,
	}

	actor := in.Actor
	if actor == "" {
		actor = "cafctl-infer"
	}

	if err := m.persistServiceLocked(svc); err != nil {
		return nil, err
	}

	if err := m.attestLocked(ctx, "inference.deploy", serviceID, actor,
		map[string]any{"name": in.Name, "model_ref": in.ModelRef, "endpoint": endpoint, "replicas": in.Replicas},
		map[string]any{"service_id": serviceID, "status": string(svc.Status)},
		map[string]any{"routes": svc.Routes, "model_validated": validator != nil}); err != nil {
		return nil, err
	}
	return svc, nil
}

// SetRoute replaces the service's traffic weights. Weights must be positive and
// sum to exactly 100; the service must exist and not be stopped. Writes
// attestation action "inference.route".
func (m *FSMInferenceMesh) SetRoute(ctx context.Context, serviceID string, weights map[string]int) error {
	if err := validateServiceID(serviceID); err != nil {
		return fmt.Errorf("%w: %q: %s", ErrInvalidServiceID, serviceID, err)
	}
	if len(weights) == 0 {
		return errors.New("inference: route weights cannot be empty")
	}
	total := 0
	for v, w := range weights {
		if v == "" {
			return errors.New("inference: route weight versions cannot be empty")
		}
		if w <= 0 {
			return fmt.Errorf("inference: weight for version %q must be positive, got %d", v, w)
		}
		total += w
	}
	if total != 100 {
		return fmt.Errorf("inference: route weights for %q must sum to 100, got %d", serviceID, total)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	svc, err := m.getServiceLocked(serviceID)
	if err != nil {
		return err
	}
	if svc.Status == StatusStopped {
		return fmt.Errorf("%w: cannot set routes on stopped service %q", ErrStopped, serviceID)
	}

	previous := svc.Routes
	svc.Routes = weights
	svc.UpdatedAt = time.Now().UTC()
	if err := m.persistServiceLocked(svc); err != nil {
		return err
	}

	return m.attestLocked(ctx, "inference.route", serviceID, "cafctl-infer",
		map[string]any{"weights": weights},
		map[string]any{"service_id": serviceID, "status": string(svc.Status)},
		map[string]any{"previous_routes": previous, "new_routes": weights})
}

// RecordStat appends one load-stat line to <serviceID>/stats.jsonl and attests.
// The latency triple must satisfy p50 <= p95 <= p99; requests/errors must be
// non-negative; a zero Timestamp is filled with now (UTC). Stats may be recorded
// for stopped services (historical reporting). Action "inference.stat".
// All float fields are checked for finite values (rejects NaN/Inf).
func (m *FSMInferenceMesh) RecordStat(ctx context.Context, serviceID string, stat LoadStat) error {
	// Check all float fields for finite values (reject NaN/Inf) first.
	fields := []struct {
		name string
		val  float64
	}{
		{"latency_p50_ms", stat.LatencyP50Ms},
		{"latency_p95_ms", stat.LatencyP95Ms},
		{"latency_p99_ms", stat.LatencyP99Ms},
		{"throughput_rps", stat.ThroughputRPS},
	}
	for _, f := range fields {
		if math.IsNaN(f.val) || math.IsInf(f.val, 0) {
			return fmt.Errorf("inference: invalid load stat for %q: %s must be a finite number, got %v", serviceID, f.name, f.val)
		}
	}

	if err := validateServiceID(serviceID); err != nil {
		return fmt.Errorf("%w: %q: %s", ErrInvalidServiceID, serviceID, err)
	}
	if stat.LatencyP50Ms > stat.LatencyP95Ms || stat.LatencyP95Ms > stat.LatencyP99Ms {
		return fmt.Errorf("inference: invalid load stat for %q: latency must satisfy p50<=p95<=p99 (got %.2f/%.2f/%.2f)",
			serviceID, stat.LatencyP50Ms, stat.LatencyP95Ms, stat.LatencyP99Ms)
	}
	if stat.Requests < 0 || stat.Errors < 0 {
		return fmt.Errorf("inference: invalid load stat for %q: requests/errors must be non-negative", serviceID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.getServiceLocked(serviceID); err != nil {
		return err
	}

	if stat.Timestamp.IsZero() {
		stat.Timestamp = time.Now().UTC()
	}
	stat.ServiceID = serviceID

	statsPath, err := safeJoin(m.root, serviceID, "stats.jsonl")
	if err != nil {
		return fmt.Errorf("inference: stats path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(statsPath), 0o755); err != nil {
		return fmt.Errorf("inference: create stats dir: %w", err)
	}
	line, err := json.Marshal(stat)
	if err != nil {
		return fmt.Errorf("inference: marshal stat: %w", err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(statsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("inference: open stats file: %w", err)
	}
	defer f.Close()
	// Record the pre-append size so a failed/partial write can be rolled back:
	// a torn JSONL line would poison every future Stats() read of this file.
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("inference: stat stats file: %w", err)
	}
	offset := info.Size()
	n, werr := f.Write(line)
	if werr != nil || n != len(line) {
		// Roll back any partial bytes so the JSONL log never carries a torn
		// line that would poison future Stats() reads. Best effort rollback;
		// report the original write failure with additional context.
		_ = f.Close()
		_ = os.Truncate(statsPath, offset)
		if werr == nil {
			werr = io.ErrShortWrite
		}
		if terr := os.Truncate(statsPath, offset); terr != nil {
			return fmt.Errorf("inference: append stat (wrote %d of %d bytes): %w; rollback failed: %v", n, len(line), werr, terr)
		}
		return fmt.Errorf("inference: append stat (wrote %d of %d bytes): %w", n, len(line), werr)
	}

	return m.attestLocked(ctx, "inference.stat", serviceID, "cafctl-infer",
		map[string]any{"requests": stat.Requests, "errors": stat.Errors,
			"latency_ms": []float64{stat.LatencyP50Ms, stat.LatencyP95Ms, stat.LatencyP99Ms}},
		map[string]any{"appended": true},
		map[string]any{"throughput_rps": stat.ThroughputRPS, "timestamp": stat.Timestamp.Format(time.RFC3339Nano)})
}

// Stats returns the most recent limit entries for a service, newest first.
// limit <= 0 returns all recorded entries. Reads the JSONL log from disk.
func (m *FSMInferenceMesh) Stats(serviceID string, limit int) ([]LoadStat, error) {
	if err := validateServiceID(serviceID); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidServiceID, serviceID, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	statsPath, err := safeJoin(m.root, serviceID, "stats.jsonl")
	if err != nil {
		return nil, fmt.Errorf("inference: stats path: %w", err)
	}
	data, err := os.ReadFile(statsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: no stats recorded for %q", ErrNotFound, serviceID)
		}
		return nil, fmt.Errorf("inference: read stats for %q: %w", serviceID, err)
	}

	var all []LoadStat
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s LoadStat
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("inference: parse stat line %d for %q: %w", i+1, serviceID, err)
		}
		all = append(all, s)
	}

	n := len(all)
	if limit > 0 && limit < n {
		all = all[n-limit:]
	}
	// newest first: reverse the tail slice in place
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if all == nil {
		all = []LoadStat{}
	}
	return all, nil
}

// ListServices returns all services, newest first by CreatedAt (ID breaks ties).
func (m *FSMInferenceMesh) ListServices() ([]Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listServicesLocked()
}

// GetService retrieves one service by ID.
func (m *FSMInferenceMesh) GetService(id string) (*Service, error) {
	if err := validateServiceID(id); err != nil {
		return nil, fmt.Errorf("%w: %q: %s", ErrInvalidServiceID, id, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	svc, err := m.getServiceLocked(id)
	if err != nil {
		return nil, err
	}
	copySvc := *svc
	return &copySvc, nil
}

// Stop transitions a service to stopped. Stopping an already-stopped service is
// an error (idempotent reject). Action "inference.stop".
func (m *FSMInferenceMesh) Stop(ctx context.Context, serviceID string) error {
	if err := validateServiceID(serviceID); err != nil {
		return fmt.Errorf("%w: %q: %s", ErrInvalidServiceID, serviceID, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	svc, err := m.getServiceLocked(serviceID)
	if err != nil {
		return err
	}
	if svc.Status == StatusStopped {
		return fmt.Errorf("%w: service %q is already stopped", ErrStopped, serviceID)
	}

	from := svc.Status
	svc.Status = StatusStopped
	svc.UpdatedAt = time.Now().UTC()
	if err := m.persistServiceLocked(svc); err != nil {
		return err
	}

	return m.attestLocked(ctx, "inference.stop", serviceID, "cafctl-infer",
		map[string]any{"service_id": serviceID},
		map[string]any{"status": string(svc.Status)},
		map[string]any{"from": string(from), "to": string(svc.Status)})
}

// MarkDegraded transitions serving → degraded with a recorded reason. Any other
// source status is rejected. Action "inference.degraded".
func (m *FSMInferenceMesh) MarkDegraded(ctx context.Context, serviceID, reason string) error {
	if err := validateServiceID(serviceID); err != nil {
		return fmt.Errorf("%w: %q: %s", ErrInvalidServiceID, serviceID, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	svc, err := m.getServiceLocked(serviceID)
	if err != nil {
		return err
	}
	if svc.Status != StatusServing {
		return fmt.Errorf("inference: cannot mark %q degraded: status is %q, expected %q", serviceID, svc.Status, StatusServing)
	}

	svc.Status = StatusDegraded
	svc.UpdatedAt = time.Now().UTC()
	if err := m.persistServiceLocked(svc); err != nil {
		return err
	}

	return m.attestLocked(ctx, "inference.degraded", serviceID, "cafctl-infer",
		map[string]any{"service_id": serviceID},
		map[string]any{"status": string(svc.Status)},
		map[string]any{"from": string(StatusServing), "to": string(svc.Status), "reason": reason})
}

// ----------------------------------------------------------------------------
// Internal helpers (caller holds m.mu unless stated otherwise)
// ----------------------------------------------------------------------------

// currentValidator snapshots the validator under lock (Deploy reads it before
// taking the write lock for the whole operation).
func (m *FSMInferenceMesh) currentValidator() ValidateModelFunc {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.validator
}

func (m *FSMInferenceMesh) getServiceLocked(serviceID string) (*Service, error) {
	services, err := m.loadServicesLocked()
	if err != nil {
		return nil, err
	}
	svc, ok := services[serviceID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, serviceID)
	}
	return &svc, nil
}

func (m *FSMInferenceMesh) listServicesLocked() ([]Service, error) {
	services, err := m.loadServicesLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Service, 0, len(services))
	for _, svc := range services {
		out = append(out, svc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// persistServiceLocked merges svc into services.json with an atomic tmp+rename.
func (m *FSMInferenceMesh) persistServiceLocked(svc *Service) error {
	services, err := m.loadServicesLocked()
	if err != nil {
		return err
	}
	services[svc.ID] = *svc
	data, err := json.MarshalIndent(services, "", "  ")
	if err != nil {
		return fmt.Errorf("inference: marshal services: %w", err)
	}
	path, err := safeJoin(m.root, servicesFile)
	if err != nil {
		return fmt.Errorf("inference: services path: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("inference: write services tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("inference: commit services.json: %w", err)
	}
	return nil
}

func (m *FSMInferenceMesh) loadServicesLocked() (map[string]Service, error) {
	path, err := safeJoin(m.root, servicesFile)
	if err != nil {
		return nil, fmt.Errorf("inference: services path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Service{}, nil
		}
		return nil, fmt.Errorf("inference: read services.json: %w", err)
	}
	services := map[string]Service{}
	if err := json.Unmarshal(data, &services); err != nil {
		return nil, fmt.Errorf("inference: parse services.json: %w", err)
	}
	return services, nil
}

// attestLocked writes one receipt through the evidence ledger (real signing and
// hash chaining; the backing store depends on the injected ledger). Caller
// holds m.mu.
func (m *FSMInferenceMesh) attestLocked(ctx context.Context, action, subject, actor string, input, output, payload map[string]any) error {
	if m.ledger == nil {
		return nil
	}
	ev, err := m.ledger.Record(ctx, evidence.RecordInput{
		Actor:   actor,
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("inference: attestation %s failed: %w", action, err)
	}
	m.last = ev
	return nil
}

// ----------------------------------------------------------------------------
// Validation helpers
// ----------------------------------------------------------------------------

const servicesFile = "services.json"

var (
	modelRefPartRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	serviceIDRe    = regexp.MustCompile(`^[a-z0-9-]+$`)
	maxRefPartLen  = 64
)

// parseModelRef splits "name@version" (e.g. "my-model@v3"). Both halves must be
// non-empty, start alphanumeric, and use only [A-Za-z0-9._-] — deliberately
// looser than registry semver because mesh versions are traffic tags (v3, canary).
func parseModelRef(ref string) (name, version string, err error) {
	name, version, found := strings.Cut(ref, "@")
	name, version = strings.TrimSpace(name), strings.TrimSpace(version)
	if !found || name == "" || version == "" {
		return "", "", fmt.Errorf("inference: invalid model ref %q: expected \"name@version\" (e.g. my-model@v3)", ref)
	}
	if len(name) > maxRefPartLen || len(version) > maxRefPartLen {
		return "", "", fmt.Errorf("inference: invalid model ref %q: name/version exceed %d characters", ref, maxRefPartLen)
	}
	if !modelRefPartRe.MatchString(name) {
		return "", "", fmt.Errorf("inference: invalid model ref %q: name must start alphanumeric and use [A-Za-z0-9._-]", ref)
	}
	if !modelRefPartRe.MatchString(version) {
		return "", "", fmt.Errorf("inference: invalid model ref %q: version must start alphanumeric and use [A-Za-z0-9._-]", ref)
	}
	return name, version, nil
}

// validateServiceID enforces a filesystem-safe service ID ([a-z0-9-] only) —
// the primary path-traversal guard for the per-service stats directory.
func validateServiceID(id string) error {
	if id == "" {
		return errors.New("service ID is required")
	}
	if !serviceIDRe.MatchString(id) {
		return errors.New("only [a-z0-9-] allowed (path-traversal protection)")
	}
	return nil
}

// safeJoin joins base with segments and verifies the resolved path stays inside
// base — defense in depth against path traversal (same pattern as
// modelregistry/training).
func safeJoin(base string, segs ...string) (string, error) {
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("inference: safeJoin base %q must be absolute", base)
	}
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
