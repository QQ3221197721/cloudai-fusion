package gitops

import "testing"

func TestEvidenceGitopsEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceGitopsEngine()
	res, err := e.Reconcile("sha-before", "sha-after", true, "payment-service")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "gitops" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceGitopsEngine_DriftSeverityScoring(t *testing.T) {
	e := NewEvidenceGitopsEngine()
	// Set up dependency graph: payment depends on db, so db drift impacts payment
	e.AddDownstreamDependent("db", "payment")
	e.AddDownstreamDependent("db", "order")
	e.SetCriticality("db", 0.9) // high criticality

	// Simulate drift in db
	res, _ := e.Reconcile("a", "b", true, "db")
	severity := res.Severity
	if severity == nil {
		t.Fatal("expected severity analysis")
	}
	if severity.AffectedCount < 2 {
		t.Errorf("expected impact >= 2, got %d (%v)", severity.AffectedCount, severity.ImpactServices)
	}
	if severity.SeverityScore <= 0 {
		t.Error("severity score must be positive")
	}
}

func TestEvidenceGitopsEngine_NoDriftLowSeverity(t *testing.T) {
	e := NewEvidenceGitopsEngine()
	res, _ := e.Reconcile("a", "b", false, "")
	if res.DriftDetected {
		t.Error("no drift should be detected")
	}
}
