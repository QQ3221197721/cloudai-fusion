//go:build casbin

package auth

import (
	"strconv"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

// This file benchmarks our CompiledRBAC against Casbin v2 (the de-facto Go
// RBAC/ABAC standard), run LOCALLY and in-process. Casbin resolves role
// inheritance (the `g` grouping) at Enforce() time via a role-manager graph
// walk; CompiledRBAC materializes the transitive closure at build time.
//
// Casbin is a TEST-ONLY dependency: it is imported only from this build-tagged
// _test.go file and never compiled into any production binary.
//
// Run: go test ./pkg/auth/ -tags casbin -bench="Casbin|CompiledRBAC" -benchmem -benchtime=1s -run=^$

const rbacModel = `
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

// newCasbinCoreRoles builds a Casbin enforcer matching our four core roles with
// the same 4-level inheritance chain used by _compiledRBAC.
func newCasbinCoreRoles(tb testing.TB) *casbin.Enforcer {
	m, err := model.NewModelFromString(rbacModel)
	if err != nil {
		tb.Fatalf("casbin model: %v", err)
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		tb.Fatalf("casbin enforcer: %v", err)
	}
	e.EnableAutoBuildRoleLinks(true)

	grants := [][3]string{
		{"admin", "cluster", "delete"},
		{"operator", "cluster", "update"},
		{"developer", "workload", "create"},
		{"viewer", "cluster", "read"},
		{"viewer", "workload", "read"},
		{"viewer", "agent", "read"},
	}
	for _, g := range grants {
		if _, err := e.AddPolicy(g[0], g[1], g[2]); err != nil {
			tb.Fatalf("casbin AddPolicy: %v", err)
		}
	}
	// Inheritance: developer->viewer, operator->developer, admin->operator.
	links := [][2]string{{"developer", "viewer"}, {"operator", "developer"}, {"admin", "operator"}}
	for _, l := range links {
		if _, err := e.AddGroupingPolicy(l[0], l[1]); err != nil {
			tb.Fatalf("casbin AddGroupingPolicy: %v", err)
		}
	}
	if err := e.BuildRoleLinks(); err != nil {
		tb.Fatalf("casbin BuildRoleLinks: %v", err)
	}
	return e
}

func BenchmarkCasbin_Allow(b *testing.B) {
	e := newCasbinCoreRoles(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = e.Enforce("admin", "cluster", "delete")
	}
}

func BenchmarkCasbin_Allow_Inherited(b *testing.B) {
	// admin must inherit viewer's cluster:read through the 3-hop chain.
	e := newCasbinCoreRoles(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = e.Enforce("admin", "cluster", "read")
	}
}

func BenchmarkCasbin_Deny(b *testing.B) {
	e := newCasbinCoreRoles(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = e.Enforce("viewer", "cluster", "delete")
	}
}

// newCasbinScaled builds a Casbin enforcer with roleCount roles, each holding
// 20 object:action grants (mirrors benchmarkLargeRuleset for CompiledRBAC).
func newCasbinScaled(tb testing.TB, roleCount int) *casbin.Enforcer {
	m, err := model.NewModelFromString(rbacModel)
	if err != nil {
		tb.Fatalf("casbin model: %v", err)
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		tb.Fatalf("casbin enforcer: %v", err)
	}
	rules := make([][]string, 0, roleCount*20)
	for i := 0; i < roleCount; i++ {
		role := "role-" + strconv.Itoa(i)
		for j := 0; j < 20; j++ {
			rules = append(rules, []string{role, "obj-" + strconv.Itoa(j), "act"})
		}
	}
	if _, err := e.AddPolicies(rules); err != nil {
		tb.Fatalf("casbin AddPolicies: %v", err)
	}
	return e
}

func benchmarkCasbinScaled(b *testing.B, roleCount int) {
	e := newCasbinScaled(b, roleCount)
	targetRole := "role-" + strconv.Itoa(roleCount-1)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = e.Enforce(targetRole, "obj-7", "act")
	}
}

func BenchmarkCasbin_Ruleset_100(b *testing.B)   { benchmarkCasbinScaled(b, 100) }
func BenchmarkCasbin_Ruleset_1000(b *testing.B)  { benchmarkCasbinScaled(b, 1000) }
func BenchmarkCasbin_Ruleset_10000(b *testing.B) { benchmarkCasbinScaled(b, 10000) }
