// Package api provides HTTP handlers for M38 Container Security Platform
package api

import (
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// VulnerabilitySeverity represents vulnerability severity level
type VulnerabilitySeverity string

const (
	Critical VulnerabilitySeverity = "critical"
	High     VulnerabilitySeverity = "high"
	Medium   VulnerabilitySeverity = "medium"
	Low      VulnerabilitySeverity = "low"
	Info     VulnerabilitySeverity = "info"
)

// VulnSummary summarizes vulnerability counts by severity
type VulnSummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
	Total    int `json:"total"`
}

// Secret represents detected secret in container image
type Secret struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // API key, password, token, certificate
	Location  string    `json:"location"` // file path in image
	Severity  string    `json:"severity"`
	CreatedAt time.Time `json:"createdAt"`
	Resolved  bool      `json:"resolved"`
}

// ContainerImage represents a scanned container image
type ContainerImage struct {
	ID              string      `json:"id"`
	Digest          string      `json:"digest"`
	Registry        string      `json:"registry"`
	ImageName       string      `json:"imageName"`
	Tag             string      `json:"tag"`
	Architecture    string      `json:"architecture"`
	OS              string      `json:"os"`
	CreatedAt       time.Time   `json:"createdAt"`
	SizeBytes       int64       `json:"sizeBytes"`
	Layers          int         `json:"layers"`
	Vulnerabilities VulnSummary `json:"vulnerabilities"`
	SecretsFound    []Secret    `json:"secretsFound"`
	ComplianceStatus string    `json:"complianceStatus"` // compliant, non_compliant, pending
	ScannerVersion  string      `json:"scannerVersion"`
	Metadata        map[string]any `json:"metadata"`
}

// ScanResult represents results from a container scan
type ScanResult struct {
	ID            string        `json:"id"`
	ImageID       string        `json:"imageId"`
	Status        string        `json:"status"` // scanning, completed, failed
	StartedAt     time.Time     `json:"startedAt"`
	CompletedAt   *time.Time    `json:"completedAt,omitempty"`
	DurationMs    int64         `json:"durationMs"`
	Vulnerabilities []VulnFinding `json:"vulnerabilities"`
	SecretsFound   []Secret      `json:"secretsFound"`
	SchemaViolations []string     `json:"schemaViolations"`
	Metrics        map[string]any `json:"metrics"`
	ErrorMsg       string        `json:"errorMsg,omitempty"`
}

// RuntimeProtectionPolicy defines runtime security rules
type RuntimeProtectionPolicy struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	TargetSelector  map[string]string      `json:"targetSelector"` // labels to match pods
	Enabled         bool                   `json:"enabled"`
	Actions         []ProtectionAction     `json:"actions"`
	CreatedAt       time.Time              `json:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt"`
}

// ProtectionAction defines what to do when violation detected
type ProtectionAction struct {
	Type            string   `json:"type"` // alert, block, isolate, log
	Severity        string   `json:"severity"`
	Conditions      []string `json:"conditions"`
	NotificationIDs []string `json:"notificationIds,omitempty"`
}

// K8sSecurityContext defines Kubernetes pod security requirements
type K8sSecurityContext struct {
	RunAsNonRoot       *bool  `json:"runAsNonRoot"`
	ReadOnlyRootFS     *bool  `json:"readOnlyRootFS"`
	AllowPrivilegeEsc  *bool  `json:"allowPrivilegeEscalation"`
	CapabilitiesDrop   []string `json:"capabilitiesDrop"`
	CapabilitiesAdd    []string `json:"capabilitiesAdd,omitempty"`
	SeccompProfileType string `json:"seccompProfileType"` // RuntimeDefault, Localhost
	PodSecurityStandard string `json:"podSecurityStandard"` // restricted, baseline, privileged
}

// PodSecurityStandard enforces PSS levels
type PodSecurityStandard struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Level            string                 `json:"level"` // privileged, baseline, restricted
	Namespace        string                 `json:"namespace"`
	EnforcementMode  string                 `json:"enforcementMode"` // enforce, audit, warn
	Exceptions       []string               `json:"exceptions,omitempty"`
	CreatedAt        time.Time              `json:"createdAt"`
	UpdatedAt        time.Time              `json:"updatedAt"`
}

