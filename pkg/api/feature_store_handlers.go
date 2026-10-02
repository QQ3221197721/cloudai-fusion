// Package api provides RESTful HTTP handlers for the Feature Store subsystem.
// Endpoints:
//   - GET /api/v1/features - List all features with filtering
//   - GET /api/v1/features/:id - Get feature details
//   - POST /api/v1/features - Register new feature
//   - PUT /api/v1/features/:id - Update feature metadata
//   - DELETE /api/v1/features/:id - Delete feature
//   - GET /api/v1/feature-groups - List feature groups
//   - POST /api/v1/features/query - Query feature values from online store
//   - GET /api/v1/features/:id/metrics - Get usage analytics
//   - POST /api/v1/materialization-jobs - Schedule batch materialization
package api

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/featurestore"
	"github.com/gin-gonic/gin"
)

// FeatureStoreHandler handles Feature Store HTTP requests
type FeatureStoreHandler struct {
	manager    *featurestore.Manager
	evidence   *evidence.Ledger
}

// NewFeatureStoreHandler creates a new handler instance
func NewFeatureStoreHandler(mgr *featurestore.Manager, ev *evidence.Ledger) *FeatureStoreHandler {
	return &FeatureStoreHandler{
		manager:  mgr,
		evidence: ev,
	}
}

// RegisterRoutes registers all Feature Store routes
func (h *FeatureStoreHandler) RegisterRoutes(router *gin.Engine) {
	features := router.Group("/features")
	{
		features.GET("", h.listFeatures)
		features.POST("", h.createFeature)
		features.GET("/:id", h.getFeature)
		features.PUT("/:id", h.updateFeature)
		features.DELETE("/:id", h.deleteFeature)
		features.GET("/:id/metrics", h.getUsageMetrics)
	}

	groups := router.Group("feature-groups")
	{
		groups.GET("", h.listFeatureGroups)
		groups.GET("/:id", h.getFeatureGroup)
	}

	query := router.Group("features/query")
	{
		query.POST("", h.queryFeatures)
	}

	materialization := router.Group("materialization-jobs")
	{
		materialization.POST("", h.submitMaterializationJob)
	}
}

// listFeatures godoc
// @Summary List all features
// @Description Get all registered features with optional filtering
// @Tags feature-store
// @Param type query string false "Filter by feature type"
// @Param entity query string false "Filter by entity type"
// @Param group_id query string false "Filter by group ID"
// @Param search query string false "Search in feature name"
// @Param tags query string false "Filter by tags (comma-separated)"
// @Param enabled_only query bool false "Show only enabled features"
// @Success 200 {array} featurestore.Feature
// @Router /api/v1/features [get]
func (h *FeatureStoreHandler) listFeatures(c *gin.Context) {
	filters := &featurestore.FeatureFilter{}

	if typeStr := c.Query("type"); typeStr != "" {
		filters.Type = featurestore.ValueType(typeStr)
	}
	if entityStr := c.Query("entity"); entityStr != "" {
		filters.Entity = featurestore.EntityType(entityStr)
	}
	if groupID := c.Query("group_id"); groupID != "" {
		filters.GroupID = groupID
	}
	if searchTerm := c.Query("search"); searchTerm != "" {
		filters.SearchTerm = searchTerm
	}
	if tagsStr := c.Query("tags"); tagsStr != "" {
		filters.Tags = strings.Split(tagsStr, ",")
	}
	if enabledOnly := c.Query("enabled_only"); enabledOnly == "true" {
		filters.EnabledOnly = true
	}

	features, err := h.manager.ListFeatures(c.Request.Context(), filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to list features",
		})
		return
	}
	
	// Record evidence
	if h.evidence != nil {
		inputBytes, _ := json.Marshal(gin.H{
			"action":      "LIST_FEATURES",
			"filters":     filters,
			"user_id":     c.GetString("user_id"),
			"timestamp":   time.Now().UTC(),
		})
		receipt := evidence.Receipt{
			Action:    "FEATURES_LISTED",
			Subject:   "feature_registry",
			Actor:     c.GetString("user_id"),
			Timestamp: time.Now().UTC(),
			InputHash: fmt.Sprintf("%x", sha256.Sum256(inputBytes)),
			Metadata: gin.H{
				"type":         "query",
				"result_count": len(features),
			},
		}
		if attestErr := h.evidence.RecordReceipt(receipt); attestErr != nil {
			logger.WithError(attestErr).Warn("Evidence failed (non-critical)")
		}
	}
	
	c.JSON(http.StatusOK, gin.H{
		"features": features,
		"count":    len(features),
	})
}

