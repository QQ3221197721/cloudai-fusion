package enterprise

import (
	"context"
	"testing"
	"time"
)

func newTestManager() *Manager {
	return NewManager(ManagerConfig{})
}

// ============================================================================
// SSO Tests
// ============================================================================

func TestConfigureSAMLSSO(t *testing.T) {
	m := newTestManager()

	err := m.ConfigureSSO(context.Background(), &SSOConfig{
		TenantID: "tenant-1",
		Name:     "Corp SAML",
		Protocol: SSOProtocolSAML,
		RoleMapping: map[string]string{
			"admins":     "admin",
			"developers": "developer",
		},
		SAML: &SAMLConfig{
			EntityID:   "https://idp.corp.com",
			SSOURL:     "https://idp.corp.com/sso",
			SPEntityID: "cloudai-fusion",
			ACSURL:     "https://cloudai.io/auth/saml/callback",
		},
	})
	if err != nil {
		t.Fatalf("ConfigureSSO(SAML) failed: %v", err)
	}

	configs := m.ListSSOConfigs(context.Background(), "tenant-1")
	if len(configs) != 1 {
		t.Fatalf("expected 1 SSO config, got %d", len(configs))
	}
	if configs[0].Protocol != SSOProtocolSAML {
		t.Errorf("expected SAML protocol, got %q", configs[0].Protocol)
	}
}

func TestConfigureLDAPSSO(t *testing.T) {
	m := newTestManager()

	err := m.ConfigureSSO(context.Background(), &SSOConfig{
		TenantID: "tenant-2",
		Name:     "Corp LDAP",
		Protocol: SSOProtocolLDAP,
		LDAP: &LDAPConfig{
			Host:           "ldap.corp.com",
			Port:           636,
			UseTLS:         true,
			BaseDN:         "dc=corp,dc=com",
			UserSearchBase: "ou=users,dc=corp,dc=com",
			UserFilter:     "(uid=%s)",
		},
	})
	if err != nil {
		t.Fatalf("ConfigureSSO(LDAP) failed: %v", err)
	}

	configs := m.ListSSOConfigs(context.Background(), "tenant-2")
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
}

func TestConfigureSSOValidation(t *testing.T) {
	m := newTestManager()

	// Missing tenant ID
	err := m.ConfigureSSO(context.Background(), &SSOConfig{Protocol: SSOProtocolSAML})
	if err == nil {
		t.Error("expected error for missing tenant_id")
	}

	// Missing protocol
	err = m.ConfigureSSO(context.Background(), &SSOConfig{TenantID: "t1"})
	if err == nil {
		t.Error("expected error for missing protocol")
	}

	// SAML without config
	err = m.ConfigureSSO(context.Background(), &SSOConfig{TenantID: "t1", Protocol: SSOProtocolSAML})
	if err == nil {
		t.Error("expected error for SAML without config")
	}

	// LDAP without config
	err = m.ConfigureSSO(context.Background(), &SSOConfig{TenantID: "t1", Protocol: SSOProtocolLDAP})
	if err == nil {
		t.Error("expected error for LDAP without config")
	}
}

func TestAuthenticateSSO(t *testing.T) {
	m := newTestManager()

	m.ConfigureSSO(context.Background(), &SSOConfig{
		TenantID: "t1", Name: "Test IdP", Protocol: SSOProtocolSAML,
		RoleMapping: map[string]string{"admins": "admin"},
		SAML: &SAMLConfig{EntityID: "https://idp.test", SSOURL: "https://idp.test/sso"},
	})

	configs := m.ListSSOConfigs(context.Background(), "t1")
	result, err := m.AuthenticateSSO(context.Background(), configs[0].ID, "john")
	if err != nil {
		t.Fatalf("AuthenticateSSO failed: %v", err)
	}

	if result.Email != "john@company.com" {
		t.Errorf("expected john@company.com, got %q", result.Email)
	}
	if result.MappedRole != "admin" {
		t.Errorf("expected admin role, got %q", result.MappedRole)
	}
	if result.Protocol != SSOProtocolSAML {
		t.Errorf("expected SAML, got %q", result.Protocol)
	}
}