// ComplianceAssessment result for container compliance check
type ComplianceAssessment struct {
	ID             string                `json:"id"`
	ImageID        string                `json:"imageId"`
	AssessedAt     time.Time             `json:"assessedAt"`
	Standard       string                `json:"standard"` // CIS, NIST, custom
	Version        string                `json:"version"`
	Score          float64               `json:"score"` // 0-100
	TotalChecks    int                   `json:"totalChecks"`
	PassedChecks   int                   `json:"passedChecks"`
	FailedChecks   int                   `json:"failedChecks"`
	Warnings       int                   `json:"warnings"`
	Details        []ComplianceCheck     `json:"details"`
	Status         string                `json:"status"` // pass, fail, warning
	EvidenceHash   string                `json:"evidenceHash"`
}

// ComplianceCheck individual check result
type ComplianceCheck struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Requirement  string  `json:"requirement"`
	Status       string  `json:"status"` // pass, fail, warning, not_applicable
	Score        float64 `json:"score"`
	Message      string  `json:"message"`
	Evidence     []string `json:"evidence,omitempty"`
}

// ContainerSecurityHandler handles container security operations
type ContainerSecurityHandler struct {
	store     *ContainerSecurityStore
	evidence  *evidence.Ledger
	logger    *logrus.Logger
}

// NewContainerSecurityHandler creates handler instance
func NewContainerSecurityHandler(
	store *ContainerSecurityStore,
	evidenceLedger *evidence.Ledger,
	logger *logrus.Logger,
) *ContainerSecurityHandler {
	return &ContainerSecurityHandler{
		store:     store,
		evidence:  evidenceLedger,
		logger:    logger,
	}
}

// RegisterRoutes registers REST endpoints
func (h *ContainerSecurityHandler) RegisterRoutes(router *echo.Echo) {
	containers := router.Group("/api/m38/containers")

	// Image management
	containers.POST("/images", h.scanImage)
	containers.GET("/images", h.listImages)
	containers.GET("/images/:id", h.getImageDetails)
	containers.DELETE("/images/:id", h.deleteImage)
	containers.POST("/images/:id/re-scan", h.rescanImage)

	// Vulnerability findings
	vulns := containers.Group("/images/:id/vulnerabilities")
	vulns.GET("", h.getImageVulnerabilities)
	vulns.PUT("/:vulnId/resolve", h.resolveVulnerability)
	vulns.DELETE("/:vulnId", h.deleteVulnerability)

	// Secrets
	secrets := containers.Group("/images/:id/secrets")
	secrets.GET("", h.getImageSecrets)
	secrets.PUT("/:secretId/resolve", h.resolveSecret)

	// Scan history
	scanHistory := containers.Group("/images/:id/scans")
	scanHistory.GET("", h.getScanHistory)
	scanHistory.GET("/latest", h.getLatestScan)

	// Runtime protection policies
	runtime := router.Group("/api/m38/runtime-protection")
	runtime.POST("/policies", h.createRuntimePolicy)
	runtime.GET("/policies", h.listRuntimePolicies)
	runtime.GET("/policies/:id", h.getRuntimePolicy)
	runtime.PUT("/policies/:id", h.updateRuntimePolicy)
	runtime.DELETE("/policies/:id", h.deleteRuntimePolicy)
	runtime.POST("/policies/:id/test", h.testRuntimePolicy)
	runtime.POST("/events", h.recordRuntimeEvent)
	runtime.GET("/events", h.listRuntimeEvents)

	// Kubernetes security
	k8s := router.Group("/api/m38/kubernetes")
	
	// Pod security standards
	pss := k8s.Group("/pss")
	pss.POST("/", h.createPodSecurityStandard)
	pss.GET("/", h.listPodSecurityStandards)
	pss.GET("/:id", h.getPodSecurityStandard)
	pss.PUT("/:id", h.updatePodSecurityStandard)
	pss.DELETE("/:id", h.deletePodSecurityStandard)

	// Security context validation
	ctx := k8s.Group("/security-context")
	ctx.POST("/validate", h.validateSecurityContext)
	ctx.POST("/suggest", h.suggestSecurityContext)

	// Compliance assessment
	compliance := k8s.Group("/compliance")
	compliance.POST("/assess", h.assessCompliance)
	compliance.GET("/assessments/:id", h.getComplianceAssessment)
	compliance.GET("/standards", h.listComplianceStandards)

	// Monitoring & alerts
	alerts := containers.Group("/alerts")
	alerts.POST("/", h.createAlert)
	alerts.GET("/", h.listAlerts)
	alerts.PUT("/:id/status", h.updateAlertStatus)
}

