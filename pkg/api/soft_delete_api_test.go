package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ============================================================================
// Mock Dependencies
// ============================================================================

type MockAuditLogger struct {
	mock.Mock
}

func (m *MockAuditLogger) Log(ctx context.Context, log store.AuditLog) error {
	m.Called(ctx, log)
	return nil
}

func (m *MockAuditLogger) QueryHistory(ctx context.Context, tableName, recordID string) ([]store.AuditLog, error) {
	args := m.Called(ctx, tableName, recordID)
	return args.Get(0).([]store.AuditLog), args.Error(1)
}

type MockUserIDProvider struct {
	mock.Mock
}

func (m *MockUserIDProvider) GetCurrentUserInfo() (*UserInfo, error) {
	args := m.Called()
	userInfo := args.Get(0).(*UserInfo)
	return userInfo, args.Error(1)
}

type UserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type MockDatabaseConnection struct {
	mock.Mock
}

func (m *MockDatabaseConnection) UpdateEntity(ctx context.Context, entity interface{}) error {
	args := m.Called(ctx, entity)
	return args.Error(0)
}

// ============================================================================
// Unit Tests for SoftDeleteAPI
// ============================================================================

func TestSoftDeleteAPI_SoftDelete_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	// Setup mocks
	mockLogger := &MockLogger{}
	mockStoreMgr := &MockSoftDeleteManager{}
	
	apiHandler := api.NewSoftDeleteAPI(mockStoreMgr, mockLogger)
	router := gin.New()
	apiHandler.RegisterRoutes(router)
	
	// Create request
	reqBody := api.DeleteRequest{
		Reason: "Test deletion reason with sufficient length",
	}
	
	jsonBody, _ := json.Marshal(reqBody)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	
	ctx := context.WithValue(context.Background(), "user_id", "test-user-123")
	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{},
		Header: make(http.Header),
		Body:   io.NopCloser(bytes.NewBuffer(jsonBody)),
		Context: ctx,
	}
	
	c.Params = gin.Params{
		{Key: "type", Value: "orders"},
		{Key: "id", Value: "order-abc-123"},
	}
	
	router.ServeHTTP(w, c.Request)
	
	assert.Equal(t, http.StatusOK, w.Code)
	
	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	
	assert.Equal(t, true, response["success"])
	assert.Equal(t, "order-abc-123", response["id"])
}

func TestSoftDeleteAPI_SoftDelete_AlreadyDeleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	mockLogger := &MockLogger{}
	mockStoreMgr := &MockSoftDeleteManager{}
	
	mockStoreMgr.On("IsSoftDeleted", mock.Anything, "orders", "order-123").Return(true).Once()
	
	apiHandler := api.NewSoftDeleteAPI(mockStoreMgr, mockLogger)
	router := gin.New()
	apiHandler.RegisterRoutes(router)
	
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	
	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{},
		Header: make(http.Header),
		Body:   io.NopCloser(bytes.NewBufferString(`{"reason": "test reason here"}`)),
	}
	
	c.Params = gin.Params{
		{Key: "type", Value: "orders"},
		{Key: "id", Value: "order-123"},
	}
	
	router.ServeHTTP(w, c.Request)
	
	assert.Equal(t, http.StatusConflict, w.Code)
	
	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	
	assert.Equal(t, "already soft-deleted", response["error"].(map[string]interface{})["message"])
}

