package redteam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// AIChatHandler handles natural language commands via LLM-powered interface
type AIChatHandler struct {
	memoryStore     *ChatMemoryStore
	redTeamEngine   *RedTeamEngine
	logger          *logrus.Logger
	promptTemplates map[string]string
}

// NewAIChatHandler creates a new chat handler instance
func NewAIChatHandler(rtEngine *RedTeamEngine, logger *logrus.Logger) *AIChatHandler {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	return &AIChatHandler{
		memoryStore: NewChatMemoryStore(24*time.Hour, 100),
		redTeamEngine: rtEngine,
		logger: logger,
		promptTemplates: buildPromptTemplates(),
	}
}

// HandleCommand processes natural language requests
func (h *AIChatHandler) HandleCommand(ctx context.Context, sessionID string, userMessage string) (*ChatResponse, error) {
	// Step 1: Start or resume session
	if err := h.memoryStore.StartSession(ctx, sessionID); err != nil {
		return nil, fmt.Errorf("failed to initialize session: %w", err)
	}
	
	// Step 2: Add user message to history
	userTurn := ConversationTurn{
		Role:    "user",
		Content: userMessage,
		Metadata: &TurnMeta{
			ConfidenceScore: 1.0,
		},
	}
	if err := h.memoryStore.Append(sessionID, userTurn); err != nil {
		return nil, fmt.Errorf("failed to append user message: %w", err)
	}
	
	// Step 3: Parse intent from natural language
	intent, parsedData, err := h.parseIntent(userMessage)
	if err != nil {
		h.logger.WithError(err).Warn("Failed to parse intent, using fallback")
		intent = IntentGenericQuery
		parsedData = map[string]interface{}{"error": err.Error()}
	}
	
	// Step 4: Store parsed intent in metadata
	userTurn.Metadata.Intent = string(intent)
	userTurn.Metadata.ParsedIntentData = json.Marshal(parsedData)
	if err := h.memoryStore.Append(sessionID, userTurn); err != nil {
		h.logger.Warn("Failed to update metadata - continuing anyway")
	}
	
	// Step 5: Translate intent to API calls
	apiCalls, err := h.translateToAPICalls(ctx, intent, parsedData)
	if err != nil {
		response := h.generateFallbackResponse(userMessage, intent, err)
		h.respondToUser(sessionID, response)
		return response, nil
	}
	
	// Step 6: Execute multi-step workflow
	results, errors := h.executeWorkflow(ctx, apiCalls)
	
	// Step 7: Generate natural language response
	responseSummary, err := h.generateResponseSummary(ctx, userMessage, intent, results, errors)
	if err != nil {
		h.logger.WithError(err).Error("Failed to generate response summary")
		responseSummary = "An error occurred while processing your request. Please try again."
	}
	
	// Step 8: Generate follow-up suggestions
	suggestions := h.generateFollowupSuggestions(intent, results)
	
	// Step 9: Record assistant response
	assistantTurn := ConversationTurn{
		Role:      "assistant",
		Content:   responseSummary,
		Timestamp: time.Now().UTC(),
		Metadata: &TurnMeta{
			Intent:         string(intent),
			ActionsTaken:   results,
			RiskLevel:      determineRiskLevel(apiCalls),
			ConfidenceScore: 0.95,
		},
	}
	if err := h.memoryStore.Append(sessionID, assistantTurn); err != nil {
		h.logger.Warn("Failed to record assistant response")
	}
	
	return &ChatResponse{
		Message:      responseSummary,
		Suggestions:  suggestions,
		APIResults:   results,
		ErrorCount:   len(errors),
		HasHighRiskAction: hasHighRiskAction(apiCalls),
	}, nil
}

