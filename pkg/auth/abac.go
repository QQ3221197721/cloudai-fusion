// Package auth - abac.go implements Attribute-Based Access Control (ABAC).
// Extends the existing RBAC model with fine-grained attribute policies that evaluate
// subject attributes (user, role, department, clearance), resource attributes
// (type, owner, sensitivity), action attributes, and environment conditions
// (time, IP range, MFA status).
package auth

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// ABAC Policy Model
// ============================================================================

// ABACPolicy defines an attribute-based access control policy.
type ABACPolicy struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Priority    int             `json:"priority"` // higher = evaluated first
	Effect      PolicyEffect    `json:"effect"`   // allow or deny
	Subject     SubjectMatch    `json:"subject"`
	Resource    ResourceMatch   `json:"resource"`
	Action      ActionMatch     `json:"action"`
	Environment EnvironmentMatch `json:"environment,omitempty"`
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
}

// PolicyEffect is the outcome of a matched policy.
type PolicyEffect string

const (
	EffectAllow PolicyEffect = "allow"
	EffectDeny  PolicyEffect = "deny"
)

// SubjectMatch defines the subject attributes to match.
type SubjectMatch struct {
	Roles       []string          `json:"roles,omitempty"`        // any of these roles
	UserIDs     []string          `json:"user_ids,omitempty"`     // specific user IDs
	Departments []string          `json:"departments,omitempty"`  // organizational units
	Tags        map[string]string `json:"tags,omitempty"`         // arbitrary key-value attributes
	MFARequired bool              `json:"mfa_required,omitempty"` // must have completed MFA
}

// ResourceMatch defines the resource attributes to match.
type ResourceMatch struct {
	Types       []string          `json:"types,omitempty"`       // e.g., "cluster", "workload"
	IDs         []string          `json:"ids,omitempty"`         // specific resource IDs
	Namespaces  []string          `json:"namespaces,omitempty"`  // K8s namespaces
	Owners      []string          `json:"owners,omitempty"`      // resource owner user IDs
	Sensitivity []string          `json:"sensitivity,omitempty"` // public, internal, confidential, restricted
	Tags        map[string]string `json:"tags,omitempty"`
}

// ActionMatch defines the actions to match.
type ActionMatch struct {
	Operations []string `json:"operations,omitempty"` // create, read, update, delete, list, exec
}

// EnvironmentMatch defines environmental conditions.
type EnvironmentMatch struct {
	TimeWindows []TimeWindow `json:"time_windows,omitempty"` // allowed time ranges
	IPRanges    []string     `json:"ip_ranges,omitempty"`    // CIDR notation
	SourceTypes []string     `json:"source_types,omitempty"` // api, console, ci-cd, sdk
}

// TimeWindow represents an allowed time range.
type TimeWindow struct {
	Weekdays []time.Weekday `json:"weekdays,omitempty"` // empty = all days
	StartHour int           `json:"start_hour"`         // 0-23 UTC
	EndHour   int           `json:"end_hour"`           // 0-23 UTC
}

// ============================================================================
// ABAC Request Context
// ============================================================================

// ABACRequest encapsulates the attributes of an access request.
type ABACRequest struct {
	// Subject attributes
	UserID     string
	Username   string
	Role       Role
	Department string
	Tags       map[string]string
	MFADone    bool

	// Resource attributes
	ResourceType string
	ResourceID   string
	Namespace    string
	Owner        string
	Sensitivity  string
	ResourceTags map[string]string

	// Action
	Operation string // create, read, update, delete, list, exec

	// Environment
	ClientIP   string
	SourceType string // api, console, ci-cd
	RequestTime time.Time
}

// ABACDecision is the result of policy evaluation.
type ABACDecision struct {
	Allowed   bool         `json:"allowed"`
	Effect    PolicyEffect `json:"effect"`
	PolicyID  string       `json:"policy_id,omitempty"`
	Reason    string       `json:"reason"`
}

// ============================================================================
// ABAC Engine
// ============================================================================

// ABACEngine evaluates attribute-based access control policies.
type ABACEngine struct {
	policies []*ABACPolicy
	logger   *logrus.Logger
	mu       sync.RWMutex
}

// ABACConfig configures the ABAC engine.
type ABACConfig struct {
	Policies []*ABACPolicy
	Logger   *logrus.Logger
}

// NewABACEngine creates a new ABAC engine with the given policies.
func NewABACEngine(cfg ABACConfig) *ABACEngine {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	engine := &ABACEngine{
		policies: cfg.Policies,
		logger:   cfg.Logger,
	}
	if engine.policies == nil {
		engine.policies = DefaultABACPolicies()
	}
	return engine
}

