
// Package attack_graph - types.go defines core data structures for CVE knowledge graph.
package attack_graph

import (
	"time"
)

// VulnerabilityStatus represents the current state of a vulnerability
type VulnerabilityStatus string

const (
	StatusPublished   VulnerabilityStatus = "published"   // Newly disclosed
	StatusExploited   VulnerabilityStatus = "exploited"   // Being actively exploited
	StatusPatchable   VulnerabilityStatus = "patchable"   // Patch available
	StatusMitigated   VulnerabilityStatus = "mitigated"   // Mitigation deployed
	StatusAcceptedRisk VulnerabilityStatus = "accepted_risk"  // Risk accepted
)

// VulnerabilityState tracks lifecycle of a vulnerability in our environment
type VulnerabilityState struct {
	ID              string            `json:"id"`
	VulnID          string            `json:"vuln_id"`
	Status          VulnerabilityStatus `json:"status"`
	DisclosedDate   time.Time         `json:"disclosed_date"`
	FirstDetectedAt time.Time         `json:"first_detected_at"`
	PatchAvailableAt *time.Time        `json:"patch_available_at,omitempty"`
	MitigatedAt     *time.Time        `json:"mitigated_at,omitempty"`
	AffectedAssets  []AssetRef        `json:"affected_assets"`
	RiskScore       float64           `json:"risk_score"`
	Owner           string            `json:"owner"`
}

// AssetRef references an asset that might be vulnerable
type AssetRef struct {
	AssetType string `json:"asset_type"` // host, container, api_endpoint, service_account
	AssetID   string `json:"asset_id"`
	Namespace string `json:"namespace,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`
}

// ThreatIntelligence provides context about active threats
type ThreatIntel struct {
	Source    string    `json:"source"`
	Campaign  string    `json:"campaign,omitempty"`
	APTGroup  string    `json:"apt_group,omitempty"`
	LastSeen  time.Time `json:"last_seen"`
	Tactics   []string  `json:"tactics"`
	Techniques []string `json:"techniques"`
	Indicators []IOC    `json:"indicators"`
}

// IOC represents an Indication of Compromise
type IOC struct {
	Type string `json:"type"` // ip, domain, hash, file_pattern
	Value string `json:"value"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen time.Time `json:"last_seen"`
	Confidence float64 `json:"confidence"`
}

// KillChainNode represents a node in the kill chain graph
type KillChainNode struct {
	NodeID string `json:"node_id"`
	Phase KillChainPhase `json:"phase"`
	VulnIDs []string `json:"vuln_ids"`
	Description string `json:"description"`
	MitreTechniques []string `json:"mitre_techniques"`
	AttackPatterns []string `json:"attack_patterns"`
}

// KillChainEdge represents a directed edge between phases
type KillChainEdge struct {
	Source KillChainPhase `json:"source"`
	Target KillChainPhase `json:"target"`
	Method string `json:"method"` // delivery, exploitation, etc.
}

// AttackVectorAnalysis describes how an attacker can reach this system
type AttackVectorAnalysis struct {
	EntryPoints []EntryPoint `json:"entry_points"`
	BlastRadius float64      `json:"blast_radius"` // probability of lateral movement
	DetectionGap []string     `json:"detection_gaps"`
	RemediationSteps []RemediationAction `json:"remediation_steps"`
}

// EntryPoint represents a potential entry point for attackers
type EntryPoint struct {
	Name string `json:"name"`
	Protocol string `json:"protocol"`
	Port int `json:"port"`
	Service string `json:"service"`
	ExposureLevel string `json:"exposure_level"` // public,internal,limited
	AssociatedCVEs []string `json:"associated_cves"`
}

// RemediationAction describes a specific remediation step
type RemediationAction struct {
	ActionType      string `json:"action_type"`
	Description     string `json:"description"`
	Priority        string `json:"priority"`
	EffortEstimate  string `json:"effort_estimate"`
	TestingRequired bool   `json:"testing_required"`
	Reference       string `json:"reference"`
	ExpectedEffectiveness float64 `json:"expected_effectiveness"` // 0-1 probability
}
