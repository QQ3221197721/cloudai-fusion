<p align="center">
  <h1 align="center">CloudAI Fusion</h1>
  <p align="center">Cloud-Native AI Unified Management Platform</p>
  <p align="center">
    <a href="https://github.com/cloudai-fusion/cloudai-fusion/actions"><img src="https://github.com/cloudai-fusion/cloudai-fusion/workflows/CloudAI%20Fusion%20CI%2FCD/badge.svg" alt="CI"></a>
    <a href="https://codecov.io/gh/cloudai-fusion/cloudai-fusion"><img src="https://codecov.io/gh/cloudai-fusion/cloudai-fusion/branch/main/graph/badge.svg" alt="Coverage"></a>
    <a href="https://goreportcard.com/report/github.com/cloudai-fusion/cloudai-fusion"><img src="https://goreportcard.com/badge/github.com/cloudai-fusion/cloudai-fusion" alt="Go Report"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License"></a>
    <a href="https://github.com/cloudai-fusion/cloudai-fusion/releases"><img src="https://img.shields.io/github/v/release/cloudai-fusion/cloudai-fusion" alt="Release"></a>
  </p>
</p>

---

**CloudAI Fusion** is an open-source platform that unifies cloud-native infrastructure management with AI-powered resource scheduling. It solves three core enterprise pain points:

- **Cloud-native deployment complexity** — 68% of enterprises struggle with cluster management
- **Multi-cloud security fragmentation** — 86% of organizations use multi-cloud but face siloed security
- **AI resource waste** — GPU utilization under 30% in traditional deployment models

## Key Features

| Feature | Description |
|---------|-------------|
| **Multi-Cloud Management** | Unified API for Alibaba Cloud ACK, AWS EKS, Azure AKS, GCP GKE, Huawei CCE, Tencent TKE |
| **GPU Topology-Aware Scheduling** | NVLink-aware placement, fine-grained GPU sharing, preemption, RL-based optimization |
| **4 AI Agents** | Scheduling optimizer, security monitor, cost analyzer, operations automator |
| **LLM Integration** | OpenAI GPT-4o / DashScope Qwen-Max / Ollama / vLLM with graceful fallback |
| **AI Chat Assistant** | Conversational operations assistant with LLM-powered incident analysis |
| **eBPF Service Mesh** | Sidecarless networking via Istio Ambient / Cilium with <1% overhead |
| **Wasm Runtime** | Millisecond cold-start functions for serverless & edge workloads |
| **Edge-Cloud Architecture** | Three-tier (Cloud → Edge → Terminal) with 50B-parameter edge model support |
| **Security & Compliance** | Pod security, network policies, CIS benchmarks, vulnerability scanning, threat detection |
| **Full Observability** | Prometheus metrics, OpenTelemetry tracing, Grafana dashboards, intelligent alerting |
| **Feature Toggles** | Runtime feature flags with profiles (minimal / standard / full) for modular deployment |
| **Docker Optimized** | Multi-stage builds, distroless images for Go services, GPU image reduced from 5-8GB to ~2-3GB |

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         CloudAI Fusion                              │
├─────────────┬──────────────┬─────────────┬─────────────────────────┤
│  API Server │  Scheduler   │    Agent    │      AI Engine          │
│   (Go/Gin)  │ (GPU-aware)  │ (DaemonSet) │   (Python/FastAPI)     │
├─────────────┴──────────────┴─────────────┴─────────────────────────┤
│  Auth │ Cloud │ Cluster │ Security │ Monitor │ Mesh │ Wasm │ Edge  │
├───────┴───────┴─────────┴──────────┴─────────┴──────┴──────┴───────┤
│            PostgreSQL  │  Redis  │  Kafka  │  NATS  │  Prometheus     │
└─────────────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop) (with Docker Compose)

### Run (one command)

**Windows:**
```bash
cd cloudai-fusion
start.bat
```

**Linux / macOS:**
```bash
cd cloudai-fusion
chmod +x start.sh && ./start.sh
```

This starts all services (API Server, Scheduler, Agent, AI Engine, PostgreSQL, Redis, Kafka, NATS, Prometheus, Grafana, Jaeger).

### Verify

```bash
# Health check
curl http://localhost:8080/healthz

# Login (get JWT token)
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# List clusters (use token from login response)
curl http://localhost:8080/api/v1/clusters \
  -H "Authorization: Bearer <token>"
```

### Service Endpoints

| Service | URL |
|---------|-----|
| API Server | http://localhost:8080 |
| AI Engine | http://localhost:8090 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 (admin / cloudai) |
| Jaeger | http://localhost:16686 |

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.25, Gin, Cobra, Viper, GORM |
| AI Engine | Python 3.11, FastAPI, PyTorch, TensorFlow, NumPy, scikit-learn, OpenAI SDK |
| Container | Kubernetes 1.30+, Istio Ambient, Cilium eBPF |
| Database | PostgreSQL 16, Redis 7 |
| Messaging | Apache Kafka, NATS |
| Monitoring | Prometheus, Grafana, OpenTelemetry, Jaeger |
| CI/CD | GitHub Actions, Docker, Helm 3 |

## Project Structure

