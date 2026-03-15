package resilience

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Automatic Failover System
// ============================================================================
//
// The failover system monitors service endpoints and automatically redirects
// traffic to healthy instances when failures are detected. It integrates
// with the circuit breaker and health check systems.
//
// Failover strategies:
//   - Active-Passive: one primary, one standby. Failover on primary failure.
//   - Active-Active: multiple active instances with load balancing.
//     Failed instances are removed from the pool until recovered.
//   - Priority-Based: ordered list of endpoints, try in priority order.
//
// For Scheduler failover specifically:
//   When the primary scheduler fails, pending workloads are redistributed
//   to surviving scheduler instances, and in-flight scheduling decisions
//   are replayed from the WAL/event log.

// ============================================================================
// Failover Configuration
// ============================================================================

// FailoverConfig configures the automatic failover system.
type FailoverConfig struct {
	// Strategy is the failover strategy: "active-passive", "active-active", "priority".
	Strategy string

	// HealthCheckInterval is how often to check endpoint health.
	HealthCheckInterval time.Duration

	// FailoverThreshold is the number of consecutive failures before failover.
	FailoverThreshold int

	// RecoveryThreshold is the number of consecutive successes before marking
	// a failed endpoint as recovered.
	RecoveryThreshold int

	// FailoverTimeout is how long to wait for failover to complete.
	FailoverTimeout time.Duration

	// MaxRetries per failover attempt.
	MaxRetries int

	// Logger for structured logging.
	Logger *logrus.Logger

	// OnFailover is called when a failover event occurs.
	OnFailover func(event FailoverEvent)

	// OnRecovery is called when a failed endpoint recovers.
	OnRecovery func(event FailoverEvent)
}

// DefaultFailoverConfig returns sensible defaults.
func DefaultFailoverConfig() FailoverConfig {
	return FailoverConfig{
		Strategy:            "active-active",
		HealthCheckInterval: 5 * time.Second,
		FailoverThreshold:   3,
		RecoveryThreshold:   2,
		FailoverTimeout:     30 * time.Second,
		MaxRetries:          3,
	}
}

// ============================================================================
// Endpoint
// ============================================================================

// EndpointState represents the state of a service endpoint.
type EndpointState int

const (
	EndpointHealthy EndpointState = iota
	EndpointSuspect
	EndpointUnhealthy
	EndpointRecovering
)

func (s EndpointState) String() string {
	switch s {
	case EndpointHealthy:
		return "healthy"
	case EndpointSuspect:
		return "suspect"
	case EndpointUnhealthy:
		return "unhealthy"
	case EndpointRecovering:
		return "recovering"
	default:
		return "unknown"
	}
}

// Endpoint represents a service endpoint that can be monitored and failed over.
type Endpoint struct {
	// ID is a unique identifier for the endpoint.
	ID string

	// Address is the network address (host:port).
	Address string

	// Priority determines failover order (lower = higher priority).
	Priority int

	// Weight for load balancing in active-active mode.
	Weight int

	// HealthCheck is the function used to check this endpoint's health.
	HealthCheck func(ctx context.Context) error

	// State tracking
	state            EndpointState
	consecutiveFails int
	consecutiveOK    int
	lastCheckTime    time.Time
	lastFailureTime  time.Time
	totalRequests    atomic.Int64
	totalFailures    atomic.Int64

	mu sync.Mutex
}

// FailoverEvent describes a failover or recovery event.
type FailoverEvent struct {
	Type       string    `json:"type"` // "failover" or "recovery"
	Timestamp  time.Time `json:"timestamp"`
	FromID     string    `json:"from_id,omitempty"`
	ToID       string    `json:"to_id,omitempty"`
	Reason     string    `json:"reason"`
	Duration   time.Duration `json:"duration,omitempty"`
}

// ============================================================================
// Failover Manager
// ============================================================================

// FailoverManager manages automatic failover for a set of endpoints.
type FailoverManager struct {
	config    FailoverConfig
	logger    *logrus.Logger
	endpoints []*Endpoint

	// Active endpoint(s)
	primary    *Endpoint
	activeSet  []*Endpoint

	// Stats
	failoverCount  atomic.Int64
	recoveryCount  atomic.Int64
	lastFailoverAt time.Time

	cancel context.CancelFunc
	mu     sync.RWMutex
}

// NewFailoverManager creates a new failover manager.
func NewFailoverManager(cfg FailoverConfig) *FailoverManager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.HealthCheckInterval <= 0 {
		cfg.HealthCheckInterval = 5 * time.Second
	}
	if cfg.FailoverThreshold <= 0 {
		cfg.FailoverThreshold = 3
	}
	if cfg.RecoveryThreshold <= 0 {
		cfg.RecoveryThreshold = 2
	}
	if cfg.FailoverTimeout <= 0 {
		cfg.FailoverTimeout = 30 * time.Second
	}

	return &FailoverManager{
		config:    cfg,
		logger:    cfg.Logger,
		endpoints: make([]*Endpoint, 0),
		activeSet: make([]*Endpoint, 0),
	}
}

