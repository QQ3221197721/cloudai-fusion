// Package disasterrecovery provides CloudAI Fusion plugins for the
// cross-cloud PostgreSQL disaster recovery system.  Three plugin roles:
//
//   - DRCollectorPlugin  → monitor.collector  (replication lag, RPO/RTO metrics)
//   - DRAlerterPlugin    → monitor.alerter    (failover alerts via Slack/DingTalk)
//   - DRWebhookPlugin    → webhook.validating (failover decision validation)
//
// These plugins integrate the pg-disaster-recovery monitoring capabilities
// into the CloudAI Fusion observability and safety pipeline.
package disasterrecovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// ============================================================================
// Shared types & config
// ============================================================================

// DRConfig holds connection details for primary and standby databases.
type DRConfig struct {
	// PrimaryHost is the Alibaba Cloud RDS PostgreSQL endpoint.
	PrimaryHost string `json:"primary_host" yaml:"primaryHost"`
	// StandbyHost is the Azure Flexible PostgreSQL endpoint.
	StandbyHost string `json:"standby_host" yaml:"standbyHost"`
	// LagThresholdSeconds triggers alerts when exceeded.
	LagThresholdSeconds int `json:"lag_threshold_seconds" yaml:"lagThresholdSeconds"`
	// SlackWebhook is the Slack incoming webhook URL for alerts.
	SlackWebhook string `json:"slack_webhook,omitempty" yaml:"slackWebhook,omitempty"`
	// DingtalkWebhook is the DingTalk robot webhook URL for alerts.
	DingtalkWebhook string `json:"dingtalk_webhook,omitempty" yaml:"dingtalkWebhook,omitempty"`
}

var drHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// validateDRHost checks that a host:port string is safe to connect to.
func validateDRHost(hostPort string) error {
	host := hostPort
	// Strip port if present.
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	// Block metadata endpoints.
	if host == "169.254.169.254" || host == "metadata.google.internal" {
		return fmt.Errorf("blocked metadata endpoint: %s", host)
	}
	// Resolve and check IP ranges.
	ips, err := net.LookupIP(host)
	if err != nil {
		// Allow K8s service names.
		if strings.Contains(host, ".") || len(host) <= 63 {
			return nil
		}
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("host %q resolves to blocked IP %s", host, ip)
		}
		// Block AWS/Alibaba metadata.
		metadataIP := net.ParseIP("169.254.169.254")
		if ip.Equal(metadataIP) {
			return fmt.Errorf("host %q resolves to blocked metadata IP", host)
		}
	}
	return nil
}

// validateWebhookURL validates a webhook URL to prevent SSRF.
func validateWebhookURL(rawURL string) error {
	if rawURL == "" {
		return nil // optional
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL %q: %w", rawURL, err)
	}
	if u.Scheme != "https" {
		// Allow http for internal/testing, but warn.
		if u.Scheme != "http" {
			return fmt.Errorf("webhook URL %q must use http or https", rawURL)
		}
	}
	return validateDRHost(u.Host)
}

// ============================================================================
// 1. DRCollectorPlugin — monitor.collector
// ============================================================================

// DRCollectorPlugin collects replication lag, RPO/RTO, and consistency metrics
// from the pg-disaster-recovery monitoring endpoints.
type DRCollectorPlugin struct {
	plugin.BasePlugin
	config DRConfig

	// cached metrics for webhook validation
	mu             sync.RWMutex
	lastLagSeconds int
	lastPrimaryUp  bool
	lastStandbyUp  bool
	lastCheckedAt  time.Time
}

// NewDRCollectorPlugin creates the disaster-recovery collector.
func NewDRCollectorPlugin(config DRConfig) (plugin.Plugin, error) {
	return &DRCollectorPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "dr-collector",
			Version:     "1.0.0",
			Description: "Collects PostgreSQL cross-cloud DR metrics: replication lag, RPO/RTO, consistency",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtMonitorCollector,
			},
			Priority: 100,
			Tags:     map[string]string{"category": "disaster-recovery", "tier": "contrib"},
		}),
		config: config,
	}, nil
}

