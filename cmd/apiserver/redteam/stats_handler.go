package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/models"
	"gorm.io/gorm"
)

// StatsHandler provides real-time dashboard statistics from production database.
type StatsHandler struct {
	db *gorm.DB
}

// NewStatsHandler creates a new statistics handler.
func NewStatsHandler(db *gorm.DB) *StatsHandler {
	return &StatsHandler{db: db}
}

// DashboardStats represents live metrics from PostgreSQL.
type DashboardStats struct {
	TotalScans         int       `json:"total_scans"`
	ActiveCampaigns    int       `json:"active_campaigns"`
	CriticalFindings   int       `json:"critical_findings"`
	HighFindings       int       `json:"high_findings"`
	PendingOrders      int       `json:"pending_orders"`
	ApprovedOrders     int       `json:"approved_orders"`
	ComplianceScore    float64   `json:"compliance_score"`
	LastUpdated        time.Time `json:"last_updated"`
}

// GetDashboardStats returns live statistics from the database using parallel queries.
func (h *StatsHandler) GetDashboardStats(c *gin.Context) {
	ctx := c.Request.Context()
	var stats DashboardStats
	stats.LastUpdated = time.Now()

	// Use WaitGroup to execute queries in parallel
	var wg sync.WaitGroup
	var criticalCount, highCount, activeCount, pendingCount, approvedCount, totalScans int
	var err error

	// Query 1: Active campaigns (running status)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = h.db.WithContext(ctx).Model(&models.AttackCampaign{}).Where("status = ?", "running").Count(&activeCount).Error
	}()

	// Query 2: Critical findings
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = h.db.WithContext(ctx).Model(&models.Finding{}).Where("severity = ?", "critical").Count(&criticalCount).Error
	}()

	// Query 3: High findings
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = h.db.WithContext(ctx).Model(&models.Finding{}).Where("severity = ?", "high").Count(&highCount).Error
	}()

	// Query 4: Pending work orders
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = h.db.WithContext(ctx).Model(&models.WorkOrder{}).Where("status = ?", "pending").Count(&pendingCount).Error
	}()

	// Query 5: Approved work orders
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = h.db.WithContext(ctx).Model(&models.WorkOrder{}).Where("status = ?", "approved").Count(&approvedCount).Error
	}()

	// Query 6: Total scans (campaigns created in last 30 days)
	wg.Add(1)
	go func() {
		defer wg.Done()
		thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
		err = h.db.WithContext(ctx).Model(&models.AttackCampaign{}).Where("created_at > ?", thirtyDaysAgo).Count(&totalScans).Error
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	// Assign results
	stats.ActiveCampaigns = activeCount
	stats.CriticalFindings = criticalCount
	stats.HighFindings = highCount
	stats.PendingOrders = pendingCount
	stats.ApprovedOrders = approvedCount
	stats.TotalScans = totalScans

	// Calculate compliance score (weighted security posture)
	stats.ComplianceScore = h.calculateComplianceScore(ctx, criticalCount, highCount)

	c.JSON(http.StatusOK, stats)
}

// calculateComplianceScore computes security posture percentage based on findings.
func (h *StatsHandler) calculateComplianceScore(ctx context.Context, criticalCount, highCount int) float64 {
	if criticalCount == 0 && highCount == 0 {
		return 100.0
	}

	// Weighted formula: 70% weight for critical, 30% for high vulnerabilities
	totalIssues := float64(criticalCount + highCount)
	riskWeight := (float64(criticalCount)*0.7 + float64(highCount)*0.3) / totalIssues
	
	score := 100.0 - (riskWeight * 100.0)
	
	// Round to 2 decimal places
	return float64(int(score*100)) / 100
}

// GetFindingsBySeverity returns breakdown of vulnerabilities by severity level.
func (h *StatsHandler) GetFindingsBySeverity(c *gin.Context) {
	ctx := c.Request.Context()

	type SeverityCount struct {
		Severity string `json:"severity"`
		Count    int    `json:"count"`
	}

	var counts []SeverityCount
	severities := []string{"critical", "high", "medium", "low", "informational"}

	for _, severity := range severities {
		var count int
		if err := h.db.WithContext(ctx).Model(&models.Finding{}).Where("severity = ?", severity).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database query failed"})
			return
		}
		counts = append(counts, SeverityCount{Severity: severity, Count: count})
	}

	c.JSON(http.StatusOK, gin.H{"findings_by_severity": counts})
}

// GetRecentActivity returns recent security assessments and activities.
func (h *StatsHandler) GetRecentActivity(c *gin.Context) {
	ctx := c.Request.Context()
	limit := c.Query("limit")
	if limit == "" {
		limit = "10"
	}

	var campaigns []models.AttackCampaign
	if err := h.db.WithContext(ctx).Preload("WorkOrder.User").
		Joins("LEFT JOIN redteam_work_orders ON attack_campaigns.work_order_id = redteam_work_orders.id").
		ORDER("attack_campaigns.created_at DESC").Limit(limit).Find(&campaigns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch recent activity"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"recent_campaigns": campaigns})
}

// GetSecurityMetrics returns detailed security metrics over time.
func (h *StatsHandler) GetSecurityMetrics(c *gin.Context) {
	ctx := c.Request.Context()
	trend := c.Query("trend") // 7d, 30d, 90d
	if trend == "" {
		trend = "30d"
	}

	var days int
	switch trend {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		days = 30
	}

	since := time.Now().AddDate(0, 0, -days)

	// Get daily campaign creation trends
	type DailyTrend struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}

	var trends []DailyTrend
	err := h.db.WithContext(ctx).
		Model(&models.AttackCampaign{}).
		Select("DATE(created_at) as date, COUNT(*) as count").
		Where("created_at > ?", since).
		GROUP("DATE(created_at)").
		ORDER("date ASC").
		Scan(&trends).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch trends"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"trend_period": trend,
		"daily_trends": trends,
	})
}
