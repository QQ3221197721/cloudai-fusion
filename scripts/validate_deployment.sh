#!/bin/bash
# ============================================================================
# Post-Deployment Validation Script for CloudAI Fusion
# Comprehensive validation of deployment health and functionality
# ============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Configuration
NAMESPACE="${NAMESPACE:-production}"
DEPLOYMENT_NAME="${DEPLOYMENT_NAME:-cloudai-fusion-ai}"
CHECK_TIMEOUT_MINUTES="${CHECK_TIMEOUT:-5}"
VALIDATION_REPORT="validation-report-$(date +%Y%m%d-%H%M%S).json"

# Counters
TOTAL_CHECKS=0
PASSED_CHECKS=0
FAILED_CHECKS=0
WARNINGS=0

# Arrays for detailed tracking
declare -a CHECK_RESULTS=()
declare -a FAILED_VALIDATIONS=()
declare -a WARNINGS_LIST=()

# ============================================================================
# Utility Functions
# ============================================================================

log_info() {
    echo -e "${CYAN}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[✓ PASS]${NC} $*"
    ((PASSED_CHECKS++)) || true
    TOTAL_CHECKS=$((TOTAL_CHECKS + 1))
}

log_error() {
    echo -e "${RED}[✗ FAIL]${NC} $*"
    ((FAILED_CHECKS++)) || true
    TOTAL_CHECKS=$((TOTAL_CHECKS + 1))
    FAILED_VALIDATIONS+=("$*")
}

log_warning() {
    echo -e "${YELLOW}[⚠️ WARNING]${NC} $*"
    ((WARNINGS++)) || true
    TOTAL_CHECKS=$((TOTAL_CHECKS + 1))
    WARNINGS_LIST+=("$*")
}

log_module() {
    echo ""
    echo -e "${BOLD}${BLUE}========================================${NC}"
    echo -e "${BOLD}${BLUE}$*${NC}"
    echo -e "${BOLD}${BLUE}========================================${NC}"
    echo ""
}

show_help() {
    cat << EOF
${BOLD}CloudAI Fusion Deployment Validator${NC}

${BOLD}Usage:${NC}
  $0 [OPTIONS]

${BOLD}Options:${NC}
  --namespace NAMESPACE   Kubernetes namespace (default: production)
  --deployment NAME       Deployment name (default: cloudai-fusion-ai)
  --timeout MINUTES       Validation timeout in minutes (default: 5)
  --help                  Show this help message

${BOLD}Validation Checks:${NC}
  ✓ Pod status and readiness
  ✓ Service connectivity  
  ✓ Health endpoint response
  ✓ Metrics collection
  ✓ Logging integration
  ✓ Resource utilization
  ✓ Container restart count

${BOLD}Output:${NC}
  - JSON report saved to: $VALIDATION_REPORT
  - Console summary with pass/fail statistics

EOF
}

cleanup() {
    local exit_code=$?
    
    if [ $exit_code -ne 0 ]; then
        log_error "Validation failed with exit code $exit_code"
    fi
    
    generate_report
    
    if [ -n "$GITHUB_OUTPUT" ]; then
        echo "passed_checks=$PASSED_CHECKS" >> $GITHUB_OUTPUT
        echo "failed_checks=$FAILED_CHECKS" >> $GITHUB_OUTPUT
    fi
    
    exit $exit_code
}

trap cleanup EXIT

check_kubectl_available() {
    log_module "Prerequisites Check"
    
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        return 1
    fi
    
    log_success "kubectl available"
    
    # Check cluster connection
    if kubectl cluster-info &> /dev/null; then
        log_success "Kubernetes cluster connected"
    else
        log_error "Cannot connect to Kubernetes cluster"
        return 1
    fi
    
    return 0
}

