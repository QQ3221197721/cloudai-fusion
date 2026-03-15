#!/usr/bin/env bash
# ============================================================================
# CloudAI Fusion - Unified Health Check & Diagnostic Tool (Linux/macOS)
# ============================================================================
# Usage:
#   scripts/diagnose.sh              Full diagnostic
#   scripts/diagnose.sh --quick      Quick health check only
#   scripts/diagnose.sh --logs       Show recent error logs
#   scripts/diagnose.sh --fix        Attempt auto-fix common issues
#   scripts/diagnose.sh --json       Output JSON report
# ============================================================================
set -uo pipefail

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
DIM='\033[2m'
NC='\033[0m'

MODE="full"
PASS=0
FAIL=0
WARN=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --quick) MODE="quick"; shift ;;
        --logs)  MODE="logs"; shift ;;
        --fix)   MODE="fix"; shift ;;
        --json)  MODE="json"; shift ;;
        --help|-h)
            echo "Usage: scripts/diagnose.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --quick    Quick health check (containers + endpoints only)"
            echo "  --logs     Show recent error logs from all services"
            echo "  --fix      Attempt auto-fix common issues"
            echo "  --json     Output JSON-formatted report"
            echo "  --help     Show this help"
            exit 0 ;;
        *) echo -e "${RED}Unknown: $1${NC}"; exit 1 ;;
    esac
done

pass() { ((PASS++)); echo -e "  ${GREEN}[PASS]${NC} $1"; }
fail() { ((FAIL++)); echo -e "  ${RED}[FAIL]${NC} $1"; }
warn() { ((WARN++)); echo -e "  ${YELLOW}[WARN]${NC} $1"; }
info() { echo -e "  ${DIM}[INFO]${NC} $1"; }

check_http() {
    local name="$1" url="$2"
    if curl -sf --max-time 3 "$url" > /dev/null 2>&1; then
        pass "$name: healthy"
    else
        fail "$name: unreachable ($url)"
    fi
}

check_tcp() {
    local name="$1" host="$2" port="$3"
    if (echo > "/dev/tcp/$host/$port") 2>/dev/null; then
        pass "$name: port $port open"
    elif nc -z -w2 "$host" "$port" 2>/dev/null; then
        pass "$name: port $port open"
    else
        fail "$name: port $port closed"
    fi
}

echo ""
echo -e "${CYAN}=============================================================="
echo "  CloudAI Fusion - Deployment Diagnostics"
echo "  Mode: $MODE"
echo "  Time: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo -e "==============================================================${NC}"
echo ""

# ========================================================================
# Section 1: Prerequisites
# ========================================================================
echo -e "${CYAN}[1/6] Prerequisites${NC}"
echo "---------------------------------------------------------------"

command -v docker &>/dev/null && pass "Docker installed" || fail "Docker not found"
docker info &>/dev/null && pass "Docker daemon running" || fail "Docker daemon not running — start Docker Desktop"
docker compose version &>/dev/null && pass "Docker Compose V2 available" || fail "Docker Compose V2 not available"
[[ -f .env ]] && pass ".env file exists" || fail ".env file missing — run scripts/env-generate.sh"

if [[ "$MODE" == "logs" ]]; then
    echo ""
    echo -e "${CYAN}Recent Error Logs (last 20 lines, filtered)${NC}"
    echo "---------------------------------------------------------------"
    for svc in apiserver scheduler agent ai-engine postgres redis kafka nats; do
        errors=$(docker compose logs --tail=50 "$svc" 2>/dev/null | grep -iE "error|fatal|panic|fail" | tail -5)
        if [[ -n "$errors" ]]; then
            echo -e "\n  ${RED}--- $svc ---${NC}"
            echo "$errors" | sed 's/^/    /'
        else
            echo -e "  ${GREEN}--- $svc ---${NC} (no errors)"
        fi
    done
    exit 0
fi

# ========================================================================
# Section 2: Port Availability
# ========================================================================
if [[ "$MODE" != "quick" ]]; then
    echo ""
    echo -e "${CYAN}[2/6] Port Availability${NC}"
    echo "---------------------------------------------------------------"

    declare -A PORT_MAP=(
        [8080]="API Server" [8081]="Scheduler" [8082]="Agent" [8090]="AI Engine"
        [5432]="PostgreSQL" [6379]="Redis" [9092]="Kafka" [4222]="NATS"
        [9090]="Prometheus" [3000]="Grafana" [16686]="Jaeger"
    )

    for port in 8080 8081 8082 8090 5432 6379 9092 4222 9090 3000 16686; do
        if ss -tlnp 2>/dev/null | grep -q ":${port} " 2>/dev/null || \
           lsof -i ":${port}" &>/dev/null; then
            info "Port $port (${PORT_MAP[$port]}): in use"
        else
            info "Port $port (${PORT_MAP[$port]}): free"
        fi
    done
fi

# ========================================================================
# Section 3: Container Status
# ========================================================================
echo ""
echo -e "${CYAN}[3/6] Container Status${NC}"
echo "---------------------------------------------------------------"

