# CloudAI Fusion Operations Guide

> **Audience**: Platform administrators, SRE teams, DevOps engineers

## Table of Contents

- [1. Deployment Architecture](#1-deployment-architecture)
- [2. Production Deployment](#2-production-deployment)
- [3. Monitoring & Alerting](#3-monitoring--alerting)
- [4. Backup & Recovery](#4-backup--recovery)
- [5. Upgrade Strategy](#5-upgrade-strategy)
- [6. Scaling Operations](#6-scaling-operations)
- [7. Certificate Management](#7-certificate-management)
- [8. Log Management](#8-log-management)
- [9. Disaster Recovery](#9-disaster-recovery)
- [10. Runbook Library](#10-runbook-library)
- [11. Docker Image Optimization](#11-docker-image-optimization)
- [12. Debug Endpoints](#12-debug-endpoints)
- [13. GPU Setup](#13-gpu-setup)
- [14. Feature Toggles](#14-feature-toggles)
- [15. Diagnostic Tools](#15-diagnostic-tools)

---

## 1. Deployment Architecture

### Production Topology

```
                    Load Balancer (L7)
                         │
            ┌────────────┼────────────┐
            ▼            ▼            ▼
      API Server    API Server    API Server
      (replica 1)  (replica 2)  (replica 3)
            │            │            │
            └────────────┼────────────┘
                         │
         ┌───────────────┼───────────────┐
         ▼               ▼               ▼
    Scheduler       AI Engine        Agent
    (HA pair)      (2+ replicas)   (DaemonSet)
         │               │               │
    ┌────┴────┬──────────┴───┬───────────┘
    ▼         ▼              ▼
PostgreSQL  Redis        Kafka/NATS
(primary+   (Sentinel    (3-broker
 replica)    cluster)     cluster)
```

### Component Resource Requirements

| Component | CPU | Memory | Replicas | Storage |
|-----------|-----|--------|----------|---------|
| API Server | 2 cores | 4 GB | 3+ | — |
| Scheduler | 4 cores | 8 GB | 2 (HA) | — |
| AI Engine | 4 cores | 8 GB | 2+ | — |
| Agent | 0.5 cores | 512 MB | 1/node | — |
| PostgreSQL | 4 cores | 16 GB | 2 (primary+replica) | 100 GB SSD |
| Redis | 2 cores | 8 GB | 3 (Sentinel) | 20 GB |
| Kafka | 2 cores | 4 GB | 3 | 50 GB/broker |
| Prometheus | 2 cores | 8 GB | 2 (HA) | 200 GB |
| Grafana | 1 core | 2 GB | 2 | 10 GB |

---

## 2. Production Deployment

### Helm Deployment

```bash
# Add Helm repository
helm repo add cloudai https://charts.cloudai-fusion.io
helm repo update

# Install with production values
helm upgrade --install cloudai-fusion cloudai/cloudai-fusion \
  --namespace cloudai-fusion --create-namespace \
  --values production-values.yaml \
  --set image.tag=v1.0.0 \
  --set postgresql.primary.persistence.size=100Gi \
  --set redis.sentinel.enabled=true \
  --set apiserver.replicas=3 \
  --set scheduler.replicas=2
```

### Production Values (`production-values.yaml`)

```yaml
global:
  environment: production
  imagePullPolicy: Always

apiserver:
  replicas: 3
  resources:
    requests: { cpu: "2", memory: "4Gi" }
    limits: { cpu: "4", memory: "8Gi" }
  autoscaling:
    enabled: true
    minReplicas: 3
    maxReplicas: 10
    targetCPU: 70

scheduler:
  replicas: 2
  leaderElection: true
  resources:
    requests: { cpu: "4", memory: "8Gi" }

aiEngine:
  replicas: 2
  resources:
    requests: { cpu: "4", memory: "8Gi" }

agent:
  tolerations:
    - key: nvidia.com/gpu
      operator: Exists

postgresql:
  primary:
    persistence:
      size: 100Gi
      storageClass: gp3
  readReplicas:
    replicaCount: 1

redis:
  sentinel:
    enabled: true
  replica:
    replicaCount: 3

ingress:
  enabled: true
  className: nginx
  tls:
    - secretName: cloudai-tls
      hosts: ["api.cloudai-fusion.example.com"]
```

### Health Check Endpoints

| Component | Endpoint | Expected |
|-----------|----------|----------|
| API Server | `GET /healthz` | `{"status":"healthy"}` |
| API Server | `GET /readyz` | `{"status":"ready"}` |
| API Server | `GET /debug/info` | Runtime info (JWT admin required) |
| AI Engine | `GET /healthz` | `{"status":"healthy"}` |
| Scheduler | `GET /healthz` | `{"status":"healthy"}` |

---

## 3. Monitoring & Alerting

### Prometheus Metrics

Key metrics to monitor:

| Metric | Type | Description | Alert Threshold |
|--------|------|-------------|-----------------|
| `cloudai_api_request_duration_seconds` | Histogram | API latency | P99 > 1s |
| `cloudai_api_request_total` | Counter | Total API requests | Error rate > 1% |
| `cloudai_scheduler_queue_depth` | Gauge | Pending workloads | > 100 |
| `cloudai_scheduler_schedule_duration_seconds` | Histogram | Scheduling latency | P99 > 5s |
| `cloudai_gpu_utilization_percent` | Gauge | GPU utilization | < 20% (waste) |
| `cloudai_node_gpu_memory_used_bytes` | Gauge | GPU memory usage | > 90% |
| `cloudai_edge_offline_nodes` | Gauge | Offline edge nodes | > 0 |
| `cloudai_spot_interruption_probability` | Gauge | Spot risk | > 0.7 |
| `cloudai_autoscale_events_total` | Counter | Scaling events | > 20/hour |
| `cloudai_selfheal_incidents_total` | Counter | Incidents | Severity=critical |

### Grafana Dashboards

Pre-built dashboards are in `monitoring/grafana/dashboards/`:

| Dashboard | Description |
|-----------|-------------|
| **Platform Overview** | Cluster health, API throughput, error rates |
| **GPU Fleet** | GPU utilization, memory, temperature across nodes |
| **Scheduler Performance** | Queue depth, scheduling latency, placement success |
| **Cost Analytics** | Spend breakdown, spot savings, RI coverage |
| **Edge Computing** | Edge node status, offline duration, sync lag |
| **AIOps** | Scaling events, MTTR, incident timeline |

### Alert Rules

```yaml
# Critical Alerts (PagerDuty)
- alert: APIServerDown
  expr: up{job="cloudai-apiserver"} == 0
  for: 1m
  labels: { severity: critical }

- alert: SchedulerQueueBacklog
  expr: cloudai_scheduler_queue_depth > 200
  for: 5m
  labels: { severity: high }

- alert: GPUNodeUnhealthy
  expr: cloudai_node_gpu_health == 0
  for: 2m
  labels: { severity: high }

- alert: SpotInterruptionImminent
  expr: cloudai_spot_interruption_probability > 0.8
  for: 1m
  labels: { severity: high }

# Warning Alerts (Slack)
- alert: HighAPILatency
  expr: histogram_quantile(0.99, cloudai_api_request_duration_seconds_bucket) > 1
  for: 5m
  labels: { severity: warning }

- alert: LowGPUUtilization
  expr: avg(cloudai_gpu_utilization_percent) < 30
  for: 30m
  labels: { severity: warning }
```

---

## 4. Backup & Recovery

### Database Backup

```bash
# Automated daily backup (CronJob)
apiVersion: batch/v1
kind: CronJob
metadata:
  name: pg-backup
spec:
  schedule: "0 2 * * *"    # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:16
            command:
            - /bin/sh
            - -c
            - |
              pg_dump -h $PGHOST -U $PGUSER $PGDATABASE | \
              gzip > /backups/cloudai-$(date +%Y%m%d-%H%M%S).sql.gz
            volumeMounts:
            - name: backup-storage
              mountPath: /backups
```

### Backup Retention Policy

| Type | Frequency | Retention |
|------|-----------|-----------|
| Full DB dump | Daily | 30 days |
| WAL archives | Continuous | 7 days |
| Configuration | On change | 90 days |
| Prometheus snapshots | Weekly | 90 days |

### Recovery Procedure

```bash
# 1. Stop API servers
kubectl scale deployment cloudai-apiserver --replicas=0

# 2. Restore database
gunzip -c cloudai-20260312-020000.sql.gz | \
  psql -h $PGHOST -U $PGUSER $PGDATABASE

# 3. Verify data integrity
psql -h $PGHOST -U $PGUSER $PGDATABASE -c "SELECT count(*) FROM clusters;"

# 4. Restart API servers
kubectl scale deployment cloudai-apiserver --replicas=3

# 5. Verify health
curl http://api.cloudai-fusion.example.com/healthz
```

---

## 5. Upgrade Strategy

### Rolling Upgrade Process

```bash
# 1. Review release notes
# 2. Backup database
make backup-db

# 3. Update Helm values
helm upgrade cloudai-fusion cloudai/cloudai-fusion \
  --set image.tag=v1.1.0 \
  --reuse-values

# 4. Monitor rollout
kubectl rollout status deployment/cloudai-apiserver -n cloudai-fusion
kubectl rollout status deployment/cloudai-scheduler -n cloudai-fusion

# 5. Run smoke tests
make test-integration

# 6. If issues, rollback
kubectl rollout undo deployment/cloudai-apiserver -n cloudai-fusion
```

### Database Migration

```bash
# Migrations run automatically on API server startup.
# For manual migration:
go run ./cmd/apiserver migrate up

# Rollback one migration:
go run ./cmd/apiserver migrate down 1
```

### Canary Deployment

The platform supports canary deployments via the CI/CD pipeline:

1. Deploy canary (10% traffic) → Monitor for 10 minutes
2. Promote to 50% → Monitor for 15 minutes
3. Full rollout → Verify all health checks
4. Auto-rollback on error rate > 5% or P99 latency > 2s

---

## 6. Scaling Operations

### Horizontal Scaling

```bash
# Scale API servers
kubectl scale deployment cloudai-apiserver --replicas=5

# Scale AI engine
kubectl scale deployment cloudai-ai-engine --replicas=4

# HPA is recommended for production
kubectl autoscale deployment cloudai-apiserver \
  --min=3 --max=10 --cpu-percent=70
```

### Database Scaling

| Scale Vector | Approach |
|--------------|----------|
| Read throughput | Add PostgreSQL read replicas |
| Write throughput | Vertical scaling (CPU/RAM) |
| Connection count | PgBouncer connection pooling |
| Storage | Online volume expansion |

### Redis Scaling

```bash
# Scale Redis Sentinel cluster
helm upgrade cloudai-fusion cloudai/cloudai-fusion \
  --set redis.replica.replicaCount=5
```

---

## 7. Certificate Management

### TLS Certificate Rotation

```bash
# Using cert-manager for automatic rotation
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: cloudai-tls
spec:
  secretName: cloudai-tls
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
    - api.cloudai-fusion.example.com
  renewBefore: 720h  # Renew 30 days before expiry
```

### Internal mTLS

The eBPF/Cilium service mesh handles internal mTLS automatically. For manual rotation:

```bash
# Rotate internal CA
kubectl -n cloudai-fusion delete secret cloudai-internal-ca
kubectl -n cloudai-fusion rollout restart deployment --selector=app.kubernetes.io/part-of=cloudai-fusion
```

---

## 8. Log Management

### Log Levels

| Level | When to Use |
|-------|-------------|
| `debug` | Development only; verbose request/response logging |
| `info` | Default production; key operations and state changes |
| `warn` | Recoverable issues; degraded but functional |
| `error` | Failures requiring attention |

### Structured Log Format

All components use JSON structured logging:

```json
{
  "level": "info",
  "time": "2026-03-12T10:30:00Z",
  "msg": "workload scheduled",
  "component": "scheduler",
  "workload_id": "abc-123",
  "node": "gpu-node-03",
  "gpu_count": 4,
  "duration_ms": 45
}
```

### Log Aggregation

Recommended stack: **Fluentd/Fluent Bit → Elasticsearch → Kibana**

```yaml
# Fluent Bit DaemonSet config
[INPUT]
    Name              tail
    Path              /var/log/containers/cloudai-*.log
    Parser            docker
    Tag               cloudai.*

[FILTER]
    Name              kubernetes
    Match             cloudai.*

[OUTPUT]
    Name              es
    Match             cloudai.*
    Host              elasticsearch.logging.svc
    Index             cloudai-logs
```

---

## 9. Disaster Recovery

### RPO/RTO Targets

| Tier | RPO | RTO | Components |
|------|-----|-----|------------|
| Tier 1 | 0 (sync replication) | 5 min | PostgreSQL, Redis |
| Tier 2 | 1 hour | 30 min | Prometheus data |
| Tier 3 | 24 hours | 4 hours | Configuration, secrets |

### Cross-Region DR

```bash
# Primary region: cn-hangzhou
# DR region: cn-shanghai

# Setup PostgreSQL streaming replication to DR
# Setup Redis cross-region replication
# Deploy standby API servers in DR region (scaled to 0)

# Failover procedure:
# 1. Promote DR PostgreSQL to primary
# 2. Scale up DR API servers
# 3. Update DNS to point to DR load balancer
# 4. Verify all services healthy
```

### DR Drill Schedule

| Drill Type | Frequency | Duration |
|------------|-----------|----------|
| Database failover test | Monthly | 1 hour |
| Full DR switchover | Quarterly | 4 hours |
| Chaos engineering | Bi-weekly | 2 hours |
| Backup restore verification | Weekly | 30 min |

---

## 10. Runbook Library

### RB-001: API Server Not Responding

```
Symptoms: Health check failing, 502/503 from load balancer
Diagnosis:
  1. kubectl get pods -n cloudai-fusion -l app=apiserver
  2. kubectl logs -n cloudai-fusion deployment/cloudai-apiserver --tail=100
  3. Check PostgreSQL connectivity: kubectl exec -it <pod> -- pg_isready
Resolution:
  - If OOMKilled: Increase memory limits
  - If CrashLoopBackOff: Check config and database connectivity
  - If pending: Check node resources and PVC binding
```

### RB-002: GPU Scheduler Queue Growing

```
Symptoms: cloudai_scheduler_queue_depth > 100
Diagnosis:
  1. Check scheduler logs for errors
  2. Verify GPU node availability: kubectl get nodes -l nvidia.com/gpu
  3. Check if nodes are cordoned or have taints
Resolution:
  - Add GPU nodes if capacity exhausted
  - Check for stuck workloads: GET /api/v1/workloads?status=scheduled
  - Restart scheduler if leader election issues
```

### RB-003: Database Connection Pool Exhaustion

```
Symptoms: "too many connections" errors in API server logs
Diagnosis:
  1. SELECT count(*) FROM pg_stat_activity;
  2. Check max_connections in postgresql.conf
  3. Review connection pool settings in cloudai-fusion.yaml
Resolution:
  - Deploy PgBouncer as connection pooler
  - Increase max_open_conns in config
  - Identify and fix connection leaks
```

### RB-004: Edge Node Offline Recovery

```
Symptoms: Edge node stuck in Offline/Degraded state
Diagnosis:
  1. Check edge node network connectivity
  2. Review offline queue size and pending syncs
  3. Check local storage health on edge device
Resolution:
  - Restore network → node will auto-transition to Recovering
  - If stuck: restart edge agent service
  - If data conflict: check conflict resolution logs
  - Force full sync: POST /api/v1/edge/nodes/{id}/sync?mode=full
```

### RB-005: High Spot Instance Interruption Risk

```
Symptoms: cloudai_spot_interruption_probability > 0.7
Diagnosis:
  1. Check current spot prices vs on-demand
  2. Review migration queue: GET /api/v1/finops/spot/migrations
  3. Verify fallback instance types are available
Resolution:
  - Auto-migration should handle if enabled
  - Manual migration: POST /api/v1/finops/spot/migrate
  - Switch critical workloads to on-demand instances
  - Review bid strategy: GET /api/v1/finops/spot/strategy
```

---

## 11. Docker Image Optimization

All images use multi-stage builds to minimize attack surface and deployment size:

| Image | Build Stage | Runtime Base | Approximate Size |
|-------|------------|--------------|------------------|
| apiserver | `golang:1.25` | `gcr.io/distroless/static:nonroot` | ~15 MB |
| scheduler | `golang:1.25` | `gcr.io/distroless/static:nonroot` | ~12 MB |
| agent | `golang:1.25` | `gcr.io/distroless/static:nonroot` | ~10 MB |
| ai-engine (CPU) | `python:3.11` | `python:3.11-slim` | ~900 MB |
| ai-engine (GPU) | `python:3.11` | `nvidia/cuda:12.4.1-base-ubuntu22.04` | ~2-3 GB |

Distroless images have **no shell**, so health checks use the embedded `healthcheck` binary:

```dockerfile
HEALTHCHECK CMD ["/healthcheck", "--port", "8080"]
```

---

## 12. Debug Endpoints

Debug endpoints are **disabled by default** and protected by 6 layers of security.

### Enable Debug Access

```bash
# 1. Set environment variable
export CLOUDAI_DEBUG_ENABLED=true

# 2. (Optional) Restrict to specific IPs
export CLOUDAI_DEBUG_ALLOWED_IPS="10.0.0.0/8,192.168.1.100"
```

### Available Debug Endpoints

| Endpoint | Method | Description | Auth |
|----------|--------|-------------|------|
| `/debug/info` | GET | Runtime info (goroutines, memory, uptime) | JWT + admin |
| `/debug/pprof/` | GET | Go pprof profiles (rate-limited) | JWT + admin |
| `/debug/log-level` | PUT | Change log level at runtime | JWT + admin |
| `/debug/services` | GET | Cross-service health probe | JWT + admin |

### Security Layers

1. Safe default: disabled unless `CLOUDAI_DEBUG_ENABLED=true`
2. JWT authentication: same middleware as all API endpoints
3. Admin role: only `role=admin` tokens accepted
4. IP allowlist: optional CIDR/IP restriction
5. Audit logging: all access attempts logged
6. Rate limiting: pprof limited to ~2 req/min (anti-DoS)

---

## 13. GPU Setup

### Linux

```bash
# Automated setup script
scripts/setup-gpu.sh

# Detects: NVIDIA driver, nvidia-container-toolkit
# If missing, provides installation commands
```

### Windows (WSL2)

```bash
# Automated detection and guidance
scripts/setup-gpu.bat

# Checks: WSL2, Docker Desktop GPU passthrough, NVIDIA driver
```

### macOS

No GPU support available; AI Engine automatically falls back to CPU mode.

### Start with GPU

```bash
# Linux / macOS
./start.sh --gpu

# Windows
start.bat --gpu

# Or via Docker Compose directly
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d
```

---

## 14. Feature Toggles

Non-core features can be enabled/disabled at runtime via feature flags.

### Profiles

| Profile | Enabled Modules | Use Case |
|---------|-----------------|----------|
| `minimal` | auth, cluster, monitor, scheduler | Development, CI/CD |
| `standard` | + edge, mesh, finops, aiops, deploy | Staging, small prod |
| `full` | + wasm, feature_toggle, federation, advanced | Full production |

### Configuration

```yaml
# cloudai-fusion.yaml
features:
  profile: standard
  edge_computing: true
  service_mesh: true
  wasm_runtime: false
  finops: true
```

### Runtime Management

```bash
# List all feature flags
make features-list
curl http://localhost:8080/api/v1/features

# Enable a feature at runtime
curl -X PUT http://localhost:8080/api/v1/features/wasm_runtime \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"enabled": true}'
```

---

## 15. Diagnostic Tools

### Quick Health Check

```bash
# Run full diagnostics on all services
make diagnose

# Windows
scripts\diagnose.bat

# Linux / macOS
scripts/diagnose.sh
```

The diagnostic tool checks:
- 13 service health endpoints
- PostgreSQL / Redis / Kafka / NATS connectivity
- Port availability
- Recent error log aggregation
- Docker container status

### Environment Setup

```bash
# Generate .env with secure random secrets
make setup

# Or directly
scripts/env-generate.bat    # Windows
scripts/env-generate.sh     # Linux / macOS
```