func (p *DRCollectorPlugin) Init(_ context.Context, config map[string]interface{}) error {
	if v, ok := config["primary_host"].(string); ok {
		p.config.PrimaryHost = v
	}
	if v, ok := config["standby_host"].(string); ok {
		p.config.StandbyHost = v
	}
	if v, ok := config["lag_threshold_seconds"].(float64); ok {
		p.config.LagThresholdSeconds = int(v)
	}
	// Validate hosts to prevent SSRF.
	if p.config.PrimaryHost != "" {
		if err := validateDRHost(p.config.PrimaryHost); err != nil {
			return fmt.Errorf("invalid primary host: %w", err)
		}
	}
	if p.config.StandbyHost != "" {
		if err := validateDRHost(p.config.StandbyHost); err != nil {
			return fmt.Errorf("invalid standby host: %w", err)
		}
	}
	return nil
}

func (p *DRCollectorPlugin) Health(ctx context.Context) error {
	// Check if primary is reachable.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s:8080/health", p.config.PrimaryHost), nil)
	if err != nil {
		return err
	}
	resp, err := drHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DR primary monitor unreachable: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("DR primary monitor returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// MetricNames lists the metrics this collector produces.
func (p *DRCollectorPlugin) MetricNames() []string {
	return []string{
		"dr_replication_lag_seconds",
		"dr_primary_healthy",
		"dr_standby_healthy",
		"dr_rpo_seconds",
		"dr_rto_seconds",
		"dr_consistency_check_passed",
	}
}

// Collect scrapes the DR monitoring endpoint and returns metrics.
func (p *DRCollectorPlugin) Collect(ctx context.Context) ([]plugin.MetricSample, error) {
	now := time.Now()
	status, err := p.fetchDRStatus(ctx)
	if err != nil {
		// Return degraded metrics on error.
		return []plugin.MetricSample{
			{Name: "dr_primary_healthy", Value: 0, Timestamp: now, Labels: drLabels(p.config, "primary")},
			{Name: "dr_standby_healthy", Value: 0, Timestamp: now, Labels: drLabels(p.config, "standby")},
		}, nil
	}

	// Update cached state.
	p.mu.Lock()
	p.lastLagSeconds = status.ReplicationLagSeconds
	p.lastPrimaryUp = status.PrimaryHealthy
	p.lastStandbyUp = status.StandbyHealthy
	p.lastCheckedAt = now
	p.mu.Unlock()

	samples := []plugin.MetricSample{
		{
			Name: "dr_replication_lag_seconds", Value: float64(status.ReplicationLagSeconds),
			Timestamp: now, Labels: drLabels(p.config, "replication"), Unit: "seconds",
		},
		{
			Name: "dr_primary_healthy", Value: boolToFloat(status.PrimaryHealthy),
			Timestamp: now, Labels: drLabels(p.config, "primary"),
		},
		{
			Name: "dr_standby_healthy", Value: boolToFloat(status.StandbyHealthy),
			Timestamp: now, Labels: drLabels(p.config, "standby"),
		},
		{
			Name: "dr_rpo_seconds", Value: float64(status.RPOSeconds),
			Timestamp: now, Labels: drLabels(p.config, "rpo"), Unit: "seconds",
		},
		{
			Name: "dr_rto_seconds", Value: float64(status.RTOSeconds),
			Timestamp: now, Labels: drLabels(p.config, "rto"), Unit: "seconds",
		},
		{
			Name: "dr_consistency_check_passed", Value: boolToFloat(status.ConsistencyOK),
			Timestamp: now, Labels: drLabels(p.config, "consistency"),
		},
	}
	return samples, nil
}

// GetLastStatus returns the most recently collected DR status (thread-safe).
func (p *DRCollectorPlugin) GetLastStatus() (lagSeconds int, primaryUp, standbyUp bool, checkedAt time.Time) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastLagSeconds, p.lastPrimaryUp, p.lastStandbyUp, p.lastCheckedAt
}

// DRStatus is the JSON response from the DR monitoring endpoint.
type DRStatus struct {
	PrimaryHealthy        bool `json:"primary_healthy"`
	StandbyHealthy        bool `json:"standby_healthy"`
	ReplicationLagSeconds int  `json:"replication_lag_seconds"`
	RPOSeconds            int  `json:"rpo_seconds"`
	RTOSeconds            int  `json:"rto_seconds"`
	ConsistencyOK         bool `json:"consistency_ok"`
}

// fetchDRStatus calls the DR monitoring HTTP endpoint.
func (p *DRCollectorPlugin) fetchDRStatus(ctx context.Context) (*DRStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s:8080/dr/status", p.config.PrimaryHost), nil)
	if err != nil {
		return nil, err
	}
	resp, err := drHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("DR status endpoint returned HTTP %d", resp.StatusCode)
	}

	var status DRStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode DR status: %w", err)
	}
	return &status, nil
}

