// Package jira implements comprehensive Jira integration for CloudAI Fusion
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Jira Integration Core
// ============================================================================

// JiraIntegration provides complete Jira platform integration
type JiraIntegration struct {
	baseURL      string
	apiToken     string
	username     string
	client       *http.Client
	logger       *logrus.Logger
	authCache    map[string]bool // project -> auth status
	webhookURL   string
}

// ProjectMetadata contains Jira project information
type ProjectMetadata struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Self        string `json:"self"`
}

// IssueType defines available issue types
type IssueType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Priority defines issue priority levels
type Priority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Component represents a Jira component
type Component struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// NewJiraIntegration creates Jira integration instance
func NewJiraIntegration(baseURL, username, apiToken string, logger *logrus.Logger) (*JiraIntegration, error) {
	if logger == nil {
		logger = logrus.New()
	}
	
	if baseURL == "" {
		return nil, fmt.Errorf("Jira base URL required")
	}
	
	if username == "" || apiToken == "" {
		return nil, fmt.Errorf("Jira credentials required")
	}
	
	// Create HTTP client with timeout
	client := &http.Client{Timeout: 30 * time.Second}
	
	// Verify connection
	integration := &JiraIntegration{
		baseURL:   strings.TrimRight(baseURL, "/"),
		username:  username,
		apiToken:  apiToken,
		client:    client,
		logger:    logger.WithField("component", "jira_integration").Logger,
		authCache: make(map[string]bool),
	}
	
	// Test authentication
	if err := integration.testConnection(); err != nil {
		return nil, fmt.Errorf("failed to connect to Jira: %w", err)
	}
	
	integration.logger.Info("Successfully connected to Jira")
	return integration, nil
}

// testConnection verifies Jira connectivity and authentication
func (ji *JiraIntegration) testConnection() error {
	url := ji.baseURL + "/rest/api/2/myself"
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	
	req.SetBasicAuth(ji.username, ji.apiToken)
	
	resp, err := ji.client.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authentication failed: HTTP %d - %s", resp.StatusCode, string(body))
	}
	
	ji.logger.Debug("Jira connection verified successfully")
	return nil
}

// GetProjects retrieves available Jira projects
func (ji *JiraIntegration) GetProjects(ctx context.Context) ([]ProjectMetadata, error) {
	ji.logger.Info("Fetching Jira projects...")
	
	url := ji.baseURL + "/rest/api/2/project"
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	req.SetBasicAuth(ji.username, ji.apiToken)
	
	resp, err := ji.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var projects []ProjectMetadata
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, err
	}
	
	ji.logger.Infof("Retrieved %d projects from Jira", len(projects))
	return projects, nil
}

