package auto_remediation

import (
	"context"
	"time"
)

// ReportGenerator creates formatted reports from remediation data
type ReportGenerator struct {
	llmClient  LLMClient
	promptEngine *PromptEngine
	mode       string
}

// NewReportGenerator creates a new report generator
func NewReportGenerator(llmClient LLMClient, promptEngine *PromptEngine, mode string) *ReportGenerator {
	return &ReportGenerator{
		llmClient:    llmClient,
		promptEngine: promptEngine,
		mode:         mode,
	}
}

// GenerateFullReport generates a comprehensive remediation report
func (rg *ReportGenerator) GenerateFullReport(ctx context.Context, data RemediationData) (*FullRemediationReport, error) {
	report := &FullRemediationReport{
		Version:     "v1.0",
		GeneratedAt: time.Now(),
		Mode:        rg.mode,
	}

	var err error
	
	// Generate remediation recommendation
	if report.Remediation, err = rg.promptEngine.GenerateRemediation(ctx, rg.llmClient, data); err != nil {
		logger.Errorf("Failed to generate remediation: %v", err)
	}

	// Generate PoC if requested
	if data.GeneratePoC {
		logger.Info("Generating proof-of-concept code")
		if report.PoC, err = rg.promptEngine.GeneratePoC(ctx, rg.llmClient, data.Vulnerability); err != nil {
			logger.Errorf("Failed to generate PoC: %v", err)
		}
	}

	// Map to MITRE ATT&CK
	if data.RequireMITREMapping {
		logger.Info("Mapping to MITRE ATT&CK framework")
		if report.MITREMapping, err = rg.promptEngine.MapToMitRE(ctx, rg.llmClient, data.Vulnerability); err != nil {
			logger.Errorf("Failed to map to MITRE ATT&CK: %v", err)
		}
	}

	// Generate executive summary
	if data.IncludeExecutiveSummary {
		logger.Info("Generating executive summary")
		summaryData := SummaryData{
			VulnCount:         len(data.Findings),
			HighCriticalCount: countBySeverity(data.Findings, High, Critical),
			AssetCount:        len(affectedAssets(data.Findings)),
			BusinessRisk:      calculateBusinessRisk(data),
			KeyFindings:       extractKeyFindings(data.Findings),
			AttackScenarios:   constructAttackScenarios(data),
			BusinessImpactStatement: data.BusinessContext,
			RecommendedActions: extractActionItems(report.Remediation),
			BudgetEstimate:    estimateBudget(data),
			Timeline:          estimateTimeline(data),
			TeamResources:     estimateTeamResources(data),
		}
		
		if report.ExecutiveSummary, err = rg.promptEngine.GenerateExecutiveSummary(ctx, rg.llmClient, summaryData); err != nil {
			logger.Errorf("Failed to generate executive summary: %v", err)
		}
	}

	return report, nil
}

// countBySeverity counts findings by severity levels
func countBySeverity(findings []Finding, severities ...Severity) int {
	count := 0
	for _, f := range findings {
		for _, s := range severities {
			if f.Severity == s {
				count++
				break
			}
		}
	}
	return count
}

// affectedAssets extracts unique asset locations from findings
func affectedAssets(findings []Finding) []string {
	assets := make(map[string]bool)
	for _, f := range findings {
		assets[f.Location] = true
	}
	result := make([]string, 0, len(assets))
	for asset := range assets {
		result = append(result, asset)
	}
	return result
}

// extractKeyFindings identifies top findings for executive audience
func extractKeyFindings(findings []Finding) []string {
	const MaxFindings = 5
	var findingsList []string
	
	// Sort by severity and CVSS
	sorted := make([]Finding, len(findings))
	copy(sorted, findings)
	sort.Slice(sorted, func(i, j int) bool {
		scoreI := sorted[i].Severity.Weight() * sorted[i].CVSSScore
		scoreJ := sorted[j].Severity.Weight() * sorted[j].CVSSScore
		return scoreI > scoreJ
	})
	
	for _, f := range sorted[:min(len(sorted), MaxFindings)] {
		desc := fmt.Sprintf("%s (%.1f CVSS) in %s - %s", 
			f.Type, f.CVSSScore, f.Location, f.Severity.String())
		findingsList = append(findingsList, desc)
	}
	
	return findingsList
}

// constructAttackScenarios builds narrative attack paths
func constructAttackScenarios(data RemediationData) []string {
	var scenarios []string
	
	// Simple scenario construction based on findings
	if len(data.Findings) >= 2 {
		scenario := fmt.Sprintf(
			"Attacker could exploit %s to gain initial access, then use %s to escalate privileges",
			data.Findings[0].Type,
			data.Findings[1].Type,
		)
		scenarios = append(scenarios, scenario)
	}
	
	return scenarios
}

// extractActionItems pulls action items from remediation response
func extractActionItems(rem *RemediationResponse) []string {
	var actions []string
	
	if rem.ImmediateActions != nil {
		for i, action := range rem.ImmediateActions {
			if i < 3 { // Top 3 actions
				actions = append(actions, action)
			}
		}
	}
	
	return actions
}

