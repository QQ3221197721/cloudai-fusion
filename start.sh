#!/usr/bin/env bash
# ============================================================================
# CloudAI Fusion - Quick Start Script (Linux/macOS)
# ============================================================================
# Modes:
#   ./start.sh              — Standard profile, CPU mode (~60-90s)
#   ./start.sh --minimal    — Minimal profile, core only, fastest startup
#   ./start.sh --full       — Full profile + monitoring stack (~2-3 min)
#   ./start.sh --gpu        — GPU mode (NVIDIA CUDA)
#   ./start.sh --fast       — Skip health wait (~30-45s)
#   ./start.sh --stop       — Stop all services
#
# Feature profile (controls which modules are enabled):
#   --minimal   Core only (API/Scheduler/Agent + DB/Redis). No edge/wasm/mesh.
#   (default)   Standard — monitoring, edge, WASM, security, cost.
#   --full      Everything including experimental (RL, LLM ops, multi-cluster).
#
# Override individual features:
#   CLOUDAI_FF_EDGE_COMPUTING=false ./start.sh
#
# GPU support:
#   Linux:   NVIDIA driver + nvidia-container-toolkit required
#   macOS:   NVIDIA GPU not supported (auto-fallback to CPU)
#   Windows: Use start.bat --gpu instead (WSL2 required)
#
# Setup GPU:  bash scripts/setup-gpu.sh
# ============================================================================
set -euo pipefail

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

MODE="core"
GPU_MODE=0
HEALTH_TIMEOUT_FACTOR=1
COMPOSE_FILES="-f docker-compose.yml"
FEATURE_PROFILE="standard"

# Detect OS
OS_TYPE="$(uname -s)"
case "$OS_TYPE" in
    Linux)  PLATFORM="linux" ;;
    Darwin) PLATFORM="macos" ;;
    *)      PLATFORM="unknown" ;;
esac

for arg in "$@"; do
    case "$arg" in
        --fast)     MODE="fast"; HEALTH_TIMEOUT_FACTOR=0 ;;
        --full)     MODE="full"; FEATURE_PROFILE="full" ;;
        --minimal)  MODE="minimal"; FEATURE_PROFILE="minimal" ;;
        --gpu)      GPU_MODE=1 ;;
        --stop)
            echo -e "${YELLOW}Stopping all services...${NC}"
            docker compose --profile monitoring down 2>/dev/null || docker compose down
            echo -e "${GREEN}All services stopped.${NC}"
            exit 0
            ;;
        --help|-h)
            echo "Usage: $0 [--minimal|--fast|--full|--gpu|--stop]"
            echo "  (default)    Standard profile, CPU mode (~60-90s)"
            echo "  --minimal    Minimal profile — core services only, fastest startup"
            echo "  --gpu        Enable NVIDIA GPU for AI Engine (Linux only)"
            echo "  --fast       Minimal health wait (~30-45s)"
            echo "  --full       Full profile + monitoring (Prometheus/Grafana/Jaeger)"
            echo "  --stop       Stop all services"
            echo ""
            echo "Feature profiles:"
            echo "  minimal   = Core only (no edge/wasm/mesh/monitoring)"
            echo "  standard  = Production-ready features (default)"
            echo "  full      = Everything enabled (experimental included)"
            echo ""
            echo "Override individual features:"
            echo "  CLOUDAI_FF_EDGE_COMPUTING=false ./start.sh"
            echo ""
            echo "GPU setup:   bash scripts/setup-gpu.sh"
            echo "Diagnostics: bash scripts/diagnose.sh"
            exit 0
            ;;
    esac
done

# Export feature profile so containers inherit it
export CLOUDAI_FEATURE_PROFILE="$FEATURE_PROFILE"

