package support

import (
	"context"
	"testing"
	"time"
)

func TestTicketCRUD(t *testing.T) {
	ctx := context.Background()
	store := &InMemoryTicketStore{}

	t.Run("create_and_get", func(t *testing.T) {
		ticket := &Ticket{Title: "Test issue", Description: "desc", Priority: PriorityMedium}
		if err := store.Create(ctx, ticket); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if ticket.ID == "" {
			t.Error("expected ID after Create")
		}
		got, err := store.Get(ctx, ticket.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != StatusOpen {
			t.Errorf("Status=%s; want %s", got.Status, StatusOpen)
		}
	})

	t.Run("update_and_close", func(t *testing.T) {
		ticket := &Ticket{Title: "Update me", Description: "", Priority: PriorityHigh}
		if err := store.Create(ctx, ticket); err != nil {
			t.Fatalf("Create: %v", err)
		}
		ticket.Status = StatusInProgress
		if err := store.Update(ctx, ticket); err != nil {
			t.Fatalf("Update: %v", err)
		}
		got, _ := store.Get(ctx, ticket.ID)
		if got.Status != StatusInProgress {
			t.Errorf("Status=%s; want %s", got.Status, StatusInProgress)
		}
		if err := store.Close(ctx, ticket.ID); err != nil {
			t.Fatalf("Close: %v", err)
		}
		closed, _ := store.Get(ctx, ticket.ID)
		if closed.Status != StatusClosed {
			t.Errorf("Status=%s; want %s", closed.Status, StatusClosed)
		}
	})

	t.Run("delete", func(t *testing.T) {
		ticket := &Ticket{Title: "ToDelete", Priority: PriorityLow}
		if err := store.Create(ctx, ticket); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Delete(ctx, ticket.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		_, err := store.Get(ctx, ticket.ID)
		if err == nil {
			t.Error("expected error after Delete")
		}
	})
}

func TestSLADetection(t *testing.T) {
	tracker := &SLATracker{}

	t.Run("breach_detection_high_priority", func(t *testing.T) {
		now := time.Now()
		ticket := &Ticket{
			ID:        "T1",
			Title:     "Urgent issue",
			Priority:  PriorityHigh,
			CreatedAt: now.Add(-30 * time.Minute),
			Status:    StatusOpen,
			SLA:       &SLADefinition{ResponseTimeout: 30 * time.Minute},
		}
		if !tracker.IsResponseBreach(ticket) {
			t.Error("expected response breach")
		}
	})

	t.Run("no_breach_open_medium", func(t *testing.T) {
		now := time.Now()
		ticket := &Ticket{
			ID:        "T2",
			Priority:  PriorityMedium,
			CreatedAt: now.Add(-60 * time.Minute),
			Status:    StatusOpen,
			SLA:       &SLADefinition{ResolutionDeadline: 8 * time.Hour},
		}
		if tracker.IsResolutionBreach(ticket) {
			t.Error("expected no resolution breach")
		}
	})

	t.Run("closed_no_breach", func(t *testing.T) {
		now := time.Now()
		ticket := &Ticket{
			ID:        "T3",
			Priority:  PriorityCritical,
			CreatedAt: now.Add(-1 * time.Hour),
			Status:    StatusClosed,
			SLA:       &SLADefinition{ResolutionDeadline: 30 * time.Minute},
		}
		if tracker.IsResolutionBreach(ticket) {
			t.Error("closed tickets shouldn't trigger resolution breach check here")
		}
	})
}

func TestListOperations(t *testing.T) {
	ctx := context.Background()
	store := &InMemoryTicketStore{}

	tickets := []Priority{PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical}
	for _, p := range tickets {
		ticket := &Ticket{Title: "Test " + p.String(), Description: "", Priority: p}
		if err := store.Create(ctx, ticket); err != nil {
			t.Fatalf("Create(%v): %v", p, err)
		}
	}

	t.Run("list_all", func(t *testing.T) {
		list, err := store.List(ctx, ListOptions{})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(list) < 4 {
			t.Errorf("len(list)=%d; want >=4", len(list))
		}
	})

	t.Run("filter_by_status", func(t *testing.T) {
		allOpen, err := store.List(ctx, ListOptions{Status: StatusOpen})
		if err != nil {
			t.Fatalf("List(Status=Open): %v", err)
		}
		if len(allOpen) == 0 {
			t.Error("expected open tickets")
		}
	})

	t.Run("limit", func(t *testing.T) {
		limit3, err := store.List(ctx, ListOptions{Limit: 3})
		if err != nil {
			t.Fatalf("List(Limit=3): %v", err)
		}
		if len(limit3) > 3 {
			t.Errorf("len=%d; want <=3", len(limit3))
		}
	})
}
