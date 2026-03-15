// Package deploy provides deployment management for CloudAI Fusion,
// including canary releases, automatic rollback, and deployment health monitoring.
//
// Architecture:
//   - CanaryController: manages progressive traffic shifting for canary releases
//   - RollbackController: monitors deployment health and triggers automatic rollback
//   - DeploymentManager: orchestrates multi-component deployment with ordering
package deploy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Deployment Types
// ============================================================================

// DeploymentPhase represents the current phase of a deployment.
type DeploymentPhase string

const (
	PhasePending     DeploymentPhase = "pending"
	PhaseCanary      DeploymentPhase = "canary"
	PhaseProgressing DeploymentPhase = "progressing"
	PhasePromoting   DeploymentPhase = "promoting"
	PhaseRollingBack DeploymentPhase = "rolling_back"
	PhaseComplete    DeploymentPhase = "complete"
	PhaseRolledBack  DeploymentPhase = "rolled_back"
	PhaseFailed      DeploymentPhase = "failed"
)

// DeploymentHealth represents health status of a deployment.
type DeploymentHealth string

const (
	HealthHealthy   DeploymentHealth = "healthy"
	HealthDegraded  DeploymentHealth = "degraded"
	HealthUnhealthy DeploymentHealth = "unhealthy"
	HealthUnknown   DeploymentHealth = "unknown"
)

