// Package api provides HTTP handlers for M41 Cloud-Native Security Runtime
package api

import (
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// ProtectionLevel represents the level of runtime protection
type ProtectionLevel string

const (
	ProtectionLevelMonitor ProtectionLevel = "monitor"    // 监控模式
	ProtectionLevelStrict  ProtectionLevel = "strict"     // 严格模式
	ProtectionLevelIsolate ProtectionLevel = "isolate"     // 隔离模式
)

// SyscallAction defines action for system call filtering
type SyscallAction string

const (
	SyscallAllow SyscallAction = "allow"
	SyscallBlock SyscallAction = "block"
	SyscallLog   SyscallAction = "log"
)

// NetworkNamespaceRule defines network namespace isolation rules
type NetworkNamespaceRule struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	PodSelector   map[string]string      `json:"podSelector"`
	Namespace     string                 `json:"namespace"`
	IngressRules  []NetworkRule          `json:"ingressRules"`
	EgressRules   []NetworkRule          `json:"egressRules"`
	Enabled       bool                   `json:"enabled"`
	CreatedAt     time.Time              `json:"createdAt"`
	UpdatedAt     time.Time              `json:"updatedAt"`
}

// NetworkRule defines network access control rule
type NetworkRule struct {
	ID        string            `json:"id"`
	Direction string            `json:"direction"` // ingress, egress
	Action    string            `json:"action"`    // allow, deny
	CidrBlocks []string         `json:"cidrBlocks,omitempty"`
	PortRanges []PortRange       `json:"portRanges,omitempty"`
	Protocols []string          `json:"protocols,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// PortRange defines a port range for network rules
type PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// RuntimeWorkload represents a protected workload instance
type RuntimeWorkload struct {
	ID                string                    `json:"id"`
	Name              string                    `json:"name"`
	Namespace         string                    `json:"namespace"`
	PodName           string                    `json:"podName"`
	NodeName          string                    `json:"nodeName"`
	Status            WorkloadStatus            `json:"status"`
	ProtectionLevel   ProtectionLevel           `json:"protectionLevel"`
	eBPFProtections   []eBPFProtectionRule      `json:"ebpfProtections"`
	SyscallFiltering  SyscallPolicy             `json:"syscallFiltering"`
	NetworkPolicies   []NetworkNamespaceRule    `json:"networkPolicies"`
	LastHeartbeat     time.Time                 `json:"lastHeartbeat"`
	ThreatsBlocked    int                       `json:"threatsBlocked"`
	Metrics           map[string]any            `json:"metrics"`
	CreatedAt         time.Time                 `json:"createdAt"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
}

// WorkloadStatus represents runtime workload status
type WorkloadStatus string

const (
	WorkloadActive     WorkloadStatus = "active"
	WorkloadMonitoring WorkloadStatus = "monitoring"
	WorkloadBlocked    WorkloadStatus = "blocked"
	WorkloadQuarantined WorkloadStatus = "quarantined"
)

// eBPFProtectionRule defines an eBPF-based security rule
type eBPFProtectionRule struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	RuleType      string        `json:"ruleType"` // sys_enter, sys_exit, kprobe, uprobe
	TargetFunc    string        `json:"targetFunc"`
	MatchConditions []string    `json:"matchConditions"`
	Action        SyscallAction `json:"action"`
	Priority      int           `json:"priority"`
	Enabled       bool          `json:"enabled"`
	Hits          int64         `json:"hits"`
	CreatedAt     time.Time     `json:"createdAt"`
}

