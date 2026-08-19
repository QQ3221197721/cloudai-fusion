// Package observability — Module 48 (Smart Alert Management) and Module 49
// (Self-Healing Controller).
//
// Module 48 provides SHA-256 fingerprint based deduplication, inhibition
// (a higher-severity alert suppressing its derived lower-severity alerts),
// time-based escalation to the next on-call level, and a pluggable Notifier
// abstraction (stdout for tests/dev, webhook for real delivery — endpoints are
// always injected by the caller, never hardcoded).
//
// Module 49 provides a small library of healing actions guarded by safety
// gates: a rate limit and a maximum concurrent impact fraction on destructive
// actions, plus idempotent replay protection. Every executed action produces a
// cryptographically signed receipt via the shared pkg/evidence ReceiptBuilder —
// this module never re-implements any cryptography.
package observability

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Severity model (Module 48)
// ============================================================================

// SeverityAI is the alert severity used by the AIOps agent. It is intentionally
// distinct from the package-level observability.Severity type so both can
// coexist.
type SeverityAI string

const (
	// SeverityAILevel0 is the most severe (paging) level.
	SeverityAILevel0 SeverityAI = "critical"
	SeverityAILevel1 SeverityAI = "error"
	SeverityAILevel2 SeverityAI = "medium"
	SeverityAILevel3 SeverityAI = "warning"
	SeverityAILevel4 SeverityAI = "info"
)

// severityOrderAI ranks severities so that higher-severity alerts can inhibit
// lower-severity ones. Higher number == more severe.
var severityOrderAI = map[SeverityAI]int{
	SeverityAILevel0: 5,
	SeverityAILevel1: 4,
	SeverityAILevel2: 3,
	SeverityAILevel3: 2,
	SeverityAILevel4: 1,
}

func severityRankAI(s SeverityAI) int {
	if r, ok := severityOrderAI[s]; ok {
		return r
	}
	return 0
}

// ============================================================================
// Alert model and fingerprinting (Module 48)
// ============================================================================

// SmartAlert is an incoming alert to be processed by the AIOps agent.
type SmartAlert struct {
	ID        string
	Name      string
	Severity  SeverityAI
	Source    string
	Message   string
	Labels    map[string]string
	Timestamp time.Time
}

// SmartAlertStatus is the lifecycle state of a tracked alert.
type SmartAlertStatus string

const (
	SmartAlertStatusActive       SmartAlertStatus = "active"
	SmartAlertStatusSilenced     SmartAlertStatus = "silenced"
	SmartAlertStatusAcknowledged SmartAlertStatus = "acknowledged"
	SmartAlertStatusResolved     SmartAlertStatus = "resolved"
	SmartAlertStatusHealing      SmartAlertStatus = "healing"
)

// SmartAlertState is the server-side, deduplicated state for a fingerprint.
type SmartAlertState struct {
	Fingerprint  string
	Name         string
	Severity     SeverityAI
	Source       string
	Message      string
	Labels       map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Count        int
	Status       SmartAlertStatus
	AckBy        string
	AckAt        *time.Time
	ResolvedAt   *time.Time
	Suppressed   bool
	SuppressedBy string
	Receipt      *evidence.Receipt
	ActionLog    []string
}

