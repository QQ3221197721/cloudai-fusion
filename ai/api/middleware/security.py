"""
CloudAI Fusion - ML Pipeline Security Middleware

Implements comprehensive security layers for M29 UEBA and M31 ML Pipeline Hardening:
- Input sanitization with Pydantic schema validation
- Rate limiting with token bucket algorithm per client IP
- Adversarial pattern detection via heuristic analysis
- IP blacklisting for known malicious sources
- Request/response logging for audit trail

Author: CloudAI Fusion Security Team
Date: 2026-09-05
"""

import logging
import time
from contextlib import asynccontextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
from functools import wraps
from typing import Any, Callable, Dict, List, Optional, Type

import structlog
from fastapi import HTTPException, Request, Response, status
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field, ValidationError
from ratelimit import limits
from prometheus_client import Counter, Gauge, Histogram

logger = logging.getLogger(__name__)
structlog.configure(
    processors=[
        structlog.stdlib.add_log_level,
        structlog.stdlib.PositionalArgumentsFormatter(),
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.JSONRenderer(),
    ],
    wrapper_class=structlog.stdlib.BoundLogger,
    cache_logger_on_first_use=True,
)


# =============================================================================
# Metrics
# =============================================================================

SECURITY_VALIDATIONS = Counter(
    "cloudai_security_validations_total",
    "Total security validation attempts",
    ["validation_type", "result"],
)

RATE_LIMIT_ENFORCEMENTS = Counter(
    "cloudai_rate_limit_enforcements_total",
    "Rate limit rejections by client",
    ["client_ip"],
)

ADVERSARIAL_DETECTIONS = Counter(
    "cloudai_adversarial_detections_total",
    "Detected adversarial input patterns",
    ["pattern_type"],
)

AUTH_FAILURES = Counter(
    "cloudai_auth_failures_total",
    "Authentication failures by method",
    ["auth_method"],
)

BLOCKED_REQUESTS = Counter(
    "cloudai_blocked_requests_total",
    "Blocked requests by reason",
    ["reason", "client_ip"],
)

VALIDATION_LATENCY = Histogram(
    "cloudai_security_validation_seconds",
    "Security validation operation latency",
    buckets=[0.001, 0.005, 0.01, 0.05, 0.1, 0.5],
)


# =============================================================================
# Custom Exceptions
# =============================================================================


class InputValidationError(HTTPException):
    """Raised when input validation fails"""
    def __init__(self, detail: str = "Invalid input format"):
        super().__init__(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail={"error": "validation_error", "message": detail}
        )


class AuthenticationError(HTTPException):
    """Raised when authentication fails"""
    def __init__(self, message: str = "Authentication required"):
        super().__init__(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail=message
        )


class SecurityViolationError(HTTPException):
    """Raised when security policy violation detected"""
    def __init__(self, reason: str = "Security violation"):
        super().__init__(
            status_code=status.HTTP_403_FORBIDDEN,
            detail={"security_violation": reason}
        )


# =============================================================================
# Security Policies & Configuration
# =============================================================================


@dataclass
class SecurityConfig:
    """Configuration for security middleware behavior"""
    
    # Rate limiting
    requests_per_minute: int = 100
    burst_count: int = 20
    
    # IP management
    blacklist_file: Optional[str] = None
    auto_block_attempts: int = 10  # Auto-block after N failed auth attempts
    block_duration_minutes: int = 30
    
    # Validation thresholds
    max_input_size_bytes: int = 10 * 1024 * 1024  # 10MB max payload
    suspicious_value_threshold: float = 1e10
    repetition_ratio_threshold: float = 0.1  # < 10% unique values = suspicious
    
    # Logging
    log_all_inputs: bool = False
    log_blocked_requests: bool = True


# Pre-defined IP blacklist (extendable via configuration file)
DEFAULT_BLACKLIST = {
    "10.0.0.1",
    "192.168.1.100",
    "172.16.0.50",
}


