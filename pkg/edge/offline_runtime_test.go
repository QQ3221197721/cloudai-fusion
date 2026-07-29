package edge

import (
	"context"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// newOfflineTestLogger returns a silent logger for tests.
func newOfflineTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	return l
}

// ---------------------------------------------------------------------------
// Invalid State Transition (not covered by edge_hardware_test.go)
// ---------------------------------------------------------------------------

// TestOfflineRuntime_InvalidTransition verifies that an invalid event returns
// an error and does not change state.
func TestOfflineRuntime_InvalidTransition(t *testing.T) {
	rt := NewOfflineRuntime("node-inv", DefaultOfflineRuntimeConfig(), newOfflineTestLogger())

	// sync_complete is not valid from online
	err := rt.HandleEvent(EventSyncComplete, "unexpected")
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}
	if !strings.Contains(err.Error(), "invalid transition") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rt.State(); got != StateOnline {
		t.Fatalf("state should remain online, got %q", got)
	}
}

// TestOfflineRuntime_FatalError verifies online → failed path.
func TestOfflineRuntime_FatalError(t *testing.T) {
	rt := NewOfflineRuntime("node-fatal", DefaultOfflineRuntimeConfig(), newOfflineTestLogger())

	if err := rt.HandleEvent(EventFatalError, "disk corruption"); err != nil {
		t.Fatalf("fatal_error: %v", err)
	}
	if got := rt.State(); got != StateFailed {
		t.Fatalf("state = %q, want failed", got)
	}

	// From failed, resource_recovered → degraded
	if err := rt.HandleEvent(EventResourceRecovered, "partial recovery"); err != nil {
		t.Fatalf("resource_recovered from failed: %v", err)
	}
	if got := rt.State(); got != StateDegraded {
		t.Fatalf("state = %q, want degraded", got)
	}
}

// ---------------------------------------------------------------------------
// PerformHealthCheck — nil input (not covered by edge_hardware_test.go)
// ---------------------------------------------------------------------------

// TestOfflineRuntime_HealthCheck_NilUsage verifies nil input is handled safely.
func TestOfflineRuntime_HealthCheck_NilUsage(t *testing.T) {
	rt := NewOfflineRuntime("node-nil", DefaultOfflineRuntimeConfig(), newOfflineTestLogger())

	snap := rt.PerformHealthCheck(nil)
	if snap.Healthy {
		t.Fatal("expected unhealthy with nil usage")
	}
	found := false
	for _, issue := range snap.Issues {
		if strings.Contains(issue, "no resource data") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'no resource data' in issues, got %v", snap.Issues)
	}
}

// TestOfflineRuntime_HealthCheck_AllThresholds verifies each resource axis
// independently triggers unhealthy when exceeding threshold.
func TestOfflineRuntime_HealthCheck_AllThresholds(t *testing.T) {
	cfg := DefaultOfflineRuntimeConfig()
	rt := NewOfflineRuntime("node-th", cfg, newOfflineTestLogger())

	cases := []struct {
		name  string
		usage EdgeResourceUsage
		issue string
	}{
		{"Memory", EdgeResourceUsage{MemoryPercent: 98}, "Memory critical"},
		{"Disk", EdgeResourceUsage{DiskPercent: 98}, "Disk critical"},
		{"GPU", EdgeResourceUsage{GPUPercent: 99}, "GPU critical"},
		{"Temperature", EdgeResourceUsage{Temperature: 95}, "Temperature critical"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := rt.PerformHealthCheck(&tc.usage)
			if snap.Healthy {
				t.Fatalf("%s: expected unhealthy", tc.name)
			}
			found := false
			for _, issue := range snap.Issues {
				if strings.Contains(issue, tc.issue) {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s: expected %q in issues, got %v", tc.name, tc.issue, snap.Issues)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LocalDecisionEngine — deferred (not covered by edge_hardware_test.go)
// ---------------------------------------------------------------------------

// TestLocalDecisionEngine_BestEffort verifies best-effort workload with normal
// resources gets approved (matches the best-effort policy).
func TestLocalDecisionEngine_BestEffort(t *testing.T) {
	eng := NewLocalDecisionEngine(100, newOfflineTestLogger())

	res := &EdgeResourceUsage{CPUPercent: 10, MemoryPercent: 10}
	d := eng.Evaluate("best-effort", res, 50)
	if d.Result != "approved" {
		t.Fatalf("expected approved, got %q (reason: %s)", d.Result, d.Reason)
	}
}

// TestLocalDecisionEngine_Stats verifies Stats() counters.
func TestLocalDecisionEngine_Stats(t *testing.T) {
	eng := NewLocalDecisionEngine(100, newOfflineTestLogger())
	normalRes := &EdgeResourceUsage{CPUPercent: 50, MemoryPercent: 50}
	highRes := &EdgeResourceUsage{CPUPercent: 97, MemoryPercent: 50}

	eng.Evaluate("critical", normalRes, 100) // approved
	eng.Evaluate("critical", highRes, 100)   // denied (CPU too high)

	stats := eng.Stats()
	if stats["approved"].(int) != 1 {
		t.Fatalf("approved = %v, want 1", stats["approved"])
	}
	if stats["denied"].(int) != 1 {
		t.Fatalf("denied = %v, want 1", stats["denied"])
	}
	if stats["total"].(int) != 2 {
		t.Fatalf("total = %v, want 2", stats["total"])
	}
}

// ---------------------------------------------------------------------------
// HealthChecker — detailed score (not covered by edge_hardware_test.go)
// ---------------------------------------------------------------------------

// TestHealthChecker_Score verifies the numeric score calculation.
func TestHealthChecker_Score(t *testing.T) {
	hc := NewHealthChecker("node-score", newOfflineTestLogger())
	hc.RunAll(context.Background())

	summary := hc.Summary()
	if summary["pass"].(int) != 8 {
		t.Fatalf("pass = %v, want 8", summary["pass"])
	}
	if summary["fail"].(int) != 0 {
		t.Fatalf("fail = %v, want 0", summary["fail"])
	}
	if summary["score"].(float64) != 100.0 {
		t.Fatalf("score = %v, want 100.0", summary["score"])
	}
}

// TestHealthChecker_WithFailingCheck verifies that a failing check reduces
// health score and flips IsHealthy.
func TestHealthChecker_WithFailingCheck(t *testing.T) {
	hc := NewHealthChecker("node-fail", newOfflineTestLogger())

	// Inject a custom failing check
	hc.checks = append(hc.checks, HealthCheckFunc{
		Name: "custom_fail", Category: "hardware",
		CheckFn: func(ctx context.Context) HealthCheckResult {
			return HealthCheckResult{Name: "custom_fail", Category: "hardware", Status: "fail", Message: "broken"}
		},
	})

	hc.RunAll(context.Background())

	if hc.IsHealthy() {
		t.Fatal("expected IsHealthy = false with a failing check")
	}
	summary := hc.Summary()
	if summary["fail"].(int) != 1 {
		t.Fatalf("fail = %v, want 1", summary["fail"])
	}
	if summary["healthy"].(bool) != false {
		t.Fatal("expected healthy = false in summary")
	}
}
