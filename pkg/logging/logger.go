// Package logging provides structured logging with trace/request ID correlation,
// dynamic log level adjustment, log sampling, and multi-sink output
// for CloudAI Fusion services. It wraps logrus to provide JSON-formatted output
// with automatic field injection from context (trace_id, request_id, user_id).
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Context keys for trace correlation fields.
type contextKey string

const (
	keyTraceID   contextKey = "trace_id"
	keySpanID    contextKey = "span_id"
	keyRequestID contextKey = "request_id"
	keyUserID    contextKey = "user_id"
	keyComponent contextKey = "component"
)

// Logger wraps logrus.Logger with context-aware structured logging,
// dynamic log level adjustment, and log sampling support.
type Logger struct {
	*logrus.Logger
	component string
	sampler   *LogSampler // nil = no sampling
	mu        sync.RWMutex
}

// Config configures the logger.
type Config struct {
	Level     string // debug, info, warn, error
	Format    string // json (default), text
	Output    io.Writer
	Component string // service component name (e.g. "apiserver", "scheduler")

	// Multi-sink output: additional writers beyond the primary output.
	AdditionalOutputs []io.Writer

	// EnableSampling activates log sampling to prevent log flooding.
	EnableSampling bool

	// SamplerConfig configures log sampling (used when EnableSampling=true).
	SamplerConfig *SamplerConfig
}

// New creates a new structured Logger.
func New(cfg Config) *Logger {
	l := logrus.New()

	// Set formatter — always JSON in production
	switch cfg.Format {
	case "text":
		l.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339Nano,
		})
	default:
		l.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat:  time.RFC3339Nano,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
			},
		})
	}

	// Set level
	lvl, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		lvl = logrus.InfoLevel
	}
	l.SetLevel(lvl)

	// Set output: primary + additional sinks
	var output io.Writer
	if cfg.Output != nil {
		output = cfg.Output
	} else {
		output = os.Stdout
	}

	if len(cfg.AdditionalOutputs) > 0 {
		writers := make([]io.Writer, 0, len(cfg.AdditionalOutputs)+1)
		writers = append(writers, output)
		writers = append(writers, cfg.AdditionalOutputs...)
		l.SetOutput(io.MultiWriter(writers...))
	} else {
		l.SetOutput(output)
	}

	logger := &Logger{
		Logger:    l,
		component: cfg.Component,
	}

	// Setup sampling if enabled
	if cfg.EnableSampling {
		sc := DefaultSamplerConfig()
		if cfg.SamplerConfig != nil {
			sc = *cfg.SamplerConfig
		}
		logger.sampler = NewLogSampler(sc)
	}

	return logger
}

// WithContext returns a logrus.Entry enriched with trace/request IDs from context.
func (l *Logger) WithContext(ctx context.Context) *logrus.Entry {
	fields := logrus.Fields{}

	if l.component != "" {
		fields["component"] = l.component
	}

	if ctx == nil {
		return l.WithFields(fields)
	}

	if v, ok := ctx.Value(keyTraceID).(string); ok && v != "" {
		fields["trace_id"] = v
	}
	if v, ok := ctx.Value(keySpanID).(string); ok && v != "" {
		fields["span_id"] = v
	}
	if v, ok := ctx.Value(keyRequestID).(string); ok && v != "" {
		fields["request_id"] = v
	}
	if v, ok := ctx.Value(keyUserID).(string); ok && v != "" {
		fields["user_id"] = v
	}
	if v, ok := ctx.Value(keyComponent).(string); ok && v != "" {
		fields["component"] = v
	}

	return l.WithFields(fields)
}

// Logrus returns the underlying *logrus.Logger for compatibility with
// existing code that expects a plain logrus logger.
func (l *Logger) Logrus() *logrus.Logger {
	return l.Logger
}

// ============================================================================
// Dynamic Log Level Adjustment
// ============================================================================

// SetLevel dynamically changes the log level at runtime.
func (l *Logger) SetLevel(level string) error {
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", level, err)
	}
	l.Logger.SetLevel(lvl)
	l.Logger.WithField("new_level", level).Info("Log level changed dynamically")
	return nil
}

// GetLevel returns the current log level as a string.
func (l *Logger) GetLevel() string {
	return l.Logger.GetLevel().String()
}

