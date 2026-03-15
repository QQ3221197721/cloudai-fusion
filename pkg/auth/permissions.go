// Package auth - permissions.go provides fine-grained permission management.
// Extends the basic RBAC Permission model with API-level and data-level controls,
// resource scoping (namespace/tenant/owner), field-level access, and dynamic
// permission evaluation with conditions.
package auth

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Permission Scope Model
// ============================================================================

// PermissionScope defines the scope of a permission grant.
type PermissionScope string

const (
	ScopeGlobal    PermissionScope = "global"    // all resources
	ScopeCluster   PermissionScope = "cluster"   // within a cluster
	ScopeNamespace PermissionScope = "namespace"  // within a namespace
	ScopeTenant    PermissionScope = "tenant"     // within a tenant
	ScopeOwner     PermissionScope = "owner"      // only own resources
)

// DataAccessLevel defines the data-level access restriction.
type DataAccessLevel string

const (
	DataAccessFull      DataAccessLevel = "full"       // all fields
	DataAccessStandard  DataAccessLevel = "standard"   // non-sensitive fields
	DataAccessRestricted DataAccessLevel = "restricted" // minimal fields only
	DataAccessNone      DataAccessLevel = "none"        // no data access
)

// PermissionGrant represents a fine-grained permission assigned to a principal.
type PermissionGrant struct {
	ID             string          `json:"id"`
	PrincipalID    string          `json:"principal_id"`   // user ID or group ID
	PrincipalType  string          `json:"principal_type"` // user, group, service-account
	Permission     Permission      `json:"permission"`     // base permission (e.g., cluster:read)
	Scope          PermissionScope `json:"scope"`
	ScopeRef       string          `json:"scope_ref,omitempty"` // cluster ID, namespace, tenant ID
	DataAccess     DataAccessLevel `json:"data_access"`
	FieldFilter    *FieldFilter    `json:"field_filter,omitempty"`
	Conditions     []PermCondition `json:"conditions,omitempty"`
	Enabled        bool            `json:"enabled"`
}

// FieldFilter defines which fields of a resource are visible/editable.
type FieldFilter struct {
	AllowedFields  []string `json:"allowed_fields,omitempty"`  // whitelist (empty = all)
	DeniedFields   []string `json:"denied_fields,omitempty"`   // blacklist
	MaskedFields   []string `json:"masked_fields,omitempty"`   // shown as "***"
	ReadOnlyFields []string `json:"read_only_fields,omitempty"`
}

// PermCondition defines a runtime condition for a permission.
type PermCondition struct {
	Type  string `json:"type"`  // ip_range, time_window, mfa, resource_tag
	Key   string `json:"key"`
	Op    string `json:"op"`    // eq, neq, in, not_in, contains, cidr
	Value string `json:"value"`
}

// ============================================================================
// Permission Manager
// ============================================================================

// PermissionManager manages fine-grained permission grants and evaluation.
type PermissionManager struct {
	grants  []*PermissionGrant
	logger  *logrus.Logger
	mu      sync.RWMutex
}

// PermissionManagerConfig configures the permission manager.
type PermissionManagerConfig struct {
	Grants []*PermissionGrant
	Logger *logrus.Logger
}

// NewPermissionManager creates a new fine-grained permission manager.
func NewPermissionManager(cfg PermissionManagerConfig) *PermissionManager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	pm := &PermissionManager{
		grants: cfg.Grants,
		logger: cfg.Logger,
	}
	if pm.grants == nil {
		pm.grants = DefaultPermissionGrants()
	}
	return pm
}

// AddGrant adds a permission grant.
func (pm *PermissionManager) AddGrant(grant *PermissionGrant) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.grants = append(pm.grants, grant)
	pm.logger.WithFields(logrus.Fields{
		"principal": grant.PrincipalID,
		"perm":      grant.Permission,
		"scope":     grant.Scope,
	}).Debug("Permission grant added")
}

// RemoveGrant removes a permission grant by ID.
func (pm *PermissionManager) RemoveGrant(grantID string) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i, g := range pm.grants {
		if g.ID == grantID {
			pm.grants = append(pm.grants[:i], pm.grants[i+1:]...)
			return true
		}
	}
	return false
}

