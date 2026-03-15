package errors

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Unified API Error Response
// ============================================================================

// APIErrorResponse is the standard JSON error body returned by all API endpoints.
// Every error response from the platform follows this exact structure.
//
//	{
//	  "error": {
//	    "code": "NOT_FOUND",
//	    "message": "cluster 'abc-123' not found",
//	    "details": {"resource_type": "cluster", "resource_id": "abc-123"},
//	    "request_id": "req-uuid-here"
//	  }
//	}
type APIErrorResponse struct {
	Error APIErrorBody `json:"error"`
}

// APIErrorBody is the inner error object.
type APIErrorBody struct {
	Code      ErrorCode              `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
}

// ============================================================================
// Gin Response Helpers
// ============================================================================

// RespondError writes a unified error response to a Gin context.
// It automatically extracts the HTTP status and error code from an *AppError.
// For non-AppError errors, it defaults to a 500 Internal Server Error.
func RespondError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	requestID, _ := c.Get("request_id")
	reqIDStr, _ := requestID.(string)

	var appErr *AppError
	if errors.As(err, &appErr) {
		c.JSON(appErr.HTTPStatus, APIErrorResponse{
			Error: APIErrorBody{
				Code:      appErr.Code,
				Message:   appErr.Message,
				Details:   appErr.Details,
				RequestID: reqIDStr,
			},
		})
		return
	}

	// Fallback for non-AppError errors — never leak internal error messages
	c.JSON(http.StatusInternalServerError, APIErrorResponse{
		Error: APIErrorBody{
			Code:      CodeInternal,
			Message:   "an unexpected error occurred",
			RequestID: reqIDStr,
		},
	})
}

// RespondValidationError writes a 400 error for request binding failures.
func RespondValidationError(c *gin.Context, err error) {
	requestID, _ := c.Get("request_id")
	reqIDStr, _ := requestID.(string)

	c.JSON(http.StatusBadRequest, APIErrorResponse{
		Error: APIErrorBody{
			Code:      CodeValidation,
			Message:   err.Error(),
			RequestID: reqIDStr,
		},
	})
}

// RespondSuccess writes a standard success response.
func RespondSuccess(c *gin.Context, status int, data interface{}) {
	c.JSON(status, data)
}

// RespondList writes a standard paginated list response.
func RespondList(c *gin.Context, items interface{}, total int) {
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": total,
	})
}

// RespondMessage writes a simple message response.
func RespondMessage(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"message":    message,
		"request_id": common.NewUUID(),
	})
}

// ============================================================================
// Error Recovery Middleware
// ============================================================================

// RecoveryMiddleware returns a Gin middleware that catches panics and
// returns a structured 500 error response instead of crashing.
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				requestID, _ := c.Get("request_id")
				reqIDStr, _ := requestID.(string)

				c.AbortWithStatusJSON(http.StatusInternalServerError, APIErrorResponse{
					Error: APIErrorBody{
						Code:      CodeInternal,
						Message:   "an unexpected internal error occurred",
						RequestID: reqIDStr,
					},
				})
			}
		}()
		c.Next()
	}
}
