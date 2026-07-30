// Package defensive_integration provides integration tests that validate
// the defensive programming framework is correctly applied across CloudAI Fusion.
// These tests serve as both verification and documentation of real-world usage patterns.
package defensive_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
)

// ============================================================================
// Test Suite: Nil Safety Guards
// ============================================================================

func TestRequireNonNil_Integration(t *testing.T) {
	t.Run("should reject nil pointer", func(t *testing.T) {
		var nilUser *UserProfile = nil
		err := defensive.RequireNonNil(nilUser, "user")
		
		assert.Error(t, err, "nil user should be rejected")
		assert.Contains(t, err.Error(), "must be non-nil")
	})
	
	t.Run("should accept valid pointer", func(t *testing.T) {
		validUser := &UserProfile{Name: "Alice"}
		err := defensive.RequireNonNil(validUser, "user")
		
		assert.NoError(t, err)
	})
	
	t.Run("should reject typed nil slice", func(t *testing.T) {
		var nilItems []Item = nil
		err := defensive.RequireNonNil(nilItems, "items")
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be non-nil")
	})
	
	t.Run("should accept empty but not nil slice", func(t *testing.T) {
		emptyItems := make([]Item, 0)
		err := defensive.RequireNonNil(emptyItems, "items")
		
		assert.NoError(t, err, "empty slice is valid in Go semantics")
	})
}

func TestRequireNotNil_Integration(t *testing.T) {
	t.Run("should detect typed nil for custom struct", func(t *testing.T) {
		var nilOrder *Order = nil
		err := defensive.RequireNotNil(nilOrder, "order")
		
		assert.Error(t, err, "typed nil should be detected")
	})
	
	t.Run("should pass valid instance", func(t *testing.T) {
		order := &Order{ID: "ORD-001"}
		err := defensive.RequireNotNil(order, "order")
		
		assert.NoError(t, err)
	})
}

func TestSafeDeref_Integration(t *testing.T) {
	t.Run("should safely dereference valid pointer", func(t *testing.T) {
		rate := 0.15
		value := defensive.SafeDeref(&rate)
		
		assert.Equal(t, 0.15, value)
	})
	
	t.Run("should return zero value for nil", func(t *testing.T) {
		var nilRate *float64 = nil
		value := defensive.SafeDeref(nilRate)
		
		assert.Equal(t, float64(0), value)
	})
	
	t.Run("should handle multiple levels of nesting", func(t *testing.T) {
		user := &UserProfile{
			DiscountRate: strPtr(0.1),
		}
		
		finalRate := defensive.SafeDeref(user.DiscountRate)
		assert.Equal(t, 0.1, finalRate)
	})
}

// ============================================================================
// Test Suite: Input Validation Guards
// ============================================================================