// ListGrants returns all grants for a principal.
func (pm *PermissionManager) ListGrants(principalID string) []*PermissionGrant {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var result []*PermissionGrant
	for _, g := range pm.grants {
		if g.PrincipalID == principalID && g.Enabled {
			result = append(result, g)
		}
	}
	return result
}

// ============================================================================
// Permission Check
// ============================================================================

// PermissionCheckRequest holds the context for a permission evaluation.
type PermissionCheckRequest struct {
	UserID       string
	Role         Role
	Permission   Permission
	ResourceType string
	ResourceID   string
	Namespace    string
	TenantID     string
	OwnerID      string
	ClientIP     string
	MFAVerified  bool
	ResourceTags map[string]string
}

// PermissionCheckResult is the outcome of a permission evaluation.
type PermissionCheckResult struct {
	Allowed     bool            `json:"allowed"`
	DataAccess  DataAccessLevel `json:"data_access"`
	FieldFilter *FieldFilter    `json:"field_filter,omitempty"`
	GrantID     string          `json:"grant_id,omitempty"`
	Reason      string          `json:"reason"`
}

// CheckPermission evaluates whether a principal has a specific permission
// considering scope, data access level, field filters, and conditions.
func (pm *PermissionManager) CheckPermission(req PermissionCheckRequest) PermissionCheckResult {
	// First: check basic RBAC (fallback)
	if HasPermission(req.Role, req.Permission) {
		// RBAC allows — look for fine-grained grants to refine data access
		result := PermissionCheckResult{
			Allowed:    true,
			DataAccess: DataAccessFull,
			Reason:     fmt.Sprintf("allowed by RBAC role '%s'", req.Role),
		}

		// Check for restrictive grants that narrow scope
		pm.mu.RLock()
		defer pm.mu.RUnlock()

		for _, grant := range pm.grants {
			if !grant.Enabled {
				continue
			}
			if grant.PrincipalID != req.UserID && grant.PrincipalID != string(req.Role) {
				continue
			}
			if grant.Permission != req.Permission {
				continue
			}

			// Evaluate scope
			if !pm.matchScope(grant, req) {
				continue
			}

			// Evaluate conditions
			if !pm.matchConditions(grant.Conditions, req) {
				continue
			}

			// Apply most restrictive data access
			if dataAccessLevel(grant.DataAccess) > dataAccessLevel(result.DataAccess) {
				result.DataAccess = grant.DataAccess
			}

			// Apply field filter
			if grant.FieldFilter != nil {
				result.FieldFilter = mergeFieldFilters(result.FieldFilter, grant.FieldFilter)
			}

			result.GrantID = grant.ID
		}

		return result
	}

	// RBAC denies — check for explicit fine-grained grants
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, grant := range pm.grants {
		if !grant.Enabled {
			continue
		}
		if grant.PrincipalID != req.UserID {
			continue
		}
		if grant.Permission != req.Permission {
			continue
		}
		if !pm.matchScope(grant, req) {
			continue
		}
		if !pm.matchConditions(grant.Conditions, req) {
			continue
		}

		return PermissionCheckResult{
			Allowed:     true,
			DataAccess:  grant.DataAccess,
			FieldFilter: grant.FieldFilter,
			GrantID:     grant.ID,
			Reason:      fmt.Sprintf("allowed by explicit grant '%s'", grant.ID),
		}
	}

	return PermissionCheckResult{
		Allowed:    false,
		DataAccess: DataAccessNone,
		Reason:     fmt.Sprintf("no grant found for permission '%s'", req.Permission),
	}
}

// matchScope checks if the grant's scope matches the request context.
func (pm *PermissionManager) matchScope(grant *PermissionGrant, req PermissionCheckRequest) bool {
	switch grant.Scope {
	case ScopeGlobal:
		return true
	case ScopeCluster:
		return grant.ScopeRef == "" || grant.ScopeRef == req.ResourceID
	case ScopeNamespace:
		return grant.ScopeRef == "" || grant.ScopeRef == req.Namespace
	case ScopeTenant:
		return grant.ScopeRef == "" || grant.ScopeRef == req.TenantID
	case ScopeOwner:
		return req.OwnerID == req.UserID
	default:
		return false
	}
}

