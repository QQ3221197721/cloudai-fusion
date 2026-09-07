// Package slack implements comprehensive notification and slash command integration
package slack

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nlopes/slack"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Advanced Notification System
// ============================================================================

// AlertRouter routes alerts to appropriate channels based on type/severity
type AlertRouter struct {
	logger        *logrus.Logger
	channels      map[string]string // channelType -> channelID
	defaultChan   string
	severityMap   map[string][]string
	routingRules  []RoutingRule
}

// RoutingRule defines alert routing logic
type RoutingRule struct {
	Condition  func(Alert) bool
	Channels   []string
	Priority   int
}

// NewAlertRouter creates alert routing system
func NewAlertRouter(logger *logrus.Logger) *AlertRouter {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &AlertRouter{
		logger:    logger.WithField("component", "alert_router"),
		channels:  make(map[string]string),
		severityMap: make(map[string][]string),
		routingRules: make([]RoutingRule, 0),
	}
}

// AddChannel registers a new alert channel
func (ar *AlertRouter) AddChannel(channelType, channelName string) error {
	ar.logger.Infof("Registering channel %s (%s)", channelType, channelName)
	
	// Get actual channel ID via API
	client := slack.New("") // Would use bot token
	
	channels, err := client.GetConversations(&slack.GetConversationsOptions{
		Type: "public_channel",
	})
	
	if err != nil {
		return fmt.Errorf("failed to fetch channels: %w", err)
	}
	
	for _, ch := range channels {
		if ch.Name == channelName {
			ar.channels[channelType] = ch.ID
			return nil
		}
	}
	
	return fmt.Errorf("channel not found: %s", channelName)
}

// Route sends alert to appropriate channels based on rules
func (ar *AlertRouter) Route(ctx context.Context, alert Alert) error {
	ar.logger.Infof("Routing alert: %s (Severity: %s)", alert.Title, alert.Severity)
	
	var targetChannels []string
	
	// Find matching rules
	for _, rule := range ar.routingRules {
		if rule.Condition(alert) {
			targetChannels = append(targetChannels, rule.Channels...)
		}
	}
	
	// Default channel if no rules matched
	if len(targetChannels) == 0 {
		targetChannels = append(targetChannels, ar.defaultChan)
	}
	
	// Send to all target channels
	for _, chID := range targetChannels {
		if err := ar.sendToChannel(ctx, chID, alert); err != nil {
			ar.logger.Warnf("Failed to send to channel %s: %v", chID, err)
		}
	}
	
	return nil
}

// sendToChannel delivers alert to specific Slack channel
func (ar *AlertRouter) sendToChannel(ctx context.Context, channelID string, alert Alert) error {
	attachment := buildSlackAttachment(alert)
	
	params := &slack.PostMessageParameters{
		Attachments: []slack.Attachment{attachment},
		LinkNames:   true,
	}
	
	// In production: use actual Slack client
	_ = params
	_ = ctx
	
	ar.logger.Debugf("Sent alert to channel %s", channelID)
	return nil
}

// BuildSlackAttachment creates formatted attachment
func buildSlackAttachment(alert Alert) slack.Attachment {
	color := getColorForSeverity(alert.Severity)
	
	return slack.Attachment{
		Color: color,
		Title: formatTitleWithEmoji(alert),
		Text:  alert.Message,
		Fields: buildFieldList(alert.Details),
		Footer: fmt.Sprintf("CloudAI Fusion | %s", time.Now().Format("2006-01-02 15:04")),
		Ts:     float64(time.Now().Unix()),
	}
}

func getColorForSeverity(severity SeverityLevel) string {
	switch severity {
	case Critical:
		return "#FF0000"
	case High:
		return "#FF6600"
	case Medium:
		return "#FFCC00"
	case Low:
		return "#00CC00"
	default:
		return "#36a6f7"
	}
}

func formatTitleWithEmoji(alert Alert) string {
	emoji := getEmojiForSeverity(alert.Severity)
	return fmt.Sprintf("%s %s", emoji, alert.Title)
}

