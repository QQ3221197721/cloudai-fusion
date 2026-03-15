#!/usr/bin/env bash
# ============================================================================
# CloudAI Fusion - Dev Container Post-Create Setup
# Runs once after the container is created
# ============================================================================
set -euo pipefail

echo "=========================================="
echo "  CloudAI Fusion Dev Container Setup"
echo "=========================================="

# Go dependencies
echo "[1/5] Downloading Go modules..."
cd /workspace
go mod download

# Go tools
echo "[2/5] Installing Go development tools..."
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest 2>/dev/null || true
go install golang.org/x/tools/cmd/goimports@latest 2>/dev/null || true
go install github.com/swaggo/swag/cmd/swag@latest 2>/dev/null || true
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest 2>/dev/null || true
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest 2>/dev/null || true

# Python dependencies
echo "[3/5] Installing Python dependencies..."
if [[ -f ai/requirements.txt ]]; then
    pip install --quiet -r ai/requirements.txt 2>/dev/null || true
fi

# Create .env for local development if not exists
echo "[4/5] Ensuring .env exists..."
if [[ ! -f .env ]]; then
    cat > .env <<'ENVEOF'
# Auto-generated for Dev Container
CLOUDAI_ENV=development
CLOUDAI_DB_PASSWORD=devcontainer_secret
CLOUDAI_JWT_SECRET=devcontainer-jwt-secret-not-for-production-use-32bytes
GF_ADMIN_PASSWORD=devcontainer
CLOUDAI_LOG_LEVEL=debug
ENVEOF
    echo "  Created .env with devcontainer defaults"
fi

# Verify build
echo "[5/5] Verifying Go build..."
go build ./... 2>/dev/null && echo "  Build: OK" || echo "  Build: FAILED (non-blocking)"

echo ""
echo "=========================================="
echo "  Setup complete!"
echo ""
echo "  Quick commands:"
echo "    make help         - Show all targets"
echo "    make build        - Build Go binaries"
echo "    make test-go      - Run Go tests"
echo "    make dev          - Start all services"
echo "    make diagnose     - Health check all services"
echo "=========================================="
