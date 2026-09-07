package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/models"
	"gorm.io/gorm"
)

// WorkOrderHandler provides CRUD operations for authorization requests.
type WorkOrderHandler struct {
	db *gorm.DB
}

// NewWorkOrderHandler creates a new work order handler.
func NewWorkOrderHandler(db *gorm.DB) *WorkOrderHandler {
	return &WorkOrderHandler{db: db}
}

// CreateWorkOrderRequest represents the request body for creating a work order.
type CreateWorkOrderRequest struct {
	CompanyName          string   `json:"company_name" binding:"required"`
	ContactEmail         string   `json:"contact_email" binding:"required,email"`
	Justification        string   `json:"justification" binding:"required"`
	TargetList           []string `json:"target_list" binding:"required"`
	AuthorizationLetterURL string `json:"authorization_letter_url,omitempty"`
	LegalContractURL     string   `json:"legal_contract_url,omitempty"`
	PriorityLevel        int      `json:"priority_level" binding:"min=1,max=4"`
}

// UpdateWorkOrderRequest represents the request body for updating a work order.
type UpdateWorkOrderRequest struct {
	Status          string  `json:"status"`
	PriorityLevel   *int    `json:"priority_level"`
	ReviewComments  string  `json:"review_comments,omitempty"`
	RejectionReason string  `json:"rejection_reason,omitempty"`
}

// ListWorkOrderResponse represents paginated work order list response.
type ListWorkOrderResponse struct {
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	PerPage int             `json:"per_page"`
	Data    []WorkOrderItem `json:"data"`
}

// WorkOrderItem simplified work order data for listing.
type WorkOrderItem struct {
	ID            uuid.UUID       `json:"id"`
	UserID        uuid.UUID       `json:"user_id"`
	UserName      string          `json:"user_name"`
	CompanyName   string          `json:"company_name"`
	TargetCount   int             `json:"target_count"`
	Status        string          `json:"status"`
	PriorityLevel int             `json:"priority_level"`
	SubmittedAt   time.Time       `json:"submitted_at"`
	ReviewedAt    *time.Time      `json:"reviewed_at,omitempty"`
	ReviewerName  *string         `json:"reviewer_name,omitempty"`
}

// Create creates a new work order authorization request.
func (h *WorkOrderHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateWorkOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user from context (set by JWT middleware)
	userIDStr, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	// Build work order entity
	workOrder := &models.WorkOrder{
		UserID:               userID,
		CompanyName:          req.CompanyName,
		ContactEmail:         req.ContactEmail,
		Justification:        req.Justification,
		TargetList:           models.NullStringArray{Strings: req.TargetList, Valid: true},
		AuthorizationLetterURL: req.AuthorizationLetterURL,
		LegalContractURL:     req.LegalContractURL,
		Status:               "pending",
		PriorityLevel:        req.PriorityLevel,
		SubmittedAt:          time.Now(),
	}

	// Validate before database insert
	if err := workOrder.ValidateWorkOrder(); err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": err.Error()})
		return
	}

	// Create in database
	if err := h.db.WithContext(ctx).Create(workOrder).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create work order", "details": err.Error()})
		return
	}

	// Record audit log
	h.createAuditLog(ctx, workOrder.ID, "submitted", userID)

	c.JSON(http.StatusCreated, workOrder)
}

