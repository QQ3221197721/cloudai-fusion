// Package api provides HTTP handlers for M33 Cloud Security Posture Management
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

type ComplianceFramework string

const (
	FrameworkCIS       ComplianceFramework = "cis"
	FrameworkNIST      ComplianceFramework = "nist"
	FrameworkSOC2      ComplianceFramework = "soc2"
	FrameworkPCIDSS    ComplianceFramework = "pci-dss"
	FrameworkHIPAA     ComplianceFramework = "hipaa"
)

type FindingSeverity string

const (
	SeverityCritical FindingSeverity = "critical"
	SeverityHigh     FindingSeverity = "high"
	SeverityMedium   FindingSeverity = "medium"
	SeverityLow      FindingSeverity = "low"
)

type Resource struct {
	ID          string        `json:"id"`
	Type        string        // aws_s3, aws_ec2, gcp_bucket, azure_vnet
	Name        string        `json:"name"`
	Region      string        `json:"region"`
	AccountID   string        `json:"accountId"`
	Tags        map[string]any `json:"tags"`
	CreatedAt   time.Time     `json:"createdAt"`
	ComplianceStatus string      // compliant, non_compliant, unknown
}

type Finding struct {
	ID            string               `json:"id"`
	RuleID        string               `json:"ruleId"`
	RuleName      string               `json:"ruleName"`
	Description   string               `json:"description"`
	Severity      FindingSeverity      `json:"severity"`
	Framework     ComplianceFramework  `json:"framework"`
	ResourceID    string               `json:"resourceId"`
	ResourceType  string               `json:"resourceType"`
	Remediation   string               `json:"remediation"`
	Evidence      map[string]any       `json:"evidence"`
	Status        string               // active, suppressed, resolved
	CreatedAt     time.Time            `json:"createdAt"`
	ResolvedAt    time.Time            `json:"resolvedAt,omitempty"`
}

type ComplianceAssessment struct {
	ID             string              `json:"id"`
	Framework      ComplianceFramework `json:"framework"`
	Score          float64             `json:"score"`
	TotalRules     int                 `json:"totalRules"`
	PassedRules    int                 `json:"passedRules"`
	FailingRules   int                 `json:"failingRules"`
	ResourcesScan  int                 `json:"resourcesScanned"`
	Findings       []Finding           `json:"findings"`
	Status         string              // pending, completed, error
	StartedAt      time.Time           `json:"startedAt"`
	CompletedAt    time.Time           `json:"completedAt,omitempty"`
	ErrorMsg       string              `json:"errorMsg,omitempty"`
}

type CSPMStore interface {
	CreateResource(r *Resource) error
	GetResource(id string) (*Resource, error)
	UpdateResource(id string, updates map[string]any) error
	DeleteResource(id string) error
	ListResources(filters map[string]any, limit, offset int) ([]Resource, error)
	
	CreateFinding(f *Finding) error
	UpdateFindingStatus(id string, status string) error
	ListFindings(filters map[string]any, limit int) ([]Finding, error)
	
	CreateAssessment(a *ComplianceAssessment) error
	GetAssessment(id string) (*ComplianceAssessment, error)
	UpdateAssessment(id string, updates map[string]any) error
	RunAssessment(ctx interface{}, framework ComplianceFramework, resources []Resource) (*ComplianceAssessment, error)
	ListAssessments(limit int) ([]ComplianceAssessment, error)
}

type CSPMHandler struct {
	store    CSPMStore
	evidence *evidence.Ledger
	logger   *logrus.Logger
}

func NewCSPMHandler(store CSPMStore, logger *logrus.Logger, evidence *evidence.Ledger) *CSPMHandler {
	return &CSPMHandler{store: store, logger: logger, evidence: evidence}
}

