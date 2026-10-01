// Package api provides HTTP handlers for M42 Threat Intelligence Sharing Platform
package api

import (
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// STIX Cyber Observable types
type STIXObjectType string

const (
	STIXObjectIndicator       STIXObjectType = "indicator"
	STIXObjectMalware         STIXObjectType = "malware"
	STIXObjectCampaign        STIXObjectType = "campaign"
	STIXObjectThreatActor     STIXObjectType = "threat-actor"
	STIXObjectInfrastructure  STIXObjectType = "infrastructure"
)

// SharingTrustLevel defines trust level in sharing groups
type SharingTrustLevel int

const (
	TrustLevelLow    SharingTrustLevel = 1 // Limited sharing
	TrustLevelMedium SharingTrustLevel = 2 // Standard sharing
	TrustLevelHigh   SharingTrustLevel = 3 // Full intelligence sharing
	TrustLevelTopSecret SharingTrustLevel = 4 // Restricted high-level intel
)

// STIXIndicator represents threat indicator
type STIXIndicator struct {
	ID          string                 `json:"id"`
	Type        STIXObjectType         `json:"type" binding:"required"`
	Pattern     string                 `json:"pattern" binding:"required"` // STIX pattern syntax
	PatternType string                 `json:"patternType"`               // stix, sigma, suricata
	ValidFrom   time.Time              `json:"validFrom"`
	ValidTo     *time.Time             `json:"validTo,omitempty"`
	Labels      []string               `json:"labels"`
	Description string                 `json:"description"`
	Confidence  int                    `json:"confidence"` // 0-100
	Severity    string                 `json:"severity"`  // low, medium, high, critical
	Status      string                 `json:"status"`    // active, inactive, experimental
	Source      string                 `json:"source"`
	Context     map[string]any         `json:"context"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
}

// SharingGroup represents a threat intel sharing community
type SharingGroup struct {
	ID              string                `json:"id"`
	Name            string                `json:"name" binding:"required"`
	Description     string                `json:"description"`
	OrganizationID  string                `json:"organizationId"`
	TrustLevel      SharingTrustLevel     `json:"trustLevel" binding:"required"`
	Members         []MemberInfo          `json:"members"`
	IndicatorsShared int                  `json:"indicatorsShared"`
	Policies        SharingPolicies       `json:"policies"`
	LastSync        time.Time             `json:"lastSync"`
	Active          bool                  `json:"active"`
	CreatedAt       time.Time             `json:"createdAt"`
	UpdatedAt       time.Time             `json:"updatedAt"`
}

// MemberInfo represents group member
type MemberInfo struct {
	ID           string   `json:"id"`
	Organization string   `json:"organization"`
	Roles        []string `json:"roles"` // owner, admin, contributor, viewer
	JoinedAt     time.Time `json:"joinedAt"`
	LastActivity time.Time `json:"lastActivity"`
}

// SharingPolicies defines sharing rules
type SharingPolicies struct {
	AutoShareOutbound bool     `json:"autoShareOutbound"`
	RequireReviewInbound bool   `json:"requireReviewInbound"`
	AllowedTypes []string  `json:"allowedTypes"` // indicator types to share
	SensitiveLabels []string `json:"sensitiveLabels"` // labels requiring review
	SyncIntervalHours int    `json:"syncIntervalHours"`
}

// ThreatIntelFeed represents external threat intel source
type ThreatIntelFeed struct {
	ID             string            `json:"id"`
	Name           string            `json:"name" binding:"required"`
	URL            string            `json:"url" binding:"required"`
	AuthMethod     string            `json:"authMethod"` // api_key, oauth, basic, none
	Credentials    map[string]string `json:"credentials,omitempty"`
	FormatType     string            `json:"formatType"` // stix2.1, csv, json, xml
	UpdateInterval time.Duration     `json:"updateInterval"`
	Enabled        bool              `json:"enabled"`
	LastSync       *time.Time        `json:"lastSync,omitempty"`
	Error          string            `json:"error,omitempty"`
	IndicatorsImported int            `json:"indicatorsImported"`
}

// SharingSession represents an active sharing session between organizations
type SharingSession struct {
	ID              string            `json:"id"`
	GroupID         string            `json:"groupId"`
	PartnerOrg      string            `json:"partnerOrg"`
	Status          string            `json:"status"` // active, pending, closed
	Direction       string            `json:"direction"` // inbound, outbound, bidirectional
	StartedAt       time.Time         `json:"startedAt"`
	LastExchange    time.Time         `json:"lastExchange"`
	IndicatorsSent  int               `json:"indicatorsSent"`
	IndicatorsReceived int             `json:"indicatorsReceived"`
	Metadata        map[string]any    `json:"metadata"`
}

// INDicatorSharingStore interface for persistence
type INDicatorSharingStore interface {
	CreateIndicator(ind *STIXIndicator) error
	GetIndicator(id string) (*STIXIndicator, error)
	UpdateIndicator(id string, updates map[string]any) error
	DeleteIndicator(id string) error
	ListIndicators(filters map[string]any, limit, offset int) ([]STIXIndicator, error)
	
	CreateSharingGroup(group *SharingGroup) error
	GetSharingGroup(id string) (*SharingGroup, error)
	UpdateSharingGroup(id string, updates map[string]any) error
	DeleteSharingGroup(id string) error
	ListSharingGroups(orgID string) ([]SharingGroup, error)
	AddMember(groupID string, member MemberInfo) error
	RemoveMember(groupID, memberID string) error
	
	CreateFeed(feed *ThreatIntelFeed) error
	GetFeed(id string) (*ThreatIntelFeed, error)
	UpdateFeed(id string, updates map[string]any) error
	DeleteFeed(id string) error
	ListFeeds() ([]ThreatIntelFeed, error)
	TriggerFeedSync(feedID string) error
	
	CreateSession(session *SharingSession) error
	GetSession(id string) (*SharingSession, error)
	UpdateSession(id string, updates map[string]any) error
	CloseSession(id string) error
	ListSessions(groupID string) ([]SharingSession, error)
	
	BulkCreateIndicators(indicators []STIXIndicator) error
	BulkUpdateIndicators(updates map[string]map[string]any) error
	
	CountByType() (map[string]int, error)
	CountBySeverity() (map[string]int, error)
}

// ThreatIntelSharingHandler handles threat intel sharing requests
type ThreatIntelSharingHandler struct {
	store  INDicatorSharingStore
	ledger *evidence.Ledger
	logger *logrus.Logger
}

// NewThreatIntelSharingHandler creates new handler
func NewThreatIntelSharingHandler(
	store INDicatorSharingStore,
	ledger *evidence.Ledger,
	logger *logrus.Logger,
) *ThreatIntelSharingHandler {
	return &ThreatIntelSharingHandler{
		store:  store,
		ledger: ledger,
		logger: logger,
	}
}

// RegisterSharingRoutes registers all M42 routes
func RegisterSharingRoutes(router *echo.Echo, handler *ThreatIntelSharingHandler) {
	sharing := router.Group("/api/v1/threat-intel-sharing")
	{
		// Indicator Management
		sharing.POST("/indicators", handler.handleCreateIndicator)
		sharing.GET("/indicators", handler.handleListIndicators)
		sharing.GET("/indicators/:id", handler.handleGetIndicator)
		sharing.PUT("/indicators/:id", handler.handleUpdateIndicator)
		sharing.DELETE("/indicators/:id", handler.handleDeleteIndicator)
		sharing.POST("/indicators/bulk-import", handler.handleBulkImportIndicators)
		
		// Sharing Groups
		sharing.POST("/groups", handler.handleCreateSharingGroup)
		sharing.GET("/groups", handler.handleListSharingGroups)
		sharing.GET("/groups/:id", handler.handleGetSharingGroup)
		sharing.PUT("/groups/:id", handler.handleUpdateSharingGroup)
		sharing.DELETE("/groups/:id", handler.handleDeleteSharingGroup)
		
		// Group Memberships
		sharing.POST("/groups/:id/members", handler.handleAddMember)
		sharing.DELETE("/groups/:id/members/:memberId", handler.handleRemoveMember)
		
		// Threat Feeds
		sharing.POST("/feeds", handler.handleCreateFeed)
		sharing.GET("/feeds", handler.handleListFeeds)
		sharing.GET("/feeds/:id", handler.handleGetFeed)
		sharing.PUT("/feeds/:id", handler.handleUpdateFeed)
		sharing.DELETE("/feeds/:id", handler.handleDeleteFeed)
		sharing.POST("/feeds/:id/sync", handler.handleSyncFeed)
		
		// Sharing Sessions
		sharing.POST("/sessions", handler.handleCreateSession)
		sharing.GET("/sessions", handler.handleListSessions)
		sharing.GET("/sessions/:id", handler.handleGetSession)
		sharing.POST("/sessions/:id/exchange", handler.handleExchangeIndicators)
		sharing.POST("/sessions/:id/close", handler.handleCloseSession)
		
		// Analytics
		sharing.GET("/analytics/indicators", handler.handleGetIndicatorAnalytics)
		sharing.GET("/analytics/sharing-stats", handler.handleGetSharingStats)
	}
}

func (h *ThreatIntelSharingHandler) handleCreateIndicator(c echo.Context) error {
	var req struct {
		Type        STIXObjectType `json:"type" binding:"required"`
		Pattern     string         `json:"pattern" binding:"required"`
		PatternType string         `json:"patternType"`
		Labels      []string       `json:"labels"`
		Confidence  int            `json:"confidence"`
		Severity    string         `json:"severity"`
	}
	
	if err := c.BindJSON(&req); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	indicatorID := uuid.New().String()
	now := time.Now()
	
	indicator := &STIXIndicator{
		ID:          indicatorID,
		Type:        req.Type,
		Pattern:     req.Pattern,
		PatternType: req.PatternType,
		Labels:      req.Labels,
		Confidence:  req.Confidence,
		Severity:    req.Severity,
		Status:      "active",
		ValidFrom:   now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	
	if err := h.store.CreateIndicator(indicator); err != nil {
		h.logger.WithError(err).Error("Failed to create indicator")
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create indicator"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"indicator_id": indicatorID,
		"type":         req.Type,
		"actor":        c.GetString("user_id"),
	}).Info("Created STIX indicator")
	
	if h.ledger != nil {
		h.ledger.Attest(evidence.Receipt{
			Action:  "INDICATOR_CREATED",
			Subject: indicatorID,
			Actor:   c.GetString("user_id"),
		})
	}
	
	return c.JSON(http.StatusCreated, gin.H{
		"indicator": indicator,
		"message":   "indicator created successfully",
	})
}

func (h *ThreatIntelSharingHandler) handleListIndicators(c echo.Context) error {
	limit := 100
	offset := 0
	
	c.IntParam(c.QueryParam("limit"), &limit)
	c.IntParam(c.QueryParam("offset"), &offset)
	
	filters := make(map[string]any)
	if status := c.QueryParam("status"); status != "" {
		filters["status"] = status
	}
	if typeParam := c.QueryParam("type"); typeParam != "" {
		filters["type"] = typeParam
	}
	
	indicators, err := h.store.ListIndicators(filters, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list indicators"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"indicators": indicators,
		"total":      len(indicators),
		"limit":      limit,
		"offset":     offset,
	})
}

func (h *ThreatIntelSharingHandler) handleGetIndicator(c echo.Context) error {
	id := c.Param("id")
	
	indicator, err := h.store.GetIndicator(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, gin.H{"error": "indicator not found"})
	}
	
	return c.JSON(http.StatusOK, indicator)
}

func (h *ThreatIntelSharingHandler) handleUpdateIndicator(c echo.Context) error {
	id := c.Param("id")
	
	var updates map[string]any
	if err := c.BindJSON(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
	}
	
	updates["updated_at"] = time.Now()
	
	if err := h.store.UpdateIndicator(id, updates); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update indicator"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id": id,
		"message": "indicator updated successfully",
	})
}

func (h *ThreatIntelSharingHandler) handleDeleteIndicator(c echo.Context) error {
	id := c.Param("id")
	
	if err := h.store.DeleteIndicator(id); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete indicator"})
	}
	
	return c.JSON(http.StatusOK, gin.H{
		"id": id,
		"message": "indicator deleted successfully",
	})
}

func (h *ThreatIntelSharingHandler) handleBulkImportIndicators(c echo.Context) error {
	var indicators []struct {
		Type        string            `json:"type" binding:"required"`
		Pattern     string            `json:"pattern" binding:"required"`
		Labels      []string          `json:"labels"`
		Context     map[string]any    `json:"context"`
	}
	
	if err := c.BindJSON(&indicators); err != nil {
		return c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
	}
	
	stixIndicators := make([]STIXIndicator, len(indicators))
	for i, ind := range indicators {
		now := time.Now()
		stixIndicators[i] = STIXIndicator{
			ID:        uuid.New().String(),
			Type:      STIXObjectType(ind.Type),
			Pattern:   ind.Pattern,
			Labels:    ind.Labels,
			Context:   ind.Context,
			Status:    "active",
			ValidFrom: now,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	
	if err := h.store.BulkCreateIndicators(stixIndicators); err != nil {
		return c.JSON(http.StatusInternalServerError, gin.H{"error": "bulk import failed"})
	}
	
	h.logger.WithFields(logrus.Fields{
		"count": len(stixIndicators),
		"action": "bulk_import_indicators",
	}).Info("Bulk imported STIX indicators")
	
	return c.JSON(http.StatusOK, gin.H{
		"imported": len(stixIndicators),
		"message": "indicators imported successfully",
	})
}

// Placeholder implementations for remaining handlers...
// In production, implement full CRUD operations with evidence attestation

func (h *ThreatIntelSharingHandler) handleCreateSharingGroup(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "M42 implementation placeholder"})
}

func (h *ThreatIntelSharingHandler) handleListSharingGroups(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleGetSharingGroup(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleUpdateSharingGroup(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleDeleteSharingGroup(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleAddMember(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleRemoveMember(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleCreateFeed(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleListFeeds(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleGetFeed(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleUpdateFeed(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleDeleteFeed(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleSyncFeed(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleCreateSession(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleListSessions(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleGetSession(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleExchangeIndicators(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleCloseSession(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleGetIndicatorAnalytics(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}

func (h *ThreatIntelSharingHandler) handleGetSharingStats(c echo.Context) error {
	return c.JSON(http.StatusOK, gin.H{"message": "placeholder"})
}