func TestDeleteSSO(t *testing.T) {
	m := newTestManager()

	m.ConfigureSSO(context.Background(), &SSOConfig{
		TenantID: "t1", Name: "Del", Protocol: SSOProtocolOIDC,
	})

	configs := m.ListSSOConfigs(context.Background(), "t1")
	err := m.DeleteSSO(context.Background(), configs[0].ID)
	if err != nil {
		t.Fatalf("DeleteSSO failed: %v", err)
	}

	configs = m.ListSSOConfigs(context.Background(), "t1")
	if len(configs) != 0 {
		t.Errorf("expected 0 configs after delete, got %d", len(configs))
	}
}

// ============================================================================
// Audit Report Tests
// ============================================================================

func TestGenerateAuditReport(t *testing.T) {
	m := newTestManager()
	now := time.Now()

	report, err := m.GenerateAuditReport(
		context.Background(), "tenant-1", ReportTypeAccess,
		now.AddDate(0, -1, 0), now, "admin",
	)
	if err != nil {
		t.Fatalf("GenerateAuditReport failed: %v", err)
	}

	if report.Status != "ready" {
		t.Errorf("expected ready status, got %q", report.Status)
	}
	if report.Summary.TotalEvents == 0 {
		t.Error("expected non-zero total events")
	}
	if len(report.Sections) == 0 {
		t.Error("expected report sections")
	}
}

func TestListAuditReports(t *testing.T) {
	m := newTestManager()
	now := time.Now()

	m.GenerateAuditReport(context.Background(), "t1", ReportTypeAccess, now, now, "admin")
	m.GenerateAuditReport(context.Background(), "t1", ReportTypeSecurity, now, now, "admin")

	reports := m.ListAuditReports(context.Background(), "t1")
	if len(reports) != 2 {
		t.Errorf("expected 2 reports, got %d", len(reports))
	}
}

func TestGetAuditReport(t *testing.T) {
	m := newTestManager()
	now := time.Now()

	created, _ := m.GenerateAuditReport(context.Background(), "t1", ReportTypeCompliance, now, now, "admin")
	got, err := m.GetAuditReport(context.Background(), "t1", created.ID)
	if err != nil {
		t.Fatalf("GetAuditReport failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch")
	}
}

func TestScheduleReport(t *testing.T) {
	m := newTestManager()

	sr, err := m.ScheduleReport(context.Background(), "t1", ReportTypeAccess, "weekly", []string{"admin@corp.com"})
	if err != nil {
		t.Fatalf("ScheduleReport failed: %v", err)
	}
	if sr.Schedule != "weekly" {
		t.Errorf("expected weekly, got %q", sr.Schedule)
	}
	if !sr.Enabled {
		t.Error("expected enabled")
	}
}

func TestScheduleReportInvalid(t *testing.T) {
	m := newTestManager()
	_, err := m.ScheduleReport(context.Background(), "t1", ReportTypeAccess, "hourly", nil)
	if err == nil {
		t.Error("expected error for invalid schedule")
	}
}

// ============================================================================
// SLA Tests
// ============================================================================

func TestCreateSLAContract(t *testing.T) {
	m := newTestManager()

	contract, err := m.CreateSLAContract(context.Background(), "t1", SLATierEnterprise)
	if err != nil {
		t.Fatalf("CreateSLAContract failed: %v", err)
	}

	if contract.AvailabilityTarget != 0.9999 {
		t.Errorf("expected 0.9999, got %f", contract.AvailabilityTarget)
	}
	if contract.SupportHours != "24x7" {
		t.Errorf("expected 24x7, got %q", contract.SupportHours)
	}
	if !contract.CreditPolicy.Enabled {
		t.Error("expected credit policy enabled")
	}
}

