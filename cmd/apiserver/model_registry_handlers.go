// Package apiserver - M2 Model Lifecycle Management HTTP Endpoints
// ============================================================================
// Purpose: 为 M2 Model Lifecycle Management 提供 RESTful API 接口
//          
// Endpoints:
//   GET    /api/v1/models              - 列出所有注册模型 (with optional name filter)
//   POST   /api/v1/models              - 注册新模型
//   GET    /api/v1/models/:name        - 获取特定模型的版本列表
//   GET    /api/v1/models/:name/:version - 获取特定版本的详细信息
//   DELETE /api/v1/models/:name/:version - 删除模型版本（逻辑删除）
//   POST   /api/v1/models/:name/rollback - 回滚到指定版本
//   GET    /api/v1/models/:name/lineage - 获取模型血缘关系图
//   POST   /api/v1/models/:name/verify  - 验证模型完整性
//   GET    /api/v1/models/stats         - 获取模型仓库统计信息
//
// Usage example:
//   curl -X POST http://localhost:8080/api/v1/models \
//     -H "Content-Type: application/json" \
//     -d '{
//       "name": "resnet50",
//       "version": "1.0.0",
//       "framework": "pytorch",
//       "artifact_path": "/models/resnet50.pth",
//       "metrics": {"accuracy": 0.92}
//     }'
// ============================================================================

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
)

var (
	globalModelRegistry *modelregistry.FSRegistry
	modelLogger         = logrus.New()
)

// RegisterRequest 模型注册请求
type RegisterRequest struct {
	Name          string            `json:"name" binding:"required"`
	Version       string            `json:"version" binding:"required"`
	ArtifactPath  string            `json:"artifact_path" binding:"required"`
	DatasetRef    string            `json:"dataset_ref,omitempty"`
	CodeRef       string            `json:"code_ref,omitempty"`
	ParentVersion string            `json:"parent_version,omitempty"`
	Hyperparams   map[string]string `json:"hyperparams,omitempty"`
	TaskType      string            `json:"task_type,omitempty"`
	Framework     string            `json:"framework,omitempty"`
	Summary       string            `json:"summary,omitempty"`
	Metrics       map[string]float64 `json:"metrics,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	CreatedBy     string            `json:"created_by,omitempty"`
}

// RegisterResponse 注册响应
type RegisterResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Model   *modelregistry.ModelArtifact `json:"model,omitempty"`
}

// ListModelsResponse 列出模型响应
type ListModelsResponse struct {
	Models []modelregistry.ModelArtifact `json:"models"`
	Total  int                           `json:"total"`
}

// LineageResponse 血缘关系响应
type LineageResponse struct {
	Root  string                    `json:"root"`
	Nodes []*modelregistry.ModelArtifact `json:"nodes"`
	Edges []modelregistry.LineageEdge `json:"edges"`
	Depth int                       `json:"depth"`
}

// VerifyResponse 验证响应
type VerifyResponse struct {
	Ref              string   `json:"ref"`
	BlobPresent      bool     `json:"blob_present"`
	BlobHashOK       bool     `json:"blob_hash_ok"`
	AttestationFound bool     `json:"attestation_found"`
	RecordDigestOK   bool     `json:"record_digest_ok"`
	ChainVerified    bool     `json:"chain_verified"`
	Tampered         bool     `json:"tampered"`
	Checks           []string `json:"checks"`
}

// StatsResponse 统计信息响应
type StatsResponse struct {
	TotalModels    int            `json:"total_models"`
	TotalVersions  int            `json:"total_versions"`
	TotalBlobs     int            `json:"total_blobs"`
	StorageBytes   int64          `json:"storage_bytes"`
	LastUpdated    time.Time      `json:"last_updated"`
	ModelsByFrame  map[string]int `json:"models_by_framework"`
}

// DeployRequest 部署请求
type DeployRequest struct {
	Endpoint    string            `json:"endpoint" binding:"required"`
	TrafficPerm float64           `json:"traffic_percent,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// DeployResponse 部署响应
type DeployResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	Endpoint    string `json:"endpoint"`
	Status      string `json:"status"`
	HealthCheck string `json:"health_check"`
}

// InitializeM2ModelRegistry 初始化全局模型注册实例
func InitializeM2ModelRegistry(rootPath string, ledger *evidence.Ledger) error {
	reg, err := modelregistry.NewFSRegistry(rootPath, ledger)
	if err != nil {
		return fmt.Errorf("failed to initialize model registry: %w", err)
	}

	globalModelRegistry = reg
	modelLogger.WithField("path", rootPath).Info("M2 Model Registry initialized")
	return nil
}

