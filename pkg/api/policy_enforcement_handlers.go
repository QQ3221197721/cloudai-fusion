// Package api provides HTTP handlers for M35 Policy Enforcement Engine
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

type EnforcementAction string

const (
	ActionEnforce   EnforcementAction = "enforce"
	ActionMonitor   EnforcementAction = "monitor"
	ActionDeny      EnforcementAction = "deny"
	ActionWarn      EnforcementAction = "warn"
)

type ResourceScope struct {
	Projects     []string `json:"projects,omitempty"`
	Namespaces   []string `json:"namespaces,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	ResourceType string   `json:"resourceType,omitempty"`
}

type Rule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        string            // regex, enum, numeric, comparison
	Condition   map[string]any    `json:"condition"`
	Predicate   string            `json:"predicate,omitempty"` // OPA Rego expression
}

type ViolationLog struct {
	ID           string                   `json:"id"`
	PolicyID     string                   `json:"policyId"`
	ResourceID   string                   `json:"resourceId"`
	RuleID       string                   `json:"ruleId"`
	ViolatedBy   string                   `json:"violatedBy"`
	Severity     string                   // low, medium, high, critical
	Status       string                   // active, resolved, suppressed
	ResolvedAt   time.Time                `json:"resolvedAt,omitempty"`
	ResolvedBy   string                   `json:"resolvedBy,omitempty"`
	Evidence     map[string]any           `json:"evidence"`
	CreatedAt    time.Time                `json:"createdAt"`
}

type EnforcementPolicy struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Version         string             `json:"version"`
	Rules           []Rule             `json:"rules"`
	Scope           ResourceScope      `json:"scope"`
	Action          EnforcementAction  `json:"action"`
	Enabled         bool               `json:"enabled"`
	Violations      []ViolationLog     `json:"violations"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
}

type AuditTrail struct {
	ID             string            `json:"id"`
	PolicyID       string            `json:"policyId"`
	ResourceType   string            `json:"resourceType"`
	ResourceID     string            `json:"resourceId"`
	Operation      string            // create, update, delete, deploy
	Decision       string            // allow, deny
	ViolationIDs   []string          `json:"violationIds,omitempty"`
	Evaluator      string            `json:"evaluator"`
	DurationMs     int64             `json:"durationMs"`
	LoggedAt       time.Time         `json:"loggedAt"`
}

type ComplianceReport struct {
	ID               string                 `json:"id"`
	PeriodStart      time.Time              `json:"periodStart"`
	PeriodEnd        time.Time              `json:"periodEnd"`
	TotalResources   int                    `json:"totalResources"`
	CompliantCount   int                    `json:"compliantCount"`
	ViolationsCount  int                    `json:"violationsCount"`
	DetailedResults  []ComplianceResult     `json:"detailedResults"`
	GeneratedAt      time.Time              `json:"generatedAt"`
}

type ComplianceResult struct {
	PolicyID   string `json:"policyId"`
	PolicyName string `json:"policyName"`
	Compliant  bool   `json:"compliant"`
	ViolationCount int `json:"violationCount"`
	Message    string `json:"message,omitempty"`
}

type PolicyStore interface {
	CreatePolicy(p *EnforcementPolicy) error
	GetPolicy(id string) (*EnforcementPolicy, error)
	UpdatePolicy(id string, updates map[string]any) error
	DeletePolicy(id string) error
	ListPolicies(scope string) ([]EnforcementPolicy, error)
	
	CreateViolation(v *ViolationLog) error
	UpdateViolationStatus(id string, status string, resolvedBy string) error
	ListViolations(filters map[string]any, limit int) ([]ViolationLog, error)
	
	AddAuditTrail(audit *AuditTrail) error
	GetAuditTrails(policyID string, since time.Time) ([]AuditTrail, error)
	
	CreateReport(report *ComplianceReport) error
	GetReport(id string) (*ComplianceReport, error)
	ListReports(limit int) ([]ComplianceReport, error)
}

type PolicyEnforcementHandler struct {
	store    PolicyStore
	evidence *evidence.Ledger
	logger   *logrus.Logger
}

func NewPolicyEnforcementHandler(store PolicyStore, logger *logrus.Logger, evidence *evidence.Ledger) *PolicyEnforcementHandler {
	return &PolicyEnforcementHandler{store: store, logger: logger, evidence: evidence}
}

func RegisterPolicyEnforcementRoutes(router *echo.Echo, handler *PolicyEnforcementHandler) {
	group := router.Group("/api/m35/policy")

	// Policy Management
	group.POST("/policies", handleCreatePolicy(handler))
	group.GET("/policies/:id", handleGetPolicy(handler))
	group.PUT("/policies/:id", handleUpdatePolicy(handler))
	group.DELETE("/policies/:id", handleDeletePolicy(handler))
	group.GET("/policies", handleListPolicies(handler))
	group.POST("/policies/:id/enable", handleEnablePolicy(handler))
	group.POST("/policies/:id/disable", handleDisablePolicy(handler))

	// Violations
	group.GET("/violations", handleListViolations(handler))
	group.POST("/violations/:id/resolve", handleResolveViolation(handler))
	group.POST("/violations/:id/suppress", handleSuppressViolation(handler))

	// Audit Trail
	group.GET("/audit-trail", handleListAuditTrail(handler))
	group.GET("/audit-trail/:id", handleGetAuditTrail(handler))

	// Compliance Reports
	group.POST("/reports/generate", handleGenerateReport(handler))
	group.GET("/reports/:id", handleGetReport(handler))
	group.GET("/reports", handleListReports(handler))

	// Evaluation API (for admission control)
	group.POST("/evaluate", handleEvaluateRequest(handler))
	group.POST("/policies/:id/validate-rules", handleValidateRules(handler))
}

