// Package errors provides a structured error handling system for CloudAI Fusion.
// It defines application-level error types, error codes, and helper constructors
// following industry-standard API error practices.
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// ============================================================================
// Error Codes — Stable, documented, never reused
// ============================================================================

// ErrorCode is a machine-readable error identifier for API consumers.
type ErrorCode string

const (
	// General (10xxx)
	CodeInternal         ErrorCode = "INTERNAL_ERROR"
	CodeUnknown          ErrorCode = "UNKNOWN_ERROR"
	CodeValidation       ErrorCode = "VALIDATION_ERROR"
	CodeBadRequest       ErrorCode = "BAD_REQUEST"
	CodeNotImplemented   ErrorCode = "NOT_IMPLEMENTED"

	// Auth (11xxx)
	CodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	CodeForbidden        ErrorCode = "FORBIDDEN"
	CodePermissionDenied ErrorCode = "PERMISSION_DENIED"
	CodeTokenExpired     ErrorCode = "TOKEN_EXPIRED"
	CodeTokenInvalid     ErrorCode = "TOKEN_INVALID"
	CodeAccountDisabled  ErrorCode = "ACCOUNT_DISABLED"

	// Resource (12xxx)
	CodeNotFound         ErrorCode = "NOT_FOUND"
	CodeAlreadyExists    ErrorCode = "ALREADY_EXISTS"
	CodeConflict         ErrorCode = "CONFLICT"
	CodeGone             ErrorCode = "GONE"

	// State (13xxx)
	CodeInvalidState     ErrorCode = "INVALID_STATE_TRANSITION"
	CodePreconditionFail ErrorCode = "PRECONDITION_FAILED"

	// Rate/Quota (14xxx)
	CodeRateLimited      ErrorCode = "RATE_LIMITED"
	CodeQuotaExceeded    ErrorCode = "QUOTA_EXCEEDED"

	// Infra (15xxx)
	CodeServiceUnavail   ErrorCode = "SERVICE_UNAVAILABLE"
	CodeTimeout          ErrorCode = "TIMEOUT"
	CodeDatabaseError    ErrorCode = "DATABASE_ERROR"
	CodeUpstreamError    ErrorCode = "UPSTREAM_ERROR"
	CodeCircuitOpen      ErrorCode = "CIRCUIT_OPEN"
)

// ============================================================================
// AppError — The core application error type
// ============================================================================

