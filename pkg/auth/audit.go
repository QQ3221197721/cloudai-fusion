// Package auth - audit.go provides comprehensive audit logging middleware.
// Records all API operations with structured audit events including user identity,
// action details, resource information, request/response metadata, and timing.
// Integrates with the security manager's audit log subsystem.
package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Audit Event Model
// ============================================================================

// AuditEvent represents a structured audit log entry.
type AuditEvent struct {
	ID            string                 `json:"id"`
	Timestamp     time.Time              `json:"timestamp"`
	TraceID       string                 `json:"trace_id,omitempty"`
	UserID        string                 `json:"user_id"`
	Username      string                 `json:"username"`
	Role          string                 `json:"role"`
	ClientIP      string                 `json:"client_ip"`
	UserAgent     string                 `json:"user_agent"`
	Method        string                 `json:"method"`
	Path          string                 `json:"path"`
	Query         string                 `json:"query,omitempty"`
	StatusCode    int                    `json:"status_code"`
	Latency       time.Duration          `json:"latency"`
	RequestSize   int                    `json:"request_size"`
	ResponseSize  int                    `json:"response_size"`
	Action        string                 `json:"action"`       // derived: create, read, update, delete
	ResourceType  string                 `json:"resource_type"` // derived from path
	ResourceID    string                 `json:"resource_id,omitempty"`
	Outcome       string                 `json:"outcome"` // success, failure, error
	ErrorMessage  string                 `json:"error_message,omitempty"`
	RequestBody   string                 `json:"request_body,omitempty"` // sanitised
	Tags          map[string]string      `json:"tags,omitempty"`
	Extra         map[string]interface{} `json:"extra,omitempty"`
}

// AuditLevel controls which events are recorded.
type AuditLevel string

const (
	AuditLevelNone     AuditLevel = "none"      // no auditing
	AuditLevelMetadata AuditLevel = "metadata"   // method, path, user, status
	AuditLevelRequest  AuditLevel = "request"    // + request body
	AuditLevelFull     AuditLevel = "full"        // + response body (expensive)
)

// ============================================================================
// Audit Sink
// ============================================================================

// AuditSink is a destination for audit events.
type AuditSink interface {
	Write(event *AuditEvent)
}

// LoggerAuditSink writes audit events via logrus.
type LoggerAuditSink struct {
	logger *logrus.Logger
}

// NewLoggerAuditSink creates a logrus-based audit sink.
func NewLoggerAuditSink(logger *logrus.Logger) *LoggerAuditSink {
	return &LoggerAuditSink{logger: logger}
}

// Write outputs the audit event as a structured log entry.
func (s *LoggerAuditSink) Write(event *AuditEvent) {
	fields := logrus.Fields{
		"audit_id":      event.ID,
		"user_id":       event.UserID,
		"username":      event.Username,
		"role":          event.Role,
		"client_ip":     event.ClientIP,
		"method":        event.Method,
		"path":          event.Path,
		"status":        event.StatusCode,
		"latency_ms":    event.Latency.Milliseconds(),
		"action":        event.Action,
		"resource_type": event.ResourceType,
		"outcome":       event.Outcome,
	}
	if event.ResourceID != "" {
		fields["resource_id"] = event.ResourceID
	}
	if event.ErrorMessage != "" {
		fields["error"] = event.ErrorMessage
	}
	if event.TraceID != "" {
		fields["trace_id"] = event.TraceID
	}

	s.logger.WithFields(fields).Info("AUDIT")
}

// ChannelAuditSink buffers audit events into a channel for async processing.
type ChannelAuditSink struct {
	ch chan *AuditEvent
}

// NewChannelAuditSink creates a channel-based audit sink.
func NewChannelAuditSink(bufSize int) *ChannelAuditSink {
	if bufSize <= 0 {
		bufSize = 4096
	}
	return &ChannelAuditSink{ch: make(chan *AuditEvent, bufSize)}
}

// Write sends the event to the channel (non-blocking, drops on overflow).
func (s *ChannelAuditSink) Write(event *AuditEvent) {
	select {
	case s.ch <- event:
	default:
		// Drop event on channel full (backpressure)
	}
}

