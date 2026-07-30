package defensive

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// Nil Guard Tests
// ============================================================================

func TestRequireNonNil(t *testing.T) {
	tests := []struct {
		name    string
		val     interface{}
		field   string
		wantErr bool
	}{
		{"nil pointer", (*string)(nil), "ptr", true},
		{"valid pointer", strPtr("hello"), "ptr", false},
		{"nil slice", ([]int)(nil), "slice", true},
		{"empty slice (not nil)", []int{}, "slice", false}, // Empty slice is NOT nil
		{"nil map", (map[string]int)(nil), "map", true},
		{"valid map", map[string]int{"a": 1}, "map", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireNonNil(tt.val, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequireNonNil() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireNotNil(t *testing.T) {
	tests := []struct {
		name    string
		val     interface{}
		field   string
		wantErr bool
	}{
		{"typed nil", (*UserProfile)(nil), "user", true},
		{"valid typed value", &UserProfile{Name: "John"}, "user", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RequireNotNil(tt.val, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequireNotNil() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type UserProfile struct {
	Name string
}

// ============================================================================
// Range Validation Tests
// ============================================================================

func TestValidateRange(t *testing.T) {
	tests := []struct {
		name      string
		value     float64
		min       float64
		max       float64
		field     string
		wantErr   bool
		errMsgSub string
	}{
		{"value in range", 50.0, 0.0, 100.0, "score", false, ""},
		{"value equals min", 0.0, 0.0, 100.0, "score", false, ""},
		{"value equals max", 100.0, 0.0, 100.0, "score", false, ""},
		{"value below min", -1.0, 0.0, 100.0, "score", true, "range [0.000000, 100.000000]"},
		{"value above max", 101.0, 0.0, 100.0, "score", true, "range [0.000000, 100.000000]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRange(tt.value, tt.min, tt.max, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRange() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.wantErr && tt.errMsgSub != "" {
				if errMsg := err.Error(); errMsgSub(errMsg, tt.errMsgSub) == false {
					t.Errorf("ValidateRange() error message = %v, does not contain %v", errMsg, tt.errMsgSub)
				}
			}
		})
	}
}

func TestValidateSliceBounds(t *testing.T) {
	tests := []struct {
		name      string
		index     int
		length    int
		field     string
		wantErr   bool
		errType   string // "IndexOutOfBoundsError"
		direction string
	}{
		{"valid index 0", 0, 10, "items", false, "", ""},
		{"valid index middle", 5, 10, "items", false, "", ""},
		{"valid index last", 9, 10, "items", false, "", ""},
		{"negative index", -1, 10, "items", true, "IndexOutOfBoundsError", "negative"},
		{"exceeds bounds", 10, 10, "items", true, "IndexOutOfBoundsError", "exceeds"},
		{"way too large", 100, 10, "items", true, "IndexOutOfBoundsError", "exceeds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSliceBounds(tt.index, tt.length, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSliceBounds() error = %v, wantErr %v", err, tt.wantErr)
			}
			
			if err != nil && tt.errType != "" {
				if _, ok := err.(*IndexOutOfBoundsError); !ok {
					t.Errorf("ValidateSliceBounds() returned %T, want %s", err, tt.errType)
				}
				
				if idxErr, ok := err.(*IndexOutOfBoundsError); ok {
					if idxErr.Direction != tt.direction {
						t.Errorf("ValidateSliceBounds() direction = %v, want %v", idxErr.Direction, tt.direction)
					}
				}
			}
		})
	}
}

// ============================================================================
// String Validation Tests
// ============================================================================

func TestValidateNonEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		s         string
		field     string
		wantErr   bool
		errField  string
	}{
		{"non-empty valid", "hello", "name", false, ""},
		{"whitespace only", "   ", "name", true, "name"},
		{"empty string", "", "name", true, "name"},
		{"newline only", "\n\t", "name", true, "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNonEmptyString(tt.s, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNonEmptyString() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if valErr, ok := err.(*ValidationErrorStruct); ok {
					if valErr.Field != tt.errField {
						t.Errorf("ValidationError.Field = %v, want %v", valErr.Field, tt.errField)
					}
				}
			}
		})
	}
}

// ============================================================================
// Error Type Tests
// ============================================================================

func TestAppError(t *testing.T) {
	// Test 1: Direct creation with cause
	baseErr := fmt.Errorf("base validation error")
	appErr := NewAppError(ErrorCodeValidation, "failed to validate").WithMetadata("source", "api_handler")
	if appErr.Cause != nil {
		t.Error("NewAppError should not set cause without passing it")
	}
	
	// Test 2: With explicit cause
	appErrWithCause := NewAppError(ErrorCodeValidation, "validation failed", baseErr)
	if appErrWithCause.Cause != baseErr {
		t.Errorf("NewAppError.Cause = %v, want %v", appErrWithCause.Cause, baseErr)
	}
	
	// Test 3: Wrap non-AppError
	wrapped := Wrap(baseErr, ErrorCodeTimeout, "timeout exceeded")
	if wrapped.Cause != baseErr {
		t.Errorf("Wrap().Cause = %v, want %v", wrapped.Cause, baseErr)
	}
	
	if wrapped.Code != ErrorCodeTimeout {
		t.Errorf("Wrap().Code = %v, want %v", wrapped.Code, ErrorCodeTimeout)
	}
}

