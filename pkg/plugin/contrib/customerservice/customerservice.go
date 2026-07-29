// Package customerservice provides CloudAI Fusion plugins for the
// AI Customer Service platform.  Three plugin roles:
//
//   - CSCollectorPlugin      → monitor.collector      (AI service metrics)
//   - CSWebhookPlugin        → webhook.mutating       (customer message processing)
//   - CSThreatDetectorPlugin → security.threat.detect (anomalous conversation detection)
//
// These plugins integrate the Spring Boot AI customer service into the
// CloudAI Fusion observability, webhook, and security pipelines.
package customerservice

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

// CSConfig holds connection details for the AI customer service.
type CSConfig struct {
	// BaseURL is the Spring Boot service endpoint (e.g. "http://ai-cs:8080").
	BaseURL string `json:"base_url" yaml:"baseURL"`
	// APIKey is an optional authentication key for the service.
	APIKey string `json:"api_key,omitempty" yaml:"apiKey,omitempty"`
	// ThreatThreshold is the confidence below which conversations are flagged.
	ThreatThreshold float64 `json:"threat_threshold" yaml:"threatThreshold"`
	// MaxRequestsPerMinute is the rate limit for anomaly detection.
	MaxRequestsPerMinute int `json:"max_requests_per_minute" yaml:"maxRequestsPerMinute"`
}

var csHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// validateCSURL validates the customer service URL to prevent SSRF.
func validateCSURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL %q must use http or https", rawURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL %q has no hostname", rawURL)
	}
	// Block metadata endpoints.
	if host == "169.254.169.254" || host == "metadata.google.internal" {
		return fmt.Errorf("blocked metadata endpoint: %s", host)
	}
	// Resolve and check IP ranges.
	ips, err := net.LookupIP(host)
	if err != nil {
		if strings.Contains(host, ".") || len(host) <= 63 {
			return nil // trust K8s service names
		}
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("URL %q resolves to blocked IP %s", rawURL, ip)
		}
		metadataIP := net.ParseIP("169.254.169.254")
		if ip.Equal(metadataIP) {
			return fmt.Errorf("URL %q resolves to blocked metadata IP", rawURL)
		}
	}
	return nil
}

// ============================================================================
// 1. CSCollectorPlugin — monitor.collector
// ============================================================================

// CSCollectorPlugin collects AI customer service metrics: request rates,
// escalation rates, confidence distribution, and model latency.
type CSCollectorPlugin struct {
	plugin.BasePlugin
	config CSConfig
	mu     sync.RWMutex
	lastStats *CSStats
}

// NewCSCollectorPlugin creates the CS collector.
func NewCSCollectorPlugin(config CSConfig) (plugin.Plugin, error) {
	return &CSCollectorPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "cs-collector",
			Version:     "1.0.0",
			Description: "Collects AI customer service metrics: requests, escalations, confidence, latency",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtMonitorCollector,
			},
			Priority: 150,
			Tags:     map[string]string{"category": "customer-service", "tier": "contrib"},
		}),
		config: config,
	}, nil
}

func (p *CSCollectorPlugin) Init(_ context.Context, config map[string]interface{}) error {
	if v, ok := config["base_url"].(string); ok {
		p.config.BaseURL = v
	}
	if v, ok := config["api_key"].(string); ok {
		p.config.APIKey = v
	}
	if v, ok := config["threat_threshold"].(float64); ok {
		p.config.ThreatThreshold = v
	}
	if p.config.BaseURL != "" {
		if err := validateCSURL(p.config.BaseURL); err != nil {
			return fmt.Errorf("invalid CS URL: %w", err)
		}
	}
	return nil
}

