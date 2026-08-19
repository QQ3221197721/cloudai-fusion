package auth

// module27_authz_test.go adds Module-27 (RBAC/ABAC authorization) benchmarks and
// correctness tests used for the competitive comparison against Casbin / OPA /
// K8s RBAC. It intentionally isolates the new coverage from the pre-existing
// auth_bench_test.go so the baseline benchmarks remain untouched.
//
// Benchmark coverage added here:
//   - Three-layer permission model (RBAC role check + PermissionGrant refinement)
//   - ABAC policy evaluation, simple (role only) and complex (multi-condition:
//     department + tags + MFA + resource sensitivity + CIDR IP range + time window)
//   - bcrypt at explicit, labelled cost factors (numbers are meaningless without cost)
//   - FieldFilter field-level filtering / masking overhead

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

// silentLogger returns a logger that discards output so benchmark timings are
// not polluted by log I/O.
func silentLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.PanicLevel)
	return l
}

// ============================================================================
// Benchmark: three-layer permission model (RBAC + fine-grained PermissionGrant)
// ============================================================================

func newBenchPermissionManager() *PermissionManager {
	return NewPermissionManager(PermissionManagerConfig{
		Grants: DefaultPermissionGrants(),
		Logger: silentLogger(),
	})
}

// BenchmarkPermissionManager_CheckPermission_Allow measures a full three-layer
// evaluation: RBAC role check followed by fine-grained grant scope/condition
// refinement and field-filter merge.
func BenchmarkPermissionManager_CheckPermission_Allow(b *testing.B) {
	pm := newBenchPermissionManager()
	req := PermissionCheckRequest{
		UserID:       "u-1",
		Role:         RoleOperator,
		Permission:   PermClusterRead,
		ResourceType: "cluster",
		ResourceID:   "c-1",
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !pm.CheckPermission(req).Allowed {
			b.Fatal("expected allow")
		}
	}
}

// BenchmarkPermissionManager_CheckPermission_Deny measures the deny path where
// RBAC rejects and no explicit grant is found.
func BenchmarkPermissionManager_CheckPermission_Deny(b *testing.B) {
	pm := newBenchPermissionManager()
	req := PermissionCheckRequest{
		UserID:       "u-2",
		Role:         RoleViewer,
		Permission:   PermClusterDelete,
		ResourceType: "cluster",
		ResourceID:   "c-1",
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if pm.CheckPermission(req).Allowed {
			b.Fatal("expected deny")
		}
	}
}

// ============================================================================
// Benchmark: ABAC policy evaluation
// ============================================================================

// complexABACEngine builds an engine whose matching policy only fires after the
// request satisfies subject (role+department+tags+MFA), resource
// (type+namespace+sensitivity+tags), action, and environment (CIDR + time
// window + source type) predicates. Several higher-priority policies are placed
// ahead of it so Evaluate must traverse and partially match multiple policies.
func complexABACEngine() *ABACEngine {
	now := time.Now().UTC()
	policies := []*ABACPolicy{
		{
			ID: "deny-restricted-no-mfa", Name: "deny-restricted-no-mfa",
			Priority: 1000, Effect: EffectDeny, Enabled: true,
			Subject:  SubjectMatch{},
			Resource: ResourceMatch{Sensitivity: []string{"restricted"}},
			Action:   ActionMatch{Operations: []string{"delete"}},
		},
		{
			ID: "allow-operator-generic", Name: "allow-operator-generic",
			Priority: 700, Effect: EffectAllow, Enabled: true,
			Subject:  SubjectMatch{Roles: []string{"operator"}},
			Resource: ResourceMatch{Types: []string{"workload"}}, // won't match cluster
			Action:   ActionMatch{Operations: []string{"read"}},
		},
		{
			ID: "allow-complex", Name: "allow-complex",
			Priority: 500, Effect: EffectAllow, Enabled: true,
			Subject: SubjectMatch{
				Roles:       []string{"operator"},
				Departments: []string{"platform-sre"},
				Tags:        map[string]string{"clearance": "high", "team": "infra"},
				MFARequired: true,
			},
			Resource: ResourceMatch{
				Types:       []string{"cluster"},
				Namespaces:  []string{"prod"},
				Sensitivity: []string{"confidential"},
				Tags:        map[string]string{"env": "prod"},
			},
			Action: ActionMatch{Operations: []string{"update"}},
			Environment: EnvironmentMatch{
				IPRanges:    []string{"10.0.0.0/8"},
				SourceTypes: []string{"api"},
				TimeWindows: []TimeWindow{{StartHour: 0, EndHour: 24}},
			},
			CreatedAt: now,
		},
	}
	return NewABACEngine(ABACConfig{Policies: policies, Logger: silentLogger()})
}