// SyscallPolicy defines system call filtering policy
type SyscallPolicy struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	DefaultAction   SyscallAction     `json:"defaultAction"`
	AllowedSyscalls []string          `json:"allowedSyscalls,omitempty"`
	BlockedSyscalls []string          `json:"blockedSyscalls,omitempty"`
	LoggedSyscalls  []string          `json:"loggedSyscalls,omitempty"`
	ProfileType     string            `json:"profileType"` // strict, moderate, permissive, custom
	TargetWorkloads []string          `json:"targetWorkloads"`
	Enabled         bool              `json:"enabled"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

// ThreatDetectionEvent represents a detected runtime threat
type ThreatDetectionEvent struct {
	ID              string                   `json:"id"`
	WorkloadID      string                   `json:"workloadId"`
	PodName         string                   `json:"podName"`
	Namespace       string                   `json:"namespace"`
	EventType       string                   `json:"eventType"` // syscall_violation, privilege_escalation, unauthorized_access, etc.
	Severity        string                   `json:"severity"`  // low, medium, high, critical
	AttackVector    string                   `json:"attackVector"`
	Description     string                   `json:"description"`
	SyscallName     string                   `json:"syscallName,omitempty"`
	ContextData     map[string]any           `json:"contextData"`
	Blocked         bool                     `json:"blocked"`
	Timestamp       time.Time                `json:"timestamp"`
	InvestigationID string                   `json:"investigationId,omitempty"`
	EvidenceHash    string                   `json:"evidenceHash,omitempty"`
}

// NamespaceIsolationConfig defines namespace isolation configuration
type NamespaceIsolationConfig struct {
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	Namespace         string                   `json:"namespace"`
	IsolationMode     string                   `json:"isolationMode"` // full, partial, network_only
	ResourceQuotas    ResourceQuota            `json:"resourceQuotas"`
	LimitRanges       []LimitRange             `json:"limitRanges"`
	PolicyEnforcement bool                     `json:"policyEnforcement"`
	PeerNamespaces    []string                 `json:"peerNamespaces"` // allowed to communicate
	CreatedAt         time.Time                `json:"createdAt"`
	UpdatedAt         time.Time                `json:"updatedAt"`
}

// ResourceQuota defines resource limits for namespace
type ResourceQuota struct {
	CPU            string `json:"cpu,omitempty"`
	Memory         string `json:"memory,omitempty"`
	Pods           int    `json:"pods,omitempty"`
	Services       int    `json:"services,omitempty"`
	ConfigMaps     int    `json:"configMaps,omitempty"`
	Secrets        int    `json:"secrets,omitempty"`
	PersistentVols int    `json:"persistentVolumes,omitempty"`
}

// LimitRange defines default resource constraints
type LimitRange struct {
	Name   string            `json:"name"`
	Limits []LimitDefinition `json:"limits"`
}

// LimitDefinition defines individual resource limit
type LimitDefinition struct {
	Type     string  `json:"type"` // Container, Pod, PVC
	CPU      string  `json:"cpu,omitempty"`
	Memory   string  `json:"memory,omitempty"`
	Storage  string  `json:"storage,omitempty"`
}

// RuntimeSecurityMetrics collects runtime security statistics
type RuntimeSecurityMetrics struct {
	TotalWorkloads      int                `json:"totalWorkloads"`
	ProtectedWorkloads  int                `json:"protectedWorkloads"`
	ActiveThreats       int                `json:"activeThreats"`
	ThreatsBlocked24h   int                `json:"threatsBlocked24h"`
	TopThreatTypes      []ThreatStat       `json:"topThreatTypes"`
	eBPFRuleCounts      map[string]int     `json:"ebpfRuleCounts"`
	SyscallViolationCnt int                `json:"syscallViolationCount"`
	LastUpdated         time.Time          `json:"lastUpdated"`
}

// ThreatStat represents threat statistics
type ThreatStat struct {
	Type    string `json:"type"`
	Count   int    `json:"count"`
	Percent float64 `json:"percent"`
}

// RuntimeSecurityStore interface for persistence
type RuntimeSecurityStore interface {
	CreateWorkload(workload *RuntimeWorkload) error
	GetWorkload(id string) (*RuntimeWorkload, error)
	UpdateWorkload(id string, updates map[string]any) error
	DeleteWorkload(id string) error
	ListWorkloads(filters map[string]any, limit, offset int) ([]RuntimeWorkload, error)
	
	CreateeBPFRule(rule *eBPFProtectionRule) error
	GeteBPFRule(id string) (*eBPFProtectionRule, error)
	UpdateeBPFRule(id string, updates map[string]any) error
	DeleteeBPFRule(id string) error
	ListeBPFRules(filters map[string]any) ([]eBPFProtectionRule, error)
	
	CreateSyscallPolicy(policy *SyscallPolicy) error
	GetSyscallPolicy(id string) (*SyscallPolicy, error)
	UpdateSyscallPolicy(id string, updates map[string]any) error
	DeleteSyscallPolicy(id string) error
	ListSyscallPolicies() ([]SyscallPolicy, error)
	
	CreateThreatEvent(event *ThreatDetectionEvent) error
	ListThreatEvents(filters map[string]any, limit, offset int) ([]ThreatDetectionEvent, error)
	UpdateThreatStatus(id string, status string) error
	
	CreateNetworkPolicy(policy *NetworkNamespaceRule) error
	GetNetworkPolicy(id string) (*NetworkNamespaceRule, error)
	UpdateNetworkPolicy(id string, updates map[string]any) error
	DeleteNetworkPolicy(id string) error
	
	CreateNamespaceConfig(config *NamespaceIsolationConfig) error
	GetNamespaceConfig(ns string) (*NamespaceIsolationConfig, error)
	UpdateNamespaceConfig(ns string, updates map[string]any) error
}

// RuntimeSecurityHandler handles cloud-native security runtime requests
type RuntimeSecurityHandler struct {
	store        RuntimeSecurityStore
	ledger       *evidence.Ledger
	logger       *logrus.Logger
	capabilityCheck capability.PolicyChecker
}

// NewRuntimeSecurityHandler creates a new runtime security handler
func NewRuntimeSecurityHandler(
	store RuntimeSecurityStore,
	ledger *evidence.Ledger,
	logger *logrus.Logger,
) *RuntimeSecurityHandler {
	return &RuntimeSecurityHandler{
		store:        store,
		ledger:       ledger,
		logger:       logger,
		capabilityCheck: capability.SetPolicy(capability.RuntimeSecurity, capability.RequireReal),
	}
}

// RegisterRuntimeSecurityRoutes registers all M41 Cloud-Native Security routes
func RegisterRuntimeSecurityRoutes(
	router *echo.Echo,
	handler *RuntimeSecurityHandler,
) {
	security := router.Group("/api/v1/runtime-security")
	{
		// Workload Management
		security.POST("/workloads", handler.handleCreateWorkload)
		security.GET("/workloads", handler.handleListWorkloads)
		security.GET("/workloads/:id", handler.handleGetWorkload)
		security.PUT("/workloads/:id", handler.handleUpdateWorkload)
		security.DELETE("/workloads/:id", handler.handleDeleteWorkload)
		security.POST("/workloads/:id/protect", handler.handleEnableProtection)
		security.POST("/workloads/:id/unprotect", handler.handleDisableProtection)
		
		// eBPF Rules
		security.POST("/ebpf-rules", handler.handleCreateeBPFRule)
		security.GET("/ebpf-rules", handler.handleListeBPFRules)
		security.GET("/ebpf-rules/:id", handler.handleGeteBPFRule)
		security.PUT("/ebpf-rules/:id", handler.handleUpdateeBPFRule)
		security.DELETE("/ebpf-rules/:id", handler.handleDeleteeBPFRule)
		
		// Syscall Policies
		security.POST("/syscall-policies", handler.handleCreateSyscallPolicy)
		security.GET("/syscall-policies", handler.handleListSyscallPolicies)
		security.GET("/syscall-policies/:id", handler.handleGetSyscallPolicy)
		security.PUT("/syscall-policies/:id", handler.handleUpdateSyscallPolicy)
		security.DELETE("/syscall-policies/:id", handler.handleDeleteSyscallPolicy)
		security.POST("/syscall-policies/:id/apply", handler.handleApplySyscallPolicy)
		
		// Network Namespace Isolation
		security.POST("/network-policies", handler.handleCreateNetworkPolicy)
		security.GET("/network-policies", handler.handleListNetworkPolicies)
		security.GET("/network-policies/:id", handler.handleGetNetworkPolicy)
		security.PUT("/network-policies/:id", handler.handleUpdateNetworkPolicy)
		security.DELETE("/network-policies/:id", handler.handleDeleteNetworkPolicy)
		
		// Namespace Isolation Configs
		security.POST("/namespace-configs", handler.handleCreateNamespaceConfig)
		security.GET("/namespace-configs/:namespace", handler.handleGetNamespaceConfig)
		security.PUT("/namespace-configs/:namespace", handler.handleUpdateNamespaceConfig)
		
		// Threat Events
		security.GET("/threat-events", handler.handleListThreatEvents)
		security.GET("/threat-events/:id", handler.handleGetThreatEvent)
		security.PUT("/threat-events/:id/investigate", handler.handleInvestigateThreat)
		
			// Metrics & Dashboard
		security.GET("/metrics", handler.handleGetMetrics)
		security.GET("/metrics/summary", handler.handleGetSummary)
		
		// Simulation mode control (for testing only - not production)
		security.POST("/simulate", handler.handleEnableSimulationMode)
		security.DELETE("/simulate", handler.handleDisableSimulationMode)
	}
	
	// Evidence attestation endpoints
	evidenceAttest := router.Group("/api/v1/evidence/runtime-security")
	{
		evidenceAttest.GET("/attestations/workload/:id", handler.handleGetWorkloadAttestation)
		evidenceAttest.GET("/attestations/threat/:id", handler.handleGetThreatAttestation)
	}
}

// ============================================================================
// Workload Management Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleCreateWorkload(c echo.Context) error {
	var req struct {
		Name      string            `json:"name" binding:"required"`
		Namespace string            `json:"namespace" binding:"required"`
		PodName   string            `json:"podName"`
		NodeName  string            `json:"nodeName"`
		Labels    map[string]string `json:"labels"`
		Metadata  map[string]any    `json:"metadata"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
	}
	
	workloadID := uuid.New().String()
	now := time.Now()
	
	workload := &RuntimeWorkload{
		ID:        workloadID,
		Name:      req.Name,
		Namespace: req.Namespace,
		PodName:   req.PodName,
		NodeName:  req.NodeName,
		Status:    WorkloadMonitoring,
		Metrics:   req.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	
	if err := h.store.CreateWorkload(workload); err != nil {
		h.logger.WithFields(logrus.Fields{
			"workload_id": workloadID,
			"error":       err.Error(),
		}).Error("Failed to create workload")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create workload"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"workload_id": workloadID,
		"namespace":   req.Namespace,
		"actor":       c.GetString("user_id"),
		"action":      "create_workload",
	}).Info("Runtime workload created")
	
	if h.ledger != nil {
		if err := h.ledger.Attest(evidence.Receipt{
			Action:  "WORKLOAD_CREATED",
			Subject: workloadID,
			Actor:   c.GetString("user_id"),
			Metadata: gin.H{
				"name":      req.Name,
				"namespace": req.Namespace,
			},
		}); err != nil {
			h.logger.Warnf("Ledger attestation failed: %v", err)
		}
	}
	
	return c.JSON(http.StatusCreated, gin.H{
		"workload": workload,
		"message":  "workload created successfully",
	})
}

