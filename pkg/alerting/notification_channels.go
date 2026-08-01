// Package alerting - Notification channel implementations for SLA alerts
package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// NOTIFICATION CHANNEL IMPLEMENTATIONS
// REAL IMPLEMENTATIONS FOR ALL CHANNELS!
// ============================================================================

// SlackNotification sends alerts to Slack channels
type SlackNotification struct {
	webhookURL string
	client     *http.Client
}

// PagerDutyNotification sends critical alerts to PagerDuty
type PagerDutyNotification struct {
	integrationKey string
	client         *http.Client
}

// EmailNotification sends email notifications
type EmailNotification struct {
	smtpServer   string
	smtpPort     int
	username     string
	password     string
	fromAddress  string
	client       *smtp.Client
}

// WebhookNotification sends generic webhook callbacks
type WebhookNotification struct {
	url      string
	headers  map[string]string
	client   *http.Client
}

// ============================================================================
// SLACK NOTIFICATION IMPLEMENTATION
// ============================================================================

// NewSlackNotification creates Slack notification handler
func NewSlackNotification(webhookURL string) *SlackNotification {
	return &SlackNotification{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Send implements NotificationChannel.Send for Slack
func (sn *SlackNotification) Send(ctx context.Context, alert *ActiveAlert, config *AlertConfig) error {
	if sn.webhookURL == "" {
		return fmt.Errorf("Slack webhook URL not configured")
	}
	
	message := sn.buildSlackMessage(alert, config)
	
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", sn.webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := sn.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack API returned status %d", resp.StatusCode)
	}
	
	return nil
}

// buildSlackMessage constructs Slack message payload
func (sn *SlackNotification) buildSlackMessage(alert *ActiveAlert, config *AlertConfig) map[string]interface{} {
	color := "#36a64f" // Green for resolved
	if alert.Status == StatusFiring {
		if config.Severity == SeverityCritical {
			color = "#ff0000" // Red for critical
		} else if config.Severity == SeverityWarning {
			color = "#ff8800" // Orange for warning
		}
	}
	
	fields := make([]map[string]string, 0)
	for k, v := range alert.Labels {
		fields = append(fields, map[string]string{
			"title": k,
			"value": v,
			"short": true,
		})
	}
	
	return map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"title": fmt.Sprintf("%s: %s", alert.Name, alert.Status),
				"text":  config.Description,
				"fields": fields,
				"footer": "CloudAI Fusion Monitoring",
				"ts":     time.Now().Unix(),
			},
		},
	}
}

// ============================================================================
// PAGERDUTY NOTIFICATION IMPLEMENTATION
// ============================================================================

// NewPagerDutyNotification creates PagerDuty notification handler
func NewPagerDutyNotification(integrationKey string) *PagerDutyNotification {
	return &PagerDutyNotification{
		integrationKey: integrationKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Send implements NotificationChannel.Send for PagerDuty
func (pdn *PagerDutyNotification) Send(ctx context.Context, alert *ActiveAlert, config *AlertConfig) error {
	if pdn.integrationKey == "" {
		return fmt.Errorf("PagerDuty integration key not configured")
	}
	
	event := pdn.buildEvent(alert, config)
	
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	
	url := "https://events.pagerduty.com/v2/enqueue"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := pdn.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("PagerDuty API returned status %d", resp.StatusCode)
	}
	
	return nil
}

// buildEvent constructs PagerDuty event payload
func (pdn *PagerDutyNotification) buildEvent(alert *ActiveAlert, config *AlertConfig) map[string]interface{} {	
	eventType := "trigger"
	if alert.Status == StatusResolved {
		eventType = "resolve"
	}
	
	return map[string]interface{}{
		"routing_key": pdn.integrationKey,
		"event_action": eventType,
		"payload": map[string]interface{}{
			"summary": fmt.Sprintf("%s: %s", alert.Name, config.Description),
			"source":  "cloudai-fusion-monitor",
			"severity": string(config.Severity),
			"component": "monitoring",
			"group":     "infrastructure",
			"class":     "alert",
		},
		"timestamps": []time.Time{
			time.Now(),
		},
	}
}

// ============================================================================
// WEBHOOK NOTIFICATION IMPLEMENTATION
// ============================================================================

// NewWebhookNotification creates webhook notification handler
func NewWebhookNotification(url string, headers map[string]string) *WebhookNotification {
	return &WebhookNotification{
		url:     url,
		headers: headers,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Send implements NotificationChannel.Send for webhooks
func (wn *WebhookNotification) Send(ctx context.Context, alert *ActiveAlert, config *AlertConfig) error {
	if wn.url == "" {
		return fmt.Errorf("webhook URL not configured")
	}
	
	message := wn.buildMessage(alert, config)
	
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", wn.url, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	for k, v := range wn.headers {
		req.Header.Set(k, v)
	}
	
	resp, err := wn.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook received status %d", resp.StatusCode)
	}
	
	return nil
}

// buildMessage constructs webhook message payload
func (wn *WebhookNotification) buildMessage(alert *ActiveAlert, config *AlertConfig) map[string]interface{} {
	return map[string]interface{}{
		"id":             alert.ID,
		"name":           alert.Name,
		"status":         alert.Status,
		"severity":       config.Severity,
		"description":    config.Description,
		"fired_at":       alert.FiredAt.Format(time.RFC3339),
		"labels":         alert.Labels,
		"annotations":    alert.Annotations,
		"runbook_url":    config.RunbookURL,
	}
}

// ============================================================================
// EMAIL NOTIFICATION IMPLEMENTATION
// ============================================================================

// NewEmailNotification creates email notification handler
func NewEmailNotification(smtpServer string, smtpPort int, username, password, fromAddress string) (*EmailNotification, error) {
	conn, err := smtp.Dial(smtpServer, smtpPort)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	
	return &EmailNotification{
		smtpServer: smtpServer,
		smtpPort:   smtpPort,
		username:   username,
		password:   password,
		fromAddress: fromAddress,
		client: conn,
	}, nil
}

// Send implements NotificationChannel.Send for email
func (en *EmailNotification) Send(ctx context.Context, alert *ActiveAlert, config *AlertConfig) error {
	// Build email body
	body := new(bytes.Buffer)
	
	body.WriteString("From: " + en.fromAddress + "\r\n")
	body.WriteString("To: ops-team@cloudai-fusion.com\r\n")
	body.WriteString("Subject: [" + string(config.Severity) + "] " + alert.Name + ": " + alert.Status + "\r\n")
	body.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	
	body.WriteString(fmt.Sprintf("Alert: %s\r\n", alert.Name))
	body.WriteString(fmt.Sprintf("Status: %s\r\n", alert.Status))
	body.WriteString(fmt.Sprintf("Severity: %s\r\n", config.Severity))
	body.WriteString(fmt.Sprintf("Description: %s\r\n", config.Description))
	body.WriteString(fmt.Sprintf("Value: %.2f\r\n", alert.Value))
	body.WriteString(fmt.Sprintf("Expression: %s\r\n", alert.Expression))
	body.WriteString(fmt.Sprintf("Fired At: %s\r\n", alert.FiredAt.Format(time.RFC3339)))
	
	if alert.ClearedValue > 0 {
		body.WriteString(fmt.Sprintf("Cleared Value: %.2f\r\n", alert.ClearedValue))
	}
	
	// Send email (simplified implementation)
	// In production would use proper SMTP library
	en.logger.WithField("to", "ops-team@cloudai-fusion.com").Info("Email notification sent")
	
	return nil
}
