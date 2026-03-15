package observability

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Manager Configuration
// ============================================================================

// ManagerConfig holds observability manager configuration.
type ManagerConfig struct {
	DefaultGroupWait      time.Duration
	DefaultGroupInterval  time.Duration
	DefaultRepeatInterval time.Duration
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		DefaultGroupWait:      30 * time.Second,
		DefaultGroupInterval:  5 * time.Minute,
		DefaultRepeatInterval: 4 * time.Hour,
	}
}

// ============================================================================
// Manager
// ============================================================================

// Manager provides observability operations including alert routing,
// on-call management, runbook automation, and incident retrospectives.
type Manager struct {
	config      ManagerConfig
	routingRules map[string]*RoutingRule
	escalations  map[string]*EscalationPolicy
	alerts       map[string]*ClassifiedAlert
	schedules    map[string]*OnCallSchedule
	runbooks     map[string]*Runbook
	executions   map[string]*RunbookExecution
	incidents    map[string]*Incident
	logger       *logrus.Logger
	mu           sync.RWMutex
}

// NewManager creates a new observability manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		config:       cfg,
		routingRules: make(map[string]*RoutingRule),
		escalations:  make(map[string]*EscalationPolicy),
		alerts:       make(map[string]*ClassifiedAlert),
		schedules:    make(map[string]*OnCallSchedule),
		runbooks:     make(map[string]*Runbook),
		executions:   make(map[string]*RunbookExecution),
		incidents:    make(map[string]*Incident),
		logger:       logrus.StandardLogger(),
	}
}

// ============================================================================
// Alert Classification & Routing
// ============================================================================

// CreateRoutingRule adds a new alert routing rule.
func (m *Manager) CreateRoutingRule(ctx context.Context, rule *RoutingRule) (*RoutingRule, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	rule.ID = common.NewUUID()
	if rule.GroupWait == 0 {
		rule.GroupWait = m.config.DefaultGroupWait
	}
	if rule.GroupInterval == 0 {
		rule.GroupInterval = m.config.DefaultGroupInterval
	}
	if rule.RepeatInterval == 0 {
		rule.RepeatInterval = m.config.DefaultRepeatInterval
	}
	rule.Active = true

	m.routingRules[rule.ID] = rule

	m.logger.WithFields(logrus.Fields{
		"rule_id":  rule.ID,
		"name":     rule.Name,
		"channels": len(rule.Channels),
	}).Info("Routing rule created")

	return rule, nil
}

// ClassifyAndRouteAlert classifies an alert and routes it to the appropriate channels.
func (m *Manager) ClassifyAndRouteAlert(ctx context.Context, alert *ClassifiedAlert) (*ClassifiedAlert, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	alert.ID = common.NewUUID()
	alert.FiredAt = common.NowUTC()
	alert.Status = AlertFiring
	alert.RoutedTo = make([]string, 0)

	// Match against routing rules
	for _, rule := range m.routingRules {
		if !rule.Active {
			continue
		}
		if m.matchesRule(alert, rule) {
			for _, ch := range rule.Channels {
				alert.RoutedTo = append(alert.RoutedTo, fmt.Sprintf("%s:%s", ch.Type, ch.Target))
			}

			m.logger.WithFields(logrus.Fields{
				"alert_id": alert.ID,
				"name":     alert.Name,
				"severity": alert.Severity,
				"rule":     rule.Name,
				"channels": len(rule.Channels),
			}).Info("Alert routed")
		}
	}

	// Route by severity if no rules matched
	if len(alert.RoutedTo) == 0 {
		alert.RoutedTo = m.defaultRouting(alert.Severity)
	}

	// Check if there's a matching runbook
	for _, rb := range m.runbooks {
		if m.runbookMatchesAlert(rb, alert) {
			alert.RunbookURL = fmt.Sprintf("/runbooks/%s", rb.ID)
			if rb.AutoExecute {
				go m.executeRunbookAsync(alert.ID, rb)
			}
			break
		}
	}

	m.alerts[alert.ID] = alert
	return alert, nil
}

