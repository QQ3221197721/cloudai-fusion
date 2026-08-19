
// Package metasploit - Data structures for Metasploit Framework integration
package metasploit

import (
	"time"
)

// ExploitModule represents a Metasploit exploit module
type ExploitModule struct {
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	CVE            []string      `json:"cve_references"`
	CVSSScore      float64       `json:"cvss_score,omitempty"`
	DefaultPayload string        `json:"default_payload"`
	Platform       []string      `json:"platforms"`
	TargetPlatforms []string     `json:"target_platforms"`
	Status         string        `json:"status"` // perfect, normal, manual, unstable, unknown
	Ref            []string      `json:"references"` // CVE, BID, URL, etc.
}

// ServiceInfo represents detected network service information
type ServiceInfo struct {
	Name    string   `json:"service_name"`
	Version string   `json:"version"`
	Protocol string  `json:"protocol"`
	Port     int      `json:"port"`
	CVEs     []string `json:"associated_cves,omitempty"`
	Banner   string   `json:"banner,omitempty"`
}

// VulnerabilityReport summarizes scan results for a target
type VulnerabilityReport struct {
	Target          TargetInfo      `json:"target"`
	ScannedAt       time.Time       `json:"scanned_at"`
	Vulnerabilities []ExploitFinding `json:"vulnerabilities"`
	RiskLevel       string          `json:"risk_level"` // critical, high, medium, low
}

// ExploitFinding represents a specific vulnerability-exploit pairing
type ExploitFinding struct {
	CVE           string        `json:"cve"`
	Exploit       ExploitModule `json:"exploit"`
	RiskScore     float64       `json:"risk_score"`
	Remediation   string        `json:"remediation"`
	PoCAvailable  bool          `json:"poc_available"`
}

// PenTestReport is the comprehensive penetration test report
type PenTestReport struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Targets     []TargetInfo  `json:"targets"`
	Findings    []Finding     `json:"findings"`
	Sessions    []*SessionInfo `json:"active_sessions"`
	Summary     ReportSummary  `json:"summary"`
}

// Finding represents a single finding in the penetration test report
type Finding struct {
	Target      TargetInfo  `json:"target"`
	Vulnerability ExploitFinding `json:"vulnerability"`
	Remediation string      `json:"remediation"`
}

// ReportSummary provides overview statistics
type ReportSummary struct {
	TotalTargets       int     `json:"total_targets"`
	TotalFindings      int     `json:"total_findings"`
	CriticalFindings   int     `json:"critical_findings"`
	HighFindings       int     `json:"high_findings"`
	MediumFindings     int     `json:"medium_findings"`
	LowFindings        int     `json:"low_findings"`
	ActiveSessions     int     `json:"active_sessions"`
	AverageRiskScore   float64 `json:"average_risk_score"`
	HighestRiskTarget  string  `json:"highest_risk_target"`
}

// AttackChain represents an automated attack sequence
type AttackChain struct {
	ID              string          `json:"chain_id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Stages          []AttackStage   `json:"stages"`
	StartTime       time.Time       `json:"start_time"`
	EndTime         *time.Time      `json:"end_time,omitempty"`
	Status          string          `json:"status"` // running, completed, failed
	Success         bool            `json:"success"`
	TargetSystem    string          `json:"target_system"`
	ExploitsUsed    []string        `json:"exploits_used"`
	DurationSeconds int             `json:"duration_seconds"`
}

// AttackStage represents a single stage in the attack chain
type AttackStage struct {
	Order        int               `json:"order"`
	Action       string            `json:"action"`
	Exploit      ExploitModule     `json:"exploit"`
	Target       TargetInfo        `json:"target"`
	Privileges   string            `json:"privileges,omitempty"`
	Result       string            `json:"result"` // success, failure, skipped
	SessionID    string            `json:"session_id,omitempty"`
	Timestamp    time.Time         `json:"timestamp"`
	RiskScore    float64           `json:"risk_score"`
	Notes        string            `json:"notes,omitempty"`
}

// RemediationPlan contains recommended fixes for identified vulnerabilities
type RemediationPlan struct {
	Vulnerability string        `json:"vulnerability"`
	Priority      string        `json:"priority"` // critical, high, medium, low
	Actions       []FixAction   `json:"actions"`
	EstimatedEffort string      `json:"estimated_effort"` // low, medium, high
	References    []string      `json:"references"`
}

// FixAction is a specific remediation step
type FixAction struct {
	Type        string `json:"type"` // patch, configure, disable, monitor
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
	DocumentationURL string `json:"documentation_url,omitempty"`
}

// Helper functions for risk calculation

// calculateRiskScore computes combined risk score
func calculateRiskScore(exploit ExploitModule, target TargetInfo) float64 {
	baseScore := 0.0
	
	if exploit.CVSSScore > 0 {
		baseScore = exploit.CVSSScore
	} else {
		// Fallback scoring
		switch len(exploit.CVE) {
		case 0:
			baseScore = 3.0
		case 1:
			baseScore = 5.0
		default:
			baseScore = 7.0
		}
	}
	
	// Adjust for exploit status
	statusFactor := map[string]float64{
		"perfect":   1.2,
		"normal":    1.0,
		"manual":    0.8,
		"unstable":  0.6,
		"unknown":   0.5,
	}
	
	factor := statusFactor[exploit.Status]
	if factor == 0 {
		factor = 1.0
	}
	
	return baseScore * factor
}

// getRemediation generates remediation advice
func getRemediation(cve string) string {
	// In production, this would query external knowledge base
	// For now, return generic advice
	return "Apply security patches from vendor and implement compensating controls"
}
