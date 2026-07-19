// Package api - fabric.go exposes the Verifiable Knowledge Graph (VKG) over HTTP.
// One query answers "show me every signed receipt correlated to <key>, across all
// pillars" - the Verifiable Fabric's interconnect made usable to operators and
// auditors, projected live from the same signed, tamper-evident ledger.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
)

// handleEvidenceLineage returns the VKG lineage for ?key=<k>: every receipt
// correlated to the key (by PCA correlation or Subject), Seq ascending. The
// receipts are ordinary signed ledger receipts, so each is independently
// verifiable (inclusion proof / chain export); this endpoint is the cross-pillar
// index over them.
func handleEvidenceLineage(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Query("key")
		if key == "" {
			apperrors.RespondError(c, apperrors.Validation("query parameter 'key' is required", nil))
			return
		}
		all, err := l.Store().All(c.Request.Context())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence read failed", err))
			return
		}
		g := fabric.NewGraph(fabric.PCACorrelations, fabric.BySubject)
		g.AddAll(all)
		lineage := g.Lineage(key)
		c.JSON(http.StatusOK, gin.H{
			"key":      key,
			"count":    len(lineage),
			"receipts": lineage,
		})
	}
}