# --------------------------------------------------------------------------
# Helper: wait for a container to become healthy
# --------------------------------------------------------------------------
wait_healthy() {
    local svc="$1" timeout="${2:-60}" elapsed=0
    timeout=$((timeout * HEALTH_TIMEOUT_FACTOR))
    if [[ $timeout -le 0 ]]; then
        echo -e "  ${CYAN}[SKIP]${NC} $svc: health wait skipped (--fast mode)"
        return 0
    fi
    while [[ $elapsed -lt $timeout ]]; do
        health=$(docker compose ps --format '{{.Health}}' "$svc" 2>/dev/null || echo "")
        if [[ "$health" == *"healthy"* ]]; then
            echo -e "  ${GREEN}[OK]${NC} $svc: healthy (${elapsed}s)"
            return 0
        fi
        sleep 2
        ((elapsed += 2))
    done
    echo -e "  ${YELLOW}[WARN]${NC} $svc did not become healthy within ${timeout}s"
    echo "         Run: docker compose logs $svc"
}

STEP_TOTAL=5
[[ "$MODE" == "full" ]] && STEP_TOTAL=6
[[ $GPU_MODE -eq 1 ]] && STEP_TOTAL=$((STEP_TOTAL + 1))

# --------------------------------------------------------------------------
# GPU mode: platform check + compose overlay
# --------------------------------------------------------------------------
if [[ $GPU_MODE -eq 1 ]]; then
    if [[ "$PLATFORM" == "macos" ]]; then
        echo ""
        echo -e "${YELLOW}========================================================${NC}"
        echo -e "${YELLOW}  WARNING: macOS does NOT support NVIDIA GPU passthrough${NC}"
        echo -e "${YELLOW}========================================================${NC}"
        echo ""
        echo -e "  NVIDIA dropped macOS driver support since Mojave (10.14)."
        echo -e "  Docker Desktop for Mac cannot access NVIDIA GPUs."
        echo ""
        # Check Apple Silicon for MPS
        if sysctl -n machdep.cpu.brand_string 2>/dev/null | grep -qi "apple"; then
            echo -e "  ${GREEN}Apple Silicon detected!${NC}"
            echo -e "  You can use PyTorch MPS backend instead:"
            echo -e "    Set AI_DEVICE=mps in .env"
            echo ""
        fi
        echo -e "  Falling back to ${GREEN}CPU mode${NC}..."
        echo -e "  For GPU training, use a Linux server or cloud GPU instance."
        echo -e "  Run: bash scripts/setup-gpu.sh for details."
        echo ""
        GPU_MODE=0
        STEP_TOTAL=$((STEP_TOTAL - 1))
    else
        COMPOSE_FILES="-f docker-compose.yml -f docker-compose.gpu.yml"
    fi
fi

DEVICE_LABEL="CPU"
[[ $GPU_MODE -eq 1 ]] && DEVICE_LABEL="GPU (NVIDIA CUDA)"

echo ""
echo -e "${CYAN}========================================================"
echo "  CloudAI Fusion - Cloud-Native AI Unified Management"
echo "  Mode: ${MODE}  |  Profile: ${FEATURE_PROFILE}  |  Device: ${DEVICE_LABEL}"
echo -e "========================================================${NC}"
echo ""

# --------------------------------------------------------------------------
# Step 1: Check Docker
# --------------------------------------------------------------------------
CUR_STEP=1
echo -e "${YELLOW}[${CUR_STEP}/${STEP_TOTAL}]${NC} Checking prerequisites..."

if ! command -v docker &> /dev/null; then
    echo -e "${RED}[ERROR] Docker is not installed.${NC}"
    echo "Install: https://docs.docker.com/get-docker/"
    exit 1
fi

if ! docker compose version &> /dev/null; then
    echo -e "${RED}[ERROR] Docker Compose V2 is not available.${NC}"
    exit 1
fi

if ! docker info &> /dev/null; then
    echo -e "${RED}[ERROR] Docker daemon is not running.${NC}"
    exit 1
fi

echo "  Docker is running ($PLATFORM)."

