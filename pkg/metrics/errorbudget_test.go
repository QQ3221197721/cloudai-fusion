package metrics

import (
	"testing"
	"time"
)

// ============================================================================
// Error Budget Manager Tests
// ============================================================================

func TestNewErrorBudgetManager(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{})
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	budgets := mgr.GetAllBudgets()
	if len(budgets) != 4 { // DefaultSLOs: apiserver, scheduler, ai-engine, controlplane
		t.Errorf("expected 4 service budgets, got %d", len(budgets))
	}
}

func TestDefaultBudgetPolicies(t *testing.T) {
	policies := DefaultBudgetPolicies()
	if len(policies) != 4 {
		t.Errorf("expected 4 policies, got %d", len(policies))
	}
	// Verify order: normal < warning < critical < exhausted
	for i := 1; i < len(policies); i++ {
		if policies[i].ThresholdConsumed <= policies[i-1].ThresholdConsumed {
			t.Errorf("policies should be in ascending threshold order")
		}
	}
}

func TestBudgetPolicyLevelString(t *testing.T) {
	tests := []struct {
		level BudgetPolicyLevel
		want  string
	}{
		{BudgetPolicyNormal, "normal"},
		{BudgetPolicyWarning, "warning"},
		{BudgetPolicyCritical, "critical"},
		{BudgetPolicyExhausted, "exhausted"},
		{BudgetPolicyLevel(99), "unknown"},
	}
	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.want {
			t.Errorf("BudgetPolicyLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestRecordRequestAndEvaluate(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{})

	// Record 1000 successful requests
	for i := 0; i < 1000; i++ {
		mgr.RecordRequest("apiserver", false)
	}
	mgr.Evaluate()

	b, err := mgr.GetBudget("apiserver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.TotalRequests != 1000 {
		t.Errorf("expected 1000 total, got %d", b.TotalRequests)
	}
	if b.ErrorRequests != 0 {
		t.Errorf("expected 0 errors, got %d", b.ErrorRequests)
	}
	if b.ConsumedRatio != 0 {
		t.Errorf("expected 0 consumed, got %f", b.ConsumedRatio)
	}
	if b.RemainingRatio != 1.0 {
		t.Errorf("expected 1.0 remaining, got %f", b.RemainingRatio)
	}
	if b.ActivePolicy != BudgetPolicyNormal {
		t.Errorf("expected normal policy, got %s", b.ActivePolicy.String())
	}
}

func TestBudgetConsumption50Pct(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{
		SLODefinitions: []SLODefinition{
			{Service: "test-svc", AvailabilityTarget: 0.99, Window: 30 * 24 * time.Hour},
		},
	})

	// 99% target → 1% allowed error. 0.6% actual → 60% consumed → warning
	for i := 0; i < 1000; i++ {
		mgr.RecordRequest("test-svc", i < 6) // 6/1000 = 0.6% error → 60% consumed
	}
	mgr.Evaluate()

	b, _ := mgr.GetBudget("test-svc")
	if b.ActivePolicy != BudgetPolicyWarning {
		t.Errorf("expected warning at ~60%% consumed, got %s (consumed=%.4f)", b.ActivePolicy.String(), b.ConsumedRatio)
	}
	if b.DeploymentsFrozen {
		t.Error("deployments should NOT be frozen at warning level")
	}
}

func TestBudgetConsumption90Pct(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{
		SLODefinitions: []SLODefinition{
			{Service: "test-svc", AvailabilityTarget: 0.99, Window: 30 * 24 * time.Hour},
		},
	})

	// 99% target → 1% allowed. 0.9% actual → 90% consumed
	for i := 0; i < 1000; i++ {
		mgr.RecordRequest("test-svc", i < 9) // 9/1000 = 0.9%
	}
	mgr.Evaluate()

	b, _ := mgr.GetBudget("test-svc")
	if b.ActivePolicy != BudgetPolicyCritical {
		t.Errorf("expected critical at 90%% consumed, got %s (consumed=%.2f)", b.ActivePolicy.String(), b.ConsumedRatio)
	}
	if !b.DeploymentsFrozen {
		t.Error("deployments should be frozen at critical level")
	}
}

func TestBudgetExhausted(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{
		SLODefinitions: []SLODefinition{
			{Service: "test-svc", AvailabilityTarget: 0.99, Window: 30 * 24 * time.Hour},
		},
	})

	// 99% target → 1% allowed. 2% actual → 200% consumed → exhausted
	for i := 0; i < 1000; i++ {
		mgr.RecordRequest("test-svc", i < 20) // 20/1000 = 2%
	}
	mgr.Evaluate()

	b, _ := mgr.GetBudget("test-svc")
	if b.ActivePolicy != BudgetPolicyExhausted {
		t.Errorf("expected exhausted, got %s (consumed=%.2f)", b.ActivePolicy.String(), b.ConsumedRatio)
	}
	if !b.DeploymentsFrozen {
		t.Error("deployments should be frozen when exhausted")
	}
}