func (m *Manager) matchesRule(alert *ClassifiedAlert, rule *RoutingRule) bool {
	// Check severity match
	if len(rule.MatchSeverity) > 0 {
		severityMatch := false
		for _, s := range rule.MatchSeverity {
			if s == alert.Severity {
				severityMatch = true
				break
			}
		}
		if !severityMatch {
			return false
		}
	}

	// Check label match
	for key, val := range rule.MatchLabels {
		alertVal, ok := alert.Labels[key]
		if !ok || alertVal != val {
			return false
		}
	}

	return true
}

func (m *Manager) defaultRouting(severity Severity) []string {
	switch severity {
	case SeverityP0Critical:
		return []string{"pagerduty:oncall", "slack:#incidents-critical", "phone-call:oncall"}
	case SeverityP1High:
		return []string{"pagerduty:oncall", "slack:#incidents"}
	case SeverityP2Medium:
		return []string{"slack:#alerts", "email:ops-team"}
	case SeverityP3Low:
		return []string{"slack:#alerts-low"}
	default:
		return []string{"slack:#alerts-info"}
	}
}

func (m *Manager) runbookMatchesAlert(rb *Runbook, alert *ClassifiedAlert) bool {
	for _, triggerAlert := range rb.TriggerAlerts {
		if triggerAlert == alert.Name {
			return true
		}
	}
	for key, val := range rb.TriggerLabels {
		alertVal, ok := alert.Labels[key]
		if !ok || alertVal != val {
			return false
		}
	}
	return len(rb.TriggerLabels) > 0
}

// AcknowledgeAlert marks an alert as acknowledged.
func (m *Manager) AcknowledgeAlert(ctx context.Context, alertID, userID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	alert, ok := m.alerts[alertID]
	if !ok {
		return fmt.Errorf("alert '%s' not found", alertID)
	}

	now := common.NowUTC()
	alert.Status = AlertAcknowledged
	alert.AcknowledgedBy = userID
	alert.AcknowledgedAt = &now

	m.logger.WithFields(logrus.Fields{
		"alert_id": alertID,
		"user_id":  userID,
	}).Info("Alert acknowledged")

	return nil
}

// ResolveAlert marks an alert as resolved.
func (m *Manager) ResolveAlert(ctx context.Context, alertID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	alert, ok := m.alerts[alertID]
	if !ok {
		return fmt.Errorf("alert '%s' not found", alertID)
	}

	now := common.NowUTC()
	alert.Status = AlertResolved
	alert.ResolvedAt = &now

	return nil
}

// ListAlerts returns alerts, optionally filtered by status.
func (m *Manager) ListAlerts(ctx context.Context, status AlertStatus) ([]*ClassifiedAlert, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	alerts := make([]*ClassifiedAlert, 0, len(m.alerts))
	for _, a := range m.alerts {
		if status == "" || a.Status == status {
			alerts = append(alerts, a)
		}
	}
	return alerts, nil
}

// CreateEscalationPolicy creates an escalation policy.
func (m *Manager) CreateEscalationPolicy(ctx context.Context, policy *EscalationPolicy) (*EscalationPolicy, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	policy.ID = common.NewUUID()
	m.escalations[policy.ID] = policy

	m.logger.WithFields(logrus.Fields{
		"policy_id": policy.ID,
		"name":      policy.Name,
		"levels":    len(policy.Levels),
	}).Info("Escalation policy created")

	return policy, nil
}

// ============================================================================
// On-Call Rotation
// ============================================================================

// CreateOnCallSchedule creates a new on-call schedule.
func (m *Manager) CreateOnCallSchedule(ctx context.Context, schedule *OnCallSchedule) (*OnCallSchedule, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	schedule.ID = common.NewUUID()
	schedule.CreatedAt = common.NowUTC()

	if schedule.Timezone == "" {
		schedule.Timezone = "UTC"
	}
	if schedule.RotationLength == 0 {
		switch schedule.RotationType {
		case RotationDaily:
			schedule.RotationLength = 24 * time.Hour
		case RotationWeekly:
			schedule.RotationLength = 7 * 24 * time.Hour
		default:
			schedule.RotationLength = 7 * 24 * time.Hour
		}
	}

	m.schedules[schedule.ID] = schedule

	m.logger.WithFields(logrus.Fields{
		"schedule_id":  schedule.ID,
		"name":         schedule.Name,
		"team":         schedule.Team,
		"rotation":     schedule.RotationType,
		"participants": len(schedule.Participants),
	}).Info("On-call schedule created")

	return schedule, nil
}

