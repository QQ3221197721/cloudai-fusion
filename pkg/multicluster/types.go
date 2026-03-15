// Package multicluster provides multi-cluster management capabilities
// including cluster federation (Karmada/Clusternet), cross-cluster load
// balancing, disaster recovery (multi-active/hot-standby), and cluster
// lifecycle management.
package multicluster

import (
	"time"
)

// ============================================================================
// Federation Types
// ============================================================================

// FederationType identifies the federation backend.
type FederationType string

const (
	FederationKarmada    FederationType = "karmada"
	FederationClusternet FederationType = "clusternet"
)

// MemberCluster represents a cluster in the federation.
type MemberCluster struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Region          string            `json:"region"`
	Zone            string            `json:"zone,omitempty"`
	Provider        string            `json:"provider"`
	Endpoint        string            `json:"endpoint"`
	Role            ClusterRole       `json:"role"`
	Status          ClusterMemberStatus `json:"status"`
	Capacity        ClusterCapacity   `json:"capacity"`
	Weight          int               `json:"weight"`
	Labels          map[string]string `json:"labels,omitempty"`
	Taints          []ClusterTaint    `json:"taints,omitempty"`
	JoinedAt        time.Time         `json:"joined_at"`
	LastHeartbeatAt time.Time         `json:"last_heartbeat_at"`
}

// ClusterRole defines the role of a member cluster.
type ClusterRole string

const (
	RoleControlPlane ClusterRole = "control-plane"
	RoleMember       ClusterRole = "member"
	RoleEdge         ClusterRole = "edge"
)

// ClusterMemberStatus represents federation membership status.
type ClusterMemberStatus string

const (
	MemberStatusReady      ClusterMemberStatus = "ready"
	MemberStatusNotReady   ClusterMemberStatus = "not-ready"
	MemberStatusJoining    ClusterMemberStatus = "joining"
	MemberStatusLeaving    ClusterMemberStatus = "leaving"
	MemberStatusOffline    ClusterMemberStatus = "offline"
)

// ClusterCapacity represents the resource capacity of a cluster.
type ClusterCapacity struct {
	CPUMillicores     int64 `json:"cpu_millicores"`
	MemoryBytes       int64 `json:"memory_bytes"`
	GPUCount          int   `json:"gpu_count"`
	PodCapacity       int   `json:"pod_capacity"`
	NodeCount         int   `json:"node_count"`
	UsedCPUMillicores int64 `json:"used_cpu_millicores"`
	UsedMemoryBytes   int64 `json:"used_memory_bytes"`
	UsedGPUCount      int   `json:"used_gpu_count"`
}

// ClusterTaint is similar to K8s node taint but for clusters.
type ClusterTaint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// ============================================================================
// Cross-Cluster Load Balancing
// ============================================================================

// LoadBalancingPolicy defines the cross-cluster load balancing strategy.
type LoadBalancingPolicy string

const (
	LBPolicyWeightedRoundRobin LoadBalancingPolicy = "weighted-round-robin"
	LBPolicyLatencyBased       LoadBalancingPolicy = "latency-based"
	LBPolicyGeoBased           LoadBalancingPolicy = "geo-based"
	LBPolicyCostOptimized      LoadBalancingPolicy = "cost-optimized"
	LBPolicyResourceBased      LoadBalancingPolicy = "resource-based"
)

// GlobalService represents a service deployed across multiple clusters.
type GlobalService struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Namespace  string                `json:"namespace"`
	Policy     LoadBalancingPolicy   `json:"policy"`
	Endpoints  []ServiceEndpoint     `json:"endpoints"`
	HealthCheck *ServiceHealthCheck  `json:"health_check,omitempty"`
	StickySession bool               `json:"sticky_session"`
	Labels     map[string]string     `json:"labels,omitempty"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

// ServiceEndpoint represents a service instance in a specific cluster.
type ServiceEndpoint struct {
	ClusterID  string  `json:"cluster_id"`
	Address    string  `json:"address"`
	Port       int     `json:"port"`
	Weight     int     `json:"weight"`
	Healthy    bool    `json:"healthy"`
	LatencyMs  float64 `json:"latency_ms"`
	Region     string  `json:"region"`
}

// ServiceHealthCheck defines health check configuration.
type ServiceHealthCheck struct {
	Protocol          string        `json:"protocol"`
	Path              string        `json:"path"`
	Port              int           `json:"port"`
	Interval          time.Duration `json:"interval"`
	Timeout           time.Duration `json:"timeout"`
	HealthyThreshold  int           `json:"healthy_threshold"`
	UnhealthyThreshold int          `json:"unhealthy_threshold"`
}

// ============================================================================
// Disaster Recovery
// ============================================================================

// DRStrategy defines the disaster recovery strategy.
type DRStrategy string

const (
	DRMultiActive  DRStrategy = "multi-active"
	DRHotStandby   DRStrategy = "hot-standby"
	DRWarmStandby  DRStrategy = "warm-standby"
	DRColdStandby  DRStrategy = "cold-standby"
	DRPilotLight   DRStrategy = "pilot-light"
)

// DRPlan represents a disaster recovery plan.
type DRPlan struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Strategy         DRStrategy        `json:"strategy"`
	PrimaryClusterID string            `json:"primary_cluster_id"`
	StandbyClusters  []StandbyCluster  `json:"standby_clusters"`
	RPO              time.Duration     `json:"rpo"`
	RTO              time.Duration     `json:"rto"`
	FailoverPolicy   *FailoverPolicy   `json:"failover_policy"`
	DataReplication  *DataReplication   `json:"data_replication"`
	Status           DRPlanStatus      `json:"status"`
	LastTestedAt     *time.Time        `json:"last_tested_at,omitempty"`
	LastFailoverAt   *time.Time        `json:"last_failover_at,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
}