func TestValidateRange_Integration(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		min       float64
		max       float64
		field     string
		wantErr   bool
		errSubstr string
	}{
		{"valid range center", 50.0, 0.0, 100.0, "score", false, ""},
		{"value equals min", 0.0, 0.0, 100.0, "age", false, ""},
		{"value equals max", 100.0, 0.0, 100.0, "age", false, ""},
		{"below minimum", -1.0, 0.0, 100.0, "age", true, "range [0.000000, 100.000000]"},
		{"above maximum", 101.0, 0.0, 100.0, "age", true, "range [0.000000, 100.000000]"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := defensive.ValidateRange(tt.value, tt.min, tt.max, tt.field)
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateIntRange_Integration(t *testing.T) {
	t.Run("should accept valid page number", func(t *testing.T) {
		err := defensive.ValidateIntRange(1, 1, 100, "page")
		assert.NoError(t, err)
	})
	
	t.Run("should reject zero page number", func(t *testing.T) {
		err := defensive.ValidateIntRange(0, 1, 100, "page")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be in range [1, 100]")
	})
	
	t.Run("should reject negative index", func(t *testing.T) {
		err := defensive.ValidateIntRange(-1, 0, 1000, "index")
		assert.Error(t, err)
	})
	
	t.Run("should reject excessive page size", func(t *testing.T) {
		err := defensive.ValidateIntRange(1001, 1, 1000, "size")
		assert.Error(t, err)
	})
}

func TestValidateSliceBounds_Integration(t *testing.T) {
	items := make([]string, 10)
	
	t.Run("should accept valid indices", func(t *testing.T) {
		assert.NoError(t, defensive.ValidateSliceBounds(0, len(items), "index"))
		assert.NoError(t, defensive.ValidateSliceBounds(5, len(items), "index"))
		assert.NoError(t, defensive.ValidateSliceBounds(9, len(items), "index"))
	})
	
	t.Run("should reject negative index", func(t *testing.T) {
		err := defensive.ValidateSliceBounds(-1, len(items), "index")
		assert.Error(t, err)
		assert.IsType(t, &defensive.IndexOutOfBoundsError{}, err)
	})
	
	t.Run("should reject out-of-bounds index", func(t *testing.T) {
		err := defensive.ValidateSliceBounds(10, len(items), "index")
		assert.Error(t, err)
		idxErr := err.(*defensive.IndexOutOfBoundsError)
		assert.Equal(t, idxErr.Direction, "exceeds")
	})
	
	t.Run("should reject massive index", func(t *testing.T) {
		err := defensive.ValidateSliceBounds(10000, len(items), "index")
		assert.Error(t, err)
	})
}

func TestValidateNonEmptyString_Integration(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		field     string
		wantErr   bool
		errSubstr string
	}{
		{"valid email", "test@example.com", "email", false, ""},
		{"valid user ID", "usr_123456", "user_id", false, ""},
		{"whitespace only", "   ", "name", true, "must not be empty"},
		{"empty string", "", "title", true, "must not be empty"},
		{"newline only", "\n\t", "description", true, "must not be empty"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := defensive.ValidateNonEmptyString(tt.input, tt.field)
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// Test Suite: Error Handling Patterns
// ============================================================================

func TestAppError_Integration(t *testing.T) {
	t.Run("should create error with explicit cause", func(t *testing.T) {
		baseErr := fmt.Errorf("database connection failed")
		appErr := defensive.NewAppError(defensive.ErrorCodeInternal, 
			"failed to fetch users", baseErr)
		
		assert.Equal(t, defensive.ErrorCodeInternal, appErr.Code)
		assert.Contains(t, appErr.Message, "failed to fetch users")
		assert.Equal(t, baseErr, appErr.Cause)
	})
	
	t.Run("should create error without cause", func(t *testing.T) {
		appErr := defensive.NewAppError(defensive.ErrorCodeValidation, 
			"invalid request body")
		
		assert.Nil(t, appErr.Cause)
		assert.Equal(t, defensive.ErrorCodeValidation, appErr.Code)
	})
	
	t.Run("should support WithMetadata", func(t *testing.T) {
		appErr := defensive.NewAppError(defensive.ErrorCodeNotFound, 
			"user not found").
			WithMetadata("user_id", "usr_123").
			WithMetadata("retry_after", 60)
		
		assert.Equal(t, "usr_123", appErr.Metadata["user_id"])
		assert.Equal(t, float64(60), appErr.Metadata["retry_after"])
	})
	
	t.Run("should implement Unwrap correctly", func(t *testing.T) {
		baseErr := fmt.Errorf("original error")
		wrapped := defensive.Wrap(baseErr, defensive.ErrorCodeTimeout, 
			"operation timeout")
		
		unwrapped := wrapped.Unwrap()
		assert.Equal(t, baseErr, unwrapped)
		assert.True(t, errors.Is(wrapped, baseErr))
	})
}

func TestWrapFunction_Integration(t *testing.T) {
	t.Run("should wrap raw error", func(t *testing.T) {
		original := fmt.Errorf("connection refused")
		wrapped := defensive.Wrap(original, defensive.ErrorCodeInternal, 
			"failed to connect database")
		
		assert.NotNil(t, wrapped)
		assert.Equal(t, original, wrapped.Cause)
		assert.Equal(t, defensive.ErrorCodeInternal, wrapped.Code)
	})
	
	t.Run("should not double-wrap AppError", func(t *testing.T) {
		existing := defensive.NotFound("tenant", "tenant-123")
		reWrapped := defensive.Wrap(existing, defensive.ErrorCodeNotFound, 
			"tenant lookup failed")
		
		assert.Same(t, existing, reWrapped, "should return same instance")
	})
	
	t.Run("should handle nil gracefully", func(t *testing.T) {
		result := defensive.Wrap(nil, defensive.ErrorCodeInternal, "message")
		assert.Nil(t, result)
	})
}

func TestValidationErrorHelper_Integration(t *testing.T) {
	err := defensive.ValidationError("email", "invalid format")
	
	assert.Equal(t, defensive.ErrorCodeValidation, err.Code)
	assert.Contains(t, err.Message, "invalid format")
	assert.Equal(t, "email", err.Metadata["field"])
}

func TestNotFoundHelper_Integration(t *testing.T) {
	err := defensive.NotFound("user", "usr_abc123")
	
	assert.Equal(t, defensive.ErrorCodeNotFound, err.Code)
	assert.Contains(t, err.Message, "not found")
	assert.Equal(t, "user", err.Metadata["resource"])
	assert.Equal(t, "usr_abc123", err.Metadata["identifier"])
}

// ============================================================================
// Test Suite: Try-Fallback Pattern
// ============================================================================

func TestTryPattern_Integration(t *testing.T) {
	t.Run("should execute function successfully", func(t *testing.T) {
		result, err := defensive.Try(func() (*UserProfile, error) {
			return &UserProfile{Name: "Bob"}, nil
		}, nil)
		
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "Bob", result.Name)
	})
	
	t.Run("should return fallback on error", func(t *testing.T) {
		defaultUser := &UserProfile{Name: "Anonymous"}
		result := defensive.Try(func() (*UserProfile, error) {
			return nil, fmt.Errorf("load failed")
		}, defaultUser)
		
		assert.Same(t, defaultUser, result)
	})
	
	t.Run("should work with value types", func(t *testing.T) {
		configPath := defensive.Try(func() (string, error) {
			return "/etc/config.yaml", nil
		}, "/default/config.yaml")
		
		assert.Equal(t, "/etc/config.yaml", configPath)
	})
}

func TestCoalescePattern_Integration(t *testing.T) {
	t.Run("should return first non-zero value", func(t *testing.T) {
		val := defensive.Coalesce([]float64{0.0, 0.0, 0.15, 0.2}, 0.05)
		assert.Equal(t, 0.15, val)
	})
	
	t.Run("should use default when all values are zero", func(t *testing.T) {
		val := defensive.Coalesce([]float64{0.0, 0.0}, 0.05)
		assert.Equal(t, 0.05, val)
	})
	
	t.Run("should handle single element slice", func(t *testing.T) {
		val := defensive.Coalesce([]int{42}, 0)
		assert.Equal(t, 42, val)
	})
}

// ============================================================================
// Test Suite: HTTP Middleware Integration
// ============================================================================

func TestDefensiveMiddleware_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.Default()
	
	// Apply defensive middleware
	router.Use(defensive.DefensiveMiddleware())
	
	router.GET("/test", func(c *gin.Context) {
		requestID := c.GetString("request_id")
		assert.NotEmpty(t, requestID, "request ID should be generated")
		
		ctx := c.Get("defensive_context")
		assert.NotNil(t, ctx, "defensive context should be set")
		
		c.JSON(http.StatusOK, gin.H{
			"status":      "ok",
			"request_id":  requestID,
		})
	})
	
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptestRecorder()
	router.ServeHTTP(w, req)
	
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequestValidator_Integration(t *testing.T) {
	router := gin.Default()
	router.Use(defensive.DefensiveMiddleware())
	
	router.GET("/users/:id", func(c *gin.Context) {
		validator := &defensive.RequestValidator{c: c}
		
		// Validate required parameter
		if err := validator.ValidateParam("id"); err != nil {
			defensive.StandardErrorHandler(c, []error{err})
			c.Abort()
			return
		}
		
		id := c.Param("id")
		c.JSON(http.StatusOK, gin.H{"user_id": id})
	})
	
	router.GET("/users/search", func(c *gin.Context) {
		validator := &defensive.RequestValidator{c: c}
		
		// Validate optional query parameter
		if err := validator.ValidateQuery("limit", true); err != nil {
			defensive.StandardErrorHandler(c, []error{err})
			c.Abort()
			return
		}
		
		limitStr := c.Query("limit")
		c.JSON(http.StatusOK, gin.H{"limit": limitStr})
	})
	
	t.Run("should validate required param", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/users/usr_123", nil)
		w := httptestRecorder()
		
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "usr_123")
	})
	
	t.Run("should fail validation when param missing", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/users/", nil)
		w := httptestRecorder()
		
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "VALIDATION_ERROR")
	})
	
	t.Run("should allow optional param absence", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/users/search", nil)
		w := httptestRecorder()
		
		router.ServeHTTP(w, req)
		
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// ============================================================================
// Test Suite: Real Business Logic Scenarios
// ============================================================================

// Simulated domain models for testing
type UserProfile struct {
	Name           string
	Email          string
	Age            *int
	DiscountRate   *float64
	DefaultDiscount float64
}

type Item struct {
	ID   string
	Name string
}

type Order struct {
	ID          string
	UserID      *string
	Items       []Item
	Status      string
	TotalAmount float64
}

func strPtr(s string) *string {
	return &s
}

func httptestRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func TestProcessUserUpdate_RealScenario(t *testing.T) {
	ctx := context.Background()
	
	// Scenario 1: Valid user update
	t.Run("should process valid update", func(t *testing.T) {
		userID := "usr_123"
		profile := &UserProfile{
			Name:  "Alice Updated",
			Email: "alice@example.com",
			Age:   intPtr(28),
		}
		
		// Defensive guards
		require.NoError(t, defensive.ValidateNonEmptyString(userID, "user_id"))
		require.NoError(t, defensive.RequireNonNil(profile, "profile"))
		
		// Age validation with range check
		if profile.Age != nil {
			err := defensive.ValidateRange(float64(*profile.Age), 0, 150, "age")
			require.NoError(t, err)
		}
		
		// Simulate processing
		assert.Equal(t, "Alice Updated", profile.Name)
	})
	
	// Scenario 2: Missing user object
	t.Run("should reject nil user", func(t *testing.T) {
		userID := "usr_456"
		var profile *UserProfile = nil
		
		err := defensive.RequireNonNil(profile, "profile")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be non-nil")
	})
	
	// Scenario 3: Out-of-range age
	t.Run("should reject invalid age", func(t *testing.T) {
		age := -5
		profile := &UserProfile{Age: &age}
		
		err := defensive.ValidateRange(float64(*profile.Age), 0, 150, "age")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "range [0.000000, 150.000000]")
	})
}

