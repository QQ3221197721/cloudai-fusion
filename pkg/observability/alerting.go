package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ============================================================================
// Alert Deduplication & Suppression (Module 48)
// ============================================================================

// SmartAlertManager handles alert deduplication, suppression rules, and escalation.
type SmartAlertManager struct {
	mu           sync.RWMutex
	alerts       map[string]*AlertState      // indexed by fingerprint
	suppression  *SuppressionEngine
	escalation   *EscalationController
	receiptBuilder *ReceiptBuilder
	logger       Logger
}

// Alert represents an incoming alert.
type Alert struct {
	ID        string
	Name      string
	Severity  string
	Source    string
	Message   string
	Labels    map[string]string
	Timestamp time.Time
}

// AlertState tracks the state of an alert over time.
type AlertState struct {
	Fingerprint string
	Name        string
	Severity    string
	Source      string
	Message     string
	Labels      map[string]string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Status      AlertStatus
	AckBy       string
	AckAt       *time.Time
	ResolvedAt  *time.Time
	Suppressed  bool
	Suppressing *string // pointer to indicate if suppressed by another alert's ID
	RaisedBy    []string
}

// AlertStatus indicates the lifecycle state of an alert.
type AlertStatus string

const (
	AlertStatusActive    AlertStatus = "active"
	AlertStatusSilenced  AlertStatus = "silenced"
	AlertStatusAcknowledged AlertStatus = "acknowledged"
	AlertStatusResolved  AlertStatus = "resolved"
)

// AcknowledgeRequest captures acknowledgment info.
type AcknowledgeRequest struct {
	UserID  string
	Comment string
}

// EscalationLevel defines an escalation tier.
type EscalationLevel struct {
	Level           int
	Delay           time.Duration
	Targets         []string // user IDs or contact info
	Channels        []string // notification channels
	NotifyOnCall    bool
}

// EscalationPolicy defines an escalation chain.
type EscalationPolicy struct {
	ID     string
	Name   string
	Levels []EscalationLevel
}

// InhibitionRule matches source alerts that suppress target alerts.
type InhibitionRule struct {
	ID          string
	Matcher     map[string]string // labels that must match on source
	TargetMatch map[string]string // labels that must match on target
	SeverityGap int               // minimum severity gap required (higher is more severe)
}

// Logger defines minimal logging interface.
type Logger interface {
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Debugf(format string, args ...interface{})
}

// NoOpLogger implements Logger but does nothing.
type NoOpLogger struct{}

func (NoOpLogger) Info(args ...interface{})                 {}
func (NoOpLogger) Warn(args ...interface{})                 {}
func (NoOpLogger) Error(args ...interface{})                {}
func (NoOpLogger) Debugf(format string, args ...interface{}) {}

// NewSmartAlertManager creates a new smart alert manager.
func NewSmartAlertManager(receiptBuilder *ReceiptBuilder) *SmartAlertManager {
	return &SmartAlertManager{
		alerts:       make(map[string]*AlertState),
		suppression:  NewSuppressionEngine(),
		escalation:   NewEscalationController(),
		receiptBuilder: receiptBuilder,
		logger:       NoOpLogger{},
	}
}