func TestSLAReportMet(t *testing.T) {
	m := newTestManager()
	m.CreateSLAContract(context.Background(), "t1", SLATierEnterprise)

	report, err := m.RecordSLAReport(context.Background(), "t1", "2026-02",
		0.9999, 4.32, nil)
	if err != nil {
		t.Fatalf("RecordSLAReport failed: %v", err)
	}
	if !report.IsMet {
		t.Error("expected SLA to be met")
	}
	if report.CreditDue != 0 {
		t.Errorf("expected 0 credit, got %f", report.CreditDue)
	}
}

func TestSLAReportMissed(t *testing.T) {
	m := newTestManager()
	m.CreateSLAContract(context.Background(), "t1", SLATierEnterprise)

	report, err := m.RecordSLAReport(context.Background(), "t1", "2026-03",
		0.9985, 60.0, []SLAIncident{
			{ID: "inc-1", Service: "apiserver", Duration: 30 * time.Minute, Impact: "partial"},
		})
	if err != nil {
		t.Fatalf("RecordSLAReport failed: %v", err)
	}
	if report.IsMet {
		t.Error("expected SLA to be missed")
	}
	if report.CreditDue <= 0 {
		t.Error("expected positive credit due")
	}
}

func TestGetSLAReports(t *testing.T) {
	m := newTestManager()
	m.CreateSLAContract(context.Background(), "t1", SLATierBusiness)
	m.RecordSLAReport(context.Background(), "t1", "2026-01", 0.9995, 2.0, nil)
	m.RecordSLAReport(context.Background(), "t1", "2026-02", 0.998, 8.0, nil)

	reports := m.GetSLAReports(context.Background(), "t1")
	if len(reports) != 2 {
		t.Errorf("expected 2 reports, got %d", len(reports))
	}
}

func TestDefaultSLAContractTiers(t *testing.T) {
	tests := []struct {
		tier   SLATier
		target float64
	}{
		{SLATierStandard, 0.995},
		{SLATierBusiness, 0.999},
		{SLATierPremium, 0.9995},
		{SLATierEnterprise, 0.9999},
	}
	for _, tt := range tests {
		c := DefaultSLAContract(tt.tier)
		if c.AvailabilityTarget != tt.target {
			t.Errorf("tier %s: expected target %f, got %f", tt.tier, tt.target, c.AvailabilityTarget)
		}
	}
}

// ============================================================================
// Ticket System Tests
// ============================================================================

func TestCreateTicket(t *testing.T) {
	m := newTestManager()

	err := m.CreateTicket(context.Background(), "t1", &SupportTicket{
		Title:       "GPU scheduling issue",
		Description: "Jobs stuck in pending state",
		Priority:    PriorityCritical,
		Category:    CategoryCluster,
		CreatedBy:   "user-1",
	})
	if err != nil {
		t.Fatalf("CreateTicket failed: %v", err)
	}

	tickets := m.ListTickets(context.Background(), "t1")
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].Status != TicketStatusOpen {
		t.Errorf("expected open status, got %q", tickets[0].Status)
	}
	if tickets[0].SLADeadline == nil {
		t.Error("expected SLA deadline to be set")
	}
}

func TestCreateTicketValidation(t *testing.T) {
	m := newTestManager()
	err := m.CreateTicket(context.Background(), "t1", &SupportTicket{})
	if err == nil {
		t.Error("expected error for empty title")
	}
}

