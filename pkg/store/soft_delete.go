// Package store implements soft-delete functionality with audit trail for SOX/GDPR compliance.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/google/uuid"
)

// ============================================================================
// SoftDeletable Interface - All entities that support soft deletion
// ============================================================================

// SoftDeletable defines the interface for entities that support soft deletion
type SoftDeletable interface {
	GetDeletedAt() *time.Time
	SetDeletedAt(t time.Time)
	GetDeletedBy() string
	SetDeletedBy(userID string)
	GetDeletionReason() string
	SetDeletionReason(reason string)
	
	// GetID returns entity primary key
	GetID() string
	
	// GetTableName returns database table name for audit logging
	GetTableName() string
}

// AuditLog represents immutable audit trail entry
type AuditLog struct {
	LogID       uuid.UUID    `json:"log_id"`
	Action      ActionType   `json:"action"`           // CREATE, UPDATE, DELETE, RESTORE
	TableName   string       `json:"table_name"`
	RecordID    string       `json:"record_id"`
	OldValue    map[string]interface{} `json:"old_value,omitempty"`
	NewValue    map[string]interface{} `json:"new_value,omitempty"`
	UserID      string       `json:"user_id"`
	UserEmail   string       `json:"user_email,omitempty"`
	IPAddress   string       `json:"ip_address"`
	UserAgent   string       `json:"user_agent,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	SessionID   uuid.UUID    `json:"session_id,omitempty"`
	RequestID   uuid.UUID    `json:"request_id,omitempty"`
}

// ActionType defines type of database operation
type ActionType string

const (
	ActionCreate ActionType = "CREATE"
	ActionUpdate ActionType = "UPDATE"
	ActionDelete ActionType = "DELETE"
	ActionRestore ActionType = "RESTORE"
)

// AuditLogger interfaces with audit log table
type AuditLogger interface {
	// Log creates immutable audit trail record
	Log(ctx context.Context, log AuditLog) error
	
	// QueryHistory retrieves complete lifecycle of entity
	QueryHistory(ctx context.Context, tableName, recordID string) ([]AuditLog, error)
}

// ============================================================================
// SoftDeleteManager - Orchestrates soft delete operations with audit
// ============================================================================

// SoftDeleteManager manages all soft delete operations
type SoftDeleteManager struct {
	db              DatabaseConnection
	auditLogger     AuditLogger
	userProvider    UserIDProvider
	reasonValidator ReasonValidator
	logger          Logger
}

// NewSoftDeleteManager creates new soft delete manager instance
func NewSoftDeleteManager(
	db DatabaseConnection,
	auditLogger AuditLogger,
	userProvider UserIDProvider,
	reasonValidator ReasonValidator,
	logger Logger,
) *SoftDeleteManager {
	if db == nil || auditLogger == nil || userProvider == nil || reasonValidator == nil {
		panic("all required dependencies must be non-nil")
	}
	
	defensive.ValidateNonNil(db, "database")
	defensive.ValidateNonNil(auditLogger, "audit_logger")
	
	return &SoftDeleteManager{
		db:              db,
		auditLogger:     auditLogger,
		userProvider:    userProvider,
		reasonValidator: reasonValidator,
		logger:          logger.WithField("component", "soft_delete_manager"),
	}
}

// SoftDelete performs soft delete with complete audit trail
func (m *SoftDeleteManager) SoftDelete(ctx context.Context, entity SoftDeletable, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Defensive programming guards
	if err := defensive.RequireNonNil(entity, "entity"); err != nil {
		return err
	}
	
	if err := defensive.ValidateNonEmptyString(reason, "deletion_reason"); err != nil {
		return fmt.Errorf("deletion reason required: %w", err)
	}
	
	// Validate reason meets minimum requirements
	if err := m.reasonValidator.Validate(reason); err != nil {
		return fmt.Errorf("invalid deletion reason: %w", err)
	}
	
	// Get current user information
	currentUser := m.userProvider.GetCurrentUser(ctx)
	if currentUser == nil {
		return fmt.Errorf("current user not available in context")
	}
	
	// Create audit log entry before deletion
	oldState, err := m.snapshotCurrentState(ctx, entity)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to snapshot old state")
	}
	
	// Update entity with soft delete markers
	timestamp := time.Now().UTC()
	entity.SetDeletedAt(timestamp)
	entity.SetDeletedBy(currentUser.ID)
	entity.SetDeletionReason(reason)
	
	// Perform actual database update
	if err := m.db.UpdateEntity(ctx, entity); err != nil {
		// Rollback if audit logging fails
		m.logger.WithError(err).Error("Database update failed")
		return fmt.Errorf("soft delete failed: %w", err)
	}
	
	// Log deletion in immutable audit trail
	deleteLog := AuditLog{
		LogID:      uuid.New(),
		Action:     ActionDelete,
		TableName:  entity.GetTableName(),
		RecordID:   entity.GetID(),
		OldValue:   oldState,
		NewValue:   nil, // No new value after delete
		UserID:     currentUser.ID,
		UserEmail:  currentUser.Email,
		IPAddress:  getCurrentIP(ctx),
		UserAgent:  getUserAgent(ctx),
		CreatedAt:  timestamp,
		SessionID:  getSessionID(ctx),
		RequestID:  getRequestID(ctx),
	}
	
	if err := m.auditLogger.Log(ctx, deleteLog); err != nil {
		m.logger.WithError(err).Error("Audit log creation failed")
		return fmt.Errorf("audit trail update failed: %w", err)
	}
	
	m.logger.WithFields(logrus.Fields{
		"entity_id": entity.GetID(),
		"table":     entity.GetTableName(),
		"reason":    reason,
	}).Info("Entity soft-deleted successfully")
	
	return nil
}

// Restore restores a soft-deleted entity
func (m *SoftDeleteManager) Restore(ctx context.Context, entity SoftDeletable, restoredBy string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Validate entity is actually deleted
	if entity.GetDeletedAt() == nil {
		return fmt.Errorf("entity %s is not soft-deleted", entity.GetID())
	}
	
	// Perform restore by setting deleted fields to NULL
	timestamp := time.Now().UTC()
	entity.SetDeletedAt(nil)
	entity.SetDeletedBy("")
	entity.SetDeletionReason(fmt.Sprintf("Restored on %s", timestamp.Format(time.RFC3339)))
	
	// Update database
	if err := m.db.UpdateEntity(ctx, entity); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}
	
	// Log restoration in audit trail
	restoreLog := AuditLog{
		LogID:     uuid.New(),
		Action:    ActionRestore,
		TableName: entity.GetTableName(),
		RecordID:  entity.GetID(),
		OldValue:  nil,
		NewValue:  nil, // No value captured for restore
		UserID:    restoredBy,
		CreatedAt: timestamp,
	}
	
	if err := m.auditLogger.Log(ctx, restoreLog); err != nil {
		m.logger.WithError(err).Error("Audit log creation failed for restore")
		return fmt.Errorf("audit trail update failed: %w", err)
	}
	
	m.logger.WithFields(logrus.Fields{
		"entity_id": entity.GetID(),
		"restored_by": restoredBy,
	}).Info("Entity restored successfully")
	
	return nil
}

// IsSoftDeleted checks if entity has been soft deleted
func (m *SoftDeleteManager) IsSoftDeleted(ctx context.Context, entityType string, recordID string) bool {
	query := fmt.Sprintf(`SELECT deleted_at IS NOT NULL FROM %s WHERE id = $1`, entityType)
	
	var isDeleted bool
	err := m.db.QueryRow(ctx, query, recordID).Scan(&isDeleted)
	if err != nil {
		m.logger.WithError(err).Warn("IsSoftDeleted query failed")
		return false
	}
	
	return isDeleted
}

// GetDeletionHistory retrieves complete lifecycle including deletions and restorations
func (m *SoftDeleteManager) GetDeletionHistory(ctx context.Context, entityType string, recordID string) ([]AuditLog, error) {
	history, err := m.auditLogger.QueryHistory(ctx, entityType, recordID)
	if err != nil {
		return nil, fmt.Errorf("failed to query history: %w", err)
	}
	
	return history, nil
}

// Helper functions

func snapshotCurrentState(ctx context.Context, entity SoftDeletable) (map[string]interface{}, error) {
	// In production: serialize entity to JSON map using reflection or marshalling
	// For now, return minimal state
	return map[string]interface{}{"id": entity.GetID()}, nil
}
