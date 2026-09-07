# CloudAI Fusion v1.0.0 - Configuration Guide

**Version**: 1.0.0  
**Date**: September 5, 2026  

---

## Environment Variables (Required)

### Database Connection

| Variable | Required | Example Value | Description |
|----------|----------|---------------|-------------|
| `DB_URL` | Yes | `postgresql://user:password@db-host:5432/cloudai_fusion?sslmode=require` | PostgreSQL connection string |
| `DB_MAX_CONNECTIONS` | No | `20` | Maximum pool connections (default: 10) |
| `DB_MIN_CONNECTIONS` | No | `5` | Minimum pool connections (default: 2) |

**PostgreSQL Setup Example**:
```bash
# Create database
psql -U postgres -c "CREATE DATABASE cloudai_fusion;"
psql -U postgres -c "ALTER USER postgres WITH PASSWORD 'mysecretpassword';"

# Apply schema migrations
psql -d cloudai_fusion -f sql/migrations/001_init.sql
psql -d cloudai_fusion -f sql/migrations/002_scheduler_tables.sql
```

### Cache Backend

| Variable | Required | Example Value | Description |
|----------|----------|---------------|-------------|
| `REDIS_URL` | No | `redis://redis-host:6379` | Redis connection string (optional, disabled if missing) |
| `REDIS_MAX_POOL_SIZE` | No | `50` | Connection pool size (default: 25) |
| `REDIS_TIMEOUT_MS` | No | `1000` | Request timeout in milliseconds (default: 500) |

**Redis Configuration Example**:
```bash
# Production: Enable persistence
redis-server --appendonly yes --maxmemory 2gb --maxmemory-policy allkeys-lru

# Local development (no persistence for speed):
redis-server --save "" --appendonly no
```

---

## Scheduler Configuration

### Core Parameters

| Variable | Default | Valid Range | Description |
|----------|---------|-------------|-------------|
| `SCHEDULING_INTERVAL_MS` | `10000` | `[1000, 60000]` | Base scheduling tick interval in milliseconds |
| `MIN_SCHEDULING_INTERVAL_MS` | `5000` | `[1000, SCHEDULING_INTERVAL_MS]` | Adaptive minimum during heavy load |
| `MAX_SCHEDULING_INTERVAL_MS` | `60000` | `[SCHEDULING_INTERVAL_MS, 300000]` | Adaptive maximum during idle periods |
| `QUEUE_SNAPSHOT_INTERVAL_S` | `30` | `[10, 300]` | Snapshot interval for crash recovery (seconds) |

### Resource Limits

| Variable | Default | Valid Range | Description |
|----------|---------|-------------|-------------|
| `MAX_QUEUE_DEPTH` | `10000` | `[100, 100000]` | Maximum queued jobs before rejection |
| `MAX_RUNNING_JOBS_PER_NODE` | `8` | `[1, 64]` | Max concurrent jobs per GPU node |
| `NODE_CACHE_SYNC_INTERVAL_S` | `60` | `[10, 300]` | K8s node list refresh interval (seconds) |

**Production Tuning Example**:
```bash
# High-throughput cluster (10K+ ops/sec):
export SCHEDULING_INTERVAL_MS=2000
export MAX_QUEUE_DEPTH=50000
export NODE_CACHE_SYNC_INTERVAL_S=30

# Energy-efficient cluster (batch processing):
export SCHEDULING_INTERVAL_MS=60000
export MIN_SCHEDULING_INTERVAL_MS=30000
export MAX_SCHEDULING_INTERVAL_MS=300000
```

---

## TLS/SSL Configuration

### Self-Signed Certificate Generation (Development Only)

```bash
openssl req -x509 -newkey rsa:4096 \
  -keyout tls.key \
  -out tls.crt \
  -days 365 \
  -nodes \
  -subj "/CN=localhost/O=CloudAI Dev/C=US"

# Set secure permissions
chmod 600 tls.key
chmod 644 tls.crt
```

### Production Certificate Integration

```yaml
# Kubernetes secret creation example
kubectl create secret tls cloudai-fusion-tls \
  --cert=/etc/secrets/tls.crt \
  --key=/etc/secrets/tls.key \
  --namespace cloudai-system
```

### Runtime TLS Setup
```bash
# In config.yaml or environment variables:
TLS_ENABLED=true
TLS_CERT_PATH=/etc/cloudai-fusion/tls.crt
TLS_KEY_PATH=/etc/cloudai-fusion/tls.key
```

---

## Performance Tuning (Go Runtime)

### Memory Management

| Variable | Recommended Values | Description |
|----------|------------------|-------------|
| `GOGC` | `20` (high throughput), `100` (balanced), `400` (low memory) | GC intensity tuning (lower = more aggressive collection) |
| `GOMEMLIMIT` | `2GiB`, `4GiB`, `8GiB` | Total heap memory limit |
| `GODEBUG` | `madvdontneed=1` | Enable memory release on low-utilization systems |

**High-Performance Cluster**:
```bash
export GOGC=20
export GOMEMLIMIT=8GiB
export GODEBUG=madvdontneed=1
```