// StandbyCluster represents a standby/backup cluster.
type StandbyCluster struct {
	ClusterID  string     `json:"cluster_id"`
	Priority   int        `json:"priority"`
	Role       string     `json:"role"`
	SyncStatus string     `json:"sync_status"`
	SyncLag    time.Duration `json:"sync_lag"`
}

// FailoverPolicy controls automatic vs manual failover.
type FailoverPolicy struct {
	Automatic          bool          `json:"automatic"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	FailureThreshold   int           `json:"failure_threshold"`
	CooldownPeriod     time.Duration `json:"cooldown_period"`
	PreferredOrder     []string      `json:"preferred_order,omitempty"`
}

// DataReplication defines how data is replicated across clusters.
type DataReplication struct {
	Mode              string        `json:"mode"`
	SyncInterval      time.Duration `json:"sync_interval"`
	ConflictResolution string       `json:"conflict_resolution"`
	IncludeNamespaces []string       `json:"include_namespaces,omitempty"`
	ExcludeNamespaces []string       `json:"exclude_namespaces,omitempty"`
}

// DRPlanStatus represents the status of a DR plan.
type DRPlanStatus string

const (
	DRStatusActive    DRPlanStatus = "active"
	DRStatusStandby   DRPlanStatus = "standby"
	DRStatusFailover  DRPlanStatus = "failover"
	DRStatusDegraded  DRPlanStatus = "degraded"
	DRStatusInactive  DRPlanStatus = "inactive"
)

// FailoverEvent records a failover occurrence.
type FailoverEvent struct {
	ID            string    `json:"id"`
	DRPlanID      string    `json:"dr_plan_id"`
	FromClusterID string    `json:"from_cluster_id"`
	ToClusterID   string    `json:"to_cluster_id"`
	Trigger       string    `json:"trigger"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	Duration      time.Duration `json:"duration,omitempty"`
}

// ============================================================================
// Cluster Lifecycle
// ============================================================================

// LifecyclePhase represents the lifecycle phase of a cluster.
type LifecyclePhase string

const (
	PhaseProvisioning   LifecyclePhase = "provisioning"
	PhaseBootstrapping  LifecyclePhase = "bootstrapping"
	PhaseRunning        LifecyclePhase = "running"
	PhaseUpgrading      LifecyclePhase = "upgrading"
	PhaseScaling        LifecyclePhase = "scaling"
	PhaseDraining       LifecyclePhase = "draining"
	PhaseDecommissioning LifecyclePhase = "decommissioning"
	PhaseDeleted        LifecyclePhase = "deleted"
)

// LifecycleEvent records a cluster lifecycle event.
type LifecycleEvent struct {
	ID        string         `json:"id"`
	ClusterID string         `json:"cluster_id"`
	Phase     LifecyclePhase `json:"phase"`
	Message   string         `json:"message"`
	Timestamp time.Time      `json:"timestamp"`
}

// UpgradePolicy defines how cluster upgrades are performed.
type UpgradePolicy struct {
	Strategy         string        `json:"strategy"`
	MaxUnavailable   int           `json:"max_unavailable"`
	DrainTimeout     time.Duration `json:"drain_timeout"`
	PauseOnFailure   bool          `json:"pause_on_failure"`
	AutoRollback     bool          `json:"auto_rollback"`
	MaintenanceWindow *MaintenanceWindow `json:"maintenance_window,omitempty"`
}

// MaintenanceWindow defines when maintenance operations can occur.
type MaintenanceWindow struct {
	DayOfWeek string `json:"day_of_week"`
	StartHour int    `json:"start_hour"`
	Duration  int    `json:"duration_hours"`
	Timezone  string `json:"timezone"`
}
