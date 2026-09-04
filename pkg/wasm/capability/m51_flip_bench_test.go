// Package wasm/capability — Module 51: FLIP Benchmark vs REAL Casbin v2 RBAC Competitor.
//
// HEAD-TO-HEAD LATENCY: precompiled WASM/WASI bitmap (pkg/wasm/capability) vs
// real competitor enforcement: GitHub's production-grade Casbin v2 RBAC engine.
//
// Both sides are loaded with identical policies and MUST return identical allow/deny
// decisions per test coverage (correctness proof). Then we measure ns/op for the hot
// permission-check path at N=100 and N=1000 capabilities, count=6 median.
//
// NEVER fake, NEVER edge-only. All numbers come from real Casbin enforcement.
package capability

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/stretchr/testify/require"
)

// generatePermissions builds N deterministic capabilities for benchmarking.
func generatePermissions(n int) []PermissionSpec {
	perms := make([]PermissionSpec, n)
	actions := []string{"import", "export", "read"}
	for i := 0; i < n; i++ {
		perms[i] = PermissionSpec{
			Subject: fmt.Sprintf("perm_%d", i),
			Object:  fmt.Sprintf("/data/path/%d", i),
			Action:  actions[i%len(actions)],
		}
	}
	return perms
}

// ============================================================================
// CASBIN V2 ENFORCER (REAL COMPETITOR)
// ============================================================================

type casbinEnforcer struct {
	enf        *casbin.Enforcer
	configured bool
}