check_pod_status() {
    log_module "Pod Status Validation"
    
    local pods=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME -o jsonpath='{.items[*].metadata.name}')
    
    if [ -z "$pods" ]; then
        log_error "No pods found for deployment $DEPLOYMENT_NAME"
        return 1
    fi
    
    log_info "Found pods: $pods"
    
    for pod in $pods; do
        local phase=$(kubectl get pod $pod -n $NAMESPACE -o jsonpath='{.status.phase}')
        
        if [ "$phase" != "Running" ]; then
            log_warning "Pod $pod is not running (phase: $phase)"
            continue
        fi
        
        # Check container statuses
        local ready=$(kubectl get pod $pod -n $NAMESPACE -o jsonpath='{.status.containerStatuses[*].ready}')
        local terminated=$(kubectl get pod $pod -n $NAMESPACE -o jsonpath='{.status.containerStatuses[*].lastState.terminated}')
        
        if [ -n "$terminated" ] && [ "$terminated" != "{}" ]; then
            log_warning "Pod $pod has terminated containers"
        elif [ "$ready" == "false" ]; then
            log_error "Pod $pod has unready containers"
        else
            log_success "Pod $pod is healthy"
        fi
    done
    
    return 0
}

check_readiness_probe() {
    log_module "Readiness Probe Validation"
    
    local start_time=$(date +%s)
    local timeout_seconds=$((CHECK_TIMEOUT_MINUTES * 60))
    
    while true; do
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        
        if [ $elapsed -gt $timeout_seconds ]; then
            log_error "Readiness probe check timed out after ${CHECK_TIMEOUT_MINUTES} minutes"
            return 1
        fi
        
        local pods=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME \
            -o jsonpath='{.items[*].metadata.name} {.items[*].status.conditions[?(@.type=="Ready")].status}')
        
        local ready_count=$(echo "$pods" | grep "True" | wc -w)
        local total_pods=$(echo "$pods" | wc -w)
        
        if [ $ready_count -eq $total_pods ] && [ $ready_count -gt 0 ]; then
            log_success "All pods are ready ($ready_count/$total_pods)"
            return 0
        fi
        
        log_info "Waiting for pods to become ready... ($ready_count/$total_pods)"
        sleep 10
    done
}

check_health_endpoint() {
    log_module "Health Endpoint Validation"
    
    # Get service endpoint
    local service_name="${DEPLOYMENT_NAME}-ai"
    
    local port=$(kubectl get svc -n $NAMESPACE $service_name -o jsonpath='{.spec.ports[?(@.name=="http")].port}' 2>/dev/null || echo "8000")
    
    # Try to access via kubectl port-forward
    log_info "Checking health endpoint at http://$service_name:$port/health"
    
    local health_url="http://$service_name.$NAMESPACE.svc:$port/health"
    
    # Use kubectl exec to make HTTP request
    local pod_name=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME -o jsonpath='{.items[0].metadata.name}')
    
    local health_response=$(kubectl exec $pod_name -n $NAMESPACE -- \
        curl -sf http://localhost:8000/health 2>&1 || true)
    
    if [ -n "$health_response" ]; then
        log_success "Health endpoint responding correctly"
        log_info "Response: $(echo "$health_response" | head -c 200)"
        return 0
    else
        log_error "Health endpoint not accessible"
        return 1
    fi
}

check_metrics_collection() {
    log_module "Metrics Collection Validation"
    
    local metrics_port=$(kubectl get deploy $DEPLOYMENT_NAME -n $NAMESPACE \
        -o jsonpath='{.spec.template.spec.containers[0].ports[*].containerPort}' 2>/dev/null || echo "9090")
    
    # Check if Prometheus annotations exist
    local has_prometheus=$(kubectl get deploy $DEPLOYMENT_NAME -n $NAMESPACE \
        -o jsonpath='{.spec.template.metadata.annotations."prometheus\\.io/scrape"}')
    
    if [ "$has_prometheus" == "true" ]; then
        log_success "Prometheus scraping configured"
    else
        log_warning "No Prometheus annotations detected"
    fi
    
    # Check metrics endpoint inside pod
    local pod_name=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME -o jsonpath='{.items[0].metadata.name}')
    
    local metrics_response=$(kubectl exec $pod_name -n $NAMESPACE -- \
        curl -sf http://localhost:9090/metrics 2>&1 | head -c 500 || true)
    
    if [ -n "$metrics_response" ]; then
        log_success "Metrics endpoint accessible"
        
        # Check for expected metrics
        if echo "$metrics_response" | grep -q "http_requests_total"; then
            log_success "HTTP metrics present"
        fi
        
        if echo "$metrics_response" | grep -q "process_resident_memory_bytes"; then
            log_success "Process metrics present"
        fi
    else
        log_warning "Metrics endpoint not accessible yet"
    fi
}

