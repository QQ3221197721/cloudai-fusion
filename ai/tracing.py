"""
CloudAI Fusion - AI Engine Distributed Tracing (OpenTelemetry)
=============================================================
Provides cross-language trace context propagation between Go and Python services.

Architecture:
  Go (apiserver/scheduler/agent) ──traceparent header──→ Python (ai-engine)
       │                                                        │
       └── OTLP/gRPC ──→ Jaeger/OTel Collector ←── OTLP/gRPC ─┘

Key features:
  - W3C Trace Context propagation (traceparent/tracestate headers)
  - FastAPI middleware that extracts incoming trace context & creates spans
  - structlog integration: auto-injects trace_id/span_id into every log line
  - Graceful fallback: works as no-op when Jaeger/collector is unavailable
  - OTLP/gRPC exporter to Jaeger (same protocol as Go services)
"""

import logging
import os
from contextvars import ContextVar
from typing import Any, Dict, Optional

# ---------------------------------------------------------------------------
# OpenTelemetry imports (all in requirements.txt)
# ---------------------------------------------------------------------------
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.propagate import extract, inject, set_global_textmap
from opentelemetry.propagators.composite import CompositeHTTPPropagator
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor, ConsoleSpanExporter
from opentelemetry.trace import (
    SpanKind,
    StatusCode,
    get_current_span,
)
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

# Optional: W3C Baggage propagation
try:
    from opentelemetry.baggage.propagation import W3CBaggagePropagator

    _HAS_BAGGAGE = True
except ImportError:
    _HAS_BAGGAGE = False


logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Context variables for trace correlation in logs
# ---------------------------------------------------------------------------
_trace_id_var: ContextVar[str] = ContextVar("trace_id", default="")
_span_id_var: ContextVar[str] = ContextVar("span_id", default="")
_request_id_var: ContextVar[str] = ContextVar("request_id", default="")

# Global tracer provider reference
_provider: Optional[TracerProvider] = None


def get_trace_id() -> str:
    """Get current trace_id from context (for log injection)."""
    return _trace_id_var.get("")


def get_span_id() -> str:
    """Get current span_id from context (for log injection)."""
    return _span_id_var.get("")


def get_request_id() -> str:
    """Get current request_id from context."""
    return _request_id_var.get("")


# ---------------------------------------------------------------------------
# TracerProvider lifecycle
# ---------------------------------------------------------------------------


class TracingConfig:
    """Configuration for the tracing subsystem."""

    def __init__(
        self,
        service_name: str = "cloudai-ai-engine",
        service_version: str = "0.1.0",
        otlp_endpoint: Optional[str] = None,
        enabled: bool = True,
        sample_rate: float = 1.0,
        console_export: bool = False,
        environment: str = "development",
    ):
        self.service_name = service_name
        self.service_version = service_version
        self.otlp_endpoint = otlp_endpoint or os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
        self.enabled = enabled
        self.sample_rate = sample_rate
        self.console_export = console_export
        self.environment = environment or os.getenv("CLOUDAI_ENV", "development")


def init_tracing(config: Optional[TracingConfig] = None) -> TracerProvider:
    """
    Initialize OpenTelemetry tracing for the Python AI engine.

    Sets up:
    - W3C Trace Context + Baggage propagation (compatible with Go side)
    - OTLP/gRPC exporter to Jaeger/OTel Collector
    - Resource attributes (service.name, service.version, environment)
    - Batch span processor for efficient export

    Returns the TracerProvider (also set as global).
    """
    global _provider

    if config is None:
        config = TracingConfig()

    resource = Resource.create(
        {
            "service.name": config.service_name,
            "service.version": config.service_version,
            "environment": config.environment,
            "language": "python",
        }
    )

    provider = TracerProvider(resource=resource)

    if config.enabled and config.otlp_endpoint:
        try:
            # Strip protocol prefix if present (gRPC doesn't use http://)
            endpoint = config.otlp_endpoint
            if endpoint.startswith("http://"):
                endpoint = endpoint[len("http://") :]
            elif endpoint.startswith("https://"):
                endpoint = endpoint[len("https://") :]

            exporter = OTLPSpanExporter(
                endpoint=endpoint,
                insecure=True,  # Use TLS in production
            )
            provider.add_span_processor(
                BatchSpanProcessor(
                    exporter,
                    max_queue_size=2048,
                    max_export_batch_size=512,
                    export_timeout_millis=30000,
                )
            )
            logger.info(
                "OTLP trace exporter initialized: endpoint=%s, service=%s",
                endpoint,
                config.service_name,
            )
        except Exception as e:
            logger.warning("Failed to initialize OTLP exporter (tracing degraded): %s", e)

    if config.console_export:
        provider.add_span_processor(BatchSpanProcessor(ConsoleSpanExporter()))

    # Set as global provider
    trace.set_tracer_provider(provider)
    _provider = provider

    # Set W3C Trace Context propagator (same as Go side)
    propagators = [TraceContextTextMapPropagator()]
    if _HAS_BAGGAGE:
        propagators.append(W3CBaggagePropagator())
    set_global_textmap(CompositeHTTPPropagator(propagators))

    logger.info(
        "OpenTelemetry tracing initialized: service=%s, env=%s, otlp=%s",
        config.service_name,
        config.environment,
        bool(config.otlp_endpoint),
    )

    return provider


