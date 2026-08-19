
package redteam

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// IncidentClassifier automatically detects and classifies security incidents
type IncidentClassifier struct {
	logger        *logrus.Logger
	ruleSet       *DetectionRuleset
	classifierModel MLModelInterface
}

// NewIncidentClassifier creates a classifier with default rules
func NewIncidentClassifier(logger *logrus.Logger) *IncidentClassifier {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	return &IncidentClassifier{
		logger:      logger,
		ruleSet:     NewDetectionRuleset(),
		classifierModel: NewSimpleMLClassifier(logger), // Placeholder for actual ML model
	}
}

// ClassifyEvent analyzes a security event and determines its type and severity
func (ic *IncidentClassifier) ClassifyEvent(ctx context.Context, event SecurityEvent) (*ClassifiedIncident, error) {
	startTime := time.Now()
	
	ic.logger.WithFields(logrus.Fields{
		"event_id":   event.ID,
		"source":     event.Source,
		"timestamp":  event.Timestamp,
	}).Info("Starting incident classification")
	
	// Extract features from the event
	features := ic.extractFeatures(event)
	
	// Apply rule-based detection first
	ruleResults := ic.applyDetectionRules(features)
	
	// If rules are inconclusive, use ML classification
	var predictedType IncidentType
	var confidence float64
	
	if len(ruleResults) > 0 {
		// Rule-based classification wins
		predictedType = ruleResults[0].IncidentType
		confidence = ruleResults[0].Confidence
		
		// Enhance confidence if multiple rules match
		if len(ruleResults) > 1 {
			confidence = min(confidence+0.1, 1.0)
		}
	} else {
		// Fall back to ML model
		prediction := ic.classifierModel.Predict(features)
		predictedType = prediction.Label
		confidence = prediction.Confidence
	}
	
	// Calculate severity based on type and impact
	severity := ic.calculateSeverity(predictedType, features)
	
	// Determine agent type needed for response
	agentType := ic.selectAgentForIncident(predictedType)
	
	duration := time.Since(startTime)
	
	ic.logger.WithFields(logrus.Fields{
		"classification_time_ms": duration.Milliseconds(),
		"incident_type":          predictedType,
		"confidence":             confidence,
		"severity":               severity,
		"recommended_agent":      agentType,
	}).Info("Incident classified successfully")
	
	return &ClassifiedIncident{
		EventID:       event.ID,
		IncidentType:  predictedType,
		Severity:      severity,
		Confidence:    confidence,
		RecommendedAgent: agentType,
		MatchingRules: ruleResults,
		AnalysisDuration: duration,
		Timestamp:     time.Now().UTC(),
		RawEvent:      event,
	}, nil
}

// extractFeatures pulls relevant data from the security event
func (ic *IncidentClassifier) extractFeatures(event SecurityEvent) map[string]interface{} {
	features := make(map[string]interface{})
	
	// Basic event characteristics
	features["event_type"] = event.Type
	features["source_ip"] = event.SourceIP
	features["target_host"] = event.TargetHost
	features["user_agent"] = event.UserAgent
	
	// Behavioral patterns
	if event.Metadata != nil {
		if metadata, ok := event.Metadata.(map[string]interface{}); ok {
			for key, value := range metadata {
				features[key] = value
			}
		}
	}
	
	// Frequency analysis
	features["event_count_1h"] = event.Frequency.LastHour
	features["event_count_24h"] = event.Frequency.Last24Hours
	
	// Context enrichment
	features["is_internal_ip"] = isInternalIP(event.SourceIP)
	features["is_known_malicious"] = event.KnownThreats.HasIndicator(event.SourceIP)
	
	return features
}

// applyDetectionRules applies predefined detection rules
func (ic *IncidentClassifier) applyDetectionRules(features map[string]interface{}) []RuleMatch {
	matches := make([]RuleMatch, 0)
	
	for _, rule := range ic.ruleSet.Rules {
		result := ic.evaluateRule(rule, features)
		if result.Matched {
			matches = append(matches, result)
		}
	}
	
	// Sort by confidence descending
	sortByConfidence(matches)
	
	return matches
}

