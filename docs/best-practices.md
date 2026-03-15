# CloudAI Fusion Best Practices

> **Purpose**: Production-proven patterns for getting the most out of CloudAI Fusion

## Table of Contents

- [1. GPU Scheduling Best Practices](#1-gpu-scheduling-best-practices)
- [2. Cost Optimization (FinOps)](#2-cost-optimization-finops)
- [3. Security Hardening](#3-security-hardening)
- [4. Edge Computing Deployment](#4-edge-computing-deployment)
- [5. Plugin Development](#5-plugin-development)
- [6. AIOps Configuration](#6-aiops-configuration)
- [7. High Availability](#7-high-availability)
- [8. Performance Tuning](#8-performance-tuning)

---

## 1. GPU Scheduling Best Practices

### 1.1 Workload Classification

Classify workloads to enable optimal scheduling:

| Workload Type | GPU Mode | Priority | Preemptible |
|---------------|----------|----------|-------------|
| Large-scale training (>8 GPU) | Exclusive | High (50) | No |
| Fine-tuning (1-4 GPU) | Exclusive | Medium (100) | Yes |
| Batch inference | MPS sharing | Low (200) | Yes |
| Real-time inference | MIG partition | High (50) | No |
| Development/experimentation | MPS sharing | Low (300) | Yes |

### 1.2 Topology-Aware Placement

**DO**:
- Enable `gpu_topology_discovery: true` for multi-GPU training
- Set `NCCL_DEBUG=INFO` to verify inter-GPU bandwidth
- Use node affinity for NVLink-connected GPU groups
- Request GPUs in powers of 2 (2, 4, 8) for optimal NVLink topology

**DON'T**:
- Don't spread multi-GPU training across NVSwitch fabric when NVLink is available
- Don't mix GPU types within a single training job
- Don't disable topology scoring for >2 GPU jobs

### 1.3 GPU Memory Management

```yaml
# Recommended: Set explicit GPU memory limits
resource_request:
  nvidia.com/gpu: 4
  nvidia.com/gpu-memory: "32Gi"  # Per-GPU memory limit

# Enable gradient checkpointing for large models
environment:
  PYTORCH_CUDA_ALLOC_CONF: "max_split_size_mb:512"
  CUDA_MEMORY_FRACTION: "0.95"
```

### 1.4 MIG Partitioning Strategy

| Use Case | MIG Profile | Memory | Best For |
|----------|-------------|--------|----------|
| Small inference | 1g.5gb | 5 GB | BERT, ResNet |
| Medium inference | 2g.10gb | 10 GB | GPT-2, YOLOv8 |
| Fine-tuning | 3g.20gb | 20 GB | LoRA fine-tuning |
| Large inference | 4g.40gb | 40 GB | LLaMA-7B |
| Full GPU | 7g.80gb | 80 GB | LLaMA-70B training |

### 1.5 Queue Management

```yaml
# Priority class definitions
scheduler:
  priority_classes:
    - name: critical-training
      priority: 10
      preemption: never
    - name: standard-training
      priority: 100
      preemption: lower-priority
    - name: batch-inference
      priority: 200
      preemption: any
    - name: development
      priority: 500
      preemption: any
```

---

## 2. Cost Optimization (FinOps)

### 2.1 Spot Instance Strategy

**Golden Rules**:
1. Use spot instances for fault-tolerant workloads only (batch inference, data preprocessing)
2. Never run stateful training on spot without checkpointing
3. Set up automatic migration before predicted interruptions
4. Maintain 20% on-demand capacity as fallback buffer

```yaml
# Recommended spot configuration
finops:
  spot:
    interruption_threshold: 0.7      # Migrate when >70% probability
    max_bid_multiplier: 1.3          # Never bid >130% of on-demand
    migration_lead_time: 10m         # Start migration 10min early
    checkpoint_interval: 15m         # Checkpoint every 15 minutes
    fallback_to_on_demand: true      # Auto-fallback for critical workloads
```

### 2.2 Reserved Instance Optimization

**Decision Framework**:

| Usage Pattern | Recommendation |
|---------------|----------------|
| Steady 24/7 usage (>80% utilization) | 3-year All Upfront RI (58% savings) |
| Business hours only (8h/day) | 1-year No Upfront RI (30% savings) |
| Burst workloads (variable) | Spot + on-demand mix |
| GPU training clusters | 1-year Partial Upfront (37% savings) |

### 2.3 Cost Allocation Tags

Always tag workloads for accurate cost attribution:

```json
{
  "tags": {
    "team": "ml-platform",
    "project": "llm-training",
    "environment": "production",
    "cost_center": "CC-4521",
    "owner": "alice@company.com"
  }
}
```

### 2.4 Development Environment Savings

| Practice | Savings |
|----------|---------|
| Auto-stop dev clusters off-hours (6 PM - 8 AM) | 58% |
| Use spot instances for dev/staging | 60-80% |
| Right-size dev GPU (T4 instead of A100) | 90% |
| Shared GPU via MPS for notebooks | 60-75% |
| Set 72-hour TTL on dev workloads | Prevents zombie resources |

### 2.5 Budget Alerts

```yaml
# Set up budgets for each team
finops:
  budgets:
    - name: "ml-team"
      type: team
      monthly_limit: 50000
      alert_thresholds: [0.5, 0.8, 0.95, 1.0]
      notifications:
        - channel: slack
          target: "#ml-team-costs"
        - channel: email
          target: "ml-lead@company.com"
```

---

## 3. Security Hardening

### 3.1 Authentication Best Practices

| Practice | Implementation |
|----------|----------------|
| Rotate JWT secrets monthly | Automate via cert-manager CronJob |
| Set token expiry to 8 hours | `auth.token_expiry: 8h` |
| Enable OIDC federation | Configure with corporate IdP |
| Enforce strong passwords | Min 12 chars, uppercase, number, special |
| Enable audit logging | `audit.enabled: true` |

### 3.2 RBAC Principle of Least Privilege

```yaml
# Example: Data scientist role (minimal permissions)
role: developer
permissions:
  - workloads.create
  - workloads.read
  - workloads.delete.own    # Only own workloads
  - clusters.read
  - monitoring.read
  - ai.chat
  # Explicitly NOT: clusters.create, users.manage, security.admin
```

### 3.3 Network Security

```yaml
# Namespace isolation with Cilium
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: training-isolation
spec:
  endpointSelector:
    matchLabels: { app: training }
  ingress:
    - fromEndpoints:
        - matchLabels: { app: scheduler }
    - fromEndpoints:
        - matchLabels: { app: monitoring }
  egress:
    - toEndpoints:
        - matchLabels: { app: model-registry }
    - toCIDR: ["0.0.0.0/0"]
      toPorts:
        - ports: [{ port: "443", protocol: TCP }]
```

### 3.4 Container Security Checklist

- [ ] Scan all images with Trivy before deployment
- [ ] Use distroless or minimal base images
- [ ] Run containers as non-root user
- [ ] Enable read-only root filesystem where possible
- [ ] Set resource limits on all containers
- [ ] Enable Pod Security Standards (restricted)
- [ ] Rotate container registry credentials quarterly
- [ ] Sign images with cosign/Sigstore

### 3.5 Secret Management

```bash
# Use external secret management (Vault, AWS Secrets Manager)
# Never store secrets in:
#   - Git repositories
#   - Container images
#   - Environment variables in pod specs (use secretKeyRef)
#   - ConfigMaps

# Recommended: External Secrets Operator
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: cloudai-secrets
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: cloudai-secrets
  data:
    - secretKey: db-password
      remoteRef: { key: cloudai/database, property: password }
```

---

## 4. Edge Computing Deployment

### 4.1 Edge Node Sizing

| Workload | Min CPU | Min RAM | GPU | Storage |
|----------|---------|---------|-----|---------|
| Inference only | 4 cores | 8 GB | 1x T4/Jetson | 64 GB |
| Inference + caching | 8 cores | 16 GB | 1x T4 | 256 GB |
| Offline autonomous | 8 cores | 32 GB | 1-2x GPU | 512 GB |
| Full edge processing | 16 cores | 64 GB | 2-4x GPU | 1 TB |

### 4.2 Offline Resilience

```yaml
# Edge configuration for maximum offline resilience
edge:
  offline:
    max_duration: 72h                # Maximum offline operation time
    local_cache_size: 10GB           # Local model and data cache
    decision_policy: standard        # local scheduling policy
    health_check_interval: 30s       # Self-check frequency
    sync_on_reconnect: true          # Auto-sync when back online
    conflict_resolution: lww         # Last Writer Wins
  compression:
    enabled: true
    default_methods: ["pruning", "quantization_aware_training"]
    accuracy_budget: 0.02            # Max 2% accuracy loss
```

### 4.3 Model Optimization for Edge

**Compression Pipeline** (recommended order):

1. **Structured Pruning** (30-50% size reduction, <1% accuracy loss)
2. **Knowledge Distillation** (if teacher model available)
3. **Quantization Aware Training** (INT8: 4x speedup, <0.5% accuracy loss)

```json
{
  "compression_pipeline": [
    {"method": "structured_pruning", "params": {"sparsity": 0.4}},
    {"method": "quantization_aware_training", "params": {"bits": 8, "symmetric": true}}
  ],
  "accuracy_budget": 0.02,
  "target_latency_ms": 50
}
```

### 4.4 Sync Strategy

| Priority | Data Type | Sync Mode |
|----------|-----------|-----------|
| Critical | Model updates, security patches | Immediate, guaranteed delivery |
| High | Inference results, alerts | Within 1 minute |
| Normal | Metrics, logs | Batched every 5 minutes |
| Low | Analytics, reports | Batched every 30 minutes |
| Bulk | Historical data, backups | Off-peak hours only |

---

## 5. Plugin Development

### 5.1 Plugin Design Principles

1. **Single Responsibility**: Each plugin should do one thing well
2. **Graceful Degradation**: Plugin failures should not crash the platform
3. **Stateless Execution**: Store state externally (database, Redis), not in memory
4. **Idempotent Operations**: Plugin methods should be safe to retry
5. **Observable**: Export metrics and structured logs

### 5.2 Extension Point Selection Guide

| Goal | Extension Point | Example |
|------|----------------|---------|
| Custom node filtering | `scheduler.filter` | GPU memory filter |
| Custom node scoring | `scheduler.score` | Cost-aware scoring |
| Pre-deployment validation | `webhook.validating` | Image policy |
| Post-deployment mutation | `webhook.mutating` | Inject sidecar |
| Custom metrics | `monitor.collector` | Application metrics |
| Alert channels | `monitor.alerter` | WeChat, DingTalk |
| Auth providers | `auth.authenticator` | LDAP, SAML |
| Security checks | `security.scanner` | License compliance |

### 5.3 Plugin Testing Checklist

```go
// Use the built-in test harness
harness := plugin.NewPluginTestHarness(nil)
harness.RegisterPlugin(myPlugin, config)

// Run full lifecycle test
result := harness.RunLifecycleTest(ctx, "my-plugin")
assert.True(t, result.Passed)
assert.True(t, result.HealthOK)
assert.Less(t, result.InitTime, 5*time.Second)
assert.Less(t, result.StopTime, 10*time.Second)

// Run linter
linter := plugin.NewPluginLinter()
report := linter.Lint(myPlugin)
assert.True(t, report.Passed)
assert.GreaterOrEqual(t, report.Score, 80)
```

### 5.4 Plugin Versioning

Follow semantic versioning strictly:
- **MAJOR**: Breaking API changes (metadata structure, method signatures)
- **MINOR**: New features, backward compatible
- **PATCH**: Bug fixes only

---

## 6. AIOps Configuration

### 6.1 Auto-Scaling Tuning

```yaml
aiops:
  autoscale:
    # Tune these based on workload characteristics
    evaluation_interval: 30s       # How often to check metrics
    prediction_window: 5m          # How far ahead to predict
    scale_up_cooldown: 3m          # Wait between scale-ups
    scale_down_cooldown: 5m        # Wait between scale-downs (longer!)
    stabilization_window: 2m       # Ignore transient spikes
    tolerance: 0.1                 # 10% tolerance band

    # GPU workload specific
    gpu_scale_up_cooldown: 5m      # GPU nodes take longer to provision
    gpu_min_utilization: 0.5       # Don't scale down if GPU >50% utilized
```

### 6.2 Self-Healing Playbook Design

**Playbook Design Principles**:
1. Start with the least disruptive action
2. Include verification steps between actions
3. Set appropriate timeouts for each step
4. Define escalation paths for failures
5. Test playbooks in staging before production

### 6.3 Capacity Planning Cadence

| Activity | Frequency | Owner |
|----------|-----------|-------|
| Review capacity forecast | Weekly | SRE team |
| Execute capacity plan | Monthly | Platform team |
| Review RI coverage | Monthly | FinOps team |
| Full capacity audit | Quarterly | Architecture team |
| DR capacity test | Quarterly | SRE team |

---

## 7. High Availability

### 7.1 Component HA Matrix

| Component | Strategy | Min Replicas | Leader Election |
|-----------|----------|--------------|-----------------|
| API Server | Active-Active | 3 | No |
| Scheduler | Active-Standby | 2 | Yes |
| AI Engine | Active-Active | 2 | No |
| PostgreSQL | Primary-Replica | 2 | Patroni |
| Redis | Sentinel | 3 | Yes |
| Kafka | Multi-broker | 3 | ZooKeeper/KRaft |

### 7.2 Anti-Affinity Rules

```yaml
# Spread API servers across nodes and zones
affinity:
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels: { app: cloudai-apiserver }
        topologyKey: kubernetes.io/hostname
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels: { app: cloudai-apiserver }
          topologyKey: topology.kubernetes.io/zone
```

---

## 8. Performance Tuning

### 8.1 Database Tuning

```sql
-- PostgreSQL recommended settings for CloudAI Fusion
ALTER SYSTEM SET shared_buffers = '4GB';
ALTER SYSTEM SET effective_cache_size = '12GB';
ALTER SYSTEM SET work_mem = '256MB';
ALTER SYSTEM SET maintenance_work_mem = '1GB';
ALTER SYSTEM SET max_connections = 200;
ALTER SYSTEM SET max_parallel_workers_per_gather = 4;

-- Critical indexes
CREATE INDEX CONCURRENTLY idx_workloads_status ON workloads(status) WHERE status IN ('pending', 'running');
CREATE INDEX CONCURRENTLY idx_workloads_cluster ON workloads(cluster_id, created_at DESC);
CREATE INDEX CONCURRENTLY idx_audit_logs_time ON audit_logs(created_at DESC);
```

### 8.2 Redis Tuning

```conf
# Redis configuration for caching
maxmemory 8gb
maxmemory-policy allkeys-lru
tcp-keepalive 300
timeout 0
hz 10
```

### 8.3 Go Runtime Tuning

```bash
# API Server and Scheduler
GOGC=100                    # Default GC threshold (lower = more frequent GC, less memory)
GOMAXPROCS=0                # Use all available CPUs
GOMEMLIMIT=3750MiB          # Soft memory limit (75% of container limit)
```

### 8.4 API Server Connection Limits

```yaml
server:
  read_timeout: 30s
  write_timeout: 30s
  max_header_bytes: 1048576   # 1MB
  max_body_bytes: 10485760    # 10MB

database:
  max_open_conns: 50
  max_idle_conns: 10
  conn_max_lifetime: 1h
  conn_max_idle_time: 10m
```

---

## Summary: Top 10 Recommendations

1. **Enable GPU topology discovery** for multi-GPU training workloads
2. **Use spot instances** for fault-tolerant workloads with auto-migration
3. **Set resource limits** on all containers, including GPU memory
4. **Enable audit logging** and rotate JWT secrets monthly
5. **Compress models** before deploying to edge (pruning + quantization)
6. **Configure predictive auto-scaling** with workload-specific metrics
7. **Run self-healing playbooks** in dry-run mode first
8. **Tag all resources** for accurate cost attribution
9. **Use pod anti-affinity** to spread critical components across zones
10. **Review capacity forecasts weekly** and act on recommendations proactively
