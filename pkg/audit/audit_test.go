package audit

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newTestManager() *Manager {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	return NewManager(ManagerConfig{
		RetentionDays: 365,
		MaxEvents:     10000,
		Logger:        logger,
	})
}

func TestNewManager(t *testing.T) {
	m := newTestManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.config.RetentionDays != 365 {
		t.Errorf("RetentionDays = %d, want 365", m.config.RetentionDays)
	}
	if m.config.MaxEvents != 10000 {
		t.Errorf("MaxEvents = %d, want 10000", m.config.MaxEvents)
	}
}

func TestNewManagerDefaults(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if m.config.RetentionDays != 365 {
		t.Errorf("default RetentionDays = %d, want 365", m.config.RetentionDays)
	}
	if m.config.MaxEvents != 100000 {
		t.Errorf("default MaxEvents = %d, want 100000", m.config.MaxEvents)
	}
}

func TestRecordEvent(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	err := m.RecordEvent(ctx, &AuditEvent{
		UserID:   "user-1",
		Username: "alice",
		Action:   "create_workload",
		Resource: "workload",
		Category: CategoryWorkload,
		Severity: SeverityInfo,
		Result:   "success",
	})
	if err != nil {
		t.Fatalf("RecordEvent error: %v", err)
	}

	if m.GetEventCount() != 1 {
		t.Errorf("event count = %d, want 1", m.GetEventCount())
	}

	// Verify auto-filled fields
	events := m.QueryEvents(EventFilter{})
	if len(events) != 1 {
		t.Fatalf("query returned %d events, want 1", len(events))
	}
	if events[0].ID == "" {
		t.Error("event ID should be auto-generated")
	}
	if events[0].Timestamp.IsZero() {
		t.Error("event timestamp should be auto-set")
	}
}

func TestRecordEventCancelledContext(t *testing.T) {
	m := newTestManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.RecordEvent(ctx, &AuditEvent{
		UserID: "user-1", Action: "test", Result: "success",
	})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestRecordEventMaxEviction(t *testing.T) {
	m := NewManager(ManagerConfig{MaxEvents: 5})
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		m.RecordEvent(ctx, &AuditEvent{
			UserID: "user-1", Action: "test", Result: "success",
		})
	}

	if m.GetEventCount() != 5 {
		t.Errorf("event count = %d, want 5 (max eviction)", m.GetEventCount())
	}
}

func TestQueryEventsFilter(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	// Record diverse events
	events := []*AuditEvent{
		{UserID: "user-1", TenantID: "tenant-a", Username: "alice", Action: "login",
			Category: CategoryAuth, Severity: SeverityInfo, Result: "success"},
		{UserID: "user-1", TenantID: "tenant-a", Username: "alice", Action: "create_workload",
			Category: CategoryWorkload, Severity: SeverityInfo, Result: "success"},
		{UserID: "user-2", TenantID: "tenant-b", Username: "bob", Action: "delete_cluster",
			Category: CategoryAdmin, Severity: SeverityCritical, Result: "failure"},
		{UserID: "user-2", TenantID: "tenant-b", Username: "bob", Action: "login",
			Category: CategoryAuth, Severity: SeverityInfo, Result: "success"},
		{UserID: "user-3", TenantID: "tenant-a", Username: "charlie", Action: "update_config",
			Category: CategoryConfig, Severity: SeverityWarning, Result: "success"},
	}
	for _, e := range events {
		m.RecordEvent(ctx, e)
	}

	// Filter by tenant
	result := m.QueryEvents(EventFilter{TenantID: "tenant-a"})
	if len(result) != 3 {
		t.Errorf("tenant-a filter: got %d, want 3", len(result))
	}

	// Filter by user
	result = m.QueryEvents(EventFilter{UserID: "user-2"})
	if len(result) != 2 {
		t.Errorf("user-2 filter: got %d, want 2", len(result))
	}

	// Filter by category
	result = m.QueryEvents(EventFilter{Category: CategoryAuth})
	if len(result) != 2 {
		t.Errorf("auth category filter: got %d, want 2", len(result))
	}

	// Filter by severity
	result = m.QueryEvents(EventFilter{Severity: SeverityCritical})
	if len(result) != 1 {
		t.Errorf("critical severity filter: got %d, want 1", len(result))
	}

	// Filter by result
	result = m.QueryEvents(EventFilter{Result: "failure"})
	if len(result) != 1 {
		t.Errorf("failure result filter: got %d, want 1", len(result))
	}

	// Filter with limit
	result = m.QueryEvents(EventFilter{Limit: 2})
	if len(result) != 2 {
		t.Errorf("limit filter: got %d, want 2", len(result))
	}
}

