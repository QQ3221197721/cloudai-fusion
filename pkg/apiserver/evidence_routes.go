// Package apiserver provides HTTP handlers for CloudAI Fusion REST API
package apiserver

import (
	"net/http"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/gin-gonic/gin"
)

// EvidenceAPIServer handles all evidence-related REST endpoints
type EvidenceAPIServer struct {
	ledger *evidence.Ledger
}

// NewEvidenceAPIServer creates a new EvidenceAPIServer instance
func NewEvidenceAPIServer(ledg *evidence.Ledger) *EvidenceAPIServer {
	return &EvidenceAPIServer{ledger: ledg}
}

// Register registers all evidence routes with the gin router
func (e *EvidenceAPIServer) Register(router *gin.Engine) {
	api := router.Group("/api/v1")
	{
		// ==================== Ledger Operations ====================
		api.GET("/evidence", e.getLedgerStatus)
		api.GET("/evidence/records", e.listRecords)
		api.GET("/evidence/records/:id", e.getRecordDetail)
		
		// ==================== Verification ====================
		api.GET("/evidence/records/:id/proof", e.getMerkleProof)
		api.GET("/evidence/verify", e.verifyChain)
		
		// ==================== Export ====================
		api.GET("/evidence/export/bundle", e.exportBundle)
		api.GET("/evidence/checkpoint", e.getCheckpoint)
		
		// ==================== Key Management ====================
		api.GET("/evidence/pubkey", e.getPublicKey)
		api.POST("/evidence/rotate-key", e.rotateKey) // Requires admin auth
	}
}

// getLedgerStatus gets ledger summary
func (e *EvidenceAPIServer) getLedgerStatus(c *gin.Context) {
	status := e.ledger.GetStatus()
	c.JSON(http.StatusOK, status)
}

// listRecords lists signed receipts
func (e *EvidenceAPIServer) listRecords(c *gin.Context) {
	limit := c.DefaultQuery("limit", "100")
	action := c.Query("action")
	actor := c.Query("actor")
	
	records, err := e.ledger.ListRecords(limit, action, actor)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"total": len(records),
		"records": records,
	})
}

// getRecordDetail gets a single receipt by ID
func (e *EvidenceAPIServer) getRecordDetail(c *gin.Context) {
	recordID := c.Param("id")
	record, err := e.ledger.GetRecord(recordID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
		return
	}
	c.JSON(http.StatusOK, record)
}

// getMerkleProof gets Merkle inclusion proof for a receipt
func (e *EvidenceAPIServer) getMerkleProof(c *gin.Context) {
	recordID := c.Param("id")
	proof := e.ledger.GetMerkleProof(recordID)
	
	if proof == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Proof not found"})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"record_id": recordID,
		"proof": proof,
	})
}

// verifyChain verifies chain integrity server-side
func (e *EvidenceAPIServer) verifyChain(c *gin.Context) {
	report, err := e.ledger.VerifyChain()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, report)
}

// exportBundle exports chain + public key for offline verification
func (e *EvidenceAPIServer) exportBundle(c *gin.Context) {
	bundle, err := e.ledger.ExportBundle()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, bundle)
}

// getCheckpoint gets signed tree head (STH)
func (e *EvidenceAPIServer) getCheckpoint(c *gin.Context) {
	checkpoint := e.ledger.GetCheckpoint()
	c.JSON(http.StatusOK, checkpoint)
}

// getPublicKey gets Ed25519 public key
func (e *EvidenceAPIServer) getPublicKey(c *gin.Context) {
	publicKey := e.ledger.GetPublicKey()
	c.JSON(http.StatusOK, publicKey)
}

// rotateKey rotates the evidence signing key (admin only)
func (e *EvidenceAPIServer) rotateKey(c *gin.Context) {
	err := e.ledger.RotateKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"status": "key rotated"})
}