// Events returns the event channel for consumers.
func (s *ChannelAuditSink) Events() <-chan *AuditEvent {
	return s.ch
}

// ============================================================================
// Audit Middleware
// ============================================================================

// AuditConfig configures the audit middleware.
type AuditConfig struct {
	Level            AuditLevel
	Sinks            []AuditSink
	Logger           *logrus.Logger
	SkipPaths        []string          // paths to skip (e.g., /healthz)
	SanitizeFields   []string          // fields to redact from request body
	MaxBodyCapture   int               // max bytes of request body to capture
	EnableRequestLog bool              // log request body at Request level
}

// bodyWriter wraps gin.ResponseWriter to capture response size.
type bodyWriter struct {
	gin.ResponseWriter
	size int
}

func (w *bodyWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

// AuditMiddleware returns a Gin middleware that records audit events for all requests.
func AuditMiddleware(cfg AuditConfig) gin.HandlerFunc {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.Level == AuditLevelNone {
		return func(c *gin.Context) { c.Next() }
	}
	if cfg.MaxBodyCapture <= 0 {
		cfg.MaxBodyCapture = 8192
	}
	if len(cfg.SanitizeFields) == 0 {
		cfg.SanitizeFields = []string{"password", "secret", "token", "api_key", "credit_card"}
	}

	skipSet := make(map[string]bool)
	for _, p := range cfg.SkipPaths {
		skipSet[p] = true
	}

	// Default sink: logger
	if len(cfg.Sinks) == 0 {
		cfg.Sinks = []AuditSink{NewLoggerAuditSink(cfg.Logger)}
	}

	return func(c *gin.Context) {
		// Skip excluded paths
		if skipSet[c.Request.URL.Path] {
			c.Next()
			return
		}

		start := time.Now()

		// Capture request body if needed
		var reqBody string
		if cfg.Level == AuditLevelRequest || cfg.Level == AuditLevelFull {
			if c.Request.Body != nil {
				body, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(cfg.MaxBodyCapture)))
				if err == nil {
					reqBody = sanitizeBody(string(body), cfg.SanitizeFields)
					c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
				}
			}
		}

		// Wrap response writer to capture size
		bw := &bodyWriter{ResponseWriter: c.Writer, size: 0}
		c.Writer = bw

		// Process request
		c.Next()

		// Build audit event
		event := &AuditEvent{
			ID:           generateAuditID(),
			Timestamp:    start.UTC(),
			UserID:       getStringFromCtx(c, "user_id"),
			Username:     getStringFromCtx(c, "username"),
			Role:         getStringFromCtx(c, "role"),
			ClientIP:     c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
			Method:       c.Request.Method,
			Path:         c.Request.URL.Path,
			Query:        c.Request.URL.RawQuery,
			StatusCode:   c.Writer.Status(),
			Latency:      time.Since(start),
			RequestSize:  int(c.Request.ContentLength),
			ResponseSize: bw.size,
			Action:       deriveAction(c.Request.Method),
			ResourceType: deriveResourceType(c.Request.URL.Path),
			ResourceID:   c.Param("id"),
		}

		// Set outcome
		switch {
		case event.StatusCode >= 200 && event.StatusCode < 300:
			event.Outcome = "success"
		case event.StatusCode >= 400 && event.StatusCode < 500:
			event.Outcome = "failure"
		default:
			event.Outcome = "error"
		}

		// Capture trace ID if present
		if traceID := c.GetHeader("X-Trace-ID"); traceID != "" {
			event.TraceID = traceID
		}

		// Include request body at appropriate level
		if cfg.EnableRequestLog && reqBody != "" {
			event.RequestBody = reqBody
		}

		// Dispatch to all sinks
		for _, sink := range cfg.Sinks {
			sink.Write(event)
		}
	}
}

// ============================================================================
// Audit Store (In-Memory Ring Buffer)
// ============================================================================

// AuditStore provides queryable in-memory storage for recent audit events.
type AuditStore struct {
	events   []*AuditEvent
	maxSize  int
	mu       sync.RWMutex
}