func TestSoftDeleteAPI_InvalidResourceType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	mockLogger := &MockLogger{}
	mockStoreMgr := &MockSoftDeleteManager{}
	
	apiHandler := api.NewSoftDeleteAPI(mockStoreMgr, mockLogger)
	router := gin.New()
	apiHandler.RegisterRoutes(router)
	
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	
	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{},
		Header: make(http.Header),
	}
	
	c.Params = gin.Params{
		{Key: "type", Value: "invalid_resource_type"},
		{Key: "id", Value: "resource-123"},
	}
	
	router.ServeHTTP(w, c.Request)
	
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSoftDeleteAPI_InvalidDeletionReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	mockLogger := &MockLogger{}
	mockStoreMgr := &MockSoftDeleteManager{}
	
	apiHandler := api.NewSoftDeleteAPI(mockStoreMgr, mockLogger)
	router := gin.New()
	apiHandler.RegisterRoutes(router)
	
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	
	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{},
		Header: make(http.Header),
		Body:   io.NopCloser(bytes.NewBufferString(`{"reason": "short"}`)), // Too short
	}
	
	c.Params = gin.Params{
		{Key: "type", Value: "orders"},
		{Key: "id", Value: "order-123"},
	}
	
	router.ServeHTTP(w, c.Request)
	
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestSoftDeleteWorkflow_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	// Setup complete API chain
	logger := NewTestLogger()
	storeMgr := &testSoftDeleteManager{
		isDeleted: false,
	}
	
	apiHandler := api.NewSoftDeleteAPI(storeMgr, logger)
	router := gin.Default()
	apiHandler.RegisterRoutes(router)
	
	// Test 1: Create resource (simulated via POST to create endpoint)
	createReq := map[string]interface{}{
		"id":          "test-order-001",
		"customer_id": "cust-123",
		"amount":      100.00,
	}
	
	// Simulate create operation would set isDeleted = false
	storeMgr.isDeleted = false
	
	// Test 2: Delete the resource
	deleteBody := map[string]interface{}{
		"reason": "Customer requested cancellation of order due to incorrect item specification",
	}
	
	jsonBody, _ := json.Marshal(deleteBody)
	w := httptest.NewRecorder()
	
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{},
		Header: make(http.Header),
		Body:   io.NopCloser(bytes.NewBuffer(jsonBody)),
	}
	
	c.Params = gin.Params{
		{Key: "type", Value: "orders"},
		{Key: "id", Value: "test-order-001"},
	}
	
	router.ServeHTTP(w, c.Request)
	
	// Verify delete was successful
	assert.Equal(t, http.StatusOK, w.Code)
	
	var deleteResponse map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &deleteResponse)
	assert.Equal(t, true, deleteResponse["success"])
	
	// Set as deleted for next test
	storeMgr.isDeleted = true
	
	// Test 3: Try to delete again (should fail)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{},
		Header: make(http.Header),
		Body:   io.NopCloser(bytes.NewBuffer(jsonBody)),
	}
	
	c2.Params = gin.Params{
		{Key: "type", Value: "orders"},
		{Key: "id", Value: "test-order-001"},
	}
	
	router.ServeHTTP(w2, c2.Request)
	
	// Should return conflict status
	assert.Equal(t, http.StatusConflict, w2.Code)
	
	// Test 4: Restore the resource
	restoreURL := "/api/v2/resources/orders/test-order-001/restore"
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	
	c3.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{Path: restoreURL},
		Header: make(http.Header),
	}
	
	c3.Params = gin.Params{
		{Key: "type", Value: "orders"},
		{Key: "id", Value: "test-order-001"},
	}
	
	router.ServeHTTP(w3, c3.Request)
	
	// Verify restore was successful
	assert.Equal(t, http.StatusOK, w3.Code)
	
	var restoreResponse map[string]interface{}
	json.Unmarshal(w3.Body.Bytes(), &restoreResponse)
	assert.Equal(t, true, restoreResponse["success"])
	
	// Set back to not deleted for history check
	storeMgr.isDeleted = false
	
	// Test 5: Get deletion history
	historyURL := "/api/v2/resources/orders/test-order-001/history"
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	
	c4.Request = &http.Request{
		Method: "GET",
		URL:    &url.URL{Path: historyURL},
		Header: make(http.Header),
	}
	
	c4.Params = gin.Params{
		{Key: "type", Value: "orders"},
		{Key: "id", Value: "test-order-001"},
	}
	
	router.ServeHTTP(w4, c4.Request)
	
	// Verify history retrieved successfully
	assert.Equal(t, http.StatusOK, w4.Code)
	
	var historyResponse map[string]interface{}
	json.Unmarshal(w4.Body.Bytes(), &historyResponse)
	assert.NotNil(t, historyResponse["history"])
	assert.GreaterOrEqual(t, len(historyResponse["history"].([]interface{})), 2) // DELETE + RESTORE events
}

// ============================================================================
// Performance Benchmarks
// ============================================================================

func BenchmarkSoftDeleteAPI_Delete(b *testing.B) {
	gin.SetMode(gin.TestMode)
	
	mockLogger := &MockLogger{}
	mockStoreMgr := &MockSoftDeleteManager{}
	
	apiHandler := api.NewSoftDeleteAPI(mockStoreMgr, mockLogger)
	router := gin.New()
	apiHandler.RegisterRoutes(router)
	
	reqBody := []byte(`{"reason": "This is a test deletion reason with sufficient character count for validation"}`)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		
		c.Request = &http.Request{
			Method: "POST",
			URL:    &url.URL{},
			Header: make(http.Header),
			Body:   io.NopCloser(bytes.NewBuffer(reqBody)),
		}
		
		c.Params = gin.Params{
			{Key: "type", Value: "orders"},
			{Key: "id", Value: "benchmark-order-" + strconv.Itoa(i)},
		}
		
		router.ServeHTTP(w, c.Request)
	}
}

func BenchmarkSoftDeleteAPI_History(b *testing.B) {
	gin.SetMode(gin.TestMode)
	
	mockLogger := &MockLogger{}
	mockStoreMgr := &MockSoftDeleteManager{}
	
	apiHandler := api.NewSoftDeleteAPI(mockStoreMgr, mockLogger)
	router := gin.New()
	apiHandler.RegisterRoutes(router)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		
		c.Request = &http.Request{
			Method: "GET",
			URL:    &url.URL{},
			Header: make(http.Header),
		}
		
		c.Params = gin.Params{
			{Key: "type", Value: "orders"},
			{Key: "id", Value: "history-order-" + strconv.Itoa(i)},
		}
		
		router.ServeHTTP(w, c.Request)
	}
}

