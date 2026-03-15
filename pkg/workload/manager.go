// Package workload provides complete AI workload lifecycle management.
// Supports creation, scheduling, monitoring, and cleanup of training/inference
// workloads with database-backed persistence and state machine transitions.
package workload

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// ============================================================================
// Configuration & Types
// ============================================================================

// ManagerConfig holds workload manager configuration
type ManagerConfig struct {
	Store  *store.Store
	Logger *logrus.Logger
}

// CreateWorkloadRequest defines the API request to create a workload
type CreateWorkloadRequest struct {
	Name            string              `json:"name" binding:"required"`
	Namespace       string              `json:"namespace"`
	ClusterID       string              `json:"cluster_id" binding:"required"`
	Type            string              `json:"type" binding:"required"` // training, inference, fine-tuning, batch, serving
	Priority        int                 `json:"priority"`
	Framework       string              `json:"framework"`       // pytorch, tensorflow, jax
	ModelName       string              `json:"model_name"`
	Image           string              `json:"image" binding:"required"`
	Command         string              `json:"command"`
	ResourceRequest common.ResourceRequest `json:"resource_request" binding:"required"`
	EnvVars         map[string]string   `json:"env_vars,omitempty"`
}

// WorkloadResponse is the API response for workload operations
type WorkloadResponse struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Namespace       string              `json:"namespace"`
	ClusterID       string              `json:"cluster_id"`
	Type            string              `json:"type"`
	Status          string              `json:"status"`
	Priority        int                 `json:"priority"`
	Framework       string              `json:"framework"`
	ModelName       string              `json:"model_name,omitempty"`
	Image           string              `json:"image"`
	ResourceRequest common.ResourceRequest `json:"resource_request"`
	AssignedNode    string              `json:"assigned_node,omitempty"`
	AssignedGPUs    string              `json:"assigned_gpus,omitempty"`
	StartedAt       *time.Time          `json:"started_at,omitempty"`
	CompletedAt     *time.Time          `json:"completed_at,omitempty"`
	ErrorMessage    string              `json:"error_message,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// WorkloadStatusUpdate defines a status transition request
type WorkloadStatusUpdate struct {
	Status  string `json:"status" binding:"required"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// ============================================================================
// State Machine - Valid Transitions
// ============================================================================

// validTransitions defines the allowed state transitions for workloads
var validTransitions = map[string][]string{
	"pending":   {"queued", "cancelled", "failed"},
	"queued":    {"scheduled", "cancelled", "failed"},
	"scheduled": {"running", "cancelled", "failed"},
	"running":   {"succeeded", "failed", "cancelled", "preempted"},
	"preempted": {"queued", "cancelled"},
	// Terminal states: succeeded, failed, cancelled — no further transitions
}

// isValidTransition checks if a state transition is allowed
func isValidTransition(from, to string) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false // terminal state or unknown
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// ============================================================================
// Manager
// ============================================================================

// Manager provides complete workload lifecycle management backed by PostgreSQL
type Manager struct {
	store  *store.Store
	logger *logrus.Logger
	mu     sync.RWMutex
}

// NewManager creates a new workload manager
func NewManager(cfg ManagerConfig) (*Manager, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &Manager{
		store:  cfg.Store,
		logger: logger,
	}, nil
}

// HasStore returns whether the manager has a database store
func (m *Manager) HasStore() bool {
	return m.store != nil
}

// ============================================================================
// CRUD Operations
// ============================================================================

// Create creates a new workload and persists it to the database
func (m *Manager) Create(ctx context.Context, req *CreateWorkloadRequest) (*WorkloadResponse, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	if m.store == nil {
		return nil, apperrors.ErrServiceUnavailable
	}

	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	// Serialize resource request to JSON
	resJSON, err := json.Marshal(req.ResourceRequest)
	if err != nil {
		return nil, apperrors.Validation("invalid resource request", nil).WithCause(err)
	}

	envJSON := "{}"
	if len(req.EnvVars) > 0 {
		b, _ := json.Marshal(req.EnvVars)
		envJSON = string(b)
	}

	now := time.Now().UTC()
	model := &store.WorkloadModel{
		ID:               common.NewUUID(),
		Name:             req.Name,
		Namespace:        ns,
		ClusterID:        req.ClusterID,
		Type:             req.Type,
		Status:           "pending",
		Priority:         req.Priority,
		Framework:        req.Framework,
		ModelName:        req.ModelName,
		Image:            req.Image,
		Command:          req.Command,
		ResourceRequest:  string(resJSON),
		GPUTypeRequired:  string(req.ResourceRequest.GPUType),
		GPUCountRequired: req.ResourceRequest.GPUCount,
		GPUMemRequired:   req.ResourceRequest.GPUMemoryBytes,
		EnvVars:          envJSON,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := m.store.CreateWorkload(model); err != nil {
		return nil, apperrors.Database("create workload", err)
	}

	m.logger.WithFields(logrus.Fields{
		"workload_id": model.ID,
		"name":        model.Name,
		"type":        model.Type,
		"cluster_id":  model.ClusterID,
	}).Info("Workload created")

	return modelToResponse(model), nil
}

// Get retrieves a workload by ID
func (m *Manager) Get(ctx context.Context, id string) (*WorkloadResponse, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	if m.store == nil {
		return nil, apperrors.ErrServiceUnavailable
	}

	model, err := m.store.GetWorkloadByID(id)
	if err != nil {
		return nil, apperrors.NotFound("workload", id)
	}
	return modelToResponse(model), nil
}

// List returns paginated workloads with optional filters
func (m *Manager) List(ctx context.Context, clusterID, status string, page, pageSize int) ([]WorkloadResponse, int64, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, 0, err
	}
	if m.store == nil {
		return []WorkloadResponse{}, 0, nil
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	models, total, err := m.store.ListWorkloads(clusterID, status, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]WorkloadResponse, len(models))
	for i, model := range models {
		responses[i] = *modelToResponse(&model)
	}
	return responses, total, nil
}

