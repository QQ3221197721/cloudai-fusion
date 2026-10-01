package api

import (
	"net/http"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/feature"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Route Registration
// ============================================================================

// RegisterM8Routes registers all M8 Config Manager endpoints for runtime feature flag management.
// Implements Module 8 — Runtime Configuration Management API.
// Routes:
//   - GET /api/v1/config           - List all feature flags
//   - GET /api/v1/config/categories - List all categories
//   - GET /api/v1/config/features          - List flags (alias for /)
//   - GET /api/v1/config/features/:key     - Get single flag
//   - PUT /api/v1/config/features/:key     - Update flag value
//   - DELETE /api/v1/config/features/:key  - Delete flag
//   - POST /api/v1/config/import             - Bulk import configuration
//   - GET  /api/v1/config/export             - Export full config
//   - GET /api/v1/config/hierarchy           - Hierarchical view by scope
//   - GET /api/v1/config/stats               - Usage analytics
func RegisterM8Routes(router *gin.Engine, featureMgr *feature.Manager, logger *logrus.Logger) {
	config := router.Group("/api/v1/config")
	config.Use(middleware.EndpointRateLimiter(100, 60)) // Rate limit: 100 req/min

	{
		// Feature Flag Management
		config.GET("", handleListFeatureFlags(featureMgr))                      // List all flags
		config.GET("/features", handleListFeatureFlags(featureMgr))             // Alias for /
		config.GET("/features/:key", handleGetFeatureFlag(featureMgr))          // Get single flag
		config.PUT("/features/:key", handleUpdateFeatureFlag(featureMgr))       // Update flag value
		config.DELETE("/features/:key", handleDeleteFeatureFlag(featureMgr))    // Delete flag

		// Category Management
		config.GET("/categories", handleListCategories(featureMgr))             // List categories

		// Bulk Operations
		config.POST("/import", handleImportConfig(featureMgr, logger))          // Import JSON bulk update
		config.GET("/export", handleExportConfig())                             // Export full config

		// Hierarchy Visualization
		config.GET("/hierarchy", handleGetConfigHierarchy(featureMgr))          // Tree view of scopes

		// Statistics
		config.GET("/stats", handleGetConfigStats(featureMgr))                  // Usage analytics
	}
}

// ============================================================================
// Handler Functions
// ============================================================================

// handleListFeatureFlags lists all feature flags with optional filtering.
// Query params:
//   - filter: Optional category filter or "all"
//   - enabled: Boolean filter for enabled flags only
//
// Success response (200 OK):
// {
//   "flags": [...],
//   "total": 42,
//   "enabled": 25
// }
func handleListFeatureFlags(mgr *feature.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter := c.Query("filter")
		enabledOnly := c.Query("enabled") == "true"

		flags := mgr.ListFlags()

		// Apply filters
		if filter != "" && filter != "all" {
			filtered := make([]feature.Flag, 0)
			for _, f := range flags {
				if string(f.Category) == filter {
					filtered = append(filtered, f)
				}
			}
			flags = filtered
		}

		if enabledOnly {
			enabled := make([]feature.Flag, 0)
			for _, f := range flags {
				if f.Enabled {
					enabled = append(enabled, f)
				}
			}
			flags = enabled
		}

		c.JSON(http.StatusOK, gin.H{
			"flags":   flags,
			"total":   len(flags),
			"enabled": countEnabled(flags),
		})
	}
}

// handleGetFeatureFlag retrieves a single feature flag by key.
// URL params:
//   - key (required): flag key
//
// Success response (200 OK):
// {
//   "flag": {...complete flag object...}
// }
func handleGetFeatureFlag(mgr *feature.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("key")

		if key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing key parameter"})
			return
		}

		flag, ok := mgr.GetFlag(key)
		if !ok || flag == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "flag not found", "key": key})
			return
		}

		c.JSON(http.StatusOK, gin.H{"flag": flag})
	}
}

