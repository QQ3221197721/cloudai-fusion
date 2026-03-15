package metrics

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Error Budget Prometheus Metrics
// ============================================================================

var (
	// ErrorBudgetConsumed tracks cumulative error budget consumption ratio (0-1).
	ErrorBudgetConsumed = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "error_budget",
			Name:      "consumed_ratio",
			Help:      "Fraction of error budget consumed (0 = none, 1 = exhausted)",
		},
		[]string{"service", "period"},
	)

	// ErrorBudgetPolicy tracks which policy level is currently active.
	ErrorBudgetPolicy = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "error_budget",
			Name:      "policy_level",
			Help:      "Active error budget policy level (0=normal, 1=warning, 2=critical, 3=exhausted)",
		},
		[]string{"service"},
	)

	// AlertConvergenceTotal tracks alert convergence operations.
	AlertConvergenceTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "alert_convergence",
			Name:      "operations_total",
			Help:      "Total alert convergence operations",
		},
		[]string{"action"}, // deduplicated, grouped, silenced, escalated
	)

	// AlertConvergenceActiveAlerts tracks currently active (deduplicated) alerts.
	AlertConvergenceActiveAlerts = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "alert_convergence",
			Name:      "active_alerts",
			Help:      "Number of currently active deduplicated alerts",
		},
	)

	// AlertConvergenceSuppressedTotal tracks total suppressed (noisy) alerts.
	AlertConvergenceSuppressedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "alert_convergence",
			Name:      "suppressed_total",
			Help:      "Total suppressed alerts due to convergence",
		},
	)
)

// ============================================================================
// Error Budget Policy
// ============================================================================

// BudgetPolicyLevel indicates the severity level of budget consumption.
type BudgetPolicyLevel int

const (
	BudgetPolicyNormal   BudgetPolicyLevel = 0 // < 50% consumed
	BudgetPolicyWarning  BudgetPolicyLevel = 1 // 50-80% consumed
	BudgetPolicyCritical BudgetPolicyLevel = 2 // 80-100% consumed
	BudgetPolicyExhausted BudgetPolicyLevel = 3 // 100% consumed
)

// String returns the policy level name.
func (l BudgetPolicyLevel) String() string {
	switch l {
	case BudgetPolicyNormal:
		return "normal"
	case BudgetPolicyWarning:
		return "warning"
	case BudgetPolicyCritical:
		return "critical"
	case BudgetPolicyExhausted:
		return "exhausted"
	default:
		return "unknown"
	}
}

// BudgetPolicy defines actions at each consumption threshold.
type BudgetPolicy struct {
	Level              BudgetPolicyLevel `json:"level"`
	ThresholdConsumed  float64           `json:"threshold_consumed"` // 0.0-1.0
	FreezeDeployments  bool              `json:"freeze_deployments"`
	RequireApproval    bool              `json:"require_approval"`
	AlertSeverity      string            `json:"alert_severity"` // info, warning, critical, page
	NotifyChannels     []string          `json:"notify_channels"`
	Description        string            `json:"description"`
}

// DefaultBudgetPolicies returns the standard graduated error budget policies.
func DefaultBudgetPolicies() []BudgetPolicy {
	return []BudgetPolicy{
		{
			Level: BudgetPolicyNormal, ThresholdConsumed: 0.0,
			FreezeDeployments: false, RequireApproval: false,
			AlertSeverity: "info", NotifyChannels: []string{"slack-sre"},
			Description: "Error budget healthy — normal operations",
		},
		{
			Level: BudgetPolicyWarning, ThresholdConsumed: 0.50,
			FreezeDeployments: false, RequireApproval: false,
			AlertSeverity: "warning", NotifyChannels: []string{"slack-sre", "email-oncall"},
			Description: "Error budget 50% consumed — monitor closely",
		},
		{
			Level: BudgetPolicyCritical, ThresholdConsumed: 0.80,
			FreezeDeployments: true, RequireApproval: true,
			AlertSeverity: "critical", NotifyChannels: []string{"slack-sre", "pagerduty"},
			Description: "Error budget 80% consumed — freeze non-critical deployments",
		},
		{
			Level: BudgetPolicyExhausted, ThresholdConsumed: 1.00,
			FreezeDeployments: true, RequireApproval: true,
			AlertSeverity: "critical", NotifyChannels: []string{"slack-sre", "pagerduty", "email-leadership"},
			Description: "Error budget exhausted — all deployments frozen, incident response required",
		},
	}
}

