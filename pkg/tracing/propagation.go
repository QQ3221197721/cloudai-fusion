package tracing

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// ============================================================================
// W3C Trace Context + Baggage Propagation Helpers
// ============================================================================

// InjectHTTP injects the current trace context and baggage into outgoing
// HTTP request headers using W3C Trace Context format.
func InjectHTTP(ctx context.Context, req *http.Request) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
}

// ExtractHTTP extracts trace context and baggage from incoming HTTP headers.
func ExtractHTTP(ctx context.Context, headers http.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(headers))
}

// InjectMap injects trace context into a generic string map (e.g., message headers).
func InjectMap(ctx context.Context, carrier map[string]string) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(carrier))
}

// ExtractMap extracts trace context from a generic string map.
func ExtractMap(ctx context.Context, carrier map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}

// ============================================================================
// Baggage Helpers — propagate business context across services
// ============================================================================

// SetBaggage adds key-value pairs to the W3C Baggage for cross-service propagation.
// Example: SetBaggage(ctx, "tenant.id", "acme", "user.id", "u-123")
func SetBaggage(ctx context.Context, kvPairs ...string) context.Context {
	if len(kvPairs)%2 != 0 {
		return ctx
	}
	members := make([]baggage.Member, 0, len(kvPairs)/2)
	for i := 0; i < len(kvPairs); i += 2 {
		m, err := baggage.NewMember(kvPairs[i], kvPairs[i+1])
		if err == nil {
			members = append(members, m)
		}
	}
	if len(members) == 0 {
		return ctx
	}
	b, err := baggage.New(members...)
	if err != nil {
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, b)
}

// GetBaggage retrieves a baggage value by key from context.
func GetBaggage(ctx context.Context, key string) string {
	b := baggage.FromContext(ctx)
	m := b.Member(key)
	return m.Value()
}

// ============================================================================
// Span Context Extraction
// ============================================================================

// TraceIDFromContext extracts the trace ID from the current span in context.
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanIDFromContext extracts the span ID from the current span in context.
func SpanIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}

// IsSampled returns whether the current trace is being sampled.
func IsSampled(ctx context.Context) bool {
	return trace.SpanContextFromContext(ctx).IsSampled()
}

// ============================================================================
// Span Enrichment Helpers
// ============================================================================

// AddEvent adds a timestamped event to the current span.
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes sets additional attributes on the current span.
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}

// RecordError records an error on the current span.
func RecordError(ctx context.Context, err error, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err, trace.WithAttributes(attrs...))
}
