// Package api provides HTTP handlers for M39 Identity & Access Management
package api

import (
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// RoleType represents role category
type RoleType string

const (
	GlobalAdmin RoleType = "global_admin"
	SecurityAdmin RoleType = "security_admin"
	DevOpsAdmin RoleType = "devops_admin"
	Developer RoleType = "developer"
	Viewer RoleType = "viewer"
	Auditor RoleType = "auditor"
)

// Permission defines an access right
type Permission struct {
	Resource string   `json:"resource"`
	Action   string   `json:"action"` // read, write, delete, admin
	ObjectID *string  `json:"objectId,omitempty"` // specific resource ID
}

// UserRoleAssignment represents role to user mapping
type UserRoleAssignment struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	RoleID    string    `json:"roleId"`
	Role      RoleType  `json:"role"`
	Scopes    []string  `json:"scopes"` // project, namespace, resource scopes
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// UserAccount represents authenticated user
type UserAccount struct {
	ID           string              `json:"id"`
	Username     string              `json:"username"`
	Email        string              `json:"email"`
	FullName     string              `json:"fullName"`
	Status       string              `json:"status"` // active, inactive, locked
	Roles        []UserRoleAssignment `json:"roles"`
	Permissions  []Permission        `json:"permissions"`
	PrivilegedAccess []PrivilegedAccess `json:"privilegedAccess"`
	MFAEnabled   bool                `json:"mfaEnabled"`
	LastLoginAt  *time.Time          `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
}

// PrivilegedAccess JIT privileged access request
type PrivilegedAccess struct {
	ID             string              `json:"id"`
	RequesterID    string              `json:"requesterId"`
	TargetResource string              `json:"targetResource"`
	ResourceType   string              `json:"resourceType"` // container, k8s, database, vault
	RequestedBy    string              `json:"requestedBy"`
	RequestedAt    time.Time           `json:"requestedAt"`
	Approvers      []string            `json:"approvers"`
	ApprovalStatus string              `json:"approvalStatus"` // pending, approved, rejected
	ApprovedBy     []string            `json:"approvedBy,omitempty"`
	ApprovedAt     *time.Time          `json:"approvedAt,omitempty"`
	GrantedAt      *time.Time          `json:"grantedAt,omitempty"`
	ExpiresAt      time.Time           `json:"expiresAt"`
	Reason         string              `json:"reason"`
	AuditLog       []AuditEntry        `json:"auditLog"`
}

// AuditEntry records action in audit trail
type AuditEntry struct {
	Timestamp   time.Time   `json:"timestamp"`
	Action      string      `json:"action"`
	User        string      `json:"user"`
	Details     map[string]any `json:"details"`
	IPAddress   string      `json:"ipAddress"`
	UserAgent   string      `json:"userAgent"`
}

// AccessAuditLog comprehensive access audit record
type AccessAuditLog struct {
	ID            string        `json:"id"`
	UserID        string        `json:"userId"`
	Action        string        `json:"action"`
	Resource      string        `json:"resource"`
	AccessType    string        `json:"accessType"` // read, write, admin
	Timestamp     time.Time     `json:"timestamp"`
	IPAddress     string        `json:"ipAddress"`
	UserAgent     string        `json:"userAgent"`
	Success       bool          `json:"success"`
	ErrorMsg      string        `json:"errorMsg,omitempty"`
	Context       map[string]any `json:"context"`
}

// RBACPolicy represents role-based access control policy
type RBACPolicy struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Permissions []Permission           `json:"permissions"`
	Conditions  map[string]any         `json:"conditions"` // time-based, location-based rules
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
}

// PermissionMatrix represents complete permission matrix
type PermissionMatrix struct {
	Roles       []RoleDefinition `json:"roles"`
	Resources   []ResourceDef    `json:"resources"`
	Assignments []UserAssignment `json:"assignments"`
}

// RoleDefinition defines role permissions
type RoleDefinition struct {
	ID          string        `json:"id"`
	Name        RoleType      `json:"name"`
	Description string        `json:"description"`
	Permissions []Permission  `json:"permissions"`
	Users       []string      `json:"users"`
}

// ResourceDef defines manageable resources
type ResourceDef struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Owners      []string `json:"owners"`
	AccessList  []string `json:"accessList"`
}

// UserAssignment maps users to resources
type UserAssignment struct {
	UserID   string   `json:"userId"`
	Resource string   `json:"resource"`
	Role     string   `json:"role"`
	Grantor  string   `json:"grantor"`
	GrantedAt time.Time `json:"grantedAt"`
}

// IdentityAccessHandler handles IAM operations
type IdentityAccessHandler struct {
	store      *IdentityAccessStore
	evidence   *evidence.Ledger
	logger     *logrus.Logger
}

// NewIdentityAccessHandler creates handler instance
func NewIdentityAccessHandler(
	store *IdentityAccessStore,
	evidenceLedger *evidence.Ledger,
	logger *logrus.Logger,
) *IdentityAccessHandler {
	return &IdentityAccessHandler{
		store:      store,
		evidence:   evidenceLedger,
		logger:     logger,
	}
}

// RegisterRoutes registers REST endpoints
func (h *IdentityAccessHandler) RegisterRoutes(router *echo.Echo) {
	iam := router.Group("/api/m39/iam")

	// User management
	users := iam.Group("/users")
	users.POST("/", h.createUser)
	users.GET("/", h.listUsers)
	users.GET("/:id", h.getUser)
	users.PUT("/:id", h.updateUser)
	users.DELETE("/:id", h.deleteUser)
	users.POST("/:id/password", h.resetPassword)
	users.POST("/:id/lock", h.lockUser)
	users.POST("/:id/unlock", h.unlockUser)

	// Role assignments
	assignments := iam.Group("/assignments")
	assignments.POST("/", h.assignRole)
	assignments.GET("/", h.listAssignments)
	assignments.GET("/user/:userId", h.getUserAssignments)
	assignments.GET("/role/:roleId", h.getRoleAssignees)
	assignments.PUT("/:id", h.updateAssignment)
	assignments.DELETE("/:id", h.removeAssignment)
	assignments.POST("/:id/renew", h.renewAssignment)

	// Privileged access workflow
	privileged := iam.Group("/privileged-access")
	privileged.POST("/", h.requestPrivilegedAccess)
	privileged.GET("/", h.listPrivilegedRequests)
	privileged.GET("/:id", h.getPrivilegedRequest)
	privileged.PUT("/:id/approve", h.approvePrivilegedAccess)
	privileged.PUT("/:id/reject", h.rejectPrivilegedAccess)
	privileged.POST("/:id/grant", h.grantPrivilegedAccess)
	privileged.POST("/:id/revoke", h.revokePrivilegedAccess)

	// Audit logging
	audit := iam.Group("/audit")
	audit.GET("/", h.queryAuditLogs)
	audit.GET("/user/:userId", h.getUserAuditLogs)
	audit.GET("/resource/:resourceId", h.getResourceAuditLogs)
	audit.POST("/export", h.exportAuditLogs)

	// RBAC management
	rbac := iam.Group("/rbac")
	rbac.POST("/policies", h.createRBACPolicy)
	rbac.GET("/policies", h.listRBACPolicies)
	rbac.GET("/policies/:id", h.getRBACPolicy)
	rbac.PUT("/policies/:id", h.updateRBACPolicy)
	rbac.DELETE("/policies/:id", h.deleteRBACPolicy)

	// Permission matrix
	matrix := iam.Group("/permission-matrix")
	matrix.GET("/", h.getPermissionMatrix)
	matrix.POST("/calculate", h.calculateEffectivePermissions)

	// JIT elevation
	jit := iam.Group("/jit-elevation")
	jit.POST("/requests", h.requestJITElevation)
	jit.GET("/requests", h.listJITRequests)
	jit.PUT("/requests/:id/approve", h.approveJITElevation)
	jit.PUT("/requests/:id/revoke", h.revokeJITElevation)
}

// createUser creates new user account
func (h *IdentityAccessHandler) createUser(c echo.Context) error {
	var user UserAccount
	if err := c.Bind(&user); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	user.ID = uuid.New().String()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()
	user.Status = "active"
	if user.Permissions == nil {
		user.Permissions = make([]Permission, 0)
	}
	if user.Roles == nil {
		user.Roles = make([]UserRoleAssignment, 0)
	}

	if err := h.store.CreateUser(&user); err != nil {
		h.logger.Errorf("Failed to create user: %v", err)
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	// Record evidence
	ctx := capability.GetContext(c.Request().Context())
	h.evidence.Attest(ctx, evidence.Event{
		Type:         evidence.UserCreated,
		ResourceID:   user.ID,
		ResourceType: "user_account",
		Actor:        ctx.User,
		Metadata:     map[string]any{"username": user.Username},
	})

	return c.JSON(http.StatusCreated, user)
}

// listUsers retrieves all users
func (h *IdentityAccessHandler) listUsers(c echo.Context) error {
	status := c.QueryParam("status")
	search := c.QueryParam("search")

	users, err := h.store.ListUsers(status, search)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, users)
}

// getUser retrieves specific user
func (h *IdentityAccessHandler) getUser(c echo.Context) error {
	id := c.Param("id")
	user, err := h.store.GetUser(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, user)
}

// updateUser updates user info
func (h *IdentityAccessHandler) updateUser(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	user, err := h.store.UpdateUser(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, user)
}

// deleteUser deletes user account
func (h *IdentityAccessHandler) deleteUser(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteUser(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "User deleted"})
}

// resetPassword resets user password
func (h *IdentityAccessHandler) resetPassword(c echo.Context) error {
	id := c.Param("id")
	var req struct {
		NewPassword string `json:"newPassword"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	if err := h.store.ResetPassword(id, req.NewPassword); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Password reset successfully"})
}

// lockUser locks user account
func (h *IdentityAccessHandler) lockUser(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.LockUser(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "User locked"})
}