func RegisterCSPMRoutes(router *echo.Echo, handler *CSPMHandler) {
	group := router.Group("/api/m33/cspm")

	// Resource Management
	group.POST("/resources", handleCreateResource(handler))
	group.GET("/resources/:id", handleGetResource(handler))
	group.PUT("/resources/:id", handleUpdateResource(handler))
	group.DELETE("/resources/:id", handleDeleteResource(handler))
	group.GET("/resources", handleListResources(handler))

	// Findings
	group.GET("/findings", handleListFindings(handler))
	group.POST("/findings/:id/resolved", handleResolveFinding(handler))
	group.POST("/findings/:id/suppress", handleSuppressFinding(handler))

	// Assessment
	group.POST("/assessments/run", handleRunAssessment(handler))
	group.GET("/assessments/:id", handleGetAssessment(handler))
	group.GET("/assessments", handleListAssessments(handler))
	group.DELETE("/assessments/:id", handleDeleteAssessment(handler))

	// Analytics
	group.GET("/analytics/compliance-score", handleGetComplianceScore(handler))
}

// Resource Handlers
func handleCreateResource(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var resource Resource
		if err := c.Bind(&resource); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		resource.ID = uuid.New().String()
		resource.CreatedAt = time.Now()
		resource.ComplianceStatus = "unknown"

		if err := h.store.CreateResource(&resource); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "CREATE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "CREATE_RESOURCE", "id": resource.ID}, nil)

		return c.JSON(http.StatusCreated, map[string]any{"code": "SUCCESS", "data": resource})
	}
}

func handleListResources(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters := make(map[string]any)
		if resourceType := c.QueryParam("type"); resourceType != "" {
			filters["type"] = resourceType
		}
		if region := c.QueryParam("region"); region != "" {
			filters["region"] = region
		}

		limit := 100
		offset := 0
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)
		fmt.Sscanf(c.QueryParam("offset"), "%d", &offset)

		resources, err := h.store.ListResources(filters, limit, offset)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": resources})
	}
}

// Finding Handlers
func handleListFindings(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters := make(map[string]any)
		if severity := c.QueryParam("severity"); severity != "" {
			filters["severity"] = severity
		}
		if status := c.QueryParam("status"); status != "" {
			filters["status"] = status
		}
		if framework := c.QueryParam("framework"); framework != "" {
			filters["framework"] = framework
		}

		limit := 200
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)

		findings, err := h.store.ListFindings(filters, limit)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": findings})
	}
}

func handleResolveFinding(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.UpdateFindingStatus(id, "resolved"); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "RESOLVE_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "RESOLVE_FINDING", "id": id}, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleSuppressFinding(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.UpdateFindingStatus(id, "suppressed"); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "SUPPRESS_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

// Assessment Handlers
func handleRunAssessment(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var req struct {
			Framework string `json:"framework"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		assessment := &ComplianceAssessment{
			ID:        uuid.New().String(),
			Framework: ComplianceFramework(req.Framework),
			Status:    "pending",
			StartedAt: time.Now(),
		}

		if _, err := h.store.RunAssessment(nil, assessment.Framework, nil); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "ASSESSMENT_FAILED"})
		}

		h.evidence.AddEntry(map[string]any{"type": "START_ASSESSMENT", "id": assessment.ID}, nil)

		return c.JSON(http.StatusAccepted, map[string]any{"code": "SUCCESS", "data": assessment})
	}
}

func handleGetAssessment(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		assessment, err := h.store.GetAssessment(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": assessment})
	}
}

func handleListAssessments(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		limit := 50
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)

		assessments, err := h.store.ListAssessments(limit)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": assessments})
	}
}

func handleDeleteAssessment(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.UpdateAssessment(id, map[string]any{"status": "deleted"}); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DELETE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleGetComplianceScore(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		score := map[string]any{
			"overall": 75.5,
			"by_framework": map[string]float64{
				"cis":       82.3,
				"nist":      71.2,
				"soc2":      68.9,
				"pci-dss":   85.0,
			},
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": score})
	}
}

func handleGetResource(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		r, err := h.store.GetResource(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}
		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": r})
	}
}

func handleUpdateResource(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var updates map[string]any
		if err := c.Bind(&updates); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}
		updates["updated_at"] = time.Now().UnixMilli()

		if err := h.store.UpdateResource(id, updates); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "UPDATE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

func handleDeleteResource(h *CSPMHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		
		if err := h.store.DeleteResource(id); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DELETE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}
