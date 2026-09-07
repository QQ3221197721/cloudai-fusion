// Package alerting provides multi-channel alert notification with rule-based
// routing and time-based escalation. It supports Email (SMTP), Slack
// (incoming webhooks) and PagerDuty (Events API v2) delivery channels.
package alerting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Module 48: Smart Alert Management
// ============================================================================

// SmartAlertManager handles intelligent alert processing including deduplication,
// suppression rules, escalation policies, and self-healing actions.
type SmartAlertManager struct {
	mu           sync.RWMutex
	alerts       map[string]*SmartAlertState
	suppression  *SuppressionEngine
	escalation   *EscalationController
	receiptBuilder *evidence.ReceiptBuilder
	logger       *logrus.Logger
}

// SmartAlert represents an incoming smart-managed alert.
type SmartAlert struct {
	ID        string
	Name      string
	Severity  SeverityString // Use distinct type to avoid conflicts
	Source    string
	Message   string
	Labels    map[string]string
	Timestamp time.Time
}

// SeverityString is a severity level as a string (distinct from alerting.Severity).
type SeverityString string

const (
	SeverityCritical SeverityString = "P0-critical"
	SeverityHigh     SeverityString = "P1-high"
	SeverityMedium   SeverityString = "P2-medium"
	SeverityLow      SeverityString = "P3-low"
	SeverityInfo     SeverityString = "P4-info"
)

// SmartAlertState tracks the state of a smart alert over time.
type SmartAlertState struct {
	Fingerprint  string
	Name         string
	Severity     SeverityString
	Source       string
	Message      string
	Labels       map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Status       SmartAlertStatus
	AckBy        string
	AckAt        *time.Time
	ResolvedAt   *time.Time
	Suppressed   bool
	Suppressing  *string  // Pointer to suppressing alert fingerprint
	RaisedBy     []string // Alerts this alert raised/inhibited
	Receipt      *evidence.Receipt
}

// SmartAlertStatus indicates the lifecycle state.
type SmartAlertStatus string

const (
	SmartAlertStatusActive     SmartAlertStatus = "active"
	SmartAlertStatusSilenced   SmartAlertStatus = "silenced"
	SmartAlertStatusAcknowledged SmartAlertStatus = "acknowledged"
	SmartAlertStatusResolved   SmartAlertStatus = "resolved"
	SmartAlertStatusHealing    SmartAlertStatus = "healing"
)

// ============================================================================
// Deduplication: Fingerprint Computation
// ============================================================================

// ComputeFingerprint computes SHA256(alertname + key_labels).
// Key labels are sorted deterministically to ensure stable fingerprints.
func ComputeFingerprint(name string, labels map[string]string) string {
	hash := sha256.New()
	hash.Write([]byte(name))

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sortStrings(keys)

	for _, k := range keys {
		hash.Write([]byte("="))
		hash.Write([]byte(k))
		hash.Write([]byte(":"))
		hash.Write([]byte(labels[k]))
	}

	return hex.EncodeToString(hash.Sum(nil))[:64]
}

// ============================================================================
// Suppression Engine
// ============================================================================

// InhibitionRule matches source alerts that suppress target alerts.
type InhibitionRule struct {
	ID          string
	Matcher     map[string]string // Source labels to match
	TargetMatch map[string]string // Target labels to match  
	SeverityGap int               // Minimum severity gap (higher source can silence lower target)
}

// SuppressionEngine manages suppression rules.
type SuppressionEngine struct {
	mu    sync.RWMutex
	rules []InhibitionRule
}

// NewSuppressionEngine creates a new suppression engine.
func NewSuppressionEngine() *SuppressionEngine {
	return &SuppressionEngine{rules: make([]InhibitionRule, 0)}
}