// unlockUser unlocks user account
func (h *IdentityAccessHandler) unlockUser(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.UnlockUser(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "User unlocked"})
}

// assignRole assigns role to user
func (h *IdentityAccessHandler) assignRole(c echo.Context) error {
	var assignment UserRoleAssignment
	if err := c.Bind(&assignment); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	assignment.ID = uuid.New().String()
	assignment.CreatedAt = time.Now()

	ctx := capability.GetContext(c.Request().Context())
	assignment.CreatedBy = ctx.User

	if err := h.store.AssignRole(&assignment); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, assignment)
}

// listAssignments retrieves all assignments
func (h *IdentityAccessHandler) listAssignments(c echo.Context) error {
	userID := c.QueryParam("userId")
	roleID := c.QueryParam("roleId")

	assignments, err := h.store.ListAssignments(userID, roleID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, assignments)
}

// getUserAssignments gets user's role assignments
func (h *IdentityAccessHandler) getUserAssignments(c echo.Context) error {
	userID := c.Param("userId")
	assignments, err := h.store.GetUserAssignments(userID)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, assignments)
}

// getRoleAssignees gets all users with specific role
func (h *IdentityAccessHandler) getRoleAssignees(c echo.Context) error {
	roleId := c.Param("roleId")
	users, err := h.store.GetRoleAssignees(roleId)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Role not found"})
	}
	return c.JSON(http.StatusOK, users)
}

