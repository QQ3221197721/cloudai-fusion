// Package prometheus implements metrics collection and alerting integration
package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Metrics Collector Implementation
// ============================================================================

// MetricsCollector collects and exposes system metrics
type MetricsCollector struct {
	logger *logrus.Logger
	
	// Core metrics
	requestCount     *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	errorCount       *prometheus.CounterVec
	activeExploits   *prometheus.GaugeVec
	cveScanResults   *prometheus.GaugeVec
	
	// Custom business metrics
	exploitSuccessRate *prometheus.Gauge
	edrBypassSuccess   *prometheus.Gauge
	kerberosTickets    *prometheus.Gauge
}

// NewMetricsCollector creates metrics collector instance
func NewMetricsCollector(logger *logrus.Logger) *MetricsCollector {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &MetricsCollector{
		logger: logger.WithField("component", "metrics_collector"),
		
		// Initialize core metrics
		requestCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cloudai_fusion_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"endpoint", "method"},
		),
		
		requestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "cloudai_fusion_request_duration_seconds",
				Help:    "Request duration histogram",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"endpoint"},
		),
		
		errorCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cloudai_fusion_errors_total",
				Help: "Total number of errors",
			},
			[]string{"type"},
		),
		
		activeExploits: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "cloudai_fusion_active_exploits",
				Help: "Number of currently active exploits",
			},
			[]string{"cve_id"},
		),
		
		cveScanResults: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "cloudai_fusion_cve_scan_results",
				Help: "CVE scan result count",
			},
			[]string{"severity"},
		),
		
		// Business metrics
		exploitSuccessRate: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "cloudai_fusion_exploit_success_rate",
				Help: "Overall exploit success rate percentage",
			},
		),
		
		edrBypassSuccess: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "cloudai_fusion_edr_bypass_success",
				Help: "EDR bypass success rate",
			},
		),
		
		kerberosTickets: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "cloudai_fusion_kerberos_tickets_issued",
				Help: "Total Kerberos tickets issued",
			},
		),
	}
}

// ============================================================================
// Metrics Export Endpoint
// ============================================================================

// ExportHandler exposes metrics endpoint for Prometheus scraping
func (mc *MetricsCollector) ExportHandler(w http.ResponseWriter, r *http.Request) {
	mc.logger.Debug("Prometheus scrape request received")
	
	// Prometheus client library handles this automatically
	prometheus.DefaultGatherer.Collect()
	
	// Serve metrics in Prometheus format
	prometheus.Handler().ServeHTTP(w, r)
}

// ============================================================================
// Alert Rules Configuration
// ============================================================================

// AlertRule defines threshold-based alerts
type AlertRule struct {
	Name          string
	Metric        string
	Condition     string // e.g., ">95", "<50", "==100"
	Threshold     float64
	Duration      time.Duration
	Severity      string
	Description   string
	Action        func(metricValue float64) error
}

// AlertConfig holds alert rules configuration
type AlertConfig struct {
	Rules  []AlertRule
	Channel string // Slack/DingTalk webhook URL
}

// NewAlertConfig creates default alert configuration
func NewAlertConfig(webhookURL string) *AlertConfig {
	return &AlertConfig{
		Rules: []AlertRule{
			{
				Name:      "High Error Rate",
				Metric:    "cloudai_fusion_errors_total",
				Condition: ">=",
				Threshold: 100,
				Duration:  time.Minute * 5,
				Severity:  "critical",
				Description: "Error rate exceeds 100 in 5 minutes",
			},
			{
				Name:      "Low Exploit Success",
				Metric:    "cloudai_fusion_exploit_success_rate",
				Condition: "<",
				Threshold: 80.0,
				Duration:  time.Minute * 15,
				Severity:  "high",
				Description: "Exploit success rate below 80%",
			},
			{
				Name:      "Elevated Memory Usage",
				Metric:    "process_resident_memory_bytes",
				Condition: ">=",
				Threshold: 2e+09, // 2GB
				Duration:  time.Minute * 10,
				Severity:  "medium",
				Description: "Memory usage exceeds 2GB",
			},
		},
		Channel: webhookURL,
	}
}