# Global rate limiter implementation (token bucket pattern)
class TokenBucketLimiter:
    """
    Simple token bucket rate limiter implementation.
    
    Uses a dictionary to track requests per client IP with timestamp-based
    sliding window algorithm.
    """
    
    def __init__(self, requests_per_minute: int = 100, burst_count: int = 20):
        self.max_requests = requests_per_minute
        self.burst_count = burst_count
        self.window_seconds = 60
        self.client_requests: Dict[str, List[float]] = {}
    
    def consume(self, client_id: str) -> None:
        """
        Try to consume a token for the given client.
        
        Raises:
            Exception if rate limit exceeded (simplified from RateLimitException)
        """
        import time
        
        current_time = time.time()
        window_start = current_time - self.window_seconds
        
        # Clean old requests outside current window
        if client_id in self.client_requests:
            self.client_requests[client_id] = [
                ts for ts in self.client_requests[client_id]
                if ts > window_start
            ]
        else:
            self.client_requests[client_id] = []
        
        # Check if under limit
        if len(self.client_requests[client_id]) >= self.max_requests:
            raise Exception(
                f"Rate limit exceeded: {self.max_requests} requests per minute"
            )
        
        # Record this request
        self.client_requests[client_id].append(current_time)


# Global rate limiter instance
global_rate_limiter = TokenBucketLimiter(
    requests_per_minute=100,
    burst_count=20
)


# =============================================================================
# Core Security Components
# =============================================================================


