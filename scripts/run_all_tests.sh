#!/bin/bash
# ============================================================================
# CloudAI Fusion Module Test Suite
# Comprehensive test execution for M47/M29/M31/M49/M9 modules
# ============================================================================
# Usage: ./run_all_tests.sh [options]
# Options:
#   --module MODULE     Run tests for specific module (M47, M29, M31, M49, M9)
#   --parallel COUNT    Run tests in parallel
#   --verbose          Enable verbose output
#   --coverage         Generate coverage reports
#   --help             Show this help message
# ============================================================================

set -euo pipefail

# Colors and formatting
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Configuration
MODULES=("M47" "M29" "M31" "M49" "M9")
RUNNING_MODULE=""
PARALLEL_JOBS=4
VERBOSE=false
GENERATE_COVERAGE=false
TEST_FILTER=""
TIMEOUT="30m"

# Counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

# Directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${SCRIPT_DIR}/.."
AI_DIR="${ROOT_DIR}/ai"
LOG_DIR="${ROOT_DIR}/logs/test-$(date +%Y%m%d-%H%M%S)"

# ============================================================================
# Utility Functions
# ============================================================================

log_info() {
    echo -e "${CYAN}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[✓ PASS]${NC} $*"
}

log_error() {
    echo -e "${RED}[✗ FAIL]${NC} $*" >&2
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_module() {
    echo ""
    echo -e "${BOLD}${MAGENTA}========================================${NC}"
    echo -e "${BOLD}${MAGENTA}$*${NC}"
    echo -e "${BOLD}${MAGENTA}========================================${NC}"
    echo ""
}

show_help() {
    cat << EOF
${BOLD}CloudAI Fusion Module Test Suite${NC}

${BOLD}Usage:${NC}
  $0 [OPTIONS]

${BOLD}Options:${NC}
  --module MODULE     Run tests for specific module (M47, M29, M31, M49, M9)
  --parallel COUNT    Run tests in parallel (default: 4)
  --verbose          Enable verbose output
  --coverage         Generate coverage reports
  --filter REGEX      Filter tests by regex pattern
  --timeout DURATION Set test timeout (default: 30m)
  --help             Show this help message

${BOLD}Examples:${NC}
  # Run all tests
  $0

  # Run only M47 tracing tests
  $0 --module M47

  # Run tests with coverage reporting
  $0 --coverage

  # Run in verbose mode with custom filter
  $0 --verbose --filter "TestM47_CrossService"

${BOLD}Modules:${NC}
  M47  - Tracing binaries + Python AI engine integration
  M29  - FastAPI endpoints + model registry services
  M31  - FastAPI endpoints + model registry services
  M49  - K8s operators + chaos testing framework
  M9   - Go quantile libraries for metrics collection

EOF
}

setup_logging() {
    mkdir -p "$LOG_DIR"
    log_info "Logs will be saved to: ${LOG_DIR}"
    
    if [ "$VERBOSE" = true ]; then
        exec >> "${LOG_DIR}/test-output.log" 2>&1
    fi
}

cleanup() {
    log_info "Cleaning up..."
    
    # Kill any background processes
    jobs -p | xargs -r kill 2>/dev/null || true
    
    log_success "Cleanup completed"
}

trap cleanup EXIT

parse_arguments() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --module)
                RUNNING_MODULE="$2"
                shift 2
                ;;
            --parallel)
                PARALLEL_JOBS="$2"
                shift 2
                ;;
            --verbose)
                VERBOSE=true
                shift
                ;;
            --coverage)
                GENERATE_COVERAGE=true
                shift
                ;;
            --filter)
                TEST_FILTER="$2"
                shift 2
                ;;
            --timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            --help)
                show_help
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

select_modules_to_run() {
    if [ -n "$RUNNING_MODULE" ]; then
        if [[ ! " ${MODULES[*]} " =~ " ${RUNNING_MODULE} " ]]; then
            log_error "Invalid module: $RUNNING_MODULE"
            echo "Available modules: ${MODULES[*]}"
            exit 1
        fi
        echo "$RUNNING_MODULE"
    else
        echo "${MODULES[*]}"
    fi
}

report_result() {
    local module=$1
    local status=$2
    
    case $status in
        PASS)
            ((PASSED_TESTS++)) || true
            log_success "✅ $module: All tests passed"
            ;;
        FAIL)
            ((FAILED_TESTS++)) || true
            log_error "❌ $module: Tests failed"
            ;;
        SKIP)
            ((SKIPPED_TESTS++)) || true
            log_warning "⚠️  $module: Skipped"
            ;;
    esac
}