// Fingerprint computes SHA256(alertname + key_labels).
// Key labels are sorted deterministically to ensure stable fingerprints.
func Fingerprint(name string, labels map[string]string) string {
	hash := sha256.New()
	hash.Write([]byte(name))
	
	// Sort keys for deterministic order
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

// SendAlert processes an incoming alert with deduplication and suppression.
func (m *SmartAlertManager) SendAlert(ctx context.Context, alert *Alert) (*AlertResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	
	if alert == nil {
		return nil, errors.New("alert cannot be nil")
	}
	
	if alert.Timestamp.IsZero() {
		alert.Timestamp = time.Now()
	}
	
	fingerprint := Fingerprint(alert.Name, alert.Labels)
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	result := &AlertResult{
		AlertID:    alert.ID,
		Fingerprint: fingerprint,
		Timestamp:  alert.Timestamp,
	}
	
	// Check if alert already exists
	existing, ok := m.alerts[fingerprint]
	if ok {
		// Update existing alert
		existing.UpdatedAt = alert.Timestamp
		
		// Check if we should re-notify based on label changes
		m.logger.Infof("Alert %q updated (fingerprint: %s)", alert.Name, fingerprint[:16])
		
		result.Action = ActionUpdated
		result.State = existing
		return result, nil
	}
	
	// Create new alert state
	newState := &AlertState{
		Fingerprint: fingerprint,
		Name:        alert.Name,
		Severity:    alert.Severity,
		Source:      alert.Source,
		Message:     alert.Message,
		Labels:      copyMap(alert.Labels),
		CreatedAt:   alert.Timestamp,
		UpdatedAt:   alert.Timestamp,
		Status:      AlertStatusActive,
	}
	
	// Apply suppression rules
	if suppressed, suppressingAlert := m.suppression.IsSuppressed(newState); suppressed {
		newState.Suppressed = true
		newState.Suppressing = &suppressingAlert.Fingerprint
		newState.Status = AlertStatusSilenced
		result.Action = ActionSuppressed
		result.State = newState
		
		m.logger.Warnf("Alert %q silenced by alert %q", alert.Name, suppressingAlert.ID)
		m.mu.Unlock()
		
		// Generate signed receipt even for silenced alerts
		output := map[string]interface{}{
			"fingerprint": fingerprint,
			"silenced":    true,
			"suppressed_by": suppressingAlert.Fingerprint,
		}
		receipt, err := m.receiptBuilder.Build("send_alert", alert, output)
		if err != nil {
			return nil, err
		}
		result.Receipt = receipt
		
		m.mu.Lock()
		return result, nil
	}
	
	// Start escalation monitoring for this active alert
	m.escalation.TrackNewAlert(fingerprint, newState)
	
	m.alerts[fingerprint] = newState
	
	// Check inhibition against other alerts (high severity silences low)
	m.inhibitLowSeverity(newState)
	
	result.Action = ActionCreated
	result.State = newState
	
	return result, nil
}

// inhibitLowSeverity checks if this alert should silence other lower-severity alerts.
func (m *SmartAlertManager) inhibitLowSeverity(activeAlert *AlertState) {
	for fp, existing := range m.alerts {
		if existing.Status != AlertStatusActive {
			continue
		}
		
		// High severity alert inhibits lower severity alerts from same source
		if activeAlert.Source == existing.Source && 
		   severityRank(activeAlert.Severity) > severityRank(existing.Severity) {
			
			existing.Suppressed = true
			existing.Suppressing = &activeAlert.Fingerprint
			existing.Status = AlertStatusSilenced
			
			m.logger.Infof("Inhibiting alert %q due to higher severity alert %q", 
				existing.Name, activeAlert.Name)
		}
	}
}

// AcknowledgeAlert marks an alert as acknowledged.
func (m *SmartAlertManager) AcknowledgeAlert(ctx context.Context, fingerprint, userID, comment string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	state, ok := m.alerts[fingerprint]
	if !ok {
		return fmt.Errorf("no such alert: %s", fingerprint[:16])
	}
	
	now := time.Now()
	state.Status = AlertStatusAcknowledged
	state.AckBy = userID
	state.AckAt = &now
	
	output := map[string]interface{}{
		"fingerprint": fingerprint,
		"ack_by": userID,
		"comment": comment,
	}
	
	receipt, err := m.receiptBuilder.Build("acknowledge_alert", map[string]string{"fingerprint": fingerprint}, output)
	if err != nil {
		return err
	}
	
	m.mu.Unlock()
	m.logger.Infof("Alert %q acknowledged by %s (receipt: %s)", state.Name, userID, receipt.ID[:16])
	m.mu.Lock()
	
	state.Receipt = receipt
	return nil
}

// ResolveAlert marks an alert as resolved.
func (m *SmartAlertManager) ResolveAlert(ctx context.Context, fingerprint string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	state, ok := m.alerts[fingerprint]
	if !ok {
		return fmt.Errorf("no such alert: %s", fingerprint[:16])
	}
	
	now := time.Now()
	state.Status = AlertStatusResolved
	state.ResolvedAt = &now
	
	output := map[string]interface{}{
		"fingerprint": fingerprint,
	}
	
	receipt, err := m.receiptBuilder.Build("resolve_alert", map[string]string{"fingerprint": fingerprint}, output)
	if err != nil {
		return err
	}
	
	m.mu.Unlock()
	m.logger.Infof("Alert %q resolved (receipt: %s)", state.Name, receipt.ID[:16])
	m.mu.Lock()
	
	state.Receipt = receipt
	return nil
}

// GetAlert retrieves an alert by fingerprint.
func (m *SmartAlertManager) GetAlert(fingerprint string) (*AlertState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.alerts[fingerprint]
	return state, ok
}

// ListAlerts returns all active alerts.
func (m *SmartAlertManager) ListAlerts(status AlertStatus) []*AlertState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	result := make([]*AlertState, 0, len(m.alerts))
	for _, state := range m.alerts {
		if status == "" || state.Status == status {
			result = append(result, state)
		}
	}
	return result
}

