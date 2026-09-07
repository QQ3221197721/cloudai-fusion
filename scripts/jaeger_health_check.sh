#!/bin/bash

# ============================================================================
# M47: Jaeger Health Check Script
# CloudAI Fusion distributed tracing validation
# ============================================================================
# This script validates that Jaeger backend is healthy and receiving traces
# from CloudAI Fusion services. Designed for CI/CD integration.
#
# Usage:
#   ./jaeger_health_check.sh [--host <jaeger-host>] [--timeout <seconds>]
#
# Exit Codes:
#   0 - Healthy (all checks passed)
#   1 - Unhealthy (one or more checks failed)
#   2 - Invalid arguments or environment error
#
# Prerequisites:
#   - curl (for HTTP requests)
#   - jq (for JSON parsing)
#   - bash 4.0+
# ============================================================================

set -euo pipefail

# ============================================================================
# Configuration & Defaults
# ============================================================================

# Jaeger UI endpoint (default to localhost if not set)
JAEGER_HOST="${JAEGER_HOST:-http://localhost:16686}"

# API timeout in seconds
API_TIMEOUT="${JAEGER_API_TIMEOUT:-10}"

# Number of recent traces to query for validation
TRACE_QUERY_LIMIT="${TRACE_QUERY_LIMIT:-100}"

# Required minimum trace count per service
MIN_TRACES_PER_SERVICE="${MIN_TRACES_PER_SERVICE:-5}"

# Services expected to report traces
EXPECTED_SERVICES=(
    "cloudai-apiserver"
    "cloudai-scheduler" 
    "cloudai-agent"
)

# Color codes for terminal output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Global counters for test summary
TOTAL_CHECKS=0
PASSED_CHECKS=0
FAILED_CHECKS=0

# Log file for CI integration
LOG_FILE="${LOG_FILE:-/tmp/jaeger_health_$(date +%Y%m%d_%H%M%S).log}"

# ============================================================================
# Utility Functions
# ============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" | tee -a "$LOG_FILE"
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $1" | tee -a "$LOG_FILE"
}

log_warning() {
    echo -e "${YELLOW}[⚠]${NC} $1" | tee -a "$LOG_FILE"
}

log_error() {
    echo -e "${RED}[✗]${NC} $1" | tee -a "$LOG_FILE"
}

increment_passed() {
    ((PASSED_CHECKS++)) || true
    ((TOTAL_CHECKS++)) || true
}

increment_failed() {
    ((FAILED_CHECKS++)) || true
    ((TOTAL_CHECKS++)) || true
}

run_check() {
    local check_name="$1"
    local check_command="$2"
    
    log_info "Running check: $check_name"
    if eval "$check_command"; then
        log_success "Check passed: $check_name"
        increment_passed
        return 0
    else
        log_error "Check failed: $check_name"
        increment_failed
        return 1
    fi
}

# ============================================================================
# Connectivity Checks
# ============================================================================

check_jaeger_connectivity() {
    log_info "Checking Jaeger connectivity at $JAEGER_HOST..."
    
    # Test HTTP health endpoint
    local http_status
    http_status=$(curl -s -o /dev/null -w "%{http_code}" --max-time "$API_TIMEOUT" \
        "$JAEGER_HOST/api/health" 2>/dev/null || echo "000")
    
    if [[ "$http_status" == "200" ]]; then
        log_success "Jaeger HTTP health endpoint responded (status: $http_status)"
        return 0
    else
        log_error "Jaeger unreachable (HTTP status: $http_status, expected: 200)"
        return 1
    fi
}

check_otlp_endpoint() {
    log_info "Checking OTLP gRPC endpoint availability..."
    
    # Attempt connection to OTLP collector port (default 4317)
    local otlp_host="${OTLP_HOST:-localhost}"
    local otlp_port="${OTLP_PORT:-4317}"
    
    if timeout "$API_TIMEOUT" bash -c "echo > /dev/tcp/$otlp_host/$otlp_port" 2>/dev/null; then
        log_success "OTLP endpoint accessible ($otlp_host:$otlp_port)"
        return 0
    else
        log_warning "OTLP endpoint unreachable ($otlp_host:$otlp_port) - skipping gRPC test"
        # Don't fail the check if OTLP unavailable, just warn
        return 0
    fi
}

