// Package support provides ticket CRUD operations with SLA tracking and
// intelligent assignment routing (round-robin / load-based).
package support

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Priority represents ticket priority levels. Lower values are higher priority.
type Priority int

const (
	PriorityLow Priority = iota + 1 // standard business hours SLA
	PriorityMedium
	PriorityHigh
	PriorityCritical
)

// String renders a user-facing label.
func (p Priority) String() string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	default:
		return "unknown"
	}
}

// Status defines the current lifecycle state of a ticket.
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in-progress"
	StatusClosed     Status = "closed"
)

// Ticket is a customer or internal request.
type Ticket struct {
	ID          string
	Title       string
	Description string
	Priority    Priority
	Status      Status
	AssigneeID  string // assigned employee/user ID; empty if unassigned
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SLA         *SLADefinition
}

// SLADefinition specifies time-bound promises for response/resolution.
type SLADefinition struct {
	ResponseTimeout  time.Duration // max time to first response
	ResolutionDeadline time.Duration // max time from creation to resolution
}

// SLAPolicy returns the appropriate SLA based on priority.
var SLAPolicy = map[Priority]SLADefinition{
	PriorityLow:        {ResponseTimeout: 4 * time.Hour, ResolutionDeadline: 24 * time.Hour},
	PriorityMedium:     {ResponseTimeout: 2 * time.Hour, ResolutionDeadline: 8 * time.Hour},
	PriorityHigh:       {ResponseTimeout: 30 * time.Minute, ResolutionDeadline: 2 * time.Hour},
	PriorityCritical:   {ResponseTimeout: 15 * time.Minute, ResolutionDeadline: 30 * time.Minute},
}

// TicketStore persists tickets.
type TicketStore interface {
	Create(ctx context.Context, ticket *Ticket) error
	Get(ctx context.Context, id string) (*Ticket, error)
	List(ctx context.Context, opts ListOptions) ([]*Ticket, error)
	Update(ctx context.Context, ticket *Ticket) error
	Close(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// InMemoryTicketStore is an in-memory store for development and testing.
type InMemoryTicketStore struct {
	mu   sync.RWMutex
	tics []*Ticket
	next int
}

// Create implements TicketStore.
func (s *InMemoryTicketStore) Create(ctx context.Context, t *Ticket) error {
	if t == nil || t.Title == "" {
		return fmt.Errorf("ticket title is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	t.ID = fmt.Sprintf("TKT-%d", s.next)
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	t.SLA = func() *SLADefinition {
		p := SLAPolicy[t.Priority]
		return &p
	}()
	t.Status = StatusOpen
	s.tics = append(s.tics, t)
	return nil
}

// Get implements TicketStore.
func (s *InMemoryTicketStore) Get(_ context.Context, id string) (*Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tics {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("ticket %s not found", id)
}

// ListOptions filters list queries.
type ListOptions struct {
	Status      Status
	Priority    Priority
	Limit       int // 0 = no limit
}

// List implements TicketStore.
func (s *InMemoryTicketStore) List(_ context.Context, opts ListOptions) ([]*Ticket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Ticket, 0, len(s.tics))
	for _, t := range s.tics {
		if opts.Status != "" && t.Status != opts.Status {
			continue
		}
		if opts.Priority != 0 && t.Priority != opts.Priority {
			continue
		}
		result = append(result, t)
	}
	if opts.Limit > 0 && len(result) > opts.Limit {
		result = result[:opts.Limit]
	}
	return result, nil
}

// SLATracker monitors SLA breaches.
type SLATracker struct {
	mu     sync.RWMutex
	breaches map[string]time.Time // ticket ID -> breach time
}

// IsResponseBreach returns true if response timeout was exceeded.
func (s *SLATracker) IsResponseBreach(t *Ticket) bool {
	if t.SLA == nil || t.CreatedAt.IsZero() {
		return false
	}
	// Breach occurs when elapsed duration >= SLA timeout threshold
	return time.Since(t.CreatedAt) >= t.SLA.ResponseTimeout
}

// IsResolutionBreach returns true if resolution deadline passed.
func (s *SLATracker) IsResolutionBreach(t *Ticket) bool {
	if t.Status != StatusClosed && t.SLA != nil {
		if t.CreatedAt.IsZero() {
			return false
		}
		// Breach occurs when elapsed duration >= SLA deadline threshold
		return time.Since(t.CreatedAt) >= t.SLA.ResolutionDeadline
	}
	return false
}

// SLATimeoutForPriority returns the appropriate SLA for a priority level.
func (s *SLATracker) SLATimeoutForPriority(p Priority) SLADefinition {
	// Valid priorities are Low(1) through Critical(4)
	if p >= PriorityLow && p <= PriorityCritical {
		return SLAPolicy[p]
	}
	return SLAPolicy[PriorityMedium]
}

// Update implements TicketStore.
func (s *InMemoryTicketStore) Update(_ context.Context, t *Ticket) error {
	if t == nil || t.ID == "" {
		return fmt.Errorf("ticket ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.tics {
		if existing.ID == t.ID {
			t.UpdatedAt = time.Now().UTC()
			s.tics[i] = t
			return nil
		}
	}
	return fmt.Errorf("ticket %s not found", t.ID)
}

// Close implements TicketStore.
func (s *InMemoryTicketStore) Close(ctx context.Context, id string) error {
	return s.Update(ctx, &Ticket{ID: id, Status: StatusClosed})
}

// Delete implements TicketStore.
func (s *InMemoryTicketStore) Delete(ctx context.Context, id string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tics {
		if t.ID == id {
			s.tics = append(s.tics[:i], s.tics[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("ticket %s not found", id)
}
