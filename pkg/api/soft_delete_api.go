// Package api implements REST API endpoints for soft delete operations.
package api

import (
	"net/http"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// SoftDeleteAPI - REST API endpoints for soft delete functionality
// ============================================================================

type SoftDeleteAPI struct {
	softDeleteManager *SoftDeleteManager
	logger            Logger
}

// NewSoftDeleteAPI creates new soft delete API instance
func NewSoftDeleteAPI(softDeleteManager *SoftDeleteManager, logger Logger) *SoftDeleteAPI {
	if softDeleteManager == nil {
		panic("soft delete manager cannot be nil")
	}
	
	defensive.RequireNonNil(logger, "logger")
	
	return &SoftDeleteAPI{
		softDeleteManager: softDeleteManager,
		logger:            logger.WithField("component", "soft_delete_api"),
	}
}

// ============================================================================
// Public Endpoints
// ============================================================================

// SoftDelete handles DELETE requests with audit trail
// POST /api/v2/resources/{type}/{id}/delete
func (api *SoftDeleteAPI) SoftDelete(c *gin.Context) {
	ctx := c.Request.Context()
	
	// Get path parameters
	resourceType := c.Param("type")
	recordID := c.Param("id")
	
	if resourceType == "" || recordID == "" {
		api.respondError(c, http.StatusBadRequest, 
			defensive.ValidationError("path_params", "type and id required"))
		return
	}
	
	// Validate resource type
	if !isValidResourceType(resourceType) {
		api.respondError(c, http.StatusBadRequest, 
			defensive.ValidationError("type", "valid resource type required"))
		return
	}
	
	// Parse request body
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.respondError(c, http.StatusBadRequest, 
			defensive.Wrap(err, defensive.ErrorCodeValidation, "invalid request body"))
		return
	}
	
	// Validate deletion reason
	if err := validateDeletionReason(req.Reason); err != nil {
		api.respondError(c, http.StatusBadRequest, err)
		return
	}
	
	// Check if entity is already deleted
	isDeleted := api.softDeleteManager.IsSoftDeleted(ctx, resourceType, recordID)
	if isDeleted {
		api.respondError(c, http.StatusConflict,
			defensive.Errorf("%s/%s is already soft-deleted", resourceType, recordID))
		return
	}
	
	// Perform soft delete operation
	err := api.performSoftDelete(ctx, resourceType, recordID, req)
	if err != nil {
		api.logger.WithError(err).WithFields(logrus.Fields{
			"type": resourceType,
			"id": recordID,
		}).Error("Soft delete failed")
		
		api.respondError(c, http.StatusInternalServerError,
			defensive.Wrap(err, defensive.ErrorCodeInternal, "deletion failed"))
		return
	}
	
	api.respondSuccess(c, http.StatusOK, 
		DeleteConfirmation{
			Success: true,
			ID:      recordID,
			Type:    resourceType,
		})
}

// Restore handles restore operations for soft-deleted entities
// POST /api/v2/resources/{type}/{id}/restore
func (api *SoftDeleteAPI) Restore(c *gin.Context) {
	ctx := c.Request.Context()
	
	resourceType := c.Param("type")
	recordID := c.Param("id")
	
	if resourceType == "" || recordID == "" {
		api.respondError(c, http.StatusBadRequest, 
			defensive.ValidationError("path_params", "type and id required"))
		return
	}
	
	// Verify entity exists and is soft-deleted
	isDeleted := api.softDeleteManager.IsSoftDeleted(ctx, resourceType, recordID)
	if !isDeleted {
		api.respondError(c, http.StatusBadRequest,
			defensive.Errorf("%s/%s is not soft-deleted", resourceType, recordID))
		return
	}
	
	// Perform restore operation
	err := api.performRestore(ctx, resourceType, recordID, c.GetString("user_id"))
	if err != nil {
		api.logger.WithError(err).WithFields(logrus.Fields{
			"type": resourceType,
			"id": recordID,
		}).Error("Restore failed")
		
		api.respondError(c, http.StatusInternalServerError,
			defensive.Wrap(err, defensive.ErrorCodeInternal, "restore failed"))
		return
	}
	
	api.respondSuccess(c, http.StatusOK,
		RestoreConfirmation{
			Success: true,
			ID:      recordID,
			Type:    resourceType,
		})
}

