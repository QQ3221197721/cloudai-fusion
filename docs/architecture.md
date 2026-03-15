# Architecture Overview

## System Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                            Clients                                    │
│              (Web UI / CLI / kubectl / REST API)                      │
└────────────────────────────┬─────────────────────────────────────────┘
                             │ HTTPS / gRPC
┌────────────────────────────▼─────────────────────────────────────────┐
│                        API Server (Go/Gin)                            │
│  ┌──────────┬──────────┬──────────┬──────────┬──────────┬─────────┐ │
│  │   Auth   │  Cloud   │ Cluster  │ Workload │ Security │  Cost   │ │
│  │  (JWT+   │ Provider │ Manager  │ Lifecycle│ Manager  │ Analyst │ │
│  │  RBAC)   │ Manager  │          │ Manager  │          │         │ │
│  └──────────┴──────────┴──────────┴──────────┴──────────┴─────────┘ │
└─────┬──────────────┬──────────────┬──────────────┬──────────────────┘
      │              │              │              │
┌─────▼─────┐ ┌─────▼─────┐ ┌─────▼──────────────▼─────────────────┐
│ Scheduler │ │   Agent   │ │           AI Engine (FastAPI)          │
│(GPU-aware)│ │(DaemonSet)│ │ ┌──────────┬──────────┬────────────┐ │
│           │ │           │ │ │ Schedule │Security  │    Cost     │ │
│ Topology  │ │  Metrics  │ │ │  Agent   │  Agent   │   Agent    │ │
│ RL Optim  │ │ Collector │ │ ├──────────┴──────────┴────────────┤ │
│ MPS/MIG   │ │ DCGM+node │ │ │ Operations Agent (LLM-driven)   │ │
└─────┬─────┘ └─────┬─────┘ │ ├─────────────────────────────────┤ │
      │              │       │ │ LLM Client Abstraction Layer    │ │
      │              │       │ │ OpenAI │ DashScope │ Ollama     │ │
      │              │       │ └─────────────────────────────────┘ │
      │              │       └───────────────────────────────┬─────┘
      │              │                                       │
┌─────▼──────────────▼───────────────────────────────────────▼─────────┐
│                      Kubernetes Clusters                              │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────────────────┐  │
│  │  Alibaba    │  │    AWS EKS   │  │  Azure AKS / GCP GKE /    │  │
│  │  Cloud ACK  │  │              │  │  Huawei CCE / Tencent TKE  │  │
│  └─────────────┘  └──────────────┘  └────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
      │
┌─────▼────────────────────────────────────────────────────────────────┐
│                        Data Layer                                     │
│  ┌────────────┐  ┌──────────┐  ┌──────────┐  ┌───────┐  ┌───────────────────┐  │
│  │ PostgreSQL │  │  Redis   │  │  Kafka   │  │ NATS  │  │   Prometheus      │  │
│  │  (GORM)    │  │ (Cache)  │  │ (Events) │  │(RT Msg)│  │   (Metrics TSDB)  │  │
│  └────────────┘  └──────────┘  └──────────┘  └───────┘  └───────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
```

## Component Details

### API Server (`cmd/apiserver`)
- **Framework**: Go + Gin
- **Responsibilities**: REST API gateway, request routing, authentication, authorization
- **Key packages**: `pkg/api`, `pkg/auth`, `pkg/cloud`, `pkg/cluster`, `pkg/workload`

### Scheduler (`cmd/scheduler`)
- **Framework**: Go
- **Responsibilities**: GPU topology-aware scheduling, NVLink affinity, preemption, RL optimization
- **Key packages**: `pkg/scheduler` (engine, gpu_topology, rl_optimizer, gpu_sharing)
- **GPU Topology**: nvidia-smi topo matrix discovery, NVLink bandwidth estimation
- **RL Optimization**: Q-learning tabular optimizer (24 actions × 5D state space, epsilon-greedy)
- **GPU Sharing**: NVIDIA MPS (multi-process service) and MIG (multi-instance GPU) CLI integration

### Agent (`cmd/agent`)
- **Framework**: Go + Cobra
- **Responsibilities**: Node-level operations, metrics collection, AI-driven insights
- **Key packages**: `pkg/agent`
- **Integration**: NVIDIA DCGM exporter, node_exporter (Prometheus format), 3-tier data source chain

### AI Engine (`ai/`)
- **Framework**: Python + FastAPI
- **Responsibilities**: 4 AI agents (scheduling, security, cost, operations), LLM integration, chat assistant
- **Key modules**:
  - `ai/agents/llm_client.py` -- Unified LLM client (OpenAI/DashScope/Ollama/vLLM) with 3-strategy JSON parsing
  - `ai/agents/fine_tuning.py` -- Domain-specific LLM fine-tuning pipeline (OpenAI/DashScope/LoRA)
  - `ai/agents/operations_agent.py` -- LLM-powered incident analysis with rule-based runbook fallback
  - `ai/agents/server.py` -- Multi-agent FastAPI server with all endpoints
  - `ai/anomaly/detector.py` -- Ensemble anomaly detection (Z-score, IQR, EMA, RoC)
  - `ai/scheduler/train.py` -- RL scheduling model trainer
  - `ai/tracing.py` -- OpenTelemetry W3C Trace Context propagation (cross-language with Go)

## LLM Integration Architecture

```
┌──────────────────────────────────────────────────────┐
│                    LLM Client                         │
│  ┌─────────┐  ┌───────────┐  ┌────────┐  ┌───────┐ │
│  │ OpenAI  │  │ DashScope │  │ Ollama │  │ vLLM  │ │
│  │ GPT-4o  │  │ Qwen-Max  │  │ Local  │  │ Self- │ │
│  │         │  │           │  │ Models │  │ host  │ │
│  └────┬────┘  └─────┬─────┘  └───┬────┘  └───┬───┘ │
│       │             │            │            │      │
│       └─────────────┴────────────┴────────────┘      │
│              Priority-based Provider Chain            │
│              (auto-fallback on failure)               │
└──────────────┬────────────────────────┬──────────────┘
               │                        │
  ┌────────────▼────┐      ┌────────────▼────────────┐
  │ Prompt Templates │      │  Structured JSON Output  │
  │ - scheduling     │      │  - 3-strategy parsing:   │
  │ - security       │      │    1. Direct JSON parse  │
  │ - cost           │      │    2. Regex code block   │
  │ - operations     │      │       extraction         │
  │ - insights       │      │    3. Bracket-based find │
  │ - chat           │      │  - response_format:      │
  └─────────────────┘      │    json_object           │
                            └──────────────────────────┘
