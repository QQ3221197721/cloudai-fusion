// Package apiserver provides HTTP handlers for CloudAI Fusion REST API
package apiserver

import (
	"net/http"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/rbac"
	"github.com/gin-gonic/gin"
)

// RBACAPIServer handles all RBAC-related REST endpoints
type RBACAPIServer struct {
	manager *rbac.Manager
}

// NewRBACAPIServer creates a new RBACAPIServer instance
func NewRBACAPIServer(rbacMgr *rbac.Manager) *RBACAPIServer {
	return &RBACAPIServer{manager: rbacMgr}
}

// Register registers all RBAC routes with the gin router
func (r *RBACAPIServer) Register(router *gin.Engine) {
	api := router.Group("/api/v1")
	api.Use(authMiddleware()) // Require authentication
	
	{
		// ==================== Role Management ====================
		api.GET("/rbac/roles", r.listRoles)
		api.POST("/rbac/roles", r.createRole)
		api.GET("/rbac/roles/:roleId", r.getRole)
		api.PUT("/rbac/roles/:roleId", r.updateRole)
		api.DELETE("/rbac/roles/:roleId", r.deleteRole)
		
		// ==================== Permission Matrix ====================
		api.GET("/rbac/permissions/batch", r.getPermissionMatrix)
		
		// ==================== User-Role Mapping ====================
		api.GET("/users/:userId/roles", r.getUserRoles)
		api.PUT("/users/:userId/roles", r.assignRoles)
		api.DELETE("/users/:userId/roles/:roleId", r.removeRole)
	}
}

// listRoles lists all roles
func (r *RBACAPIServer) listRoles(c *gin.Context) {
	roles := r.manager.ListRoles()
	c.JSON(http.StatusOK, gin.H{
		"total": len(roles),
		"roles": roles,
	})
}

// createRole creates a new role
func (r *RBACAPIServer) createRole(c *gin.Context) {
	var req struct {
		Name        string            `json:"name" binding:"required"`
		Description string            `json:"description"`
		Permissions map[string]bool   `json:"permissions" binding:"required"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	role := &rbac.Role{
		ID:          generateUUID(),
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
		IsBuiltIn:   false,
		CreatedAt:   "2026-08-02T00:00:00Z",
	}
	
	if err := r.manager.RegisterRole(role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusCreated, role)
}

// getRole gets a specific role by ID
func (r *RBACAPIServer) getRole(c *gin.Context) {
	roleID := c.Param("roleId")
	role, err := r.manager.GetRole(roleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, role)
}

// updateRole updates an existing role
func (r *RBACAPIServer) updateRole(c *gin.Context) {
	roleID := c.Param("roleId")
	
	role, err := r.manager.GetRole(roleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}
	
	if role.IsBuiltIn {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot modify built-in role"})
		return
	}
	
	var req struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Permissions map[string]bool   `json:"permissions"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	if req.Name != "" {
		role.Name = req.Name
	}
	if req.Description != "" {
		role.Description = req.Description
	}
	if req.Permissions != nil {
		role.Permissions = req.Permissions
	}
	
	if err := r.manager.RegisterRole(role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, role)
}

// deleteRole deletes a role
func (r *RBACAPIServer) deleteRole(c *gin.Context) {
	roleID := c.Param("roleId")
	
	role, err := r.manager.GetRole(roleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}
	
	if role.IsBuiltIn {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete built-in role"})
		return
	}
	
	if err := r.manager.DeleteRole(roleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusNoContent, nil)
}

// getPermissionMatrix gets permission matrix for multiple users
func (r *RBACAPIServer) getPermissionMatrix(c *gin.Context) {
	var req struct {
		UserIDs []string `json:"user_ids" binding:"required"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	matrix := make(map[string]map[string]bool)
	for _, userID := range req.UserIDs {
		roles := r.manager.GetUserRoles(userID)
		userPerms := make(map[string]bool)
		for _, roleID := range roles {
			if role, ok := r.manager.GetRole(roleID); ok {
				for perm, hasPerm := range role.Permissions {
					userPerms[perm] = userPerms[perm] || hasPerm
				}
			}
		}
		matrix[userID] = userPerms
	}
	
	c.JSON(http.StatusOK, gin.H{
		"matrix": matrix,
	})
}

// getUserRoles gets roles assigned to a user
func (r *RBACAPIServer) getUserRoles(c *gin.Context) {
	userID := c.Param("userId")
	roles := r.manager.GetUserRoles(userID)
	c.JSON(http.StatusOK, gin.H{
		"user_id": userID,
		"roles":   roles,
	})
}

// assignRoles assigns one or more roles to a user
func (r *RBACAPIServer) assignRoles(c *gin.Context) {
	userID := c.Param("userId")
	var req struct {
		RoleIDs []string `json:"role_ids" binding:"required"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	if err := r.manager.AssignRoles(userID, req.RoleIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"user_id": userID,
		"roles":   req.RoleIDs,
	})
}

// removeRole removes a role from a user
func (r *RBACAPIServer) removeRole(c *gin.Context) {
	userID := c.Param("userId")
	roleID := c.Param("roleId")
	
	if err := r.manager.RemoveRole(userID, roleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"user_id": userID,
		"message": "Role removed successfully",
	})
}