// updateAssignment updates assignment
func (h *IdentityAccessHandler) updateAssignment(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	assignment, err := h.store.UpdateAssignment(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Assignment not found"})
	}
	return c.JSON(http.StatusOK, assignment)
}

// removeAssignment removes role from user
func (h *IdentityAccessHandler) removeAssignment(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.RemoveAssignment(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Assignment not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Assignment removed"})
}

// renewAssignment extends assignment expiry
func (h *IdentityAccessHandler) renewAssignment(c echo.Context) error {
	id := c.Param("id")
	var req struct {
		Days int `json:"days"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	assignment, err := h.store.RenewAssignment(id, req.Days)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Assignment not found"})
	}
	return c.JSON(http.StatusOK, assignment)
}

// requestPrivilegedAccess requests privileged access
func (h *IdentityAccessHandler) requestPrivilegedAccess(c echo.Context) error {
	var request PrivilegedAccess
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	request.ID = uuid.New().String()
	request.RequestedAt = time.Now()
	request.ApprovalStatus = "pending"
	request.ExpiresAt = time.Now().Add(4 * time.Hour) // 4 hour default

	ctx := capability.GetContext(c.Request().Context())
	request.RequestedBy = ctx.User

	if err := h.store.RequestPrivilegedAccess(&request); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	// Evidence of privilege escalation request
	h.evidence.Attest(ctx, evidence.Event{
		Type:         evidence.PrivilegedAccessRequested,
		ResourceID:   request.ID,
		ResourceType: "privilege_request",
		Actor:        ctx.User,
		Metadata: map[string]any{
			"targetResource": request.TargetResource,
			"resourceType": request.ResourceType,
		},
	})

	return c.JSON(http.StatusCreated, request)
}

// listPrivilegedRequests lists access requests
func (h *IdentityAccessHandler) listPrivilegedRequests(c echo.Context) error {
	requesterID := c.QueryParam("requesterId")
	status := c.QueryParam("status")

	requests, err := h.store.ListPrivilegedRequests(requesterID, status)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, requests)
}

// getPrivilegedRequest retrieves specific request
func (h *IdentityAccessHandler) getPrivilegedRequest(c echo.Context) error {
	id := c.Param("id")
	request, err := h.store.GetPrivilegedRequest(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Request not found"})
	}
	return c.JSON(http.StatusOK, request)
}

// approvePrivilegedAccess approves request
func (h *IdentityAccessHandler) approvePrivilegedAccess(c echo.Context) error {
	id := c.Param("id")
	var req struct {
		Approver string `json:"approver"`
		Note     string `json:"note"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	request, err := h.store.ApprovePrivilegedAccess(id, req.Approver, req.Note)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Request not found"})
	}
	return c.JSON(http.StatusOK, request)
}

// rejectPrivilegedAccess rejects request
func (h *IdentityAccessHandler) rejectPrivilegedAccess(c echo.Context) error {
	id := c.Param("id")
	var req struct {
		Rejector string `json:"rejector"`
		Reason   string `json:"reason"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	request, err := h.store.RejectPrivilegedAccess(id, req.Rejector, req.Reason)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Request not found"})
	}
	return c.JSON(http.StatusOK, request)
}

// grantPrivilegedAccess grants temporary privileges
func (h *IdentityAccessHandler) grantPrivilegedAccess(c echo.Context) error {
	id := c.Param("id")
	now := time.Now()

	request, err := h.store.GrantPrivilegedAccess(id, &now)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Request not found"})
	}
	return c.JSON(http.StatusOK, request)
}

// revokePrivilegedAccess revokes granted privileges
func (h *IdentityAccessHandler) revokePrivilegedAccess(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.RevokePrivilegedAccess(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Request not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Privileges revoked"})
}

// queryAuditLogs queries audit logs
func (h *IdentityAccessHandler) queryAuditLogs(c echo.Context) error {
	userID := c.QueryParam("userId")
	resource := c.QueryParam("resource")
	action := c.QueryParam("action")
	startTime := c.QueryParam("startTime")
	endTime := c.QueryParam("endTime")

	logs, err := h.store.QueryAuditLogs(userID, resource, action, startTime, endTime)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, logs)
}

// getUserAuditLogs gets user-specific audit logs
func (h *IdentityAccessHandler) getUserAuditLogs(c echo.Context) error {
	userID := c.Param("userId")
	logs, err := h.store.GetUserAuditLogs(userID)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, logs)
}

// getResourceAuditLogs gets resource-specific audit logs
func (h *IdentityAccessHandler) getResourceAuditLogs(c echo.Context) error {
	resourceID := c.Param("resourceId")
	logs, err := h.store.getResourceAuditLogs(resourceID)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Resource not found"})
	}
	return c.JSON(http.StatusOK, logs)
}

// exportAuditLogs exports audit logs to file
func (h *IdentityAccessHandler) exportAuditLogs(c echo.Context) error {
	format := c.QueryParam("format") // csv, json, pdf
	exportID, err := h.store.ExportAuditLogs(format)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Export failed"})
	}
	return c.JSON(http.StatusOK, map[string]string{
		"exportId": exportID,
		"format":   format,
	})
}

// createRBACPolicy creates new RBAC policy
func (h *IdentityAccessHandler) createRBACPolicy(c echo.Context) error {
	var policy RBACPolicy
	if err := c.Bind(&policy); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	policy.ID = uuid.New().String()
	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()

	if err := h.store.CreateRBACPolicy(&policy); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, policy)
}

// listRBACPolicies retrieves all policies
func (h *IdentityAccessHandler) listRBACPolicies(c echo.Context) error {
	policies, err := h.store.ListRBACPolicies()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, policies)
}

// getRBACPolicy retrieves specific policy
func (h *IdentityAccessHandler) getRBACPolicy(c echo.Context) error {
	id := c.Param("id")
	policy, err := h.store.GetRBACPolicy(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, policy)
}

// updateRBACPolicy updates policy
func (h *IdentityAccessHandler) updateRBACPolicy(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	policy, err := h.store.UpdateRBACPolicy(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, policy)
}

// deleteRBACPolicy deletes policy
func (h *IdentityAccessHandler) deleteRBACPolicy(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteRBACPolicy(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Policy not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Policy deleted"})
}

// getPermissionMatrix retrieves complete permission matrix
func (h *IdentityAccessHandler) getPermissionMatrix(c echo.Context) error {
	matrix := h.store.GetPermissionMatrix()
	return c.JSON(http.StatusOK, matrix)
}

// calculateEffectivePermissions calculates effective permissions for user
func (h *IdentityAccessHandler) calculateEffectivePermissions(c echo.Context) error {
	var req struct {
		UserID string `json:"userId"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	permissions := h.store.CalculateEffectivePermissions(req.UserID)
	return c.JSON(http.StatusOK, map[string]any{
		"userId":      req.UserID,
		"permissions": permissions,
	})
}

// requestJITElevation requests Just-In-Time elevation
func (h *IdentityAccessHandler) requestJITElevation(c echo.Context) error {
	var req struct {
		TargetResource string `json:"targetResource"`
		RequiredLevel  string `json:"requiredLevel"`
		Reason         string `json:"reason"`
		DurationHours  int    `json:"durationHours"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	requestID := uuid.New().String()
	request := PrivilegedAccess{
		ID:             requestID,
		TargetResource: req.TargetResource,
		RequestedBy:    capability.GetContext(c.Request().Context()).User,
		RequestedAt:    time.Now(),
		ExpiresAt:      time.Now().Add(time.Duration(req.DurationHours) * time.Hour),
		Reason:         req.Reason,
		ApprovalStatus: "pending",
	}

	if err := h.store.RequestJITElevation(&request); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"requestId": requestID})
}

// listJITRequests lists JIT elevation requests
func (h *IdentityAccessHandler) listJITRequests(c echo.Context) error {
	status := c.QueryParam("status")
	requests, err := h.store.ListJITRequests(status)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, requests)
}

// approveJITElevation approves JIT elevation
func (h *IdentityAccessHandler) approveJITElevation(c echo.Context) error {
	id := c.Param("id")
	approver := capability.GetContext(c.Request().Context()).User

	request, err := h.store.ApproveJITElevation(id, approver)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Request not found"})
	}
	return c.JSON(http.StatusOK, request)
}

// revokeJITElevation revokes JIT elevation
func (h *IdentityAccessHandler) revokeJITElevation(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.RevokeJITElevation(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Request not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Elevation revoked"})
}
