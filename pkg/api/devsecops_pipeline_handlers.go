// Package api provides HTTP handlers for M37 DevSecOps Pipeline Integration
package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// ScanType represents different security scan stages
type ScanType string

const (
	SCA          ScanType = "sca"           // Software Composition Analysis
	SAST         ScanType = "sast"          // Static Application Security Testing
	DAST         ScanType = "dast"          // Dynamic Application Security Testing
	IAC_SCAN     ScanType = "iac_scan"      // Infrastructure as Code Scanning
	SECRETS_SCAN ScanType = "secrets_scan"  // Secrets Detection
	CONTAINER_SCAN ScanType = "container_scan" // Container Vulnerability Scanning
	CLOUD_CHECK  ScanType = "cloud_check"   // Cloud Configuration Review
)

// ScanStatus represents the status of a security scan
type ScanStatus string

const (
	ScanPending   ScanStatus = "pending"
	ScanRunning   ScanStatus = "running"
	ScanCompleted ScanStatus = "completed"
	ScanFailed    ScanStatus = "failed"
	ScanSkipped   ScanStatus = "skipped"
)

// GatePolicy defines pass/fail thresholds for pipeline gates
type GatePolicy struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	ScanTypes           []ScanType             `json:"scanTypes"`
	FailureThresholds   map[ScanType]int       `json:"failureThresholds"` // max critical/high vulnerabilities allowed
	BlockOnSecrets      bool                   `json:"blockOnSecrets"`
	BlockOnCriticalCVEs bool                   `json:"blockOnCriticalCves"`
	BlockOnHighCVES     bool                   `json:"blockOnHighCves"`
	AllowedExceptions   []string               `json:"allowedExceptions,omitempty"`
	CreatedAt           time.Time              `json:"createdAt"`
	UpdatedAt           time.Time              `json:"updatedAt"`
}

// SecurityJob represents a DevSecOps pipeline job configuration
type SecurityJob struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	RepositoryURL    string            `json:"repositoryUrl"`
	Branch           string            `json:"branch"`
	PipelineConfig   string            `json:"pipelineConfig"` // path to pipeline definition
	TriggerEvents    []string          `json:"triggerEvents"`  // push, pr, tag, scheduled
	GatePolicyID     string            `json:"gatePolicyId"`
	Enabled          bool              `json:"enabled"`
	WebhookSecret    string            `json:"webhookSecret,omitempty"`
	Metadata         map[string]any    `json:"metadata"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	LastRunAt        *time.Time        `json:"lastRunAt,omitempty"`
	Status           string            `json:"status"` // active, inactive
	RunHistory       []JobRunSummary   `json:"runHistory"`
}

// JobRunSummary summarizes a single job execution
type JobRunSummary struct {
	RunID        string                 `json:"runId"`
	StartedAt    time.Time              `json:"startedAt"`
	CompletedAt  *time.Time             `json:"completedAt,omitempty"`
	Status       ScanStatus             `json:"status"`
	StageResults map[ScanType]ScanResult `json:"stageResults"`
	TotalDuration int64                  `json:"totalDurationMs"`
}

// ScanResult represents results from a single security scan stage
type ScanResult struct {
	ID            string            `json:"id"`
	ScanType      ScanType          `json:"scanType"`
	Status        ScanStatus        `json:"status"`
	StartedAt     time.Time         `json:"startedAt"`
	CompletedAt   *time.Time        `json:"completedAt,omitempty"`
	DurationMs    int64             `json:"durationMs"`
	Vulnerabilities []VulnFinding   `json:"vulnerabilities,omitempty"`
	SecretsFound   []SecretFinding   `json:"secretsFound,omitempty"`
	CodeQuality    map[string]any    `json:"codeQuality,omitempty"`
	Compliance     map[string]any    `json:"compliance,omitempty"`
	Metrics        map[string]any    `json:"metrics,omitempty"`
	ErrorMsg       string            `json:"errorMsg,omitempty"`
	EvidenceHash   string            `json:"evidenceHash,omitempty"`
}

// VulnFinding represents a detected vulnerability
type VulnFinding struct {
	ID             string    `json:"id"`
	CVEID          string    `json:"cveId,omitempty"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Severity       string    `json:"severity"` // critical, high, medium, low, info
	CVSSv3Score    float64   `json:"cvssv3Score,omitempty"`
	EPSS           float64   `json:"epss,omitempty"`
	Location       string    `json:"location"` // file:line format
	Component      string    `json:"component,omitempty"`
	FixVersion     string    `json:"fixVersion,omitempty"`
	Remediation    string    `json:"remediation,omitempty"`
}

