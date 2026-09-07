// Package enterprise_tests - Enterprise-grade Red Team Testing Framework
package enterprise_tests

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// Authorization levels
	READ    = "read"
	EXECUTE = "execute"
	ADMIN   = "admin"
)

// AuthorizationGate manages access control for sensitive operations
type AuthorizationGate struct {
	logger      *logrus.Logger
	mode        string
	workOrder   *WorkOrder
	validating  bool
	mu          sync.RWMutex
}

// WorkOrder represents operational authorization documentation
type WorkOrder struct {
	ID            string
	Creator       string
	Description   string
	TargetSystems []string
	StartTime     time.Time
	EndTime       time.Time
	Authorization Level
	Status        string // "pending", "approved", "active", "completed", "revoked"
	TicketNumber  string
	LegalReview   bool
	RiskAssessment string
}

// NewAuthorizationGate creates new authorization gate
func NewAuthorizationGate(mode string) *AuthorizationGate {
	return &AuthorizationGate{
		logger: logrus.WithField("component", "auth_gate"),
		mode: mode,
		validating: false,
	}
}

// ValidateBeforeExploit checks if operation is authorized before execution
func (a *AuthorizationGate) ValidateBeforeExploit(operation string, level string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if a.mode == SANDBOX_MODE {
		a.logger.Warnf("[SANDBOX] Skipping production validation for %s", operation)
		return nil // Allowed in sandbox
	}
	
	// In production mode, require valid work order
	if a.workOrder == nil {
		return fmt.Errorf("no active work order found")
	}
	
	if !a.isWorkOrderValid() {
		return fmt.Errorf("work order expired or invalid")
	}
	
	if a.canExecute(operation) {
		return nil // Authorized
	}
	
	return fmt.Errorf("operation '%s' not authorized by work order", operation)
}

// CreateWorkOrder creates a new work order document
func (a *AuthorizationGate) CreateWorkOrder(id, creator, description string, 
	targets []string, startTime, endTime time.Time) *WorkOrder {
	
	a.mu.Lock()
	defer a.mu.Unlock()
	
	workOrder := &WorkOrder{
		ID:            id,
		Creator:       creator,
		Description:   description,
		TargetSystems: targets,
		StartTime:     startTime,
		EndTime:       endTime,
		Authorization: ADMIN, // Default to highest level
		Status:        "active",
		LegalReview:   true,
		RiskAssessment: "LOW-MEDIUM", // Would be calculated based on targets
	}
	
	a.workOrder = workOrder
	a.logger.Infof("✓ Work order created: %s (%s)", id, description)
	
	return workOrder
}

// ValidateWorkOrder checks if work order meets all requirements
func (a *AuthorizationGate) ValidateWorkOrder(wo *WorkOrder) bool {
	now := time.Now()
	
	// Check expiry
	if wo.EndTime.Before(now) {
		return false
	}
	
	// Check not started yet
	if wo.StartTime.After(now) {
		return false
	}
	
	// Check status
	if wo.Status != "active" && wo.Status != "approved" {
		return false
	}
	
	// Check legal review passed
	if !wo.LegalReview {
		return false
	}
	
	return true
}

func (a *AuthorizationGate) isWorkOrderValid() bool {
	return a.ValidateWorkOrder(a.workOrder)
}

func (a *AuthorizationGate) canExecute(operation string) bool {
	if a.workOrder == nil {
		return false
	}
	
	// Allow all operations if admin level
	if a.workOrder.Authorization == ADMIN {
		return true
	}
	
	// Map operations to required levels
	requiredLevel := map[string]string{
		"phishing_campaign":         EXECUTE,
		"supply_chain_attack":       EXECUTE,
		"ntlm_relay":                EXECUTE,
		"waf_exploitation":          EXECUTE,
		"code_signing":              ADMIN,
		"domain_compromise":         ADMIN,
		"data_exfiltration":         ADMIN,
	}
	
	reqLevel, exists := requiredLevel[operation]
	if !exists {
		return false
	}
	
	if a.workOrder.Authorization >= reqLevel {
		return true
	}
	
	return false
}

func (wo *WorkOrder) String() string {
	return fmt.Sprintf("WorkOrder{id=%s, creator=%s, status=%s}", wo.ID, wo.Creator, wo.Status)
}

type Level int

const (
	NONE Level = iota
	READ
	EXECUTE
	ADMIN
)

func (l Level) String() string {
	switch l {
	case NONE:
		return "NONE"
	case READ:
		return "READ"
	case EXECUTE:
		return "EXECUTE"
	case ADMIN:
		return "ADMIN"
	default:
		return "UNKNOWN"
	}
}