func TestIsDeploymentAllowed(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{
		SLODefinitions: []SLODefinition{
			{Service: "web", AvailabilityTarget: 0.99, Window: 30 * 24 * time.Hour},
		},
	})

	// Healthy budget
	for i := 0; i < 100; i++ {
		mgr.RecordRequest("web", false)
	}
	mgr.Evaluate()

	allowed, reason := mgr.IsDeploymentAllowed("web")
	if !allowed {
		t.Errorf("expected deployment allowed, got denied: %s", reason)
	}

	// Untracked service
	allowed2, _ := mgr.IsDeploymentAllowed("unknown-svc")
	if !allowed2 {
		t.Error("untracked service should allow deployments")
	}
}

func TestGetBudgetNotFound(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{})
	_, err := mgr.GetBudget("nonexistent")
	if err == nil {
		t.Error("expected error for unknown service")
	}
}

func TestPolicyTransitions(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{
		SLODefinitions: []SLODefinition{
			{Service: "svc", AvailabilityTarget: 0.99, Window: 30 * 24 * time.Hour},
		},
	})

	// Start at normal (0% consumed)
	for i := 0; i < 100; i++ {
		mgr.RecordRequest("svc", false)
	}
	mgr.Evaluate()

	// Push to warning (>50% consumed): 7 errors in 1000 total = 0.7% → 70% consumed
	for i := 0; i < 900; i++ {
		mgr.RecordRequest("svc", i < 7)
	}
	mgr.Evaluate()

	b, _ := mgr.GetBudget("svc")
	if len(b.PolicyTransitions) < 1 {
		t.Errorf("expected at least 1 policy transition, got %d", len(b.PolicyTransitions))
	}
}

func TestStartStop(t *testing.T) {
	mgr := NewErrorBudgetManager(ErrorBudgetConfig{
		EvalInterval: 50 * time.Millisecond,
	})
	// Just ensure Start/Stop don't panic
	mgr.Stop() // stop before start should be safe
}

// ============================================================================
// Alert Convergence Tests
// ============================================================================

func TestNewAlertConvergenceEngine(t *testing.T) {
	engine := NewAlertConvergenceEngine(DefaultAlertConvergenceConfig())
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if len(engine.config.GroupByLabels) == 0 {
		t.Error("expected non-empty group-by labels")
	}
}

func TestProcessAlertFirstAlert(t *testing.T) {
	engine := NewAlertConvergenceEngine(DefaultAlertConvergenceConfig())

	alert := &Alert{
		ID: "a1", Name: "HighLatency", Severity: AlertCritical,
		Service: "apiserver", Message: "P99 > 2s", FiredAt: time.Now(),
	}

	shouldNotify, groupKey, _ := engine.ProcessAlert(alert)
	if !shouldNotify {
		t.Error("first alert in group should trigger notification")
	}
	if groupKey == "" {
		t.Error("expected non-empty group key")
	}
}

func TestProcessAlertDeduplication(t *testing.T) {
	engine := NewAlertConvergenceEngine(AlertConvergenceConfig{
		DeduplicationTTL: 1 * time.Minute,
		GroupByLabels:    []string{"service"},
	})

	alert := &Alert{
		Name: "HighLatency", Severity: AlertCritical,
		Service: "apiserver", FiredAt: time.Now(),
	}

	// First: should notify
	notify1, _, _ := engine.ProcessAlert(alert)
	if !notify1 {
		t.Error("first alert should notify")
	}

	// Second (duplicate): should suppress
	notify2, _, reason := engine.ProcessAlert(alert)
	if notify2 {
		t.Error("duplicate alert should be suppressed")
	}
	if reason != "deduplicated" {
		t.Errorf("expected 'deduplicated' reason, got %q", reason)
	}
}

func TestProcessAlertGrouping(t *testing.T) {
	engine := NewAlertConvergenceEngine(AlertConvergenceConfig{
		DeduplicationTTL: 0, // disable dedup for this test
		GroupByLabels:    []string{"service"},
	})

	// Two different alerts for same service
	a1 := &Alert{Name: "HighLatency", Severity: AlertCritical, Service: "apiserver", FiredAt: time.Now()}
	a2 := &Alert{Name: "HighErrorRate", Severity: AlertWarning, Service: "apiserver", FiredAt: time.Now()}

	engine.ProcessAlert(a1)
	engine.ProcessAlert(a2)

	groups := engine.GetActiveGroups()
	if len(groups) != 1 {
		t.Errorf("expected 1 group (same service), got %d", len(groups))
	}
	if groups[0].Count != 2 {
		t.Errorf("expected 2 alerts in group, got %d", groups[0].Count)
	}
	// Highest severity should be critical
	if groups[0].Severity != AlertCritical {
		t.Errorf("expected critical severity, got %s", groups[0].Severity)
	}
}