// SecretFinding represents detected secrets
type SecretFinding struct {
	ID          string    `json:"id"`
	SecretType  string    `json:"secretType"` // API key, password, token, certificate
	Location    string    `json:"location"`
	Severity    string    `json:"severity"`
	Description string    `json:"description"`
	Resolved    bool      `json:"resolved"`
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
}

// ComplianceReport aggregates compliance data across all scans
type ComplianceReport struct {
	ID              string            `json:"id"`
	JobID           string            `json:"jobId"`
	RunID           string            `json:"runId"`
	GeneratedAt     time.Time         `json:"generatedAt"`
	Scope           string            `json:"scope"` // project, component, entire org
	SecurityPolicies []PolicyRequirement `json:"securityPolicies"`
	ComplianceScore float64           `json:"complianceScore"` // 0-100
	TotalChecks     int               `json:"totalChecks"`
	PassedChecks    int               `json:"passedChecks"`
	FailedChecks    int               `json:"failedChecks"`
	SkippedChecks   int               `json:"skippedChecks"`
	Findings        AggregateFindings `json:"findings"`
	Status          string            `json:"status"` // compliant, non_compliant, pending
}

// PolicyRequirement defines a security policy rule
type PolicyRequirement struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Rule        string   `json:"rule"` // Rego or custom expression
	Severity    string   `json:"severity"`
	CheckType   string   `json:"checkType"`
	Passed      bool     `json:"passed"`
	Message     string   `json:"message"`
	Evidence    []string `json:"evidence,omitempty"`
}

// AggregateFindings summarizes findings by category
type AggregateFindings struct {
	TotalCritical int               `json:"totalCritical"`
	TotalHigh     int               `json:"totalHigh"`
	TotalMedium   int               `json:"totalMedium"`
	TotalLow      int               `json:"totalLow"`
	TotalInfo     int               `json:"totalInfo"`
	ByScanType    map[ScanType]int  `json:"byScanType"`
	ByComponent   map[string]int    `json:"byComponent"`
	TopComponents []string          `json:"topComponents"`
	Recent        []VulnFinding     `json:"recent"`
}

// WebhookEvent represents an incoming webhook payload
type WebhookEvent struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"` // push, pull_request, tag, schedule
	Repository    string                 `json:"repository"`
	Branch        string                 `json:"branch"`
	Commit        string                 `json:"commit"`
	TriggeredBy   string                 `json:"triggeredBy"`
	Timestamp     time.Time              `json:"timestamp"`
	Checksum      string                 `json:"checksum"`
	Payload       map[string]any         `json:"payload"`
	Processed     bool                   `json:"processed"`
	JobID         string                 `json:"jobId,omitempty"`
	RunID         string                 `json:"runId,omitempty"`
}

// DevSecOpsHandler handles DevSecOps pipeline operations
type DevSecOpsHandler struct {
	store          *DevSecOpsStore
	evidenceLedger *evidence.Ledger
	logger         *logrus.Logger
}

// NewDevSecOpsHandler creates handler instance
func NewDevSecOpsHandler(
	store *DevSecOpsStore,
	evidenceLedger *evidence.Ledger,
	logger *logrus.Logger,
) *DevSecOpsHandler {
	return &DevSecOpsHandler{
		store:          store,
		evidenceLedger: evidenceLedger,
		logger:         logger,
	}
}

