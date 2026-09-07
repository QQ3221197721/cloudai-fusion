// Package apiserver - TEE Attestation HTTP Endpoints
// ============================================================================
// Purpose: 为 TEE Remote Attestation 功能提供 HTTP API 接口，让用户可以通过
//          浏览器/curl 直接调用和测试。
//          
// Endpoints:
//   POST /api/v1/tee/attest          - 执行 attest 请求（main path）
//   GET  /api/v1/tee/status           - 查询当前系统状态（GPU 拓扑/能力）
//   GET  /api/v1/tee/stats            - 返回统计算法（cache hits/misses）
//   POST /api/v1/tee/enclave/create   - 手动创建 enclave（用于调试）
//
// Usage example:
//   curl -X POST http://localhost:8080/api/v1/tee/attest \
//     -H "Content-Type: application/json" \
//     -d '{"enclave_id": "my-app", "nonce": "abc123"}'
// ============================================================================

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/tee"
)

var (
	// Global attestation instance（需要在启动时初始化）
	globalAttestor *tee.UnifiedAttestor
	logger         = logrus.New()
)

// AttestRequest 客户端发起的证明请求
type AttestRequest struct {
	EnclaveID string `json:"enclave_id"`    // 目标 enclave 标识
	Nonce     string `json:"nonce"`         // 客户端挑战值（base64 编码）
	Mode      string `json:"mode,omitempty"` // fastest/reliable/batch
}

// AttestResponse 证明响应
type AttestResponse struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	SessionToken  string `json:"session_token,omitempty"`  // 签发的 token（base64）
	Measurement   string `json:"measurement,omitempty"`    // MRENCLAVE（仿真模式下为空）
	Capability    string `json:"capability"`               // 本机能力（no-sgx/nvlink-dominant/etc.）
	DurationMs    int64  `json:"duration_ms"`              // 耗时（微秒级优化验证）
	Trusted       bool   `json:"trusted"`                  // 是否硬件可信（仿真模式永远 false）
	ModeUsed      string `json:"mode_used"`                // 实际使用的操作模式
}

// StatusResponse 系统状态
type StatusResponse struct {
	Available    bool   `json:"available"`
	OS           string `json:"os"`
	HasSGX       bool   `json:"has_sgx"`
	GPUSupported bool   `json:"gpu_supported"`
	GPUTopoMode  string `json:"gpu_topo_mode"`
	NvLinkLinks  int    `json:"nvlink_links"`
}

// StatsResponse 统计信息
type StatsResponse struct {
	TotalRequests int64 `json:"total_requests"`
	CacheHits     int64 `json:"cache_hits"`
	CacheMisses   int64 `json:"cache_misses"`
	BatchesUsed   int64 `json:"batches_used"`
	SavedTimeSec  float64 `json:"saved_time_sec"`
}

// InitializeTEEAttestation 初始化全局 attestation 实例（在 apiserver 启动时调用）
func InitializeTEEAttestation(mode tee.AttestorMode) error {
	// 创建 BoundAttestor（默认 simulation 模式）
	ba, err := tee.NewBoundAttestor(tee.WithSimulation())
	if err != nil {
		return err
	}

	// 创建 UnifiedAttestor
	globalAttestor = tee.NewUnifiedAttestor(ba, mode)
	
	logger.WithField("mode", mode).Info("TEE attestation initialized")
	return nil
}

// SetupTEERoutes 注册 TEE 相关的 HTTP routes
func SetupTEERoutes(router *gin.Engine, logger *logrus.Logger) {
	teeGroup := router.Group("/api/v1/tee")
	{
		// Main attest endpoint
		teeGroup.POST("/attest", handleAttest(logger))
		
		// Status endpoint
		teeGroup.GET("/status", handleStatus(logger))
		
		// Stats endpoint
		teeGroup.GET("/stats", handleStats(logger))
		
		// Debug endpoint: manual enclave creation
		teeGroup.POST("/enclave/create", handleCreateEnclave(logger))
	}
}