# ============================================================================
# Test Execution Functions
# ============================================================================

run_m47_tests() {
    log_module "M47: Tracing Binaries + Python AI Engine Integration"
    
    cd "$ROOT_DIR"
    
    # Run unit tests with race detection
    log_info "Running M47 unit tests with race detector..."
    if go test -race \
        ./pkg/tracing \
        -run "^TestM47_" \
        -v \
        -timeout "$TIMEOUT" \
        -parallel 4 \
        $(if [ "$GENERATE_COVERAGE" = true ]; then echo "-coverprofile=m47_unit_coverage.out"; fi); then
        
        log_success "M47 Unit Tests: PASSED"
        
        if [ "$GENERATE_COVERAGE" = true ] && [ -f m47_unit_coverage.out ]; then
            log_info "Coverage report generated: m47_unit_coverage.out"
            
            if command -v go >/dev/null 2>&1; then
                go tool cover -func=m47_unit_coverage.out | head -20
            fi
        fi
    else
        log_error "M47 Unit Tests: FAILED"
        return 1
    fi
    
    # Run cross-service E2E tests
    log_info "Running M47 cross-service E2E tests..."
    if go test \
        ./pkg/tracing \
        -run "^TestM47_CrossService|^TestM47_Parallel|^TestMultiSpan" \
        -v \
        -timeout 15m \
        -tags integration; then
        
        log_success "M47 Cross-Service E2E Tests: PASSED"
    else
        log_error "M47 Cross-Service E2E Tests: FAILED"
        return 1
    fi
    
    return 0
}

run_m29_m31_tests() {
    log_module "M29/M31: FastAPI Endpoints + Model Registry Services"
    
    cd "$AI_DIR"
    
    # Run security tests
    log_info "Running M29/M31 security tests..."
    if pytest \
        tests/test_m29_m31_security.py \
        tests/test_model_registry.py \
        -v \
        --tb=short \
        -n auto \
        $(if [ "$GENERATE_COVERAGE" = true ]; then echo "--cov=. --cov-report=term --cov-report=xml:m29_m31_coverage.xml"; fi) \
        $(if [ -n "$TEST_FILTER" ]; then echo "-k \"$TEST_FILTER\""; fi); then
        
        log_success "M29/M31 Security Tests: PASSED"
        
        if [ "$GENERATE_COVERAGE" = true ] && [ -f m29_m31_coverage.xml ]; then
            log_info "Python coverage report generated: m29_m31_coverage.xml"
        fi
    else
        log_error "M29/M31 Security Tests: FAILED"
        return 1
    fi
    
    # Run API endpoint tests
    log_info "Running M29/M31 API endpoint tests..."
    if pytest \
        tests/test_server.py \
        tests/test_api_endpoints.py \
        -v \
        --tb=short \
        -n auto \
        --maxfail=3; then
        
        log_success "M29/M31 API Endpoint Tests: PASSED"
    else
        log_error "M29/M31 API Endpoint Tests: FAILED"
        return 1
    fi
    
    return 0
}

run_m49_tests() {
    log_module "M49: K8s Operators + Chaos Testing Framework"
    
    cd "$ROOT_DIR"
    
    # Run self-healing tests
    log_info "Running M49 self-healing tests..."
    if go test \
        ./pkg/aiops \
        -run "^(TestK8s|TestCircuit|TestSelfHeal)" \
        -v \
        -timeout 15m \
        -parallel 2 \
        $(if [ "$GENERATE_COVERAGE" = true ]; then echo "-coverprofile=m49_aiops_coverage.out"; fi); then
        
        log_success "M49 Self-Healing Tests: PASSED"
        
        if [ "$GENERATE_COVERAGE" = true ] && [ -f m49_aiops_coverage.out ]; then
            log_info "Coverage report generated: m49_aiops_coverage.out"
        fi
    else
        log_error "M49 Self-Healing Tests: FAILED"
        return 1
    fi
    
    # Run chaos engineering tests (isolated mode)
    log_info "Running M49 chaos tests (isolated mode)..."
    if go test \
        ./pkg/chaos \
        -run "^TestCPU|^TestMemory|^TestNetwork|^TestDisk" \
        -v \
        -timeout 20m \
        -parallel 2 \
        -tags isolated \
        $(if [ "$GENERATE_COVERAGE" = true ]; then echo "-coverprofile=m49_chaos_coverage.out"; fi); then
        
        log_success "M49 Chaos Tests: PASSED"
        
        if [ "$GENERATE_COVERAGE" = true ] && [ -f m49_chaos_coverage.out ]; then
            log_info "Coverage report generated: m49_chaos_coverage.out"
        fi
    else
        log_error "M49 Chaos Tests: FAILED"
        return 1
    fi
    
    return 0
}

