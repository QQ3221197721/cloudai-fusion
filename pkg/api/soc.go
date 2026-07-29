// Package api - soc.go exposes the Operations-layer wells (L3-L8) over HTTP under
// /api/v1/soc: submit telemetry for detection (endpoint/network/workload/identity/
// image), list findings and SOAR playbooks, and orchestrate a response for a
// finding. Detection/list are security-read; analysis and response are
// security-manage (wired in router.go). Every analysis/response records a signed
// evidence receipt inside the engine.
package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
)

// handleSOCFindings lists recent findings (newest first). Optional ?limit=N.
func handleSOCFindings(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 100
		if v := c.Query("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		findings := eng.Findings(limit)
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

// handleSOCPlaybooks lists the L8 SOAR playbooks.
func handleSOCPlaybooks(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		pbs := eng.Playbooks()
		c.JSON(http.StatusOK, gin.H{"playbooks": pbs, "total": len(pbs)})
	}
}

// handleSOCMitigations lists the currently-active mitigations the L8 actuator has
// applied (e.g. blocked networks, isolated hosts) — the observable effect of
// automated responses, with each actuator's real-vs-simulated nature reflected.
func handleSOCMitigations(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		mits := eng.ActiveMitigations()
		c.JSON(http.StatusOK, gin.H{
			"mitigations":   mits,
			"total":         len(mits),
			"actuator":      eng.Actuator().Name(),
			"actuator_real": eng.Actuator().IsReal(),
		})
	}
}

// endpointAnalyzeRequest is the body for POST /soc/analyze/endpoint (L3).
type endpointAnalyzeRequest struct {
	Host       string   `json:"host"`
	FileHashes []string `json:"file_hashes"`
}

// detectRequest is the body for POST /soc/detect (L3-L7 Sigma log detection).
type detectRequest struct {
	Category string           `json:"category"`
	Events   []map[string]any `json:"events"`
}

// handleSOCDetect runs the Sigma detection engine over a batch of structured log
// events and returns (and stores) the resulting findings.
func handleSOCDetect(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req detectRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid detect request: "+err.Error(), nil))
			return
		}
		if req.Category == "" {
			apperrors.RespondError(c, apperrors.Validation("category is required", nil))
			return
		}
		findings, err := eng.AnalyzeLogs(c.Request.Context(), req.Category, req.Events)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("sigma detection failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"findings":    findings,
			"total":       len(findings),
			"rules_total": eng.SigmaRuleCount(),
		})
	}
}

func handleSOCAnalyzeEndpoint(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req endpointAnalyzeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid endpoint request: "+err.Error(), nil))
			return
		}
		findings, err := eng.AnalyzeEndpoint(c.Request.Context(), req.Host, req.FileHashes)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("endpoint analysis failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

// networkAnalyzeRequest is the body for POST /soc/analyze/network (L4).
type networkAnalyzeRequest struct {
	Host    string   `json:"host"`
	IPs     []string `json:"ips"`
	Domains []string `json:"domains"`
}

func handleSOCAnalyzeNetwork(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req networkAnalyzeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid network request: "+err.Error(), nil))
			return
		}
		findings, err := eng.AnalyzeNetwork(c.Request.Context(), req.Host, req.IPs, req.Domains)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("network analysis failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

func handleSOCAnalyzeWorkload(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var spec soc.WorkloadSpec
		if err := c.ShouldBindJSON(&spec); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid workload request: "+err.Error(), nil))
			return
		}
		findings, err := eng.AnalyzeWorkload(c.Request.Context(), spec)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("workload analysis failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

// identityAnalyzeRequest is the body for POST /soc/analyze/identity (L6).
type identityAnalyzeRequest struct {
	Events []soc.AuthEvent `json:"events"`
}

func handleSOCAnalyzeIdentity(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req identityAnalyzeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid identity request: "+err.Error(), nil))
			return
		}
		findings, err := eng.AnalyzeIdentity(c.Request.Context(), req.Events)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("identity analysis failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

func handleSOCAnalyzeImage(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var scan soc.ImageScan
		if err := c.ShouldBindJSON(&scan); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid image request: "+err.Error(), nil))
			return
		}
		findings, err := eng.AnalyzeImage(c.Request.Context(), scan)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("image analysis failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

// handleSOCRespond runs the L8 SOAR orchestrator for a stored finding.
func handleSOCRespond(eng *soc.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp, err := eng.Respond(c.Request.Context(), c.Param("id"))
		if err != nil {
			apperrors.RespondError(c, apperrors.NotFound("finding", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// handleSOCCollectEndpoint runs a real (or simulated) EDR collection on the host
// and feeds observed executable hashes through L3 detection. The response reports
// the collector's real-vs-simulated mode so the run is never a silent simulation.
func handleSOCCollectEndpoint(eng *soc.Engine, collector soc.EDRCollector) gin.HandlerFunc {
	return func(c *gin.Context) {
		findings, err := eng.CollectEndpoint(c.Request.Context(), collector)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("edr collection failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"findings":  findings,
			"total":     len(findings),
			"collector": collector.Name(),
			"real":      collector.IsReal(),
		})
	}
}