func TestNotFound(t *testing.T) {
	err := NotFound("tenant", "tenant-123")
	
	if err.Code != ErrorCodeNotFound {
		t.Errorf("NotFound().Code = %v, want %v", err.Code, ErrorCodeNotFound)
	}
	
	if err.Metadata["resource"] != "tenant" {
		t.Errorf("NotFound().Metadata[\"resource\"] = %v, want \"tenant\"", err.Metadata["resource"])
	}
	
	if err.Metadata["identifier"] != "tenant-123" {
		t.Errorf("NotFound().Metadata[\"identifier\"] = %v, want \"tenant-123\"", err.Metadata["identifier"])
	}
}

func TestUnwrapAppError(t *testing.T) {
	// Direct AppError
	appErr := &AppError{Code: ErrorCodeInternal, Message: "test"}
	if extracted, ok := UnwrapAppError(appErr); !ok || extracted != appErr {
		t.Errorf("UnwrapAppError(direct) failed")
	}

	// Wrapped error
	wrapped := Wrap(fmt.Errorf("base error"), ErrorCodeTimeout, "timeout exceeded")
	if extracted, ok := UnwrapAppError(wrapped); !ok || extracted.Cause.Error() != "base error" {
		t.Errorf("UnwrapAppError(wrapped) failed")
	}

	// Non-error nil
	if _, ok := UnwrapAppError(nil); ok {
		t.Errorf("UnwrapAppError(nil) should return (nil, false)")
	}
}

// ============================================================================
// Utility Function Tests
// ============================================================================

func TestCoalesce(t *testing.T) {
	tests := []struct {
		name        string
		values      []float64
		defaultVal  float64
		expected    float64
		description string
	}{
		{"first non-zero", []float64{0, 0, 5.0}, 0.05, 5.0, "should pick first non-zero"},
		{"all zero, use default", []float64{0, 0, 0}, 0.05, 0.05, "fallback to default"},
		{"single value", []float64{10.0}, 0.05, 10.0, "single element slice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Coalesce(tt.values, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("Coalesce() = %v, want %v (%s)", result, tt.expected, tt.description)
			}
		})
	}
}

func TestSafeDeref(t *testing.T) {
	val := 42
	ptr := &val
	nilPtr := (*int)(nil)

	if SafeDeref(ptr) != 42 {
		t.Errorf("SafeDeref(valid ptr) = %v, want 42", SafeDeref(ptr))
	}

	if SafeDeref(nilPtr) != 0 {
		t.Errorf("SafeDeref(nil ptr) = %v, want 0 (zero value)", SafeDeref(nilPtr))
	}

	// Test with strings
	str := "hello"
	result := SafeDeref(&str)
	if result != "hello" {
		t.Errorf("SafeDeref(string ptr) = %v, want 'hello'", result)
	}
}

func TestFilterNonNil(t *testing.T) {
	input := []interface{}{1, nil, "hello", nil, 3.14}
	expected := []interface{}{1, "hello", 3.14}

	result := FilterNonNil(input)
	if len(result) != len(expected) {
		t.Errorf("FilterNonNil() length = %v, want %v", len(result), len(expected))
	}

	for i, v := range expected {
		if result[i] != v {
			t.Errorf("FilterNonNil()[%d] = %v, want %v", i, result[i], v)
		}
	}
}

// ============================================================================
// Duration and Time Tests
// ============================================================================

func TestCoalesceDuration(t *testing.T) {
	tests := []struct {
		name     string
		d1       time.Duration
		d2       time.Duration
		expected time.Duration
		desc     string
	}{
		{"d1 positive", time.Second * 5, time.Second, time.Second * 5, "prefer d1 when positive"},
		{"d1 zero, use d2", 0, time.Minute, time.Minute, "use d2 when d1 is zero"},
		{"both zero", 0, 0, 0, "return zero when both zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CoalesceDuration(tt.d1, tt.d2)
			if result != tt.expected {
				t.Errorf("CoalesceDuration() = %v, want %v (%s)", result, tt.expected, tt.desc)
			}
		})
	}
}

// Helper functions
func strPtr(s string) *string {
	return &s
}

func errMsgSub(str, substr string) bool {
	return len(str) >= len(substr) && (str == substr || contains(str, substr))
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s[:len(substr)] == substr || containsInMiddle(s, substr))
}

func containsInMiddle(s, substr string) bool {
	for i := 1; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
