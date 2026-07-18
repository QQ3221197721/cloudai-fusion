// Package api - redteam.go exposes the Verifiable AI Red Team subsystem over
// HTTP (/api/v1/redteam). Engagement lifecycle (create/get/list/abort), the
// human-in-the-loop approval, and the verifiable report + evidence are surfaced
// here. Mutating routes require security-manage; reads require security-read.
// Every action already records a signed receipt inside the subsystem, so these
// endpoints are a thin, additive control surface over pkg/redteam.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

// createEngagementRequest is the body for POST /redteam/engagements.
type createEngagementRequest struct {
	TenantID string        `json:"tenant_id"`
	Scope    redteam.Scope `json:"scope"`
}

// principalOf returns the authenticated username (set by auth middleware), or a
// safe default.
func principalOf(c *gin.Context) string {
	if u := c.GetString("username"); u != "" {
		return u
	}
	return "api"
}

// handleRedTeamCreate creates an engagement from a signed scope.
func handleRedTeamCreate(mgr *redteam.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createEngagementRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid engagement request: "+err.Error(), nil))
			return
		}
		principal := principalOf(c)
		var (
			e   *redteam.Engagement
			err error
		)
		if req.TenantID != "" {
			e, err = mgr.CreateForTenant(c.Request.Context(), req.TenantID, req.Scope, principal)
		} else {
			e, err = mgr.Create(c.Request.Context(), req.Scope, principal)
		}
		if err != nil {
			apperrors.RespondError(c, apperrors.Validation(err.Error(), nil))
			return
		}
		c.JSON(http.StatusCreated, e)
	}
}

// handleRedTeamList lists engagements (optionally filtered by tenant_id).
func handleRedTeamList(mgr *redteam.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var list []*redteam.Engagement
		if tenant := c.Query("tenant_id"); tenant != "" {
			list = mgr.ListByTenant(tenant)
		} else {
			list = mgr.List()
		}
		c.JSON(http.StatusOK, gin.H{"engagements": list, "total": len(list)})
	}
}

// handleRedTeamGet returns a single engagement (status + findings + scope).
func handleRedTeamGet(mgr *redteam.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		e, err := mgr.Get(c.Param("id"))
		if err != nil {
			apperrors.RespondError(c, apperrors.NotFound("engagement", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, e)
	}
}

// handleRedTeamAbort trips the kill-switch for an engagement.
func handleRedTeamAbort(mgr *redteam.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Reason string `json:"reason"`
		}
		_ = c.ShouldBindJSON(&req)
		if req.Reason == "" {
			req.Reason = "api kill-switch"
		}
		if err := mgr.Abort(c.Request.Context(), c.Param("id"), req.Reason); err != nil {
			apperrors.RespondError(c, apperrors.NotFound("engagement", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "aborted", "engagement_id": c.Param("id")})
	}
}

// handleRedTeamApprove records a human approval for a pending high-risk action.
func handleRedTeamApprove(mgr *redteam.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			ActionID string `json:"action_id"`
			RiskTier int    `json:"risk_tier"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.ActionID == "" {
			apperrors.RespondError(c, apperrors.Validation("approve requires action_id", nil))
			return
		}
		e, err := mgr.Get(c.Param("id"))
		if err != nil {
			apperrors.RespondError(c, apperrors.NotFound("engagement", c.Param("id")))
			return
		}
		e.Gate().Approve(c.Request.Context(), req.ActionID, principalOf(c), true, redteam.RiskTier(req.RiskTier))
		c.JSON(http.StatusOK, gin.H{"approved": true, "action_id": req.ActionID})
	}
}

// handleRedTeamReport builds a verifiable engagement report (embeds a signed
// checkpoint). Requires the evidence ledger.
func handleRedTeamReport(mgr *redteam.Manager, l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		e, err := mgr.Get(c.Param("id"))
		if err != nil {
			apperrors.RespondError(c, apperrors.NotFound("engagement", c.Param("id")))
			return
		}
		all, err := l.Store().All(c.Request.Context())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence read failed", err))
			return
		}
		rep, err := redteam.BuildReport(c.Request.Context(), e, all, l, l, nil)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("report build failed", err))
			return
		}
		c.JSON(http.StatusOK, rep)
	}
}

// handleRedTeamEvidence returns the receipts belonging to an engagement (no side
// effects). Requires the evidence ledger.
func handleRedTeamEvidence(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		all, err := l.Store().All(c.Request.Context())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence read failed", err))
			return
		}
		recs := redteam.EngagementReceipts(all, c.Param("id"))
		c.JSON(http.StatusOK, gin.H{"engagement_id": c.Param("id"), "receipts": recs, "total": len(recs)})
	}
}