// estimateBudget provides rough budget estimation
func estimateBudget(data RemediationData) string {
	// Simplified estimation logic
	baseCost := len(data.Findings) * 4 // Hours per finding
	hourlyRate := 150 // Average consulting rate
	
	total := baseCost * hourlyRate
	return fmt.Sprintf("$%d - $%d USD", total-20000, total+20000)
}

// estimateTimeline provides timeline estimates
func estimateTimeline(data RemediationData) string {
	if len(data.Findings) == 0 {
		return "< 1 week"
	}
	
	criticalCount := countBySeverity(data.Findings, Critical)
	highCount := countBySeverity(data.Findings, High)
	
	totalWeeks := criticalCount*2 + highCount + len(data.Findings)
	
	return fmt.Sprintf("%d-%d weeks", totalWeeks, totalWeeks+2)
}

// estimateTeamResources provides team resource recommendations
func estimateTeamResources(data RemediationData) string {
	requiredRoles := []string{"Security Engineer", "DevOps Engineer"}
	
	if len(data.Findings) > 10 {
		requiredRoles = append(requiredRoles, "Security Manager")
	}
	
	return strings.Join(requiredRoles, ", ")
}

// GenerateExecutiveReportPDF would export report as PDF
// Implementation would integrate with PDF generation library
func (rg *ReportGenerator) GenerateExecutiveReportPDF(ctx context.Context, report *FullRemediationReport) ([]byte, error) {
	// Placeholder - integrate with PDF library
	// This would be implemented using libraries like gopdf or external service
	return nil, errors.New("PDF generation not yet implemented")
}

// GenerateMarkdownReport exports report in Markdown format
func (rg *ReportGenerator) GenerateMarkdownReport(report *FullRemediationReport) (string, error) {
	var buf bytes.Buffer
	
	buf.WriteString("# Security Vulnerability Remediation Report\n\n")
	buf.WriteString(fmt.Sprintf("**Generated:** %s\n**Mode:** %s\n\n", 
		report.GeneratedAt.Format(time.RFC3339), report.Mode))
	
	if report.ExecutiveSummary != "" {
		buf.WriteString("## Executive Summary\n\n")
		buf.WriteString(report.ExecutiveSummary + "\n\n")
	}
	
	if report.Remediation != nil {
		buf.WriteString("## Technical Analysis\n\n")
		
		buf.WriteString("### Root Cause\n\n")
		buf.WriteString(report.Remediation.RootCause + "\n\n")
		
		if report.Remediation.ImmediateActions != nil && len(report.Remediation.ImmediateActions) > 0 {
			buf.WriteString("### Immediate Actions Required\n\n")
			for i, action := range report.Remediation.ImmediateActions {
				buf.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, action))
			}
		}
		
		if report.Remediation.LongTermPrevention != "" {
			buf.WriteString("### Long-term Prevention\n\n")
			buf.WriteString(report.Remediation.LongTermPrevention + "\n\n")
		}
	}
	
	if report.MITREMapping != nil {
		buf.WriteString("## MITRE ATT&CK Mapping\n\n")
		buf.WriteString(generateMITRESection(report.MITREMapping))
	}
	
	if report.PoC != "" {
		buf.WriteString("\n## Proof of Concept\n\n")
		buf.WriteString("```python\n")
		buf.WriteString(report.PoC)
		buf.WriteString("\n```\n")
	}
	
	return buf.String(), nil
}

// generateMITRESection formats MITRE mapping data
func generateMITRESection(mapping *MitREMapping) string {
	var buf bytes.Buffer
	
	if len(mapping.Tactics) > 0 {
		buf.WriteString("### Tactics\n\n")
		for _, tactic := range mapping.Tactics {
			buf.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", 
				tactic.Name, tactic.ID, tactic.Evidence))
		}
		buf.WriteString("\n")
	}
	
	if len(mapping.Techniques) > 0 {
		buf.WriteString("### Techniques\n\n")
		for _, tech := range mapping.Techniques {
			buf.WriteString(fmt.Sprintf("#### %s (%s)\n\n", tech.Name, tech.ID))
			buf.WriteString(tech.Description + "\n\n")
			buf.WriteString(fmt.Sprintf("**Evidence:** %s\n", tech.Evidence))
			buf.WriteString(fmt.Sprintf("**Confidence:** %.0f%%\n\n", tech.Confidence*100))
		}
	}
	
	return buf.String()
}

// FullRemediationReport contains all report sections
type FullRemediationReport struct {
	Version           string                 `json:"version"`
	GeneratedAt       time.Time              `json:"generated_at"`
	Mode            string                 `json:"mode"`
	Remediation     *RemediationResponse   `json:"remediation,omitempty"`
	PoC             string                 `json:"poc,omitempty"`
	MITREMapping    *MitREMapping          `json:"mitre_mapping,omitempty"`
	ExecutiveSummary string               `json:"executive_summary,omitempty"`
	RawData         RemediationData        `json:"raw_data,omitempty"`
}