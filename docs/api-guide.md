# API Guide

Base URL: `http://localhost:8080`

All protected endpoints require a JWT token in the `Authorization: Bearer <token>` header.

## Authentication

### Login

```
POST /api/v1/auth/login
```

**Request:**
```json
{
  "username": "admin",
  "password": "admin123"
}
```

**Response (200):**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-03-05T13:00:00Z",
  "user": {
    "id": "00000000-0000-0000-0000-000000000001",
    "username": "admin",
    "role": "admin"
  }
}
```

### Register

```
POST /api/v1/auth/register
```

**Request:**
```json
{
  "username": "developer1",
  "email": "dev1@example.com",
  "password": "securepassword"
}
```

## Cluster Management

### List Clusters

```
GET /api/v1/clusters
```

### Import Cluster

```
POST /api/v1/clusters
```

**Request:**
```json
{
  "name": "production-gpu",
  "provider": "aliyun",
  "region": "cn-hangzhou",
  "kubernetes_version": "1.30",
  "endpoint": "https://cluster-api.example.com"
}
```

### Get Cluster Details

```
GET /api/v1/clusters/:id
```

### Get Cluster Health

```
GET /api/v1/clusters/:id/health
```

### Get Cluster Nodes

```
GET /api/v1/clusters/:id/nodes
```

### Get GPU Topology

```
GET /api/v1/clusters/:id/gpu-topology
```

## Workload Management

### Create Workload

```
POST /api/v1/workloads
```

**Request:**
```json
{
  "name": "bert-finetune-job",
  "cluster_id": "uuid-of-cluster",
  "type": "training",
  "framework": "pytorch",
  "image": "nvcr.io/nvidia/pytorch:24.03-py3",
  "gpu_type_required": "nvidia-a100",
  "gpu_count_required": 4,
  "priority": 100,
  "resource_request": {
    "cpu": "16",
    "memory": "64Gi"
  }
}
```

**Response (201):**
```json
{
  "id": "generated-uuid",
  "name": "bert-finetune-job",
  "status": "pending",
  "created_at": "2026-03-04T12:00:00Z"
}
```

### List Workloads

```
GET /api/v1/workloads?cluster_id=xxx&status=running&page=1&page_size=20
```

### Update Workload Status

```
PUT /api/v1/workloads/:id/status
```

**Request:**
```json
{
  "status": "running",
  "reason": "Scheduled to node gpu-node-03"
}
```

### Get Workload Events

```
GET /api/v1/workloads/:id/events
```

**Response:**
```json
{
  "events": [
    {
      "from_status": "pending",
      "to_status": "queued",
      "reason": "Accepted by scheduler",
      "created_at": "2026-03-04T12:00:01Z"
    },
    {
      "from_status": "queued",
      "to_status": "scheduled",
      "reason": "Assigned to gpu-node-03",
      "created_at": "2026-03-04T12:00:05Z"
    }
  ],
  "total": 2
}
```

## Cloud Providers

### List Providers

```
GET /api/v1/providers
```

### List Provider GPU Instances

```
GET /api/v1/providers/:name/gpu-instances
```

### Get GPU Pricing

```
GET /api/v1/providers/:name/pricing
```

## Security

### List Policies

```
GET /api/v1/security/policies
```

### Run Vulnerability Scan

```
POST /api/v1/security/scan
```

**Request:**
```json
{
  "cluster_id": "uuid-of-cluster",
  "scan_type": "vulnerability"
}
```

### Compliance Report

```
GET /api/v1/security/compliance/:clusterId/:framework
```

Frameworks: `cis`, `nist`, `pci-dss`, `hipaa`

## Monitoring

### Alert Rules

```
GET /api/v1/monitoring/alerts/rules
```

### Alert Events

```
GET /api/v1/monitoring/alerts/events
```

## Cost Management

### Cost Summary

```
GET /api/v1/cost/summary
```

### Cost Optimization Recommendations

```
GET /api/v1/cost/optimization
```

## Health Endpoints (No Auth Required)

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness probe |
| `GET /version` | Version info |

## Service Mesh (eBPF / Cilium / Istio Ambient)

### Mesh Status

```
GET /api/v1/mesh/status
```

Returns the current service mesh status including mode (Ambient/Sidecar), mTLS state, and connected services.

### List Mesh Policies

```
GET /api/v1/mesh/policies
```

Returns all network policies (L3/L4/L7) managed by the service mesh.

### Create Mesh Policy

```
POST /api/v1/mesh/policies
```

**Request:**
```json
{
  "name": "deny-external-traffic",
  "namespace": "production",
  "type": "network",
  "action": "deny",
  "from": [{"namespace": "external"}],
  "to": [{"port": 8080}]
}
```

### Traffic Metrics

```
GET /api/v1/mesh/traffic?namespace=default
```

Returns real-time traffic metrics (requests/sec, latency, error rates) for the specified namespace.

## WebAssembly Runtime

### List Wasm Modules

```
GET /api/v1/wasm/modules
```

Returns all registered Wasm modules with their runtime (Spin/WasmEdge) and status.

### Register Wasm Module

```
POST /api/v1/wasm/modules
```

**Request:**
```json
{
  "name": "image-resize",
  "source": "oci://registry.example.com/wasm/image-resize:v1",
  "runtime": "spin",
  "memory_limit_mb": 64,
  "environment": {"MAX_WIDTH": "1920"}
}
```

### Deploy Wasm Function

```
POST /api/v1/wasm/deploy
```

**Request:**
```json
{
  "module_name": "image-resize",
  "target_cluster": "edge-cluster-01",
  "replicas": 3,
  "trigger": "http"
}
```

### List Wasm Instances

```
GET /api/v1/wasm/instances
```

Returns all running Wasm instances with their cold-start time and resource usage.

### Wasm Metrics

```
GET /api/v1/wasm/metrics
```

Returns performance metrics for Wasm functions (invocations, latency p50/p99, memory usage).

## Edge-Cloud Architecture

### Edge Topology

```
GET /api/v1/edge/topology
```

Returns the three-tier topology: Cloud nodes, Edge nodes, and Terminal devices.

**Response:**
```json
{
  "cloud_nodes": 3,
  "edge_nodes": 12,
  "terminal_devices": 156,
  "topology": [
    {"id": "edge-01", "tier": "edge", "region": "cn-shanghai", "power_watts": 180, "status": "online"}
  ]
}
```

### List Edge Nodes

```
GET /api/v1/edge/nodes
```

### Register Edge Node

```
POST /api/v1/edge/nodes
```

**Request:**
```json
{
  "name": "factory-edge-01",
  "region": "cn-shanghai",
  "capabilities": ["gpu", "npu"],
  "max_power_watts": 200,
  "endpoint": "grpc://192.168.1.100:9443"
}
```

### Deploy to Edge

```
POST /api/v1/edge/deploy
```

**Request:**
```json
{
  "workload_id": "inference-model-v2",
  "target_nodes": ["factory-edge-01", "factory-edge-02"],
  "model_size_params": "7B",
  "sync_policy": "eventual"
}
```

### Edge Sync Policies

```
GET /api/v1/edge/sync-policies
```

Returns data synchronization policies between cloud and edge nodes.

## Error Format

All errors follow a consistent format:

```json
{
  "error": "descriptive error message"
}
```

## Full OpenAPI Specification

See [`api/openapi.yaml`](../api/openapi.yaml) for the complete OpenAPI 3.1 specification.

---

# AI Engine API (port 8090)

The AI Engine runs separately on port 8090 and provides LLM-powered agent capabilities.
All endpoints gracefully degrade to rule-based logic when no LLM backend is configured.

## Scheduling Optimization

```
POST /api/v1/scheduling/optimize
```

**Request:**
```json
{
  "workload_id": "bert-finetune-001",
  "workload_type": "training",
  "gpu_count": 4,
  "gpu_type": "nvidia-a100",
  "priority": 80,
  "topology_aware": true,
  "available_nodes": [
    {
      "node_name": "gpu-node-01",
      "cluster_id": "prod-cluster",
      "gpu_utilization": 55.0,
      "gpu_memory_usage": 40.0,
      "cpu_utilization": 30.0,
      "memory_utilization": 50.0
    }
  ]
}
```

**Response:**
```json
{
  "workload_id": "bert-finetune-001",
  "recommended_node": "gpu-node-01",
  "gpu_indices": [0, 1, 2, 3],
  "confidence": 0.87,
  "estimated_cost_per_hour": 12.80,
  "optimization_score": 0.872,
  "reasoning": "Multi-factor scoring: gpu-node-01 (score=0.872)",
  "llm_analysis": "Node-01 has optimal NVLink topology for 4-GPU training..."
}
```

## Anomaly Detection

```
POST /api/v1/anomaly/detect
```

**Request:**
```json
{
  "cluster_id": "prod-cluster",
  "metrics": [
    {
      "node_name": "gpu-node-02",
      "cluster_id": "prod-cluster",
      "gpu_utilization": 98.0,
      "gpu_memory_usage": 92.0,
      "cpu_utilization": 85.0,
      "memory_utilization": 91.0
    }
  ]
}
```

**Response:**
```json
{
  "cluster_id": "prod-cluster",
  "anomalies": [
    {"type": "gpu_high_utilization", "node": "gpu-node-02", "value": 98.0},
    {"type": "memory_pressure", "node": "gpu-node-02", "value": 91.0}
  ],
  "severity": "high",
  "analysis": "Analyzed 1 nodes, found 2 anomalies",
  "llm_threat_assessment": "High memory pressure combined with near-max GPU...",
  "recommendations": ["Scale horizontally or increase memory limits"],
  "confidence": 0.92
}
```

## Cost Analysis

```
POST /api/v1/cost/analyze
```

**Request:**
```json
{
  "cluster_ids": ["prod-cluster", "dev-cluster"],
  "period_days": 30,
  "include_projections": true
}
```

## AI Insights

```
GET /api/v1/insights
```

Returns dynamically generated insights (via LLM or rule-based fallback). Each insight includes agent type, severity, title, description, recommendation, and confidence.

## Model Status

```
GET /api/v1/models/status
```

Honest reporting of all AI models and LLM integration status. Shows configured providers, API key availability, and 5 model statuses.

## Chat (AI Assistant)

```
POST /api/v1/chat
```

**Request:**
```json
{
  "message": "How can I improve GPU utilization across my clusters?",
  "context": {
    "cluster_count": 3,
    "gpu_utilization": 45.0,
    "monthly_spend": 28000
  }
}
```

**Response:**
```json
{
  "response": "Based on your 45% average GPU utilization across 3 clusters...",
  "agent_type": "llm_assistant",
  "confidence": 0.85,
  "suggestions": ["Ask about GPU utilization", "Request cost optimization"],
  "llm_provider": "openai"
}
```

## Incident Analysis

```
POST /api/v1/ops/incident
```

**Request:**
```json
{
  "incident_id": "inc-20260304-001",
  "title": "GPU Node Unresponsive",
  "severity": "critical",
  "category": "gpu_failure",
  "description": "gpu-node-03 not responding, ECC errors detected on GPU 2",
  "affected_resources": ["gpu-node-03"]
}
```

**Response** includes root_cause, immediate_actions, remediation_script, and prevention_measures.

## Scaling Recommendations

```
POST /api/v1/ops/scaling
```

**Request:**
```json
{
  "avg_gpu_utilization": 90,
  "avg_cpu_utilization": 75,
  "queue_depth": 15
}
```

## Incident History

```
GET /api/v1/ops/history
```

Returns list of recent incident analyses.
