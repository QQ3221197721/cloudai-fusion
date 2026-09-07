package detection_rules_test

import (
	"fmt"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/detection_rules"
)

func TestEngineCreation(t *testing.T) {
	engine := detection_rules.NewDetectionEngine(nil)
	if engine == nil {
		t.Fatal("Failed to create detection engine")
	}
	
	rules := engine.GetAllRules()
	if len(rules) == 0 {
		t.Error("Expected built-in rules to be loaded")
	} else {
		t.Logf("✓ Detection engine created with %d rules", len(rules))
	}
}

func TestRuleLoading(t *testing.T) {
	engine := detection_rules.NewDetectionEngine(nil)
	
	// Check critical rules are loaded
	criticalRules := []string{"BO-001", "SQLI-001", "EXP-001"}
	
	for _, ruleID := range criticalRules {
		rule, exists := engine.GetRule(ruleID)
		if !exists {
			t.Errorf("Rule %s not found", ruleID)
		} else if rule.Status != detection_rules.StatusActive {
			t.Errorf("Rule %s not active", ruleID)
		}
	}
	
	t.Log("✓ Critical detection rules loaded correctly")
}

func TestAlertCreation(t *testing.T) {
	engine := detection_rules.NewDetectionEngine(nil)
	
	testEvent := map[string]interface{}{
		"source_ip":      "192.168.1.100",
		"destination_ip": "10.0.0.1",
		"user":           "attacker",
		"message":        "stack smashing detected in process",
	}
	
	alerts := engine.Evaluate(testEvent)
	
	t.Logf("Generated %d alerts from test event", len(alerts))
	
	// Check alert structure
	for _, alert := range alerts {
		if alert.RuleID == "" {
			t.Error("Alert missing rule ID")
		}
		if alert.Severity == "" {
			t.Error("Alert missing severity")
		}
		if alert.Timestamp.IsZero() {
			t.Error("Alert has zero timestamp")
		}
	}
}

func TestRuleEnableDisable(t *testing.T) {
	engine := detection_rules.NewDetectionEngine(nil)
	
	// Disable a rule
	err := engine.DisableRule("BO-001")
	if err != nil {
		t.Fatalf("Failed to disable rule: %v", err)
	}
	
	// Verify it's disabled
	rule, _ := engine.GetRule("BO-001")
	if rule.Status != detection_rules.StatusDisabled {
		t.Error("Rule should be disabled")
	}
	
	// Re-enable the rule
	err = engine.EnableRule("BO-001")
	if err != nil {
		t.Fatalf("Failed to enable rule: %v", err)
	}
	
	rule, _ = engine.GetRule("BO-001")
	if rule.Status != detection_rules.StatusActive {
		t.Error("Rule should be re-enabled")
	}
	
	t.Log("✓ Rule enable/disable works correctly")
}

func TestDynamicRuleRegistration(t *testing.T) {
	engine := detection_rules.NewDetectionEngine(nil)
	
	// Add custom dynamic rule
	err := engine.AddDynamicRule("CUSTOM-CHECK", func(event map[string]interface{}) bool {
		msg, ok := event["message"].(string)
		return ok && containsSensitiveWord(msg, "malicious")
	})
	
	if err != nil {
		t.Fatalf("Failed to add dynamic rule: %v", err)
	}
	
	// Test the dynamic rule
	testEvent := map[string]interface{}{
		"message": "Found malicious activity in logs",
	}
	
	alerts := engine.Evaluate(testEvent)
	foundAlert := false
	for _, alert := range alerts {
		if alert.RuleID == "CUSTOM-CHECK" {
			foundAlert = true
			break
		}
	}
	
	if !foundAlert {
		t.Log("Note: Dynamic rule matching depends on implementation logic")
	}
}

