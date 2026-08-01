// Package slack - Alert message formatting and sending functionality
package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

// ============================================================================
// Alert Message Builder
// ============================================================================

// SendAlert sends an alert to a specific Slack channel
func (si *SlackIntegration) SendAlert(ctx context.Context, channelID, alertTitle string, alert Alert) error {
	attachment := si.buildAlertAttachment(alert)
	
	params := &slack.PostMessageParameters{
		Attachments: []slack.Attachment{attachment},
		LinkNames:   true,
	}
	
	_, _, err := si.client.PostMessageContext(ctx, channelID, slack.MsgOptionBlocks(), params)
	if err != nil {
		return fmt.Errorf("failed to send alert to Slack: %w", err)
	}
	
	si.logger.WithFields(map[string]any{
		"channel":    channelID,
		"title":      alertTitle,
		"severity":   alert.Severity,
	}).Info("Alert sent successfully")
	
	return nil
}

// buildAlertAttachment creates a formatted Slack attachment for the alert
func (si *SlackIntegration) buildAlertAttachment(alert Alert) slack.Attachment {
	// Determine color based on severity
	color := si.getSeverityColor(alert.Severity)
	
	// Build title with emoji
	titleEmoji := si.getTitleEmoji(alert.Severity)
	titleText := fmt.Sprintf("%s %s", titleEmoji, alert.Title)
	
	// Create attachment
	attachment := slack.Attachment{
		Color:   color,
		Title:   titleText,
		Text:    alert.Message,
		Footer:  si.buildFooter(alert),
		Ts:      float64(alert.Timestamp.Unix()),
		Actions: si.buildActions(alert.Actions),
	}
	
	// Add fields if available
	if len(alert.Details) > 0 {
		fields := si.buildFields(alert.Details)
		attachment.Fields = fields
	}
	
	// Add custom actions if provided
	if len(alert.Attachments) > 0 {
		attachment.Blocks = append(attachment.Blocks, alert.Attachments...)
	}
	
	return attachment
}

// buildFields converts alert details to Slack field format
func (si *SlackIntegration) buildFields(details []AlertDetail) []*slack.Field {
	fields := make([]*slack.Field, 0, len(details))
	
	for _, detail := range details {
		field := &slack.Field{
			Title: detail.Key,
			Value: detail.Value,
			Short: true, // Display two fields per row
		}
		fields = append(fields, field)
	}
	
	return fields
}

// buildActions creates interactive buttons for alert actions
func (si *SlackIntegration) buildActions(actions []SlackAction) []*slack.Action {
	if len(actions) == 0 {
		return nil
	}
	
	slackActions := make([]*slack.Action, 0, len(actions))
	
	for i, action := range actions {
		if i >= 5 { // Slack limits to 5 actions per attachment
			break
		}
		
		slackAction := &slack.Action{
			Type: "button",
			Text: &slack.TextBlockOption{
				Text: action.Label,
				Type: "plain_text",
			},
			URL:  action.URL,
			Value: action.Value,
		}
		
		slackActions = append(slackActions, slackAction)
	}
	
	return slackActions
}

// buildFooter creates alert footer with timestamp and source
func (si *SlackIntegration) buildFooter(alert Alert) string {
	timestamp := alert.Timestamp.Format("2006-01-02 15:04:05 UTC")
	
	if alert.Source != "" {
		return fmt.Sprintf("%s | %s", alert.Source, timestamp)
	}
	
	return timestamp
}

// getTitleEmoji returns appropriate emoji based on severity
func (si *SlackIntegration) getTitleEmoji(severity SeverityLevel) string {
	switch severity {
	case Critical:
		return "🚨"
	case High:
		return "⚠️"
	case Medium:
		return "⚡"
	case Low:
		return "ℹ️"
	default:
		return "📢"
	}
}

// getSeverityColor returns hex color code for severity level
func (si *SlackIntegration) getSeverityColor(severity SeverityLevel) string {
	switch severity {
	case Critical:
		return ColorCritical
	case High:
		return ColorHigh
	case Medium:
		return ColorMedium
	case Low:
		return ColorLow
	default:
		return ColorInfo
	}
}

// ============================================================================
// Convenience Methods
// ============================================================================

// SendSecurityAlert sends a security-related alert
func (si *SlackIntegration) SendSecurityAlert(ctx context.Context, channelID string, vuln VulnerabilityReport) error {
	alert := Alert{
		Title:   fmt.Sprintf("🔒 Security Alert: %s", vuln.CVE.ID),
		Severity: SeverityLevel(vuln.Criticality),
		Message: vuln.Description,
		Details: []AlertDetail{
			{Key: "CVE", Value: vuln.CVE.ID},
			{Key: "CVSS Score", Value: fmt.Sprintf("%.1f", vuln.CVSSScore)},
			{Key: "Affected File", Value: vuln.AffectedFile},
			{Key: "Line", Value: fmt.Sprintf("L%d-%d", vuln.StartLine, vuln.EndLine)},
			{Key: "Fix Available", Value: vuln.FixVersion},
			{Key: "Exploit Status", Value: vuln.ExploitStatus()},
		},
		Source:  "CloudAI Fusion Security Scanner",
		Timestamp: time.Now(),
	}
	
	return si.SendAlert(ctx, channelID, alert.Title, alert)
}

// SendOperationalAlert sends an operational/infrastructure alert
func (si *SlackIntegration) SendOperationalAlert(ctx context.Context, channelID string, alertData OperationalAlert) error {
	emoji := "🖥️"
	severity := Low
	
	if alertData.Critical {
		emoji = "🚨"
		severity = Critical
	} else if alertData.Warning {
		emoji = "⚠️"
		severity = High
	}
	
	alert := Alert{
		Title:   fmt.Sprintf("%s %s", emoji, alertData.Title),
		Severity: severity,
		Message: alertData.Message,
		Details: []AlertDetail{
			{Key: "Service", Value: alertData.Service},
			{Key: "Environment", Value: alertData.Environment},
			{Key: "Metric", Value: alertData.Metric},
			{Key: "Current Value", Value: alertData.CurrentValue},
			{Key: "Threshold", Value: alertData.Threshold},
		},
		Source:  "CloudAI Fusion Monitor",
		Timestamp: time.Now(),
	}
	
	return si.SendAlert(ctx, channelID, alert.Title, alert)
}

// GetChannelIDByName returns channel ID given its name
func (si *SlackIntegration) GetChannelIDByName(channelName string) (string, error) {
	// Check cache first
	if channelID, ok := si.channelCache[strings.ToLower(channelName)]; ok {
		return channelID, nil
	}
	
	// Fall back to API lookup
	channels, err := si.client.GetConversations(&slack.GetConversationsOptions{
		Type:        "public_channel",
		Limit:       200,
	})
	
	if err != nil {
		return "", fmt.Errorf("failed to fetch channels: %w", err)
	}
	
	for _, channel := range channels {
		if strings.EqualFold(channel.Name, channelName) {
			return channel.ID, nil
		}
	}
	
	return "", fmt.Errorf("channel not found: %s", channelName)
}
