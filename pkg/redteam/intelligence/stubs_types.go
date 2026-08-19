package redteam

import (
	"time"

	"github.com/sirupsen/logrus"
)

// CVEItem represents a CVE entry for intelligence processing
type CVEItem struct {
	ID         string      `json:"id"`
	CVE        CVEData     `json:"cve"`
	Impact     ImpactScore `json:"impact"`
	References []Ref       `json:"references,omitempty"`
}

// CVEData contains core CVE descriptive data
type CVEData struct {
	Description []NVDDescription `json:"descriptions"`
	References  []Ref            `json:"references,omitempty"`
	CVEID       string           `json:"cve_id,omitempty"`
	Summary     string           `json:"summary,omitempty"`
	Published   *time.Time       `json:"published,omitempty"`
}

// NVDDescription represents a description entry from NVD
type NVDDescription struct {
	Value string `json:"value"`
	Lang  string `json:"lang,omitempty"`
}

// ImpactScore represents CVSS impact scoring
type ImpactScore struct {
	BaseScore    float64 `json:"base_score"`
	BaseSeverity string  `json:"base_severity"`
	VectorString string  `json:"vector_string"`
}

// Ref represents a CVE reference link
type Ref struct {
	URL    string `json:"url"`
	Source string `json:"source,omitempty"`
}

// CVSSMetrics contains CVSS scoring metrics
type CVSSMetrics struct {
	BaseScore    float64 `json:"baseScore"`
	VectorString string  `json:"vectorString"`
	Severity     string  `json:"baseSeverity"`
}

// AttackConstraints defines constraints for attack path generation
type AttackConstraints struct {
	MaxSteps          int     `json:"max_steps"`
	MaxDuration       int     `json:"max_duration_minutes"`
	AvoidDetection    bool    `json:"avoid_detection"`
	MinSuccessRate    float64 `json:"min_success_rate"`
	AllowedTactics    []string `json:"allowed_tactics,omitempty"`
	DisallowedTactics []string `json:"disallowed_tactics,omitempty"`
}

// AttackRuleset defines rules governing attack path construction
type AttackRuleset struct {
	Rules           []AttackRule `json:"rules"`
	DefaultPriority int          `json:"default_priority"`
}

// AttackRule defines a single attack rule
type AttackRule struct {
	Name       string `json:"name"`
	Condition  string `json:"condition"`
	Priority   int    `json:"priority"`
}

// NewAttackRuleset creates a default attack ruleset
func NewAttackRuleset() *AttackRuleset {
	return &AttackRuleset{
		Rules:           make([]AttackRule, 0),
		DefaultPriority: 5,
	}
}

// ScoringPolicy controls how attack paths are scored and prioritized
type ScoringPolicy struct {
	PreferShorter         bool `json:"prefer_shorter"`
	PreferVerifiedPoC     bool `json:"prefer_verified_poc"`
	AvoidNoisyVectors     bool `json:"avoid_noisy_vectors"`
	MinimizeDetectionRisk bool `json:"minimize_detection_risk"`
}

// RedTeamEngine is the core orchestration engine for red team operations
type RedTeamEngine struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// RemediationResult holds the outcome of an auto-remediation process
type RemediationResult struct {
	IncidentID    string        `json:"incident_id"`
	Status        RemediationStatus `json:"status"`
	Message       string        `json:"message,omitempty"`
	ActionsTaken  []string      `json:"actions_taken,omitempty"`
	Timestamp     time.Time     `json:"timestamp"`
	Duration      time.Duration `json:"duration,omitempty"`
	Error         string        `json:"error,omitempty"`
}

// RemediationStatus represents the status of remediation
type RemediationStatus string

const (
	StatusSuccess    RemediationStatus = "success"
	StatusFailure    RemediationStatus = "failure"
	StatusInProgress RemediationStatus = "in_progress"
	StatusPendingApproval RemediationStatus = "pending_approval"
)

// fallbackResponse generates a response when the primary processing fails
func (h *AIChatHandler) fallbackResponse(message string, intent ChatIntent, err error) *ChatResponse {
	return &ChatResponse{
		Message:     "I encountered an issue processing your request. Please try again.",
		Suggestions: []string{"Try rephrasing", "Check system status"},
	}
}

