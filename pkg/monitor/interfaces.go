package monitor

// MonitoringService defines the contract for monitoring and alerting.
// The API layer depends on this interface rather than the concrete *Service,
// enabling mock implementations for handler unit testing.
type MonitoringService interface {
	// GetAlertRules returns all configured alert rules.
	GetAlertRules() []*AlertRule

	// GetRecentEvents returns recent alert events.
	GetRecentEvents(limit int) []*AlertEvent
}

// Compile-time interface satisfaction check.
var _ MonitoringService = (*Service)(nil)