// DeploymentStatus holds the full status of a deployment.
type DeploymentStatus struct {
	Component     string           `json:"component"`
	Phase         DeploymentPhase  `json:"phase"`
	Health        DeploymentHealth `json:"health"`
	StableVersion string           `json:"stable_version"`
	CanaryVersion string           `json:"canary_version,omitempty"`
	CanaryWeight  int              `json:"canary_weight"`
	ErrorRate     float64          `json:"error_rate_percent"`
	P99LatencyMs  float64          `json:"p99_latency_ms"`
	StartedAt     time.Time        `json:"started_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	Message       string           `json:"message,omitempty"`
}

// ============================================================================
// Canary Controller — progressive traffic shifting
// ============================================================================

// CanaryConfig configures canary deployment behavior.
type CanaryConfig struct {
	// InitialWeight is the starting traffic percentage for canary (default: 5).
	InitialWeight int `json:"initial_weight"`

	// ProgressionSteps defines the traffic weight progression.
	// e.g., [5, 10, 25, 50, 100] — promotes in 5 steps.
	ProgressionSteps []int `json:"progression_steps"`

	// StepInterval is the observation window between weight increases.
	StepInterval time.Duration `json:"step_interval"`

	// ErrorRateThreshold triggers rollback when canary error rate exceeds this (percent).
	ErrorRateThreshold float64 `json:"error_rate_threshold"`

	// LatencyThreshold triggers rollback when P99 latency exceeds this (ms).
	LatencyThreshold float64 `json:"latency_threshold_ms"`

	// MinReadyDuration is the minimum time the canary must be healthy before promotion.
	MinReadyDuration time.Duration `json:"min_ready_duration"`

	// MaxFailedChecks is the maximum consecutive health check failures before rollback.
	MaxFailedChecks int `json:"max_failed_checks"`

	// AutoPromote enables automatic promotion when all checks pass.
	AutoPromote bool `json:"auto_promote"`

	// AutoRollback enables automatic rollback on failure.
	AutoRollback bool `json:"auto_rollback"`
}

// DefaultCanaryConfig returns production-grade canary defaults.
func DefaultCanaryConfig() CanaryConfig {
	return CanaryConfig{
		InitialWeight:      5,
		ProgressionSteps:   []int{5, 10, 25, 50, 100},
		StepInterval:       5 * time.Minute,
		ErrorRateThreshold: 1.0,
		LatencyThreshold:   500.0,
		MinReadyDuration:   2 * time.Minute,
		MaxFailedChecks:    3,
		AutoPromote:        true,
		AutoRollback:       true,
	}
}

// MetricsProvider supplies deployment metrics for health evaluation.
type MetricsProvider interface {
	// GetErrorRate returns the current error rate (percent) for a deployment track.
	GetErrorRate(ctx context.Context, component, track string) (float64, error)
	// GetP99Latency returns the P99 latency (ms) for a deployment track.
	GetP99Latency(ctx context.Context, component, track string) (float64, error)
	// GetReadyReplicas returns the number of ready replicas.
	GetReadyReplicas(ctx context.Context, component, track string) (int, error)
}

// DeploymentExecutor performs actual Kubernetes deployment operations.
type DeploymentExecutor interface {
	// DeployCanary deploys a canary version with the given image.
	DeployCanary(ctx context.Context, component, image string, replicas int) error
	// SetTrafficWeight configures traffic split between stable and canary.
	SetTrafficWeight(ctx context.Context, component string, canaryWeight int) error
	// PromoteCanary replaces stable with canary.
	PromoteCanary(ctx context.Context, component string) error
	// RollbackCanary removes canary and restores 100% stable.
	RollbackCanary(ctx context.Context, component string) error
	// GetCurrentImage returns the current stable image for a component.
	GetCurrentImage(ctx context.Context, component string) (string, error)
}

// CanaryController manages canary deployment lifecycle.
type CanaryController struct {
	config   CanaryConfig
	metrics  MetricsProvider
	executor DeploymentExecutor
	logger   *logrus.Logger

	// Active deployments
	active map[string]*DeploymentStatus
	mu     sync.RWMutex
}

// NewCanaryController creates a new canary deployment controller.
func NewCanaryController(cfg CanaryConfig, metrics MetricsProvider, executor DeploymentExecutor, logger *logrus.Logger) *CanaryController {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &CanaryController{
		config:   cfg,
		metrics:  metrics,
		executor: executor,
		logger:   logger,
		active:   make(map[string]*DeploymentStatus),
	}
}

// StartCanary initiates a canary deployment for a component.
func (cc *CanaryController) StartCanary(ctx context.Context, component, newImage string) (*DeploymentStatus, error) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if existing, ok := cc.active[component]; ok {
		if existing.Phase == PhaseCanary || existing.Phase == PhaseProgressing {
			return nil, fmt.Errorf("canary already in progress for %s (phase: %s)", component, existing.Phase)
		}
	}

	// Get current stable image
	stableImage, err := cc.executor.GetCurrentImage(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("failed to get current image: %w", err)
	}

	// Deploy canary
	if err := cc.executor.DeployCanary(ctx, component, newImage, 1); err != nil {
		return nil, fmt.Errorf("failed to deploy canary: %w", err)
	}

	// Set initial traffic weight
	if err := cc.executor.SetTrafficWeight(ctx, component, cc.config.InitialWeight); err != nil {
		return nil, fmt.Errorf("failed to set traffic weight: %w", err)
	}

	status := &DeploymentStatus{
		Component:     component,
		Phase:         PhaseCanary,
		Health:        HealthUnknown,
		StableVersion: stableImage,
		CanaryVersion: newImage,
		CanaryWeight:  cc.config.InitialWeight,
		StartedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Message:       fmt.Sprintf("Canary started with %d%% traffic", cc.config.InitialWeight),
	}
	cc.active[component] = status

	cc.logger.WithFields(logrus.Fields{
		"component":      component,
		"canary_image":   newImage,
		"stable_image":   stableImage,
		"initial_weight": cc.config.InitialWeight,
	}).Info("Canary deployment started")

	return status, nil
}

// EvaluateCanary checks canary health and progresses or rolls back.
func (cc *CanaryController) EvaluateCanary(ctx context.Context, component string) (*DeploymentStatus, error) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	status, ok := cc.active[component]
	if !ok {
		return nil, fmt.Errorf("no active canary for %s", component)
	}

	if status.Phase != PhaseCanary && status.Phase != PhaseProgressing {
		return status, nil
	}

	// Collect metrics
	errorRate, err := cc.metrics.GetErrorRate(ctx, component, "canary")
	if err != nil {
		cc.logger.WithError(err).Warn("Failed to get canary error rate")
		errorRate = 0
	}
	p99Latency, err := cc.metrics.GetP99Latency(ctx, component, "canary")
	if err != nil {
		cc.logger.WithError(err).Warn("Failed to get canary P99 latency")
		p99Latency = 0
	}

	status.ErrorRate = errorRate
	status.P99LatencyMs = p99Latency
	status.UpdatedAt = time.Now()

	// Check health thresholds
	if errorRate > cc.config.ErrorRateThreshold {
		status.Health = HealthUnhealthy
		status.Message = fmt.Sprintf("Error rate %.2f%% exceeds threshold %.1f%%", errorRate, cc.config.ErrorRateThreshold)
		if cc.config.AutoRollback {
			return cc.doRollback(ctx, status)
		}
		return status, nil
	}

	if p99Latency > cc.config.LatencyThreshold {
		status.Health = HealthDegraded
		status.Message = fmt.Sprintf("P99 latency %.0fms exceeds threshold %.0fms", p99Latency, cc.config.LatencyThreshold)
		if cc.config.AutoRollback {
			return cc.doRollback(ctx, status)
		}
		return status, nil
	}

	status.Health = HealthHealthy

	// Progress to next weight step
	if cc.config.AutoPromote {
		return cc.progressCanary(ctx, status)
	}

	return status, nil
}

func (cc *CanaryController) progressCanary(ctx context.Context, status *DeploymentStatus) (*DeploymentStatus, error) {
	currentWeight := status.CanaryWeight
	var nextWeight int

	for _, step := range cc.config.ProgressionSteps {
		if step > currentWeight {
			nextWeight = step
			break
		}
	}

	if nextWeight == 0 || currentWeight >= 100 {
		// Fully promoted
		return cc.doPromote(ctx, status)
	}

	// Increase weight
	if err := cc.executor.SetTrafficWeight(ctx, status.Component, nextWeight); err != nil {
		return nil, fmt.Errorf("failed to set weight to %d%%: %w", nextWeight, err)
	}

	status.CanaryWeight = nextWeight
	status.Phase = PhaseProgressing
	status.Message = fmt.Sprintf("Traffic increased to %d%%", nextWeight)

	cc.logger.WithFields(logrus.Fields{
		"component": status.Component,
		"weight":    nextWeight,
	}).Info("Canary traffic increased")

	return status, nil
}

func (cc *CanaryController) doPromote(ctx context.Context, status *DeploymentStatus) (*DeploymentStatus, error) {
	status.Phase = PhasePromoting

	if err := cc.executor.PromoteCanary(ctx, status.Component); err != nil {
		status.Phase = PhaseFailed
		status.Message = fmt.Sprintf("Promotion failed: %v", err)
		return status, err
	}

	status.Phase = PhaseComplete
	status.CanaryWeight = 0
	status.StableVersion = status.CanaryVersion
	status.CanaryVersion = ""
	status.Message = "Canary promoted to stable"

	cc.logger.WithField("component", status.Component).Info("Canary promoted to stable")
	return status, nil
}

func (cc *CanaryController) doRollback(ctx context.Context, status *DeploymentStatus) (*DeploymentStatus, error) {
	status.Phase = PhaseRollingBack

	if err := cc.executor.RollbackCanary(ctx, status.Component); err != nil {
		status.Phase = PhaseFailed
		status.Message = fmt.Sprintf("Rollback failed: %v", err)
		return status, err
	}

	status.Phase = PhaseRolledBack
	status.CanaryWeight = 0
	status.CanaryVersion = ""
	status.Message = fmt.Sprintf("Rolled back: error_rate=%.2f%%, p99=%.0fms", status.ErrorRate, status.P99LatencyMs)

	cc.logger.WithFields(logrus.Fields{
		"component":  status.Component,
		"error_rate": status.ErrorRate,
		"p99":        status.P99LatencyMs,
	}).Warn("Canary rolled back")

	return status, nil
}

// GetStatus returns the current status of a canary deployment.
func (cc *CanaryController) GetStatus(component string) (*DeploymentStatus, bool) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	status, ok := cc.active[component]
	return status, ok
}

// ListActive returns all active canary deployments.
func (cc *CanaryController) ListActive() []*DeploymentStatus {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	result := make([]*DeploymentStatus, 0, len(cc.active))
	for _, s := range cc.active {
		result = append(result, s)
	}
	return result
}

// ============================================================================
// Rollback Controller — automated deployment health monitoring
// ============================================================================

// RollbackConfig configures automatic rollback behavior.
type RollbackConfig struct {
	// CheckInterval is how often to check deployment health.
	CheckInterval time.Duration `json:"check_interval"`

	// ErrorRateThreshold triggers rollback when exceeded.
	ErrorRateThreshold float64 `json:"error_rate_threshold"`

	// LatencyThreshold triggers rollback when P99 exceeds this (ms).
	LatencyThreshold float64 `json:"latency_threshold_ms"`

	// ConsecutiveFailures is the number of consecutive failed checks before rollback.
	ConsecutiveFailures int `json:"consecutive_failures"`

	// CooldownPeriod prevents multiple rollbacks in quick succession.
	CooldownPeriod time.Duration `json:"cooldown_period"`

	// MaxRevisionHistory is how many rollback revisions to keep.
	MaxRevisionHistory int `json:"max_revision_history"`
}

// DefaultRollbackConfig returns production rollback defaults.
func DefaultRollbackConfig() RollbackConfig {
	return RollbackConfig{
		CheckInterval:       30 * time.Second,
		ErrorRateThreshold:  5.0,
		LatencyThreshold:    1000.0,
		ConsecutiveFailures: 3,
		CooldownPeriod:      10 * time.Minute,
		MaxRevisionHistory:  10,
	}
}

// RollbackEvent records a rollback action.
type RollbackEvent struct {
	Component     string          `json:"component"`
	FromVersion   string          `json:"from_version"`
	ToVersion     string          `json:"to_version"`
	Reason        string          `json:"reason"`
	ErrorRate     float64         `json:"error_rate"`
	P99LatencyMs  float64         `json:"p99_latency_ms"`
	Timestamp     time.Time       `json:"timestamp"`
	Automatic     bool            `json:"automatic"`
}

// RollbackController monitors deployments and triggers automatic rollback.
type RollbackController struct {
	config   RollbackConfig
	metrics  MetricsProvider
	executor DeploymentExecutor
	logger   *logrus.Logger

	// State tracking
	failureCounts map[string]int       // component → consecutive failure count
	lastRollback  map[string]time.Time // component → last rollback time
	events        []RollbackEvent
	mu            sync.RWMutex
}

// NewRollbackController creates a new rollback controller.
func NewRollbackController(cfg RollbackConfig, metrics MetricsProvider, executor DeploymentExecutor, logger *logrus.Logger) *RollbackController {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &RollbackController{
		config:        cfg,
		metrics:       metrics,
		executor:      executor,
		logger:        logger,
		failureCounts: make(map[string]int),
		lastRollback:  make(map[string]time.Time),
	}
}

// CheckHealth evaluates deployment health and triggers rollback if needed.
func (rc *RollbackController) CheckHealth(ctx context.Context, component string) (*DeploymentHealth, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Check cooldown
	if lastRB, ok := rc.lastRollback[component]; ok {
		if time.Since(lastRB) < rc.config.CooldownPeriod {
			health := HealthUnknown
			return &health, nil
		}
	}

	errorRate, err := rc.metrics.GetErrorRate(ctx, component, "stable")
	if err != nil {
		rc.logger.WithError(err).WithField("component", component).Debug("Failed to get error rate")
		health := HealthUnknown
		return &health, nil
	}

	p99Latency, err := rc.metrics.GetP99Latency(ctx, component, "stable")
	if err != nil {
		rc.logger.WithError(err).WithField("component", component).Debug("Failed to get latency")
		health := HealthUnknown
		return &health, nil
	}

	// Evaluate thresholds
	unhealthy := false
	var reason string

	if errorRate > rc.config.ErrorRateThreshold {
		unhealthy = true
		reason = fmt.Sprintf("error rate %.2f%% > %.1f%%", errorRate, rc.config.ErrorRateThreshold)
	}
	if p99Latency > rc.config.LatencyThreshold {
		unhealthy = true
		reason += fmt.Sprintf("; P99 %.0fms > %.0fms", p99Latency, rc.config.LatencyThreshold)
	}

	if unhealthy {
		rc.failureCounts[component]++
		rc.logger.WithFields(logrus.Fields{
			"component":      component,
			"consecutive":    rc.failureCounts[component],
			"error_rate":     errorRate,
			"p99_latency_ms": p99Latency,
		}).Warn("Unhealthy deployment detected")

		if rc.failureCounts[component] >= rc.config.ConsecutiveFailures {
			// Trigger rollback
			rc.logger.WithField("component", component).Error("Triggering automatic rollback")
			if rbErr := rc.executor.RollbackCanary(ctx, component); rbErr != nil {
				rc.logger.WithError(rbErr).Error("Automatic rollback failed")
			}

			event := RollbackEvent{
				Component:    component,
				Reason:       reason,
				ErrorRate:    errorRate,
				P99LatencyMs: p99Latency,
				Timestamp:    time.Now(),
				Automatic:    true,
			}
			rc.events = append(rc.events, event)
			rc.lastRollback[component] = time.Now()
			rc.failureCounts[component] = 0

			health := HealthUnhealthy
			return &health, nil
		}

		health := HealthDegraded
		return &health, nil
	}

	// Reset failure count on healthy check
	rc.failureCounts[component] = 0
	health := HealthHealthy
	return &health, nil
}

// RunMonitorLoop runs the continuous health monitoring loop.
func (rc *RollbackController) RunMonitorLoop(ctx context.Context, components []string) {
	ticker := time.NewTicker(rc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, component := range components {
				rc.CheckHealth(ctx, component)
			}
		}
	}
}

// GetEvents returns rollback events history.
func (rc *RollbackController) GetEvents() []RollbackEvent {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	result := make([]RollbackEvent, len(rc.events))
	copy(result, rc.events)
	return result
}

// ============================================================================
// Deployment Manager — multi-component orchestration
// ============================================================================

// ComponentOrder defines the deployment order for CloudAI Fusion components.
var ComponentOrder = []string{
	"apiserver",
	"scheduler",
	"agent",
	"ai-engine",
}

// DeploymentPlan describes a multi-component deployment.
type DeploymentPlan struct {
	ID         string            `json:"id"`
	Components map[string]string `json:"components"` // component → image
	Strategy   string            `json:"strategy"`   // "canary", "rolling", "blue-green"
	CreatedAt  time.Time         `json:"created_at"`
}

// DeploymentManager orchestrates multi-component deployments.
type DeploymentManager struct {
	canary   *CanaryController
	rollback *RollbackController
	logger   *logrus.Logger
}

// NewDeploymentManager creates a new deployment manager.
func NewDeploymentManager(canary *CanaryController, rollback *RollbackController, logger *logrus.Logger) *DeploymentManager {
	return &DeploymentManager{
		canary:   canary,
		rollback: rollback,
		logger:   logger,
	}
}

// ExecutePlan deploys all components in the plan in the correct order.
func (dm *DeploymentManager) ExecutePlan(ctx context.Context, plan *DeploymentPlan) error {
	for _, component := range ComponentOrder {
		image, ok := plan.Components[component]
		if !ok {
			continue
		}

		dm.logger.WithFields(logrus.Fields{
			"component": component,
			"image":     image,
			"strategy":  plan.Strategy,
		}).Info("Deploying component")

		switch plan.Strategy {
		case "canary":
			if _, err := dm.canary.StartCanary(ctx, component, image); err != nil {
				return fmt.Errorf("canary deploy failed for %s: %w", component, err)
			}
		default:
			dm.logger.WithField("strategy", plan.Strategy).Warn("Strategy not implemented, using canary")
			if _, err := dm.canary.StartCanary(ctx, component, image); err != nil {
				return fmt.Errorf("deploy failed for %s: %w", component, err)
			}
		}
	}
	return nil
}
