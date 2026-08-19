
package redteam

import (
	"time"
)

// VisualizationOptions configures how the attack path should be visualized
type VisualizationOptions struct {
	TargetCVEs      []string        `json:"target_cves"`
	GoalPhases      []string        `json:"goal_phases"`
	MaxDepth        int             `json:"max_depth"`
	Constraints     AttackConstraints `json:"constraints"`
	IncludeMetadata bool            `json:"include_metadata"`
	LayoutType      LayoutType      `json:"layout_type"`
	Filters         FilterOptions   `json:"filters"`
}

// LayoutType defines different layout algorithms
type LayoutType string

const (
	LayoutForceDirected LayoutType = "force_directed"
	LayoutCircular       LayoutType = "circular"
	LayoutHierarchical   LayoutType = "hierarchical"
	LayoutNetwork        LayoutType = "network"
)

// FilterOptions controls what gets displayed in the visualization
type FilterOptions struct {
	ExcludedTypes       []NodeType    `json:"excluded_types,omitempty"`
	MinCentrality       float64       `json:"min_centrality,omitempty"`
	HighlightCriticalNodes bool        `json:"highlight_critical_nodes"`
	CollapseGroups      bool          `json:"collapse_groups"`
	ShowOnlyPaths       []string      `json:"show_only_paths,omitempty"` // Specific path IDs to highlight
}

// VisualAttackData represents the final visualization output
type VisualAttackData struct {
	GraphData       *VisualGraphData `json:"graph_data"`
	PathAnalysis    PathAnalysisResult `json:"path_analysis"`
	LayoutMetrics   LayoutMetrics      `json:"layout_metrics"`
	Timestamp       time.Time        `json:"timestamp"`
}

// PathAnalysisResult contains analysis of the attack path
type PathAnalysisResult struct {
	TotalSteps           int               `json:"total_steps"`
	TotalDuration        time.Duration     `json:"total_duration"`
	DetectionRisk        float64           `json:"detection_risk"`
	ExploitReliability   float64           `json:"exploit_reliability"`
	AverageRiskScore     float64           `json:"average_risk_score,omitempty"`
	LongestPathSegment   time.Duration     `json:"longest_path_segment,omitempty"`
}

// LayoutMetrics provides performance information about layout computation
type LayoutMetrics struct {
	ComputationTimeMs int64  `json:"computation_time_ms"`
	NodeCount        int    `json:"node_count"`
	LinkCount        int    `json:"link_count"`
	LayoutType       string `json:"layout_type"`
}

// KillChainPhase represents a stage in the cyber kill chain
type KillChainPhase struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Order       int      `json:"order"`
}

var AllKillChainPhases = []KillChainPhase{
	{Name: "Reconnaissance", Description: "Information gathering", Order: 1},
	{Name: "Weaponization", Description: "Creating exploitable material", Order: 2},
	{Name: "Delivery", Description: "Transmitting payload to target", Order: 3},
	{Name: "Exploitation", Description: "Executing exploit code", Order: 4},
	{Name: "Installation", Description: "Installing malware on target", Order: 5},
	{Name: "Command and Control", Description: "Establishing C2 channel", Order: 6},
	{Name: "Actions on Objectives", Description: "Achieving attacker goals", Order: 7},
}