func drLabels(cfg DRConfig, role string) map[string]string {
	return map[string]string{
		"primary": cfg.PrimaryHost,
		"standby": cfg.StandbyHost,
		"role":    role,
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// ============================================================================
// 2. DRAlerterPlugin — monitor.alerter
// ============================================================================

// DRAlerterPlugin sends disaster-recovery alerts to Slack and DingTalk.
type DRAlerterPlugin struct {
	plugin.BasePlugin
	config DRConfig
}

// NewDRAlerterPlugin creates the DR alerter.
func NewDRAlerterPlugin(config DRConfig) (plugin.Plugin, error) {
	return &DRAlerterPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "dr-alerter",
			Version:     "1.0.0",
			Description: "Sends PostgreSQL DR failover alerts to Slack and DingTalk",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtMonitorAlerter,
			},
			Priority: 50, // high priority for DR alerts
			Tags:     map[string]string{"category": "disaster-recovery", "tier": "contrib"},
		}),
		config: config,
	}, nil
}

func (p *DRAlerterPlugin) Init(_ context.Context, config map[string]interface{}) error {
	if v, ok := config["slack_webhook"].(string); ok {
		p.config.SlackWebhook = v
	}
	if v, ok := config["dingtalk_webhook"].(string); ok {
		p.config.DingtalkWebhook = v
	}
	// Validate webhook URLs to prevent SSRF.
	if err := validateWebhookURL(p.config.SlackWebhook); err != nil {
		return fmt.Errorf("invalid Slack webhook: %w", err)
	}
	if err := validateWebhookURL(p.config.DingtalkWebhook); err != nil {
		return fmt.Errorf("invalid DingTalk webhook: %w", err)
	}
	return nil
}

func (p *DRAlerterPlugin) Health(_ context.Context) error { return nil }

// SupportedChannels returns the notification channels this alerter supports.
func (p *DRAlerterPlugin) SupportedChannels() []string {
	channels := []string{}
	if p.config.SlackWebhook != "" {
		channels = append(channels, "slack")
	}
	if p.config.DingtalkWebhook != "" {
		channels = append(channels, "dingtalk")
	}
	return channels
}

// SendAlert dispatches an alert to configured channels.
func (p *DRAlerterPlugin) SendAlert(ctx context.Context, alert *plugin.Alert) error {
	// Determine severity color.
	color := "#00FF00"
	switch alert.Severity {
	case "critical":
		color = "#FF0000"
	case "warning":
		color = "#FFA500"
	}

	// Send to Slack.
	if p.config.SlackWebhook != "" {
		if err := p.sendSlackAlert(ctx, alert, color); err != nil {
			return fmt.Errorf("slack alert failed: %w", err)
		}
	}

	// Send to DingTalk.
	if p.config.DingtalkWebhook != "" {
		if err := p.sendDingtalkAlert(ctx, alert); err != nil {
			return fmt.Errorf("dingtalk alert failed: %w", err)
		}
	}

	return nil
}

func (p *DRAlerterPlugin) sendSlackAlert(ctx context.Context, alert *plugin.Alert, color string) error {
	payload := map[string]interface{}{
		"text": fmt.Sprintf("[DR Alert] %s", alert.Name),
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"text":  alert.Message,
				"ts":    alert.FiredAt.Unix(),
			},
		},
	}
	return p.postJSON(ctx, p.config.SlackWebhook, payload)
}

func (p *DRAlerterPlugin) sendDingtalkAlert(ctx context.Context, alert *plugin.Alert) error {
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": fmt.Sprintf("[DR Alert] %s", alert.Name),
			"text":  fmt.Sprintf("### [DR Alert] %s\n%s", alert.Name, alert.Message),
		},
	}
	return p.postJSON(ctx, p.config.DingtalkWebhook, payload)
}

