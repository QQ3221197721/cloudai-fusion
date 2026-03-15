package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ============================================================================
// Span Builders — pre-configured span creators for common subsystems
// ============================================================================

const (
	tracerName = "cloudai-fusion"

	// Span attribute keys
	attrDBSystem    = "db.system"
	attrDBStatement = "db.statement"
	attrDBTable     = "db.sql.table"
	attrDBOperation = "db.operation"

	attrCacheSystem = "cache.system"
	attrCacheKey    = "cache.key"
	attrCacheHit    = "cache.hit"

	attrMsgSystem      = "messaging.system"
	attrMsgDestination = "messaging.destination"
	attrMsgOperation   = "messaging.operation"
)

// ============================================================================
// Database Span
// ============================================================================

// DBSpanConfig configures a database span.
type DBSpanConfig struct {
	System    string // "postgresql", "redis", "sqlite"
	Operation string // "SELECT", "INSERT", "UPDATE", "DELETE"
	Table     string
	Statement string // SQL statement (sanitized, no PII)
}

// StartDBSpan creates a span for a database operation.
func StartDBSpan(ctx context.Context, cfg DBSpanConfig) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	spanName := fmt.Sprintf("DB %s %s", cfg.Operation, cfg.Table)
	if cfg.Table == "" {
		spanName = fmt.Sprintf("DB %s", cfg.Operation)
	}

	attrs := []attribute.KeyValue{
		attribute.String(attrDBSystem, cfg.System),
		attribute.String(attrDBOperation, cfg.Operation),
	}
	if cfg.Table != "" {
		attrs = append(attrs, attribute.String(attrDBTable, cfg.Table))
	}
	if cfg.Statement != "" {
		attrs = append(attrs, attribute.String(attrDBStatement, cfg.Statement))
	}

	return tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
}

// EndDBSpan ends the database span, recording error if any.
func EndDBSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// ============================================================================
// Cache Span
// ============================================================================

// CacheSpanConfig configures a cache span.
type CacheSpanConfig struct {
	System    string // "redis", "memory", "multi-level"
	Operation string // "GET", "SET", "DELETE", "EXISTS"
	Key       string
}

// StartCacheSpan creates a span for a cache operation.
func StartCacheSpan(ctx context.Context, cfg CacheSpanConfig) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	spanName := fmt.Sprintf("Cache %s", cfg.Operation)

	attrs := []attribute.KeyValue{
		attribute.String(attrCacheSystem, cfg.System),
		attribute.String(attrDBOperation, cfg.Operation),
	}
	if cfg.Key != "" {
		attrs = append(attrs, attribute.String(attrCacheKey, cfg.Key))
	}

	return tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
}

// EndCacheSpan ends the cache span, recording hit/miss and error.
func EndCacheSpan(span trace.Span, hit bool, err error) {
	span.SetAttributes(attribute.Bool(attrCacheHit, hit))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// ============================================================================
// Messaging Span
// ============================================================================

// MsgSpanConfig configures a messaging span.
type MsgSpanConfig struct {
	System      string // "nats", "kafka", "memory"
	Destination string // queue/topic name
	Operation   string // "publish", "consume", "ack"
}

// StartPublishSpan creates a span for publishing a message.
func StartPublishSpan(ctx context.Context, cfg MsgSpanConfig) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	spanName := fmt.Sprintf("%s publish %s", cfg.System, cfg.Destination)

	return tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String(attrMsgSystem, cfg.System),
			attribute.String(attrMsgDestination, cfg.Destination),
			attribute.String(attrMsgOperation, "publish"),
		),
	)
}

// StartConsumeSpan creates a span for consuming a message.
func StartConsumeSpan(ctx context.Context, cfg MsgSpanConfig) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	spanName := fmt.Sprintf("%s consume %s", cfg.System, cfg.Destination)

	return tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String(attrMsgSystem, cfg.System),
			attribute.String(attrMsgDestination, cfg.Destination),
			attribute.String(attrMsgOperation, "consume"),
		),
	)
}

// EndMsgSpan ends the messaging span, recording error if any.
func EndMsgSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// ============================================================================
// EventBus Span
// ============================================================================

// StartEventBusSpan creates a span for an event bus publish/subscribe operation.
func StartEventBusSpan(ctx context.Context, operation, topic string) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	spanName := fmt.Sprintf("eventbus.%s %s", operation, topic)

	kind := trace.SpanKindProducer
	if operation == "subscribe" || operation == "handle" {
		kind = trace.SpanKindConsumer
	}

	return tracer.Start(ctx, spanName,
		trace.WithSpanKind(kind),
		trace.WithAttributes(
			attribute.String("eventbus.topic", topic),
			attribute.String("eventbus.operation", operation),
		),
	)
}

// ============================================================================
// Controller/Reconcile Span
// ============================================================================

// StartReconcileSpan creates a span for a controller reconciliation cycle.
func StartReconcileSpan(ctx context.Context, controllerName, resourceKey string) (context.Context, trace.Span) {
	tracer := otel.Tracer(tracerName)
	spanName := fmt.Sprintf("reconcile/%s", controllerName)

	return tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("controller.name", controllerName),
			attribute.String("controller.resource_key", resourceKey),
		),
	)
}