func complexABACRequest() ABACRequest {
	return ABACRequest{
		UserID:       "u-42",
		Role:         RoleOperator,
		Department:   "platform-sre",
		Tags:         map[string]string{"clearance": "high", "team": "infra"},
		MFADone:      true,
		ResourceType: "cluster",
		Namespace:    "prod",
		Sensitivity:  "confidential",
		ResourceTags: map[string]string{"env": "prod"},
		Operation:    "update",
		ClientIP:     "10.1.2.3",
		SourceType:   "api",
		RequestTime:  time.Now().UTC(),
	}
}

// BenchmarkABACEvaluate_Simple measures a single-predicate (role-only) allow.
func BenchmarkABACEvaluate_Simple(b *testing.B) {
	engine := NewABACEngine(ABACConfig{Logger: silentLogger()})
	req := ABACRequest{
		Role:         RoleAdmin,
		ResourceType: "cluster",
		Operation:    "read",
		ClientIP:     "10.0.0.1",
		RequestTime:  time.Now().UTC(),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !engine.Evaluate(req).Allowed {
			b.Fatal("expected allow")
		}
	}
}

// BenchmarkABACEvaluate_Complex measures a multi-condition allow that must clear
// subject/resource/action/environment predicates including CIDR + time window.
func BenchmarkABACEvaluate_Complex(b *testing.B) {
	engine := complexABACEngine()
	req := complexABACRequest()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !engine.Evaluate(req).Allowed {
			b.Fatal("expected allow")
		}
	}
}

// ============================================================================
// Benchmark: bcrypt at explicit cost factors (cost MUST be reported)
// ============================================================================

func benchmarkBcryptHashAtCost(b *testing.B, cost int) {
	b.Helper()
	pw := []byte("BenchmarkP@ssword123!")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := bcrypt.GenerateFromPassword(pw, cost); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkBcryptVerifyAtCost(b *testing.B, cost int) {
	b.Helper()
	pw := []byte("BenchmarkP@ssword123!")
	hash, err := bcrypt.GenerateFromPassword(pw, cost)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bcrypt.CompareHashAndPassword(hash, pw)
	}
}

// bcrypt.DefaultCost is 10; production deployments frequently use 12. Both are
// benchmarked so the reported latency is unambiguous.
func BenchmarkBcryptHash_Cost10(b *testing.B)   { benchmarkBcryptHashAtCost(b, 10) }
func BenchmarkBcryptHash_Cost12(b *testing.B)   { benchmarkBcryptHashAtCost(b, 12) }
func BenchmarkBcryptVerify_Cost10(b *testing.B) { benchmarkBcryptVerifyAtCost(b, 10) }
func BenchmarkBcryptVerify_Cost12(b *testing.B) { benchmarkBcryptVerifyAtCost(b, 12) }

// ============================================================================
// Benchmark: FieldFilter field-level filtering / masking
// ============================================================================

func benchResourceData() map[string]interface{} {
	return map[string]interface{}{
		"id":         "c-1",
		"name":       "prod-cluster",
		"status":     "running",
		"provider":   "aws",
		"region":     "us-east-1",
		"created_at": "2026-01-01T00:00:00Z",
		"kubeconfig": "apiVersion: v1 ...",
		"api_key":    "sk-secret-value",
		"credentials": map[string]string{
			"password": "p", "token": "t",
		},
		"labels": map[string]string{"team": "infra"},
	}
}