// Agent constructor stubs
func newRansomewareResponseAgent(logger *logrus.Logger) interface{} { return nil }
func newDataExfiltrationAgent(logger *logrus.Logger) interface{}   { return nil }
func newPrivilegeEscalationAgent(logger *logrus.Logger) interface{} { return nil }
func newLateralMovementAgent(logger *logrus.Logger) interface{}    { return nil }
func newMalwareRemovalAgent(logger *logrus.Logger) interface{}     { return nil }

// Workflow method stubs for AIChatHandler
func (h *AIChatHandler) buildAttackWorkflow(target string) ([]APIStep, error) {
	return []APIStep{{Type: APIStepLaunchCVEExploit, Description: "Attack workflow", Parameters: map[string]interface{}{"target": target}}}, nil
}
func (h *AIChatHandler) buildReportWorkflow(reportType string) ([]APIStep, error) {
	return []APIStep{{Type: APIStepGenerateReport, Description: "Report workflow", Parameters: map[string]interface{}{"type": reportType}}}, nil
}
func (h *AIChatHandler) buildVulnAnalysisWorkflow(keyword string) ([]APIStep, error) {
	return []APIStep{{Type: APIStepAnalyzeVulnerability, Description: "Vuln analysis", Parameters: map[string]interface{}{"keyword": keyword}}}, nil
}
func (h *AIChatHandler) buildAttackPathWorkflow(desc string) ([]APIStep, error) {
	return []APIStep{{Type: APIStepBuildAttackPath, Description: "Attack path", Parameters: map[string]interface{}{"desc": desc}}}, nil
}
func (h *AIChatHandler) buildRemediationWorkflow(trigger string) ([]APIStep, error) {
	return []APIStep{{Type: APIStepTriggerAutoRemediation, Description: "Remediation", Parameters: map[string]interface{}{"trigger": trigger}}}, nil
}
func (h *AIChatHandler) buildCapabilitiesWorkflow() ([]APIStep, error) {
	return []APIStep{{Type: APIStepKnowledgeBaseQuery, Description: "Capabilities overview", Parameters: nil}}, nil
}

// Condition operator constants
const (
	ConditionOperatorEqual       = "eq"
	ConditionOperatorGreaterThan = "gt"
	ConditionOperatorContains    = "contains"
	ConditionOperatorInList      = "in_list"
)

// getBaseSeverityScore returns base severity score for incident type
func getBaseSeverityScore(incidentType IncidentType) float64 {
	scores := map[IncidentType]float64{
		IncidentTypeRansomeware:         2.0,
		IncidentTypeDataExfiltration:    1.5,
		IncidentTypePrivilegeEscalation: 1.5,
		IncidentTypeLateralMovement:     1.2,
		IncidentTypePhishing:            0.8,
		IncidentTypeMalware:             1.5,
		IncidentTypeUnauthorizedAccess:  1.0,
		IncidentTypeDenialOfService:     1.0,
		IncidentTypeInsiderThreat:       1.8,
		IncidentTypeAccountCompromise:   1.2,
	}
	if score, ok := scores[incidentType]; ok {
		return score
	}
	return 0.5
}

// getFollowingPhases returns phases that can follow a given kill chain phase
func (kcc *KillChainChainer) getFollowingPhases(currentPhase string) []string {
	phaseOrder := map[string][]string{
		"Initial Access":        {"Execution", "Persistence"},
		"Execution":             {"Persistence", "Privilege Escalation", "Defense Evasion"},
		"Persistence":           {"Privilege Escalation", "Defense Evasion"},
		"Privilege Escalation":  {"Defense Evasion", "Credential Access", "Discovery"},
		"Defense Evasion":       {"Credential Access", "Discovery", "Lateral Movement"},
		"Credential Access":     {"Discovery", "Lateral Movement"},
		"Discovery":             {"Lateral Movement", "Collection"},
		"Lateral Movement":      {"Collection", "Command and Control"},
		"Collection":            {"Command and Control", "Exfiltration"},
		"Command and Control":   {"Exfiltration", "Actions on Objectives"},
		"Exfiltration":          {"Actions on Objectives"},
	}
	if phases, ok := phaseOrder[currentPhase]; ok {
		return phases
	}
	return []string{"Execution"}
}
