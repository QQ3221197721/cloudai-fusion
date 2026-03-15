package workload

import (
	"context"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// WorkloadService defines the contract for workload lifecycle management.
// The API layer depends on this interface rather than the concrete *Manager,
// enabling mock implementations for handler unit testing and pluggable backends.
type WorkloadService interface {
	// Create creates a new workload.
	Create(ctx context.Context, req *CreateWorkloadRequest) (*WorkloadResponse, error)

	// List returns workloads with pagination and filtering.
	List(ctx context.Context, clusterID, status string, page, pageSize int) ([]WorkloadResponse, int64, error)

	// Get returns a single workload by ID.
	Get(ctx context.Context, id string) (*WorkloadResponse, error)

	// Delete removes a workload.
	Delete(ctx context.Context, id string) error

	// UpdateStatus transitions a workload to a new status.
	UpdateStatus(ctx context.Context, id string, update *WorkloadStatusUpdate) (*WorkloadResponse, error)

	// GetEvents returns events for a workload.
	GetEvents(ctx context.Context, workloadID string, limit int) ([]store.WorkloadEvent, error)
}

// Compile-time interface satisfaction check.
var _ WorkloadService = (*Manager)(nil)
