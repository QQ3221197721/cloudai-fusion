package auto_remediation

import (
	"fmt"
)

// Severity defines vulnerability severity levels
type Severity int

const (
	Low Severity = iota
	Medium
	High
	Critical
)

// String returns severity name
func (s Severity) String() string {
	names := []string{"Low", "Medium", "High", "Critical"}
	if int(s) >= 0 && int(s) < len(names) {
		return names[s]
	}
	return "Unknown"
}

// Score returns CVSS-like numeric score
func (s Severity) Score() float64 {
	scores := map[Severity]float64{
		Low:    3.0,
		Medium: 5.5,
		High:   7.5,
		Critical: 9.5,
	}
	if score, ok := scores[s]; ok {
		return score
	}
	return 0.0
}

// Weight returns multiplier for risk calculation
func (s Severity) Weight() float64 {
	weights := map[Severity]float64{
		Low:    1.0,
		Medium: 2.0,
		High:   3.0,
		Critical: 4.0,
	}
	if weight, ok := weights[s]; ok {
		return weight
	}
	return 1.0
}

// UrgencyLevel defines fix urgency
type UrgencyLevel string

const (
	UrgencyImmediate UrgencyLevel = "immediate" // Within 24 hours
	UrgencySoon      UrgencyLevel = "soon"      // Within 1 week
	UrgencyScheduled UrgencyLevel = "scheduled" // Next patch cycle
	UrgencyMonitor   UrgencyLevel = "monitor"   // No immediate action required
)

// Vulnerability represents a security vulnerability
type Vulnerability struct {
	CVE                string                 `json:"cve"`
	Type              string                 `json:"type"`
	Description       string                 `json:"description"`
	CVSSScore        float64                `json:"cvss_score"`
	CVSSVector       string                 `json:"cvss_vector,omitempty"`
	Component        string                 `json:"component"`
	Version          string                 `json:"version"`
	AffectedOS       []string               `json:"affected_os,omitempty"`
	Exploitability   ExploitabilityStatus   `json:"exploitability"`
	Remediation      RemediationInfo        `json:"remediation,omitempty"`
	MitreMappings    []MitreMapping         `json:"mitre_mappings,omitempty"`
	References       []Reference            `json:"references,omitempty"`
}

// MitreMapping maps vulnerability to MITRE ATT&CK
type MitreMapping struct {
	Tactic      string `json:"tactic"`
	TechniqueID string `json:"technique_id"`
	TechniqueName string `json:"technique_name"`
	Evidence    string `json:"evidence"`
	Confidence  float64 `json:"confidence"`
}

// Reference is a source reference
type Reference struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Source   string `json:"source"`
}

// ExploitabilityStatus indicates how easily exploitable
type ExploitabilityStatus string

const (
	ExploitableInWild     ExploitabilityStatus = "active_exploit"
	PossibleWithEffort    ExploitabilityStatus = "possible"
	TheoreticallyPossible ExploitabilityStatus = "theoretical"
	NotCurrentlyExploited ExploitabilityStatus = "unlikely"
)

// RemediationInfo contains remediation details
type RemediationInfo struct {
	Solution      string   `json:"solution"`
	PatchAvailable bool    `json:"patch_available"`
	VendorURL     string   `json:"vendor_url,omitempty"`
	Workaround    string   `json:"workaround,omitempty"`
}

// RemediationData holds all data for remediation generation
type RemediationData struct {
	Vulnerability
	Findings           []Finding             `json:"findings"`
	ATT&CKTactic      string                `json:"attack_tactic"`
	ATT&CKTechnique   string                `json:"attack_technique,omitempty"`
	BusinessContext   string                `json:"business_context"`
	ComplianceRules   []ComplianceRule      `json:"compliance_rules,omitempty"`
	RemediationHistory []RemediationRecord `json:"remediation_history,omitempty"`
	GeneratePoC       bool                  `json:"generate_poc"`
	RequireMITREMapping bool                `json:"require_mitre_mapping"`
	IncludeExecutiveSummary bool            `json:"include_executive_summary"`
}

