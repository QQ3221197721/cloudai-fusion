package auth

import "testing"

func TestCompiledRBAC_DirectGrants(t *testing.T) {
	rb := NewCompiledRBACBuilder().
		Grant("admin", "cluster:create").
		Grant("viewer", "cluster:read").
		Compile()

	if !rb.Enforce("admin", "cluster:create") {
		t.Error("admin should have cluster:create")
	}
	if rb.Enforce("viewer", "cluster:create") {
		t.Error("viewer must not have cluster:create")
	}
	if rb.Enforce("nobody", "cluster:read") {
		t.Error("unknown role must be denied")
	}
}

func TestCompiledRBAC_TransitiveInheritance(t *testing.T) {
	// viewer <- developer <- operator <- admin
	rb := NewCompiledRBACBuilder().
		Grant("viewer", "cluster:read").
		Grant("developer", "workload:create").
		Grant("operator", "cluster:update").
		Grant("admin", "cluster:delete").
		Inherit("developer", "viewer").
		Inherit("operator", "developer").
		Inherit("admin", "operator").
		Compile()

	// admin should inherit everything down the chain
	for _, perm := range []string{"cluster:read", "workload:create", "cluster:update", "cluster:delete"} {
		if !rb.Enforce("admin", perm) {
			t.Errorf("admin should inherit %q through the chain", perm)
		}
	}
	// developer should have its own + viewer's, but not operator's
	if !rb.Enforce("developer", "cluster:read") {
		t.Error("developer should inherit viewer's cluster:read")
	}
	if rb.Enforce("developer", "cluster:update") {
		t.Error("developer must not have operator's cluster:update")
	}
}

func TestCompiledRBAC_CycleSafe(t *testing.T) {
	// a <-> b cycle plus a grant on each; Compile must terminate.
	rb := NewCompiledRBACBuilder().
		Grant("a", "perm:a").
		Grant("b", "perm:b").
		Inherit("a", "b").
		Inherit("b", "a").
		Compile()

	if !rb.Enforce("a", "perm:b") || !rb.Enforce("b", "perm:a") {
		t.Error("cyclic inheritance should union both roles' permissions")
	}
}

func TestCompiledRBAC_Counts(t *testing.T) {
	rb := NewCompiledRBACBuilder().
		Grant("admin", "p1").
		Grant("admin", "p2").
		Grant("viewer", "p1").
		Compile()

	if rb.GrantCount() != 3 {
		t.Errorf("GrantCount = %d, want 3", rb.GrantCount())
	}
	if rb.RoleCount() != 2 {
		t.Errorf("RoleCount = %d, want 2", rb.RoleCount())
	}
	if rb.Permissions("admin") != 2 {
		t.Errorf("admin effective perms = %d, want 2", rb.Permissions("admin"))
	}
}

// TestCompiledRBAC_MatchesHasPermission verifies the compiled engine agrees with
// the built-in RBAC table for the platform's default roles/permissions.
func TestCompiledRBAC_MatchesHasPermission(t *testing.T) {
	rb := NewCompiledRBACBuilder()
	for role, perms := range rolePermissions {
		for _, p := range perms {
			rb.Grant(string(role), string(p))
		}
	}
	compiled := rb.Compile()

	allPerms := []Permission{
		PermClusterCreate, PermClusterRead, PermClusterUpdate, PermClusterDelete,
		PermWorkloadCreate, PermWorkloadRead, PermWorkloadUpdate, PermWorkloadDelete,
		PermSecurityManage, PermSecurityRead, PermUserManage, PermUserRead,
		PermProviderManage, PermProviderRead, PermMonitorRead, PermMonitorManage,
		PermCostRead, PermCostManage, PermAgentManage, PermAgentRead,
	}
	roles := []Role{RoleAdmin, RoleOperator, RoleDeveloper, RoleViewer}
	for _, r := range roles {
		for _, p := range allPerms {
			want := HasPermission(r, p)
			got := compiled.Enforce(string(r), string(p))
			if want != got {
				t.Errorf("mismatch role=%s perm=%s: HasPermission=%v CompiledRBAC=%v", r, p, want, got)
			}
		}
	}
}
