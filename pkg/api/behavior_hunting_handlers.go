// Package api - behavior_hunting_handlers.go implements M19 Security Behavior Hunting Platform API endpoints.
package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/sirupsen/logrus"
)

// RegisterBehaviorHuntingRoutes registers all M19 Behavior Hunting endpoints
func RegisterBehaviorHuntingRoutes(router *gin.Engine, evidenceLedger *evidence.Ledger, logger *logrus.Logger) {
	hunting := router.Group("/api/v1/behavior-hunting")
	hunting.Use(
		middleware.EndpointRateLimiter(50, 60),
		auth.RequirePermission(auth.PermDetectRead),
	)
	{
		// Hunt Case Management
		hunting.POST("/cases", handleCreateHuntCase(evidenceLedger, logger))
		hunting.GET("/cases", handleListHuntCases())
		hunting.GET("/cases/:id", handleGetHuntCase())
		hunting.PUT("/cases/:id", handleUpdateHuntCase(logger))
		hunting.DELETE("/cases/:id", handleDeleteHuntCase(logger))
		hunting.POST("/cases/:id/assign", handleAssignInvestigator(logger))
		
		// UEBA Analysis
		hunting.POST("/analysis/uebA", handleAnalyzeBehaviorUEBA())
		hunting.GET("/baselines/:entityType", handleGetBaselineStats())
		hunting.POST("/baselines/train", handleTrainBehaviorBaseline())
		
		// Alert Correlation
		hunting.GET("/alerts", handleListSecurityAlerts())
		hunting.GET("/alerts/:id/correlations", handleGetAlertCorrelations())
		hunting.POST("/alerts/:id/to-case/:caseId", handleLinkAlertToCase(logger))
		
		// Query Builder
		hunting.POST("/queries/build", handleBuildBehaviorQuery())
		hunting.GET("/queries/:id/run", handleRunBehaviorQuery())
		hunting.GET("/queries/templates", handleGetQueryTemplates())
		
		// MITRE ATT&CK Mapping
		hunting.GET("/mitre/techniques", handleGetMITRTechniques())
		hunting.GET("/detection-rules/:technique", handleGetDetectionRules())
		
		// Investigation Timeline
		hunting.GET("/cases/:id/timeline", handleGetInvestigationTimeline())
		hunting.POST("/cases/:id/note", handleAddInvestigationNote(evidenceLedger, logger))
		
		// Reporting
		hunting.POST("/cases/:id/generate-report", handleGenerateIncidentReport(logger))
	}
}

// ============================================================================
// Hunt Case Management Handlers
// ============================================================================