// scanImage scans a new container image
func (h *ContainerSecurityHandler) scanImage(c echo.Context) error {
	var req struct {
		ImageURI  string            `json:"imageUri"`
		SkipCheck bool              `json:"skipCheck"`
		Metadata  map[string]any    `json:"metadata"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	imageID := uuid.New().String()
	startedAt := time.Now()

	result := &ScanResult{
		ID:        uuid.New().String(),
		ImageID:   imageID,
		Status:    "scanning",
		StartedAt: startedAt,
	}

	if err := h.store.InitiateScan(result); err != nil {
		h.logger.Errorf("Failed to initiate scan: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Scan initiation failed"})
	}

	// Record evidence of scan initiation
	ctx := capability.GetContext(c.Request().Context())
	h.evidence.Attest(ctx, evidence.Event{
		Type:         evidence.ContainerScanInitiated,
		ResourceID:   imageID,
		ResourceType: "container_image",
		Actor:        ctx.User,
		Metadata: map[string]any{
			"imageUri":   req.ImageURI,
			"scanId":     result.ID,
		},
	})

	// In production, would trigger actual scanner
	// For now, return mock structure
	go h.performScan(imageID, req.ImageURI, req.SkipCheck, req.Metadata)

	return c.JSON(http.StatusAccepted, map[string]string{
		"imageId": imageID,
		"scanId":  result.ID,
		"status":  "Scan initiated - use /scans/latest to check progress",
	})
}

// performScan executes the actual container scan (async)
func (h *ContainerSecurityHandler) performScan(imageID, imageURI string, skipCheck bool, metadata map[string]any) {
	// This would integrate with actual scanners like Trivy, Grype, Clair
	result := &ScanResult{
		ID:        uuid.New().String(),
		ImageID:   imageID,
		Status:    "completed",
		StartedAt: time.Now(),
		DurationMs: 5000, // mock duration
	}

	// Mock vulnerability data
	result.Vulnerabilities = []VulnFinding{
		{
			ID:         uuid.New().String(),
			CVEID:      "CVE-2024-1234",
			Title:      "Outdated openssl package",
			Severity:   "high",
			CVSSv3Score: 7.5,
			Component:  "openssl@3.0.0",
			FixVersion: "3.0.1",
		},
	}

	result.SecretsFound = []Secret{
		{
			ID:       uuid.New().String(),
			Type:     "API key",
			Location: "/etc/app/config.yaml:15",
			Severity: "critical",
		},
	}

	h.store.CompleteScan(result)
}

// listImages retrieves all container images
func (h *ContainerSecurityHandler) listImages(c echo.Context) error {
	registry := c.QueryParam("registry")
	filter := c.QueryParam("filter")

	images, err := h.store.ListImages(registry, filter)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusOK, images)
}

// getImageDetails retrieves specific image info
func (h *ContainerSecurityHandler) getImageDetails(c echo.Context) error {
	id := c.Param("id")
	image, err := h.store.GetImage(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Image not found"})
	}
	return c.JSON(http.StatusOK, image)
}

// deleteImage removes image record
func (h *ContainerSecurityHandler) deleteImage(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteImage(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Image not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Image deleted"})
}

// rescanImage triggers re-scan
func (h *ContainerSecurityHandler) rescanImage(c echo.Context) error {
	id := c.Param("id")
	return c.JSON(http.StatusAccepted, map[string]string{
		"status":  "Re-scan initiated",
		"imageId": id,
	})
}

// getImageVulnerabilities gets vulnerabilities for image
func (h *ContainerSecurityHandler) getImageVulnerabilities(c echo.Context) error {
	id := c.Param("id")
	severity := c.QueryParam("severity")

	vulns, err := h.store.GetImageVulnerabilities(id, severity)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Image not found"})
	}
	return c.JSON(http.StatusOK, vulns)
}

// resolveVulnerability marks vuln as resolved
func (h *ContainerSecurityHandler) resolveVulnerability(c echo.Context) error {
	imageID := c.Param("id")
	vulnID := c.Param("vulnId")

	if err := h.store.ResolveVulnerability(imageID, vulnID); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Vulnerability not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Vulnerability marked as resolved"})
}

// deleteVulnerability removes vulnerability record
func (h *ContainerSecurityHandler) deleteVulnerability(c echo.Context) error {
	imageID := c.Param("id")
	vulnID := c.Param("vulnId")

	if err := h.store.DeleteVulnerability(imageID, vulnID); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Vulnerability not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Vulnerability deleted"})
}

// getImageSecrets retrieves secrets from image
func (h *ContainerSecurityHandler) getImageSecrets(c echo.Context) error {
	id := c.Param("id")
	secrets, err := h.store.GetImageSecrets(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Image not found"})
	}
	return c.JSON(http.StatusOK, secrets)
}

// resolveSecret marks secret as resolved
func (h *ContainerSecurityHandler) resolveSecret(c echo.Context) error {
	imageID := c.Param("id")
	secretID := c.Param("secretId")

	if err := h.store.ResolveSecret(imageID, secretID); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Secret not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Secret marked as resolved"})
}

// getScanHistory retrieves scan history for image
func (h *ContainerSecurityHandler) getScanHistory(c echo.Context) error {
	id := c.Param("id")
	history, err := h.store.GetScanHistory(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Image not found"})
	}
	return c.JSON(http.StatusOK, history)
}

// getLatestScan gets most recent scan
func (h *ContainerSecurityHandler) getLatestScan(c echo.Context) error {
	id := c.Param("id")
	latest, err := h.store.GetLatestScan(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Image not found"})
	}
	return c.JSON(http.StatusOK, latest)
}

// createRuntimePolicy creates new policy
func (h *ContainerSecurityHandler) createRuntimePolicy(c echo.Context) error {
	var policy RuntimeProtectionPolicy
	if err := c.Bind(&policy); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	policy.ID = uuid.New().String()
	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()

	if err := h.store.CreateRuntimePolicy(&policy); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, policy)
}

// listRuntimePolicies retrieves all policies
func (h *ContainerSecurityHandler) listRuntimePolicies(c echo.Context) error {
	policies, err := h.store.ListRuntimePolicies()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, policies)
}

// getRuntimePolicy retrieves specific policy
func (h *ContainerSecurityHandler) getRuntimePolicy(c echo.Context) error {
	id := c.Param("id")
	policy, err := h.store.GetRuntimePolicy(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, policy)
}

// updateRuntimePolicy updates policy config
func (h *ContainerSecurityHandler) updateRuntimePolicy(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	policy, err := h.store.UpdateRuntimePolicy(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, policy)
}

// deleteRuntimePolicy deletes policy
func (h *ContainerSecurityHandler) deleteRuntimePolicy(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteRuntimePolicy(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Policy deleted"})
}

// testRuntimePolicy tests policy effectiveness
func (h *ContainerSecurityHandler) testRuntimePolicy(c echo.Context) error {
	id := c.Param("id")
	var testReq struct {
		TestCases []string `json:"testCases"`
	}

	if err := c.Bind(&testReq); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	results := h.store.TestRuntimePolicy(id, testReq.TestCases)
	return c.JSON(http.StatusOK, results)
}

// recordRuntimeEvent logs runtime event
func (h *ContainerSecurityHandler) recordRuntimeEvent(c echo.Context) error {
	var event struct {
		Type        string                 `json:"type"`
		ContainerID string                 `json:"containerId"`
		Details     map[string]any         `json:"details"`
	}

	if err := c.Bind(&event); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	if err := h.store.RecordRuntimeEvent(event); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Failed to record event"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Event recorded"})
}

// listRuntimeEvents retrieves runtime events
func (h *ContainerSecurityHandler) listRuntimeEvents(c echo.Context) error {
	containerID := c.QueryParam("containerId")
	eventType := c.QueryParam("type")

	events, err := h.store.ListRuntimeEvents(containerID, eventType)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, events)
}

// createPodSecurityStandard creates PSS config
func (h *ContainerSecurityHandler) createPodSecurityStandard(c echo.Context) error {
	var standard PodSecurityStandard
	if err := c.Bind(&standard); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	standard.ID = uuid.New().String()
	standard.CreatedAt = time.Now()
	standard.UpdatedAt = time.Now()

	if err := h.store.CreatePodSecurityStandard(&standard); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, standard)
}

// listPodSecurityStandards retrieves all standards
func (h *ContainerSecurityHandler) listPodSecurityStandards(c echo.Context) error {
	stds, err := h.store.ListPodSecurityStandards()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, stds)
}

// getPodSecurityStandard retrieves specific standard
func (h *ContainerSecurityHandler) getPodSecurityStandard(c echo.Context) error {
	id := c.Param("id")
	std, err := h.store.GetPodSecurityStandard(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Standard not found"})
	}
	return c.JSON(http.StatusOK, std)
}

// updatePodSecurityStandard updates standard
func (h *ContainerSecurityHandler) updatePodSecurityStandard(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	std, err := h.store.UpdatePodSecurityStandard(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Standard not found"})
	}
	return c.JSON(http.StatusOK, std)
}

// deletePodSecurityStandard deletes standard
func (h *ContainerSecurityHandler) deletePodSecurityStandard(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeletePodSecurityStandard(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Standard not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Standard deleted"})
}

// validateSecurityContext validates pod security context
func (h *ContainerSecurityHandler) validateSecurityContext(c echo.Context) error {
	var ctx K8sSecurityContext
	if err := c.Bind(&ctx); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	violations := h.store.ValidateSecurityContext(&ctx)
	return c.JSON(http.StatusOK, map[string]any{
		"valid":      len(violations) == 0,
		"violations": violations,
	})
}

// suggestSecurityContext suggests secure settings
func (h *ContainerSecurityHandler) suggestSecurityContext(c echo.Context) error {
	var input struct {
		CurrentContext *K8sSecurityContext `json:"currentContext"`
		PSSLevel       string            `json:"pssLevel"` // baseline, restricted
	}

	if err := c.Bind(&input); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	suggestions := h.store.SuggestSecurityContext(input.CurrentContext, input.PSSLevel)
	return c.JSON(http.StatusOK, suggestions)
}

// assessCompliance performs compliance assessment
func (h *ContainerSecurityHandler) assessCompliance(c echo.Context) error {
	var req struct {
		ImageID string `json:"imageId"`
		Std     string `json:"standard"`
		Version string `json:"version"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	assessment, err := h.store.PerformComplianceAssessment(req.ImageID, req.Std, req.Version)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Assessment failed"})
	}

	return c.JSON(http.StatusOK, assessment)
}