// List returns paginated list of work orders with filtering.
func (h *WorkOrderHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	// Get query parameters
	page := c.Query("page")
	if page == "" {
		page = "1"
	}
	perPage := c.Query("per_page")
	if perPage == "" {
		perPage = "20"
	}
	status := c.Query("status")
	priority := c.Query("priority")

	var total int64
	query := h.db.WithContext(ctx).Model(&models.WorkOrder{})

	// Apply filters
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if priority != "" {
		query = query.Where("priority_level = ?", priority)
	}

	// Count total records
	query.Count(&total)

	// Parse pagination
	var offset int
	fmt.Sscanf(page, "%d", &offset)
	offset = (offset - 1) * 20

	fmt.Sscanf(perPage, "%d", &perPage)

	// Fetch records
	var workOrders []models.WorkOrder
	if err := query.Preload("User").Preload("Reviewer").Offset(offset).Limit(perPage).Order("submitted_at DESC").Find(&workOrders).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch work orders"})
		return
	}

	// Transform to response format
	var items []WorkOrderItem
	for _, wo := range workOrders {
		item := WorkOrderItem{
			ID:            wo.ID,
			UserID:        wo.UserID,
			UserName:      wo.User.Username,
			CompanyName:   wo.CompanyName,
			TargetCount:   len(wo.TargetList.Strings),
			Status:        wo.Status,
			PriorityLevel: wo.PriorityLevel,
			SubmittedAt:   wo.SubmittedAt,
			ReviewedAt:    wo.ReviewedAt,
		}
		if wo.Reviewer != nil {
			reviewerName := wo.Reviewer.Username
			item.ReviewerName = &reviewerName
		}
		items = append(items, item)
	}

	response := ListWorkOrderResponse{
		Total:   int(total),
		Page:    offset/perPage + 1,
		PerPage: perPage,
		Data:    items,
	}

	c.JSON(http.StatusOK, response)
}

// Get retrieves a single work order by ID.
func (h *WorkOrderHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	workOrderID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid work order ID"})
		return
	}

	var workOrder models.WorkOrder
	if err := h.db.WithContext(ctx).
		Preload("User").
		Preload("Reviewer").
		Preload("Campaigns").
		Preload("AuditTrail.PerformedBy").
		First(&workOrder, workOrderID).Error; err != nil {
		
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "work order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch work order"})
		return
	}

	c.JSON(http.StatusOK, workOrder)
}

// Update modifies an existing work order (admin/pentester only).
func (h *WorkOrderHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	workOrderID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid work order ID"})
		return
	}

	var workOrder models.WorkOrder
	if err := h.db.WithContext(ctx).First(&workOrder, workOrderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.h{"error": "work order not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to fetch work order"})
		return
	}

	var req UpdateWorkOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": err.Error()})
		return
	}

	// Get current user (should be admin or pentester)
	userIDStr, _ := c.Get("user_id")
	currentUserID, _ := uuid.Parse(userIDStr.(string))

	// Determine updates based on role and status changes
	oldStatus := workOrder.Status
	updates := make(map[string]interface{})

	// Status transitions logic
	if req.Status != "" && req.Status != oldStatus {
		validTransitions := map[string][]string{
			"pending":   {"approved", "rejected"},
			"approved":  {}, // No outgoing transitions
			"rejected":  {"pending"},
			"cancelled": {},
			"expired":   {},
		}

		targetTransitions := validTransitions[oldStatus]
		allowed := false
		for _, t := range targetTransitions {
			if t == req.Status {
				allowed = true
				break
			}
		}

		if !allowed {
			c.JSON(http.StatusBadRequest, gin.h{"error": fmt.Sprintf("cannot transition from %s to %s", oldStatus, req.Status)})
			return
		}

		updates["status"] = req.Status
		
		// Set review timestamps
		if req.Status == "approved" || req.Status == "rejected" {
			now := time.Now()
			updates["reviewed_at"] = now
			
			// Auto-set expiry date for approved orders (30 days)
			if req.Status == "approved" {
				expiresAt := now.AddDate(0, 1, 0)
				updates["expires_at"] = expiresAt
			}
		}

		// Set reviewer info
		updates["reviewer_id"] = currentUserID
		updates["review_comments"] = req.ReviewComments
		if req.RejectionReason != "" {
			updates["rejection_reason"] = req.RejectionReason
		}
	}

	// Priority level can always be updated
	if req.PriorityLevel != nil {
		updates["priority_level"] = *req.PriorityLevel
	}

	// Execute update
	if len(updates) > 0 {
		if err := h.db.WithContext(ctx).Model(&workOrder).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to update work order"})
			return
		}

		// Create audit log entry
		action := "updated"
		if updates["status"] != nil {
			action = updates["status"].(string)
		}
		h.createAuditLog(ctx, workOrderID, action, currentUserID)
	}

	// Fetch updated record
	var updatedWorkOrder models.WorkOrder
	h.db.WithContext(ctx).Preload("User").Preload("Reviewer").First(&updatedWorkOrder, workOrderID)

	c.JSON(http.StatusOK, updatedWorkOrder)
}

