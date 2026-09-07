// Copyright 2026 CloudAI Fusion. All rights reserved.
// Licensed under the Apache License v2.0 (see /LICENSE file).
// IMPORTANT: Work order system for production penetration testing approval workflow

package osce3_validation

import (
	"fmt"
	"time"
)

// WorkOrderSystem manages authorization workflows for Tier 2 production tests
type WorkOrderSystem struct {
	orders       map[string]*WorkOrder
	nextOrderID  int
	createdAt    time.Time
}

// WorkOrder represents a formal request for penetration testing activity
type WorkOrder struct {
	// ID unique work order identifier
	ID string `json:"id"`

	// TenantID associated with the work order
	TenantID string `json:"tenant_id"`

	// Feature being requested (exploit_execution, credential_dumping, etc.)
	Feature string `json:"feature"`

	// Description explains the intended operation
	Description string `json:"description"`

	// Targets list of IPs authorized for this work order
	Targets []string `json:"targets"`

	// OrderType type classification (standard, target-specific, emergency)
	OrderType string `json:"order_type"`

	// Status current state (pending, approved, rejected, cancelled)
	Status string `json:"status"`

	// Submitter who created the work order
	Submitter string `json:"submitter"`

	// SubmittedAt timestamp of submission
	SubmittedAt time.Time `json:"submitted_at"`

	// ApprovedBy who approved the order
	ApprovedBy string `json:"approved_by,omitempty"`

	// ApprovedAt when approval occurred
	ApprovedAt time.Time `json:"approved_at,omitempty"`

	// Comments any additional notes
	Comments string `json:"comments,omitempty"`

	// ValidUntil expiration time
	ValidUntil time.Time `json:"valid_until"`

	// AuditTrail contains RFC3339 timestamped events
	AuditTrail []AuditEvent `json:"audit_trail"`
}

// AuditEvent logs actions in RFC3339 format
type AuditEvent struct {
	// Timestamp RFC3339 formatted timestamp
	Timestamp time.Time `json:"timestamp"`

	// Action performed action name
	Action string `json:"action"`

	// User user performing action
	User string `json:"user"`

	// Details additional context
	Details string `json:"details,omitempty"`
}

// NewWorkOrderSystem initializes the work order management system
func NewWorkOrderSystem() *WorkOrderSystem {
	return &WorkOrderSystem{
		orders:      make(map[string]*WorkOrder),
		nextOrderID: 1,
		createdAt:   time.Now(),
	}
}

// SubmitWorkOrder creates and submits a new work order
func (sys *WorkOrderSystem) SubmitWorkOrder(
	tenantID string,
	feature string,
	description string,
	targets []string,
	orderType string,
	submitter string,
) (*WorkOrder, error) {
	// Validate input
	if tenantID == "" {
		return nil, fmt.Errorf("tenant ID is required")
	}
	if feature == "" {
		return nil, fmt.Errorf("feature is required")
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one target is required")
	}

	// Generate order ID
	orderID := fmt.Sprintf("WO-%s-%03d", 
		time.Now().UTC().Format("20060102"),
		sys.nextOrderID,
	)
	sys.nextOrderID++

	// Create work order
	order := &WorkOrder{
		ID:          orderID,
		TenantID:    tenantID,
		Feature:     feature,
		Description: description,
		Targets:     targets,
		OrderType:   orderType,
		Status:      "pending",
		Submitter:   submitter,
		SubmittedAt: time.Now().UTC(),
		ValidUntil:  time.Now().UTC().Add(24 * time.Hour), // 24h validity
		AuditTrail: []AuditEvent{
			{
				Timestamp: time.Now().UTC(),
				Action:    "SUBMIT",
				User:      submitter,
				Details:   fmt.Sprintf("Work order created: %s for feature %s", orderID, feature),
			},
		},
	}

	// Store order
	sys.orders[orderID] = order

	// Log to audit trail
	fmt.Printf("📝 Work Order #%s submitted by %s\n", orderID, submitter)
	fmt.Printf("   Feature: %s\n", feature)
	fmt.Printf("   Targets: %v\n", targets)
	fmt.Printf("   Type: %s\n", orderType)

	return order, nil
}

// ApproveWorkOrder approves a pending work order
func (sys *WorkOrderSystem) ApproveWorkOrder(
	orderID string,
	approver string,
	comments string,
	approvers []*ApproversConfig,
) error {
	// Find order
	order, exists := sys.orders[orderID]
	if !exists {
		return fmt.Errorf("work order not found: %s", orderID)
	}

	// Check status
	if order.Status != "pending" {
		return fmt.Errorf("order must be pending for approval, current status: %s", order.Status)
	}

	// Validate approvers configuration
	for _, cfg := range approvers {
		if !cfg.isValid() {
			return fmt.Errorf("invalid approver config: %+v", cfg)
		}
	}

	// Update order status
	order.Status = "approved"
	order.ApprovedBy = approver
	order.ApprovedAt = time.Now().UTC()
	order.Comments = comments

	// Add approval event to audit trail
	order.AuditTrail = append(order.AuditTrail, AuditEvent{
		Timestamp: time.Now().UTC(),
		Action:    "APPROVE",
		User:      approver,
		Details:   fmt.Sprintf("Approved by security-admin: %s", comments),
	})

	// Log approval
	fmt.Printf("✅ Work Order #%s APPROVED by %s\n", orderID, approver)
	fmt.Printf("   Comments: %s\n", comments)
	fmt.Printf("   Valid until: %s\n", order.ValidUntil.Format(time.RFC3339))

	return nil
}