func TestQueryEventsTimeFilter(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	now := time.Now().UTC()
	timestamps := []time.Time{
		now.Add(-72 * time.Hour),
		now.Add(-48 * time.Hour),
		now.Add(-24 * time.Hour),
		now.Add(-1 * time.Hour),
	}

	for i, ts := range timestamps {
		m.RecordEvent(ctx, &AuditEvent{
			ID:        "",
			Timestamp: ts,
			UserID:    "user-1",
			Action:    "test",
			Result:    "success",
			Metadata:  map[string]string{"index": string(rune('0' + i))},
		})
	}

	start := now.Add(-50 * time.Hour)
	end := now.Add(-2 * time.Hour)
	result := m.QueryEvents(EventFilter{StartTime: &start, EndTime: &end})
	if len(result) != 2 {
		t.Errorf("time range filter: got %d events, want 2", len(result))
	}
}

func TestGetEventStats(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	testEvents := []*AuditEvent{
		{UserID: "u1", Action: "a1", Result: "success", Severity: SeverityInfo},
		{UserID: "u2", Action: "a2", Result: "success", Severity: SeverityInfo},
		{UserID: "u3", Action: "a3", Result: "failure", Severity: SeverityCritical},
		{UserID: "u4", Action: "a4", Result: "denied", Severity: SeverityCritical},
		{UserID: "u5", Action: "a5", Result: "success", Severity: SeverityWarning},
	}
	for _, e := range testEvents {
		m.RecordEvent(ctx, e)
	}

	stats := m.GetEventStats()
	if stats["total"] != 5 {
		t.Errorf("total = %d, want 5", stats["total"])
	}
	if stats["success"] != 3 {
		t.Errorf("success = %d, want 3", stats["success"])
	}
	if stats["failure"] != 1 {
		t.Errorf("failure = %d, want 1", stats["failure"])
	}
	if stats["denied"] != 1 {
		t.Errorf("denied = %d, want 1", stats["denied"])
	}
	if stats["critical"] != 2 {
		t.Errorf("critical = %d, want 2", stats["critical"])
	}
}

func TestRecordSecurityEvent(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	err := m.RecordSecurityEvent(ctx, "intrusion_detected", "admin", "Suspicious login attempt")
	if err != nil {
		t.Fatalf("RecordSecurityEvent error: %v", err)
	}

	events := m.QueryEvents(EventFilter{Category: CategorySecurity})
	if len(events) != 1 {
		t.Fatalf("security events: got %d, want 1", len(events))
	}
	if events[0].Severity != SeverityCritical {
		t.Errorf("severity = %s, want critical", events[0].Severity)
	}
	if events[0].Metadata["detail"] != "Suspicious login attempt" {
		t.Errorf("metadata detail mismatch")
	}
}

func TestGenerateMLPS3Report(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	// Seed authentication event so auth checks pass
	m.RecordEvent(ctx, &AuditEvent{
		UserID: "admin", Action: "login", Category: CategoryAuth,
		Severity: SeverityInfo, Result: "success",
	})
	// Seed network policy event
	m.RecordEvent(ctx, &AuditEvent{
		UserID: "system", Action: "network_policy_applied", Category: CategorySecurity,
		Severity: SeverityInfo, Result: "success",
	})

	now := time.Now().UTC()
	report := m.GenerateMLPS3Report(now.Add(-30*24*time.Hour), now)

	if report.Framework != FrameworkMLPS3 {
		t.Errorf("framework = %s, want mlps3", report.Framework)
	}
	if report.TotalChecks != 15 {
		t.Errorf("total checks = %d, want 15", report.TotalChecks)
	}
	if report.Score <= 0 {
		t.Error("score should be > 0")
	}
	if report.ID == "" {
		t.Error("report ID should be generated")
	}

	// With auth + network events, most controls should pass
	if report.Passed < 10 {
		t.Errorf("passed = %d, expected >= 10 with seeded events", report.Passed)
	}
}

func TestGenerateSOC2Report(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	// Seed events for better scores
	m.RecordEvent(ctx, &AuditEvent{
		UserID: "admin", Action: "login", Category: CategoryAuth,
		Severity: SeverityInfo, Result: "success",
	})

	now := time.Now().UTC()
	report := m.GenerateSOC2Report(now.Add(-30*24*time.Hour), now)

	if report.Framework != FrameworkSOC2 {
		t.Errorf("framework = %s, want soc2", report.Framework)
	}
	if report.TotalChecks != 18 {
		t.Errorf("total checks = %d, want 18", report.TotalChecks)
	}
	if report.Score <= 0 {
		t.Error("score should be > 0")
	}
	// SOC2 has some warnings but no hard failures
	if report.Status != "partial" {
		t.Errorf("status = %s, want partial (due to MFA/DR warnings)", report.Status)
	}
}