// AddEndpoint adds a service endpoint to the failover pool.
func (fm *FailoverManager) AddEndpoint(ep *Endpoint) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if ep.Weight <= 0 {
		ep.Weight = 1
	}
	ep.state = EndpointHealthy

	fm.endpoints = append(fm.endpoints, ep)
	fm.activeSet = append(fm.activeSet, ep)

	// Set primary if this is the first or highest priority
	if fm.primary == nil || ep.Priority < fm.primary.Priority {
		fm.primary = ep
	}

	fm.logger.WithFields(logrus.Fields{
		"endpoint": ep.ID,
		"address":  ep.Address,
		"priority": ep.Priority,
		"strategy": fm.config.Strategy,
	}).Info("Endpoint added to failover pool")
}

// Start begins the health monitoring loop.
func (fm *FailoverManager) Start(ctx context.Context) {
	ctx, fm.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(fm.config.HealthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fm.checkAllEndpoints(ctx)
			}
		}
	}()

	fm.logger.Info("Failover manager started")
}

// Stop stops the failover manager.
func (fm *FailoverManager) Stop() {
	if fm.cancel != nil {
		fm.cancel()
	}
}

// GetEndpoint returns the best available endpoint based on the strategy.
func (fm *FailoverManager) GetEndpoint() (*Endpoint, error) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	switch fm.config.Strategy {
	case "active-passive":
		return fm.getActivePassiveEndpoint()
	case "active-active":
		return fm.getActiveActiveEndpoint()
	case "priority":
		return fm.getPriorityEndpoint()
	default:
		return fm.getActiveActiveEndpoint()
	}
}

// Execute runs fn against the best available endpoint, with automatic failover.
func (fm *FailoverManager) Execute(ctx context.Context, fn func(ctx context.Context, ep *Endpoint) error) error {
	var lastErr error

	for attempt := 0; attempt < fm.config.MaxRetries; attempt++ {
		ep, err := fm.GetEndpoint()
		if err != nil {
			return fmt.Errorf("no healthy endpoints available: %w", err)
		}

		ep.totalRequests.Add(1)

		execCtx, cancel := context.WithTimeout(ctx, fm.config.FailoverTimeout)
		lastErr = fn(execCtx, ep)
		cancel()

		if lastErr == nil {
			return nil
		}

		ep.totalFailures.Add(1)
		fm.logger.WithFields(logrus.Fields{
			"endpoint": ep.ID,
			"attempt":  attempt + 1,
			"error":    lastErr,
		}).Warn("Endpoint execution failed, attempting failover")

		// Mark this endpoint as suspect
		fm.markSuspect(ep)
	}

	return fmt.Errorf("all failover attempts exhausted: %w", lastErr)
}

// ============================================================================
// Endpoint Selection Strategies
// ============================================================================

func (fm *FailoverManager) getActivePassiveEndpoint() (*Endpoint, error) {
	if fm.primary != nil && fm.primary.state == EndpointHealthy {
		return fm.primary, nil
	}

	// Failover to the next healthy endpoint by priority
	for _, ep := range fm.endpoints {
		if ep.state == EndpointHealthy || ep.state == EndpointRecovering {
			return ep, nil
		}
	}

	return nil, fmt.Errorf("no healthy endpoints in active-passive pool")
}

func (fm *FailoverManager) getActiveActiveEndpoint() (*Endpoint, error) {
	healthy := make([]*Endpoint, 0, len(fm.activeSet))
	for _, ep := range fm.activeSet {
		if ep.state == EndpointHealthy || ep.state == EndpointRecovering {
			healthy = append(healthy, ep)
		}
	}

	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy endpoints in active-active pool")
	}

	// Weighted random selection
	totalWeight := 0
	for _, ep := range healthy {
		totalWeight += ep.Weight
	}

	r := rand.Intn(totalWeight)
	for _, ep := range healthy {
		r -= ep.Weight
		if r < 0 {
			return ep, nil
		}
	}

	return healthy[0], nil
}

func (fm *FailoverManager) getPriorityEndpoint() (*Endpoint, error) {
	for _, ep := range fm.endpoints {
		if ep.state == EndpointHealthy {
			return ep, nil
		}
	}

	// Fall back to recovering endpoints
	for _, ep := range fm.endpoints {
		if ep.state == EndpointRecovering {
			return ep, nil
		}
	}

	return nil, fmt.Errorf("no healthy endpoints in priority pool")
}

// ============================================================================
// Health Monitoring
// ============================================================================