// RemediationResponse contains LLM-generated remediation recommendation
type RemediationResponse struct {
	RawResponse        string    `json:"raw_response"`
	GeneratedAt        time.Time `json:"generated_at"`
	SourceModel        string    `json:"source_model"`
	RootCause          string    `json:"root_cause"`
	ImmediateActions  []string  `json:"immediate_actions"`
	LongTermPrevention string   `json:"long_term_prevention"`
	ValidationSteps   []string  `json:"validation_steps"`
	RollbackPlan      string    `json:"rollback_plan"`
	
	// Structured fields (when parsed from JSON response)
	RiskScore         float64              `json:"risk_score,omitempty"`
	EstimatedFixTime  string               `json:"estimated_fix_time,omitempty"`
	RequiredRoles    []string             `json:"required_roles,omitempty"`
}

// MitREMapping contains full MITRE mapping result
type MitREMapping struct {
	Tactics            []MitTEElement `json:"tactics"`
	Techniques        []MitTEchnique `json:"techniques"`
	MitigationSuggestions []MitREMitigation `json:"mitigation_suggestions,omitempty"`
	ThreatIntelligence []ThreatIntel  `json:"threat_intelligence,omitempty"`
}

// MitTEElement represents MITRE tactic/element
type MitTEElement struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Evidence  string `json:"evidence"`
	Priority  int    `json:"priority"`
}

// MitTEchnique represents a technique with sub-techniques
type MitTEchnique struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Subtechniques []MitTEElement      `json:"subtechniques,omitempty"`
	Evidence      string              `json:"evidence"`
	Confidence    float64             `json:"confidence"`
	Tactics       []string            `json:"tactics"`
}

// MitREMitigation is an MITRE mitigation suggestion
type MitREMitigation struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Relevance  string `json:"relevance"` // high/medium/low
	Action     string `json:"action"`
}

// ThreatIntel provides threat intelligence context
type ThreatIntel struct {
	CampaignName   string    `json:"campaign_name"`
	ActorName      string    `json:"actor_name"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	Affinity       string    `json:"affinity"` // High/Medium/Low
	Description    string    `json:"description"`
}

// SummaryData collects information for executive summary
type SummaryData struct {
	VulnCount                  int              `json:"vuln_count"`
	HighCriticalCount          int              `json:"high_critical_count"`
	AssetCount                 int              `json:"asset_count"`
	BusinessRisk               string           `json:"business_risk"`
	KeyFindings               []string         `json:"key_findings"`
	AttackScenarios           []string         `json:"attack_scenarios,omitempty"`
	BusinessImpactStatement   string           `json:"business_impact_statement"`
	RecommendedActions        []string         `json:"recommended_actions"`
	BudgetEstimate            string           `json:"budget_estimate"`
	Timeline                  string           `json:"timeline"`
	TeamResources             string           `json:"team_resources"`
}

// ViolatedRule represents a violated compliance rule
type ViolatedRule struct {
	RuleID      string      `json:"rule_id"`
	RuleName    string      `json:"rule_name"`
	Severity    Severity    `json:"severity"`
	Category    RuleCategory `json:"category"`
	Violation   string      `json:"violation"`
	Evidence    string      `json:"evidence"`
	Suggestion  string      `json:"suggestion"`
}

// ComplianceStatus represents overall compliance status
type ComplianceStatus string

const (
	StatusCompliant ComplianceStatus = "compliant"
	StatusWarning ComplianceStatus = "warning"
	StatusCritical ComplianceStatus = "critical"
)

// ComplianceReport contains compliance check results
type ComplianceReport struct {
	Version       string        `json:"version"`
	RuleCount     int           `json:"rule_count"`
	CompliantRules []string     `json:"compliant_rules"`
	ViolatedRules []ViolatedRule `json:"violated_rules"`
	Decisions    []ComplianceDecision `json:"decisions,omitempty"`
	Summary      ReportSummary `json:"summary"`
	Timestamp    time.Time     `json:"timestamp"`
	Mode        string        `json:"mode"`
}

// ReportSummary provides compliance summary statistics
type ReportSummary struct {
	TotalRules        int             `json:"total_rules"`
	CompliantCount    int             `json:"compliant_count"`
	ViolatedCount     int             `json:"violated_count"`
	ComplianceRate    float64         `json:"compliance_rate"`
	CriticalViolations int            `json:"critical_violations"`
	HighViolations    int             `json:"high_violations"`
	OverallStatus     ComplianceStatus `json:"overall_status"`
	Recommendation    string          `json:"recommendation"`
}