async def shutdown_tracing():
    """Gracefully shutdown the tracing provider, flushing pending spans."""
    global _provider
    if _provider is not None:
        _provider.shutdown()
        logger.info("OpenTelemetry tracing shut down")
        _provider = None


def get_tracer(name: str = "cloudai-ai-engine") -> trace.Tracer:
    """Get a named tracer from the global provider."""
    return trace.get_tracer(name)


# ---------------------------------------------------------------------------
# structlog processor — auto-inject trace_id/span_id into every log
# ---------------------------------------------------------------------------


def add_trace_context(logger_instance: Any, method_name: str, event_dict: Dict[str, Any]) -> Dict[str, Any]:
    """
    structlog processor that injects trace_id and span_id into log events.

    Usage in structlog.configure():
        processors=[
            ...,
            add_trace_context,
            ...,
        ]
    """
    tid = get_trace_id()
    sid = get_span_id()
    rid = get_request_id()

    if tid:
        event_dict["trace_id"] = tid
    if sid:
        event_dict["span_id"] = sid
    if rid:
        event_dict["request_id"] = rid

    # Also try from active OTel span (fallback if context vars not set)
    if not tid:
        span = get_current_span()
        ctx = span.get_span_context()
        if ctx and ctx.trace_id:
            event_dict["trace_id"] = format(ctx.trace_id, "032x")
        if ctx and ctx.span_id:
            event_dict["span_id"] = format(ctx.span_id, "016x")

    return event_dict


# ---------------------------------------------------------------------------
# FastAPI Middleware — W3C Trace Context extraction + span creation
# ---------------------------------------------------------------------------


class _HeaderCarrier:
    """Adapter for extracting trace context from ASGI/Starlette headers."""

    def __init__(self, headers: dict):
        self._headers = {k.lower(): v for k, v in headers.items()}

    def get(self, key: str, default: Optional[str] = None) -> Optional[str]:
        return self._headers.get(key.lower(), default)

    def keys(self):
        return self._headers.keys()


def create_tracing_middleware():
    """
    Create a FastAPI/Starlette ASGI middleware for distributed tracing.

    What it does:
    1. Extracts W3C traceparent from incoming request headers
       (propagated by Go apiserver/scheduler/agent)
    2. Creates a server span for the request
    3. Sets trace_id/span_id context vars for structlog injection
    4. Adds X-Trace-ID response header for frontend correlation
    5. Records HTTP status and errors on the span
    """
    tracer = get_tracer()

    async def tracing_middleware(request, call_next):
        # Extract propagated context from incoming headers
        headers = dict(request.headers)
        carrier = _HeaderCarrier(headers)
        parent_ctx = extract(carrier)

        # Build span name
        path = request.url.path
        method = request.method
        span_name = f"{method} {path}"

        # Start server span with extracted parent context
        with tracer.start_as_current_span(
            span_name,
            context=parent_ctx,
            kind=SpanKind.SERVER,
            attributes={
                "http.method": method,
                "http.url": str(request.url),
                "http.route": path,
                "http.client_ip": request.client.host if request.client else "unknown",
                "http.user_agent": headers.get("user-agent", ""),
            },
        ) as span:
            # Set context vars for structlog log injection
            ctx = span.get_span_context()
            trace_id = format(ctx.trace_id, "032x")
            span_id = format(ctx.span_id, "016x")

            t_token = _trace_id_var.set(trace_id)
            s_token = _span_id_var.set(span_id)

            # Extract X-Request-ID if present (from Go services)
            req_id = headers.get("x-request-id", "")
            r_token = _request_id_var.set(req_id)

            try:
                response = await call_next(request)

                # Record response status
                span.set_attribute("http.status_code", response.status_code)
                if response.status_code >= 500:
                    span.set_status(StatusCode.ERROR, f"HTTP {response.status_code}")

                # Add trace correlation headers to response
                response.headers["X-Trace-ID"] = trace_id
                response.headers["X-Span-ID"] = span_id

                return response

            except Exception as exc:
                span.set_status(StatusCode.ERROR, str(exc))
                span.record_exception(exc)
                raise

            finally:
                _trace_id_var.reset(t_token)
                _span_id_var.reset(s_token)
                _request_id_var.reset(r_token)

    return tracing_middleware


# ---------------------------------------------------------------------------
# Outgoing request trace propagation (for ai-engine → external calls)
# ---------------------------------------------------------------------------


def inject_trace_headers(headers: Optional[Dict[str, str]] = None) -> Dict[str, str]:
    """
    Inject W3C trace context into outgoing HTTP request headers.

    Use when ai-engine makes outgoing calls (e.g., to LLM providers).
    This ensures the trace chain continues even into external services.

    Example:
        headers = inject_trace_headers()
        response = await httpx_client.post(url, headers=headers)
    """
    if headers is None:
        headers = {}
    inject(headers)
    return headers


# ---------------------------------------------------------------------------
# Utility: create child spans for business logic
# ---------------------------------------------------------------------------


def start_span(
    name: str,
    kind: SpanKind = SpanKind.INTERNAL,
    attributes: Optional[Dict[str, Any]] = None,
) -> trace.Span:
    """
    Start a new child span (shortcut).

    Usage:
        with start_span("llm_inference", attributes={"model": "qwen-max"}) as span:
            result = await llm.generate(prompt)
            span.set_attribute("tokens", result.usage.total_tokens)
    """
    tracer = get_tracer()
    return tracer.start_as_current_span(name, kind=kind, attributes=attributes or {})