// matchConditions evaluates all conditions (AND logic).
func (pm *PermissionManager) matchConditions(conditions []PermCondition, req PermissionCheckRequest) bool {
	for _, cond := range conditions {
		if !pm.evalCondition(cond, req) {
			return false
		}
	}
	return true
}

// evalCondition evaluates a single condition.
func (pm *PermissionManager) evalCondition(cond PermCondition, req PermissionCheckRequest) bool {
	switch cond.Type {
	case "mfa":
		return req.MFAVerified
	case "ip_range":
		// Simplified IP prefix check
		return strings.HasPrefix(req.ClientIP, cond.Value)
	case "resource_tag":
		if req.ResourceTags == nil {
			return false
		}
		tagVal, ok := req.ResourceTags[cond.Key]
		if !ok {
			return cond.Op == "neq" || cond.Op == "not_in"
		}
		switch cond.Op {
		case "eq":
			return tagVal == cond.Value
		case "neq":
			return tagVal != cond.Value
		case "in":
			for _, v := range strings.Split(cond.Value, ",") {
				if strings.TrimSpace(v) == tagVal {
					return true
				}
			}
			return false
		case "contains":
			return strings.Contains(tagVal, cond.Value)
		}
	}
	return true // unknown conditions are permissive
}

// dataAccessLevel returns a numeric severity for a data access level.
func dataAccessLevel(level DataAccessLevel) int {
	switch level {
	case DataAccessFull:
		return 0
	case DataAccessStandard:
		return 1
	case DataAccessRestricted:
		return 2
	case DataAccessNone:
		return 3
	default:
		return 0
	}
}

// mergeFieldFilters combines two field filters (most restrictive wins).
func mergeFieldFilters(a, b *FieldFilter) *FieldFilter {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	merged := &FieldFilter{}
	// Union of denied fields
	seen := make(map[string]bool)
	for _, f := range a.DeniedFields {
		if !seen[f] {
			merged.DeniedFields = append(merged.DeniedFields, f)
			seen[f] = true
		}
	}
	for _, f := range b.DeniedFields {
		if !seen[f] {
			merged.DeniedFields = append(merged.DeniedFields, f)
			seen[f] = true
		}
	}
	// Union of masked fields
	seen = make(map[string]bool)
	for _, f := range a.MaskedFields {
		if !seen[f] {
			merged.MaskedFields = append(merged.MaskedFields, f)
			seen[f] = true
		}
	}
	for _, f := range b.MaskedFields {
		if !seen[f] {
			merged.MaskedFields = append(merged.MaskedFields, f)
			seen[f] = true
		}
	}
	// Union of read-only fields
	seen = make(map[string]bool)
	for _, f := range a.ReadOnlyFields {
		if !seen[f] {
			merged.ReadOnlyFields = append(merged.ReadOnlyFields, f)
			seen[f] = true
		}
	}
	for _, f := range b.ReadOnlyFields {
		if !seen[f] {
			merged.ReadOnlyFields = append(merged.ReadOnlyFields, f)
			seen[f] = true
		}
	}
	// Intersection of allowed fields (most restrictive)
	if len(a.AllowedFields) > 0 && len(b.AllowedFields) > 0 {
		setA := make(map[string]bool)
		for _, f := range a.AllowedFields {
			setA[f] = true
		}
		for _, f := range b.AllowedFields {
			if setA[f] {
				merged.AllowedFields = append(merged.AllowedFields, f)
			}
		}
	} else if len(a.AllowedFields) > 0 {
		merged.AllowedFields = a.AllowedFields
	} else {
		merged.AllowedFields = b.AllowedFields
	}
	return merged
}

// ============================================================================
// Data Filter (Response Filtering)
// ============================================================================