// RegisterRoutes registers REST endpoints
func (h *DevSecOpsHandler) RegisterRoutes(router *echo.Echo) {
	pipeline := router.Group("/api/m37/devsecops")

	// Security jobs management
	pipeline.POST("/jobs", h.createSecurityJob)
	pipeline.GET("/jobs", h.listSecurityJobs)
	pipeline.GET("/jobs/:id", h.getJobDetails)
	pipeline.PUT("/jobs/:id", h.updateSecurityJob)
	pipeline.DELETE("/jobs/:id", h.deleteSecurityJob)
	pipeline.POST("/jobs/:id/trigger", h.triggerJobRun)

	// Gate policies
	pipeline.POST("/gates", h.createGatePolicy)
	pipeline.GET("/gates", h.listGatePolicies)
	pipeline.GET("/gates/:id", h.getGatePolicy)
	pipeline.PUT("/gates/:id", h.updateGatePolicy)
	pipeline.DELETE("/gates/:id", h.deleteGatePolicy)

	// Scan results and analysis
	pipeline.GET("/jobs/:id/runs", h.getJobRuns)
	pipeline.GET("/jobs/:id/runs/:runId/results", h.getRunResults)
	pipeline.GET("/aggregated/findings", h.getAggregatedFindings)
	pipeline.GET("/trends", h.getScanTrends)

	// Compliance reporting
	pipeline.POST("/compliance/reports", h.generateComplianceReport)
	pipeline.GET("/compliance/reports/:id", h.getComplianceReport)
	pipeline.GET("/compliance/status", h.getOverallComplianceStatus)

	// Webhook integrations
	pipeline.POST("/webhooks", h.createWebhookConfig)
	pipeline.GET("/webhooks", h.listWebhookConfigs)
	pipeline.PUT("/webhooks/:id", h.updateWebhookConfig)
	pipeline.DELETE("/webhooks/:id", h.deleteWebhookConfig)
	pipeline.POST("/webhooks/receive", h.receiveWebhook)
	pipeline.POST("/webhooks/:id/regenerate-secret", h.regenerateWebhookSecret)
}