func (p *CSCollectorPlugin) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.BaseURL+"/api/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := csHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("CS service unreachable: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("CS service returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// MetricNames lists the metrics this collector produces.
func (p *CSCollectorPlugin) MetricNames() []string {
	return []string{
		"cs_requests_total",
		"cs_escalated_total",
		"cs_resolved_total",
		"cs_avg_confidence",
		"cs_p95_latency_ms",
		"cs_active_sessions",
	}
}

// Collect fetches stats from the CS service and returns metrics.
func (p *CSCollectorPlugin) Collect(ctx context.Context) ([]plugin.MetricSample, error) {
	stats, err := p.fetchStats(ctx)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.lastStats = stats
	p.mu.Unlock()

	now := time.Now()
	labels := map[string]string{"service": "ai-customer-service"}

	return []plugin.MetricSample{
		{Name: "cs_requests_total", Value: float64(stats.TotalRequests), Timestamp: now, Labels: labels},
		{Name: "cs_escalated_total", Value: float64(stats.EscalatedCount), Timestamp: now, Labels: labels},
		{Name: "cs_resolved_total", Value: float64(stats.ResolvedCount), Timestamp: now, Labels: labels},
		{Name: "cs_avg_confidence", Value: stats.AverageConfidence, Timestamp: now, Labels: labels},
		{Name: "cs_p95_latency_ms", Value: float64(stats.P95LatencyMs), Timestamp: now, Labels: labels, Unit: "milliseconds"},
		{Name: "cs_active_sessions", Value: float64(stats.ActiveSessions), Timestamp: now, Labels: labels},
	}, nil
}

// GetLastStats returns the most recently collected stats (thread-safe).
func (p *CSCollectorPlugin) GetLastStats() *CSStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastStats
}

// CSStats is the JSON response from the CS /api/v1/stats endpoint.
type CSStats struct {
	TotalRequests    int     `json:"total_requests"`
	EscalatedCount   int     `json:"escalated_count"`
	ResolvedCount    int     `json:"resolved_count"`
	AverageConfidence float64 `json:"average_confidence"`
	P95LatencyMs     int     `json:"p95_latency_ms"`
	ActiveSessions   int     `json:"active_sessions"`
}

func (p *CSCollectorPlugin) fetchStats(ctx context.Context) (*CSStats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.BaseURL+"/api/v1/stats", nil)
	if err != nil {
		return nil, err
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := csHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CS stats endpoint returned HTTP %d", resp.StatusCode)
	}

	var stats CSStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode CS stats: %w", err)
	}
	return &stats, nil
}

// ============================================================================
// 2. CSWebhookPlugin — webhook.mutating
// ============================================================================

// CSWebhookPlugin processes customer messages through the AI service as a
// mutating webhook. It intercepts customer service requests, enriches them
// with AI responses, and returns the mutated object.
type CSWebhookPlugin struct {
	plugin.BasePlugin
	config    CSConfig
	collector *CSCollectorPlugin
}

// NewCSWebhookPlugin creates the CS mutating webhook.
func NewCSWebhookPlugin(config CSConfig, collector *CSCollectorPlugin) (plugin.Plugin, error) {
	return &CSWebhookPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "cs-webhook",
			Version:     "1.0.0",
			Description: "Mutating webhook that processes customer messages through AI service",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtWebhookMutating,
			},
			Priority:     200,
			Dependencies: []string{"cs-collector"},
			Tags:         map[string]string{"category": "customer-service", "tier": "contrib"},
		}),
		config:    config,
		collector: collector,
	}, nil
}

func (p *CSWebhookPlugin) Init(_ context.Context, _ map[string]interface{}) error { return nil }
func (p *CSWebhookPlugin) Health(_ context.Context) error                         { return nil }

