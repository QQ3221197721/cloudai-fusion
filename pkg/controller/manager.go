package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/election"
)

// ============================================================================
// Controller Manager — orchestrates multiple reconciliation loops
// ============================================================================

// ManagerConfig holds configuration for the controller manager.
type ManagerConfig struct {
	// Logger for structured logging.
	Logger *logrus.Logger

	// MaxConcurrentReconciles is the maximum number of concurrent Reconcile
	// calls per controller. Default: 1 (serial processing).
	MaxConcurrentReconciles int

	// SyncPeriod is how often controllers perform a full resync.
	// Default: 10 minutes.
	SyncPeriod time.Duration

	// LeaderElection enables leader election (for HA deployments).
	// When true, only the leader instance runs reconciliation loops.
	LeaderElection bool

	// LeaderElectionID is the lock name used for leader election.
	LeaderElectionID string

	// LeaderElectionConfig provides custom leader election configuration.
	// If nil and LeaderElection is true, defaults are used.
	LeaderElectionConfig *election.Config

	// MetricsBindAddress is the address for Prometheus metrics.
	MetricsBindAddress string
}

// controllerEntry holds a registered reconciler and its work queue.
type controllerEntry struct {
	reconciler Reconciler
	queue      *WorkQueue
}

// Manager manages multiple controllers, each running its own reconciliation loop.
// Modeled after controller-runtime's ctrl.Manager.
type Manager struct {
	controllers map[string]*controllerEntry // name -> entry
	config      ManagerConfig
	logger      *logrus.Logger

	// Event recorder for audit trail
	events     []Event
	eventMu    sync.RWMutex
	maxEvents  int

	// Leader Election — only the leader runs reconciliation
	elector    election.LeaderElector
	isLeader   bool

	// Health state
	started    bool
	healthy    bool
	startTime  time.Time

	// Lifecycle
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// NewManager creates a new controller manager.
func NewManager(cfg ManagerConfig) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	if cfg.MaxConcurrentReconciles <= 0 {
		cfg.MaxConcurrentReconciles = 1
	}
	if cfg.SyncPeriod <= 0 {
		cfg.SyncPeriod = 10 * time.Minute
	}

	return &Manager{
		controllers: make(map[string]*controllerEntry),
		config:      cfg,
		logger:      logger,
		maxEvents:   1000,
		healthy:     true,
	}
}

// RegisterReconciler adds a reconciler to the manager.
// Must be called before Start().
func (m *Manager) RegisterReconciler(r Reconciler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot register reconciler after manager has started")
	}

	name := r.Name()
	if _, exists := m.controllers[name]; exists {
		return fmt.Errorf("reconciler %q already registered", name)
	}

	m.controllers[name] = &controllerEntry{
		reconciler: r,
		queue:      NewWorkQueue(),
	}

	m.logger.WithFields(logrus.Fields{
		"controller":    name,
		"resource_kind": r.ResourceKind(),
	}).Info("Reconciler registered")

	return nil
}

// Start starts all registered controllers and blocks until ctx is cancelled.
// If leader election is enabled, reconciliation loops only run on the leader.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return fmt.Errorf("manager already started")
	}
	m.started = true
	m.startTime = time.Now()

	ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	m.logger.WithField("controllers", len(m.controllers)).Info("Starting controller manager")

	// Initialize leader election if enabled
	if m.config.LeaderElection {
		if err := m.setupLeaderElection(ctx); err != nil {
			m.logger.WithError(err).Warn("Leader election setup failed, running as standalone leader")
			m.isLeader = true
		} else {
			// Wait for leadership or context cancellation
			m.logger.Info("Waiting for leader election result...")
		}
	} else {
		m.isLeader = true
	}

	// Start each controller's reconcile loop(s)
	for name, entry := range m.controllers {
		for i := 0; i < m.config.MaxConcurrentReconciles; i++ {
			m.wg.Add(1)
			go m.runWorker(ctx, name, entry)
		}

		// Start periodic resync for each controller
		m.wg.Add(1)
		go m.runResyncLoop(ctx, name, entry)
	}

	// Block until context is cancelled
	<-ctx.Done()

	m.logger.Info("Controller manager shutting down")

	// Resign leadership on shutdown
	if m.elector != nil {
		m.elector.Resign()
	}

	// Shut down all queues
	for _, entry := range m.controllers {
		entry.queue.ShutDown()
	}

	// Wait for all workers to finish
	m.wg.Wait()

	m.logger.Info("Controller manager stopped")
	return nil
}

