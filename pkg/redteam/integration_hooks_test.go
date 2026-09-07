package redteam

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// ============================================================================
// Integration Tests: Attack Surface Assessment embedded in platform workflow
//
// These tests prove that security assessment happens AUTOMATICALLY as part of
// normal platform operations (deploy, RBAC change) — not as a separate manual step.
// ============================================================================

// TestDeployHook_AutoScan verifies that deploying a service auto-triggers attack scan.
func TestDeployHook_AutoScan(t *testing.T) {
	// Setup: Build an attack graph representing the cluster
	graph := NewADGraph()
	graph.AddNode("apiserver", "computer", true)  // high-value target
	graph.AddNode("db-primary", "computer", true) // high-value target
	graph.AddNode("frontend", "computer", false)
	graph.AddNode("new-service", "computer", false)
	graph.AddEdge("new-service", "frontend", "NetworkAccess", "T1021")
	graph.AddEdge("frontend", "apiserver", "AdminTo", "T1078")

	hook := NewDeployHook(graph)
	ctx := context.Background()

	// Simulate: customer deploys "new-service" with ports 80, 443, 8080
	payload, _ := json.Marshal(map[string]interface{}{
		"app":     "new-service",
		"version": "v1.2.0",
		"env":     "production",
		"ports":   []int{80, 443, 8080},
	})

	start := time.Now()
	report, err := hook.OnDeployCompleted(ctx, payload)
	latency := time.Since(start)

	if err != nil {
		t.Fatalf("deploy hook failed: %v", err)
	}

	t.Logf("=== Deploy Auto-Scan Result ===")
	t.Logf("Service: %s %s", report.ServiceName, report.Version)
	t.Logf("Exposed ports: %d", report.ExposedPorts)
	t.Logf("Misconfigured ports (no TLS): %d", report.MisconfiguPorts)
	t.Logf("Reachable attack paths: %d", report.ReachablePaths)
	t.Logf("CVE matches: %d", report.CVEMatches)
	t.Logf("Security Score: %d/100 (%s)", report.SecurityScore, report.Verdict)
	t.Logf("Latency: %v (target: <500ms)", latency)
	t.Logf("Evidence hash: %s", report.ReportHash[:32]+"...")

	if report.ReachablePaths > 0 {
		t.Logf("Path details:")
		for _, p := range report.PathDetails {
			t.Logf("  - %s", p)
		}
	}

	// Verify requirements
	if latency > 500*time.Millisecond {
		t.Errorf("latency %v exceeds 500ms target", latency)
	}
	if report.ReportHash == "" {
		t.Error("report must have evidence hash")
	}
	if report.ExposedPorts != 3 {
		t.Errorf("expected 3 exposed ports, got %d", report.ExposedPorts)
	}
}

// TestDeployHook_SafeService verifies that a safe deployment gets high score.
func TestDeployHook_SafeService(t *testing.T) {
	graph := NewADGraph()
	graph.AddNode("isolated-service", "computer", false)
	// No edges to high-value targets = safe

	hook := NewDeployHook(graph)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]interface{}{
		"app":     "isolated-service",
		"version": "v1.0.0",
		"env":     "dev",
		"ports":   []int{443}, // HTTPS only
	})

	report, _ := hook.OnDeployCompleted(ctx, payload)
	t.Logf("Safe service score: %d/100 (%s)", report.SecurityScore, report.Verdict)

	if report.SecurityScore < 80 {
		t.Errorf("safe service should score >= 80, got %d", report.SecurityScore)
	}
	if report.Verdict != "safe" {
		t.Errorf("expected 'safe' verdict, got %q", report.Verdict)
	}
}