// ComputeFingerprint derives a stable SHA-256 fingerprint from the alert name
// and its labels. Label ordering is normalized so identical label sets always
// yield the same fingerprint. Returns 64 hex characters.
func ComputeFingerprint(name string, labels map[string]string) string {
	h := sha256.New()
	h.Write([]byte(name))
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte{0})
		h.Write([]byte(k))
		h.Write([]byte{'='})
		h.Write([]byte(labels[k]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ============================================================================
// Action / result types (Module 48)
// ============================================================================

// AIOPAction is the outcome of processing an alert through SendAlert.
type AIOPAction int

const (
	AIOPActionUnknown AIOPAction = iota
	AIOPActionCreated
	AIOPActionUpdated
	AIOPActionSuppressed
	AIOPActionResolved
)

// String renders the action for logs.
func (a AIOPAction) String() string {
	switch a {
	case AIOPActionCreated:
		return "created"
	case AIOPActionUpdated:
		return "updated"
	case AIOPActionSuppressed:
		return "suppressed"
	case AIOPActionResolved:
		return "resolved"
	default:
		return "unknown"
	}
}

// AIOPResult is returned by SendAlert.
type AIOPResult struct {
	Action      AIOPAction
	Fingerprint string
	// State holds the *SmartAlertState (typed as interface{} to keep the public
	// result decoupled from internal state mutation).
	State   interface{}
	Receipt *evidence.Receipt
}

// ============================================================================
// Inhibition / suppression engine (Module 48)
// ============================================================================

// InhibitRule suppresses a target alert while a higher-severity source alert is
// active. Matcher selects the source (suppressing) alert by label; TargetMatch
// selects the alert to be suppressed. SeverityGap is the minimum required
// severity-rank difference (source - target) for suppression to apply.
type InhibitRule struct {
	ID          string
	Matcher     map[string]string
	TargetMatch map[string]string
	SeverityGap int
}

// SuppressionEngine evaluates inhibition rules. Safe for concurrent use.
type SuppressionEngine struct {
	mu    sync.RWMutex
	rules []InhibitRule
}

// NewSuppressionEngine creates an empty engine.
func NewSuppressionEngine() *SuppressionEngine {
	return &SuppressionEngine{}
}

// AddRule appends an inhibition rule.
func (e *SuppressionEngine) AddRule(r InhibitRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, r)
}

// RemoveRule deletes a rule by ID.
func (e *SuppressionEngine) RemoveRule(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.rules[:0]
	for _, r := range e.rules {
		if r.ID != id {
			out = append(out, r)
		}
	}
	e.rules = out
}

// ShouldSuppress reports whether the target alert should be suppressed given
// the set of currently tracked alerts. It returns the fingerprint of the
// suppressing (source) alert when suppression applies.
func (e *SuppressionEngine) ShouldSuppress(target *SmartAlertState, active map[string]*SmartAlertState) (bool, string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, rule := range e.rules {
		if !labelsMatch(rule.TargetMatch, target.Labels) {
			continue
		}
		for fp, a := range active {
			if fp == target.Fingerprint {
				continue
			}
			if a.Status == SmartAlertStatusResolved || a.Status == SmartAlertStatusSilenced {
				continue
			}
			if !labelsMatch(rule.Matcher, a.Labels) {
				continue
			}
			if severityRankAI(a.Severity)-severityRankAI(target.Severity) >= rule.SeverityGap {
				return true, fp
			}
		}
	}
	return false, ""
}

// ============================================================================
// Escalation controller (Module 48)
// ============================================================================

// AIOScalLevel is one step in an escalation policy.
type AIOScalLevel struct {
	Name     string
	Receiver string
	// After is the delay from the previous level (or from tracking start for the
	// first escalation) before this level fires.
	After time.Duration
}

// AIOScalPolicy is an ordered list of escalation levels.
type AIOScalPolicy struct {
	Levels []AIOScalLevel
}

// MonitoringState tracks an unacknowledged alert for escalation.
type MonitoringState struct {
	Fingerprint  string
	State        *SmartAlertState
	CurrentLevel int
	TrackedAt    time.Time
	LastEscalate time.Time
	Acknowledged bool
}

// EscalationEvent records a single escalation transition.
type EscalationEvent struct {
	Fingerprint string
	FromLevel   int
	ToLevel     int
	Receiver    string
	At          time.Time
}

// EscalationController escalates unacknowledged alerts through policy levels.
// Safe for concurrent use.
type EscalationController struct {
	mu      sync.Mutex
	policy  AIOScalPolicy
	tracked map[string]*MonitoringState
	events  []EscalationEvent
}

// NewEscalationController creates a controller with the given policy.
func NewEscalationController(policy AIOScalPolicy) *EscalationController {
	return &EscalationController{
		policy:  policy,
		tracked: make(map[string]*MonitoringState),
	}
}

// TrackNewAlert starts (or restarts) escalation tracking for a fingerprint.
func (c *EscalationController) TrackNewAlert(fp string, state *SmartAlertState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tracked[fp] = &MonitoringState{
		Fingerprint:  fp,
		State:        state,
		CurrentLevel: 0,
		TrackedAt:    time.Now(),
	}
}

// MarkAcknowledged stops escalation for a fingerprint (ack or resolve).
func (c *EscalationController) MarkAcknowledged(fp string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ms, ok := c.tracked[fp]; ok {
		ms.Acknowledged = true
		delete(c.tracked, fp)
	}
}

// ProcessTick advances escalation for all tracked alerts whose next level is
// due at now, returning the events that fired.
func (c *EscalationController) ProcessTick(now time.Time) []EscalationEvent {
	c.mu.Lock()
	defer c.mu.Unlock()

	var fired []EscalationEvent
	for fp, ms := range c.tracked {
		if ms.Acknowledged {
			continue
		}
		next := ms.CurrentLevel + 1
		if next >= len(c.policy.Levels) {
			continue
		}
		level := c.policy.Levels[next]
		ref := ms.TrackedAt
		if !ms.LastEscalate.IsZero() {
			ref = ms.LastEscalate
		}
		if now.Sub(ref) >= level.After {
			ev := EscalationEvent{
				Fingerprint: fp,
				FromLevel:   ms.CurrentLevel,
				ToLevel:     next,
				Receiver:    level.Receiver,
				At:          now,
			}
			ms.CurrentLevel = next
			ms.LastEscalate = now
			c.events = append(c.events, ev)
			fired = append(fired, ev)
		}
	}
	return fired
}

// Events returns a copy of all recorded escalation events.
func (c *EscalationController) Events() []EscalationEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]EscalationEvent, len(c.events))
	copy(out, c.events)
	return out
}

