// Package store provides comprehensive test coverage for soft delete functionality.
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ============================================================================
// Mock Implementations for Testing
// ============================================================================

type mockAuditLogger struct {
	logs []store.AuditLog
}

func (m *mockAuditLogger) Log(ctx context.Context, log store.AuditLog) error {
	m.logs = append(m.logs, log)
	return nil
}

func (m *mockAuditLogger) QueryHistory(ctx context.Context, tableName, recordID string) ([]store.AuditLog, error) {
	var history []store.AuditLog
	for _, log := range m.logs {
		if log.TableName == tableName && log.RecordID == recordID {
			history = append(history, log)
		}
	}
	return history, nil
}

type mockUserIDProvider struct{}

func (m *mockUserIDProvider) GetCurrentUserInfo() (*userInfo, error) {
	return &userInfo{ID: "test-user", Email: "test@example.com"}, nil
}

type userInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type mockDBConnection struct{}

func (m *mockDBConnection) UpdateEntity(ctx context.Context, entity interface{}) error {
	// Simulate successful update
	return nil
}

type mockReasonValidator struct{}

func (m *mockReasonValidator) Validate(reason string) error {
	// Simple validation: min 10 chars
	if len(reason) < 10 {
		return assert.AnError
	}
	return nil
}

type testSoftDeletable struct {
	ID             string
	TableName      string
	DeletedAt      *time.Time
	DeletedBy      string
	DeletionReason string
}

func (t *testSoftDeletable) GetID() string                    { return t.ID }
func (t *testSoftDeletable) GetTableName() string             { return t.TableName }
func (t *testSoftDeletable) GetDeletedAt() *time.Time         { return t.DeletedAt }
func (t *testSoftDeletable) SetDeletedAt(time.Time)           {}
func (t *testSoftDeletable) GetDeletedBy() string             { return t.DeletedBy }
func (t *testSoftDeletable) SetDeletedBy(string)              {}
func (t *testSoftDeletable) GetDeletionReason() string        { return t.DeletionReason }
func (t *testSoftDeletable) SetDeletionReason(string)         {}

// ============================================================================
// Unit Tests for SoftDeleteManager
// ============================================================================

func TestSoftDeleteManager_SoftDelete_Success(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	entity := &testSoftDeletable{
		ID:          "order-123",
		TableName:   "orders",
		DeletedAt:   nil,
		DeletedBy:   "",
		DeletionReason: "",
	}
	
	err := manager.SoftDelete(context.Background(), entity, "Customer requested cancellation due to order accuracy issues")
	
	assert.NoError(t, err, "Soft delete should succeed")
	assert.NotNil(t, entity.DeletedAt, "DeletedAt should be set")
	assert.Equal(t, "test-user", entity.DeletedBy, "DeletedBy should be set")
	assert.Equal(t, "Customer requested cancellation due to order accuracy issues", entity.DeletionReason)
	
	assert.Len(t, logger.logs, 1, "Should create one audit log entry")
	assert.Equal(t, store.ActionDelete, logger.logs[0].Action, "Action should be DELETE")
	assert.Equal(t, "orders", logger.logs[0].TableName)
	assert.Equal(t, "order-123", logger.logs[0].RecordID)
}

func TestSoftDeleteManager_SoftDelete_AlreadyDeleted(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	now := time.Now().UTC()
	entity := &testSoftDeletable{
		ID:          "order-456",
		TableName:   "orders",
		DeletedAt:   &now,
		DeletedBy:   "previous-user",
		DeletionReason: "Previous deletion reason",
	}
	
	err := manager.SoftDelete(context.Background(), entity, "Attempted re-deletion")
	
	// Should fail because entity is already deleted
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already soft-deleted")
}

func TestSoftDeleteManager_SoftDelete_InvalidReason(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	entity := &testSoftDeletable{
		ID:          "order-789",
		TableName:   "orders",
		DeletedAt:   nil,
	}
	
	err := manager.SoftDelete(context.Background(), entity, "Short") // Too short
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid deletion reason")
}

func TestSoftDeleteManager_Restore(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	now := time.Now().UTC()
	entity := &testSoftDeletable{
		ID:            "order-restore-test",
		TableName:     "orders",
		DeletedAt:     &now,
		DeletedBy:     "previous-user",
		DeletionReason: "Original deletion reason",
	}
	
	err := manager.Restore(context.Background(), entity, "restorer-user")
	
	assert.NoError(t, err, "Restore should succeed")
	assert.Nil(t, entity.DeletedAt, "DeletedAt should be cleared")
	assert.Equal(t, "", entity.DeletedBy, "DeletedBy should be cleared")
	assert.Contains(t, entity.DeletionReason, "Restored on", "Deletion reason should contain restore info")
	
	assert.Len(t, logger.logs, 1, "Should create one audit log for restore")
	assert.Equal(t, store.ActionRestore, logger.logs[0].Action, "Action should be RESTORE")
}