run_m9_tests() {
    log_module "M9: Go Quantile Libraries for Metrics Collection"
    
    cd "$ROOT_DIR"
    
    # Run benchmarks
    log_info "Running M9 P² & t-digest quantile benchmarks..."
    if go test \
        -bench=M9 \
        -benchmem \
        -count=3 \
        -benchtime=2s \
        ./pkg/quantile \
        > m9_benchmark_results.txt 2>&1; then
        
        log_success "M9 Benchmarks: COMPLETED"
        log_info "Benchmark results:"
        cat m9_benchmark_results.txt
    else
        log_error "M9 Benchmarks: FAILED"
        return 1
    fi
    
    # Run unit tests
    log_info "Running M9 unit tests..."
    if go test \
        ./pkg/quantile \
        -v \
        -timeout 10m \
        $(if [ "$GENERATE_COVERAGE" = true ]; then echo "-coverprofile=m9_coverage.out"; fi); then
        
        log_success "M9 Unit Tests: PASSED"
        
        if [ "$GENERATE_COVERAGE" = true ] && [ -f m9_coverage.out ]; then
            log_info "Coverage report generated: m9_coverage.out"
        fi
    else
        log_error "M9 Unit Tests: FAILED"
        return 1
    fi
    
    return 0
}

# ============================================================================
# Main Execution
# ============================================================================

main() {
    echo -e "${BOLD}${CYAN}"
    echo "=================================================="
    echo "    CloudAI Fusion Module Test Suite v1.0.0"
    echo "=================================================="
    echo -e "${NC}"
    
    parse_arguments "$@"
    
    setup_logging
    
    log_info "Starting test suite at $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    log_info "Parallel jobs: $PARALLEL_JOBS"
    log_info "Verbose mode: $VERBOSE"
    log_info "Coverage enabled: $GENERATE_COVERAGE"
    
    # Select modules to run
    MODULES_TO_RUN=$(select_modules_to_run)
    
    ALL_PASSED=true
    
    # Run tests for each selected module
    for module in $MODULES_TO_RUN; do
        case $module in
            M47)
                if ! run_m47_tests; then
                    ALL_PASSED=false
                fi
                ;;
            M29|M31)
                if ! run_m29_m31_tests; then
                    ALL_PASSED=false
                fi
                ;;
            M49)
                if ! run_m49_tests; then
                    ALL_PASSED=false
                fi
                ;;
            M9)
                if ! run_m9_tests; then
                    ALL_PASSED=false
                fi
                ;;
        esac
    done
    
    # Summary
    echo ""
    echo -e "${BOLD}${CYAN}========================================${NC}"
    echo -e "${BOLD}${CYAN}     Test Suite Summary${NC}"
    echo -e "${BOLD}${CYAN}========================================${NC}"
    echo ""
    echo -e "Total Modules: ${#MODULES[@]}"
    echo -e "Passed: ${GREEN}${PASSED_TESTS}${NC}"
    echo -e "Failed: ${RED}${FAILED_TESTS}${NC}"
    echo -e "Skipped: ${YELLOW}${SKIPPED_TESTS}${NC}"
    echo ""
    
    if [ "$ALL_PASSED" = true ]; then
        echo -e "${GREEN}${BOLD}✅ All tests passed!${NC}"
        echo ""
        
        if [ "$GENERATE_COVERAGE" = true ]; then
            echo -e "${BOLD}Coverage reports generated:${NC}"
            ls -lh ${ROOT_DIR}/*coverage.out ${ROOT_DIR}/*_coverage.out ${ROOT_DIR}/*.xml 2>/dev/null || true
        fi
        
        exit 0
    else
        echo -e "${RED}${BOLD}❌ $FAILED_TESTS module(s) failed${NC}"
        echo ""
        log_error "Some tests failed. Check logs at: $LOG_DIR"
        exit 1
    fi
}

# Entry point
main "$@"
