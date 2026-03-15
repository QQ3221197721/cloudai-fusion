package metrics

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// Gin HTTP Middleware — automatic golden signal recording
// ============================================================================

// GinMiddlewareConfig configures the metrics middleware.
type GinMiddlewareConfig struct {
	// ServiceName is used for SLI tracking.
	ServiceName string

	// SLOTracker, if set, records each request for SLO evaluation.
	SLOTracker *SLOTracker

	// SkipPaths are URL paths that should not be recorded (e.g. /metrics, /healthz).
	SkipPaths map[string]bool

	// GroupPathFunc normalizes URL paths to avoid high cardinality.
	// If nil, gin.FullPath() is used.
	GroupPathFunc func(c *gin.Context) string
}

// GinMiddleware returns a Gin middleware that records the Four Golden Signals
// (latency, traffic, errors, saturation) for every HTTP request.
func GinMiddleware(cfg GinMiddlewareConfig) gin.HandlerFunc {
	if cfg.SkipPaths == nil {
		cfg.SkipPaths = map[string]bool{
			"/metrics": true,
			"/healthz": true,
			"/readyz":  true,
			"/livez":   true,
		}
	}

	return func(c *gin.Context) {
		// Skip metrics for health/metrics endpoints
		if cfg.SkipPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		start := time.Now()
		HTTPRequestsInFlight.Inc()

		c.Next()

		HTTPRequestsInFlight.Dec()
		duration := time.Since(start)

		// Determine path label (avoid high cardinality)
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		if cfg.GroupPathFunc != nil {
			path = cfg.GroupPathFunc(c)
		}

		method := c.Request.Method
		statusCode := fmt.Sprintf("%d", c.Writer.Status())

		// Golden Signal: Traffic
		HTTPRequestsTotal.WithLabelValues(method, path, statusCode).Inc()

		// Golden Signal: Latency
		HTTPRequestDuration.WithLabelValues(method, path, statusCode).Observe(duration.Seconds())

		// Golden Signal: Errors
		status := c.Writer.Status()
		if status >= 400 {
			errType := "client_error"
			if status >= 500 {
				errType = "server_error"
			}
			HTTPErrorsTotal.WithLabelValues(method, path, statusCode, errType).Inc()
		}

		// Golden Signal: Saturation (response size)
		HTTPResponseSizeBytes.WithLabelValues(method, path).Observe(float64(c.Writer.Size()))

		// SLO tracking
		if cfg.SLOTracker != nil && cfg.ServiceName != "" {
			isError := status >= 500
			cfg.SLOTracker.RecordRequest(cfg.ServiceName, duration, isError)
		}
	}
}

// RecoveryMiddleware wraps gin.Recovery to count panics.
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				PanicsRecoveredTotal.Inc()
				// Re-panic so that gin's default recovery can handle it
				panic(err)
			}
		}()
		c.Next()
	}
}
