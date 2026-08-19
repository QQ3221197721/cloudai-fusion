// Package jira unit tests for complete Jira integration
package jira

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestAlertToTicketConverter_Convert(t *testing.T) {
	logger := logrus.New()
	converter := NewAlertToTicketConverter(logger)
	
	alert := SecurityAlert{
		CVEID:    "CVE-2024-1234",
		Severity: Critical,
		Title:    "Critical RCE Vulnerability",
		Message:  "Remote code execution detected in production",
		Source:   "CloudAI Fusion Scanner",
		Timestamp: time.Now(),
		Details: []AlertDetail{
			{Key: "Affected File", Value: "pkg/scan/engine.go"},
			{Key: "Line", Value: "L245"},
		},
	}
	
	ticket := converter.Convert(alert, "SEC", "Bug")
	
	assert.NotNil(t, ticket)
	assert.Contains(t, ticket["fields"], "summary")
	assert.Contains(t, ticket["fields"], "description")
	assert.Contains(t, ticket["fields"], "priority")
	
	summary := ticket["fields"].(map[string]interface{})["summary"].(string)
	assert.Contains(t, summary, "CVE-2024-1234")
	assert.Contains(t, summary, "Critical")
}

func TestWorkflowManager_RequestApproval(t *testing.T) {
	logger := logrus.New()
	jiraMock := &JiraIntegration{}
	wm := NewWorkflowManager(jiraMock, logger)
	
	err := wm.RequestApproval("PROJ-123", "alice@cloudai.fusion")
	
	assert.NoError(t, err)
	assert.Len(t, wm.approvals, 1)
	
	req := wm.approvals["PROJ-123"]
	assert.Equal(t, "PROJ-123", req.TicketKey)
	assert.Equal(t, Pending, req.Status)
	assert.Equal(t, "alice@cloudai.fusion", req.Requester)
}

func TestWorkflowManager_ApproveTicket(t *testing.T) {
	logger := logrus.New()
	jiraMock := &JiraIntegration{}
	wm := NewWorkflowManager(jiraMock, logger)
	
	// First request approval
	err := wm.RequestApproval("PROJ-123", "bob@cloudai.fusion")
	assert.NoError(t, err)
	
	// Then approve it
	err = wm.ApproveTicket("PROJ-123", "manager@cloudai.fusion", "Approved after review")
	
	assert.NoError(t, err)
	
	req := wm.approvals["PROJ-123"]
	assert.Equal(t, Approved, req.Status)
	assert.Equal(t, "manager@cloudai.fusion", req.ApprovedBy)
	assert.Equal(t, "Approved after review", req.Reason)
}

func TestWorkflowManager_RejectTicket(t *testing.T) {
	logger := logrus.New()
	jiraMock := &JiraIntegration{}
	wm := NewWorkflowManager(jiraMock, logger)
	
	// First request approval
	err := wm.RequestApproval("PROJ-456", "charlie@cloudai.fusion")
	assert.NoError(t, err)
	
	// Then reject it
	err = wm.RejectTicket("PROJ-456", "senior@cloudai.fusion", "Does not meet security requirements")
	
	assert.NoError(t, err)
	
	req := wm.approvals["PROJ-456"]
	assert.Equal(t, Rejected, req.Status)
	assert.NotNil(t, wm.rejections["PROJ-456"])
	
	log := wm.rejections["PROJ-456"]
	assert.Equal(t, "senior@cloudai.fusion", log.RejectedBy)
	assert.Equal(t, "Does not meet security requirements", log.Reason)
}

func TestWorkflowManager_GetPendingApprovals(t *testing.T) {
	logger := logrus.New()
	jiraMock := &JiraIntegration{}
	wm := NewWorkflowManager(jiraMock, logger)
	
	// Request approvals for multiple tickets
	wm.RequestApproval("PROJ-1", "user1")
	wm.RequestApproval("PROJ-2", "user2")
	wm.RequestApproval("PROJ-3", "user3")
	
	pending := wm.GetPendingApprovals()
	
	assert.Len(t, pending, 3)
	for _, req := range pending {
		assert.Equal(t, Pending, req.Status)
	}
	
	// Approve one of them
	wm.ApproveTicket("PROJ-2", "admin", "Approved")
	
	pending = wm.GetPendingApprovals()
	assert.Len(t, pending, 2) // Only PROJ-1 and PROJ-3 should remain
}

func TestConvert_SeverityEmojiMapping(t *testing.T) {
	logger := logrus.New()
	converter := NewAlertToTicketConverter(logger)
	
	tests := []struct {
		severity SeverityLevel
		expected string
	}{
		{Critical, "🚨"},
		{High, "⚠️"},
		{Medium, "⚡"},
		{Low, "ℹ️"},
		{Info, "📢"},
	}
	
	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			alert := SecurityAlert{
				Severity: tt.severity,
				Title:    "Test Alert",
				CVEID:    "TEST-001",
			}
			
			ticket := converter.Convert(alert, "SEC", "Bug")
			summary := ticket["fields"].(map[string]interface{})["summary"].(string)
			
			assert.Contains(t, summary, tt.expected)
		})
	}
}

func TestConvert_PriorityMapping(t *testing.T) {
	logger := logrus.New()
	converter := NewAlertToTicketConverter(logger)
	
	tests := []struct {
		severity SeverityLevel
		expected string
	}{
		{Critical, "Highest"},
		{High, "High"},
		{Medium, "Medium"},
		{Low, "Low"},
		{Info, "Normal"},
	}
	
	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			alert := SecurityAlert{
				Severity: tt.severity,
				Title:    "Test Alert",
				CVEID:    "TEST-001",
			}
			
			ticket := converter.Convert(alert, "SEC", "Bug")
			priority := ticket["fields"].(map[string]interface{})["priority"].(map[string]string)["name"]
			
			assert.Equal(t, tt.expected, priority)
		})
	}
}
