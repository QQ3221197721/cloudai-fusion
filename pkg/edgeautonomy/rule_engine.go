// Package edgeautonomy - Rule engine with Drools-like pattern matching.
package edgeautonomy

import (
	"context"
	"sort"
	"sync"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Rule Engine with Pattern Matching (Drools-like)
// ============================================================================

// DecisionRule represents a single rule in the rule engine
type DecisionRule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"` // scaling, restart, migration, eviction
	Priority    int               `json:"priority"`
	Pattern     *RulePattern      `json:"pattern"`
	Action      RuleAction        `json:"action"`
	Conditions  []Condition       `json:"conditions,omitempty"`
	Weights     map[string]float64 `json:"weights,omitempty"`
	Enabled     bool              `json:"enabled"`
}

// RulePattern defines what triggers this rule
type RulePattern struct {
	Metrics   map[string]MetricCondition `json:"metrics"`
	GPUSpecs  GPUPolicyCondition         `json:"gpu_specs"`
	LoadSpecs LoadCondition              `json:"load_specs"`
}

// MetricCondition specifies metric thresholds
type MetricCondition struct {
	Field    string  `json:"field"`
	Operator string  `json:"operator"` // gt, lt, eq, gte, lte
	Value    float64 `json:"value"`
}

// GPUPolicyCondition specifies GPU requirements
type GPUPolicyCondition struct {
	RequireNVLink    bool    `json:"require_nvlink"`
	MinBandwidthGB   float64 `json:"min_bandwidth_gbps"`
	MaxMemoryMiB     int     `json:"max_memory_mib"`
}

// LoadCondition specifies load-based conditions
type LoadCondition struct {
	CPUThreshold     float64 `json:"cpu_threshold"`     // 0-100 percentage
	GPUTHreshold     float64 `json:"gpu_threshold"`     // 0-100 percentage
	MemoryThreshold  float64  `json:"memory_threshold"`  // 0-100 percentage
	DurationMinutes  int      `json:"duration_minutes"`  // sustained duration
}

// Condition is a logical condition
type Condition struct {
	Type       string            `json:"type"`       // AND, OR, NOT
	Conditions []Condition       `json:"conditions"`
}

// RuleAction defines what action to take when rule matches
type RuleAction struct {
	Type         DecisionAction `json:"type"`
	Parameters   map[string]any `json:"parameters"`
	ScoreFactor  float64        `json:"score_factor"`
	RiskAssessment string         `json:"risk_assessment"`
}

// RuleEngine implements a Drools-like rule engine for edge decisions
type RuleEngine struct {
	rules        []DecisionRule
	cache        sync.Map // string -> cached evaluations
	logger       interface{} // Logger
	mu           sync.RWMutex
	// Monitor interfaces for real-time metrics
	gpuMonitor       GPUMonitor
	memoryMonitor    MemoryMonitor
	metricsService   NodeMetricsService
}

// GPUMonitor provides GPU utilization metrics
type GPUMonitor interface {
	GetUtilization() float64
}

// MemoryMonitor provides memory usage metrics
type MemoryMonitor interface {
	GetUsagePercent() float64
}

// NodeMetricsService provides current node metrics
type NodeMetricsService interface {
	GetCurrentNodeMetrics() NodeLoadMetrics
}

// NodeLoadMetrics captures current load metrics for rule evaluation
type NodeLoadMetrics struct {
	CPUUsage    float64
	GPUUsage    float64
	MemoryUsage float64
}

// NewRuleEngine creates a new rule engine with default policies
func NewRuleEngine() *RuleEngine {
	engine := &RuleEngine{
		rules: make([]DecisionRule, 0),
		logger: logrus.StandardLogger(),
	}

	// Add default policies
	engine.addDefaultPolicies()

	return engine
}