func (p *DRAlerterPlugin) postJSON(ctx context.Context, webhookURL string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := drHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ============================================================================
// 3. DRWebhookPlugin — webhook.validating
// ============================================================================

// DRWebhookPlugin validates failover decisions by checking DR status before
// allowing the platform to proceed with database-related operations.
type DRWebhookPlugin struct {
	plugin.BasePlugin
	config    DRConfig
	collector *DRCollectorPlugin // back-reference to get cached status
}

// NewDRWebhookPlugin creates the DR validating webhook.
func NewDRWebhookPlugin(config DRConfig, collector *DRCollectorPlugin) (plugin.Plugin, error) {
	return &DRWebhookPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "dr-webhook",
			Version:     "1.0.0",
			Description: "Validates failover decisions and blocks operations during DR transitions",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtWebhookValidating,
			},
			Priority:     10, // run very early for safety
			Dependencies: []string{"dr-collector"},
			Tags:         map[string]string{"category": "disaster-recovery", "tier": "contrib"},
		}),
		config:    config,
		collector: collector,
	}, nil
}

func (p *DRWebhookPlugin) Init(_ context.Context, _ map[string]interface{}) error { return nil }
func (p *DRWebhookPlugin) Health(_ context.Context) error                         { return nil }

// Validate checks if a failover-related operation should be allowed.
// It implements the webhook validation logic inline (not HTTP-based) since
// it has direct access to the collector's cached state.
func (p *DRWebhookPlugin) Validate(ctx context.Context, req *plugin.WebhookRequest) (*plugin.WebhookResponse, error) {
	// Parse the operation from the request.
	var op FailoverOperation
	if err := json.Unmarshal(req.Object, &op); err != nil {
		return &plugin.WebhookResponse{
			UID:     req.UID,
			Allowed: false,
			Result:  plugin.ErrorResult(p.Metadata().Name, fmt.Errorf("invalid operation: %w", err)),
		}, nil
	}

	// Get current DR status.
	lag, primaryUp, standbyUp, _ := p.collector.GetLastStatus()

	switch op.Action {
	case "failover":
		// Failover is only allowed if primary is down AND standby is up.
		if primaryUp {
			return &plugin.WebhookResponse{
				UID:     req.UID,
				Allowed: false,
				Result:  plugin.NewResult(plugin.Skip, p.Metadata().Name, "primary still healthy, failover not needed"),
			}, nil
		}
		if !standbyUp {
			return &plugin.WebhookResponse{
				UID:     req.UID,
				Allowed: false,
				Result:  plugin.ErrorResult(p.Metadata().Name, fmt.Errorf("standby is also down, cannot failover")),
			}, nil
		}
		// Check replication lag is acceptable.
		if lag > p.config.LagThresholdSeconds*10 {
			return &plugin.WebhookResponse{
				UID:     req.UID,
				Allowed: false,
				Result:  plugin.NewResult(plugin.Unschedulable, p.Metadata().Name, fmt.Sprintf("replication lag %ds too high for safe failover", lag)),
			}, nil
		}
		return &plugin.WebhookResponse{
			UID:     req.UID,
			Allowed: true,
			Result:  plugin.SuccessResult(p.Metadata().Name),
		}, nil

	case "rollback":
		// Rollback is allowed if original primary is back online.
		if !primaryUp {
			return &plugin.WebhookResponse{
				UID:     req.UID,
				Allowed: false,
				Result:  plugin.NewResult(plugin.Wait, p.Metadata().Name, "original primary not yet recovered"),
			}, nil
		}
		return &plugin.WebhookResponse{
			UID:     req.UID,
			Allowed: true,
			Result:  plugin.SuccessResult(p.Metadata().Name),
		}, nil

	default:
		return &plugin.WebhookResponse{
			UID:     req.UID,
			Allowed: true,
			Result:  plugin.SuccessResult(p.Metadata().Name),
		}, nil
	}
}

// FailoverOperation describes a failover/rollback operation request.
type FailoverOperation struct {
	Action      string `json:"action"`       // "failover" or "rollback"
	SourceHost  string `json:"source_host"`  // current primary
	TargetHost  string `json:"target_host"`  // target primary
	ConfirmCode string `json:"confirm_code"` // safety confirmation
}
