#!/bin/bash
# Quick Start Script for Mock Intel IAS Server
# Usage: ./start-mock-ias.sh [--port 8080] [--mode random]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Parse command-line arguments
PORT=8080
MODE="success"

while [[ $# -gt 0 ]]; do
    case $1 in
        --port|-p)
            PORT="$2"
            shift 2
            ;;
        --mode|-m)
            MODE="$2"
            shift 2
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Check if Python is installed
if ! command -v python3 &> /dev/null; then
    echo -e "${RED}Error: Python 3 not found. Please install Python 3.8+${NC}"
    exit 1
fi

# Check if Flask is installed
if ! python3 -c "import flask" 2>/dev/null; then
    echo -e "${YELLOW}Flask not detected. Installing dependencies...${NC}"
    pip3 install -r requirements.txt
fi

echo -e "\n${GREEN}╔═══════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║                                                           ║${NC}"
echo -e "${GREEN}║   Starting Mock Intel IAS Server                        ║${NC}"
echo -e "${GREEN}║   Mode: ${YELLOW}${MODE^^}${NC} | Port: ${YELLOW}${PORT}${NC}                           ${NC}"
echo -e "${GREEN}║   ⚠️  NOT FOR PRODUCTION USE ⚠️                          ║${NC}"
echo -e "${GREEN}║                                                           ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════════╝${NC}"
echo ""

# Execute mock server
python3 internal/tee/mock_ias_server.py \
    --port "$PORT" \
    --mode "$MODE" \
    --debug
