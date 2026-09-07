package auto_remediation

import (
	"bytes"
	"strings"
	"text/template"
	"time"
)

// PromptEngine manages all prompt templates for remediation tasks
type PromptEngine struct {
	prompts map[string]*template.Template
	sanitizer InputSanitizer
}

// NewPromptEngine creates a new prompt engine with templates
func NewPromptEngine() *PromptEngine {
	engine := &PromptEngine{
		prompts: make(map[string]*template.Template),
		sanitizer: NewInputSanitizer(),
	}
	
	engine.initTemplates()
	return engine
}

// initTemplates initializes all prompt templates
func (pe *PromptEngine) initTemplates() {
	// Remediation Recommendation Template
	pe.prompts["remediation_recommendation"] = template.Must(template.New("remediation").Parse(`
You are an expert security analyst specializing in vulnerability remediation.

## Context
CVE: {{.CVE}}
CVSS Score: {{.CVSSScore}}
Vulnerability Type: {{.VulnType}}
MITRE ATT&CK Tactic: {{.ATT&CKTactic}}
{{if .ATT&CKTechnique}}MITRE ATT&CK Technique: {{.ATT&CKTechnique}}{{end}}

## Previous Remediation History
{{if .RemediationHistory}}
{{range .RemediationHistory}}- {{.Action}} by {{.Actor}} at {{.Timestamp}}{{end}}
{{else}}No previous remediation attempts found.{{end}}

## Policy Compliance Rules
{{if .ComplianceRules}}
{{range .ComplianceRules}}- {{.Rule}}({{.Severity}}){{end}}
{{else}}No specific compliance rules applied.{{end}}

## Findings Summary
{{range $idx, $finding := .Findings}}
Finding #{{$idx + 1}}:
- Type: {{.Type}}
- Severity: {{.Severity}}
- Location: {{.Location}}
- Evidence: {{.Evidence | trunc 200}}
{{end}}

## Task
Generate a comprehensive remediation recommendation including:
1. **Root Cause Analysis**: Why this vulnerability exists
2. **Immediate Fixes**: Step-by-step remediation instructions
3. **Long-term Prevention**: Architectural changes to prevent recurrence
4. **Validation Steps**: How to verify the fix is effective
5. **Rollback Plan**: What to do if the fix causes issues

Output format: JSON object with fields: root_cause, immediate_actions[], long_term_prevention, validation_steps[], rollback_plan

Constraints:
- Be specific and actionable
- Include code snippets where applicable
- Reference industry standards (OWASP, MITRE ATT&CK, CISA KEV)
- Consider business impact and risk tolerance
`).TrimSpace())

	// Compliance Verification Template
	pe.prompts["compliance_verification"] = template.Must(template.New("compliance").Parse(`
You are a security compliance auditor. Verify if the proposed remediation meets policy requirements.

## Compliance Framework
Framework: {{.Framework}}
Version: {{.Version}}
Required Controls:
{{range .Controls}}- {{.Name}} (ID: {{.ID}}) - {{.Description}}
{{end}}

## Proposed Remediation
{{.ProposedRemediation}}

## Original Vulnerability Details
CVE: {{.CVE}}
Impact: {{.Impact}}
Current Status: {{.CurrentStatus}}

## Verification Checklist
Please check each control:
{{range .VerificationChecks}}{{.}}
{{end}}

## Output Format
Return JSON:
{
  "compliant": boolean,
  "passed_controls": [control names],
  "failed_controls": [{"name": "name", "reason": "explanation"}],
  "recommendations": ["action items"],
  "risk_level": "low|medium|high|critical",
  "confidence_score": 0.0-1.0
}
`).TrimSpace())

	// Risk Assessment Template
	pe.prompts["risk_assessment"] = template.Must(template.New("risk_assessment").Parse(`
You are a risk assessment specialist. Calculate the updated risk score after potential remediation.

## Current Risk Factors
Base CVSS Score: {{.CVSSScore}}
Exploitability: {{.Exploitability}}
Business Impact: {{.BusinessImpact}}
Asset Criticality: {{.AssetCriticality}}
Exposure Level: {{.ExposureLevel}}
Threat Intelligence: {{.ThreatIntel}}

## Attack Surface Details
{{if .AttackSurface}}
{{range .AttackSurface}}- Asset: {{.Asset}} 
  Public Exposure: {{.PublicExposure}}
  Data Sensitivity: {{.DataSensitivity}}
  Current Protections: {{.Protections}}{{end}}
{{end}}

## Proposed Changes
{{.ProposedChanges}}

## Temporal Factors
Time to Exploit (estimated): {{.TimeToExploit}}
Patch Availability: {{.PatchAvailability}}
Active Exploitation in Wild: {{.ActiveExploitation}}

## Output Requirements
Calculate and return:
{
  "pre_remediation": {
    "overall_risk_score": number (1-10),
    "likelihood": number (1-5),
    "impact": number (1-5),
    "confidence": number (0-1)
  },
  "post_remediation": {
    "overall_risk_score": number (1-10),
    "likelihood": number (1-5),
    "impact": number (1-5),
    "risk_reduction_percentage": number
  },
  "justification": "string explanation",
  "urgency": "immediate|soon|scheduled|monitor"
}
`).TrimSpace())

	// Executive Summary Template
	pe.prompts["executive_summary"] = template.Must(template.New("exec_summary").Parse(`
You are a security executive communicator. Create a concise executive summary from technical findings.

## Technical Briefing
Vulnerabilities Found: {{.VulnCount}}
High/Critical Issues: {{.HighCriticalCount}}
Assets Impacted: {{.AssetCount}}
Estimated Business Risk: {{.BusinessRisk}}

## Key Findings
{{range .KeyFindings}}- {{.}}{{end}}

## Attack Scenarios
{{if .AttackScenarios}}
{{range .AttackScenarios}}{{.}}
{{end}}
{{end}}

## Business Impact Assessment
{{.BusinessImpactStatement}}

## Recommended Actions
{{if .RecommendedActions}}
{{range .RecommendedActions}}{{.}}
{{end}}
{{end}}

## Resource Requirements
Budget Estimate: {{.BudgetEstimate}}
Timeline: {{.Timeline}}
Team Resources Needed: {{.TeamResources}}

## Output Guidelines
Write a professional, non-alarmist summary suitable for C-level executives. Focus on:
- Business risks (not just technical details)
- Clear action items with owners
- Cost/benefit analysis
- Regulatory/compliance implications
- Competitive advantage of addressing now

Length: 300-500 words. Tone: Professional, urgent but controlled.
`)

	// PoC Generation Template
	pe.prompts["poc_generation"] = template.Must(template.New("poc").Parse(`
You are a security researcher creating proof-of-concept (PoC) code.

## Vulnerability Description
CVE: {{.CVE}}
Vulnerability Type: {{.VulnType}}
CVSS Vector: {{.CVSSVector}}
Affected Component: {{.Component}}
Affected Version: {{.Version}}

## Technical Details
{{.TechnicalDescription}}

## Available Information
Input Parameters: {{.InputParams}}
Network Context: {{.NetworkContext}}
Authentication Required: {{.AuthRequired}}
Prerequisites: {{.Prerequisites}}

## Safety Constraints
1. NEVER include destructive operations
2. Add clear warnings about authorized use only
3. Include error handling
4. Make it reversible/safe to run
5. Add comments explaining each step

## Code Generation Requirements
Generate a complete PoC script that:
1. Demonstrates the vulnerability safely
2. Has clear input/output logging
3. Includes usage instructions
4. Contains safety checks
5. Works across common environments

Output Format: Complete Python/Go/Bash script with detailed comments.
Include: imports, main function, examples, error handling.
`)

	// Mitre Mapping Template
	pe.prompts["mitre_mapping"] = template.Must(template.New("mitre_mapping").Parse(`
You are a threat intelligence analyst mapping vulnerabilities to MITRE ATT&CK framework.

## Vulnerability Characteristics
CVE: {{.CVE}}
Description: {{.VulnDescription}}
Attack Vector: {{.AttackVector}}
Privileges Required: {{.PrivilegesRequired}}
User Interaction: {{.UserInteraction}}
Scope: {{.Scope}}
Impact Categories: {{.ImpactCategories}}

## Behavior Patterns
Observed Behaviors:
{{range .ObservedBehaviors}}- {{.}}{{end}}

## Known Campaigns
Associated Campaigns:
{{range .Campaigns}}- {{.}}{{end}}

## Mapping Instructions
Map to MITRE ATT&CK:
1. Identify Tactic(s) based on goal/purpose
2. Identify Technique(s) based on methods used
3. Identify Sub-technique(s) for specificity
4. Provide evidence for each mapping

## Output Format
{
  "tactics": [{"id": "TA0001", "name": "Initial Access", "evidence": "..."}],
  "techniques": [
    {
      "id": "T1190", 
      "name": "Exploit Public-Facing Application",
      "subtechniques": [],
      "evidence": "...",
      "confidence": 0.9
    }
  ],
  "mitigation_suggestions": [
    {"id": "M1190", "name": "...", "relevance": "high"}
  ]
}
`)
}

// GenerateRemediation generates remediation recommendations
func (pe *PromptEngine) GenerateRemediation(ctx context.Context, llmClient LLMClient, data RemediationData) (*RemediationResponse, error) {
	// Sanitize input to prevent prompt injection
	data = pe.sanitizer.SanitizeRemediationData(data)
	
	// Execute template
	tmpl := pe.prompts["remediation_recommendation"]
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	
	prompt := buf.String()
	
	// Call LLM
	response, err := llmClient.Complete(ctx, prompt, &CompletionOptions{
		Model:         DashScopeDefault.Name,
		MaxTokens:     4096,
		Temperature:   0.3, // Low temperature for factual responses
		RetryAttempts: 3,
	})
	
	if err != nil {
		return nil, err
	}
	
	// Parse response
	var result RemediationResponse
	if parsed, err := pe.parseJSONResponse(response); err == nil {
		jsonBytes, _ := json.Marshal(parsed)
		json.Unmarshal(jsonBytes, &result)
	} else {
		// Fallback: extract key sections manually
		result = pe.extractStructuredContent(response)
	}
	
	result.GeneratedAt = time.Now()
	result.SourceModel = "qwen3.5-256b"
	
	return &result, nil
}

// GeneratePoC generates proof-of-concept code
func (pe *PromptEngine) GeneratePoC(ctx context.Context, llmClient LLMClient, vuln Vulnerability) (string, error) {
	vuln = pe.sanitizer.SanitizeVulnerability(vuln)
	
	tmpl := pe.prompts["poc_generation"]
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vuln); err != nil {
		return "", err
	}
	
	prompt := buf.String()
	
	// Higher temperature for creative code generation
	response, err := llmClient.Complete(ctx, prompt, &CompletionOptions{
		Model:         DashScopeDefault.Name,
		MaxTokens:     8192,
		Temperature:   0.8,
		RetryAttempts: 2,
	})
	
	return response, err
}

// MapToMitRE maps vulnerability to MITRE ATT&CK
func (pe *PromptEngine) MapToMitRE(ctx context.Context, llmClient LLMClient, vuln Vulnerability) (*MitREMapping, error) {
	vuln = pe.sanitizer.SanitizeVulnerability(vuln)
	
	tmpl := pe.prompts["mitre_mapping"]
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vuln); err != nil {
		return nil, err
	}
	
	prompt := buf.String()
	
	mapping := &MitREMapping{}
	_, err := llmClient.ChatJSON(ctx, []Message{
		{Role: "user", Content: prompt},
	}, mapping, &CompletionOptions{
		Model:       DashScopeDefault.Name,
		Temperature: 0.4,
		ResponseFormat: ResponseFormat{
			Type: "json_object",
		},
	})
	
	return mapping, err
}

// GenerateExecutiveSummary creates executive summary
func (pe *PromptEngine) GenerateExecutiveSummary(ctx context.Context, llmClient LLMClient, summaryData SummaryData) (string, error) {
	tmpl := pe.prompts["executive_summary"]
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, summaryData); err != nil {
		return "", err
	}
	
	prompt := buf.String()
	
	response, err := llmClient.Complete(ctx, prompt, &CompletionOptions{
		Model:         DashScopeDefault.Name,
		MaxTokens:     2048,
		Temperature:   0.7,
		RetryAttempts: 3,
	})
	
	return response, err
}

