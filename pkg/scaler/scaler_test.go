package scaler

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func TestAddPolicy_Persists(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	p := Policy{
		Name:            "test-policy",
		Metric:          "latency_p95",
		Threshold:       25,
		Direction:       "regression_triggers_up",
		MinNodes:        1,
		MaxNodes:        10,
		CooldownMinutes: 5,
	}

	if err := s.AddPolicy(ctx, p); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	// Verify policy persisted
	policies := s.ListPolicies()
	if len(policies) == 0 {
		t.Fatal("no policies found")
	}

	if policies[0].Name != "test-policy" {
		t.Errorf("expected name 'test-policy', got %q", policies[0].Name)
	}

	// Check files exist in <store>/scaler/ subdirectory
	if _, err := os.Stat(filepath.Join(tmpDir, "scaler", "policies.json")); os.IsNotExist(err) {
		t.Fatal("policies.json not created")
	}
}

func TestEvaluateMonitorAlert_ScaleUp_Triggered(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	budgetLimit := 100.0
	currentCost := 80.0

	// Add matching policy so regression triggers scale_up (max > 4 baseline)
	err = s.AddPolicy(ctx, Policy{
		Name:            "p95-regression-trigger",
		Metric:          "latency_p95",
		Threshold:       25,
		Direction:       "regression_triggers_up",
		MinNodes:        1,
		MaxNodes:        10,
		CooldownMinutes: 5,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	decision, err := s.EvaluateMonitorAlert(ctx, "latency_p95", 30, budgetLimit, currentCost)
	if err != nil {
		t.Fatalf("EvaluateMonitorAlert: %v", err)
	}

	if decision.Action != "scale_up" {
		t.Errorf("expected action 'scale_up', got %q", decision.Action)
	}

	if !decision.BudgetOK {
		t.Error("expected BudgetOK=true, got false")
	}

	if decision.TargetNodes <= decision.CurrentNodes {
		t.Errorf("expected target > current nodes, got %d vs %d", decision.TargetNodes, decision.CurrentNodes)
	}

	if !strings.Contains(decision.Reason, "regression") {
		t.Errorf("expected reason to mention regression, got %q", decision.Reason)
	}
}

func TestEvaluateMonitorAlert_BudgetRejected(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	budgetLimit := 100.0
	currentCost := 99.0 // Nearly at budget — $99 + $2 = $101 > $100 → BUDGET REJECTED

	// First add matching policy so regression triggers evaluation
	policy := Policy{
		Name:            "budget-guard",
		Metric:          "latency_p95",
		Threshold:       25,
		Direction:       "regression_triggers_up",
		MinNodes:        1,
		MaxNodes:        10,
		CooldownMinutes: 5,
	}
	if err := s.AddPolicy(ctx, policy); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	decision, err := s.EvaluateMonitorAlert(ctx, "latency_p95", 30, budgetLimit, currentCost)
	if err != nil {
		t.Fatalf("EvaluateMonitorAlert: %v", err)
	}

	// Should be rejected due to budget (99 + 2 = 101 > 100)
	if decision.BudgetOK {
		t.Error("expected BudgetOK=false due to budget exceeded")
	}

	if decision.Action != "no_change" {
		t.Errorf("expected action 'no_change' when budget exceeded, got %q", decision.Action)
	}

	if decision.TargetNodes != decision.CurrentNodes {
		t.Errorf("expected target==current when rejected, got %d vs %d", decision.TargetNodes, decision.CurrentNodes)
	}

	if !strings.Contains(decision.Reason, "BUDGET REJECTED") {
		t.Errorf("expected reason to mention BUDGET REJECTED, got %q", decision.Reason)
	}
}

func TestEvaluateMonitorAlert_MaxNodes_Capped(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	budgetLimit := 100.0
	currentCost := 50.0

	// Set max_nodes = 4 (same as current baseline)
	err = s.AddPolicy(ctx, Policy{
		Name:            "capped-policy",
		Metric:          "latency_p95",
		Threshold:       10,
		Direction:       "regression_triggers_up",
		MinNodes:        1,
		MaxNodes:        4,
		CooldownMinutes: 5,
	})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	decision, err := s.EvaluateMonitorAlert(ctx, "latency_p95", 30, budgetLimit, currentCost)
	if err != nil {
		t.Fatalf("EvaluateMonitorAlert: %v", err)
	}

	// Should be capped at max_nodes
	if decision.TargetNodes > 4 {
		t.Errorf("expected target capped at 4, got %d", decision.TargetNodes)
	}
}

func TestEvaluateExperiment_UpgradeRecommended(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	budgetLimit := 100.0
	currentCost := 60.0

	// Accuracy gain 3.5pp >= 2.0 threshold
	decision, err := s.EvaluateExperiment(ctx, 3.5, budgetLimit, currentCost)
	if err != nil {
		t.Fatalf("EvaluateExperiment: %v", err)
	}

	if decision.Action != "scale_up" {
		t.Errorf("expected action 'scale_up', got %q", decision.Action)
	}

	if !strings.Contains(decision.Reason, "accuracy gain") {
		t.Errorf("expected reason to mention accuracy gain, got %q", decision.Reason)
	}

	if !strings.Contains(decision.Reason, "≥ 2.0pp") {
		t.Errorf("expected reason to mention threshold, got %q", decision.Reason)
	}
}

func TestEvaluateExperiment_BudgetRejected(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	budgetLimit := 100.0
	currentCost := 99.0 // At almost full budget

	decision, err := s.EvaluateExperiment(ctx, 5.0, budgetLimit, currentCost)
	if err != nil {
		t.Fatalf("EvaluateExperiment: %v", err)
	}

	if decision.BudgetOK {
		t.Error("expected BudgetOK=false when budget exceeded")
	}

	if decision.Action != "no_change" {
		t.Errorf("expected 'no_change' when rejected, got %q", decision.Action)
	}
}

func TestApply_Once_Only(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	decision, err := s.EvaluateMonitorAlert(ctx, "latency_p95", 30, 100.0, 60.0)
	if err != nil {
		t.Fatalf("EvaluateMonitorAlert: %v", err)
	}

	if decision.Applied {
		t.Error("new decision should not be Applied yet")
	}

	// First apply should succeed
	if err := s.Apply(ctx, decision.ID); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	history := s.GetHistory()
	found := false
	for _, d := range history {
		if d.ID == decision.ID && d.Applied {
			found = true
			if d.AppliedAt == nil {
				t.Error("AppliedAt should not be nil after apply")
			}
		}
	}
	if !found {
		t.Error("decision not found in history with Applied=true")
	}

	// Second apply should fail
	if err := s.Apply(ctx, decision.ID); err == nil {
		t.Error("expected error on second Apply, got nil")
	} else if !strings.Contains(err.Error(), "already applied") {
		t.Errorf("expected 'already applied' error, got %v", err)
	}
}

func TestHistory_Append(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()

	// Generate multiple decisions (decisions are verified via history below)
	_, _ = s.EvaluateMonitorAlert(ctx, "latency_p95", 30, 100.0, 50.0)
	_, _ = s.EvaluateExperiment(ctx, 3.5, 100.0, 50.0)
	_, _ = s.EvaluateMonitorAlert(ctx, "throughput", 25, 100.0, 50.0)

	history := s.GetHistory()
	if len(history) < 3 {
		t.Fatalf("expected at least 3 decisions, got %d", len(history))
	}

	// Verify IDs are unique
	ids := make(map[string]bool)
	for _, d := range history[:3] {
		if ids[d.ID] {
			t.Errorf("duplicate ID found: %s", d.ID)
		}
		ids[d.ID] = true
	}

	// Verify newest first order (should be sorted by CreatedAt desc)
	if !history[0].CreatedAt.After(history[len(history)-1].CreatedAt) {
		t.Error("history should be sorted newest-first")
	}

	// Verify JSONL file exists in <store>/scaler/ subdirectory
	data, err := os.ReadFile(filepath.Join(tmpDir, "scaler", "decisions.jsonl"))
	if err != nil {
		t.Fatalf("read decisions.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines in JSONL, got %d", len(lines))
	}
}

func TestScaleDecision_IDFormat(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	decision, err := s.EvaluateMonitorAlert(ctx, "latency_p95", 30, 100.0, 50.0)
	if err != nil {
		t.Fatalf("EvaluateMonitorAlert: %v", err)
	}

	if !strings.HasPrefix(decision.ID, "sd-") {
		t.Errorf("expected ID prefix 'sd-', got %q", decision.ID)
	}

	idSuffix := decision.ID[3:]
	if len(idSuffix) != 16 {
		t.Errorf("expected 16 hex chars after 'sd-', got %d: %q", len(idSuffix), idSuffix)
	}
}

func TestPolicy_CreatedAtSet(t *testing.T) {
	tmpDir := t.TempDir()

	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("ephemeral signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	s, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		t.Fatalf("NewFSMScaler: %v", err)
	}

	ctx := context.Background()
	policy := Policy{
		Name:        "test-policy-createdAt",
		Metric:      "accuracy",
		Threshold:   5,
		Direction:   "regression_triggers_up",
		MinNodes:    1,
		MaxNodes:    5,
	}

	timeBefore := time.Now().UTC()
	if err := s.AddPolicy(ctx, policy); err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	timeAfter := time.Now().UTC()

	policies := s.ListPolicies()
	if len(policies) == 0 {
		t.Fatal("no policies")
	}

	p := policies[0]
	if p.CreatedAt.Before(timeBefore) || p.CreatedAt.After(timeAfter) {
		t.Errorf("CreatedAt out of range: %s", p.CreatedAt.Format(time.RFC3339))
	}
}
