// Package monitor provides unified monitoring, alerting, and observability
// through Prometheus metrics, OpenTelemetry tracing, and intelligent
// anomaly detection integration.
package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// ============================================================================
// Configuration
// ============================================================================

// ServiceConfig holds monitoring service configuration
type ServiceConfig struct {
	PrometheusEndpoint string
	JaegerEndpoint     string
	MetricsPort        int
}

// ============================================================================
// Metrics Definitions
// ============================================================================

var (
	// API metrics
	apiRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "api",
			Name:      "requests_total",
			Help:      "Total number of API requests",
		},
		[]string{"method", "path", "status"},
	)

	apiRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "api",
			Name:      "request_duration_seconds",
			Help:      "API request duration in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15),
		},
		[]string{"method", "path"},
	)

	// Cluster metrics
	clusterCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "cluster",
			Name:      "total_count",
			Help:      "Total number of managed clusters",
		},
	)

	clusterNodeCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "cluster",
			Name:      "node_count",
			Help:      "Number of nodes per cluster",
		},
		[]string{"cluster_id", "cluster_name"},
	)

	clusterHealthStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "cluster",
			Name:      "health_status",
			Help:      "Cluster health status (1=healthy, 0=unhealthy)",
		},
		[]string{"cluster_id", "cluster_name"},
	)

	// GPU metrics
	gpuUtilization = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "gpu",
			Name:      "utilization_percent",
			Help:      "GPU utilization percentage",
		},
		[]string{"cluster_id", "node_name", "gpu_index", "gpu_type"},
	)

	gpuMemoryUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "gpu",
			Name:      "memory_used_bytes",
			Help:      "GPU memory usage in bytes",
		},
		[]string{"cluster_id", "node_name", "gpu_index", "gpu_type"},
	)

	gpuTemperature = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "gpu",
			Name:      "temperature_celsius",
			Help:      "GPU temperature in Celsius",
		},
		[]string{"cluster_id", "node_name", "gpu_index"},
	)

	gpuPowerUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "gpu",
			Name:      "power_usage_watts",
			Help:      "GPU power consumption in watts",
		},
		[]string{"cluster_id", "node_name", "gpu_index"},
	)

	// Scheduler metrics
	schedulerQueueLength = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "scheduler",
			Name:      "queue_length",
			Help:      "Number of workloads in scheduling queue",
		},
	)

	schedulerRunningWorkloads = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "scheduler",
			Name:      "running_workloads",
			Help:      "Number of currently running workloads",
		},
	)

	schedulerDecisionDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "cloudai",
			Subsystem: "scheduler",
			Name:      "decision_duration_seconds",
			Help:      "Time taken for a scheduling decision",
			Buckets:   prometheus.ExponentialBuckets(0.01, 2, 10),
		},
	)

	schedulerPreemptionsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "scheduler",
			Name:      "preemptions_total",
			Help:      "Total number of workload preemptions",
		},
	)

	// Cost metrics
	costTotalHourly = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "cloudai",
			Subsystem: "cost",
			Name:      "hourly_usd",
			Help:      "Estimated hourly cost in USD",
		},
		[]string{"cluster_id", "provider"},
	)

	costSavingsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "cost",
			Name:      "monitor_savings_total_usd",
			Help:      "Total cost savings achieved in USD (monitor)",
		},
	)

	// Security metrics
	securityEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "security",
			Name:      "events_total",
			Help:      "Total security events detected",
		},
		[]string{"severity", "type"},
	)

	// Agent metrics
	agentActionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cloudai",
			Subsystem: "agent",
			Name:      "actions_total",
			Help:      "Total actions taken by AI agents",
		},
		[]string{"agent_type", "action"},
	)
)

func init() {
	prometheus.MustRegister(
		apiRequestsTotal, apiRequestDuration,
		clusterCount, clusterNodeCount, clusterHealthStatus,
		gpuUtilization, gpuMemoryUsage, gpuTemperature, gpuPowerUsage,
		schedulerQueueLength, schedulerRunningWorkloads,
		schedulerDecisionDuration, schedulerPreemptionsTotal,
		costTotalHourly, costSavingsTotal,
		securityEventsTotal,
		agentActionsTotal,
	)
}

// ============================================================================
// Alert Models
// ============================================================================

