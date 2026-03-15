// Package enterprise provides enterprise-grade features for CloudAI Fusion,
// including SSO integration (SAML/LDAP), audit reporting, SLA guarantees,
// and a technical support ticket system.
package enterprise

import (
	"time"
)

// ============================================================================
// SSO Integration (SAML/LDAP)
// ============================================================================

// SSOProtocol defines the SSO protocol type
type SSOProtocol string

const (
	SSOProtocolSAML SSOProtocol = "saml"
	SSOProtocolLDAP SSOProtocol = "ldap"
	SSOProtocolOIDC SSOProtocol = "oidc"
)

// SSOConfig defines SSO provider configuration
type SSOConfig struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenant_id"`
	Name           string            `json:"name"`
	Protocol       SSOProtocol       `json:"protocol"`
	Enabled        bool              `json:"enabled"`
	AutoProvision  bool              `json:"auto_provision"`
	DefaultRole    string            `json:"default_role"`
	RoleMapping    map[string]string `json:"role_mapping"`
	GroupAttribute string            `json:"group_attribute"`
	SAML           *SAMLConfig       `json:"saml,omitempty"`
	LDAP           *LDAPConfig       `json:"ldap,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastSyncAt     *time.Time        `json:"last_sync_at,omitempty"`
}

// SAMLConfig holds SAML-specific configuration
type SAMLConfig struct {
	EntityID             string `json:"entity_id"`
	SSOURL               string `json:"sso_url"`
	SLOLogoutURL         string `json:"slo_logout_url,omitempty"`
	Certificate          string `json:"certificate"`
	SPEntityID           string `json:"sp_entity_id"`
	ACSURL               string `json:"acs_url"`
	NameIDFormat         string `json:"name_id_format"`
	SignRequests         bool   `json:"sign_requests"`
	WantAssertionsSigned bool   `json:"want_assertions_signed"`
}

// LDAPConfig holds LDAP-specific configuration
type LDAPConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	UseTLS          bool   `json:"use_tls"`
	StartTLS        bool   `json:"start_tls"`
	BindDN          string `json:"bind_dn"`
	BindPassword    string `json:"-"`
	BaseDN          string `json:"base_dn"`
	UserSearchBase  string `json:"user_search_base"`
	UserFilter      string `json:"user_filter"`
	GroupSearchBase string `json:"group_search_base"`
	GroupFilter     string `json:"group_filter"`
	EmailAttribute  string `json:"email_attribute"`
	NameAttribute   string `json:"name_attribute"`
}

// SSOLoginResult represents the result of an SSO authentication
type SSOLoginResult struct {
	UserID      string      `json:"user_id"`
	Email       string      `json:"email"`
	DisplayName string      `json:"display_name"`
	Groups      []string    `json:"groups"`
	MappedRole  string      `json:"mapped_role"`
	Provider    string      `json:"provider"`
	Protocol    SSOProtocol `json:"protocol"`
	SessionID   string      `json:"session_id"`
	AuthTime    time.Time   `json:"auth_time"`
}

// ============================================================================
// Audit Reports
// ============================================================================

// AuditReportType defines types of audit reports
type AuditReportType string

const (
	ReportTypeAccess     AuditReportType = "access"
	ReportTypeSecurity   AuditReportType = "security"
	ReportTypeCompliance AuditReportType = "compliance"
	ReportTypeChange     AuditReportType = "change"
	ReportTypeCost       AuditReportType = "cost"
)

// AuditReport represents a generated audit report
type AuditReport struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	Type        AuditReportType `json:"type"`
	Title       string          `json:"title"`
	Period      ReportPeriod    `json:"period"`
	Status      string          `json:"status"`
	Summary     ReportSummary   `json:"summary"`
	Sections    []ReportSection `json:"sections"`
	GeneratedBy string          `json:"generated_by"`
	GeneratedAt time.Time       `json:"generated_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
}

// ReportPeriod defines the time range for a report
type ReportPeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ReportSummary holds high-level report statistics
type ReportSummary struct {
	TotalEvents     int            `json:"total_events"`
	UniqueUsers     int            `json:"unique_users"`
	ByAction        map[string]int `json:"by_action"`
	ByOutcome       map[string]int `json:"by_outcome"`
	RiskScore       float64        `json:"risk_score"`
	ComplianceScore float64        `json:"compliance_score"`
}

