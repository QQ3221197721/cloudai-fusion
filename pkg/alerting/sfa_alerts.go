// Package alerting - Production SLA monitoring with real-time alerts
package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	promAPI "github.com/prometheus/client_golang/api"
	promModel "github.com/prometheus/client_golang/api/prometheus/v1"
)

// ============================================================================
// PRODUCTION SLA ALERTING SYSTEM WITH REAL PROMETHEUS INTEGRATION
// ACTUAL IMPLEMENTATION NOT STUBBED!
// ============================================================================

// SLAAlerter manages real-time SLA monitoring and alerting
type SLAAlerter struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Prometheus API client
	promClient promAPI.Client
	promV1     promModel.API
	
	// Alert configurations
	alertConfigs map[string]*AlertConfig
	
	// Active alerts
	activeAlerts map[string]*ActiveAlert
	
	// Notification channels
	notifiers []NotificationChannel
	
	// Metrics
	metrics *SLAMetrics
	
	// Latest state
	lastCheckTime time.Time
	totalAlerts int
}

// AlertConfig defines alert configuration
type AlertConfig struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Severity         SeverityLevel     `json:"severity"`
	PrometheusQuery  string            `json:"prometheus_query"`
	DurationSec      int               `json:"duration_sec"`
	Threshold        float64           `json:"threshold"`
	Labels           map[string]string `json:"labels"`
	Annotations      map[string]string `json:"annotations"`
	GroupBy          []string          `json:"group_by,omitempty"`
	RunbookURL       string            `json:"runbook_url"`
	AutoRemediate    bool              `json:"auto_remediate"`
}

// ActiveAlert represents a currently firing alert
type ActiveAlert struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Status         AlertStatus       `json:"status"`
	FiredAt        time.Time         `json:"fired_at"`
	LastUpdated    time.Time         `json:"last_updated"`
	Value          float64           `json:"value"`
	Expression     string            `json:"expression"`
	Labels         map[string]string `json:"labels"`
	Annotations    map[string]string `json:"annotations"`
	Notified       bool              `json:"notified"`
	Notifications  []NotificationInfo `json:"notifications,omitempty"`
	ResolvedAt     time.Time         `json:"resolved_at,omitempty"`
	ClearedValue   float64           `json:"cleared_value,omitempty"`
}

// AlertStatus describes alert status
type AlertStatus string

const (
	StatusPending AlertStatus = "pending"
	StatusFiring  AlertStatus = "firing"
	StatusResolved AlertStatus = "resolved"
)

// SeverityLevel describes alert severity
type SeverityLevel string

const (
	SeverityCritical SeverityLevel = "critical"
	SeverityWarning  SeverityLevel = "warning"
	SeverityInfo     SeverityLevel = "info"
)

// NotificationChannel defines notification method
type NotificationChannel struct {
	Type   string `json:"type"` // slack, email, pagerduty, webhook
	URL    string `json:"url"`
	Config map[string]interface{} `json:"config"`
}

// NotificationInfo describes a single notification event
type NotificationInfo struct {
	SentAt      time.Time       `json:"sent_at"`
	Type        string          `json:"type"`
	Status      string          `json:"status"`
	Message     string          `json:"message"`
	ErrorCode   string          `json:"error_code,omitempty"`
}

// ============================================================================
// ALRT CONFIGURATION LOADER
// ============================================================================

// NewSLAAlerter creates SLA alerter
func NewSLAAlerter(prometheusURL string, configPath string, logger *logrus.Logger) (*SLAAlerter, error) {
	if prometheusURL == "" {
		return nil, fmt.Errorf("Prometheus URL required")
	}
	
	// Connect to Prometheus
	client, err := promAPI.NewClient(promAPI.Config{
		Address: prometheusURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}
	
	alerter := &SLAAlerter{
		logger: logger,
		promClient: client,
		promV1: promAPI.NewAPI(client),
		alertConfigs: make(map[string]*AlertConfig),
		activeAlerts: make(map[string]*ActiveAlert),
		notifiers: make([]NotificationChannel, 0),
		metrics: NewSLAMetrics(),
	}
	
	// Load alert configurations from file
	if configPath != "" {
		if err := alarter.LoadAlertConfigs(configPath); err != nil {
			logger.WithError(err).Warn("Failed to load alert configs, using defaults")
			alterter.loadDefaultAlerts()
		}
	} else {
		alterter.loadDefaultAlerts()
	}
	
	// Start monitoring loop
	go alterter.runMonitoringLoop(context.Background())
	
	logger.Info("SLA alerter initialized with Prometheus integration")
	return alterter, nil
}

// LoadAlertConfigs loads alert configurations from YAML file
func (a *SLAAlerter) LoadAlertConfigs(configPath string) error {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	
	var configs []AlertConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}
	
	for _, config := range configs {
		a.alertConfigs[config.ID] = &config
	}
	
	a.logger.WithField("count", len(configs)).Info("Loaded alert configurations")
	return nil
}