check_logging_integration() {
    log_module "Logging Integration Validation"
    
    local pod_name=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME -o jsonpath='{.items[0].metadata.name}')
    
    # Check logs from last 5 minutes
    local log_count=$(kubectl logs $pod_name -n $NAMESPACE --tail=100 2>&1 | wc -l || echo "0")
    
    if [ "$log_count" -gt 0 ]; then
        log_success "Logs accessible (${log_count} lines)"
        
        # Check for errors in recent logs
        local error_logs=$(kubectl logs $pod_name -n $NAMESPACE --since=5m 2>&1 | grep -ci "error" || echo "0")
        
        if [ "$error_logs" -lt 5 ]; then
            log_success "Low error rate in logs ($error_logs errors)"
        else
            log_warning "High error rate in logs ($error_logs errors)"
        fi
    else
        log_warning "No logs found"
    fi
}

check_resource_utilization() {
    log_module "Resource Utilization Validation"
    
    local pod_name=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME -o jsonpath='{.items[0].metadata.name}')
    
    # Get resource usage
    local resources=$(kubectl top pod $pod_name -n $NAMESPACE 2>/dev/null || true)
    
    if [ -n "$resources" ]; then
        log_success "Resource metrics available:"
        log_info "$resources"
    else
        log_warning "Resource metrics not available (metrics-server may not be installed)"
    fi
    
    # Check requests vs limits
    local cpu_request=$(kubectl get deploy $DEPLOYMENT_NAME -n $NAMESPACE \
        -o jsonpath='{.spec.template.spec.containers[0].resources.requests.cpu}' || echo "unknown")
    
    local memory_limit=$(kubectl get deploy $DEPLOYMENT_NAME -n $NAMESPACE \
        -o jsonpath='{.spec.template.spec.containers[0].resources.limits.memory}' || echo "unknown")
    
    if [ "$cpu_request" != "unknown" ]; then
        log_success "CPU requests configured: $cpu_request"
    fi
    
    if [ "$memory_limit" != "unknown" ]; then
        log_success "Memory limits configured: $memory_limit"
    fi
}

check_container_restarts() {
    log_module "Container Restart Count Validation"
    
    local pod_name=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME -o jsonpath='{.items[0].metadata.name}')
    
    local restart_count=$(kubectl get pod $pod_name -n $NAMESPACE \
        -o jsonpath='{.status.containerStatuses[0].restartCount}' || echo "-1")
    
    if [ "$restart_count" -eq 0 ]; then
        log_success "Container stable (no restarts)"
    elif [ "$restart_count" -lt 3 ]; then
        log_warning "Container restarted $restart_count time(s)"
    else
        log_error "Container restarted too many times ($restart_count)"
    fi
    
    # Check last reason for restart
    local state=$(kubectl get pod $pod_name -n $NAMESPACE \
        -o jsonpath='{.status.containerStatuses[0].lastState.why}' || echo "N/A")
    
    if [ "$state" != "N/A" ] && [ "$state" != "{}}" ]; then
        log_warning "Last restart reason: $state"
    fi
}

