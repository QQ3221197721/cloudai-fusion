package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Manager — plugin lifecycle orchestrator
// ============================================================================

// ManagerConfig configures the plugin Manager.
type ManagerConfig struct {
	// InitTimeout is the max duration for a single plugin Init call.
	InitTimeout time.Duration
	// StartTimeout is the max duration for a single plugin Start call.
	StartTimeout time.Duration
	// StopTimeout is the max duration for a single plugin Stop call.
	StopTimeout time.Duration
	// HealthCheckInterval is how often plugin health is polled.
	HealthCheckInterval time.Duration
	// Logger for plugin lifecycle events.
	Logger *logrus.Logger
	// PluginConfigs maps plugin-name → config that gets passed to Init.
	PluginConfigs map[string]map[string]interface{}
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		InitTimeout:         30 * time.Second,
		StartTimeout:        30 * time.Second,
		StopTimeout:         15 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		PluginConfigs:       make(map[string]map[string]interface{}),
	}
}

// Manager orchestrates the lifecycle of all plugins in a Registry.
type Manager struct {
	config   ManagerConfig
	registry *Registry
	logger   *logrus.Logger

	mu       sync.RWMutex
	statuses map[string]*Status // name → runtime status
	order    []string           // init/start order (topo-sorted)
	cancel   context.CancelFunc
}

// NewManager creates a new plugin Manager backed by the given Registry.
func NewManager(registry *Registry, config ManagerConfig) *Manager {
	logger := config.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Manager{
		config:   config,
		registry: registry,
		logger:   logger,
		statuses: make(map[string]*Status),
	}
}

// ============================================================================
// Lifecycle — Init → Start → HealthLoop → Stop
// ============================================================================

// InitAll builds the registry and calls Init on each plugin in dependency order.
func (m *Manager) InitAll(ctx context.Context) error {
	order, err := m.registry.Build()
	if err != nil {
		return fmt.Errorf("plugin registry build failed: %w", err)
	}
	m.order = order

	for _, name := range order {
		p, err := m.registry.Get(name)
		if err != nil {
			return err
		}

		meta := p.Metadata()
		m.setStatus(name, &Status{
			Name:       name,
			Version:    meta.Version,
			Phase:      PhaseInitializing,
			Extensions: meta.ExtensionPoints,
		})

		initCtx, cancel := context.WithTimeout(ctx, m.config.InitTimeout)
		cfg := m.config.PluginConfigs[name]

		m.logger.WithField("plugin", name).Info("initializing plugin")
		if err := p.Init(initCtx, cfg); err != nil {
			cancel()
			m.setPhase(name, PhaseError, err.Error())
			return fmt.Errorf("plugin %q init failed: %w", name, err)
		}
		cancel()
		m.setPhase(name, PhaseReady, "")
		m.logger.WithField("plugin", name).Info("plugin initialized")
	}
	return nil
}

// StartAll calls Start on each plugin in dependency order and begins health monitoring.
func (m *Manager) StartAll(ctx context.Context) error {
	for _, name := range m.order {
		p, err := m.registry.Get(name)
		if err != nil {
			return err
		}

		startCtx, cancel := context.WithTimeout(ctx, m.config.StartTimeout)
		m.logger.WithField("plugin", name).Info("starting plugin")
		if err := p.Start(startCtx); err != nil {
			cancel()
			m.setPhase(name, PhaseError, err.Error())
			return fmt.Errorf("plugin %q start failed: %w", name, err)
		}
		cancel()

		now := time.Now()
		m.mu.Lock()
		if st, ok := m.statuses[name]; ok {
			st.Phase = PhaseRunning
			st.Healthy = true
			st.StartedAt = &now
			st.LastError = ""
		}
		m.mu.Unlock()
		m.logger.WithField("plugin", name).Info("plugin started")
	}

	// Start background health monitor.
	if m.config.HealthCheckInterval > 0 {
		hCtx, hCancel := context.WithCancel(ctx)
		m.cancel = hCancel
		go m.healthLoop(hCtx)
	}
	return nil
}

// StopAll gracefully stops all plugins in reverse dependency order.
func (m *Manager) StopAll(ctx context.Context) error {
	// Cancel health monitor.
	if m.cancel != nil {
		m.cancel()
	}

	var firstErr error
	// Reverse order for teardown.
	for i := len(m.order) - 1; i >= 0; i-- {
		name := m.order[i]
		p, err := m.registry.Get(name)
		if err != nil {
			continue
		}

		m.setPhase(name, PhaseStopping, "")
		stopCtx, cancel := context.WithTimeout(ctx, m.config.StopTimeout)
		m.logger.WithField("plugin", name).Info("stopping plugin")
		if err := p.Stop(stopCtx); err != nil {
			m.logger.WithError(err).WithField("plugin", name).Error("plugin stop failed")
			m.setPhase(name, PhaseError, err.Error())
			if firstErr == nil {
				firstErr = fmt.Errorf("plugin %q stop failed: %w", name, err)
			}
		} else {
			m.setPhase(name, PhaseStopped, "")
		}
		cancel()
	}
	return firstErr
}

// ============================================================================
// Health monitoring
// ============================================================================

func (m *Manager) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkHealth(ctx)
		}
	}
}

func (m *Manager) checkHealth(ctx context.Context) {
	for _, name := range m.order {
		p, err := m.registry.Get(name)
		if err != nil {
			continue
		}

		hCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = p.Health(hCtx)
		cancel()

		m.mu.Lock()
		if st, ok := m.statuses[name]; ok {
			st.Healthy = (err == nil)
			if st.StartedAt != nil {
				st.Uptime = time.Since(*st.StartedAt)
			}
			if err != nil {
				st.LastError = err.Error()
			} else {
				st.LastError = ""
			}
		}
		m.mu.Unlock()
	}
}

// ============================================================================
// Accessors
// ============================================================================

// Status returns the current status of a specific plugin.
func (m *Manager) Status(name string) (*Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	st, ok := m.statuses[name]
	if !ok {
		return nil, &ErrPluginNotFound{Name: name}
	}
	// Return a copy.
	cp := *st
	return &cp, nil
}

// AllStatuses returns a snapshot of all plugin statuses.
func (m *Manager) AllStatuses() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Status, 0, len(m.statuses))
	for _, st := range m.statuses {
		cp := *st
		if cp.StartedAt != nil {
			cp.Uptime = time.Since(*cp.StartedAt)
		}
		result = append(result, cp)
	}
	return result
}

// PluginOrder returns the resolved init/start order.
func (m *Manager) PluginOrder() []string {
	return m.order
}

// Registry returns the underlying registry.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// ============================================================================
// Internal helpers
// ============================================================================

func (m *Manager) setStatus(name string, st *Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[name] = st
}

func (m *Manager) setPhase(name string, phase Phase, lastErr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.statuses[name]; ok {
		st.Phase = phase
		st.LastError = lastErr
		if phase == PhaseError {
			st.Healthy = false
		}
	}
}
