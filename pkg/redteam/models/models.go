// Package models defines the Red Team Platform data structures and database schemas.
package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// =========================================
// Core User Model (Authentication & Authorization)
// =========================================

// User represents a platform user with role-based access control.
type User struct {
	ID                      uuid.UUID                `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Username                string                   `gorm:"type:citext;uniqueIndex;not null" json:"username"`
	Email                   string                   `gorm:"type:citext;uniqueIndex;not null" json:"email"`
	FullName                string                   `gorm:"type:varchar(255)" json:"full_name,omitempty"`
	PasswordHash            string                   `gorm:"type:text;not null" json:"-"` // Never expose password hash
	Role                    string                   `gorm:"type:varchar(50);not null;default:'pentester'" json:"role"`
	Permissions             NullStringArray          `gorm:"type:jsonb;default:'[\"read\"]'::jsonb" json:"permissions"`
	Active                  bool                     `gorm:"type:boolean;default:true" json:"active"`
	LastLogin               *time.Time               `gorm:"type:timestamptz" json:"last_login,omitempty"`
	FailedLoginAttempts     int                      `gorm:"type:int;default:0" json:"-"`
	LockedUntil             *time.Time               `gorm:"type:timestamptz" json:"-"`
	CreatedAt               time.Time                `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt               time.Time                `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"updated_at"`
	CreatedBy               *uuid.UUID               `gorm:"type:uuid;index" json:"-"`
	WorkOrders              []WorkOrder              `gorm:"foreignKey:UserID" json:"work_orders,omitempty"`
	ReviewComments          []WorkOrderAudit         `gorm:"foreignKey:PerformedBy" json:"-"`
	RefreshTokens           []RefreshToken           `gorm:"foreignKey:UserID" json:"-"`
	AuthorizationIdentifier string                 `gorm:"-"` // For bcrypt hashing
}

// BeforeSave hook to handle UpdatedAt automatically
func (u *User) BeforeSave(tx *gorm.DB) error {
	u.UpdatedAt = time.Now()
	return nil
}

// HasPermission checks if the user has a specific permission.
func (u *User) HasPermission(permission string) bool {
	if !u.Permissions.Valid || len(u.Permissions.Strings) == 0 {
		return false
	}
	for _, p := range u.Permissions.Strings {
		if p == permission {
			return true
		}
	}
	return false
}

// IsLocked returns whether the account is currently locked.
func (u *User) IsLocked() bool {
	if u.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*u.LockedUntil)
}

// =========================================
// Work Order Model (Authorization Request)
// =========================================

// WorkOrder represents a penetration test authorization request.
type WorkOrder struct {
	ID                   uuid.UUID       `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID               uuid.UUID       `gorm:"type:uuid;not null;index" json:"user_id"`
	User                 User            `gorm:"foreignKey:UserID" json:"user,omitempty"`
	CompanyName          string          `gorm:"type:varchar(255);not null" json:"company_name"`
	ContactEmail         string          `gorm:"type:varchar(255);not null" json:"contact_email"`
	Justification        string          `gorm:"type:text;not null" json:"justification"`
	TargetList           NullStringArray `gorm:"type:jsonb;default:'[]'::jsonb;" json:"target_list"`
	AuthorizationLetterURL string        `gorm:"type:text" json:"authorization_letter_url,omitempty"`
	LegalContractURL     string          `gorm:"type:text" json:"legal_contract_url,omitempty"`
	Status               string          `gorm:"type:varchar(50);not null;default:'pending'" json:"status"`
	PriorityLevel        int             `gorm:"type:int;default:2" json:"priority_level"` // 1=critical, 2=high, 3=normal, 4=low
	SubmittedAt          time.Time       `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"submitted_at"`
	ReviewedAt           *time.Time      `gorm:"type:timestamptz" json:"reviewed_at,omitempty"`
	ApprovedAt           *time.Time      `gorm:"type:timestamptz" json:"approved_at,omitempty"`
	ExpiresAt            *time.Time      `gorm:"type:timestamptz" json:"expires_at,omitempty"`
	ReviewerID           *uuid.UUID      `gorm:"type:uuid;index" json:"reviewer_id,omitempty"`
	Reviewer             *User           `gorm:"foreignKey:ReviewerID" json:"reviewer,omitempty"`
	ReviewComments       string          `gorm:"type:text" json:"review_comments,omitempty"`
	RejectionReason      string          `gorm:"type:text" json:"rejection_reason,omitempty"`
	Campaigns            []AttackCampaign `gorm:"foreignKey:WorkOrderID" json:"campaigns,omitempty"`
	AuditTrail           []WorkOrderAudit `gorm:"foreignKey:WorkOrderID" json:"audit_trail,omitempty"`
}

// IsExpired checks if the work order has expired.
func (w *WorkOrder) IsExpired() bool {
	if w.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*w.ExpiresAt)
}

// CanCreateCampaign checks if a campaign can be created from this work order.
func (w *WorkOrder) CanCreateCampaign() bool {
	return w.Status == "approved" && !w.IsExpired()
}

// =========================================
// Attack Campaign Model
// =========================================

// AttackCampaign represents a security assessment execution.
type AttackCampaign struct {
	ID              uuid.UUID       `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Name            string          `gorm:"type:varchar(255);not null" json:"name"`
	Description     string          `gorm:"type:text" json:"description,omitempty"`
	WorkOrderID     *uuid.UUID      `gorm:"type:uuid;index" json:"work_order_id,omitempty"`
	WorkOrder       *WorkOrder      `gorm:"foreignKey:WorkOrderID" json:"work_order,omitempty"`
	AttackTypes     NullStringArray `gorm:"type:varchar(50);not null;default:'{}'" json:"attack_types"`
	Targets         NullStringArray `gorm:"type:varchar(50);not null;default:'{}'" json:"targets"`
	Status          string          `gorm:"type:varchar(50);not null;default:'scheduled'" json:"status"`
	ProgressPercent int             `gorm:"type:int;default:0" json:"progress_percentage"`
	StartedAt       *time.Time      `gorm:"type:timestamptz" json:"started_at,omitempty"`
	CompletedAt     *time.Time      `gorm:"type:timestamptz" json:"completed_at,omitempty"`
	CreatedAt       time.Time       `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"created_at"`
	TotalDuration   *time.Duration  `gorm:"type:interval" json:"total_duration,omitempty"`
	FindingsCount   int             `gorm:"type:int;default:0" json:"findings_count"`
	Findings        []Finding       `gorm:"foreignKey:CampaignID" json:"findings,omitempty"`
}

