package security

import (
	"context"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	mgr, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestDefaultPolicies(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	policies, err := mgr.ListPolicies(context.Background())
	if err != nil {
		t.Fatalf("ListPolicies failed: %v", err)
	}
	if len(policies) != 3 {
		t.Fatalf("expected 3 default policies, got %d", len(policies))
	}

	names := map[string]bool{}
	for _, p := range policies {
		names[p.Name] = true
	}
	for _, expected := range []string{"Pod Security Standards", "Default Network Policy", "Image Security Policy"} {
		if !names[expected] {
			t.Errorf("missing default policy: %s", expected)
		}
	}
}

func TestGetPolicy(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})

	p, err := mgr.GetPolicy(context.Background(), "policy-pod-security")
	if err != nil {
		t.Fatalf("GetPolicy failed: %v", err)
	}
	if p.Name != "Pod Security Standards" {
		t.Errorf("expected 'Pod Security Standards', got %q", p.Name)
	}
	if p.Type != PolicyTypePodSecurity {
		t.Errorf("expected type %q, got %q", PolicyTypePodSecurity, p.Type)
	}
}

func TestGetPolicyNotFound(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	_, err := mgr.GetPolicy(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent policy")
	}
}

func TestCreatePolicy(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})

	policy := &SecurityPolicy{
		Name:        "Test Policy",
		Type:        PolicyTypeNetwork,
		Scope:       "cluster",
		Enforcement: EnforcementAudit,
		Rules: []PolicyRule{
			{Name: "rule-1", Condition: "true", Action: "allow"},
		},
	}
	err := mgr.CreatePolicy(context.Background(), policy)
	if err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}
	if policy.ID == "" {
		t.Error("expected ID to be assigned")
	}
	if policy.Status != "active" {
		t.Errorf("expected status 'active', got %q", policy.Status)
	}

	policies, _ := mgr.ListPolicies(context.Background())
	if len(policies) != 4 {
		t.Fatalf("expected 4 policies after create, got %d", len(policies))
	}
}

func TestGetComplianceReport_CIS(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	report, err := mgr.GetComplianceReport(context.Background(), "cluster-1", "CIS")
	if err != nil {
		t.Fatalf("GetComplianceReport failed: %v", err)
	}
	if report.ClusterID != "cluster-1" {
		t.Errorf("expected cluster-1, got %q", report.ClusterID)
	}
	if report.Framework != "CIS" {
		t.Errorf("expected CIS, got %q", report.Framework)
	}
	// Static CIS checks: 11 checks (6 pass, 5 warn)
	if len(report.Checks) < 5 {
		t.Errorf("expected at least 5 CIS checks, got %d", len(report.Checks))
	}
	if report.Score <= 0 {
		t.Errorf("expected positive score, got %f", report.Score)
	}
	if report.Passed == 0 {
		t.Error("expected at least some passed checks")
	}
	// Verify CIS IDs present
	hasID := map[string]bool{}
	for _, c := range report.Checks {
		hasID[c.ID] = true
	}
	for _, id := range []string{"CIS-1.1.1", "CIS-1.2.1", "CIS-5.2.1"} {
		if !hasID[id] {
			t.Errorf("missing CIS check %s", id)
		}
	}
}

func TestGetComplianceReport_NIST(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	report, err := mgr.GetComplianceReport(context.Background(), "cluster-1", "NIST")
	if err != nil {
		t.Fatalf("GetComplianceReport NIST failed: %v", err)
	}
	if report.Framework != "NIST-800-190" {
		t.Errorf("expected NIST-800-190, got %q", report.Framework)
	}
	if len(report.Checks) < 5 {
		t.Errorf("expected at least 5 NIST checks, got %d", len(report.Checks))
	}
	if report.Score <= 0 {
		t.Errorf("expected positive score, got %f", report.Score)
	}
}

func TestRecordAndGetAuditLogs(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})

	mgr.RecordAuditLog(&AuditLogEntry{
		UserID: "u1", Username: "admin", Action: "create",
		ResourceType: "cluster", ResourceID: "c1", Status: "success",
	})
	mgr.RecordAuditLog(&AuditLogEntry{
		UserID: "u2", Username: "dev", Action: "delete",
		ResourceType: "workload", ResourceID: "w1", Status: "success",
	})

	logs, err := mgr.GetAuditLogs(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetAuditLogs failed: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0].ID == "" || logs[1].ID == "" {
		t.Error("expected IDs to be assigned")
	}
}

