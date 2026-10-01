package redteam

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// ATTACK MODE SYSTEM - Dual Mode Architecture
// ============================================================================
// Implements OSCE³ dual-mode paradigm:
// Mode 1: Sandbox Isolation - No work order needed, safe for training/dev
// Mode 2: Production Attack - Work order approval required, real offensive ops
// ============================================================================

// AttackMode represents the operational mode of the red team platform
type AttackMode int

const (
	// ModeSandboxIsolation: Safe test environment (VM/Docker isolated)
	// Requires NO work order - for training, development, authorized ranges only
	ModeSandboxIsolation AttackMode = iota
	
	// ModeProductionAttack: Real-world offensive operations  
	// REQUIRES work order approval - actual penetration testing
	ModeProductionAttack
)

// String returns human-readable mode name
func (m AttackMode) String() string {
	switch m {
	case ModeSandboxIsolation:
		return "SANDBOX_ISOLATION"
	case ModeProductionAttack:
		return "PRODUCTION_ATTACK"
	default:
		return "UNKNOWN"
	}
}

// RequiresWorkOrder returns true if this mode needs formal authorization
func (m AttackMode) RequiresWorkOrder() bool {
	return m == ModeProductionAttack
}

// ModeDescription returns detailed explanation of operational constraints
func (m AttackMode) ModeDescription() string {
	descriptions := map[AttackMode]string{
		ModeSandboxIsolation:   "安全测试环境 - Docker/VM隔离，仅授权靶场无需工单",
		ModeProductionAttack:   "真实攻击武器库 - 需要正式授权审批",
	}
	if desc, ok := descriptions[m]; ok {
		return desc
	}
	return "未知操作模式"
}

// ============================================================================
// WORK ORDER SYSTEM - Authorization Workflow
// ============================================================================
// Per OSEP/PEN-300 standards:
// - Formal request process for production attacks
// - Multi-level approval chain
// - Complete audit trail with ISO 8601 timestamps
// ============================================================================

// WorkOrderStatus defines work order lifecycle states
type WorkOrderStatus string

const (
	WorkOrderPending    WorkOrderStatus = "pending"     // Awaiting review
	WorkOrderApproved   WorkOrderStatus = "approved"    // Authorized for execution
	WorkOrderRejected   WorkOrderStatus = "rejected"    // Denied by reviewer
	WorkOrderCancelled  WorkOrderStatus = "cancelled"   // Requester withdrew
)

// PriorityLevel defines urgency classification
type PriorityLevel int

const (
	PriorityNormal PriorityLevel = iota // 0 - Standard turnaround
	PriorityHigh                        // 1 - Expedited review
	PriorityCritical                    // 2 - Immediate action required
)

