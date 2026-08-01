package redteam

import (
	"time"
)

// SecurityEvent represents an incoming security alert or log entry
type SecurityEvent struct {
	ID          string                 `json:"event_id"`
	Type        string                 `json:"event_type"`
	Source      string                 `json:"source"`
	SourceIP    string                 `json:"source_ip"`
	TargetHost  string                 `json:"target_host"`
	UserAgent   string                 `json:"user_agent"`
	Timestamp   time.Time              `json:"timestamp"`
	Frequency   EventFrequency         `json:"frequency"`
	KnownThreats ThreatIndicatorSet     `json:"known_threats,omitempty"`
	Metadata    interface{}            `json:"metadata,omitempty"`
	RawPayload  string                 `json:"raw_payload"`
}

// EventFrequency tracks event occurrence patterns
type EventFrequency struct {
	LastHour      int64 `json:"last_hour"`
	Last24Hours   int64 `json:"last_24_hours"`
	Last7Days     int64 `json:"last_7_days"`
}

// ThreatIndicatorSet contains known threat intelligence
type ThreatIndicatorSet struct {
	MaliciousIPs   []string `json:"malicious_ips"`
	MaliciousHashes []string `json:"malicious_hashes"`
	SuspiciousUserAgents []string `json:"suspicious_user_agents"`
}

func (tis *ThreatIndicatorSet) HasIndicator(value string) bool {
	for _, indicator := range tis.MaliciousIPs {
		if indicator == value {
			return true
		}
	}
	for _, hash := range tis.MaliciousHashes {
		if hash == value {
			return true
		}
	}
	return false
}

// ClassifiedIncident contains the result of incident classification
type ClassifiedIncident struct {
	EventID          string             `json:"event_id"`
	IncidentType     IncidentType       `json:"incident_type"`
	Severity         SeverityLevel      `json:"severity"`
	Confidence       float64            `json:"confidence"`
	RecommendedAgent AgentType          `json:"recommended_agent"`
	MatchingRules    []RuleMatch        `json:"matching_rules"`
	AnalysisDuration time.Duration      `json:"analysis_duration"`
	Timestamp        time.Time          `json:"timestamp"`
	RawEvent         SecurityEvent      `json:"raw_event"`
}

// IncidentType represents different types of security incidents
type IncidentType string

const (
	IncidentTypeRansomeware           IncidentType = "ransomware"
	IncidentTypeDataExfiltration      IncidentType = "data_exfiltration"
	IncidentTypePrivilegeEscalation   IncidentType = "privilege_escalation"
	IncidentTypeLateralMovement       IncidentType = "lateral_movement"
	IncidentTypePhishing              IncidentType = "phishing"
	IncidentTypeMalware               IncidentType = "malware"
	IncidentTypeUnauthorizedAccess    IncidentType = "unauthorized_access"
	IncidentTypeDenialOfService       IncidentType = "denial_of_service"
	IncidentTypeInsiderThreat         IncidentType = "insider_threat"
	IncidentTypeAccountCompromise     IncidentType = "account_compromise"
	IncidentTypeUnknown               IncidentType = "unknown"
)

// SeverityLevel represents the severity of an incident
type SeverityLevel int

const (
	SeverityCritical SeverityLevel = iota + 1 // Critical (9.0-10.0)
	SeverityHigh                               // High (7.0-8.9)
	SeverityMedium                             // Medium (4.0-6.9)
	SeverityLow                                // Low (0.0-3.9)
)

func (sl SeverityLevel) String() string {
	switch sl {
	case SeverityCritical:
		return "critical"
	case SeverityHigh:
		return "high"
	case SeverityMedium:
		return "medium"
	case SeverityLow:
		return "low"
	default:
		return "unknown"
	}
}

// RuleMatch represents a matching detection rule
type RuleMatch struct {
	RuleID       string  `json:"rule_id"`
	RuleName     string  `json:"rule_name"`
	Matched      bool    `json:"matched"`
	Confidence   float64 `json:"confidence"`
	IncidentType IncidentType `json:"incident_type"`
	Evidence     string  `json:"evidence"`
}

// DetectionRule defines a pattern for detecting specific threats
type DetectionRule struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Description         string        `json:"description"`
	Conditions          []Condition   `json:"conditions"`
	IncidentType        IncidentType  `json:"incident_type"`
	SeverityOffset      float64       `json:"severity_offset"`
	ConfidenceThreshold float64       `json:"confidence_threshold"`
	Enabled             bool          `json:"enabled"`
	AutoRemediate       bool          `json:"auto_remediate"`
}

// Condition defines a single matching criterion
type Condition struct {
	Field       string   `json:"field"`
	Operator    string   `json:"operator"` // eq, gt, lt, contains, in_list
	Value       string   `json:"value"`
	Negate      bool     `json:"negate,omitempty"`
}

// AgentType specifies different remediation agents
type AgentType string

const (
	AgentTypeGeneric              AgentType = "generic"
	AgentTypeRansomewareResponse  AgentType = "ransomeware_response"
	AgentTypeDataExfiltration     AgentType = "data_exfiltration"
	AgentTypePrivilegeEscalation  AgentType = "privilege_escalation"
	AgentTypeLateralMovement      AgentType = "lateral_movement"
	AgentTypePhishingResponse     AgentType = "phishing_response"
	AgentTypeMalwareRemoval       AgentType = "malware_removal"
	AgentTypeAccessControl        AgentType = "access_control"
	AgentTypeDoSDetection         AgentType = "dos_detection"
	AgentTypeInsiderThreat        AgentType = "insider_threat"
	AgentTypeAccountSecurity      AgentType = "account_security"
)

// MLModelInterface is the interface for machine learning models
type MLModelInterface interface {
	Predict(features map[string]interface{}) Prediction
	Train(trainingData [][]FeatureLabel) error
	GetFeatureImportance() map[string]float64
}

// Prediction holds ML model prediction results
type Prediction struct {
	Label       IncidentType `json:"label"`
	Confidence  float64      `json:"confidence"`
	AllClasses  []ClassScore `json:"all_classes,omitempty"`
}

// ClassScore represents confidence for each possible class
type ClassScore struct {
	Class   string  `json:"class"`
	Score   float64 `json:"score"`
}

// FeatureLabel represents labeled training data
type FeatureLabel struct {
	Features map[string]interface{}
	Label    IncidentType
}
