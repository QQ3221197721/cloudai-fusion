// Package api - evidence.go exposes the Verifiable Control Plane over HTTP.
// These endpoints let an operator list and inspect signed receipts, verify the
// whole chain server-side, and — most importantly — EXPORT the chain plus the
// public key so a third party can verify it OFFLINE (cmd/cafctl verify). The
// public key endpoint is intentionally unauthenticated so anyone can pin it.
package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// handleEvidenceSummary returns high-level ledger status: size, signing key id,
// run mode, and how many records carry a real (Rekor) transparency anchor.
func handleEvidenceSummary(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		count, err := l.Store().Count(ctx)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence count failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"key_id":         l.Signer().KeyID(),
			"count":          count,
			"anchor_backend": l.Anchorer().Backend(),
			"anchor_real":    l.Anchorer().Real(),
			"verify_hint":    "GET /api/v1/evidence/export | cafctl verify --bundle - --pubkey <pinned.pem>",
		})
	}
}

// handleEvidenceList returns receipts newest-first, filterable by action/subject/actor.
func handleEvidenceList(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 100
		if v := c.Query("limit"); v != "" {
			if _, err := fmt.Sscanf(v, "%d", &limit); err != nil || limit <= 0 {
				limit = 100
			}
		}
		records, err := l.Store().List(c.Request.Context(), evidence.Filter{
			Action:  c.Query("action"),
			Subject: c.Query("subject"),
			Actor:   c.Query("actor"),
			Limit:   limit,
		})
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence list failed", err))
			return
		}
		c.JSON(http.StatusOK, gin.H{"records": records, "total": len(records)})
	}
}

// handleEvidenceGet returns a single receipt by ID.
func handleEvidenceGet(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		rec, err := l.Store().Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence get failed", err))
			return
		}
		if rec == nil {
			apperrors.RespondError(c, apperrors.NotFound("evidence", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, rec)
	}
}

// handleEvidenceVerify verifies the entire chain server-side against the ledger's
// own public key and returns the per-record report. (An auditor should still
// verify offline with a pinned key — see /export.)
func handleEvidenceVerify(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		all, err := l.Store().All(c.Request.Context())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence read failed", err))
			return
		}
		report, err := evidence.VerifyChain(all, l.Signer().PublicKey())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence verify failed", err))
			return
		}
		evidence.ObserveVerify(report.Valid)
		status := http.StatusOK
		if !report.Valid {
			status = http.StatusConflict // 409: the ledger does not verify
		}
		c.JSON(status, report)
	}
}

// handleEvidenceExport returns the full chain plus the public key as a portable
// bundle for offline verification.
func handleEvidenceExport(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		bundle, err := l.Export(c.Request.Context())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence export failed", err))
			return
		}
		c.JSON(http.StatusOK, bundle)
	}
}

// handleEvidencePubKey returns the signing public key as PEM. Unauthenticated by
// design: publishing the verifying key is how third parties establish trust.
func handleEvidencePubKey(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		pemBytes, err := evidence.MarshalPublicKeyPEM(l.Signer().PublicKey())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence pubkey failed", err))
			return
		}
		c.Header("X-Evidence-Key-Id", l.Signer().KeyID())
		c.Data(http.StatusOK, "application/x-pem-file", pemBytes)
	}
}

// handleEvidenceCheckpoint returns a signed Merkle tree head (STH) committing to
// the entire ledger. Pin it out-of-band, then prove inclusion/consistency against it.
func handleEvidenceCheckpoint(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		cp, err := l.Checkpoint(c.Request.Context())
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence checkpoint failed", err))
			return
		}
		c.JSON(http.StatusOK, cp)
	}
}

// handleEvidenceInclusionProof returns an RFC 6962 inclusion proof for a receipt
// plus the signed checkpoint it is proven against.
func handleEvidenceInclusionProof(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		proof, err := l.InclusionProofByID(c.Request.Context(), c.Param("id"))
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("evidence inclusion proof failed", err))
			return
		}
		if proof == nil {
			apperrors.RespondError(c, apperrors.NotFound("evidence", c.Param("id")))
			return
		}
		c.JSON(http.StatusOK, proof)
	}
}