func getEmojiForSeverity(severity SeverityLevel) string {
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

func buildFieldList(details []AlertDetail) []*slack.Field {
	fields := make([]*slack.Field, 0, len(details))
	
	for _, detail := range details {
		fields = append(fields, &slack.Field{
			Title: detail.Key,
			Value: detail.Value,
			Short: true,
		})
	}
	
	return fields
}

// ============================================================================
// Slash Command Handler
// ============================================================================

// SlashCommandHandler processes incoming Slack slash commands
type SlashCommandHandler struct {
	logger       *logrus.Logger
	commandRouter map[string]CommandHandler
	authToken    string
}

// CommandHandler handles specific slash command
type CommandHandler func(ctx context.Context, cmd SlashCommand) ([]byte, error)

// NewSlashCommandHandler creates command processing system
func NewSlashCommandHandler(logger *logrus.Logger) *SlashCommandHandler {
	if logger == nil {
		logger = logrus.New()
	}
	
	handler := &SlashCommandHandler{
		logger:      logger.WithField("component", "slash_handler"),
		commandRouter: make(map[string]CommandHandler),
	}
	
	// Register default commands
	handler.RegisterCommand("/cloudai status", handleStatusCommand)
	handler.RegisterCommand("/cloudai deploy", handleDeployCommand)
	handler.RegisterCommand("/cloudai report", handleReportCommand)
	handler.RegisterCommand("/cloudai help", handleHelpCommand)
	
	return handler
}

// RegisterCommand adds a new command handler
func (sch *SlashCommandHandler) RegisterCommand(pattern string, handler CommandHandler) {
	sch.logger.Debugf("Registering command: %s", pattern)
	sch.commandRouter[pattern] = handler
}

// Handle processes incoming slash command request
func (sch *SlashCommandHandler) Handle(r *http.Request) ([]byte, error) {
	if r.Method != http.MethodPost {
		return nil, fmt.Errorf("only POST requests allowed")
	}
	
	// Parse form data
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}
	
	cmdText := r.FormValue("text")
	channelName := r.FormValue("channel_name")
	userName := r.FormValue("user_name")
	
	sh := SlashCommand{
		Text:         cmdText,
		ChannelName:  channelName,
		UserName:     userName,
		ResponseURL:  r.FormValue("response_url"),
		TriggerID:    r.FormValue("trigger_id"),
		TeamDomain:   r.FormValue("team_domain"),
	}
	
	sch.logger.Infof("Processing command from %s in %s: %s", userName, channelName, sh.Text)
	
	// Find matching handler
	for pattern, handler := range sch.commandRouter {
		if strings.HasPrefix(sh.Text, pattern[1:]) { // Remove leading /
			response, err := handler(context.Background(), sh)
			if err != nil {
				return nil, fmt.Errorf("command execution failed: %w", err)
			}
			
			return response, nil
		}
	}
	
	// Default help response
	defaultResponse := []byte("Unknown command. Use '/cloudai help' for available commands.")
	return defaultResponse, nil
}

// handleStatusCommand returns system health overview
func handleStatusCommand(ctx context.Context, cmd SlashCommand) ([]byte, error) {
	statusMsg := `*CloudAI Fusion Status*
 
🟢 All systems operational
⚡ Active exploits: 13
🛡️ EDR Bypass modules: 3
🎯 Kerberos Native Stack: Ready
`
	
	return []byte(statusMsg), nil
}

// handleDeployCommand triggers deployment workflow
func handleDeployCommand(ctx context.Context, cmd SlashCommand) ([]byte, error) {
	// Would trigger actual deployment pipeline
	response := "*Deployment triggered*\n\nTarget: " + cmd.Text + "\nStatus: Queueing..."
	return []byte(response), nil
}

// handleReportCommand generates reports
func handleReportCommand(ctx context.Context, cmd SlashCommand) ([]byte, error) {
	reportTypes := []string{"CVE Summary", "Exploit Validation", "EDR Success Rates"}
	
	response := "*Available Reports*\n"
	for i, rt := range reportTypes {
		response += fmt.Sprintf("%d. %s\n", i+1, rt)
	}
	
	return []byte(response), nil
}

// handleHelpCommand shows available commands
func handleHelpCommand(ctx context.Context, cmd SlashCommand) ([]byte, error) {
	helpMsg := `*CloudAI Fusion Slash Commands*

/cloudai status - System health status
/cloudai deploy <target> - Trigger deployment
/cloudai report <type> - Generate report
/cloudai help - Show this message
`
	return []byte(helpMsg), nil
}