**Memory-Constrained Environment**:
```bash
export GOGC=400
export GOMEMLIMIT=2GiB
export GODEBUG=gctrigger=off  # Disable auto-GC, manual control needed
```

### CPU Optimization

```bash
# Affinity binding for specific CPU cores (avoid oversubscription)
taskset -c 0-11 ./apiserver  # Use first 12 cores only

# Prevent Go from using all CPUs (reserve resources for other services)
GOMAXPROCS=8 ./apiserver
```

---

## Logging Configuration

| Variable | Default | Valid Values | Description |
|----------|---------|--------------|-------------|
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`, `detailed_audit` | Log verbosity level |
| `LOG_FORMAT` | `json` | `json`, `text` | Output format |
| `LOG_FILE` | `-` | File path or `-` for stdout | Output destination |
| `LOG_ROTATION_DAYS` | `7` | Positive integer | Daily log rotation period |

**Production Logging**:
```bash
# Audit-focused logging for compliance:
export LOG_LEVEL=detailed_audit
export LOG_FORMAT=json
export LOG_FILE=/var/log/cloudai-fusion/app.log

# Standard production setup:
export LOG_LEVEL=info
export LOG_FORMAT=json
export LOG_FILE=/var/log/cloudai-fusion/app.log
```

**Log Rotation Setup** (`/etc/logrotate.d/cloudai-fusion`):
```conf /var/log/cloudai-fusion/*.log {
    daily
    rotate 14
    compress
    delaycompress
    notifempty
    create 640 cloudai cloudai
    sharedscripts
    postrotate
        systemctl reload cloudai-fusion
    endscript
}
```

---

## Security Hardening

### RBAC Setup (Kubernetes)

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: cloudai-fusion-role
  namespace: cloudai-system
rules:
- apiGroups: [""]
  resources: ["pods", "services", "deployments"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: cloudai-fusion-binding
  namespace: cloudai-system
subjects:
- kind: ServiceAccount
  name: cloudai-fusion
  namespace: cloudai-system
roleRef:
  kind: Role
  name: cloudai-fusion-role
  apiGroup: rbac.authorization.k8s.io
```

### Network Policy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: cloudai-fusion-policy
  namespace: cloudai-system
spec:
  podSelector:
    matchLabels:
      app: cloudai-fusion-apiserver
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: cafctl
    ports:
    - port: 8080
      protocol: TCP
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: postgres
    ports:
    - port: 5432
      protocol: TCP
  - to:
    - podSelector:
        matchLabels:
          app: redis
    ports:
    - port: 6379
      protocol: TCP
```

### Secret Management

```bash
# Store sensitive data in Kubernetes secrets
kubectl create secret generic cloudai-fusion-secrets \
  --from-literal=DB_PASSWORD=mysecretpassword \
  --from-literal=REDIS_PASSWORD=redissecret \
  --namespace cloudai-system

# Mount as environment variable (reference via secretKeyRef)
env:
- name: DB_URL
  valueFrom:
    secretKeyRef:
      name: cloudai-fusion-secrets
      key: DB_URL
```

---

## Monitoring & Observability

### Prometheus Metrics Endpoint

```bash
# Enabled by default at /metrics
curl http://localhost:8080/metrics

# Exporter configuration in Prometheus scrape_configs:
- job_name: 'cloudai-fusion'
  static_configs:
    - targets: ['cloudai-fusion-apiserver.cloudai-system.svc.cluster.local:8080']
  metrics_path: '/metrics'
```

### Health Check Endpoint

```bash
# HTTP health check for Kubernetes probes
HEALTH_CHECK_ENDPOINT=/health
HEALTH_CHECK_INTERVAL_S=30
HEALTH_CHECK_TIMEOUT_S=10
```

### Custom Health Probes
```bash
# Add custom health checks to config.yaml
health_checks:
- name: database_connected
  type: ping
  endpoint: postgresql://user:pass@db:5432/cloudai_fusion
  timeout_ms: 5000
  critical: true

- name: cache_available
  type: ping
  endpoint: redis://cache:6379
  timeout_ms: 2000
  critical: false  # Not critical, service can degrade
```

---

## Debug Mode (Temporary Only!)

⚠️ **WARNING**: Never enable debug mode in production!

```bash
# Enable comprehensive request/response logging (performance impact!)
export LOG_LEVEL=detailed_debug
export DEBUG_TRACE=true
export DEBUG_ALLOCATIONS=true
export DEBUG_GC_STATS=true

# Useful for troubleshooting performance issues temporarily
# Always disable after debugging session
```

**Debug Session Cleanup**:
```bash
# Monitor debug logs for sensitive data leaks
grep -r "authorization\|password\|secret" /var/log/cloudai-fusion/debug.log | less

# Clear debug logs after troubleshooting
truncate -s 0 /var/log/cloudai-fusion/debug.log
```

---

*Configuration guide generated: September 5, 2026*
*Compatible with CloudAI Fusion v1.0.0*
*For production deployments, review each parameter carefully before applying changes*