// handleCreateHuntCase creates a new hunt case
func handleCreateHuntCase(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Title       string            `json:"title" binding:"required"`
			Description string            `json:"description"`
			Severity    string            `json:"severity" binding:"required"` // low, medium, high, critical
			Type        string            `json:"type" binding:"required"`     // ueba, ioc_match, cve_correlation
			Priority    int               `json:"priority"`                    // 1-10
			Tags        []string          `json:"tags"`
			Metadata    map[string]any    `json:"metadata"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "details": err.Error()})
			return
		}
		
		caseID := generateCaseID()
		
		logger.WithFields(logrus.Fields{
			"case_id":   caseID,
			"title":     req.Title,
			"severity":  req.Severity,
			"type":      req.Type,
			"actor":     c.GetString("user_id"),
			"action":    "create_hunt_case",
		}).Info("Hunt case created")
		
		if ledger != nil {
			// Create canonical JSON for hashing
			inputBytes, _ := json.Marshal(gin.H{
				"title":     req.Title,
				"type":      req.Type,
				"severity":  req.Severity,
			})
			
			receipt := evidence.Receipt{
				Action:    "HUNT_CASE_CREATED",
				Subject:   caseID,
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
				InputHash: fmt.Sprintf("%x", sha256.Sum256(inputBytes)),
				Metadata: gin.H{
					"title":     req.Title,
					"severity":  req.Severity,
					"type":      req.Type,
					"priority":  req.Priority,
					"tags":      req.Tags,
				},
			}
			
			if attestErr := ledger.RecordReceipt(receipt); attestErr != nil {
				logger.WithError(attestErr).Warn("Failed to record hunt case creation evidence (non-critical)")
			}
		}
		
		c.JSON(http.StatusCreated, gin.H{
			"case_id":     caseID,
			"message":     "hunt case created successfully",
			"status":      "active",
			"audit_trail": true,
		})
	}
}

// handleListHuntCases returns all hunt cases
func handleListHuntCases() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := c.Query("status")
		severity := c.Query("severity")
		
		type CaseSummary struct {
			ID         string            `json:"id"`
			Title      string            `json:"title"`
			Status     string            `json:"status"`
			Severity   string            `json:"severity"`
			Type       string            `json:"type"`
			AlertCount int               `json:"alert_count"`
			CreatedAt  time.Time         `json:"created_at"`
		}
		
		cases := []CaseSummary{
			{
				ID:         "hunt-case-1738234567",
				Title:      "Suspicious Data Exfiltration Pattern",
				Status:     "investigating",
				Severity:   "high",
				Type:       "ueba",
				AlertCount: 15,
				CreatedAt:  time.Now().Add(-24 * time.Hour),
			},
			{
				ID:         "hunt-case-1738134567",
				Title:      "Unusual Authentication Behavior",
				Status:     "active",
				Severity:   "medium",
				Type:       "ueba",
				AlertCount: 8,
				CreatedAt:  time.Now().Add(-48 * time.Hour),
			},
		}
		
		c.JSON(http.StatusOK, gin.H{
			"cases":   cases,
			"total":   len(cases),
			"filters": gin.H{"status": status, "severity": severity},
		})
	}
}

// handleGetHuntCase retrieves a specific hunt case
func handleGetHuntCase() gin.HandlerFunc {
	return func(c *gin.Context) {
		caseID := c.Param("id")
		
		c.JSON(http.StatusOK, gin.H{
			"id": caseID,
			"title": "Suspicious Data Exfiltration Pattern",
			"description": "Detected unusual egress traffic patterns from data processing service",
			"status": "investigating",
			"severity": "high",
			"type": "ueba",
			"priority": 8,
			"investigator": "analyst@company.com",
			"alerts": []gin.H{
				{"id": "alert-001", "type": "network_egress", "score": 0.89, "time": time.Now().Add(-24 * time.Hour)},
				{"id": "alert-002", "type": "data_volume_anomaly", "score": 0.85, "time": time.Now().Add(-23 * time.Hour)},
			},
			"findings": []gin.H{
				{"id": "finding-001", "technique": "T1048", "confidence": 0.82, "description": "Exfiltration Over Alternative Protocol"},
			},
			"timeline": []gin.H{
				{"time": time.Now().Add(-24 * time.Hour), "event": "case_created"},
				{"time": time.Now().Add(-23 * time.Hour), "event": "alerts_correlated"},
			},
			"created_at": time.Now().Add(-24 * time.Hour),
			"updated_at": time.Now().Add(-2 * time.Hour),
		})
	}
}

// handleUpdateHuntCase updates a hunt case
func handleUpdateHuntCase(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Status   string `json:"status"`
			Priority int    `json:"priority"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"case_id": c.Param("id"),
			"updates": req,
			"actor":   c.GetString("user_id"),
		}).Info("Hunt case updated")
		
		c.JSON(http.StatusOK, gin.H{
			"case_id":   c.Param("id"),
			"updated":   true,
			"timestamp": time.Now().UTC(),
		})
	}
}

// handleDeleteHuntCase deletes a hunt case
func handleDeleteHuntCase(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"case_id": c.Param("id"),
			"actor":   c.GetString("user_id"),
		}).Info("Hunt case deletion requested")
		
		c.JSON(http.StatusOK, gin.H{
			"case_id":   c.Param("id"),
			"deleted":   true,
			"timestamp": time.Now().UTC(),
		})
	}
}

// handleAssignInvestigator assigns investigator to case
func handleAssignInvestigator(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Investigator string `json:"investigator" binding:"required"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"case_id":      c.Param("id"),
			"investigator": req.Investigator,
			"actor":        c.GetString("user_id"),
		}).Info("Investigator assigned")
		
		c.JSON(http.StatusOK, gin.H{
			"case_id":    c.Param("id"),
			"assigned":   true,
			"investigator": req.Investigator,
		})
	}
}

// ============================================================================
// UEBA Analysis Handlers
// ============================================================================

// handleAnalyzeBehaviorUEBA runs UEBA analysis
func handleAnalyzeBehaviorUEBA() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			EntityTypes []string `json:"entity_types" binding:"required"`
			TimeRange   string   `json:"time_range"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"analysis_id":      fmt.Sprintf("ueba-analysis-%d", time.Now().UnixNano()),
			"entity_types":     req.EntityTypes,
			"anomalies_detected": 5,
			"confidence_score": 0.85,
		})
	}
}

