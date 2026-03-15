#!/usr/bin/env bash
# ============================================================================
# CloudAI Fusion - GPU Environment Setup (Linux / macOS)
# ============================================================================
#
# This script checks and configures GPU support for Docker containers.
#
# Supported:
#   Linux:  Checks NVIDIA driver + installs nvidia-container-toolkit
#   macOS:  Detects unsupported platform, recommends CPU mode
#
# Usage:
#   bash scripts/setup-gpu.sh              # Interactive check & setup
#   bash scripts/setup-gpu.sh --check      # Check only, no install
#   bash scripts/setup-gpu.sh --install    # Auto-install (requires sudo)
#
# ============================================================================
set -euo pipefail

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

MODE="${1:---check}"
ERRORS=0

banner() {
    echo ""
    echo -e "${CYAN}========================================================"
    echo "  CloudAI Fusion — GPU Environment Setup"
    echo -e "========================================================${NC}"
    echo ""
}

ok()   { echo -e "  ${GREEN}[OK]${NC}   $1"; }
fail() { echo -e "  ${RED}[FAIL]${NC} $1"; ((ERRORS++)) || true; }
warn() { echo -e "  ${YELLOW}[WARN]${NC} $1"; }
info() { echo -e "  ${CYAN}[INFO]${NC} $1"; }

# ----------------------------------------------------------------
# Detect OS
# ----------------------------------------------------------------
detect_os() {
    OS="$(uname -s)"
    case "$OS" in
        Linux)  PLATFORM="linux" ;;
        Darwin) PLATFORM="macos" ;;
        *)      PLATFORM="unknown" ;;
    esac
    echo -e "${BOLD}Platform:${NC} $OS ($PLATFORM)"
    echo ""
}

# ----------------------------------------------------------------
# macOS: No NVIDIA GPU support
# ----------------------------------------------------------------
check_macos() {
    echo -e "${BOLD}[1/4] GPU Support Check${NC}"
    warn "macOS does NOT support NVIDIA GPU passthrough to Docker."
    echo ""
    echo -e "  ${YELLOW}Why?${NC}"
    echo "    - Apple dropped NVIDIA driver support since macOS Mojave (10.14)"
    echo "    - Docker Desktop for Mac uses HyperKit/Virtualization.framework"
    echo "    - No GPU passthrough mechanism exists for macOS Docker"
    echo ""
    echo -e "  ${GREEN}Recommended: Use CPU mode (default)${NC}"
    echo "    ./start.sh            # CPU mode, works on all platforms"
    echo "    AI_DEVICE=cpu in .env # Explicitly set CPU device"
    echo ""
    echo -e "  ${CYAN}For GPU training, consider:${NC}"
    echo "    1. Remote Linux GPU server + SSH"
    echo "    2. Cloud GPU instances (AWS p3/p4, GCP A100, Azure NC)"
    echo "    3. Google Colab / Kaggle Notebooks"
    echo "    4. Apple M-series MPS backend (experimental PyTorch):"
    echo "       AI_DEVICE=mps in .env (Apple Silicon only)"
    echo ""

    # Check Apple Silicon MPS
    if sysctl -n machdep.cpu.brand_string 2>/dev/null | grep -qi "apple"; then
        ok "Apple Silicon detected — PyTorch MPS backend available"
        echo "    Set AI_DEVICE=mps in .env for Apple GPU acceleration"
    else
        info "Intel Mac — CPU-only mode"
    fi
    echo ""
}

# ----------------------------------------------------------------
# Linux: Full GPU check pipeline
# ----------------------------------------------------------------
check_linux_driver() {
    echo -e "${BOLD}[1/4] NVIDIA Driver${NC}"

    if ! command -v nvidia-smi &>/dev/null; then
        fail "nvidia-smi not found — NVIDIA driver is not installed"
        echo ""
        echo "    Install NVIDIA driver:"
        echo "      Ubuntu/Debian: sudo apt install nvidia-driver-550"
        echo "      RHEL/CentOS:   sudo dnf install nvidia-driver"
        echo "      Arch:           sudo pacman -S nvidia"
        echo "      Or download from: https://www.nvidia.com/drivers"
        echo ""
        return 1
    fi

    DRIVER_VERSION=$(nvidia-smi --query-gpu=driver_version --format=csv,noheader 2>/dev/null | head -1)
    GPU_NAME=$(nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null | head -1)
    GPU_MEM=$(nvidia-smi --query-gpu=memory.total --format=csv,noheader 2>/dev/null | head -1)
    GPU_COUNT=$(nvidia-smi --query-gpu=count --format=csv,noheader 2>/dev/null | head -1)

    ok "NVIDIA Driver $DRIVER_VERSION"
    echo "    GPU:    $GPU_NAME"
    echo "    Memory: $GPU_MEM"
    echo "    Count:  $GPU_COUNT"

    # Check minimum driver version (525.60 for CUDA 12.x)
    MAJOR=$(echo "$DRIVER_VERSION" | cut -d. -f1)
    if [[ "$MAJOR" -lt 525 ]]; then
        warn "Driver $DRIVER_VERSION < 525.60 (minimum for CUDA 12.x)"
        echo "    Upgrade: sudo apt install nvidia-driver-550"
    fi
    echo ""
}

check_linux_docker() {
    echo -e "${BOLD}[2/4] Docker${NC}"

    if ! command -v docker &>/dev/null; then
        fail "Docker is not installed"
        echo "    Install: https://docs.docker.com/engine/install/"
        echo ""
        return 1
    fi

    if ! docker info &>/dev/null; then
        fail "Docker daemon is not running"
        echo "    Start: sudo systemctl start docker"
        echo ""
        return 1
    fi

    DOCKER_VERSION=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "unknown")
    ok "Docker $DOCKER_VERSION"
    echo ""
}