func newCasbinACLModel() (model.Model, error) {
	const conf = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = r.sub == p.sub && r.obj == p.obj && r.act == p.act
`
	return model.NewModelFromString(conf)
}

func newCasbinEnforcer() (*casbinEnforcer, error) {
	m, err := newCasbinACLModel()
	if err != nil {
		return nil, fmt.Errorf("build casbin model: %w", err)
	}
	enf, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, fmt.Errorf("create casbin enforcer: %w", err)
	}
	enf.EnableAutoSave(false)
	enf.EnableLog(false)
	return &casbinEnforcer{enf: enf}, nil
}

func (c *casbinEnforcer) configure(grants []PermissionSpec) error {
	rules := make([][]string, len(grants))
	for i, g := range grants {
		rules[i] = []string{g.Subject, g.Object, g.Action}
	}
	if _, err := c.enf.AddPolicies(rules); err != nil {
		return fmt.Errorf("add casbin policies: %w", err)
	}
	c.configured = true
	return nil
}

func (c *casbinEnforcer) CheckPermission(sub, obj, act string) bool {
	if !c.configured {
		return false
	}
	ok, _ := c.enf.Enforce(sub, obj, act)
	return ok
}

// ============================================================================
// CAP'N PROTO STYLE REFERENCE (MAP-OF-MAPS STRUCTURE)
// ============================================================================

type capnProtoRef struct {
	perms map[string]map[string]map[string]struct{} // subject->object->action set
	count int
}

func newCapnProtoRef() *capnProtoRef {
	return &capnProtoRef{perms: make(map[string]map[string]map[string]struct{})}
}

func (c *capnProtoRef) setup(grants []PermissionSpec) {
	for _, g := range grants {
		if c.perms[g.Subject] == nil {
			c.perms[g.Subject] = make(map[string]map[string]struct{})
		}
		if c.perms[g.Subject][g.Object] == nil {
			c.perms[g.Subject][g.Object] = make(map[string]struct{})
		}
		c.perms[g.Subject][g.Object][g.Action] = struct{}{}
		c.count++
	}
}

func (c *capnProtoRef) CheckPermission(sub, obj, act string) bool {
	acts, ok := c.perms[sub][obj]
	if !ok {
		return false
	}
	_, ok = acts[act]
	return ok
}

// ============================================================================
// FLIP BENCHMARKS
// ============================================================================

var benchSink bool

func BenchmarkCapabilityBitmap_N100(b *testing.B) {
	grants := generatePermissions(100)
	m := NewManager(grants)
	var s bool
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = m.CheckPermission("perm_50", "/data/path/50", "import")
	}
	benchSink = s
	runtime.KeepAlive(m)
}

func BenchmarkCapabilityBitmap_N1000(b *testing.B) {
	grants := generatePermissions(1000)
	m := NewManager(grants)
	var s bool
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = m.CheckPermission("perm_999", "/data/path/999", "import")
	}
	benchSink = s
	runtime.KeepAlive(m)
}

func BenchmarkCasbinRBAC_N100(b *testing.B) {
	grants := generatePermissions(100)
	e, err := newCasbinEnforcer()
	if err != nil {
		b.Fatalf("casbin init: %v", err)
	}
	if err := e.configure(grants); err != nil {
		b.Fatalf("casbin configure: %v", err)
	}
	var s bool
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = e.CheckPermission("perm_50", "/data/path/50", "import")
	}
	benchSink = s
}

func BenchmarkCasbinRBAC_N1000(b *testing.B) {
	grants := generatePermissions(1000)
	e, err := newCasbinEnforcer()
	if err != nil {
		b.Fatalf("casbin init: %v", err)
	}
	if err := e.configure(grants); err != nil {
		b.Fatalf("casbin configure: %v", err)
	}
	var s bool
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = e.CheckPermission("perm_999", "/data/path/999", "import")
	}
	benchSink = s
}

func BenchmarkCapnProtoRef_N100(b *testing.B) {
	grants := generatePermissions(100)
	r := newCapnProtoRef()
	r.setup(grants)
	var s bool
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = r.CheckPermission("perm_50", "/data/path/50", "import")
	}
	benchSink = s
}

func BenchmarkCapnProtoRef_N1000(b *testing.B) {
	grants := generatePermissions(1000)
	r := newCapnProtoRef()
	r.setup(grants)
	var s bool
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = r.CheckPermission("perm_999", "/data/path/999", "import")
	}
	benchSink = s
}

// ============================================================================
// CORRECTNESS PROOF
// ============================================================================

func TestM51_CapabilityCorrectness_SameDecisionsAsCasbin(t *testing.T) {
	grants := generatePermissions(200)

	our := NewManager(grants)
	casbinE, err := newCasbinEnforcer()
	require.NoError(t, err)
	require.NoError(t, casbinE.configure(grants))
	cp := newCapnProtoRef()
	cp.setup(grants)

	actions := []string{"import", "export", "read"}

	type query struct{ sub, obj, act string }
	var queries []query
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("perm_%d", i)
		obj := fmt.Sprintf("/data/path/%d", i)
		for _, a := range actions {
			queries = append(queries, query{id, obj, a})
		}
		queries = append(queries, query{id, "/data/path/other", "read"})
		queries = append(queries, query{"ghost_subject", obj, "read"})
	}
	queries = append(queries, query{"perm_0", "/etc/passwd", "read"}, query{"", "", ""})

	mism := 0
	allowCount := 0
	for _, q := range queries {
		ourDec := our.CheckPermission(q.sub, q.obj, q.act)
		casDec := casbinE.CheckPermission(q.sub, q.obj, q.act)
		cnpDec := cp.CheckPermission(q.sub, q.obj, q.act)

		if ourDec {
			allowCount++
		}
		if ourDec != casDec {
			mism++
			t.Errorf("OUR vs Casbin mismatch: sub=%q obj=%q act=%q our=%v casbin=%v", q.sub, q.obj, q.act, ourDec, casDec)
		}
		if ourDec != cnpDec {
			mism++
			t.Errorf("OUR vs Cap'nProto mismatch: sub=%q obj=%q act=%q our=%v capnp=%v", q.sub, q.obj, q.act, ourDec, cnpDec)
		}
	}

	require.Zero(t, mism, "all three systems must produce identical decisions")
	require.Positive(t, allowCount, "correctness matrix must include ALLOW decisions")
	t.Logf("correctness OK: %d queries, %d ALLOW, 0 mismatches across OUR/Casbin/Cap'nProto", len(queries), allowCount)
}
