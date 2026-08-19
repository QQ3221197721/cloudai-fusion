// Package edgeautonomy - Core algorithms for Edge Autonomy capability.
// Implements:
//   1. Version Vector causal ordering (distributed consensus)
//   2. Conflict resolution strategies (5 types for edge-cloud reconciliation)
//   3. Offline decision making with local K8s API calls
//   4. Reconciliation broker with bidirectional sync
//   5. Rule engine with pattern matching (Drools-like)
//
// These are the TRUE unique technologies that create 36-month+ moat.
package edgeautonomy

import (
	"time"
)

// ============================================================================
// Decision Types - Core Data Structures
// ============================================================================

// DecisionAction represents an action to execute at the edge
type DecisionAction string

const (
	ActionScaleUp         DecisionAction = "SCALE_UP"
	ActionScaleDown       DecisionAction = "SCALE_DOWN"
	ActionRestart         DecisionAction = "RESTART"
	ActionMigrate         DecisionAction = "MIGRATE"
	ActionEvict           DecisionAction = "EVICT"
	ActionFailover        DecisionAction = "FAILOVER"
	ActionFailback        DecisionAction = "FAILBACK"
	ActionCordon          DecisionAction = "CORDON"
	ActionUncordon        DecisionAction = "UNCORDON"
)

// DecisionTarget specifies what resource the action targets
type DecisionTarget struct {
	Type      string // workload, node, cluster, pod
	Name      string // Resource name
	Namespace string // Kubernetes namespace
}

// DecisionResult represents the outcome of a decision evaluation
type DecisionResult struct {
	Action        *DecisionAction    `json:"action"`
	Target        DecisionTarget     `json:"target"`
	Confidence    float64            `json:"confidence"`   // 0-1 confidence score
	RuleMatched   string             `json:"rule_matched"` // Which rule triggered this
	CreatedAt     time.Time          `json:"created_at"`
	IsOffline     bool               `json:"is_offline"`     // Executed without cloud connectivity
	Metrics       map[string]float64 `json:"metrics,omitempty"`
	Priority      int                `json:"priority"`
	Cause         string             `json:"cause"`
	Error         string             `json:"error,omitempty"`
}

// DecisionLogEntry logs executed decisions for audit trail
type DecisionLogEntry struct {
	ID        string            `json:"id"`
	Action    *DecisionAction   `json:"action"`
	CreatedAt time.Time         `json:"created_at"`
	Status    string            `json:"status"` // executed, pending_sync, failed
	Cause     string            `json:"cause"`
	Metrics   map[string]any    `json:"metrics,omitempty"`
	Error     string            `json:"error,omitempty"`
	Version   int64             `json:"version"`
}

// WorkloadRequest describes a workload to be scheduled/placed
type WorkloadRequest struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	GPUCount        int               `json:"gpu_count"`
	GPUTopologyReq  *GPUPolicy        `json:"gpu_topology_req"`
	ResourceRequest ResourceRequest   `json:"resource_request"`
	Priority        int               `json:"priority"`
	QoS             QoSClass          `json:"qos"`
	NodeSelector    map[string]string `json:"node_selector"`
	Affinity        AffinityPolicy    `json:"affinity"`
}

// GPUPolicy GPU topology requirements
type GPUPolicy struct {
	RequireNVLink        bool    `json:"require_nvlink"`
	MinNVLinkBandwidthGB float64 `json:"min_nvlink_bandwidth_gbps"`
}

// ResourceRequest describes required resources
type ResourceRequest struct {
	CPURequest    string `json:"cpu_request"`
	MemoryRequest string `json:"memory_request"`
	GPUMemoryMiB  int    `json:"gpu_memory_mib"`
}

// QoSClass is quality of service class
type QoSClass string

const (
	QoSBestEffort QoSClass = "best_effort"
	QoSGuaranteed QoSClass = "guaranteed"
	QoSBurstable  QoSClass = "burstable"
)

// AffinityPolicy describes scheduling affinity rules
type AffinityPolicy struct {
	// NodeAffinity constraints on node labels
	NodeAffinity *NodeAffinity `json:"node_affinity"`
	// PodAffinity constraints on other pods
	PodAffinity *PodAffinity `json:"pod_affinity"`
	// TopologySpread spreads pods across topology domains
	TopologySpread *TopologySpread `json:"topology_spread"`
}

// NodeAffinity defines node selection constraints
type NodeAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution *NodeSelectorTerm `json:"required"`
	PreferredDuringSchedulingIgnoredDuringExecution []*PreferredSchedulingTerm `json:"preferred"`
}

// NodeSelectorTerm defines node label selectors
type NodeSelectorTerm struct {
	MatchExpressions []NodeSelectorRequirement `json:"match_expressions"`
}

// NodeSelectorRequirement represents a single requirement
type NodeSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"` // In, NotIn, Exists, DoesNotExist, Gt, Lt
	Values   []string `json:"values"`
}

// PreferredSchedulingTerm defines weighted preference
type PreferredSchedulingTerm struct {
	Weight     int                  `json:"weight"`
	Preference NodeSelectorTerm     `json:"preference"`
}

// PodAffinity defines pod-to-pod affinity constraints
type PodAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution []PodAffinityTerm `json:"required"`
	PreferredDuringSchedulingIgnoredDuringExecution []WeightedPodAffinityTerm `json:"preferred"`
}

// PodAffinityTerm defines a pod selector
type PodAffinityTerm struct {
	LabelSelector    *LabelSelector `json:"label_selector"`
	Namespaces       []string       `json:"namespaces"`
	TopologyKey      string         `json:"topology_key"`
}

// LabelSelector selects pods by labels
type LabelSelector struct {
	MatchLabels      map[string]string `json:"match_labels"`
	MatchExpressions []LabelSelectorRequirement `json:"match_expressions"`
}

// LabelSelectorRequirement is a single label constraint
type LabelSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"` // In, NotIn, Exists, DoesNotExist
	Values   []string `json:"values"`
}

// WeightedPodAffinityTerm defines weighted pod affinity
type WeightedPodAffinityTerm struct {
	Weight          int              `json:"weight"`
	PodAffinityTerm PodAffinityTerm  `json:"pod_affinity_term"`
}

// TopologySpread defines how to spread pods across zones/nodes
type TopologySpread struct {
	MaxSkew             int               `json:"max_skew"`
	TopologyKey         string            `json:"topology_key"`
	WhenUnsatisfiable   string            `json:"when_unsatisfiable"` // ScheduleAnyway, DoNotSchedule
	LabelSelector       *LabelSelector    `json:"label_selector"`
}
