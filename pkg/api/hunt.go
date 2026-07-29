// Package api - hunt.go exposes the L2 Threat Hunting well and the L1 intel sync
// trigger over HTTP. POST /api/v1/hunt runs a correlation hunt (CVEs + IOCs →
// MITRE ATT&CK-mapped findings); POST /api/v1/intel/sync triggers an offline
// feed synchronization on the L1 Hub. Both require security-read/manage
// respectively (wired in router.go). These endpoints make L1/L2 genuinely
// reachable (not merely instantiated), satisfying their wired readiness claim.
package api

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/hunt"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// handleHuntRun runs an L2 correlation hunt from the request query.
func handleHuntRun(eng *hunt.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var q hunt.Query
		if err := c.ShouldBindJSON(&q); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid hunt query: "+err.Error(), nil))
			return
		}
		findings, err := eng.Hunt(c.Request.Context(), q)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("hunt failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

// behaviorRequest is the body for POST /hunt/behavior (L2 UEBA). Train
// observations warm the baseline (no findings); observe observations are scored.
type behaviorRequest struct {
	Name    string             `json:"name"`
	Train   []hunt.Observation `json:"train,omitempty"`
	Observe []hunt.Observation `json:"observe"`
}

// handleHuntBehavior runs L2 UEBA behavior analysis: it optionally warms the
// per-entity baseline with `train`, then scores `observe` for statistical
// anomalies (numeric deviation, rare/first-seen categories) → MITRE findings.
func handleHuntBehavior(eng *hunt.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req behaviorRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid behavior request: "+err.Error(), nil))
			return
		}
		if len(req.Train) > 0 {
			eng.TrainBehavior(req.Train)
		}
		findings, err := eng.AnalyzeBehavior(c.Request.Context(), req.Name, req.Observe)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("behavior analysis failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"findings": findings, "total": len(findings)})
	}
}

// handleIntelSync triggers an L1 offline feed synchronization on the Hub.
func handleIntelSync(hub *intel.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		res, err := hub.SyncAll(c.Request.Context())
		if err != nil {
			// SyncAll returns a partial result plus an error when some sources fail;
			// surface both so the caller sees what did import.
			c.JSON(http.StatusOK, gin.H{"result": res, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"result": res})
	}
}

// maxSTIXBundleBytes caps a pushed STIX bundle to protect memory (16 MiB).
const maxSTIXBundleBytes = 16 << 20

// handleIntelSTIX ingests a STIX 2.1 bundle pushed in the request body (the
// push-model integration for MISP/OTX exports). It upserts IOCs/CVEs/techniques
// into L1 and returns the sync result.
func handleIntelSTIX(hub *intel.Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSTIXBundleBytes))
		if err != nil {
			apperrors.RespondError(c, apperrors.Validation("read STIX body: "+err.Error(), nil))
			return
		}
		res, err := hub.ImportSTIXBundle(c.Request.Context(), body)
		if err != nil {
			// Return the partial result plus the error (some objects may have imported).
			c.JSON(http.StatusOK, gin.H{"result": res, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"result": res})
	}
}
