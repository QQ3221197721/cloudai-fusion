package auth

import (
	"strconv"
	"testing"
	"time"
)

// rbEnforce is a tiny wrapper so the compiler cannot inline away the call.
func rbEnforce(rb *CompiledRBAC, role, perm string) bool { return rb.Enforce(role, perm) }

// ============================================================================
// Benchmark: CompiledRBAC (O(1) hot path via transitive-closure materialization)
// ============================================================================

// _compiledRBAC mirrors the platform's four core roles with a 4-level
// inheritance chain (Viewer <- Developer <- Operator <- Admin).
var _compiledRBAC = func() *CompiledRBAC {
	rb := NewCompiledRBACBuilder()
	permSets := map[string][]string{
		"Admin":     {"cluster:create", "cluster:read", "cluster:update", "cluster:delete", "workload:create", "workload:read", "workload:update", "workload:delete", "security:manage", "security:read", "user:manage", "user:read", "provider:manage", "provider:read", "monitor:read", "monitor:manage", "cost:read", "cost:manage", "agent:manage", "agent:read"},
		"Operator":  {"cluster:read", "cluster:update", "workload:create", "workload:read", "workload:update", "security:read", "user:read", "provider:read", "monitor:read", "monitor:manage", "cost:read", "agent:read"},
		"Developer": {"cluster:read", "workload:create", "workload:read", "workload:update", "security:read", "provider:read", "monitor:read", "cost:read", "agent:read"},
		"Viewer":    {"cluster:read", "workload:read", "security:read", "provider:read", "monitor:read", "cost:read", "agent:read"},
	}
	for role, perms := range permSets {
		for _, p := range perms {
			rb.Grant(role, p)
		}
	}
	rb.Inherit("Developer", "Viewer")
	rb.Inherit("Operator", "Developer")
	rb.Inherit("Admin", "Operator")
	return rb.Compile()
}()

var rbSink bool

func BenchmarkCompiledRBAC_Allow(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = rbEnforce(_compiledRBAC, "Admin", "cluster:create")
	}
}

func benchmarkCompiledRBACDeny(b *testing.B, role, permission string) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = rbEnforce(_compiledRBAC, role, permission)
	}
}

func BenchmarkCompiledRBAC_Deny_Viewer(b *testing.B)      { benchmarkCompiledRBACDeny(b, "Viewer", "cluster:create") }
func BenchmarkCompiledRBAC_Deny_Operator(b *testing.B)    { benchmarkCompiledRBACDeny(b, "Operator", "security:manage") }
func BenchmarkCompiledRBAC_Deny_UnknownRole(b *testing.B) { benchmarkCompiledRBACDeny(b, "Unknown", "anything") }

// ============================================================================
// Benchmark: Large rulesets (100 / 1000 / 10000 roles x 20 perms) — O(1) invariant
// ============================================================================

func benchmarkLargeRuleset(b *testing.B, roleCount int) {
	builder := NewCompiledRBACBuilder()
	names := make([]string, 0, roleCount)
	for i := 0; i < roleCount; i++ {
		name := "role-" + strconv.Itoa(i)
		names = append(names, name)
		for j := 0; j < 20; j++ {
			builder.Grant(name, "perm-"+strconv.Itoa(j))
		}
	}
	rb := builder.Compile()
	targetPerm := "perm-7"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = rbEnforce(rb, names[i%len(names)], targetPerm)
	}
}

func BenchmarkRuleset_100(b *testing.B)   { benchmarkLargeRuleset(b, 100) }
func BenchmarkRuleset_1000(b *testing.B)  { benchmarkLargeRuleset(b, 1000) }
func BenchmarkRuleset_10000(b *testing.B) { benchmarkLargeRuleset(b, 10000) }

// linearScanEnforce is the naive "before" baseline: a per-call linear scan over
// a flat grant slice, mirroring a pre-optimization RBAC check. It is defined
// only in the benchmark to contrast against CompiledRBAC.Enforce.
func linearScanEnforce(grants []RBACGrant, role, perm string) bool {
	for _, g := range grants {
		if g.Role == role && g.Permission == perm {
			return true
		}
	}
	return false
}