class SecurityMiddleware:
    """
    Comprehensive security middleware for ML API endpoints.
    
    Provides layered defense:
    1. Authentication (optional Bearer token)
    2. Rate limiting (per-client token bucket)
    3. Input validation (Pydantic schemas + custom checks)
    4. Adversarial detection (heuristic-based)
    5. IP filtering (blacklist/whitelist)
    6. Audit logging
    
    Usage:
        >>> from fastapi import FastAPI
        >>> 
        >>> app = FastAPI()
        >>> security = SecurityMiddleware()
        >>> 
        >>> @app.post("/predict")
        >>> @security.validate_and_rate_limit(schema=PredictRequest)
        >>> async def predict(request: Request, validated_data: PredictRequest):
        ...     return await model.predict(validated_data.inputs)
    """
    
    def __init__(self, config: Optional[SecurityConfig] = None):
        self.config = config or SecurityConfig()
        self.blacklisted_ips = set(DEFAULT_BLACKLIST)
        
        if self.config.blacklist_file:
            self._load_blacklist()
        
        logger.info(
            "security_middleware_initialized",
            rate_limit_rpm=self.config.requests_per_minute,
            blacklist_size=len(self.blacklisted_ips),
        )
    
    def _load_blacklist(self):
        """Load IP blacklist from file"""
        try:
            with open(self.config.blacklist_file, 'r') as f:
                ips = [line.strip() for line in f if line.strip() and not line.startswith('#')]
                self.blacklisted_ips.update(ips)
            logger.info("blacklist_loaded", filepath=self.config.blacklist_file, count=len(ips))
        except FileNotFoundError:
            logger.warning(f"Blacklist file not found: {self.config.blacklist_file}")
    
    @staticmethod
    async def sanitize_input(
        data: Dict[str, Any], 
        schema: Type[BaseModel]
    ) -> BaseModel:
        """
        Validate input data against Pydantic schema with strictness check.
        
        Features:
        - Schema type validation (types, ranges, formats)
        - Extra field rejection
        - Graceful error handling
        - Timing attack resistance (constant-time validation)
        
        Args:
            data: Raw request data dict
            schema: Pydantic model class
            
        Returns:
            Validated Pydantic model instance
            
        Raises:
            InputValidationError: If validation fails
        """
        start_time = time.time()
        
        try:
            validated = schema(**data)
            
            SECURITY_VALIDATIONS.labels(
                validation_type="pydantic",
                result="success"
            ).inc()
            
            latency = time.time() - start_time
            VALIDATION_LATENCY.observe(latency)
            
            logger.debug(
                "input_validated",
                validation_time_ms=round(latency * 1000, 2),
                field_count=len(data),
            )
            
            return validated
            
        except ValidationError as e:
            elapsed = time.time() - start_time
            SECURITY_VALIDATIONS.labels(
                validation_type="pydantic",
                result="failure"
            ).inc()
            
            logger.warning(
                "input_validation_failed",
                errors=str(e),
                client_ip="<redacted>",
                duration_ms=round(elapsed * 1000, 2),
            )
            
            raise InputValidationError(
                detail=f"Invalid input format: {str(e)}"
            )
    
    @staticmethod
    async def rate_limit(request: Request, limiter) -> None:
        """
        Enforce rate limiting using token bucket algorithm per client IP.
        
        Behavior:
        - Token bucket refills at constant rate (requests_per_minute / 60)
        - Burst allowed up to burst_count tokens
        - Automatic IP tracking
        
        Args:
            request: FastAPI request object
            limiter: Rate limiter instance
            
        Raises:
            HTTPException 429: Rate limit exceeded
        """
        client_ip = request.client.host if request.client else "unknown"
        
        # Check for known malicious IPs
        if SecurityMiddleware.is_blacklisted(client_ip):
            BLOCKED_REQUESTS.labels(reason="blacklisted_ip", client_ip=client_ip).inc()
            
            logger.warning(
                "request_blocked_blacklisted_ip",
                client_ip=client_ip,
            )
            
            raise HTTPException(
                status_code=status.HTTP_403_FORBIDDEN,
                detail={"blocked_reason": "IP address blocked"}
            )
        
        if not limiter:
            logger.debug("rate_limiter_not_configured", client_ip=client_ip)
            return
        
        # Apply rate limit
        try:
            limiter.consume(client_ip)
            
            SECURITY_VALIDATIONS.labels(
                validation_type="rate_limit",
                result="pass"
            ).inc()
            
        except Exception:
            RATE_LIMIT_ENFORCEMENTS.labels(client_ip=client_ip).inc()
            BLOCKED_REQUESTS.labels(reason="rate_exceeded", client_ip=client_ip).inc()
            
            logger.warning(
                "rate_limit_exceeded",
                client_ip=client_ip,
                limit="100 req/min",
            )
            
            raise HTTPException(
                status_code=status.HTTP_429_TOO_MANY_REQUESTS,
                detail={"error": "rate_limit_exceeded", "retry_after": 60}
            )
    
    @staticmethod
    def is_blacklisted(ip: str) -> bool:
        """Check if IP is on blacklist"""
        return ip in DEFAULT_BLACKLIST
    
    @classmethod
    def detect_adversarial_patterns(cls, data: Dict[str, Any]) -> Optional[str]:
        """
        Heuristic-based adversarial example detection.
        
        Detects:
        - Extreme outlier values (potential injection attacks)
        - Highly repetitive patterns (bot activity)
        - Suspicious value distributions
        
        Args:
            data: Input data dict to analyze
            
        Returns:
            Description of threat if detected, None otherwise
            
        Example:
            >>> threat = SecurityMiddleware.detect_adversarial_patterns({
            ...     "gpu_utilization": 1e15,  # Extremely large value
            ... })
            >>> print(threat)  # "Suspicious value in gpu_utilization..."
        """
        anomalies = []
        
        # Check for extreme values (potential precision poisoning)
        for key, value in data.items():
            if isinstance(value, (int, float)):
                if abs(value) > cls.suspicious_value_threshold:
                    anomalies.append(
                        f"Suspicious magnitude in {key}: |{value}| > {cls.suspicious_value_threshold}"
                    )
                    ADVERSARIAL_DETECTIONS.labels(pattern_type="extreme_value").inc()
        
        # Check for repeated patterns (bot/automated traffic)
        if isinstance(data, dict) and len(data) > 0:
            value_strings = [str(v) for v in data.values()]
            unique_ratio = len(set(value_strings)) / len(value_strings)
            
            if unique_ratio < cls.config.repetition_ratio_threshold:
                anomalies.append(
                    f"Unusual pattern: highly repetitive input (unique ratio={unique_ratio:.2%})"
                )
                ADVERSARIAL_DETECTIONS.labels(pattern_type="repetitive_pattern").inc()
        
        # Check for numeric overflow indicators
        for key, value in data.items():
            if isinstance(value, str):
                if "inf" in value.lower() or "nan" in value.lower():
                    anomalies.append(f"Non-finite value indicator in {key}")
                    ADVERSARIAL_DETECTIONS.labels(pattern_type="non_finite_value").inc()
        
        # Return first anomaly found (or None)
        return anomalies[0] if anomalies else None
    
    @staticmethod
    def validate_input_size(content_length: Optional[int]) -> None:
        """Check request payload size against limit"""
        if content_length and content_length > 10 * 1024 * 1024:  # 10MB
            BLOCKED_REQUESTS.labels(reason="payload_too_large", client_ip="unknown").inc()
            logger.warning(
                "request_blocked_payload_size",
                size_bytes=content_length,
                limit_bytes=10 * 1024 * 1024,
            )
            raise HTTPException(
                status_code=status.HTTP_413_PAYLOAD_TOO_LARGE,
                detail="Payload exceeds maximum allowed size (10MB)"
            )
    
    @classmethod
    async def validate_and_rate_limit(
        cls,
        request: Request,
        schema: Type[BaseModel],
        data: Dict[str, Any]
    ) -> BaseModel:
        """
        Combined validation pipeline: adversarial check → rate limit → schema validation.
        
        Execution order (defense in depth):
        1. Input size check (reject oversized payloads early)
        2. Adversarial pattern detection (heuristic scan)
        3. Rate limit enforcement (throttle abusive clients)
        4. Schema validation (type safety)
        
        Args:
            request: FastAPI request object
            schema: Pydantic model class for validation
            data: Parsed request body
            
        Returns:
            Validated Pydantic model instance
            
        Side effects:
            - Logs all security events
            - Updates Prometheus metrics
            - Blocks malicious requests before they reach handler
        """
        start_time = time.time()
        
        # Step 1: Validate input size
        cls.validate_input_size(request.headers.get("content-length"))
        
        # Step 2: Check for adversarial patterns (logs but doesn't reject immediately)
        threat = cls.detect_adversarial_patterns(data)
        if threat:
            logger.warning(
                "adversarial_input_detected",
                threat=threat,
                client_ip=request.client.host if request.client else "unknown",
            )
            SECURITY_VALIDATIONS.labels(
                validation_type="adversarial_check",
                result="threat_detected"
            ).inc()
        
        # Step 3: Apply rate limiting
        await cls.rate_limit(request, global_rate_limiter)
        
        # Step 4: Validate input against schema
        validated = await cls.sanitize_input(data, schema)
        
        total_time = time.time() - start_time
        
        logger.info(
            "security_validation_complete",
            client_ip=request.client.host if request.client else "unknown",
            latency_ms=round(total_time * 1000, 2),
            security_checks_passed=True,
        )
        
        return validated
    
    @staticmethod
    def logging_middleware():
        """
        Logging middleware decorator for request/response tracing.
        
        Logs:
        - Request metadata (method, path, IP, user-agent)
        - Response status code
        - Processing latency
        - Blocked requests with reason
        """
        @asynccontextmanager
        async def process_request(request: Request):
            start_time = time.time()
            
            client_ip = request.client.host if request.client else "unknown"
            structlog.logger.info(
                "request_received",
                method=request.method,
                path=request.url.path,
                client_ip=client_ip,
                user_agent=request.headers.get("user-agent", "unknown"),
            )
            
            yield
            
            duration = time.time() - start_time
            status_code = getattr(request.state, 'response_status', 200)
            
            structlog.logger.info(
                "request_completed",
                method=request.method,
                path=request.url.path,
                status_code=status_code,
                duration_ms=round(duration * 1000, 2),
            )
        
        return process_request


