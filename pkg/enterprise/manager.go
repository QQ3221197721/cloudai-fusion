package enterprise

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ManagerConfig holds enterprise manager configuration
type ManagerConfig struct {
	SLACheckInterval time.Duration `json:"sla_check_interval"`
}

// Manager provides enterprise feature management
type Manager struct {
	ssoConfigs      map[string]*SSOConfig
	reports         map[string][]*AuditReport
	scheduled       map[string]*ScheduledReport
	contracts       map[string]*SLAContract
	slaReports      map[string][]*SLAReport
	tickets         map[string]*SupportTicket
	ticketsByTenant map[string][]string
	config          ManagerConfig
	logger          *logrus.Logger
	mu              sync.RWMutex
}

// NewManager creates a new enterprise manager
func NewManager(cfg ManagerConfig) *Manager {
	if cfg.SLACheckInterval == 0 {
		cfg.SLACheckInterval = 5 * time.Minute
	}
	return &Manager{
		ssoConfigs:      make(map[string]*SSOConfig),
		reports:         make(map[string][]*AuditReport),
		scheduled:       make(map[string]*ScheduledReport),
		contracts:       make(map[string]*SLAContract),
		slaReports:      make(map[string][]*SLAReport),
		tickets:         make(map[string]*SupportTicket),
		ticketsByTenant: make(map[string][]string),
		config:          cfg,
		logger:          logrus.StandardLogger(),
	}
}

// ============================================================================
// SSO Integration
// ============================================================================

// ConfigureSSO sets up SSO for a tenant
func (m *Manager) ConfigureSSO(ctx context.Context, cfg *SSOConfig) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if cfg.Protocol == "" {
		return fmt.Errorf("SSO protocol is required")
	}
	switch cfg.Protocol {
	case SSOProtocolSAML:
		if cfg.SAML == nil || cfg.SAML.EntityID == "" || cfg.SAML.SSOURL == "" {
			return fmt.Errorf("SAML entity_id and sso_url are required")
		}
	case SSOProtocolLDAP:
		if cfg.LDAP == nil || cfg.LDAP.Host == "" || cfg.LDAP.BaseDN == "" {
			return fmt.Errorf("LDAP host and base_dn are required")
		}
	}

	now := common.NowUTC()
	cfg.ID = common.NewUUID()
	cfg.Enabled = true
	cfg.CreatedAt = now
	cfg.UpdatedAt = now
	if cfg.DefaultRole == "" {
		cfg.DefaultRole = "viewer"
	}

	m.ssoConfigs[cfg.ID] = cfg

	m.logger.WithFields(logrus.Fields{
		"sso_id": cfg.ID, "tenant_id": cfg.TenantID,
		"protocol": cfg.Protocol, "name": cfg.Name,
	}).Info("SSO configured")

	return nil
}

// GetSSOConfig returns SSO configuration by ID
func (m *Manager) GetSSOConfig(_ context.Context, ssoID string) (*SSOConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.ssoConfigs[ssoID]
	if !ok {
		return nil, fmt.Errorf("SSO config %q not found", ssoID)
	}
	return cfg, nil
}

// ListSSOConfigs returns all SSO configs for a tenant
func (m *Manager) ListSSOConfigs(_ context.Context, tenantID string) []*SSOConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*SSOConfig
	for _, cfg := range m.ssoConfigs {
		if cfg.TenantID == tenantID {
			result = append(result, cfg)
		}
	}
	return result
}

// AuthenticateSSO simulates SSO authentication
func (m *Manager) AuthenticateSSO(ctx context.Context, ssoID, username string) (*SSOLoginResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.ssoConfigs[ssoID]
	if !ok {
		return nil, fmt.Errorf("SSO config %q not found", ssoID)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("SSO provider %q is disabled", cfg.Name)
	}

	role := cfg.DefaultRole
	groups := []string{"users"}
	for group, mappedRole := range cfg.RoleMapping {
		groups = append(groups, group)
		role = mappedRole
	}

	now := common.NowUTC()
	cfg.LastSyncAt = &now

	result := &SSOLoginResult{
		UserID:      common.NewUUID(),
		Email:       fmt.Sprintf("%s@company.com", username),
		DisplayName: username,
		Groups:      groups,
		MappedRole:  role,
		Provider:    cfg.Name,
		Protocol:    cfg.Protocol,
		SessionID:   common.NewUUID(),
		AuthTime:    now,
	}

	m.logger.WithFields(logrus.Fields{
		"sso_id": ssoID, "protocol": cfg.Protocol,
		"user": username, "role": role,
	}).Info("SSO authentication successful")

	return result, nil
}

