# Quick Start Guide

Get CloudAI Fusion running quickly — and understand, at every step, whether you're
running on **real** backends or **simulated** ones.

## Run modes (read this first)

CloudAI Fusion never silently fakes a backend. The `run_mode` setting controls what is allowed:

| `CLOUDAI_RUN_MODE` | Simulated backends | Typical use |
|--------------------|--------------------|-------------|
| `simulation` (default in dev) | allowed | local development, tests |
| `degraded` | allowed, surfaced in `/readyz` + warnings | staging |
| `production` | **forbidden — the server refuses to boot** | production |

Check what's real at any time:

```bash
curl http://localhost:8080/api/v1/capabilities
```

## Prerequisites

| Requirement | Version | Check |
|-------------|---------|-------|
| Go | 1.25+ | `go version` |
| PostgreSQL | 16+ (for persistence) | `psql --version` |
| Redis / NATS or Kafka / Kubernetes | for production run mode | — |
| Docker (optional) | 24+ | `docker --version` |

## Option 1: Local development (simulation mode — fastest)

```bash
git clone https://github.com/QQ3221197721/cloudai-fusion.git
cd cloudai-fusion
go build ./...

# run_mode defaults to simulation in a development env → boots with in-memory fallbacks
go run ./cmd/apiserver --config cloudai-fusion.yaml
```

Verify:

```bash
curl http://localhost:8080/healthz
# {"service":"cloudai-fusion-apiserver","status":"healthy",...}

curl http://localhost:8080/api/v1/capabilities
# lists each subsystem as "real" or "simulated" + run_mode
```

> Login/registration need a database. Without one, auth endpoints return 503 (by design) —
> the rest of the API still serves. Attach PostgreSQL (below) to enable auth.

## Option 2: With real backends (degraded / production)

### 1. PostgreSQL

```sql
CREATE USER cloudai WITH PASSWORD 'change-me-strong';
CREATE DATABASE cloudai OWNER cloudai;
```

```bash
psql -U cloudai -d cloudai -f scripts/init-db.sql   # seeds demo admin (admin/admin123)
```

### 2. Start with real dependencies

```bash
# Linux / macOS
export CLOUDAI_RUN_MODE=degraded          # or: production (strict)
export CLOUDAI_DB_PASSWORD='change-me-strong'
export CLOUDAI_REDIS_ADDR=localhost:6379  # real Redis (go-redis)
export CLOUDAI_NATS_URL=nats://localhost:4222   # real NATS (or set kafka_brokers)
export CLOUDAI_JWT_SECRET="$(openssl rand -hex 32)"
go run ./cmd/apiserver --config cloudai-fusion.yaml
```

```powershell
# Windows PowerShell
$env:CLOUDAI_RUN_MODE = "degraded"
$env:CLOUDAI_DB_PASSWORD = "change-me-strong"
$env:CLOUDAI_JWT_SECRET = "<32+ byte hex>"
go run ./cmd/apiserver --config cloudai-fusion.yaml
```

In **`production`** mode, if any subsystem can only offer a simulated backend the process
**exits at boot** with a message like:

```
startup blocked by run_mode policy: run_mode=production but 1 subsystem(s) are simulated:
[messaging.producer(driver=memory)] — configure real backends or lower run_mode
```

Bring up local middleware quickly with Docker:

```bash
docker run -d --name redis -p 6379:6379 redis:7
docker run -d --name nats  -p 4222:4222 nats:2
# Kubernetes Lease leader election / ArgoCD need a cluster (kind/minikube).
```

## Option 3: Docker Compose (full stack)

```bash
make setup          # generate .env with secure secrets (Windows: scripts/env-generate.bat)
docker compose up -d
```

| Service | URL | Credentials |
|---------|-----|-------------|
| API Server | http://localhost:8080 | admin / admin123 (seeded demo) |
| AI Engine | http://localhost:8090 | - |
| Prometheus | http://localhost:9090 | - |
| Grafana | http://localhost:3000 | admin / cloudai |
| Jaeger UI | http://localhost:16686 | - |

## Configuration

Precedence: **CLI flags > environment (`CLOUDAI_` prefix) > `cloudai-fusion.yaml` > defaults.**

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDAI_RUN_MODE` | (derives from env) | `simulation` / `degraded` / `production` |
| `CLOUDAI_DB_HOST` / `_PORT` / `_NAME` / `_USER` | localhost / 5432 / cloudai / cloudai | PostgreSQL |
| `CLOUDAI_DB_PASSWORD` | (empty) | DB password (required in production) |
| `CLOUDAI_REDIS_ADDR` | localhost:6379 | Redis (real go-redis when reachable) |
| `CLOUDAI_NATS_URL` | nats://localhost:4222 | NATS (real nats.go when reachable) |
| `CLOUDAI_KAFKA_BROKERS` | localhost:9092 | Kafka brokers (sarama) |
| `CLOUDAI_JWT_SECRET` | (dev auto-gen) | JWT secret; production requires 32+ bytes, high entropy |
| `CLOUDAI_LOG_LEVEL` | info | debug / info / warn / error |
| `CLOUDAI_DEBUG_ENABLED` | false | Enable `/debug/*` (JWT admin + optional IP allowlist) |
| `OPENAI_API_KEY` / `DASHSCOPE_API_KEY` | (empty) | Enable LLM features |
| `ARGOCD_SERVER` / `ARGOCD_AUTH_TOKEN` | (empty) | Enable real ArgoCD GitOps sync |

## Enabling LLM features

The AI Engine works without an LLM (rule-based heuristics). Set at least one backend to
enable generative AI:

```bash
export OPENAI_API_KEY=sk-xxx          # or
export DASHSCOPE_API_KEY=sk-xxx       # Alibaba Qwen; or run Ollama locally:
ollama serve && ollama pull llama3:8b
```

Check honest model status:

```bash
curl http://localhost:8090/api/v1/models/status   # which models/LLMs are real vs rule-based
```

## Verifying released images (supply chain)

```bash
# cosign keyless signature (Sigstore) on all 4 images
make verify-signatures GHCR_REPO=QQ3221197721/cloudai-fusion IMAGE_TAG=<tag>

# SLSA L3 provenance (requires slsa-verifier)
make verify-provenance GHCR_REPO=QQ3221197721/cloudai-fusion IMAGE_TAG=<tag>
```

## Next steps

- [Architecture](architecture.md) — run-mode/capability model, components, data flow
- [API Guide](api-guide.md) — full API reference
- [Operations Guide](operations-guide.md) — production deployment, monitoring, DR
- [Best Practices](best-practices.md) — performance and security hardening
- [Examples](../examples/) — sample workloads and deployments