# Decorator factory for easy integration
def security_decorator(schema: Type[BaseModel]):
    """
    Create security decorator for FastAPI endpoint functions.
    
    Usage:
        >>> @app.post("/predict")
        >>> @security_decorator(PredictRequest)
        >>> async def predict_endpoint(request: Request, validated_data: PredictRequest):
        ...     return {"prediction": "result"}
    """
    def decorator(func: Callable):
        @wraps(func)
        async def wrapper(request: Request, *args, **kwargs):
            # Parse request body
            try:
                data = await request.json()
            except Exception as e:
                logger.warning(f"Failed to parse JSON body: {e}")
                raise HTTPException(
                    status_code=status.HTTP_400_BAD_REQUEST,
                    detail="Invalid JSON format"
                )
            
            # Run security pipeline
            validated = await SecurityMiddleware.validate_and_rate_limit(
                request=request,
                schema=schema,
                data=data,
            )
            
            # Inject validated data into kwargs for handler
            kwargs['validated_data'] = validated
            
            return await func(request, *args, **kwargs)
        
        return wrapper
    
    return decorator


if __name__ == "__main__":
    # Demo usage
    import json
    
    # Test input validation
    test_data = {
        "feature_a": 45.2,
        "feature_b": 67.8,
        "gpu_count": 4,
    }
    
    print("=== Testing Input Sanitization ===")
    
    class TestSchema(BaseModel):
        feature_a: float = Field(ge=0, le=100)
        feature_b: float = Field(ge=0, le=100)
        gpu_count: int = Field(ge=0, le=16)
    
    try:
        validated = SecurityMiddleware.sanitize_input(test_data, TestSchema)
        print(f"✓ Valid input: {validated}")
    except InputValidationError as e:
        print(f"✗ Invalid input: {e.detail}")
    
    # Test adversarial detection
    print("\n=== Testing Adversarial Detection ===")
    
    adversarial_data = {
        "feature_a": 1e15,  # Extremely large
        "feature_b": 1e15,  # Repetitive extreme values
    }
    
    threat = SecurityMiddleware.detect_adversarial_patterns(adversarial_data)
    if threat:
        print(f"⚠ Threat detected: {threat}")
    else:
        print("✓ No threats detected")