// Delete soft-deletes a workload (only allowed for terminal or pending states)
func (m *Manager) Delete(ctx context.Context, id string) error {
	if err := apperrors.CheckContext(ctx); err != nil {
		return err
	}
	if m.store == nil {
		return apperrors.ErrServiceUnavailable
	}

	model, err := m.store.GetWorkloadByID(id)
	if err != nil {
		return apperrors.NotFound("workload", id)
	}

	// Only allow deletion of terminal/pending workloads
	switch model.Status {
	case "pending", "succeeded", "failed", "cancelled":
		// OK
	default:
		return apperrors.Conflict(fmt.Sprintf("cannot delete workload in '%s' state; cancel it first", model.Status))
	}

	if err := m.store.DeleteWorkload(id); err != nil {
		return apperrors.Database("delete workload", err)
	}

	m.logger.WithField("workload_id", id).Info("Workload deleted")
	return nil
}

// UpdateStatus transitions a workload to a new state (with validation)
func (m *Manager) UpdateStatus(ctx context.Context, id string, update *WorkloadStatusUpdate) (*WorkloadResponse, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	if m.store == nil {
		return nil, apperrors.ErrServiceUnavailable
	}

	model, err := m.store.GetWorkloadByID(id)
	if err != nil {
		return nil, apperrors.NotFound("workload", id)
	}

	if !isValidTransition(model.Status, update.Status) {
		return nil, apperrors.InvalidStateTransition("workload", model.Status, update.Status)
	}

	// Perform the status update in a transaction
	if err := m.store.UpdateWorkloadStatus(id, model.Status, update.Status, update.Reason); err != nil {
		return nil, apperrors.Database("update workload status", err)
	}

	// Set timestamps for terminal states
	now := time.Now().UTC()
	model.Status = update.Status
	model.UpdatedAt = now

	switch update.Status {
	case "running":
		model.StartedAt = &now
		m.store.UpdateWorkload(model)
	case "succeeded", "failed", "cancelled":
		model.CompletedAt = &now
		if update.Message != "" {
			model.ErrorMessage = update.Message
		}
		m.store.UpdateWorkload(model)
	}

	m.logger.WithFields(logrus.Fields{
		"workload_id": id,
		"from":        model.Status,
		"to":          update.Status,
		"reason":      update.Reason,
	}).Info("Workload status updated")

	return modelToResponse(model), nil
}

// GetEvents returns the state transition history for a workload
func (m *Manager) GetEvents(ctx context.Context, workloadID string, limit int) ([]store.WorkloadEvent, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	if m.store == nil {
		return []store.WorkloadEvent{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	return m.store.ListWorkloadEvents(workloadID, limit)
}

// ============================================================================
// Conversion Helpers
// ============================================================================

func modelToResponse(model *store.WorkloadModel) *WorkloadResponse {
	resp := &WorkloadResponse{
		ID:           model.ID,
		Name:         model.Name,
		Namespace:    model.Namespace,
		ClusterID:    model.ClusterID,
		Type:         model.Type,
		Status:       model.Status,
		Priority:     model.Priority,
		Framework:    model.Framework,
		ModelName:    model.ModelName,
		Image:        model.Image,
		AssignedNode: model.AssignedNode,
		AssignedGPUs: model.AssignedGPUs,
		StartedAt:    model.StartedAt,
		CompletedAt:  model.CompletedAt,
		ErrorMessage: model.ErrorMessage,
		CreatedAt:    model.CreatedAt,
		UpdatedAt:    model.UpdatedAt,
	}

	// Deserialize resource request
	var rr common.ResourceRequest
	if err := json.Unmarshal([]byte(model.ResourceRequest), &rr); err == nil {
		resp.ResourceRequest = rr
	}

	return resp
}