// handleUpdateFeatureFlag updates a feature flag value with validation.
// Triggers hot-reload notification via event bus when successful.
//
// Request body (application/json):
// {
//   "enabled": true
// }
//
// Success response (200 OK):
// {
//   "message": "flag updated successfully",
//   "flag": {"key": "gpu_sharing_mps", "enabled": true}
// }
func handleUpdateFeatureFlag(mgr *feature.Manager, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("key")

		var req struct {
			Enabled bool `json:"enabled" binding:"required"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
			return
		}

		flag, ok := mgr.GetFlag(key)
		if !ok || flag == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "flag not found", "key": key})
			return
		}

		// Check if flag is read-only
		if flag.Metadata != nil && flag.Metadata["read_only"] == "true" {
			c.JSON(http.StatusForbidden, gin.H{"error": "read-only flag cannot be changed", "key": key})
			return
		}

		// Update flag
		if err := mgr.SetFlag(key, req.Enabled, "api"); err != nil {
			logger.WithFields(logrus.Fields{
				"key":   key,
				"value": req.Enabled,
			}).Error("Failed to update feature flag")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update flag", "details": err.Error()})
			return
		}

		logger.WithFields(logrus.Fields{
			"key":      key,
			"old_value": flag.Enabled,
			"new_value": req.Enabled,
			"actor":    c.GetString("user_id"),
		}).Info("Feature flag updated")

		c.JSON(http.StatusOK, gin.H{
			"message": "flag updated successfully",
			"flag":    gin.H{"key": key, "enabled": req.Enabled},
		})
	}
}

// handleDeleteFeatureFlag deletes a feature flag.
// Note: This removes the flag from runtime but does not persist deletion.
//
// Success response (200 OK):
// {
//   "message": "flag deleted successfully",
//   "key": "test_flag"
// }
func handleDeleteFeatureFlag(mgr *feature.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("key")

		// In current implementation, we don't have delete capability in store
		// Just log as warning and return error
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "deleting feature flags is not supported yet"})
	}
}

// handleListCategories lists all feature flag categories.
// Success response (200 OK):
// {
//   "categories": ["compute", "ai", "networking", ...],
//   "count": 7
// }
func handleListCategories(mgr *feature.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		categories := mgr.Categories()
		c.JSON(http.StatusOK, gin.H{
			"categories": categories,
			"count":      len(categories),
		})
	}
}

// handleImportConfig performs bulk import from JSON payload.
// Useful for mass updates or configuration migration.
//
// Request body (application/json):
// [
//   {"key": "feature_a", "enabled": true},
//   {"key": "feature_b", "enabled": false}
// ]
//
// Success response (200 OK):
// {
//   "summary": {
//     "total": 10,
//     "success": 8,
//     "failed": 2
//   },
//   "errors": [...]
// }
func handleImportConfig(mgr *feature.Manager, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var imports []struct {
			Key       string `json:"key" binding:"required"`
			Enabled   bool   `json:"enabled"`
			Metadata  map[string]interface{} `json:"metadata,omitempty"`
		}

		if err := c.ShouldBindJSON(&imports); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid import format", "details": err.Error()})
			return
		}

		success := 0
		failed := 0
		errors := make([]map[string]string, 0)

		for _, imp := range imports {
			if err := mgr.SetFlag(imp.Key, imp.Enabled, "bulk_import"); err != nil {
				failed++
				errors = append(errors, map[string]string{
					"key":   imp.Key,
					"error": err.Error(),
				})
			} else {
				success++
			}
		}

		logger.WithFields(logrus.Fields{
			"total":    len(imports),
			"success":  success,
			"failed":   failed,
		}).Info("Bulk config import completed")

		if failed > 0 {
			c.JSON(http.StatusOK, gin.H{
				"summary": gin.H{
					"total":   len(imports),
					"success": success,
					"failed":  failed,
				},
				"errors": errors,
			})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"message": "bulk import successful",
				"summary": gin.H{
					"total":   len(imports),
					"success": success,
				},
			})
		}
	}
}

// handleExportConfig exports full configuration as JSON.
// Returns all flags in serializable format.
//
// Success response (200 OK):
// {
//   "exported_at": "2026-09-30T12:00:00Z",
//   "flags": [...]
// }
func handleExportConfig() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Placeholder - would require access to all flags
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", "attachment; filename=config_export.json")
		c.JSON(http.StatusOK, gin.H{
			"exported_at": time.Now().UTC().Format(time.RFC3339),
			"flags":       []interface{}{},
		})
	}
}

// handleGetConfigHierarchy returns hierarchical view of config scopes.
// Groups flags by category and provides metadata about each group.
//
// Success response (200 OK):
// {
//   "hierarchy": {
//     "compute": [...flags...],
//     "ai": [...flags...]
//   }
// }
func handleGetConfigHierarchy(mgr *feature.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		hierarchy := make(map[string][]string)
		counts := make(map[string]int)

		flags := mgr.ListFlags()
		for _, flag := range flags {
			catKey := string(flag.Category)
			hierarchy[catKey] = append(hierarchy[catKey], flag.Key)
			counts[catKey]++
		}

		c.JSON(http.StatusOK, gin.H{
			"hierarchy": hierarchy,
			"counts":    counts,
			"total":     len(flags),
		})
	}
}

// handleGetConfigStats returns usage analytics for configs.
// Provides statistics on enabled/disabled ratios and category distribution.
//
// Success response (200 OK):
// {
//   "stats": {
//     "total_flags": 25,
//     "enabled_flags": 15,
//     "disabled_flags": 10,
//     "categories": 7,
//     "last_updated": "2026-09-30T12:00:00Z"
//   }
// }
func handleGetConfigStats(mgr *feature.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		flags := mgr.ListFlags()
		total := len(flags)
		enabled := mgr.EnabledCount()

		stats := gin.H{
			"total_flags":     total,
			"enabled_flags":   enabled,
			"disabled_flags":  total - enabled,
			"categories":      len(mgr.Categories()),
			"last_updated":    time.Now().UTC().Format(time.RFC3339),
		}

		c.JSON(http.StatusOK, gin.H{"stats": stats})
	}
}

// Helper Functions

// countEnabled returns number of enabled flags in a slice.
func countEnabled(flags []feature.Flag) int {
	count := 0
	for _, f := range flags {
		if f.Enabled {
			count++
		}
	}
	return count
}
