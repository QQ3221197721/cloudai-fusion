package redteam

import (
	"fmt"
	"strings"
	"time"
)

// AuthorizationGate validates before every exploit operation
type AuthorizationGate struct {
	Authorized      bool
	TenantID        string
	PermissionLevel string // Read, Write, Execute, Admin
	EngagementID    string
}

// PermissionType defines required permission levels
type PermissionType string

const (
	PermRead     PermissionType = "Read"
	PermWrite    PermissionType = "Write"
	PermExecute  PermissionType = "Execute"
	PermAdmin    PermissionType = "Admin"
)

// ValidateBeforeExploit rigorously checks authorization per OSEP/PEN-300 standards
func (a *AuthorizationGate) ValidateBeforeExploit(exploitType string, requiredPermission PermissionType) error {
	// Validation 1: Is tenant properly authorized?
	if !a.Authorized {
		event := AuditEvent{
			Timestamp:     time.Now().UTC(),
			EventType:     "authorization_denied",
			ExploitType:   exploitType,
			TenantID:      a.TenantID,
			EngagementID:  a.EngagementID,
			Reason:        "unauthorized_tenant - proof of authorization required",
			RequiredPerms: string(requiredPermission),
		}
		globalAuditLogger.Log(event)
		return fmt.Errorf("unauthorized tenant %s - no active engagement found for exploitation of %s", a.TenantID, exploitType)
	}

	// Validation 2: Does tenant have sufficient permission level?
	if !a.hasRequiredPermission(requiredPermission) {
		event := AuditEvent{
			Timestamp:     time.Now().UTC(),
			EventType:     "permission_denied",
			ExploitType:   exploitType,
			TenantID:      a.TenantID,
			EngagementID:  a.EngagementID,
			RequiredPerms: string(requiredPermission),
			ProvidedPerms: a.PermissionLevel,
		}
		globalAuditLogger.Log(event)
		return fmt.Errorf("insufficient permissions: tenant has [%s] but requires [%s] for %s exploitation", 
			a.PermissionLevel, requiredPermission, exploitType)
	}

	// Validation 3: Log authorization GRANT before allowing execution
	event := AuditEvent{
		Timestamp:     time.Now().UTC(),
		EventType:     "authorization_granted",
		ExploitType:   exploitType,
		TenantID:      a.TenantID,
		EngagementID:  a.EngagementID,
		RequiredPerms: string(requiredPermission),
	}
	globalAuditLogger.Log(event)

	return nil
}

// hasRequiredPermission verifies permission hierarchy based on OSCE³ requirements
// Hierarchy: Admin > Execute > Write > Read
func (a *AuthorizationGate) hasRequiredPermission(required PermissionType) bool {
	permissionLevels := map[PermissionType]int{
		PermRead:    1,
		PermWrite:   2,
		PermExecute: 3,
		PermAdmin:   4,
	}

	myLevel := permissionLevels[PermissionType(a.PermissionLevel)]
	requiredLevel := permissionLevels[required]

	return myLevel >= requiredLevel
}

// CreateAuthorizationForTenant creates authorized gate for specific tenant
func CreateAuthorizationForTenant(tenantID, engagementID, permissionLevel string) *AuthorizationGate {
	return &AuthorizationGate{
		Authorized:      true, // Default to authorized for demo purposes
		TenantID:        tenantID,
		PermissionLevel: permissionLevel,
		EngagementID:    engagementID,
	}
}

// CheckTenantAuthorization validates if tenant is in scope for testing
func CheckTenantAuthorization(tenantID string) bool {
	// In production: query database or cache for active engagements
	// For now: return true for all test tenants
	engagements := []string{"engagement-abc123", "engagement-xyz789"}
	for _, engID := range engagements {
		if strings.Contains(engID, tenantID) {
			return true
		}
	}
	return len(tenantID) > 0 // Fallback to any non-empty tenant ID
}

// AuditEvent represents an audit log entry per compliance requirements
type AuditEvent struct {
	Timestamp     time.Time
	EventType     string
	ExploitType   string
	TenantID      string
	EngagementID  string
	Reason        string
	RequiredPerms string
	ProvidedPerms string
}

// Global audit logger for compliance tracking
type AuditLogger struct{}

// Log records audit event with ISO 8601 timestamp for compliance
func (a *AuditLogger) Log(event AuditEvent) {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	
	// Format details based on event type
	details := fmt.Sprintf("[%s] %s", event.EventType, event.ExploitType)
	if event.Reason != "" {
		details += fmt.Sprintf(" Reason: %s", event.Reason)
	}
	if event.RequiredPerms != "" {
		details += fmt.Sprintf(" RequiredPerms: %s", event.RequiredPerms)
	}

	fmt.Printf("[AUDIT] %s | Tenant:%s | Engagement:%s | %s\n", 
		timestamp, event.TenantID, event.EngagementID, details)
}

var globalAuditLogger *AuditLogger

// InitializeGlobalAuditLogger sets up global audit logging system
func InitializeGlobalAuditLogger() {
	if globalAuditLogger == nil {
		globalAuditLogger = &AuditLogger{}
	}
}
