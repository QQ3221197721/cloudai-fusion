// Package auth - rbac_engine.go provides a high-performance, compiled RBAC
// authorizer with role inheritance.
//
// Design rationale (performance moat):
//   - Traditional RBAC engines (e.g. Casbin) resolve role inheritance at
//     enforcement time by walking the role graph (a BFS/DFS per Enforce call),
//     so decision latency grows with both policy-set size and inheritance-chain
//     depth, and each call allocates temporary structures.
//   - CompiledRBAC instead MATERIALIZES the transitive closure of role
//     inheritance once at build time. Every role's *effective* permission set
//     (own + all inherited) is flattened into a hash set. Enforcement is then a
//     two-level map lookup: O(1), independent of policy-set size and inheritance
//     depth, and allocation-free on the hot path.
//
// The trade-off is documented in docs/performance-validation-auth.md: build
// (compile) cost is paid once and amortized over the lifetime of the ruleset;
// inheritance edges that change require a recompile. For authorization — a
// read-heavy, write-rare workload — this is the correct optimization point.
package auth

// RBACGrant is a single (role -> permission) authorization rule.
type RBACGrant struct {
	Role       string
	Permission string
}

// RoleInheritance is a (child -> parent) edge: the child role inherits every
// permission granted to the parent role, transitively.
type RoleInheritance struct {
	Child  string
	Parent string
}

// CompiledRBAC is an immutable, high-performance RBAC authorizer. It holds the
// fully materialized transitive closure of role inheritance so that Enforce is
// an O(1), zero-allocation decision regardless of ruleset size or chain depth.
type CompiledRBAC struct {
	// effective maps a role to the set of every permission it holds, including
	// permissions inherited from ancestor roles.
	effective map[string]map[string]struct{}
	// roleCount / grantCount are retained for introspection and metrics.
	roleCount  int
	grantCount int
}

// CompiledRBACBuilder accumulates grants and inheritance edges prior to Compile.
// It is not safe for concurrent use; build on one goroutine, then share the
// resulting immutable *CompiledRBAC freely across goroutines.
type CompiledRBACBuilder struct {
	directGrants map[string]map[string]struct{}
	parents      map[string]map[string]struct{}
	roles        map[string]struct{}
	grantCount   int
}

// NewCompiledRBACBuilder returns an empty builder.
func NewCompiledRBACBuilder() *CompiledRBACBuilder {
	return &CompiledRBACBuilder{
		directGrants: make(map[string]map[string]struct{}),
		parents:      make(map[string]map[string]struct{}),
		roles:        make(map[string]struct{}),
	}
}

// Grant records that role directly holds permission.
func (b *CompiledRBACBuilder) Grant(role, permission string) *CompiledRBACBuilder {
	b.roles[role] = struct{}{}
	set, ok := b.directGrants[role]
	if !ok {
		set = make(map[string]struct{})
		b.directGrants[role] = set
	}
	if _, exists := set[permission]; !exists {
		set[permission] = struct{}{}
		b.grantCount++
	}
	return b
}

// Inherit records that child inherits every permission of parent (transitively).
func (b *CompiledRBACBuilder) Inherit(child, parent string) *CompiledRBACBuilder {
	b.roles[child] = struct{}{}
	b.roles[parent] = struct{}{}
	set, ok := b.parents[child]
	if !ok {
		set = make(map[string]struct{})
		b.parents[child] = set
	}
	set[parent] = struct{}{}
	return b
}

// Compile materializes the transitive closure of role inheritance and returns
// an immutable authorizer. Inheritance cycles are handled safely (a role never
// visits itself twice), so a cyclic graph degrades to the union of the cycle's
// permissions rather than looping forever.
func (b *CompiledRBACBuilder) Compile() *CompiledRBAC {
	effective := make(map[string]map[string]struct{}, len(b.roles))

	// visited is reused across roles but reset per root to bound allocations.
	for role := range b.roles {
		acc := make(map[string]struct{})
		visited := make(map[string]struct{}, 4)
		b.collect(role, acc, visited)
		effective[role] = acc
	}

	return &CompiledRBAC{
		effective:  effective,
		roleCount:  len(b.roles),
		grantCount: b.grantCount,
	}
}

// collect performs a depth-first union of direct grants along the ancestor
// chain of role, guarding against cycles via visited.
func (b *CompiledRBACBuilder) collect(role string, acc, visited map[string]struct{}) {
	if _, seen := visited[role]; seen {
		return
	}
	visited[role] = struct{}{}

	for perm := range b.directGrants[role] {
		acc[perm] = struct{}{}
	}
	for parent := range b.parents[role] {
		b.collect(parent, acc, visited)
	}
}

// Enforce reports whether role is authorized for permission. It is the hot
// path: a two-level map lookup with no heap allocations, safe for concurrent
// use on a compiled authorizer.
func (c *CompiledRBAC) Enforce(role, permission string) bool {
	perms, ok := c.effective[role]
	if !ok {
		return false
	}
	_, ok = perms[permission]
	return ok
}

// Permissions returns the number of effective permissions for a role (own +
// inherited). Returns 0 for unknown roles. Intended for introspection/metrics.
func (c *CompiledRBAC) Permissions(role string) int {
	return len(c.effective[role])
}

// RoleCount returns the number of distinct roles known to the authorizer.
func (c *CompiledRBAC) RoleCount() int { return c.roleCount }

// GrantCount returns the number of distinct direct grants that were compiled.
func (c *CompiledRBAC) GrantCount() int { return c.grantCount }
