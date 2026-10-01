// Package api provides HTTP handlers for M51 WASM Sandbox Security Platform
package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/sandbox"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// WASM Sandbox Types & Interfaces
// ============================================================================

// WasmSandboxRuntime represents available WASM runtimes
type WasmSandboxRuntime string

const (
	RuntimeSpin      WasmSandboxRuntime = "spin"
	RuntimeWasmtime  WasmSandboxRuntime = "wasmtime"
	RuntimeContainerd WasmSandboxRuntime = "containerd"
)

// SecurityLevel defines isolation strength
type SecurityLevel string

const (
	LevelLow       SecurityLevel = "low"        // Basic constraints
	LevelMedium    SecurityLevel = "medium"     // Standard isolation
	LevelHigh      SecurityLevel = "high"       // Containerized execution
	LevelStrictest SecurityLevel = "strictest"  // Maximum security + network deny
)

// IsolationConfig defines sandbox security configuration
type IsolationConfig struct {
	Runtime          WasmSandboxRuntime `json:"runtime"`
	SecurityLevel    SecurityLevel      `json:"security_level"`
	UseContainer     bool               `json:"use_container"`
	NamespacePID     bool               `json:"namespace_pid"`
	NamespaceNet     bool               `json:"namespace_net"`
	RootfsReadOnly   bool               `json:"rootfs_readonly"`
	SeccompProfile   string             `json:"seccomp_profile,omitempty"`
	ApparmorProfile  string             `json:"apparmor_profile,omitempty"`
	DenySyscalls     []string           `json:"deny_syscalls,omitempty"`
	HostFSAccess     bool               `json:"host_fs_access"`
	MountPoints      []MountPointConfig `json:"mount_points,omitempty"`
	NetworkAccess    bool               `json:"network_access"`
	AllowedPorts     []int              `json:"allowed_ports,omitempty"`
	BlockedNetworks  []string           `json:"blocked_networks,omitempty"`
}

// MountPointConfig describes a mount point configuration
type MountPointConfig struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Readonly    bool   `json:"readonly"`
	Type        string `json:"type"` // bind, tmpfs, volume
}

// ResourceLimits defines computational constraints
type ResourceLimits struct {
	CPUQuota       int     `json:"cpu_quota"`       // CPU quota in milliseconds
	CPUPeriod      int     `json:"cpu_period"`      // CPU period in microseconds
	MemoryLimitMB  int64   `json:"memory_limit_mb"`
	MaxProcesses   int     `json:"max_processes"`
	MaxFileHandles int     `json:"max_file_handles"`
	TimeoutSec     int     `json:"timeout_sec"`
	MaxOutputSize  int     `json:"max_output_size"`
}