// evaluateRule checks if a single rule matches the given features
func (ic *IncidentClassifier) evaluateRule(rule DetectionRule, features map[string]interface{}) RuleMatch {
	matchCount := 0
	totalConditions := len(rule.Conditions)
	
	for _, condition := range rule.Conditions {
		value := features[condition.Field]
		
		var matched bool
		switch condition.Operator {
		case ConditionOperatorEqual:
			matched = fmt.Sprintf("%v", value) == condition.Value
		case ConditionOperatorGreaterThan:
			if numVal, ok := value.(float64); ok {
				if ruleVal, err := parseFloat(condition.Value); err == nil {
					matched = numVal > ruleVal
				}
			}
		case ConditionOperatorContains:
			if strVal, ok := value.(string); ok {
				matched = strings.Contains(strVal, condition.Value)
			}
		case ConditionOperatorInList:
			if sliceVal, ok := value.([]string); ok {
				for _, item := range sliceVal {
					if item == condition.Value {
						matched = true
						break
					}
				}
			}
		}
		
		if matched {
			matchCount++
		}
	}
	
	threshold := float64(len(rule.Conditions)) * rule.ConfidenceThreshold
	actualMatchRate := float64(matchCount) / float64(totalConditions)
	
	return RuleMatch{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Matched:     actualMatchRate >= threshold,
		Confidence:  actualMatchRate,
		IncidentType: rule.IncidentType,
		Evidence:    buildEvidence(rule, matchCount, totalConditions),
	}
}

// calculateSeverity determines the overall severity level
func (ic *IncidentClassifier) calculateSeverity(incidentType IncidentType, features map[string]interface{}) SeverityLevel {
	baseScore := getBaseSeverityScore(incidentType)
	
	// Adjust based on features
	riskFactors := []struct{
		name string
		score float64
		condition func(map[string]interface{}) bool
	}{
		{"External Source", 0.5, func(f map[string]interface{}) bool {
			isInternal, _ := f["is_internal_ip"].(bool)
			return !isInternal
		}},
		{"Known Malicious", 1.0, func(f map[string]interface{}) bool {
			known, _ := f["is_known_malicious"].(bool)
			return known
		}},
		{"High Frequency", 0.3, func(f map[string]interface{}) bool {
			count, ok := f["event_count_1h"].(float64)
			return ok && count > 100
		}},
		{"Privilege Escalation Attempt", 1.5, func(f map[string]interface{}) bool {
			return incidentType == IncidentTypePrivilegeEscalation
		}},
		{"Lateral Movement", 1.0, func(f map[string]interface{}) bool {
			return incidentType == IncidentTypeLateralMovement
		}},
	}
	
	adjustment := 0.0
	for _, factor := range riskFactors {
		if factor.condition(features) {
			adjustment += factor.score
		}
	}
	
	finalScore := baseScore + adjustment
	
	// Map score to severity level
	switch {
	case finalScore >= 2.5:
		return SeverityCritical
	case finalScore >= 1.5:
		return SeverityHigh
	case finalScore >= 0.8:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

// selectAgentForIncident determines which remediation agent should handle this incident
func (ic *IncidentClassifier) selectAgentForIncident(incidentType IncidentType) AgentType {
	agentMapping := map[IncidentType]AgentType{
		IncidentTypeRansomeware:              AgentTypeRansomewareResponse,
		IncidentTypeDataExfiltration:         AgentTypeDataExfiltration,
		IncidentTypePrivilegeEscalation:      AgentTypePrivilegeEscalation,
		IncidentTypeLateralMovement:          AgentTypeLateralMovement,
		IncidentTypePhishing:                 AgentTypePhishingResponse,
		IncidentTypeMalware:                  AgentTypeMalwareRemoval,
		IncidentTypeUnauthorizedAccess:       AgentTypeAccessControl,
		IncidentTypeDenialOfService:          AgentTypeDoSDetection,
		IncidentTypeInsiderThreat:            AgentTypeInsiderThreat,
		IncidentTypeAccountCompromise:        AgentTypeAccountSecurity,
	}
	
	if agentType, ok := agentMapping[incidentType]; ok {
		return agentType
	}
	
	return AgentTypeGeneric
}