```
cloudai-fusion/
├── cmd/                          # Service entry points
│   ├── apiserver/                # API Server
│   ├── scheduler/                # GPU Scheduler
│   ├── agent/                    # Node Agent
│   └── healthcheck/              # Lightweight healthcheck binary (for distroless)
├── pkg/                          # Go packages (48 packages, 47/47 tests pass)
│   ├── api/                      # HTTP routes, handlers, debug endpoints (6-layer auth)
│   ├── auth/                     # JWT + RBAC + OIDC federation (4 roles, 20+ perms)
│   ├── cache/                    # Redis cache + distributed lock + PubSub
│   ├── cloud/                    # Multi-cloud providers (6 clouds)
│   ├── cluster/                  # Kubernetes cluster management
│   ├── config/                   # Unified configuration + JWT secret validation
│   ├── edge/                     # Edge-cloud architecture + 50B model quantization
│   ├── feature/                  # Runtime feature toggles (minimal/standard/full)
│   ├── mesh/                     # eBPF service mesh
│   ├── monitor/                  # Prometheus metrics & alerting
│   ├── scheduler/                # GPU scheduling + queue snapshot persistence
│   ├── security/                 # Policy, scanning, compliance, threats
│   ├── store/                    # Database layer (GORM)
│   └── wasm/                     # WebAssembly container runtime
├── ai/                           # Python AI components
│   ├── agents/                   # Multi-agent FastAPI server + LLM client
│   ├── anomaly/                  # Anomaly detection engine
│   └── scheduler/                # RL scheduling optimizer
├── deploy/helm/                  # Helm chart (7 templates)
├── docker/                       # Dockerfiles (5 services, distroless + multi-stage)
├── monitoring/                   # Prometheus + Grafana configs
├── scripts/                      # env-generate, diagnose, setup-gpu, deploy-cloud
├── api/                          # OpenAPI 3.1 specification
├── .devcontainer/                # Dev Container (Go + Python + full toolchain)
├── docker-compose.yml            # Full-stack local deployment (profiles support)
├── docker-compose.gpu.yml        # GPU overlay (NVIDIA runtime)
├── Makefile                      # Build, test, deploy, diagnose commands
└── start.bat / start.sh          # One-click start (--gpu, --minimal flags)
```

## Development

```bash
# First-time setup (generate .env with secure secrets)
make setup

# Build all Go binaries
make build

# Run tests
make test

# Run tests with coverage report
make coverage-report

# Run linter
make lint

# Build Docker images (optimized: distroless for Go, multi-stage for AI)
make docker-build

# Deploy to Kubernetes
make helm-install

# Start with GPU support
./start.sh --gpu          # Linux / macOS
start.bat --gpu           # Windows (WSL2 + NVIDIA)

# Start minimal mode (core services only)
./start.sh --minimal

# Health diagnostics
make diagnose

# Feature flag management
make features-list
```

## API Overview

Full specification: [`api/openapi.yaml`](api/openapi.yaml)

| Endpoint Group | Description |
|---------------|-------------|
| `POST /api/v1/auth/login` | JWT authentication |
| `GET /api/v1/clusters` | List managed K8s clusters |
| `GET /api/v1/providers` | List cloud providers |
| `POST /api/v1/workloads` | Submit AI workload |
| `GET /api/v1/security/policies` | Security policies |
| `GET /api/v1/monitoring/alerts/events` | Alert events |
| `GET /api/v1/cost/summary` | Cost analysis |
| `GET /api/v1/mesh/status` | Service mesh status |
| `POST /api/v1/wasm/deploy` | Deploy Wasm function |
| `GET /api/v1/edge/topology` | Edge-cloud topology |
| **AI Engine** (port 8090) | |
| `POST /api/v1/scheduling/optimize` | LLM-enhanced GPU scheduling |
| `POST /api/v1/anomaly/detect` | Anomaly detection + threat analysis |
| `POST /api/v1/cost/analyze` | Cost optimization with LLM insights |
| `GET /api/v1/insights` | Dynamic AI-powered insights |
| `GET /api/v1/models/status` | Honest model & LLM status |
| `POST /api/v1/chat` | Conversational AI assistant |
| `POST /api/v1/ops/incident` | Incident root cause analysis |
| `POST /api/v1/ops/scaling` | Predictive scaling |
| `GET /api/v1/ops/history` | Incident history |
| **Debug** (requires `CLOUDAI_DEBUG_ENABLED=true` + JWT admin) | |
| `GET /debug/info` | Runtime information |
| `GET /debug/pprof/` | Go pprof (rate-limited) |
| `PUT /debug/log-level` | Dynamic log level |
| `GET /debug/services` | Cross-service health probe |

## Roadmap

| Version | Timeline | Focus |
|---------|----------|-------|
| **v0.1 MVP** | 2026 Q2 | Core management, basic scheduling, auth, monitoring |
| **v1.0** | 2026 Q3 | Multi-cloud SDK integration, Istio Ambient, AIOps |
| **v2.0** | 2026 Q4 | Full Agent system, edge deployment, cost optimization |
| **v3.0** | 2027 H1 | Cross-cloud security, heterogeneous scheduling, plugin ecosystem |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

[Apache License 2.0](LICENSE)
