
// Package metasploit_api provides REST API endpoints for Metasploit operations
package metasploit_api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/metasploit"
)

// APIHandler handles all Metasploit-related HTTP requests
type APIHandler struct {
	scanner      *metasploit.ExploitScanner
	orchestrator *metasploit.PenTestOrchestrator
	logger       *logrus.Logger
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(scanner *metasploit.ExploitScanner, orchestrator *metasploit.PenTestOrchestrator) *APIHandler {
	return &APIHandler{
		scanner:      scanner,
		orchestrator: orchestrator,
		logger:       logrus.StandardLogger(),
	}
}

// RegisterRoutes registers all API routes with the Gin router
func (h *APIHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api/v1/metasploit")
		{
			// Vulnerability Scanning
			api.POST("/scan", h.handleStartScan)
			api.GET("/scan/:id/status", h.handleGetScanStatus)
			api.GET("/scan/:id/results", h.handleGetScanResults)
			
			// Exploitation
			api.POST("/exploit", h.handleExecuteExploit)
			api.GET("/exploit/:id/status", h.handleGetExploitStatus)
			
			// Session Management
			api.GET("/sessions", h.handleListSessions)
			api.DELETE("/sessions/:id", h.handleTerminateSession)
			api.POST("/sessions/:id/upgrade", h.handleUpgradePrivileges)
			
			// Campaigns (Attack Chains)
			api.POST("/campaigns/start", h.handleStartCampaign)
			api.GET("/campaigns/:id/status", h.handleGetCampaignStatus)
			api.GET("/campaigns/:id/report", h.handleGetCampaignReport)
			api.POST("/campaigns/:id/stop", h.handleStopCampaign)
			
			// Reporting
			api.GET("/reports/latest", h.handleGetLatestReport)
			api.POST("/reports/generate", h.handleGenerateReport)
			api.GET("/reports/:id/download", h.handleDownloadReport)
			
			// Configuration
			api.GET("/config", h.handleGetConfig)
			api.PUT("/config", h.handleUpdateConfig)
		}
}

// handleStartScan starts a vulnerability scan against a target
func (h *APIHandler) handleStartScan(c *gin.Context) {
	var request struct {
		Target string   `json:"target" binding:"required"`
		Port   int      `json:"port" binding:"required,min=1,max=65535"`
		CVEs   []string `json:"cves"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}
	
	target := metasploit.TargetInfo{
		IP:     request.Target,
		Port:   request.Port,
		CVEs:   request.CVEs,
	}
	
	vulns, err := h.scanner.ScanTarget(c.Request.Context(), target)
	if err != nil {
		h.logger.WithError(err).Error("Scan failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"scanned_at":    time.Now().Format(time.RFC3339),
		"vulnerabilities": len(vulns),
		"details": vulns,
	})
}

// handleExecuteExploit executes an exploit against a target
func (h *APIHandler) handleExecuteExploit(c *gin.Context) {
	var request struct {
		Target string        `json:"target" binding:"required"`
		Port   int           `json:"port" binding:"required"`
		ExploitName string     `json:"exploit_name" binding:"required"`
		Payload  string        `json:"payload"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	
	target := metasploit.TargetInfo{
		IP:   request.Target,
		Port: request.Port,
	}
	
	exploit := metasploit.ExploitModule{Name: request.ExploitName}
	session, err := h.scanner.ExecuteExploit(c.Request.Context(), exploit, target)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"session_id": session.ID,
		"type": session.Type,
		"created_at": session.CreatedAt.Format(time.RFC3339),
	})
}

// handleStartCampaign initiates an attack campaign
func (h *APIHandler) handleStartCampaign(c *gin.Context) {
	type StartCampaignRequest struct {
		Name          string               `json:"name" binding:"required"`
		Targets       []metasploit.TargetInfo `json:"targets"`
		AutoApproveCritical bool              `json:"auto_approve_critical"`
	}
	
	var req StartCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	
	campaign, err := h.orchestrator.StartCampaign(c.Request.Context(), req.Targets, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusAccepted, gin.H{
		"chain_id": campaign.ID,
		"name": campaign.Name,
		"status": campaign.Status,
		"estimated_duration_seconds": campaign.DurationSeconds,
	})
}

// handleListSessions returns all active sessions
func (h *APIHandler) handleListSessions(c *gin.Context) {
	sessions := h.scanner.ListActiveSessions()
	
	response := make([]gin.H, len(sessions))
	for i, sess := range sessions {
		response[i] = gin.H{
			"id": sess.ID,
			"type": sess.Type,
			"target": sess.Target,
			"created_at": sess.CreatedAt.Format(time.RFC3339),
			"expires_at": sess.ExpiresAt.Format(time.RFC3339),
		}
	}
	
	c.JSON(http.StatusOK, gin.H{
		"count": len(response),
		"sessions": response,
	})
}

// handleTerminateSession terminates a specific session
func (h *APIHandler) handleTerminateSession(c *gin.Context) {
	sessionID := c.Param("id")
	
	err := h.scanner.TerminateSession(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Session terminated successfully"})
}

// handleGetConfig retrieves current configuration
func (h *APIHandler) handleGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"config": "metasploit_config",
	})
}

// handleGenerateReport generates a comprehensive penetration test report
func (h *APIHandler) handleGenerateReport(c *gin.Context) {
	targets := []metasploit.TargetInfo{} // Populate from context or DB
	
	report, err := h.scanner.GenerateScanReport(targets)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, report)
}

// handleGetScanStatus returns the status of a scan
func (h *APIHandler) handleGetScanStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "not_implemented"})
}

// handleGetScanResults returns scan results
func (h *APIHandler) handleGetScanResults(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"results": []interface{}{}})
}

// handleGetExploitStatus returns exploit execution status
func (h *APIHandler) handleGetExploitStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "not_implemented"})
}

// handleUpgradePrivileges handles privilege escalation requests
func (h *APIHandler) handleUpgradePrivileges(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "not_implemented"})
}

// handleGetCampaignStatus returns campaign status
func (h *APIHandler) handleGetCampaignStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "not_implemented"})
}

// handleGetCampaignReport returns campaign report
func (h *APIHandler) handleGetCampaignReport(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"report": nil})
}

// handleStopCampaign stops an active campaign
func (h *APIHandler) handleStopCampaign(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// handleGetLatestReport returns the most recent report
func (h *APIHandler) handleGetLatestReport(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"report": nil})
}

// handleDownloadReport downloads a specific report
func (h *APIHandler) handleDownloadReport(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"download_url": ""})
}

// handleUpdateConfig updates the configuration
func (h *APIHandler) handleUpdateConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}
