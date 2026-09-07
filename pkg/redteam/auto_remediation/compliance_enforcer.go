package auto_remediation

import (
	"context"
	"encoding/json"
	"time"
)

// ComplianceEnforcer validates remediation recommendations against OBE3 compliance rules
type ComplianceEnforcer struct {
	rules     []ComplianceRule
	mode      string // real or simulation
	verifier  RuleVerifier
}

// ComplianceRule defines an OBE3 compliance requirement
type ComplianceRule struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Severity    Severity      `json:"severity"`
	Category    RuleCategory  `json:"category"`
	Pattern     string        `json:"pattern"`          // Regex pattern to match
	Action      string        `json:"action"`           // Expected action if violated
	Enabled     bool          `json:"enabled"`
}

// RuleCategory categorizes compliance rules
type RuleCategory string

const (
	CatSecurity     RuleCategory = "security"
	CatAudit                    = "audit"
	CatPolicy                   = "policy"
	CatLegal                    = "legal"
	CatOperational              = "operational"
)

// RuleVerifier verifies rule compliance
type RuleVerifier interface {
	Verify(ctx context.Context, rule ComplianceRule, content string) (bool, VerificationResult)
}

// VerificationResult contains verification outcome
type VerificationResult struct {
	Compliant   bool                  `json:"compliant"`
	Violation   string                `json:"violation,omitempty"`
	Evidence    string                `json:"evidence,omitempty"`
	Suggestion  string                `json:"suggestion,omitempty"`
	Timestamp   time.Time             `json:"timestamp"`
}

// ComplianceDecision records enforcement decision
type ComplianceDecision struct {
	RuleID      string                 `json:"rule_id"`
	Action      string                 `json:"action"`
	Decision    string                 `json:"decision"` // allow/deny/warn
	Violations  []string               `json:"violations,omitempty"`
	Context     map[string]interface{} `json:"context"`
	Timestamp   time.Time              `json:"timestamp"`
	MitigatedAt *time.Time             `json:"mitigated_at,omitempty"`
}

// NewComplianceEnforcer creates a new enforcer with default OBE3 rules
func NewComplianceEnforcer(mode string) *ComplianceEnforcer {
	return &ComplianceEnforcer{
		rules:  getDefaultOBE3Rules(),
		mode:   mode,
		verifier: &DefaultVerifier{},
	}
}

// getDefaultOBE3Rules returns standard OBE3 compliance rules
func getDefaultOBE3Rules() []ComplianceRule {
	return []ComplianceRule{
		{
			ID:          "OBE3-001",
			Name:        "No Unvalidated User Input",
			Description: "All user inputs must be validated and sanitized",
			Severity:    Critical,
			Category:    CatSecurity,
			Pattern:     `(?i)(eval|exec|system)\s*\(\s*\w+`,
			Action:      "Implement input validation before execution",
			Enabled:     true,
		},
		{
			ID:          "OBE3-002",
			Name:        "Authentication Required",
			Description: "Sensitive operations require authentication",
			Severity:    Critical,
			Category:    CatSecurity,
			Pattern:     `(?i)(admin|secret|password)\s*=\s*(true|false)` + `(?!\s*requireAuth)`,
			Action:      "Add authentication middleware",
			Enabled:     true,
		},
		{
			ID:          "OBE3-003",
			Name:        "Audit Logging Required",
			Description: "Critical actions must be logged for audit trail",
			Severity:    High,
			Category:    CatAudit,
			Pattern:     `(?i)(delete|update|modify)\s*[;(]` + `(?!\s*log)`,
			Action:      "Add audit logging before state changes",
			Enabled:     true,
		},
		{
			ID:          "OBE3-004",
			Name:        "Evidence Chain Integrity",
			Description: "All security decisions must have verifiable evidence chain",
			Severity:    High,
			Category:    CatAudit,
			Pattern:     `\b(decide|enforce|remediate)\b` + `(?!\s*\.\(Record\)|.*evidence\.Ledger)`,
			Action:      "Integrate with evidence ledger",
			Enabled:     true,
		},
		{
			ID:          "OBE3-005",
			Name:        "No Hardcoded Secrets",
			Description: "Secrets must come from secure storage, not code",
			Severity:    Critical,
			Category:    CatSecurity,
			Pattern:     `(?i)(password|api_key|secret)\s*=\s*["\'][^"\']+["\']`,
			Action:      "Use secrets manager or environment variables",
			Enabled:     true,
		},
		{
			ID:          "OBE3-006",
			Name:        "Rate Limiting Required",
			Description: "API endpoints must implement rate limiting",
			Severity:    Medium,
			Category:    CatPolicy,
			Pattern:     `(?i)(http\.Handle|router\.Get)\s*\(.*"/api/` + `(?!\s*\.(Use|With)\s*\(rateLimit))`,
			Action:      "Add rate limiting middleware",
			Enabled:     true,
		},
	}
}

