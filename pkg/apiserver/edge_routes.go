// Package apiserver provides HTTP handlers for CloudAI Fusion REST API
package apiserver

import (
	"net/http"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/edgeautonomy"
	"github.com/gin-gonic/gin"
)

// EdgeAPIServer handles all edge-related REST endpoints
type EdgeAPIServer struct {
	manager     *edgeautonomy.Manager
	syncManager *edgeautonomy.DeltaSyncManager
}

// NewEdgeAPIServer creates a new EdgeAPIServer instance
func NewEdgeAPIServer(edgemgr *edgeautonomy.Manager) *EdgeAPIServer {
	return &EdgeAPIServer{
		manager:     edgemgr,
		syncManager: edgemgr.GetDeltaSyncManager(),
	}
}

// Register registers all edge routes with the gin router
func (e *EdgeAPIServer) Register(router *gin.Engine) {
	api := router.Group("/api/v1")
	{
		// ==================== Node Management ====================
		api.GET("/edge/nodes", e.listNodes)
		api.GET("/edge/nodes/:id", e.getNodeDetail)
		api.POST("/edge/nodes", e.createEdgeNode)
		api.PUT("/edge/nodes/:id", e.updateEdgeNode)
		api.DELETE("/edge/nodes/:id", e.deleteEdgeNode)

		// ==================== Deployment ====================
		api.POST("/edge/deploy", e.deployWorkload)
		api.GET("/edge/deployments/:id", e.getDeploymentStatus)
		api.POST("/edge/deployments/:id/abort", e.abortDeployment)

		// ==================== Model Registry ====================
		api.GET("/edge/models", e.listModels)
		api.POST("/edge/models", e.uploadModel)
		api.GET("/edge/models/:id", e.getModelDetail)
		api.DELETE("/edge/models/:id", e.deleteModel)

		// ==================== Sync Monitoring ====================
		api.GET("/edge/sync/sessions", e.listSyncSessions)
		api.GET("/edge/sync/sessions/:sessionId", e.getSyncStatus)

		// ==================== Configuration ====================
		api.GET("/edge/policies", e.getSyncPolicies)
		api.PUT("/edge/policies", e.updateSyncPolicies)
		api.POST("/edge/offline/mode", e.toggleOfflineMode)
	}
}

// listNodes lists all edge nodes
func (e *EdgeAPIServer) listNodes(c *gin.Context) {
	nodes := e.manager.GetAllNodes()
	c.JSON(http.StatusOK, gin.H{
		"total": len(nodes),
		"nodes": nodes,
	})
}

// getNodeDetail gets details of a specific edge node
func (e *EdgeAPIServer) getNodeDetail(c *gin.Context) {
	nodeID := c.Param("id")
	node, err := e.manager.GetNode(nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, node)
}

// createEdgeNode creates a new edge node
func (e *EdgeAPIServer) createEdgeNode(c *gin.Context) {
	var req struct {
		ID        string            `json:"id" binding:"required"`
		Address   string            `json:"address" binding:"required"`
		Port      int               `json:"port" binding:"required"`
		Capabilities map[string]string `json:"capabilities"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	node := &edgeautonomy.EdgeNode{
		ID:             req.ID,
		Address:        req.Address,
		Port:           req.Port,
		Status:         "online",
		Capabilities:   make(map[string]bool),
		Metadata:       req.Capabilities,
	}

	if err := e.manager.RegisterNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, node)
}

// updateEdgeNode updates an existing edge node
func (e *EdgeAPIServer) updateEdgeNode(c *gin.Context) {
	nodeID := c.Param("id")
	var req struct {
		Address  *string            `json:"address"`
		Capabilities map[string]bool `json:"capabilities"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	node, err := e.manager.GetNode(nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		return
	}

	if req.Address != nil {
		node.Address = *req.Address
	}
	if req.Capabilities != nil {
		node.Capabilities = req.Capabilities
	}
	if req.Metadata != nil {
		node.Metadata = req.Metadata
	}

	if err := e.manager.UpdateNode(node); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, node)
}