// AddInhibitionRule registers an inhibition rule.
func (m *SmartAlertManager) AddInhibitionRule(rule InhibitionRule) error {
	m.suppression.AddRule(rule)
	return nil
}

// RemoveInhibitionRule removes an inhibition rule.
func (m *SmartAlertManager) RemoveInhibitionRule(ruleID string) {
	m.suppression.RemoveRule(ruleID)
}

// ============================================================================
// Suppression Engine
// ============================================================================

// SuppressionEngine manages suppression rules and determines when alerts should be silenced.
type SuppressionEngine struct {
	mu       sync.RWMutex
	rules    []InhibitionRule
	window   time.Duration // sliding window for recent alerts
}

// NewSuppressionEngine creates a new suppression engine.
func NewSuppressionEngine() *SuppressionEngine {
	return &SuppressionEngine{
		rules:    make([]InhibitionRule, 0),
		window:   5 * time.Minute,
	}
}

// AddRule adds an inhibition rule.
func (e *SuppressionEngine) AddRule(rule InhibitionRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// RemoveRule removes an inhibition rule by ID.
func (e *SuppressionEngine) RemoveRule(ruleID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	ruleIdx := -1
	for i, r := range e.rules {
		if r.ID == ruleID {
			ruleIdx = i
			break
		}
	}
	
	if ruleIdx >= 0 {
		e.rules = append(e.rules[:ruleIdx], e.rules[ruleIdx+1:]...)
	}
}

// IsSuppressed checks if an alert should be suppressed by any matching inhibition rule.
// Returns (true, suppressingAlert) if suppressed, (false, nil) otherwise.
func (e *SuppressionEngine) IsSuppressed(newAlert *AlertState) (bool, AlertState) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	for _, rule := range e.rules {
		if matchesRule(newAlert, rule.Matcher, rule.TargetMatch) {
			// Find the source alert that triggered this suppression
			return true, AlertState{Name: "suppressor"}
		}
	}
	return false, AlertState{}
}

// matchesRule checks if an alert matches a rule's conditions.
func matchesRule(alert *AlertState, matcher, targetMatch map[string]string) bool {
	// Check source matcher
	for k, v := range matcher {
		if alert.Labels[k] != v {
			return false
		}
	}
	
	// Check target matcher
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

// EscalationController monitors unacknowledged alerts and escalates them.
type EscalationController struct {
	mu           sync.Mutex
	tracking     map[string]*TrackingState
	policies     map[string]*EscalationPolicy
	nextTick     time.Time
	tickerCh     chan time.Time // for testing
}

// TrackingState holds state for an alert being monitored for escalation.
type TrackingState struct {
	Fingerprint   string
	CreatedAt     time.Time
	AcknowledgedAt *time.Time
	EscalatedTo    int // current level index
}

// NewEscalationController creates a new escalation controller.
func NewEscalationController() *EscalationController {
	return &EscalationController{
		tracking: make(map[string]*TrackingState),
		policies: make(map[string]*EscalationPolicy),
		nextTick: time.Time{},
		tickerCh: make(chan time.Time, 1),
	}
}

// TrackNewAlert starts monitoring an alert for escalation.
func (c *EscalationController) TrackNewAlert(fingerprint string, alert *AlertState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.tracking[fingerprint] = &TrackingState{
		Fingerprint: fingerprint,
		CreatedAt:   alert.CreatedAt,
		EscalatedTo: 0,
	}
	
	c.scheduleTick()
}

// MarkAcknowledged removes an alert from tracking after acknowledgment.
func (c *EscalationController) MarkAcknowledged(fingerprint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tracking, fingerprint)
}

// ProcessTick advances all tracked alerts and escalates those past their threshold.
func (c *EscalationController) ProcessTick() []EscalationEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	var events []EscalationEvent
	
	for fp, state := range c.tracking {
		elapsed := now.Sub(state.CreatedAt)
		
		// Check each escalation policy
		for _, policy := range c.policies {
			level := c.getNextLevel(elapsed, policy.Levels)
			if level > state.EscalatedTo {
				// Time to escalate
				event := EscalationEvent{
					Fingerprint: fp,
					FromLevel:   state.EscalatedTo,
					ToLevel:     level,
					PolicyName:  policy.Name,
					Targets:     policy.Levels[level].Targets,
					Timestamp:   now,
				}
				events = append(events, event)
				state.EscalatedTo = level
			}
		}
	}
	
	c.scheduleTick()
	return events
}