func TestTicketLifecycle(t *testing.T) {
	m := newTestManager()

	m.CreateTicket(context.Background(), "t1", &SupportTicket{
		Title: "API error", Priority: PriorityHigh, Category: CategoryAPI, CreatedBy: "u1",
	})
	tickets := m.ListTickets(context.Background(), "t1")
	ticketID := tickets[0].ID

	// Assign
	err := m.AssignTicket(context.Background(), ticketID, "engineer-1")
	if err != nil {
		t.Fatalf("AssignTicket failed: %v", err)
	}

	ticket, _ := m.GetTicket(context.Background(), ticketID)
	if ticket.Status != TicketStatusAssigned {
		t.Errorf("expected assigned, got %q", ticket.Status)
	}

	// Comment
	err = m.AddComment(context.Background(), ticketID, TicketComment{
		Author: "engineer-1", IsStaff: true, Content: "Looking into this",
	})
	if err != nil {
		t.Fatalf("AddComment failed: %v", err)
	}

	ticket, _ = m.GetTicket(context.Background(), ticketID)
	if ticket.FirstResponseAt == nil {
		t.Error("expected FirstResponseAt after staff comment")
	}
	if len(ticket.Comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(ticket.Comments))
	}

	// Resolve
	err = m.ResolveTicket(context.Background(), ticketID, "Fixed by restarting scheduler")
	if err != nil {
		t.Fatalf("ResolveTicket failed: %v", err)
	}

	ticket, _ = m.GetTicket(context.Background(), ticketID)
	if ticket.Status != TicketStatusResolved {
		t.Errorf("expected resolved, got %q", ticket.Status)
	}
	if ticket.Resolution != "Fixed by restarting scheduler" {
		t.Error("expected resolution message")
	}

	// Rate
	err = m.RateTicket(context.Background(), ticketID, 5)
	if err != nil {
		t.Fatalf("RateTicket failed: %v", err)
	}
}

func TestRateTicketInvalid(t *testing.T) {
	m := newTestManager()
	m.CreateTicket(context.Background(), "t1", &SupportTicket{
		Title: "Test", Priority: PriorityLow, CreatedBy: "u1",
	})
	tickets := m.ListTickets(context.Background(), "t1")

	err := m.RateTicket(context.Background(), tickets[0].ID, 6)
	if err == nil {
		t.Error("expected error for invalid rating")
	}
}

func TestTicketMetrics(t *testing.T) {
	m := newTestManager()

	m.CreateTicket(context.Background(), "t1", &SupportTicket{
		Title: "T1", Priority: PriorityCritical, Category: CategoryCluster, CreatedBy: "u1",
	})
	m.CreateTicket(context.Background(), "t1", &SupportTicket{
		Title: "T2", Priority: PriorityLow, Category: CategoryBilling, CreatedBy: "u2",
	})

	tickets := m.ListTickets(context.Background(), "t1")
	m.AddComment(context.Background(), tickets[0].ID, TicketComment{
		Author: "eng", IsStaff: true, Content: "On it",
	})
	m.ResolveTicket(context.Background(), tickets[0].ID, "Fixed")
	m.RateTicket(context.Background(), tickets[0].ID, 4)

	metrics := m.GetTicketMetrics(context.Background(), "t1")
	if metrics.TotalTickets != 2 {
		t.Errorf("expected 2 total, got %d", metrics.TotalTickets)
	}
	if metrics.ResolvedTickets != 1 {
		t.Errorf("expected 1 resolved, got %d", metrics.ResolvedTickets)
	}
	if metrics.OpenTickets != 1 {
		t.Errorf("expected 1 open, got %d", metrics.OpenTickets)
	}
	if metrics.SatisfactionAvg != 4.0 {
		t.Errorf("expected satisfaction 4.0, got %f", metrics.SatisfactionAvg)
	}
}

func TestTicketSLAWithContract(t *testing.T) {
	m := newTestManager()
	m.CreateSLAContract(context.Background(), "t1", SLATierEnterprise)

	m.CreateTicket(context.Background(), "t1", &SupportTicket{
		Title: "Critical issue", Priority: PriorityCritical, CreatedBy: "u1",
	})

	tickets := m.ListTickets(context.Background(), "t1")
	if tickets[0].SLADeadline == nil {
		t.Fatal("expected SLA deadline")
	}

	// Enterprise critical = 15min SLA
	deadline := *tickets[0].SLADeadline
	diff := deadline.Sub(tickets[0].CreatedAt)
	if diff < 14*time.Minute || diff > 16*time.Minute {
		t.Errorf("expected ~15min SLA deadline, got %v", diff)
	}
}

func TestCancelledContext(t *testing.T) {
	m := newTestManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.ConfigureSSO(ctx, &SSOConfig{TenantID: "t1", Protocol: SSOProtocolSAML})
	if err == nil {
		t.Error("expected error for cancelled context")
	}

	_, err = m.CreateSLAContract(ctx, "t1", SLATierStandard)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