func TestCalculateDiscount_RealScenario(t *testing.T) {
	// Scenario 1: User with explicit discount rate
	t.Run("should apply explicit discount", func(t *testing.T) {
		discountRate := 0.15
		user := &UserProfile{
			DiscountRate:    &discountRate,
			DefaultDiscount: 0.05,
		}
		
		amount := 100.0
		discount := defensive.SafeDeref(user.DiscountRate)
		finalPrice := amount * (1 - discount)
		
		assert.Equal(t, 85.0, finalPrice)
	})
	
	// Scenario 2: User with no discount rate (nil)
	t.Run("should fall back to default", func(t *testing.T) {
		user := &UserProfile{
			DiscountRate:    nil,
			DefaultDiscount: 0.05,
		}
		
		amount := 100.0
		discount := defensive.SafeDeref(user.DiscountRate)
		finalDiscount := defensive.Coalesce([]float64{
			discount, 
			user.DefaultDiscount, 
			0.0,
		}, 0.05)
		
		finalPrice := amount * (1 - finalDiscount)
		assert.Equal(t, 95.0, finalPrice)
	})
}

func TestValidateEngagement_RealScenario(t *testing.T) {
	type EngagementScope struct {
		Name           string
		DurationHours  int
		Targets        []string
	}
	
	// Scenario 1: Valid engagement scope
	t.Run("should accept valid scope", func(t *testing.T) {
		scope := &EngagementScope{
			Name:          "Security Audit Q3",
			DurationHours: 48,
			Targets:       []string{"api-prod-001", "web-prod-002"},
		}
		
		// Chain validations
		checks := []struct {
			fn func() error
			msg string
		}{
			{
				fn: func() error {
					return defensive.ValidateNonEmptyString(scope.Name, "name")
				},
				msg: "name required",
			},
			{
				fn: func() error {
					return defensive.ValidateRange(float64(scope.DurationHours), 1, 720, "duration")
				},
				msg: "duration must be 1-720 hours",
			},
			{
				fn: func() error {
					if len(scope.Targets) == 0 {
						return defensive.ValidationError("targets", "at least one target required")
					}
					return nil
				},
				msg: "targets required",
			},
		}
		
		for _, check := range checks {
			err := check.fn()
			assert.NoError(t, err, check.msg)
		}
	})
	
	// Scenario 2: Missing name
	t.Run("should reject scope without name", func(t *testing.T) {
		scope := &EngagementScope{
			DurationHours: 24,
			Targets:       []string{"target-1"},
		}
		
		err := defensive.ValidateNonEmptyString(scope.Name, "name")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must not be empty")
	})
	
	// Scenario 3: Invalid duration
	t.Run("should reject impossible duration", func(t *testing.T) {
		scope := &EngagementScope{
			Name:          "Test",
			DurationHours: 1000, // Too long
			Targets:       []string{"target-1"},
		}
		
		err := defensive.ValidateRange(float64(scope.DurationHours), 1, 720, "duration")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "range [1.000000, 720.000000]")
	})
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

// Benchmark Tests for Performance Analysis
func BenchmarkRequireNonNil(b *testing.B) {
	user := &UserProfile{Name: "Benchmark"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = defensive.RequireNonNil(user, "user")
	}
}

func BenchmarkValidateRange(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = defensive.ValidateRange(50.0, 0.0, 100.0, "score")
	}
}

func BenchmarkSafeDeref(b *testing.B) {
	rate := 0.15
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = defensive.SafeDeref(&rate)
	}
}