func TestSilenceRule(t *testing.T) {
	engine := NewAlertConvergenceEngine(DefaultAlertConvergenceConfig())

	engine.AddSilence(&SilenceRule{
		ID:       "s1",
		Matchers: map[string]string{"service": "apiserver"},
		Reason:   "planned maintenance",
		StartsAt: time.Now().Add(-1 * time.Hour),
		EndsAt:   time.Now().Add(1 * time.Hour),
	})

	alert := &Alert{
		Name: "HighLatency", Severity: AlertCritical,
		Service: "apiserver", FiredAt: time.Now(),
	}

	shouldNotify, _, reason := engine.ProcessAlert(alert)
	if shouldNotify {
		t.Error("silenced alert should not notify")
	}
	if reason != "silenced by rule" {
		t.Errorf("expected 'silenced by rule', got %q", reason)
	}
}

func TestSilenceRuleNoMatchDifferentService(t *testing.T) {
	engine := NewAlertConvergenceEngine(DefaultAlertConvergenceConfig())

	engine.AddSilence(&SilenceRule{
		ID:       "s1",
		Matchers: map[string]string{"service": "scheduler"},
		StartsAt: time.Now().Add(-1 * time.Hour),
		EndsAt:   time.Now().Add(1 * time.Hour),
	})

	alert := &Alert{
		Name: "HighLatency", Severity: AlertCritical,
		Service: "apiserver", FiredAt: time.Now(),
	}

	shouldNotify, _, _ := engine.ProcessAlert(alert)
	if !shouldNotify {
		t.Error("alert for different service should not be silenced")
	}
}

func TestRemoveSilence(t *testing.T) {
	engine := NewAlertConvergenceEngine(DefaultAlertConvergenceConfig())

	engine.AddSilence(&SilenceRule{
		ID: "s1", Matchers: map[string]string{"service": "test"},
		StartsAt: time.Now().Add(-1 * time.Hour), EndsAt: time.Now().Add(1 * time.Hour),
	})

	if !engine.RemoveSilence("s1") {
		t.Error("expected successful removal")
	}
	if engine.RemoveSilence("nonexistent") {
		t.Error("expected false for nonexistent silence")
	}
}

func TestGetSilences(t *testing.T) {
	engine := NewAlertConvergenceEngine(DefaultAlertConvergenceConfig())

	engine.AddSilence(&SilenceRule{
		ID: "active", Matchers: map[string]string{"service": "a"},
		StartsAt: time.Now().Add(-1 * time.Hour), EndsAt: time.Now().Add(1 * time.Hour),
	})
	engine.AddSilence(&SilenceRule{
		ID: "expired", Matchers: map[string]string{"service": "b"},
		StartsAt: time.Now().Add(-2 * time.Hour), EndsAt: time.Now().Add(-1 * time.Hour),
	})

	active := engine.GetSilences()
	if len(active) != 1 {
		t.Errorf("expected 1 active silence, got %d", len(active))
	}
}

func TestCleanupExpired(t *testing.T) {
	engine := NewAlertConvergenceEngine(AlertConvergenceConfig{
		DeduplicationTTL: 1 * time.Millisecond, // very short for test
		RepeatInterval:   1 * time.Millisecond,
		GroupByLabels:    []string{"service"},
	})

	engine.ProcessAlert(&Alert{
		Name: "test", Service: "svc", Severity: AlertWarning, FiredAt: time.Now(),
	})

	time.Sleep(5 * time.Millisecond)

	cleaned := engine.CleanupExpired()
	if cleaned == 0 {
		t.Error("expected some entries to be cleaned")
	}
}

func TestSeverityRank(t *testing.T) {
	if severityRank(AlertInfo) >= severityRank(AlertWarning) {
		t.Error("info should be lower than warning")
	}
	if severityRank(AlertWarning) >= severityRank(AlertCritical) {
		t.Error("warning should be lower than critical")
	}
	if severityRank(AlertCritical) >= severityRank(AlertPage) {
		t.Error("critical should be lower than page")
	}
}

func TestAlertGroupSeverityEscalation(t *testing.T) {
	engine := NewAlertConvergenceEngine(AlertConvergenceConfig{
		DeduplicationTTL: 0,
		GroupByLabels:    []string{"service"},
	})

	// First alert: warning
	engine.ProcessAlert(&Alert{Name: "slow", Severity: AlertWarning, Service: "svc", FiredAt: time.Now()})
	// Second alert: critical (same group)
	engine.ProcessAlert(&Alert{Name: "down", Severity: AlertCritical, Service: "svc", FiredAt: time.Now()})

	groups := engine.GetActiveGroups()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].Severity != AlertCritical {
		t.Errorf("expected severity escalation to critical, got %s", groups[0].Severity)
	}
}