// ============================================================================
// Notifier abstraction (Module 48)
// ============================================================================

// Notifier delivers alert notifications to a channel. Concrete channels never
// hardcode endpoints or secrets — those are injected by the caller.
type Notifier interface {
	Name() string
	Notify(ctx context.Context, state *SmartAlertState) error
}

// StdoutNotifier writes a human-readable line per alert. Useful for tests/dev.
type StdoutNotifier struct {
	Out io.Writer
}

// NewStdoutNotifier returns a notifier writing to os.Stdout.
func NewStdoutNotifier() *StdoutNotifier {
	return &StdoutNotifier{Out: os.Stdout}
}

// Name implements Notifier.
func (n *StdoutNotifier) Name() string { return "stdout" }

// Notify implements Notifier.
func (n *StdoutNotifier) Notify(_ context.Context, state *SmartAlertState) error {
	w := n.Out
	if w == nil {
		w = os.Stdout
	}
	_, err := fmt.Fprintf(w, "[ALERT] name=%s severity=%s status=%s fp=%s msg=%q\n",
		state.Name, state.Severity, state.Status, shortFP(state.Fingerprint), state.Message)
	return err
}

// WebhookNotifier POSTs a JSON payload to a caller-provided URL. The URL and
// any credentials are injected by the caller; nothing is hardcoded here.
type WebhookNotifier struct {
	URL    string
	Client *http.Client
}