// setupLeaderElection initializes the leader election system.
func (m *Manager) setupLeaderElection(ctx context.Context) error {
	cfg := election.DefaultConfig()

	if m.config.LeaderElectionConfig != nil {
		cfg = *m.config.LeaderElectionConfig
	}

	if m.config.LeaderElectionID != "" {
		cfg.LockName = m.config.LeaderElectionID
	}
	cfg.Logger = m.logger

	cfg.Callbacks = election.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			m.mu.Lock()
			m.isLeader = true
			m.mu.Unlock()
			m.logger.Info("This instance became the leader — reconciliation loops active")

			m.recordEvent(Event{
				Type:       EventNormal,
				Reason:     "LeaderElected",
				Message:    fmt.Sprintf("Instance %s became leader", cfg.Identity),
				Timestamp:  time.Now().UTC(),
				Controller: "leader-election",
			})
		},
		OnStoppedLeading: func() {
			m.mu.Lock()
			m.isLeader = false
			m.mu.Unlock()
			m.logger.Warn("This instance lost leadership — reconciliation loops paused")

			m.recordEvent(Event{
				Type:       EventWarning,
				Reason:     "LeaderLost",
				Message:    fmt.Sprintf("Instance %s lost leadership", cfg.Identity),
				Timestamp:  time.Now().UTC(),
				Controller: "leader-election",
			})
		},
		OnNewLeader: func(identity string) {
			m.logger.WithField("leader", identity).Info("New leader elected")
		},
	}

	elector, err := election.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}

	m.elector = elector

	// Run election in background
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := elector.Run(ctx); err != nil {
			m.logger.WithError(err).Error("Leader election failed")
		}
	}()

	return nil
}

// IsLeader returns whether this controller manager instance is the leader.
func (m *Manager) IsLeader() bool {
	if m.elector != nil {
		return m.elector.IsLeader()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isLeader
}

// ElectionStats returns leader election statistics, if election is enabled.
func (m *Manager) ElectionStats() *election.ElectionStats {
	if m.elector == nil {
		return nil
	}
	stats := m.elector.Stats()
	return &stats
}

// Stop gracefully stops the controller manager.
func (m *Manager) Stop() {
	m.mu.RLock()
	cancel := m.cancel
	m.mu.RUnlock()

	if cancel != nil {
		cancel()
	}
}

// runWorker runs the reconcile loop for a single controller.
func (m *Manager) runWorker(ctx context.Context, name string, entry *controllerEntry) {
	defer m.wg.Done()

	logger := m.logger.WithField("controller", name)
	logger.Debug("Controller worker started")

	for {
		req, shutdown := entry.queue.Get()
		if shutdown {
			logger.Debug("Controller worker shutting down")
			return
		}

		m.processItem(ctx, name, entry, req)
	}
}

// processItem reconciles a single request and handles results/errors.
func (m *Manager) processItem(ctx context.Context, name string, entry *controllerEntry, req Request) {
	defer entry.queue.Done(req)

	// Skip reconciliation if not the leader (HA mode)
	if m.config.LeaderElection && !m.IsLeader() {
		// Re-queue with delay — will be processed when we become leader
		entry.queue.AddAfter(req, 5*time.Second)
		return
	}

	logger := m.logger.WithFields(logrus.Fields{
		"controller": name,
		"object":     req.String(),
	})

	start := time.Now()
	result, err := entry.reconciler.Reconcile(ctx, req)
	duration := time.Since(start)

	if err != nil {
		logger.WithError(err).WithField("duration", duration).Warn("Reconciliation failed, requeuing with backoff")

		// Record error event
		m.recordEvent(Event{
			Type:       EventWarning,
			Reason:     "ReconcileError",
			Message:    fmt.Sprintf("Reconciliation failed: %v", err),
			Object:     req,
			Timestamp:  time.Now().UTC(),
			Controller: name,
		})

		// Requeue with exponential backoff
		entry.queue.AddRateLimited(req)
		return
	}

	// Success — reset backoff
	entry.queue.Forget(req)

	if result.RequeueAfter > 0 {
		logger.WithFields(logrus.Fields{
			"duration":      duration,
			"requeue_after": result.RequeueAfter,
		}).Debug("Reconciliation complete, requeue scheduled")

		entry.queue.AddAfter(req, result.RequeueAfter)
		return
	}

	if result.Requeue {
		logger.WithField("duration", duration).Debug("Reconciliation complete, immediate requeue")
		entry.queue.Add(req)
		return
	}

	logger.WithField("duration", duration).Debug("Reconciliation complete")
}

// runResyncLoop periodically enqueues all known objects for a full resync.
func (m *Manager) runResyncLoop(ctx context.Context, name string, entry *controllerEntry) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.SyncPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.logger.WithField("controller", name).Debug("Periodic resync triggered")
			// Enqueue a sentinel request to trigger a full list-and-reconcile
			entry.queue.Add(Request{
				Name: "__resync__",
			})
		}
	}
}

