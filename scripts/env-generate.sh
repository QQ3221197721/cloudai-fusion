#!/usr/bin/env bash
# ============================================================================
# CloudAI Fusion - Environment Variable Generator (Linux/macOS)
# Generates .env with cryptographically secure secrets
# ============================================================================
# Usage:
#   scripts/env-generate.sh                  (interactive — dev profile)
#   scripts/env-generate.sh --profile dev
#   scripts/env-generate.sh --profile prod
#   scripts/env-generate.sh --non-interactive
# ============================================================================
set -euo pipefail

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
ENV_FILE="$PROJECT_DIR/.env"
PROFILE="dev"
INTERACTIVE=1

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --profile) PROFILE="$2"; shift 2 ;;
        --non-interactive) INTERACTIVE=0; shift ;;
        --help|-h) 
            echo "Usage: scripts/env-generate.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --profile dev|staging|prod   Environment profile (default: dev)"
            echo "  --non-interactive             Skip interactive prompts"
            echo "  --help                        Show this help"
            echo ""
            echo "Profiles:"
            echo "  dev       Debug logging, no SSL, no Redis auth"
            echo "  staging   Info logging, SSL required, no Redis auth"
            echo "  prod      Warn logging, SSL required, Redis auth enabled"
            exit 0 ;;
        *) echo -e "${RED}Unknown option: $1${NC}"; exit 1 ;;
    esac
done

echo ""
echo -e "${CYAN}=============================================================="
echo "  CloudAI Fusion - Environment Generator"
echo "  Profile: $PROFILE"
echo -e "==============================================================${NC}"
echo ""

# Check if .env already exists
if [[ -f "$ENV_FILE" ]]; then
    echo -e "${YELLOW}[WARN] .env already exists at $ENV_FILE${NC}"
    if [[ "$INTERACTIVE" -eq 1 ]]; then
        read -rp "Overwrite? [y/N]: " OVERWRITE
        if [[ "${OVERWRITE,,}" != "y" ]]; then
            echo "Aborted."
            exit 0
        fi
    else
        echo "       Non-interactive mode: creating .env.generated instead"
        ENV_FILE="$PROJECT_DIR/.env.generated"
    fi
fi

# --------------------------------------------------------------------------
# Generate cryptographically secure random hex strings
# --------------------------------------------------------------------------
echo -e "${YELLOW}[1/5]${NC} Generating secure secrets..."

generate_hex() {
    local bytes=$1
    if command -v openssl &>/dev/null; then
        openssl rand -hex "$bytes"
    elif [[ -r /dev/urandom ]]; then
        head -c "$bytes" /dev/urandom | xxd -p | tr -d '\n'
    else
        # Fallback: python
        python3 -c "import secrets; print(secrets.token_hex($bytes))"
    fi
}

JWT_SECRET=$(generate_hex 32)
DB_PASSWORD=$(generate_hex 16)
GF_PASSWORD=$(generate_hex 16)
REDIS_PASSWORD=$(generate_hex 16)

echo "       JWT Secret:   ${JWT_SECRET:0:8}...  (64 hex chars)"
echo "       DB Password:  ${DB_PASSWORD:0:8}...  (32 hex chars)"

# --------------------------------------------------------------------------
# Profile-specific defaults
# --------------------------------------------------------------------------
echo ""
echo -e "${YELLOW}[2/5]${NC} Applying profile: $PROFILE"

case "$PROFILE" in
    prod|production)
        LOG_LEVEL="warn"
        ENV_NAME="production"
        DB_SSLMODE="require"
        REDIS_AUTH="true"
        ;;
    staging)
        LOG_LEVEL="info"
        ENV_NAME="staging"
        DB_SSLMODE="require"
        REDIS_AUTH="false"
        ;;
    *)
        LOG_LEVEL="debug"
        ENV_NAME="development"
        DB_SSLMODE="disable"
        REDIS_AUTH="false"
        ;;
esac

echo "       Environment:  $ENV_NAME"
echo "       Log Level:    $LOG_LEVEL"
echo "       DB SSL Mode:  $DB_SSLMODE"

# --------------------------------------------------------------------------
# Optional: LLM API keys (interactive only)
# --------------------------------------------------------------------------
OPENAI_KEY=""
DASHSCOPE_KEY=""
OLLAMA_URL=""