// Call processes a customer message through the AI service.
func (p *CSWebhookPlugin) Call(ctx context.Context, req *plugin.WebhookRequest) (*plugin.WebhookResponse, error) {
	// Parse the customer message from the request.
	var msg CustomerMessage
	if err := json.Unmarshal(req.Object, &msg); err != nil {
		return &plugin.WebhookResponse{
			UID:     req.UID,
			Allowed: false,
			Result:  plugin.ErrorResult(p.Metadata().Name, fmt.Errorf("invalid message: %w", err)),
		}, nil
	}

	// Forward to AI customer service.
	aiResponse, err := p.invokeAI(ctx, msg)
	if err != nil {
		return &plugin.WebhookResponse{
			UID:     req.UID,
			Allowed: true, // allow but with fallback response
			Result:  plugin.SuccessResult(p.Metadata().Name),
			MutatedObject: marshalMessage(&CustomerResponse{
				Reply:           "抱歉，系统正在处理中，请稍候或联系人工客服。",
				NeedEscalation:  true,
				EscalationReason: "AI服务暂时不可用",
			}),
		}, nil
	}

	// Build mutated response.
	mutated := marshalMessage(aiResponse)

	return &plugin.WebhookResponse{
		UID:           req.UID,
		Allowed:       true,
		Result:        plugin.SuccessResult(p.Metadata().Name),
		MutatedObject: mutated,
	}, nil
}

// CustomerMessage is the incoming customer request.
type CustomerMessage struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Channel   string `json:"channel,omitempty"`
	OrderID   string `json:"order_id,omitempty"`
}

// CustomerResponse is the AI-generated response.
type CustomerResponse struct {
	Reply            string  `json:"reply"`
	IntentCategory   string  `json:"intent_category"`
	Confidence       float64 `json:"confidence"`
	NeedEscalation   bool    `json:"need_escalation"`
	EscalationReason string  `json:"escalation_reason,omitempty"`
	ModelVersion     string  `json:"model_version"`
	ResponseTimeMs   int     `json:"response_time_ms"`
}

func (p *CSWebhookPlugin) invokeAI(ctx context.Context, msg CustomerMessage) (*CustomerResponse, error) {
	// Defense-in-depth: validate URL before each call.
	if err := validateCSURL(p.config.BaseURL); err != nil {
		return nil, fmt.Errorf("blocked CS URL: %w", err)
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.BaseURL+"/api/v1/chat", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := csHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("CS chat endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var aiResp CustomerResponse
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return nil, fmt.Errorf("decode AI response: %w", err)
	}
	return &aiResp, nil
}

func marshalMessage(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return json.RawMessage(data)
}

// ============================================================================
// 3. CSThreatDetectorPlugin — security.threat.detect
// ============================================================================

// CSThreatDetectorPlugin analyzes customer conversations for anomalous patterns:
// - Rapid-fire requests (potential abuse/bot)
// - Injection attempts in message content
// - Unusual escalation patterns
type CSThreatDetectorPlugin struct {
	plugin.BasePlugin
	config CSConfig
	mu     sync.RWMutex
	// rate tracking per user
	userRequests map[string]*userRateState
}

type userRateState struct {
	requestCount int
	windowStart  time.Time
	lastSeen     time.Time
}

// NewCSThreatDetectorPlugin creates the CS threat detector.
func NewCSThreatDetectorPlugin(config CSConfig) (plugin.Plugin, error) {
	return &CSThreatDetectorPlugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:        "cs-threat-detector",
			Version:     "1.0.0",
			Description: "Detects anomalous customer conversations: abuse, injection, unusual escalation patterns",
			Author:      "CloudAI Fusion Team",
			License:     "Apache-2.0",
			ExtensionPoints: []plugin.ExtensionPoint{
				plugin.ExtSecurityThreatDetect,
			},
			Priority: 100,
			Tags:     map[string]string{"category": "customer-service", "tier": "contrib"},
		}),
		config:         config,
		userRequests:   make(map[string]*userRateState),
	}, nil
}

func (p *CSThreatDetectorPlugin) Init(_ context.Context, config map[string]interface{}) error {
	if v, ok := config["threat_threshold"].(float64); ok {
		p.config.ThreatThreshold = v
	}
	if v, ok := config["max_requests_per_minute"].(float64); ok {
		p.config.MaxRequestsPerMinute = int(v)
	}
	if p.config.ThreatThreshold == 0 {
		p.config.ThreatThreshold = 0.3
	}
	if p.config.MaxRequestsPerMinute == 0 {
		p.config.MaxRequestsPerMinute = 60
	}
	return nil
}