// BenchmarkBaselineLinear_10000 is the "before" measurement for the scale
// optimization: worst-case linear scan of 10000 grants (target near the end).
func BenchmarkBaselineLinear_10000(b *testing.B) {
	grants := make([]RBACGrant, 0, 10000)
	for i := 0; i < 500; i++ {
		for j := 0; j < 20; j++ {
			grants = append(grants, RBACGrant{Role: "role-" + strconv.Itoa(i), Permission: "perm-" + strconv.Itoa(j)})
		}
	}
	// Worst case: role that is last in the slice, permission that is last.
	role := "role-499"
	perm := "perm-19"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = linearScanEnforce(grants, role, perm)
	}
}

// BenchmarkOptimizedCompiled_10000 is the "after" measurement at the same scale.
func BenchmarkOptimizedCompiled_10000(b *testing.B) {
	builder := NewCompiledRBACBuilder()
	for i := 0; i < 500; i++ {
		for j := 0; j < 20; j++ {
			builder.Grant("role-"+strconv.Itoa(i), "perm-"+strconv.Itoa(j))
		}
	}
	rb := builder.Compile()
	role := "role-499"
	perm := "perm-19"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = rbEnforce(rb, role, perm)
	}
}

// ============================================================================
// Benchmark: Role-inheritance chain depth (materialized => O(1) regardless)
// ============================================================================

func benchmarkInheritanceChain(b *testing.B, depth int) {
	builder := NewCompiledRBACBuilder()
	roles := make([]string, depth+1)
	for i := 0; i <= depth; i++ {
		roles[i] = "r" + strconv.Itoa(i)
	}
	// The deepest ancestor owns the permissions; the chain root inherits them.
	for i := 0; i < 20; i++ {
		builder.Grant(roles[depth], "base-perm-"+strconv.Itoa(i))
	}
	for i := depth - 1; i >= 0; i-- {
		builder.Inherit(roles[i], roles[i+1])
	}
	rb := builder.Compile()
	root := roles[0]
	targetPerm := "base-perm-9"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = rbEnforce(rb, root, targetPerm)
	}
}

func BenchmarkChainDepth_2(b *testing.B)   { benchmarkInheritanceChain(b, 2) }
func BenchmarkChainDepth_5(b *testing.B)   { benchmarkInheritanceChain(b, 5) }
func BenchmarkChainDepth_10(b *testing.B)  { benchmarkInheritanceChain(b, 10) }
func BenchmarkChainDepth_25(b *testing.B)  { benchmarkInheritanceChain(b, 25) }
func BenchmarkChainDepth_50(b *testing.B)  { benchmarkInheritanceChain(b, 50) }
func BenchmarkChainDepth_100(b *testing.B) { benchmarkInheritanceChain(b, 100) }

// ============================================================================
// Benchmark: Concurrent throughput on the immutable authorizer (RunParallel)
// ============================================================================

func BenchmarkCompiledRBAC_Parallel(b *testing.B) {
	rb := _compiledRBAC
	roles := []string{"Admin", "Operator", "Developer", "Viewer", "Unknown"}
	perms := []string{"cluster:read", "cluster:create", "workload:delete", "agent:manage", "unknown-perm"}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			rbSink = rbEnforce(rb, roles[i%len(roles)], perms[i%len(perms)])
			i++
		}
	})
}

// ============================================================================
// Benchmark: ABAC policy engine (deny-override, priority-ordered evaluation)
// ============================================================================

func buildABACEngine(policyCount int) *ABACEngine {
	policies := make([]*ABACPolicy, 0, policyCount)
	for i := 0; i < policyCount; i++ {
		policies = append(policies, &ABACPolicy{
			ID:       "p" + strconv.Itoa(i),
			Name:     "policy-" + strconv.Itoa(i),
			Priority: policyCount - i,
			Effect:   EffectAllow,
			Enabled:  true,
			Subject:  SubjectMatch{Roles: []string{"role-" + strconv.Itoa(i)}},
			Resource: ResourceMatch{Types: []string{"workload"}},
			Action:   ActionMatch{Operations: []string{"read"}},
		})
	}
	// Append one policy that will actually match our probe request last, so the
	// engine must scan the whole set (worst-case allow after full traversal).
	policies = append(policies, &ABACPolicy{
		ID: "match", Name: "match", Priority: 0, Effect: EffectAllow, Enabled: true,
		Subject:  SubjectMatch{Roles: []string{"probe"}},
		Resource: ResourceMatch{Types: []string{"workload"}},
		Action:   ActionMatch{Operations: []string{"read"}},
	})
	return NewABACEngine(ABACConfig{Policies: policies})
}

