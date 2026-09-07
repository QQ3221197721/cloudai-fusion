package auto_remediation

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// Orchestrator coordinates all remediation components
type Orchestrator struct {
	llmClient       LLMClient
	promptEngine    *PromptEngine
	aggregator      *FindingsAggregator
	enforcer        *ComplianceEnforcer
	reportGenerator *ReportGenerator
	evidenceLedger  *evidence.Ledger
	mode            string
}

// NewOrchestrator creates a new remediation orchestrator
func NewOrchestrator(
	llmClient LLMClient,
	promptEngine *PromptEngine,
	enforcer *ComplianceEnforcer,
	reportGenerator *ReportGenerator,
	evidenceLedger *evidence.Ledger,
	mode string,
) *Orchestrator {
	return &Orchestrator{
		llmClient:       llmClient,
		promptEngine:    promptEngine,
		aggregator:      NewFindingsAggregator(context.Background()),
		enforcer:        enforcer,
		reportGenerator: reportGenerator,
		evidenceLedger:  evidenceLedger,
		mode:            mode,
	}
}

// ProcessVulnerability performs end-to-end vulnerability remediation
func (o *Orchestrator) ProcessVulnerability(ctx context.Context, vuln Vulnerability, findings []Finding) (*FullRemediationReport, error) {
	logger.Infof("Starting vulnerability remediation for %s", vuln.CVE)
	
	startTime := time.Now()
	defer func() {
		logger.Infof("Vulnerability remediation completed in %v", time.Since(startTime))
	}()
	
	// Aggregate findings first
	o.aggregator.AddMultiple(findings)
	aggResult := o.aggregator.Aggregate(ctx)
	
	logger.Infof("Found %d total vulnerabilities, risk score: %.1f", 
		aggResult.TotalFindings, aggResult.RiskScore)
	
	// Create remediation data
	remData := RemediationData{
		Vulnerability:         vuln,
		Findings:              findings,
		ATT&CKTactic:          getMitreTactic(vuln),
		BusinessContext:       "Production environment - high availability required",
		ComplianceRules:       o.enforcer.rules,
		GeneratePoC:           true,
		RequireMITREMapping:   true,
		IncludeExecutiveSummary: true,
	}
	
	// Generate full report
	report, err := o.reportGenerator.GenerateFullReport(ctx, remData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate report: %w", err)
	}
	
	// Validate against compliance rules
	if report.Remediation != nil {
		complianceReport, err := o.enforcer.ValidateRemediation(ctx, *report.Remediation)
		if err != nil {
			logger.Warnf("Compliance validation failed: %v", err)
		} else {
			report.Compliance = complianceReport
			logger.Infof("Compliance check: %.0f%% compliant (%d/%d rules)", 
				complianceReport.Summary.ComplianceRate,
				complianceReport.Summary.CompliantCount,
				complianceReport.Summary.TotalRules)
		}
	}
	
	// Record in evidence ledger
	if o.evidenceLedger != nil {
		if _, err := o.recordInEvidence(ctx, vuln, report); err != nil {
			logger.Warnf("Failed to record in evidence ledger: %v", err)
		}
	}
	
	return report, nil
}

// recordInEvidence records remediation decision in evidence ledger
func (o *Orchestrator) recordInEvidence(ctx context.Context, vuln Vulnerability, report *FullRemediationReport) (*evidence.Evidence, error) {
	input := evidence.RecordInput{
		Actor:   "auto_remediation_agent",
		Action:  "remediation.generate",
		Subject: vuln.CVE,
		Input: map[string]interface{}{
			"cve":         vuln.CVE,
			"type":        vuln.Type,
			"cvss_score":  vuln.CVSSScore,
			"findings_count": len(report.RawData.Findings),
		},
		Output: map[string]interface{}{
			"remediation_generated": report.Remediation != nil,
			"poc_generated":         report.PoC != "",
			"mitre_mapped":          report.MITREMapping != nil,
		},
		Payload: report,
	}
	
	return o.evidenceLedger.Record(ctx, input)
}

// AddFinding adds a finding for aggregation
func (o *Orchestrator) AddFinding(finding Finding) {
	o.aggregator.AddFinding(finding)
}

// GetAggregationResult returns current aggregation state
func (o *Orchestrator) GetAggregationResult(ctx context.Context) *AggregationResult {
	return o.aggregator.Aggregate(ctx)
}

// ProcessBulkVulnerabilities processes multiple vulnerabilities
func (o *Orchestrator) ProcessBulkVulnerabilities(ctx context.Context, vulns []Vulnerability) ([]*FullRemediationReport, error) {
reports := make([]*FullRemediationReport, len(vulns))
	
	for i, vuln := range vulns {
		// Extract findings for this vulnerability
		var findings []Finding
		for _, f := range o.aggregator.GetFindings() {
			if strings.Contains(f.Location, vuln.Component) || 
			   strings.Contains(f.Type, vuln.Type) {
				findings = append(findings, f)
			}
		}
		
		report, err := o.ProcessVulnerability(ctx, vuln, findings)
		if err != nil {
			logger.Errorf("Failed to process %s: %v", vuln.CVE, err)
			continue
		}
		
		reports[i] = report
		
		// Clear aggregator for next iteration if needed
		if len(vulns) > 1 && i < len(vulns)-1 {
			o.aggregator.Clear()
		}
	}
	
	return reports, nil
}

// GenerateQuickReport generates a simplified report for rapid response
func (o *Orchestrator) GenerateQuickReport(ctx context.Context, vuln Vulnerability, topFindings []Finding) (string, error) {
	// Fast path: skip PoC generation, use minimal prompts
	
	remData := RemediationData{
		Vulnerability:         vuln,
		Findings:              topFindings[:min(len(topFindings), 3)],
		GeneratePoC:           false,
		RequireMITREMapping:   false,
		IncludeExecutiveSummary: false,
	}
	
	report, err := o.reportGenerator.GenerateFullReport(ctx, remData)
	if err != nil {
		return "", err
	}
	
	if report.Remediation == nil {
		return "", errors.New("no remediation generated")
	}
	
	// Format as markdown for quick reading
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("## Quick Remediation for %s\n\n", vuln.CVE))
	buf.WriteString("**Root Cause:** " + report.Remediation.RootCause + "\n\n")
	
	if report.Remediation.ImmediateActions != nil {
		buf.WriteString("**Immediate Actions:**\n\n")
		for _, action := range report.Remediation.ImmediateActions[:min(len(report.Remediation.ImmediateActions), 2)] {
			buf.WriteString(fmt.Sprintf("- %s\n", action))
		}
	}
	
	return buf.String(), nil
}

// Helper functions
func getMitreTactic(vuln Vulnerability) string {
	// Simple mapping based on vulnerability type
	typeToTactic := map[string]string{
		"sql_injection":     "Impact",
		"injection":         "Impact",
		"xss":               "Initial Access",
		"csrf":             "Initial Access",
		"authentication":    "Initial Access",
		"authorization":    "Privilege Escalation",
		"information_disclosure": "Discovery",
		"brute_force":      "Initial Access",
	}
	
	for key, tactic := range typeToTactic {
		if strings.Contains(strings.ToLower(vuln.Type), key) {
			return tactic
		}
	}
	
	return "Unknown"
}

// Ensure all imports are used
var _ = fmt.Sprintf
var _ = time.Now