func (p *CSThreatDetectorPlugin) Health(_ context.Context) error { return nil }

// Detect analyzes conversation signals for threats.
func (p *CSThreatDetectorPlugin) Detect(_ context.Context, signals []map[string]interface{}) ([]plugin.ThreatSignal, error) {
	var threats []plugin.ThreatSignal
	now := time.Now()

	for _, signal := range signals {
		// Extract user info.
		userID, _ := signal["user_id"].(string)
		message, _ := signal["message"].(string)
		confidence, _ := signal["confidence"].(float64)

		// Check 1: Rate limiting abuse.
		if userID != "" {
			if p.checkRateLimit(userID, now) {
				threats = append(threats, plugin.ThreatSignal{
					ID:          fmt.Sprintf("cs-rate-abuse-%s-%d", userID, now.Unix()),
					Timestamp:   now,
					Type:        "rate_abuse",
					Severity:    "HIGH",
					Source:      "cs-threat-detector",
					Description: fmt.Sprintf("User %s exceeded rate limit (%d req/min)", userID, p.config.MaxRequestsPerMinute),
					Evidence:    map[string]string{"user_id": userID, "action": "rate_limit_exceeded"},
					Mitigations: []string{"throttle_user", "require_captcha"},
					PluginName:  p.Metadata().Name,
				})
			}
		}

		// Check 2: Injection attempt detection.
		if message != "" && p.detectInjection(message) {
			threats = append(threats, plugin.ThreatSignal{
				ID:          fmt.Sprintf("cs-injection-%d", now.UnixNano()),
				Timestamp:   now,
				Type:        "injection_attempt",
				Severity:    "CRITICAL",
				Source:      "cs-threat-detector",
				Description: "Potential prompt injection or SQL injection in customer message",
				Evidence:    map[string]string{"message_preview": truncate(message, 100)},
				Mitigations: []string{"block_message", "flag_for_review", "alert_security_team"},
				PluginName:  p.Metadata().Name,
			})
		}

		// Check 3: Low confidence pattern (potential adversarial input).
		if confidence > 0 && confidence < p.config.ThreatThreshold {
			threats = append(threats, plugin.ThreatSignal{
				ID:          fmt.Sprintf("cs-low-confidence-%d", now.UnixNano()),
				Timestamp:   now,
				Type:        "anomalous_input",
				Severity:    "MEDIUM",
				Source:      "cs-threat-detector",
				Description: fmt.Sprintf("Very low AI confidence (%.2f) suggests adversarial input", confidence),
				Evidence:    map[string]string{"confidence": fmt.Sprintf("%.2f", confidence)},
				Mitigations: []string{"escalate_to_human", "log_for_analysis"},
				PluginName:  p.Metadata().Name,
			})
		}
	}

	return threats, nil
}

// checkRateLimit returns true if the user has exceeded the rate limit.
func (p *CSThreatDetectorPlugin) checkRateLimit(userID string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	state, exists := p.userRequests[userID]
	if !exists {
		p.userRequests[userID] = &userRateState{
			requestCount: 1,
			windowStart:  now,
			lastSeen:     now,
		}
		return false
	}

	// Reset window if more than 1 minute has passed.
	if now.Sub(state.windowStart) > time.Minute {
		state.requestCount = 1
		state.windowStart = now
		state.lastSeen = now
		return false
	}

	state.requestCount++
	state.lastSeen = now

	return state.requestCount > p.config.MaxRequestsPerMinute
}

// detectInjection checks for common injection patterns.
func (p *CSThreatDetectorPlugin) detectInjection(message string) bool {
	// Common injection patterns (case-insensitive).
	patterns := []string{
		"ignore previous instructions",
		"ignore all previous",
		"you are now",
		"new instructions:",
		"system prompt",
		"```sql",
		"'; DROP TABLE",
		"UNION SELECT",
		"<script>",
		"javascript:",
	}
	lower := strings.ToLower(message)
	for _, pattern := range patterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