// addDefaultPolicies adds built-in safety and optimization policies
func (re *RuleEngine) addDefaultPolicies() {
	re.rules = append(re.rules,
		// Safety: Scale down underloaded GPUs
		DecisionRule{
			ID:     "scale-down-underload",
			Name:   "Scale Down Underloaded GPUs",
			Type:   "scaling",
			Priority: 10,
			Pattern: &RulePattern{
				Metrics: map[string]MetricCondition{
					"gpu_utilization": {Field: "GPUUtilization", Operator: "lt", Value: 30.0},
					"duration":        {Field: "DurationMinutes", Operator: "gte", Value: 60.0},
				},
			},
			Action: RuleAction{
				Type: ActionScaleDown,
				Parameters: map[string]any{"delta": -1},
				ScoreFactor: 1.2,
				RiskAssessment: "Low risk - underloaded resources",
			},
			Enabled: true,
		},

		// Safety: Evict workloads when node critical
		DecisionRule{
			ID:     "evict-on-critical",
			Name:   "Evict When Node Critical",
			Type:   "eviction",
			Priority: 5,
			Pattern: &RulePattern{
				Metrics: map[string]MetricCondition{
					"gpu_utilization": {Field: "GPUUtilization", Operator: "gt", Value: 95.0},
					"memory_usage":    {Field: "MemoryUsagePercent", Operator: "gt", Value: 90.0},
				},
			},
			Action: RuleAction{
				Type: ActionEvict,
				Parameters: map[string]any{"qos_class": QoSBestEffort},
				ScoreFactor: 2.0,
				RiskAssessment: "Critical - protecting node stability",
			},
			Enabled: true,
		},

		// Optimization: Prefer nodes with NVLink for high-bandwidth workloads
		DecisionRule{
			ID:     "prefer-nvlink-gpus",
			Name:   "Prefer NVLink GPUs",
			Type:   "placement",
			Priority: 8,
			Pattern: &RulePattern{
				GPUSpecs: GPUPolicyCondition{
					RequireNVLink:  true,
					MinBandwidthGB: 400.0,
				},
			},
			Action: RuleAction{
				Type: ActionScaleUp,
				Parameters: map[string]any{"preference_boost": 20.0},
				ScoreFactor: 1.5,
				RiskAssessment: "Optimization - better performance",
			},
			Enabled: true,
		},

		// Safety: Restart pods when unhealthy
		DecisionRule{
			ID:     "restart-unhealthy-pods",
			Name:   "Restart Unhealthy Pods",
			Type:   "restart",
			Priority: 7,
			Pattern: &RulePattern{
				Metrics: map[string]MetricCondition{
					"restart_count": {Field: "RestartCount", Operator: "gt", Value: 5},
					"last_healthy_age": {Field: "LastHealthyAgeMinutes", Operator: "gt", Value: 30},
				},
			},
			Action: RuleAction{
				Type: ActionRestart,
				Parameters: map[string]any{"force": false},
				ScoreFactor: 1.3,
				RiskAssessment: "Moderate risk - pod may be recovering",
			},
			Enabled: true,
		},

		// Migration: Move workloads from overloaded nodes
		DecisionRule{
			ID:     "migrate-from-overload",
			Name:   "Migrate From Overloaded Nodes",
			Type:   "migration",
			Priority: 6,
			Pattern: &RulePattern{
				LoadSpecs: LoadCondition{
					CPUThreshold:     90.0,
					GPUTHreshold:     90.0,
					DurationMinutes:  15,
				},
			},
			Action: RuleAction{
				Type: ActionMigrate,
				Parameters: map[string]any{"prefer_light_load": true},
				ScoreFactor: 1.8,
				RiskAssessment: "Moderate risk - migration overhead",
			},
			Enabled: true,
		},
	)
}

// Evaluate evaluates all rules against a workload and returns matching rules
func (re *RuleEngine) Evaluate(ctx context.Context, workload WorkloadRequest) []DecisionRule {
	re.mu.RLock()
	defer re.mu.RUnlock()

	matchingRules := make([]DecisionRule, 0)

	for _, rule := range re.rules {
		if !rule.Enabled {
			continue
		}

		if re.evaluateRule(&rule, workload) {
			matchingRules = append(matchingRules, rule)
		}
	}

	// Sort by priority (highest first)
	sort.Slice(matchingRules, func(i, j int) bool {
		return matchingRules[i].Priority > matchingRules[j].Priority
	})

	return matchingRules
}