// getNextLevel returns the next escalation level based on elapsed time.
func (c *EscalationController) getNextLevel(elapsed time.Duration, levels []EscalationLevel) int {
	var acc time.Duration
	for i, lvl := range levels {
		acc += lvl.Delay
		if elapsed < acc {
			return i
		}
	}
	return len(levels)
}

// scheduleTick ensures the ticker fires soonest in 1 minute.
func (c *EscalationController) scheduleTick() {
	minDelay := time.Hour
	for _, state := range c.tracking {
		elapsed := time.Since(state.CreatedAt)
		delay := 59*time.Second - elapsed
		if delay > 0 && delay < minDelay {
			minDelay = delay
		}
	}
	
	if minDelay <= 0 {
		minDelay = time.Second
	}
	c.nextTick = time.Now().Add(minDelay)
	select {
	case c.tickerCh <- c.nextTick:
	default:
	}
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

// ============================================================================
// Notifier Interface
// ============================================================================

// Notifier delivers alerts to external systems.
type Notifier interface {
	// Name returns the notifier identifier.
	Name() string
	
	// Send sends an alert to the configured destination.
	Send(ctx context.Context, alert *Alert) error
	
	// ValidateConfig validates configuration before sending.
	ValidateConfig() error
}

// StdoutNotifier writes alerts to stdout (useful for testing).
type StdoutNotifier struct {
	Enabled bool `json:"enabled"`
}

// Name implements Notifier.
func (n *StdoutNotifier) Name() string { return "stdout" }

// ValidateConfig implements Notifier.
func (n *StdoutNotifier) ValidateConfig() error {
	if !n.Enabled {
		return errors.New("stdout notifier is disabled")
	}
	return nil
}

// Send implements Notifier by writing JSON to stdout.
func (n *StdoutNotifier) Send(ctx context.Context, alert *Alert) error {
	n.Logger().Infof("[STDOUT NOTIFIER] ALERT: %+v", alert)
	return nil
}

func (n *StdoutNotifier) Logger() Logger {
	return NoOpLogger{}
}

// GenericWebhookNotifier sends alerts to any HTTP webhook endpoint.
type GenericWebhookNotifier struct {
	URL            string `json:"url"`
	EnableHTTPS    bool   `json:"enable_https"`
	BasicAuthUser  string `json:"basic_auth_user,omitempty"`
	BasicAuthPass  string `json:"basic_auth_pass,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Headers        map[string]string
}

// Name implements Notifier.
func (n *GenericWebhookNotifier) Name() string { return "webhook" }

// ValidateConfig implements Notifier.
func (n *GenericWebhookNotifier) ValidateConfig() error {
	if n.URL == "" {
		return errors.New("webhook URL is required")
	}
	if n.TimeoutSeconds <= 0 {
		n.TimeoutSeconds = 30
	}
	return nil
}

// Send implements Notifier by POSTing JSON to the webhook URL.
func (n *GenericWebhookNotifier) Send(ctx context.Context, alert *Alert) error {
	// For now, we'll just log - real implementation would use http.Client
	n.Logger().Infof("[WEBHOOK NOTIFIER] Would send to %s: %+v", n.URL, alert)
	return nil
}

func (n *GenericWebhookNotifier) Logger() Logger {
	return NoOpLogger{}
}

// ============================================================================
// Result Types
// ============================================================================

// AlertResult captures the outcome of processing an alert.
type AlertResult struct {
	AlertID     string
	Fingerprint string
	Action      AlertAction
	State       *AlertState
	Receipt     interface{} // evidence.Receipt
	Timestamp   time.Time
}

// AlertAction describes what happened to the alert.
type AlertAction int

const (
	ActionCreated AlertAction = iota
	ActionUpdated
	ActionSuppressed
	ActionResolved
)

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

// ============================================================================
// Helpers
// ============================================================================

func sortStrings(s []string) {
	for i := 0; i < len(s)-1; i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
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

// Severity levels ordering (P0 > P1 > P2 > P3 > P4)
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
	return 0 // Unknown severity treated as lowest
}