// ============================================================================
// Grafana Dashboard Integration
// ============================================================================

// DashboardProvider generates Grafana dashboard JSON
type DashboardProvider struct {
	logger   *logrus.Logger
	baseURL  string
	apiToken string
}

// NewDashboardProvider creates Grafana dashboard provider
func NewDashboardProvider(baseURL, apiToken string, logger *logrus.Logger) *DashboardProvider {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &DashboardProvider{
		logger:   logger.WithField("component", "grafana_provider"),
		baseURL:  baseURL,
		apiToken: apiToken,
	}
}

// GenerateOverviewDashboard creates overview dashboard JSON
func (dp *DashboardProvider) GenerateOverviewDashboard(ctx context.Context) ([]byte, error) {
	dp.logger.Info("Generating overview dashboard...")
	
	dashboard := map[string]interface{}{
		"title":   "CloudAI Fusion Overview",
		"panels":  dp.createPanels(),
		"time":    map[string]string{"from": "now-6h", "to": "now"},
		"timezone": "browser",
	}
	
	// Marshal to JSON
	data, err := json.MarshalIndent(dashboard, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dashboard: %w", err)
	}
	
	return data, nil
}

func (dp *DashboardProvider) createPanels() []map[string]interface{} {
	panels := make([]map[string]interface{}, 0)
	
	// Request rate panel
	panels = append(panels, map[string]interface{}{
		"id":    1,
		"title": "Request Rate",
		"type":  "graph",
		"targets": []map[string]string{
			{
				"expr":  `rate(cloudai_fusion_requests_total[5m])`,
				"legend": `{{endpoint}}/{{method}}`,
			},
		},
	})
	
	// Success rate panel
	panels = append(panels, map[string]interface{}{
		"id":    2,
		"title": "Exploit Success Rate",
		"type":  "gauge",
		"targets": []map[string]string{
			{
				"expr": `cloudai_fusion_exploit_success_rate`,
			},
		},
		"thresholds": map[string]interface{}{
			"steps": []map[string]interface{}{
				{"color": "red", "value": 0},
				{"color": "yellow", "value": 80},
				{"color": "green", "value": 90},
			},
		},
	})
	
	// Active exploits panel
	panels = append(panels, map[string]interface{}{
		"id":    3,
		"title": "Active Exploits",
		"type":  "table",
		"targets": []map[string]string{
			{
				"expr": `cloudai_fusion_active_exploits`,
			},
		},
	})
	
	return panels
}

// UploadDashboard pushes dashboard to Grafana
func (dp *DashboardProvider) UploadDashboard(ctx context.Context, dashboardJSON []byte) error {
	// POST to Grafana API /api/dashboards/db
	url := fmt.Sprintf("%s/api/dashboards/db", dp.baseURL)
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(dashboardJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", dp.apiToken))
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("grafana returned status %d", resp.StatusCode)
	}
	
	dp.logger.Info("Dashboard uploaded successfully")
	return nil
}

// ============================================================================
// Performance Monitoring
// ============================================================================

// PerformanceMonitor tracks system performance metrics
type PerformanceMonitor struct {
	logger *logrus.Logger
	startTime time.Time
	metrics map[string]float64
}

// NewPerformanceMonitor creates performance monitor
func NewPerformanceMonitor(logger *logrus.Logger) *PerformanceMonitor {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &PerformanceMonitor{
		logger:    logger.WithField("component", "perf_monitor"),
		startTime: time.Now(),
		metrics:   make(map[string]float64),
	}
}

// RecordMetric records a custom metric
func (pm *PerformanceMonitor) RecordMetric(name string, value float64) {
	pm.metrics[name] = value
	pm.logger.Debugf("Recorded metric: %s = %.2f", name, value)
}

// GetUptime returns system uptime
func (pm *PerformanceMonitor) GetUptime() time.Duration {
	return time.Since(pm.startTime)
}

// GetCPUUsage returns current CPU usage percentage
func (pm *PerformanceMonitor) GetCPUUsage() float64 {
	// Would use runtime.ReadMemStats or similar
	return pm.metrics["cpu_usage"]
}