// NewWebhookNotifier builds a webhook notifier for the given URL.
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{
		URL:    url,
		Client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Notifier.
func (n *WebhookNotifier) Name() string { return "webhook" }

// Notify implements Notifier.
func (n *WebhookNotifier) Notify(ctx context.Context, state *SmartAlertState) error {
	if n.URL == "" {
		return errors.New("webhook notifier: empty URL")
	}
	payload, err := json.Marshal(map[string]interface{}{
		"fingerprint": state.Fingerprint,
		"name":        state.Name,
		"severity":    string(state.Severity),
		"status":      string(state.Status),
		"message":     state.Message,
		"labels":      state.Labels,
	})
	if err != nil {
		return err
	}
	client := n.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook notifier: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ============================================================================
// Self-healing controller (Module 49)
// ============================================================================

// HealingActionType identifies a remediation action.
type HealingActionType string

const (
	HealActionRestartPod HealingActionType = "restart_pod"
	HealActionDrainNode  HealingActionType = "drain_node"
	HealActionFailover   HealingActionType = "failover"
	HealActionScaleOut   HealingActionType = "scale_out"
)

// RateLimitConfig bounds how often a destructive action may run.
type RateLimitConfig struct {
	MaxPerWindow int
	Window       time.Duration
}

// HealingAction describes a remediation action and its safety envelope.
type HealingAction struct {
	Type          HealingActionType
	Description   string
	Preconditions map[string]interface{}
	RateLimit     RateLimitConfig
	// MaxImpactFrac is the maximum fraction of the cluster that may be
	// concurrently affected by this (destructive) action, e.g. 0.10 == 10%.
	MaxImpactFrac float64
	Timeout       time.Duration
	Destructive   bool
}

// ConcurrentImpact tracks how many nodes are currently affected by in-flight
// destructive actions.
type ConcurrentImpact struct {
	ActiveNodes int
	LastUpdate  time.Time
}

// InventoryItem is a known resource the healer may act on.
type InventoryItem struct {
	ID     string
	Kind   string
	Status string
}

// ActionOutcome is the result of an executeWithGates call.
type ActionOutcome struct {
	ActionID string
	Result   string
	Error    error
	Receipt  *evidence.Receipt
}

// SelfHealer executes healing actions behind safety gates: idempotency, rate
// limiting, and a maximum concurrent impact fraction. Safe for concurrent use.
type SelfHealer struct {
	mu              sync.Mutex
	actions         map[string]*HealingAction
	inventory       map[string]*InventoryItem
	clusterSize     int
	impactTracker   ConcurrentImpact
	rateWindows     map[string]map[int64]int // actionType -> windowIndex -> count
	idempotentCache map[string]string        // inputHash -> receiptID
	receiptBuilder  *evidence.ReceiptBuilder
	logger          *logrus.Logger
}

// NewSelfHealer creates a healer signing receipts with the given key. The
// cluster size defaults to 100 and can be overridden with SetClusterSize.
func NewSelfHealer(privKey ed25519.PrivateKey) *SelfHealer {
	return &SelfHealer{
		actions:         make(map[string]*HealingAction),
		inventory:       make(map[string]*InventoryItem),
		clusterSize:     100,
		rateWindows:     make(map[string]map[int64]int),
		idempotentCache: make(map[string]string),
		receiptBuilder:  evidence.NewReceiptBuilder("aiops-selfheal", privKey),
		logger:          logrus.New(),
	}
}

// SetClusterSize sets the cluster node count used by the impact gate.
func (h *SelfHealer) SetClusterSize(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n > 0 {
		h.clusterSize = n
	}
}

// RegisterAction registers (or replaces) a healing action.
func (h *SelfHealer) RegisterAction(action HealingAction) {
	h.mu.Lock()
	defer h.mu.Unlock()
	a := action
	h.actions[string(action.Type)] = &a
}

// RegisterInventory records a resource the healer can act on.
func (h *SelfHealer) RegisterInventory(item InventoryItem) {
	h.mu.Lock()
	defer h.mu.Unlock()
	it := item
	h.inventory[item.ID] = &it
}

// executeWithGates runs an action through the safety gates in order:
//  1. idempotency — an identical (action, targets) request short-circuits;
//  2. rate limit — destructive actions capped per time window;
//  3. impact gate — destructive actions capped at MaxImpactFrac of the cluster.
//
// Gate checks are evaluated before any state mutation so a failed gate never
// consumes rate budget or impact capacity. On success it emits a signed receipt
// via the shared evidence.ReceiptBuilder and caches it for idempotent replay.
func (h *SelfHealer) executeWithGates(actionType HealingActionType, targets []string) (*ActionOutcome, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	action, ok := h.actions[string(actionType)]
	if !ok {
		return nil, fmt.Errorf("unknown healing action: %s", actionType)
	}

	// Gate 1: idempotency.
	inputHash := hashAction(actionType, targets)
	if rid, seen := h.idempotentCache[inputHash]; seen {
		return &ActionOutcome{
			ActionID: rid,
			Result:   "idempotent_skip",
		}, nil
	}

	requested := len(targets)
	now := time.Now()

	// Gate 2: rate limit (check only, commit later).
	rateGated := action.Destructive && action.RateLimit.Window > 0 && action.RateLimit.MaxPerWindow > 0
	var windowIdx int64
	var windows map[int64]int
	if rateGated {
		windowIdx = now.UnixNano() / int64(action.RateLimit.Window)
		windows = h.rateWindows[string(actionType)]
		if windows == nil {
			windows = make(map[int64]int)
			h.rateWindows[string(actionType)] = windows
		}
		if windows[windowIdx] >= action.RateLimit.MaxPerWindow {
			return nil, fmt.Errorf("rate limit exceeded for %s: max %d per %s",
				actionType, action.RateLimit.MaxPerWindow, action.RateLimit.Window)
		}
	}

	// Gate 3: concurrent impact fraction (check only, commit later).
	impactGated := action.Destructive && action.MaxImpactFrac > 0
	if impactGated {
		maxAllowed := int(float64(h.clusterSize) * action.MaxImpactFrac)
		if maxAllowed < 1 {
			maxAllowed = 1
		}
		if h.impactTracker.ActiveNodes+requested > maxAllowed {
			return nil, fmt.Errorf("impact limit reached for %s: %d active + %d requested exceeds max %d (%.0f%% of %d nodes)",
				actionType, h.impactTracker.ActiveNodes, requested, maxAllowed, action.MaxImpactFrac*100, h.clusterSize)
		}
	}

	// All gates passed — commit gate state.
	if rateGated {
		windows[windowIdx]++
	}
	if impactGated {
		h.impactTracker.ActiveNodes += requested
		h.impactTracker.LastUpdate = now
	}

	// Emit a signed receipt via the shared evidence builder (no crypto here).
	input := map[string]interface{}{
		"action":  string(actionType),
		"targets": targets,
	}
	output := map[string]interface{}{
		"result":       "executed",
		"active_nodes": h.impactTracker.ActiveNodes,
	}
	receipt, err := h.receiptBuilder.Build("heal:"+string(actionType), input, output)
	if err != nil {
		return nil, fmt.Errorf("build healing receipt: %w", err)
	}

	// Cache for idempotent replay protection.
	h.idempotentCache[inputHash] = receipt.ID

	return &ActionOutcome{
		ActionID: receipt.ID,
		Result:   "executed",
		Receipt:  receipt,
	}, nil
}

// ReleaseImpact returns capacity to the impact gate after a destructive action
// completes (or is rolled back). The active count never drops below zero.
func (h *SelfHealer) ReleaseImpact(count int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.impactTracker.ActiveNodes -= count
	if h.impactTracker.ActiveNodes < 0 {
		h.impactTracker.ActiveNodes = 0
	}
	h.impactTracker.LastUpdate = time.Now()
}

// hashAction produces a stable hash of an action and its (order-independent)
// target set, used as the idempotency key.
func hashAction(t HealingActionType, targets []string) string {
	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(string(t) + "|" + strings.Join(sorted, ",")))
	return hex.EncodeToString(sum[:])
}

// ============================================================================
// AIOPS agent (Module 48 orchestration + Module 49 wiring)
// ============================================================================

// AIOPSAgent ties together deduplication, suppression, escalation, notification
// and self-healing. Safe for concurrent use.
type AIOPSAgent struct {
	mu             sync.RWMutex
	alerts         map[string]*SmartAlertState
	engine         *SuppressionEngine
	escalator      *EscalationController
	healer         *SelfHealer
	notifiers      []Notifier
	receiptBuilder *evidence.ReceiptBuilder
	logger         *logrus.Logger
}

// NewAIOPSAgent creates an agent signing receipts with privKey. A default
// three-level escalation policy is installed; callers may register notifiers.
func NewAIOPSAgent(privKey ed25519.PrivateKey, logger *logrus.Logger) *AIOPSAgent {
	if logger == nil {
		logger = logrus.New()
	}
	defaultPolicy := AIOScalPolicy{
		Levels: []AIOScalLevel{
			{Name: "primary", Receiver: "oncall-primary", After: 0},
			{Name: "secondary", Receiver: "oncall-secondary", After: 5 * time.Minute},
			{Name: "manager", Receiver: "incident-manager", After: 15 * time.Minute},
		},
	}
	return &AIOPSAgent{
		alerts:         make(map[string]*SmartAlertState),
		engine:         NewSuppressionEngine(),
		escalator:      NewEscalationController(defaultPolicy),
		healer:         NewSelfHealer(privKey),
		receiptBuilder: evidence.NewReceiptBuilder("aiops", privKey),
		logger:         logger,
	}
}

// RegisterNotifier adds a notification channel.
func (a *AIOPSAgent) RegisterNotifier(n Notifier) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notifiers = append(a.notifiers, n)
}