// AlertRule defines a monitoring alert rule
type AlertRule struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	ClusterID    string            `json:"cluster_id,omitempty"`
	Severity     AlertSeverity     `json:"severity"`
	Condition    string            `json:"condition"`
	Threshold    float64           `json:"threshold"`
	Duration     time.Duration     `json:"duration"`
	Channels     []string          `json:"notification_channels"`
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// AlertSeverity defines alert severity levels
type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertEvent represents a triggered alert
type AlertEvent struct {
	ID         string            `json:"id"`
	RuleID     string            `json:"rule_id"`
	RuleName   string            `json:"rule_name"`
	ClusterID  string            `json:"cluster_id,omitempty"`
	Severity   AlertSeverity     `json:"severity"`
	Message    string            `json:"message"`
	Status     string            `json:"status"` // firing, resolved
	Labels     map[string]string `json:"labels,omitempty"`
	FiredAt    time.Time         `json:"fired_at"`
	ResolvedAt *time.Time        `json:"resolved_at,omitempty"`
}

// ============================================================================
// Service
// ============================================================================

// Service provides monitoring and alerting capabilities
type Service struct {
	config     ServiceConfig
	alertRules []*AlertRule          // in-memory cache
	events     []*AlertEvent         // in-memory cache
	store      *store.Store          // DB persistence (nil = in-memory only)
	logger     *logrus.Logger
	mu         sync.RWMutex
}

// NewService creates a new monitoring service
func NewService(cfg ServiceConfig) (*Service, error) {
	svc := &Service{
		config:     cfg,
		alertRules: defaultAlertRules(),
		events:     make([]*AlertEvent, 0),
		logger:     logrus.StandardLogger(),
	}
	return svc, nil
}

// SetStore injects a database store for persistent alert management
func (s *Service) SetStore(st *store.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = st
	if st != nil {
		s.loadFromDB()
	}
}

// loadFromDB populates the in-memory cache from PostgreSQL
func (s *Service) loadFromDB() {
	if s.store == nil {
		return
	}
	ruleModels, _, err := s.store.ListAlertRules(0, 1000)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to load alert rules from DB")
	} else if len(ruleModels) > 0 {
		rules := make([]*AlertRule, 0, len(ruleModels))
		for _, m := range ruleModels {
			rules = append(rules, alertRuleModelToRule(&m))
		}
		s.alertRules = rules
		s.logger.WithField("count", len(ruleModels)).Info("Loaded alert rules from database")
	}
	eventModels, err := s.store.ListAlertEvents(100)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to load alert events from DB")
	} else if len(eventModels) > 0 {
		events := make([]*AlertEvent, 0, len(eventModels))
		for _, m := range eventModels {
			events = append(events, alertEventModelToEvent(&m))
		}
		s.events = events
		s.logger.WithField("count", len(eventModels)).Info("Loaded alert events from database")
	}
}

// Start begins the monitoring service (metrics endpoint, alert evaluation)
func (s *Service) Start(ctx context.Context) {
	// Start metrics HTTP server
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		addr := fmt.Sprintf(":%d", s.config.MetricsPort)
		server := &http.Server{Addr: addr, Handler: mux}
		s.logger.WithField("addr", addr).Info("Metrics endpoint started")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error("Metrics server failed")
		}
	}()

	// Start alert evaluation loop
	go s.evaluateAlerts(ctx)
}

