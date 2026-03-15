// Package security - threat.go provides rule-based threat detection engine.
// Analyzes audit logs, K8s events, and runtime behavior to detect security threats
// including brute-force attacks, privilege escalation, anomalous API access,
// and data exfiltration attempts.
package security

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ThreatDetectionConfig holds threat detection configuration
type ThreatDetectionConfig struct {
	// Brute force: max failed logins within window
	BruteForceThreshold int
	BruteForceWindow    time.Duration
	// Anomaly: unusual API call rate
	APIRateThreshold int // calls per minute
	APIRateWindow    time.Duration
	// Scan interval
	DetectionInterval time.Duration
}

// ThreatDetector provides rule-based security threat detection
type ThreatDetector struct {
	config       ThreatDetectionConfig
	threats      []*ThreatEvent
	auditWindow  []*AuditLogEntry // recent audit entries for analysis
	rules        []detectionRule
	logger       *logrus.Logger
	mu           sync.RWMutex
}

// detectionRule defines a single threat detection rule
type detectionRule struct {
	ID          string
	Name        string
	Type        string // brute-force, privilege-escalation, data-exfiltration, anomalous-access
	Severity    string
	Description string
	Evaluate    func(entries []*AuditLogEntry) *ThreatEvent
}

// NewThreatDetector creates a new threat detection engine with default rules
func NewThreatDetector(cfg ThreatDetectionConfig) *ThreatDetector {
	if cfg.BruteForceThreshold == 0 {
		cfg.BruteForceThreshold = 5
	}
	if cfg.BruteForceWindow == 0 {
		cfg.BruteForceWindow = 5 * time.Minute
	}
	if cfg.APIRateThreshold == 0 {
		cfg.APIRateThreshold = 100
	}
	if cfg.APIRateWindow == 0 {
		cfg.APIRateWindow = 1 * time.Minute
	}
	if cfg.DetectionInterval == 0 {
		cfg.DetectionInterval = 30 * time.Second
	}

	td := &ThreatDetector{
		config:      cfg,
		threats:     make([]*ThreatEvent, 0),
		auditWindow: make([]*AuditLogEntry, 0),
		logger:      logrus.StandardLogger(),
	}

	td.rules = td.buildDefaultRules()
	return td
}

// IngestAuditEntry adds an audit log entry to the analysis window
func (td *ThreatDetector) IngestAuditEntry(entry *AuditLogEntry) {
	td.mu.Lock()
	defer td.mu.Unlock()

	td.auditWindow = append(td.auditWindow, entry)

	// Keep only entries within the largest detection window
	maxWindow := td.config.BruteForceWindow
	if td.config.APIRateWindow > maxWindow {
		maxWindow = td.config.APIRateWindow
	}
	cutoff := time.Now().Add(-maxWindow * 2)
	filtered := make([]*AuditLogEntry, 0, len(td.auditWindow))
	for _, e := range td.auditWindow {
		if e.Timestamp.After(cutoff) {
			filtered = append(filtered, e)
		}
	}
	td.auditWindow = filtered
}

// GetThreats returns all detected threats
func (td *ThreatDetector) GetThreats() []*ThreatEvent {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.threats
}

// RunDetection executes all detection rules against the current audit window
func (td *ThreatDetector) RunDetection(ctx context.Context) []*ThreatEvent {
	td.mu.Lock()
	defer td.mu.Unlock()

	var newThreats []*ThreatEvent
	for _, rule := range td.rules {
		threat := rule.Evaluate(td.auditWindow)
		if threat != nil {
			// Check for duplicate (same type + source within last hour)
			isDupe := false
			for _, existing := range td.threats {
				if existing.Type == threat.Type && existing.Source == threat.Source &&
					time.Since(existing.DetectedAt) < 1*time.Hour {
					isDupe = true
					break
				}
			}
			if !isDupe {
				td.threats = append(td.threats, threat)
				newThreats = append(newThreats, threat)
				td.logger.WithFields(logrus.Fields{
					"threat_type": threat.Type,
					"severity":    threat.Severity,
					"source":      threat.Source,
				}).Warn("Security threat detected")
			}
		}
	}

	return newThreats
}

// StartDetectionLoop starts periodic threat detection
func (td *ThreatDetector) StartDetectionLoop(ctx context.Context) {
	ticker := time.NewTicker(td.config.DetectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			td.RunDetection(ctx)
		}
	}
}

