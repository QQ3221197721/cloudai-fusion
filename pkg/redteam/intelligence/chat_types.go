
package redteam

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// ChatIntent represents different types of user commands
type ChatIntent string

const (
	IntentGenericQuery      ChatIntent = "generic_query"
	IntentLaunchAttack      ChatIntent = "launch_attack"
	IntentGenerateReport    ChatIntent = "generate_report"
	IntentAnalyzeVulnerability ChatIntent = "analyze_vulnerability"
	IntentBuildAttackPath   ChatIntent = "build_attack_path"
	IntentTriggerAutoRemediation ChatIntent = "trigger_auto_remediation"
	IntentCapabilitiesOverview ChatIntent = "capabilities_overview"
)

// APIStepType defines different types of API operations
type APIStepType string

const (
	APIStepKnowledgeBaseQuery     APIStepType = "knowledge_base_query"
	APIStepLaunchCVEExploit       APIStepType = "launch_cve_exploit"
	APIStepGenerateReport         APIStepType = "generate_report"
	APIStepAnalyzeVulnerability   APIStepType = "analyze_vulnerability"
	APIStepBuildAttackPath        APIStepType = "build_attack_path"
	APIStepTriggerAutoRemediation APIStepType = "trigger_auto_remediation"
)

// APIStep represents a single operation in a multi-step workflow
type APIStep struct {
	Type            APIStepType          `json:"type"`
	Description     string               `json:"description"`
	Parameters      map[string]interface{} `json:"parameters"`
	RequiresApproval bool                 `json:"requires_approval"`
	AutoApproved    bool                 `json:"auto_approved"`
	Priority        int                  `json:"priority"`
	TimeoutSec      int                  `json:"timeout_sec"`
}

// ChatResponse contains the response from the chat handler
type ChatResponse struct {
	Message           string           `json:"message"`
	Suggestions       []string         `json:"suggestions"`
	APIResults        []string         `json:"api_results,omitempty"`
	ErrorCount        int              `json:"error_count"`
	HasHighRiskAction bool             `json:"has_high_risk_action"`
	Timestamp         time.Time        `json:"timestamp"`
}

// buildPromptTemplates creates reusable prompt templates
func determineRiskLevel(steps []APIStep) RiskLevel {
	highRiskTypes := []APIStepType{
		APIStepLaunchCVEExploit,
		APIStepTriggerAutoRemediation,
	}
	
	for _, step := range steps {
		for _, highRisk := range highRiskTypes {
			if step.Type == highRisk {
				return RiskHigh
			}
		}
	}
	
	return RiskLow
}

func hasHighRiskAction(steps []APIStep) bool {
	highRiskTypes := []APIStepType{
		APIStepLaunchCVEExploit,
		APIStepTriggerAutoRemediation,
	}
	
	for _, step := range steps {
		for _, highRisk := range highRiskTypes {
			if step.Type == highRisk && !step.AutoApproved {
				return true
			}
		}
	}
	
	return false
}

// Helper functions for NLP
func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func extractKeyword(message string) string {
	parts := strings.Split(message, " ")
	if len(parts) > 2 {
		return parts[len(parts)-1]
	}
	return message
}

func extractReportType(message string) string {
	if strings.Contains(message, "vulnerability") {
		return "vulnerability"
	} else if strings.Contains(message, "risk") {
		return "risk_assessment"
	} else if strings.Contains(message, "compliance") {
		return "compliance"
	}
	return "general"
}

// generateUUID creates a simple UUID-like identifier
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