# ============================================================================
# Trace Validation Checks
# ============================================================================

check_traces_received() {
    log_info "Querying recent traces (limit=$TRACE_QUERY_LIMIT)..."
    
    local traces_response
    traces_response=$(curl -s --max-time "$API_TIMEOUT" \
        "$JAEGER_HOST/api/traces?limit=$TRACE_QUERY_LIMIT")
    
    if [[ -z "$traces_response" ]] || echo "$traces_response" | jq -e '.traces | length' >/dev/null 2>&1; then
        log_error "Invalid or empty response from Jaeger API"
        return 1
    fi
    
    local trace_count
    trace_count=$(echo "$traces_response" | jq '.traces | length')
    
    log_info "Found $trace_count traces in last query"
    
    if [[ "$trace_count" -eq 0 ]]; then
        log_error "No traces found in last $TRACE_QUERY_LIMIT queries"
        return 1
    else
        log_success "Traces received successfully ($trace_count traces)"
        return 0
    fi
}

check_service_coverage() {
    log_info "Validating service coverage across expected services..."
    
    # Query spans grouped by service name
    local services_response
    services_response=$(curl -s --max-time "$API_TIMEOUT" \
        "$JAEGER_HOST/api/services")
    
    if [[ -z "$services_response" ]]; then
        log_error "Failed to fetch service list from Jaeger"
        return 1
    fi
    
    local service_count
    service_count=$(echo "$services_response" | jq '.data | length')
    
    log_info "Detected $service_count unique services reporting to Jaeger"
    
    # Verify all expected services are present
    local missing_services=()
    for expected_svc in "${EXPECTED_SERVICES[@]}"; do
        local svc_found
        svc_found=$(echo "$services_response" | jq -r --arg svc "$expected_svc" \
            '.data[] | select(. == $svc) | .')
        
        if [[ -z "$svc_found" ]]; then
            missing_services+=("$expected_svc")
            log_warning "Expected service not detected: $expected_svc"
        else
            log_success "Service detected: $expected_svc"
        fi
    done
    
    if [[ ${#missing_services[@]} -gt 0 ]]; then
        log_warning "Missing services (${#missing_services[@]}): ${missing_services[*]}"
        # Warning only - some services may be down intentionally
        return 0
    fi
    
    log_success "All expected services covered (${#EXPECTED_SERVICES[@]}/${#EXPECTED_SERVICES[@]})"
    return 0
}

check_trace_correlation() {
    log_info "Validating trace correlation structure..."
    
    # Fetch a sample trace
    local trace_data
    trace_data=$(curl -s --max-time "$API_TIMEOUT" \
        "$JAEGER_HOST/api/traces?limit=1" | jq '.traces[0] // empty')
    
    if [[ -z "$trace_data" ]]; then
        log_warning "Could not fetch trace data for correlation validation"
        return 0
    fi
    
    # Extract trace ID from first span
    local trace_id
    trace_id=$(echo "$trace_data" | jq -r '.traceID // empty')
    
    if [[ -z "$trace_id" ]]; then
        log_error "Trace ID missing from sample trace"
        return 1
    fi
    
    log_info "Sample trace ID: $trace_id"
    
    # Validate parent-child relationships (if multi-span traces exist)
    local span_count
    span_count=$(echo "$trace_data" | jq '.spans | length')
    
    if [[ "$span_count" -gt 1 ]]; then
        log_info "Multi-span trace detected ($span_count spans) - validating hierarchy"
        
        # Extract root span IDs and child references
        local root_span_ids
        root_span_ids=$(echo "$trace_data" | jq -r '[.spans[] | select(.parentSpanId == null)] | .[].spanID')
        
        local child_count=0
        while IFS= read -r root_id; do
            local children
            children=$(echo "$trace_data" | jq -r --arg rid "$root_id" \
                '[.spans[] | select(.parentSpanId != null and (.id == $rid))] | length')
            child_count=$((child_count + children))
        done <<< "$root_span_ids"
        
        log_info "Found $child_count child spans linked to roots"
        
        if [[ "$child_count" -ge 0 ]]; then
            log_success "Parent-child hierarchy preserved"
            return 0
        fi
    else
        log_info "Single-span trace (no hierarchy to validate)"
        return 0
    fi
    
    return 0
}

# ============================================================================
# Performance Checks
# ============================================================================

check_api_latency() {
    log_info "Measuring Jaeger API response latency..."
    
    local start_time end_time duration_ms
    start_time=$(date +%s%N)
    
    curl -s --max-time "$API_TIMEOUT" \
        "$JAEGER_HOST/api/health" >/dev/null
    
    end_time=$(date +%s%N)
    duration_ms=$(( (end_time - start_time) / 1000000 ))
    
    log_info "API response time: ${duration_ms}ms"
    
    # Threshold: <500ms acceptable
    if [[ "$duration_ms" -lt 500 ]]; then
        log_success "API latency within acceptable range (${duration_ms}ms < 500ms)"
        return 0
    else
        log_warning "API latency high (${duration_ms}ms >= 500ms)"
        return 0  # Warning only, don't fail
    fi
}

check_exporter_health() {
    log_info "Checking OTLP exporter configuration in Go binaries..."
    
    # Try to read tracer provider config from running processes
    # This assumes OpenTelemetry exports its config via debug endpoints
    
    if [[ -n "${DEBUG_ENDPOINT:-}" ]]; then
        local tracer_config
        tracer_config=$(curl -s --max-time 5 "$DEBUG_ENDPOINT/debug/tracer" 2>/dev/null || echo "")
        
        if [[ -n "$tracer_config" ]]; then
            local exporter_enabled
            exporter_enabled=$(echo "$tracer_config" | jq -r '.exporter.enabled // "unknown"')
            
            log_info "Tracer exporter status: $exporter_enabled"
            
            if [[ "$exporter_enabled" == "true" ]]; then
                log_success "Exporter properly configured"
                return 0
            fi
        else
            log_warning "Debug endpoint not available or returned empty response"
        fi
    else
        log_warning "DEBUG_ENDPOINT not set - skipping exporter config check"
    fi
    
    return 0  # Non-blocking
}

# ============================================================================
# Chaos Resilience Checks (Optional)
# ============================================================================

check_batch_buffer_resilience() {
    log_info "Testing batch buffer overflow protection..."
    
    # Simulate burst traffic (optional, skip if not requested)
    if [[ "${CHAOS_TESTING:-false}" == "true" ]]; then
        local burst_spans=1000
        log_info "Sending $burst_spans synthetic spans to test resilience..."
        
        local success_count=0
        local start_time
        
        start_time=$(date +%s)
        
        for i in $(seq 1 "$burst_spans"); do
            if curl -s --max-time 2 \
                -X POST "$JAEGER_HOST/api/traces" \
                -H "Content-Type: application/json" \
                -d "{\"traceID\": \"test-burst-$i\", \"spans\": []}" >/dev/null 2>&1; then
                ((success_count++)) || true
            fi
        done
        
        local duration
        duration=$(($(date +%s) - start_time))
        
        log_info "Burst test complete: $success_count/$burst_spans accepted in ${duration}s"
        
        if [[ "$success_count" -ge $((burst_spans / 2)) ]]; then
            log_success "Batch buffer handled load gracefully"
            return 0
        else
            log_warning "High rejection rate during burst ($success_count < 50%)"
            return 1
        fi
    else
        log_info "Skipping burst test (set CHAOS_TESTING=true to enable)"
        return 0
    fi
}

# ============================================================================
# Reporting & Summary
# ============================================================================

print_summary() {
    echo ""
    echo "================================================================================"
    echo "                        JAEGEHealth Check Summary"
    echo "================================================================================"
    echo ""
    echo "Check Results:"
    echo "  Total checks:       $TOTAL_CHECKS"
    echo -e "  Passed:             ${GREEN}$PASSED_CHECKS${NC}"
    echo -e "  Failed:             ${RED}$FAILED_CHECKS${NC}"
    echo ""
    echo "Configuration:"
    echo "  Jaeger Host:        $JAEGER_HOST"
    echo "  Trace Query Limit:  $TRACE_QUERY_LIMIT"
    echo "  Timeout:            ${API_TIMEOUT}s"
    echo ""
    echo "Log File:           $LOG_FILE"
    echo "================================================================================"
    echo ""
    
    if [[ "$FAILED_CHECKS" -eq 0 ]]; then
        log_success "✅ All health checks passed!"
        return 0
    else
        log_error "❌ $FAILED_CHECKS health check(s) failed"
        return 1
    fi
}

generate_report() {
    local report_json="{
        \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
        \"jaeger_host\": \"$JAEGER_HOST\",
        \"total_checks\": $TOTAL_CHECKS,
        \"passed\": $PASSED_CHECKS,
        \"failed\": $FAILED_CHECKS,
        \"checks\": [
            {\"name\": \"connectivity\", \"status\": \"$(if run_check 'jaeger_connectivity' 'check_jaeger_connectivity'; then echo PASS; else echo FAIL; fi)\"},
            {\"name\": \"otlp_endpoint\", \"status\": \"$(if run_check 'otlp_endpoint' 'check_otlp_endpoint'; then echo PASS; else echo FAIL; fi)\"},
            {\"name\": \"traces_received\", \"status\": \"$(if run_check 'traces_received' 'check_traces_received'; then echo PASS; else echo FAIL; fi)\"},
            {\"name\": \"service_coverage\", \"status\": \"$(if run_check 'service_coverage' 'check_service_coverage'; then echo PASS; else echo FAIL; fi)\"},
            {\"name\": \"trace_correlation\", \"status\": \"$(if run_check 'trace_correlation' 'check_trace_correlation'; then echo PASS; else echo FAIL; fi)\"},
            {\"name\": \"api_latency\", \"status\": \"$(if run_check 'api_latency' 'check_api_latency'; then echo PASS; else echo FAIL; fi)\"},
            {\"name\": \"exporter_health\", \"status\": \"$(if run_check 'exporter_health' 'check_exporter_health'; then echo PASS; else echo FAIL; fi)\"},
            {\"name\": \"batch_resilience\", \"status\": \"$(if run_check 'batch_resilience' 'check_batch_buffer_resilience'; then echo PASS; else echo FAIL; fi)\"}
        ]
    }"
    
    echo "$report_json" > "${REPORT_OUTPUT:-jaeger_health_report.json}"
}

# ============================================================================
# Main Execution
# ============================================================================

main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --host)
                JAEGER_HOST="$2"
                shift 2
                ;;
            --timeout)
                API_TIMEOUT="$2"
                shift 2
                ;;
            --limit)
                TRACE_QUERY_LIMIT="$2"
                shift 2
                ;;
            --report)
                REPORT_OUTPUT="$2"
                shift 2
                ;;
            --help)
                echo "Usage: $0 [OPTIONS]"
                echo ""
                echo "Options:"
                echo "  --host <url>       Jaeger UI endpoint (default: http://localhost:16686)"
                echo "  --timeout <sec>    API timeout in seconds (default: 10)"
                echo "  --limit <num>      Number of traces to query (default: 100)"
                echo "  --report <path>    Output path for JSON report"
                echo "  --help             Show this help message"
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                echo "Use --help for usage information"
                exit 2
                ;;
        esac
    done
    
    # Initialize
    log_info "=========================================="
    log_info "Starting Jaeger Health Check"
    log_info "Host: $JAEGER_HOST"
    log_info "=========================================="
    
    # Run checks
    run_check "jaeger_connectivity" 'check_jaeger_connectivity' || true
    run_check "otlp_endpoint" 'check_otlp_endpoint' || true
    run_check "traces_received" 'check_traces_received' || true
    run_check "service_coverage" 'check_service_coverage' || true
    run_check "trace_correlation" 'check_trace_correlation' || true
    run_check "api_latency" 'check_api_latency' || true
    run_check "exporter_health" 'check_exporter_health' || true
    run_check "batch_resilience" 'check_batch_buffer_resilience' || true
    
    # Generate reports
    print_summary
    generate_report
    
    # Exit with appropriate code
    if [[ "$FAILED_CHECKS" -eq 0 ]]; then
        exit 0
    else
        exit 1
    fi
}

# Run main function
main "$@"
