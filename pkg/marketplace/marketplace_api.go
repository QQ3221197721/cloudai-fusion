// Package marketplace - REST API for plugin management
package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// APIHandler handles HTTP requests for marketplace operations
type APIHandler struct {
	pm   *PluginManager
	reg  *PluginRegistry
}

// NewAPIHandler creates new API handler
func NewAPIHandler(pm *PluginManager, reg *PluginRegistry) *APIHandler {
	return &APIHandler{
		pm:  pm,
		reg: reg,
	}
}

// RegisterRoutes registers marketplace API routes
func (h *APIHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api/v1/marketplace")
	{
		// Plugin Discovery
		api.GET("/capabilities", h.handleListCapabilities)
		api.GET("/extensions", h.handleListExtensions)
		
		// Plugin Management
		api.GET("/plugins", h.handleListPlugins)
		api.GET("/plugins/:id", h.handleGetPlugin)
		api.POST("/plugins/:id/enable", h.handleEnablePlugin)
		api.POST("/plugins/:id/disable", h.handleDisablePlugin)
		api.DELETE("/plugins/:id", h.handleDeletePlugin)
		
		// Hot Reload
		api.POST("/plugins/:id/reload", h.handleHotReloadPlugin)
		
		// Health & Status
		api.GET("/health", h.handleHealthCheck)
		api.GET("/stats", h.handleStats)
	}
}

// handleListCapabilities lists all available capabilities
func (h *APIHandler) handleListCapabilities(c *gin.Context) {
	caps := h.reg.ListAvailableCapabilities()
	
	response := gin.H{
		"total": len(caps),
		"capabilities": make([]gin.H, 0, len(caps)),
	}
	
	for _, cap := range caps {
		response["capabilities"] = append(response["capabilities"].([]gin.H), gin.H{
			"name":       string(cap),
			"available":  true,
		})
	}
	
	c.JSON(http.StatusOK, response)
}

// handleListExtensions lists all extension points
func (h *APIHandler) handleListExtensions(c *gin.Context) {
	extensions := AllExtensionPoints
	
	response := gin.H{
		"total":     len(extensions),
		"extensions": make([]gin.H, 0, len(extensions)),
	}
	
	for _, ext := range extensions {
		response["extensions"] = append(response["extensions"].([]gin.H), gin.H{
			"id":            ext.ID,
			"name":          ext.Name,
			"description":   ext.Description,
			"version":       ext.Version,
			"capabilities":  ext.Categories,
		})
	}
	
	c.JSON(http.StatusOK, response)
}

// handleListPlugins lists all loaded plugins
func (h *APIHandler) handleListPlugins(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset := (page - 1) * limit
	
	allPlugins := h.pm.ListPlugins()
	total := len(allPlugins)
	
	// Pagination
	if offset >= total {
		allPlugins = []*Plugin{}
	} else if offset+limit < total {
		allPlugins = allPlugins[offset : offset+limit]
	}
	
	response := gin.H{
		"total":    total,
		"page":     page,
		"limit":    limit,
		"offset":   offset,
		"plugins":  make([]gin.H, 0, len(allPlugins)),
	}
	
	for _, p := range allPlugins {
		response["plugins"] = append(response["plugins"].([]gin.H), gin.H{
			"id":            p.ID,
			"name":          p.Manifest.Name,
			"version":       p.Manifest.Version,
			"status":        string(p.Status),
			"capabilities":  p.Manifest.Capabilities,
			"extension_points": p.Manifest.ExtensionPoints,
			"registered_at": p.RegisteredAt.Format(time.RFC3339),
			"hot_reloaded":  p.HotReloaded,
		})
	}
	
	c.JSON(http.StatusOK, response)
}

// handleGetPlugin gets specific plugin details
func (h *APIHandler) handleGetPlugin(c *gin.Context) {
	id := c.Param("id")
	
	plugin, exists := h.pm.plugins.Load(id)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin not found"})
		return
	}
	
	p := plugin.(*Plugin)
	response := gin.H{
		"id":               p.ID,
		"name":             p.Manifest.Name,
		"version":          p.Manifest.Version,
		"author":           p.Manifest.Author,
		"description":      p.Manifest.Description,
		"runtime":          p.Manifest.Runtime,
		"status":           string(p.Status),
		"capabilities":     p.Manifest.Capabilities,
		"extension_points": p.Manifest.ExtensionPoints,
		"registered_at":    p.RegisteredAt.Format(time.RFC3339),
		"last_heartbeat":   p.LastHeartbeat.Format(time.RFC3339),
		"config_schema":    p.Manifest.ConfigSchema,
		"dependencies":     p.Manifest.Dependencies,
	}
	
	c.JSON(http.StatusOK, response)
}

// handleEnablePlugin enables a disabled plugin
func (h *APIHandler) handleEnablePlugin(c *gin.Context) {
	id := c.Param("id")
	
	if err := h.reg.EnablePlugin(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message":  "Plugin enabled successfully",
		"id":       id,
	})
}

// handleDisablePlugin disables a running plugin
func (h *APIHandler) handleDisablePlugin(c *gin.Context) {
	id := c.Param("id")
	
	if err := h.reg.DisablePlugin(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message":  "Plugin disabled successfully",
		"id":       id,
	})
}

// handleDeletePlugin removes a plugin completely
func (h *APIHandler) handleDeletePlugin(c *gin.Context) {
	id := c.Param("id")
	
	// First disable if running
	h.reg.DisablePlugin(id)
	
	// Remove from registry
	h.pm.plugins.Delete(id)
	
	c.JSON(http.StatusOK, gin.H{
		"message":  "Plugin removed successfully",
		"id":       id,
	})
}

// handleHotReloadPlugin triggers hot reload of a plugin
func (h *APIHandler) handleHotReloadPlugin(c *gin.Context) {
	id := c.Param("id")
	
	ctx := c.Request.Context()
	if err := h.pm.HotReloadPlugin(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message":  "Plugin reloaded successfully",
		"id":       id,
		"reloaded": true,
	})
}

// handleHealthCheck returns system health status
func (h *APIHandler) handleHealthCheck(c *gin.Context) {
	plugins := h.pm.ListPlugins()
	status := "healthy"
	
	var healthyCount, totalCount int
	for _, p := range plugins {
		totalCount++
		if p.Status == StatusRunning || p.Status == StatusReady {
			healthyCount++
		} else {
			status = "degraded"
		}
	}
	
	response := gin.H{
		"status":         status,
		"timestamp":      time.Now().Format(time.RFC3339),
		"total_plugins":  totalCount,
		"healthy_plugins": healthyCount,
	}
	
	c.JSON(http.StatusOK, response)
}

// handleStats returns marketplace statistics
func (h *APIHandler) handleStats(c *gin.Context) {
	plugins := h.pm.ListPlugins()
	
	stats := make(map[string]int)
	for _, p := range plugins {
		runtime := p.Manifest.Runtime
		stats[runtime]++
	}
	
	capabilityCounts := make(map[Capability]int)
	for _, p := range plugins {
		for _, cap := range p.Manifest.Capabilities {
			capabilityCounts[cap]++
		}
	}
	
	response := gin.H{
		"total_plugins":     len(plugins),
		"plugins_by_runtime": stats,
		"total_capabilities": len(capabilityCounts),
		"capability_usage": make(map[string]int),
	}
	
	for cap, count := range capabilityCounts {
		response["capability_usage"][string(cap)] = count
	}
	
	c.JSON(http.StatusOK, response)
}