// DeleteSSO removes an SSO configuration
func (m *Manager) DeleteSSO(_ context.Context, ssoID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ssoConfigs[ssoID]; !ok {
		return fmt.Errorf("SSO config %q not found", ssoID)
	}
	delete(m.ssoConfigs, ssoID)
	return nil
}

// ============================================================================
// Audit Reports
// ============================================================================

// GenerateAuditReport creates an audit report
func (m *Manager) GenerateAuditReport(ctx context.Context, tenantID string, reportType AuditReportType, start, end time.Time, generatedBy string) (*AuditReport, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	report := &AuditReport{
		ID: common.NewUUID(), TenantID: tenantID, Type: reportType,
		Title:  fmt.Sprintf("%s Audit Report", reportType),
		Period: ReportPeriod{Start: start, End: end},
		Status: "ready",
		Summary: ReportSummary{
			TotalEvents: 150, UniqueUsers: 12,
			ByAction:  map[string]int{"create": 45, "read": 60, "update": 30, "delete": 15},
			ByOutcome: map[string]int{"success": 140, "failure": 10},
			RiskScore: 0.15, ComplianceScore: 0.95,
		},
		Sections: []ReportSection{
			{Title: "Activity Summary", Type: "table", Data: []map[string]interface{}{
				{"period": start.Format("2006-01-02"), "events": 150, "users": 12},
			}},
			{Title: "Security Events", Type: "metrics", Data: []map[string]interface{}{
				{"metric": "failed_logins", "value": 5},
				{"metric": "permission_denials", "value": 3},
			}},
		},
		GeneratedBy: generatedBy, GeneratedAt: now,
		ExpiresAt: now.AddDate(0, 3, 0),
	}

	m.reports[tenantID] = append(m.reports[tenantID], report)

	m.logger.WithFields(logrus.Fields{
		"report_id": report.ID, "tenant_id": tenantID, "type": reportType,
	}).Info("Audit report generated")

	return report, nil
}

// GetAuditReport returns a specific audit report
func (m *Manager) GetAuditReport(_ context.Context, tenantID, reportID string) (*AuditReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.reports[tenantID] {
		if r.ID == reportID {
			return r, nil
		}
	}
	return nil, fmt.Errorf("report %q not found", reportID)
}

// ListAuditReports returns all reports for a tenant
func (m *Manager) ListAuditReports(_ context.Context, tenantID string) []*AuditReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reports[tenantID]
}

// ScheduleReport creates a recurring report schedule
func (m *Manager) ScheduleReport(_ context.Context, tenantID string, reportType AuditReportType, schedule string, recipients []string) (*ScheduledReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := common.NowUTC()
	var nextRun time.Time
	switch schedule {
	case "daily":
		nextRun = now.Add(24 * time.Hour)
	case "weekly":
		nextRun = now.Add(7 * 24 * time.Hour)
	case "monthly":
		nextRun = now.AddDate(0, 1, 0)
	default:
		return nil, fmt.Errorf("invalid schedule: %s", schedule)
	}

	sr := &ScheduledReport{
		ID: common.NewUUID(), TenantID: tenantID, Type: reportType,
		Schedule: schedule, Recipients: recipients, Enabled: true, NextRunAt: nextRun,
	}
	m.scheduled[sr.ID] = sr

	m.logger.WithFields(logrus.Fields{
		"schedule_id": sr.ID, "type": reportType, "schedule": schedule,
	}).Info("Report schedule created")

	return sr, nil
}

// ============================================================================
// SLA Management
// ============================================================================

// CreateSLAContract creates an SLA contract for a tenant
func (m *Manager) CreateSLAContract(ctx context.Context, tenantID string, tier SLATier) (*SLAContract, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	contract := DefaultSLAContract(tier)
	contract.ID = common.NewUUID()
	contract.TenantID = tenantID
	contract.Status = "active"
	contract.EffectiveFrom = common.NowUTC()
	contract.EffectiveTo = common.NowUTC().AddDate(1, 0, 0)

	m.contracts[tenantID] = &contract

	m.logger.WithFields(logrus.Fields{
		"contract_id": contract.ID, "tenant_id": tenantID,
		"tier": tier, "target": contract.AvailabilityTarget,
	}).Info("SLA contract created")

	return &contract, nil
}

// GetSLAContract returns the SLA contract for a tenant
func (m *Manager) GetSLAContract(_ context.Context, tenantID string) (*SLAContract, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	contract, ok := m.contracts[tenantID]
	if !ok {
		return nil, fmt.Errorf("no SLA contract for tenant %q", tenantID)
	}
	return contract, nil
}

