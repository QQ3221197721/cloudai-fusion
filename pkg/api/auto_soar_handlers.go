// Package api provides HTTP handlers for M32 Automated SOAR Response System
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

// TriggerCondition defines when a playbook should execute
type TriggerCondition struct {
	ID          string   `json:"id"`
	Type        string   // incident_created, ioc_detected, alert_received
	Resource    string   // entity being watched
	Criteria    map[string]any // operators: {"status": "critical", "severity": ["high", "critical"]}
}

// ExecutionStep represents a step in playbook workflow
type ExecutionStep struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            // api_call, script, wait, condition
	Order    int               `json:"order"`
	Config   map[string]any    `json:"config,omitempty"`
	Timeout  time.Duration     `json:"timeout,omitempty"`
	Retry    int               `json:"retry,omitempty"`
}

// Playbook represents automated response workflow
type Playbook struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Version        string            `json:"version"`
	Triggers       []TriggerCondition `json:"triggers"`
	Steps          []ExecutionStep   `json:"steps"`
	Status         string            // active, inactive, testing
	Priority       int               // lower = higher priority
	Schedule       string            // cron expression
	MaxExecutions  int               // max runs before deactivation
	Cooldown       time.Duration     // minimum interval between executions
	LastExecution  time.Time         `json:"lastExecution"`
	ExecutionCount int               `json:"executionCount"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// Incident represents security event needing response
type Incident struct {
	ID              string                   `json:"id"`
	Title           string                   `json:"title"`
	Type            string                   // ioc_match, alert, anomaly
	Severity        string                   // low, medium, high, critical
	Status          string                   // new, in_progress, resolved, closed
	PlaybookID      string                   `json:"playbookId,omitempty"`
	RelatedIOCs     []string                 `json:"relatedIocs"`
	AssignedTo      string                   `json:"assignedTo,omitempty"`
	Evidence        map[string]any           `json:"evidence"`
	ResolvedAt      time.Time                `json:"resolvedAt,omitempty"`
	ResolutionNotes string                   `json:"resolutionNotes,omitempty"`
	CreatedAt       time.Time                `json:"createdAt"`
	UpdatedAt       time.Time                `json:"updatedAt"`
}

// SOARStore interface for SOAR data persistence
type SOARStore interface {
	CreatePlaybook(pb *Playbook) error
	GetPlaybook(id string) (*Playbook, error)
	UpdatePlaybook(id string, updates map[string]any) error
	DeletePlaybook(id string) error
	ListPlaybooks(status string) ([]Playbook, error)
	
	CreateIncident(inc *Incident) error
	GetIncident(id string) (*Incident, error)
	UpdateIncident(id string, updates map[string]any) error
	CloseIncident(id string, resolution string) error
	ListIncidents(filters map[string]any, limit, offset int) ([]Incident, error)
	
	LogExecution(executionLog *ExecutionLog) error
	GetExecutions(playbookID string, since time.Time) ([]ExecutionLog, error)
}

// ExecutionLog tracks playbook run
type ExecutionLog struct {
	ID            string            `json:"id"`
	PlaybookID    string            `json:"playbookId"`
	IncidentID    string            `json:"incidentId"`
	StartedAt     time.Time         `json:"startedAt"`
	CompletedAt   time.Time         `json:"completedAt,omitempty"`
	Status        string            // running, succeeded, failed, cancelled
	StepsExecuted []StepResult      `json:"stepsExecuted"`
	Error         string            `json:"error,omitempty"`
}

// StepResult captures execution of single step
type StepResult struct {
	StepID   string        `json:"stepId"`
	StepName string        `json:"stepName"`
	Status   string        // success, failed, skipped
	Result   any           `json:"result,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
}

// AutoSOARHandler handles M32 SOAR API requests
type AutoSOARHandler struct {
	store    SOARStore
	logger   *logrus.Logger
	evidence *evidence.Ledger
}