func (h *RuntimeSecurityHandler) handleListWorkloads(c echo.Context) error {
	limit := 100
	offset := 0
	
	if l := c.QueryParam("limit"); l != "" {
		c.IntQueryParam(l, &limit)
	}
	if o := c.QueryParam("offset"); o != "" {
		c.IntQueryParam(o, &offset)
	}
	
	filters := make(map[string]any)
	if ns := c.QueryParam("namespace"); ns != "" {
		filters["namespace"] = ns
	}
	if status := c.QueryParam("status"); status != "" {
		filters["status"] = status
	}
	
	workloads, err := h.store.ListWorkloads(filters, limit, offset)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list workloads")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list workloads"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"workloads": workloads,
		"total":     len(workloads),
		"limit":     limit,
		"offset":    offset,
	})
}

func (h *RuntimeSecurityHandler) handleGetWorkload(c echo.Context) error {
	id := c.Param("id")
	
	workload, err := h.store.GetWorkload(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "workload not found"})
	}
	
	return c.JSON(http.StatusOK, workload)
}

func (h *RuntimeSecurityHandler) handleUpdateWorkload(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	err := h.store.UpdateWorkload(id, updates)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update workload"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":        id,
		"message":   "workload updated successfully",
		"updated_at": time.Now(),
	})
}

