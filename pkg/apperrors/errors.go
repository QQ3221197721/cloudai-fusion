// Package apperrors provides a comprehensive error handling system for the CloudAI Fusion platform
// This package centralizes all application-specific error definitions and helper functions
package apperrors

import (
	"errors"
	"fmt"
	"net/http"
)

// Common application errors - these are sentinel errors used throughout the codebase
var (
	// General purpose errors
	ErrNotFound          = errors.New("resource not found")
	ErrUnauthorized      = errors.New("unauthorized access")
	ErrForbidden         = errors.New("forbidden access")
	ErrInvalidInput      = errors.New("invalid input")
	ErrConflict          = errors.New("conflict - resource already exists")
	ErrAlreadyExists     = errors.New("resource already exists")
	ErrNotImplemented    = errors.New("feature not implemented")
	ErrInternalServer    = errors.New("internal server error")
	ErrServiceUnavailable = errors.New("service unavailable")
	ErrRateLimitReached  = errors.New("rate limit exceeded")

	// Edge autonomy specific errors
	ErrNodeNotFound           = errors.New("edge node not found")
	ErrNodeUnreachable        = errors.New("edge node unreachable")
	ErrSyncFailed             = errors.New("synchronization failed")
	ErrConflictResolutionFail = errors.New("conflict resolution failed")
	ErrVersionVectorCorrupt   = errors.New("version vector data corrupted")

	// GitOps specific errors
	ErrManifestNotFound       = errors.New("deployment manifest not found")
	ErrManifestInvalid        = errors.New("deployment manifest invalid")
	ErrDriftDetected          = errors.New("configuration drift detected")
	ErrDeploymentFailed       = errors.New("deployment failed")
	ErrRollbackFailed         = errors.New("roll back failed")
	ErrValidationFailed       = errors.New("validation failed")

	// RedTeam security specific errors
	ErrExploitNotFound    = errors.New("exploit definition not found")
	ErrExploitFailed      = errors.New("exploit execution failed")
	ErrIsolationFailed    = errors.New("sandbox isolation failed")
	ErrEvidenceNotRecorded = errors.New("evidence recording failed")
	ErrSecurityPolicyViolated = errors.New("security policy violated")

	// Data layer specific errors
	ErrDataNotFound          = errors.New("data record not found")
	ErrDataInvalid           = errors.New("data validation failed")
	ErrDataDuplication       = errors.New("duplicate data detected")
	ErrDataIntegrity         = errors.New("data integrity check failed")

	// Cache specific errors
	ErrCacheMiss        = errors.New("cache miss")
	ErrCacheSetFailed   = errors.New("cache set failed")
	ErrCacheGetFailed   = errors.New("cache get failed")
	ErrCacheCorrupted   = errors.New("cache data corrupted")
)

// HTTP status codes mapped from error types
func ErrorCode(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch err {
	case ErrNotFound:
		return http.StatusNotFound
	case ErrUnauthorized:
		return http.StatusUnauthorized
	case ErrForbidden:
		return http.StatusForbidden
	case ErrInvalidInput:
		return http.StatusBadRequest
	case ErrConflict, ErrAlreadyExists:
		return http.StatusConflict
	case ErrNotImplemented:
		return http.StatusNotImplemented
	case ErrInternalServer:
		return http.StatusInternalServerError
	case ErrServiceUnavailable:
		return http.StatusServiceUnavailable
	case ErrRateLimitReached:
		return http.StatusTooManyRequests
	default:
		// Check if error wraps one of the known errors
		var wrapped *WrappedError
		if errors.As(err, &wrapped) {
			return wrapped.StatusCode
		}
		return http.StatusInternalServerError
	}
}

// Message returns human-readable error message
func Message(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	// Add context if wrapped
	var wrapped *WrappedError
	if errors.As(err, &wrapped) {
		if wrapped.Context != "" {
			msg += fmt.Sprintf(" (context: %s)", wrapped.Context)
		}
	}

	return msg
}

// IsNotFound checks if error is ErrNotFound or wraps it
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrNotFound)
}

// IsUnauthorized checks if error is ErrUnauthorized or wraps it
func IsUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrUnauthorized)
}

// IsForbidden checks if error is ErrForbidden or wraps it
func IsForbidden(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrForbidden)
}

// IsInvalidInput checks if error is ErrInvalidInput or wraps it
func IsInvalidInput(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrInvalidInput)
}

// IsAlreadyExists checks if error is ErrAlreadyExists or wraps it
func IsAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrAlreadyExists) || errors.Is(err, ErrConflict)
}

// WrappedError wraps an error with additional context and HTTP status code
type WrappedError struct {
	Err        error
	StatusCode int
	Context    string
}

func (w *WrappedError) Error() string {
	if w.Context != "" {
		return fmt.Sprintf("%s: %v", w.Context, w.Err)
	}
	return w.Err.Error()
}

func (w *WrappedError) Unwrap() error {
	return w.Err
}

// Wrap wraps an error with context and optional HTTP status code
func Wrap(err error, status int) error {
	if err == nil {
		return nil
	}
	return &WrappedError{
		Err:        err,
		StatusCode: status,
		Context:    "",
	}
}

// WrapWithMessage wraps an error with context message and HTTP status code
func WrapWithMessage(err error, msg string, status int) error {
	if err == nil {
		return nil
	}
	return &WrappedError{
		Err:        err,
		StatusCode: status,
		Context:    msg,
	}
}

// Must panics if err is not nil
func Must(err error) {
	if err != nil {
		panic(err)
	}
}

// Validate ensures error is valid before use
func Validate(err error) bool {
	return err != nil && err.Error() != ""
}

// Aggregate aggregates multiple errors into one
func Aggregate(errs ...error) error {
	validErrs := make([]error, 0)
	for _, err := range errs {
		if err != nil {
			validErrs = append(validErrs, err)
		}
	}

	if len(validErrs) == 0 {
		return nil
	}

	if len(validErrs) == 1 {
		return validErrs[0]
	}

	return &aggregateError{errs: validErrs}
}

type aggregateError struct {
	errs []error
}

func (ae *aggregateError) Error() string {
	msg := "multiple errors: "
	for i, err := range ae.errs {
		if i > 0 {
			msg += "; "
		}
		msg += err.Error()
	}
	return msg
}

func (ae *aggregateError) Unwrap() error {
	return ae.errs[0]
}