// ============================================================================
// Error Budget Manager
// ============================================================================

// ErrorBudgetConfig configures the error budget manager.
type ErrorBudgetConfig struct {
	SLODefinitions []SLODefinition `json:"slo_definitions"`
	Policies       []BudgetPolicy  `json:"policies"`
	EvalInterval   time.Duration   `json:"eval_interval"`
	Logger         *logrus.Logger
}

// ErrorBudgetManager tracks error budget consumption and enforces policies.
type ErrorBudgetManager struct {
	config    ErrorBudgetConfig
	budgets   map[string]*ServiceBudget // service -> budget state
	policies  []BudgetPolicy
	logger    *logrus.Logger
	mu        sync.RWMutex
	cancel    context.CancelFunc
}

// ServiceBudget tracks budget state for a single service.
type ServiceBudget struct {
	Service            string            `json:"service"`
	Target             float64           `json:"target"`              // availability target
	Window             time.Duration     `json:"window"`              // budget window (e.g. 30d)
	TotalRequests      int64             `json:"total_requests"`
	ErrorRequests      int64             `json:"error_requests"`
	ConsumedRatio      float64           `json:"consumed_ratio"`      // 0.0-1.0
	RemainingRatio     float64           `json:"remaining_ratio"`     // 0.0-1.0
	ActivePolicy       BudgetPolicyLevel `json:"active_policy"`
	BurnRates          map[string]float64 `json:"burn_rates"`         // window -> rate
	DeploymentsFrozen  bool              `json:"deployments_frozen"`
	LastEvaluatedAt    time.Time         `json:"last_evaluated_at"`
	PolicyTransitions  []PolicyTransition `json:"policy_transitions,omitempty"`
}

// PolicyTransition records a policy level change.
type PolicyTransition struct {
	From      BudgetPolicyLevel `json:"from"`
	To        BudgetPolicyLevel `json:"to"`
	Consumed  float64           `json:"consumed_ratio"`
	Timestamp time.Time         `json:"timestamp"`
}

// NewErrorBudgetManager creates a new error budget manager.
func NewErrorBudgetManager(cfg ErrorBudgetConfig) *ErrorBudgetManager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.EvalInterval == 0 {
		cfg.EvalInterval = 30 * time.Second
	}
	if len(cfg.SLODefinitions) == 0 {
		cfg.SLODefinitions = DefaultSLOs()
	}
	if len(cfg.Policies) == 0 {
		cfg.Policies = DefaultBudgetPolicies()
	}

	mgr := &ErrorBudgetManager{
		config:   cfg,
		budgets:  make(map[string]*ServiceBudget),
		policies: cfg.Policies,
		logger:   cfg.Logger,
	}

	for _, slo := range cfg.SLODefinitions {
		mgr.budgets[slo.Service] = &ServiceBudget{
			Service:        slo.Service,
			Target:         slo.AvailabilityTarget,
			Window:         slo.Window,
			RemainingRatio: 1.0,
			BurnRates:      map[string]float64{"1h": 0, "6h": 0, "24h": 0},
		}
	}

	cfg.Logger.WithField("services", len(mgr.budgets)).Info("Error budget manager initialized")
	return mgr
}

// RecordRequest records a request for budget tracking.
func (m *ErrorBudgetManager) RecordRequest(service string, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.budgets[service]
	if !ok {
		return
	}

	b.TotalRequests++
	if isError {
		b.ErrorRequests++
	}
}