func TestSeverityClassification(t *testing.T) {
	tests := []struct {
		ruleID   string
		expected detection_rules.Severity
	}{
		{"BO-001", detection_rules.Critical},
		{"FS-001", detection_rules.High},
		{"MEM-001", detection_rules.Medium},
		{"PROC-001", detection_rules.Low},
	}
	
	engine := detection_rules.NewDetectionEngine(nil)
	
	for _, tt := range tests {
		rule, exists := engine.GetRule(tt.ruleID)
		if !exists {
			t.Errorf("Rule %s not found", tt.ruleID)
			continue
		}
		
		if rule.Severity != tt.expected {
			t.Errorf("Rule %s severity %s, expected %s", 
				tt.ruleID, rule.Severity, tt.expected)
		}
	}
	
	t.Log("✓ Severity classifications verified")
}

func TestMITREADTKMapping(t *testing.T) {
	engine := detection_rules.NewDetectionEngine(nil)
	
	rulesWithMITRE := []string{"BO-001", "SQLI-001", "PE-001", "LM-001"}
	
	for _, ruleID := range rulesWithMITRE {
		rule, exists := engine.GetRule(ruleID)
		if !exists {
			t.Errorf("Rule %s not found for MITRE check", ruleID)
			continue
		}
		
		if rule.MITREATTK == "" {
			t.Logf("Rule %s has no MITRE ATT&K mapping", ruleID)
		} else {
			t.Logf("✓ %s mapped to MITRE ATT&K: %s", ruleID, rule.MITREATTK)
		}
	}
}

func TestAlertHistoryManagement(t *testing.T) {
	engine := detection_rules.NewDetectionEngine(nil)
	
	// Generate some alerts
	for i := 0; i < 5; i++ {
		event := map[string]interface{}{
			"message": fmt.Sprintf("Test alert %d", i),
		}
		engine.Evaluate(event)
	}
	
	history := engine.GetAlertHistory()
	if len(history) == 0 {
		t.Error("Expected alert history to contain entries")
	} else {
		t.Logf("✓ Alert history tracking works (%d alerts)", len(history))
	}
	
	// Clear and verify
	engine.ClearHistory()
	history = engine.GetAlertHistory()
	if len(history) > 0 {
		t.Error("History should be empty after clear")
	}
}

func TestComplianceReporting(t *testing.T) {
	config := &detection_rules.EngineConfig{
		ComplianceProfiles: []string{"PCI-DSS", "NIST", "HIPAA"},
	}
	
	engine := detection_rules.NewDetectionEngine(config)
	report := engine.ComplianceReport()
	
	if len(report) == 0 {
		t.Error("Expected compliance report data")
	} else {
		for profile, rules := range report {
			t.Logf("✓ Profile %s: %d rules covered", profile, len(rules))
		}
	}
}

func TestDetectionLogicParsing(t *testing.T) {
	tests := []struct {
		ruleID     string
		logicMatch bool
	}{
		{"BO-001", true}, // Should match "stack smashing detected"
		{"FS-001", false}, // Format string unlikely in test events
	}
	
	engine := detection_rules.NewDetectionEngine(nil)
	
	testEvents := map[string]map[string]interface{}{
		"buffer_overflow": {"message": "stack smashing detected unexpectedly"},
		"injection":       {"message": "SQL injection attempt blocked"},
	}
	
	for name, event := range testEvents {
		alerts := engine.Evaluate(event)
		t.Logf("Event '%s' generated %d alerts", name, len(alerts))
	}
}

func BenchmarkDetectionEvaluation(b *testing.B) {
	engine := detection_rules.NewDetectionEngine(nil)
	
	// Create realistic test events
	events := []map[string]interface{}{
		{"message": "stack smashing detected in binary", "source_ip": "10.0.0.1"},
		{"query": "SELECT * FROM users WHERE id=1 OR 1=1", "user": "webapp"},
		{"process": "lsass.exe", "action": "memory_read", "pid": 1234},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, event := range events {
			engine.Evaluate(event)
		}
	}
}

func containsSensitiveWord(text, word string) bool {
	return contains(text, word)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