// LevelHandler returns an HTTP handler that can GET/PUT the log level.
// GET  /log-level          -> returns {"level": "info"}
// PUT  /log-level?level=debug -> changes level
func (l *Logger) LevelHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"level": l.GetLevel()})

		case http.MethodPut, http.MethodPost:
			newLevel := r.URL.Query().Get("level")
			if newLevel == "" {
				// Try reading from body
				var body struct{ Level string `json:"level"` }
				if json.NewDecoder(r.Body).Decode(&body) == nil {
					newLevel = body.Level
				}
			}
			if newLevel == "" {
				http.Error(w, `{"error":"missing level parameter"}`, http.StatusBadRequest)
				return
			}
			if err := l.SetLevel(newLevel); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"level": l.GetLevel()})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

// ============================================================================
// Sampled Logging — rate-limited structured logging
// ============================================================================

// SamplerConfig configures log sampling.
type SamplerConfig struct {
	// InitialCount: number of identical messages to always log before sampling starts.
	InitialCount int64

	// ThereafterRate: after InitialCount, log every N-th message.
	ThereafterRate int64

	// WindowDuration: reset counters after this duration.
	WindowDuration time.Duration
}

// DefaultSamplerConfig returns a sensible default sampler configuration.
func DefaultSamplerConfig() SamplerConfig {
	return SamplerConfig{
		InitialCount:   100,
		ThereafterRate: 100,
		WindowDuration: 1 * time.Minute,
	}
}

// LogSampler implements log sampling to prevent log flooding.
type LogSampler struct {
	config     SamplerConfig
	counters   sync.Map // key -> *samplerCounter
}

type samplerCounter struct {
	count     atomic.Int64
	resetTime atomic.Int64 // unix nanos
}

// NewLogSampler creates a new log sampler.
func NewLogSampler(cfg SamplerConfig) *LogSampler {
	if cfg.InitialCount <= 0 {
		cfg.InitialCount = 100
	}
	if cfg.ThereafterRate <= 0 {
		cfg.ThereafterRate = 100
	}
	if cfg.WindowDuration <= 0 {
		cfg.WindowDuration = 1 * time.Minute
	}
	return &LogSampler{config: cfg}
}

// ShouldLog returns true if this message should be logged (not sampled out).
func (s *LogSampler) ShouldLog(key string) bool {
	now := time.Now().UnixNano()

	val, _ := s.counters.LoadOrStore(key, &samplerCounter{})
	ctr := val.(*samplerCounter)

	// Reset counter if window expired
	lastReset := ctr.resetTime.Load()
	if now-lastReset > s.config.WindowDuration.Nanoseconds() {
		ctr.count.Store(0)
		ctr.resetTime.Store(now)
	}

	n := ctr.count.Add(1)

	// Always log the first N messages in a window
	if n <= s.config.InitialCount {
		return true
	}

	// Thereafter, log every M-th message
	return (n-s.config.InitialCount)%s.config.ThereafterRate == 0
}

// SampledLog logs a message if the sampler allows it (call instead of direct logging).
func (l *Logger) SampledLog(ctx context.Context, level logrus.Level, sampleKey, msg string) {
	if l.sampler != nil && !l.sampler.ShouldLog(sampleKey) {
		return
	}
	l.WithContext(ctx).Log(level, msg)
}

// SampledLogf logs a formatted message with sampling.
func (l *Logger) SampledLogf(ctx context.Context, level logrus.Level, sampleKey, format string, args ...interface{}) {
	if l.sampler != nil && !l.sampler.ShouldLog(sampleKey) {
		return
	}
	l.WithContext(ctx).Logf(level, format, args...)
}

// ============================================================================
// Context Helpers — inject trace correlation fields into context
// ============================================================================

// WithTraceID injects a trace ID into the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, keyTraceID, traceID)
}

// WithSpanID injects a span ID into the context.
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, keySpanID, spanID)
}

// WithRequestID injects a request ID into the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, keyRequestID, requestID)
}

// WithUserID injects a user ID into the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, keyUserID, userID)
}

// WithComponent injects a component name into the context.
func WithComponent(ctx context.Context, component string) context.Context {
	return context.WithValue(ctx, keyComponent, component)
}

// TraceIDFromContext extracts the trace ID from context.
func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyTraceID).(string); ok {
		return v
	}
	return ""
}

// RequestIDFromContext extracts the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyRequestID).(string); ok {
		return v
	}
	return ""
}