func TestGetAuditLogsLimit(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	for i := 0; i < 5; i++ {
		mgr.RecordAuditLog(&AuditLogEntry{Action: "test", Status: "success"})
	}
	logs, _ := mgr.GetAuditLogs(context.Background(), 3)
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs with limit, got %d", len(logs))
	}
}

func TestGetThreats(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	threats, err := mgr.GetThreats(context.Background())
	if err != nil {
		t.Fatalf("GetThreats failed: %v", err)
	}
	if threats == nil {
		t.Fatal("expected non-nil threats slice")
	}
	if len(threats) != 0 {
		t.Errorf("expected 0 threats initially, got %d", len(threats))
	}
}

func TestThreatDetection_BruteForce(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})

	// Simulate 6 failed login attempts from same IP
	for i := 0; i < 6; i++ {
		mgr.RecordAuditLog(&AuditLogEntry{
			Username: "attacker", Action: "login", Status: "failure",
			IPAddress: "192.168.1.100", ResourceType: "auth",
		})
	}

	// Run detection
	newThreats := mgr.ThreatDetection().RunDetection(context.Background())
	if len(newThreats) == 0 {
		t.Fatal("expected brute-force threat to be detected")
	}
	if newThreats[0].Type != "brute-force" {
		t.Errorf("expected brute-force type, got %q", newThreats[0].Type)
	}
	if newThreats[0].Severity != "high" {
		t.Errorf("expected high severity, got %q", newThreats[0].Severity)
	}
}

func TestFederationManager(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	fed := mgr.Federation()
	if fed == nil {
		t.Fatal("expected non-nil federation manager")
	}

	// Register provider
	err := fed.RegisterProvider(OIDCProviderConfig{
		ID:        "test-provider",
		Name:      "Test IdP",
		Type:      OIDCProviderGeneric,
		IssuerURL: "https://idp.example.com",
		ClientID:  "test-client",
	})
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	providers := fed.ListProviders()
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].ClientSecret != "***" {
		t.Error("expected client secret to be masked")
	}
}

func TestScannerCreation(t *testing.T) {
	s := NewScanner(ScannerConfig{})
	if s == nil {
		t.Fatal("expected non-nil scanner")
	}
	// Without K8s client, pod scan should return error
	_, err := s.ScanClusterPods(context.Background(), "test")
	if err == nil {
		t.Error("expected error without K8s client")
	}
}

func TestComplianceEngine_Static(t *testing.T) {
	e := NewComplianceEngine()
	report, err := e.RunCISBenchmark(context.Background(), "test-cluster")
	if err != nil {
		t.Fatalf("RunCISBenchmark failed: %v", err)
	}
	if report.Framework != "CIS" {
		t.Errorf("expected CIS, got %q", report.Framework)
	}
	if report.Passed+report.Failed+report.Warnings != len(report.Checks) {
		t.Error("passed + failed + warnings should equal total checks")
	}
}

func TestRunVulnerabilityScan(t *testing.T) {
	mgr, _ := NewManager(ManagerConfig{})
	scan, err := mgr.RunVulnerabilityScan(context.Background(), "cluster-1", "image")
	if err != nil {
		t.Fatalf("RunVulnerabilityScan failed: %v", err)
	}
	if scan.ClusterID != "cluster-1" {
		t.Errorf("expected cluster-1, got %q", scan.ClusterID)
	}
	if scan.ScanType != "image" {
		t.Errorf("expected scan type 'image', got %q", scan.ScanType)
	}
	if scan.Status != "running" {
		t.Errorf("expected status 'running', got %q", scan.Status)
	}
}

func TestPolicyTypeConstants(t *testing.T) {
	tests := map[PolicyType]string{
		PolicyTypeNetwork:     "network",
		PolicyTypeRBAC:        "rbac",
		PolicyTypePodSecurity: "pod-security",
		PolicyTypeCompliance:  "compliance",
		PolicyTypeSecret:      "secret",
		PolicyTypeImage:       "image",
	}
	for pt, expected := range tests {
		if string(pt) != expected {
			t.Errorf("PolicyType %q != %q", pt, expected)
		}
	}
}