func TestGetComplianceSummary(t *testing.T) {
	m := newTestManager()
	now := time.Now().UTC()

	summary := m.GetComplianceSummary(now.Add(-30*24*time.Hour), now)

	if _, ok := summary["mlps3"]; !ok {
		t.Error("summary missing mlps3")
	}
	if _, ok := summary["soc2"]; !ok {
		t.Error("summary missing soc2")
	}
	if _, ok := summary["generated_at"]; !ok {
		t.Error("summary missing generated_at")
	}

	mlps3 := summary["mlps3"].(map[string]interface{})
	if mlps3["score"].(float64) <= 0 {
		t.Error("mlps3 score should be > 0")
	}
}

func TestCleanup(t *testing.T) {
	m := NewManager(ManagerConfig{RetentionDays: 7, MaxEvents: 10000})
	ctx := context.Background()

	// Add old event (30 days ago)
	m.RecordEvent(ctx, &AuditEvent{
		Timestamp: time.Now().UTC().AddDate(0, 0, -30),
		UserID:    "old-user", Action: "old_action", Result: "success",
	})
	// Add recent event
	m.RecordEvent(ctx, &AuditEvent{
		UserID: "new-user", Action: "new_action", Result: "success",
	})

	removed := m.Cleanup()
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if m.GetEventCount() != 1 {
		t.Errorf("remaining events = %d, want 1", m.GetEventCount())
	}
}

func TestExportEvents(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	now := time.Now().UTC()
	m.RecordEvent(ctx, &AuditEvent{
		Timestamp: now.Add(-2 * time.Hour), UserID: "u1", Action: "a1", Result: "success",
	})
	m.RecordEvent(ctx, &AuditEvent{
		Timestamp: now.Add(-1 * time.Hour), UserID: "u2", Action: "a2", Result: "success",
	})

	exported := m.ExportEvents(now.Add(-3*time.Hour), now)
	if len(exported) != 2 {
		t.Errorf("exported = %d, want 2", len(exported))
	}
}

func TestEventFilterMatches(t *testing.T) {
	event := &AuditEvent{
		TenantID: "t1", UserID: "u1", Category: CategoryAuth,
		Severity: SeverityInfo, Action: "login", Resource: "session",
		Result: "success", Timestamp: time.Now().UTC(),
	}

	// Empty filter matches everything
	if !(&EventFilter{}).Matches(event) {
		t.Error("empty filter should match all events")
	}

	// Matching filter
	if !(EventFilter{TenantID: "t1", UserID: "u1"}).Matches(event) {
		t.Error("matching filter should return true")
	}

	// Non-matching tenant
	if (EventFilter{TenantID: "t2"}).Matches(event) {
		t.Error("wrong tenant should not match")
	}

	// Non-matching action
	if (EventFilter{Action: "logout"}).Matches(event) {
		t.Error("wrong action should not match")
	}
}

func TestAuditRetentionCheck(t *testing.T) {
	// RetentionDays >= 180 -> pass
	m180 := NewManager(ManagerConfig{RetentionDays: 180})
	if m180.checkAuditRetention() != "pass" {
		t.Error("180 days retention should pass")
	}

	// RetentionDays < 180 -> fail
	m90 := NewManager(ManagerConfig{RetentionDays: 90})
	if m90.checkAuditRetention() != "fail" {
		t.Error("90 days retention should fail")
	}
}

func TestScoreReport(t *testing.T) {
	m := newTestManager()

	report := &ComplianceReport{
		Controls: []ComplianceControl{
			{Status: "pass"}, {Status: "pass"}, {Status: "pass"},
			{Status: "warning"}, {Status: "fail"},
		},
	}
	m.scoreReport(report)

	if report.TotalChecks != 5 {
		t.Errorf("total = %d, want 5", report.TotalChecks)
	}
	if report.Passed != 3 {
		t.Errorf("passed = %d, want 3", report.Passed)
	}
	if report.Failed != 1 {
		t.Errorf("failed = %d, want 1", report.Failed)
	}
	if report.Warnings != 1 {
		t.Errorf("warnings = %d, want 1", report.Warnings)
	}
	// Score: 3/5*100 = 60%
	if report.Score != 60.0 {
		t.Errorf("score = %f, want 60.0", report.Score)
	}
	if report.Status != "non_compliant" {
		t.Errorf("status = %s, want non_compliant (has failures)", report.Status)
	}

	// All pass
	allPass := &ComplianceReport{
		Controls: []ComplianceControl{{Status: "pass"}, {Status: "pass"}},
	}
	m.scoreReport(allPass)
	if allPass.Status != "compliant" {
		t.Errorf("all-pass status = %s, want compliant", allPass.Status)
	}
}