echo ""
echo -e "${YELLOW}[3/5]${NC} LLM Integration (optional)"

if [[ "$INTERACTIVE" -eq 1 ]]; then
    read -rp "  OpenAI API Key  [skip]: " OPENAI_KEY
    read -rp "  DashScope API Key [skip]: " DASHSCOPE_KEY
    read -rp "  Ollama Base URL  [http://localhost:11434/v1]: " OLLAMA_URL
fi
OLLAMA_URL="${OLLAMA_URL:-http://localhost:11434/v1}"

# --------------------------------------------------------------------------
# Write .env file
# --------------------------------------------------------------------------
echo ""
echo -e "${YELLOW}[4/5]${NC} Writing $ENV_FILE..."

cat > "$ENV_FILE" <<EOF
# ============================================================================
# CloudAI Fusion - Environment Variables
# Generated: $(date -u '+%Y-%m-%dT%H:%M:%SZ')
# Profile: $PROFILE
# ============================================================================
# SECURITY: Never commit this file to version control!
# Regenerate: scripts/env-generate.sh --profile $PROFILE
# ============================================================================

# --------------------------------------------------------------------------
# Environment
# --------------------------------------------------------------------------
CLOUDAI_ENV=$ENV_NAME
CLOUDAI_LOG_LEVEL=$LOG_LEVEL

# --------------------------------------------------------------------------
# Database (PostgreSQL) - REQUIRED
# --------------------------------------------------------------------------
CLOUDAI_DB_PASSWORD=$DB_PASSWORD
CLOUDAI_DB_SSLMODE=$DB_SSLMODE

# --------------------------------------------------------------------------
# Authentication - REQUIRED
# --------------------------------------------------------------------------
CLOUDAI_JWT_SECRET=$JWT_SECRET

# --------------------------------------------------------------------------
# Grafana Admin
# --------------------------------------------------------------------------
GF_ADMIN_PASSWORD=$GF_PASSWORD

# --------------------------------------------------------------------------
# Redis
# --------------------------------------------------------------------------
EOF

if [[ "$REDIS_AUTH" == "true" ]]; then
    echo "CLOUDAI_REDIS_PASSWORD=$REDIS_PASSWORD" >> "$ENV_FILE"
else
    echo "# CLOUDAI_REDIS_PASSWORD=" >> "$ENV_FILE"
fi

cat >> "$ENV_FILE" <<EOF

# --------------------------------------------------------------------------
# LLM Integration (optional)
# --------------------------------------------------------------------------
EOF

if [[ -n "$OPENAI_KEY" ]]; then
    echo "OPENAI_API_KEY=$OPENAI_KEY" >> "$ENV_FILE"
else
    echo "# OPENAI_API_KEY=sk-..." >> "$ENV_FILE"
fi

if [[ -n "$DASHSCOPE_KEY" ]]; then
    echo "DASHSCOPE_API_KEY=$DASHSCOPE_KEY" >> "$ENV_FILE"
else
    echo "# DASHSCOPE_API_KEY=sk-..." >> "$ENV_FILE"
fi

cat >> "$ENV_FILE" <<EOF
OLLAMA_BASE_URL=$OLLAMA_URL
# VLLM_BASE_URL=http://localhost:8000/v1

# --------------------------------------------------------------------------
# Monitoring
# --------------------------------------------------------------------------
# OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
EOF

echo "       Done."

# --------------------------------------------------------------------------
# Summary
# --------------------------------------------------------------------------
echo ""
echo -e "${YELLOW}[5/5]${NC} Summary"
echo ""
echo -e "${GREEN}=============================================================="
echo "  .env generated successfully!"
echo "=============================================================="
echo ""
echo "  File:      $ENV_FILE"
echo "  Profile:   $PROFILE ($ENV_NAME)"
echo "  Secrets:   Auto-generated (cryptographically secure)"
echo ""
echo "  Next steps:"
echo "    1. Review the .env file"
echo "    2. Run: docker compose up -d"
echo "    3. Run: scripts/diagnose.sh  (verify health)"
echo -e "==============================================================${NC}"
echo ""