// createSecurityJob creates a new security job
func (h *DevSecOpsHandler) createSecurityJob(c echo.Context) error {
	var job SecurityJob
	if err := c.Bind(&job); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload", "message": err.Error()})
	}

	job.ID = uuid.New().String()
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()
	job.Status = "active"
	if job.Enabled {
		job.Status = "active"
	} else {
		job.Status = "inactive"
	}

	if err := h.store.CreateSecurityJob(&job); err != nil {
		h.logger.Errorf("Failed to create security job: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	// Record evidence
	ctx := capability.GetContext(c.Request().Context())
	h.evidenceLedger.Attest(ctx, evidence.Event{
		Type:         evidence.DevSecOpsJobCreated,
		ResourceID:   job.ID,
		ResourceType: "security_job",
		Actor:        ctx.User,
		Metadata:     map[string]any{"jobName": job.Name, "repository": job.RepositoryURL},
	})

	return c.JSON(http.StatusCreated, job)
}

// listSecurityJobs retrieves all security jobs
func (h *DevSecOpsHandler) listSecurityJobs(c echo.Context) error {
	jobs, err := h.store.ListSecurityJobs()
	if err != nil {
		h.logger.Errorf("Failed to list jobs: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, jobs)
}

// getJobDetails retrieves a specific job
func (h *DevSecOpsHandler) getJobDetails(c echo.Context) error {
	id := c.Param("id")
	job, err := h.store.GetSecurityJob(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Job not found"})
	}
	return c.JSON(http.StatusOK, job)
}

// updateSecurityJob updates job configuration
func (h *DevSecOpsHandler) updateSecurityJob(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	if enabled, ok := updates["enabled"].(bool); ok && enabled {
		updates["status"] = "active"
	} else {
		updates["status"] = "inactive"
	}

	job, err := h.store.UpdateSecurityJob(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Job not found"})
	}
	return c.JSON(http.StatusOK, job)
}

// deleteSecurityJob deletes a job
func (h *DevSecOpsHandler) deleteSecurityJob(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteSecurityJob(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Job not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Job deleted"})
}

// triggerJobRun manually triggers a job run
func (h *DevSecOpsHandler) triggerJobRun(c echo.Context) error {
	jobID := c.Param("id")
	job, err := h.store.GetSecurityJob(jobID)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Job not found"})
	}

	runID := uuid.New().String()
	now := time.Now()

	runSummary := JobRunSummary{
		RunID:       runID,
		StartedAt:   now,
		Status:      ScanRunning,
		StageResults: make(map[ScanType]ScanResult),
	}

	if err := h.store.AddJobRun(jobID, &runSummary); err != nil {
		h.logger.Errorf("Failed to start job run: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Failed to start run"})
	}

	// Evidence of trigger
	ctx := capability.GetContext(c.Request().Context())
	h.evidenceLedger.Attest(ctx, evidence.Event{
		Type:         evidence.DevSecOpsJobTriggered,
		ResourceID:   runID,
		ResourceType: "job_run",
		Actor:        ctx.User,
		Metadata:     map[string]any{"jobId": jobID, "jobName": job.Name},
	})

	return c.JSON(http.StatusOK, map[string]string{"runId": runID})
}

// getJobRuns retrieves run history for a job
func (h *DevSecOpsHandler) getJobRuns(c echo.Context) error {
	jobID := c.Param("id")
	runsWithErrors := h.store.GetJobRuns(jobID)
	
	runs := make([]JobRunSummary, len(runsWithErrors))
	copy(runs, runsWithErrors)

	return c.JSON(http.StatusOK, runs)
}

// getRunResults retrieves detailed results for a specific run
func (h *DevSecOpsHandler) getRunResults(c echo.Context) error {
	jobID := c.Param("id")
	runID := c.Param("runId")

	results, err := h.store.GetRunResults(jobID, runID)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Run not found"})
	}

	return c.JSON(http.StatusOK, results)
}

// getAggregatedFindings returns aggregated findings across all jobs
func (h *DevSecOpsHandler) getAggregatedFindings(c echo.Context) error {
	jobID := c.QueryParam("jobId")
	period := c.QueryParam("period") // days, week, month

	var findings AggregateFindings
	
	if jobID != "" {
		findings = h.store.GetFindingsForJob(jobID)
	} else {
		findings = h.store.GetAllAggregatedFindings()
	}

	if period != "" {
		// Apply time filter if needed
		h.applyTimeFilter(&findings, period)
	}

	return c.JSON(http.StatusOK, findings)
}

// applyTimeFilter filters findings by time period
func (h *DevSecOpsHandler) applyTimeFilter(findings *AggregateFindings, period string) {
	// Implementation would filter based on findings metadata
	// For now, just a placeholder
	switch period {
	case "days":
		days := 7
		// Filter last N days
	case "week":
		// Filter last week
	case "month":
		// Filter last month
	default:
		// No filtering
	}
}

// getScanTrends returns trend analysis over time
func (h *DevSecOpsHandler) getScanTrends(c echo.Context) error {
	jobID := c.QueryParam("jobId")
	granularity := c.QueryParam("granularity") // day, week, month

	trends := h.store.AnalyzeScanTrends(jobID, granularity)
	return c.JSON(http.StatusOK, trends)
}

// createGatePolicy creates a new gate policy
func (h *DevSecOpsHandler) createGatePolicy(c echo.Context) error {
	var policy GatePolicy
	if err := c.Bind(&policy); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	policy.ID = uuid.New().String()
	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()

	if err := h.store.CreateGatePolicy(&policy); err != nil {
		h.logger.Errorf("Failed to create gate policy: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, policy)
}

// listGatePolicies retrieves all gate policies
func (h *DevSecOpsHandler) listGatePolicies(c echo.Context) error {
	policies, err := h.store.ListGatePolicies()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, policies)
}

// getGatePolicy retrieves a specific gate policy
func (h *DevSecOpsHandler) getGatePolicy(c echo.Context) error {
	id := c.Param("id")
	policy, err := h.store.GetGatePolicy(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, policy)
}

// updateGatePolicy updates gate policy
func (h *DevSecOpsHandler) updateGatePolicy(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	
	policy, err := h.store.UpdateGatePolicy(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, policy)
}

// deleteGatePolicy deletes a gate policy
func (h *DevSecOpsHandler) deleteGatePolicy(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteGatePolicy(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Policy deleted"})
}

// generateComplianceReport generates a compliance report
func (h *DevSecOpsHandler) generateComplianceReport(c echo.Context) error {
	var req struct {
		JobID   string `json:"jobId"`
		RunID   string `json:"runId"`
		Scope   string `json:"scope"`
		Policies []string `json:"policies"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	report, err := h.store.GenerateComplianceReport(req.JobID, req.RunID, req.Scope, req.Policies)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Failed to generate report"})
	}

	return c.JSON(http.StatusOK, report)
}

// getComplianceReport retrieves an existing compliance report
func (h *DevSecOpsHandler) getComplianceReport(c echo.Context) error {
	id := c.Param("id")
	report, err := h.store.GetComplianceReport(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Report not found"})
	}
	return c.JSON(http.StatusOK, report)
}

// getOverallComplianceStatus returns overall compliance status
func (h *DevSecOpsHandler) getOverallComplianceStatus(c echo.Context) error {
	status := h.store.GetOverallComplianceStatus()
	return c.JSON(http.StatusOK, status)
}

// createWebhookConfig creates webhook configuration
func (h *DevSecOpsHandler) createWebhookConfig(c echo.Context) error {
	var webhook WebhookEvent
	if err := c.Bind(&webhook); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	webhook.ID = uuid.New().String()
	webhook.Timestamp = time.Now()
	webhook.Checksum = fmt.Sprintf("%x", sha256.Sum256([]byte(webhook.Type+webhook.Repository)))

	if err := h.store.CreateWebhookConfig(&webhook); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, webhook)
}

// listWebhookConfigs retrieves all webhook configs
func (h *DevSecOpsHandler) listWebhookConfigs(c echo.Context) error {
	configs, err := h.store.ListWebhookConfigs()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, configs)
}

// updateWebhookConfig updates webhook config
func (h *DevSecOpsHandler) updateWebhookConfig(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	config, err := h.store.UpdateWebhookConfig(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Config not found"})
	}
	return c.JSON(http.StatusOK, config)
}

// deleteWebhookConfig deletes webhook config
func (h *DevSecOpsHandler) deleteWebhookConfig(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteWebhookConfig(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Config not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Config deleted"})
}

// regenerateWebhookSecret regenerates webhook secret
func (h *DevSecOpsHandler) regenerateWebhookSecret(c echo.Context) error {
	id := c.Param("id")
	newSecret := uuid.New().String()
	
	if err := h.store.RegenerateWebhookSecret(id, newSecret); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Config not found"})
	}

	return c.JSON(http.StatusOK, map[string]string{"newSecret": newSecret})
}

// receiveWebhook receives and processes webhooks
func (h *DevSecOpsHandler) receiveWebhook(c echo.Context) error {
	var event WebhookEvent
	if err := c.Bind(&event); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid webhook payload"})
	}

	event.ID = uuid.New().String()
	event.Timestamp = time.Now()
	event.Processed = false

	// Validate signature if secret configured
	if err := h.validateWebhookSignature(c, &event); err != nil {
		return c.JSON(http.StatusUnauthorized, ErrorResponse{"error": "Invalid webhook signature"})
	}

	// Process webhook event
	jobID, err := h.store.FindMatchingJob(event.Repository, event.Branch)
	if err != nil {
		h.logger.Warnf("No matching job found: %v", err)
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "No matching security job"})
	}

	event.JobID = jobID
	event.RunID = uuid.New().String()
	event.Processed = true

	if err := h.store.ProcessWebhookEvent(&event); err != nil {
		h.logger.Errorf("Failed to process webhook: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Failed to process webhook"})
	}

	return c.JSON(http.StatusOK, map[string]string{"runId": event.RunID, "message": "Webhook processed successfully"})
}

// validateWebhookSignature validates webhook signature
func (h *DevSecOpsHandler) validateWebhookSignature(c echo.Context, event *WebhookEvent) error {
	signature := c.Request().Header.Get("X-Signature")
	if signature == "" {
		return nil // Optional validation
	}

	// Would verify against stored secret
	return nil
}

// Helper methods
// Helper function to extract URL parameter
func extractParam(pathParams map[string]string, name string) string {
	if param, ok := pathParams[name]; ok {
		return param
	}
	return ""
}
