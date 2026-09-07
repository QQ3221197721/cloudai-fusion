// Package rbac implements Role-Based Access Control with fine-grained permissions
package rbac

import (
	"sync"
	"github.com/sirupsen/logrus"
)

// PermissionSet defines granular permissions for resources and actions
type PermissionSet map[string]bool

// Role represents a role with its associated permissions
type Role struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Permissions PermissionSet `json:"permissions"`
	IsBuiltIn   bool          `json:"is_builtin"`
	CreatedAt   string        `json:"created_at"`
}

// UserRoleMapping maps users to their assigned roles
type UserRoleMapping struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
}

// Manager handles all RBAC operations
type Manager struct {
	mu               sync.RWMutex
	roles            map[string]*Role
	userRoles        map[string][]string
	permissionsIndex map[string][]string // permission -> role_ids
	logger           *logrus.Logger
}

// NewRBACManager creates a new RBAC manager with built-in roles
func NewRBACManager() *Manager {
	m := &Manager{
		roles:            make(map[string]*Role),
		userRoles:        make(map[string][]string),
		permissionsIndex: make(map[string][]string),
		logger:           logrus.StandardLogger(),
	}
	
	m.registerBuiltInRoles()
	return m
}

// registerBuiltInRoles initializes platform roles
func (m *Manager) registerBuiltInRoles() {
	m.RegisterRole(&Role{
		ID:          "platform-admin",
		Name:        "Platform Administrator",
		Description: "Full access to all system functions",
		Permissions: m.createAdminPermissions(),
		IsBuiltIn:   true,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	
	m.RegisterRole(&Role{
		ID:          "ml-engineer",
		Name:        "ML Engineer",
		Description: "Manage workloads, clusters, and GPU resources",
		Permissions: m.createMLEngineerPermissions(),
		IsBuiltIn:   true,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	
	m.RegisterRole(&Role{
		ID:          "security-analyst",
		Name:        "Security Analyst",
		Description: "Security monitoring, threat hunting, compliance",
		Permissions: m.createSecurityAnalystPermissions(),
		IsBuiltIn:   true,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	
	m.RegisterRole(&Role{
		ID:          "auditor",
		Name:        "Auditor",
		Description: "Read-only access to audit logs and evidence",
		Permissions: m.createAuditorPermissions(),
		IsBuiltIn:   true,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	
	m.RegisterRole(&Role{
		ID:          "cost-manager",
		Name:        "Cost Manager",
		Description: "Financial management and showback reports",
		Permissions: m.createCostManagerPermissions(),
		IsBuiltIn:   true,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	
	m.RegisterRole(&Role{
		ID:          "viewer",
		Name:        "Viewer",
		Description: "Basic read-only access to resources",
		Permissions: m.createViewerPermissions(),
		IsBuiltIn:   true,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	
	m.logger.Info("Registered 6 built-in roles")
}

func (m *Manager) createAdminPermissions() PermissionSet {
	return PermissionSet{
		"clusters:list": true, "clusters:create": true, "clusters:update": true, "clusters:delete": true,
		"workloads:list": true, "workloads:create": true, "workloads:stop": true,
		"security:view": true, "security:scan": true, "security:policies:manage": true,
		"edge:nodes:manage": true, "edge:deploy": true, "edge:models:manage": true,
		"rbac:manage": true, "tenants:manage": true, "billing:manage": true,
		"evidence:view": true, "evidence:export": true,
		"support:tickets:manage": true, "admin:all": true,
	}
}

func (m *Manager) createMLEngineerPermissions() PermissionSet {
	return PermissionSet{
		"clusters:list": true, "clusters:health_check": true,
		"workloads:list": true, "workloads:create": true, "workloads:stop": true, "workloads:logs": true,
		"scheduling:optimize": true,
		"gpu:topology:view": true,
		"cost:view": true,
	}
}

func (m *Manager) createSecurityAnalystPermissions() PermissionSet {
	return PermissionSet{
		"security:view": true, "security:scan": true, "security:policies:view": true,
		"soc:findings:view": true, "soc:playbooks:run": true,
		"hunt:run": true, "intel:sync": true,
		"threat_intel:ingest": true, "compliance:view": true,
		"audit:logs:view": true,
	}
}

func (m *Manager) createAuditorPermissions() PermissionSet {
	return PermissionSet{
		"clusters:list": true, "clusters:details": true,
		"workloads:list": true,
		"evidence:ledger:view": true, "evidence:records:view": true,
		"audit:logs:view": true, "audit:logs:export": true,
		"compliance:reports:view": true,
	}
}

func (m *Manager) createCostManagerPermissions() PermissionSet {
	return PermissionSet{
		"cost:view": true, "cost:analyze": true, "cost:recommendations:view": true,
		"showback:reports:view": true, "billing:invoices:view": true,
		"tenants:usage:view": true,
	}
}

func (m *Manager) createViewerPermissions() PermissionSet {
	return PermissionSet{
		"clusters:list": true, "clusters:details": true,
		"workloads:list": true,
		"models:view": true,
	}
}

// RegisterRole adds a new role to the system
func (m *Manager) RegisterRole(role *Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, exists := m.roles[role.ID]; exists {
		return fmt.Errorf("role %s already exists", role.ID)
	}
	
	m.roles[role.ID] = role
	
	// Index permissions for quick lookup
	for perm := range role.Permissions {
		m.permissionsIndex[perm] = append(m.permissionsIndex[perm], role.ID)
	}
	
	m.logger.WithField("role_id", role.ID).Info("Role registered")
	return nil
}

// GetRole returns a role by ID
func (m *Manager) GetRole(roleID string) (*Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	role, ok := m.roles[roleID]
	if !ok {
		return nil, fmt.Errorf("role %s not found", roleID)
	}
	
	return role, nil
}

// ListRoles returns all roles
func (m *Manager) ListRoles() []*Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	roles := make([]*Role, 0, len(m.roles))
	for _, role := range m.roles {
		roles = append(roles, role)
	}
	return roles
}

// AssignRoles assigns one or more roles to a user
func (m *Manager) AssignRoles(userID string, roleIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Validate all role IDs exist
	for _, roleID := range roleIDs {
		if _, ok := m.roles[roleID]; !ok {
			return fmt.Errorf("role %s does not exist", roleID)
		}
	}
	
	m.userRoles[userID] = roleIDs
	m.logger.WithFields(logrus.Fields{
		"user_id": userID,
		"roles":   roleIDs,
	}).Info("Roles assigned to user")
	
	return nil
}

// GetUserRoles returns roles assigned to a user
func (m *Manager) GetUserRoles(userID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return m.userRoles[userID]
}

// Can checks if a user has a specific permission
func (m *Manager) Can(userID, permission string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	roles := m.userRoles[userID]
	if len(roles) == 0 {
		return false
	}
	
	// Check all roles for permission
	for _, roleID := range roles {
		if role, ok := m.roles[roleID]; ok {
			if role.Permissions[permission] {
				return true
			}
		}
	}
	
	return false
}

// HasPermission checks if a role has a specific permission
func (m *Manager) HasPermission(roleID, permission string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	role, ok := m.roles[roleID]
	if !ok {
		return false
	}
	
	return role.Permissions[permission]
}

// RemoveRole removes a role from a user
func (m *Manager) RemoveRole(userID, roleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	roles, ok := m.userRoles[userID]
	if !ok {
		return fmt.Errorf("user %s has no roles assigned", userID)
	}
	
	// Remove role from list
	newRoles := make([]string, 0, len(roles)-1)
	for _, r := range roles {
		if r != roleID {
			newRoles = append(newRoles, r)
		}
	}
	
	m.userRoles[userID] = newRoles
	return nil
}

// DeleteRole permanently deletes a role
func (m *Manager) DeleteRole(roleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if _, ok := m.roles[roleID]; !ok {
		return fmt.Errorf("role %s does not exist", roleID)
	}
	
	if m.roles[roleID].IsBuiltIn {
		return fmt.Errorf("cannot delete built-in role")
	}
	
	delete(m.roles, roleID)
	
	// Clean up index
	for perm := range m.permissionsIndex {
		newRoles := make([]string, 0, len(m.permissionsIndex[perm])-1)
		for _, r := range m.permissionsIndex[perm] {
			if r != roleID {
				newRoles = append(newRoles, r)
			}
		}
		m.permissionsIndex[perm] = newRoles
	}
	
	m.logger.WithField("role_id", roleID).Info("Role deleted")
	return nil
}