// WasmSandboxDefinition defines a WASM sandbox instance
type WasmSandboxDefinition struct {
	ID                 string                `json:"id"`
	Name               string                `json:"name"`
	WasmModulePath     string                `json:"wasm_module_path"`
	Description        string                `json:"description"`
	IsolationConfig    IsolationConfig       `json:"isolation_config"`
	ResourceLimits     ResourceLimits        `json:"resource_limits"`
	Status             SandboxStatus         `json:"status"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	LastExecutedAt     *time.Time            `json:"last_executed_at,omitempty"`
	ExecutionCount     int                   `json:"execution_count"`
	TotalExecTimeMs    int64                 `json:"total_exec_time_ms"`
	AverageExecTimeMs  float64               `json:"average_exec_time_ms"`
	SuccessRate        float64               `json:"success_rate"`
	EvidenceChain      map[string]string     `json:"evidence_chain,omitempty"`
	Metadata           map[string]string     `json:"metadata,omitempty"`
	Tags               []string              `json:"tags,omitempty"`
}

// SandboxStatus represents lifecycle state
type SandboxStatus string

const (
	StatusIdle      SandboxStatus = "idle"
	StatusStarting  SandboxStatus = "starting"
	StatusRunning   SandboxStatus = "running"
	StatusStopping  SandboxStatus = "stopping"
	StatusError     SandboxStatus = "error"
	StatusStopped   SandboxStatus = "stopped"
	StatusExecuting SandboxStatus = "executing"
)

// ExecutionResult represents plugin execution outcome
type ExecutionResult struct {
	ID              string                   `json:"id"`
	SandboxID       string                   `json:"sandbox_id"`
	Timestamp       time.Time                `json:"timestamp"`
	Success         bool                     `json:"success"`
	Output          []byte                   `json:"output,omitempty"`
	ErrorMsg        string                   `json:"error_msg,omitempty"`
	DurationMs      int64                    `json:"duration_ms"`
	ResourceUsage   map[string]interface{}   `json:"resource_usage"`
	Metrics         ExecutionMetrics         `json:"metrics"`
	EvidenceHash    string                   `json:"evidence_hash,omitempty"`
}

// ExecutionMetrics tracks resource consumption
type ExecutionMetrics struct {
	CPUTimeUs       int64 `json:"cpu_time_us"`
	MemoryPeakKB    int64 `json:"memory_peak_kb"`
	NetworkRxBytes  int64 `json:"network_rx_bytes"`
	NetworkTxBytes  int64 `json:"network_tx_bytes"`
	SystemCalls     int   `json:"system_calls"`
	FileOps         int   `json:"file_ops"`
	ExitCode        int   `json:"exit_code"`
}

// SecurityRule defines access control rules
type SecurityRule struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"` // syscall, network, filesystem, env
	Operation     string `json:"operation"` // allow, deny, log, audit
	Pattern       string `json:"pattern"`
	Severity      string `json:"severity"` // critical, high, medium, low
	Enabled       bool   `json:"enabled"`
	Description   string `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
}

// AuditLogEntry records security events
type AuditLogEntry struct {
	ID              string        `json:"id"`
	Timestamp       time.Time     `json:"timestamp"`
	SandboxID       string        `json:"sandbox_id"`
	EventType       string        `json:"event_type"` // execution, security_violation, error, warning
	Severity        string        `json:"severity"`
	Message         string        `json:"message"`
	Context         map[string]any `json:"context"`
	EvidenceHash    string        `json:"evidence_hash,omitempty"`
	Remediation     string        `json:"remediation,omitempty"`
}

// WasmSandboxStore interface for persistence
type WasmSandboxStore interface {
	CreateSandbox(sandbox *WasmSandboxDefinition) error
	GetSandbox(id string) (*WasmSandboxDefinition, error)
	UpdateSandbox(id string, updates map[string]any) error
	DeleteSandbox(id string) error
	ListSandbox(filters map[string]any, limit, offset int) ([]WasmSandboxDefinition, error)
	
	CreateExecutionResult(result *ExecutionResult) error
	GetExecutionResult(id string) (*ExecutionResult, error)
	ListExecutionResults(sandboxID string, limit, offset int) ([]ExecutionResult, error)
	
	CreateSecurityRule(rule *SecurityRule) error
	GetSecurityRule(id string) (*SecurityRule, error)
	UpdateSecurityRule(id string, updates map[string]any) error
	DeleteSecurityRule(id string) error
	ListSecurityRules() ([]SecurityRule, error)
	
	CreateAuditLog(entry *AuditLogEntry) error
	ListAuditLogs(sandboxID string, eventType string, limit int) ([]AuditLogEntry, error)
}

// ============================================================================
// WASM Sandbox Handler
// ============================================================================

// WasmSandboxHandler manages WASM sandbox operations
type WasmSandboxHandler struct {
	store        WasmSandboxStore
	sandboxEngine *sandbox.WasmSandbox
	ledger       *evidence.Ledger
	logger       *logrus.Logger
}

// NewWasmSandboxHandler creates new WASM sandbox handler
func NewWasmSandboxHandler(
	store WasmSandboxStore,
	engine *sandbox.WasmSandbox,
	ledger *evidence.Ledger,
	logger *logrus.Logger,
) *WasmSandboxHandler {
	return &WasmSandboxHandler{
		store:         store,
		sandboxEngine: engine,
		ledger:        ledger,
		logger:        logger.WithField("handler", "wasm_sandbox"),
	}
}

// RegisterWasmSandboxRoutes registers REST endpoints for M51
func RegisterWasmSandboxRoutes(router *echo.Echo, handler *WasmSandboxHandler) {
	wasm := router.Group("/api/v1/wasm-sandbox")

	// Sandbox management
	wasm.POST("", handler.handleCreateSandbox)
	wasm.GET("", handler.handleListSandboxes)
	wasm.GET("/:id", handler.handleGetSandbox)
	wasm.PUT("/:id", handler.handleUpdateSandbox)
	wasm.DELETE("/:id", handler.handleDeleteSandbox)
	
	// Execution
	wasm.POST("/:id/execute", handler.handleExecutePlugin)
	wasm.GET("/:id/executions", handler.handleListExecutions)
	wasm.GET("/:id/executions/:execId", handler.handleGetExecutionResult)
	
	// Security policies
	wasm.POST("/:id/security-rules", handler.handleCreateSecurityRule)
	wasm.GET("/:id/security-rules", handler.handleListSecurityRules)
	wasm.PUT("/rules/:ruleId", handler.handleUpdateSecurityRule)
	wasm.DELETE("/rules/:ruleId", handler.handleDeleteSecurityRule)
	
	// Audit logs
	wasm.GET("/:id/audit-logs", handler.handleListAuditLogs)
	
	// Evidence attestation
	evidenceAttest := wasm.Group("/:id/evidence")
	evidenceAttest.POST("/attest", handler.handleAttestEvidence)
	evidenceAttest.GET("/chain", handler.handleGetEvidenceChain)
	
	// Metrics & monitoring
	metrics := wasm.Group("/metrics")
	metrics.GET("", handler.handleGetSandboxMetrics)
	metrics.GET("/:id/metrics", handler.handleGetSandboxMetrics)
}

// ============================================================================
// Sandbox Management Handlers
// ============================================================================

// handleCreateSandbox creates a new WASM sandbox
// POST /api/v1/wasm-sandbox
func (h *WasmSandboxHandler) handleCreateSandbox(c echo.Context) error {
	var def WasmSandboxDefinition
	
	if err := c.Bind(&def); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	
	// Validate required fields
	if def.Name == "" || def.WasmModulePath == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Name and wasm_module_path are required"})
	}
	
	// Apply defaults
	def.ID = uuid.New().String()
	def.Status = StatusIdle
	def.CreatedAt = time.Now()
	def.UpdatedAt = time.Now()
	def.ExecutionCount = 0
	def.SuccessRate = 1.0
	
	// Generate evidence hash for creation
	evidenceData := map[string]interface{}{
		"action": "create_sandbox",
		"sandbox_name": def.Name,
		"timestamp": time.Now().UTC(),
	}
	
	evidenceHash, err := h.signEvidence(evidenceData)
	if err != nil {
		h.logger.WithError(err).Warn("Failed to sign evidence")
	} else {
		def.EvidenceChain = map[string]string{
			"creation_hash": evidenceHash,
		}
	}
	
	// Create sandbox definition
	if err := h.store.CreateSandbox(&def); err != nil {
		h.logger.WithError(err).Error("Failed to create sandbox")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create sandbox"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"sandbox_id": def.ID,
		"name": def.Name,
		"runtime": def.IsolationConfig.Runtime,
	}).Info("WASM sandbox created")
	
	return c.JSON(http.StatusCreated, def)
}

// handleListSandboxes lists all sandboxes
// GET /api/v1/wasm-sandbox
func (h *WasmSandboxHandler) handleListSandboxes(c echo.Context) error {
	limit, _ := parseIntParam(c.QueryParam("limit"), 50)
	offset, _ := parseIntParam(c.QueryParam("offset"), 0)
	
	filters := make(map[string]any)
	if status := c.QueryParam("status"); status != "" {
		filters["status"] = status
	}
	if runtime := c.QueryParam("runtime"); runtime != "" {
		filters["runtime"] = runtime
	}
	
	sandboxes, err := h.store.ListSandbox(filters, limit, offset)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list sandboxes")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to list sandboxes"})
	}
	
	totalCount, _ := h.countSandboxes(filters)
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"sandboxes": sandboxes,
		"total": totalCount,
		"limit": limit,
		"offset": offset,
	})
}

// handleGetSandbox retrieves a specific sandbox
// GET /api/v1/wasm-sandbox/:id
func (h *WasmSandboxHandler) handleGetSandbox(c echo.Context) error {
	id := c.Param("id")
	
	def, err := h.store.GetSandbox(id)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get sandbox")
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Sandbox not found"})
	}
	
	return c.JSON(http.StatusOK, def)
}

// handleUpdateSandbox updates sandbox configuration
// PUT /api/v1/wasm-sandbox/:id
func (h *WasmSandboxHandler) handleUpdateSandbox(c echo.Context) error {
	id := c.Param("id")
	updates := make(map[string]any)
	
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	
	updates["updated_at"] = time.Now()
	
	if err := h.store.UpdateSandbox(id, updates); err != nil {
		h.logger.WithError(err).Error("Failed to update sandbox")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update sandbox"})
	}
	
	// Verify updated sandbox
	updated, err := h.store.GetSandbox(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Sandbox not found"})
	}
	
	return c.JSON(http.StatusOK, updated)
}

// handleDeleteSandbox deletes a sandbox
// DELETE /api/v1/wasm-sandbox/:id
func (h *WasmSandboxHandler) handleDeleteSandbox(c echo.Context) error {
	id := c.Param("id")
	
	// Stop running sandbox first
	def, err := h.store.GetSandbox(id)
	if err == nil && def.Status == StatusRunning {
		h.logger.WithField("sandbox", id).Warn("Stopping running sandbox before delete")
		// Would call sandbox shutdown here
	}
	
	if err := h.store.DeleteSandbox(id); err != nil {
		h.logger.WithError(err).Error("Failed to delete sandbox")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete sandbox"})
	}
	
	h.logger.WithField("sandbox", id).Info("WASM sandbox deleted")
	return c.NoContent(http.StatusNoContent)
}

// ============================================================================
// Execution Handlers
// ============================================================================

// handleExecutePlugin executes a WASM plugin in the sandbox
// POST /api/v1/wasm-sandbox/:id/execute
func (h *WasmSandboxHandler) handleExecutePlugin(c echo.Context) error {
	id := c.Param("id")
	
	// Get sandbox definition
	def, err := h.store.GetSandbox(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Sandbox not found"})
	}
	
	// Check if sandbox is ready
	if def.Status != StatusIdle && def.Status != StatusRunning {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("Cannot execute: sandbox status is %s", def.Status),
		})
	}
	
	// Prepare execution request
	execReq := struct {
		Input    []byte            `json:"input"`
		Timeout  int               `json:"timeout"`
		Context  map[string]any    `json:"context"`
	}{}
	
	if err := c.Bind(&execReq); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	
	if execReq.Timeout <= 0 {
		execReq.Timeout = def.ResourceLimits.TimeoutSec
	}
	
	// Update sandbox status
	h.store.UpdateSandbox(id, map[string]any{"status": StatusExecuting})
	defer func() {
		h.store.UpdateSandbox(id, map[string]any{"status": StatusIdle})
	}()
	
	// Execute plugin using sandbox engine
	startTime := time.Now()
	output, execErr := h.sandboxEngine.ExecutePlugin(c.Request().Context(), def.WasmModulePath, execReq.Timeout)
	durationMs := int64(time.Since(startTime) / time.Millisecond)
	
	result := &ExecutionResult{
		ID:         uuid.New().String(),
		SandboxID:  id,
		Timestamp:  time.Now(),
		DurationMs: durationMs,
		Metrics: ExecutionMetrics{
			ExitCode: 0,
		},
	}
	
	if execErr != nil {
		result.Success = false
		result.ErrorMsg = execErr.Error()
		result.Metrics.ExitCode = 1
		h.logger.WithFields(logrus.Fields{
			"sandbox": id,
			"duration_ms": durationMs,
			"error": execErr,
		}).Error("Plugin execution failed")
	} else {
		result.Success = true
		result.Output = output
		result.Metrics.ExitCode = 0
		h.logger.WithFields(logrus.Fields{
			"sandbox": id,
			"duration_ms": durationMs,
			"output_size": len(output),
		}).Info("Plugin execution completed")
	}
	
	// Record execution result
	if err := h.store.CreateExecutionResult(result); err != nil {
		h.logger.WithError(err).Warn("Failed to record execution result")
	}
	
	// Update sandbox stats
	h.store.UpdateSandbox(id, map[string]any{
		"last_executed_at":     time.Now(),
		"execution_count":      exec.Count(def.ExecutionCount) + 1,
		"total_exec_time_ms":   exec.Sum(def.TotalExecTimeMs, durationMs),
	})
	
	// Sign evidence for critical operation
	evidenceData := map[string]interface{}{
		"action": "plugin_execution",
		"sandbox_id": id,
		"result": result.Success,
		"duration_ms": durationMs,
		"timestamp": time.Now().UTC(),
	}
	
	evidenceHash, signErr := h.signEvidence(evidenceData)
	if signErr == nil {
		result.EvidenceHash = evidenceHash
	}
	
	return c.JSON(http.StatusOK, result)
}

// handleListExecutions lists execution results for a sandbox
// GET /api/v1/wasm-sandbox/:id/executions
func (h *WasmSandboxHandler) handleListExecutions(c echo.Context) error {
	sandboxID := c.Param("id")
	limit, _ := parseIntParam(c.QueryParam("limit"), 50)
	offset, _ := parseIntParam(c.QueryParam("offset"), 0)
	
	results, err := h.store.ListExecutionResults(sandboxID, limit, offset)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list executions")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to list executions"})
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"executions": results,
		"sandbox_id": sandboxID,
	})
}

// handleGetExecutionResult retrieves specific execution result
// GET /api/v1/wasm-sandbox/:id/executions/:execId
func (h *WasmSandboxHandler) handleGetExecutionResult(c echo.Context) error {
	sandboxID := c.Param("id")
	execID := c.Param("execId")
	
	result, err := h.store.GetExecutionResult(execID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Execution result not found"})
	}
	
	if result.SandboxID != sandboxID {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Execution result not found"})
	}
	
	return c.JSON(http.StatusOK, result)
}

// ============================================================================
// Security Policy Handlers
// ============================================================================

// handleCreateSecurityRule creates new security rule
// POST /api/v1/wasm-sandbox/:id/security-rules
func (h *WasmSandboxHandler) handleCreateSecurityRule(c echo.Context) error {
	var rule SecurityRule
	
	if err := c.Bind(&rule); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	
	rule.ID = uuid.New().String()
	rule.CreatedAt = time.Now()
	rule.Enabled = true
	
	if err := h.store.CreateSecurityRule(&rule); err != nil {
		h.logger.WithError(err).Error("Failed to create security rule")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to create security rule"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"rule_id": rule.ID,
		"type": rule.Type,
		"operation": rule.Operation,
	}).Info("Security rule created")
	
	return c.JSON(http.StatusCreated, rule)
}

// handleListSecurityRules lists security rules
// GET /api/v1/wasm-sandbox/:id/security-rules
func (h *WasmSandboxHandler) handleListSecurityRules(c echo.Context) error {
	sandboxID := c.Param("id")
	
	// Verify sandbox exists
	_, err := h.store.GetSandbox(sandboxID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Sandbox not found"})
	}
	
	rules, err := h.store.ListSecurityRules()
	if err != nil {
		h.logger.WithError(err).Error("Failed to list security rules")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to list security rules"})
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"sandbox_id": sandboxID,
		"rules": rules,
	})
}

// handleUpdateSecurityRule updates security rule
// PUT /api/v1/wasm-sandbox/rules/:ruleId
func (h *WasmSandboxHandler) handleUpdateSecurityRule(c echo.Context) error {
	ruleID := c.Param("ruleId")
	updates := make(map[string]any)
	
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}
	
	if err := h.store.UpdateSecurityRule(ruleID, updates); err != nil {
		h.logger.WithError(err).Error("Failed to update security rule")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update security rule"})
	}
	
	updated, err := h.store.GetSecurityRule(ruleID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Rule not found"})
	}
	
	return c.JSON(http.StatusOK, updated)
}

// handleDeleteSecurityRule deletes security rule
// DELETE /api/v1/wasm-sandbox/rules/:ruleId
func (h *WasmSandboxHandler) handleDeleteSecurityRule(c echo.Context) error {
	ruleID := c.Param("ruleId")
	
	if err := h.store.DeleteSecurityRule(ruleID); err != nil {
		h.logger.WithError(err).Error("Failed to delete security rule")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to delete security rule"})
	}
	
	h.logger.WithField("rule", ruleID).Info("Security rule deleted")
	return c.NoContent(http.StatusNoContent)
}

// ============================================================================
// Audit Logs & Evidence Handlers
// ============================================================================

// handleListAuditLogs lists audit log entries
// GET /api/v1/wasm-sandbox/:id/audit-logs
func (h *WasmSandboxHandler) handleListAuditLogs(c echo.Context) error {
	sandboxID := c.Param("id")
	limit, _ := parseIntParam(c.QueryParam("limit"), 100)
	eventType := c.QueryParam("event_type")
	
	logs, err := h.store.ListAuditLogs(sandboxID, eventType, limit)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list audit logs")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to list audit logs"})
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"sandbox_id": sandboxID,
		"audit_logs": logs,
		"count": len(logs),
	})
}

// handleAttestEvidence signs evidence for critical operation
// POST /api/v1/wasm-sandbox/:id/evidence/attest
func (h *WasmSandboxHandler) handleAttestEvidence(c echo.Context) error {
	sandboxID := c.Param("id")
	
	attestation := struct {
		Action      string                 `json:"action" binding:"required"`
		Timestamp   time.Time              `json:"timestamp"`
		Metadata    map[string]interface{} `json:"metadata"`
	}{}
	
	if err := c.Bind(&attestation); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid attestation request"})
	}
	
	if attestation.Timestamp.IsZero() {
		attestation.Timestamp = time.Now()
	}
	
	hash, err := h.signEvidence(map[string]interface{}{
		"sandbox_id": sandboxID,
		"action":     attestation.Action,
		"timestamp":  attestation.Timestamp,
		"metadata":   attestation.Metadata,
	})
	
	if err != nil {
		h.logger.WithError(err).Error("Failed to attest evidence")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Evidence attestation failed"})
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"sandbox_id": sandboxID,
		"hash":       hash,
		"algorithm":  "Ed25519",
		"timestamp":  time.Now().UTC(),
	})
}

// handleGetEvidenceChain retrieves full evidence chain for sandbox
// GET /api/v1/wasm-sandbox/:id/evidence/chain
func (h *WasmSandboxHandler) handleGetEvidenceChain(c echo.Context) error {
	sandboxID := c.Param("id")
	
	def, err := h.store.GetSandbox(sandboxID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Sandbox not found"})
	}
	
	chain := def.EvidenceChain
	if chain == nil {
		chain = make(map[string]string)
	}
	
	executions, _ := h.store.ListExecutionResults(sandboxID, 1000, 0)
	for _, exec := range executions {
		if exec.EvidenceHash != "" {
			chain[fmt.Sprintf("execution_%s", exec.ID)] = exec.EvidenceHash
		}
	}
	
	return c.JSON(http.StatusOK, map[string]interface{}{
		"sandbox_id":  sandboxID,
		"evidence_chain": chain,
		"entry_count": len(chain),
	})
}

// ============================================================================
// Metrics & Monitoring Handlers
// ============================================================================

// handleGetSandboxMetrics retrieves sandbox metrics
// GET /api/v1/wasm-sandbox/metrics OR /api/v1/wasm-sandbox/:id/metrics
func (h *WasmSandboxHandler) handleGetSandboxMetrics(c echo.Context) error {
	sandboxID := c.Param("id")
	
	if sandboxID == "" {
		// Global metrics
		metrics := h.sandboxEngine.GetMetrics()
		return c.JSON(http.StatusOK, map[string]interface{}{
			"scope": "global",
			"metrics": metrics,
		})
	}
	
	// Sandbox-specific metrics
	def, err := h.store.GetSandbox(sandboxID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Sandbox not found"})
	}
	
	metrics := map[string]interface{}{
		"sandbox_id": sandboxID,
		"name": def.Name,
		"status": def.Status,
		"execution_count": def.ExecutionCount,
		"average_execution_time_ms": def.AverageExecTimeMs,
		"success_rate_percent": def.SuccessRate * 100,
		"created_at": def.CreatedAt,
	}
	
	return c.JSON(http.StatusOK, metrics)
}

// ============================================================================
// Helper Functions
// ============================================================================

func (h *WasmSandboxHandler) signEvidence(data map[string]interface{}) (string, error) {
	if h.ledger == nil {
		return "", fmt.Errorf("evidence ledger not configured")
	}
	
	ctx := context.Background()
	signature, err := h.ledger.Attest(ctx, "wasm_sandbox", "m51_module", data)
	if err != nil {
		return "", err
	}
	
	return signature.Hash, nil
}

func parseIntParam(s string, defaultValue int) (int, error) {
	if s == "" {
		return defaultValue, nil
	}
	
	value, err := strconv.Atoi(s)
	if err != nil {
		return defaultValue, err
	}
	
	return value, nil
}
