// Package aisecops_test provides integration tests for the AISecOps Wells Framework.
package aisecops_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/aisecops"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestWellsFramework_Initialization verifies framework creation succeeds with valid ledger.
func TestWellsFramework_Initialization(t *testing.T) {
	// Create in-memory evidence store
	store := evidence.NewMemoryStore()

	// Generate ephemeral signing key (for testing only)
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("Failed to generate signer: %v", err)
	}

	// Initialize ledger
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:  store,
		Signer: signer,
	})
	if err != nil {
		t.Fatalf("Failed to create ledger: %v", err)
	}

	// Create wells framework
	fw := aisecops.New(ledger)
	if fw == nil {
		t.Fatal("Expected non-nil WellsFramework instance")
	}

	// Verify all wells are registered
	report, err := fw.VerifyAll(context.Background())
	if err != nil {
		t.Fatalf("Verification failed: %v", err)
	}

	if report.TotalWells != 7 {
		t.Errorf("Expected 7 wells, got %d", report.TotalWells)
	}

	if !report.FrameworkValid {
		t.Error("Expected framework to be valid")
	}
}

// TestWellResult_Serialization verifies each well's result can be serialized/deserialized.
func TestWellResult_Serialization(t *testing.T) {
	results := []struct {
		id      string
		name    string
		valid   bool
		details map[string]interface{}
	}{
		{"L9", "Security Gateway", true, map[string]interface{}{"throughput": "8,470 req/s"}},
		{"L10", "Threat Hunting", true, map[string]interface{}{"latency_p99": "2.1ms"}},
		{"L14", "DevSecOps Gate", true, map[string]interface{}{"samples_verified": "SBOM integrity checked"}},
		{"L16", "WellRouter", true, map[string]interface{}{"policy_execution_proof": "Verified cryptographically"}},
		{"M30", "Sigma Detection", true, map[string]interface{}{"rule_engine_audit": "Logged to chain"}},
		{"M32", "SOAR Playbook", true, map[string]interface{}{"playbook_attestation": "Signed steps"}},
		{"M51", "Capability Security", true, map[string]interface{}{"access_control_logging": "Recorded in ledger"}},
	}

	for _, r := range results {
		wr := aisecops.WellResult{
			WellID:   r.id,
			WellName: r.name,
			Valid:    r.valid,
			Details:  r.details,
		}

		jsonBytes, err := json.Marshal(wr)
		if err != nil {
			t.Fatalf("Failed to marshal WellResult for %s: %v", r.id, err)
		}

		var wr2 aisecops.WellResult
		err = json.Unmarshal(jsonBytes, &wr2)
		if err != nil {
			t.Fatalf("Failed to unmarshal WellResult for %s: %v", r.id, err)
		}

		if wr2.WellID != wr.WellID {
			t.Errorf("Expected WellID %s, got %s", wr.WellID, wr2.WellID)
		}

		if wr2.Valid != wr.Valid {
			t.Errorf("Expected Valid %v, got %v", wr.Valid, wr2.Valid)
		}
	}
}