// WorkOrder represents formal authorization request per compliance requirements
type WorkOrder struct {
	ID                 uuid.UUID          `json:"id"`
	RequesterID        uuid.UUID          `json:"requester_id"`
	RequesterName      string             `json:"requester_name"`
	TargetSystem       string             `json:"target_system"`
	Description        string             `json:"description"`
	AttackScope        []string           `json:"attack_scope"` // IPs/domains under scope
	LegalAuthority     string             `json:"legal_authority,omitempty` // Contract/EOA ref number
	AuthorizationType  string             `json:"authorization_type"` // EOA/contract/etc
	Priority           PriorityLevel      `json:"priority"`
	Status             WorkOrderStatus    `json:"status"`
	Approvers          []ApproverRecord   `json:"approvers,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	ReviewedAt         *time.Time         `json:"reviewed_at,omitempty"`
	ApprovedAt         *time.Time         `json:"approved_at,omitempty"`
	ExpiresAt          time.Time          `json:"expires_at"`
	RejectionReason    string             `json:"rejection_reason,omitempty"`
	AuditTrail         []AuditEvent       `json:"audit_trail,omitempty"`
}

// ApproverRecord tracks individual approval decisions
type ApproverRecord struct {
	ApproverID   uuid.UUID `json:"approver_id"`
	ApproverName string    `json:"approver_name"`
	Action       string    `json:"action"` // approved/rejected/returned
	Comments     string    `json:"comments,omitempty"`
	ApprovedAt   time.Time `json:"approved_at"`
}

// AuditEvent records every work order event for compliance
type AuditEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	ActorID    uuid.UUID `json:"actor_id"`
	ActorName  string    `json:"actor_name"`
	ActionType string    `json:"action_type"` // created/submitted/approved/rejected/cancelled
	Details    string    `json:"details,omitempty"`
}

// NewWorkOrder creates a new authorization request
func NewWorkOrder(requesterID uuid.UUID, requesterName, targetSystem string, priority PriorityLevel) *WorkOrder {
	now := time.Now()
	expiresAt := now.Add(72 * time.Hour) // Default 72-hour validity
	
	if priority == PriorityCritical {
		expiresAt = now.Add(24 * time.Hour) // Critical expires in 24h
	} else if priority == PriorityHigh {
		expiresAt = now.Add(48 * time.Hour)
	}
	
	return &WorkOrder{
		ID:            uuid.New(),
		RequesterID:   requesterID,
		RequesterName: requesterName,
		TargetSystem:  targetSystem,
		Description:   fmt.Sprintf("Attack authorization for %s", targetSystem),
		AttackScope:   []string{targetSystem},
		AuthorizationType: "EOA", // Engagement Authorization
		Priority:      priority,
		Status:        WorkOrderPending,
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
		Approvers:     make([]ApproverRecord, 0),
		AuditTrail:    make([]AuditEvent, 0),
	}
}

// AddAuditEvent records an event in the audit trail
func (wo *WorkOrder) AddAuditEvent(actorID uuid.UUID, actorName, actionType, details string) {
	wo.AuditTrail = append(wo.AuditTrail, AuditEvent{
		Timestamp:  time.Now(),
		ActorID:    actorID,
		ActorName:  actorName,
		ActionType: actionType,
		Details:    details,
	})
}

// Approve transitions work order to approved status
func (wo *WorkOrder) Approve(approverID uuid.UUID, approverName, comments string) error {
	if wo.Status != WorkOrderPending {
		return fmt.Errorf("work order cannot be approved - current status: %s", wo.Status)
	}
	
	now := time.Now()
	wo.Status = WorkOrderApproved
	wo.ApprovedAt = &now
	wo.ReviewedAt = &now
	
	wo.Approvers = append(wo.Approvers, ApproverRecord{
		ApproverID:   approverID,
		ApproverName: approverName,
		Action:       "approved",
		Comments:     comments,
		ApprovedAt:   now,
	})
	
	wo.AddAuditEvent(approverID, approverName, "approved", 
		fmt.Sprintf("Work order approved%s", func() string {
			if comments != "" {
				return fmt.Sprintf(": %s", comments)
			}
			return ""
		}()))
	
	return nil
}

// Reject marks work order as rejected with reason
func (wo *WorkOrder) Reject(reviewerID uuid.UUID, reviewerName, reason string) error {
	if wo.Status != WorkOrderPending {
		return fmt.Errorf("work order cannot be rejected - current status: %s", wo.Status)
	}
	
	now := time.Now()
	wo.Status = WorkOrderRejected
	wo.ReviewedAt = &now
	wo.RejectionReason = reason
	
	wo.Approvers = append(wo.Approvers, ApproverRecord{
		ApproverID:   reviewerID,
		ApproverName: reviewerName,
		Action:       "rejected",
		Comments:     reason,
		ApprovedAt:   now,
	})
	
	wo.AddAuditEvent(reviewerID, reviewerName, "rejected", reason)
	return nil
}

// IsExpired checks if work order has exceeded validity period
func (wo *WorkOrder) IsExpired() bool {
	return time.Now().After(wo.ExpiresAt)
}

// IsValidForExecution checks if work order can be used for production attacks
func (wo *WorkOrder) IsValidForExecution() bool {
	return wo.Status == WorkOrderApproved && !wo.IsExpired()
}

// ============================================================================
// BRIDGE ROUTER - Mode Selection Logic
// ============================================================================

// BridgeRouter determines which attack mode to use based on user context
type BridgeRouter struct {
	workOrderStore    WorkOrderStoreInterface
	authzService      AuthorizationServiceInterface
	currentUser       *User
	defaultMode       AttackMode
}

// WorkOrderStoreInterface abstracts work order persistence
type WorkOrderStoreInterface interface {
	GetActiveOrder(userID string) *WorkOrder
	GetOrderByID(orderID string) (*WorkOrder, error)
	CreateOrder(order *WorkOrder) error
	UpdateOrder(order *WorkOrder) error
}

// AuthorizationServiceInterface abstracts permission checking
type AuthorizationServiceInterface interface {
	HasProductionAttackPermission(user *User) bool
	GetPermissionLevel(user *User) PermissionType
}

// User represents authenticated platform user
type User struct {
	ID              uuid.UUID
	Username        string
	Email           string
	Role            string
	Permissions     []string
	IsActive        bool
	LastLogin       time.Time
}

// HasPermission checks if user has specific permission
func (u *User) HasPermission(permission string) bool {
	for _, p := range u.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// NewBridgeRouter initializes mode selection infrastructure
func NewBridgeRouter(store WorkOrderStoreInterface, authz AuthorizationServiceInterface, defaultMode AttackMode) *BridgeRouter {
	return &BridgeRouter{
		workOrderStore: store,
		authzService:   authz,
		defaultMode:    defaultMode,
	}
}

// SetCurrentUser sets the active user context for mode determination
func (br *BridgeRouter) SetCurrentUser(user *User) {
	br.currentUser = user
}

// DetermineMode selects appropriate attack mode based on authorization state
func (br *BridgeRouter) DetermineMode() AttackMode {
	if br.currentUser == nil {
		return br.defaultMode
	}
	
	// Check production attack permissions
	hasPerms := br.authzService.HasProductionAttackPermission(br.currentUser)
	if !hasPerms {
		// Limited permissions → sandbox mode only
		return ModeSandboxIsolation
	}
	
	// Check work order approval
	if br.workOrderStore != nil {
		activeOrder := br.workOrderStore.GetActiveOrder(br.currentUser.ID.String())
		if activeOrder != nil && activeOrder.IsValidForExecution() {
			return ModeProductionAttack
		}
	}
	
	// Fall back to sandbox if no valid work order
	return ModeSandboxIsolation
}

// CanExecuteInMode checks if operation is allowed in given mode
func (br *BridgeRouter) CanExecuteInMode(mode AttackMode) bool {
	if mode == ModeSandboxIsolation {
		return true // Always allowed
	}
	
	// Production mode requires both perms and valid work order
	if !br.authzService.HasProductionAttackPermission(br.currentUser) {
		return false
	}
	
	if br.workOrderStore != nil {
		order := br.workOrderStore.GetActiveOrder(br.currentUser.ID.String())
		if order != nil && order.IsValidForExecution() {
			return true
		}
	}
	
	return false
}

// RecommendedMode suggests optimal mode for current user
func (br *BridgeRouter) RecommendedMode() (mode AttackMode, reason string) {
	mode = br.DetermineMode()
	
	switch mode {
	case ModeSandboxIsolation:
		if !br.authzService.HasProductionAttackPermission(br.currentUser) {
			reason = "User lacks production attack permissions"
		} else {
			reason = "No active work order found - using sandbox mode for safety"
		}
	case ModeProductionAttack:
		reason = "Valid work order approved for production operations"
	}
	
	return mode, reason
}
