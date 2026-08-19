package soc

import (
	"testing"
)

func TestEvidenceSocEngine_RecordDecision(t *testing.T) {
	engine := NewEvidenceSocEngine()
	
	result, err := engine.RecordDecision("TICKET-001", "escalate", "critical vulnerability detected")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if result.TicketID != "TICKET-001" {
		t.Errorf("expected ticket 'TICKET-001', got '%s'", result.TicketID)
	}
	
	if result.Decision != "escalate" {
		t.Errorf("expected decision 'escalate', got '%s'", result.Decision)
	}
	
	if result.Receipt == nil {
		t.Error("expected non-nil receipt")
	}
}

func TestEvidenceSocEngine_InvalidDecision(t *testing.T) {
	engine := NewEvidenceSocEngine()
	
	_, err := engine.RecordDecision("TICKET-002", "delete", "should fail")
	if err == nil {
		t.Error("expected error for invalid decision type")
	}
}

func TestEvidenceSocEngine_ThreatPriority(t *testing.T) {
	engine := NewEvidenceSocEngine()
	
	engine.RegisterThreat("HIGH", ThreatProfile{CVSS: 9.5, ExploitExists: true, AssetCriticality: 3})
	engine.RegisterThreat("LOW", ThreatProfile{CVSS: 2.0, ExploitExists: false, AssetCriticality: 0})
	
	high, _ := engine.RecordDecision("HIGH", "escalate", "critical")
	low, _ := engine.RecordDecision("LOW", "close", "low risk")
	
	if high.PriorityScore <= low.PriorityScore {
		t.Errorf("expected high-CVSS threat to have higher priority: high=%f low=%f", high.PriorityScore, low.PriorityScore)
	}
}