// GetIssueTypes retrieves available issue types for a project
func (ji *JiraIntegration) GetIssueTypes(projectKey string) ([]IssueType, error) {
	ji.logger.Debugf("Fetching issue types for project %s", projectKey)
	
	url := ji.baseURL + "/rest/api/2/issue/createmeta/" + projectKey + "/issuetypes"
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	req.SetBasicAuth(ji.username, ji.apiToken)
	
	resp, err := ji.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var responseData struct {
		Issuetypes []IssueType `json:"issuetypes"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return nil, err
	}
	
	ji.logger.Debugf("Found %d issue types for project %s", len(responseData.Issuetypes), projectKey)
	return responseData.Issuetypes, nil
}

// ============================================================================
// Auto Ticket Creation
// ============================================================================

// AlertToTicketConverter converts security alerts to Jira tickets
type AlertToTicketConverter struct {
	logger *logrus.Logger
}

// NewAlertToTicketConverter creates ticket converter
func NewAlertToTicketConverter(logger *logrus.Logger) *AlertToTicketConverter {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &AlertToTicketConverter{
		logger: logger.WithField("component", "alert_converter").Logger,
	}
}

// Convert converts alert to Jira ticket format
func (atc *AlertToTicketConverter) Convert(alert SecurityAlert, projectKey string, issueType string) map[string]interface{} {
	atc.logger.Info("Converting alert to Jira ticket...")
	
	// Build Jira ticket structure
	ticket := map[string]interface{}{
		"fields": map[string]interface{}{
			"project": map[string]string{
				"key": projectKey,
			},
			"issuetype": map[string]string{
				"name": issueType,
			},
			"summary": atc.buildSummary(alert),
			"description": atc.buildDescription(alert),
			"priority": map[string]string{
				"name": atc.getPriority(alert.Severity),
			},
			"labels": []string{"cloudai-fusion", "security-alert", "auto-created"},
			"customfield_10001": alert.CVEID, // CVE ID custom field
			"customfield_10002": alert.Source, // Source system
			"created":            time.Now().Format(time.RFC3339),
		},
	}
	
	// Add components if specified
	if len(alert.Components) > 0 {
		ticket["fields"].(map[string]interface{})["components"] = atc.buildComponents(alert.Components)
	}
	
	// Add assignee if provided
	if alert.Assignee != "" {
		ticket["fields"].(map[string]interface{})["assignee"] = map[string]string{
			"name": alert.Assignee,
		}
	}
	
	atc.logger.Info("Alert converted to Jira ticket format")
	return ticket
}

func (atc *AlertToTicketConverter) buildSummary(alert SecurityAlert) string {
	emoji := atc.getSeverityEmoji(alert.Severity)
	return fmt.Sprintf("%s [%s] %s - %s", 
		emoji,
		alert.Severity,
		alert.Title,
		alert.CVEID,
	)
}

func (atc *AlertToTicketConverter) buildDescription(alert SecurityAlert) string {
	var buffer bytes.Buffer
	
	buffer.WriteString(fmt.Sprintf("=== Security Alert Details ===\n\n"))
	buffer.WriteString(fmt.Sprintf("**CVE ID**: %s\n", alert.CVEID))
	buffer.WriteString(fmt.Sprintf("**Severity**: %s\n", alert.Severity))
	buffer.WriteString(fmt.Sprintf("**Title**: %s\n\n", alert.Title))
	
	buffer.WriteString("=== Alert Message ===\n")
	buffer.WriteString(fmt.Sprintf("%s\n\n", alert.Message))
	
	if len(alert.Details) > 0 {
		buffer.WriteString("=== Additional Details ===\n")
		for _, detail := range alert.Details {
			buffer.WriteString(fmt.Sprintf("**%s**: %s\n", detail.Key, detail.Value))
		}
		buffer.WriteString("\n")
	}
	
	buffer.WriteString(fmt.Sprintf("**Source**: %s\n", alert.Source))
	buffer.WriteString(fmt.Sprintf("**Timestamp**: %s\n", alert.Timestamp.Format("2006-01-02 15:04:05")))
	
	if alert.Recommendation != "" {
		buffer.WriteString("\n=== Recommended Action ===\n")
		buffer.WriteString(fmt.Sprintf("%s\n", alert.Recommendation))
	}
	
	return buffer.String()
}

func (atc *AlertToTicketConverter) getSeverityEmoji(severity SeverityLevel) string {
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

func (atc *AlertToTicketConverter) getPriority(severity SeverityLevel) string {
	switch severity {
	case Critical:
		return "Highest"
	case High:
		return "High"
	case Medium:
		return "Medium"
	case Low:
		return "Low"
	default:
		return "Normal"
	}
}

func (atc *AlertToTicketConverter) buildComponents(componentNames []string) []map[string]string {
	components := make([]map[string]string, 0, len(componentNames))
	for _, name := range componentNames {
		components = append(components, map[string]string{"name": name})
	}
	return components
}

// ============================================================================
// Ticket Creation Endpoint
// ============================================================================

// CreateTicket creates a new Jira issue
func (ji *JiraIntegration) CreateTicket(ctx context.Context, ticket map[string]interface{}) (*TicketResponse, error) {
	ji.logger.Info("Creating new Jira ticket...")
	
	url := ji.baseURL + "/rest/api/2/issue"
	
	data, err := json.Marshal(ticket)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ticket: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	
	req.SetBasicAuth(ji.username, ji.apiToken)
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := ji.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Jira returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var result TicketResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	
	ji.logger.Infof("Created ticket %s at %s", result.Key, result.Self)
	return &result, nil
}

// BulkCreateTickets creates multiple tickets in batch
func (ji *JiraIntegration) BulkCreateTickets(ctx context.Context, tickets []map[string]interface{}) ([]*TicketResponse, error) {
	ji.logger.Infof("Creating %d tickets in bulk...", len(tickets))
	
	results := make([]*TicketResponse, 0, len(tickets))
	
	for i, ticket := range tickets {
		result, err := ji.CreateTicket(ctx, ticket)
		if err != nil {
			ji.logger.Warnf("Failed to create ticket %d: %v", i, err)
			continue
		}
		
		results = append(results, result)
	}
	
	ji.logger.Infof("Successfully created %d out of %d tickets", len(results), len(tickets))
	return results, nil
}

// ============================================================================
// Workflow Automation
// ============================================================================

// WorkflowManager handles ticket workflow operations
type WorkflowManager struct {
	jira       *JiraIntegration
	logger     *logrus.Logger
	approvals  map[string]*ApprovalRequest
	rejections map[string]*RejectionLog
}

// ApprovalRequest tracks approval requests
type ApprovalRequest struct {
	RequestID   string
	TicketKey   string
	Requester   string
	RequestedBy string
	CreatedAt   time.Time
	Status      ApprovalStatus
	ApprovedBy  string
	ApprovedAt  time.Time
	Reason      string
}

// ApprovalStatus defines approval states
type ApprovalStatus string

const (
	Pending ApprovalStatus = "pending"
	Approved ApprovalStatus = "approved"
	Rejected ApprovalStatus = "rejected"
)

// RejectionLog logs rejection reasons
type RejectionLog struct {
	LogID      string
	TicketKey  string
	RejectedBy string
	Reason     string
	LoggedAt   time.Time
}

// NewWorkflowManager creates workflow manager
func NewWorkflowManager(jira *JiraIntegration, logger *logrus.Logger) *WorkflowManager {
	if logger == nil {
		logger = logrus.New()
	}
	
	return &WorkflowManager{
		jira:       jira,
		logger:     logger.WithField("component", "workflow_manager").Logger,
		approvals:  make(map[string]*ApprovalRequest),
		rejections: make(map[string]*RejectionLog),
	}
}

// RequestApproval initiates approval process for a ticket
func (wm *WorkflowManager) RequestApproval(ticketKey string, requester string) error {
	wm.logger.Infof("Requesting approval for ticket %s", ticketKey)
	
	request := &ApprovalRequest{
		RequestID:   fmt.Sprintf("APP-%d", time.Now().UnixNano()),
		TicketKey:   ticketKey,
		Requester:   requester,
		RequestedBy: requester,
		CreatedAt:   time.Now(),
		Status:      Pending,
	}
	
	wm.approvals[ticketKey] = request
	
	// Update ticket description with approval request
	comment := fmt.Sprintf("🔒 **Approval Required**\n\n"+
		"Ticket %s requires approval before proceeding.\n\n"+
		"**Request ID**: %s\n"+
		"**Requester**: %s\n"+
		"**Timestamp**: %s\n",
		ticketKey,
		request.RequestID,
		request.Requester,
		request.CreatedAt.Format("2006-01-02 15:04:05"),
	)
	
	wm.AddComment(ticketKey, comment)
	
	return nil
}

// ApproveTicket approves a pending ticket
func (wm *WorkflowManager) ApproveTicket(ticketKey string, approver string, reason string) error {
	request, exists := wm.approvals[ticketKey]
	if !exists {
		return fmt.Errorf("no approval request found for ticket %s", ticketKey)
	}
	
	if request.Status != Pending {
		return fmt.Errorf("ticket %s is not pending approval", ticketKey)
	}
	
	request.Status = Approved
	request.ApprovedBy = approver
	request.ApprovedAt = time.Now()
	request.Reason = reason
	
	// Update ticket with approval
	comment := fmt.Sprintf("✅ **Approved**\n\n"+
		"Ticket %s has been approved.\n\n"+
		"**Approver**: %s\n"+
		"**Approval Time**: %s\n"+
		"**Reason**: %s\n",
		ticketKey,
		approver,
		request.ApprovedAt.Format("2006-01-02 15:04:05"),
		reason,
	)
	
	wm.AddComment(ticketKey, comment)
	
	wm.logger.Infof("Approved ticket %s by %s", ticketKey, approver)
	return nil
}

// RejectTicket rejects an approval request
func (wm *WorkflowManager) RejectTicket(ticketKey string, rejector string, reason string) error {
	request, exists := wm.approvals[ticketKey]
	if !exists {
		return fmt.Errorf("no approval request found for ticket %s", ticketKey)
	}
	
	if request.Status != Pending {
		return fmt.Errorf("ticket %s is not pending approval", ticketKey)
	}
	
	request.Status = Rejected
	
	// Log rejection
	log := &RejectionLog{
		LogID:      fmt.Sprintf("REJ-%d", time.Now().UnixNano()),
		TicketKey:  ticketKey,
		RejectedBy: rejector,
		Reason:     reason,
		LoggedAt:   time.Now(),
	}
	
	wm.rejections[ticketKey] = log
	
	// Update ticket with rejection
	comment := fmt.Sprintf("❌ **Rejected**\n\n"+
		"Ticket %s has been rejected.\n\n"+
		"**Rejector**: %s\n"+
		"**Rejection Time**: %s\n"+
		"**Reason**: %s\n",
		ticketKey,
		rejector,
		log.LoggedAt.Format("2006-01-02 15:04:05"),
		reason,
	)
	
	wm.AddComment(ticketKey, comment)
	
	wm.logger.Infof("Rejected ticket %s by %s", ticketKey, rejector)
	return nil
}

// AddComment adds a comment to a ticket
func (wm *WorkflowManager) AddComment(ticketKey string, comment string) error {
	// Implementation would call Jira REST API to add comment
	// For now, just log
	wm.logger.Debugf("Would add comment to ticket %s", ticketKey)
	return nil
}

// GetPendingApprovals returns all pending approval requests
func (wm *WorkflowManager) GetPendingApprovals() []*ApprovalRequest {
	pending := make([]*ApprovalRequest, 0)
	
	for _, req := range wm.approvals {
		if req.Status == Pending {
			pending = append(pending, req)
		}
	}
	
	return pending
}