// Evaluate runs all matching policies against the request.
// Uses deny-override: if any matching policy denies, the request is denied.
// If no policy matches, access is denied by default (closed-world assumption).
func (e *ABACEngine) Evaluate(req ABACRequest) ABACDecision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if req.RequestTime.IsZero() {
		req.RequestTime = time.Now().UTC()
	}

	var matchedAllow *ABACPolicy

	// Evaluate policies in priority order (higher priority first)
	for _, policy := range e.policies {
		if !policy.Enabled {
			continue
		}
		if !e.matchPolicy(policy, req) {
			continue
		}

		// Deny-override: any deny immediately rejects
		if policy.Effect == EffectDeny {
			return ABACDecision{
				Allowed:  false,
				Effect:   EffectDeny,
				PolicyID: policy.ID,
				Reason:   fmt.Sprintf("denied by policy '%s': %s", policy.Name, policy.Description),
			}
		}

		if matchedAllow == nil {
			matchedAllow = policy
		}
	}

	if matchedAllow != nil {
		return ABACDecision{
			Allowed:  true,
			Effect:   EffectAllow,
			PolicyID: matchedAllow.ID,
			Reason:   fmt.Sprintf("allowed by policy '%s'", matchedAllow.Name),
		}
	}

	// Default deny
	return ABACDecision{
		Allowed: false,
		Effect:  EffectDeny,
		Reason:  "no matching policy found (default deny)",
	}
}

// AddPolicy adds a new ABAC policy.
func (e *ABACEngine) AddPolicy(policy *ABACPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policies = append(e.policies, policy)
	// Re-sort by priority descending
	e.sortPolicies()
}

// RemovePolicy removes a policy by ID.
func (e *ABACEngine) RemovePolicy(policyID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, p := range e.policies {
		if p.ID == policyID {
			e.policies = append(e.policies[:i], e.policies[i+1:]...)
			return true
		}
	}
	return false
}

// ListPolicies returns all policies.
func (e *ABACEngine) ListPolicies() []*ABACPolicy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*ABACPolicy, len(e.policies))
	copy(result, e.policies)
	return result
}

func (e *ABACEngine) sortPolicies() {
	// Insertion sort by priority descending
	for i := 1; i < len(e.policies); i++ {
		key := e.policies[i]
		j := i - 1
		for j >= 0 && e.policies[j].Priority < key.Priority {
			e.policies[j+1] = e.policies[j]
			j--
		}
		e.policies[j+1] = key
	}
}

// ============================================================================
// Policy Matching
// ============================================================================

func (e *ABACEngine) matchPolicy(policy *ABACPolicy, req ABACRequest) bool {
	return e.matchSubject(policy.Subject, req) &&
		e.matchResource(policy.Resource, req) &&
		e.matchAction(policy.Action, req) &&
		e.matchEnvironment(policy.Environment, req)
}

func (e *ABACEngine) matchSubject(m SubjectMatch, req ABACRequest) bool {
	// Roles
	if len(m.Roles) > 0 && !containsString(m.Roles, string(req.Role)) {
		return false
	}
	// User IDs
	if len(m.UserIDs) > 0 && !containsString(m.UserIDs, req.UserID) {
		return false
	}
	// Departments
	if len(m.Departments) > 0 && !containsString(m.Departments, req.Department) {
		return false
	}
	// MFA
	if m.MFARequired && !req.MFADone {
		return false
	}
	// Tags (all must match)
	for k, v := range m.Tags {
		if req.Tags[k] != v {
			return false
		}
	}
	return true
}

func (e *ABACEngine) matchResource(m ResourceMatch, req ABACRequest) bool {
	if len(m.Types) > 0 && !containsString(m.Types, req.ResourceType) {
		return false
	}
	if len(m.IDs) > 0 && !containsString(m.IDs, req.ResourceID) {
		return false
	}
	if len(m.Namespaces) > 0 && !containsString(m.Namespaces, req.Namespace) {
		return false
	}
	if len(m.Owners) > 0 && !containsString(m.Owners, req.Owner) {
		return false
	}
	if len(m.Sensitivity) > 0 && !containsString(m.Sensitivity, req.Sensitivity) {
		return false
	}
	for k, v := range m.Tags {
		if req.ResourceTags[k] != v {
			return false
		}
	}
	return true
}

func (e *ABACEngine) matchAction(m ActionMatch, req ABACRequest) bool {
	if len(m.Operations) == 0 {
		return true // no constraint
	}
	return containsString(m.Operations, req.Operation)
}

func (e *ABACEngine) matchEnvironment(m EnvironmentMatch, req ABACRequest) bool {
	// Time window check
	if len(m.TimeWindows) > 0 {
		inWindow := false
		for _, tw := range m.TimeWindows {
			if matchTimeWindow(tw, req.RequestTime) {
				inWindow = true
				break
			}
		}
		if !inWindow {
			return false
		}
	}

	// IP range check
	if len(m.IPRanges) > 0 && req.ClientIP != "" {
		inRange := false
		ip := net.ParseIP(req.ClientIP)
		if ip != nil {
			for _, cidr := range m.IPRanges {
				_, network, err := net.ParseCIDR(cidr)
				if err == nil && network.Contains(ip) {
					inRange = true
					break
				}
			}
		}
		if !inRange {
			return false
		}
	}

	// Source type check
	if len(m.SourceTypes) > 0 && !containsString(m.SourceTypes, req.SourceType) {
		return false
	}

	return true
}

