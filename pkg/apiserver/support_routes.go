// Package apiserver provides HTTP handlers for CloudAI Fusion REST API
package apiserver

import (
	"net/http"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/support"
	"github.com/gin-gonic/gin"
)

// SupportAPIServer handles all support ticket-related REST endpoints
type SupportAPIServer struct {
	manager *support.Manager
}

// NewSupportAPIServer creates a new SupportAPIServer instance
func NewSupportAPIServer(supportMgr *support.Manager) *SupportAPIServer {
	return &SupportAPIServer{manager: supportMgr}
}

// Register registers all support routes with the gin router
func (s *SupportAPIServer) Register(router *gin.Engine) {
	api := router.Group("/api/v1")
	api.Use(authMiddleware()) // Require authentication
	
	{
		// ==================== Ticket Management ====================
		api.GET("/support/tickets", s.listTickets)
		api.POST("/support/tickets", s.createTicket)
		api.GET("/support/tickets/:ticketId", s.getTicket)
		api.PUT("/support/tickets/:ticketId/comments", s.addComment)
		
		// ==================== Assignment ====================
		api.POST("/support/tickets/:ticketId/assign", s.assignTicket)
		
		// ==================== Resolution ====================
		api.POST("/support/tickets/:ticketId/resolve", s.resolveTicket)
		api.POST("/support/tickets/:ticketId/close", s.closeTicket)
		
		// ==================== Metrics ====================
		api.GET("/support/tickets/metrics", s.getMetrics)
	}
}

// listTickets lists tickets for the current user
func (s *SupportAPIServer) listTickets(c *gin.Context) {
	tickets, err := s.manager.ListTickets("current-user", support.TicketFilters{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"total":   len(tickets),
		"tickets": tickets,
	})
}

// createTicket creates a new support ticket
func (s *SupportAPIServer) createTicket(c *gin.Context) {
	var req struct {
		Title       string            `json:"title" binding:"required"`
		Description string            `json:"description" binding:"required"`
		Priority    string            `json:"priority"`
		Category    string            `json:"category" binding:"required"`
		Metadata    map[string]string `json:"metadata"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	userID := c.GetString("user_id")
	ticketReq := &support.UserTicketRequest{
		Title:       req.Title,
		Description: req.Description,
		Priority:    support.Priority(req.Priority),
		Category:    req.Category,
		Metadata:    req.Metadata,
	}
	
	ticket, err := s.manager.CreateTicket(userID, ticketReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusCreated, ticket)
}

// getTicket gets a specific ticket by ID
func (s *SupportAPIServer) getTicket(c *gin.Context) {
	ticketID := c.Param("ticketId")
	ticket, err := s.manager.GetTicket(ticketID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ticket)
}

// addComment adds a comment to a ticket
func (s *SupportAPIServer) addComment(c *gin.Context) {
	ticketID := c.Param("ticketId")
	var req struct {
		Content  string `json:"content" binding:"required"`
		Internal bool   `json:"internal"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	comment, err := s.manager.AddComment(ticketID, "current-user", req.Content, req.Internal)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusCreated, comment)
}

// assignTicket assigns a ticket to an agent
func (s *SupportAPIServer) assignTicket(c *gin.Context) {
	ticketID := c.Param("ticketId")
	var req struct {
		AgentID string `json:"agent_id" binding:"required"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	err := s.manager.AssignTicket(ticketID, req.AgentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"status": "assigned"})
}

// resolveTicket resolves a ticket
func (s *SupportAPIServer) resolveTicket(c *gin.Context) {
	ticketID := c.Param("ticketId")
	var req struct {
		Resolution string `json:"resolution" binding:"required"`
	}
	if err := c.ShouldJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	
	err := s.manager.ResolveTicket(ticketID, "current-user", req.Resolution)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"status": "resolved"})
}

// closeTicket permanently closes a ticket
func (s *SupportAPIServer) closeTicket(c *gin.Context) {
	ticketID := c.Param("ticketId")
	
	err := s.manager.CloseTicket(ticketID, "current-user")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"status": "closed"})
}

// getMetrics gets support ticket metrics
func (s *SupportAPIServer) getMetrics(c *gin.Context) {
	metrics := s.manager.GetMetrics()
	c.JSON(http.StatusOK, metrics)
}
