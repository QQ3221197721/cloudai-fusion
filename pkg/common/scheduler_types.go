// Package common provides shared types, utilities, and interfaces
// used across all CloudAI Fusion components.
package common

import (
	"time"
)

// ============================================================================
// Scheduling & Self-Heal Action Types
// Unified source of truth for action types across self-heal, scheduling, and AI ops modules.
// Fault types are defined in benchmark_workloads.go for chaos engineering tests.
// =============================================================================

// ActionType defines available remediation actions - centralized definition to avoid duplicate declarations
type ActionType string

const (
	// Kubernetes remediation actions
	ActionPodRestart       ActionType = "pod_restart"
	ActionNodeCordon       ActionType = "node_cordon"
	ActionServiceFailover  ActionType = "service_failover"
	ActionPreemption       ActionType = "preemption"
	
	// Scaling actions
	ActionScaleUp   ActionType = "scale_up"
	ActionScaleDown ActionType = "scale_down"
	
	// Recovery actions
	ActionRestart        ActionType = "restart"
	ActionFailover       ActionType = "failover"
	ActionRollback       ActionType = "rollback"
	ActionIsolate        ActionType = "isolate"
	ActionNotifyOps      ActionType = "notify_ops"
	ActionRunDiagnostic  ActionType = "run_diagnostic"
	ActionExecuteCommand ActionType = "exec_command"
	
	// Security remediation actions (Red Team)
	ActionIsolateNetwork    ActionType = "isolate_network"
	ActionTerminateProcess  ActionType = "terminate_process"
	ActionQuarantineFile    ActionType = "quarantine_file"
	ActionBlockUserAccount  ActionType = "block_user_account"
	ActionPatchVulnerability ActionType = "patch_vulnerability"
	ActionRotateCredentials ActionType = "rotate_credentials"
)

// ============================================================================
// Remediation Results
// ============================================================================

// RemediationResult holds the outcome of a remediation execution
// Used by: aiops, redteam/autoremediation, scheduler
type RemediationResult struct {
	FaultType     FaultType    `json:"fault_type"`
	ActionType    ActionType   `json:"action_type"`
	Success       bool         `json:"success"`
	ErrorMessage  string       `json:"error_message,omitempty"`
	StartTime     time.Time    `json:"start_time"`
	EndTime       time.Time    `json:"end_time"`
	Duration      time.Duration `json:"duration"`
	MetricsStatus string       `json:"metrics_status"` // pending/success/failure/timeout
	Resources     []string     `json:"resources,omitempty"`     // affected resources
	RetryCount    int          `json:"retry_count,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// ============================================================================
// Fault Injection Interface
// Standard interface for fault injection across testing and production
// ============================================================================

// FaultInjector is an interface for injecting faults in tests and controlled environments
type FaultInjector interface {
	Inject(faultType FaultType, severity float64) error
	Cleanup() error
}

// ============================================================================
// Decision & Configuration Types
// ============================================================================

// DecisionResult describes outcome of decision making
type DecisionResult string

const (
	ResultSuccess    DecisionResult = "success"
	ResultFailed     DecisionResult = "failed"
	ResultRolledBack DecisionResult = "rolled_back"
)

// HealingPolicy configures automated healing behavior
type HealingPolicy struct {
	PolicyName     string                 `json:"policy_name"`
	FaultTypes     []FaultType            `json:"fault_types"`
	Actions        []ActionType           `json:"actions"`
	ConfidenceThreshold float64           `json:"confidence_threshold"`
	SafeMode       bool                 `json:"safe_mode"`
	TimeoutSec     int                  `json:"timeout_sec"`
	MaxRetries     int                  `json:"max_retries"`
	Metadata       map[string]any         `json:"metadata,omitempty"`
}

// WorkloadFault represents a fault injected into a workload for testing
type WorkloadFault struct {
	Type       FaultType             `json:"type"`
	Severity   float64               `json:"severity"`
	Metadata   map[string]any        `json:"metadata"`
	DetectedAt time.Time           `json:"detected_at"`
	Source     string                `json:"source"`
	InjectedBy string                `json:"injected_by"`
	CleanedUp  bool                  `json:"cleaned_up"`
}

// RemediationAction defines a specific remediation action to execute
type RemediationAction struct {
	Type       ActionType          `json:"type"`
	Target     string              `json:"target"`
	Parameters map[string]any      `json:"parameters,omitempty"`
	Timeout    time.Duration       `json:"timeout,omitempty"`
	Priority   int                 `json:"priority,omitempty"` // lower = higher priority
	FaultType  FaultType           `json:"fault_type,omitempty"`
}

// RollbackAction defines how to undo remediation if needed
type RollbackAction struct {
	Enabled  bool          `json:"enabled"`
	Delay    time.Duration `json:"delay,omitempty"` // Wait before rollback
	Strategy string        `json:"strategy"`        // immediate, gradual, manual
}