// SendAlert processes an incoming alert: dedup by fingerprint, evaluate
// suppression, and (for new active alerts) start escalation tracking and
// notify. Returns the action taken and the resulting state.
func (a *AIOPSAgent) SendAlert(ctx context.Context, alert *SmartAlert) (*AIOPResult, error) {
	if alert == nil {
		return nil, errors.New("nil alert")
	}
	fp := ComputeFingerprint(alert.Name, alert.Labels)

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()

	// Deduplication: a known fingerprint updates the existing state.
	if existing, ok := a.alerts[fp]; ok {
		existing.Count++
		existing.UpdatedAt = now
		existing.Message = alert.Message
		if existing.Status == SmartAlertStatusResolved {
			existing.Status = SmartAlertStatusActive
			existing.ResolvedAt = nil
		}
		addActionLog(existing, "updated (deduplicated)")
		return &AIOPResult{
			Action:      AIOPActionUpdated,
			Fingerprint: fp,
			State:       existing,
		}, nil
	}

	state := &SmartAlertState{
		Fingerprint: fp,
		Name:        alert.Name,
		Severity:    alert.Severity,
		Source:      alert.Source,
		Message:     alert.Message,
		Labels:      copyLabels(alert.Labels),
		CreatedAt:   now,
		UpdatedAt:   now,
		Count:       1,
		Status:      SmartAlertStatusActive,
	}

	// Suppression: a higher-severity active alert inhibits this one.
	if suppress, byFP := a.engine.ShouldSuppress(state, a.alerts); suppress {
		state.Status = SmartAlertStatusSilenced
		state.Suppressed = true
		state.SuppressedBy = byFP
		addActionLog(state, "suppressed by "+shortFP(byFP))
		a.alerts[fp] = state
		return &AIOPResult{
			Action:      AIOPActionSuppressed,
			Fingerprint: fp,
			State:       state,
		}, nil
	}

	// Sign a receipt attesting to the alert creation.
	receipt, err := a.receiptBuilder.Build("alert:create",
		map[string]interface{}{
			"fingerprint": fp,
			"name":        alert.Name,
			"severity":    string(alert.Severity),
		},
		map[string]interface{}{
			"status": string(state.Status),
		})
	if err == nil {
		state.Receipt = receipt
	} else {
		a.logger.WithError(err).Warn("failed to build alert receipt")
	}

	a.alerts[fp] = state
	a.escalator.TrackNewAlert(fp, state)
	addActionLog(state, "created")
	a.dispatch(ctx, state)

	return &AIOPResult{
		Action:      AIOPActionCreated,
		Fingerprint: fp,
		State:       state,
		Receipt:     receipt,
	}, nil
}

