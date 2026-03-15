package errors

import (
	"context"
)

// CheckContext returns a Timeout/Cancelled AppError if the context is done.
// Use at method entry points and before expensive operations (DB, K8s API, HTTP)
// to short-circuit when the caller has already cancelled or timed out.
//
//	if err := apperrors.CheckContext(ctx); err != nil {
//	    return nil, err
//	}
func CheckContext(ctx context.Context) *AppError {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		if err == context.DeadlineExceeded {
			return Timeout("request deadline exceeded").WithCause(err)
		}
		// context.Canceled
		return &AppError{
			Code:       CodeTimeout,
			Message:    "request cancelled",
			HTTPStatus: 499, // nginx-style "Client Closed Request"
			Cause:      err,
		}
	}
	return nil
}

// ContextTimeout is the default timeout applied to database and external
// service calls when the caller's context has no deadline.
const (
	// DBTimeout is the default timeout for database operations.
	DBTimeout = 5 // seconds

	// ExternalCallTimeout is the default timeout for external API calls
	// (cloud SDK, K8s API, edge HTTP).
	ExternalCallTimeout = 30 // seconds
)
