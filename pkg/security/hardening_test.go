package security

import (
	"context"
	"testing"
)

func newTestHardeningManager() *HardeningManager {
	return NewHardeningManager(HardeningConfig{
		PSS:    DefaultPSSConfig(),
		Cosign: DefaultCosignConfig(),
	})
}

// ============================================================================
// PSS Config Tests
// ============================================================================

func TestDefaultPSSConfig(t *testing.T) {
	cfg := DefaultPSSConfig()
	if cfg.Profile != PSSRestricted {
		t.Errorf("expected restricted, got %s", cfg.Profile)
	}
	if !cfg.Enforce || !cfg.Audit || !cfg.Warn {
		t.Error("expected all enforcement modes enabled")
	}
	if len(cfg.ExemptNamespaces) != 3 {
		t.Errorf("expected 3 exempt namespaces, got %d", len(cfg.ExemptNamespaces))
	}
}

func TestGenerateNamespaceLabels(t *testing.T) {
	cfg := DefaultPSSConfig()
	labels := GenerateNamespaceLabels(cfg)

	expected := map[string]string{
		"pod-security.kubernetes.io/enforce":         "restricted",
		"pod-security.kubernetes.io/enforce-version": "latest",
		"pod-security.kubernetes.io/audit":           "restricted",
		"pod-security.kubernetes.io/audit-version":   "latest",
		"pod-security.kubernetes.io/warn":            "restricted",
		"pod-security.kubernetes.io/warn-version":    "latest",
	}

	if len(labels) != len(expected) {
		t.Errorf("expected %d labels, got %d", len(expected), len(labels))
	}

	for k, v := range expected {
		if labels[k] != v {
			t.Errorf("label %s: expected %s, got %s", k, v, labels[k])
		}
	}
}

func TestGenerateNamespaceLabelsPartial(t *testing.T) {
	cfg := PSSConfig{Profile: PSSBaseline, Enforce: true, Version: "v1.28"}
	labels := GenerateNamespaceLabels(cfg)

	if len(labels) != 2 {
		t.Errorf("expected 2 labels (enforce only), got %d", len(labels))
	}
	if labels["pod-security.kubernetes.io/enforce"] != "baseline" {
		t.Error("expected baseline profile")
	}
}

func TestRestrictedProfileRules(t *testing.T) {
	rules := RestrictedProfileRules()
	if len(rules) < 10 {
		t.Errorf("expected at least 10 restricted rules, got %d", len(rules))
	}

	ruleNames := make(map[string]bool)
	for _, r := range rules {
		if r.Name == "" {
			t.Error("rule must have a name")
		}
		ruleNames[r.Name] = true
	}

	requiredRules := []string{"Privileged", "RunAsNonRoot", "Capabilities", "Seccomp", "AllowPrivilegeEscalation"}
	for _, name := range requiredRules {
		if !ruleNames[name] {
			t.Errorf("missing required rule: %s", name)
		}
	}
}

// ============================================================================
// PSS Manager Tests
// ============================================================================

func TestApplyPSSToNamespace(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	policy, err := hm.ApplyPSSToNamespace(ctx, "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy.Namespace != "production" {
		t.Errorf("expected namespace production, got %s", policy.Namespace)
	}
	if len(policy.Labels) != 6 {
		t.Errorf("expected 6 labels, got %d", len(policy.Labels))
	}
}

func TestApplyPSSExemptNamespace(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	_, err := hm.ApplyPSSToNamespace(ctx, "kube-system")
	if err == nil {
		t.Error("expected error for exempt namespace")
	}
}

func TestAuditNamespace(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	hm.ApplyPSSToNamespace(ctx, "test-ns")
	violations, err := hm.AuditNamespace(ctx, "test-ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if violations == nil {
		violations = []PSSViolation{}
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations in clean namespace, got %d", len(violations))
	}
}

func TestAuditNamespaceNotFound(t *testing.T) {
	hm := newTestHardeningManager()
	_, err := hm.AuditNamespace(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for unknown namespace")
	}
}

func TestReportViolation(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	hm.ApplyPSSToNamespace(ctx, "test-ns")

	hm.ReportViolation(PSSViolation{
		Namespace: "test-ns",
		PodName:   "bad-pod",
		Container: "main",
		Profile:   PSSRestricted,
		Rule:      "Privileged",
		Description: "Container runs as privileged",
		Severity:  "critical",
	})

	violations := hm.GetViolations("test-ns")
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Rule != "Privileged" {
		t.Errorf("expected Privileged rule, got %s", violations[0].Rule)
	}
	if violations[0].ID == "" {
		t.Error("expected non-empty violation ID")
	}
}

func TestGetViolationsAll(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	hm.ApplyPSSToNamespace(ctx, "ns1")
	hm.ApplyPSSToNamespace(ctx, "ns2")

	hm.ReportViolation(PSSViolation{Namespace: "ns1", PodName: "p1", Rule: "Privileged", Severity: "critical"})
	hm.ReportViolation(PSSViolation{Namespace: "ns2", PodName: "p2", Rule: "RunAsNonRoot", Severity: "high"})

	all := hm.GetViolations("")
	if len(all) != 2 {
		t.Errorf("expected 2 total violations, got %d", len(all))
	}

	ns1 := hm.GetViolations("ns1")
	if len(ns1) != 1 {
		t.Errorf("expected 1 ns1 violation, got %d", len(ns1))
	}
}

