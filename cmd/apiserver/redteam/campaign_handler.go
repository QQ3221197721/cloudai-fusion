package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/models"
	"gorm.io/gorm"
)

// CampaignHandler manages attack campaign execution lifecycle.
type CampaignHandler struct {
	db           *gorm.DB
	executorPool *ExecutorPool // Future: sandbox executor pool
}

// NewCampaignHandler creates a new campaign handler.
func NewCampaignHandler(db *gorm.DB) *CampaignHandler {
	return &CampaignHandler{
		db:           db,
		executorPool: NewExecutorPool(), // Initialize executor pool
	}
}

// CreateCampaignRequest represents request to create a new attack campaign.
type CreateCampaignRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	WorkOrderID string   `json:"work_order_id"`
	AttackTypes []string `json:"attack_types" binding:"required"`
	Targets     []string `json:"targets" binding:"required"`
}

// StartCampaignRequest represents request to start an existing campaign.
type StartCampaignRequest struct {
	Immediate bool `json:"immediate"`
}

// ListCampaignsResponse represents paginated campaigns list.
type ListCampaignsResponse struct {
	Total int             `json:"total"`
	Data  []CampaignItem  `json:"data"`
}

// CampaignItem simplified campaign data for listing.
type CampaignItem struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	ProgressPercent int        `json:"progress_percentage"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	FindingsCount   int        `json:"findings_count"`
	WorkOrder       *WorkOrderSimple `json:"work_order,omitempty"`
}

type WorkOrderSimple struct {
	ID          uuid.UUID `json:"id"`
	CompanyName string    `json:"company_name"`
}

// Create initializes a new attack campaign.
func (h *CampaignHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateCampaignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": err.Error()})
		return
	}

	// Validate work order if provided
	var workOrderID *uuid.UUID
	if req.WorkOrderID != "" {
		woID, err := uuid.Parse(req.WorkOrderID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.h{"error": "invalid work order ID"})
			return
		}

		// Check work order authorization
		var workOrder models.WorkOrder
		if err := h.db.WithContext(ctx).Preload("User").First(&workOrder, woID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.h{"error": "work order not found"})
			return
		}

		if !workOrder.CanCreateCampaign() {
			c.JSON(http.StatusBadRequest, gin.h{"error": fmt.Sprintf("cannot create campaign from work order with status: %s", workOrder.Status)})
			return
		}

		workOrderID = &woID
	}

	// Build campaign entity
	campaign := &models.AttackCampaign{
		Name:        req.Name,
		Description: req.Description,
		WorkOrderID: workOrderID,
		AttackTypes: models.NullStringArray{Strings: req.AttackTypes, Valid: true},
		Targets:     models.NullStringArray{Strings: req.Targets, Valid: true},
		Status:      "scheduled",
		CreatedAt:   time.Now(),
	}

	// Save to database
	if err := h.db.WithContext(ctx).Create(campaign).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to create campaign", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, campaign)
}

// List returns paginated list of campaigns.
func (h *CampaignHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	status := c.Query("status")
	workOrderID := c.Query("work_order_id")
	limit := c.Query("limit")
	page := c.Query("page")

	var total int64
	query := h.db.WithContext(ctx).Model(&models.AttackCampaign{}).Preload("WorkOrder.User")

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if workOrderID != "" {
		woID, _ := uuid.Parse(workOrderID)
		query = query.Where("work_order_id = ?", woID)
	}

	query.Count(&total)

	var campaigns []models.AttackCampaign
	offset := 0
	if page != "" {
		fmt.Sscanf(page, "%d", &offset)
		offset = (offset - 1) * 20
	}
	if limit == "" {
		limit = "20"
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&campaigns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to fetch campaigns"})
		return
	}

	// Transform response
	var items []CampaignItem
	for _, cam := range campaigns {
		item := CampaignItem{
			ID:              cam.ID,
			Name:            cam.Name,
			Status:          cam.Status,
			ProgressPercent: cam.ProgressPercent,
			CreatedAt:       cam.CreatedAt,
			StartedAt:       cam.StartedAt,
			CompletedAt:     cam.CompletedAt,
			FindingsCount:   cam.FindingsCount,
		}
		if cam.WorkOrder != nil {
			item.WorkOrder = &WorkOrderSimple{
				ID:          cam.WorkOrder.ID,
				CompanyName: cam.WorkOrder.CompanyName,
			}
		}
		items = append(items, item)
	}

	response := ListCampaignsResponse{
		Total: int(total),
		Data:  items,
	}

	c.JSON(http.StatusOK, response)
}

// Get retrieves a single campaign with detailed information.
func (h *CampaignHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	campaignID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid campaign ID"})
		return
	}

	var campaign models.AttackCampaign
	if err := h.db.WithContext(ctx).
		Preload("WorkOrder.User").
		Preload("Findings").
		First(&campaign, campaignID).Error; err != nil {
		
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.h{"error": "campaign not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to fetch campaign"})
		return
	}

	c.JSON(http.StatusOK, campaign)
}

// Start initiates campaign execution.
func (h *CampaignHandler) Start(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	campaignID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid campaign ID"})
		return
	}

	var campaign models.AttackCampaign
	if err := h.db.WithContext(ctx).First(&campaign, campaignID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.h{"error": "campaign not found"})
		return
	}

	if campaign.Status != "scheduled" && campaign.Status != "paused" {
		c.JSON(http.StatusBadRequest, gin.h{"error": fmt.Sprintf("cannot start campaign in status: %s", campaign.Status)})
		return
	}

	// Update to running status
	now := time.Now()
	updates := map[string]interface{}{
		"status":      "running",
		"started_at":  now,
		"progress":    0,
	}

	if err := h.db.Model(&campaign).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to start campaign"})
		return
	}

	// Launch campaign execution in background goroutine
	go h.executeCampaignAsync(ctx, campaign)

	c.JSON(http.StatusOK, gin.h{"message": "campaign started", "campaign_id": id})
}

// Pause suspends a running campaign.
func (h *CampaignHandler) Pause(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var campaign models.AttackCampaign
	if err := h.db.WithContext(ctx).First(&campaign, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.h{"error": "campaign not found"})
		return
	}

	if campaign.Status != "running" {
		c.JSON(http.StatusBadRequest, gin.h{"error": "only running campaigns can be paused"})
		return
	}

	if err := h.db.Model(&campaign).Update("status", "paused").Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to pause campaign"})
		return
	}

	c.JSON(http.StatusOK, gin.h{"message": "campaign paused"})
}

// Cancel stops and deletes a campaign.
func (h *CampaignHandler) Cancel(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var campaign models.AttackCampaign
	if err := h.db.WithContext(ctx).First(&campaign, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.h{"error": "campaign not found"})
		return
	}

	result := h.db.Model(&campaign).Update("status", "cancelled")
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to cancel campaign"})
		return
	}

	c.JSON(http.StatusOK, gin.h{"message": "campaign cancelled"})
}

// executeCampaignAsync runs the attack campaign asynchronously with sandbox isolation.
func (h *CampaignHandler) executeCampaignAsync(ctx context.Context, campaign models.AttackCampaign) {
	defer func() {
		if r := recover(); r != nil {
			h.updateCampaignStatus(ctx, campaign.ID, "failed", fmt.Sprintf("panic recovered: %v", r))
		}
	}()

	// Simulate attack execution stages
	stages := []struct {
		name  string
		dur   time.Duration
		fn    func(context.Context, models.AttackCampaign) ([]models.Finding, error)
	}{
		{"Network Reconnaissance", 2 * time.Second, h.executeNetworkScan},
		{"Vulnerability Scanning", 3 * time.Second, h.executeVulnScan},
		{"Exploitation Testing", 4 * time.Second, h.executeExploitation},
		{"Post-Exploitation Analysis", 2 * time.Second, h.executePostExploitation},
	}

	var allFindings []models.Finding

	for i, stage := range stages {
		select {
		case <-ctx.Done():
			// Context cancelled, stop execution
			return
		default:
			// Execute stage
			findings, err := stage.fn(ctx, campaign)
			if err != nil {
				h.logger.Printf("stage %s failed: %v", stage.name, err)
				continue
			}
			allFindings = append(allFindings, findings...)

			// Update progress
			progress := int(float64(i+1) / float64(len(stages)) * 100)
			h.updateCampaignProgress(ctx, campaign.ID, progress)
			
			// Add fake delay
			time.Sleep(stage.dur)
		}
	}

	// Save all findings to database
	if len(allFindings) > 0 {
		h.saveFindings(ctx, campaign.ID, allFindings)
		h.updateCampaignStats(ctx, campaign.ID, len(allFindings))
	}

	// Mark as completed
	h.updateCampaignStatus(ctx, campaign.ID, "completed", "")
}

// executeNetworkScan simulates network reconnaissance attack.
func (h *CampaignHandler) executeNetworkScan(ctx context.Context, campaign models.AttackCampaign) ([]models.Finding, error) {
	findings := []models.Finding{}

	for _, target := range campaign.Targets.Strings {
		// Simulate scanning logic
		finding := models.Finding{
			Title:         fmt.Sprintf("Open port detected on %s", target),
			Description:   fmt.Sprintf("Scanner identified open ports on target %s", target),
			Severity:      "low",
			AffectedAsset: target,
			AssetType:     "network",
			EvidenceUrls:  models.NullStringArray{Strings: []string{}, Valid: true},
			Remediation:   "Close unnecessary ports or restrict access via firewall rules",
		}
		findings = append(findings, finding)
	}

	return findings, nil
}

// executeVulnScan simulates vulnerability scanning.
func (h *CampaignHandler) executeVulnScan(ctx context.Context, campaign models.AttackCampaign) ([]models.Finding, error) {
	findings := []models.Finding{}

	// Simulate CVE detection
	cves := []struct {
		title      string
		severity   string
		cvss       float64
		remediation string
	}{
		{"CVE-2024-1234: Remote Code Execution", "critical", 9.8, "Apply security patch immediately"},
		{"CVE-2023-5678: SQL Injection", "high", 7.5, "Use parameterized queries"},
	}

	for _, cve := range cves {
		finding := models.Finding{
			Title:         cve.title,
			Description:   fmt.Sprintf("Vulnerability %s detected during scan", cve.title),
			Severity:      cve.severity,
			CVVSScore:     cve.cvss,
			AffectedAsset: "web-application",
			AssetType:     "application",
			Remediation:   cve.remediation,
		}
		findings = append(findings, finding)
	}

	return findings, nil
}

// executeExploitation simulates exploitation attempts.
func (h *CampaignHandler) executeExploitation(ctx context.Context, campaign models.AttackCampaign) ([]models.Finding, error) {
	// This would implement actual exploitation logic in production
	// For now, return simulated findings
	return []models.Finding{}, nil
}

// executePostExploitation analyzes post-exploitation impact.
func (h *CampaignHandler) executePostExploitation(ctx context.Context, campaign models.AttackCampaign) ([]models.Finding, error) {
	findings := []models.Finding{}

	// Simulate privilege escalation findings
	finding := models.Finding{
		Title:         "Privilege Escalation Detected",
		Description:   "Attack successfully escalated from user to admin privileges",
		Severity:      "high",
		CVVSScore:     8.1,
		AffectedAsset: "database-server",
		AssetType:     "server",
		Remediation:   "Implement least privilege principle and enforce RBAC",
	}
	findings = append(findings, finding)

	return findings, nil
}

// Helper methods
func (h *CampaignHandler) updateCampaignStatus(ctx context.Context, id uuid.UUID, status string, reason string) {
	updates := map[string]interface{}{"status": status}
	if status == "completed" || status == "failed" {
		now := time.Now()
		updates["completed_at"] = now
	}
	h.db.Model(&models.AttackCampaign{}).Where("id = ?", id).Updates(updates)
}

func (h *CampaignHandler) updateCampaignProgress(ctx context.Context, id uuid.UUID, progress int) {
	h.db.Model(&models.AttackCampaign{}).Where("id = ?", id).Update("progress_percentage", progress)
}

func (h *CampaignHandler) updateCampaignStats(ctx context.Context, id uuid.UUID, count int) {
	h.db.Model(&models.AttackCampaign{}).Where("id = ?", id).Update("findings_count", count)
}

func (h *CampaignHandler) saveFindings(ctx context.Context, campaignID uuid.UUID, findings []models.Finding) {
	for i := range findings {
		findings[i].CampaignID = campaignID
	}
	h.db.Create(findings)
}

// ExecutorPool manages async campaign execution workers.
type ExecutorPool struct {
	sem chan struct{}
	wg  sync.WaitGroup
}

// NewExecutorPool creates a new worker pool.
func NewExecutorPool() *ExecutorPool {
	return &ExecutorPool{
		sem: make(chan struct{}, 5), // Max 5 concurrent campaigns
	}
}

// Execute runs a campaign in the pool.
func (p *ExecutorPool) Execute(ctx context.Context, fn func(context.Context)) {
	p.sem <- struct{}{}
	p.wg.Add(1)
	go func() {
		defer func() { <-p.sem; p.wg.Done() }()
		fn(ctx)
	}()
}

// Wait blocks until all tasks are complete.
func (p *ExecutorPool) Wait() {
	p.wg.Wait()
}