// SetupM2ModelRoutes 注册 M2 模型管理相关的 HTTP routes
func SetupM2ModelRoutes(router *gin.Engine, logger *logrus.Logger) {
	if logger != nil {
		modelLogger = logger
	}

	modelsGroup := router.Group("/api/v1/models")
	{
		// Model registry operations
		modelsGroup.GET("", handleListModels(modelLogger))
		modelsGroup.POST("", handleRegisterModel(modelLogger))
		
		// Model-specific operations
		modelsGroup.GET("/:name", handleGetModelVersions(modelLogger))
		modelsGroup.GET("/:name/:version", handleGetVersionDetails(modelLogger))
		modelsGroup.DELETE("/:name/:version", handleDeleteVersion(modelLogger))
		
		// Model lifecycle operations
		modelsGroup.POST("/:name/rollback", handleRollback(modelLogger))
		modelsGroup.GET("/:name/lineage", handleGetLineage(modelLogger))
		modelsGroup.POST("/:name/verify", handleVerify(modelLogger))
		
		// Deployment operations
		modelsGroup.POST("/:name/deploy", handleDeploy(modelLogger))
		
		// Statistics
		modelsGroup.GET("/stats", handleGetStats(modelLogger))
	}
}

// @Summary 列出所有已注册的模型
// @Description 返回所有模型或按名称过滤的模型列表
// @Tags models
// @Produce json
// @Param name query string false "模型名称过滤"
// @Success 200 {object} ListModelsResponse
// @Router /api/v1/models [get]
func handleListModels(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Query("name")
		
		ctx := context.Background()
		
		var models []modelregistry.ModelArtifact
		var err error
		
		if name != "" {
			models, err = globalModelRegistry.List(ctx, name)
			if err != nil && err.Error() == "modelregistry: not found" {
				c.JSON(http.StatusOK, ListModelsResponse{Models: []modelregistry.ModelArtifact{}, Total: 0})
				return
			}
		} else {
			models, err = globalModelRegistry.List(ctx, "")
		}
		
		if err != nil {
			logger.WithError(err).Error("Failed to list models")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list models"})
			return
		}
		
		// Sort by created_at descending
		sort.Slice(models, func(i, j int) bool {
			return models[i].CreatedAt.After(models[j].CreatedAt)
		})
		
		c.JSON(http.StatusOK, ListModelsResponse{
			Models: models,
			Total:  len(models),
		})
	}
}

// @Summary 注册新模型
// @Description 注册一个新的模型版本到仓库
// @Tags models
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "模型注册请求"
// @Success 201 {object} RegisterResponse
// @Router /api/v1/models [post]
func handleRegisterModel(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		// Validate artifact path exists
		if _, err := os.Stat(req.ArtifactPath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Artifact file not found: %s", req.ArtifactPath)})
			return
		}
		
		ctx := context.Background()
		
		input := modelregistry.RegisterInput{
			Name:          req.Name,
			Version:       req.Version,
			ArtifactPath:  req.ArtifactPath,
			DatasetRef:    req.DatasetRef,
			CodeRef:       req.CodeRef,
			ParentVersion: req.ParentVersion,
			Hyperparams:   req.Hyperparams,
			TaskType:      req.TaskType,
			Framework:     req.Framework,
			Summary:       req.Summary,
			Metrics:       req.Metrics,
			Tags:          req.Tags,
			CreatedBy:     req.CreatedBy,
		}
		
		artifact, err := globalModelRegistry.Register(ctx, input)
		if err != nil {
			logger.WithError(err).Error("Failed to register model")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"name":    artifact.Name,
			"version": artifact.Version,
		}).Info("Model registered successfully")
		
		c.JSON(http.StatusCreated, RegisterResponse{
			Success: true,
			Message: "Model registered successfully",
			Model:   artifact,
		})
	}
}

