// Package observability provides operational observability capabilities
// including alert classification and routing, on-call rotation management,
// runbook automation, and incident retrospective (post-mortem) workflows.
package observability

import (
	"time"
)

// ============================================================================
// Alert Classification & Routing
// ============================================================================

// Severity defines alert severity levels with associated response times.
type Severity string

const (
	SeverityP0Critical  Severity = "P0-critical"
	SeverityP1High      Severity = "P1-high"
	SeverityP2Medium    Severity = "P2-medium"
	SeverityP3Low       Severity = "P3-low"
	SeverityP4Info      Severity = "P4-info"
)

// AlertChannel defines notification delivery channels.
type AlertChannel string

const (
	ChannelPagerDuty    AlertChannel = "pagerduty"
	ChannelSlack        AlertChannel = "slack"
	ChannelEmail        AlertChannel = "email"
	ChannelWebhook      AlertChannel = "webhook"
	ChannelDingTalk     AlertChannel = "dingtalk"
	ChannelWeCom        AlertChannel = "wecom"
	ChannelSMS          AlertChannel = "sms"
	ChannelPhoneCall    AlertChannel = "phone-call"
)

// RoutingRule defines how alerts are routed based on labels and severity.
type RoutingRule struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	MatchLabels    map[string]string `json:"match_labels"`
	MatchSeverity  []Severity        `json:"match_severity,omitempty"`
	Channels       []ChannelConfig   `json:"channels"`
	EscalationPolicy string          `json:"escalation_policy,omitempty"`
	MuteWindows    []MuteWindow      `json:"mute_windows,omitempty"`
	GroupBy        []string          `json:"group_by,omitempty"`
	GroupWait      time.Duration     `json:"group_wait"`
	GroupInterval  time.Duration     `json:"group_interval"`
	RepeatInterval time.Duration     `json:"repeat_interval"`
	Active         bool              `json:"active"`
}

// ChannelConfig configures a notification channel.
type ChannelConfig struct {
	Type     AlertChannel      `json:"type"`
	Target   string            `json:"target"`
	Template string            `json:"template,omitempty"`
	Config   map[string]string `json:"config,omitempty"`
}

// MuteWindow defines a period during which alerts are suppressed.
type MuteWindow struct {
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	DaysOfWeek []int `json:"days_of_week,omitempty"`
	Timezone  string `json:"timezone"`
}

// EscalationPolicy defines an escalation chain for unacknowledged alerts.
type EscalationPolicy struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Levels []EscalationLevel `json:"levels"`
}

// EscalationLevel represents a single escalation tier.
type EscalationLevel struct {
	Level        int           `json:"level"`
	Delay        time.Duration `json:"delay"`
	Targets      []string      `json:"targets"`
	Channels     []AlertChannel `json:"channels"`
	NotifyOnCall bool          `json:"notify_on_call"`
}

// ClassifiedAlert represents an alert with routing metadata.
type ClassifiedAlert struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Severity        Severity          `json:"severity"`
	Source          string            `json:"source"`
	Labels          map[string]string `json:"labels"`
	Summary         string            `json:"summary"`
	Description     string            `json:"description,omitempty"`
	RoutedTo        []string          `json:"routed_to"`
	AcknowledgedBy  string            `json:"acknowledged_by,omitempty"`
	AcknowledgedAt  *time.Time        `json:"acknowledged_at,omitempty"`
	ResolvedAt      *time.Time        `json:"resolved_at,omitempty"`
	FiredAt         time.Time         `json:"fired_at"`
	Status          AlertStatus       `json:"status"`
	RunbookURL      string            `json:"runbook_url,omitempty"`
	IncidentID      string            `json:"incident_id,omitempty"`
}

// AlertStatus represents the status of an alert.
type AlertStatus string

const (
	AlertFiring       AlertStatus = "firing"
	AlertAcknowledged AlertStatus = "acknowledged"
	AlertResolved     AlertStatus = "resolved"
	AlertSilenced     AlertStatus = "silenced"
)

// ============================================================================
// On-Call Rotation
// ============================================================================

// OnCallSchedule defines an on-call rotation schedule.
type OnCallSchedule struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Team           string           `json:"team"`
	Timezone       string           `json:"timezone"`
	RotationType   RotationType     `json:"rotation_type"`
	RotationLength time.Duration    `json:"rotation_length"`
	Participants   []OnCallMember   `json:"participants"`
	Overrides      []ScheduleOverride `json:"overrides,omitempty"`
	EscalationID   string           `json:"escalation_id,omitempty"`
	StartDate      time.Time        `json:"start_date"`
	CreatedAt      time.Time        `json:"created_at"`
}

// RotationType defines the rotation pattern.
type RotationType string

const (
	RotationDaily  RotationType = "daily"
	RotationWeekly RotationType = "weekly"
	RotationCustom RotationType = "custom"
)

// OnCallMember represents a participant in the on-call rotation.
type OnCallMember struct {
	UserID    string            `json:"user_id"`
	Name      string            `json:"name"`
	Email     string            `json:"email"`
	Phone     string            `json:"phone,omitempty"`
	SlackID   string            `json:"slack_id,omitempty"`
	Order     int               `json:"order"`
	Channels  []AlertChannel    `json:"preferred_channels,omitempty"`
}

// ScheduleOverride allows temporary on-call assignments.
type ScheduleOverride struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Reason    string    `json:"reason,omitempty"`
}