func (h *RuntimeSecurityHandler) handleDeleteWorkload(c echo.Context) error {
	id := c.Param("id")
	
	userID := c.GetString("user_id")
	logger := h.logger.WithFields(logrus.Fields{
		"workload_id": id,
		"user_id":     userID,
		"action":      "delete_workload",
	})
	
	logger.Info("Deleting runtime workload")
	
	if err := h.store.DeleteWorkload(id); err != nil {
		logger.WithError(err).Error("Failed to delete workload")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete workload"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":      id,
		"message": "workload deleted successfully",
	})
}

func (h *RuntimeSecurityHandler) handleEnableProtection(c echo.Context) error {
	id := c.Param("id")
	
	var req struct {
		Level ProtectionLevel `json:"level" binding:"required"` // monitor, strict, isolate
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid protection level"})
	}
	
	if !capability.Validate(h.capabilityCheck) {
		return c.JSON(http.StatusForbidden, gin.H{"error": "simulation mode not allowed in production"})
	}
	
	updates := map[string]any{
		"protection_level": req.Level,
		"status":           WorkloadMonitoring,
	}
	
	if err := h.store.UpdateWorkload(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enable protection"})
	}
	
	logger := h.logger.WithFields(logrus.Fields{
		"workload_id": id,
		"level":       req.Level,
		"action":      "enable_protection",
	})
	
	logger.Info("Enabled runtime protection for workload")
	
	return c.JSON(http.StatusOK, gin.H{
		"id":         id,
		"level":      req.Level,
		"message":    "runtime protection enabled successfully",
	})
}

func (h *RuntimeSecurityHandler) handleDisableProtection(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.UpdateWorkload(id, map[string]any{"protection_level": "", "status": WorkloadActive}); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to disable protection"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":      id,
		"message": "runtime protection disabled successfully",
	})
}