// @Summary 获取模型的版本列表
// @Description 获取特定模型的所有版本
// @Tags models
// @Produce json
// @Param name path string true "模型名称"
// @Success 200 {object} ListModelsResponse
// @Router /api/v1/models/{name} [get]
func handleGetModelVersions(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		
		ctx := context.Background()
		models, err := globalModelRegistry.List(ctx, name)
		if err != nil {
			logger.WithError(err).Error("Failed to get model versions")
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model not found: %s", name)})
			return
		}
		
		sort.Slice(models, func(i, j int) bool {
			return models[i].CreatedAt.After(models[j].CreatedAt)
		})
		
		c.JSON(http.StatusOK, ListModelsResponse{
			Models: models,
			Total:  len(models),
		})
	}
}

// @Summary 获取特定版本的详细信息
// @Description 获取模型某个版本的完整信息
// @Tags models
// @Produce json
// @Param name path string true "模型名称"
// @Param version path string true "版本号"
// @Success 200 {object} modelregistry.ModelArtifact
// @Router /api/v1/models/{name}/{version} [get]
func handleGetVersionDetails(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		version := c.Param("version")
		
		ctx := context.Background()
		
		artifact, err := globalModelRegistry.Get(ctx, name, version)
		if err != nil {
			logger.WithError(err).Error("Failed to get model version")
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Version not found: %s:%s", name, version)})
			return
		}
		
		c.JSON(http.StatusOK, artifact)
	}
}

// @Summary 删除模型版本
// @Description 删除指定的模型版本（标记为归档，保留数据）
// @Tags models
// @Produce json
// @Param name path string true "模型名称"
// @Param version path string true "版本号"
// @Success 200 {object} gin.H
// @Router /api/v1/models/{name}/{version} [delete]
func handleDeleteVersion(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		version := c.Param("version")
		
		ctx := context.Background()
		
		// Rollback if this is the current version
		current, _ := globalModelRegistry.Current(name)
		if current == version {
			versions, _ := globalModelRegistry.List(ctx, name)
			if len(versions) > 1 {
				// Switch to another version
				newVersion := versions[0].Version
				if err := globalModelRegistry.Rollback(ctx, name, version, newVersion); err != nil {
					logger.WithError(err).Error("Failed to rollback before delete")
					c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete current version without alternative"})
					return
				}
			}
		}
		
		// Archive the version (set tags.archived = true)
		artifact, err := globalModelRegistry.Get(ctx, name, version)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		
		if artifact.Tags == nil {
			artifact.Tags = make(map[string]string)
		}
		artifact.Tags["archived"] = "true"
		
		// Write updated artifact (re-register with same version)
		err = globalModelRegistry.Register(ctx, modelregistry.RegisterInput{
			Name:        artifact.Name,
			Version:     artifact.Version,
			ArtifactPath: "", // Keep existing blob
			Tags:        artifact.Tags,
		})
		
		if err != nil {
			logger.WithError(err).Error("Failed to archive version")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"name":    name,
			"version": version,
		}).Info("Model version archived")
		
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("Version %s:%s archived", name, version),
		})
	}
}

// @Summary 回滚到指定版本
// @Description 将当前 serving version 回滚到指定版本
// @Tags models
// @Accept json
// @Produce json
// @Param name path string true "模型名称"
// @Param request body DeployRequest true "回滚请求"
// @Success 200 {object} DeployResponse
// @Router /api/v1/models/{name}/rollback [post]
func handleRollback(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		
		var req DeployRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		ctx := context.Background()
		
		// Get current version
		current, err := globalModelRegistry.Current(name)
		if err != nil {
			logger.WithError(err).Warn("No current version set")
		}
		
		err = globalModelRegistry.Rollback(ctx, name, current, req.Endpoint)
		if err != nil {
			logger.WithError(err).Error("Rollback failed")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		logger.WithFields(logrus.Fields{
			"name":        name,
			"from":        current,
			"to":          req.Endpoint,
		}).Info("Model rolled back successfully")
		
		c.JSON(http.StatusOK, DeployResponse{
			Success: true,
			Message: fmt.Sprintf("Rolled back from %s to %s", current, req.Endpoint),
			Endpoint:    req.Endpoint,
			Status:      "rolled_back",
		})
	}
}

// @Summary 获取模型血缘关系
// @Description 获取模型的 lineage DAG，显示训练历史
// @Tags models
// @Produce json
// @Param name path string true "模型名称"
// @Param version path string true "版本号"
// @Success 200 {object} LineageResponse
// @Router /api/v1/models/{name}/lineage [get]
func handleGetLineage(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		version := c.DefaultQuery("version", "latest")
		
		ctx := context.Background()
		
		lineage, err := globalModelRegistry.Lineage(ctx, name, version)
		if err != nil {
			logger.WithError(err).Error("Failed to get lineage")
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, LineageResponse{
			Root:  lineage.Root,
			Nodes: lineage.Nodes,
			Edges: lineage.Edges,
			Depth: lineage.Depth,
		})
	}
}