check_circuit_breaker() {
    log_module "Circuit Breaker Status Validation"
    
    local pod_name=$(kubectl get pods -n $NAMESPACE -l app=$DEPLOYMENT_NAME -o jsonpath='{.items[0].metadata.name}')
    
    local circuit_status=$(kubectl exec $pod_name -n $NAMESPACE -- \
        curl -sf http://localhost:8000/circuit-breaker-status 2>&1 || true)
    
    if [ -n "$circuit_status" ]; then
        log_success "Circuit breaker endpoints available"
        
        if echo "$circuit_status" | grep -qi "closed"; then
            log_success "Circuit breaker closed (healthy)"
        else
            log_warning "Circuit breaker open or half-open"
        fi
    else
        log_warning "Circuit breaker metrics not available"
    fi
}

generate_report() {
    log_module "Generating Validation Report"
    
    local timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local success_rate=$(awk "BEGIN {printf \"%.2f\", ($PASSED_CHECKS / $TOTAL_CHECKS) * 100}")
    
    cat > $VALIDATION_REPORT << EOF
{
  "report_generated_at": "$timestamp",
  "configuration": {
    "namespace": "$NAMESPACE",
    "deployment": "$DEPLOYMENT_NAME",
    "timeout_minutes": $CHECK_TIMEOUT_MINUTES
  },
  "summary": {
    "total_checks": $TOTAL_CHECKS,
    "passed_checks": $PASSED_CHECKS,
    "failed_checks": $FAILED_CHECKS,
    "warnings": $WARNINGS,
    "success_rate_percent": $success_rate
  },
  "status": "$(if [ $FAILED_CHECKS -eq 0 ]; then echo "PASS"; else echo "FAIL"; fi)",
  "failed_validations": [
$(for fail in "${FAILED_VALIDATIONS[@]:-}"; do echo "    \"$fail\","; done | sed '$ s/,$//')
  ],
  "warnings": [
$(for warn in "${WARNINGS_LIST[@]:-}"; do echo "    \"$warn\","; done | sed '$ s/,$//')
  ],
  "recommendations": []
}
EOF
    
    log_success "Report saved to: $VALIDATION_REPORT"
}

print_summary() {
    log_module "Validation Summary"
    
    echo ""
    echo "┌─────────────────────────────────────────────┐"
    echo "│         Validation Results Summary          │"
    echo "└─────────────────────────────────────────────┘"
    echo ""
    echo -e "Total Checks:    ${TOTAL_CHECKS}"
    echo -e "Passed:          ${GREEN}${PASSED_CHECKS}${NC}"
    echo -e "Failed:          ${RED}${FAILED_CHECKS}${NC}"
    echo -e "Warnings:        ${YELLOW}${WARNINGS}${NC}"
    echo ""
    
    local success_rate=$(awk "BEGIN {printf \"%.1f\", ($PASSED_CHECKS / $TOTAL_CHECKS) * 100}")
    echo -e "Success Rate:    ${success_rate}%"
    echo ""
    
    if [ $FAILED_CHECKS -eq 0 ]; then
        echo -e "${GREEN}✅ ALL VALIDATIONS PASSED${NC}"
        echo ""
        
        if [ $WARNINGS -gt 0 ]; then
            echo -e "${YELLOW}⚠️  Warnings detected:${NC}"
            for warn in "${WARNINGS_LIST[@]}"; do
                echo "   • $warn"
            done
            echo ""
        fi
        
        echo "Deployment is healthy and ready for production traffic."
        exit 0
    else
        echo -e "${RED}❌ SOME VALIDATIONS FAILED${NC}"
        echo ""
        
        if [ ${#FAILED_VALIDATIONS[@]} -gt 0 ]; then
            echo -e "${RED}Failed validations:${NC}"
            for fail in "${FAILED_VALIDATIONS[@]}"; do
                echo "   • $fail"
            done
            echo ""
        fi
        
        echo "Review failed checks and remediate before proceeding."
        exit 1
    fi
}

main() {
    local help_flag=false
    
    while [[ $# -gt 0 ]]; do
        case $1 in
            --namespace)
                NAMESPACE="$2"
                shift 2
                ;;
            --deployment)
                DEPLOYMENT_NAME="$2"
                shift 2
                ;;
            --timeout)
                CHECK_TIMEOUT_MINUTES="$2"
                shift 2
                ;;
            --help|-h)
                help_flag=true
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                exit 1
                ;;
        esac
    done
    
    if [ "$help_flag" = true ]; then
        show_help
        exit 0
    fi
    
    echo -e "${BOLD}${CYAN}"
    echo "============================================================"
    echo "    CloudAI Fusion Post-Deployment Validator v1.0.0"
    echo "============================================================"
    echo -e "${NC}"
    
    echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "Namespace: $NAMESPACE"
    echo "Deployment: $DEPLOYMENT_NAME"
    echo "Timeout: ${CHECK_TIMEOUT_MINUTES} minutes"
    echo ""
    
    # Run all validations
    check_kubectl_available || exit 1
    check_pod_status
    check_readiness_probe
    check_health_endpoint
    check_metrics_collection
    check_logging_integration
    check_resource_utilization
    check_container_restarts
    check_circuit_breaker
    
    print_summary
}

main "$@"