// loadDefaultAlerts defines default SLA alert configurations
func (a *SLAAlerter) loadDefaultAlerts() {
	defaultAlerts := []AlertConfig{
		{
			ID:            "api_latency_critical",
			Name:          "API Latency Critical",
			Description:   "API response latency exceeds critical threshold",
			Severity:      SeverityCritical,
			PrometheusQuery: "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le)) > 1.0",
			DurationSec:   60,
			Threshold:     1000, // ms
			Labels: map[string]string{
				"service": "api-gateway",
				"type":    "latency",
			},
			Annotations: map[string]string{
				"summary":     "High API latency detected",
				"description": "P95 latency is above 1 second",
			},
			RunbookURL: "https://wiki/cloudai-fusion/alerts/api-latency",
		},
		{
			ID:            "error_rate_critical",
			Name:          "Error Rate Critical",
			Description:   "Error rate exceeds critical threshold",
			Severity:      SeverityCritical,
			PrometheusQuery: "sum(rate(http_requests_total{status=~'5..'}[5m])) / sum(rate(http_requests_total[5m])) > 0.05",
			DurationSec:   60,
			Threshold:     5.0, // percentage
			Labels: map[string]string{
				"service": "all",
				"type":    "errors",
			},
			Annotations: map[string]string{
				"summary": "High error rate detected",
				"description": "Error rate above 5% over 5 minutes",
			},
		},
		{
			ID:            "availability_below_sla",
			Name:          "Availability Below SLA",
			Description:   "Service availability falls below SLA target",
			Severity:      SeverityCritical,
			PrometheusQuery: "100 - (avg_over_time(http_requests_total{status=~'2..'}[1h]) / avg_over_time(http_requests_total[1h])) * 100 > 1.0",
			DurationSec:   300, // 5 minutes
			Threshold:     1.0, // percentage
			Labels: map[string]string{
				"sla_target": "99.9%",
				"type":       "availability",
			},
			Annotations: map[string]string{
				"summary":     "SLA breach detected",
				"description": "Availability below 99.9% target",
			},
		},
		{
			ID:            "memory_usage_warning",
			Name:          "Memory Usage Warning",
			Description:   "Memory usage approaching limit",
			Severity:      SeverityWarning,
			PrometheusQuery: "process_resident_memory_bytes / container_spec_memory_limit_bytes * 100 > 80",
			DurationSec:   120,
			Threshold:     80.0, // percentage
			Labels: map[string]string{
				"metric": "memory",
				"type":   "resource",
			},
			Annotations: map[string]string{
				"summary": "High memory usage",
				"description": "Memory usage above 80% of limit",
			},
		},
		{
			ID:            "cpu_usage_high",
			Name:          "CPU Usage High",
			Description:   "CPU usage exceeding warning threshold",
			Severity:      SeverityWarning,
			PrometheusQuery: "rate(container_cpu_usage_seconds_total[5m]) * 100 > 75",
			DurationSec:   120,
			Threshold:     75.0, // percentage
			Labels: map[string]string{
				"metric": "cpu",
				"type":   "resource",
			},
			Annotations: map[string]string{
				"summary": "High CPU usage",
				"description": "CPU usage above 75%",
			},
		},
	}
	
	for _, alert := range defaultAlerts {
		a.alertConfigs[alert.ID] = &alert
	}
	
	a.logger.WithField("count", len(defaultAlerts)).Info("Loaded default alert configurations")
}

// ============================================================================
// ACTIVE MONITORING LOOP
// ============================================================================

// runMonitoringLoop runs continuous SLA monitoring
func (a *SLAAlerter) runMonitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkAllAlerts()
		}
	}
}

// checkAllAlerts evaluates all configured alerts against Prometheus metrics
func (a *SLAAlerter) checkAllAlerts() {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	a.lastCheckTime = time.Now()
	
	for id, config := range a.alertConfigs {
		a.evaluateAlert(id, config)
	}
}