// FilterFields applies field-level access controls to a response map.
func FilterFields(data map[string]interface{}, filter *FieldFilter, dataAccess DataAccessLevel) map[string]interface{} {
	if dataAccess == DataAccessNone {
		return map[string]interface{}{"_restricted": true}
	}
	if filter == nil && dataAccess == DataAccessFull {
		return data
	}

	result := make(map[string]interface{})

	// Build denied set
	denied := make(map[string]bool)
	if filter != nil {
		for _, f := range filter.DeniedFields {
			denied[f] = true
		}
	}

	// Build masked set
	masked := make(map[string]bool)
	if filter != nil {
		for _, f := range filter.MaskedFields {
			masked[f] = true
		}
	}

	// Build allowed set
	allowed := make(map[string]bool)
	if filter != nil && len(filter.AllowedFields) > 0 {
		for _, f := range filter.AllowedFields {
			allowed[f] = true
		}
	}

	for key, val := range data {
		// Check denied
		if denied[key] {
			continue
		}

		// Check allowed whitelist
		if len(allowed) > 0 && !allowed[key] {
			continue
		}

		// Apply data access level filtering
		if dataAccess == DataAccessRestricted {
			// Only show non-sensitive fields
			if isSensitiveField(key) {
				continue
			}
		}

		// Apply masking
		if masked[key] {
			result[key] = "***"
			continue
		}

		result[key] = val
	}

	return result
}

// isSensitiveField returns true for commonly sensitive field names.
func isSensitiveField(field string) bool {
	lower := strings.ToLower(field)
	sensitivePatterns := []string{
		"password", "secret", "token", "key", "credential",
		"ssn", "credit_card", "bank_account", "private",
		"api_key", "access_key", "secret_key",
	}
	for _, p := range sensitivePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// ============================================================================
// Gin Middleware
// ============================================================================

// FineGrainedPermission returns a Gin middleware that enforces fine-grained
// permission checks including scope and data access level.
func FineGrainedPermission(pm *PermissionManager, perm Permission, resourceType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		roleStr, _ := c.Get("role")

		req := PermissionCheckRequest{
			UserID:       fmt.Sprintf("%v", userID),
			Role:         Role(fmt.Sprintf("%v", roleStr)),
			Permission:   perm,
			ResourceType: resourceType,
			ResourceID:   c.Param("id"),
			Namespace:    c.Query("namespace"),
			TenantID:     c.GetHeader("X-Tenant-ID"),
			ClientIP:     c.ClientIP(),
		}

		// Check MFA
		if mfa, exists := c.Get("mfa_verified"); exists {
			req.MFAVerified, _ = mfa.(bool)
		}

		result := pm.CheckPermission(req)
		if !result.Allowed {
			c.AbortWithStatusJSON(403, gin.H{
				"code":    403,
				"message": result.Reason,
			})
			return
		}

		// Set data access context for response filtering
		c.Set("data_access", string(result.DataAccess))
		if result.FieldFilter != nil {
			c.Set("field_filter", result.FieldFilter)
		}

		c.Next()
	}
}

// ============================================================================
// Default Permission Grants
// ============================================================================

// DefaultPermissionGrants returns sensible default grants that extend RBAC.
func DefaultPermissionGrants() []*PermissionGrant {
	return []*PermissionGrant{
		{
			ID: "grant-admin-global", PrincipalID: string(RoleAdmin),
			PrincipalType: "role", Permission: PermSecurityManage,
			Scope: ScopeGlobal, DataAccess: DataAccessFull, Enabled: true,
		},
		{
			ID: "grant-operator-cluster", PrincipalID: string(RoleOperator),
			PrincipalType: "role", Permission: PermClusterRead,
			Scope: ScopeCluster, DataAccess: DataAccessStandard, Enabled: true,
			FieldFilter: &FieldFilter{
				MaskedFields: []string{"kubeconfig", "credentials", "api_key"},
			},
		},
		{
			ID: "grant-developer-ns", PrincipalID: string(RoleDeveloper),
			PrincipalType: "role", Permission: PermWorkloadRead,
			Scope: ScopeNamespace, DataAccess: DataAccessStandard, Enabled: true,
			FieldFilter: &FieldFilter{
				DeniedFields: []string{"secrets", "service_account_token"},
			},
		},
		{
			ID: "grant-viewer-restricted", PrincipalID: string(RoleViewer),
			PrincipalType: "role", Permission: PermClusterRead,
			Scope: ScopeGlobal, DataAccess: DataAccessRestricted, Enabled: true,
			FieldFilter: &FieldFilter{
				AllowedFields: []string{"id", "name", "status", "provider", "region", "created_at"},
				MaskedFields:  []string{"kubeconfig"},
			},
		},
		{
			ID: "grant-owner-full", PrincipalID: "*",
			PrincipalType: "user", Permission: PermWorkloadUpdate,
			Scope: ScopeOwner, DataAccess: DataAccessFull, Enabled: true,
		},
	}
}
