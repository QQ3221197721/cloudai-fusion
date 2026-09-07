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

// FindingsHandler manages vulnerability finding lifecycle.
type FindingsHandler struct {
	db *gorm.DB
}

// NewFindingsHandler creates a new findings handler.
func NewFindingsHandler(db *gorm.DB) *FindingsHandler {
	return &FindingsHandler{db: db}
}

// CreateFindingRequest represents request to create a vulnerability finding.
type CreateFindingRequest struct {
	Title         string   `json:"title" binding:"required"`
	Description   string   `json:"description" binding:"required"`
	Severity      string   `json:"severity" binding:"required"`
	CVVSScore     float64  `json:"cvss_score,omitempty"`
	AffectedAsset string   `json:"affected_asset" binding:"required"`
	AssetType     string   `json:"asset_type,omitempty"`
	EvidenceURLs  []string `json:"evidence_urls"`
	Remediation   string   `json:"remediation" binding:"required"`
	References    []string `json:"references,omitempty"`
}

// UpdateFindingRequest represents request to update a finding.
type UpdateFindingRequest struct {
	Verified  *bool   `json:"verified"`
	Patched   *bool   `json:"patched"`
	Title     *string `json:"title"`
	Remediation *string `json:"remediation"`
}

// ListFindingsResponse represents paginated findings list.
type ListFindingsResponse struct {
	Total int             `json:"total"`
	Data  []FindingItem   `json:"data"`
}

// FindingItem simplified finding data for listing.
type FindingItem struct {
	ID            uuid.UUID  `json:"id"`
	Title         string     `json:"title"`
	Severity      string     `json:"severity"`
	CVVSScore     float64    `json:"cvss_score,omitempty"`
	AffectedAsset string     `json:"affected_asset"`
	CreatedAt     time.Time  `json:"created_at"`
	Verified      bool       `json:"verified"`
	Patched       bool       `json:"patched"`
	Campaign      CampaignLink `json:"campaign,omitempty"`
}

type CampaignLink struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Create adds a new vulnerability finding.
func (h *FindingsHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateFindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": err.Error()})
		return
	}

	// Parse campaign ID from query param
	campaignIDStr := c.Query("campaign_id")
	if campaignIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.h{"error": "campaign_id is required"})
		return
	}

	campaignID, err := uuid.Parse(campaignIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid campaign ID"})
		return
	}

	// Check if campaign exists
	var campaign models.AttackCampaign
	if err := h.db.WithContext(ctx).First(&campaign, campaignID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.h{"error": "campaign not found"})
		return
	}

	// Build finding entity
	finding := &models.Finding{
		CampaignID:    campaignID,
		Title:         req.Title,
		Description:   req.Description,
		Severity:      req.Severity,
		CVVSScore:     req.CVVSScore,
		AffectedAsset: req.AffectedAsset,
		AssetType:     req.AsssetType,
		EvidenceUrls:  models.NullStringArray{Strings: req.EvidenceURLs, Valid: true},
		Remediation:   req.Remediation,
		References:    models.NullStringArray{Strings: req.References, Valid: len(req.References) > 0},
		DiscoveredAt:  time.Now(),
	}

	// Validate before database insert
	if err := finding.ValidateFinding(); err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": err.Error()})
		return
	}

	// Create in database
	if err := h.db.WithContext(ctx).Create(finding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to create finding", "details": err.Error()})
		return
	}

	// Increment campaign findings count
	h.incrementFindingsCount(ctx, campaignID)

	c.JSON(http.StatusCreated, finding)
}

// List returns paginated list of findings with filtering.
func (h *FindingsHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	status := c.Query("status")
	severity := c.Query("severity")
	campaignID := c.Query("campaign_id")
	page := c.Query("page")
	perPage := c.Query("per_page")

	if page == "" {
		page = "1"
	}
	if perPage == "" {
		perPage = "20"
	}

	var total int64
	query := h.db.WithContext(ctx).Model(&models.Finding{}).Preload("Campaign")

	if status != "" {
		switch status {
		case "verified":
			query = query.Where("verified = ?", true)
		case "unverified":
			query = query.Where("verified = ?", false)
		case "patched":
			query = query.Where("patched = ?", true)
		case "unpatched":
			query = query.Where("patched = ?", false)
		}
	}
	if severity != "" {
		query = query.Where("severity = ?", severity)
	}
	if campaignID != "" {
		woID, _ := uuid.Parse(campaignID)
		query = query.Where("campaign_id = ?", woID)
	}

	query.Count(&total)

	var findings []models.Finding
	offset := 0
	fmt.Sscanf(page, "%d", &offset)
	offset = (offset - 1) * 20

	fmt.Sscanf(perPage, "%d", &perPage)

	if err := query.Order("discovered_at DESC").Offset(offset).Limit(perPage).Find(&findings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to fetch findings"})
		return
	}

	// Transform response
	var items []FindingItem
	for _, f := range findings {
		item := FindingItem{
			ID:            f.ID,
			Title:         f.Title,
			Severity:      f.Severity,
			CVVSScore:     f.CVVSScore,
			AffectedAsset: f.AffectedAsset,
			CreatedAt:     f.DiscoveredAt,
			Verified:      f.Verified,
			Patched:       f.Patched,
		}
		if f.Campaign.ID != uuid.Nil {
			item.Campaign = CampaignLink{
				ID:   f.Campaign.ID,
				Name: f.Campaign.Name,
			}
		}
		items = append(items, item)
	}

	response := ListFindingsResponse{
		Total: int(total),
		Data:  items,
	}

	c.JSON(http.StatusOK, response)
}