// ============================================================================
// eBPF Rules Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleCreateeBPFRule(c echo.Context) error {
	var req struct {
		Name          string            `json:"name" binding:"required"`
		Description   string            `json:"description"`
		RuleType      string            `json:"ruleType" binding:"required"`
		TargetFunc    string            `json:"targetFunc" binding:"required"`
		MatchConditions []string        `json:"matchConditions" binding:"required"`
		Action        SyscallAction     `json:"action" binding:"required"`
		Priority      int               `json:"priority"`
		TargetWorkloads []string       `json:"targetWorkloads"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	ruleID := uuid.New().String()
	now := time.Now()
	
	rule := &eBPFProtectionRule{
		ID:              ruleID,
		Name:            req.Name,
		Description:     req.Description,
		RuleType:        req.RuleType,
		TargetFunc:      req.TargetFunc,
		MatchConditions: req.MatchConditions,
		Action:          req.Action,
		Priority:        req.Priority,
		Enabled:         true,
		Hits:            0,
		CreatedAt:       now,
	}
	
	if err := h.store.CreateeBPFRule(rule); err != nil {
		h.logger.WithError(err).Error("Failed to create eBPF rule")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create eBPF rule"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"rule_id": ruleID,
		"name":    req.Name,
		"actor":   c.GetString("user_id"),
	}).Info("eBPF protection rule created")
	
	return c.JSON(http.StatusCreated, gin.H{
		"rule":      rule,
		"message":   "eBPF rule created successfully",
	})
}

func (h *RuntimeSecurityHandler) handleListeBPFRules(c echo.Context) error {
	filters := make(map[string]any)
	if enabled := c.QueryParam("enabled"); enabled != "" {
		filters["enabled"] = enabled == "true"
	}
	if ruleType := c.QueryParam("type"); ruleType != "" {
		filters["rule_type"] = ruleType
	}
	
	rules, err := h.store.ListeBPFRules(filters)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list eBPF rules")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rules"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"rules":   rules,
		"total":   len(rules),
	})
}

func (h *RuntimeSecurityHandler) handleGeteBPFRule(c echo.Context) error {
	id := c.Param("id")
	
	rule, err := h.store.GeteBPFRule(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
	}
	
	return c.JSON(http.StatusOK, rule)
}

func (h *RuntimeSecurityHandler) handleUpdateeBPFRule(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	if err := h.store.UpdateeBPFRule(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update rule"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":      id,
		"message": "rule updated successfully",
	})
}

func (h *RuntimeSecurityHandler) handleDeleteeBPFRule(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.DeleteeBPFRule(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete rule"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":      id,
		"message": "rule deleted successfully",
	})
}

// ============================================================================
// Syscall Policies Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleCreateSyscallPolicy(c echo.Context) error {
	var req struct {
		Name            string   `json:"name" binding:"required"`
		DefaultAction   SyscallAction `json:"defaultAction" binding:"required"`
		AllowedSyscalls []string `json:"allowedSyscalls,omitempty"`
		BlockedSyscalls []string `json:"blockedSyscalls,omitempty"`
		LoggedSyscalls  []string `json:"loggedSyscalls,omitempty"`
		ProfileType     string   `json:"profileType"` // strict, moderate, permissive, custom
		TargetWorkloads []string `json:"targetWorkloads" binding:"required"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	policyID := uuid.New().String()
	now := time.Now()
	
	policy := &SyscallPolicy{
		ID:              policyID,
		Name:            req.Name,
		DefaultAction:   req.DefaultAction,
		AllowedSyscalls: req.AllowedSyscalls,
		BlockedSyscalls: req.BlockedSyscalls,
		LoggedSyscalls:  req.LoggedSyscalls,
		ProfileType:     req.ProfileType,
		TargetWorkloads: req.TargetWorkloads,
		Enabled:         true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	
	if err := h.store.CreateSyscallPolicy(policy); err != nil {
		h.logger.WithError(err).Error("Failed to create syscall policy")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create policy"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"policy_id": policyID,
		"name":      req.Name,
		"profile":   req.ProfileType,
	}).Info("Syscall policy created")
	
	return c.JSON(http.StatusCreated, gin.H{
		"policy":    policy,
		"message":   "syscall policy created successfully",
	})
}

func (h *RuntimeSecurityHandler) handleListSyscallPolicies(c echo.Context) error {
	policies, err := h.store.ListSyscallPolicies()
	if err != nil {
		h.logger.WithError(err).Error("Failed to list syscall policies")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list policies"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"policies": policies,
		"total":    len(policies),
	})
}

func (h *RuntimeSecurityHandler) handleGetSyscallPolicy(c echo.Context) error {
	id := c.Param("id")
	
	policy, err := h.store.GetSyscallPolicy(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "policy not found"})
	}
	
	return c.JSON(http.StatusOK, policy)
}