// CalculateDuration computes the campaign duration if completed.
func (a *AttackCampaign) CalculateDuration() (*time.Duration, error) {
	if a.StartedAt == nil {
		return nil, errors.New("campaign has not started yet")
	}
	if a.CompletedAt == nil {
		return nil, errors.New("campaign is still running")
	}
	duration := a.CompletedAt.Sub(*a.StartedAt)
	a.TotalDuration = &duration
	return &duration, nil
}

// =========================================
// Finding Model (Vulnerability Results)
// =========================================

// Finding represents a vulnerability discovered during an attack campaign.
type Finding struct {
	ID            uuid.UUID       `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CampaignID    uuid.UUID       `gorm:"type:uuid;not null;index" json:"campaign_id"`
	Campaign      AttackCampaign  `gorm:"foreignKey:CampaignID" json:"campaign,omitempty"`
	Title         string          `gorm:"type:varchar(255);not null" json:"title"`
	Description   string          `gorm:"type:text;not null" json:"description"`
	Severity      string          `gorm:"type:varchar(50);not null;index" json:"severity"`
	CVVSScore     float64         `gorm:"type:numeric(3,1)" json:"cvss_score,omitempty"`
	AffectedAsset string          `gorm:"type:varchar(255)" json:"affected_asset"`
	AssetType     string          `gorm:"type:varchar(50)" json:"asset_type,omitempty"`
	EvidenceUrls  NullStringArray `gorm:"type:jsonb;default:'[]'::jsonb;" json:"evidence_urls"`
	Remediation   string          `gorm:"type:text" json:"remediation"`
	References    NullStringArray `gorm:"type:text[]" json:"references,omitempty"`
	Verified      bool            `gorm:"type:boolean;default:false;index" json:"verified"`
	Patched       bool            `gorm:"type:boolean;default:false;index" json:"patched"`
	DiscoveredAt  time.Time       `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"discovered_at"`
	UpdatedAt     time.Time       `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// BeforeSave hook to handle UpdatedAt automatically
func (f *Finding) BeforeSave(tx *gorm.DB) error {
	f.UpdatedAt = time.Now()
	return nil
}

// GetCVSSRating returns the CVSS severity rating string.
func (f *Finding) GetCVSSRating() string {
	switch {
	case f.CVVSScore >= 9.0:
		return "critical"
	case f.CVVSScore >= 7.0:
		return "high"
	case f.CVVSScore >= 4.0:
		return "medium"
	case f.CVVSScore > 0.0:
		return "low"
	default:
		return "informational"
	}
}

// =========================================
// Audit Trail Models
// =========================================

// WorkOrderAudit represents audit log entries for work order actions.
type WorkOrderAudit struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	WorkOrderID uuid.UUID  `gorm:"type:uuid;not null;index" json:"work_order_id"`
	WorkOrder   WorkOrder  `gorm:"foreignKey:WorkOrderID" json:"work_order,omitempty"`
	Action      string     `gorm:"type:varchar(50);not null" json:"action"`
	PerformedBy uuid.UUID  `gorm:"type:uuid;not null;index" json:"performed_by"`
	PerformedAt time.Time  `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"performed_at"`
	IPAddress   string     `gorm:"type:inet" json:"ip_address,omitempty"`
	UserAgent   string     `gorm:"type:text" json:"user_agent,omitempty"`
	ChangesMade NullJSON   `gorm:"type:jsonb" json:"changes_made,omitempty"`
}

