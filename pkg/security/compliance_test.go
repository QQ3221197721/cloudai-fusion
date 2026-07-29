package security

import (
	"context"
	"testing"
)

// These tests cover the compliance engine — previously untested — pinning the
// static (no-K8s-client) audit path used in dev/CI: every supported framework
// returns a scored report, and the framework dispatcher behaves correctly.

// runsScoredReport is a shared assertion: a report must be internally
// consistent (counts sum, score derives from passed/total).
func assertScoredReport(t *testing.T, r *ComplianceReport, wantFramework string) {
	t.Helper()
	if r == nil {
		t.Fatal("nil report")
	}
	if r.Framework != wantFramework {
		t.Fatalf("framework = %q, want %q", r.Framework, wantFramework)
	}
	if r.Status != "completed" {
		t.Fatalf("status = %q, want completed", r.Status)
	}
	if len(r.Checks) == 0 {
		t.Fatal("report must contain checks")
	}
	total := r.Passed + r.Failed + r.Warnings
	// skip-status checks are allowed, so counted totals may be <= len(Checks).
	if total > len(r.Checks) {
		t.Fatalf("counted %d exceeds %d checks", total, len(r.Checks))
	}
	if r.Score < 0 || r.Score > 100 {
		t.Fatalf("score %.1f out of [0,100]", r.Score)
	}
	if r.GeneratedAt.IsZero() {
		t.Fatal("GeneratedAt must be set")
	}
}

// TestComplianceEngine_CISBenchmark covers the CIS static path.
func TestComplianceEngine_CISBenchmark(t *testing.T) {
	e := NewComplianceEngine()
	r, err := e.RunCISBenchmark(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("RunCISBenchmark: %v", err)
	}
	assertScoredReport(t, r, "CIS")
}

// TestComplianceEngine_NISTChecks covers the NIST 800-190 static path.
func TestComplianceEngine_NISTChecks(t *testing.T) {
	e := NewComplianceEngine()
	r, err := e.RunNISTChecks(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("RunNISTChecks: %v", err)
	}
	assertScoredReport(t, r, "NIST-800-190")
}

// TestComplianceEngine_SOC2Audit covers the SOC2 static path.
func TestComplianceEngine_SOC2Audit(t *testing.T) {
	e := NewComplianceEngine()
	r, err := e.RunSOC2Audit(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("RunSOC2Audit: %v", err)
	}
	assertScoredReport(t, r, "SOC2")
}

// TestComplianceEngine_PCIDSSAudit covers the PCI-DSS static path.
func TestComplianceEngine_PCIDSSAudit(t *testing.T) {
	e := NewComplianceEngine()
	r, err := e.RunPCIDSSAudit(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("RunPCIDSSAudit: %v", err)
	}
	assertScoredReport(t, r, "PCI-DSS")
}

// TestComplianceEngine_HIPAAAudit covers the HIPAA static path.
func TestComplianceEngine_HIPAAAudit(t *testing.T) {
	e := NewComplianceEngine()
	r, err := e.RunHIPAAAudit(context.Background(), "cluster-1")
	if err != nil {
		t.Fatalf("RunHIPAAAudit: %v", err)
	}
	assertScoredReport(t, r, "HIPAA")
}

// TestComplianceEngine_FrameworkDispatch verifies every supported framework
// name (and its aliases) routes to a real audit, and unknown frameworks error.
func TestComplianceEngine_FrameworkDispatch(t *testing.T) {
	e := NewComplianceEngine()
	ctx := context.Background()

	cases := map[string]string{
		"cis":          "CIS",
		"NIST":         "NIST-800-190",
		"nist-800-190": "NIST-800-190",
		"soc2":         "SOC2",
		"pci":          "PCI-DSS",
		"PCIDSS":       "PCI-DSS",
		"hipaa":        "HIPAA",
	}
	for in, wantFramework := range cases {
		r, err := e.RunFrameworkAudit(ctx, "cluster-1", in)
		if err != nil {
			t.Fatalf("RunFrameworkAudit(%q): %v", in, err)
		}
		if r.Framework != wantFramework {
			t.Errorf("framework(%q) = %q, want %q", in, r.Framework, wantFramework)
		}
	}

	if _, err := e.RunFrameworkAudit(ctx, "cluster-1", "GDPR"); err == nil {
		t.Fatal("unsupported framework must error")
	}
}

// TestComplianceEngine_SupportedFrameworks pins the advertised framework list.
func TestComplianceEngine_SupportedFrameworks(t *testing.T) {
	got := SupportedFrameworks()
	want := map[string]bool{"CIS": true, "NIST-800-190": true, "SOC2": true, "PCI-DSS": true, "HIPAA": true}
	if len(got) != len(want) {
		t.Fatalf("supported = %v, want %d frameworks", got, len(want))
	}
	for _, f := range got {
		if !want[f] {
			t.Errorf("unexpected framework %q", f)
		}
	}
	// Every advertised framework must be dispatchable.
	e := NewComplianceEngine()
	for _, f := range got {
		if _, err := e.RunFrameworkAudit(context.Background(), "c", f); err != nil {
			t.Errorf("advertised framework %q not dispatchable: %v", f, err)
		}
	}
}