SERVICES=(postgres redis kafka nats ai-engine apiserver scheduler agent prometheus grafana jaeger postgres-exporter redis-exporter)
for svc in "${SERVICES[@]}"; do
    state=$(docker compose ps --format '{{.State}}' "$svc" 2>/dev/null || echo "")
    if [[ -z "$state" ]]; then
        warn "$svc: not started"
    elif [[ "$state" == *"running"* ]]; then
        # Check if healthy
        health=$(docker compose ps --format '{{.Health}}' "$svc" 2>/dev/null || echo "")
        if [[ "$health" == *"healthy"* ]]; then
            pass "$svc: running (healthy)"
        elif [[ "$health" == *"unhealthy"* ]]; then
            fail "$svc: running (unhealthy)"
        else
            pass "$svc: running"
        fi
    else
        fail "$svc: $state"
    fi
done

[[ "$MODE" == "quick" ]] && { echo ""; goto_summary; }

# ========================================================================
# Section 4: Health Endpoints
# ========================================================================
echo ""
echo -e "${CYAN}[4/6] Health Endpoints${NC}"
echo "---------------------------------------------------------------"

# Infrastructure
check_tcp  "PostgreSQL"  "localhost" "5432"
check_tcp  "Redis"       "localhost" "6379"
check_tcp  "Kafka"       "localhost" "9092"
check_http "NATS"        "http://localhost:8222/healthz"
check_tcp  "NATS Client" "localhost" "4222"

# Application services
check_http "API Server"  "http://localhost:8080/healthz"
check_http "Scheduler"   "http://localhost:8081/healthz"
check_http "Agent"       "http://localhost:8082/healthz"
check_http "AI Engine"   "http://localhost:8090/healthz"

# Monitoring
check_http "Prometheus"  "http://localhost:9090/-/healthy"
check_http "Grafana"     "http://localhost:3000/api/health"
check_http "Jaeger"      "http://localhost:16686/"

# ========================================================================
# Section 5: Cross-Service Connectivity
# ========================================================================
echo ""
echo -e "${CYAN}[5/6] Cross-Service Connectivity${NC}"
echo "---------------------------------------------------------------"

# API Server readiness (includes DB check)
readyz=$(curl -sf --max-time 5 "http://localhost:8080/readyz" 2>/dev/null || echo "")
if echo "$readyz" | grep -q '"ready"' 2>/dev/null; then
    pass "apiserver → readyz (DB connected)"
elif [[ -n "$readyz" ]]; then
    warn "apiserver → readyz: degraded ($readyz)"
else
    fail "apiserver → readyz: unreachable"
fi

# NATS JetStream check
nats_varz=$(curl -sf --max-time 3 "http://localhost:8222/varz" 2>/dev/null || echo "")
if echo "$nats_varz" | grep -q '"jetstream"' 2>/dev/null; then
    pass "NATS JetStream: enabled"
else
    warn "NATS JetStream: status unknown"
fi

# ========================================================================
# Section 6: Recent Errors
# ========================================================================
echo ""
echo -e "${CYAN}[6/6] Recent Errors${NC}"
echo "---------------------------------------------------------------"

ERROR_COUNT=0
for svc in apiserver scheduler agent ai-engine; do
    errors=$(docker compose logs --tail=30 "$svc" 2>/dev/null | grep -icE "error|fatal|panic" || true)
    if [[ "$errors" -gt 0 ]]; then
        warn "$svc: $errors error(s) in recent logs"
        ((ERROR_COUNT += errors))
    else
        pass "$svc: no errors in recent logs"
    fi
done

if [[ "$ERROR_COUNT" -gt 0 ]]; then
    info "Run 'scripts/diagnose.sh --logs' for details"
fi

# ========================================================================
# Summary
# ========================================================================
echo ""
echo -e "${CYAN}=============================================================="
echo "  Diagnostic Summary"
echo -e "==============================================================${NC}"
echo ""
echo -e "  PASS: ${GREEN}$PASS${NC}   FAIL: ${RED}$FAIL${NC}   WARN: ${YELLOW}$WARN${NC}"
echo ""

if [[ "$FAIL" -eq 0 ]]; then
    echo -e "  ${GREEN}Status: ALL CHECKS PASSED${NC}"
    echo ""
    echo "  Services:"
    echo "    API Server:  http://localhost:8080"
    echo "    Grafana:     http://localhost:3000  (admin/cloudai)"
    echo "    Jaeger:      http://localhost:16686"
    echo "    Prometheus:  http://localhost:9090"
    echo "    NATS Monitor: http://localhost:8222"
else
    echo -e "  ${RED}Status: $FAIL ISSUE(S) DETECTED${NC}"
    echo ""
    echo "  Common fixes:"
    echo "    1. Generate .env:   scripts/env-generate.sh"
    echo "    2. Start services:  docker compose up -d"
    echo "    3. Check logs:      docker compose logs -f [service]"
    echo "    4. Restart:         docker compose restart [service]"
    echo "    5. Full reset:      docker compose down -v && docker compose up -d"
fi

echo ""
echo -e "${CYAN}==============================================================${NC}"
echo ""

# Auto-fix mode
if [[ "$MODE" == "fix" && "$FAIL" -gt 0 ]]; then
    echo -e "${YELLOW}Attempting auto-fix...${NC}"
    
    if [[ ! -f .env ]]; then
        echo "  Generating .env..."
        bash scripts/env-generate.sh --non-interactive
    fi
    
    echo "  Restarting services..."
    docker compose up -d
    
    echo "  Waiting 30 seconds for services to stabilize..."
    sleep 30
    
    echo "  Re-running quick diagnostics..."
    bash scripts/diagnose.sh --quick
fi

exit "$FAIL"