// AcknowledgeAlert marks an alert acknowledged and stops its escalation.
func (a *AIOPSAgent) AcknowledgeAlert(_ context.Context, fp, user string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.alerts[fp]
	if !ok {
		return fmt.Errorf("alert not found: %s", shortFP(fp))
	}
	now := time.Now()
	state.Status = SmartAlertStatusAcknowledged
	state.AckBy = user
	state.AckAt = &now
	state.UpdatedAt = now
	addActionLog(state, "acknowledged by "+user)
	a.escalator.MarkAcknowledged(fp)
	return nil
}

// ResolveAlert marks an alert resolved and stops its escalation.
func (a *AIOPSAgent) ResolveAlert(_ context.Context, fp string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.alerts[fp]
	if !ok {
		return fmt.Errorf("alert not found: %s", shortFP(fp))
	}
	now := time.Now()
	state.Status = SmartAlertStatusResolved
	state.ResolvedAt = &now
	state.UpdatedAt = now
	addActionLog(state, "resolved")
	a.escalator.MarkAcknowledged(fp)
	return nil
}

// GetAlert returns the tracked state for a fingerprint.
func (a *AIOPSAgent) GetAlert(fp string) (*SmartAlertState, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s, ok := a.alerts[fp]
	return s, ok
}

// ListAlerts returns a snapshot of all tracked alerts.
func (a *AIOPSAgent) ListAlerts() []*SmartAlertState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*SmartAlertState, 0, len(a.alerts))
	for _, s := range a.alerts {
		out = append(out, s)
	}
	return out
}