// ValidateRemediation validates a remediation recommendation
func (ce *ComplianceEnforcer) ValidateRemediation(ctx context.Context, remResponse RemediationResponse) (*ComplianceReport, error) {
	report := &ComplianceReport{
		Version:       "v1.0",
		RuleCount:     len(ce.rules),
		CompliantRules: make([]string, 0),
		ViolatedRules: make([]ViolatedRule, 0),
		Decisions:   make([]ComplianceDecision, 0),
		Timestamp:   time.Now(),
		Mode:        ce.mode,
	}

	for _, rule := range ce.rules {
		if !rule.Enabled {
			continue
		}

		// Check all text fields against rule pattern
		textToCheck := []string{
			remResponse.RootCause,
			remResponse.RawResponse,
		}
		if remResponse.ImmediateActions != nil {
			for _, action := range remResponse.ImmediateActions {
				textToCheck = append(textToCheck, action)
			}
		}

		compliant, result := ce.verifyText(rule, textToCheck)

		if compliant {
			report.CompliantRules = append(report.CompliantRules, rule.ID)
		} else {
			violated := ViolatedRule{
				RuleID:      rule.ID,
				RuleName:    rule.Name,
				Severity:    rule.Severity,
				Category:    rule.Category,
				Violation:   result.Violation,
				Evidence:    result.Evidence,
				Suggestion:  result.Suggestion,
			}
			report.ViolatedRules = append(report.ViolatedRules, violated)

			// Record compliance decision
			decision := ComplianceDecision{
				RuleID:  rule.ID,
				Action:  rule.Action,
				Decision: determineEnforcementDecision(rule.Severity, ce.mode),
				Violations: []string{result.Violation},
				Context: map[string]interface{}{
					"suggested_fix": result.Suggestion,
					"target_field":  "remediation_recommendation",
				},
				Timestamp: time.Now(),
			}
			report.Decisions = append(report.Decisions, decision)
		}
	}

	report.Summary = ce.generateSummary(report)
	return report, nil
}

// verifyText checks text against a single rule
func (ce *ComplianceEnforcer) verifyText(rule ComplianceRule, texts []string) (bool, VerificationResult) {
	result := VerificationResult{
		Timestamp: time.Now(),
	}

	for i, text := range texts {
		compliant, violation := ce.verifier.VerifyPattern(rule.Pattern, text)
		
		if !compliant {
			result.Compliant = false
			result.Violation = violation
			result.Evidence = truncateString(text, 500)
			result.Suggestion = rule.Action
			
			// Add context about which field was checked
			fieldNames := map[int]string{
				0: "root_cause",
				1: "raw_response",
			}
			if i < len(fieldNames) {
				result.Violation = fmt.Sprintf("[%s] %s", fieldNames[i], result.Violation)
			}
			
			return false, result
		}
	}

	result.Compliant = true
	return true, result
}

// generateSummary creates summary statistics
func (ce *ComplianceEnforcer) generateSummary(report *ComplianceReport) ReportSummary {
	total := len(report.CompliantRules) + len(report.ViolatedRules)
	compliantCount := len(report.CompliantRules)
	
	variancePercent := float64(compliantCount) / float64(total) * 100
	
	// Count violations by severity
	severityCount := make(map[Severity]int)
	for _, v := range report.ViolatedRules {
		severityCount[v.Severity]++
	}
	
	criticalViolations := severityCount[Critical]
	highViolations := severityCount[High]
	
	overallStatus := StatusCompliant
	if criticalViolations > 0 {
		overallStatus = StatusCritical
	} else if highViolations > 0 || len(report.ViolatedRules) > 0 {
		overallStatus = StatusWarning
	}
	
	return ReportSummary{
		TotalRules:        total,
		CompliantCount:    compliantCount,
		ViolatedCount:     len(report.ViolatedRules),
		ComplianceRate:    math.Round(variancePercent*10) / 10,
		CriticalViolations: criticalViolations,
		HighViolations:    highViolations,
		OverallStatus:     overallStatus,
		Recommendation:    ce.getRecommendation(overallStatus),
	}
}

// getRecommendation provides recommendation based on status
func (ce *ComplianceEnforcer) getRecommendation(status ComplianceStatus) string {
	switch status {
	case StatusCritical:
		return "Immediate remediation required before deployment"
	case StatusWarning:
		return "Review and address violations before next release"
	case StatusCompliant:
		return "Compliance requirements met"
	default:
		return "Manual review required"
	}
}

// determineEnforcementDecision determines how strictly to enforce
func determineEnforcementDecision(severity Severity, mode string) string {
	if mode == capability.Real {
		if severity == Critical {
			return "deny" // Block critical violations in real mode
		}
		if severity == High {
			return "warn" // Warn but allow with approval
		}
		return "allow" // Allow lower severities
	}
	return "warn" // Always warn in simulation mode
}

// DefaultVerifier implements basic pattern verification
type DefaultVerifier struct{}

// VerifyPattern checks if content matches rule pattern
func (v *DefaultVerifier) VerifyPattern(pattern, content string) (bool, string) {
	if pattern == "" {
		return true, ""
	}
	
	matches, err := regexp.MatchString(pattern, content)
	if err != nil {
		return true, fmt.Sprintf("Invalid regex pattern: %s", pattern)
	}
	
	if matches {
		return false, "Potential violation detected by pattern matching"
	}
	
	return true, ""
}

// Verify implements RuleVerifier interface
func (v *DefaultVerifier) Verify(ctx context.Context, rule ComplianceRule, content string) (bool, VerificationResult) {
	compliant, violation := v.VerifyPattern(rule.Pattern, content)
	return compliant, VerificationResult{
		Compliant: compliant,
		Violation: violation,
		Timestamp: time.Now(),
	}
}

// Additional methods would include advanced pattern matching, 
// ML-based detection, and integration with policy engines