// handleAttest 处理 attest 请求
func handleAttest(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		
		var req AttestRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid-json",
				"message": err.Error(),
			})
			return
		}
		
		if req.EnclaveID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "missing-enclave-id",
			})
			return
		}
		
		if req.Mode == "" {
			req.Mode = "reliable" // default
		}
		
		// Decode nonce from base64
		nonceBytes, err := base64.StdEncoding.DecodeString(req.Nonce)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid-nonce-format",
				"message": "nonce must be base64 encoded",
			})
			return
		}
		
		if len(nonceBytes) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "empty-nonce",
			})
			return
		}
		
		log.WithFields(logrus.Fields{
			"enclave_id": req.EnclaveID,
			"mode":       req.Mode,
			"nonce_len":  len(nonceBytes),
		}).Info("Processing attestation request")
		
		// Execute attestation
		tok, reattested, err := globalAttestor.Attest(context.Background(), req.EnclaveID, nonceBytes)
		
		durationMs := int64(time.Since(start).Milliseconds())
		
		resp := AttestResponse{
			Success:    err == nil,
			Message:    "",
			Trusted:    false,
			ModeUsed:   req.Mode,
			DurationMs: durationMs,
		}
		
		if err != nil {
			resp.Message = "attestation-failed"
			resp.Success = false
			
			log.WithError(err).Warn("Attestation failed")
			
			c.JSON(http.StatusInternalServerError, resp)
			return
		}
		
		if tok == nil {
			resp.Message = "no-token-generated"
			c.JSON(http.StatusInternalServerError, resp)
			return
		}
		
		// Success!
		resp.Success = true
		resp.Message = "attestation-success"
		
		// Serialize token to JSON + base64 for transport
		tokenJSON, _ := json.Marshal(tok)
		resp.SessionToken = base64.StdEncoding.EncodeToString(tokenJSON)
		
		// Add capability info
		cap := tee.DetectSGXCapability()
		if cap.Available {
			resp.Capability = "sgx-enabled"
			if cap.DCAPReady {
				resp.Capability = "sgx-dcap-ready"
			}
		} else {
			resp.Capability = "simulation-mode"
		}
		
		// Note: measurement will be empty in simulation mode
		// Trusted=false always unless real DCAP backend is integrated
		
		log.WithFields(logrus.Fields{
			"restarted": reattested,
			"tok_id":    tok.SessionID,
			"duration":  fmt.Sprintf("%dms", durationMs),
		}).Info("Attestation completed")
		
		c.JSON(http.StatusOK, resp)
	}
}

// handleStatus 处理 status 请求
func handleStatus(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Detect SGX capability
		cap := tee.DetectSGXCapability()
		
		// Detect GPU topology if available
		var gpuReport *tee.TopologyReport
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		
		gpuReport, _ = tee.DetectGPUTopology(ctx) // 忽略错误，无 GPU 就空报告
		
		resp := StatusResponse{
			Available:    true,
			OS:           cap.OS,
			HasSGX:       cap.Available,
			GPUSupported: gpuReport != nil && len(gpuReport.GPUs) > 0,
			NvLinkLinks:  0,
		}
		
		if gpuReport != nil {
			resp.GPUSupported = len(gpuReport.GPUs) > 0
			resp.GPUTopoMode = gpuReport.DominantMode
			resp.NvLinkLinks = gpuReport.NvLinkLinks
		}
		
		log.WithField("sgx", cap.Available).Info("Status requested")
		
		c.JSON(http.StatusOK, resp)
	}
}

// handleStats 处理 stats 请求
func handleStats(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if globalAttestor == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "attestation-not-initialized",
			})
			return
		}
		
		stats := globalAttestor.Stats()
		
		resp := StatsResponse{
			TotalRequests: stats.TotalRequests,
			CacheHits:     stats.CacheHits,
			CacheMisses:   stats.CacheMisses,
			BatchesUsed:   stats.BatchesUsed,
			SavedTimeSec:  stats.SavedTime.Seconds(),
		}
		
		log.WithFields(logrus.Fields{
			"total":  resp.TotalRequests,
			"hits":   resp.CacheHits,
			"misses": resp.CacheMisses,
		}).Info("Stats requested")
		
		c.JSON(http.StatusOK, resp)
	}
}

// handleCreateEnclave 手动创建 enclave（用于调试）
func handleCreateEnclave(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if globalAttestor == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "attestation-not-initialized",
			})
			return
		}
		
		var req struct {
			EnclaveID string `json:"enclave_id"`
			Nonce     string `json:"nonce"`
		}
		
		if err := c.ShouldBindJSON(&req); err != nil || req.EnclaveID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "missing-enclave-id",
			})
			return
		}
		
		nonceBytes, _ := base64.StdEncoding.DecodeString(req.Nonce)
		tok, reattested, err := globalAttestor.Attest(context.Background(), req.EnclaveID, nonceBytes)
		
		if err != nil || tok == nil {
			log.WithError(err).Error("Failed to create enclave")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "creation-failed",
			})
			return
		}
		
		log.WithFields(logrus.Fields{
			"id":       req.EnclaveID,
			"reattested": reattested,
		}).Info("Manual enclave created")
		
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"token":   base64.StdEncoding.EncodeToString([]byte(tok.SessionID)),
		})
	}
}