func TestEnforcementModeConstants(t *testing.T) {
	if string(EnforcementEnforce) != "enforce" {
		t.Error("EnforcementEnforce mismatch")
	}
	if string(EnforcementAudit) != "audit" {
		t.Error("EnforcementAudit mismatch")
	}
	if string(EnforcementWarn) != "warn" {
		t.Error("EnforcementWarn mismatch")
	}
}

// ============================================================================
// OIDC Federation Enhanced Tests
// ============================================================================

func TestFederationManager_JITProvision(t *testing.T) {
	fm := NewFederationManager()

	identity := &FederatedIdentity{
		ProviderID:   "test-provider",
		ProviderType: OIDCProviderGeneric,
		ExternalSub:  "ext-user-123",
		Email:        "alice@example.com",
		DisplayName:  "Alice",
		MappedRole:   "developer",
	}

	jitCfg := JITProvisionConfig{
		Enabled:      true,
		DefaultRole:  "viewer",
		AutoActivate: true,
	}

	// First provisioning
	user, err := fm.JITProvision(identity, jitCfg)
	if err != nil {
		t.Fatalf("JITProvision failed: %v", err)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %q", user.Email)
	}
	if user.Role != "developer" {
		t.Errorf("expected role 'developer', got %q", user.Role)
	}
	if !user.Active {
		t.Error("expected user to be active with AutoActivate")
	}

	// Re-login should update, not create duplicate
	identity.DisplayName = "Alice Updated"
	user2, err := fm.JITProvision(identity, jitCfg)
	if err != nil {
		t.Fatalf("JITProvision re-login failed: %v", err)
	}
	if user2.DisplayName != "Alice Updated" {
		t.Errorf("expected updated name, got %q", user2.DisplayName)
	}

	// Should only have 1 provisioned user
	users := fm.ListProvisionedUsers()
	if len(users) != 1 {
		t.Errorf("expected 1 provisioned user, got %d", len(users))
	}
}

func TestFederationManager_JITProvisionDomainRestriction(t *testing.T) {
	fm := NewFederationManager()

	jitCfg := JITProvisionConfig{
		Enabled:        true,
		AutoActivate:   true,
		AllowedDomains: []string{"example.com", "corp.io"},
	}

	// Allowed domain
	identity := &FederatedIdentity{
		ProviderID:  "p1",
		ExternalSub: "u1",
		Email:       "bob@example.com",
		MappedRole:  "viewer",
	}
	_, err := fm.JITProvision(identity, jitCfg)
	if err != nil {
		t.Fatalf("expected allowed domain to work: %v", err)
	}

	// Rejected domain
	badIdentity := &FederatedIdentity{
		ProviderID:  "p1",
		ExternalSub: "u2",
		Email:       "eve@evil.com",
		MappedRole:  "viewer",
	}
	_, err = fm.JITProvision(badIdentity, jitCfg)
	if err == nil {
		t.Fatal("expected domain restriction to reject evil.com")
	}
}

func TestFederationManager_JITProvisionDisabled(t *testing.T) {
	fm := NewFederationManager()
	identity := &FederatedIdentity{ProviderID: "p1", ExternalSub: "u1", Email: "a@b.com"}
	_, err := fm.JITProvision(identity, JITProvisionConfig{Enabled: false})
	if err == nil {
		t.Fatal("expected error when JIT provisioning is disabled")
	}
}