// Delete removes a work order (admin only, soft delete recommended).
func (h *WorkOrderHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	workOrderID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid work order ID"})
		return
	}

	// Check if has associated campaigns
	var campaignCount int64
	h.db.Model(&models.AttackCampaign{}).Where("work_order_id = ?", workOrderID).Count(&campaignCount)
	if campaignCount > 0 {
		c.JSON(http.StatusForbidden, gin.h{"error": "cannot delete work order with existing campaigns"})
		return
	}

	result := h.db.WithContext(ctx).Delete(&models.WorkOrder{}, workOrderID)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to delete work order"})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.h{"error": "work order not found"})
		return
	}

	c.JSON(http.StatusOK, gin.h{"message": "work order deleted successfully"})
}

// Approve approves a pending work order (admin only).
func (h *WorkOrderHandler) Approve(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	workOrderID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid work order ID"})
		return
	}

	var workOrder models.WorkOrder
	if err := h.db.WithContext(ctx).First(&workOrder, workOrderID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.h{"error": "work order not found"})
		return
	}

	if workOrder.Status != "pending" {
		c.JSON(http.StatusBadRequest, gin.h{"error": fmt.Sprintf("cannot approve work order with status: %s", workOrder.Status)})
		return
	}

	// Get current user as reviewer
	userIDStr, _ := c.Get("user_id")
	reviewerID, _ := uuid.Parse(userIDStr.(string))

	now := time.Now()
	updates := map[string]interface{}{
		"status":         "approved",
		"reviewed_at":    now,
		"approved_at":    now,
		"reviewer_id":    reviewerID,
		"review_comments": c.Query("comments"),
		"expires_at":     now.AddDate(0, 1, 0), // 30 day validity
	}

	if err := h.db.WithContext(ctx).Model(&workOrder).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to approve work order"})
		return
	}

	h.createAuditLog(ctx, workOrderID, "approved", reviewerID)

	var approvedWorkOrder models.WorkOrder
	h.db.WithContext(ctx).Preload("User").Preload("Reviewer").First(&approvedWorkOrder, workOrderID)

	c.JSON(http.StatusOK, approvedWorkOrder)
}

// Reject rejects a pending work order (admin only).
func (h *WorkOrderHandler) Reject(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	workOrderID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid work order ID"})
		return
	}

	reason := c.PostForm("reason")
	if reason == "" {
		c.JSON(http.StatusBadRequest, gin.h{"error": "rejection reason is required"})
		return
	}

	// Call Update handler with rejection status
	c.Set("update_request", UpdateWorkOrderRequest{
		Status:          "rejected",
		RejectionReason: reason,
	})

	h.Update(c)
}

// Audit retrieves audit trail for a work order.
func (h *WorkOrderHandler) Audit(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	workOrderID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid work order ID"})
		return
	}

	var audits []models.WorkOrderAudit
	if err := h.db.WithContext(ctx).
		Preload("PerformedBy").
		Where("work_order_id = ?", workOrderID).
		Order("performed_at DESC").
		Find(&audits).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to fetch audit trail"})
		return
	}

	c.JSON(http.StatusOK, gin.h{"audit_trail": audits})
}

// Helper methods
func (h *WorkOrderHandler) createAuditLog(ctx context.Context, workOrderID uuid.UUID, action string, performedBy uuid.UUID) {
	log := &models.WorkOrderAudit{
		WorkOrderID: workOrderID,
		Action:      action,
		PerformedBy: performedBy,
		PerformedAt: time.Now(),
		IPAddress:   c.ClientIP(), // Should extract from gin context
	}
	h.db.WithContext(ctx).Create(log)
}
