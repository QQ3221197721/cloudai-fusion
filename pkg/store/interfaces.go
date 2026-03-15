// Package store - Interface definitions for data persistence layer.
// DataStore provides the abstract contract for all CRUD operations,
// enabling mock implementations for testing and alternative backends.
package store

// DataStore defines the complete persistence contract for CloudAI Fusion.
// All subsystems depend on this interface rather than the concrete *Store type,
// enabling mock/stub implementations for unit testing and alternative backends
// (e.g., in-memory, Redis, CockroachDB).
type DataStore interface {
	Ping() error
	Close() error

	// ---- User Management ----
	UserRepository

	// ---- Cluster Management ----
	ClusterRepository

	// ---- Workload Management ----
	WorkloadRepository

	// ---- Security Management ----
	SecurityRepository

	// ---- Monitoring ----
	MonitorRepository

	// ---- Service Mesh ----
	MeshRepository

	// ---- WebAssembly ----
	WasmRepository

	// ---- Edge-Cloud ----
	EdgeRepository
}

// UserRepository defines user CRUD operations.
type UserRepository interface {
	CreateUser(user *User) error
	GetUserByID(id string) (*User, error)
	GetUserByUsername(username string) (*User, error)
	GetUserByEmail(email string) (*User, error)
	ListUsers(offset, limit int) ([]User, int64, error)
	UpdateUser(user *User) error
	DeleteUser(id string) error
}

// ClusterRepository defines cluster CRUD operations.
type ClusterRepository interface {
	CreateCluster(cluster *ClusterModel) error
	GetClusterByID(id string) (*ClusterModel, error)
	ListClusters(offset, limit int) ([]ClusterModel, int64, error)
	UpdateCluster(cluster *ClusterModel) error
	UpdateClusterStatus(id, status string, nodeCount, gpuCount int) error
	DeleteCluster(id string) error
	CountClusters() int64
}

// WorkloadRepository defines workload CRUD operations.
type WorkloadRepository interface {
	CreateWorkload(w *WorkloadModel) error
	GetWorkloadByID(id string) (*WorkloadModel, error)
	ListWorkloads(clusterID, status string, offset, limit int) ([]WorkloadModel, int64, error)
	UpdateWorkload(w *WorkloadModel) error
	UpdateWorkloadStatus(id, fromStatus, toStatus, reason string) error
	DeleteWorkload(id string) error
	ListWorkloadEvents(workloadID string, limit int) ([]WorkloadEvent, error)
}

// SecurityRepository defines security policy and scan CRUD operations.
type SecurityRepository interface {
	CreateSecurityPolicy(p *SecurityPolicyModel) error
	GetSecurityPolicyByID(id string) (*SecurityPolicyModel, error)
	ListSecurityPolicies(offset, limit int) ([]SecurityPolicyModel, int64, error)
	UpdateSecurityPolicy(p *SecurityPolicyModel) error
	DeleteSecurityPolicy(id string) error

	CreateVulnerabilityScan(scan *VulnerabilityScanModel) error
	GetVulnerabilityScanByID(id string) (*VulnerabilityScanModel, error)
	ListVulnerabilityScans(clusterID string, offset, limit int) ([]VulnerabilityScanModel, int64, error)
	UpdateVulnerabilityScan(scan *VulnerabilityScanModel) error

	CreateAuditLog(log *AuditLog) error
	ListAuditLogs(limit int) ([]AuditLog, error)
}

// MonitorRepository defines monitoring alert CRUD operations.
type MonitorRepository interface {
	CreateAlertRule(rule *AlertRuleModel) error
	GetAlertRuleByID(id string) (*AlertRuleModel, error)
	ListAlertRules(offset, limit int) ([]AlertRuleModel, int64, error)
	UpdateAlertRule(rule *AlertRuleModel) error
	DeleteAlertRule(id string) error

	CreateAlertEvent(event *AlertEventModel) error
	ListAlertEvents(limit int) ([]AlertEventModel, error)
	UpdateAlertEventStatus(id, status string) error
}

// MeshRepository defines service mesh policy CRUD operations.
type MeshRepository interface {
	CreateMeshPolicy(p *MeshPolicyModel) error
	GetMeshPolicyByID(id string) (*MeshPolicyModel, error)
	ListMeshPolicies(offset, limit int) ([]MeshPolicyModel, int64, error)
	DeleteMeshPolicy(id string) error
}

// WasmRepository defines WebAssembly module and instance CRUD operations.
type WasmRepository interface {
	CreateWasmModule(m *WasmModuleModel) error
	GetWasmModuleByID(id string) (*WasmModuleModel, error)
	ListWasmModules(offset, limit int) ([]WasmModuleModel, int64, error)
	DeleteWasmModule(id string) error

	CreateWasmInstance(inst *WasmInstanceModel) error
	ListWasmInstances(status string, offset, limit int) ([]WasmInstanceModel, int64, error)
	UpdateWasmInstance(inst *WasmInstanceModel) error
	DeleteWasmInstance(id string) error
}

// EdgeRepository defines edge node CRUD operations.
type EdgeRepository interface {
	CreateEdgeNode(node *EdgeNodeModel) error
	GetEdgeNodeByID(id string) (*EdgeNodeModel, error)
	ListEdgeNodes(tier string, offset, limit int) ([]EdgeNodeModel, int64, error)
	UpdateEdgeNode(node *EdgeNodeModel) error
	UpdateEdgeNodeHeartbeat(id string, status string, powerWatts float64, resourceUsage string) error
	DeleteEdgeNode(id string) error
}

// Compile-time interface satisfaction check.
var _ DataStore = (*Store)(nil)