// Evaluate recalculates all service budgets and determines active policies.
func (m *ErrorBudgetManager) Evaluate() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	for svc, b := range m.budgets {
		if b.TotalRequests == 0 {
			b.ConsumedRatio = 0
			b.RemainingRatio = 1.0
			b.LastEvaluatedAt = now
			continue
		}

		allowedErrorRate := 1.0 - b.Target
		if allowedErrorRate <= 0 {
			allowedErrorRate = 0.0001 // avoid division by zero
		}

		actualErrorRate := float64(b.ErrorRequests) / float64(b.TotalRequests)
		consumed := actualErrorRate / allowedErrorRate
		b.ConsumedRatio = math.Min(consumed, 2.0) // cap at 200%
		b.RemainingRatio = math.Max(0, 1.0-consumed)
		b.LastEvaluatedAt = now

		// Calculate burn rates
		b.BurnRates["1h"] = consumed  // simplified: use overall rate
		b.BurnRates["6h"] = consumed
		b.BurnRates["24h"] = consumed

		// Determine active policy
		oldPolicy := b.ActivePolicy
		newPolicy := m.determinePolicyLevel(b.ConsumedRatio)

		if newPolicy != oldPolicy {
			b.PolicyTransitions = append(b.PolicyTransitions, PolicyTransition{
				From: oldPolicy, To: newPolicy, Consumed: b.ConsumedRatio, Timestamp: now,
			})
			m.logger.WithFields(logrus.Fields{
				"service":  svc,
				"from":     oldPolicy.String(),
				"to":       newPolicy.String(),
				"consumed": fmt.Sprintf("%.1f%%", b.ConsumedRatio*100),
			}).Warn("Error budget policy transition")
		}

		b.ActivePolicy = newPolicy
		b.DeploymentsFrozen = m.isPolicyFreezing(newPolicy)

		// Update Prometheus metrics
		ErrorBudgetConsumed.WithLabelValues(svc, "30d").Set(b.ConsumedRatio)
		ErrorBudgetPolicy.WithLabelValues(svc).Set(float64(newPolicy))
	}
}

// determinePolicyLevel returns the policy level for a given consumption ratio.
func (m *ErrorBudgetManager) determinePolicyLevel(consumed float64) BudgetPolicyLevel {
	level := BudgetPolicyNormal
	for _, p := range m.policies {
		if consumed >= p.ThresholdConsumed {
			level = p.Level
		}
	}
	return level
}

// isPolicyFreezing checks if the given policy level requires deployment freeze.
func (m *ErrorBudgetManager) isPolicyFreezing(level BudgetPolicyLevel) bool {
	for _, p := range m.policies {
		if p.Level == level {
			return p.FreezeDeployments
		}
	}
	return false
}

// GetBudget returns the budget state for a service.
func (m *ErrorBudgetManager) GetBudget(service string) (*ServiceBudget, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.budgets[service]
	if !ok {
		return nil, fmt.Errorf("no budget tracked for service %s", service)
	}
	// Return a copy
	cp := *b
	return &cp, nil
}

// GetAllBudgets returns budget states for all tracked services.
func (m *ErrorBudgetManager) GetAllBudgets() map[string]*ServiceBudget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*ServiceBudget, len(m.budgets))
	for k, v := range m.budgets {
		cp := *v
		result[k] = &cp
	}
	return result
}

// IsDeploymentAllowed checks if deployments are currently allowed for a service.
func (m *ErrorBudgetManager) IsDeploymentAllowed(service string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.budgets[service]
	if !ok {
		return true, "service not tracked"
	}
	if b.DeploymentsFrozen {
		return false, fmt.Sprintf("error budget %s: %.1f%% consumed, policy=%s",
			service, b.ConsumedRatio*100, b.ActivePolicy.String())
	}
	return true, "ok"
}

// Start begins periodic budget evaluation.
func (m *ErrorBudgetManager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)
	go m.evalLoop(ctx)
	m.logger.Info("Error budget manager started")
}

// Stop halts the evaluation loop.
func (m *ErrorBudgetManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *ErrorBudgetManager) evalLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.EvalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Evaluate()
		}
	}
}

// ============================================================================
// Smart Alert Convergence
// ============================================================================

// AlertSeverity defines alert severity levels.
type AlertSeverity string

const (
	AlertInfo     AlertSeverity = "info"
	AlertWarning  AlertSeverity = "warning"
	AlertCritical AlertSeverity = "critical"
	AlertPage     AlertSeverity = "page"
)

// Alert represents an incoming alert.
type Alert struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Severity    AlertSeverity     `json:"severity"`
	Service     string            `json:"service"`
	Source      string            `json:"source"`
	Message     string            `json:"message"`
	Labels      map[string]string `json:"labels,omitempty"`
	FiredAt     time.Time         `json:"fired_at"`
	ResolvedAt  *time.Time        `json:"resolved_at,omitempty"`
}