// BenchmarkFilterFields_Mask measures masking + deny list application (standard
// data access).
func BenchmarkFilterFields_Mask(b *testing.B) {
	filter := &FieldFilter{
		DeniedFields: []string{"credentials"},
		MaskedFields: []string{"kubeconfig", "api_key"},
	}
	data := benchResourceData()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = FilterFields(data, filter, DataAccessStandard)
	}
}

// BenchmarkFilterFields_Whitelist measures allow-list (restricted access) which
// also runs the sensitive-field heuristic.
func BenchmarkFilterFields_Whitelist(b *testing.B) {
	filter := &FieldFilter{
		AllowedFields: []string{"id", "name", "status", "provider", "region", "created_at"},
		MaskedFields:  []string{"kubeconfig"},
	}
	data := benchResourceData()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = FilterFields(data, filter, DataAccessRestricted)
	}
}

// ============================================================================
// Correctness: key rotation, unauthorized deny, field masking
// ============================================================================

// signHS256 signs a minimal token with the given secret so key-rotation
// verification can be exercised end-to-end.
func signHS256(t *testing.T, secret []byte, userID string) string {
	t.Helper()
	claims := &Claims{
		UserID:   userID,
		Username: "rot-user",
		Role:     RoleViewer,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

// verifyClaimsFunc returns a claimsFunc for VerifyWithAnyValidKey that parses an
// HS256 token against a candidate SigningKey's secret.
func verifyClaimsFunc(tokenString string) func(key *SigningKey) (*Claims, error) {
	return func(key *SigningKey) (*Claims, error) {
		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(tk *jwt.Token) (interface{}, error) {
			if _, ok := tk.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return key.Secret, nil
		})
		if err != nil {
			return nil, err
		}
		claims, ok := token.Claims.(*Claims)
		if !ok || !token.Valid {
			return nil, ErrTokenInvalid
		}
		return claims, nil
	}
}

// TestModule27_KeyRotation_OldTokenBehavior verifies that after rotation a token
// signed with the deprecated key still validates during its grace window, and
// stops validating once that key is revoked.
func TestModule27_KeyRotation_OldTokenBehavior(t *testing.T) {
	kr, err := NewKeyRotator()
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	oldKey := kr.CurrentKey()
	oldToken := signHS256(t, oldKey.Secret, "u-old")

	// Rotate: old key becomes "deprecated" (still valid within window).
	if _, err := kr.Rotate(); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// New tokens use the new key.
	newKey := kr.CurrentKey()
	if newKey.Kid == oldKey.Kid {
		t.Fatal("expected a new current key after rotation")
	}
	newToken := signHS256(t, newKey.Secret, "u-new")

	// Old token still verifies during the deprecation grace window (multi-key).
	if _, err := kr.VerifyWithAnyValidKey(oldToken, verifyClaimsFunc(oldToken)); err != nil {
		t.Errorf("old token should still verify during deprecation window: %v", err)
	}
	// New token verifies against the current key.
	if _, err := kr.VerifyWithAnyValidKey(newToken, verifyClaimsFunc(newToken)); err != nil {
		t.Errorf("new token should verify: %v", err)
	}

	// Revoke the old key: its tokens must now be rejected.
	kr.mu.Lock()
	if k, ok := kr.keys[oldKey.Kid]; ok {
		k.Revoked = true
		k.Status = "revoked"
	}
	kr.mu.Unlock()

	if _, err := kr.VerifyWithAnyValidKey(oldToken, verifyClaimsFunc(oldToken)); err == nil {
		t.Error("old token must be rejected after its signing key is revoked")
	}
	// New token still fine.
	if _, err := kr.VerifyWithAnyValidKey(newToken, verifyClaimsFunc(newToken)); err != nil {
		t.Errorf("new token should still verify after old key revoked: %v", err)
	}
}

// TestModule27_UnauthorizedDenied verifies unauthorized access is rejected at
// all three layers: RBAC (HasPermission), fine-grained PermissionManager, and
// ABAC.
func TestModule27_UnauthorizedDenied(t *testing.T) {
	// Layer 1: RBAC.
	if HasPermission(RoleViewer, PermClusterDelete) {
		t.Error("RBAC: viewer must not have cluster:delete")
	}

	// Layer 2: fine-grained PermissionManager.
	pm := NewPermissionManager(PermissionManagerConfig{Logger: silentLogger()})
	res := pm.CheckPermission(PermissionCheckRequest{
		UserID:       "u-view",
		Role:         RoleViewer,
		Permission:   PermClusterDelete,
		ResourceType: "cluster",
		ResourceID:   "c-1",
	})
	if res.Allowed {
		t.Errorf("PermissionManager: viewer must be denied cluster:delete, reason=%q", res.Reason)
	}
	if res.DataAccess != DataAccessNone {
		t.Errorf("denied request should carry DataAccessNone, got %q", res.DataAccess)
	}

	// Layer 3: ABAC (default policies deny viewer delete).
	engine := NewABACEngine(ABACConfig{Logger: silentLogger()})
	dec := engine.Evaluate(ABACRequest{
		Role:         RoleViewer,
		ResourceType: "cluster",
		Operation:    "delete",
		Sensitivity:  "confidential",
		RequestTime:  time.Now().UTC(),
	})
	if dec.Allowed {
		t.Errorf("ABAC: viewer delete must be denied, reason=%q", dec.Reason)
	}
}

// TestModule27_FieldFilterMasksSensitive verifies field-level filtering masks,
// denies, and whitelists fields correctly.
func TestModule27_FieldFilterMasksSensitive(t *testing.T) {
	data := map[string]interface{}{
		"id":         "c-1",
		"name":       "prod",
		"kubeconfig": "SECRET-KUBECONFIG",
		"api_key":    "sk-123",
		"password":   "hunter2",
	}

	// Masking: masked fields become "***", denied fields disappear.
	filter := &FieldFilter{
		DeniedFields: []string{"password"},
		MaskedFields: []string{"kubeconfig", "api_key"},
	}
	out := FilterFields(data, filter, DataAccessStandard)

	if _, ok := out["password"]; ok {
		t.Error("denied field 'password' must be removed")
	}
	if out["kubeconfig"] != "***" {
		t.Errorf("kubeconfig should be masked to '***', got %v", out["kubeconfig"])
	}
	if out["api_key"] != "***" {
		t.Errorf("api_key should be masked to '***', got %v", out["api_key"])
	}
	if out["name"] != "prod" {
		t.Errorf("non-sensitive field 'name' should pass through, got %v", out["name"])
	}

	// Restricted access drops fields whose NAME matches the sensitive-field
	// heuristic (substring match). Note: "kubeconfig" does NOT contain any
	// sensitive pattern (e.g. "key"), so the heuristic does not catch it — it
	// must be denied/masked via an explicit FieldFilter. This is an honest
	// limitation of the name-based heuristic, verified here.
	restricted := FilterFields(data, nil, DataAccessRestricted)
	for _, k := range []string{"api_key", "password"} {
		if _, ok := restricted[k]; ok {
			t.Errorf("restricted access must drop heuristic-sensitive field %q", k)
		}
	}
	if _, ok := restricted["kubeconfig"]; !ok {
		t.Error("kubeconfig is NOT matched by the name heuristic; it should pass through restricted access without an explicit filter")
	}
	if restricted["name"] != "prod" {
		t.Error("restricted access should keep non-sensitive 'name'")
	}

	// No access returns the restricted sentinel.
	none := FilterFields(data, nil, DataAccessNone)
	if none["_restricted"] != true {
		t.Errorf("DataAccessNone must return restricted sentinel, got %v", none)
	}
}
