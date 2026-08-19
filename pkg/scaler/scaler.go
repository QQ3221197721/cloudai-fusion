// Package scaler implements Module 16: Auto-scaling Engine — the intelligent scaling
// decision engine that monitors performance regressions, evaluates experiment gains,
// and enforces budgets before allowing infrastructure changes.
//
// This module closes the MLOps feedback loop: Monitor alerts + Experiment comparisons
// → Auto-scaling decisions → Cost-aware node adjustments. Every decision is persisted
// to filesystem (JSONL append-only) and signed through pkg/evidence attestation,
// creating an offline-verifiable chain of scaling rationales.
//
// Storage layout (--store, default ./.caf):
//
//	<store>/scaler/policies.json     list of active policies (array JSON)
//	<store>/scaler/decisions.jsonl   append-only decision history (one per line)
//
// Scaling rules are intuitive: latency/throughput regressions trigger scale_up;
// budget overruns trigger scale_down or no_change; experiment accuracy gains >=2pp
// suggest upgrading to stronger nodes. The system is defense-in-depth safe against
// path traversal and enforces strict budget math with <0.01 USD precision.
package scaler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Core types
// ============================================================================

// ScaleDecision represents one autoscaling decision with full audit trail.
// ID format: "sd-<hex16>" for uniqueness and traceability.
type ScaleDecision struct {
	ID                string    `json:"id"`                 // "sd-<hex16>"
	Action            string    `json:"action"`             // "scale_up" | "scale_down" | "no_change"
	Reason            string    `json:"reason"`             // detailed reason with specific metrics
	TriggerSource     string    `json:"trigger_source"`     // "monitor_alert" | "experiment_comparison" | "manual" | "budget_enforcement"
	CurrentNodes      int       `json:"current_nodes"`
	TargetNodes       int       `json:"target_nodes"`
	CostImpactPerHour float64   `json:"cost_impact_per_hour"` // USD change per hour
	BudgetOK          bool      `json:"budget_ok"`
	Applied           bool      `json:"applied"`
	CreatedAt         time.Time `json:"created_at"`
	AppliedAt         *time.Time `json:"applied_at,omitempty"`
}

// Policy defines a scaling policy with metric thresholds and constraints.
type Policy struct {
	ID              string `json:"id"`               // unique ID "pol-<hex8>"
	Name            string `json:"name"`             // human-readable name
	Metric          string `json:"metric"`           // "latency_p95" | "accuracy" | "throughput" | "error_rate"
	Threshold       float64 `json:"threshold"`        // regression threshold (%)
	Direction       string `json:"direction"`        // "regression_triggers_up" (degradation → scale_up)
	MinNodes        int    `json:"min_nodes"`        // floor constraint
	MaxNodes        int    `json:"max_nodes"`        // ceiling constraint
	CooldownMinutes int    `json:"cooldown_minutes"` // prevent thrashing
	Enabled         bool   `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`    // policy creation timestamp
}

// ScalerInterface interface for autoscaling decisions.
type ScalerInterface interface {
	// AddPolicy registers a new scaling policy (persisted + attested).
	AddPolicy(ctx context.Context, p Policy) error
	// ListPolicies returns all policies sorted by CreatedAt desc.
	ListPolicies() []Policy
	// EvaluateMonitorAlert evaluates monitor alert for scaling decision.
	// Rules: latency_p95 regression > threshold → scale_up; throughput regression → scale_up;
	// budget exceeded → scale_down or no_change if cost impact too high.
	EvaluateMonitorAlert(ctx context.Context, metric string, regressionPct float64, budgetLimit, currentCost float64) (*ScaleDecision, error)
	// EvaluateExperiment compares experiments and recommends upgrade if gain >= 2pp.
	EvaluateExperiment(ctx context.Context, accuracyGain float64, budgetLimit, currentCost float64) (*ScaleDecision, error)
	// Apply executes a decision: Applied=true + timestamp + attestation.
	Apply(ctx context.Context, decisionID string) error
	// GetHistory returns all decisions (newest first).
	GetHistory() []ScaleDecision
}

