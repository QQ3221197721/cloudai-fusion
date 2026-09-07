package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SetupRoutes registers all Red Team platform REST APIs.
func SetupRoutes(r *gin.Engine, db *gorm.DB) {
	api := r.Group("/api/v1")

	// ========== Authentication Endpoints ==========
	auth := api.Group("/auth")
	{
		auth.POST("/login", loginHandler(db))       // Public endpoint
		auth.POST("/logout", requireAuth(authHandler(db))(logoutHandler(db)))              // Protected
		auth.POST("/refresh", refreshHandler(db))    // Refresh tokens
	}

	// ========== Dashboard Statistics ==========
	stats := api.Group("/stats")
	stats.Use(requireAuth(authHandler(db)))
	{
		stats.GET("/dashboard", getDashboardStatsHandler(db))
		stats.GET("/findings-by-severity", getFindingsBySeverityHandler(db))
		stats.GET("/recent-activity", getRecentActivityHandler(db))
		stats.GET("/security-metrics", getSecurityMetricsHandler(db))
	}

	// ========== Work Order Management ==========
	workOrders := api.Group("/work-orders")
	workOrders.Use(requireAuth(authHandler(db)))
	{
		workOrders.POST("/", createWorkOrderHandler(db))
		workOrders.GET("/", listWorkOrderHandler(db))
		workOrders.GET("/:id", getWorkOrderHandler(db))
		workOrders.PUT("/:id", updateWorkOrderHandler(db))
		workOrders.DELETE("/:id", deleteWorkOrderHandler(db))
		workOrders.POST("/:id/approve", approveWorkOrderHandler(db))
		workOrders.POST("/:id/reject", rejectWorkOrderHandler(db))
		workOrders.GET("/:id/audit", getAuditTrailHandler(db))
	}

	// ========== Attack Campaigns ==========
	campaigns := api.Group("/campaigns")
	campaigns.Use(requireAuth(authHandler(db)))
	{
		campaigns.POST("/", createCampaignHandler(db))
		campaigns.GET("/", listCampaignHandler(db))
		campaigns.GET("/:id", getCampaignHandler(db))
		campaigns.POST("/:id/start", startCampaignHandler(db))
		campaigns.POST("/:id/pause", pauseCampaignHandler(db))
		campaigns.POST("/:id/cancel", cancelCampaignHandler(db))
	}

	// ========== Vulnerability Findings ==========
	findings := api.Group("/findings")
	findings.Use(requireAuth(authHandler(db)))
	{
		findings.POST("/", createFindingHandler(db))
		findings.GET("/", listFindingHandler(db))
		findings.GET("/:id", getFindingHandler(db))
		findings.PUT("/:id", updateFindingHandler(db))
		findings.DELETE("/:id", deleteFindingHandler(db))
		findings.POST("/batch-update", batchUpdateFindingsHandler(db))
		findings.GET("/export", exportReportHandler(db))
	}

	// Health check and ping
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "red-team-platform"})
	})
}

// Helper: JWT Auth Handler singleton
func authHandler(db *gorm.DB) *JWTAuthHandler {
	handler, _ := NewJWTAuthHandler(db, "")
	return handler
}

// Middleware: Require valid JWT token
func requireAuth(handler *JWTAuthHandler) gin.HandlerFunc {
	return handler.Middleware()
}

// Split authorization header
func splitAuthHeader(header string) []string {
	return strings.SplitN(strings.TrimSpace(header), " ", 2)
}

// ====== Auth Handlers =======

func loginHandler(db *gorm.DB) gin.HandlerFunc {
	handler := authHandler(db)
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		response, err := handler.Login(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		c.JSON(http.StatusOK, response)
	}
}

func refreshHandler(db *gorm.DB) gin.HandlerFunc {
	handler := authHandler(db)
	return func(c *gin.Context) {
		var req struct {
			RefreshToken string `json:"refresh_token" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		response, err := handler.RefreshToken(c.Request.Context(), req.RefreshToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, response)
	}
}

func logoutHandler(db *gorm.DB) gin.HandlerFunc {
	handler := authHandler(db)
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		parts := splitAuthHeader(authHeader)
		if len(parts) != 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token format"})
			return
		}

		if err := handler.Logout(c.Request.Context(), parts[1]); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.h{"message": "logged out successfully"})
	}
}

func getDashboardStatsHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewStatsHandler(db)
	return handler.GetDashboardStats
}

func getFindingsBySeverityHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewStatsHandler(db)
	return handler.GetFindingsBySeverity
}

func getRecentActivityHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewStatsHandler(db)
	return handler.GetRecentActivity
}

func getSecurityMetricsHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewStatsHandler(db)
	return handler.GetSecurityMetrics
}

// ====== Work Order Handlers =======

func createWorkOrderHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return handler.Create
}

func listWorkOrderHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return handler.List
}

func getWorkOrderHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return handler.Get
}

func updateWorkOrderHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return handler.Update
}

func deleteWorkOrderHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return handler.Delete
}

func approveWorkOrderHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return handler.Approve
}

func rejectWorkOrderHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return func(c *gin.Context) {
		reason := c.PostForm("reason")
		if reason == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "rejection reason required"})
			return
		}
		c.Set("reason", reason)
		handler.Update(c)
	}
}

func getAuditTrailHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewWorkOrderHandler(db)
	return handler.Audit
}

// ====== Campaign Handlers ========

func createCampaignHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewCampaignHandler(db)
	return handler.Create
}

func listCampaignHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewCampaignHandler(db)
	return handler.List
}

func getCampaignHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewCampaignHandler(db)
	return handler.Get
}

func startCampaignHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewCampaignHandler(db)
	return handler.Start
}

func pauseCampaignHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewCampaignHandler(db)
	return handler.Pause
}

func cancelCampaignHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewCampaignHandler(db)
	return handler.Cancel
}

// ====== Finding Handlers ========

func createFindingHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewFindingsHandler(db)
	return handler.Create
}

func listFindingHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewFindingsHandler(db)
	return handler.List
}

func getFindingHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewFindingsHandler(db)
	return handler.Get
}

func updateFindingHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewFindingsHandler(db)
	return handler.Update
}

func deleteFindingHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewFindingsHandler(db)
	return handler.Delete
}

func batchUpdateFindingsHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewFindingsHandler(db)
	return handler.BatchUpdateStatus
}

func exportReportHandler(db *gorm.DB) gin.HandlerFunc {
	handler := NewFindingsHandler(db)
	return handler.ExportReport
}