// OnCallShift represents the current on-call shift.
type OnCallShift struct {
	ScheduleID string    `json:"schedule_id"`
	Primary    OnCallMember `json:"primary"`
	Secondary  *OnCallMember `json:"secondary,omitempty"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
}

// ============================================================================
// Runbook Automation
// ============================================================================

// Runbook defines an automated runbook for incident response.
type Runbook struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	TriggerAlerts   []string          `json:"trigger_alerts,omitempty"`
	TriggerLabels   map[string]string `json:"trigger_labels,omitempty"`
	Steps           []RunbookStep     `json:"steps"`
	Timeout         time.Duration     `json:"timeout"`
	AutoExecute     bool              `json:"auto_execute"`
	RequiresApproval bool            `json:"requires_approval"`
	Owner           string            `json:"owner"`
	LastExecutedAt  *time.Time        `json:"last_executed_at,omitempty"`
	ExecutionCount  int               `json:"execution_count"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// RunbookStep defines a single step in a runbook.
type RunbookStep struct {
	Order       int               `json:"order"`
	Name        string            `json:"name"`
	Type        StepType          `json:"type"`
	Command     string            `json:"command,omitempty"`
	Script      string            `json:"script,omitempty"`
	Webhook     string            `json:"webhook,omitempty"`
	Timeout     time.Duration     `json:"timeout"`
	OnFailure   string            `json:"on_failure"`
	Params      map[string]string `json:"params,omitempty"`
}

// StepType defines the type of runbook step.
type StepType string

const (
	StepTypeCommand    StepType = "command"
	StepTypeScript     StepType = "script"
	StepTypeWebhook    StepType = "webhook"
	StepTypeKubectl    StepType = "kubectl"
	StepTypeHTTP       StepType = "http"
	StepTypeManual     StepType = "manual"
	StepTypeCondition  StepType = "condition"
)

// RunbookExecution records the execution of a runbook.
type RunbookExecution struct {
	ID          string            `json:"id"`
	RunbookID   string            `json:"runbook_id"`
	RunbookName string            `json:"runbook_name"`
	AlertID     string            `json:"alert_id,omitempty"`
	IncidentID  string            `json:"incident_id,omitempty"`
	Status      ExecutionStatus   `json:"status"`
	Steps       []StepResult      `json:"steps"`
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	ExecutedBy  string            `json:"executed_by"`
	Output      string            `json:"output,omitempty"`
}

// ExecutionStatus represents the status of a runbook execution.
type ExecutionStatus string

const (
	ExecutionPending   ExecutionStatus = "pending"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionSuccess   ExecutionStatus = "success"
	ExecutionFailed    ExecutionStatus = "failed"
	ExecutionCancelled ExecutionStatus = "cancelled"
	ExecutionApprovalNeeded ExecutionStatus = "approval-needed"
)

// StepResult records the outcome of a runbook step.
type StepResult struct {
	StepOrder   int           `json:"step_order"`
	StepName    string        `json:"step_name"`
	Status      string        `json:"status"`
	Output      string        `json:"output,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
	StartedAt   time.Time     `json:"started_at"`
}

// ============================================================================
// Incident Retrospective (Post-Mortem)
// ============================================================================

// Incident represents a tracked incident.
type Incident struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Severity        Severity          `json:"severity"`
	Status          IncidentStatus    `json:"status"`
	Commander       string            `json:"commander"`
	Responders      []string          `json:"responders"`
	Alerts          []string          `json:"alert_ids"`
	Timeline        []TimelineEntry   `json:"timeline"`
	Impact          *IncidentImpact   `json:"impact,omitempty"`
	Retrospective   *Retrospective    `json:"retrospective,omitempty"`
	StartedAt       time.Time         `json:"started_at"`
	MitigatedAt     *time.Time        `json:"mitigated_at,omitempty"`
	ResolvedAt      *time.Time        `json:"resolved_at,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
}

// IncidentStatus represents the lifecycle of an incident.
type IncidentStatus string

const (
	IncidentOpen        IncidentStatus = "open"
	IncidentInvestigating IncidentStatus = "investigating"
	IncidentMitigated   IncidentStatus = "mitigated"
	IncidentResolved    IncidentStatus = "resolved"
	IncidentPostMortem  IncidentStatus = "post-mortem"
	IncidentClosed      IncidentStatus = "closed"
)

// TimelineEntry records events during an incident.
type TimelineEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Author      string    `json:"author"`
	Description string    `json:"description"`
	Type        string    `json:"type"`
}

// IncidentImpact captures the measurable impact of an incident.
type IncidentImpact struct {
	Duration         time.Duration `json:"duration"`
	AffectedServices []string      `json:"affected_services"`
	AffectedClusters []string      `json:"affected_clusters"`
	AffectedUsers    int           `json:"affected_users"`
	SLOImpact        string        `json:"slo_impact,omitempty"`
	ErrorBudgetBurn  float64       `json:"error_budget_burn_pct"`
	EstimatedCostUSD float64       `json:"estimated_cost_usd"`
}

// Retrospective captures the post-incident review.
type Retrospective struct {
	Summary         string        `json:"summary"`
	RootCause       string        `json:"root_cause"`
	Contributing    []string      `json:"contributing_factors"`
	WhatWentWell    []string      `json:"what_went_well"`
	WhatWentWrong   []string      `json:"what_went_wrong"`
	ActionItems     []ActionItem  `json:"action_items"`
	LessonsLearned  []string      `json:"lessons_learned"`
	BlamelessNotes  string        `json:"blameless_notes,omitempty"`
	ReviewedBy      []string      `json:"reviewed_by"`
	ReviewDate      *time.Time    `json:"review_date,omitempty"`
}

// ActionItem tracks follow-up tasks from a retrospective.
type ActionItem struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Owner       string     `json:"owner"`
	Priority    string     `json:"priority"`
	DueDate     *time.Time `json:"due_date,omitempty"`
	Status      string     `json:"status"`
	JiraTicket  string     `json:"jira_ticket,omitempty"`
}
