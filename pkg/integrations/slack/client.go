// Package slack - CloudAI Fusion Slack Integration
// Provides real-time notifications, slash commands, and interactive messages
package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nlopes/slack"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Core Structures & Interfaces
// ============================================================================

// SlackIntegration implements the Integration interface for Slack
type SlackIntegration struct {
	botToken     string
	userToken    string
	authTokens   map[string]string // channel ID -> user token
	client       *slack.Client
	logger       *logrus.Logger
	channelCache map[string]string // friendly name -> channel ID
}

// Alert represents a security or operational alert to be sent to Slack
type Alert struct {
	Title       string
	Severity    SeverityLevel
	Message     string
	Details     []AlertDetail
	Actions     []SlackAction
	Attachments []slack.Attachment
	Timestamp   time.Time
	Source      string
}

// SeverityLevel defines alert severity levels
type SeverityLevel string

const (
	Critical SeverityLevel = "critical"
	High     SeverityLevel = "high"
	Medium   SeverityLevel = "medium"
	Low      SeverityLevel = "low"
	Info     SeverityLevel = "info"
)

// AlertDetail provides structured information about an alert
type AlertDetail struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
}

// SlashCommand represents incoming Slack slash commands
type SlashCommand struct {
	Token       string
	TeamID      string
	TeamDomain  string
	ChannelID   string
	ChannelName string
	UserID      string
	UserName    string
	Command     string
	Text        string
	ResponseURL string
	TriggerID   string
}

// ============================================================================
// Constants & Configuration
// ============================================================================

const (
	DefaultTimeout = 30 * time.Second
	
	// Command constants
	CmdStatus   = "status"
	CmdDeploy   = "deploy"
	CmdReport   = "report"
	CmdHelp     = "help"
	CmdApprove  = "approve"
	CmdReject   = "reject"
	
	// Attachment color codes by severity
	ColorCritical = "#FF0000"
	ColorHigh     = "#FF6600"
	ColorMedium   = "#FFCC00"
	ColorLow      = "#00CC00"
	ColorInfo     = "#36a6f7"
)

// ============================================================================
// Initialization & Configuration
// ============================================================================

// NewSlackIntegration creates a new Slack integration instance
func NewSlackIntegration(ctx context.Context, botToken, userToken string, logger *logrus.Logger) (*SlackIntegration, error) {
	if logger == nil {
		logger = logrus.New()
	}
	
	integration := &SlackIntegration{
		botToken:     botToken,
		userToken:    userToken,
		authTokens:   make(map[string]string),
		client:       slack.New(botToken, slack.OptionContextTimeout(DefaultTimeout)),
		logger:       logger.WithField("integration", "slack"),
		channelCache: make(map[string]string),
	}
	
	// Cache channel mappings if user token provided
	if userToken != "" {
		if err := integration.populateChannelCache(ctx); err != nil {
			logger.Warnf("Failed to populate channel cache: %v", err)
		}
	}
	
	return integration, nil
}

// Configure updates integration configuration
func (si *SlackIntegration) Configure(ctx context.Context, config map[string]any) error {
	// Handle dynamic configuration updates
	if token, ok := config["bot_token"].(string); ok && token != "" {
		si.client = slack.New(token, slack.OptionContextTimeout(DefaultTimeout))
		si.botToken = token
		si.logger.Info("Bot token updated")
	}
	
	if userToken, ok := config["user_token"].(string); ok && userToken != "" {
		si.userToken = userToken
		go si.populateChannelCache(ctx)
		si.logger.Info("User token updated, refreshing channel cache")
	}
	
	return nil
}

// populateChannelCache fetches all accessible channels for caching
func (si *SlackIntegration) populateChannelCache(ctx context.Context) error {
	channels, err := si.client.GetConversations(&slack.GetConversationsOptions{
		Type:        "public_channel",
		Limit:       100,
		MaxResults:  1000,
	})
	
	if err != nil {
		return fmt.Errorf("failed to fetch channels: %w", err)
	}
	
	for _, channel := range channels {
		if channel.Name != "" {
			si.channelCache[channel.Name] = channel.ID
			si.logger.Debugf("Cached channel: %s -> %s", channel.Name, channel.ID)
		}
	}
	
	return nil
}

// Name returns integration name
func (si *SlackIntegration) Name() string {
	return "slack"
}

// Version returns integration version
func (si *SlackIntegration) Version() string {
	return "v1.0.0"
}

// HealthCheck verifies the integration is working
func (si *SlackIntegration) HealthCheck(ctx context.Context) error {
	info, err := si.client.GetUserInfoContext(ctx, "ME")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	
	si.logger.Infof("Slack integration healthy - authenticated as: %s", info.Name)
	return nil
}

// Cleanup releases resources
func (si *SlackIntegration) Cleanup() error {
	si.logger.Info("Cleaning up Slack integration")
	return nil
}
