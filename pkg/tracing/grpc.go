package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ============================================================================
// gRPC Tracing Interceptors (OpenTelemetry)
// ============================================================================

// UnaryServerInterceptor returns a gRPC unary server interceptor that creates
// a span for each incoming gRPC call and propagates trace context.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	tracer := otel.Tracer("cloudai-fusion-grpc-server")

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Extract trace context from incoming metadata
		md, _ := metadata.FromIncomingContext(ctx)
		carrier := metadataCarrier(md)
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", info.FullMethod),
			),
		)
		defer span.End()

		resp, err := handler(ctx, req)
		if err != nil {
			st, _ := status.FromError(err)
			span.SetStatus(codes.Error, st.Message())
			span.SetAttributes(
				attribute.String("rpc.grpc.status_code", st.Code().String()),
			)
		} else {
			span.SetStatus(codes.Ok, "")
			span.SetAttributes(
				attribute.String("rpc.grpc.status_code", grpccodes.OK.String()),
			)
		}
		return resp, err
	}
}

// StreamServerInterceptor returns a gRPC stream server interceptor.
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	tracer := otel.Tracer("cloudai-fusion-grpc-server")

	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		ctx := ss.Context()
		md, _ := metadata.FromIncomingContext(ctx)
		carrier := metadataCarrier(md)
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", info.FullMethod),
				attribute.Bool("rpc.stream", true),
			),
		)
		defer span.End()

		wrapped := &tracedServerStream{ServerStream: ss, ctx: ctx}
		err := handler(srv, wrapped)
		if err != nil {
			st, _ := status.FromError(err)
			span.SetStatus(codes.Error, st.Message())
		}
		return err
	}
}

// UnaryClientInterceptor returns a gRPC unary client interceptor that creates
// a span for each outgoing gRPC call and injects trace context.
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	tracer := otel.Tracer("cloudai-fusion-grpc-client")

	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx, span := tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", method),
				attribute.String("rpc.target", cc.Target()),
			),
		)
		defer span.End()

		// Inject trace context into outgoing metadata
		md, _ := metadata.FromOutgoingContext(ctx)
		md = md.Copy()
		carrier := metadataCarrier(md)
		otel.GetTextMapPropagator().Inject(ctx, carrier)
		ctx = metadata.NewOutgoingContext(ctx, md)

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			st, _ := status.FromError(err)
			span.SetStatus(codes.Error, st.Message())
			span.SetAttributes(
				attribute.String("rpc.grpc.status_code", st.Code().String()),
			)
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return err
	}
}

// ============================================================================
// Metadata carrier — adapts gRPC metadata to TextMapCarrier
// ============================================================================

type metadataCarrier metadata.MD

func (c metadataCarrier) Get(key string) string {
	vals := metadata.MD(c).Get(key)
	if len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func (c metadataCarrier) Set(key, value string) {
	metadata.MD(c).Set(key, value)
}

func (c metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// tracedServerStream wraps grpc.ServerStream to carry the traced context.
type tracedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *tracedServerStream) Context() context.Context {
	return s.ctx
}
