// Package slack - Alert data structures and types
package slack

import "time"

// ============================================================================
// Vulnerability Report Types
// ============================================================================

// VulnerabilityReport represents a complete security vulnerability report
type VulnerabilityReport struct {
	CVE            CVEInfo
	Description    string
	AffectedFile   string
	StartLine      int
	EndLine        int
	FixVersion     string
	Scanner        string
	ScanTime       time.Time
	CVSSScore      float64
	Criticality    SeverityLevel
}

// CVEInfo contains CVE vulnerability details
type CVEInfo struct {
	ID            string
	Summary       string
	References    []string
	PublishedDate time.Time
	ModifiedDate  time.Time
	Metrics       CVSSMetrics
	Status        string // NVD, MITRE, etc.
}

// CVSSMetrics contains CVSS score components
type CVSSMetrics struct {
	Version       string
	BaseScore     float64
	Environmental float64
	Complete      float64
	VectorString  string
}

// ExploitStatus returns human-readable exploit status
func (c CVEInfo) ExploitStatus() string {
	switch c.Status {
	case "NVD":
		return "NVD published"
	case "MITRE":
		return "MITRE analyzed"
	default:
		return "Unknown"
	}
}

// ============================================================================
// Operational Alert Types
// ============================================================================

// OperationalAlert represents infrastructure/operational alerts
type OperationalAlert struct {
	Title        string
	Message      string
	Service      string
	Environment  string
	Metric       string
	CurrentValue string
	Threshold    string
	Warning      bool
	Critical     bool
	Timestamp    time.Time
}

// PerformanceAlert is a specialized operational alert for performance metrics
type PerformanceAlert struct {
	Service      string
	MetricName   string
	CurrentValue string
	Threshold    string
	Region       string
	Zones        []string
	Priority     SeverityLevel
}

// CostAlert tracks unexpected cost increases
type CostAlert struct {
	Project           string
	Currency          string
	CurrentSpend      float64
	ProjectedSpend    float64
	BudgetLimit       float64
	PercentageUsed    float64
	WarningDaysRemaining int
	Tags              map[string]string
}

// ============================================================================
// Integration Configuration Types
// ============================================================================

// SlackChannelConfig defines channel-specific settings
type SlackChannelConfig struct {
	ChannelID             string
	ChannelName           string
	AlertTypes            []string
	NotificationEnabled   bool
	MutePeriod            MuteSchedule
}

// MuteSchedule defines when notifications should be suppressed
type MuteSchedule struct {
	StartTime  string // "02:00"
	EndTime    string // "06:00"
	DaysOfWeek []int  // 1-7, Monday=1
	AllDay     bool
}

// ============================================================================
// SlackAction represents interactive button actions
// ============================================================================

// SlackAction defines a button action that can be included in alerts
type SlackAction struct {
	Label   string
	Value   string
	URL     string
	Confirm *ConfirmationDialog
	Type    ActionType
}

// ActionType defines the type of action
type ActionType string

const (
	ActionTypeButton    ActionType = "button"
	ActionTypeStatic    ActionType = "static"
	ActionTypeSelect    ActionType = "select"
	ActionTypeExternal  ActionType = "external"
)

// ConfirmationDialog provides safety confirmation for destructive actions
type ConfirmationDialog struct {
	Text        string
	Title       string
	OkayText    string
	DenyText    string
	IsDangerous bool
}

// ============================================================================
// Message Templates
// ============================================================================

// TemplateData provides structured data for message templates
type TemplateData struct {
	EventType         string
	Severity          SeverityLevel
	Title             string
	Message           string
	Details           map[string]string
	Context           map[string]any
	Actions           []SlackAction
	Attachments       []any
	Timestamp         time.Time
	Source            string
	ExtraMetadata     map[string]any
}
