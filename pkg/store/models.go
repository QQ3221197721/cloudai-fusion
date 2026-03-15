package store

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user account in the database
type User struct {
	ID           string         `gorm:"type:uuid;primaryKey" json:"id"`
	Username     string         `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Email        string         `gorm:"uniqueIndex;size:128;not null" json:"email"`
	PasswordHash string         `gorm:"size:256;not null" json:"-"`
	DisplayName  string         `gorm:"size:128" json:"display_name"`
	Role         string         `gorm:"size:32;not null;default:viewer" json:"role"`
	Status       string         `gorm:"size:32;not null;default:active" json:"status"`
	LastLoginAt  *time.Time     `json:"last_login_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }

// AuditLog records security-relevant events
type AuditLog struct {
	ID           string    `gorm:"type:uuid;primaryKey" json:"id"`
	UserID       string    `gorm:"size:64;index" json:"user_id"`
	Username     string    `gorm:"size:64" json:"username"`
	Action       string    `gorm:"size:64;not null" json:"action"`
	ResourceType string    `gorm:"size:64" json:"resource_type"`
	ResourceID   string    `gorm:"size:128" json:"resource_id"`
	IPAddress    string    `gorm:"size:64" json:"ip_address"`
	UserAgent    string    `gorm:"size:256" json:"user_agent"`
	Status       string    `gorm:"size:32;not null" json:"status"`
	Details      string    `gorm:"type:text" json:"details,omitempty"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }

// ============================================================================
// Cluster Model - Persistent cluster records
// ============================================================================

// ClusterModel represents a managed Kubernetes cluster persisted in PG
type ClusterModel struct {
	ID                string         `gorm:"type:uuid;primaryKey" json:"id"`
	Name              string         `gorm:"size:128;not null" json:"name"`
	Provider          string         `gorm:"size:32;not null" json:"provider"`
	ProviderClusterID string         `gorm:"size:256" json:"provider_cluster_id"`
	Region            string         `gorm:"size:64" json:"region"`
	KubernetesVersion string         `gorm:"size:32" json:"kubernetes_version"`
	Endpoint          string         `gorm:"size:512" json:"endpoint"`
	CACertificate     string         `gorm:"type:text" json:"-"`
	Status            string         `gorm:"size:32;not null;default:pending" json:"status"`
	NodeCount         int            `gorm:"default:0" json:"node_count"`
	GPUCount          int            `gorm:"default:0" json:"gpu_count"`
	TotalCPU          int64          `gorm:"default:0" json:"total_cpu_millicores"`
	TotalMemory       int64          `gorm:"default:0" json:"total_memory_bytes"`
	TotalGPUMemory    int64          `gorm:"default:0" json:"total_gpu_memory_bytes"`
	Labels            string         `gorm:"type:jsonb;default:'{}'" json:"labels"`
	Annotations       string         `gorm:"type:jsonb;default:'{}'" json:"annotations"`
	Config            string         `gorm:"type:jsonb;default:'{}'" json:"config"`
	HealthCheckAt     *time.Time     `json:"health_check_at,omitempty"`
	CreatedBy         string         `gorm:"type:uuid" json:"created_by,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ClusterModel) TableName() string { return "clusters" }

// ============================================================================
// Workload Model - AI workload lifecycle records
// ============================================================================

// WorkloadModel represents an AI workload persisted in PG
type WorkloadModel struct {
	ID               string         `gorm:"type:uuid;primaryKey" json:"id"`
	Name             string         `gorm:"size:256;not null" json:"name"`
	Namespace        string         `gorm:"size:128;default:default" json:"namespace"`
	ClusterID        string         `gorm:"type:uuid;index;not null" json:"cluster_id"`
	Type             string         `gorm:"size:32;not null" json:"type"`
	Status           string         `gorm:"size:32;not null;default:pending" json:"status"`
	Priority         int            `gorm:"default:0" json:"priority"`
	Framework        string         `gorm:"size:32" json:"framework"`
	ModelName        string         `gorm:"size:256" json:"model_name,omitempty"`
	Image            string         `gorm:"size:512" json:"image"`
	Command          string         `gorm:"type:text" json:"command,omitempty"`
	ResourceRequest  string         `gorm:"type:jsonb;not null;default:'{}'" json:"resource_request"`
	ResourceLimit    string         `gorm:"type:jsonb;default:'{}'" json:"resource_limit"`
	GPUTypeRequired  string         `gorm:"size:64" json:"gpu_type_required,omitempty"`
	GPUCountRequired int            `gorm:"default:0" json:"gpu_count_required"`
	GPUMemRequired   int64          `gorm:"default:0" json:"gpu_memory_required"`
	EnvVars          string         `gorm:"type:jsonb;default:'{}'" json:"env_vars,omitempty"`
	SchedulingPolicy string         `gorm:"type:jsonb;default:'{}'" json:"scheduling_policy,omitempty"`
	AssignedNode     string         `gorm:"size:256" json:"assigned_node,omitempty"`
	AssignedGPUs     string         `gorm:"size:256" json:"assigned_gpus,omitempty"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	CompletedAt      *time.Time     `json:"completed_at,omitempty"`
	ErrorMessage     string         `gorm:"type:text" json:"error_message,omitempty"`
	Metrics          string         `gorm:"type:jsonb;default:'{}'" json:"metrics,omitempty"`
	CreatedBy        string         `gorm:"type:uuid" json:"created_by,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WorkloadModel) TableName() string { return "workloads" }

// ============================================================================
// Workload Event - State transition history
// ============================================================================

// WorkloadEvent records state transitions for a workload
type WorkloadEvent struct {
	ID         string    `gorm:"type:uuid;primaryKey" json:"id"`
	WorkloadID string    `gorm:"type:uuid;index;not null" json:"workload_id"`
	FromStatus string    `gorm:"size:32" json:"from_status"`
	ToStatus   string    `gorm:"size:32;not null" json:"to_status"`
	Reason     string    `gorm:"size:256" json:"reason,omitempty"`
	Message    string    `gorm:"type:text" json:"message,omitempty"`
	Operator   string    `gorm:"size:128" json:"operator,omitempty"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func (WorkloadEvent) TableName() string { return "workload_events" }

// ============================================================================
// Security Policy Model
// ============================================================================

// SecurityPolicyModel persists security policies in PG
type SecurityPolicyModel struct {
	ID          string         `gorm:"type:uuid;primaryKey" json:"id"`
	Name        string         `gorm:"size:128;not null" json:"name"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Type        string         `gorm:"size:32;not null" json:"type"`
	Scope       string         `gorm:"size:32;not null;default:cluster" json:"scope"`
	ClusterID   string         `gorm:"type:uuid" json:"cluster_id,omitempty"`
	Rules       string         `gorm:"type:jsonb;not null;default:'[]'" json:"rules"`
	Enforcement string         `gorm:"size:16;not null;default:enforce" json:"enforcement"`
	Status      string         `gorm:"size:16;not null;default:active" json:"status"`
	CreatedBy   string         `gorm:"type:uuid" json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (SecurityPolicyModel) TableName() string { return "security_policies" }

// ============================================================================
// Vulnerability Scan Model
// ============================================================================

// VulnerabilityScanModel persists vulnerability scan results in PG
type VulnerabilityScanModel struct {
	ID              string         `gorm:"type:uuid;primaryKey" json:"id"`
	ClusterID       string         `gorm:"type:uuid;index" json:"cluster_id"`
	ScanType        string         `gorm:"size:32;not null" json:"scan_type"`
	Status          string         `gorm:"size:32;not null;default:pending" json:"status"`
	Findings        string         `gorm:"type:jsonb;default:'[]'" json:"findings"`
	Summary         string         `gorm:"type:jsonb;default:'{}'" json:"summary"`
	TotalFindings   int            `gorm:"default:0" json:"total_findings"`
	CriticalCount   int            `gorm:"default:0" json:"critical_count"`
	HighCount       int            `gorm:"default:0" json:"high_count"`
	MediumCount     int            `gorm:"default:0" json:"medium_count"`
	LowCount        int            `gorm:"default:0" json:"low_count"`
	StartedAt       time.Time      `json:"started_at"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (VulnerabilityScanModel) TableName() string { return "vulnerability_scans" }

// ============================================================================
// Mesh Policy Model
// ============================================================================

// MeshPolicyModel persists eBPF network policies in PG
type MeshPolicyModel struct {
	ID           string         `gorm:"type:uuid;primaryKey" json:"id"`
	Name         string         `gorm:"size:128;not null" json:"name"`
	Namespace    string         `gorm:"size:128;default:default" json:"namespace"`
	ClusterID    string         `gorm:"type:uuid" json:"cluster_id,omitempty"`
	Type         string         `gorm:"size:16;not null" json:"type"`
	Selector     string         `gorm:"type:jsonb;default:'{}'" json:"selector"`
	IngressRules string         `gorm:"type:jsonb;default:'[]'" json:"ingress_rules"`
	EgressRules  string         `gorm:"type:jsonb;default:'[]'" json:"egress_rules"`
	L7Rules      string         `gorm:"type:jsonb;default:'[]'" json:"l7_rules"`
	Enforcement  string         `gorm:"size:16;not null;default:enforce" json:"enforcement"`
	Status       string         `gorm:"size:16;not null;default:active" json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (MeshPolicyModel) TableName() string { return "mesh_policies" }

// ============================================================================
// Wasm Module & Instance Models
// ============================================================================

// WasmModuleModel persists WebAssembly modules in PG
type WasmModuleModel struct {
	ID           string         `gorm:"type:uuid;primaryKey" json:"id"`
	Name         string         `gorm:"size:256;not null" json:"name"`
	Version      string         `gorm:"size:64" json:"version"`
	Runtime      string         `gorm:"size:32;not null" json:"runtime"`
	SourceURL    string         `gorm:"size:512" json:"source_url"`
	Size         int64          `gorm:"default:0" json:"size_bytes"`
	Hash         string         `gorm:"size:128" json:"hash_sha256"`
	Capabilities string         `gorm:"type:jsonb;default:'[]'" json:"capabilities"`
	Metadata     string         `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	UploadedAt   time.Time      `json:"uploaded_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WasmModuleModel) TableName() string { return "wasm_modules" }

// WasmInstanceModel persists running Wasm instances in PG
type WasmInstanceModel struct {
	ID           string         `gorm:"type:uuid;primaryKey" json:"id"`
	ModuleID     string         `gorm:"type:uuid;index;not null" json:"module_id"`
	ModuleName   string         `gorm:"size:256" json:"module_name"`
	ClusterID    string         `gorm:"type:uuid;index" json:"cluster_id"`
	NodeName     string         `gorm:"size:256" json:"node_name"`
	Status       string         `gorm:"size:32;not null;default:pending" json:"status"`
	Runtime      string         `gorm:"size:32" json:"runtime"`
	MemoryUsedKB int64          `gorm:"default:0" json:"memory_used_kb"`
	CPUUsageMs   int64          `gorm:"default:0" json:"cpu_usage_ms"`
	RequestCount int64          `gorm:"default:0" json:"request_count"`
	ColdStartMs  float64        `gorm:"default:0" json:"cold_start_ms"`
	Labels       string         `gorm:"type:jsonb;default:'{}'" json:"labels"`
	StartedAt    time.Time      `json:"started_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WasmInstanceModel) TableName() string { return "wasm_instances" }

// ============================================================================
// Edge Node Model
// ============================================================================

// EdgeNodeModel persists edge computing nodes in PG
type EdgeNodeModel struct {
	ID                string         `gorm:"type:uuid;primaryKey" json:"id"`
	Name              string         `gorm:"size:128;not null" json:"name"`
	Region            string         `gorm:"size:64" json:"region"`
	Tier              string         `gorm:"size:16;not null" json:"tier"`
	Status            string         `gorm:"size:16;not null;default:online" json:"status"`
	ClusterID         string         `gorm:"type:uuid" json:"cluster_id,omitempty"`
	IPAddress         string         `gorm:"size:64" json:"ip_address"`
	Location          string         `gorm:"type:jsonb;default:'{}'" json:"location"`
	CPUCores          int            `gorm:"default:0" json:"cpu_cores"`
	MemoryGB          float64        `gorm:"default:0" json:"memory_gb"`
	GPUType           string         `gorm:"size:64" json:"gpu_type"`
	GPUCount          int            `gorm:"default:0" json:"gpu_count"`
	GPUMemoryGB       float64        `gorm:"default:0" json:"gpu_memory_gb"`
	StorageGB         float64        `gorm:"default:0" json:"storage_gb"`
	PowerBudgetWatts  int            `gorm:"default:200" json:"power_budget_watts"`
	CurrentPowerWatts float64        `gorm:"default:0" json:"current_power_watts"`
	NetworkBandwidth  float64        `gorm:"default:0" json:"network_bandwidth_mbps"`
	LatencyToCloudMs  float64        `gorm:"default:0" json:"latency_to_cloud_ms"`
	IsOfflineCapable  bool           `gorm:"default:false" json:"is_offline_capable"`
	DeployedModels    string         `gorm:"type:jsonb;default:'[]'" json:"deployed_models"`
	ResourceUsage     string         `gorm:"type:jsonb;default:'{}'" json:"resource_usage"`
	Labels            string         `gorm:"type:jsonb;default:'{}'" json:"labels"`
	LastHeartbeatAt   time.Time      `json:"last_heartbeat_at"`
	RegisteredAt      time.Time      `json:"registered_at"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (EdgeNodeModel) TableName() string { return "edge_nodes" }

// ============================================================================
// Alert Rule & Event Models
// ============================================================================

// AlertRuleModel persists monitoring alert rules in PG
type AlertRuleModel struct {
	ID          string         `gorm:"type:uuid;primaryKey" json:"id"`
	Name        string         `gorm:"size:128;not null" json:"name"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	ClusterID   string         `gorm:"type:uuid" json:"cluster_id,omitempty"`
	Severity    string         `gorm:"size:16;not null" json:"severity"`
	Condition   string         `gorm:"size:256;not null" json:"condition"`
	Threshold   float64        `gorm:"default:0" json:"threshold"`
	DurationSec int            `gorm:"default:0" json:"duration_sec"`
	Channels    string         `gorm:"type:jsonb;default:'[]'" json:"channels"`
	Labels      string         `gorm:"type:jsonb;default:'{}'" json:"labels"`
	Status      string         `gorm:"size:16;not null;default:active" json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (AlertRuleModel) TableName() string { return "alert_rules" }

// AlertEventModel persists triggered alert events in PG
type AlertEventModel struct {
	ID         string     `gorm:"type:uuid;primaryKey" json:"id"`
	RuleID     string     `gorm:"type:uuid;index" json:"rule_id"`
	RuleName   string     `gorm:"size:128" json:"rule_name"`
	ClusterID  string     `gorm:"type:uuid" json:"cluster_id,omitempty"`
	Severity   string     `gorm:"size:16;not null" json:"severity"`
	Message    string     `gorm:"type:text" json:"message"`
	Status     string     `gorm:"size:16;not null;default:firing" json:"status"`
	Labels     string     `gorm:"type:jsonb;default:'{}'" json:"labels"`
	FiredAt    time.Time  `gorm:"index" json:"fired_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (AlertEventModel) TableName() string { return "alert_events" }

// ============================================================================
// Scheduler Snapshot Model — Queue crash recovery
// ============================================================================

// SchedulerSnapshotModel persists the scheduler queue/running set for crash recovery.
// Only the latest snapshot is kept (upsert by key="scheduler").
type SchedulerSnapshotModel struct {
	Key       string    `gorm:"type:varchar(64);primaryKey" json:"key"` // always "scheduler"
	Data      string    `gorm:"type:text;not null" json:"data"`        // JSON-encoded QueueSnapshot
	Version   int64     `gorm:"not null;default:0" json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SchedulerSnapshotModel) TableName() string { return "scheduler_snapshots" }
