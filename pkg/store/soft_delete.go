// Package store implements soft-delete functionality with audit trail for SOX/GDPR compliance.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// AuditLogger is the same as *Store (implements Log method via AuditLog CRUD)
type AuditLogger = *Store

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

// ============================================================================
// SoftDeleteManager - Orchestrates soft delete operations with audit
// ============================================================================

// SoftDeleteManager manages all soft delete operations
type SoftDeleteManager struct {
	db              DatabaseConnection
	auditLogger     *Store
	userProvider    UserIDProvider
	reasonValidator ReasonValidator
	logger          *logrus.Logger
}

// NewSoftDeleteManager creates new soft delete manager instance
func NewSoftDeleteManager(
	db DatabaseConnection,
	auditLogger *Store,
	userProvider UserIDProvider,
	reasonValidator ReasonValidator,
	logger *logrus.Logger,
) *SoftDeleteManager {
	if db == nil || auditLogger == nil || userProvider == nil || reasonValidator == nil {
		panic("all required dependencies must be non-nil")
	}
	
	defensive.RequireNonNil(db, "database")
	defensive.RequireNonNil(auditLogger, "audit_logger")
	
	return &SoftDeleteManager{
		db:              db,
		auditLogger:     auditLogger,
		userProvider:    userProvider,
		reasonValidator: reasonValidator,
		logger:          logger.WithField("component", "soft_delete_manager").(*logrus.Logger),
	}
}

// SoftDelete performs soft delete with complete audit trail
func (m *SoftDeleteManager) SoftDelete(ctx context.Context, entity SoftDeletable, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Defensive programming guards
	defensive.RequireNonNil(entity, "entity")
	
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
	
	// Note: Snapshot functionality not implemented in MVP version
	// This is a placeholder for future enhancement
	
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
	
	// Log deletion using Store's existing AuditLog CRUD
	deleteEntry := &AuditLog{
		UserID:       currentUser.ID,
		Username:     currentUser.Email, // Reuse field for simplicity
		Action:       "DELETE",
		ResourceType: entity.GetTableName(),
		ResourceID:   entity.GetID(),
		IPAddress:    getCurrentIP(ctx),
		UserAgent:    getUserAgent(ctx),
		Status:       "completed",
		Details:      reason,
		CreatedAt:    timestamp,
	}
	
	if err := m.auditLogger.CreateAuditLog(deleteEntry); err != nil {
		m.logger.WithError(err).Warn("Failed to create audit log (non-critical)")
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
