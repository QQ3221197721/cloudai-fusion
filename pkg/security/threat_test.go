package security

import (
	"context"
	"testing"
	"time"
)

// makeAuditEntry is a test helper that builds an AuditLogEntry with the given
// fields and a fresh timestamp (so it always falls inside the detection window).
func makeAuditEntry(username, ip, action, status, resourceType, resourceID string, details map[string]interface{}) *AuditLogEntry {
	return &AuditLogEntry{
		ID:           "test-" + username + "-" + action,
		Timestamp:    time.Now(),
		Username:     username,
		IPAddress:    ip,
		Action:       action,
		Status:       status,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
	}
}

// TestThreatDetector_BruteForce verifies that ≥5 failed login attempts from the
// same IP within the detection window produce a brute-force threat event.
func TestThreatDetector_BruteForce(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{
		BruteForceThreshold: 5,
		BruteForceWindow:    5 * time.Minute,
	})

	// Inject 6 failed logins from the same IP.
	for i := 0; i < 6; i++ {
		td.IngestAuditEntry(makeAuditEntry(
			"user"+string(rune('a'+i)), "10.0.0.99",
			"login", "failure", "auth", "login-endpoint", nil,
		))
	}

	threats := td.RunDetection(context.Background())
	found := false
	for _, th := range threats {
		if th.Type == "brute-force" {
			found = true
			if th.Source != "10.0.0.99" {
				t.Fatalf("brute-force source = %q, want 10.0.0.99", th.Source)
			}
			if th.Severity != "high" {
				t.Fatalf("brute-force severity = %q, want high", th.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected brute-force threat, got none")
	}
}

// TestThreatDetector_PrivilegeEscalation verifies that a role_change to admin
// triggers a privilege-escalation threat.
func TestThreatDetector_PrivilegeEscalation(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{})

	td.IngestAuditEntry(makeAuditEntry(
		"attacker", "192.168.1.1",
		"update", "success", "user", "user-123",
		map[string]interface{}{"role_change": "viewer→admin"},
	))

	threats := td.RunDetection(context.Background())
	found := false
	for _, th := range threats {
		if th.Type == "privilege-escalation" {
			found = true
			if th.Severity != "critical" {
				t.Fatalf("privilege-escalation severity = %q, want critical", th.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected privilege-escalation threat, got none")
	}
}

// TestThreatDetector_AnomalousAPIAccess verifies that ≥100 API calls from one
// user within the rate window triggers an anomalous-access threat.
func TestThreatDetector_AnomalousAPIAccess(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{
		APIRateThreshold: 100,
		APIRateWindow:    1 * time.Minute,
	})

	for i := 0; i < 110; i++ {
		td.IngestAuditEntry(makeAuditEntry(
			"heavy-user", "10.1.2.3",
			"read", "success", "workload", "wl-"+string(rune(i)), nil,
		))
	}

	threats := td.RunDetection(context.Background())
	found := false
	for _, th := range threats {
		if th.Type == "anomalous-access" {
			found = true
			if th.Source != "heavy-user" {
				t.Fatalf("anomalous-access source = %q, want heavy-user", th.Source)
			}
		}
	}
	if !found {
		t.Fatal("expected anomalous-access threat, got none")
	}
}

// TestThreatDetector_DataExfiltration verifies that >50 distinct resource reads
// from one user triggers a data-exfiltration threat.
func TestThreatDetector_DataExfiltration(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{})

	for i := 0; i < 60; i++ {
		td.IngestAuditEntry(makeAuditEntry(
			"exfil-user", "10.5.5.5",
			"read", "success", "secret", "res-"+string(rune('A'+i/26))+string(rune('a'+i%26)), nil,
		))
	}

	threats := td.RunDetection(context.Background())
	found := false
	for _, th := range threats {
		if th.Type == "data-exfiltration" {
			found = true
			if th.Severity != "critical" {
				t.Fatalf("data-exfiltration severity = %q, want critical", th.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected data-exfiltration threat, got none")
	}
}

// TestThreatDetector_ResolveThreat verifies that resolving a threat changes its
// status and sets ResolvedAt.
func TestThreatDetector_ResolveThreat(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{
		BruteForceThreshold: 2,
		BruteForceWindow:    5 * time.Minute,
	})

	for i := 0; i < 3; i++ {
		td.IngestAuditEntry(makeAuditEntry(
			"u"+string(rune('a'+i)), "10.0.0.1",
			"login", "failure", "auth", "ep", nil,
		))
	}
	td.RunDetection(context.Background())

	threats := td.GetThreats()
	if len(threats) == 0 {
		t.Fatal("expected at least one threat")
	}
	id := threats[0].ID

	if err := td.ResolveThreat(id, "resolved"); err != nil {
		t.Fatalf("ResolveThreat: %v", err)
	}

	for _, th := range td.GetThreats() {
		if th.ID == id {
			if th.Status != "resolved" {
				t.Fatalf("status = %q, want resolved", th.Status)
			}
			if th.ResolvedAt == nil {
				t.Fatal("ResolvedAt should be set after resolve")
			}
			return
		}
	}
	t.Fatal("threat not found after resolve")
}

// TestThreatDetector_ResolveNotFound verifies that resolving a non-existent
// threat returns an error.
func TestThreatDetector_ResolveNotFound(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{})
	if err := td.ResolveThreat("nonexistent", "resolved"); err == nil {
		t.Fatal("expected error resolving nonexistent threat")
	}
}

// TestThreatDetector_Deduplication verifies that running detection twice on the
// same audit window does not produce duplicate threats.
func TestThreatDetector_Deduplication(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{
		BruteForceThreshold: 3,
		BruteForceWindow:    5 * time.Minute,
	})

	for i := 0; i < 5; i++ {
		td.IngestAuditEntry(makeAuditEntry(
			"u"+string(rune('a'+i)), "10.0.0.50",
			"login", "failure", "auth", "ep", nil,
		))
	}

	first := td.RunDetection(context.Background())
	second := td.RunDetection(context.Background())

	if len(first) == 0 {
		t.Fatal("first detection should produce threats")
	}
	if len(second) != 0 {
		t.Fatalf("second detection should produce no new threats (dedup), got %d", len(second))
	}
}

// TestThreatDetector_NoThreatsBelowThreshold verifies that below-threshold
// activity does not trigger a false positive.
func TestThreatDetector_NoThreatsBelowThreshold(t *testing.T) {
	td := NewThreatDetector(ThreatDetectionConfig{
		BruteForceThreshold: 10,
		BruteForceWindow:    5 * time.Minute,
	})

	// Only 3 failed logins — below threshold of 10.
	for i := 0; i < 3; i++ {
		td.IngestAuditEntry(makeAuditEntry(
			"benign", "10.0.0.1",
			"login", "failure", "auth", "ep", nil,
		))
	}

	threats := td.RunDetection(context.Background())
	for _, th := range threats {
		if th.Type == "brute-force" {
			t.Fatal("brute-force should not trigger below threshold")
		}
	}
}
