package defensive

import (
	"fmt"
)

// ============================================================================
// Standardized Error Wrapping - Consistent error handling pattern
// ============================================================================

// AppError represents a standardized application error with code, message, and metadata.
type AppError struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Cause     error                  `json:"-"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// NewAppError creates a new standardized error with structured code and message.
func NewAppError(code string, message string, cause ...error) *AppError {
	apErr := &AppError{
		Code:      code,
		Message:   message,
		Metadata:  make(map[string]interface{}),
	}
	
	if len(cause) > 0 && cause[0] != nil {
		apErr.Cause = cause[0]
	}
	
	return apErr
}

// Error implements error interface.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s (%v)", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying cause for errors.Is/As compatibility.
func (e *AppError) Unwrap() error {
	return e.Cause
}

// WithMetadata adds additional context to the error.
func (e *AppError) WithMetadata(key string, value interface{}) *AppError {
	e.Metadata[key] = value
	return e
}

// Wrap wraps an error with an app-specific code and context.
func Wrap(err error, code string, message string) *AppError {
	if err == nil {
		return nil
	}
	
	if appErr, ok := err.(*AppError); ok {
		// Don't double-wrap if already an AppError
		return appErr
	}
	
	// Create new AppError with the original error as cause
	return &AppError{
		Code:    code,
		Message: message,
		Cause:   err,
	}
}

// Common error codes
const (
	ErrorCodeValidation       = "VALIDATION_ERROR"
	ErrorCodeNotFound         = "NOT_FOUND"
	ErrorCodeForbidden        = "FORBIDDEN"
	ErrorCodeUnauthorized     = "UNAUTHORIZED"
	ErrorCodeConflict         = "CONFLICT"
	ErrorCodeInternal         = "INTERNAL_ERROR"
	ErrorCodeRateLimitExceed  = "RATE_LIMIT_EXCEEDED"
	ErrorCodeTimeout          = "TIMEOUT"
	ErrorCodeResourceExhausted = "RESOURCE_EXHAUSTED"
)

// Helper functions for creating common AppError instances
func ValidationError(field string, message string, cause ...error) *AppError {
	err := NewAppError(ErrorCodeValidation, message, cause...)
	if field != "" {
		err.Metadata["field"] = field
	}
	return err
}

// NotFound error
func NotFound(resource string, identifier string) *AppError {
	return NewAppError(ErrorCodeNotFound, fmt.Sprintf("%s not found", resource)).
		WithMetadata("resource", resource).
		WithMetadata("identifier", identifier)
}

// Conflict error
func Conflict(message string, cause ...error) *AppError {
	return NewAppError(ErrorCodeConflict, message, cause...)
}

// Forbidden error
func Forbidden(message string) *AppError {
	return NewAppError(ErrorCodeForbidden, message)
}

// Unauthorized error
func Unauthorized(message string) *AppError {
	return NewAppError(ErrorCodeUnauthorized, message)
}