// ResolveThreat marks a threat as resolved
func (td *ThreatDetector) ResolveThreat(threatID, resolution string) error {
	td.mu.Lock()
	defer td.mu.Unlock()

	for _, t := range td.threats {
		if t.ID == threatID {
			now := common.NowUTC()
			t.Status = resolution // resolved, false-positive
			t.ResolvedAt = &now
			return nil
		}
	}
	return apperrors.NotFound("threat", threatID)
}

// buildDefaultRules creates the default threat detection rule set
func (td *ThreatDetector) buildDefaultRules() []detectionRule {
	return []detectionRule{
		{
			ID: "rule-brute-force", Name: "Brute Force Login Detection",
			Type: "brute-force", Severity: "high",
			Description: "Detects multiple failed authentication attempts from the same source",
			Evaluate:    td.detectBruteForce,
		},
		{
			ID: "rule-privilege-escalation", Name: "Privilege Escalation Detection",
			Type: "privilege-escalation", Severity: "critical",
			Description: "Detects attempts to escalate privileges (role changes, admin access)",
			Evaluate:    td.detectPrivilegeEscalation,
		},
		{
			ID: "rule-anomalous-api", Name: "Anomalous API Access Detection",
			Type: "anomalous-access", Severity: "medium",
			Description: "Detects unusually high rate of API calls from a single user",
			Evaluate:    td.detectAnomalousAPIAccess,
		},
		{
			ID: "rule-data-exfiltration", Name: "Data Exfiltration Detection",
			Type: "data-exfiltration", Severity: "critical",
			Description: "Detects bulk data access patterns suggesting exfiltration",
			Evaluate:    td.detectDataExfiltration,
		},
		{
			ID: "rule-unauthorized-resource", Name: "Unauthorized Resource Access",
			Type: "unauthorized-access", Severity: "high",
			Description: "Detects repeated authorization failures on sensitive resources",
			Evaluate:    td.detectUnauthorizedAccess,
		},
	}
}

// detectBruteForce checks for multiple failed login attempts
func (td *ThreatDetector) detectBruteForce(entries []*AuditLogEntry) *ThreatEvent {
	cutoff := time.Now().Add(-td.config.BruteForceWindow)

	// Count failed logins per IP
	failedByIP := make(map[string]int)
	failedByUser := make(map[string]int)
	for _, e := range entries {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		if e.Action == "login" && e.Status == "failure" {
			failedByIP[e.IPAddress]++
			failedByUser[e.Username]++
		}
	}

	for ip, count := range failedByIP {
		if count >= td.config.BruteForceThreshold {
			return &ThreatEvent{
				ID:          common.NewUUID(),
				Severity:    "high",
				Type:        "brute-force",
				Source:      ip,
				Target:      "authentication-endpoint",
				Description: fmt.Sprintf("Detected %d failed login attempts from IP %s within %v", count, ip, td.config.BruteForceWindow),
				Evidence: map[string]interface{}{
					"failed_count":  count,
					"window":        td.config.BruteForceWindow.String(),
					"source_ip":     ip,
					"affected_users": keysFromMap(failedByUser),
				},
				Status:     "active",
				DetectedAt: common.NowUTC(),
			}
		}
	}
	return nil
}

// detectPrivilegeEscalation checks for role elevation attempts
func (td *ThreatDetector) detectPrivilegeEscalation(entries []*AuditLogEntry) *ThreatEvent {
	cutoff := time.Now().Add(-td.config.BruteForceWindow)

	for _, e := range entries {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		// Detect role change events by non-admin users
		if e.ResourceType == "user" && e.Action == "update" {
			if details, ok := e.Details["role_change"]; ok {
				if roleChange, ok := details.(string); ok {
					if containsStr(roleChange, "admin") {
						return &ThreatEvent{
							ID:          common.NewUUID(),
							Severity:    "critical",
							Type:        "privilege-escalation",
							Source:      fmt.Sprintf("%s (%s)", e.Username, e.IPAddress),
							Target:      e.ResourceID,
							Description: fmt.Sprintf("User '%s' attempted role elevation to admin on resource '%s'", e.Username, e.ResourceID),
							Evidence: map[string]interface{}{
								"user":        e.Username,
								"action":      e.Action,
								"resource":    e.ResourceID,
								"role_change": roleChange,
							},
							Status:     "active",
							DetectedAt: common.NowUTC(),
						}
					}
				}
			}
		}

		// Detect direct K8s RBAC modification
		if e.ResourceType == "clusterrole" || e.ResourceType == "clusterrolebinding" {
			if e.Action == "create" || e.Action == "update" {
				return &ThreatEvent{
					ID:          common.NewUUID(),
					Severity:    "high",
					Type:        "privilege-escalation",
					Source:      fmt.Sprintf("%s (%s)", e.Username, e.IPAddress),
					Target:      e.ResourceID,
					Description: fmt.Sprintf("User '%s' modified K8s RBAC resource '%s/%s'", e.Username, e.ResourceType, e.ResourceID),
					Evidence: map[string]interface{}{
						"user":          e.Username,
						"resource_type": e.ResourceType,
						"resource_id":   e.ResourceID,
						"action":        e.Action,
					},
					Status:     "active",
					DetectedAt: common.NowUTC(),
				}
			}
		}
	}
	return nil
}