// NewAuditStore creates a new audit store with a fixed capacity.
func NewAuditStore(maxSize int) *AuditStore {
	if maxSize <= 0 {
		maxSize = 10000
	}
	return &AuditStore{
		events:  make([]*AuditEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

// Write implements AuditSink, storing events in the ring buffer.
func (s *AuditStore) Write(event *AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) >= s.maxSize {
		s.events = s.events[1:]
	}
	s.events = append(s.events, event)
}

// Query returns audit events matching the filter.
func (s *AuditStore) Query(filter AuditQuery) []*AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*AuditEvent
	for _, e := range s.events {
		if filter.UserID != "" && e.UserID != filter.UserID {
			continue
		}
		if filter.Action != "" && e.Action != filter.Action {
			continue
		}
		if filter.ResourceType != "" && e.ResourceType != filter.ResourceType {
			continue
		}
		if filter.Outcome != "" && e.Outcome != filter.Outcome {
			continue
		}
		if !filter.Since.IsZero() && e.Timestamp.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && e.Timestamp.After(filter.Until) {
			continue
		}
		result = append(result, e)
		if filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}
	return result
}

// Recent returns the most recent N events.
func (s *AuditStore) Recent(n int) []*AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.events)
	if n > total {
		n = total
	}
	result := make([]*AuditEvent, n)
	copy(result, s.events[total-n:])
	return result
}

// Count returns total stored events.
func (s *AuditStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// AuditQuery defines a filter for querying audit events.
type AuditQuery struct {
	UserID       string
	Action       string
	ResourceType string
	Outcome      string
	Since        time.Time
	Until        time.Time
	Limit        int
}

// ============================================================================
// Helpers
// ============================================================================

func generateAuditID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "audit-" + hex.EncodeToString(b)
}

func getStringFromCtx(c *gin.Context, key string) string {
	v, exists := c.Get(key)
	if !exists {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// deriveAction maps HTTP method to a CRUD action.
func deriveAction(method string) string {
	switch strings.ToUpper(method) {
	case "POST":
		return "create"
	case "GET", "HEAD":
		return "read"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return "other"
	}
}

// deriveResourceType extracts the resource type from the URL path.
// e.g., /api/v1/clusters/123 → "clusters"
func deriveResourceType(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Skip known prefixes: api, v1, v2, etc.
	for i, p := range parts {
		if p == "api" || strings.HasPrefix(p, "v") {
			continue
		}
		// First non-prefix segment is the resource type
		if i < len(parts) {
			return parts[i]
		}
	}
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

// sanitizeBody redacts sensitive fields from a JSON-like body string.
func sanitizeBody(body string, sensitiveFields []string) string {
	result := body
	for _, field := range sensitiveFields {
		// Simple pattern: "field":"value" → "field":"***"
		patterns := []string{
			fmt.Sprintf(`"%s":"`, field),
			fmt.Sprintf(`"%s": "`, field),
		}
		for _, p := range patterns {
			idx := strings.Index(strings.ToLower(result), strings.ToLower(p))
			if idx >= 0 {
				start := idx + len(p)
				end := strings.Index(result[start:], `"`)
				if end > 0 {
					result = result[:start] + "***" + result[start+end:]
				}
			}
		}
	}
	return result
}

// ============================================================================
// Audit API Handlers
// ============================================================================

// AuditHandlers provides HTTP handlers for querying audit logs.
type AuditHandlers struct {
	store *AuditStore
}

// NewAuditHandlers creates handlers backed by an AuditStore.
func NewAuditHandlers(store *AuditStore) *AuditHandlers {
	return &AuditHandlers{store: store}
}

// RecentHandler returns recent audit events.
func (h *AuditHandlers) RecentHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		events := h.store.Recent(100)
		c.JSON(200, gin.H{
			"audit_events": events,
			"total":        h.store.Count(),
		})
	}
}

// QueryHandler queries audit events with filters.
func (h *AuditHandlers) QueryHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		query := AuditQuery{
			UserID:       c.Query("user_id"),
			Action:       c.Query("action"),
			ResourceType: c.Query("resource_type"),
			Outcome:      c.Query("outcome"),
			Limit:        200,
		}
		events := h.store.Query(query)
		c.JSON(200, gin.H{
			"audit_events": events,
			"count":        len(events),
		})
	}
}
