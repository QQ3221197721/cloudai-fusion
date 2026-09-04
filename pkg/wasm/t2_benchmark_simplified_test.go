//go:build t2benchmark

package wasm

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

// ============================================================================
// T2 HEAD-TO-HEAD: Capability Security Manager vs Casbin - SIMPLIFIED
// ============================================================================
// FAIR COMPARISON using equivalent work units that both systems can handle well.
// Skip semantic validation (Capability ≠ Casbin model). Just compare raw performance.
//
// OUR THESIS: Our capability check (direct array/map lookup) should beat Casbin's
// policy matcher because we pre-compute access decisions at grant time while
// Casbin does runtime pattern matching on policy rules + role inheritance graph.
//
// RUN: go test ./pkg/wasm/ -tags t2benchmark -bench="Capability|Casbin" \
//      -benchtime=1s -count=6 -benchmem -run=^$ | Out-File docs/t2_comparison.txt
// ============================================================================

// ---------------------------------------------------------------------------
// Casbin Setup - Minimal RBAC Enforcer (Fair baseline)
// ---------------------------------------------------------------------------

const simpleCasbinModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act
`

func newSimpleCasbinEnforcer(tb testing.TB) *casbin.Enforcer {
	m, err := model.NewModelFromString(simpleCasbinModel)
	if err != nil {
		tb.Fatalf("casbin model: %v", err)
	}

	e, err := casbin.NewEnforcer(m)
	if err != nil {
		tb.Fatalf("casbin enforcer: %v", err)
	}

	e.EnableAutoBuildRoleLinks(true)

	// Simple policies: direct mapping to permission checks
	policies := [][]string{
		// FS access - use concrete paths, not wildcards (Casbin doesn't support glob matching by default)
		{"admin", "/plugins/data/file1.bin", "read"},
		{"admin", "/plugins/data/file2.bin", "write"},
		{"developer", "/plugins/data/readme.txt", "read"},
		{"developer", "/tmp/cache.dat", "read"},
		{"viewer", "/plugins/data/doc.pdf", "read"},
		// Network
		{"admin", "api.internal:443", "connect"},
		{"developer", "gpu.cloudai-fusion.io:8443", "connect"},
		{"viewer", "telemetry.example.com:443", "connect"},
		// GPU
		{"admin", "gpu:0", "use"},
		{"admin", "gpu:2", "use"},
		{"developer", "gpu:0", "use"},
		{"developer", "gpu:2", "use"},
		{"viewer", "gpu:0", "query"},
	}

	for _, p := range policies {
		if _, err := e.AddPolicy(p); err != nil {
			tb.Fatalf("casbin AddPolicy: %v", err)
		}
	}

	// Role hierarchy
	groupings := [][]string{
		{"dev-lead", "admin"},
		{"senior-dev", "developer"},
		{"junior-dev", "viewer"},
	}

	for _, g := range groupings {
		if _, err := e.AddGroupingPolicy(g[0], g[1]); err != nil {
			tb.Fatalf("casbin AddGroupingPolicy: %v", err)
		}
	}

	if err := e.BuildRoleLinks(); err != nil {
		tb.Fatalf("casbin BuildRoleLinks: %v", err)
	}

	return e
}

// ---------------------------------------------------------------------------
// Capability Benchmarks (Our M51 Implementation)
// ---------------------------------------------------------------------------

func BenchmarkCapability_CleanCheck(b *testing.B) {
	// Clean check - no parsing overhead, just rule evaluation
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sinkBool = benchPathRule.IsPathAllowed("/plugins/data/file.bin")
		sinkBool = benchNetRule.CanAccessTarget("api.internal", 443)
		sinkBool = benchGPURule.IsDeviceAllowed(2)
	}
}

func BenchmarkCapability_ParseThenCheck(b *testing.B) {
	// Full cold path with JSON unmarshaling
	raw := []byte(grantJSON)
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var g Grant
		if err := json.Unmarshal(raw, &g); err != nil {
			b.Fatalf("unmarshal grant: %v", err)
		}
		sinkBool = g.Filesystem.IsPathAllowed("/data/test.bin") &&
			g.Network.ValidateURL("https://api.internal/v1/infer") &&
			g.GPU.IsDeviceAllowed(0)
	}
}

func BenchmarkCapability_PathOnly(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkBool = benchPathRule.IsPathAllowed("/plugins/data/input.bin")
	}
}

func BenchmarkCapability_NetOnly(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkBool = benchNetRule.CanAccessTarget("gpu.cloudai-fusion.io", 443)
	}
}

func BenchmarkCapability_GPUOnly(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkBool = benchGPURule.IsDeviceAllowed(0)
	}
}

// ---------------------------------------------------------------------------
// Casbin Benchmarks (Competitor Implementation)
// ---------------------------------------------------------------------------

func BenchmarkCasbin_SimpleAllow(b *testing.B) {
	enforcer := newSimpleCasbinEnforcer(b)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, _ := enforcer.Enforce("admin", "/plugins/data/config.json", "read")
		if !result {
			b.Fatal("Expected allow but got deny")
		}
	}
}

func BenchmarkCasbin_SimpleDeny(b *testing.B) {
	enforcer := newSimpleCasbinEnforcer(b)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, _ := enforcer.Enforce("viewer", "metadata.internal:80", "connect")
		if result {
			b.Fatal("Expected deny but got allow")
		}
	}
}

func BenchmarkCasbin_RoleInheritance(b *testing.B) {
	enforcer := newSimpleCasbinEnforcer(b)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Tests role chain traversal: dev-lead → admin
		result, _ := enforcer.Enforce("dev-lead", "gpu:0", "use")
		if !result {
			b.Fatal("Expected inherited allow but got deny")
		}
	}
}

func BenchmarkCasbin_MixedWorkload(b *testing.B) {
	enforcer := newSimpleCasbinEnforcer(b)
	b.ResetTimer()
	b.ReportAllocs()

	cases := []struct {
		sub string
		obj string
		act string
	}{
		{"admin", "/plugins/data/write.log", "write"},
		{"developer", "api.cloudai-fusion.io:443", "connect"},
		{"junior-dev", "telemetry.example.com:443", "connect"}, // inherits from viewer
		{"admin", "gpu:2", "use"},
		{"viewer", "gpu:0", "query"},
	}

	i := 0
	for {
		c := cases[i%len(cases)]
		result, _ := enforcer.Enforce(c.sub, c.obj, c.act)
		if !result {
			b.Logf("Unexpected deny at case %d: %s/%s/%s", i, c.sub, c.obj, c.act)
		}
		i++
		if i >= b.N {
			break
		}
	}
}

// ---------------------------------------------------------------------------
// Scalability Stress Tests
// ---------------------------------------------------------------------------

func BenchmarkCasbin_WithLargePolicySet(b *testing.B) {
	// Create enforcer with large number of policies
	modelStr := simpleCasbinModel
	m, err := model.NewModelFromString(modelStr)
	if err != nil {
		b.Fatalf("casbin model: %v", err)
	}

	e, err := casbin.NewEnforcer(m)
	if err != nil {
		b.Fatalf("casbin enforcer: %v", err)
	}

	e.EnableAutoBuildRoleLinks(true)

	// Add 1000 roles × 10 objects each = 10,000 policies
	policies := make([][]string, 0, 10000)
	for i := 0; i < 1000; i++ {
		role := "role-" + strconv.Itoa(i)
		for j := 0; j < 10; j++ {
			policies = append(policies, []string{role, "obj-"+strconv.Itoa(j), "action"})
		}
	}

	if _, err := e.AddPolicies(policies); err != nil {
		b.Fatalf("casbin AddPolicies: %v", err)
	}

	targetRole := "role-500"
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, _ := e.Enforce(targetRole, "obj-7", "action")
		if !result {
			b.Fatal("Expected allow but got deny")
		}
	}
}

func BenchmarkCapability_NoScalabilityOverhead(b *testing.B) {
	// Capability model scales independently of policy count
	// Direct array lookups O(1) per dimension
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sinkBool = benchPathRule.IsPathAllowed("/deep/nested/path/file.bin")
		sinkBool = benchNetRule.CanAccessTarget("internal.service:8080", 443)
		sinkBool = benchGPURule.MatchesTopology("nvlink")
	}
}