// GetAlertRules returns all configured alert rules (DB-first with cache fallback)
func (s *Service) GetAlertRules() []*AlertRule {
	if s.store != nil {
		models, _, err := s.store.ListAlertRules(0, 1000)
		if err == nil {
			rules := make([]*AlertRule, 0, len(models))
			for _, m := range models {
				rules = append(rules, alertRuleModelToRule(&m))
			}
			return rules
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alertRules
}

// GetRecentEvents returns recent alert events (DB-first with cache fallback)
func (s *Service) GetRecentEvents(limit int) []*AlertEvent {
	if s.store != nil {
		models, err := s.store.ListAlertEvents(limit)
		if err == nil {
			events := make([]*AlertEvent, 0, len(models))
			for _, m := range models {
				events = append(events, alertEventModelToEvent(&m))
			}
			return events
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit > len(s.events) {
		limit = len(s.events)
	}
	return s.events[len(s.events)-limit:]
}

// RecordAPIRequest records an API request metric
func RecordAPIRequest(method, path, status string, duration time.Duration) {
	apiRequestsTotal.WithLabelValues(method, path, status).Inc()
	apiRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
}

// UpdateGPUMetrics updates GPU utilization metrics
func UpdateGPUMetrics(clusterID, nodeName, gpuIndex, gpuType string, util, memUsed float64, temp int, power float64) {
	gpuUtilization.WithLabelValues(clusterID, nodeName, gpuIndex, gpuType).Set(util)
	gpuMemoryUsage.WithLabelValues(clusterID, nodeName, gpuIndex, gpuType).Set(memUsed)
	gpuTemperature.WithLabelValues(clusterID, nodeName, gpuIndex).Set(float64(temp))
	gpuPowerUsage.WithLabelValues(clusterID, nodeName, gpuIndex).Set(power)
}

func (s *Service) evaluateAlerts(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Alert evaluation logic - in production would query Prometheus
			s.logger.Debug("Evaluating alert rules")
		}
	}
}

func defaultAlertRules() []*AlertRule {
	return []*AlertRule{
		{
			ID: "gpu-high-util", Name: "GPU High Utilization",
			Description: "GPU utilization above 95% for 5 minutes",
			Severity: AlertSeverityWarning, Condition: "gpu_utilization > 95",
			Threshold: 95, Duration: 5 * time.Minute, Status: "active",
		},
		{
			ID: "gpu-memory-exhaustion", Name: "GPU Memory Near Exhaustion",
			Description: "GPU memory usage above 90%",
			Severity: AlertSeverityCritical, Condition: "gpu_memory_usage > 90",
			Threshold: 90, Duration: 2 * time.Minute, Status: "active",
		},
		{
			ID: "cluster-unreachable", Name: "Cluster Unreachable",
			Description: "Cluster API server not responding",
			Severity: AlertSeverityCritical, Condition: "cluster_health == 0",
			Threshold: 0, Duration: 1 * time.Minute, Status: "active",
		},
		{
			ID: "scheduling-queue-buildup", Name: "Scheduling Queue Buildup",
			Description: "More than 50 workloads pending in queue",
			Severity: AlertSeverityWarning, Condition: "scheduler_queue_length > 50",
			Threshold: 50, Duration: 10 * time.Minute, Status: "active",
		},
		{
			ID: "cost-spike", Name: "Cost Spike Detected",
			Description: "Hourly cost increased by more than 50%",
			Severity: AlertSeverityWarning, Condition: "cost_increase_pct > 50",
			Threshold: 50, Duration: 30 * time.Minute, Status: "active",
		},
	}
}

// ============================================================================
// DB <-> Domain Conversion
// ============================================================================

func alertRuleModelToRule(m *store.AlertRuleModel) *AlertRule {
	rule := &AlertRule{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		ClusterID:   m.ClusterID,
		Severity:    AlertSeverity(m.Severity),
		Condition:   m.Condition,
		Threshold:   m.Threshold,
		Duration:    time.Duration(m.DurationSec) * time.Second,
		Status:      m.Status,
	}
	if m.Channels != "" && m.Channels != "[]" {
		if err := json.Unmarshal([]byte(m.Channels), &rule.Channels); err != nil {
			logrus.WithError(err).WithField("rule", m.ID).Debug("Failed to unmarshal alert rule channels")
		}
	}
	if m.Labels != "" && m.Labels != "{}" {
		if err := json.Unmarshal([]byte(m.Labels), &rule.Labels); err != nil {
			logrus.WithError(err).WithField("rule", m.ID).Debug("Failed to unmarshal alert rule labels")
		}
	}
	return rule
}

func alertEventModelToEvent(m *store.AlertEventModel) *AlertEvent {
	event := &AlertEvent{
		ID:         m.ID,
		RuleID:     m.RuleID,
		RuleName:   m.RuleName,
		ClusterID:  m.ClusterID,
		Severity:   AlertSeverity(m.Severity),
		Message:    m.Message,
		Status:     m.Status,
		FiredAt:    m.FiredAt,
		ResolvedAt: m.ResolvedAt,
	}
	if m.Labels != "" && m.Labels != "{}" {
		if err := json.Unmarshal([]byte(m.Labels), &event.Labels); err != nil {
			logrus.WithError(err).WithField("event", m.ID).Debug("Failed to unmarshal alert event labels")
		}
	}
	return event
}