// deleteEdgeNode deletes an edge node
func (e *EdgeAPIServer) deleteEdgeNode(c *gin.Context) {
	nodeID := c.Param("id")
	if err := e.manager.DeleteNode(nodeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// deployWorkload deploys a workload to edge nodes
func (e *EdgeAPIServer) deployWorkload(c *gin.Context) {
	var req struct {
		WorkloadID   string   `json:"workload_id" binding:"required"`
		TargetNodes  []string `json:"target_nodes" binding:"required"`
		SyncPolicy   string   `json:"sync_policy"`
		ModelSizeParams string `json:"model_size_params"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	deploymentID, err := e.manager.DeployWorkload(req.WorkloadID, req.TargetNodes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"deployment_id": deploymentID,
		"status": "initiated",
	})
}

// getDeploymentStatus gets the status of a deployment
func (e *EdgeAPIServer) getDeploymentStatus(c *gin.Context) {
	deploymentID := c.Param("id")
	status, err := e.manager.GetDeploymentStatus(deploymentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deployment not found"})
		return
	}
	c.JSON(http.StatusOK, status)
}

// abortDeployment aborts an ongoing deployment
func (e *EdgeAPIServer) abortDeployment(c *gin.Context) {
	deploymentID := c.Param("id")
	if err := e.manager.AbortDeployment(deploymentID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "aborted"})
}

// listModels lists available models in the registry
func (e *EdgeAPIServer) listModels(c *gin.Context) {
	models := e.manager.ListModels()
	c.JSON(http.StatusOK, gin.H{
		"total": len(models),
		"models": models,
	})
}

// uploadModel uploads a model to the registry
func (e *EdgeAPIServer) uploadModel(c *gin.Context) {
	// In production, handle multipart form data for binary file upload
	// For now, accept JSON metadata
	var req struct {
		Name        string `json:"name" binding:"required"`
		Version     string `json:"version" binding:"required"`
		Framework   string `json:"framework"`
		SizeGB      float64 `json:"size_gb"`
		Description string `json:"description"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	model := &edgeautonomy.ModelInfo{
		Name:        req.Name,
		Version:     req.Version,
		Framework:   req.Framework,
		SizeGB:      req.SizeGB,
		Description: req.Description,
	}

	if err := e.manager.RegisterModel(model); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, model)
}

// getModelDetail gets details of a specific model
func (e *EdgeAPIServer) getModelDetail(c *gin.Context) {
	modelID := c.Param("id")
	model, err := e.manager.GetModel(modelID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Model not found"})
		return
	}
	c.JSON(http.StatusOK, model)
}

// deleteModel deletes a model from the registry
func (e *EdgeAPIServer) deleteModel(c *gin.Context) {
	modelID := c.Param("id")
	if err := e.manager.DeleteModel(modelID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// listSyncSessions lists all active sync sessions
func (e *EdgeAPIServer) listSyncSessions(c *gin.Context) {
	sessions := e.syncManager.ListSessions()
	c.JSON(http.StatusOK, gin.H{
		"total": len(sessions),
		"sessions": sessions,
	})
}

// getSyncStatus gets the status of a specific sync session
func (e *EdgeAPIServer) getSyncStatus(c *gin.Context) {
	sessionID := c.Param("sessionId")
	status := e.syncManager.GetSessionStatus(sessionID)
	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}
	c.JSON(http.StatusOK, status)
}

// getSyncPolicies gets current sync policies
func (e *EdgeAPIServer) getSyncPolicies(c *gin.Context) {
	policies := e.manager.GetSyncPolicies()
	c.JSON(http.StatusOK, policies)
}

// updateSyncPolicies updates sync policies
func (e *EdgeAPIServer) updateSyncPolicies(c *gin.Context) {
	var policies edgeautonomy.SyncConfig
	if err := c.ShouldJSON(&policies); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if err := e.manager.UpdateSyncPolicies(&policies); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, policies)
}

// toggleOfflineMode toggles edge autonomy mode
func (e *EdgeAPIServer) toggleOfflineMode(c *gin.Context) {
	var req struct {
		NodeID       string `json:"node_id" binding:"required"`
		Autonomous   bool   `json:"autonomous"`
		SyncStrategy string `json:"sync_strategy"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	err := e.manager.ToggleOfflineMode(req.NodeID, req.Autonomous, req.SyncStrategy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "mode updated"})
}
