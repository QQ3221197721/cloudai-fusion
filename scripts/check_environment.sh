#!/bin/bash
# ============================================================================
# Environment Setup and Validation Script
# Verifies system dependencies and configurations for CI/CD pipeline
# ============================================================================
# Usage: ./check_environment.sh
# Checks installed tools, versions, and system configuration
# ============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Required versions
REQUIRED_GO_VERSION="1.25"
REQUIRED_PYTHON_VERSION="3.11"
REQUIRED_DOCKER_VERSION="24.0"
REQUIRED_KUBECTL_VERSION="1.28"
REQUIRED_KUSTOMIZE_VERSION="v5.0"

ERRORS=0
WARNINGS=0

check_command() {
    local cmd=$1
    local desc=$2
    
    if command -v "$cmd" >/dev/null 2>&1; then
        local version=$($cmd --version 2>&1 | head -n1)
        echo -e "  ${GREEN}✓${NC} $cmd is installed ($version)"
    else
        echo -e "  ${RED}✗${NC} $cmd is NOT installed"
        ((ERRORS++)) || true
    fi
}

check_go_version() {
    echo ""
    echo -e "${BLUE}Checking Go version...${NC}"
    
    if command -v go >/dev/null 2>&1; then
        local current_version=$(go version | awk '{print $3}')
        
        # Compare versions (simple check)
        if [[ $(printf '%s\n' "$current_version" "$REQUIRED_GO_VERSION" | sort -V | head -n1) == "$REQUIRED_GO_VERSION" ]]; then
            echo -e "  ${GREEN}✓${NC} Go version OK: $current_version >= $REQUIRED_GO_VERSION"
        else
            echo -e "  ${YELLOW}⚠${NC} Go version outdated: $current_version < $REQUIRED_GO_VERSION"
            ((WARNINGS++)) || true
        fi
    else
        echo -e "  ${RED}✗${NC} Go is not installed"
        ((ERRORS++)) || true
    fi
}

check_python_version() {
    echo ""
    echo -e "${BLUE}Checking Python version...${NC}"
    
    if command -v python3 >/dev/null 2>&1; then
        local current_version=$(python3 --version | awk '{print $2}')
        
        if [[ $(printf '%s\n' "$current_version" "$REQUIRED_PYTHON_VERSION" | sort -V | head -n1) == "$REQUIRED_PYTHON_VERSION" ]]; then
            echo -e "  ${GREEN}✓${NC} Python version OK: $current_version >= $REQUIRED_PYTHON_VERSION"
        else
            echo -e "  ${YELLOW}⚠${NC} Python version outdated: $current_version < $REQUIRED_PYTHON_VERSION"
            ((WARNINGS++)) || true
        fi
    elif command -v python >/dev/null 2>&1; then
        echo -e "  ${YELLOW}⚠${NC} Using 'python' instead of 'python3'"
        ((WARNINGS++)) || true
    else
        echo -e "  ${RED}✗${NC} Python is not installed"
        ((ERRORS++)) || true
    fi
}

check_disk_space() {
    echo ""
    echo -e "${BLUE}Checking disk space...${NC}"
    
    local available=$(df -h . | awk 'NR==2 {print $4}' | sed 's/[MkG]//g')
    
    if [ "$available" -gt 10 ]; then
        echo -e "  ${GREEN}✓${NC} Disk space sufficient: ${available}GB available"
    else
        echo -e "  ${RED}✗${NC} Disk space insufficient: ${available}GB available (minimum 10GB required)"
        ((ERRORS++)) || true
    fi
}

check_git_config() {
    echo ""
    echo -e "${BLUE}Checking Git configuration...${NC}"
    
    if git config user.name >/dev/null 2>&1; then
        local name=$(git config user.name)
        local email=$(git config user.email)
        echo -e "  ${GREEN}✓${NC} Git configured: $name <$email>"
    else
        echo -e "  ${YELLOW}⚠${NC} Git user not configured. Run:"
        echo -e "     git config --global user.name \"Your Name\""
        echo -e "     git config --global user.email \"your@email.com\""
        ((WARNINGS++)) || true
    fi
}

check_go_modules() {
    echo ""
    echo -e "${BLUE}Checking Go modules...${NC}"
    
    cd cloudai-fusion
    
    if [ -f go.mod ]; then
        echo -e "  ${GREEN}✓${NC} Go module file exists"
        
        # Check if modules can be downloaded
        if go mod download 2>/dev/null; then
            echo -e "  ${GREEN}✓${NC} Go modules can be downloaded"
        else
            echo -e "  ${RED}✗${NC} Failed to download Go modules"
            ((ERRORS++)) || true
        fi
        
        # Verify modules
        if go mod verify 2>/dev/null; then
            echo -e "  ${GREEN}✓${NC} Go modules verified"
        else
            echo -e "  ${RED}✗${NC} Go modules verification failed"
            ((ERRORS++)) || true
        fi
    else
        echo -e "  ${RED}✗${NC} No go.mod file found"
        ((ERRORS++)) || true
    fi
    
    cd ..
}

check_python_dependencies() {
    echo ""
    echo -e "${BLUE}Checking Python dependencies...${NC}"
    
    cd cloudai-fusion/ai
    
    if [ -f requirements.txt ]; then
        echo -e "  ${GREEN}✓${NC} Python requirements file exists"
        
        # Try to install in dry-run mode
        if pip install -r requirements.txt --dry-run >/dev/null 2>&1; then
            echo -e "  ${GREEN}✓${NC} Python dependencies are valid"
        else
            echo -e "  ${YELLOW}⚠${NC} Could not validate dependencies (offline or network issues)"
            ((WARNINGS++)) || true
        fi
    else
        echo -e "  ${RED}✗${NC} No requirements.txt found"
        ((ERRORS++)) || true
    fi
    
    cd ..
}

generate_report() {
    echo ""
    echo -e "${BOLD}${BLUE}====================================${NC}"
    echo -e "${BOLD}${BLUE}Environment Check Summary${NC}"
    echo -e "${BOLD}${BLUE}====================================${NC}"
    echo ""
    
    if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
        echo -e "${GREEN}✅ All checks passed! System is ready for CI/CD.${NC}"
        exit 0
    elif [ $ERRORS -eq 0 ]; then
        echo -e "${YELLOW}⚠️  Completed with warnings (${WARNINGS})${NC}"
        echo ""
        echo "Proceeding despite warnings may cause issues later."
        exit 0
    else
        echo -e "${RED}❌ Found errors that need to be fixed:${NC}"
        echo ""
        echo "- Errors: $ERRORS"
        echo "- Warnings: $WARNINGS"
        echo ""
        echo "Please fix the errors above before proceeding."
        exit 1
    fi
}

main() {
    echo -e "${BOLD}${CYAN}"
    echo "=================================================="
    echo "    CloudAI Fusion Environment Check v1.0.0"
    echo "=================================================="
    echo -e "${NC}"
    echo ""
    echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "Working Directory: $(pwd)"
    echo ""
    
    echo "Required Tools:"
    check_command "go" "Go compiler"
    check_command "docker" "Docker container engine"
    check_command "kubectl" "Kubernetes CLI"
    check_command "python3" "Python 3 interpreter"
    check_command "jq" "JSON processor"
    check_command "curl" "HTTP client"
    check_command "git" "Version control"
    
    check_go_version
    check_python_version
    check_disk_space
    check_git_config
    check_go_modules
    check_python_dependencies
    
    generate_report
}

main