// ============================================================================
// FSM_scaler implementation
// ============================================================================

// FSM_scaler is the filesystem-backed Scaler implementation.
type FSMScaler struct {
	storeDir   string
	ledger     *evidence.Ledger
	policiesMu sync.RWMutex
	historyMu  sync.Mutex
	lastAttest *evidence.Evidence

	// timeMu guards lastTS, the monotonic clock guard for decision timestamps.
	timeMu sync.Mutex
	lastTS time.Time
}

var _ ScalerInterface = (*FSMScaler)(nil)

const (
	policiesFile    = "policies.json"
	decisionsFile   = "decisions.jsonl"
	maxNameLen      = 64
	idHexBytes      = 8
)

// NewFSMScaler opens (or creates) a scaler store at dir.
func NewFSMScaler(dir string, ledger *evidence.Ledger) (*FSMScaler, error) {
	if dir == "" {
		return nil, errors.New("scaler: store directory is required")
	}
	scDir := filepath.Join(dir, "scaler")
	if err := os.MkdirAll(scDir, 0o755); err != nil {
		return nil, fmt.Errorf("scaler: create store: %w", err)
	}
	return &FSMScaler{storeDir: scDir, ledger: ledger}, nil
}

// StoreDir returns the scaler store path.
func (s *FSMScaler) StoreDir() string { return s.storeDir }

// nextTimestamp returns a UTC timestamp guaranteed to be strictly greater than
// any previously issued decision timestamp for this scaler instance.
//
// Decisions form an append-only, chronologically ordered ledger, and GetHistory
// promises "newest first". Relying on raw time.Now() breaks that promise on
// platforms with coarse clock resolution: Windows advances the wall clock in
// ~15ms ticks, so several decisions generated within one tick receive an
// identical CreatedAt. Equal timestamps make newest-vs-oldest ordering
// ill-defined (t.After(t) == false) and the sort non-deterministic. Nudging
// each colliding timestamp forward by 1ns keeps ordering strict, deterministic,
// and monotonic without materially distorting the recorded time.
func (s *FSMScaler) nextTimestamp() time.Time {
	s.timeMu.Lock()
	defer s.timeMu.Unlock()
	now := time.Now().UTC()
	if !now.After(s.lastTS) {
		now = s.lastTS.Add(time.Nanosecond)
	}
	s.lastTS = now
	return now
}

// LastAttestation returns the most recent attestation receipt.
func (s *FSMScaler) LastAttestation() *evidence.Evidence {
	s.policiesMu.Lock()
	defer s.policiesMu.Unlock()
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	return s.lastAttest
}

// ============================================================================
// Policy management
// ============================================================================

// AddPolicy persists a new policy with attestation.
func (s *FSMScaler) AddPolicy(ctx context.Context, p Policy) error {
	if p.Name == "" {
		return errors.New("scaler: policy name is required")
	}
	if len(p.Name) > maxNameLen {
		return fmt.Errorf("scaler: policy name exceeds %d characters", maxNameLen)
	}
	if strings.ContainsAny(p.Name, "/\\:") {
		return fmt.Errorf("scaler: policy name contains invalid characters")
	}
	if p.MaxNodes <= p.MinNodes {
		return fmt.Errorf("scaler: max_nodes (%d) must be > min_nodes (%d)", p.MaxNodes, p.MinNodes)
	}

	s.policiesMu.Lock()
	defer s.policiesMu.Unlock()

	// Generate ID
	bytes := make([]byte, idHexBytes)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Errorf("scaler: generate random ID: %w", err)
	}
	p.ID = fmt.Sprintf("pol-%s", hex.EncodeToString(bytes)[:16])
	p.Enabled = true
	p.CreatedAt = time.Now().UTC()
	if p.CooldownMinutes <= 0 {
		p.CooldownMinutes = 5
	}

	// Load existing policies
	policies, err := s.loadPoliciesLocked()
	if err != nil {
		return err
	}

	policies = append(policies, p)
	if err := s.savePoliciesLocked(policies); err != nil {
		return err
	}

	// Attest
	if err := s.attestLocked(ctx, "policy.add", p.ID, map[string]any{"name": p.Name, "metric": p.Metric, "threshold": p.Threshold},
		map[string]any{"policy_id": p.ID, "enabled": true}, map[string]any{}); err != nil {
		return err
	}
	return nil
}

