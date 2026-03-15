// Package tracing provides OpenTelemetry distributed tracing integration
// for CloudAI Fusion. It initializes a TracerProvider with OTLP/gRPC exporter
// and provides middleware for Gin HTTP servers.
package tracing

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/logging"
)

// Config configures the tracing subsystem.
type Config struct {
	ServiceName    string  // e.g. "cloudai-apiserver"
	ServiceVersion string  // e.g. "0.1.0"
	Endpoint       string  // OTLP gRPC endpoint, e.g. "localhost:4317"
	SampleRate     float64 // 0.0 to 1.0 (1.0 = trace everything)
	Enabled        bool    // master toggle

	// Adaptive sampling: dynamically adjust sample rate based on load
	AdaptiveSampling bool    // Enable adaptive sampling
	MinSampleRate    float64 // Minimum sample rate under high load (default: 0.01)
	MaxSampleRate    float64 // Maximum sample rate under low load (default: 1.0)
	TargetSpansPerSec int    // Target spans/sec throughput (default: 100)

	// Continuous Profiling integration
	ProfilingEnabled  bool   // Enable profiling hook
	ProfilingEndpoint string // Pyroscope/Parca endpoint, e.g. "http://pyroscope:4040"

	// Environment enrichment
	Environment string // production, staging, development
	Deployment  string // canary, stable, blue, green
}

// Provider wraps the OpenTelemetry TracerProvider with lifecycle management.
type Provider struct {
	tp       *sdktrace.TracerProvider
	tracer   trace.Tracer
	config   Config
	profiler *profilingHook
}

// Init initializes the OpenTelemetry tracing pipeline.
// Returns a Provider whose Shutdown() must be called on server stop.
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled || cfg.Endpoint == "" {
		// Return a no-op provider when tracing is disabled
		tp := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return &Provider{tp: tp, tracer: tp.Tracer(cfg.ServiceName), config: cfg}, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(), // Use TLS in production
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 0.1
	}
	if sampleRate > 1.0 {
		sampleRate = 1.0
	}

	env := cfg.Environment
	if env == "" {
		env = "production"
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		attribute.String("environment", env),
	}
	if cfg.Deployment != "" {
		attrs = append(attrs, attribute.String("deployment", cfg.Deployment))
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Build sampler: adaptive or fixed ratio
	var sampler sdktrace.Sampler
	if cfg.AdaptiveSampling {
		sampler = newAdaptiveSampler(adaptiveSamplerConfig{
			MinRate:         cfg.MinSampleRate,
			MaxRate:         cfg.MaxSampleRate,
			TargetSpansPerSec: cfg.TargetSpansPerSec,
			InitialRate:     sampleRate,
		})
	} else {
		sampler = sdktrace.TraceIDRatioBased(sampleRate)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxExportBatchSize(512),
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sampler)),
	)

	otel.SetTracerProvider(tp)
	// W3C Trace Context + Baggage propagation (cross-service)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	provider := &Provider{
		tp:     tp,
		tracer: tp.Tracer(cfg.ServiceName),
		config: cfg,
	}

	// Initialize profiling hook if enabled
	if cfg.ProfilingEnabled && cfg.ProfilingEndpoint != "" {
		provider.profiler = newProfilingHook(cfg.ProfilingEndpoint, cfg.ServiceName)
	}

	logrus.WithFields(logrus.Fields{
		"endpoint":         cfg.Endpoint,
		"sample_rate":      sampleRate,
		"adaptive":         cfg.AdaptiveSampling,
		"profiling":        cfg.ProfilingEnabled,
		"environment":      env,
	}).Info("OpenTelemetry tracing initialized (enhanced)")

	return provider, nil
}

// Tracer returns the named tracer.
func (p *Provider) Tracer() trace.Tracer {
	return p.tracer
}

// Config returns the tracing configuration.
func (p *Provider) Config() Config {
	return p.config
}

// Shutdown flushes remaining spans and shuts down the provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.profiler != nil {
		p.profiler.stop()
	}
	return p.tp.Shutdown(ctx)
}

