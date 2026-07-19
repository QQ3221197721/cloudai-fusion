package redteam

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// engagement.go holds the engagement lifecycle state machine and the Manager
// that owns engagements. Creating an engagement mints a signed scope grant; all
// transitions are guarded and (for termination) recorded.

// Status is an engagement's lifecycle state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusAborted   Status = "aborted"
	StatusCompleted Status = "completed"
)

// terminal reports whether a status admits no further transitions.
func (s Status) terminal() bool { return s == StatusAborted || s == StatusCompleted }

// Finding is a security finding produced during an engagement.
type Finding struct {
	ID        string    `json:"id"`
	Asset     string    `json:"asset"`
	Severity  string    `json:"severity"`
	Title     string    `json:"title"`
	Technique string    `json:"technique"`
	CreatedAt time.Time `json:"created_at"`
}

// Engagement is one authorized red-team run and its authorization gate.
type Engagement struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id,omitempty"`
	Scope     Scope      `json:"scope"`
	Status    Status     `json:"status"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	Findings  []*Finding `json:"findings,omitempty"`

	gate *Gate
	mu   sync.Mutex
}

// Gate returns the engagement's authorization gate.
func (e *Engagement) Gate() *Gate { return e.gate }

// Manager owns engagements and reports the subsystem's capability. It is
// additive: it mutates no other subsystem.
type Manager struct {
	recorder evidence.Recorder
	logger   *logrus.Logger

	mu          sync.RWMutex
	engagements map[string]*Engagement
}

// NewManager builds a red-team manager over an evidence recorder and honestly
// reports the authorization gate to the capability registry.
func NewManager(recorder evidence.Recorder, logger *logrus.Logger) *Manager {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	_ = capability.Report("redteam.authz", "scope-gate", capability.ModeReal,
		"signed-scope authorization gate with per-action evidence")
	return &Manager{
		recorder:    recorder,
		logger:      logger,
		engagements: make(map[string]*Engagement),
	}
}

// Create registers a new engagement from a signed scope, records the scope grant,
// and returns the engagement (with its authorization gate ready).
func (m *Manager) Create(ctx context.Context, scope Scope, principal string) (*Engagement, error) {
	if len(scope.Targets) == 0 {
		return nil, fmt.Errorf("redteam: scope must authorize at least one target")
	}
	if principal == "" {
		return nil, fmt.Errorf("redteam: an authenticated principal is required to grant a scope")
	}
	e := &Engagement{
		ID:        common.NewUUID(),
		Scope:     scope,
		Status:    StatusPending,
		CreatedBy: principal,
		CreatedAt: common.NowUTC(),
	}
	e.gate = NewGate(e.ID, scope, m.recorder, m.logger)

	m.mu.Lock()
	m.engagements[e.ID] = e
	m.mu.Unlock()

	recordScopeGrant(ctx, m.recorder, m.logger, e, principal)
	m.logger.WithFields(logrus.Fields{
		"engagement": e.ID, "principal": principal, "targets": len(scope.Targets),
	}).Info("redteam: engagement scope granted")
	return e, nil
}

// Get returns an engagement by ID.
func (m *Manager) Get(id string) (*Engagement, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.engagements[id]
	if !ok {
		return nil, fmt.Errorf("redteam: engagement %q not found", id)
	}
	return e, nil
}

// List returns all engagements.
func (m *Manager) List() []*Engagement {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Engagement, 0, len(m.engagements))
	for _, e := range m.engagements {
		out = append(out, e)
	}
	return out
}

// Start transitions pending/paused -> running.
func (m *Manager) Start(id string) error {
	e, err := m.Get(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Status != StatusPending && e.Status != StatusPaused {
		return fmt.Errorf("redteam: cannot start engagement in state %q", e.Status)
	}
	e.Status = StatusRunning
	return nil
}

// Complete transitions running -> completed and seals the engagement so its
// completeness is provable (Moat A / M0).
func (m *Manager) Complete(id string) error {
	e, err := m.Get(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.Status != StatusRunning {
		e.mu.Unlock()
		return fmt.Errorf("redteam: cannot complete engagement in state %q", e.Status)
	}
	e.Status = StatusCompleted
	e.mu.Unlock()
	m.sealEngagement(context.Background(), id)
	return nil
}

// Abort trips the kill-switch and transitions the engagement to aborted. It is
// idempotent, records a redteam.engagement.abort receipt, and seals the
// engagement so an aborted run is still completeness-provable (Moat A / M0).
func (m *Manager) Abort(ctx context.Context, id, reason string) error {
	e, err := m.Get(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.Status.terminal() {
		e.mu.Unlock()
		return nil
	}
	e.gate.Abort(ctx, reason)
	e.Status = StatusAborted
	e.mu.Unlock()
	m.sealEngagement(ctx, id)
	return nil
}

// AddFinding attaches a finding to an engagement and records it.
func (m *Manager) AddFinding(ctx context.Context, id string, f *Finding) error {
	e, err := m.Get(id)
	if err != nil {
		return err
	}
	if f.ID == "" {
		f.ID = common.NewUUID()
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = common.NowUTC()
	}
	e.mu.Lock()
	e.Findings = append(e.Findings, f)
	e.mu.Unlock()

	recordFinding(ctx, m.recorder, m.logger, id, f)
	return nil
}

// sealer is satisfied by *evidence.Ledger. When the recorder is a full ledger,
// terminal transitions seal the engagement's namespace so its completeness is
// provable (Moat A / M0, docs/verifiable-moat-spec.md). A plain Recorder (e.g.
// NopRecorder) simply skips sealing.
type sealer interface {
	SealNamespace(ctx context.Context, namespace, actor string, members []*evidence.Evidence) (*evidence.Evidence, error)
	Store() evidence.Store
}

// NamespaceFor returns the evidence namespace under which an engagement's
// receipts are sealed and completeness-proven.
func NamespaceFor(engagementID string) string {
	return "redteam/engagement/" + engagementID
}

// sealEngagement gathers the engagement's receipts and records a terminal seal so
// a CompletenessProof can later show a report is EXACTLY those receipts - no more,
// no less. It is best-effort and logged on error, like the other evidence
// emitters, so a ledger hiccup never blocks a lifecycle transition.
func (m *Manager) sealEngagement(ctx context.Context, engagementID string) {
	s, ok := m.recorder.(sealer)
	if !ok {
		return
	}
	all, err := s.Store().All(ctx)
	if err != nil {
		m.logger.WithError(err).Warn("redteam: seal: read ledger failed")
		return
	}
	members := EngagementReceipts(all, engagementID)
	if _, err := s.SealNamespace(ctx, NamespaceFor(engagementID), "redteam", members); err != nil {
		m.logger.WithError(err).Warn("redteam: seal engagement failed")
	}
}
