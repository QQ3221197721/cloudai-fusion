package redteam

import (
	"fmt"
	"math"
	"sort"
)

// SimpleMLClassifier is a placeholder for actual ML model
type SimpleMLClassifier struct {
	featureWeights map[string]float64
	trained        bool
	logger         interface{} // *logrus.Logger
}

func NewSimpleMLClassifier(logger interface{}) *SimpleMLClassifier {
	return &SimpleMLClassifier{
		featureWeights: make(map[string]float64),
		trained:        false,
	}
}

func (smc *SimpleMLClassifier) Predict(features map[string]interface{}) Prediction {
	// Simple weighted voting classifier (placeholder)
	score := 0.0
	
	for feature, weight := range smc.featureWeights {
		if val, ok := features[feature]; ok {
			if strVal, ok := val.(string); ok {
				if strVal != "" {
					score += weight * 0.5
				}
			}
		}
	}
	
	// Normalize score to 0-1
	if score > 1.0 {
		score = 1.0
	}
	
	return Prediction{
		Label:      IncidentTypeUnknown,
		Confidence: math.Min(score, 0.95),
	}
}

func (smc *SimpleMLClassifier) Train(trainingData [][]FeatureLabel) error {
	// Placeholder training - in production, use actual ML library
	smc.trained = true
	return nil
}

func (smc *SimpleMLClassifier) GetFeatureImportance() map[string]float64 {
	return smc.featureWeights
}

// DetectionRuleset contains all detection rules
type DetectionRuleset struct {
	Rules []DetectionRule
}

// NewDetectionRuleset creates default detection rules
func NewDetectionRuleset() *DetectionRuleset {
	ruleset := &DetectionRuleset{
		Rules: make([]DetectionRule, 0),
	}
	
	ruleset.loadDefaultRules()
	
	return ruleset
}

func (drs *DetectionRuleset) loadDefaultRules() {
	drs.Rules = []DetectionRule{
		{
			ID:          "ransomware_pattern",
			Name:        "Ransomware Activity Detected",
			Description: "Detects ransomware-like behavior patterns",
			Conditions: []Condition{
				{Field: "event_type", Operator: "contains", Value: "encryption"},
				{Field: "event_count_1h", Operator: "gt", Value: "50"},
			},
			IncidentType:        IncidentTypeRansomeware,
			SeverityOffset:      2.0,
			ConfidenceThreshold: 0.8,
			Enabled:             true,
			AutoRemediate:       false, // Manual approval required for ransomware
		},
		{
			ID:          "data_exfil_lateral",
			Name:        "Large Data Transfer to External IP",
			Description: "Detects potential data exfiltration",
			Conditions: []Condition{
				{Field: "is_internal_ip", Operator: "eq", Value: "false"},
				{Field: "event_type", Operator: "contains", Value: "transfer"},
			},
			IncidentType:        IncidentTypeDataExfiltration,
			SeverityOffset:      1.5,
			ConfidenceThreshold: 0.7,
			Enabled:             true,
			AutoRemediate:       true,
		},
		{
			ID:          "privilege_escalation_attempt",
			Name:        "Privilege Escalation Attempt",
			Description: "Detects unauthorized privilege escalation attempts",
			Conditions: []Condition{
				{Field: "event_type", Operator: "contains", Value: "sudo"},
				{Field: "user_agent", Operator: "contains", Value: "root"},
			},
			IncidentType:        IncidentTypePrivilegeEscalation,
			SeverityOffset:      1.8,
			ConfidenceThreshold: 0.75,
			Enabled:             true,
			AutoRemediate:       false,
		},
		{
			ID:          "lateral_movement_smb",
			Name:        "SMB Lateral Movement",
			Description: "Detects SMB-based lateral movement",
			Conditions: []Condition{
				{Field: "event_type", Operator: "contains", Value: "smb"},
				{Field: "is_internal_ip", Operator: "eq", Value: "true"},
			},
			IncidentType:        IncidentTypeLateralMovement,
			SeverityOffset:      1.2,
			ConfidenceThreshold: 0.65,
			Enabled:             true,
			AutoRemediate:       true,
		},
		{
			ID:          "phishing_email_indicators",
			Name:        "Phishing Email Indicators",
			Description: "Detects phishing email characteristics",
			Conditions: []Condition{
				{Field: "user_agent", Operator: "contains", Value: "phishing"},
				{Field: "is_known_malicious", Operator: "eq", Value: "true"},
			},
			IncidentType:        IncidentTypePhishing,
			SeverityOffset:      1.0,
			ConfidenceThreshold: 0.7,
			Enabled:             true,
			AutoRemediate:       true,
		},
		{
			ID:          "malware_hash_match",
			Name:        "Known Malware Hash Match",
			Description: "Matches against known malware hash database",
			Conditions: []Condition{
				{Field: "metadata.hash", Operator: "in_list", Value: "malicious_hashes"},
			},
			IncidentType:        IncidentTypeMalware,
			SeverityOffset:      1.8,
			ConfidenceThreshold: 0.9,
			Enabled:             true,
			AutoRemediate:       true,
		},
	}
}

// AddRule appends a new detection rule
func (drs *DetectionRuleset) AddRule(rule DetectionRule) {
	drs.Rules = append(drs.Rules, rule)
}

// RemoveRule deletes a rule by ID
func (drs *DetectionRuleset) RemoveRule(ruleID string) {
	for i, rule := range drs.Rules {
		if rule.ID == ruleID {
			drs.Rules = append(drs.Rules[:i], drs.Rules[i+1:]...)
			break
		}
	}
}

// BuildEvidence constructs evidence description from matched rules
func buildEvidence(rule DetectionRule, matchCount, totalConditions int) string {
	return fmt.Sprintf("Matched %d of %d conditions (%.1f%%)",
		matchCount, totalConditions, float64(matchCount)/float64(totalConditions)*100)
}

// Helper functions
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func parseFloat(s string) (float64, error) {
	var result float64
	fmt.Sscanf(s, "%f", &result)
	return result, nil
}

func isInternalIP(ip string) bool {
	// Simplified internal IP check
	return len(ip) >= 7 && (ip[:3] == "10." || ip[:8] == "192.168." || ip[:7] == "172.16.")
	// In production, use proper CIDR matching
}

func sortByConfidence(matches []RuleMatch) {
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Confidence > matches[j].Confidence
	})
}
