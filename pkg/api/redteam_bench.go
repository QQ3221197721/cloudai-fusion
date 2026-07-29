// Package api - redteam_bench.go exposes CVE-Bench v2 and the ephemeral range
// farm over HTTP under /api/v1/redteam. These endpoints are additive over
// redteam.go: the benchmark runs the deterministic, evidence-verified built-in
// suite (no external tools), and the range endpoints manage throwaway targets
// via a RangeManager. Mutating routes require security-manage; reads require
// security-read (wired in router.go).
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

// handleRedTeamBenchmarkCases lists the built-in CVE-Bench v2 cases (metadata
// only: name + expected technique), so operators can see what the suite covers.
func handleRedTeamBenchmarkCases() gin.HandlerFunc {
	return func(c *gin.Context) {
		cases := redteam.DefaultBenchSuite()
		out := make([]gin.H, 0, len(cases))
		for _, bc := range cases {
			out = append(out, gin.H{
				"name":             bc.Name,
				"expect_technique": bc.ExpectFindingTechnique,
				"actions":          len(bc.Actions),
			})
		}
		c.JSON(http.StatusOK, gin.H{"cases": out, "total": len(out)})
	}
}

// handleRedTeamBenchmarkRun runs the built-in suite and returns per-case results
// and aggregate metrics (solve rate, scope violations, receipts-verified). The
// run is deterministic and self-contained, so it doubles as a CI regression gate.
func handleRedTeamBenchmarkRun(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		results, metrics, err := redteam.RunDefaultSuite(c.Request.Context(), logger)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("benchmark run failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"metrics": metrics, "results": results})
	}
}

// createRangeRequest is the body for POST /redteam/ranges.
type createRangeRequest struct {
	Name      string   `json:"name"`
	Apps      []string `json:"apps"`
	Manifests []string `json:"manifests"`
}

// handleRedTeamRangeCreate provisions an ephemeral practice/eval range.
func handleRedTeamRangeCreate(mgr *redteam.RangeManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createRangeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondError(c, apperrors.Validation("invalid range request: "+err.Error(), nil))
			return
		}
		r, err := mgr.Provision(c.Request.Context(), redteam.RangeSpec{
			Name:      req.Name,
			Apps:      req.Apps,
			Manifests: req.Manifests,
		})
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("range provisioning failed", err))
			return
		}
		c.JSON(http.StatusCreated, r)
	}
}

// handleRedTeamRangeList returns all tracked ranges.
func handleRedTeamRangeList(mgr *redteam.RangeManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		ranges := mgr.List()
		c.JSON(http.StatusOK, gin.H{"ranges": ranges, "total": len(ranges)})
	}
}

// handleRedTeamRangeGet returns a single range by ID.
func handleRedTeamRangeGet(mgr *redteam.RangeManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		r, ok := mgr.Get(c.Param("id"))
		if !ok {
			apperrors.RespondError(c, apperrors.NotFound("range", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, r)
	}
}

// handleRedTeamRangeTeardown tears down a range by ID.
func handleRedTeamRangeTeardown(mgr *redteam.RangeManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := mgr.Teardown(c.Request.Context(), c.Param("id")); err != nil {
			apperrors.RespondError(c, apperrors.NotFound("range", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "torn_down", "range_id": c.Param("id")})
	}
}