check_linux_nvidia_toolkit() {
    echo -e "${BOLD}[3/4] NVIDIA Container Toolkit${NC}"

    # Check if nvidia runtime is available
    if docker info 2>/dev/null | grep -qi "nvidia"; then
        ok "NVIDIA Container Toolkit is configured"
        echo ""
        return 0
    fi

    # Check if nvidia-container-toolkit package is installed
    if command -v nvidia-container-runtime &>/dev/null || \
       dpkg -l nvidia-container-toolkit 2>/dev/null | grep -q "^ii" || \
       rpm -q nvidia-container-toolkit 2>/dev/null; then
        warn "nvidia-container-toolkit installed but Docker not configured"
        echo "    Run: sudo nvidia-ctk runtime configure --runtime=docker"
        echo "    Then: sudo systemctl restart docker"
        echo ""
        return 1
    fi

    fail "NVIDIA Container Toolkit is NOT installed"
    echo ""

    if [[ "$MODE" == "--install" ]]; then
        install_nvidia_toolkit
    else
        echo "    To install automatically:"
        echo "      bash scripts/setup-gpu.sh --install"
        echo ""
        echo "    Or manually (Ubuntu/Debian):"
        echo "      curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | \\"
        echo "        sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg"
        echo "      curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \\"
        echo "        sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \\"
        echo "        sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list"
        echo "      sudo apt-get update && sudo apt-get install -y nvidia-container-toolkit"
        echo "      sudo nvidia-ctk runtime configure --runtime=docker"
        echo "      sudo systemctl restart docker"
        echo ""
        echo "    RHEL/CentOS:"
        echo "      See: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html"
        echo ""
    fi
    return 1
}

install_nvidia_toolkit() {
    info "Installing NVIDIA Container Toolkit..."
    echo ""

    # Detect package manager
    if command -v apt-get &>/dev/null; then
        echo "  Detected: Ubuntu/Debian (apt)"
        curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | \
            sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg 2>/dev/null
        curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
            sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
            sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list > /dev/null
        sudo apt-get update -qq
        sudo apt-get install -y -qq nvidia-container-toolkit
    elif command -v dnf &>/dev/null; then
        echo "  Detected: RHEL/Fedora (dnf)"
        curl -s -L https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo | \
            sudo tee /etc/yum.repos.d/nvidia-container-toolkit.repo > /dev/null
        sudo dnf install -y nvidia-container-toolkit
    elif command -v yum &>/dev/null; then
        echo "  Detected: CentOS (yum)"
        curl -s -L https://nvidia.github.io/libnvidia-container/stable/rpm/nvidia-container-toolkit.repo | \
            sudo tee /etc/yum.repos.d/nvidia-container-toolkit.repo > /dev/null
        sudo yum install -y nvidia-container-toolkit
    else
        fail "Unsupported package manager. Install manually:"
        echo "    https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html"
        return 1
    fi

    # Configure Docker runtime
    info "Configuring Docker to use NVIDIA runtime..."
    sudo nvidia-ctk runtime configure --runtime=docker
    sudo systemctl restart docker

    ok "NVIDIA Container Toolkit installed and configured"
    echo ""
}

check_linux_gpu_docker() {
    echo -e "${BOLD}[4/4] GPU Docker Test${NC}"

    info "Running: docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi"
    echo ""

    if docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi 2>/dev/null; then
        echo ""
        ok "GPU is accessible inside Docker containers!"
    else
        echo ""
        fail "GPU not accessible inside Docker"
        echo "    This usually means nvidia-container-toolkit is not properly configured."
        echo "    Try: sudo nvidia-ctk runtime configure --runtime=docker"
        echo "    Then: sudo systemctl restart docker"
    fi
    echo ""
}

# ----------------------------------------------------------------
# Summary
# ----------------------------------------------------------------
print_summary() {
    echo -e "${BOLD}========================================================"
    echo "  Summary"
    echo -e "========================================================${NC}"
    echo ""

    if [[ "$PLATFORM" == "macos" ]]; then
        echo -e "  Platform: macOS — ${YELLOW}GPU not supported${NC}"
        echo "  Recommended: ./start.sh  (CPU mode)"
        echo ""
        return 0
    fi

    if [[ $ERRORS -eq 0 ]]; then
        echo -e "  ${GREEN}All checks passed! GPU is ready.${NC}"
        echo ""
        echo "  Start with GPU:"
        echo "    ./start.sh --gpu"
        echo "    # or:"
        echo "    docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d"
        echo ""
    else
        echo -e "  ${RED}$ERRORS issue(s) found.${NC} Fix them and re-run this script."
        echo ""
        echo "  After fixing:"
        echo "    bash scripts/setup-gpu.sh --check"
        echo ""
        echo "  Or use CPU mode (no GPU required):"
        echo "    ./start.sh"
        echo ""
    fi
}

# ----------------------------------------------------------------
# Main
# ----------------------------------------------------------------
banner
detect_os

case "$PLATFORM" in
    macos)
        check_macos
        ;;
    linux)
        check_linux_driver || true
        check_linux_docker || true
        check_linux_nvidia_toolkit || true
        # Only run docker GPU test if all prerequisites pass
        if [[ $ERRORS -eq 0 ]]; then
            check_linux_gpu_docker || true
        fi
        ;;
    *)
        fail "Unsupported platform: $OS"
        echo "  Supported: Linux, macOS"
        echo "  Windows users: run scripts\\setup-gpu.bat instead"
        ;;
esac

print_summary
exit $ERRORS