# --------------------------------------------------------------------------
# Step 1.5: GPU environment check (--gpu mode only)
# --------------------------------------------------------------------------
if [[ $GPU_MODE -eq 1 ]]; then
    ((CUR_STEP++))
    echo ""
    echo -e "${YELLOW}[${CUR_STEP}/${STEP_TOTAL}]${NC} Checking GPU environment..."

    GPU_OK=1

    # Check NVIDIA driver
    if command -v nvidia-smi &>/dev/null; then
        GPU_NAME=$(nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null | head -1)
        GPU_MEM=$(nvidia-smi --query-gpu=memory.total --format=csv,noheader 2>/dev/null | head -1)
        DRIVER_VER=$(nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -1)
        echo -e "  ${GREEN}[OK]${NC} NVIDIA Driver $DRIVER_VER — $GPU_NAME ($GPU_MEM)"
    else
        echo -e "  ${RED}[FAIL]${NC} nvidia-smi not found — NVIDIA driver not installed"
        echo "         Install: https://www.nvidia.com/drivers"
        GPU_OK=0
    fi

    # Check nvidia-container-toolkit
    if docker info 2>/dev/null | grep -qi "nvidia"; then
        echo -e "  ${GREEN}[OK]${NC} NVIDIA Container Toolkit configured"
    elif command -v nvidia-container-runtime &>/dev/null; then
        echo -e "  ${YELLOW}[WARN]${NC} nvidia-container-toolkit installed but Docker not configured"
        echo "         Run: sudo nvidia-ctk runtime configure --runtime=docker"
        echo "         Then: sudo systemctl restart docker"
        GPU_OK=0
    else
        echo -e "  ${RED}[FAIL]${NC} NVIDIA Container Toolkit not installed"
        echo "         Run: bash scripts/setup-gpu.sh --install"
        GPU_OK=0
    fi

    if [[ $GPU_OK -eq 0 ]]; then
        echo ""
        echo -e "  ${YELLOW}GPU prerequisites not met. Falling back to CPU mode.${NC}"
        echo -e "  Run: ${CYAN}bash scripts/setup-gpu.sh${NC} for setup instructions."
        GPU_MODE=0
        COMPOSE_FILES="-f docker-compose.yml"
        DEVICE_LABEL="CPU (GPU fallback)"
    fi
fi

# --------------------------------------------------------------------------
# Step 2: Generate .env if missing
# --------------------------------------------------------------------------
((CUR_STEP++))
echo ""
echo -e "${YELLOW}[${CUR_STEP}/${STEP_TOTAL}]${NC} Checking environment configuration..."

if [[ ! -f .env ]]; then
    echo "  .env not found. Generating with secure defaults..."
    echo ""
    bash scripts/env-generate.sh --non-interactive
    echo ""
else
    echo "  .env already exists, using existing configuration."
fi

# --------------------------------------------------------------------------
# Step 3: Start infrastructure (parallel)
# --------------------------------------------------------------------------
((CUR_STEP++))
echo ""
echo -e "${YELLOW}[${CUR_STEP}/${STEP_TOTAL}]${NC} Starting infrastructure services..."
echo "  (PostgreSQL, Redis, Kafka, NATS)"

docker compose $COMPOSE_FILES up -d postgres redis kafka nats

echo "  Waiting for infrastructure to become healthy..."
# All infra starts in parallel; poll them concurrently
wait_healthy postgres 40 &
wait_healthy redis 20 &
wait_healthy kafka 60 &
wait_healthy nats 20 &
wait

echo "  Infrastructure ready."

# --------------------------------------------------------------------------
# Step 4: Start core services (respecting dependency order)
# --------------------------------------------------------------------------
((CUR_STEP++))
echo ""
echo -e "${YELLOW}[${CUR_STEP}/${STEP_TOTAL}]${NC} Starting core services... (device: ${DEVICE_LABEL})"