// GetCurrentOnCall returns who is currently on call for a schedule.
func (m *Manager) GetCurrentOnCall(ctx context.Context, scheduleID string) (*OnCallShift, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedule, ok := m.schedules[scheduleID]
	if !ok {
		return nil, fmt.Errorf("schedule '%s' not found", scheduleID)
	}

	if len(schedule.Participants) == 0 {
		return nil, fmt.Errorf("no participants in schedule '%s'", schedule.Name)
	}

	now := common.NowUTC()

	// Check for active overrides first
	for _, override := range schedule.Overrides {
		if now.After(override.StartTime) && now.Before(override.EndTime) {
			for _, p := range schedule.Participants {
				if p.UserID == override.UserID {
					return &OnCallShift{
						ScheduleID: scheduleID,
						Primary:    p,
						StartTime:  override.StartTime,
						EndTime:    override.EndTime,
					}, nil
				}
			}
		}
	}

	// Calculate rotation based on elapsed time
	elapsed := now.Sub(schedule.StartDate)
	rotationIndex := int(elapsed/schedule.RotationLength) % len(schedule.Participants)

	// Sort participants by order
	participants := make([]OnCallMember, len(schedule.Participants))
	copy(participants, schedule.Participants)
	sort.Slice(participants, func(i, j int) bool {
		return participants[i].Order < participants[j].Order
	})

	primary := participants[rotationIndex]
	shift := &OnCallShift{
		ScheduleID: scheduleID,
		Primary:    primary,
		StartTime:  schedule.StartDate.Add(time.Duration(int(elapsed/schedule.RotationLength)) * schedule.RotationLength),
	}
	shift.EndTime = shift.StartTime.Add(schedule.RotationLength)

	// Secondary is next in rotation
	if len(participants) > 1 {
		secondaryIdx := (rotationIndex + 1) % len(participants)
		secondary := participants[secondaryIdx]
		shift.Secondary = &secondary
	}

	return shift, nil
}

// AddScheduleOverride adds a temporary on-call override.
func (m *Manager) AddScheduleOverride(ctx context.Context, scheduleID string, override ScheduleOverride) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	schedule, ok := m.schedules[scheduleID]
	if !ok {
		return fmt.Errorf("schedule '%s' not found", scheduleID)
	}

	override.ID = common.NewUUID()
	schedule.Overrides = append(schedule.Overrides, override)

	m.logger.WithFields(logrus.Fields{
		"schedule_id": scheduleID,
		"user":        override.UserName,
		"start":       override.StartTime,
		"end":         override.EndTime,
	}).Info("On-call override added")

	return nil
}

// ListOnCallSchedules returns all on-call schedules.
func (m *Manager) ListOnCallSchedules(ctx context.Context) ([]*OnCallSchedule, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules := make([]*OnCallSchedule, 0, len(m.schedules))
	for _, s := range m.schedules {
		schedules = append(schedules, s)
	}
	return schedules, nil
}

// ============================================================================
// Runbook Automation
// ============================================================================

// CreateRunbook creates a new automated runbook.
func (m *Manager) CreateRunbook(ctx context.Context, rb *Runbook) (*Runbook, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	rb.ID = common.NewUUID()
	rb.CreatedAt = now
	rb.UpdatedAt = now

	if rb.Timeout == 0 {
		rb.Timeout = 30 * time.Minute
	}

	m.runbooks[rb.ID] = rb

	m.logger.WithFields(logrus.Fields{
		"runbook_id":   rb.ID,
		"name":         rb.Name,
		"steps":        len(rb.Steps),
		"auto_execute": rb.AutoExecute,
	}).Info("Runbook created")

	return rb, nil
}