// Get retrieves a single finding by ID.
func (h *FindingsHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	findingID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid finding ID"})
		return
	}

	var finding models.Finding
	if err := h.db.WithContext(ctx).
		Preload("Campaign.WorkOrder.User").
		First(&finding, findingID).Error; err != nil {
		
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.h{"error": "finding not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to fetch finding"})
		return
	}

	c.JSON(http.StatusOK, finding)
}

// Update modifies an existing finding.
func (h *FindingsHandler) Update(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	findingID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid finding ID"})
		return
	}

	var finding models.Finding
	if err := h.db.WithContext(ctx).First(&finding, findingID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.h{"error": "finding not found"})
		return
	}

	var req UpdateFindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": err.Error()})
		return
	}

	updates := make(map[string]interface{})

	if req.Verified != nil {
		updates["verified"] = *req.Verified
	}
	if req.Patched != nil {
		updates["patched"] = *req.Patched
	}
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.Remediation != nil {
		updates["remediation"] = *req.Remediation
	}

	if err := h.db.Model(&finding).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to update finding"})
		return
	}

	var updatedFinding models.Finding
	h.db.WithContext(ctx).First(&updatedFinding, findingID)

	c.JSON(http.StatusOK, updatedFinding)
}

// Delete removes a finding.
func (h *FindingsHandler) Delete(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	findingID, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": "invalid finding ID"})
		return
	}

	result := h.db.WithContext(ctx).Delete(&models.Finding{}, findingID)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to delete finding"})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.h{"error": "finding not found"})
		return
	}

	// Decrement campaign findings count
	var finding models.Finding
	h.db.First(&finding, findingID)
	h.decrementFindingsCount(ctx, finding.CampaignID)

	c.JSON(http.StatusOK, gin.h{"message": "finding deleted successfully"})
}

// BatchUpdateStatus updates multiple findings status at once.
func (h *FindingsHandler) BatchUpdateStatus(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		FindingIDs []string `json:"finding_ids" binding:"required"`
		Verified   *bool    `json:"verified"`
		Patched    *bool    `json:"patched"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.h{"error": err.Error()})
		return
	}

	if (req.Verified == nil && req.Patched == nil) || len(req.FindingIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.h{"error": "at least one field to update and finding IDs are required"})
		return
	}

	updates := make(map[string]interface{})
	if req.Verified != nil {
		updates["verified"] = *req.Verified
	}
	if req.Patched != nil {
		updates["patched"] = *req.Patched
	}

	var findingIDs []uuid.UUID
	for _, idStr := range req.FindingIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		findingIDs = append(findingIDs, id)
	}

	result := h.db.Model(&models.Finding{}).Where("id IN ?", findingIDs).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to update findings"})
		return
	}

	c.JSON(http.StatusOK, gin.h{
		"message":          fmt.Sprintf("%d findings updated", result.RowsAffected),
		"updated_count":    result.RowsAffected,
	})
}

// ExportReport generates a downloadable report of all findings.
func (h *FindingsHandler) ExportReport(c *gin.Context) {
	ctx := c.Request.Context()
	format := c.Query("format") // json, csv, pdf (simulated)
	campaignID := c.Query("campaign_id")

	var findings []models.Finding
	query := h.db.WithContext(ctx).Preload("Campaign").Model(&models.Finding{})

	if campaignID != "" {
		woID, _ := uuid.Parse(campaignID)
		query = query.Where("campaign_id = ?", woID)
	}

	if err := query.Find(&findings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.h{"error": "failed to export report"})
		return
	}

	switch format {
	case "json":
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", "attachment; filename=findings_report.json")
		c.JSON(http.StatusOK, findings)
	case "csv":
		c.Header("Content-Type", "text/csv")
		c.Header("Content-Disposition", "attachment; filename=findings_report.csv")
		c.Data(http.StatusOK, "text/csv", h.toCSV(findings))
	default:
		c.JSON(http.StatusBadRequest, gin.h{"error": "unsupported format"})
	}
}

// Helper methods
func (h *FindingsHandler) incrementFindingsCount(ctx context.Context, campaignID uuid.UUID) {
	h.db.Model(&models.AttackCampaign{}).Where("id = ?", campaignID).
		Update("findings_count", gorm.Expr("findings_count + 1"))
}

func (h *FindingsHandler) decrementFindingsCount(ctx context.Context, campaignID uuid.UUID) {
	h.db.Model(&models.AttackCampaign{}).Where("id = ?", campaignID).
		Update("findings_count", gorm.Expr("GREATEST(findings_count - 1, 0)"))
}

func (h *FindingsHandler) toCSV(findings []models.Finding) []byte {
	// Simple CSV generation for demonstration
	lines := []string{"ID,Title,Severity,CVSS,Affected Asset,Discovered At"}
	
	for _, f := range findings {
		line := fmt.Sprintf(`"%s","%s",%s,%f,"%s",%s`,
			f.ID.String(),
			f.Title,
			f.Severity,
			f.CVVSScore,
			f.AffectedAsset,
			f.DiscoveredAt.Format(time.RFC3339),
		)
		lines = append(lines, line)
	}
	
	return []byte(fmt.Sprintf("%s\n", join(lines, "\n")))
}

func join(strings []string, sep string) string {
	if len(strings) == 0 {
		return ""
	}
	result := strings[0]
	for _, s := range strings[1:] {
		result += sep + s
	}
	return result
}