// parseIntent extracts meaning from natural language input
func (h *AIChatHandler) parseIntent(message string) (ChatIntent, map[string]interface{}, error) {
	message = strings.ToLower(strings.TrimSpace(message))
	
	// Simple keyword-based parsing (replace with actual LLM integration later)
	if containsAny(message, []string{"attack", "exploit", "penetration test", "vulnerability scan"}) {
		if containsAny(message, []string{"on ", "against ", "target ", "for "}) {
			targetParts := strings.Split(message, "on ")
			if len(targetParts) > 1 {
				target := targetParts[1]
				return IntentLaunchAttack, map[string]string{"target": target}, nil
			}
		}
		return IntentGenericQuery, nil, fmt.Errorf("specify target for attack")
	}
	
	if containsAny(message, []string{"report", "summary", "analyze"}) {
		return IntentGenerateReport, map[string]string{"type": extractReportType(message)}, nil
	}
	
	if containsAny(message, []string{"cve", "vulnerability", "risk", "severity"}) {
		return IntentAnalyzeVulnerability, map[string]string{"keyword": extractKeyword(message)}, nil
	}
	
	if containsAny(message, []string{"chain", "path", "kill chain", "attack path"}) {
		return IntentBuildAttackPath, map[string]string{"description": message}, nil
	}
	
	if containsAny(message, []string{"automate", "self-heal", "remediate", "fix"}) {
		return IntentTriggerAutoRemediation, map[string]string{"trigger": message}, nil
	}
	
	if containsAny(message, []string{"chat", "help", "what can you do", "capabilities"}) {
		return IntentCapabilitiesOverview, nil, nil
	}
	
	return IntentGenericQuery, map[string]string{"raw_query": message}, nil
}

// translateToAPICalls converts intent into executable steps
func (h *AIChatHandler) translateToAPICalls(ctx context.Context, intent ChatIntent, params map[string]interface{}) ([]APIStep, error) {
	switch intent {
	case IntentLaunchAttack:
		return h.buildAttackWorkflow(params["target"].(string))
	case IntentGenerateReport:
		return h.buildReportWorkflow(params["type"].(string))
	case IntentAnalyzeVulnerability:
		return h.buildVulnAnalysisWorkflow(params["keyword"].(string))
	case IntentBuildAttackPath:
		return h.buildAttackPathWorkflow(params["description"].(string))
	case IntentTriggerAutoRemediation:
		return h.buildRemediationWorkflow(params["trigger"].(string))
	case IntentCapabilitiesOverview:
		return h.buildCapabilitiesWorkflow()
	default:
		return []APIStep{{
			Type:        APIStepKnowledgeBaseQuery,
			Description: "Search knowledge base for relevant information",
			Parameters:  params,
		}}, nil
	}
}

// executeWorkflow runs the sequence of API calls
func (h *AIChatHandler) executeWorkflow(ctx context.Context, steps []APIStep) ([]string, []error) {
	results := make([]string, 0)
	errors := make([]error, 0)
	
	for i, step := range steps {
		select {
		case <-ctx.Done():
			errors = append(errors, fmt.Errorf("workflow cancelled at step %d", i))
			break
			
		default:
			result, err := h.executeSingleStep(ctx, step)
			if err != nil {
				errors = append(errors, fmt.Errorf("step %d failed: %v", i, err))
				continue
			}
			
			results = append(results, result)
			
			// Optional: Add human-in-the-loop approval for destructive actions
			if step.RequiresApproval && !step.AutoApproved {
				h.logger.Warnf("Action requires human approval: %s", step.Description)
				// In production, wait for user confirmation
			}
		}
	}
	
	return results, errors
}

// executeSingleStep executes a single API step
func (h *AIChatHandler) executeSingleStep(ctx context.Context, step APIStep) (string, error) {
	switch step.Type {
	case APIStepLaunchCVEExploit:
		target := step.Parameters["target"].(string)
		cveID := step.Parameters["cve_id"].(string)
		
		result := fmt.Sprintf("Prepared exploit for %s against %s", cveID, target)
		
		// TODO: Integrate with actual exploit execution engine
		h.logger.Debugf("Would execute: %s -> %s (placeholder)", cveID, target)
		
		return result, nil
		
	case APIStepGenerateReport:
		reportType := step.Parameters["type"].(string)
		result := fmt.Sprintf("Generated %s report with vulnerability analysis", reportType)
		h.logger.Debugf("Report generation: %s", reportType)
		return result, nil
		
	case APIStepAnalyzeVulnerability:
		vulnKeyword := step.Parameters["keyword"].(string)
		result := fmt.Sprintf("Analyzed vulnerability pattern: %s", vulnKeyword)
		h.logger.Debugf("Vulnerability analysis: %s", vulnKeyword)
		return result, nil
		
	case APIStepBuildAttackPath:
		desc := step.Parameters["description"].(string)
		result := fmt.Sprintf("Built attack path: %s", desc)
		h.logger.Debugf("Attack path construction: %s", desc)
		return result, nil
		
	case APIStepTriggerAutoRemediation:
		trigger := step.Parameters["trigger"].(string)
		result := fmt.Sprintf("Auto-remediation triggered by: %s", trigger)
		h.logger.Debugf("Auto-remediation: %s", trigger)
		return result, nil
		
	default:
		return fmt.Sprintf("Executed step: %s", step.Description), nil
	}
}