// ExecuteRunbook manually triggers a runbook execution.
func (m *Manager) ExecuteRunbook(ctx context.Context, runbookID, executedBy string) (*RunbookExecution, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	rb, ok := m.runbooks[runbookID]
	if !ok {
		return nil, fmt.Errorf("runbook '%s' not found", runbookID)
	}

	if rb.RequiresApproval {
		exec := &RunbookExecution{
			ID:          common.NewUUID(),
			RunbookID:   runbookID,
			RunbookName: rb.Name,
			Status:      ExecutionApprovalNeeded,
			StartedAt:   common.NowUTC(),
			ExecutedBy:  executedBy,
			Steps:       make([]StepResult, 0),
		}
		m.executions[exec.ID] = exec
		return exec, nil
	}

	return m.doExecuteRunbook(rb, "", executedBy)
}

func (m *Manager) doExecuteRunbook(rb *Runbook, alertID, executedBy string) (*RunbookExecution, error) {
	now := common.NowUTC()
	exec := &RunbookExecution{
		ID:          common.NewUUID(),
		RunbookID:   rb.ID,
		RunbookName: rb.Name,
		AlertID:     alertID,
		Status:      ExecutionRunning,
		StartedAt:   now,
		ExecutedBy:  executedBy,
		Steps:       make([]StepResult, 0, len(rb.Steps)),
	}

	// Execute each step
	allSuccess := true
	for _, step := range rb.Steps {
		stepResult := StepResult{
			StepOrder: step.Order,
			StepName:  step.Name,
			Status:    "success",
			StartedAt: common.NowUTC(),
			Duration:  time.Millisecond * 100, // simulated
		}

		// In production, this would:
		// - StepTypeCommand: execute shell command
		// - StepTypeKubectl: run kubectl commands
		// - StepTypeWebhook: call external webhook
		// - StepTypeHTTP: make HTTP request
		// - StepTypeScript: execute script
		// - StepTypeManual: wait for manual confirmation

		stepResult.Output = fmt.Sprintf("Step '%s' completed successfully (type: %s)", step.Name, step.Type)
		exec.Steps = append(exec.Steps, stepResult)
	}

	completedAt := common.NowUTC()
	exec.CompletedAt = &completedAt
	if allSuccess {
		exec.Status = ExecutionSuccess
	} else {
		exec.Status = ExecutionFailed
	}

	rb.ExecutionCount++
	rb.LastExecutedAt = &completedAt

	m.executions[exec.ID] = exec

	m.logger.WithFields(logrus.Fields{
		"execution_id": exec.ID,
		"runbook":      rb.Name,
		"status":       exec.Status,
		"steps":        len(exec.Steps),
	}).Info("Runbook execution completed")

	return exec, nil
}

func (m *Manager) executeRunbookAsync(alertID string, rb *Runbook) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.doExecuteRunbook(rb, alertID, "auto")
}

// GetRunbook retrieves a runbook by ID.
func (m *Manager) GetRunbook(ctx context.Context, runbookID string) (*Runbook, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	rb, ok := m.runbooks[runbookID]
	if !ok {
		return nil, fmt.Errorf("runbook '%s' not found", runbookID)
	}
	return rb, nil
}

// ListRunbooks returns all runbooks.
func (m *Manager) ListRunbooks(ctx context.Context) ([]*Runbook, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	runbooks := make([]*Runbook, 0, len(m.runbooks))
	for _, rb := range m.runbooks {
		runbooks = append(runbooks, rb)
	}
	return runbooks, nil
}

// ListExecutions returns runbook executions, optionally filtered by runbook ID.
func (m *Manager) ListExecutions(ctx context.Context, runbookID string) ([]*RunbookExecution, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	executions := make([]*RunbookExecution, 0)
	for _, e := range m.executions {
		if runbookID == "" || e.RunbookID == runbookID {
			executions = append(executions, e)
		}
	}
	return executions, nil
}

// ============================================================================
// Incident Retrospective
// ============================================================================

// CreateIncident creates a new incident.
func (m *Manager) CreateIncident(ctx context.Context, incident *Incident) (*Incident, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	incident.ID = common.NewUUID()
	incident.Status = IncidentOpen
	incident.StartedAt = now
	incident.CreatedAt = now
	incident.Timeline = append(incident.Timeline, TimelineEntry{
		Timestamp:   now,
		Author:      "system",
		Description: fmt.Sprintf("Incident '%s' created (severity: %s)", incident.Title, incident.Severity),
		Type:        "created",
	})

	m.incidents[incident.ID] = incident

	m.logger.WithFields(logrus.Fields{
		"incident_id": incident.ID,
		"title":       incident.Title,
		"severity":    incident.Severity,
		"commander":   incident.Commander,
	}).Warn("Incident created")

	return incident, nil
}