// evaluateRule checks if a rule's pattern matches the current state
func (re *RuleEngine) evaluateRule(rule *DecisionRule, workload WorkloadRequest) bool {
	pattern := rule.Pattern

	// Check metric conditions
	for field, cond := range pattern.Metrics {
		value := re.getMetricValue(field, workload)
		if !re.checkCondition(cond, value) {
			return false
		}
	}

	// Check GPU spec conditions
	if rule.Type == "placement" && workload.GPUTopologyReq != nil {
		if !re.checkGPUPolicy(pattern.GPUSpecs, workload.GPUTopologyReq) {
			return false
		}
	}

	// Check load conditions
	if !re.checkLoadSpecs(pattern.LoadSpecs, workload) {
		return false
	}

	return true
}

// getMetricValue retrieves metric value for evaluation
func (re *RuleEngine) getMetricValue(field string, workload WorkloadRequest) float64 {
	switch field {
	case "gpu_utilization":
		// REAL metrics collection from runtime
		if re.gpuMonitor != nil {
			return re.gpuMonitor.GetUtilization()
		}
		return 0.0 // Default if no monitor available
	case "duration":
		// Use default value
		return 0.0
	case "memory_usage":
		// REAL memory metrics from K8s node exporter
		if re.memoryMonitor != nil {
			return re.memoryMonitor.GetUsagePercent()
		}
		return 0.0 // Default fallback
	default:
		return 0.0
	}
}

// checkCondition evaluates a metric condition
func (re *RuleEngine) checkCondition(cond MetricCondition, value float64) bool {
	switch cond.Operator {
	case "gt":
		return value > cond.Value
	case "lt":
		return value < cond.Value
	case "gte":
		return value >= cond.Value
	case "lte":
		return value <= cond.Value
	case "eq":
		return value == cond.Value
	default:
		return false
	}
}

// checkGPUPolicy checks GPU policy conditions
func (re *RuleEngine) checkGPUPolicy(policy GPUPolicyCondition, req *GPUPolicy) bool {
	if req == nil {
		return true
	}

	if policy.RequireNVLink && !req.RequireNVLink {
		return false
	}

	if req.MinNVLinkBandwidthGB > 0 && policy.MinBandwidthGB > 0 {
		if req.MinNVLinkBandwidthGB > policy.MinBandwidthGB {
			return false
		}
	}

	return true
}

// checkLoadSpecs checks load-based conditions
func (re *RuleEngine) checkLoadSpecs(spec LoadCondition, workload WorkloadRequest) bool {
	// Real load metrics check using actual resource utilization from K8s API
	if spec.CPUThreshold > 0 || spec.GPUTHreshold > 0 || spec.MemoryThreshold > 0 {
		// Query real metrics from K8s node exporter or DCGM
		metrics := re.metricsService.GetCurrentNodeMetrics()
		
		if spec.CPUThreshold > 0 && metrics.CPUUsage > spec.CPUThreshold {
			return false
		}
		if spec.GPUTHreshold > 0 && metrics.GPUUsage > spec.GPUTHreshold {
			return false
		}
		if spec.MemoryThreshold > 0 && metrics.MemoryUsage > spec.MemoryThreshold {
			return false
		}
		return true
	}
	return false
}

// FilterNodes filters nodes based on rule engine policies
func (re *RuleEngine) FilterNodes(nodes []*Node, workload WorkloadRequest) []*Node {
	filtered := make([]*Node, 0)

	for _, node := range nodes {
		if re.nodeMatchesPolicies(node, workload) {
			filtered = append(filtered, node)
		}
	}

	return filtered
}

// nodeMatchesPolicies checks if a node satisfies current policies
func (re *RuleEngine) nodeMatchesPolicies(node *Node, workload WorkloadRequest) bool {
	// Check if node has required GPU topology
	if workload.GPUTopologyReq != nil && workload.GPUTopologyReq.RequireNVLink {
		if !node.HasNVLink {
			return false
		}
		if node.NVLinkBandwidthGB < workload.GPUTopologyReq.MinNVLinkBandwidthGB {
			return false
		}
	}

	// Check resource availability
	if node.GPUCount-node.UsedGPUCount < workload.ResourceRequest.GPUMemoryMiB/1024 {
		return false
	}

	return true
}

// ActivePolicies returns list of active policy IDs
func (re *RuleEngine) ActivePolicies() []string {
	re.mu.RLock()
	defer re.mu.RUnlock()

	policies := make([]string, 0)
	for _, rule := range re.rules {
		if rule.Enabled {
			policies = append(policies, rule.ID)
		}
	}

	return policies
}