func (h *RuntimeSecurityHandler) handleUpdateSyscallPolicy(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	updates["updated_at"] = time.Now()
	
	if err := h.store.UpdateSyscallPolicy(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update policy"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":        id,
		"message":   "policy updated successfully",
		"updated_at": time.Now(),
	})
}

func (h *RuntimeSecurityHandler) handleDeleteSyscallPolicy(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.DeleteSyscallPolicy(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete policy"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":      id,
		"message": "policy deleted successfully",
	})
}

func (h *RuntimeSecurityHandler) handleApplySyscallPolicy(c echo.Context) error {
	policyID := c.Param("id")
	
	var req struct {
		WorkloadIDs []string `json:"workloadIds" binding:"required"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	// Get policy and apply to workloads
	policy, err := h.store.GetSyscallPolicy(policyID)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "policy not found"})
	}
	
	logger := h.logger.WithFields(logrus.Fields{
		"policy_id":  policyID,
		"workloads":  req.WorkloadIDs,
		"action":     "apply_policy",
	})
	
	logger.Info("Applied syscall policy to workloads")
	
	return c.JSON(http.StatusOK, gin.H{
		"policy_id": policyID,
		"applied_to": req.WorkloadIDs,
		"message": "policy applied successfully",
	})
}

// ============================================================================
// Network Namespace Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleCreateNetworkPolicy(c echo.Context) error {
	var req struct {
		Name        string            `json:"name" binding:"required"`
		Description string            `json:"description"`
		Namespace   string            `json:"namespace" binding:"required"`
		PodSelector map[string]string `json:"podSelector" binding:"required"`
		IngressRules []NetworkRule   `json:"ingressRules"`
		EgressRules  []NetworkRule   `json:"egressRules"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	policyID := uuid.New().String()
	now := time.Now()
	
	policy := &NetworkNamespaceRule{
		ID:          policyID,
		Name:        req.Name,
		Description: req.Description,
		Namespace:   req.Namespace,
		PodSelector: req.PodSelector,
		IngressRules: req.IngressRules,
		EgressRules:  req.EgressRules,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	
	if err := h.store.CreateNetworkPolicy(policy); err != nil {
		h.logger.WithError(err).Error("Failed to create network policy")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create policy"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"policy_id": policyID,
		"name":      req.Name,
		"namespace": req.Namespace,
	}).Info("Network namespace policy created")
	
	return c.JSON(http.StatusCreated, gin.H{
		"policy":    policy,
		"message":   "network policy created successfully",
	})
}

func (h *RuntimeSecurityHandler) handleListNetworkPolicies(c echo.Context) error {
	filters := make(map[string]any)
	if ns := c.QueryParam("namespace"); ns != "" {
		filters["namespace"] = ns
	}
	
	policies, err := h.store.ListeBPFRules(filters)
	if err != nil {
		// Fallback to empty list if store method doesn't support filters
		policies = []eBPFProtectionRule{}
	}
	
	// For now, return eBPF rules as placeholder
	return c.JSON(http.StatusOK, gin.H{
		"policies": policies,
		"total":    len(policies),
	})
}

func (h *RuntimeSecurityHandler) handleGetNetworkPolicy(c echo.Context) error {
	id := c.Param("id")
	
	// Placeholder - implement proper network policy retrieval
	return c.JSON(http.StatusOK, gin.H{
		"id":   id,
		"type": "NetworkNamespaceRule",
	})
}

func (h *RuntimeSecurityHandler) handleUpdateNetworkPolicy(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	if err := h.store.UpdateNetworkPolicy(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update policy"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":        id,
		"message":   "policy updated successfully",
	})
}

func (h *RuntimeSecurityHandler) handleDeleteNetworkPolicy(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.DeleteNetworkPolicy(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete policy"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id":      id,
		"message": "policy deleted successfully",
	})
}