// RecordSLAReport records SLA compliance for a period
func (m *Manager) RecordSLAReport(ctx context.Context, tenantID, period string, availability, downtimeMinutes float64, incidents []SLAIncident) (*SLAReport, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	contract, ok := m.contracts[tenantID]
	if !ok {
		return nil, fmt.Errorf("no SLA contract for tenant %q", tenantID)
	}

	isMet := availability >= contract.AvailabilityTarget
	creditDue := 0.0
	if !isMet && contract.CreditPolicy.Enabled {
		for _, t := range contract.CreditPolicy.Tiers {
			if availability >= t.MinAvailability && availability < t.MaxAvailability {
				creditDue = t.CreditPercent
				break
			}
		}
		creditDue = math.Min(creditDue, contract.CreditPolicy.MaxCredit)
	}

	report := &SLAReport{
		ID: common.NewUUID(), TenantID: tenantID, ContractID: contract.ID,
		Period: period, Availability: availability, Target: contract.AvailabilityTarget,
		IsMet: isMet, DowntimeMinutes: downtimeMinutes, Incidents: incidents,
		CreditDue: creditDue, GeneratedAt: common.NowUTC(),
	}
	m.slaReports[tenantID] = append(m.slaReports[tenantID], report)

	entry := m.logger.WithFields(logrus.Fields{
		"tenant_id": tenantID, "availability": availability, "target": contract.AvailabilityTarget, "is_met": isMet,
	})
	if isMet {
		entry.Info("SLA target met")
	} else {
		entry.WithField("credit_due", creditDue).Warn("SLA target missed")
	}

	return report, nil
}

// GetSLAReports returns SLA reports for a tenant
func (m *Manager) GetSLAReports(_ context.Context, tenantID string) []*SLAReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.slaReports[tenantID]
}

// ============================================================================
// Support Ticket System
// ============================================================================

// CreateTicket creates a new support ticket
func (m *Manager) CreateTicket(ctx context.Context, tenantID string, ticket *SupportTicket) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if ticket.Title == "" {
		return fmt.Errorf("ticket title is required")
	}

	now := common.NowUTC()
	ticket.ID = common.NewUUID()
	ticket.TenantID = tenantID
	ticket.Status = TicketStatusOpen
	ticket.CreatedAt = now
	ticket.UpdatedAt = now
	if ticket.Comments == nil {
		ticket.Comments = []TicketComment{}
	}

	// Set SLA deadline based on priority
	deadline := m.calculateSLADeadline(tenantID, ticket.Priority, now)
	if deadline != nil {
		ticket.SLADeadline = deadline
	}

	m.tickets[ticket.ID] = ticket
	m.ticketsByTenant[tenantID] = append(m.ticketsByTenant[tenantID], ticket.ID)

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticket.ID, "tenant_id": tenantID,
		"priority": ticket.Priority, "category": ticket.Category,
	}).Info("Support ticket created")

	return nil
}

// GetTicket returns a ticket by ID
func (m *Manager) GetTicket(_ context.Context, ticketID string) (*SupportTicket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ticket, ok := m.tickets[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %q not found", ticketID)
	}
	return ticket, nil
}

// ListTickets returns all tickets for a tenant
func (m *Manager) ListTickets(_ context.Context, tenantID string) []*SupportTicket {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*SupportTicket
	for _, id := range m.ticketsByTenant[tenantID] {
		if t, ok := m.tickets[id]; ok {
			result = append(result, t)
		}
	}
	return result
}

// UpdateTicketStatus updates the status of a ticket
func (m *Manager) UpdateTicketStatus(_ context.Context, ticketID string, status TicketStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}

	now := common.NowUTC()
	oldStatus := ticket.Status
	ticket.Status = status
	ticket.UpdatedAt = now

	switch status {
	case TicketStatusResolved:
		ticket.ResolvedAt = &now
	case TicketStatusClosed:
		ticket.ClosedAt = &now
	}

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticketID, "from": oldStatus, "to": status,
	}).Info("Ticket status updated")

	return nil
}

// AssignTicket assigns a ticket to a staff member
func (m *Manager) AssignTicket(_ context.Context, ticketID, assignee string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}

	ticket.AssignedTo = assignee
	ticket.Status = TicketStatusAssigned
	ticket.UpdatedAt = common.NowUTC()

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticketID, "assignee": assignee,
	}).Info("Ticket assigned")

	return nil
}

// AddComment adds a comment to a ticket
func (m *Manager) AddComment(_ context.Context, ticketID string, comment TicketComment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}

	comment.ID = common.NewUUID()
	comment.CreatedAt = common.NowUTC()
	ticket.Comments = append(ticket.Comments, comment)
	ticket.UpdatedAt = comment.CreatedAt

	// Track first response
	if comment.IsStaff && ticket.FirstResponseAt == nil {
		ticket.FirstResponseAt = &comment.CreatedAt
	}

	return nil
}