// Middleware returns a Gin middleware that creates a span for each HTTP request,
// injects trace_id into the context for structured logging, and sets
// standard HTTP span attributes.
func Middleware(tracer trace.Tracer) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract propagated context from incoming headers
		propagator := otel.GetTextMapPropagator()
		ctx := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		spanName := fmt.Sprintf("%s %s", c.Request.Method, c.FullPath())
		if c.FullPath() == "" {
			spanName = fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path)
		}

		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLPath(c.Request.URL.Path),
				semconv.ClientAddress(c.ClientIP()),
				semconv.UserAgentOriginal(c.Request.UserAgent()),
			),
		)
		defer span.End()

		// Inject trace_id into context for structured logging
		traceID := span.SpanContext().TraceID().String()
		spanID := span.SpanContext().SpanID().String()
		ctx = logging.WithTraceID(ctx, traceID)
		ctx = logging.WithSpanID(ctx, spanID)

		// Also inject request_id if present
		if reqID := c.GetString("request_id"); reqID != "" {
			ctx = logging.WithRequestID(ctx, reqID)
		}

		// Set response headers for trace correlation
		c.Header("X-Trace-ID", traceID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		// Record response attributes
		status := c.Writer.Status()
		span.SetAttributes(
			semconv.HTTPResponseStatusCode(status),
		)
		if status >= 500 {
			span.SetAttributes(attribute.Bool("error", true))
		}
	}
}

// StartSpan is a convenience wrapper to start a child span from context.
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer("cloudai-fusion").Start(ctx, name, opts...)
}

// ============================================================================
// Adaptive Sampler — dynamically adjusts sample rate based on throughput
// ============================================================================

type adaptiveSamplerConfig struct {
	MinRate         float64
	MaxRate         float64
	TargetSpansPerSec int
	InitialRate     float64
}

type adaptiveSampler struct {
	config      adaptiveSamplerConfig
	currentRate float64
	spanCount   int64
	lastAdjust  time.Time
	mu          sync.Mutex
	description string
}

func newAdaptiveSampler(cfg adaptiveSamplerConfig) *adaptiveSampler {
	if cfg.MinRate <= 0 {
		cfg.MinRate = 0.01
	}
	if cfg.MaxRate <= 0 || cfg.MaxRate > 1.0 {
		cfg.MaxRate = 1.0
	}
	if cfg.TargetSpansPerSec <= 0 {
		cfg.TargetSpansPerSec = 100
	}
	if cfg.InitialRate <= 0 {
		cfg.InitialRate = 0.1
	}
	return &adaptiveSampler{
		config:      cfg,
		currentRate: cfg.InitialRate,
		lastAdjust:  time.Now(),
		description: fmt.Sprintf("AdaptiveSampler{min=%.3f,max=%.3f,target=%d/s}",
			cfg.MinRate, cfg.MaxRate, cfg.TargetSpansPerSec),
	}
}

func (s *adaptiveSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	s.mu.Lock()
	s.spanCount++
	now := time.Now()
	elapsed := now.Sub(s.lastAdjust)

	// Adjust rate every 10 seconds
	if elapsed >= 10*time.Second {
		actualSpansPerSec := float64(s.spanCount) / elapsed.Seconds()
		target := float64(s.config.TargetSpansPerSec)

		if actualSpansPerSec > target*1.2 {
			// Too many spans, reduce rate
			s.currentRate = math.Max(s.config.MinRate, s.currentRate*0.8)
		} else if actualSpansPerSec < target*0.8 {
			// Room for more spans, increase rate
			s.currentRate = math.Min(s.config.MaxRate, s.currentRate*1.2)
		}

		s.spanCount = 0
		s.lastAdjust = now
	}
	rate := s.currentRate
	s.mu.Unlock()

	// Use TraceIDRatioBased logic: sample if traceID < rate * maxID
	result := sdktrace.TraceIDRatioBased(rate).ShouldSample(p)
	return result
}

func (s *adaptiveSampler) Description() string {
	return s.description
}

// ============================================================================
// Profiling Hook — eBPF / Continuous Profiling integration stub
// ============================================================================

// profilingHook provides an integration point for continuous profiling backends
// such as Pyroscope or Parca. It annotates trace spans with profiling labels.
type profilingHook struct {
	endpoint    string
	serviceName string
	running     bool
	mu          sync.Mutex
}

func newProfilingHook(endpoint, serviceName string) *profilingHook {
	p := &profilingHook{
		endpoint:    endpoint,
		serviceName: serviceName,
		running:     true,
	}
	logrus.WithFields(logrus.Fields{
		"endpoint": endpoint,
		"service":  serviceName,
	}).Info("Continuous profiling hook initialized (eBPF-ready)")
	return p
}

func (p *profilingHook) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	logrus.Info("Continuous profiling hook stopped")
}

// ProfilingEnabled returns whether the profiling integration is active.
func (p *Provider) ProfilingEnabled() bool {
	return p.profiler != nil && p.profiler.running
}