```

**Graceful Degradation**: When no LLM backend responds, every agent falls back to rule-based logic:
- Scheduling: multi-factor scoring only (no LLM reasoning)
- Security: threshold-based detection only (no threat analysis)
- Cost: rule-based recommendations (spot, GPU sharing, reserved)
- Operations: 6 built-in runbooks (gpu_failure, node_pressure, pod_crash, oom, network, default)
- Insights: data-driven rule generation (not hardcoded)
- Chat: keyword-matching response

## GPU Scheduling Architecture (VP3)

```
                    Workload Request
                         │
                ┌────────▼────────┐
                │  Priority Queue  │
                │  (preemption)    │
                └────────┬────────┘
                         │
              ┌──────────▼──────────┐
              │  Candidate Nodes    │
              │  (K8s API query)    │
              └──────────┬──────────┘
                         │
         ┌───────────────┼───────────────┐
         │               │               │
    ┌────▼────┐   ┌──────▼──────┐  ┌─────▼─────┐
    │Multi-   │   │ GPU Topology│  │    RL      │
    │Factor   │   │ NVLink/NVS  │  │ Q-learning │
    │Scoring  │   │ Bandwidth   │  │ Optimizer  │
    └────┬────┘   └──────┬──────┘  └─────┬─────┘
         │               │               │
         └───────────────┼───────────────┘
                         │
              ┌──────────▼──────────┐
              │  GPU Sharing Check  │
              │  MPS / MIG / None   │
              └──────────┬──────────┘
                         │
              ┌──────────▼──────────┐
              │  Real Utilization   │
              │  nvidia-smi → K8s   │
              │  API → fallback     │
              └──────────┬──────────┘
                         │
                   ScheduleResult
```

## Data Flow

1. **User Request** → API Server (JWT auth) → Route to handler
2. **Workload Submit** → Store in PostgreSQL → Queue for Scheduler
3. **Scheduling** → Read K8s nodes → GPU topology + RL + multi-factor score → Bind Pod
4. **Monitoring** → Agent collects GPU/CPU metrics via DCGM → Prometheus scrapes → Grafana displays
5. **AI Insights** → AI Engine analyzes metrics → LLM reasoning (or rule fallback) → API serves to user
6. **Incident Response** → Operations Agent → LLM root cause analysis → Remediation script → Runbook fallback
7. **Chat** → User message → LLM chat completion → Keyword fallback if unavailable

## Security Architecture

- **Authentication**: JWT tokens with configurable expiry, production-enforced entropy validation
- **Authorization**: RBAC with 4 roles (admin, operator, developer, viewer) and 20+ permissions
- **OIDC Federation**: Cross-cloud identity via Discovery + Token Exchange + JWKS rotation + JIT provisioning
- **Network**: eBPF/Cilium service mesh with mTLS
- **Compliance**: CIS K8s Benchmark checks (via K8s API), pod security policies
- **Vulnerability Scanning**: Trivy/Grype CLI integration for container images
- **Threat Detection**: Rule-based engine with MITRE ATT&CK framework mapping
- **Audit Logging**: DB-persisted audit trail with event correlation

### Debug Endpoint Security (Defense in Depth)

All `/debug/*` endpoints are protected by 6 layers:

| Layer | Mechanism | Configuration |
|-------|-----------|---------------|
| 1. Safe Default | Disabled unless `CLOUDAI_DEBUG_ENABLED=true` | Environment variable |
| 2. JWT Authentication | Same `AuthMiddleware()` as `/api/v1/*` | Bearer token required |
| 3. Admin Role | Only `role=admin` users allowed | RBAC enforcement |
| 4. IP Allowlist | Optional CIDR/IP network restriction | `CLOUDAI_DEBUG_ALLOWED_IPS` |
| 5. Audit Log | All access logged with user identity | stderr output |
| 6. Rate Limiting | pprof endpoints limited to ~2 req/min | Token bucket (anti-DoS) |

## Feature Toggle System

Runtime feature flags allow modular deployment:

```yaml
# cloudai-fusion.yaml
features:
  profile: standard          # minimal | standard | full
  edge_computing: true
  service_mesh: true
  wasm_runtime: false         # opt-in
  finops: true
```

API: `GET /api/v1/features`, `PUT /api/v1/features/:key`

## Container Image Optimization

| Image | Base | Size | Key Technique |
|-------|------|------|---------------|
| apiserver | `distroless/static` | ~15MB | CGO_ENABLED=0, no shell |
| scheduler | `distroless/static` | ~12MB | Static binary, nonroot user |
| agent | `distroless/static` | ~10MB | Static binary, nonroot user |
| ai-engine (CPU) | `python:3.11-slim` | ~900MB | Multi-stage, venv-only copy |
| ai-engine (GPU) | `nvidia/cuda:12.4.1-base` | ~2-3GB | PyTorch bundles CUDA, no cudnn-runtime |