// detectAnomalousAPIAccess checks for unusually high API call rates
func (td *ThreatDetector) detectAnomalousAPIAccess(entries []*AuditLogEntry) *ThreatEvent {
	cutoff := time.Now().Add(-td.config.APIRateWindow)

	callsByUser := make(map[string]int)
	for _, e := range entries {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		callsByUser[e.Username]++
	}

	for user, count := range callsByUser {
		if user == "" {
			continue
		}
		if count >= td.config.APIRateThreshold {
			return &ThreatEvent{
				ID:          common.NewUUID(),
				Severity:    "medium",
				Type:        "anomalous-access",
				Source:      user,
				Target:      "api-server",
				Description: fmt.Sprintf("User '%s' made %d API calls within %v (threshold: %d)", user, count, td.config.APIRateWindow, td.config.APIRateThreshold),
				Evidence: map[string]interface{}{
					"user":      user,
					"count":     count,
					"window":    td.config.APIRateWindow.String(),
					"threshold": td.config.APIRateThreshold,
				},
				Status:     "active",
				DetectedAt: common.NowUTC(),
			}
		}
	}
	return nil
}

// detectDataExfiltration checks for bulk data read patterns
func (td *ThreatDetector) detectDataExfiltration(entries []*AuditLogEntry) *ThreatEvent {
	cutoff := time.Now().Add(-td.config.APIRateWindow)

	// Track unique resources read by each user
	readsByUser := make(map[string]map[string]bool)
	for _, e := range entries {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		if e.Action == "read" || e.Action == "list" || e.Action == "export" {
			if _, ok := readsByUser[e.Username]; !ok {
				readsByUser[e.Username] = make(map[string]bool)
			}
			readsByUser[e.Username][e.ResourceType+"/"+e.ResourceID] = true
		}
	}

	for user, resources := range readsByUser {
		if user == "" {
			continue
		}
		// Alert if user reads many distinct resources in a short time
		if len(resources) > 50 {
			return &ThreatEvent{
				ID:          common.NewUUID(),
				Severity:    "critical",
				Type:        "data-exfiltration",
				Source:      user,
				Target:      "multiple-resources",
				Description: fmt.Sprintf("User '%s' accessed %d distinct resources within %v — possible data exfiltration", user, len(resources), td.config.APIRateWindow),
				Evidence: map[string]interface{}{
					"user":            user,
					"resources_count": len(resources),
					"window":          td.config.APIRateWindow.String(),
				},
				Status:     "active",
				DetectedAt: common.NowUTC(),
			}
		}
	}
	return nil
}

// detectUnauthorizedAccess checks for repeated authorization failures
func (td *ThreatDetector) detectUnauthorizedAccess(entries []*AuditLogEntry) *ThreatEvent {
	cutoff := time.Now().Add(-td.config.BruteForceWindow)

	// Count 403/permission denied by user
	deniedByUser := make(map[string]int)
	for _, e := range entries {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		if e.Status == "forbidden" || e.Status == "denied" {
			deniedByUser[e.Username]++
		}
	}

	for user, count := range deniedByUser {
		if user == "" {
			continue
		}
		if count >= 10 {
			return &ThreatEvent{
				ID:          common.NewUUID(),
				Severity:    "high",
				Type:        "unauthorized-access",
				Source:      user,
				Target:      "api-server",
				Description: fmt.Sprintf("User '%s' received %d authorization denials within %v — possible probing", user, count, td.config.BruteForceWindow),
				Evidence: map[string]interface{}{
					"user":         user,
					"denial_count": count,
					"window":       td.config.BruteForceWindow.String(),
				},
				Status:     "active",
				DetectedAt: common.NowUTC(),
			}
		}
	}
	return nil
}

func keysFromMap(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
