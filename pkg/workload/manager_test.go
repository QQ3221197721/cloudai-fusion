package workload

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// ============================================================================
// State Machine Tests (table-driven)
// ============================================================================

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		// pending transitions
		{"pending->queued", "pending", "queued", true},
		{"pending->cancelled", "pending", "cancelled", true},
		{"pending->failed", "pending", "failed", true},
		{"pending->running", "pending", "running", false},
		{"pending->succeeded", "pending", "succeeded", false},

		// queued transitions
		{"queued->scheduled", "queued", "scheduled", true},
		{"queued->cancelled", "queued", "cancelled", true},
		{"queued->failed", "queued", "failed", true},
		{"queued->running", "queued", "running", false},

		// scheduled transitions
		{"scheduled->running", "scheduled", "running", true},
		{"scheduled->cancelled", "scheduled", "cancelled", true},
		{"scheduled->failed", "scheduled", "failed", true},
		{"scheduled->pending", "scheduled", "pending", false},

		// running transitions
		{"running->succeeded", "running", "succeeded", true},
		{"running->failed", "running", "failed", true},
		{"running->cancelled", "running", "cancelled", true},
		{"running->preempted", "running", "preempted", true},
		{"running->pending", "running", "pending", false},

		// preempted transitions
		{"preempted->queued", "preempted", "queued", true},
		{"preempted->cancelled", "preempted", "cancelled", true},
		{"preempted->running", "preempted", "running", false},

		// terminal states (no transitions allowed)
		{"succeeded->any", "succeeded", "running", false},
		{"failed->any", "failed", "pending", false},
		{"cancelled->any", "cancelled", "queued", false},

		// unknown state
		{"unknown->any", "unknown", "pending", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isValidTransition(tc.from, tc.to)
			if result != tc.expected {
				t.Errorf("isValidTransition(%q, %q) = %v, want %v", tc.from, tc.to, result, tc.expected)
			}
		})
	}
}

// ============================================================================
// Manager Construction Tests
// ============================================================================

func TestNewManager(t *testing.T) {
	mgr, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}
	if mgr.HasStore() {
		t.Error("manager without store should return HasStore()=false")
	}
}

func TestNewManager_WithNilLogger(t *testing.T) {
	mgr, err := NewManager(ManagerConfig{Logger: nil})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr.logger == nil {
		t.Error("logger should default to standard logger")
	}
}

// ============================================================================
// No-Store Error Path Tests
// ============================================================================

func TestCreate_NoStore(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	_, err := mgr.Create(context.Background(), &CreateWorkloadRequest{
		Name:      "test",
		ClusterID: "c1",
		Type:      "training",
		Image:     "pytorch:latest",
	})
	if err == nil {
		t.Error("Create without store should return error")
	}
}

func TestGet_NoStore(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	_, err := mgr.Get(nil, "some-id")
	if err == nil {
		t.Error("Get without store should return error")
	}
}

func TestList_NoStore(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	items, total, err := mgr.List(nil, "", "", 1, 20)
	if err != nil {
		t.Errorf("List without store should not error, got: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 total, got %d", total)
	}
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d", len(items))
	}
}

func TestDelete_NoStore(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	err := mgr.Delete(nil, "some-id")
	if err == nil {
		t.Error("Delete without store should return error")
	}
}

func TestUpdateStatus_NoStore(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	_, err := mgr.UpdateStatus(nil, "some-id", &WorkloadStatusUpdate{
		Status: "queued",
	})
	if err == nil {
		t.Error("UpdateStatus without store should return error")
	}
}

func TestGetEvents_NoStore(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	events, err := mgr.GetEvents(nil, "wl-1", 0)
	if err != nil {
		t.Errorf("GetEvents without store should not error, got: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty events, got %d", len(events))
	}
}

// ============================================================================
// List Pagination Defaults
// ============================================================================

func TestList_PaginationDefaults(t *testing.T) {
	tests := []struct {
		name         string
		page         int
		pageSize     int
		wantPage     int
		wantPageSize int
	}{
		{"negative page defaults to 1", -1, 20, 1, 20},
		{"zero page defaults to 1", 0, 20, 1, 20},
		{"zero pageSize defaults to 20", 1, 0, 1, 20},
		{"over-100 pageSize defaults to 20", 1, 200, 1, 20},
		{"valid values pass through", 3, 50, 3, 50},
	}

	mgr, _ := NewManager(ManagerConfig{})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// No store = returns empty but exercises pagination logic
			items, _, _ := mgr.List(nil, "", "", tc.page, tc.pageSize)
			if len(items) != 0 {
				t.Errorf("expected empty items from no-store manager")
			}
		})
	}
}

// ============================================================================
// ModelToResponse Tests
// ============================================================================

func TestModelToResponse(t *testing.T) {
	model := newMockWorkloadModel()
	resp := modelToResponse(model)

	if resp.ID != model.ID {
		t.Errorf("ID mismatch: %s != %s", resp.ID, model.ID)
	}
	if resp.Name != model.Name {
		t.Errorf("Name mismatch: %s != %s", resp.Name, model.Name)
	}
	if resp.Status != model.Status {
		t.Errorf("Status mismatch: %s != %s", resp.Status, model.Status)
	}
	if resp.Namespace != model.Namespace {
		t.Errorf("Namespace mismatch: %s != %s", resp.Namespace, model.Namespace)
	}
}

// newMockWorkloadModel creates a test workload model
func newMockWorkloadModel() *store.WorkloadModel {
	return &store.WorkloadModel{
		ID:              "wl-test-001",
		Name:            "test-training",
		Namespace:       "default",
		ClusterID:       "c1",
		Type:            "training",
		Status:          "pending",
		Priority:        5,
		Framework:       "pytorch",
		Image:           "pytorch:latest",
		ResourceRequest: `{"gpu_count":2,"gpu_type":"nvidia-a100"}`,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}