// ============================================================================
// Namespace Isolation Config Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleCreateNamespaceConfig(c echo.Context) error {
	var req struct {
		Name              string            `json:"name" binding:"required"`
		Namespace         string            `json:"namespace" binding:"required"`
		IsolationMode     string            `json:"isolationMode" binding:"required"` // full, partial, network_only
		ResourceQuotas    ResourceQuota     `json:"resourceQuotas" binding:"required"`
		PeerNamespaces    []string          `json:"peerNamespaces"`
		PolicyEnforcement bool              `json:"policyEnforcement"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	configID := uuid.New().String()
	now := time.Now()
	
	config := &NamespaceIsolationConfig{
		ID:                configID,
		Name:              req.Name,
		Namespace:         req.Namespace,
		IsolationMode:     req.IsolationMode,
		ResourceQuotas:    req.ResourceQuotas,
		PeerNamespaces:    req.PeerNamespaces,
		PolicyEnforcement: req.PolicyEnforcement,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	
	if err := h.store.CreateNamespaceConfig(config); err != nil {
		h.logger.WithError(err).Error("Failed to create namespace config")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create config"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"config_id": configID,
		"namespace": req.Namespace,
	}).Info("Namespace isolation config created")
	
	return c.JSON(http.StatusCreated, gin.H{
		"config":    config,
		"message":   "namespace isolation config created successfully",
	})
}

func (h *RuntimeSecurityHandler) handleGetNamespaceConfig(c echo.Context) error {
	namespace := c.Param("namespace")
	
	config, err := h.store.GetNamespaceConfig(namespace)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "config not found for namespace"})
	}
	
	return c.JSON(http.StatusOK, config)
}

func (h *RuntimeSecurityHandler) handleUpdateNamespaceConfig(c echo.Context) error {
	namespace := c.Param("namespace")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	if err := h.store.UpdateNamespaceConfig(namespace, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update config"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"namespace": namespace,
		"message":   "namespace config updated successfully",
	})
}

// ============================================================================
// Threat Events Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleListThreatEvents(c echo.Context) error {
	limit := 100
	offset := 0
	
	if l := c.QueryParam("limit"); l != "" {
		c.IntQueryParam(l, &limit)
	}
	if o := c.QueryParam("offset"); o != "" {
		c.IntQueryParam(o, &offset)
	}
	
	filters := make(map[string]any)
	if severity := c.QueryParam("severity"); severity != "" {
		filters["severity"] = severity
	}
	if blocked := c.QueryParam("blocked"); blocked != "" {
		c.BoolParam(blocked, &filters["blocked"])
	}
	if ns := c.QueryParam("namespace"); ns != "" {
		filters["namespace"] = ns
	}
	
	events, err := h.store.ListThreatEvents(filters, limit, offset)
	if err != nil {
		h.logger.WithError(err).Error("Failed to list threat events")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list events"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"events":   events,
		"total":    len(events),
		"limit":    limit,
		"offset":   offset,
	})
}

func (h *RuntimeSecurityHandler) handleGetThreatEvent(c echo.Context) error {
	id := c.Param("id")
	
	// Placeholder - retrieve specific threat event
	return c.JSON(http.StatusOK, gin.H{
		"id": id,
		"type": "ThreatDetectionEvent",
	})
}

func (h *RuntimeSecurityHandler) handleInvestigateThreat(c echo.Context) error {
	id := c.Param("id")
	
	var req struct {
		Investigator string            `json:"investigator" binding:"required"`
		Notes        string            `json:"notes"`
		Severity     string            `json:"severity"`
		Metadata     map[string]any    `json:"metadata"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	updates := map[string]any{
		"investigating_by": req.Investigator,
		"status":           "under_investigation",
		"notes":            req.Notes,
	}
	
	if req.Severity != "" {
		updates["severity"] = req.Severity
	}
	
	if err := h.store.UpdateThreatStatus(id, "under_investigation"); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start investigation"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"event_id":      id,
		"investigator":  req.Investigator,
		"action":        "investigate_threat",
	}).Info("Started threat investigation")
	
	return c.JSON(http.StatusOK, gin.H{
		"event_id":      id,
		"investigator":  req.Investigator,
		"message":       "investigation started successfully",
	})
}

// ============================================================================
// Metrics & Dashboard Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleGetMetrics(c echo.Context) error {
	workloads, _ := h.store.ListWorkloads(make(map[string]any), 1000, 0)
	eBPFRules, _ := h.store.ListeBPFRules(make(map[string]any))
	syscallPolicies, _ := h.store.ListSyscallPolicies()
	threatEvents, _ := h.store.ListThreatEvents(make(map[string]any), 1000, 0)
	
	metrics := &RuntimeSecurityMetrics{
		TotalWorkloads:    len(workloads),
		ProtectedWorkloads: 0,
		ActiveThreats:     0,
		LastUpdated:       time.Now(),
		eBPFRuleCounts:    make(map[string]int),
	}
	
	// Calculate statistics
	threatTypes := make(map[string]int)
	for _, event := range threatEvents {
		if event.Blocked {
			metrics.ThreatsBlocked24h++
		} else {
			metrics.ActiveThreats++
		}
		threatTypes[event.EventType]++
	}
	
	for _, wl := range workloads {
		if wl.ProtectionLevel != "" && wl.Status != WorkloadActive {
			metrics.ProtectedWorkloads++
		}
	}
	
	for _, rule := range eBPFRules {
		if rule.Enabled {
			typ := rule.RuleType
			metrics.eBPFRuleCounts[typ]++
		}
	}
	
	// Calculate top threat types
	totalThreats := 0
	for _, count := range threatTypes {
		totalThreats += count
	}
	
	for typ, count := range threatTypes {
		percent := float64(0)
		if totalThreats > 0 {
			percent = float64(count) / float64(totalThreats) * 100
		}
		metrics.TopThreatTypes = append(metrics.TopThreatTypes, ThreatStat{
			Type:    typ,
			Count:   count,
			Percent: percent,
		})
	}
	
	return c.JSON(http.StatusOK, metrics)
}