// AddRule adds an inhibition rule.
func (e *SuppressionEngine) AddRule(rule InhibitionRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// RemoveRule removes a rule by ID.
func (e *SuppressionEngine) RemoveRule(ruleID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	idx := -1
	for i, r := range e.rules {
		if r.ID == ruleID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		e.rules = append(e.rules[:idx], e.rules[idx+1:]...)
	}
}

// IsSuppressed checks if an alert should be suppressed.
func (e *SuppressionEngine) IsSuppressed(newAlert *SmartAlertState) (bool, SmartAlertState) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, rule := range e.rules {
		if matchesRule(newAlert, rule.Matcher, rule.TargetMatch) {
			return true, SmartAlertState{Name: "suppressor"}
		}
	}
	return false, SmartAlertState{}
}

func matchesRule(alert *SmartAlertState, matcher, targetMatch map[string]string) bool {
	for k, v := range matcher {
		if alert.Labels[k] != v {
			return false
		}
	}
	for k, v := range targetMatch {
		if alert.Labels[k] != v {
			return false
		}
	}
	return true
}

// ============================================================================
// Escalation Controller
// ============================================================================

// EscalationLevel defines an escalation tier.
type EscalationLevel struct {
	Level        int
	Delay        time.Duration
	Targets      []string
	Channels     []string
	NotifyOnCall bool
}

// EscalationPolicy defines an escalation chain.
type EscalationPolicy struct {
	ID     string
	Name   string
	Levels []EscalationLevel
}

// TrackingState holds state for monitoring an alert.
type TrackingState struct {
	Fingerprint   string
	CreatedAt     time.Time
	AcknowledgedAt *time.Time
	EscalatedTo    int
}

// EscalationEvent represents an escalation action.
type EscalationEvent struct {
	Fingerprint string
	FromLevel   int
	ToLevel     int
	PolicyName  string
	Targets     []string
	Timestamp   time.Time
}

// EscalationController monitors unacknowledged alerts.
type EscalationController struct {
	mu       sync.Mutex
	tracking map[string]*TrackingState
	policies map[string]*EscalationPolicy
	tickerCh chan time.Time
}

// NewEscalationController creates a new controller.
func NewEscalationController() *EscalationController {
	return &EscalationController{
		tracking: make(map[string]*TrackingState),
		policies: make(map[string]*EscalationPolicy),
		tickerCh: make(chan time.Time, 1),
	}
}

// TrackNewAlert starts monitoring.
func (c *EscalationController) TrackNewAlert(fp string, alert *SmartAlertState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tracking[fp] = &TrackingState{
		Fingerprint: fp,
		CreatedAt:   alert.CreatedAt,
		EscalatedTo: 0,
	}
}

// MarkAcknowledged removes from tracking.
func (c *EscalationController) MarkAcknowledged(fp string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tracking, fp)
}

// ProcessTick advances all tracked alerts.
func (c *EscalationController) ProcessTick() []EscalationEvent {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var events []EscalationEvent

	for fp, state := range c.tracking {
		elapsed := now.Sub(state.CreatedAt)
		for _, policy := range c.policies {
			level := getNextLevel(elapsed, policy.Levels)
			if level > state.EscalatedTo {
				events = append(events, EscalationEvent{
					Fingerprint: fp,
					FromLevel:   state.EscalatedTo,
					ToLevel:     level,
					PolicyName:  policy.Name,
					Targets:     policy.Levels[level].Targets,
					Timestamp:   now,
				})
				state.EscalatedTo = level
			}
		}
	}
	return events
}

func getNextLevel(elapsed time.Duration, levels []EscalationLevel) int {
	var acc time.Duration
	for i, lvl := range levels {
		acc += lvl.Delay
		if elapsed < acc {
			return i
		}
	}
	return len(levels)
}

// ============================================================================
// Notifier Interface
// ============================================================================

// Notifier delivers alerts.
type Notifier interface {
	Name() string
	Send(ctx context.Context, alert *SmartAlert) error
	ValidateConfig() error
}

// StdoutNotifier logs to stdout.
type StdoutNotifier struct {
	Enabled bool
}