// AppError is a rich error type that carries an error code, HTTP status,
// human-readable message, and optional structured details.
// It implements the error interface and supports errors.Is/As unwrapping.
type AppError struct {
	Code       ErrorCode              `json:"code"`
	Message    string                 `json:"message"`
	HTTPStatus int                    `json:"-"`
	Details    map[string]interface{} `json:"details,omitempty"`
	Cause      error                  `json:"-"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause for errors.Is/As chain walking.
func (e *AppError) Unwrap() error {
	return e.Cause
}

// WithDetails attaches structured metadata to the error.
func (e *AppError) WithDetails(details map[string]interface{}) *AppError {
	e.Details = details
	return e
}

// WithDetail attaches a single key-value detail.
func (e *AppError) WithDetail(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithCause wraps an underlying error.
func (e *AppError) WithCause(cause error) *AppError {
	e.Cause = cause
	return e
}

// ============================================================================
// Sentinel Errors — Comparable with errors.Is()
// ============================================================================

var (
	// ErrNotFound indicates the requested resource was not found.
	ErrNotFound = &AppError{Code: CodeNotFound, Message: "resource not found", HTTPStatus: http.StatusNotFound}

	// ErrAlreadyExists indicates a resource with the same identity already exists.
	ErrAlreadyExists = &AppError{Code: CodeAlreadyExists, Message: "resource already exists", HTTPStatus: http.StatusConflict}

	// ErrConflict indicates a state conflict (e.g., concurrent modification).
	ErrConflict = &AppError{Code: CodeConflict, Message: "conflict", HTTPStatus: http.StatusConflict}

	// ErrUnauthorized indicates missing or invalid authentication credentials.
	ErrUnauthorized = &AppError{Code: CodeUnauthorized, Message: "unauthorized", HTTPStatus: http.StatusUnauthorized}

	// ErrPermissionDenied indicates the caller lacks required permissions.
	ErrPermissionDenied = &AppError{Code: CodePermissionDenied, Message: "permission denied", HTTPStatus: http.StatusForbidden}

	// ErrForbidden indicates the action is forbidden regardless of permissions.
	ErrForbidden = &AppError{Code: CodeForbidden, Message: "forbidden", HTTPStatus: http.StatusForbidden}

	// ErrValidation indicates request validation failure.
	ErrValidation = &AppError{Code: CodeValidation, Message: "validation error", HTTPStatus: http.StatusBadRequest}

	// ErrBadRequest indicates a malformed request.
	ErrBadRequest = &AppError{Code: CodeBadRequest, Message: "bad request", HTTPStatus: http.StatusBadRequest}

	// ErrInternal indicates an unexpected internal server error.
	ErrInternal = &AppError{Code: CodeInternal, Message: "internal server error", HTTPStatus: http.StatusInternalServerError}

	// ErrServiceUnavailable indicates a downstream service is temporarily unavailable.
	ErrServiceUnavailable = &AppError{Code: CodeServiceUnavail, Message: "service unavailable", HTTPStatus: http.StatusServiceUnavailable}

	// ErrTimeout indicates an operation timed out.
	ErrTimeout = &AppError{Code: CodeTimeout, Message: "operation timed out", HTTPStatus: http.StatusGatewayTimeout}

	// ErrRateLimited indicates the caller has exceeded the rate limit.
	ErrRateLimited = &AppError{Code: CodeRateLimited, Message: "rate limit exceeded", HTTPStatus: http.StatusTooManyRequests}

	// ErrInvalidState indicates an invalid state transition was attempted.
	ErrInvalidState = &AppError{Code: CodeInvalidState, Message: "invalid state transition", HTTPStatus: http.StatusConflict}

	// ErrDatabaseError indicates a database-level failure.
	ErrDatabaseError = &AppError{Code: CodeDatabaseError, Message: "database error", HTTPStatus: http.StatusInternalServerError}

	// ErrCircuitOpen indicates the circuit breaker is open.
	ErrCircuitOpen = &AppError{Code: CodeCircuitOpen, Message: "circuit breaker is open, service temporarily unavailable", HTTPStatus: http.StatusServiceUnavailable}

	// ErrTokenExpired indicates the JWT token has expired.
	ErrTokenExpired = &AppError{Code: CodeTokenExpired, Message: "token expired", HTTPStatus: http.StatusUnauthorized}

	// ErrAccountDisabled indicates the user account is disabled.
	ErrAccountDisabled = &AppError{Code: CodeAccountDisabled, Message: "account is disabled", HTTPStatus: http.StatusForbidden}
)

// Is implements sentinel error comparison.
// Two AppErrors are equal if their Code matches.
func (e *AppError) Is(target error) bool {
	var t *AppError
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// ============================================================================
// Constructors — Convenient error factories
// ============================================================================

// New creates a generic AppError with the given code, status, and message.
func New(code ErrorCode, httpStatus int, message string) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

// Newf creates a generic AppError with a formatted message.
func Newf(code ErrorCode, httpStatus int, format string, args ...interface{}) *AppError {
	return &AppError{
		Code:       code,
		Message:    fmt.Sprintf(format, args...),
		HTTPStatus: httpStatus,
	}
}

// NotFound creates a 404 error for a specific resource type and identifier.
func NotFound(resourceType, id string) *AppError {
	return &AppError{
		Code:       CodeNotFound,
		Message:    fmt.Sprintf("%s '%s' not found", resourceType, id),
		HTTPStatus: http.StatusNotFound,
		Details:    map[string]interface{}{"resource_type": resourceType, "resource_id": id},
	}
}

// AlreadyExists creates a 409 error for a duplicate resource.
func AlreadyExists(resourceType, id string) *AppError {
	return &AppError{
		Code:       CodeAlreadyExists,
		Message:    fmt.Sprintf("%s '%s' already exists", resourceType, id),
		HTTPStatus: http.StatusConflict,
		Details:    map[string]interface{}{"resource_type": resourceType, "resource_id": id},
	}
}

// Conflict creates a 409 error with a specific message.
func Conflict(message string) *AppError {
	return &AppError{
		Code:       CodeConflict,
		Message:    message,
		HTTPStatus: http.StatusConflict,
	}
}

// InvalidStateTransition creates an error for illegal state machine transitions.
func InvalidStateTransition(resource, from, to string) *AppError {
	return &AppError{
		Code:       CodeInvalidState,
		Message:    fmt.Sprintf("invalid state transition for %s: %s → %s", resource, from, to),
		HTTPStatus: http.StatusConflict,
		Details:    map[string]interface{}{"from": from, "to": to, "resource": resource},
	}
}

// Validation creates a 400 error with field-level details.
func Validation(message string, fields map[string]string) *AppError {
	details := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		details[k] = v
	}
	return &AppError{
		Code:       CodeValidation,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
		Details:    details,
	}
}

// Unauthorized creates a 401 error.
func Unauthorized(message string) *AppError {
	return &AppError{
		Code:       CodeUnauthorized,
		Message:    message,
		HTTPStatus: http.StatusUnauthorized,
	}
}

// PermissionDenied creates a 403 error with the required permission.
func PermissionDenied(permission string) *AppError {
	return &AppError{
		Code:       CodePermissionDenied,
		Message:    fmt.Sprintf("permission denied: requires '%s'", permission),
		HTTPStatus: http.StatusForbidden,
		Details:    map[string]interface{}{"required_permission": permission},
	}
}

// Internal creates a 500 error, wrapping the underlying cause.
func Internal(message string, cause error) *AppError {
	return &AppError{
		Code:       CodeInternal,
		Message:    message,
		HTTPStatus: http.StatusInternalServerError,
		Cause:      cause,
	}
}

// Database creates a database error wrapping the cause.
func Database(operation string, cause error) *AppError {
	return &AppError{
		Code:       CodeDatabaseError,
		Message:    fmt.Sprintf("database error during %s", operation),
		HTTPStatus: http.StatusInternalServerError,
		Cause:      cause,
		Details:    map[string]interface{}{"operation": operation},
	}
}

// ServiceUnavailable creates a 503 error for a named downstream service.
func ServiceUnavailable(service string) *AppError {
	return &AppError{
		Code:       CodeServiceUnavail,
		Message:    fmt.Sprintf("service '%s' is temporarily unavailable", service),
		HTTPStatus: http.StatusServiceUnavailable,
		Details:    map[string]interface{}{"service": service},
	}
}

// Timeout creates a 504 error for an operation that took too long.
func Timeout(operation string) *AppError {
	return &AppError{
		Code:       CodeTimeout,
		Message:    fmt.Sprintf("operation '%s' timed out", operation),
		HTTPStatus: http.StatusGatewayTimeout,
		Details:    map[string]interface{}{"operation": operation},
	}
}

// Upstream creates a 502 error wrapping an upstream service failure.
func Upstream(service string, cause error) *AppError {
	return &AppError{
		Code:       CodeUpstreamError,
		Message:    fmt.Sprintf("upstream service '%s' returned an error", service),
		HTTPStatus: http.StatusBadGateway,
		Cause:      cause,
		Details:    map[string]interface{}{"upstream": service},
	}
}

// Wrap converts any error to an *AppError. If the error is already an
// *AppError, it is returned as-is. Otherwise, it is wrapped as an internal error.
func Wrap(err error) *AppError {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal("unexpected error", err)
}

// IsCode checks if an error has a specific ErrorCode.
func IsCode(err error, code ErrorCode) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == code
	}
	return false
}

// HTTPStatusFromError extracts the HTTP status code from an error.
// Returns 500 for non-AppError errors.
func HTTPStatusFromError(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}