// ReportSection represents a section within a report
type ReportSection struct {
	Title string                   `json:"title"`
	Type  string                   `json:"type"`
	Data  []map[string]interface{} `json:"data"`
}

// ScheduledReport defines a recurring report schedule
type ScheduledReport struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenant_id"`
	Type       AuditReportType `json:"type"`
	Schedule   string          `json:"schedule"`
	Recipients []string        `json:"recipients"`
	Enabled    bool            `json:"enabled"`
	LastRunAt  *time.Time      `json:"last_run_at,omitempty"`
	NextRunAt  time.Time       `json:"next_run_at"`
}

// ============================================================================
// SLA Guarantees
// ============================================================================

// SLATier defines SLA guarantee levels
type SLATier string

const (
	SLATierStandard   SLATier = "standard"
	SLATierBusiness   SLATier = "business"
	SLATierPremium    SLATier = "premium"
	SLATierEnterprise SLATier = "enterprise"
)

// SLAContract represents an SLA agreement for a tenant
type SLAContract struct {
	ID                    string           `json:"id"`
	TenantID              string           `json:"tenant_id"`
	Tier                  SLATier          `json:"tier"`
	AvailabilityTarget    float64          `json:"availability_target"`
	SupportHours          string           `json:"support_hours"`
	IncidentResponseTimes map[string]string `json:"incident_response_times"`
	CreditPolicy          CreditPolicy     `json:"credit_policy"`
	EffectiveFrom         time.Time        `json:"effective_from"`
	EffectiveTo           time.Time        `json:"effective_to"`
	Status                string           `json:"status"`
}

// CreditPolicy defines SLA credit compensation rules
type CreditPolicy struct {
	Enabled   bool         `json:"enabled"`
	Tiers     []CreditTier `json:"tiers"`
	MaxCredit float64      `json:"max_credit_percent"`
}

// CreditTier maps availability to credit percentage
type CreditTier struct {
	MinAvailability float64 `json:"min_availability"`
	MaxAvailability float64 `json:"max_availability"`
	CreditPercent   float64 `json:"credit_percent"`
}

// SLAReport tracks SLA compliance over a period
type SLAReport struct {
	ID              string        `json:"id"`
	TenantID        string        `json:"tenant_id"`
	ContractID      string        `json:"contract_id"`
	Period          string        `json:"period"`
	Availability    float64       `json:"availability"`
	Target          float64       `json:"target"`
	IsMet           bool          `json:"is_met"`
	DowntimeMinutes float64       `json:"downtime_minutes"`
	Incidents       []SLAIncident `json:"incidents"`
	CreditDue       float64       `json:"credit_due"`
	GeneratedAt     time.Time     `json:"generated_at"`
}

// SLAIncident tracks downtime incidents for SLA reporting
type SLAIncident struct {
	ID          string        `json:"id"`
	StartTime   time.Time     `json:"start_time"`
	EndTime     time.Time     `json:"end_time"`
	Duration    time.Duration `json:"duration"`
	Service     string        `json:"service"`
	Description string        `json:"description"`
	Impact      string        `json:"impact"`
}

// ============================================================================
// Support Ticket System
// ============================================================================

// TicketPriority defines support ticket priorities
type TicketPriority string

const (
	PriorityCritical TicketPriority = "critical"
	PriorityHigh     TicketPriority = "high"
	PriorityMedium   TicketPriority = "medium"
	PriorityLow      TicketPriority = "low"
)

// TicketStatus defines ticket lifecycle states
type TicketStatus string

const (
	TicketStatusOpen       TicketStatus = "open"
	TicketStatusAssigned   TicketStatus = "assigned"
	TicketStatusInProgress TicketStatus = "in-progress"
	TicketStatusWaiting    TicketStatus = "waiting-customer"
	TicketStatusResolved   TicketStatus = "resolved"
	TicketStatusClosed     TicketStatus = "closed"
	TicketStatusReopened   TicketStatus = "reopened"
)

// TicketCategory defines support categories
type TicketCategory string

const (
	CategoryCluster     TicketCategory = "cluster"
	CategorySecurity    TicketCategory = "security"
	CategoryBilling     TicketCategory = "billing"
	CategoryPerformance TicketCategory = "performance"
	CategoryAPI         TicketCategory = "api"
	CategoryGeneral     TicketCategory = "general"
	CategoryBug         TicketCategory = "bug"
)