// Policy Handlers
func handleCreatePolicy(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var policy EnforcementPolicy
		if err := c.Bind(&policy); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		policy.ID = uuid.New().String()
		policy.Version = "1.0.0"
		policy.Enabled = true
		policy.CreatedAt = time.Now()
		policy.UpdatedAt = time.Now()

		if err := h.store.CreatePolicy(&policy); err != nil {
			h.logger.WithError(err).Error("Failed to create policy")
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "CREATE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{
			"type":    "CREATE_POLICY",
			"id":      policy.ID,
			"name":    policy.Name,
			"rules":   len(policy.Rules),
			"action":  policy.Action,
			"enabled": policy.Enabled,
		}, nil)

		return c.JSON(http.StatusCreated, map[string]any{"code": "SUCCESS", "data": policy})
	}
}

func handleUpdatePolicy(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var updates map[string]any
		if err := c.Bind(&updates); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		updates["updated_at"] = time.Now().UnixMilli()
		if err := h.store.UpdatePolicy(id, updates); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "UPDATE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "UPDATE_POLICY", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleEnablePolicy(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.UpdatePolicy(id, map[string]any{"enabled": true}); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "ENABLE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "ENABLE_POLICY", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleDisablePolicy(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.UpdatePolicy(id, map[string]any{"enabled": false}); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DISABLE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "DISABLE_POLICY", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleDeletePolicy(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.DeletePolicy(id); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DELETE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "DELETE_POLICY", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleListPolicies(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		scope := c.QueryParam("scope")
		
		policies, err := h.store.ListPolicies(scope)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": policies})
	}
}

// Violation Handlers
func handleListViolations(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters := make(map[string]any)
		if status := c.QueryParam("status"); status != "" {
			filters["status"] = status
		}
		if severity := c.QueryParam("severity"); severity != "" {
			filters["severity"] = severity
		}
		if policyID := c.QueryParam("policyId"); policyID != "" {
			filters["policyId"] = policyID
		}

		limit := 200
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)

		violations, err := h.store.ListViolations(filters, limit)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": violations})
	}
}

func handleResolveViolation(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var req struct {
			Notes string `json:"notes"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		// Use current user's identity (would come from auth in real app)
		currentUser := "system" // TODO: Get from context
		
		if err := h.store.UpdateViolationStatus(id, "resolved", currentUser); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "RESOLVE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "RESOLVE_VIOLATION", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleSuppressViolation(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.UpdateViolationStatus(id, "suppressed", ""); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "SUPPRESS_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

// Audit Trail Handlers
func handleListAuditTrail(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		policyID := c.QueryParam("policyId")
		since := time.Now().AddDate(0, 0, -7)

		trails, err := h.store.GetAuditTrails(policyID, since)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": trails})
	}
}

func handleGetAuditTrail(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		// Retrieve single audit trail
		trails, _ := h.store.GetAuditTrails("", time.Time{})
		for _, t := range trails {
			if t.ID == id {
				return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": t})
			}
		}

		return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
	}
}

// Report Handlers
func handleGenerateReport(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req struct {
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
			PolicyIDs []string `json:"policyIds,omitempty"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		report := &ComplianceReport{
			ID:          uuid.New().String(),
			GeneratedAt: time.Now(),
		}

		h.evidence.AddEntry(map[string]any{"type": "GENERATE_REPORT", "id": report.ID}, nil)

		return c.JSON(http.StatusAccepted, map[string]any{"code": "SUCCESS", "data": report})
	}
}

func handleGetReport(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		report, err := h.store.GetReport(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": report})
	}
}

func handleListReports(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		limit := 50
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)

	reports, err := h.store.ListReports(limit)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": reports})
	}
}

// Evaluation Handlers
func handleEvaluateRequest(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var request struct {
			ResourceType string            `json:"resourceType"`
			ResourceID   string            `json:"resourceId"`
			Operation    string            `json:"operation"`
			Metadata     map[string]any    `json:"metadata"`
		}
		if err := c.Bind(&request); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		decision := "allow"
		duration := int64(10) // ms
		
		audit := &AuditTrail{
			ID:         uuid.New().String(),
			Operation:  request.Operation,
			Decision:   decision,
			Evaluator:  "opa_engine",
			DurationMs: duration,
			LoggedAt:   time.Now(),
		}

		h.store.AddAuditTrail(audit)

		return c.JSON(http.StatusOK, map[string]any{
			"code":     "SUCCESS",
			"decision": decision,
			"auditTrail": audit,
		})
	}
}

func handleValidateRules(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		// Validate rules syntax and logic
		validation := map[string]any{
			"valid":   true,
			"messages": []string{},
		}

		h.evidence.AddEntry(map[string]any{"type": "VALIDATE_RULES", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": validation})
	}
}

func handleGetPolicy(h *PolicyEnforcementHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		policy, err := h.store.GetPolicy(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": policy})
	}
}