// @Summary 验证模型完整性
// @Description 检查模型是否被篡改，验证 content-addressing 和 attestation
// @Tags models
// @Produce json
// @Param name path string true "模型名称"
// @Param version path string true "版本号"
// @Success 200 {object} VerifyResponse
// @Router /api/v1/models/{name}/verify [post]
func handleVerify(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		version := c.DefaultQuery("version", "latest")
		
		ctx := context.Background()
		
		report, err := globalModelRegistry.Verify(ctx, name, version)
		if err != nil {
			logger.WithError(err).Error("Verification failed")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		c.JSON(http.StatusOK, VerifyResponse{
			Ref:              report.Ref,
			BlobPresent:      report.BlobPresent,
			BlobHashOK:       report.BlobHashOK,
			AttestationFound: report.AttestationFound,
			RecordDigestOK:   report.RecordDigestOK,
			ChainVerified:    report.ChainVerified,
			Tampered:         report.Tampered,
			Checks:           report.Checks,
		})
	}
}

// @Summary 部署模型到推理端点
// @Description 将模型部署到指定的 inference endpoint
// @Tags models
// @Accept json
// @Produce json
// @Param name path string true "模型名称"
// @Param request body DeployRequest true "部署请求"
// @Success 200 {object} DeployResponse
// @Router /api/v1/models/{name}/deploy [post]
func handleDeploy(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		
		var req DeployRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		
		// Get latest version
		ctx := context.Background()
		versions, err := globalModelRegistry.List(ctx, name)
		if err != nil || len(versions) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("No versions found for model %s", name)})
			return
		}
		
		latest := versions[0]
		
		// Simulate deployment (in production, would trigger actual deployment workflow)
		logger.WithFields(logrus.Fields{
			"name":        name,
			"version":     latest.Version,
			"endpoint":    req.Endpoint,
			"traffic":     req.TrafficPerm,
		}).Info("Model deployment initiated")
		
		c.JSON(http.StatusOK, DeployResponse{
			Success:     true,
			Message:     fmt.Sprintf("Deployed %s:%s to %s", name, latest.Version, req.Endpoint),
			Endpoint:    req.Endpoint,
			Status:      "deploying",
			HealthCheck: "pending",
		})
	}
}

// @Summary 获取模型仓库统计信息
// @Description 返回模型仓库的使用情况和存储统计
// @Tags models
// @Produce json
// @Success 200 {object} StatsResponse
// @Router /api/v1/models/stats [get]
func handleGetStats(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()
		
		// Get all models
		models, err := globalModelRegistry.List(ctx, "")
		if err != nil {
			logger.WithError(err).Error("Failed to get stats")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute statistics"})
			return
		}
		
		// Count models by framework
		modelsByFramework := make(map[string]int)
		totalVersions := 0
		
		for _, m := range models {
			totalVersions++
			if m.ModelCard.Framework != "" {
				modelsByFramework[m.ModelCard.Framework]++
			}
		}
		
		// Calculate storage size
		blobsDir := filepath.Join(globalModelRegistry.Root(), "blobs")
		var totalSize int64 = 0
		blobCount := 0
		
		entries, err := os.ReadDir(blobsDir)
		if err == nil {
			blobCount = len(entries)
			for _, entry := range entries {
				if !entry.IsDir() {
					info, _ := entry.Info()
					totalSize += info.Size()
				}
			}
		}
		
		// Count unique model names
		modelNames := make(map[string]bool)
		for _, m := range models {
			modelNames[m.Name] = true
		}
		
		stats := StatsResponse{
			TotalModels:   len(modelNames),
			TotalVersions: totalVersions,
			TotalBlobs:    blobCount,
			StorageBytes:  totalSize,
			LastUpdated:   time.Now().UTC(),
			ModelsByFrame: modelsByFramework,
		}
		
		if len(models) > 0 {
			// Find most recent update
			lastUpdated := models[0].CreatedAt
			for _, m := range models {
				if m.CreatedAt.After(lastUpdated) {
					lastUpdated = m.CreatedAt
				}
			}
			stats.LastUpdated = lastUpdated
		}
		
		c.JSON(http.StatusOK, stats)
	}
}
