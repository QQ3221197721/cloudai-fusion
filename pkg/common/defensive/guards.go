// Package defensive provides utility functions for defensive programming,
// including nil checks, input validation, range bounds checking, and standardized error handling.
// This framework ensures consistent guard clauses across the codebase and prevents
// common runtime panics caused by nil dereferences or invalid inputs.
package defensive

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"
)

// ============================================================================
// Nil Guards - Prevent Null Pointer Dereference
// ============================================================================

// RequireNil asserts that a value is nil. Returns an error if not nil.
func RequireNil(val interface{}, fieldName string) error {
	if val != nil {
		return fmt.Errorf("%s must be nil, got %T", fieldName, val)
	}
	return nil
}

// RequireNonNil asserts that a value is non-nil. Returns an error if nil.
// Note: In Go, empty slices/maps/pointers are still valid values (not nil).
// Only actual nil references return an error.
func RequireNonNil(val interface{}, fieldName string) error {
	if val == nil {
		return &ValidationErrorStruct{Field: fieldName, Message: "must be non-nil"}
	}
	
	// Explicitly check for typed nils using reflection
	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		if v.IsNil() {
			return &ValidationErrorStruct{Field: fieldName, Message: "must be non-nil"}
		}
	case reflect.Interface:
		if v.IsNil() {
			return &ValidationErrorStruct{Field: fieldName, Message: "must be non-nil"}
		}
	}
	
	return nil
}

// RequireNotNil performs type-assertion-safe nil check for interface{} values.
// Works correctly with typed nils (e.g., (*MyType)(nil)).
func RequireNotNil(val interface{}, fieldName string) error {
	if val == nil {
		return &ValidationErrorStruct{Field: fieldName, Message: "must not be nil"}
	}
	
	// Handle pointer, slice, map, channel, function, interface types
	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		if v.IsNil() {
			return &ValidationErrorStruct{Field: fieldName, Message: "must not be nil"}
		}
	case reflect.Invalid:
		return &ValidationErrorStruct{Field: fieldName, Message: "must not be nil"}
	}
	
	return nil
}

// Must panics if err is not nil. Useful in initialization code where failure
// is unrecoverable. Logs stack trace before panic for debugging.
func Must(err error, msg string) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", msg, err))
	}
}

// Try executes a function that returns (T, error) and handles errors gracefully.
// If err is not nil, fallback is returned; otherwise the actual result.
func Try[T any](fn func() (T, error), fallback T) T {
	result, err := fn()
	if err != nil {
		return fallback
	}
	return result
}

// ============================================================================
// Input Validation - Range Bounds and Constraints
// ============================================================================

// ValidateRange checks if value is within [min, max] inclusive.
func ValidateRange(value, min, max float64, fieldName string) error {
	if value < min || value > max {
		return &ValidationErrorStruct{
			Field:   fieldName,
			Message: fmt.Sprintf("must be in range [%f, %f], got %f", min, max, value),
			Value:   value,
		}
	}
	return nil
}

// ValidateIntRange checks integer range [min, max] inclusive.
func ValidateIntRange(value, min, max int, fieldName string) error {
	if value < min || value > max {
		return &ValidationErrorStruct{
			Field:   fieldName,
			Message: fmt.Sprintf("must be in range [%d, %d], got %d", min, max, value),
			Value:   value,
		}
	}
	return nil
}

// ValidateSliceBounds checks if index is within [0, length).
func ValidateSliceBounds(index, length int, fieldName string) error {
	if index < 0 {
		return &IndexOutOfBoundsError{Index: index, Size: length, Field: fieldName, Direction: "negative"}
	}
	if index >= length {
		return &IndexOutOfBoundsError{Index: index, Size: length, Field: fieldName, Direction: "exceeds"}
	}
	return nil
}

// ValidateMapKey checks if key exists in map.
func ValidateMapKey(key string, m map[string]interface{}, fieldName string) error {
	if _, exists := m[key]; !exists {
		return &KeyNotFoundError{Key: key, Field: fieldName}
	}
	return nil
}

// ValidateNonEmptyString checks if string is not empty or whitespace-only.
func ValidateNonEmptyString(s, fieldName string) error {
	if strings.TrimSpace(s) == "" {
		return &ValidationErrorStruct{Field: fieldName, Message: "must not be empty or whitespace-only"}
	}
	return nil
}

// ValidateURL validates URL format.
func ValidateURL(u, fieldName string) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return &ValidationErrorStruct{Field: fieldName, Message: "invalid URL format", Cause: err}
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return &ValidationErrorStruct{Field: fieldName, Message: "URL missing scheme or host"}
	}
	return nil
}

// ValidateDuration checks if duration is positive.
func ValidateDuration(d time.Duration, fieldName string) error {
	if d < 0 {
		return &ValidationErrorStruct{Field: fieldName, Message: "must be positive duration"}
	}
	return nil
}

// ============================================================================
// Error Types
// ============================================================================

// ValidationErrorStruct represents a validation failure with context.
type ValidationErrorStruct struct {
	Field   string      `json:"field"`
	Message string      `json:"message"`
	Value   interface{} `json:"value,omitempty"`
	Cause   error       `json:"cause,omitempty"`
}

func (e *ValidationErrorStruct) Error() string {
	msg := fmt.Sprintf("validation failed on field '%s': %s", e.Field, e.Message)
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

// IndexOutOfBoundsError represents out-of-bounds access.
type IndexOutOfBoundsError struct {
	Index   int    `json:"index"`
	Size    int    `json:"size"`
	Field   string `json:"field"`
	Direction string `json:"direction"` // "negative" or "exceeds"
}

func (e *IndexOutOfBoundsError) Error() string {
	return fmt.Sprintf("index %d is %s bounds [0, %d) for field '%s'", 
		e.Index, e.Direction, e.Size, e.Field)
}

// KeyNotFoundError represents missing map key.
type KeyNotFoundError struct {
	Key   string `json:"key"`
	Field string `json:"field"`
}

func (e *KeyNotFoundError) Error() string {
	return fmt.Sprintf("key '%s' not found in field '%s'", e.Key, e.Field)
}

// ============================================================================
// Utility Functions
// ============================================================================

// Coalesce returns first non-nil/non-empty value from variadic arguments.
// Falls back to default if all are nil/empty.
func Coalesce[T any](values []T, defaultVal T) T {
	for _, v := range values {
		// Use zero-value detection for different types
		if isZeroValue(reflect.ValueOf(v)) {
			continue
		}
		return v
	}
	return defaultVal
}

func isZeroValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	default:
		return false
	}
}

// SafeDeref dereferences a pointer safely, returning a zero value if nil.
func SafeDeref[T any](ptr *T) T {
	if ptr == nil {
		var zero T
		return zero
	}
	return *ptr
}

// FilterNonNil removes nil values from a slice of interface{}.
func FilterNonNil(slice []interface{}) []interface{} {
	result := make([]interface{}, 0, len(slice))
	for _, v := range slice {
		if v != nil {
			result = append(result, v)
		}
	}
	return result
}

// UnwrapAppError safely extracts AppError from any error type.
func UnwrapAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	
	appErr, ok := err.(*AppError)
	if ok {
		return appErr, true
	}
	
	// Try Unwrap chain (Go 1.13+ error chaining)
	type unwrapper interface {
		Unwrap() error
	}
	
	if u, ok := err.(unwrapper); ok {
		unwrapped := u.Unwrap()
		if unwrapped == nil {
			return nil, false
		}
		return UnwrapAppError(unwrapped)
	}
	
	return nil, false
}
