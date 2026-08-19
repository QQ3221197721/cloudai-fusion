package tenant

import "testing"

// TestRecordTenantOp_ProducesVerifiableReceipt proves each tenant operation is
// sealed into a signed, offline-verifiable receipt with a correct isolation
// verdict against the configured quota.
func TestRecordTenantOp_ProducesVerifiableReceipt(t *testing.T) {
	engine := NewEvidenceIsolationEngine()
	engine.SetQuota("acme", 100)

	within, err := engine.RecordTenantOp("acme", 80)
	if err != nil {
		t.Fatalf("record op: %v", err)
	}
	if within.Receipt == nil || !within.Receipt.Verify() {
		t.Fatal("tenant op must carry a verifiable receipt")
	}
	if !within.Isolated {
		t.Fatalf("80 units under a 100 quota must be isolated, got %+v", within)
	}

	over, err := engine.RecordTenantOp("acme", 150)
	if err != nil {
		t.Fatalf("record op: %v", err)
	}
	if over.Isolated {
		t.Fatalf("150 units over a 100 quota must NOT be isolated, got %+v", over)
	}
}

// TestRecordTenantOp_RejectsBadInput verifies input validation.
func TestRecordTenantOp_RejectsBadInput(t *testing.T) {
	engine := NewEvidenceIsolationEngine()
	if _, err := engine.RecordTenantOp("", 1); err == nil {
		t.Fatal("expected error for empty tenant id")
	}
	if _, err := engine.RecordTenantOp("t", -1); err == nil {
		t.Fatal("expected error for negative units")
	}
}

// TestDetectNoisyNeighbors flags the tenant whose usage variance is a strong
// outlier relative to the fleet, while leaving steady tenants unflagged.
func TestDetectNoisyNeighbors(t *testing.T) {
	engine := NewEvidenceIsolationEngine()

	// Two steady tenants: near-constant usage → tiny variance.
	for _, u := range []float64{10, 10, 11, 9, 10} {
		if _, err := engine.RecordTenantOp("steady-a", u); err != nil {
			t.Fatalf("record: %v", err)
		}
		if _, err := engine.RecordTenantOp("steady-b", u); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	// One bursty tenant: wild swings → huge variance.
	for _, u := range []float64{1, 200, 3, 180, 2} {
		if _, err := engine.RecordTenantOp("bursty", u); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	flagged := engine.DetectNoisyNeighbors(3.0)
	if len(flagged) == 0 {
		t.Fatal("expected the bursty tenant to be flagged")
	}
	if flagged[0].TenantID != "bursty" {
		t.Fatalf("expected bursty tenant as top noisy neighbor, got %q", flagged[0].TenantID)
	}
	if flagged[0].OutlierScore <= 1 {
		t.Fatalf("outlier score must exceed 1, got %.2f", flagged[0].OutlierScore)
	}
	for _, f := range flagged {
		if f.TenantID == "steady-a" || f.TenantID == "steady-b" {
			t.Fatalf("steady tenant %q should not be flagged", f.TenantID)
		}
	}
}