// CanExecuteFeature checks if a feature execution is authorized for tenant/target
func (sys *WorkOrderSystem) CanExecuteFeature(tenantID string, feature string, targetIP string) bool {
	// Search for valid work orders
	for _, order := range sys.orders {
		// Check tenant match
		if order.TenantID != tenantID {
			continue
		}

		// Check feature match
		if order.Feature != feature {
			continue
		}

		// Check status
		if order.Status != "approved" {
			continue
		}

		// Check expiry
		if time.Now().UTC().After(order.ValidUntil) {
			continue
		}

		// Check target inclusion
		for _, t := range order.Targets {
			if t == targetIP {
				return true
			}
		}
	}

	return false
}

// ListOrders returns all work orders with optional filtering
func (sys *WorkOrderSystem) ListOrders(filterStatus string) []*WorkOrder {
	var results []*WorkOrder
	for _, order := range sys.orders {
		if filterStatus == "" || order.Status == filterStatus {
			results = append(results, order)
		}
	}
	return results
}

// GetOrder retrieves a specific work order
func (sys *WorkOrderSystem) GetOrder(orderID string) (*WorkOrder, bool) {
	order, exists := sys.orders[orderID]
	return order, exists
}

// RejectWorkOrder rejects a pending work order
func (sys *WorkOrderSystem) RejectWorkOrder(
	orderID string,
	rejector string,
	comments string,
) error {
	order, exists := sys.orders[orderID]
	if !exists {
		return fmt.Errorf("work order not found: %s", orderID)
	}

	order.Status = "rejected"
	order.RejectedBy = rejector
	order.RejectedAt = time.Now().UTC()
	order.Comments = comments

	order.AuditTrail = append(order.AuditTrail, AuditEvent{
		Timestamp: time.Now().UTC(),
		Action:    "REJECT",
		User:      rejector,
		Details:   fmt.Sprintf("Rejected: %s", comments),
	})

	return nil
}

// CancelWorkOrder cancels an active work order
func (sys *WorkOrderSystem) CancelWorkOrder(
	orderID string,
	canceler string,
	reason string,
) error {
	order, exists := sys.orders[orderID]
	if !exists {
		return fmt.Errorf("work order not found: %s", orderID)
	}

	order.Status = "cancelled"
	order.CancelledBy = canceler
	order.CancelledAt = time.Now().UTC()
	order.Comments = reason

	order.AuditTrail = append(order.AuditTrail, AuditEvent{
		Timestamp: time.Now().UTC(),
		Action:    "CANCEL",
		User:      canceler,
		Details:   reason,
	})

	return nil
}

// String generates human-readable work order summary
func (sys *WorkOrderSystem) String() string {
	orders := sys.ListOrders("")
	
	output := fmt.Sprintf("📋 Work Order System (%d orders)\n", len(orders))
	output += fmt.Sprintf("==============================\n\n")

	if len(orders) == 0 {
		return output + "No work orders in system"
	}

	for _, order := range orders {
		output += fmt.Sprintf("[%s] %s\n", order.ID, order.Status)
		output += fmt.Sprintf("   Feature: %s\n", order.Feature)
		output += fmt.Sprintf("   Tenant: %s | Target: %v\n", order.TenantID, order.Targets)
		output += fmt.Sprintf("   Submitted: %s | Expires: %s\n", 
			order.SubmittedAt.Format("2006-01-02 15:04"),
			order.ValidUntil.Format("2006-01-02 15:04"))
		
		if order.Status == "approved" && order.ApprovedBy != "" {
			output += fmt.Sprintf("   ✅ Approved by: %s\n", order.ApprovedBy)
		} else if order.Status == "rejected" && order.RejectedBy != "" {
			output += fmt.Sprintf("   ❌ Rejected by: %s\n", order.RejectedBy)
		}

		output += fmt.Sprintf("   Audit Trail: %d events\n", len(order.AuditTrail))
		output += "\n"
	}

	return output
}

// ApproversConfig configuration for multiple approvers
type ApproversConfig struct {
	Role         string   `json:"role"`
	Required     bool     `json:"required"`
	Approvers    []string `json:"approvers"`
	MinCount     int      `json:"min_count"` // Minimum number of approvals needed
}

func (cfg *ApproversConfig) isValid() bool {
	return cfg.Role != "" && len(cfg.Approvers) > 0 && cfg.MinCount > 0
}