func matchTimeWindow(tw TimeWindow, t time.Time) bool {
	if len(tw.Weekdays) > 0 {
		dayMatch := false
		for _, d := range tw.Weekdays {
			if t.Weekday() == d {
				dayMatch = true
				break
			}
		}
		if !dayMatch {
			return false
		}
	}
	hour := t.Hour()
	if tw.StartHour <= tw.EndHour {
		return hour >= tw.StartHour && hour < tw.EndHour
	}
	// Overnight window (e.g., 22:00-06:00)
	return hour >= tw.StartHour || hour < tw.EndHour
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

// ============================================================================
// Gin Middleware
// ============================================================================

// ABACMiddleware returns a Gin middleware that enforces ABAC policies.
// Requires the auth middleware to have run first (populates user context).
func ABACMiddleware(engine *ABACEngine, resourceType, operation string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		username, _ := c.Get("username")
		roleStr, _ := c.Get("role")

		req := ABACRequest{
			UserID:       fmt.Sprintf("%v", userID),
			Username:     fmt.Sprintf("%v", username),
			Role:         Role(fmt.Sprintf("%v", roleStr)),
			ResourceType: resourceType,
			Operation:    operation,
			ClientIP:     c.ClientIP(),
			SourceType:   "api",
			RequestTime:  time.Now().UTC(),
		}

		// Extract resource ID from path params
		if id := c.Param("id"); id != "" {
			req.ResourceID = id
		}
		if ns := c.Param("namespace"); ns != "" {
			req.Namespace = ns
		}

		decision := engine.Evaluate(req)
		if !decision.Allowed {
			c.AbortWithStatusJSON(403, gin.H{
				"code":    403,
				"message": "ABAC policy denied access",
				"reason":  decision.Reason,
				"policy":  decision.PolicyID,
			})
			return
		}

		c.Set("abac_decision", decision)
		c.Next()
	}
}

// ============================================================================
// Default Policies
// ============================================================================

// DefaultABACPolicies returns a sensible set of default ABAC policies.
func DefaultABACPolicies() []*ABACPolicy {
	now := time.Now().UTC()
	return []*ABACPolicy{
		{
			ID: "abac-admin-full", Name: "Admin Full Access",
			Description: "Admins have unrestricted access to all resources",
			Priority: 1000, Effect: EffectAllow, Enabled: true,
			Subject: SubjectMatch{Roles: []string{"admin"}},
			Resource: ResourceMatch{},
			Action:   ActionMatch{},
			CreatedAt: now,
		},
		{
			ID: "abac-deny-restricted-no-mfa", Name: "Deny Restricted Without MFA",
			Description: "Restricted resources require multi-factor authentication",
			Priority: 900, Effect: EffectDeny, Enabled: true,
			Subject:  SubjectMatch{MFARequired: true},
			Resource: ResourceMatch{Sensitivity: []string{"restricted"}},
			Action:   ActionMatch{},
			CreatedAt: now,
		},
		{
			ID: "abac-operator-crud", Name: "Operator CRUD on Clusters/Workloads",
			Description: "Operators can manage clusters and workloads",
			Priority: 500, Effect: EffectAllow, Enabled: true,
			Subject:  SubjectMatch{Roles: []string{"operator"}},
			Resource: ResourceMatch{Types: []string{"cluster", "workload"}},
			Action:   ActionMatch{Operations: []string{"create", "read", "update", "delete", "list"}},
			CreatedAt: now,
		},
		{
			ID: "abac-developer-read-write", Name: "Developer Read/Write Workloads",
			Description: "Developers can create and read workloads",
			Priority: 400, Effect: EffectAllow, Enabled: true,
			Subject:  SubjectMatch{Roles: []string{"developer"}},
			Resource: ResourceMatch{Types: []string{"workload"}},
			Action:   ActionMatch{Operations: []string{"create", "read", "update", "list"}},
			CreatedAt: now,
		},
		{
			ID: "abac-viewer-readonly", Name: "Viewer Read-Only",
			Description: "Viewers have read-only access to non-restricted resources",
			Priority: 300, Effect: EffectAllow, Enabled: true,
			Subject:  SubjectMatch{Roles: []string{"viewer"}},
			Resource: ResourceMatch{Sensitivity: []string{"public", "internal"}},
			Action:   ActionMatch{Operations: []string{"read", "list"}},
			CreatedAt: now,
		},
		{
			ID: "abac-deny-outside-hours", Name: "Deny Writes Outside Business Hours",
			Description: "Non-admin write operations restricted to business hours (UTC)",
			Priority: 800, Effect: EffectDeny, Enabled: false, // disabled by default
			Subject:  SubjectMatch{Roles: []string{"operator", "developer"}},
			Resource: ResourceMatch{},
			Action:   ActionMatch{Operations: []string{"create", "update", "delete"}},
			Environment: EnvironmentMatch{
				TimeWindows: []TimeWindow{
					{StartHour: 22, EndHour: 6}, // deny 22:00-06:00 UTC
				},
			},
			CreatedAt: now,
		},
	}
}