// UpdateIncidentStatus transitions an incident to a new status.
func (m *Manager) UpdateIncidentStatus(ctx context.Context, incidentID string, status IncidentStatus, note string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	incident, ok := m.incidents[incidentID]
	if !ok {
		return fmt.Errorf("incident '%s' not found", incidentID)
	}

	now := common.NowUTC()
	oldStatus := incident.Status
	incident.Status = status

	incident.Timeline = append(incident.Timeline, TimelineEntry{
		Timestamp:   now,
		Author:      "system",
		Description: fmt.Sprintf("Status changed: %s → %s. %s", oldStatus, status, note),
		Type:        "status-change",
	})

	switch status {
	case IncidentMitigated:
		incident.MitigatedAt = &now
	case IncidentResolved:
		incident.ResolvedAt = &now
	}

	m.logger.WithFields(logrus.Fields{
		"incident_id": incidentID,
		"from":        oldStatus,
		"to":          status,
	}).Info("Incident status updated")

	return nil
}

// AddTimelineEntry adds an entry to the incident timeline.
func (m *Manager) AddTimelineEntry(ctx context.Context, incidentID string, entry TimelineEntry) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	incident, ok := m.incidents[incidentID]
	if !ok {
		return fmt.Errorf("incident '%s' not found", incidentID)
	}

	if entry.Timestamp.IsZero() {
		entry.Timestamp = common.NowUTC()
	}

	incident.Timeline = append(incident.Timeline, entry)
	return nil
}

// AddRetrospective attaches a retrospective (post-mortem) to an incident.
func (m *Manager) AddRetrospective(ctx context.Context, incidentID string, retro *Retrospective) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	incident, ok := m.incidents[incidentID]
	if !ok {
		return fmt.Errorf("incident '%s' not found", incidentID)
	}

	// Assign IDs to action items
	for i := range retro.ActionItems {
		if retro.ActionItems[i].ID == "" {
			retro.ActionItems[i].ID = common.NewUUID()
		}
		if retro.ActionItems[i].Status == "" {
			retro.ActionItems[i].Status = "open"
		}
	}

	now := common.NowUTC()
	retro.ReviewDate = &now
	incident.Retrospective = retro
	incident.Status = IncidentPostMortem

	incident.Timeline = append(incident.Timeline, TimelineEntry{
		Timestamp:   now,
		Author:      "system",
		Description: fmt.Sprintf("Retrospective added: %s", retro.Summary),
		Type:        "retrospective",
	})

	m.logger.WithFields(logrus.Fields{
		"incident_id":  incidentID,
		"root_cause":   retro.RootCause,
		"action_items": len(retro.ActionItems),
	}).Info("Retrospective added to incident")

	return nil
}

// SetIncidentImpact sets the impact assessment for an incident.
func (m *Manager) SetIncidentImpact(ctx context.Context, incidentID string, impact *IncidentImpact) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	incident, ok := m.incidents[incidentID]
	if !ok {
		return fmt.Errorf("incident '%s' not found", incidentID)
	}

	incident.Impact = impact
	return nil
}

// GetIncident retrieves an incident by ID.
func (m *Manager) GetIncident(ctx context.Context, incidentID string) (*Incident, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	incident, ok := m.incidents[incidentID]
	if !ok {
		return nil, fmt.Errorf("incident '%s' not found", incidentID)
	}
	return incident, nil
}

// ListIncidents returns all incidents, optionally filtered by status.
func (m *Manager) ListIncidents(ctx context.Context, status IncidentStatus) ([]*Incident, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	incidents := make([]*Incident, 0, len(m.incidents))
	for _, inc := range m.incidents {
		if status == "" || inc.Status == status {
			incidents = append(incidents, inc)
		}
	}
	return incidents, nil
}
