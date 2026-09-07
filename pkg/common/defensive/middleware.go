// Package defensive provides middleware for standard defensive programming across API handlers.
package defensive

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// DefensiveMiddleware adds nil checks, input validation, and standardized error responses
func DefensiveMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Add request ID for tracing
		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = generateRequestID()
			c.Set("request_id", requestID)
		}
		
		// Add typed context with safety checks
		typeCtx := RequestContext{
			RequestID: requestID,
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
			UserID:    c.GetString("user_id"),
			IP:        c.ClientIP(),
		}
		c.Set("defensive_context", typeCtx)
		
		c.Next()
	}
}

// RequestValidator validates incoming requests with guard clauses
type RequestValidator struct {
	c *gin.Context
}

// ValidateBody ensures body is not empty when Content-Length > 0
func (v *RequestValidator) ValidateBody() error {
	contentLength := v.c.Request.ContentLength
	if contentLength <= 0 {
		// Empty body is acceptable for GET/HEAD/DELETE methods
		methods := []string{"GET", "HEAD", "OPTIONS", "DELETE"}
		for _, m := range methods {
			if v.c.Request.Method == m {
				return nil
			}
		}
		return ValidationError("body", "required but missing")
	}
	
	// Try to bind body
	var dummy map[string]interface{}
	err := v.c.ShouldBind(&dummy)
	if err != nil && err.Error() != "EOF" {
		return ValidationError("body", "invalid JSON format", err)
	}
	return nil
}

// ValidateParam checks if URL parameter exists and is non-empty
func (v *RequestValidator) ValidateParam(name string) error {
	value := v.c.Param(name)
	if value == "" {
		return ValidationError("param:"+name, "required but empty")
	}
	return nil
}

// ValidateQuery validates query parameters
func (v *RequestValidator) ValidateQuery(name string, required bool) error {
	value := v.c.Query(name)
	if !required {
		return nil
	}
	if value == "" {
		return ValidationError("query:"+name, "required but missing")
	}
	return nil
}

// ValidateHeader validates HTTP headers
func (v *RequestValidator) ValidateHeader(name string, required bool) error {
	value := v.c.GetHeader(name)
	if !required {
		return nil
	}
	if value == "" {
		return ValidationError("header:"+name, "required but missing")
	}
	return nil
}

// StandardErrorHandler handles errors uniformly with AppError types
func StandardErrorHandler(c *gin.Context, errs []error) {
	var appErr *AppError
	
	// Extract first error
	if len(errs) > 0 {
		appErr, _ = UnwrapAppError(errs[0])
	}
	
	// Default response for unknown errors
	if appErr == nil {
		appErr = &AppError{
			Code:    ErrorCodeInternal,
			Message: "internal server error",
		}
	}
	
	// Set appropriate status code
	statusCode := http.StatusInternalServerError
	switch appErr.Code {
	case ErrorCodeValidation:
		statusCode = http.StatusBadRequest
	case ErrorCodeNotFound:
		statusCode = http.StatusNotFound
	case ErrorCodeForbidden:
		statusCode = http.StatusForbidden
	case ErrorCodeUnauthorized:
		statusCode = http.StatusUnauthorized
	case ErrorCodeConflict:
		statusCode = http.StatusConflict
	case ErrorCodeRateLimitExceed:
		statusCode = http.StatusTooManyRequests
	case ErrorCodeTimeout:
		statusCode = http.StatusGatewayTimeout
	}
	
	// Log before sending response
	logrus.WithFields(logrus.Fields{
		"error_code":    appErr.Code,
		"error_message": appErr.Message,
		"path":          c.Request.URL.Path,
		"method":        c.Request.Method,
	}).Error(appErr.Message)
	
	if appErr.Metadata != nil {
		for k, v := range appErr.Metadata {
			logrus.WithField(k, v).Debug("Error metadata")
		}
	}
	
	// Send structured error response
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"code":    appErr.Code,
			"message": appErr.Message,
		},
	})
}
