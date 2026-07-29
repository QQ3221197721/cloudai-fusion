// Package api - plugins.go exposes the plugin runtime over REST.
// These endpoints make the contrib plugin ecosystem operable in production:
// operators can discover which plugins are installed, inspect their lifecycle
// phase and health, and browse the marketplace-ready manifest catalog.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib"
)

// handlePluginList returns every managed plugin with its lifecycle status,
// metadata and extension points — the operator's single view of the running
// plugin ecosystem.
func handlePluginList(mgr *plugin.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		statuses := mgr.AllStatuses()
		items := make([]gin.H, 0, len(statuses))
		for _, st := range statuses {
			item := gin.H{
				"name":       st.Name,
				"phase":      st.Phase,
				"healthy":    st.Healthy,
				"last_error": st.LastError,
			}
			if p, err := mgr.Registry().Get(st.Name); err == nil {
				md := p.Metadata()
				item["version"] = md.Version
				item["description"] = md.Description
				item["extension_points"] = md.ExtensionPoints
				item["dependencies"] = md.Dependencies
			}
			items = append(items, item)
		}
		c.JSON(http.StatusOK, gin.H{
			"plugins": items,
			"total":   len(items),
			"order":   mgr.PluginOrder(),
		})
	}
}

// handlePluginHealth reports one plugin's live health (fresh probe, not cache).
func handlePluginHealth(mgr *plugin.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		p, err := mgr.Registry().Get(name)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "plugin not found", "name": name})
			return
		}
		healthErr := p.Health(c.Request.Context())
		resp := gin.H{"name": name, "healthy": healthErr == nil}
		if healthErr != nil {
			resp["detail"] = healthErr.Error()
		}
		if st, stErr := mgr.Status(name); stErr == nil {
			resp["phase"] = st.Phase
		}
		c.JSON(http.StatusOK, resp)
	}
}

// handlePluginManifests serves the marketplace-ready manifest catalog for the
// contrib plugins. The catalog is static metadata (name/version/extension
// points/permissions), available whether or not the plugins are running, so
// integrators can discover what the platform can host.
func handlePluginManifests() gin.HandlerFunc {
	manifests := contrib.GetPluginManifests()
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"manifests": manifests,
			"total":     len(manifests),
		})
	}
}