func TestSoftDeleteManager_IsSoftDeleted(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	// Test non-deleted entity
	isDeleted := manager.IsSoftDeleted(context.Background(), "orders", "order-non-deleted")
	assert.False(t, isDeleted, "Non-deleted entity should not be marked as soft-deleted")
	
	// Test deleted entity
	now := time.Now().UTC()
	testEntity := &testSoftDeletable{
		ID:          "order-deleted",
		TableName:   "orders",
		DeletedAt:   &now,
	}
	
	err := manager.SoftDelete(context.Background(), testEntity, "Deletion test reason here")
	assert.NoError(t, err)
	
	isDeleted = manager.IsSoftDeleted(context.Background(), "orders", "order-deleted")
	assert.True(t, isDeleted, "Deleted entity should be marked as soft-deleted")
}

func TestSoftDeleteManager_GetDeletionHistory(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	entity := &testSoftDeletable{
		ID:          "history-test-order",
		TableName:   "orders",
	}
	
	// Perform deletion
	err := manager.SoftDelete(context.Background(), entity, "Test deletion with sufficient length")
	assert.NoError(t, err)
	
	// Restore the entity
	err = manager.Restore(context.Background(), entity, "restorer")
	assert.NoError(t, err)
	
	// Query history
	history, err := manager.GetDeletionHistory(context.Background(), "orders", "history-test-order")
	assert.NoError(t, err)
	
	assert.GreaterOrEqual(t, len(history), 2, "Should have at least DELETE and RESTORE events")
	
	// Verify history contains correct actions
	hasDelete := false
	hasRestore := false
	for _, log := range history {
		if log.Action == store.ActionDelete {
			hasDelete = true
		}
		if log.Action == store.ActionRestore {
			hasRestore = true
		}
	}
	
	assert.True(t, hasDelete, "History should include DELETE action")
	assert.True(t, hasRestore, "History should include RESTORE action")
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestSoftDeleteManager_EmptyReason(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	entity := &testSoftDeletable{ID: "test-empty-reason", TableName: "orders"}
	
	err := manager.SoftDelete(context.Background(), entity, "")
	assert.Error(t, err)
}

func TestSoftDeleteManager_NilEntity(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	err := manager.SoftDelete(context.Background(), nil, "Some reason with sufficient length")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entity cannot be nil")
}

func TestSoftDeleteManager_ConcurrentAccess(t *testing.T) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	entity := &testSoftDeletable{ID: "concurrent-test", TableName: "orders"}
	
	// Run concurrent operations
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			
			err := manager.SoftDelete(context.Background(), entity, "Concurrent access test reason here")
			if err != nil {
				// Subsequent deletes will fail, which is expected
			}
		}()
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
	
	assert.NotNil(t, entity.DeletedAt, "Entity should be deleted after concurrent operations")
	assert.Greater(t, len(logger.logs), 1, "Should have audit logs from concurrent operations")
}

// ============================================================================
// Performance Tests
// ============================================================================

func BenchmarkSoftDeleteManager_SoftDelete(b *testing.B) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	entity := &testSoftDeletable{
		ID:          "benchmark-entity",
		TableName:   "orders",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := manager.SoftDelete(context.Background(), entity, "Benchmark deletion test with sufficient character count")
		if err != nil {
			b.Fatal(err)
		}
		// Reset for next iteration
		entity.DeletedAt = nil
		entity.DeletedBy = ""
		entity.DeletionReason = ""
	}
}

func BenchmarkSoftDeleteManager_QueryHistory(b *testing.B) {
	logger := &mockAuditLogger{}
	userProvider := &mockUserIDProvider{}
	reasonValidator := &mockReasonValidator{}
	db := &mockDBConnection{}
	
	manager := store.NewSoftDeleteManager(db, logger, userProvider, reasonValidator, nil)
	
	// Pre-populate history
	entity := &testSoftDeletable{ID: "benchmark-history", TableName: "orders"}
	for i := 0; i < 100; i++ {
		manager.SoftDelete(context.Background(), entity, "Benchmark reason test with sufficient length here")
		manager.Restore(context.Background(), entity, "restorer")
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := manager.GetDeletionHistory(context.Background(), "orders", "benchmark-history")
		if err != nil {
			b.Fatal(err)
		}
	}
}