// LoginAttempt represents login attempt tracking for rate limiting.
type LoginAttempt struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Username  string    `gorm:"type:citext;not null;index:idx_login_username_attempted" json:"username"`
	AttemptedAt time.Time `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP;index:idx_login_username_attempted" json:"attempted_at"`
	IPAddress string    `gorm:"type:inet" json:"ip_address,omitempty"`
	Success   bool      `gorm:"type:boolean;default:false" json:"success"`
}

// RefreshToken represents JWT refresh tokens for token rotation.
type RefreshToken struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Token     string    `gorm:"type:text;not null;uniqueIndex" json:"-"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	User      User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	ExpiresAt time.Time `gorm:"type:timestamptz;not null" json:"expires_at"`
	IsActive  bool      `gorm:"type:boolean;default:true;index" json:"is_active"`
	CreatedAt time.Time `gorm:"type:timestamptz;default:CURRENT_TIMESTAMP" json:"created_at"`
}

// =========================================
// Helper Types
// =========================================

// NullStringArray wraps []string to support NULL values in JSONB columns.
type NullStringArray struct {
	Strings []string
	Valid   bool // True when value is not NULL
}

// Scan implements sql.Scanner interface for PostgreSQL JSONB type.
func (n *NullStringArray) Scan(value interface{}) error {
	if value == nil {
		n.Strings = nil
		n.Valid = false
		return nil
	}
	n.Valid = true
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, &n.Strings)
}

// Value implements driver.Valuer interface for storing in database.
func (n NullStringArray) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	if n.Strings == nil {
		return []string{}, nil
	}
	return json.Marshal(n.Strings)
}

// NullJSON wraps map[string]interface{} to support NULL values.
type NullJSON struct {
	Value map[string]interface{}
	Valid bool
}

// Scan implements sql.Scanner interface.
func (n *NullJSON) Scan(value interface{}) error {
	if value == nil {
		n.Valid = false
		return nil
	}
	n.Valid = true
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, &n.Value)
}

// =========================================
// Validation Helpers
// =========================================

// ValidateUser ensures the user entity is valid before database operations.
func (u *User) ValidateUser() error {
	if u.Username == "" {
		return errors.New("username is required")
	}
	if len(u.Username) < 3 || len(u.Username) > 50 {
		return errors.New("username must be between 3 and 50 characters")
	}
	if u.Email == "" {
		return errors.New("email is required")
	}
	if len(u.PasswordHash) == 0 {
		return errors.New("password hash is required")
	}
	validRoles := map[string]bool{"admin": true, "pentester": true, "auditor": true}
	if !validRoles[u.Role] {
		return errors.New("invalid role")
	}
	return nil
}

// ValidateWorkOrder ensures the work order entity is valid.
func (w *WorkOrder) ValidateWorkOrder() error {
	if w.CompanyName == "" {
		return errors.New("company name is required")
	}
	if w.ContactEmail == "" {
		return errors.New("contact email is required")
	}
	if w.Justification == "" {
		return errors.New("justification is required")
	}
	if len(w.TargetList.Strings) == 0 {
		return errors.New("at least one target is required")
	}
	if w.PriorityLevel < 1 || w.PriorityLevel > 4 {
		return errors.New("priority level must be between 1 and 4")
	}
	return nil
}

// ValidateFinding ensures the finding entity is valid.
func (f *Finding) ValidateFinding() error {
	if f.Title == "" {
		return errors.New("finding title is required")
	}
	if f.Description == "" {
		return errors.New("finding description is required")
	}
	validSeverities := map[string]bool{"critical": true, "high": true, "medium": true, "low": true, "informational": true}
	if !validSeverities[f.Severity] {
		return errors.New("invalid severity")
	}
	if f.CVVSScore < 0 || f.CVVSScore > 10 {
		return errors.New("CVSS score must be between 0 and 10")
	}
	return nil
}