// NewAutoSOARHandler creates new SOAR handler
func NewAutoSOARHandler(store SOARStore, logger *logrus.Logger, evidence *evidence.Ledger) *AutoSOARHandler {
	return &AutoSOARHandler{
		store:    store,
		logger:   logger,
		evidence: evidence,
	}
}

// RegisterAutoSOARRoutes registers SOAR routes
func RegisterAutoSOARRoutes(router *echo.Echo, handler *AutoSOARHandler) {
	group := router.Group("/api/m32/soar")

	// Playbook Management
	group.POST("/playbooks", handleCreatePlaybook(handler))
	group.GET("/playbooks/:id", handleGetPlaybook(handler))
	group.PUT("/playbooks/:id", handleUpdatePlaybook(handler))
	group.DELETE("/playbooks/:id", handleDeletePlaybook(handler))
	group.GET("/playbooks", handleListPlaybooks(handler))
	group.POST("/playbooks/:id/test", handleTestPlaybook(handler))
	group.POST("/playbooks/:id/deploy", handleDeployPlaybook(handler))

	// Incident Management
	group.POST("/incidents", handleCreateIncident(handler))
	group.GET("/incidents/:id", handleGetIncident(handler))
	group.PUT("/incidents/:id", handleUpdateIncident(handler))
	group.POST("/incidents/:id/resolve", handleResolveIncident(handler))
	group.GET("/incidents", handleListIncidents(handler))

	// Executions
	group.GET("/playbooks/:id/executions", handleListExecutions(handler))
	group.GET("/executions/:id", handleGetExecution(handler))

	// Analytics
	group.GET("/analytics/playbook-stats", handleGetPlaybookStats(handler))
}

// Add audit trail entry with evidence recording (for backward compatibility)
func (h *AutoSOARHandler) addAuditEntry(entryType string, id string, extraData map[string]any) {
	if h.evidence == nil {
		return
	}
	
	data := gin.H{
		"type": entryType,
		"id":   id,
	}
	for k, v := range extraData {
		data[k] = v
	}
	
	receipt := evidence.Receipt{
		Action:    entryType,
		Subject:   id,
		Actor:     "system", // TODO: Extract from auth context
		Timestamp: time.Now().UTC(),
		Metadata:  data,
	}
	
	if err := h.evidence.RecordReceipt(receipt); err != nil {
		h.logger.WithError(err).Warn("Failed to record SOAR evidence (non-critical)")
	}
}

// handleCreatePlaybook creates new playbook
func handleCreatePlaybook(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var playbook Playbook
		if err := c.Bind(&playbook); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST", "message": err.Error()})
		}

		playbook.ID = uuid.New().String()
		playbook.Version = "1.0.0"
		playbook.Status = "inactive"
		playbook.CreatedAt = time.Now()
		playbook.UpdatedAt = time.Now()

		if err := h.store.CreatePlaybook(&playbook); err != nil {
			h.logger.WithError(err).Error("Failed to create playbook")
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "CREATE_FAILED", "message": "Failed to create playbook"})
		}

		h.addAuditEntry("CREATE_PLAYBOOK", playbook.ID, map[string]any{
			"name":  playbook.Name,
			"steps": len(playbook.Steps),
		})

		return c.JSON(http.StatusCreated, map[string]any{"code": "SUCCESS", "data": playbook})
	}
}