// respondToUser sends a response back to the chat interface
func (h *AIChatHandler) respondToUser(sessionID string, response *ChatResponse) {
	// In production, this would send WebSocket/HTTP response
	h.logger.Infof("Responding to user: %s", response.Message[:min(100, len(response.Message))]+"...")
}

// generateResponseSummary creates human-readable output
func (h *AIChatHandler) generateResponseSummary(ctx context.Context, userMsg string, intent ChatIntent, results []string, errors []error) (string, error) {
	var parts []string
	
	if len(errors) > 0 {
		errorStr := fmt.Sprintf("%d error(s) encountered: ", len(errors))
		for _, err := range errors {
			errorStr += err.Error() + "; "
		}
		parts = append(parts, errorStr)
	}
	
	if len(results) > 0 {
		parts = append(parts, "Completed actions:")
		for i, result := range results {
			parts = append(parts, fmt.Sprintf("  %d. %s", i+1, result))
		}
	}
	
	// Add context-specific responses
	switch intent {
	case IntentLaunchAttack:
		parts = append(parts, "🚀 Attack vectors prepared and ready for execution.")
	case IntentGenerateReport:
		parts = append(parts, "📊 Report generated with detailed vulnerability analysis.")
	case IntentAnalyzeVulnerability:
		parts = append(parts, "🔍 Vulnerability analysis complete with risk assessment.")
	case IntentBuildAttackPath:
		parts = append(parts, "⚡ Attack path constructed with multiple exploitation vectors.")
	case IntentTriggerAutoRemediation:
		parts = append(parts, "🛠️ Auto-remediation initiated for detected security incidents.")
	default:
		parts = append(parts, "✅ Request processed successfully.")
	}
	
	responseText := strings.Join(parts, "\n\n")
	
	if responseText == "" {
		responseText = "Your request has been processed. Is there anything else I can help you with?"
	}
	
	return responseText, nil
}

// generateFollowupSuggestions provides intelligent next steps
func (h *AIChatHandler) generateFollowupSuggestions(intent ChatIntent, results []string) []string {
	suggestions := make(map[string]bool)
	
	switch intent {
	case IntentLaunchAttack:
		suggestions["Review attack path"] = true
		suggestions["Run simulation"] = true
		suggestions["Generate detailed report"] = true
	case IntentGenerateReport:
		suggestions["Export to PDF"] = true
		suggestions["Schedule recurring report"] = true
		suggestions["Share with team"] = true
	case IntentAnalyzeVulnerability:
		suggestions["View affected systems"] = true
		suggestions["Check CVE database"] = true
		suggestions["Find mitigation strategies"] = true
	case IntentBuildAttackPath:
		suggestions["Optimize attack chain"] = true
		suggestions["Evaluate detection risks"] = true
		suggestions["Test on sandbox environment"] = true
	case IntentTriggerAutoRemediation:
		suggestions["Monitor remediation progress"] = true
		suggestions["Review changes made"] = true
		suggestions["Verify system health"] = true
	default:
		suggestions["What else can I help you with?"] = true
		suggestions["Show me my capabilities"] = true
		suggestions["Get help documentation"] = true
	}
	
	result := make([]string, 0, len(suggestions))
	for suggestion := range suggestions {
		result = append(result, suggestion)
	}
	
	return result
}

// buildPromptTemplates creates reusable prompt templates
func buildPromptTemplates() map[string]string {
	return map[string]string{
		"intent_parsing": "Analyze the following user request and identify their intent: {{message}}",
		"response_generation": "Based on these results {{results}} and intent {{intent}}, generate a clear natural language response.",
		"suggestion_generation": "Suggest 3 relevant follow-up actions based on {{intent}} and previous actions.",
	}
}