# ai-engine: GPU images are larger, allow more startup time
AI_TIMEOUT=45
[[ $GPU_MODE -eq 1 ]] && AI_TIMEOUT=90

docker compose $COMPOSE_FILES up -d ai-engine
wait_healthy ai-engine $AI_TIMEOUT

# apiserver + scheduler can start in parallel (both depend on infra + ai-engine)
docker compose $COMPOSE_FILES up -d apiserver scheduler
wait_healthy apiserver 45 &
wait_healthy scheduler 45 &
wait

# agent depends on apiserver + scheduler
docker compose $COMPOSE_FILES up -d agent
wait_healthy agent 30

echo "  Core services ready."

# --------------------------------------------------------------------------
# Step 5 (full mode): Start monitoring
# --------------------------------------------------------------------------
if [[ "$MODE" == "full" ]]; then
    ((CUR_STEP++))
    echo ""
    echo -e "${YELLOW}[${CUR_STEP}/${STEP_TOTAL}]${NC} Starting monitoring stack..."
    echo "  (Prometheus, Grafana, Jaeger, Exporters)"
    docker compose $COMPOSE_FILES --profile monitoring up -d
    echo "  Monitoring stack starting."
fi

# --------------------------------------------------------------------------
# Final: health verification + summary
# --------------------------------------------------------------------------
LAST_STEP=$STEP_TOTAL
echo ""
echo -e "${YELLOW}[${LAST_STEP}/${STEP_TOTAL}]${NC} Final health verification..."

API_HEALTHY=false
for i in $(seq 1 8); do
    if curl -sf http://localhost:8080/healthz > /dev/null 2>&1; then
        API_HEALTHY=true
        break
    fi
    sleep 2
done

echo ""
echo -e "${GREEN}========================================================"
echo "  CloudAI Fusion is running!"
echo "  Mode: ${MODE}  |  Profile: ${FEATURE_PROFILE}  |  Device: ${DEVICE_LABEL}"
echo "========================================================"
echo ""
echo "  Services:"
echo "    API Server:   http://localhost:8080"
echo "    Scheduler:    http://localhost:8081"
echo "    Agent:        http://localhost:8082"
echo "    AI Engine:    http://localhost:8090  [${DEVICE_LABEL}]"
if [[ "$MODE" == "full" ]]; then
echo ""
echo "  Monitoring:"
echo "    Prometheus:   http://localhost:9090"
echo "    Grafana:      http://localhost:3000  (admin/cloudai)"
echo "    Jaeger:       http://localhost:16686"
echo "    NATS Monitor: http://localhost:8222"
fi
echo ""
echo "  Quick Test:"
echo "    curl http://localhost:8080/healthz"
echo "    curl http://localhost:8080/readyz"
echo ""
if [[ "$MODE" != "full" ]]; then
echo "  Add monitoring: ./start.sh --full"
echo "                  (or: docker compose --profile monitoring up -d)"
fi
if [[ "$FEATURE_PROFILE" != "full" ]]; then
echo "  Full features:  ./start.sh --full"
fi
if [[ "$FEATURE_PROFILE" == "minimal" ]]; then
echo "  Standard:       ./start.sh"
fi
if [[ $GPU_MODE -eq 0 ]]; then
echo "  Enable GPU:     ./start.sh --gpu  (Linux only, run scripts/setup-gpu.sh first)"
fi
echo "  Feature flags:  curl http://localhost:8080/api/v1/features"
echo "  Toggle feature: curl -X PUT http://localhost:8080/api/v1/features/edge_computing -d '{\"enabled\": false}'"
echo "  GPU setup:      bash scripts/setup-gpu.sh"
echo "  Diagnostics:    scripts/diagnose.sh"
echo "  Stop:           docker compose --profile monitoring down"
echo "  Logs:           docker compose logs -f [service]"
echo -e "========================================================${NC}"
echo ""