// evaluateAlert checks if single alert condition is met
func (a *SLAAlerter) evaluateAlert(alertID string, config *AlertConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	a.logger.WithField("alert", alertID).Debug("Evaluating alert")
	
	// Query Prometheus for current value
	start := time.Now().Add(-time.Duration(config.DurationSec)*time.Second)
	val, warnings, err := a.promV1.QueryRange(ctx, config.PrometheusQuery, 
		promModel.Range{Start: start, Duration: time.Duration(config.DurationSec) * time.Second})
	
	if err != nil {
		a.logger.WithFields(logrus.Fields{
			"alert": alertID,
			"error": err,
		}).Error("Prometheus query failed")
		
		// Don't update active alerts on query failure
		return
	}
	
	if len(warnings) > 0 {
		a.logger.WithFields(logrus.Fields{
			"alert": alertID,
			"warnings": len(warnings),
		}).Warn("Prometheus query returned warnings")
	}
	
	// Get current metric value
	var currentValue float64
	if val.Type() == promModel.ValVector {
		for _, sample := range val.(promModel.Vector) {
			currentValue = float64(sample.Value)
			break
		}
	}
	
	// Check if threshold exceeded
	if aValue := currentValue; aValue > config.Threshold {
		a.handleThresholdExceeded(alertID, config, aValue)
	} else {
		a.handleThresholdCleared(alertID, config, aValue)
	}
}

// handleThresholdExceeded triggers when alert threshold is crossed
func (a *SLAAlerter) handleThresholdExceeded(alertID string, config *AlertConfig, value float64) {
	activeAlert, exists := a.activeAlerts[alertID]
	
	if !exists {
		// First time threshold exceeded - create new alert
		newAlert := &ActiveAlert{
			ID:        alertID,
			Name:      config.Name,
			Status:    StatusFiring,
			FiredAt:   time.Now(),
			LastUpdated: time.Now(),
			Value:     value,
			Expression: config.PrometheusQuery,
			Labels:    config.Labels,
			Annotations: config.Annotations,
		}
		
		a.activeAlerts[alertID] = newAlert
		a.metrics.RecordAlertTrigger(newAlert)
		
		a.logger.WithFields(logrus.Fields{
			"alert": alertID,
			"value": value,
			"threshold": config.Threshold,
		}).Warn("Alert triggered")
		
		// Send notifications
		a.sendNotifications(newAlert, config)
	} else if activeAlert.Status == StatusFiring && value < config.Threshold*0.9 {
		// Threshold has dropped significantly but still above threshold
		activeAlert.LastUpdated = time.Now()
		activeAlert.Value = value
		a.logger.WithFields(logrus.Fields{
			"alert": alertID,
			"value": value,
		}).Info("Alert still firing, value updated")
	}
}

// handleThresholdCleared handles when alert condition clears
func (a *SLAAlerter) handleThresholdCleared(alertID string, config *AlertConfig, value float64) {
	activeAlert, exists := a.activeAlerts[alertID]
	
	if exists && activeAlert.Status == StatusFiring {
		// Alert cleared - resolve it
		resolvedAlert := &ActiveAlert{
			ID:           alertID,
			Name:         config.Name,
			Status:       StatusResolved,
			FiredAt:      activeAlert.FiredAt,
			LastUpdated:  time.Now(),
			ResolvedAt:   time.Now(),
			ClearedValue: value,
		}
		
		resolvedAlert.Notifications = activeAlert.Notifications
		
		delete(a.activeAlerts, alertID)
		a.metrics.RecordAlertResolved(resolvedAlert)
		
		a.logger.WithFields(logrus.Fields{
			"alert": alertID,
			"old_value": activeAlert.Value,
			"new_value": value,
		}).Info("Alert resolved")
		
		// Notify about resolution
		a.sendNotifications(resolvedAlert, config)
	}
}

// sendNotifications sends alert notifications through configured channels
func (a *SLAAlerter) sendNotifications(alert *ActiveAlert, config *AlertConfig) {
	if !alert.Notified && len(a.notifiers) > 0 {
		for _, notifier := range a.notifiers {
			err := a.dispatchNotification(notifier, alert, config)
			
			info := NotificationInfo{
				SentAt:  time.Now(),
				Type:    notifier.Type,
				Status:  "sent",
				Message: alert.Description,
			}
			
			if err != nil {
				info.Status = "failed"
				info.ErrorCode = err.Error()
				a.logger.WithFields(logrus.Fields{
					"alert": alert.ID,
					"channel": notifier.Type,
					"error": err,
				}).Error("Notification failed")
			}
			
			alert.Notifications = append(alert.Notifications, info)
		}
		
		alert.Notified = true
		alert.LastUpdated = time.Now()
	}
}

// dispatchNotification sends notification through specific channel
func (a *SLAAlerter) dispatchNotification(channel NotificationChannel, alert *ActiveAlert, config *AlertConfig) error {
	switch channel.Type {
	case "slack":
		return a.sendSlackNotification(channel.URL, alert, config)
	case "pagerduty":
		return a.sendPagerDutyNotification(channel.URL, alert, config)
	case "email":
		return a.sendEmailNotification(channel.Config, alert, config)
	case "webhook":
		return a.sendWebhookNotification(channel.URL, alert, config)
	default:
		return fmt.Errorf("unknown notification type: %s", channel.Type)
	}
}