// SupportTicket represents a customer support ticket
type SupportTicket struct {
	ID              string           `json:"id"`
	TenantID        string           `json:"tenant_id"`
	Title           string           `json:"title"`
	Description     string           `json:"description"`
	Priority        TicketPriority   `json:"priority"`
	Status          TicketStatus     `json:"status"`
	Category        TicketCategory   `json:"category"`
	CreatedBy       string           `json:"created_by"`
	AssignedTo      string           `json:"assigned_to,omitempty"`
	Tags            []string         `json:"tags,omitempty"`
	Comments        []TicketComment  `json:"comments"`
	SLADeadline     *time.Time       `json:"sla_deadline,omitempty"`
	SLABreached     bool             `json:"sla_breached"`
	Resolution      string           `json:"resolution,omitempty"`
	Satisfaction    *int             `json:"satisfaction,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	ResolvedAt      *time.Time       `json:"resolved_at,omitempty"`
	ClosedAt        *time.Time       `json:"closed_at,omitempty"`
	FirstResponseAt *time.Time       `json:"first_response_at,omitempty"`
}

// TicketComment represents a comment on a support ticket
type TicketComment struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	IsStaff   bool      `json:"is_staff"`
	Content   string    `json:"content"`
	Internal  bool      `json:"internal"`
	CreatedAt time.Time `json:"created_at"`
}

// TicketMetrics tracks ticket system performance
type TicketMetrics struct {
	TenantID              string         `json:"tenant_id"`
	Period                string         `json:"period"`
	TotalTickets          int            `json:"total_tickets"`
	OpenTickets           int            `json:"open_tickets"`
	ResolvedTickets       int            `json:"resolved_tickets"`
	AvgFirstResponseHours float64        `json:"avg_first_response_hours"`
	AvgResolutionHours    float64        `json:"avg_resolution_hours"`
	SLACompliancePercent  float64        `json:"sla_compliance_percent"`
	SatisfactionAvg       float64        `json:"satisfaction_avg"`
	ByPriority            map[string]int `json:"by_priority"`
	ByCategory            map[string]int `json:"by_category"`
}

// DefaultSLAContract returns default SLA for a given tier
func DefaultSLAContract(tier SLATier) SLAContract {
	switch tier {
	case SLATierEnterprise:
		return SLAContract{
			Tier: SLATierEnterprise, AvailabilityTarget: 0.9999,
			SupportHours: "24x7",
			IncidentResponseTimes: map[string]string{
				"critical": "15m", "high": "1h", "medium": "4h", "low": "8h",
			},
			CreditPolicy: CreditPolicy{Enabled: true, MaxCredit: 30, Tiers: []CreditTier{
				{MinAvailability: 0.999, MaxAvailability: 0.9999, CreditPercent: 10},
				{MinAvailability: 0.99, MaxAvailability: 0.999, CreditPercent: 25},
				{MinAvailability: 0, MaxAvailability: 0.99, CreditPercent: 30},
			}},
		}
	case SLATierPremium:
		return SLAContract{
			Tier: SLATierPremium, AvailabilityTarget: 0.9995,
			SupportHours: "24x7",
			IncidentResponseTimes: map[string]string{
				"critical": "30m", "high": "2h", "medium": "8h", "low": "24h",
			},
			CreditPolicy: CreditPolicy{Enabled: true, MaxCredit: 25, Tiers: []CreditTier{
				{MinAvailability: 0.999, MaxAvailability: 0.9995, CreditPercent: 10},
				{MinAvailability: 0.99, MaxAvailability: 0.999, CreditPercent: 20},
			}},
		}
	case SLATierBusiness:
		return SLAContract{
			Tier: SLATierBusiness, AvailabilityTarget: 0.999,
			SupportHours: "extended",
			IncidentResponseTimes: map[string]string{
				"critical": "1h", "high": "4h", "medium": "12h", "low": "48h",
			},
			CreditPolicy: CreditPolicy{Enabled: true, MaxCredit: 15, Tiers: []CreditTier{
				{MinAvailability: 0.99, MaxAvailability: 0.999, CreditPercent: 10},
			}},
		}
	default:
		return SLAContract{
			Tier: SLATierStandard, AvailabilityTarget: 0.995,
			SupportHours: "business-hours",
			IncidentResponseTimes: map[string]string{
				"critical": "4h", "high": "8h", "medium": "24h", "low": "72h",
			},
			CreditPolicy: CreditPolicy{Enabled: false},
		}
	}
}