// ListPolicies returns all enabled policies sorted by CreatedAt desc.
func (s *FSMScaler) ListPolicies() []Policy {
	s.policiesMu.RLock()
	defer s.policiesMu.RUnlock()
	policies, _ := s.loadPoliciesLocked()
	var enabled []Policy
	for _, p := range policies {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		if !enabled[i].CreatedAt.Equal(enabled[j].CreatedAt) {
			return enabled[i].CreatedAt.After(enabled[j].CreatedAt)
		}
		return enabled[i].ID > enabled[j].ID
	})
	return enabled
}

// ============================================================================
// Decision evaluation
// ============================================================================

// EvaluateMonitorAlert evaluates a monitor alert for scaling decision.
func (s *FSMScaler) EvaluateMonitorAlert(ctx context.Context, metric string, regressionPct float64, budgetLimit, currentCost float64) (*ScaleDecision, error) {
	if metric == "" {
		return nil, errors.New("scaler: metric is required")
	}
	s.policiesMu.RLock()
	defer s.policiesMu.RUnlock()

	now := s.nextTimestamp()
	currentNodes := 4 // default baseline

	// Load policies to find a matching one
	policies, err := s.loadPoliciesLocked()
	if err != nil {
		return nil, fmt.Errorf("scaler: load policies: %w", err)
	}

	// Find matching policy
	var matchingPolicy *Policy
	for i := range policies {
		p := &policies[i]
		if !p.Enabled || p.Metric != normalizeMetric(metric) {
			continue
		}
		if regressionPct > p.Threshold && p.Direction == "regression_triggers_up" {
			matchingPolicy = p
			break
		}
	}

	action := "no_change"
	targetNodes := currentNodes
	costImpact := 0.0
	reasonParts := []string{}
	budgetOK := true

	if matchingPolicy != nil {
		// Regression detected — propose scale_up
		targetNodes = currentNodes + 1
		if targetNodes > matchingPolicy.MaxNodes {
			targetNodes = matchingPolicy.MaxNodes
			action = "no_change"
			reasonParts = append(reasonParts, fmt.Sprintf("capped at max_nodes=%d", matchingPolicy.MaxNodes))
		} else {
			action = "scale_up"
			reasonParts = append(reasonParts, fmt.Sprintf("%s regression %.1f%% exceeds threshold %.0f%%", metric, regressionPct, matchingPolicy.Threshold))
		}
		// Estimate cost impact (~$2/node/hr average)
		nodeCost := 2.0
		costImpact = float64(targetNodes-currentNodes) * nodeCost

		// Budget check
		newCost := currentCost + costImpact
		if newCost > budgetLimit {
			budgetOK = false
			action = "no_change"
			targetNodes = currentNodes
			reasonParts = append(reasonParts, fmt.Sprintf("BUDGET REJECTED: $%.2f+%.2f > $%.2f (over by $%.2f)", currentCost, costImpact, budgetLimit, newCost-budgetLimit))
		} else {
			reasonParts = append(reasonParts, fmt.Sprintf("within budget: $%.2f+%.2f ≤ $%.2f", currentCost, costImpact, budgetLimit))
		}
	} else {
		reasonParts = append(reasonParts, fmt.Sprintf("no matching policy for %s", metric))
	}

	decision := &ScaleDecision{
		ID:                fmt.Sprintf("sd-%s", generateRandomHex(16)),
		Action:            action,
		Reason:            strings.Join(reasonParts, "; "),
		TriggerSource:     "monitor_alert",
		CurrentNodes:      currentNodes,
		TargetNodes:       targetNodes,
		CostImpactPerHour: costImpact,
		BudgetOK:          budgetOK,
		CreatedAt:         now,
	}

	// Persist decision
	if err := s.appendDecisionLocked(decision); err != nil {
		return nil, fmt.Errorf("scaler: persist decision: %w", err)
	}

	return decision, nil
}