func benchmarkABAC(b *testing.B, policyCount int) {
	engine := buildABACEngine(policyCount)
	req := ABACRequest{
		Role:         "probe",
		ResourceType: "workload",
		Operation:    "read",
		RequestTime:  time.Now().UTC(),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = engine.Evaluate(req)
	}
}

func BenchmarkABAC_100(b *testing.B)   { benchmarkABAC(b, 100) }
func BenchmarkABAC_1000(b *testing.B)  { benchmarkABAC(b, 1000) }
func BenchmarkABAC_10000(b *testing.B) { benchmarkABAC(b, 10000) }

// ============================================================================
// Benchmark: Password Hashing (bcrypt is intentionally slow)
// ============================================================================

func BenchmarkHashPassword(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = HashPassword("BenchmarkP@ssword123!")
	}
}

func BenchmarkCheckPassword(b *testing.B) {
	hash, _ := HashPassword("BenchmarkP@ssword123!")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		CheckPassword("BenchmarkP@ssword123!", hash)
	}
}

// ============================================================================
// Benchmark: JWT Token Generation & Validation
// ============================================================================

func BenchmarkGenerateToken(b *testing.B) {
	svc, _ := NewService(Config{
		JWTSecret: "benchmark-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})
	user := &User{ID: "bench-user", Username: "benchuser", Role: RoleAdmin, Status: "active"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = svc.GenerateToken(user)
	}
}

func BenchmarkValidateToken(b *testing.B) {
	svc, _ := NewService(Config{
		JWTSecret: "benchmark-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})
	user := &User{ID: "bench-user", Username: "benchuser", Role: RoleAdmin, Status: "active"}
	tokenResp, _ := svc.GenerateToken(user)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = svc.ValidateToken(tokenResp.AccessToken)
	}
}

// ============================================================================
// Benchmark: RBAC Permission Check (production HasPermission, index-backed)
// ============================================================================

func BenchmarkHasPermission(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = HasPermission(RoleAdmin, PermClusterCreate)
	}
}

func BenchmarkHasPermission_Miss(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rbSink = HasPermission(RoleViewer, PermClusterCreate) // denied
	}
}

// hasPermissionLinear replicates the pre-optimization linear scan of
// rolePermissions, used only as the "before" baseline for HasPermission.
func hasPermissionLinear(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

func BenchmarkHasPermission_LinearBaseline(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Worst case within a role: last permission in the Admin slice.
		rbSink = hasPermissionLinear(RoleAdmin, PermAgentRead)
	}
}

// ============================================================================
// Fuzz: Password Hashing (handles arbitrary input safely)
// ============================================================================

func FuzzHashPassword(f *testing.F) {
	// Seed corpus
	f.Add("password123")
	f.Add("")
	f.Add("a")
	f.Add("very-long-password-that-exceeds-normal-length-limits-!@#$%^&*()")
	f.Add("密码测试中文")
	f.Add("\x00\x01\x02\xff")

	f.Fuzz(func(t *testing.T, password string) {
		hash, err := HashPassword(password)
		if err != nil {
			// bcrypt has a 72-byte limit; passwords exceeding it may truncate but should not crash
			return
		}
		if hash == "" {
			t.Error("hash should not be empty for valid input")
		}
		// Verify the hash is actually valid bcrypt
		if hash[0] != '$' {
			t.Errorf("hash should start with '$', got: %q", hash[:1])
		}
	})
}

// ============================================================================
// Fuzz: JWT Token Validation (should never panic)
// ============================================================================

func FuzzValidateToken(f *testing.F) {
	svc, _ := NewService(Config{
		JWTSecret: "fuzz-test-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})

	// Seed with valid token
	user := &User{ID: "1", Username: "test", Role: RoleAdmin, Status: "active"}
	tokenResp, _ := svc.GenerateToken(user)

	f.Add(tokenResp.AccessToken)
	f.Add("invalid-token")
	f.Add("")
	f.Add("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.invalid")
	f.Add("not.a.jwt")
	f.Add("\x00\xff\x01")

	f.Fuzz(func(t *testing.T, tokenString string) {
		// ValidateToken should never panic regardless of input
		_, _ = svc.ValidateToken(tokenString)
	})
}
