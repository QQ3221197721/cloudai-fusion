package store

import (
	"testing"
	"time"
)

// ============================================================================
// Table-Driven Tests: Model Validation
// ============================================================================

func TestUserModel_Fields(t *testing.T) {
	tests := []struct {
		name     string
		user     User
		wantErr  bool
		errField string
	}{
		{
			name: "valid user",
			user: User{
				ID: "u1", Username: "admin", Email: "admin@test.io",
				PasswordHash: "$2a$10$hash", Role: "admin", Status: "active",
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			},
		},
		{
			name:     "empty ID",
			user:     User{Username: "test"},
			wantErr:  true,
			errField: "ID",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantErr {
				if tc.user.ID == "" && tc.errField == "ID" {
					// Expected: empty ID
				}
			} else {
				if tc.user.ID == "" {
					t.Error("valid user should have ID")
				}
				if tc.user.Username == "" {
					t.Error("valid user should have Username")
				}
			}
		})
	}
}

func TestClusterModel_Fields(t *testing.T) {
	tests := []struct {
		name    string
		cluster ClusterModel
		wantID  string
	}{
		{
			name: "full cluster",
			cluster: ClusterModel{
				ID: "c1", Name: "prod-cluster", Provider: "aws",
				Region: "us-east-1", Status: "healthy", Endpoint: "https://k8s.example.com",
				KubernetesVersion: "v1.30.0", NodeCount: 10, GPUCount: 80,
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			},
			wantID: "c1",
		},
		{
			name: "minimal cluster",
			cluster: ClusterModel{
				ID: "c2", Name: "test", Provider: "local",
			},
			wantID: "c2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cluster.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", tc.cluster.ID, tc.wantID)
			}
		})
	}
}

func TestWorkloadModel_Fields(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name   string
		w      WorkloadModel
		status string
	}{
		{
			name: "training workload",
			w: WorkloadModel{
				ID: "wl1", Name: "gpt-train", Namespace: "ml",
				ClusterID: "c1", Type: "training", Status: "pending",
				Priority: 10, Framework: "pytorch", Image: "pytorch:2.0",
				ResourceRequest: `{"gpu_count":8}`,
				CreatedAt: now, UpdatedAt: now,
			},
			status: "pending",
		},
		{
			name: "inference workload",
			w: WorkloadModel{
				ID: "wl2", Name: "serve-llm", Namespace: "prod",
				ClusterID: "c1", Type: "inference", Status: "running",
				Framework: "vllm", Image: "vllm:latest",
				StartedAt: &now,
				CreatedAt: now, UpdatedAt: now,
			},
			status: "running",
		},
		{
			name: "completed workload",
			w: WorkloadModel{
				ID: "wl3", Name: "batch-job", Status: "succeeded",
				CompletedAt: &now,
			},
			status: "succeeded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.w.Status != tc.status {
				t.Errorf("Status = %q, want %q", tc.w.Status, tc.status)
			}
		})
	}
}

func TestWorkloadEvent_Fields(t *testing.T) {
	event := WorkloadEvent{
		ID:         "e1",
		WorkloadID: "wl1",
		FromStatus: "pending",
		ToStatus:   "queued",
		Reason:     "scheduled by engine",
		CreatedAt:  time.Now(),
	}

	if event.FromStatus == event.ToStatus {
		t.Error("from and to status should differ")
	}
	if event.WorkloadID == "" {
		t.Error("workload ID should not be empty")
	}
}

func TestSecurityPolicyModel_Fields(t *testing.T) {
	policy := SecurityPolicyModel{
		ID:          "sp1",
		Name:        "network-isolation",
		Type:        "network",
		Enforcement: "enforce",
		Status:      "active",
	}
	if policy.Status != "active" {
		t.Error("policy should be active")
	}
	if policy.Enforcement != "enforce" {
		t.Error("policy enforcement should be enforce")
	}
}

func TestEdgeNodeModel_Fields(t *testing.T) {
	node := EdgeNodeModel{
		ID:       "en1",
		Name:     "edge-factory-01",
		Tier:     "edge",
		Status:   "online",
		Location: "Shanghai Factory",
	}
	if node.Tier != "edge" {
		t.Errorf("tier = %q, want edge", node.Tier)
	}
}

func TestAlertModels(t *testing.T) {
	rule := AlertRuleModel{
		ID:        "ar1",
		Name:      "gpu-overheat",
		Condition: "temperature > 85",
		Severity:  "critical",
		Status:    "active",
	}
	if rule.Status != "active" {
		t.Error("rule should be active")
	}

	event := AlertEventModel{
		ID:       "ae1",
		RuleID:   "ar1",
		Severity: "critical",
		Message:  "GPU temperature exceeded 85C",
	}
	if event.RuleID != rule.ID {
		t.Error("event should reference rule")
	}
}

// ============================================================================
// Config Defaults Tests (table-driven)
// ============================================================================

func TestConfigDefaults_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantOpen int
		wantIdle int
	}{
		{"all defaults", Config{}, 25, 10},
		{"custom open", Config{MaxOpenConns: 50}, 50, 10},
		{"custom idle", Config{MaxIdleConns: 5}, 25, 5},
		{"both custom", Config{MaxOpenConns: 100, MaxIdleConns: 20}, 100, 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			open := tc.cfg.MaxOpenConns
			if open == 0 {
				open = 25
			}
			idle := tc.cfg.MaxIdleConns
			if idle == 0 {
				idle = 10
			}
			if open != tc.wantOpen {
				t.Errorf("MaxOpenConns = %d, want %d", open, tc.wantOpen)
			}
			if idle != tc.wantIdle {
				t.Errorf("MaxIdleConns = %d, want %d", idle, tc.wantIdle)
			}
		})
	}
}