// DeletionHistory returns complete lifecycle of entity including deletions/restorations
// GET /api/v2/resources/{type}/{id}/history
func (api *SoftDeleteAPI) DeletionHistory(c *gin.Context) {
	ctx := c.Request.Context()
	
	resourceType := c.Param("type")
	recordID := c.Param("id")
	
	// Validate parameters
	if resourceType == "" || recordID == "" {
		api.respondError(c, http.StatusBadRequest, 
			defensive.ValidationError("path_params", "type and id required"))
		return
	}
	
	// Query history from audit logs
	history, err := api.softDeleteManager.GetDeletionHistory(ctx, resourceType, recordID)
	if err != nil {
		api.logger.WithError(err).WithFields(logrus.Fields{
			"type": resourceType,
			"id": recordID,
		}).Error("Failed to query history")
		
		api.respondError(c, http.StatusInternalServerError,
			defensive.Wrap(err, defensive.ErrorCodeInternal, "failed to retrieve history"))
		return
	}
	
	// Transform history entries for response format
	historyEntries := transformAuditLogsToResponse(history)
	
	api.respondSuccess(c, http.StatusOK, DeletionHistoryResponse{
		EntityID:   recordID,
		ResourceType: resourceType,
		History:    historyEntries,
		TotalEvents: len(historyEntries),
	})
}

// ============================================================================
// Helper Methods
// ============================================================================

func (api *SoftDeleteAPI) performSoftDelete(ctx context.Context, resourceType string, recordID string, req DeleteRequest) error {
	// This would use the actual entity types from the application layer
	// For now, delegate to SoftDeleteManager which uses reflection/dynamic dispatch
	
	return api.softDeleteManager.SoftDelete(ctx, &GenericSoftDeletable{
		TableName:  resourceType,
		RecordID:   recordID,
		DeletionReason: req.Reason,
	}, getUserFromContext(ctx))
}

func (api *SoftDeleteAPI) performRestore(ctx context.Context, resourceType string, recordID string, restoredBy string) error {
	return api.softDeleteManager.Restore(ctx, &GenericSoftDeletable{
		TableName: resourceType,
		RecordID:  recordID,
	}, restoredBy)
}

// validateDeletionReason ensures reason meets compliance requirements
func validateDeletionReason(reason string) error {
	minLength := 10
	maxLength := 500
	
	if len(reason) < minLength || len(reason) > maxLength {
		return defensive.Errorf("reason length must be %d-%d characters", minLength, maxLength)
	}
	
	return nil
}

func isValidResourceType(resourceType string) bool {
	validTypes := []string{"orders", "workloads", "decisions", "evidence"}
	
	for _, valid := range validTypes {
		if resourceType == valid {
			return true
		}
	}
	
	return false
}

func getUserFromContext(ctx context.Context) string {
	userID, exists := ctx.Value("user_id").(string)
	if !exists {
		return "system"
	}
	return userID
}

// ============================================================================
// Response Structures
// ============================================================================

type DeleteRequest struct {
	Reason string `json:"reason" binding:"required,min=10,max=500"`
}

type DeleteConfirmation struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Type    string `json:"type"`
}

type RestoreConfirmation struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Type    string `json:"type"`
}

type DeletionHistoryResponse struct {
	EntityID       string        `json:"entity_id"`
	ResourceType   string        `json:"resource_type"`
	History        []EventRecord `json:"history"`
	TotalEvents    int           `json:"total_events"`
}

type EventRecord struct {
	Timestamp     time.Time    `json:"timestamp"`
	Action        string       `json:"action"`           // CREATE, UPDATE, DELETE, RESTORE
	UserID        string       `json:"user_id,omitempty"`
	IPAddress     string       `json:"ip_address,omitempty"`
	SessionID     string       `json:"session_id,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
}

// ============================================================================
// Middleware Integration
// ============================================================================

func (api *SoftDeleteAPI) RegisterRoutes(router *gin.Engine) {
	apiGroup := router.Group("/api/v2/resources")
	
	// Soft delete endpoint
	apiGroup.POST("/:type/:id/delete", api.SoftDelete)
	
	// Restore endpoint  
	apiGroup.POST("/:type/:id/restore", api.Restore)
	
	// History endpoint
	apiGroup.GET("/:type/:id/history", api.DeletionHistory)
	
	// Health check endpoint
	router.GET("/health/soft-delete", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "soft-delete-audit",
			"status":  "healthy",
			"version": "1.0.0",
		})
	})
}

// GenericSoftDeletable represents a generic entity supporting soft deletion
type GenericSoftDeletable struct {
	TableName        string
	RecordID         string
	DeletedAt        *time.Time
	DeletedBy        string
	DeletionReason   string
}

func (g *GenericSoftDeletable) GetID() string                     { return g.RecordID }
func (g *GenericSoftDeletable) GetTableName() string              { return g.TableName }
func (g *GenericSoftDeletable) GetDeletedAt() *time.Time          { return g.DeletedAt }
func (g *GenericSoftDeletable) SetDeletedAt(t time.Time)          { g.DeletedAt = &t }
func (g *GenericSoftDeletable) GetDeletedBy() string              { return g.DeletedBy }
func (g *GenericSoftDeletable) SetDeletedBy(userID string)        { g.DeletedBy = userID }
func (g *GenericSoftDeletable) GetDeletionReason() string         { return g.DeletionReason }
func (g *GenericSoftDeletable) SetDeletionReason(reason string)   { g.DeletionReason = reason }