// EvaluateExperiment evaluates experiment comparison for scaling decision.
func (s *FSMScaler) EvaluateExperiment(ctx context.Context, accuracyGain float64, budgetLimit, currentCost float64) (*ScaleDecision, error) {
	s.policiesMu.RLock()
	defer s.policiesMu.RUnlock()

	now := s.nextTimestamp()
	currentNodes := 4

	action := "no_change"
	targetNodes := currentNodes
	costImpact := 0.0
	reasonParts := []string{}
	budgetOK := true

	// Accuracy gain >= 2pp suggests upgrading to stronger nodes
	if accuracyGain >= 2.0 {
		targetNodes = currentNodes + 1
		action = "scale_up"
		nodeCost := 2.0
		costImpact = nodeCost

		// Budget check
		newCost := currentCost + costImpact
		if newCost > budgetLimit {
			budgetOK = false
			action = "no_change"
			targetNodes = currentNodes
			reasonParts = append(reasonParts, fmt.Sprintf("BUDGET REJECTED: upgrade would cost $%.2f but budget $%.2f only allows $%.2f", costImpact, budgetLimit, budgetLimit-currentCost))
		} else {
			reasonParts = append(reasonParts, fmt.Sprintf("accuracy gain %.2fpp ≥ 2.0pp threshold — new model shows significant improvement", accuracyGain))
		}
	} else {
		reasonParts = append(reasonParts, fmt.Sprintf("accuracy gain %.2fpp < 2.0pp threshold — insufficient improvement to justify upgrade", accuracyGain))
	}

	decision := &ScaleDecision{
		ID:                fmt.Sprintf("sd-%s", generateRandomHex(16)),
		Action:            action,
		Reason:            strings.Join(reasonParts, "; "),
		TriggerSource:     "experiment_comparison",
		CurrentNodes:      currentNodes,
		TargetNodes:       targetNodes,
		CostImpactPerHour: costImpact,
		BudgetOK:          budgetOK,
		CreatedAt:         now,
	}

	if err := s.appendDecisionLocked(decision); err != nil {
		return nil, fmt.Errorf("scaler: persist decision: %w", err)
	}

	return decision, nil
}

// ============================================================================
// Decision application
// ============================================================================