// parseJSONResponse parses LLM response into JSON
func (pe *PromptEngine) parseJSONResponse(text string) (any, error) {
	cleaned := strings.TrimSpace(text)
	
	// Try direct parse
	var result any
	if err := json.Unmarshal([]byte(cleaned), &result); err == nil {
		return result, nil
	}
	
	// Extract from code block
	start := strings.Index(cleaned, "```json")
	if start == -1 {
		start = strings.Index(cleaned, "```JSON")
	}
	if start == -1 {
		start = strings.Index(cleaned, "```")
	}
	
	if start != -1 {
		start += 3
		end := strings.Index(cleaned[start:], "```")
		if end != -1 {
			jsonStr := cleaned[start : start+end]
			if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
				return result, nil
			}
		}
	}
	
	return nil, errors.New("could not parse JSON response")
}

// extractStructuredContent extracts structured data from unstructured text
func (pe *PromptEngine) extractStructuredContent(text string) RemediationResponse {
	result := RemediationResponse{
		RawResponse: text,
	}
	
	// Simple regex-based extraction
	if idx := strings.Index(text, "root_cause:"); idx != -1 {
		end := findNextSection(text[idx:])
		result.RootCause = strings.TrimSpace(text[idx+11 : end])
	}
	
	return result
}

func findNextSection(text string) int {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 0 && (strings.HasPrefix(line, "##") || strings.HasPrefix(line, "-")) {
			return len(strings.Join(lines[:i], "\n"))
		}
	}
	return len(text)
}