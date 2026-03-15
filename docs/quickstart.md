# Quick Start Guide

Get CloudAI Fusion running in under 5 minutes.

## Prerequisites

| Requirement | Version | Check Command |
|-------------|---------|---------------|
| Go | 1.25+ | `go version` |
| PostgreSQL | 16+ | `psql --version` |
| Docker (optional) | 24+ | `docker --version` |

## Option 1: Local Development (Fastest)

### 1. Clone and Build

```bash
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion
go build ./...
```

### 2. Set Up PostgreSQL

Create the database and user:

```sql
CREATE USER cloudai WITH PASSWORD 'cloudai_secret';
CREATE DATABASE cloudai OWNER cloudai;
```

Initialize the schema:

```bash
psql -U cloudai -d cloudai -f scripts/init-db.sql
```

### 3. Start the API Server

```bash
# Linux / macOS
export CLOUDAI_DB_PASSWORD=cloudai_secret
go run ./cmd/apiserver/...

# Windows PowerShell
$env:CLOUDAI_DB_PASSWORD = "cloudai_secret"
go run ./cmd/apiserver/...
```

### 4. Verify

```bash
# Health check
curl http://localhost:8080/healthz
# Expected: {"service":"cloudai-fusion-apiserver","status":"healthy",...}

# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'
# Returns JWT token
```

## Option 2: Docker Compose (Full Stack)

```bash
cd cloudai-fusion

# First-time: generate .env with secure secrets
make setup          # or: scripts/env-generate.bat (Windows)

# Start all services
docker compose up -d
```

This starts all 13 services: API Server, Scheduler, Agent, AI Engine, PostgreSQL, Redis, Kafka, NATS, Prometheus, Grafana, Jaeger, and exporters.

### Start Modes

```bash
# Full stack (default)
./start.sh                # Linux / macOS
start.bat                 # Windows

# GPU support (NVIDIA)
./start.sh --gpu
start.bat --gpu

# Minimal mode (core services only, no monitoring stack)
./start.sh --minimal

# Health diagnostics
make diagnose
```

### Service Endpoints

| Service | URL | Credentials |
|---------|-----|-------------|
| API Server | http://localhost:8080 | admin / admin123 |
| AI Engine | http://localhost:8090 | - |
| Prometheus | http://localhost:9090 | - |
| Grafana | http://localhost:3000 | admin / cloudai |
| Jaeger UI | http://localhost:16686 | - |

## Configuration

CloudAI Fusion reads configuration from (in order of priority):
1. **CLI flags**: `--port 8080`
2. **Environment variables**: `CLOUDAI_DB_HOST=localhost` (prefix: `CLOUDAI_`)
3. **Config file**: `cloudai-fusion.yaml`
4. **Defaults**: built-in sensible defaults

### Key Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDAI_DB_HOST` | localhost | PostgreSQL host |
| `CLOUDAI_DB_PORT` | 5432 | PostgreSQL port |
| `CLOUDAI_DB_NAME` | cloudai | Database name |
| `CLOUDAI_DB_USER` | cloudai | Database user |
| `CLOUDAI_DB_PASSWORD` | cloudai_secret | Database password |
| `CLOUDAI_JWT_SECRET` | (dev default) | JWT signing secret (prod requires 32+ chars, high entropy) |
| `CLOUDAI_LOG_LEVEL` | info | Log level (debug/info/warn/error) |
| `CLOUDAI_DEBUG_ENABLED` | false | Enable /debug/* endpoints (requires JWT admin) |
| `CLOUDAI_DEBUG_ALLOWED_IPS` | (empty) | Optional IP/CIDR allowlist for debug endpoints |
| `OPENAI_API_KEY` | (empty) | OpenAI API key for LLM features |
| `DASHSCOPE_API_KEY` | (empty) | Alibaba DashScope key for Qwen models |
| `OLLAMA_BASE_URL` | http://localhost:11434/v1 | Local Ollama endpoint |

## Enabling LLM Features

The AI Engine supports multiple LLM backends. Set at least one API key to enable generative AI:

```bash
# Option 1: OpenAI
export OPENAI_API_KEY=sk-xxx

# Option 2: Alibaba Cloud DashScope (Qwen)
export DASHSCOPE_API_KEY=sk-xxx

# Option 3: Local models via Ollama (no API key needed)
# Install Ollama: https://ollama.com
ollama serve
ollama pull llama3:8b
```

Without any LLM key, all agents fall back to rule-based logic (scheduling scoring, threshold detection, runbook remediation).

## Verify AI Engine

```bash
# AI Engine health
curl http://localhost:8090/healthz

# Check LLM & model status
curl http://localhost:8090/api/v1/models/status

# Chat with AI assistant
curl -X POST http://localhost:8090/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "How can I improve GPU utilization?"}'

# Get AI insights
curl http://localhost:8090/api/v1/insights
```

## Next Steps

- [API Guide](api-guide.md) -- Full API reference
- [Architecture](architecture.md) -- System design overview
- [Operations Guide](operations-guide.md) -- Production deployment, monitoring, DR
- [Best Practices](best-practices.md) -- Performance tuning and security hardening
- [Examples](../examples/) -- Sample workloads and deployments