func TestGetPSSPolicy(t *testing.T) {
	hm := newTestHardeningManager()
	hm.ApplyPSSToNamespace(context.Background(), "web")

	p, err := hm.GetPSSPolicy("web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Namespace != "web" {
		t.Errorf("expected namespace web, got %s", p.Namespace)
	}
}

func TestGetPSSPolicyNotFound(t *testing.T) {
	hm := newTestHardeningManager()
	_, err := hm.GetPSSPolicy("nope")
	if err == nil {
		t.Error("expected error for missing policy")
	}
}

func TestListPSSPolicies(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	hm.ApplyPSSToNamespace(ctx, "a")
	hm.ApplyPSSToNamespace(ctx, "b")

	policies := hm.ListPSSPolicies()
	if len(policies) != 2 {
		t.Errorf("expected 2 policies, got %d", len(policies))
	}
}

// ============================================================================
// Cosign Image Verification Tests
// ============================================================================

func TestDefaultCosignConfig(t *testing.T) {
	cfg := DefaultCosignConfig()
	if !cfg.Enabled {
		t.Error("expected cosign enabled")
	}
	if cfg.Policy != "reject" {
		t.Errorf("expected reject policy, got %s", cfg.Policy)
	}
	if len(cfg.AllowedRegistries) == 0 {
		t.Error("expected non-empty allowed registries")
	}
}

func TestVerifyImageAllowed(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	result, err := hm.VerifyImage(ctx, "ghcr.io/cloudai-fusion/apiserver:v0.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Verified {
		t.Error("expected verified=true")
	}
	if result.Signer == "" {
		t.Error("expected non-empty signer")
	}
	if result.Digest == "" {
		t.Error("expected non-empty digest")
	}
	if result.Registry != "ghcr.io/cloudai-fusion" {
		t.Errorf("unexpected registry: %s", result.Registry)
	}
}

func TestVerifyImageUnauthorizedRegistry(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	_, err := hm.VerifyImage(ctx, "evil-registry.io/malicious:latest")
	if err == nil {
		t.Error("expected error for unauthorized registry")
	}
}

func TestVerifyImageDisabled(t *testing.T) {
	hm := NewHardeningManager(HardeningConfig{
		PSS: DefaultPSSConfig(),
		Cosign: CosignConfig{Enabled: false},
	})
	ctx := context.Background()

	result, err := hm.VerifyImage(ctx, "any-registry.io/image:tag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Verified {
		t.Error("expected verified when cosign disabled")
	}
}

func TestVerifyAllImages(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	images := []string{
		"ghcr.io/cloudai-fusion/apiserver:v0.1.0",
		"ghcr.io/cloudai-fusion/scheduler:v0.1.0",
		"ghcr.io/cloudai-fusion/ai-engine:v0.1.0",
	}

	summary, err := hm.VerifyAllImages(ctx, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.TotalImages != 3 {
		t.Errorf("expected 3 total, got %d", summary.TotalImages)
	}
	if summary.VerifiedImages != 3 {
		t.Errorf("expected 3 verified, got %d", summary.VerifiedImages)
	}
	if !summary.AllVerified {
		t.Error("expected all_verified=true")
	}
}

func TestVerifyAllImagesWithFailure(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	images := []string{
		"ghcr.io/cloudai-fusion/apiserver:v0.1.0",
		"evil-registry.io/bad:latest",
	}

	summary, err := hm.VerifyAllImages(ctx, images)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.VerifiedImages != 1 {
		t.Errorf("expected 1 verified, got %d", summary.VerifiedImages)
	}
	if summary.FailedImages != 1 {
		t.Errorf("expected 1 failed, got %d", summary.FailedImages)
	}
	if summary.AllVerified {
		t.Error("expected all_verified=false")
	}
}

func TestGetVerificationHistory(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	hm.VerifyImage(ctx, "ghcr.io/cloudai-fusion/apiserver:v0.1.0")
	hm.VerifyImage(ctx, "ghcr.io/cloudai-fusion/scheduler:v0.1.0")

	history := hm.GetVerificationHistory()
	if len(history) != 2 {
		t.Errorf("expected 2 entries, got %d", len(history))
	}
}

func TestSignImage(t *testing.T) {
	hm := newTestHardeningManager()
	ctx := context.Background()

	sig, err := hm.SignImage(ctx, "ghcr.io/cloudai-fusion/apiserver:v0.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Image == "" {
		t.Error("expected non-empty image")
	}
	if sig.Signature == "" {
		t.Error("expected non-empty signature")
	}
	if sig.PublicKey == "" {
		t.Error("expected non-empty public key")
	}
	if sig.Algorithm != "ECDSA-P256-SHA256" {
		t.Errorf("expected ECDSA-P256-SHA256, got %s", sig.Algorithm)
	}
}

func TestExtractRegistry(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ghcr.io/cloudai-fusion/apiserver:v1", "ghcr.io/cloudai-fusion"},
		{"docker.io/library/nginx:latest", "docker.io/library"},
		{"nginx:latest", "docker.io"},
		{"registry.k8s.io/pause:3.9", "registry.k8s.io"},
		{"localhost:5000/myimage:tag", "localhost:5000"},
	}

	for _, tt := range tests {
		got := extractRegistry(tt.input)
		if got != tt.expected {
			t.Errorf("extractRegistry(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCancelledContextPSS(t *testing.T) {
	hm := newTestHardeningManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := hm.ApplyPSSToNamespace(ctx, "test")
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func TestCancelledContextCosign(t *testing.T) {
	hm := newTestHardeningManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := hm.VerifyImage(ctx, "ghcr.io/cloudai-fusion/test:v1")
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}