// AlertGroup represents a group of related alerts.
type AlertGroup struct {
	Key       string    `json:"key"`       // grouping key (e.g. "service:apiserver")
	Alerts    []*Alert  `json:"alerts"`
	Count     int       `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Severity  AlertSeverity `json:"severity"` // highest severity in group
}

// SilenceRule suppresses alerts matching certain criteria.
type SilenceRule struct {
	ID        string            `json:"id"`
	Matchers  map[string]string `json:"matchers"` // label matchers
	Reason    string            `json:"reason"`
	CreatedBy string            `json:"created_by"`
	StartsAt  time.Time         `json:"starts_at"`
	EndsAt    time.Time         `json:"ends_at"`
}

// AlertConvergenceConfig configures the alert convergence engine.
type AlertConvergenceConfig struct {
	GroupByLabels     []string      `json:"group_by_labels"`     // Labels to group alerts by
	GroupWait         time.Duration `json:"group_wait"`          // Wait before sending first group notification
	GroupInterval     time.Duration `json:"group_interval"`      // Interval between group notifications
	RepeatInterval    time.Duration `json:"repeat_interval"`     // Don't re-alert within this interval
	DeduplicationTTL  time.Duration `json:"deduplication_ttl"`   // How long to deduplicate same alerts
	Logger            *logrus.Logger
}

// DefaultAlertConvergenceConfig returns production convergence settings.
func DefaultAlertConvergenceConfig() AlertConvergenceConfig {
	return AlertConvergenceConfig{
		GroupByLabels:    []string{"service", "severity"},
		GroupWait:        30 * time.Second,
		GroupInterval:    5 * time.Minute,
		RepeatInterval:   4 * time.Hour,
		DeduplicationTTL: 10 * time.Minute,
	}
}

// AlertConvergenceEngine implements smart alert convergence.
type AlertConvergenceEngine struct {
	config     AlertConvergenceConfig
	groups     map[string]*AlertGroup    // groupKey -> group
	silences   []*SilenceRule
	dedup      map[string]time.Time      // alertFingerprint -> lastSeen
	logger     *logrus.Logger
	mu         sync.RWMutex
}

// NewAlertConvergenceEngine creates a new convergence engine.
func NewAlertConvergenceEngine(cfg AlertConvergenceConfig) *AlertConvergenceEngine {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.DeduplicationTTL == 0 {
		cfg.DeduplicationTTL = 10 * time.Minute
	}
	if len(cfg.GroupByLabels) == 0 {
		cfg.GroupByLabels = []string{"service", "severity"}
	}

	return &AlertConvergenceEngine{
		config:   cfg,
		groups:   make(map[string]*AlertGroup),
		silences: make([]*SilenceRule, 0),
		dedup:    make(map[string]time.Time),
		logger:   cfg.Logger,
	}
}

// ProcessAlert processes an incoming alert through the convergence pipeline.
// Returns: (shouldNotify bool, groupKey string, reason string)
func (e *AlertConvergenceEngine) ProcessAlert(alert *Alert) (bool, string, string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Step 1: Check silence rules
	if e.isSilenced(alert) {
		AlertConvergenceTotal.WithLabelValues("silenced").Inc()
		AlertConvergenceSuppressedTotal.Inc()
		return false, "", "silenced by rule"
	}

	// Step 2: Deduplication
	fingerprint := e.alertFingerprint(alert)
	if lastSeen, exists := e.dedup[fingerprint]; exists {
		if time.Since(lastSeen) < e.config.DeduplicationTTL {
			AlertConvergenceTotal.WithLabelValues("deduplicated").Inc()
			AlertConvergenceSuppressedTotal.Inc()
			e.dedup[fingerprint] = time.Now()
			return false, "", "deduplicated"
		}
	}
	e.dedup[fingerprint] = time.Now()

	// Step 3: Grouping
	groupKey := e.computeGroupKey(alert)
	group, exists := e.groups[groupKey]
	if !exists {
		group = &AlertGroup{
			Key:       groupKey,
			Alerts:    make([]*Alert, 0),
			FirstSeen: time.Now(),
			Severity:  alert.Severity,
		}
		e.groups[groupKey] = group
	}

	group.Alerts = append(group.Alerts, alert)
	group.Count = len(group.Alerts)
	group.LastSeen = time.Now()

	// Update highest severity
	if severityRank(alert.Severity) > severityRank(group.Severity) {
		group.Severity = alert.Severity
	}

	AlertConvergenceTotal.WithLabelValues("grouped").Inc()
	AlertConvergenceActiveAlerts.Set(float64(len(e.groups)))

	// Step 4: Decide notification
	shouldNotify := group.Count == 1 // Notify on first alert in group
	if !shouldNotify && time.Since(group.FirstSeen) >= e.config.GroupWait {
		shouldNotify = true // Notify after group wait
	}

	return shouldNotify, groupKey, fmt.Sprintf("grouped:%d alerts", group.Count)
}

// AddSilence adds a silence rule.
func (e *AlertConvergenceEngine) AddSilence(rule *SilenceRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.silences = append(e.silences, rule)
	e.logger.WithFields(logrus.Fields{
		"id": rule.ID, "reason": rule.Reason,
		"matchers": len(rule.Matchers),
	}).Info("Silence rule added")
}

// RemoveSilence removes a silence rule by ID.
func (e *AlertConvergenceEngine) RemoveSilence(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, s := range e.silences {
		if s.ID == id {
			e.silences = append(e.silences[:i], e.silences[i+1:]...)
			return true
		}
	}
	return false
}

// GetActiveGroups returns all active alert groups.
func (e *AlertConvergenceEngine) GetActiveGroups() []*AlertGroup {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*AlertGroup, 0, len(e.groups))
	for _, g := range e.groups {
		cp := *g
		result = append(result, &cp)
	}
	return result
}

// GetSilences returns active silence rules.
func (e *AlertConvergenceEngine) GetSilences() []*SilenceRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	now := time.Now()
	result := make([]*SilenceRule, 0)
	for _, s := range e.silences {
		if now.Before(s.EndsAt) {
			result = append(result, s)
		}
	}
	return result
}

// CleanupExpired removes expired dedup entries and resolved alert groups.
func (e *AlertConvergenceEngine) CleanupExpired() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	cleaned := 0
	now := time.Now()

	// Cleanup dedup entries
	for fp, ts := range e.dedup {
		if now.Sub(ts) > e.config.DeduplicationTTL*2 {
			delete(e.dedup, fp)
			cleaned++
		}
	}

	// Cleanup stale groups (no new alerts for 2x repeat interval)
	staleThreshold := e.config.RepeatInterval * 2
	if staleThreshold == 0 {
		staleThreshold = 8 * time.Hour
	}
	for key, g := range e.groups {
		if now.Sub(g.LastSeen) > staleThreshold {
			delete(e.groups, key)
			cleaned++
		}
	}

	// Cleanup expired silences
	active := make([]*SilenceRule, 0)
	for _, s := range e.silences {
		if now.Before(s.EndsAt) {
			active = append(active, s)
		} else {
			cleaned++
		}
	}
	e.silences = active

	if cleaned > 0 {
		AlertConvergenceActiveAlerts.Set(float64(len(e.groups)))
	}
	return cleaned
}

// isSilenced checks if an alert matches any active silence rule.
func (e *AlertConvergenceEngine) isSilenced(alert *Alert) bool {
	now := time.Now()
	for _, s := range e.silences {
		if now.Before(s.StartsAt) || now.After(s.EndsAt) {
			continue
		}
		matched := true
		for k, v := range s.Matchers {
			switch k {
			case "service":
				if alert.Service != v {
					matched = false
				}
			case "name":
				if alert.Name != v {
					matched = false
				}
			case "severity":
				if string(alert.Severity) != v {
					matched = false
				}
			default:
				if alert.Labels != nil {
					if alert.Labels[k] != v {
						matched = false
					}
				} else {
					matched = false
				}
			}
			if !matched {
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// alertFingerprint generates a dedup key for an alert.
func (e *AlertConvergenceEngine) alertFingerprint(alert *Alert) string {
	return fmt.Sprintf("%s:%s:%s", alert.Name, alert.Service, alert.Severity)
}

// computeGroupKey generates a grouping key based on configured labels.
func (e *AlertConvergenceEngine) computeGroupKey(alert *Alert) string {
	parts := make([]string, 0, len(e.config.GroupByLabels))
	for _, label := range e.config.GroupByLabels {
		switch label {
		case "service":
			parts = append(parts, "service="+alert.Service)
		case "severity":
			parts = append(parts, "severity="+string(alert.Severity))
		case "name":
			parts = append(parts, "name="+alert.Name)
		default:
			if alert.Labels != nil {
				if v, ok := alert.Labels[label]; ok {
					parts = append(parts, label+"="+v)
				}
			}
		}
	}
	key := ""
	for i, p := range parts {
		if i > 0 {
			key += ","
		}
		key += p
	}
	return key
}

// severityRank returns a numeric rank for alert severity (higher = more severe).
func severityRank(s AlertSeverity) int {
	switch s {
	case AlertInfo:
		return 0
	case AlertWarning:
		return 1
	case AlertCritical:
		return 2
	case AlertPage:
		return 3
	default:
		return -1
	}
}