func TestFederationManager_TokenRevocation(t *testing.T) {
	fm := NewFederationManager()

	// Initially not revoked
	if fm.IsTokenRevoked("jti-abc") {
		t.Error("token should not be revoked initially")
	}

	// Revoke
	err := fm.RevokeToken("jti-abc", time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	if !fm.IsTokenRevoked("jti-abc") {
		t.Error("token should be revoked after RevokeToken")
	}

	// Revoke with empty JTI should fail
	err = fm.RevokeToken("", time.Now())
	if err == nil {
		t.Error("expected error for empty JTI")
	}

	// IsTokenRevoked with empty JTI should return false
	if fm.IsTokenRevoked("") {
		t.Error("empty JTI should not be revoked")
	}
}

func TestFederationManager_CleanupRevokedTokens(t *testing.T) {
	fm := NewFederationManager()

	// Add expired tokens
	past := time.Now().Add(-1 * time.Hour)
	_ = fm.RevokeToken("expired-1", past)
	_ = fm.RevokeToken("expired-2", past)
	// Add a still-valid token
	_ = fm.RevokeToken("valid-1", time.Now().Add(1*time.Hour))

	cleaned := fm.CleanupRevokedTokens()
	if cleaned != 2 {
		t.Errorf("expected 2 cleaned tokens, got %d", cleaned)
	}

	// expired tokens should be gone
	if fm.IsTokenRevoked("expired-1") {
		t.Error("expired-1 should have been cleaned")
	}
	// valid token should remain
	if !fm.IsTokenRevoked("valid-1") {
		t.Error("valid-1 should still be revoked")
	}
}

func TestFederationManager_Logout(t *testing.T) {
	fm := NewFederationManager()

	// Register a provider
	err := fm.RegisterProvider(OIDCProviderConfig{
		ID:        "test-idp",
		Name:      "Test IdP",
		Type:      OIDCProviderGeneric,
		IssuerURL: "https://idp.example.com",
		ClientID:  "test-client",
	})
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	// Logout with JTI should revoke token
	_, err = fm.Logout(context.Background(), "test-idp", "jti-logout-1", "")
	// Will fail on Discover (no real HTTP server), but should still revoke token
	if fm.IsTokenRevoked("jti-logout-1") != true {
		t.Error("expected token to be revoked after logout")
	}

	// Logout with unknown provider
	_, err = fm.Logout(context.Background(), "nonexistent", "jti-x", "")
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestFederationManager_GetProvisionedUser(t *testing.T) {
	fm := NewFederationManager()

	// Not found
	_, err := fm.GetProvisionedUser("p1", "nonexistent")
	if err == nil {
		t.Fatal("expected not-found error")
	}

	// Provision a user
	_, err = fm.JITProvision(&FederatedIdentity{
		ProviderID: "p1", ExternalSub: "u1", Email: "a@b.com", MappedRole: "admin",
	}, JITProvisionConfig{Enabled: true, AutoActivate: true})
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// Found
	user, err := fm.GetProvisionedUser("p1", "u1")
	if err != nil {
		t.Fatalf("GetProvisionedUser failed: %v", err)
	}
	if user.Email != "a@b.com" {
		t.Errorf("expected a@b.com, got %q", user.Email)
	}
	if user.Role != "admin" {
		t.Errorf("expected admin, got %q", user.Role)
	}
}

func TestFederationManager_InvalidateProviderCache(t *testing.T) {
	fm := NewFederationManager()

	// Register provider
	err := fm.RegisterProvider(OIDCProviderConfig{
		ID:        "cache-test",
		Name:      "Cache Test",
		Type:      OIDCProviderGeneric,
		IssuerURL: "https://idp.example.com",
		ClientID:  "client-1",
	})
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	// Manually inject discovery cache
	fm.mu.Lock()
	fm.discoveries["cache-test"] = &OIDCDiscovery{Issuer: "test"}
	fm.jwksCache["cache-test"] = &jwksCacheEntry{JWKS: &JWKS{}, FetchedAt: time.Now()}
	fm.mu.Unlock()

	// Invalidate
	fm.InvalidateProviderCache("cache-test")

	fm.mu.RLock()
	_, hasDiscovery := fm.discoveries["cache-test"]
	_, hasJWKS := fm.jwksCache["cache-test"]
	fm.mu.RUnlock()

	if hasDiscovery {
		t.Error("expected discovery cache to be invalidated")
	}
	if hasJWKS {
		t.Error("expected JWKS cache to be invalidated")
	}
}

func TestFederationManager_JWKSCacheEntry(t *testing.T) {
	// Verify jwksCacheEntry structure works correctly
	entry := &jwksCacheEntry{
		JWKS:      &JWKS{Keys: []JWK{{Kid: "key-1", Kty: "RSA"}}},
		FetchedAt: time.Now(),
		TTL:       defaultJWKSCacheTTL,
	}
	if entry.TTL != 1*time.Hour {
		t.Errorf("expected 1h TTL, got %v", entry.TTL)
	}
	if len(entry.JWKS.Keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(entry.JWKS.Keys))
	}
}