// handleUpdatePlaybook updates playbook
func handleUpdatePlaybook(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var updates map[string]any
		if err := c.Bind(&updates); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST", "message": err.Error()})
		}

		updates["updated_at"] = time.Now().UnixMilli()
		if err := h.store.UpdatePlaybook(id, updates); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "UPDATE_FAILED", "message": "Failed to update"})
		}

		h.addAuditEntry("UPDATE_PLAYBOOK", id, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

// handleDeployPlaybook activates playbook
func handleDeployPlaybook(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")

		if err := h.store.UpdatePlaybook(id, map[string]any{"status": "active"}); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DEPLOY_FAILED"})
		}

		h.addAuditEntry("DEPLOY_PLAYBOOK", id, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

// handleTestPlaybook tests playbook without execution
func handleTestPlaybook(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")

		playbook, err := h.store.GetPlaybook(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}

		// Validation logic here
		if len(playbook.Triggers) == 0 {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_TEST", "message": "No triggers defined"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "message": "Playbook validated"})
	}
}

// handleCreateIncident creates new incident
func handleCreateIncident(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		var incident Incident
		if err := c.Bind(&incident); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST", "message": err.Error()})
		}

		incident.ID = uuid.New().String()
		incident.Status = "new"
		incident.CreatedAt = time.Now()
		incident.UpdatedAt = time.Now()

		if err := h.store.CreateIncident(&incident); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "CREATE_FAILED"})
		}

		h.addAuditEntry("CREATE_INCIDENT", incident.ID, nil)

		return c.JSON(http.StatusCreated, map[string]any{"code": "SUCCESS", "data": incident})
	}
}

// handleResolveIncident closes incident
func handleResolveIncident(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var req struct {
			Notes string `json:"notes"`
		}
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}

		if err := h.store.CloseIncident(id, req.Notes); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "RESOLVE_FAILED"})
		}

		h.addAuditEntry("CLOSE_INCIDENT", id, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

// handleListPlaybooks lists playbooks
func handleListPlaybooks(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		status := c.QueryParam("status")
		plays, err := h.store.ListPlaybooks(status)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": plays})
	}
}

// handleListIncidents lists incidents
func handleListIncidents(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		filters := make(map[string]any)
		if severity := c.QueryParam("severity"); severity != "" {
			filters["severity"] = severity
		}
		if status := c.QueryParam("status"); status != "" {
			filters["status"] = status
		}

		limit := 100
		offset := 0
		fmt.Sscanf(c.QueryParam("limit"), "%d", &limit)
		fmt.Sscanf(c.QueryParam("offset"), "%d", &offset)

		incidents, err := h.store.ListIncidents(filters, limit, offset)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": incidents})
	}
}

// handleGetExecution retrieves execution details
func handleGetExecution(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		executions, _ := h.store.GetExecutions("", time.Time{})
		
		for _, exec := range executions {
			if exec.ID == id {
				return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": exec})
			}
		}

		return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
	}
}

// handleListExecutions lists playbook executions
func handleListExecutions(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		playbookID := c.Param("id")
		since := time.Now().AddDate(0, 0, -7)
		
		executions, err := h.store.GetExecutions(playbookID, since)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "FETCH_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": executions})
	}
}

// handleGetPlaybook retrieves single playbook
func handleGetPlaybook(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		pb, err := h.store.GetPlaybook(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}
		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": pb})
	}
}

// handleDeletePlaybook removes playbook
func handleDeletePlaybook(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		if err := h.store.DeletePlaybook(id); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "DELETE_FAILED"})
		}

		h.addAuditEntry("DELETE_PLAYBOOK", id, nil)

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

// handleGetIncident retrieves incident by ID
func handleGetIncident(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		inc, err := h.store.GetIncident(id)
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]any{"code": "NOT_FOUND"})
		}
		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": inc})
	}
}

// handleUpdateIncident modifies incident
func handleUpdateIncident(h *AutoSOARHandler) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		var updates map[string]any
		if err := c.Bind(&updates); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]any{"code": "INVALID_REQUEST"})
		}
		updates["updated_at"] = time.Now().UnixMilli()

		if err := h.store.UpdateIncident(id, updates); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"code": "UPDATE_FAILED"})
		}

		return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS"})
	}
}

// handleGetPlaybookStats returns playbook analytics
func handleGetPlaybookStats(h *AutoSOARHandler) echo.HandlerFunc {
	stats := map[string]any{
		"total_playbooks":    0,
		"active_playbooks":   0,
		"total_executions":   0,
		"succeeded_executions": 0,
	}
	
	return c.JSON(http.StatusOK, map[string]any{"code": "SUCCESS", "data": stats})
}