// TestRBACHook_DetectsEscalation verifies privilege escalation path detection.
func TestRBACHook_DetectsEscalation(t *testing.T) {
	// Setup: existing graph with normal user and admin resource
	graph := NewADGraph()
	graph.AddNode("admin-panel", "resource", true) // high-value
	graph.AddNode("operators", "group", false)
	graph.AddEdge("operators", "admin-panel", "AccessTo", "T1078")

	hook := NewRBACHook(graph)
	ctx := context.Background()

	// Simulate: grant "operators" role to regular user "john"
	payload, _ := json.Marshal(map[string]interface{}{
		"user":     "john",
		"role":     "operators",
		"resource": "",
		"action":   "grant",
	})

	start := time.Now()
	result, err := hook.OnPermissionChange(ctx, payload)
	latency := time.Since(start)

	if err != nil {
		t.Fatalf("rbac hook failed: %v", err)
	}

	t.Logf("=== RBAC Path Validation Result ===")
	t.Logf("Change: grant 'operators' to 'john'")
	t.Logf("Safe: %v", result.Safe)
	t.Logf("Risk Score: %d/100", result.RiskScore)
	t.Logf("Latency: %v (target: <500ms)", latency)

	if !result.Safe {
		t.Logf("BLOCKED! Dangerous paths found:")
		for _, dp := range result.DangerousPaths {
			t.Logf("  %s → %s (%d hops, technique: %s)", dp.From, dp.To, dp.HopCount, dp.Technique)
			t.Logf("    Path: %v", dp.Hops)
		}
		t.Logf("Recommendation: %s", result.Recommendation)
	}

	// Should detect escalation path: john → operators → admin-panel
	if result.Safe {
		t.Error("expected UNSAFE result (john can reach admin-panel via operators)")
	}
	if result.RiskScore == 0 {
		t.Error("expected non-zero risk score")
	}
	if latency > 500*time.Millisecond {
		t.Errorf("latency %v exceeds 500ms", latency)
	}
}

// TestRBACHook_SafeChange verifies that safe permission changes pass.
func TestRBACHook_SafeChange(t *testing.T) {
	graph := NewADGraph()
	graph.AddNode("viewer-dashboard", "resource", false) // NOT high-value

	hook := NewRBACHook(graph)
	ctx := context.Background()

	// Grant read-only access to non-sensitive resource
	payload, _ := json.Marshal(map[string]interface{}{
		"user":     "alice",
		"role":     "viewers",
		"resource": "viewer-dashboard",
		"action":   "grant",
	})

	result, _ := hook.OnPermissionChange(ctx, payload)
	t.Logf("Safe change: %v, risk=%d", result.Safe, result.RiskScore)

	if !result.Safe {
		t.Error("granting access to non-sensitive resource should be safe")
	}
}

// TestRBACHook_RevokeAlwaysSafe verifies revocations never trigger warnings.
func TestRBACHook_RevokeAlwaysSafe(t *testing.T) {
	graph := NewADGraph()
	graph.AddNode("admin-panel", "resource", true)

	hook := NewRBACHook(graph)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]interface{}{
		"user":   "former-admin",
		"role":   "admin",
		"action": "revoke",
	})

	result, _ := hook.OnPermissionChange(ctx, payload)
	if !result.Safe {
		t.Error("revoke should always be safe")
	}
}

// BenchmarkDeployHook measures auto-scan latency.
func BenchmarkDeployHook(b *testing.B) {
	graph := NewADGraph()
	for i := 0; i < 100; i++ {
		graph.AddNode(nodeID(i), "computer", i == 99)
	}
	for i := 0; i < 99; i++ {
		graph.AddEdge(nodeID(i), nodeID(i+1), "NetworkAccess", "T1021")
	}
	hook := NewDeployHook(graph)
	ctx := context.Background()
	payload, _ := json.Marshal(map[string]interface{}{
		"app": nodeID(0), "version": "v1", "ports": []int{80, 443},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hook.OnDeployCompleted(ctx, payload)
	}
}

// BenchmarkRBACHook measures path validation latency.
func BenchmarkRBACHook(b *testing.B) {
	graph := NewADGraph()
	graph.AddNode("admin", "resource", true)
	graph.AddNode("ops", "group", false)
	graph.AddEdge("ops", "admin", "AccessTo", "T1078")
	hook := NewRBACHook(graph)
	ctx := context.Background()
	payload, _ := json.Marshal(map[string]interface{}{
		"user": "user1", "role": "ops", "action": "grant",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hook.OnPermissionChange(ctx, payload)
	}
}
