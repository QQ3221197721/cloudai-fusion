// Package api - scheduling.go serves the verifiable scheduling decision record.
// These endpoints read "schedule.bind" receipts from the evidence ledger so a
// tenant can ask "why did my task land there / who got preempted / was it fair?"
// and get a signed, independently-verifiable answer (not a "trust us" log line).
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// schedulingBindAction is the evidence Action emitted by the scheduler on bind.
const schedulingBindAction = "schedule.bind"

// handleSchedulingDecisions lists recent scheduling-decision receipts, newest
// first. Optional ?workload=<id> narrows to a single workload's decisions.
func handleSchedulingDecisions(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		records, err := l.Store().List(c.Request.Context(), evidence.Filter{
			Action:  schedulingBindAction,
			Subject: c.Query("workload"),
			Limit:   200,
		})
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("scheduling decisions query failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"decisions": records, "total": len(records)})
	}
}

// handleSchedulingDecisionByWorkload returns the decision receipt(s) for one
// workload plus a verification of each receipt's signature and chain hash, so the
// caller sees both the reasoning AND proof the record was not tampered with.
func handleSchedulingDecisionByWorkload(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		records, err := l.Store().List(c.Request.Context(), evidence.Filter{
			Action:  schedulingBindAction,
			Subject: c.Param("workloadID"),
			Limit:   50,
		})
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("scheduling decision query failed", err))
			return
		}
		if len(records) == 0 {
			apperrors.RespondError(c, apperrors.NotFound("scheduling decision", c.Param("workloadID")))
			return
		}
		// Verify each returned receipt individually (hash + signature) against the
		// ledger's key. Chain-linkage across the whole ledger is a separate check
		// (GET /api/v1/evidence/verify); here the records are a workload-filtered
		// subset, so only per-record integrity is asserted.
		pub := l.Signer().PublicKey()
		results := make([]evidence.RecordResult, 0, len(records))
		allOK := true
		for _, r := range records {
			res := evidence.VerifyRecord(r, pub)
			if !res.HashOK || !res.SignatureOK {
				allOK = false
			}
			results = append(results, res)
		}
		c.JSON(http.StatusOK, gin.H{
			"decisions":    records,
			"verified":     allOK,
			"verification": results,
			"key_id":       l.Signer().KeyID(),
		})
	}
}
