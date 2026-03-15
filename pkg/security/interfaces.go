package security

import "context"

// SecurityService defines the contract for security management across clusters.
// The API layer depends on this interface rather than the concrete *Manager,
// enabling mock implementations for handler unit testing and pluggable backends.
type SecurityService interface {
	// ListPolicies returns all security policies.
	ListPolicies(ctx context.Context) ([]*SecurityPolicy, error)

	// CreatePolicy creates a new security policy.
	CreatePolicy(ctx context.Context, policy *SecurityPolicy) error

	// GetPolicy returns a security policy by ID.
	GetPolicy(ctx context.Context, policyID string) (*SecurityPolicy, error)

	// RunVulnerabilityScan triggers a vulnerability scan on a cluster.
	RunVulnerabilityScan(ctx context.Context, clusterID, scanType string) (*VulnerabilityScan, error)

	// GetComplianceReport returns a compliance report for a cluster.
	GetComplianceReport(ctx context.Context, clusterID, framework string) (*ComplianceReport, error)

	// GetAuditLogs returns recent audit log entries.
	GetAuditLogs(ctx context.Context, limit int) ([]*AuditLogEntry, error)

	// GetThreats returns detected threat events.
	GetThreats(ctx context.Context) ([]*ThreatEvent, error)

	// RecordAuditLog records an audit log entry.
	RecordAuditLog(entry *AuditLogEntry)
}

// Compile-time interface satisfaction check.
var _ SecurityService = (*Manager)(nil)