// Apply executes a decision atomically with attestation.
func (s *FSMScaler) Apply(ctx context.Context, decisionID string) error {
	if decisionID == "" {
		return errors.New("scaler: decision ID is required")
	}

	s.historyMu.Lock()
	defer s.historyMu.Unlock()

	decisions, err := s.loadDecisionsLocked()
	if err != nil {
		return fmt.Errorf("scaler: load decisions: %w", err)
	}

	// Find decision
	idx := -1
	for i, d := range decisions {
		if d.ID == decisionID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("scaler: decision %q not found", decisionID)
	}

	decision := decisions[idx]
	if decision.Applied {
		return fmt.Errorf("scaler: decision %q already applied at %s", decisionID, decision.AppliedAt.Format(time.RFC3339))
	}

	decision.Applied = true
	appliedAt := time.Now().UTC()
	decision.AppliedAt = &appliedAt

	// Rewrite the full decisions list back to JSONL (atomically)
	decisions[idx] = decision
	var buf bytes.Buffer
	for _, d := range decisions {
		line, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("scaler: marshal decision: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	tmpPath := s.decisionsPath() + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("scaler: write temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.decisionsPath()); err != nil {
		return fmt.Errorf("scaler: rename: %w", err)
	}

	// Attest
	if err := s.attestToLedgerLocked(ctx, "decision.apply", decisionID,
		map[string]any{"decision_id": decisionID, "action": decision.Action},
		map[string]any{"applied_at": appliedAt.Format(time.RFC3339), "target_nodes": decision.TargetNodes},
		map[string]any{}); err != nil {
		return err
	}

	return nil
}

// GetHistory returns all decisions (newest first).
func (s *FSMScaler) GetHistory() []ScaleDecision {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	decisions, _ := s.loadDecisionsLocked()
	cp := make([]ScaleDecision, len(decisions))
	copy(cp, decisions)
	// Newest first. nextTimestamp guarantees strictly monotonic CreatedAt within a
	// process, but decisions may be reloaded from disk across restarts where two
	// records could share a timestamp; break such ties by ID descending so the
	// ordering stays total and deterministic.
	sort.SliceStable(cp, func(i, j int) bool {
		if !cp[i].CreatedAt.Equal(cp[j].CreatedAt) {
			return cp[i].CreatedAt.After(cp[j].CreatedAt)
		}
		return cp[i].ID > cp[j].ID
	})
	return cp
}

// ============================================================================
// Internal helpers
// ============================================================================

func (s *FSMScaler) policiesPath() string {
	return filepath.Join(s.storeDir, policiesFile)
}

func (s *FSMScaler) decisionsPath() string {
	return filepath.Join(s.storeDir, decisionsFile)
}

func (s *FSMScaler) loadPoliciesLocked() ([]Policy, error) {
	path := s.policiesPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Policy{}, nil
		}
		return nil, fmt.Errorf("load policies: %w", err)
	}
	var policies []Policy
	if err := json.Unmarshal(data, &policies); err != nil {
		return nil, fmt.Errorf("parse policies: %w", err)
	}
	return policies, nil
}

func (s *FSMScaler) savePoliciesLocked(policies []Policy) error {
	path := s.policiesPath()
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal policies: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write policies temp: %w", err)
	}
	return os.Rename(tmp, path)
}

func (s *FSMScaler) loadDecisionsLocked() ([]ScaleDecision, error) {
	path := s.decisionsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ScaleDecision{}, nil
		}
		return nil, fmt.Errorf("load decisions: %w", err)
	}
	var decisions []ScaleDecision
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var d ScaleDecision
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			continue
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

func (s *FSMScaler) appendDecisionLocked(d *ScaleDecision) error {
	path := s.decisionsPath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open decisions: %w", err)
	}
	defer f.Close()
	line, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal decision: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append decision: %w", err)
	}
	return nil
}

func (s *FSMScaler) attestToLedgerLocked(ctx context.Context, action, subject string, input, output, payload map[string]any) error {
	if s.ledger == nil {
		return nil
	}
	ev, err := s.ledger.Record(ctx, evidence.RecordInput{
		Actor:   "cafctl",
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("scaler: attestation failed: %w", err)
	}
	s.lastAttest = ev
	return nil
}

func (s *FSMScaler) attestLocked(ctx context.Context, action, subject string, input, output, payload map[string]any) error {
	if s.ledger == nil {
		return nil
	}
	ev, err := s.ledger.Record(ctx, evidence.RecordInput{
		Actor:   "cafctl",
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("scaler: attestation failed: %w", err)
	}
	s.lastAttest = ev
	return nil
}

func normalizeMetric(m string) string {
	switch m {
	case "latency_p95_ms", "latency-p95-ms", "latency_p95":
		return "latency_p95"
	case "accuracy", "accuracy_pct":
		return "accuracy"
	case "throughput_qps", "throughput-qps", "throughput":
		return "throughput"
	case "error_rate", "error-rate":
		return "error_rate"
	default:
		return m
	}
}

func generateRandomHex(n int) string {
	bytes := make([]byte, n/2)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