// AddInhibitionRule installs a suppression rule.
func (a *AIOPSAgent) AddInhibitionRule(r InhibitRule) { a.engine.AddRule(r) }

// RemoveInhibitionRule removes a suppression rule by ID.
func (a *AIOPSAgent) RemoveInhibitionRule(id string) { a.engine.RemoveRule(id) }

// TriggerHealingAction runs a healing action through the safety gates.
func (a *AIOPSAgent) TriggerHealingAction(actionType HealingActionType, targets []string) (*ActionOutcome, error) {
	return a.healer.executeWithGates(actionType, targets)
}

// Healer exposes the underlying self-healing controller for configuration.
func (a *AIOPSAgent) Healer() *SelfHealer { return a.healer }

// ProcessEscalations advances escalation timers, returning fired events.
func (a *AIOPSAgent) ProcessEscalations(now time.Time) []EscalationEvent {
	return a.escalator.ProcessTick(now)
}

// dispatch delivers an alert to all registered notifiers (best effort). The
// caller already holds a.mu, so this does not re-lock.
func (a *AIOPSAgent) dispatch(ctx context.Context, state *SmartAlertState) {
	for _, n := range a.notifiers {
		if err := n.Notify(ctx, state); err != nil {
			a.logger.WithError(err).Warnf("notifier %s failed", n.Name())
		}
	}
}

// ============================================================================
// Helpers
// ============================================================================

// labelsMatch reports whether every key/value in matcher is present in labels.
func labelsMatch(matcher, labels map[string]string) bool {
	for k, v := range matcher {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// copyLabels returns a shallow copy of a label map.
func copyLabels(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// shortFP truncates a fingerprint for log readability.
func shortFP(fp string) string {
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
}

// addActionLog appends a timestamped entry to an alert's action log.
func addActionLog(s *SmartAlertState, msg string) {
	s.ActionLog = append(s.ActionLog, fmt.Sprintf("%s %s", time.Now().Format(time.RFC3339), msg))
}