// ResolveTicket resolves a ticket with a resolution message
func (m *Manager) ResolveTicket(_ context.Context, ticketID, resolution string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}

	now := common.NowUTC()
	ticket.Status = TicketStatusResolved
	ticket.Resolution = resolution
	ticket.ResolvedAt = &now
	ticket.UpdatedAt = now

	// Check SLA breach
	if ticket.SLADeadline != nil && now.After(*ticket.SLADeadline) {
		ticket.SLABreached = true
	}

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticketID, "sla_breached": ticket.SLABreached,
	}).Info("Ticket resolved")

	return nil
}

// RateTicket records customer satisfaction rating
func (m *Manager) RateTicket(_ context.Context, ticketID string, rating int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %q not found", ticketID)
	}
	if rating < 1 || rating > 5 {
		return fmt.Errorf("rating must be between 1 and 5")
	}

	ticket.Satisfaction = &rating
	return nil
}

// GetTicketMetrics returns ticket performance metrics
func (m *Manager) GetTicketMetrics(_ context.Context, tenantID string) *TicketMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := &TicketMetrics{
		TenantID:   tenantID,
		ByPriority: make(map[string]int),
		ByCategory: make(map[string]int),
	}

	var totalFirstResponse, totalResolution float64
	var firstResponseCount, resolutionCount int
	var satisfactionSum float64
	var satisfactionCount int

	for _, id := range m.ticketsByTenant[tenantID] {
		t, ok := m.tickets[id]
		if !ok {
			continue
		}

		metrics.TotalTickets++
		metrics.ByPriority[string(t.Priority)]++
		metrics.ByCategory[string(t.Category)]++

		if t.Status == TicketStatusResolved || t.Status == TicketStatusClosed {
			metrics.ResolvedTickets++
		} else {
			metrics.OpenTickets++
		}

		if t.FirstResponseAt != nil {
			totalFirstResponse += t.FirstResponseAt.Sub(t.CreatedAt).Hours()
			firstResponseCount++
		}
		if t.ResolvedAt != nil {
			totalResolution += t.ResolvedAt.Sub(t.CreatedAt).Hours()
			resolutionCount++
		}
		if t.Satisfaction != nil {
			satisfactionSum += float64(*t.Satisfaction)
			satisfactionCount++
		}
	}

	if firstResponseCount > 0 {
		metrics.AvgFirstResponseHours = totalFirstResponse / float64(firstResponseCount)
	}
	if resolutionCount > 0 {
		metrics.AvgResolutionHours = totalResolution / float64(resolutionCount)
	}
	if satisfactionCount > 0 {
		metrics.SatisfactionAvg = satisfactionSum / float64(satisfactionCount)
	}

	// SLA compliance
	totalResolvable := 0
	slaCompliant := 0
	for _, id := range m.ticketsByTenant[tenantID] {
		t := m.tickets[id]
		if t != nil && t.SLADeadline != nil && (t.Status == TicketStatusResolved || t.Status == TicketStatusClosed) {
			totalResolvable++
			if !t.SLABreached {
				slaCompliant++
			}
		}
	}
	if totalResolvable > 0 {
		metrics.SLACompliancePercent = float64(slaCompliant) / float64(totalResolvable) * 100
	}

	return metrics
}

func (m *Manager) calculateSLADeadline(tenantID string, priority TicketPriority, now time.Time) *time.Time {
	contract, ok := m.contracts[tenantID]
	if !ok {
		// Default SLA deadlines
		switch priority {
		case PriorityCritical:
			d := now.Add(4 * time.Hour)
			return &d
		case PriorityHigh:
			d := now.Add(8 * time.Hour)
			return &d
		case PriorityMedium:
			d := now.Add(24 * time.Hour)
			return &d
		default:
			d := now.Add(72 * time.Hour)
			return &d
		}
	}

	responseTime, ok := contract.IncidentResponseTimes[string(priority)]
	if !ok {
		return nil
	}

	var dur time.Duration
	switch responseTime {
	case "15m":
		dur = 15 * time.Minute
	case "30m":
		dur = 30 * time.Minute
	case "1h":
		dur = 1 * time.Hour
	case "2h":
		dur = 2 * time.Hour
	case "4h":
		dur = 4 * time.Hour
	case "8h":
		dur = 8 * time.Hour
	case "12h":
		dur = 12 * time.Hour
	case "24h":
		dur = 24 * time.Hour
	case "48h":
		dur = 48 * time.Hour
	case "72h":
		dur = 72 * time.Hour
	default:
		dur = 24 * time.Hour
	}

	deadline := now.Add(dur)
	return &deadline
}