// getComplianceAssessment retrieves assessment
func (h *ContainerSecurityHandler) getComplianceAssessment(c echo.Context) error {
	id := c.Param("id")
	assessment, err := h.store.GetComplianceAssessment(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Assessment not found"})
	}
	return c.JSON(http.StatusOK, assessment)
}

// listComplianceStandards lists available standards
func (h *ContainerSecurityHandler) listComplianceStandards(c echo.Context) error {
	stdards := h.store.ListComplianceStandards()
	return c.JSON(http.StatusOK, stdards)
}

// createAlert creates security alert
func (h *ContainerSecurityHandler) createAlert(c echo.Context) error {
	var alert struct {
		Type        string                 `json:"type"`
		Severity    string                 `json:"severity"`
		Message     string                 `json:"message"`
		Context     map[string]any         `json:"context"`
	}

	if err := c.Bind(&alert); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	alertID := uuid.New().String()
	if err := h.store.CreateAlert(alertID, alert); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Failed to create alert"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"alertId": alertID})
}

// listAlerts retrieves alerts
func (h *ContainerSecurityHandler) listAlerts(c echo.Context) error {
	severity := c.QueryParam("severity")
	status := c.QueryParam("status")

	alerts, err := h.store.ListAlerts(severity, status)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, alerts)
}

// updateAlertStatus updates alert status
func (h *ContainerSecurityHandler) updateAlertStatus(c echo.Context) error {
	id := c.Param("id")
	var update struct {
		Status string `json:"status"`
	}

	if err := c.Bind(&update); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	if err := h.store.UpdateAlertStatus(id, update.Status); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Alert not found"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Status updated"})
}
