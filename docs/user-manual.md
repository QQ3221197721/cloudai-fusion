# CloudAI Fusion User Manual

> **Version**: 1.0 | **Last Updated**: March 2026

## Table of Contents

- [1. Platform Overview](#1-platform-overview)
- [2. Core Concepts](#2-core-concepts)
- [3. Getting Started](#3-getting-started)
- [4. Cluster Management](#4-cluster-management)
- [5. Workload Management](#5-workload-management)
- [6. GPU Scheduling](#6-gpu-scheduling)
- [7. AI Engine & Agents](#7-ai-engine--agents)
- [8. Edge Computing](#8-edge-computing)
- [9. Cost Management (FinOps)](#9-cost-management-finops)
- [10. Security & Compliance](#10-security--compliance)
- [11. Plugin System](#11-plugin-system)
- [12. AIOps & Automation](#12-aiops--automation)
- [13. Configuration Reference](#13-configuration-reference)
- [14. CLI Reference](#14-cli-reference)

---

## 1. Platform Overview

CloudAI Fusion is a cloud-native AI unified management platform that provides:

- **Multi-cloud cluster management** (Alibaba Cloud, AWS, Azure, GCP, Huawei, Tencent)
- **GPU topology-aware scheduling** with NVLink affinity, MPS/MIG sharing, and RL optimization
- **AI-driven operations** with 4 intelligent agents (scheduling, security, cost, operations)
- **Edge-cloud collaborative computing** with offline operation and model optimization
- **FinOps cost intelligence** with Spot prediction, RI recommendations, and cost analytics
- **Plugin ecosystem** with marketplace, SDK, and developer tools
- **AIOps automation** with predictive scaling, self-healing, and capacity planning
- **Enterprise security** with RBAC, eBPF mesh, OIDC federation, and compliance scanning

### Architecture at a Glance

```
Clients (Web UI / CLI / REST API)
       │
       ▼
  API Server (Go/Gin) ─── Auth, Routing, RBAC
       │
  ┌────┼────┬──────────┐
  ▼    ▼    ▼          ▼
Scheduler  Agent   AI Engine (FastAPI)
(GPU-aware) (Metrics)  (4 AI Agents + LLM)
  │    │    │          │
  └────┴────┴──────────┘
       │
  Kubernetes Clusters (Multi-cloud)
       │
  Data Layer (PostgreSQL, Redis, Kafka, NATS, Prometheus)
```

---

## 2. Core Concepts

| Concept | Description |
|---------|-------------|
| **Cluster** | A Kubernetes cluster imported from any cloud provider |
| **Workload** | An AI training or inference job submitted for scheduling |
| **Node** | A compute node with CPU/GPU resources |
| **GPU Topology** | NVLink/NVSwitch interconnect graph for optimal GPU placement |
| **Extension Point** | A hook in the plugin system where custom logic attaches |
| **Edge Node** | A remote compute node with offline operation capability |
| **Tenant** | An isolated organizational unit with resource quotas |

### User Roles

| Role | Permissions |
|------|-------------|
| **admin** | Full access: user management, cluster CRUD, security policies |
| **operator** | Cluster operations, workload management, monitoring |
| **developer** | Submit workloads, view resources, access AI assistant |
| **viewer** | Read-only access to dashboards and reports |

---

## 3. Getting Started

### Prerequisites

| Component | Version | Purpose |
|-----------|---------|---------|
| Go | 1.22+ | Build platform binaries |
| Python | 3.11+ | AI engine components |
| Docker | 24+ | Container runtime |
| Kubernetes | 1.30+ | Cluster orchestration |
| PostgreSQL | 16+ | Primary data store |
| Redis | 7+ | Caching and sessions |

### Quick Start (Docker Compose)

```bash
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion
cp .env.example .env    # Edit with your settings
docker compose up -d    # Start all 13 services
```

Access points after startup:

| Service | URL | Default Credentials |
|---------|-----|---------------------|
| API Server | http://localhost:8080 | admin / admin123 |
| AI Engine | http://localhost:8090 | — |
| Grafana | http://localhost:3000 | admin / cloudai |
| Prometheus | http://localhost:9090 | — |
| Jaeger | http://localhost:16686 | — |

### First Steps

```bash
# 1. Authenticate
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.token')

# 2. List cloud providers
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/providers

# 3. Import a cluster
curl -X POST http://localhost:8080/api/v1/clusters \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"prod-gpu","provider":"aliyun","region":"cn-hangzhou"}'

# 4. Submit an AI training job
curl -X POST http://localhost:8080/api/v1/workloads \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"bert-train","type":"training","gpu_count_required":4}'
```

---

## 4. Cluster Management

### Importing a Cluster

CloudAI Fusion supports importing existing Kubernetes clusters from six cloud providers:

```bash
# Alibaba Cloud ACK
POST /api/v1/clusters
{
  "name": "ack-production",
  "provider": "aliyun",
  "region": "cn-hangzhou",
  "kubernetes_version": "1.30",
  "endpoint": "https://ack-api.cn-hangzhou.aliyuncs.com"
}
```

### Cluster Health Monitoring

```bash
GET /api/v1/clusters/{id}/health
```

Returns node status, resource utilization, component health, and alerts.

### GPU Topology Discovery

```bash
GET /api/v1/clusters/{id}/gpu-topology
```

Discovers NVLink/NVSwitch interconnect topology and bandwidth between GPU pairs.

---

## 5. Workload Management

### Supported Workload Types

| Type | Description | GPU Support |
|------|-------------|-------------|
| `training` | Distributed model training | Multi-GPU, multi-node |
| `inference` | Model serving and prediction | Single/Multi-GPU |
| `batch` | Batch data processing | Optional GPU |
| `interactive` | Jupyter notebooks, dev environments | Optional GPU |

### Workload Lifecycle

```
Pending → Queued → Scheduled → Running → Completed
                                  ↓
                               Failed → Retrying
```

### Submitting a Training Job

```bash
POST /api/v1/workloads
{
  "name": "llama-finetune",
  "type": "training",
  "framework": "pytorch",
  "image": "nvcr.io/nvidia/pytorch:24.03-py3",
  "gpu_type_required": "nvidia-a100",
  "gpu_count_required": 8,
  "priority": 100,
  "resource_request": {
    "cpu": "32",
    "memory": "128Gi"
  },
  "tolerations": ["gpu-node"],
  "environment": {
    "NCCL_DEBUG": "INFO",
    "MASTER_PORT": "29500"
  }
}
```

---

## 6. GPU Scheduling

### Scheduling Algorithm

The GPU scheduler uses a multi-stage pipeline:

1. **Priority Queue** — Workloads ranked by priority with preemption support
2. **Candidate Filtering** — Nodes filtered by GPU type, capacity, affinity rules
3. **Multi-Factor Scoring** — Weighted scoring across GPU utilization, memory, topology
4. **Topology Optimization** — NVLink bandwidth maximization for multi-GPU jobs
5. **RL Enhancement** — Q-learning optimizer refines placement decisions
6. **GPU Sharing** — MPS (time-slicing) or MIG (partitioning) for inference workloads

### GPU Sharing Modes

| Mode | Best For | Isolation | Granularity |
|------|----------|-----------|-------------|
| **Exclusive** | Training | Full | Whole GPU |
| **MPS** | Inference | Process-level | Time-sliced |
| **MIG** | Mixed | Hardware | GPU slices (1g.5gb, 2g.10gb, etc.) |

### Viewing Schedule Results

```bash
GET /api/v1/workloads/{id}/schedule-result
```

---

## 7. AI Engine & Agents

### Agent Overview

| Agent | Purpose | LLM Fallback |
|-------|---------|--------------|
| **Scheduling Agent** | Optimize GPU placement with LLM reasoning | Multi-factor scoring |
| **Security Agent** | Threat analysis and incident assessment | Threshold-based detection |
| **Cost Agent** | Cost optimization recommendations | Rule-based RI/Spot suggestions |
| **Operations Agent** | Root cause analysis and runbook execution | 6 built-in runbooks |

### Enabling LLM Features

```bash
# OpenAI
export OPENAI_API_KEY=sk-xxx

# Alibaba DashScope (Qwen)
export DASHSCOPE_API_KEY=sk-xxx

# Local Ollama (no key needed)
ollama serve && ollama pull llama3:8b
```

### AI Chat Assistant

```bash
POST /api/v1/chat
{
  "message": "Why is my training job slow on GPU node 03?"
}
```

---

## 8. Edge Computing

### Edge Node Registration

```bash
POST /api/v1/edge/nodes
{
  "name": "factory-edge-01",
  "location": "Shanghai Factory",
  "capabilities": ["gpu_inference", "offline_operation"],
  "resources": {"cpu_cores": 8, "memory_gb": 32, "gpu_count": 1}
}
```

### Offline Operation

Edge nodes support autonomous operation when disconnected:

- **State Machine**: Online → Disconnecting → Offline → Recovering → Online
- **Local Decision Engine**: Autonomous workload scheduling based on cached policies
- **Health Self-Check**: Hardware, software, network, and storage diagnostics

### Model Deployment to Edge

```bash
POST /api/v1/edge/nodes/{id}/deploy
{
  "model_id": "yolov8-detection",
  "compression": {
    "enabled": true,
    "methods": ["pruning", "quantization_aware_training"],
    "target_size_mb": 50
  }
}
```

### Edge-Cloud Sync

- **Vector Clocks** for causal ordering across distributed nodes
- **Delta Compression** for bandwidth-efficient sync (only changed fields)
- **Priority Queue** with 5 levels (Critical → Bulk)

---

## 9. Cost Management (FinOps)

### Spot Instance Intelligence

```bash
# Get interruption prediction
GET /api/v1/finops/spot/predict?instance_type=p4d.24xlarge

# Response
{
  "interruption_probability": 0.35,
  "recommended_action": "hold",
  "predicted_price": 12.45,
  "time_to_interruption": "4h30m"
}
```

### Reserved Instance Recommendations

```bash
GET /api/v1/finops/ri/recommendations
```

Returns optimal RI purchase recommendations with break-even analysis.

### Cost Reports

```bash
# Generate 30-day cost report
GET /api/v1/finops/reports?period_days=30
```

Includes: multi-dimensional attribution, anomaly detection, trend forecasting, optimization suggestions, budget tracking, and efficiency scoring.

---

## 10. Security & Compliance

### RBAC Configuration

```bash
# Create a developer user
POST /api/v1/auth/register
{"username": "dev1", "email": "dev1@company.com", "password": "***", "role": "developer"}
```

### Security Scanning

```bash
# Scan container image
POST /api/v1/security/scan
{"image": "myregistry/model-server:v1.2", "scanner": "trivy"}
```

### Network Policies (eBPF/Cilium)

```bash
# Apply namespace isolation
POST /api/v1/security/policies
{
  "name": "isolate-training",
  "type": "network",
  "namespace": "training",
  "rules": [{"direction": "ingress", "from": ["monitoring"]}]
}
```

---

## 11. Plugin System

### Installing Plugins

```bash
# Search marketplace
GET /api/v1/plugins/marketplace/search?q=gpu-affinity

# Install a plugin
POST /api/v1/plugins/install
{"name": "gpu-affinity-scheduler", "version": "latest"}

# List installed plugins
GET /api/v1/plugins/installed
```

### Extension Points

| Extension Point | Description |
|-----------------|-------------|
| `scheduler.filter` | Filter nodes during scheduling |
| `scheduler.score` | Score nodes for ranking |
| `security.scanner` | Custom security scanners |
| `monitor.collector` | Custom metric collectors |
| `auth.authenticator` | Custom authentication providers |
| `monitor.alerter` | Custom alert channels |

### Plugin Development Quick Start

Use the scaffold generator to create a new plugin project:

```go
generator := plugin.NewScaffoldGenerator(nil)
output, _ := generator.Generate(plugin.ScaffoldConfig{
    Name:            "my-custom-scorer",
    Version:         "0.1.0",
    ExtensionPoints: []plugin.ExtensionPoint{plugin.ExtSchedulerScore},
    WithTest:        true,
    WithConfig:      true,
})
// output.Files contains generated plugin.go, config.go, plugin_test.go
```

---

## 12. AIOps & Automation

### Predictive Auto-Scaling

```bash
# Register workload for auto-scaling
POST /api/v1/aiops/autoscale/workloads
{
  "name": "inference-api",
  "namespace": "production",
  "min_replicas": 2,
  "max_replicas": 50,
  "target_metrics": [
    {"name": "cpu", "target_value": 70, "weight": 0.3},
    {"name": "rps", "target_value": 1000, "weight": 0.5},
    {"name": "latency_p99", "target_value": 100, "weight": 0.2}
  ]
}
```

### Self-Healing

Automatic fault detection and remediation:

| Detector | Threshold | Auto-Remediation |
|----------|-----------|------------------|
| Node CPU Overload | >95% for 2min | Cordon & drain |
| Pod Restart Loop | >5 restarts in 10min | Restart + scale up |
| GPU Temperature | >90°C for 1min | Throttle + migrate |
| High Error Rate | >5% for 2min | Rollback deployment |
| Disk Full | >95% for 1min | Alert + cleanup |

### Capacity Planning

```bash
# Get capacity forecast
GET /api/v1/aiops/capacity/forecast?cluster_id=xxx&horizon_days=90

# Get expansion recommendations
GET /api/v1/aiops/capacity/plan?cluster_id=xxx
```

---

## 13. Configuration Reference

### Main Configuration File (`cloudai-fusion.yaml`)

```yaml
server:
  port: 8080
  mode: release           # debug, release, test

database:
  host: localhost
  port: 5432
  name: cloudai
  user: cloudai
  password: ${CLOUDAI_DB_PASSWORD}
  max_open_conns: 50
  max_idle_conns: 10

redis:
  addr: localhost:6379
  db: 0

auth:
  jwt_secret: ${CLOUDAI_JWT_SECRET}
  token_expiry: 24h
  bcrypt_cost: 12

scheduler:
  evaluation_interval: 10s
  enable_preemption: true
  enable_rl_optimizer: true
  gpu_topology_discovery: true

edge:
  sync_interval: 30s
  offline_max_duration: 72h
  enable_model_compression: true

finops:
  spot_prediction_window: 7d
  ri_analysis_window: 30d
  cost_anomaly_threshold: 2.0

aiops:
  autoscale_evaluation: 30s
  self_heal_detection: 15s
  capacity_forecast_horizon: 90d
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLOUDAI_DB_HOST` | localhost | PostgreSQL host |
| `CLOUDAI_DB_PORT` | 5432 | PostgreSQL port |
| `CLOUDAI_DB_PASSWORD` | — | Database password |
| `CLOUDAI_JWT_SECRET` | — | JWT signing key |
| `CLOUDAI_LOG_LEVEL` | info | debug/info/warn/error |
| `OPENAI_API_KEY` | — | OpenAI API key |
| `DASHSCOPE_API_KEY` | — | Alibaba DashScope key |
| `OLLAMA_BASE_URL` | http://localhost:11434/v1 | Ollama endpoint |

---

## 14. CLI Reference

### Build Commands

```bash
make build              # Build all binaries
make build-apiserver    # Build API server only
make build-scheduler    # Build scheduler only
make build-agent        # Build agent only
```

### Test Commands

```bash
make test               # Run all tests
make test-go            # Go tests with coverage
make test-integration   # Integration tests
make test-e2e           # End-to-end tests
make test-phase4        # Phase 4 advanced feature tests
make bench-all          # All benchmarks
```

### Docker Commands

```bash
make docker-build       # Build all images
make docker-push        # Push to registry
make docker-up          # Start with docker-compose
make docker-down        # Stop all services
```

### Development Commands

```bash
make dev                # Start dev environment
make lint               # Run linters
make fmt                # Format code
make swagger            # Generate API docs
make verify-advanced    # Verify advanced features compile
```

---

## Further Reading

- [Quick Start Guide](quickstart.md)
- [API Reference](api-guide.md)
- [Architecture Overview](architecture.md)
- [Operations Guide](operations-guide.md)
- [Troubleshooting](troubleshooting.md)
- [Best Practices](best-practices.md)
- [Contributing](../CONTRIBUTING.md)