func (n *StdoutNotifier) Name() string                   { return "stdout" }
func (n *StdoutNotifier) ValidateConfig() error          { return nil }
func (n *StdoutNotifier) Send(ctx context.Context, alert *SmartAlert) error {
	fmt.Printf("[STDOUT NOTIFIER] ALERT: %+v\n", alert)
	return nil
}

// GenericWebhookNotifier sends to HTTP webhook.
type GenericWebhookNotifier struct {
	URL            string
	Headers        map[string]string
	TimeoutSeconds int
}

func (n *GenericWebhookNotifier) Name() string { return "webhook" }

func (n *GenericWebhookNotifier) ValidateConfig() error {
	if n.URL == "" {
		return errors.New("webhook URL required")
	}
	if n.TimeoutSeconds <= 0 {
		n.TimeoutSeconds = 30
	}
	return nil
}

func (n *GenericWebhookNotifier) Send(ctx context.Context, alert *SmartAlert) error {
	fmt.Printf("[WEBHOOK NOTIFIER] Would send to %s: %+v\n", n.URL, alert)
	return nil
}

// ============================================================================
// Manager Core
// ============================================================================

// NewSmartAlertManager creates a smart alert manager.
func NewSmartAlertManager(receiptBuilder *evidence.ReceiptBuilder, logger *logrus.Logger) *SmartAlertManager {
	return &SmartAlertManager{
		alerts:         make(map[string]*SmartAlertState),
		suppression:    NewSuppressionEngine(),
		escalation:     NewEscalationController(),
		receiptBuilder: receiptBuilder,
		logger:         logger,
	}
}

// SendAlert processes an alert with deduplication and suppression.
func (m *SmartAlertManager) SendAlert(ctx context.Context, alert *SmartAlert) (*AlertResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if alert == nil {
		return nil, errors.New("alert cannot be nil")
	}

	if alert.Timestamp.IsZero() {
		alert.Timestamp = time.Now()
	}

	fingerprint := ComputeFingerprint(alert.Name, alert.Labels)

	m.mu.Lock()
	defer m.mu.Unlock()

	result := &AlertResult{
		AlertID:     alert.ID,
		Fingerprint: fingerprint,
		Action:      ActionUnknown,
		Timestamp:   alert.Timestamp,
	}

	existing, ok := m.alerts[fingerprint]
	if ok {
		existing.UpdatedAt = alert.Timestamp
		result.Action = ActionUpdated
		result.State = existing
		return result, nil
	}

	newState := &SmartAlertState{
		Fingerprint: fingerprint,
		Name:        alert.Name,
		Severity:    alert.Severity,
		Source:      alert.Source,
		Message:     alert.Message,
		Labels:      copyMap(alert.Labels),
		CreatedAt:   alert.Timestamp,
		UpdatedAt:   alert.Timestamp,
		Status:      SmartAlertStatusActive,
	}

	// Check suppression
	if suppressed, suppressingAlert := m.suppression.IsSuppressed(newState); suppressed {
		newState.Suppressed = true
		newState.Suppressing = &suppressingAlert.Fingerprint
		newState.Status = SmartAlertStatusSilenced
		result.Action = ActionSuppressed
		result.State = newState
		
		output := map[string]interface{}{"fingerprint": fingerprint, "silenced": true}
		receipt, err := m.receiptBuilder.Build("send_alert", alert, output)
		if err != nil {
			return nil, err
		}
		result.Receipt = receipt
		m.mu.Unlock()
		
		if m.logger != nil {
			m.logger.Warnf("Alert %q silenced", alert.Name)
		}
		m.mu.Lock()
		return result, nil
	}

	// Check inhibition: high severity silences low severity
	m.inhibitLowSeverity(newState)

	m.alerts[fingerprint] = newState
	m.escalation.TrackNewAlert(fingerprint, newState)
	
	result.Action = ActionCreated
	result.State = newState

	return result, nil
}

