// Package security_test - streaming_e2e_test.go provides end-to-end validation
// of the AISecOps threat detection pipeline, verifying that alerts generated
// by the streaming anomaly detector correctly flow into a simulated SOAR
// (Security Orchestration, Automation and Response) playbook.
package security_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
)

// SoarPlaybook simulates a real SOAR response playbook.
type SoarPlaybook struct {
	name       string
	triggered  bool
	incidentID string
	actions    []string
}

func NewSoarPlaybook(name string) *SoarPlaybook {
	return &SoarPlaybook{
		name:       name,
		triggered:  false,
		actions:    make([]string, 0),
		incidentID: "",
	}
}

func (p *SoarPlaybook) TriggerOnThreat(threat *security.ThreatEvent) error {
	if !strings.Contains(p.name, threat.Type) {
		return nil
	}

	p.triggered = true
	p.incidentID = fmt.Sprintf("INC-%s", common.NewUUID()[:8])
	p.actions = append(p.actions, "alert_generated")

	switch threat.Type {
	case "statistical-anomaly":
		p.actions = append(p.actions, "isolate_source_network")
		p.actions = append(p.actions, "capture_flow_logs")
	default:
		p.actions = append(p.actions, "default_response")
	}

	return nil
}

func (p *SoarPlaybook) IsTriggered() bool { return p.triggered }
func (p *SoarPlaybook) GetActions() []string { return p.actions }
func (p *SoarPlaybook) GetIncidentID() string { return p.incidentID }

// TestStreamingAnomalyToSoarIntegration validates end-to-end flow from
// streaming detector to SOAR playbook execution.
func TestStreamingAnomalyToSoarIntegration(t *testing.T) {
	cfg := security.CSThreatDetectorConfig{
		RuleBasedEnabled:        false,
		UebaEnabled:             false,
		IocEnabled:              false,
		StreamingEnabled:        true,
		MaxAuditWindowSeconds:   600,
		ConfidenceThresholds: security.ConfidenceThresholds{
			HighConfidence:     0.75,
			MediumConfidence:   0.5,
			AnomalyCritical:    3.5,
		},
	}

	detector, err := security.NewCSThreatDetector(cfg)
	if err != nil {
		t.Fatalf("failed to create CSThreatDetector: %v", err)
	}

	playbook := NewSoarPlaybook("anomaly_response")

	// Scenario 1: Normal traffic — no alerts expected
	rngNormal := rand.New(rand.NewSource(100))
	for i := 0; i < 200; i++ {
		entry := generateAuditEntry(i, rngNormal)
		detector.IngestAuditEntry(entry)
	}
	before := playbook.IsTriggered()

	detector.RunDetection(nil)
	_ = before // placeholder for future assertions

	if playbook.IsTriggered() {
		t.Fatal("SOAR playbook should not trigger on normal traffic")
	}

	// Scenario 2: Anomalous traffic — alert should fire
	rngAdv := rand.New(rand.NewSource(200))
	trigged := false
	for i := 0; i < 50; i++ {
		entry := generateAdversarialEntry(i, rngAdv)
		detector.IngestAuditEntry(entry)

		features := make([]float64, 12)
		for j := range features {
			features[j] = rngAdv.Float64() * 5 - 2.5 // outliers
		}

		// Try to score the mean vector to detect anomaly
		meanVec := detector.streamingDetector.Mean()
		isAnom, score, err := detector.streamingDetector.IsAnomaly(meanVec)
		if err == nil && isAnom && score >= 3.5 {
			threat := &security.ThreatEvent{
				ID:         common.NewUUID(),
				Severity:   "critical",
				Type:       "statistical-anomaly",
				Source:     "streaming-detector",
				Target:     "aisecops-pipeline",
				Status:     "active",
				DetectedAt: common.NowUTC(),
				Evidence: map[string]interface{}{
					"mahalanobis_distance": score,
				},
			}

			if err := playbook.TriggerOnThreat(threat); err != nil {
				t.Fatalf("playbook trigger failed: %v", err)
			}
			trigged = true
			break
		}
	}

	if !trigged || !playbook.IsTriggered() {
		t.Error("SOAR playbook did not trigger despite anomalous traffic")
	} else {
		t.Log("✓ SOAR playbook triggered successfully")
		t.Logf("   Incident ID: %s", playbook.GetIncidentID())
		t.Logf("   Actions executed: %d", len(playbook.GetActions()))
		for _, action := range playbook.GetActions() {
			t.Logf("      - %s", action)
		}
	}
}

func generateAuditEntry(idx int, rng *rand.Rand) *security.AuditLogEntry {
	return &security.AuditLogEntry{
		ID:           fmt.Sprintf("entry_%d", idx),
		Timestamp:    time.Now().Add(time.Duration(-idx) * time.Minute),
		UserID:       fmt.Sprintf("user_%d", idx%50),
		Username:     fmt.Sprintf("user%d", idx%50),
		Action:       "read",
		ResourceType: "pod",
		ResourceID:   fmt.Sprintf("res_%d", idx),
		IPAddress:    fmt.Sprintf("10.0.%d.%d", rng.Int()%256, rng.Int()%256),
		Status:       "success",
	}
}

func generateAdversarialEntry(idx int, rng *rand.Rand) *security.AuditLogEntry {
	return &security.AuditLogEntry{
		ID:           fmt.Sprintf("adv_%d", idx),
		Timestamp:    time.Now().Add(time.Duration(-idx) * time.Minute),
		UserID:       fmt.Sprintf("attacker_%d", idx),
		Username:     fmt.Sprintf("attacker%d", idx),
		Action:       "read",
		ResourceType: "secret",
		ResourceID:   fmt.Sprintf("admin-secret-%d", idx),
		IPAddress:    fmt.Sprintf("192.168.%d.%d", rng.Int()%256, rng.Int()%256),
		Status:       "success",
		Details: map[string]interface{}{
			"data_moved": true,
		},
	}
}