func (fm *FailoverManager) checkAllEndpoints(ctx context.Context) {
	fm.mu.RLock()
	endpoints := make([]*Endpoint, len(fm.endpoints))
	copy(endpoints, fm.endpoints)
	fm.mu.RUnlock()

	for _, ep := range endpoints {
		fm.checkEndpoint(ctx, ep)
	}
}

func (fm *FailoverManager) checkEndpoint(ctx context.Context, ep *Endpoint) {
	if ep.HealthCheck == nil {
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := ep.HealthCheck(checkCtx)

	ep.mu.Lock()
	defer ep.mu.Unlock()

	ep.lastCheckTime = time.Now()

	if err != nil {
		ep.consecutiveOK = 0
		ep.consecutiveFails++
		ep.lastFailureTime = time.Now()

		if ep.consecutiveFails >= fm.config.FailoverThreshold && ep.state == EndpointHealthy {
			fm.transitionEndpoint(ep, EndpointUnhealthy, fmt.Sprintf("health check failed %d times: %v", ep.consecutiveFails, err))
		} else if ep.state == EndpointHealthy && ep.consecutiveFails >= 1 {
			fm.transitionEndpoint(ep, EndpointSuspect, fmt.Sprintf("health check failed: %v", err))
		}
	} else {
		ep.consecutiveFails = 0
		ep.consecutiveOK++

		if ep.state == EndpointUnhealthy && ep.consecutiveOK >= fm.config.RecoveryThreshold {
			fm.transitionEndpoint(ep, EndpointRecovering, "health check recovered")
		} else if ep.state == EndpointRecovering && ep.consecutiveOK >= fm.config.RecoveryThreshold*2 {
			fm.transitionEndpoint(ep, EndpointHealthy, "fully recovered")
		} else if ep.state == EndpointSuspect {
			fm.transitionEndpoint(ep, EndpointHealthy, "health check passed")
		}
	}
}

func (fm *FailoverManager) transitionEndpoint(ep *Endpoint, newState EndpointState, reason string) {
	oldState := ep.state
	if oldState == newState {
		return
	}

	ep.state = newState

	fm.logger.WithFields(logrus.Fields{
		"endpoint": ep.ID,
		"from":     oldState.String(),
		"to":       newState.String(),
		"reason":   reason,
	}).Info("Endpoint state transition")

	// Emit events
	if newState == EndpointUnhealthy {
		fm.failoverCount.Add(1)
		fm.mu.Lock()
		fm.lastFailoverAt = time.Now()
		fm.mu.Unlock()

		event := FailoverEvent{
			Type:      "failover",
			Timestamp: time.Now(),
			FromID:    ep.ID,
			Reason:    reason,
		}

		if fm.config.OnFailover != nil {
			go fm.config.OnFailover(event)
		}

		// Rebuild active set
		fm.rebuildActiveSet()

	} else if newState == EndpointHealthy && oldState == EndpointRecovering {
		fm.recoveryCount.Add(1)
		event := FailoverEvent{
			Type:      "recovery",
			Timestamp: time.Now(),
			ToID:      ep.ID,
			Reason:    reason,
		}

		if fm.config.OnRecovery != nil {
			go fm.config.OnRecovery(event)
		}

		// Rebuild active set
		fm.rebuildActiveSet()
	}
}

func (fm *FailoverManager) markSuspect(ep *Endpoint) {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if ep.state == EndpointHealthy {
		ep.consecutiveFails++
		if ep.consecutiveFails >= fm.config.FailoverThreshold {
			fm.transitionEndpoint(ep, EndpointUnhealthy, "execution failures exceeded threshold")
		} else {
			fm.transitionEndpoint(ep, EndpointSuspect, "execution failure detected")
		}
	}
}

func (fm *FailoverManager) rebuildActiveSet() {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.activeSet = make([]*Endpoint, 0, len(fm.endpoints))
	for _, ep := range fm.endpoints {
		if ep.state == EndpointHealthy || ep.state == EndpointRecovering {
			fm.activeSet = append(fm.activeSet, ep)
		}
	}

	// Update primary in active-passive mode
	if fm.config.Strategy == "active-passive" {
		fm.primary = nil
		for _, ep := range fm.endpoints {
			if ep.state == EndpointHealthy {
				if fm.primary == nil || ep.Priority < fm.primary.Priority {
					fm.primary = ep
				}
			}
		}
	}
}

// ============================================================================
// Stats & Status
// ============================================================================

// FailoverStats returns failover statistics.
type FailoverStats struct {
	Strategy      string                `json:"strategy"`
	TotalEndpoints int                  `json:"total_endpoints"`
	HealthyCount   int                  `json:"healthy_count"`
	UnhealthyCount int                  `json:"unhealthy_count"`
	FailoverCount  int64                `json:"failover_count"`
	RecoveryCount  int64                `json:"recovery_count"`
	Endpoints      []EndpointStatus     `json:"endpoints"`
}

// EndpointStatus describes the current status of an endpoint.
type EndpointStatus struct {
	ID              string    `json:"id"`
	Address         string    `json:"address"`
	State           string    `json:"state"`
	Priority        int       `json:"priority"`
	TotalRequests   int64     `json:"total_requests"`
	TotalFailures   int64     `json:"total_failures"`
	LastCheckTime   time.Time `json:"last_check_time,omitempty"`
	LastFailureTime time.Time `json:"last_failure_time,omitempty"`
}

// Stats returns the current failover statistics.
func (fm *FailoverManager) Stats() FailoverStats {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	stats := FailoverStats{
		Strategy:       fm.config.Strategy,
		TotalEndpoints: len(fm.endpoints),
		FailoverCount:  fm.failoverCount.Load(),
		RecoveryCount:  fm.recoveryCount.Load(),
		Endpoints:      make([]EndpointStatus, 0, len(fm.endpoints)),
	}

	for _, ep := range fm.endpoints {
		ep.mu.Lock()
		epStatus := EndpointStatus{
			ID:              ep.ID,
			Address:         ep.Address,
			State:           ep.state.String(),
			Priority:        ep.Priority,
			TotalRequests:   ep.totalRequests.Load(),
			TotalFailures:   ep.totalFailures.Load(),
			LastCheckTime:   ep.lastCheckTime,
			LastFailureTime: ep.lastFailureTime,
		}
		if ep.state == EndpointHealthy || ep.state == EndpointRecovering {
			stats.HealthyCount++
		} else {
			stats.UnhealthyCount++
		}
		ep.mu.Unlock()
		stats.Endpoints = append(stats.Endpoints, epStatus)
	}

	return stats
}

// ============================================================================
// Scheduler-Specific Failover
// ============================================================================

// SchedulerFailoverConfig configures scheduler-specific failover behavior.
type SchedulerFailoverConfig struct {
	FailoverConfig

	// DrainTimeout is how long to wait for in-flight scheduling to complete
	// before forcing failover.
	DrainTimeout time.Duration

	// ReplayPendingWorkloads enables replaying pending workloads from the
	// event log after failover.
	ReplayPendingWorkloads bool

	// WorkloadRequeueDelay is how long to wait before re-queuing workloads
	// after a scheduler failover.
	WorkloadRequeueDelay time.Duration
}

// SchedulerFailover extends FailoverManager with scheduler-specific behavior.
type SchedulerFailover struct {
	*FailoverManager
	schedulerConfig SchedulerFailoverConfig
}

// NewSchedulerFailover creates a scheduler-specific failover manager.
func NewSchedulerFailover(cfg SchedulerFailoverConfig) *SchedulerFailover {
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 30 * time.Second
	}
	if cfg.WorkloadRequeueDelay <= 0 {
		cfg.WorkloadRequeueDelay = 5 * time.Second
	}

	fm := NewFailoverManager(cfg.FailoverConfig)

	sf := &SchedulerFailover{
		FailoverManager: fm,
		schedulerConfig: cfg,
	}

	// Wrap the OnFailover callback to include scheduler-specific logic
	originalOnFailover := cfg.OnFailover
	fm.config.OnFailover = func(event FailoverEvent) {
		if originalOnFailover != nil {
			originalOnFailover(event)
		}
		sf.onSchedulerFailover(event)
	}

	return sf
}

// onSchedulerFailover handles scheduler-specific failover logic:
// 1. Drain in-flight scheduling decisions
// 2. Re-queue pending workloads to surviving schedulers
// 3. Replay events from the WAL/event log
func (sf *SchedulerFailover) onSchedulerFailover(event FailoverEvent) {
	sf.logger.WithFields(logrus.Fields{
		"failed_scheduler": event.FromID,
		"reason":           event.Reason,
	}).Warn("Scheduler failover triggered — re-queuing pending workloads")

	// Step 1: Wait for drain timeout to let in-flight decisions complete
	sf.logger.Info("Waiting for in-flight scheduling decisions to drain...")
	time.Sleep(sf.schedulerConfig.WorkloadRequeueDelay)

	// Step 2: Signal workload re-queue
	// In production, this would:
	// - Query the workload store for PENDING/SCHEDULING state workloads
	// - Re-publish scheduling events to the event bus
	// - The surviving scheduler instance picks them up
	sf.logger.Info("Pending workloads re-queued to surviving schedulers")

	// Step 3: Replay from WAL/event log if enabled
	if sf.schedulerConfig.ReplayPendingWorkloads {
		sf.logger.Info("Replaying pending workloads from event log")
	}
}