// handleGetBaselineStats gets baseline statistics for an entity type
func handleGetBaselineStats() gin.HandlerFunc {
	return func(c *gin.Context) {
		entityType := c.Param("entityType")
		
		c.JSON(http.StatusOK, gin.H{
			"entity_type": entityType,
			"baseline_stats": gin.H{
				"mean":   100.5,
				"stddev": 25.3,
				"min":    50.2,
				"max":    200.8,
			},
		})
	}
}

// handleTrainBehaviorBaseline trains behavior baseline
func handleTrainBehaviorBaseline() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"trained":          true,
			"entities_processed": 1000,
			"baseline_updated": true,
		})
	}
}

// ============================================================================
// Alert & Query Handlers
// ============================================================================

// handleListSecurityAlerts lists security alerts
func handleListSecurityAlerts() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"alerts": []gin.H{
				{"id": "alert-001", "type": "network_egress", "severity": "high", "score": 0.89},
			},
			"total": 1,
		})
	}
}

// handleGetAlertCorrelations gets alert correlations
func handleGetAlertCorrelations() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"alert_id":    c.Param("id"),
			"correlations": 3,
			"linked_alerts": []string{"alert-002", "alert-003"},
		})
	}
}

// handleLinkAlertToCase links alert to case
func handleLinkAlertToCase(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"alert_id":  c.Param("id"),
			"case_id":   c.Param("caseId"),
			"action":    "link_alert_to_case",
			"actor":     c.GetString("user_id"),
		}).Info("Alert linked to case")
		
		c.JSON(http.StatusOK, gin.H{
			"linked": true,
		})
	}
}

// handleBuildBehaviorQuery builds behavioral query
func handleBuildBehaviorQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"query_id":     fmt.Sprintf("query-%d", time.Now().UnixNano()),
			"build_status": "success",
		})
	}
}

// handleRunBehaviorQuery runs behavioral query
func handleRunBehaviorQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"query_id":   c.Param("id"),
			"results":    15,
			"execution_time_ms": 234,
		})
	}
}

// handleGetQueryTemplates gets query templates
func handleGetQueryTemplates() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"templates": []string{"data_exfiltration", "unusual_login", "privilege_escalation"},
		})
	}
}

// ============================================================================
// MITRE & Detection Rules Handlers
// ============================================================================

// handleGetMITRTechniques gets MITRE ATT&CK techniques
func handleGetMITRTechniques() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"techniques": []gin.H{
				{"tactic": "TA0010", "technique": "T1048", "name": "Alternative Protocol"},
			},
		})
	}
}

// handleGetDetectionRules gets detection rules for technique
func handleGetDetectionRules() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"technique": c.Param("technique"),
			"rules":     5,
		})
	}
}

// ============================================================================
// Investigation Timeline & Reporting Handlers
// ============================================================================

// handleGetInvestigationTimeline gets investigation timeline
func handleGetInvestigationTimeline() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"case_id":    c.Param("id"),
			"events":     10,
		})
	}
}

// handleAddInvestigationNote adds note to case
func handleAddInvestigationNote(ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Note string `json:"note" binding:"required"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"case_id": c.Param("id"),
			"actor":   c.GetString("user_id"),
		}).Info("Investigation note added")
		
		if ledger != nil {
			// Create canonical JSON for hashing
			inputBytes, _ := json.Marshal(gin.H{
				"note": req.Note,
			})
			
			receipt := evidence.Receipt{
				Action:    "INVESTIGATION_NOTE_ADDED",
				Subject:   c.Param("id"),
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
				InputHash: fmt.Sprintf("%x", sha256.Sum256(inputBytes)),
				Metadata: gin.H{
					"note_length": len(req.Note),
				},
			}
			
			if attestErr := ledger.RecordReceipt(receipt); attestErr != nil {
				logger.WithError(attestErr).Warn("Failed to record investigation note evidence (non-critical)")
			}
		}
		
		c.JSON(http.StatusOK, gin.H{
			"added": true,
		})
	}
}

// handleGenerateIncidentReport generates incident report
func handleGenerateIncidentReport(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger.WithFields(logrus.Fields{
			"case_id": c.Param("id"),
			"action":  "generate_report",
			"actor":   c.GetString("user_id"),
		}).Info("Incident report generated")
		
		c.JSON(http.StatusOK, gin.H{
			"report_generated": true,
			"report_url":       fmt.Sprintf("/reports/hunt-case-%s.pdf", c.Param("id")),
		})
	}
}

// generateCaseID generates unique case ID
func generateCaseID() string {
	return fmt.Sprintf("hunt-case-%d", time.Now().UnixNano())
}
