package observability

import (
	"context"
	"testing"
	"time"
)

func TestCreateRoutingRule(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	rule, err := mgr.CreateRoutingRule(ctx, &RoutingRule{
		Name:          "gpu-alerts",
		MatchLabels:   map[string]string{"component": "gpu"},
		MatchSeverity: []Severity{SeverityP0Critical, SeverityP1High},
		Channels: []ChannelConfig{
			{Type: ChannelPagerDuty, Target: "gpu-oncall"},
			{Type: ChannelSlack, Target: "#gpu-alerts"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRoutingRule failed: %v", err)
	}
	if rule.ID == "" {
		t.Fatal("expected non-empty rule ID")
	}
	if !rule.Active {
		t.Error("expected rule to be active")
	}
}

func TestClassifyAndRouteAlert(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	// Create a routing rule
	mgr.CreateRoutingRule(ctx, &RoutingRule{
		Name:          "cluster-alerts",
		MatchLabels:   map[string]string{"team": "platform"},
		MatchSeverity: []Severity{SeverityP0Critical},
		Channels: []ChannelConfig{
			{Type: ChannelPagerDuty, Target: "platform-oncall"},
		},
	})

	// Route an alert that matches
	alert, err := mgr.ClassifyAndRouteAlert(ctx, &ClassifiedAlert{
		Name:     "ClusterUnreachable",
		Severity: SeverityP0Critical,
		Source:   "prometheus",
		Labels:   map[string]string{"team": "platform", "cluster": "prod-east"},
		Summary:  "Cluster prod-east API server unreachable",
	})
	if err != nil {
		t.Fatalf("ClassifyAndRouteAlert failed: %v", err)
	}
	if alert.Status != AlertFiring {
		t.Errorf("expected firing status, got %s", alert.Status)
	}
	if len(alert.RoutedTo) == 0 {
		t.Error("expected alert to be routed to at least one channel")
	}
}

func TestAlertDefaultRouting(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	// No routing rules — should use default
	alert, _ := mgr.ClassifyAndRouteAlert(ctx, &ClassifiedAlert{
		Name:     "TestAlert",
		Severity: SeverityP0Critical,
		Labels:   map[string]string{},
		Summary:  "Test critical alert",
	})
	if len(alert.RoutedTo) == 0 {
		t.Error("expected default routing for P0 alert")
	}
}

func TestAcknowledgeAlert(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	alert, _ := mgr.ClassifyAndRouteAlert(ctx, &ClassifiedAlert{
		Name: "test", Severity: SeverityP2Medium, Summary: "test",
	})

	err := mgr.AcknowledgeAlert(ctx, alert.ID, "user-123")
	if err != nil {
		t.Fatalf("AcknowledgeAlert failed: %v", err)
	}

	alerts, _ := mgr.ListAlerts(ctx, AlertAcknowledged)
	if len(alerts) != 1 {
		t.Errorf("expected 1 acknowledged alert, got %d", len(alerts))
	}
}

func TestResolveAlert(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	alert, _ := mgr.ClassifyAndRouteAlert(ctx, &ClassifiedAlert{
		Name: "resolve-test", Severity: SeverityP3Low, Summary: "test",
	})

	err := mgr.ResolveAlert(ctx, alert.ID)
	if err != nil {
		t.Fatalf("ResolveAlert failed: %v", err)
	}

	alerts, _ := mgr.ListAlerts(ctx, AlertResolved)
	if len(alerts) != 1 {
		t.Errorf("expected 1 resolved alert, got %d", len(alerts))
	}
}

func TestCreateOnCallSchedule(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	schedule, err := mgr.CreateOnCallSchedule(ctx, &OnCallSchedule{
		Name:         "platform-oncall",
		Team:         "platform",
		RotationType: RotationWeekly,
		Participants: []OnCallMember{
			{UserID: "u1", Name: "Alice", Email: "alice@example.com", Order: 1},
			{UserID: "u2", Name: "Bob", Email: "bob@example.com", Order: 2},
			{UserID: "u3", Name: "Charlie", Email: "charlie@example.com", Order: 3},
		},
		StartDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateOnCallSchedule failed: %v", err)
	}
	if schedule.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if schedule.RotationLength != 7*24*time.Hour {
		t.Errorf("expected weekly rotation length, got %v", schedule.RotationLength)
	}
}

func TestGetCurrentOnCall(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	schedule, _ := mgr.CreateOnCallSchedule(ctx, &OnCallSchedule{
		Name:         "test-oncall",
		Team:         "test",
		RotationType: RotationWeekly,
		Participants: []OnCallMember{
			{UserID: "u1", Name: "Alice", Order: 1},
			{UserID: "u2", Name: "Bob", Order: 2},
		},
		StartDate: time.Now().Add(-24 * time.Hour),
	})

	shift, err := mgr.GetCurrentOnCall(ctx, schedule.ID)
	if err != nil {
		t.Fatalf("GetCurrentOnCall failed: %v", err)
	}
	if shift.Primary.UserID == "" {
		t.Error("expected non-empty primary on-call")
	}
	if shift.Secondary == nil {
		t.Error("expected secondary on-call")
	}
}

func TestOnCallOverride(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	schedule, _ := mgr.CreateOnCallSchedule(ctx, &OnCallSchedule{
		Name:         "override-test",
		Team:         "test",
		RotationType: RotationWeekly,
		Participants: []OnCallMember{
			{UserID: "u1", Name: "Alice", Order: 1},
			{UserID: "u2", Name: "Bob", Order: 2},
		},
		StartDate: time.Now().Add(-24 * time.Hour),
	})

	now := time.Now()
	err := mgr.AddScheduleOverride(ctx, schedule.ID, ScheduleOverride{
		UserID:    "u2",
		UserName:  "Bob",
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now.Add(1 * time.Hour),
		Reason:    "Covering for Alice",
	})
	if err != nil {
		t.Fatalf("AddScheduleOverride failed: %v", err)
	}

	shift, _ := mgr.GetCurrentOnCall(ctx, schedule.ID)
	if shift.Primary.UserID != "u2" {
		t.Errorf("expected Bob (u2) as on-call due to override, got %s", shift.Primary.UserID)
	}
}

func TestCreateRunbook(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	rb, err := mgr.CreateRunbook(ctx, &Runbook{
		Name:        "restart-service",
		Description: "Restart a failing service deployment",
		Steps: []RunbookStep{
			{Order: 1, Name: "Check Pod Status", Type: StepTypeKubectl, Command: "kubectl get pods -n cloudai-system"},
			{Order: 2, Name: "Rollout Restart", Type: StepTypeKubectl, Command: "kubectl rollout restart deployment/apiserver -n cloudai-system"},
			{Order: 3, Name: "Verify Health", Type: StepTypeHTTP, Webhook: "https://apiserver/healthz"},
		},
		Owner: "platform-team",
	})
	if err != nil {
		t.Fatalf("CreateRunbook failed: %v", err)
	}
	if rb.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if len(rb.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(rb.Steps))
	}
}

func TestExecuteRunbook(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	rb, _ := mgr.CreateRunbook(ctx, &Runbook{
		Name: "test-runbook",
		Steps: []RunbookStep{
			{Order: 1, Name: "Step 1", Type: StepTypeCommand},
			{Order: 2, Name: "Step 2", Type: StepTypeHTTP},
		},
		Owner: "test",
	})

	exec, err := mgr.ExecuteRunbook(ctx, rb.ID, "admin")
	if err != nil {
		t.Fatalf("ExecuteRunbook failed: %v", err)
	}
	if exec.Status != ExecutionSuccess {
		t.Errorf("expected success, got %s", exec.Status)
	}
	if len(exec.Steps) != 2 {
		t.Errorf("expected 2 step results, got %d", len(exec.Steps))
	}
}

func TestExecuteRunbookRequiresApproval(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	rb, _ := mgr.CreateRunbook(ctx, &Runbook{
		Name:             "prod-runbook",
		RequiresApproval: true,
		Steps:            []RunbookStep{{Order: 1, Name: "Step 1", Type: StepTypeCommand}},
		Owner:            "test",
	})

	exec, err := mgr.ExecuteRunbook(ctx, rb.ID, "admin")
	if err != nil {
		t.Fatalf("ExecuteRunbook failed: %v", err)
	}
	if exec.Status != ExecutionApprovalNeeded {
		t.Errorf("expected approval-needed, got %s", exec.Status)
	}
}

func TestCreateIncident(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	incident, err := mgr.CreateIncident(ctx, &Incident{
		Title:     "Production database connection pool exhaustion",
		Severity:  SeverityP1High,
		Commander: "alice",
		Responders: []string{"alice", "bob"},
	})
	if err != nil {
		t.Fatalf("CreateIncident failed: %v", err)
	}
	if incident.Status != IncidentOpen {
		t.Errorf("expected open status, got %s", incident.Status)
	}
	if len(incident.Timeline) != 1 {
		t.Errorf("expected 1 timeline entry, got %d", len(incident.Timeline))
	}
}

func TestIncidentLifecycle(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	incident, _ := mgr.CreateIncident(ctx, &Incident{
		Title:    "API Server Down",
		Severity: SeverityP0Critical,
		Commander: "alice",
	})

	// Investigate
	mgr.UpdateIncidentStatus(ctx, incident.ID, IncidentInvestigating, "Root cause analysis in progress")

	// Mitigate
	mgr.UpdateIncidentStatus(ctx, incident.ID, IncidentMitigated, "Traffic rerouted to backup cluster")

	// Resolve
	mgr.UpdateIncidentStatus(ctx, incident.ID, IncidentResolved, "Root cause fixed and deployed")

	updated, _ := mgr.GetIncident(ctx, incident.ID)
	if updated.Status != IncidentResolved {
		t.Errorf("expected resolved, got %s", updated.Status)
	}
	if updated.MitigatedAt == nil {
		t.Error("expected mitigated_at to be set")
	}
	if updated.ResolvedAt == nil {
		t.Error("expected resolved_at to be set")
	}
	if len(updated.Timeline) != 4 { // created + 3 status changes
		t.Errorf("expected 4 timeline entries, got %d", len(updated.Timeline))
	}
}

func TestAddRetrospective(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	incident, _ := mgr.CreateIncident(ctx, &Incident{
		Title:    "Cache Failure",
		Severity: SeverityP1High,
		Commander: "bob",
	})

	err := mgr.AddRetrospective(ctx, incident.ID, &Retrospective{
		Summary:   "Redis cluster failover caused 15min downtime",
		RootCause: "Redis Sentinel misconfiguration led to split-brain during network partition",
		Contributing: []string{
			"No automated failover testing",
			"Monitoring gap for Sentinel quorum",
		},
		WhatWentWell: []string{
			"Incident commander was on-call and responded within 2 minutes",
			"Runbook for manual failover worked correctly",
		},
		WhatWentWrong: []string{
			"Automated failover failed due to stale config",
			"Alert fired 5 minutes late",
		},
		ActionItems: []ActionItem{
			{Description: "Add Sentinel quorum monitoring", Owner: "alice", Priority: "high"},
			{Description: "Implement chaos testing for Redis failover", Owner: "bob", Priority: "medium"},
		},
		LessonsLearned: []string{
			"Automated failover must be tested monthly",
			"Cache tier needs circuit breaker pattern",
		},
		ReviewedBy: []string{"alice", "bob", "charlie"},
	})
	if err != nil {
		t.Fatalf("AddRetrospective failed: %v", err)
	}

	updated, _ := mgr.GetIncident(ctx, incident.ID)
	if updated.Retrospective == nil {
		t.Fatal("expected retrospective to be set")
	}
	if len(updated.Retrospective.ActionItems) != 2 {
		t.Errorf("expected 2 action items, got %d", len(updated.Retrospective.ActionItems))
	}
	if updated.Status != IncidentPostMortem {
		t.Errorf("expected post-mortem status, got %s", updated.Status)
	}
}

func TestSetIncidentImpact(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	incident, _ := mgr.CreateIncident(ctx, &Incident{
		Title: "Outage", Severity: SeverityP0Critical, Commander: "alice",
	})

	err := mgr.SetIncidentImpact(ctx, incident.ID, &IncidentImpact{
		Duration:         45 * time.Minute,
		AffectedServices: []string{"apiserver", "scheduler"},
		AffectedUsers:    5000,
		ErrorBudgetBurn:  25.0,
		EstimatedCostUSD: 15000,
	})
	if err != nil {
		t.Fatalf("SetIncidentImpact failed: %v", err)
	}

	updated, _ := mgr.GetIncident(ctx, incident.ID)
	if updated.Impact == nil {
		t.Fatal("expected impact to be set")
	}
	if updated.Impact.AffectedUsers != 5000 {
		t.Errorf("expected 5000 affected users, got %d", updated.Impact.AffectedUsers)
	}
}

func TestListIncidents(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	mgr.CreateIncident(ctx, &Incident{Title: "Inc 1", Severity: SeverityP1High, Commander: "a"})
	mgr.CreateIncident(ctx, &Incident{Title: "Inc 2", Severity: SeverityP2Medium, Commander: "b"})

	all, _ := mgr.ListIncidents(ctx, "")
	if len(all) != 2 {
		t.Errorf("expected 2 incidents, got %d", len(all))
	}

	open, _ := mgr.ListIncidents(ctx, IncidentOpen)
	if len(open) != 2 {
		t.Errorf("expected 2 open incidents, got %d", len(open))
	}
}

func TestAlertRunbookAutoExecute(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	mgr.CreateRunbook(ctx, &Runbook{
		Name:          "auto-fix",
		TriggerAlerts: []string{"GPUHighTemp"},
		AutoExecute:   true,
		Steps:         []RunbookStep{{Order: 1, Name: "throttle", Type: StepTypeCommand}},
		Owner:         "ops",
	})

	alert, _ := mgr.ClassifyAndRouteAlert(ctx, &ClassifiedAlert{
		Name:     "GPUHighTemp",
		Severity: SeverityP1High,
		Summary:  "GPU temperature critical",
	})

	if alert.RunbookURL == "" {
		t.Error("expected runbook URL to be set for matching alert")
	}

	// Wait briefly for async execution
	time.Sleep(100 * time.Millisecond)

	executions, _ := mgr.ListExecutions(ctx, "")
	if len(executions) == 0 {
		t.Error("expected auto-executed runbook")
	}
}

func TestCancelledContext(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.CreateRoutingRule(ctx, &RoutingRule{Name: "test"})
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}

	_, err = mgr.CreateIncident(ctx, &Incident{Title: "test"})
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}

func TestCreateEscalationPolicy(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	policy, err := mgr.CreateEscalationPolicy(ctx, &EscalationPolicy{
		Name: "default-escalation",
		Levels: []EscalationLevel{
			{Level: 1, Delay: 5 * time.Minute, Targets: []string{"on-call-primary"}, NotifyOnCall: true},
			{Level: 2, Delay: 15 * time.Minute, Targets: []string{"team-lead"}, Channels: []AlertChannel{ChannelPhoneCall}},
			{Level: 3, Delay: 30 * time.Minute, Targets: []string{"engineering-director"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateEscalationPolicy failed: %v", err)
	}
	if len(policy.Levels) != 3 {
		t.Errorf("expected 3 levels, got %d", len(policy.Levels))
	}
}