// handleEvidenceConsistency returns an append-only consistency proof between two
// tree sizes (?from=N&to=M; to omitted => current size), proving no history was
// rewritten.
func handleEvidenceConsistency(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var from, to int
		if v := c.Query("from"); v != "" {
			if _, err := fmt.Sscanf(v, "%d", &from); err != nil {
				apperrors.RespondError(c, apperrors.Validation("invalid 'from' size", nil))
				return
			}
		}
		if v := c.Query("to"); v != "" {
			if _, err := fmt.Sscanf(v, "%d", &to); err != nil {
				apperrors.RespondError(c, apperrors.Validation("invalid 'to' size", nil))
				return
			}
		}
		proof, err := l.ConsistencyProof(c.Request.Context(), from, to)
		if err != nil {
			apperrors.RespondError(c, apperrors.Validation(err.Error(), nil))
			return
		}
		c.JSON(http.StatusOK, proof)
	}
}

// handleEvidenceRotateKey rotates the evidence signing key (admin only) and
// appends a signed key.rotate receipt to the tamper-evident log. If
// private_key_pem is supplied it becomes the new operator key; otherwise a new
// EPHEMERAL key is generated (dev/sim only, flagged in the response). Publish the
// returned public key so verifiers can check receipts signed after the rotation.
func handleEvidenceRotateKey(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			PrivateKeyPEM string `json:"private_key_pem"`
			Reason        string `json:"reason"`
		}
		_ = c.ShouldBindJSON(&req) // empty body is allowed (=> ephemeral key)

		var (
			signer    evidence.Signer
			ephemeral bool
		)
		if req.PrivateKeyPEM != "" {
			s, err := evidence.NewSignerFromPEM([]byte(req.PrivateKeyPEM))
			if err != nil {
				apperrors.RespondError(c, apperrors.Validation("invalid Ed25519 signing key PEM: "+err.Error(), nil))
				return
			}
			signer = s
		} else {
			s, err := evidence.GenerateEphemeralSigner()
			if err != nil {
				apperrors.RespondError(c, apperrors.Internal("generate signing key failed", err))
				return
			}
			signer = s
			ephemeral = true
		}

		ev, err := l.RotateSigner(c.Request.Context(), signer, req.Reason)
		if err != nil {
			apperrors.RespondError(c, apperrors.Internal("key rotation failed", err))
			return
		}
		pemBytes, _ := evidence.MarshalPublicKeyPEM(signer.PublicKey())
		resp := gin.H{
			"rotated":        true,
			"new_key_id":     signer.KeyID(),
			"public_key_pem": string(pemBytes),
			"ephemeral":      ephemeral,
			"evidence_id":    ev.ID,
			"evidence_hash":  ev.Hash,
		}
		if ephemeral {
			resp["warning"] = "generated an EPHEMERAL key (dev/sim only); supply private_key_pem for a durable, verifiable identity"
		}
		c.JSON(http.StatusOK, resp)
	}
}

// handleEvidenceCompleteness returns a namespace completeness proof ("no more, no
// less") for ?namespace=<ns>: it proves the sealed set for that namespace is
// EXACTLY its members, verifiable offline via `cafctl verify-completeness`. The
// namespace is pillar-agnostic - redteam/engagement/<id>, scheduler/tenant/<t>,
// finops/month/<YYYY-MM>, ... - all served by this one endpoint (the Verifiable
// Fabric thesis over HTTP).
func handleEvidenceCompleteness(l *evidence.Ledger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ns := c.Query("namespace")
		if ns == "" {
			apperrors.RespondError(c, apperrors.Validation("query parameter 'namespace' is required", nil))
			return
		}
		proof, err := l.BuildCompletenessProof(c.Request.Context(), ns)
		if err != nil {
			if errors.Is(err, evidence.ErrNoSeal) {
				apperrors.RespondError(c, apperrors.NotFound("completeness namespace", ns))
				return
			}
			apperrors.RespondError(c, apperrors.Internal("completeness proof failed", err))
			return
		}
		c.JSON(http.StatusOK, proof)
	}
}
