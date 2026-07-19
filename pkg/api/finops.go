// Package api - finops.go serves PROVABLE FinOps. POST /finops/reclaim measures
// idleness from real utilization samples, reclaims the resource, and returns a
// signed SavingsReceipt; GET /finops/savings aggregates only the MEASURED savings
// (each backed by a receipt in the evidence ledger). This is the antidote to
// "nobody trusts the cost dashboard": here every dollar is verifiable.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/finops"
)

// finopsReclaimAction mirrors the evidence Action emitted by the reclaim engine.
const finopsReclaimAction = "finops.reclaim"

// reclaimRequest is the POST /finops/reclaim body.
type reclaimRequest struct {
	Target  finops.ReclaimTarget       `json:"target" binding:"required"`
	Samples []finops.UtilizationSample `json:"samples" binding:"required"`
}

// handleFinOpsReclaim proves idleness, reclaims, and returns a signed receipt.
func handleFinOpsReclaim(engine *finops.ReclaimEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req reclaimRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apperrors.RespondValidationError(c, err)
			return
		}
		receipt, ev, err := engine.Reclaim(c.Request.Context(), req.Target, req.Samples)
		if err != nil {
			if err == finops.ErrNotIdle {
				apperrors.RespondError(c, apperrors.Validation("resource is not idle by the sampled utilization; refusing to claim savings", nil))
				return
			}
			apperrors.RespondError(c, apperrors.Internal("reclaim failed", err))
			return
		}
		resp := gin.H{"receipt": receipt}
		if ev != nil {
			resp["evidence_id"] = ev.ID
			resp["evidence_hash"] = ev.Hash
		}
		c.JSON(http.StatusOK, resp)
	}
}

// handleFinOpsSavings aggregates measured, receipted savings from the ledger.
func handleFinOpsSavings(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		records, err := l.Store().List(c.Request.Context(), evidence.Filter{
			Action: finopsReclaimAction,
			Limit:  500,
		})
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("savings query failed", err))
			return
		}

		var (
			measuredUSD   float64
			measuredHours float64
			reportedUSD   float64
			measuredCount int
		)
		receipts := make([]finops.SavingsReceipt, 0, len(records))
		for _, rec := range records {
			var sr finops.SavingsReceipt
			if err := json.Unmarshal(rec.Payload, &sr); err != nil {
				continue // skip malformed payloads rather than fail the whole query
			}
			receipts = append(receipts, sr)
			reportedUSD += sr.RealizedSavingsUSD
			if sr.Measured {
				measuredUSD += sr.RealizedSavingsUSD
				measuredHours += sr.ReclaimedGPUHours
				measuredCount++
			}
		}

		c.JSON(http.StatusOK, gin.H{
			// measured_* counts ONLY receipts whose reclaim + utilization were real.
			"measured_savings_usd": measuredUSD,
			"measured_gpu_hours":   measuredHours,
			"measured_receipts":    measuredCount,
			"reported_savings_usd": reportedUSD, // includes simulated reclaims (not proven)
			"total_receipts":       len(receipts),
			"receipts":             receipts,
			"note":                 "measured_* figures are backed by signed receipts with real reclaim+utilization backends; verify at GET /api/v1/evidence/export",
		})
	}
}