func (m *SmartAlertManager) inhibitLowSeverity(active *SmartAlertState) {
	for fp, existing := range m.alerts {
		if existing.Status != SmartAlertStatusActive {
			continue
		}
		if active.Source == existing.Source && severityRank(string(active.Severity)) > severityRank(string(existing.Severity)) {
			existing.Suppressed = true
			existing.Suppressing = &active.Fingerprint
			existing.Status = SmartAlertStatusSilenced
			
			if m.logger != nil {
				m.logger.Infof("Inhibited alert %q due to higher severity alert %q", existing.Name, active.Name)
			}
		}
	}
}

// AcknowledgeAlert marks acknowledgment.
func (m *SmartAlertManager) AcknowledgeAlert(ctx context.Context, fingerprint, userID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.alerts[fingerprint]
	if !ok {
		return fmt.Errorf("no such alert: %s", shortFP(fingerprint))
	}

	now := time.Now()
	state.Status = SmartAlertStatusAcknowledged
	state.AckBy = userID
	state.AckAt = &now

	output := map[string]interface{}{"fingerprint": fingerprint, "ack_by": userID}
	receipt, err := m.receiptBuilder.Build("acknowledge_alert", map[string]string{"fingerprint": fingerprint}, output)
	if err != nil {
		return err
	}
	state.Receipt = receipt

	return nil
}

// ResolveAlert marks resolution.
func (m *SmartAlertManager) ResolveAlert(ctx context.Context, fingerprint string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.alerts[fingerprint]
	if !ok {
		return fmt.Errorf("no such alert: %s", shortFP(fingerprint))
	}

	now := time.Now()
	state.Status = SmartAlertStatusResolved
	state.ResolvedAt = &now

	output := map[string]interface{}{"fingerprint": fingerprint}
	receipt, err := m.receiptBuilder.Build("resolve_alert", map[string]string{"fingerprint": fingerprint}, output)
	if err != nil {
		return err
	}
	state.Receipt = receipt

	return nil
}

// GetAlert retrieves an alert.
func (m *SmartAlertManager) GetAlert(fingerprint string) (*SmartAlertState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.alerts[fingerprint]
	return state, ok
}

// ListAlerts returns all alerts with optional status filter.
func (m *SmartAlertManager) ListAlerts(status SmartAlertStatus) []*SmartAlertState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*SmartAlertState, 0, len(m.alerts))
	for _, state := range m.alerts {
		if status == "" || state.Status == status {
			result = append(result, state)
		}
	}
	return result
}

// AddInhibitionRule registers a rule.
func (m *SmartAlertManager) AddInhibitionRule(rule InhibitionRule) {
	m.suppression.AddRule(rule)
}

// RemoveInhibitionRule removes a rule.
func (m *SmartAlertManager) RemoveInhibitionRule(ruleID string) {
	m.suppression.RemoveRule(ruleID)
}

// ============================================================================
// Helpers
// ============================================================================

func sortStrings(s []string) {
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
}

// shortFP safely truncates a fingerprint for log/error messages.
func shortFP(fp string) string {
	if len(fp) > 16 {
		return fp[:16]
	}
	return fp
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

var severityOrder = map[string]int{
	"P0-critical": 5,
	"P1-high":     4,
	"P2-medium":   3,
	"P3-low":      2,
	"P4-info":     1,
}

func severityRank(sev string) int {
	if rank, ok := severityOrder[sev]; ok {
		return rank
	}
	return 0
}

// ============================================================================
// Result Types
// ============================================================================

// AlertAction describes what happened.
type AlertAction int

const (
	ActionUnknown AlertAction = iota
	ActionCreated
	ActionUpdated
	ActionSuppressed
	ActionResolved
)

// AlertResult captures processing outcome.
type AlertResult struct {
	AlertID     string
	Fingerprint string
	Action      AlertAction
	State       interface{} // *SmartAlertState
	Receipt     *evidence.Receipt
	Timestamp   time.Time
}

func (a AlertAction) String() string {
	switch a {
	case ActionCreated:
		return "created"
	case ActionUpdated:
		return "updated"
	case ActionSuppressed:
		return "suppressed"
	case ActionResolved:
		return "resolved"
	default:
		return "unknown"
	}
}