// Enqueue adds a request to the specified controller's work queue.
// This is the primary way to trigger reconciliation from external events.
func (m *Manager) Enqueue(controllerName string, req Request) error {
	m.mu.RLock()
	entry, ok := m.controllers[controllerName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("controller %q not found", controllerName)
	}

	entry.queue.Add(req)
	return nil
}

// EnqueueAfter adds a delayed request to the specified controller's work queue.
func (m *Manager) EnqueueAfter(controllerName string, req Request, delay time.Duration) error {
	m.mu.RLock()
	entry, ok := m.controllers[controllerName]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("controller %q not found", controllerName)
	}

	entry.queue.AddAfter(req, delay)
	return nil
}

// ============================================================================
// Health & Metrics
// ============================================================================

// Healthy returns whether the controller manager is healthy.
func (m *Manager) Healthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started && m.healthy
}

// Status returns the current status of all controllers.
func (m *Manager) Status() ManagerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	controllers := make([]ControllerStatus, 0, len(m.controllers))
	for name, entry := range m.controllers {
		metrics := entry.queue.Metrics()
		controllers = append(controllers, ControllerStatus{
			Name:         name,
			ResourceKind: entry.reconciler.ResourceKind(),
			QueueDepth:   metrics.CurrentDepth,
			InFlight:     metrics.InFlight,
			TotalAdded:   metrics.TotalAdded,
			TotalProcessed: metrics.TotalProcessed,
			TotalRequeued:  metrics.TotalRequeued,
		})
	}

	uptime := time.Duration(0)
	if m.started {
		uptime = time.Since(m.startTime)
	}

	return ManagerStatus{
		Started:       m.started,
		Healthy:       m.healthy,
		IsLeader:      m.isLeader,
		Uptime:        uptime,
		Controllers:   controllers,
		TotalEvents:   int64(len(m.events)),
		ElectionStats: m.ElectionStats(),
	}
}

// ManagerStatus describes the overall status of the controller manager.
type ManagerStatus struct {
	Started        bool                    `json:"started"`
	Healthy        bool                    `json:"healthy"`
	IsLeader       bool                    `json:"is_leader"`
	Uptime         time.Duration           `json:"uptime"`
	Controllers    []ControllerStatus      `json:"controllers"`
	TotalEvents    int64                   `json:"total_events"`
	ElectionStats  *election.ElectionStats `json:"election_stats,omitempty"`
}

// ControllerStatus describes the status of a single controller.
type ControllerStatus struct {
	Name           string `json:"name"`
	ResourceKind   string `json:"resource_kind"`
	QueueDepth     int    `json:"queue_depth"`
	InFlight       int    `json:"in_flight"`
	TotalAdded     int64  `json:"total_added"`
	TotalProcessed int64  `json:"total_processed"`
	TotalRequeued  int64  `json:"total_requeued"`
}

// ============================================================================
// Event Recording
// ============================================================================

// recordEvent records a reconciliation event.
func (m *Manager) recordEvent(evt Event) {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()

	m.events = append(m.events, evt)

	// Evict oldest events if over capacity
	if len(m.events) > m.maxEvents {
		m.events = m.events[len(m.events)-m.maxEvents:]
	}
}

// RecordEvent allows external callers (reconcilers) to record events.
func (m *Manager) RecordEvent(evt Event) {
	m.recordEvent(evt)
}

// GetEvents returns the most recent events.
func (m *Manager) GetEvents(limit int) []Event {
	m.eventMu.RLock()
	defer m.eventMu.RUnlock()

	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}

	start := len(m.events) - limit
	if start < 0 {
		start = 0
	}

	result := make([]Event, limit)
	copy(result, m.events[start:])
	return result
}