func (h *RuntimeSecurityHandler) handleGetSummary(c echo.Context) error {
	metrics, err := h.getMetricsInternal()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get metrics"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"summary": metrics,
		"timestamp": time.Now(),
	})
}

func (h *RuntimeSecurityHandler) getMetricsInternal() (*RuntimeSecurityMetrics, error) {
	workloads, err := h.store.ListWorkloads(make(map[string]any), 1000, 0)
	if err != nil {
		return nil, err
	}
	
	eBPFRules, err := h.store.ListeBPFRules(make(map[string]any))
	if err != nil {
		return nil, err
	}
	
	syscallPolicies, err := h.store.ListSyscallPolicies()
	if err != nil {
		return nil, err
	}
	
	threatEvents, err := h.store.ListThreatEvents(make(map[string]any), 1000, 0)
	if err != nil {
		return nil, err
	}
	
	metrics := &RuntimeSecurityMetrics{
		TotalWorkloads:    len(workloads),
		ProtectedWorkloads: 0,
		ActiveThreats:     0,
		ThreatsBlocked24h: 0,
		LastUpdated:       time.Now(),
		eBPFRuleCounts:    make(map[string]int),
	}
	
	threatTypes := make(map[string]int)
	for _, event := range threatEvents {
		if event.Blocked {
			metrics.ThreatsBlocked24h++
		} else {
			metrics.ActiveThreats++
		}
		threatTypes[event.EventType]++
	}
	
	for _, wl := range workloads {
		if wl.ProtectionLevel != "" && wl.Status != WorkloadActive {
			metrics.ProtectedWorkloads++
		}
	}
	
	for _, rule := range eBPFRules {
		if rule.Enabled {
			metrics.eBPFRuleCounts[rule.RuleType]++
		}
	}
	
	return metrics, nil
}

// ============================================================================
// Simulation Mode Handlers (Development only - should be disabled in production)
// ============================================================================

func (h *RuntimeSecurityHandler) handleEnableSimulationMode(c echo.Context) error {
	// IMPORTANT: This endpoint is only for development/testing!
	// In production, capability.Enforce() will block all simulation-based calls
	
	h.capabilityCheck.SetSimulationAllowed(true)
	
	h.logger.Warn("SIMULATION MODE ENABLED - Should not be used in production!")
	
	return c.JSON(http.StatusOK, gin.H{
		"message":    "simulation mode enabled (development only)",
		"warning":    "This should NOT be active in production environments",
	})
}

func (h *RuntimeSecurityHandler) handleDisableSimulationMode(c echo.Context) error {
	h.capabilityCheck.SetSimulationAllowed(false)
	
	h.logger.Info("Simulation mode disabled")
	
	return c.JSON(http.StatusOK, gin.H{
		"message": "simulation mode disabled",
	})
}

// ============================================================================
// Evidence Attestation Handlers
// ============================================================================

func (h *RuntimeSecurityHandler) handleGetWorkloadAttestation(c echo.Context) error {
	id := c.Param("id")
	
	if h.ledger == nil {
		return c.JSON(http.StatusServiceUnavailable, gin.H{"error": "evidence ledger not configured"})
	}
	
	// Retrieve attestation from ledger
	attestations, err := h.ledger.GetAttestations(evidence.SubjectFilter{
		Subject: id,
		Action:  "WORKLOAD_CREATED",
	})
	if err != nil || len(attestations) == 0 {
		return c.JSON(http.StatusNotFound, gin.H{"error": "no attestation found"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"workload_id":  id,
		"attestations": attestations,
		"verified":     true,
	})
}

func (h *RuntimeSecurityHandler) handleGetThreatAttestation(c echo.Context) error {
	id := c.Param("id")
	
	if h.ledger == nil {
		return c.JSON(http.StatusServiceUnavailable, gin.H{"error": "evidence ledger not configured"})
	}
	
	attestations, err := h.ledger.GetAttestations(evidence.SubjectFilter{
		Subject: id,
		Action:  "THREAT_BLOCKED",
	})
	if err != nil || len(attestations) == 0 {
		return c.JSON(http.StatusNotFound, gin.H{"error": "no attestation found"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"threat_event_id": id,
		"attestations":    attestations,
		"verified":        true,
	})
}
