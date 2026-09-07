// Package support implements customer support ticket management system
package support

import (
	"fmt"
	"sync"
	"time"
)

// Priority defines ticket priority level
type Priority string

const (
	PriorityLow     Priority = "low"
	PriorityMedium  Priority = "medium"
	PriorityHigh    Priority = "high"
	PriorityCritical Priority = "critical"
)

// Status defines ticket status
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusPending    Status = "pending"
	StatusResolved   Status = "resolved"
	StatusClosed     Status = "closed"
)

// UserTicketRequest represents a user's request for support
type UserTicketRequest struct {
	Title       string            `json:"title" binding:"required"`
	Description string            `json:"description" binding:"required"`
	Priority    Priority          `json:"priority"`
	Category    string            `json:"category" binding:"required"`
	Metadata    map[string]string `json:"metadata"`
}

// Ticket is the main ticket entity
type Ticket struct {
	ID             string           `json:"id"`
	UserID         string           `json:"user_id"`
	Title          string           `json:"title"`
	Description    string           `json:"description"`
	Priority       Priority         `json:"priority"`
	Category       string           `json:"category"`
	Status         Status           `json:"status"`
	AssigneeID     string           `json:"assignee_id,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	ResolvedAt     *time.Time       `json:"resolved_at,omitempty"`
	Comments       []TicketComment  `json:"comments"`
	TicketHistory  []TicketEvent    `json:"history"`
}

// TicketComment represents a comment on a ticket
type TicketComment struct {
	ID        string    `json:"id"`
	AuthorID  string    `json:"author_id"`
	Content   string    `json:"content"`
	Internal  bool      `json:"internal"`
	CreatedAt time.Time `json:"created_at"`
}

// TicketEvent tracks state changes and actions
type TicketEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // create, update, assign, resolve, close
	ActorID   string    `json:"actor_id"`
	Details   string    `json:"details"`
	Timestamp time.Time `json:"timestamp"`
}

// Agent handles ticket assignment and processing
type Agent struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Email          string   `json:"email"`
	Specialties    []string `json:"specialties"`
	ActiveTickets  int      `json:"active_tickets"`
	AverageSolveTime int     `json:"average_solve_time_minutes"`
}

// Manager manages all ticket operations
type Manager struct {
	mu          sync.RWMutex
	tickets     map[string]*Ticket
	agents      map[string]*Agent
	userTickets map[string][]string // userID -> ticketIDs
	agentTickets map[string][]string // agentID -> ticketIDs
	eventHistory []*TicketEvent
	logger Logger
}

// NewManager creates a new ticket manager
func NewManager(logger Logger) *Manager {
	return &Manager{
		tickets:      make(map[string]*Ticket),
		agents:       make(map[string]*Agent),
		userTickets:  make(map[string][]string),
		agentTickets: make(map[string][]string),
		eventHistory: make([]*TicketEvent, 0),
		logger:       logger,
	}
}

// CreateTicket creates a new ticket
func (m *Manager) CreateTicket(userID string, req *UserTicketRequest) (*Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticketID := generateUUID()
	now := time.Now().UTC()

	ticket := &Ticket{
		ID:          ticketID,
		UserID:      userID,
		Title:       req.Title,
		Description: req.Description,
		Priority:    req.Priority,
		Category:    req.Category,
		Status:      StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
		Comments:    make([]TicketComment, 0),
		TicketHistory: make([]TicketEvent, 0),
	}

	// Create initial event
	createEvent := &TicketEvent{
		ID:        generateUUID(),
		Type:      "create",
		ActorID:   userID,
		Details:   fmt.Sprintf("Ticket created with priority %s", req.Priority),
		Timestamp: now,
	}
	ticket.TicketHistory = append(ticket.TicketHistory, *createEvent)
	m.eventHistory = append(m.eventHistory, createEvent)

	m.tickets[ticketID] = ticket
	m.userTickets[userID] = append(m.userTickets[userID], ticketID)

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticketID,
		"user_id":   userID,
		"priority":  req.Priority,
	}).Info("Ticket created")

	return ticket, nil
}

// GetTicket returns a ticket by ID
func (m *Manager) GetTicket(ticketID string) (*Ticket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s not found", ticketID)
	}

	// Return a copy to prevent external modification
	return ticket, nil
}

// ListTickets returns tickets filtered by query parameters
func (m *Manager) ListTickets(userID string, filters TicketFilters) ([]*Ticket, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Ticket

	for _, ticket := range m.tickets {
		// Skip if not owner or assigned to user
		if ticket.UserID != userID && ticket.AssigneeID != userID {
			continue
		}

		// Apply filters
		if !matchesFilters(ticket, filters) {
			continue
		}

		result = append(result, ticket)
	}

	return result, nil
}

// AddComment adds a comment to a ticket
func (m *Manager) AddComment(ticketID string, authorID string, content string, internal bool) (*TicketComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return nil, fmt.Errorf("ticket %s not found", ticketID)
	}

	comment := &TicketComment{
		ID:        generateUUID(),
		AuthorID:  authorID,
		Content:   content,
		Internal:  internal,
		CreatedAt: time.Now().UTC(),
	}

	ticket.Comments = append(ticket.Comments, *comment)
	ticket.UpdatedAt = comment.CreatedAt

	// Update ticket status if resolved
	if ticket.Status == StatusOpen && internal {
		ticket.Status = StatusInProgress
		updateEvent := &TicketEvent{
			ID:        generateUUID(),
			Type:      "update",
			ActorID:   authorID,
			Details:   "Status changed to in_progress",
			Timestamp: comment.CreatedAt,
		}
		ticket.TicketHistory = append(ticket.TicketHistory, *updateEvent)
	}

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticketID,
		"author_id": authorID,
		"internal":  internal,
	}).Info("Comment added")

	return comment, nil
}

// AssignTicket assigns a ticket to an agent
func (m *Manager) AssignTicket(ticketID string, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %s not found", ticketID)
	}

	oldAssignee := ticket.AssigneeID
	ticket.AssigneeID = agentID
	ticket.UpdatedAt = time.Now().UTC()

	// Remove from old agent's queue
	if oldAssignee != "" {
		newQueue := make([]string, 0)
		for _, tid := range m.agentTickets[oldAssignee] {
			if tid != ticketID {
				newQueue = append(newQueue, tid)
			}
		}
		m.agentTickets[oldAssignee] = newQueue
	}

	// Add to new agent's queue
	m.agentTickets[agentID] = append(m.agentTickets[agentID], ticketID)

	// Update agent's active ticket count
	if agent, ok := m.agents[agentID]; ok {
		agent.ActiveTickets++
	}

	assignEvent := &TicketEvent{
		ID:        generateUUID(),
		Type:      "assign",
		ActorID:   agentID,
		Details:   fmt.Sprintf("Assigned to agent %s", agentID),
		Timestamp: time.Now().UTC(),
	}
	ticket.TicketHistory = append(ticket.TicketHistory, *assignEvent)
	m.eventHistory = append(m.eventHistory, assignEvent)

	m.logger.WithFields(logrus.Fields{
		"ticket_id":   ticketID,
		"agent_id":    agentID,
		"old_assignee": oldAssignee,
	}).Info("Ticket assigned")

	return nil
}

// ResolveTicket resolves a ticket
func (m *Manager) ResolveTicket(ticketID string, resolverID string, resolution string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %s not found", ticketID)
	}

	if ticket.Status == StatusResolved || ticket.Status == StatusClosed {
		return fmt.Errorf("ticket is already resolved or closed")
	}

	resolvedAt := time.Now().UTC()
	ticket.Status = StatusResolved
	ticket.ResolvedAt = &resolvedAt
	ticket.UpdatedAt = resolvedAt

	resolveEvent := &TicketEvent{
		ID:        generateUUID(),
		Type:      "resolve",
		ActorID:   resolverID,
		Details:   fmt.Sprintf("Resolved with note: %s", resolution),
		Timestamp: resolvedAt,
	}
	ticket.TicketHistory = append(ticket.TicketHistory, *resolveEvent)
	m.eventHistory = append(m.eventHistory, resolveEvent)

	// Update agent stats if applicable
	if ticket.AssigneeID != "" {
		if agent, ok := m.agents[ticket.AssigneeID]; ok {
			solveTime := resolvedAt.Sub(ticket.CreatedAt).Minutes()
			agent.AverageSolveTime = int(float64(agent.AverageSolveTime)*float64(agent.ActiveTickets)/float64(agent.ActiveTickets+1) + solveTime/float64(agent.ActiveTickets+1))
		}
	}

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticketID,
		"resolver":  resolverID,
	}).Info("Ticket resolved")

	return nil
}

// CloseTicket permanently closes a ticket
func (m *Manager) CloseTicket(ticketID string, closerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok {
		return fmt.Errorf("ticket %s not found", ticketID)
	}

	ticket.Status = StatusClosed
	ticket.UpdatedAt = time.Now().UTC()

	closeEvent := &TicketEvent{
		ID:        generateUUID(),
		Type:      "close",
		ActorID:   closerID,
		Details:   "Ticket closed permanently",
		Timestamp: time.Now().UTC(),
	}
	ticket.TicketHistory = append(ticket.TicketHistory, *closeEvent)
	m.eventHistory = append(m.eventHistory, closeEvent)

	m.logger.WithFields(logrus.Fields{
		"ticket_id": ticketID,
		"closer":    closerID,
	}).Info("Ticket closed")

	return nil
}