// getFeature godoc
// @Summary Get feature details
// @Description Retrieve detailed information about a single feature
// @Tags feature-store
// @Param id path string true "Feature ID"
// @Success 200 {object} featurestore.Feature
// @Router /api/v1/features/{id} [get]
func (h *FeatureStoreHandler) getFeature(c *gin.Context) {
	featureID := c.Param("id")

	feature, err := h.manager.GetFeature(c.Request.Context(), featureID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	// Record evidence
	if h.evidence != nil {
		inputBytes, _ := json.Marshal(gin.H{
			"action":   "GET_FEATURE",
			"feature":  featureID,
			"user_id":  c.GetString("user_id"),
			"timestamp": time.Now().UTC(),
		})
		receipt := evidence.Receipt{
			Action:    "FEATURE_ACCESSED",
			Subject:   featureID,
			Actor:     c.GetString("user_id"),
			Timestamp: time.Now().UTC(),
			InputHash: fmt.Sprintf("%x", sha256.Sum256(inputBytes)),
			Metadata: gin.H{
				"type": "read",
			},
		}
		if attestErr := h.evidence.RecordReceipt(receipt); attestErr != nil {
			logger.WithError(attestErr).Warn("Evidence failed (non-critical)")
		}
	}
	
	c.JSON(http.StatusOK, feature)
}

// createFeature godoc
// @Summary Register new feature
// @Description Create a new feature in the registry
// @Tags feature-store
// @Success 201 {object} featurestore.Feature
// @Router /api/v1/features [post]
func (h *FeatureStoreHandler) createFeature(c *gin.Context) {
	var feature featurestore.Feature

	if err := c.ShouldBindJSON(&feature); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	if err := h.manager.CreateFeature(c.Request.Context(), &feature); err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	// Record evidence
	if h.evidence != nil {
		inputBytes, _ := json.Marshal(gin.H{
			"action":          "CREATE_FEATURE",
			"feature_name":    feature.Name,
			"feature_type":    feature.Type,
			"entity_type":     feature.EntityType,
			"user_id":         c.GetString("user_id"),
			"timestamp":       time.Now().UTC(),
		})
		receipt := evidence.Receipt{
			Action:    "FEATURE_REGISTERED",
			Subject:   feature.Name,
			Actor:     c.GetString("user_id"),
			Timestamp: time.Now().UTC(),
			InputHash: fmt.Sprintf("%x", sha256.Sum256(inputBytes)),
			Metadata: gin.H{
				"type":         "create",
				"feature_type": string(feature.Type),
			},
		}
		if attestErr := h.evidence.RecordReceipt(receipt); attestErr != nil {
			logger.WithError(attestErr).Warn("Evidence failed (non-critical)")
		}
	}
	
	c.JSON(http.StatusCreated, feature)
}

// updateFeature godoc
// @Summary Update feature metadata
// @Description Update an existing feature's metadata fields
// @Tags feature-store
// @Param id path string true "Feature ID"
// @Success 200 {object} featurestore.Feature
// @Router /api/v1/features/{id} [put]
func (h *FeatureStoreHandler) updateFeature(c *gin.Context) {
	featureID := c.Param("id")

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	feature, err := h.manager.UpdateFeature(c.Request.Context(), featureID, updates)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	// Record evidence
	if h.evidence != nil {
		inputBytes, _ := json.Marshal(gin.H{
			"action":            "UPDATE_FEATURE",
			"feature_id":        featureID,
			"feature_name":      feature.Name,
			"updated_fields":    updates,
			"user_id":           c.GetString("user_id"),
			"timestamp":         time.Now().UTC(),
		})
		receipt := evidence.Receipt{
			Action:    "FEATURE_UPDATED",
			Subject:   featureID,
			Actor:     c.GetString("user_id"),
			Timestamp: time.Now().UTC(),
			InputHash: fmt.Sprintf("%x", sha256.Sum256(inputBytes)),
			Metadata: gin.H{
				"type":     "update",
				"changes":  len(updates),
			},
		}
		if attestErr := h.evidence.RecordReceipt(receipt); attestErr != nil {
			logger.WithError(attestErr).Warn("Evidence failed (non-critical)")
		}
	}
	
	c.JSON(http.StatusOK, feature)
}

// deleteFeature godoc
// @Summary Delete feature
// @Description Remove a feature from the registry
// @Tags feature-store
// @Param id path string true "Feature ID"
// @Success 204
// @Router /api/v1/features/{id} [delete]
func (h *FeatureStoreHandler) deleteFeature(c *gin.Context) {
	featureID := c.Param("id")

	if err := h.manager.DeleteFeature(c.Request.Context(), featureID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	// Record evidence
	if h.evidence != nil {
		inputBytes, _ := json.Marshal(gin.H{
			"action":    "DELETE_FEATURE",
			"feature":   featureID,
			"user_id":   c.GetString("user_id"),
			"timestamp": time.Now().UTC(),
		})
		receipt := evidence.Receipt{
			Action:    "FEATURE_DELETED",
			Subject:   featureID,
			Actor:     c.GetString("user_id"),
			Timestamp: time.Now().UTC(),
			InputHash: fmt.Sprintf("%x", sha256.Sum256(inputBytes)),
			Metadata: gin.H{
				"type": "delete",
			},
		}
		if attestErr := h.evidence.RecordReceipt(receipt); attestErr != nil {
			logger.WithError(attestErr).Warn("Evidence failed (non-critical)")
		}
	}
	
	c.NoContent(http.StatusNoContent)
}

// listFeatureGroups godoc
// @Summary List feature groups
// @Description Get all feature groups with connectivity status
// @Tags feature-store
// @Success 200 {array} featurestore.FeatureGroup
// @Router /api/v1/feature-groups [get]
func (h *FeatureStoreHandler) listFeatureGroups(c *gin.Context) {
	groups, err := h.manager.ListFeatureGroups(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to list feature groups",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"groups": groups,
		"count":  len(groups),
	})
}

// getFeatureGroup godoc
// @Summary Get feature group details
// @Description Retrieve detailed information about a single feature group
// @Tags feature-store
// @Param id path string true "Feature Group ID"
// @Success 200 {object} featurestore.FeatureGroup
// @Router /api/v1/feature-groups/{id} [get]
func (h *FeatureStoreHandler) getFeatureGroup(c *gin.Context) {
	groupID := c.Param("id")

	group, err := h.manager.GetFeatureGroup(c.Request.Context(), groupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, group)
}

// queryFeatures godoc
// @Summary Query feature values from online store
// @Description Perform point-in-time correct feature retrieval from online store
// @Tags feature-store
// @Body featurestore.QueryRequest
// @Success 200 {object} featurestore.QueryResponse
// @Router /api/v1/features/query [post]
func (h *FeatureStoreHandler) queryFeatures(c *gin.Context) {
	var req featurestore.QueryRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	if len(req.FeatureIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "at least one feature_id is required",
		})
		return
	}

	if req.EventTime.IsZero() {
		req.EventTime = time.Now()
	}

	response, err := h.manager.QueryFeatures(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// getUsageMetrics godoc
// @Summary Get usage analytics
// @Description Retrieve usage metrics and downstream consumers for a feature
// @Tags feature-store
// @Param id path string true "Feature ID"
// @Success 200 {object} featurestore.UsageMetrics
// @Router /api/v1/features/{id}/metrics [get]
func (h *FeatureStoreHandler) getUsageMetrics(c *gin.Context) {
	featureID := c.Param("id")

	metrics, err := h.manager.GetUsageMetrics(c.Request.Context(), featureID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

// submitMaterializationJob godoc
// @Summary Schedule batch materialization
// @Description Create a batch job to compute and materialize features to offline store
// @Tags feature-store
// @Body featurestore.MaterializationJob
// @Success 201 {object} featurestore.MaterializationJob
// @Router /api/v1/materialization-jobs [post]
func (h *FeatureStoreHandler) submitMaterializationJob(c *gin.Context) {
	var job featurestore.MaterializationJob

	if err := c.ShouldBindJSON(&job); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	if job.GroupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "group_id is required",
		})
		return
	}

	if err := h.manager.SubmitMaterializationJob(c.Request.Context(), &job); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, job)
}

// ExportFeaturesParams represents query parameters for feature export
type ExportFeaturesParams struct {
	GroupID     string    `form:"group_id" binding:"required"`
	StartTime   time.Time `form:"start_time" binding:"required"`
	EndTime     time.Time `form:"end_time" binding:"required"`
	Format      string    `form:"format" binding:"oneof=csv parquet json"`
	Prefix      string    `form:"prefix"`
	Limit       int       `form:"limit"`
}
