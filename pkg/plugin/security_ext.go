package plugin

import (
	"context"
	"time"
)

// ============================================================================
// Security Extension Point Interfaces
// ============================================================================

// ScanTarget describes what to scan.
type ScanTarget struct {
	Type        string            `json:"type"`        // "image", "cluster", "config", "code"
	Identifier  string            `json:"identifier"`  // e.g., image ref, cluster ID
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Options     map[string]string `json:"options,omitempty"`
}

// ScanFinding is an individual vulnerability or misconfiguration.
type ScanFinding struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"` // CRITICAL, HIGH, MEDIUM, LOW, INFO
	Title       string `json:"title"`
	Description string `json:"description"`
	Package     string `json:"package,omitempty"`
	Version     string `json:"version,omitempty"`
	FixVersion  string `json:"fixVersion,omitempty"`
	CVSS        float64 `json:"cvss,omitempty"`
	Reference   string `json:"reference,omitempty"`
}

// ScanReport is the result of a security scan.
type ScanReport struct {
	PluginName  string        `json:"pluginName"`
	Target      ScanTarget    `json:"target"`
	Findings    []ScanFinding `json:"findings"`
	Summary     map[string]int `json:"summary"` // severity → count
	StartedAt   time.Time     `json:"startedAt"`
	CompletedAt time.Time     `json:"completedAt"`
}

// ScannerPlugin extends the security scanning pipeline.
// Multiple scanners can run in parallel; their findings are merged.
type ScannerPlugin interface {
	Plugin
	Scan(ctx context.Context, target ScanTarget) (*ScanReport, error)
	// SupportedTargetTypes returns which target types this scanner handles.
	SupportedTargetTypes() []string
}

// ============================================================================
// Policy Enforcement
// ============================================================================

// PolicyContext carries the data to be evaluated against security policies.
type PolicyContext struct {
	Action    string            `json:"action"`    // "deploy", "scale", "update", "delete"
	Resource  string            `json:"resource"`  // "workload", "cluster", "pod"
	Namespace string            `json:"namespace,omitempty"`
	UserID    string            `json:"userId,omitempty"`
	Roles     []string          `json:"roles,omitempty"`
	Object    interface{}       `json:"object"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// PolicyDecision is the result of policy evaluation.
type PolicyDecision struct {
	Allowed    bool     `json:"allowed"`
	Reason     string   `json:"reason,omitempty"`
	Violations []string `json:"violations,omitempty"`
	PluginName string   `json:"pluginName"`
}

// PolicyEnforcerPlugin evaluates requests against security policies.
// All registered enforcers must allow for the action to proceed.
type PolicyEnforcerPlugin interface {
	Plugin
	Enforce(ctx context.Context, pctx *PolicyContext) (*PolicyDecision, error)
}

// ============================================================================
// Audit
// ============================================================================

// AuditEvent carries data for audit logging.
type AuditEvent struct {
	Timestamp  time.Time         `json:"timestamp"`
	EventType  string            `json:"eventType"`
	Actor      string            `json:"actor"`
	Action     string            `json:"action"`
	Resource   string            `json:"resource"`
	ResourceID string            `json:"resourceId"`
	Outcome    string            `json:"outcome"` // "success", "failure", "denied"
	Details    map[string]string `json:"details,omitempty"`
	RiskLevel  string            `json:"riskLevel,omitempty"` // "low", "medium", "high", "critical"
}

// AuditorPlugin receives audit events for logging/forwarding.
// Audit plugins are fire-and-forget (best-effort).
type AuditorPlugin interface {
	Plugin
	Audit(ctx context.Context, event *AuditEvent) error
}

// ============================================================================
// Threat Detection
// ============================================================================

// ThreatSignal is a detected security threat or anomaly.
type ThreatSignal struct {
	ID          string            `json:"id"`
	Timestamp   time.Time         `json:"timestamp"`
	Type        string            `json:"type"`        // "brute_force", "privilege_escalation", "anomalous_access"
	Severity    string            `json:"severity"`    // CRITICAL, HIGH, MEDIUM, LOW
	Source      string            `json:"source"`
	Description string            `json:"description"`
	Evidence    map[string]string `json:"evidence,omitempty"`
	Mitigations []string          `json:"mitigations,omitempty"`
	PluginName  string            `json:"pluginName"`
}

// ThreatDetectorPlugin analyzes signals for security threats.
type ThreatDetectorPlugin interface {
	Plugin
	Detect(ctx context.Context, signals []map[string]interface{}) ([]ThreatSignal, error)
}

// ============================================================================
// SecurityPluginChain — runs security plugins in chain
// ============================================================================

// SecurityPluginChain orchestrates security plugin execution.
type SecurityPluginChain struct {
	registry *Registry
}

// NewSecurityPluginChain wraps a registry for security plugin execution.
func NewSecurityPluginChain(reg *Registry) *SecurityPluginChain {
	return &SecurityPluginChain{registry: reg}
}

// RunScanners runs all ScannerPlugins in parallel for the given target.
func (c *SecurityPluginChain) RunScanners(ctx context.Context, target ScanTarget) ([]*ScanReport, error) {
	plugins := c.registry.GetByExtension(ExtSecurityScanner)
	reports := make([]*ScanReport, 0, len(plugins))

	for _, p := range plugins {
		sp, ok := p.(ScannerPlugin)
		if !ok {
			continue
		}
		// Check if this scanner supports the target type.
		supported := false
		for _, t := range sp.SupportedTargetTypes() {
			if t == target.Type || t == "*" {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}

		report, err := sp.Scan(ctx, target)
		if err != nil {
			return reports, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}

// RunPolicyEnforcers runs all PolicyEnforcerPlugins. Returns denied if any rejects.
func (c *SecurityPluginChain) RunPolicyEnforcers(ctx context.Context, pctx *PolicyContext) (*PolicyDecision, error) {
	for _, p := range c.registry.GetByExtension(ExtSecurityPolicyEnforce) {
		ep, ok := p.(PolicyEnforcerPlugin)
		if !ok {
			continue
		}
		decision, err := ep.Enforce(ctx, pctx)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			return decision, nil
		}
	}
	return &PolicyDecision{Allowed: true}, nil
}

// RunAuditors sends an audit event to all AuditorPlugins (best-effort).
func (c *SecurityPluginChain) RunAuditors(ctx context.Context, event *AuditEvent) {
	for _, p := range c.registry.GetByExtension(ExtSecurityAudit) {
		if ap, ok := p.(AuditorPlugin); ok {
			_ = ap.Audit(ctx, event) // best-effort
		}
	}
}

// RunThreatDetectors runs all ThreatDetectorPlugins and merges signals.
func (c *SecurityPluginChain) RunThreatDetectors(ctx context.Context, signals []map[string]interface{}) ([]ThreatSignal, error) {
	var allThreats []ThreatSignal
	for _, p := range c.registry.GetByExtension(ExtSecurityThreatDetect) {
		td, ok := p.(ThreatDetectorPlugin)
		if !ok {
			continue
		}
		threats, err := td.Detect(ctx, signals)
		if err != nil {
			return allThreats, err
		}
		allThreats = append(allThreats, threats...)
	}
	return allThreats, nil
}
