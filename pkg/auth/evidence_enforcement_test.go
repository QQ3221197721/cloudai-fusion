package auth

import (
	"testing"
	"time"
)

func newTestController(t testing.TB) *EvidenceAccessController {
	c, err := NewEvidenceAccessController(EvidenceAccessConfig{})
	if err != nil {
		t.Fatalf("NewEvidenceAccessController: %v", err)
	}
	return c
}

func adminUser() User  { return User{ID: "u-admin", Role: RoleAdmin} }
func viewerUser() User { return User{ID: "u-view", Role: RoleViewer} }

func TestEvidenceAccess_AllowReceiptVerifies(t *testing.T) {
	c := newTestController(t)
	allowed, receipt, err := c.CheckPermission(
		adminUser(),
		Resource{Path: "/clusters/1"},
		Action{Name: "create-cluster", Permission: PermClusterCreate},
	)
	if err != nil {
		t.Fatalf("CheckPermission: %v", err)
	}
	if !allowed {
		t.Error("admin must be allowed cluster:create")
	}
	if receipt == nil || !receipt.Verify() {
		t.Fatal("decision receipt must verify")
	}
	if receipt.Module != "auth.access" || receipt.Operation != "CheckPermission" {
		t.Errorf("unexpected module/op: %s/%s", receipt.Module, receipt.Operation)
	}
	if receipt.Metadata["allowed"] != "true" {
		t.Errorf("metadata allowed mismatch: %q", receipt.Metadata["allowed"])
	}
}

func TestEvidenceAccess_DenyReceiptVerifies(t *testing.T) {
	c := newTestController(t)
	allowed, receipt, err := c.CheckPermission(
		viewerUser(),
		Resource{Path: "/clusters/1"},
		Action{Name: "delete-cluster", Permission: PermClusterDelete},
	)
	if err != nil {
		t.Fatalf("CheckPermission: %v", err)
	}
	if allowed {
		t.Error("viewer must NOT be allowed cluster:delete")
	}
	if !receipt.Verify() {
		t.Fatal("deny receipt must still verify")
	}
	if receipt.Metadata["allowed"] != "false" {
		t.Errorf("metadata allowed mismatch: %q", receipt.Metadata["allowed"])
	}
}

func TestEvidenceAccess_ReceiptBindsSubjectObjectVerb(t *testing.T) {
	c := newTestController(t)
	_, r1, _ := c.CheckPermission(adminUser(), Resource{Path: "/a"}, Action{Name: "read", Permission: PermClusterRead})
	_, r2, _ := c.CheckPermission(adminUser(), Resource{Path: "/b"}, Action{Name: "read", Permission: PermClusterRead})
	if r1.InputHash == r2.InputHash {
		t.Error("different resources must produce different input hashes")
	}
}

func TestEvidenceAccess_RiskScore(t *testing.T) {
	c := newTestController(t)
	// Admin holds many dangerous perms and (initially) uses none => high risk.
	score, receipt, err := c.GetRiskScore(adminUser())
	if err != nil {
		t.Fatalf("GetRiskScore: %v", err)
	}
	if !receipt.Verify() {
		t.Fatal("risk-score receipt must verify")
	}
	if score.DangerousGranted == 0 {
		t.Fatal("admin should have dangerous permissions granted")
	}
	if score.Score <= 0.9 {
		t.Errorf("unused-dangerous admin should score near 1.0, got %.2f", score.Score)
	}

	// After exercising a dangerous permission, the risk score must drop.
	_, _, _ = c.CheckPermission(adminUser(), Resource{Path: "/c"}, Action{Name: "del", Permission: PermClusterDelete})
	score2, _, _ := c.GetRiskScore(adminUser())
	if !(score2.Score < score.Score) {
		t.Errorf("risk score should drop after use: before=%.3f after=%.3f", score.Score, score2.Score)
	}
	if score2.DangerousUsed < 1 {
		t.Error("expected at least one dangerous permission recorded as used")
	}
}

func TestEvidenceAccess_MinPolicyRecommendsRevoke(t *testing.T) {
	c := newTestController(t)
	// Never use any admin permission => learner recommends revoking unused
	// dangerous grants for the admin role.
	recs := c.Recommendations()
	foundRevoke := false
	for _, r := range recs {
		if r.Kind == "revoke_unused" && r.Role == RoleAdmin {
			foundRevoke = true
			break
		}
	}
	if !foundRevoke {
		t.Error("expected a revoke_unused recommendation for unused dangerous admin perms")
	}
}

func TestEvidenceAccess_MinPolicyRecommendsGrant(t *testing.T) {
	c, err := NewEvidenceAccessController(EvidenceAccessConfig{AlertThreshold: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Repeatedly deny the same (role, perm) to build support for grant_needed.
	for i := 0; i < 6; i++ {
		_, _, _ = c.CheckPermission(
			viewerUser(),
			Resource{Path: "/workloads/x"},
			Action{Name: "create-wl", Permission: PermWorkloadCreate},
		)
	}
	recs := c.Recommendations()
	foundGrant := false
	for _, r := range recs {
		if r.Kind == "grant_needed" && r.Perm == PermWorkloadCreate && r.Support >= 2 {
			foundGrant = true
			break
		}
	}
	if !foundGrant {
		t.Error("expected grant_needed recommendation after repeated denials")
	}
}

func TestMinPolicyLearner_UsageTracking(t *testing.T) {
	l := newMinPolicyLearner(time.Hour, 10)
	if l.used(RoleAdmin, PermClusterCreate) {
		t.Error("should not be used before recording")
	}
	l.recordUse(RoleAdmin, PermClusterCreate)
	if !l.used(RoleAdmin, PermClusterCreate) {
		t.Error("should be used after recording")
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkEvidenceAccess_CheckPermission(b *testing.B) {
	c, err := NewEvidenceAccessController(EvidenceAccessConfig{})
	if err != nil {
		b.Fatal(err)
	}
	user := adminUser()
	res := Resource{Path: "/clusters/1"}
	act := Action{Name: "read", Permission: PermClusterRead}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		allowed, receipt, err := c.CheckPermission(user, res, act)
		if err != nil {
			b.Fatal(err)
		}
		if !allowed || !receipt.Verify() {
			b.Fatal("unexpected decision/receipt")
		}
	}
	// Target: permission check + signed receipt in the tens of microseconds.
}

func BenchmarkEvidenceAccess_RiskScore(b *testing.B) {
	c, _ := NewEvidenceAccessController(EvidenceAccessConfig{})
	user := adminUser()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = c.GetRiskScore(user)
	}
}
