# CloudAI Fusion v1.0.0 - Production Deployment Guide

**Version**: 1.0.0  
**Date**: September 5, 2026  
**Status**: PRODUCTION READY ✅

---

## Quick Start (3 Commands)

### Option A: Local Development (Docker Compose)
```bash
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion/docker
docker-compose up -d
# APIs available at http://localhost:8080
```

### Option B: Kubernetes Production (Helm)
```bash
helm repo add cloudai-fusion https://cloudai-fusion.github.io/helm-charts
helm install cloudai-fusion cloudai-fusion/fusion --namespace cloudai-system --create-namespace
```

### Option C: Bare Metal (Direct Binary)
```bash
# Download release binary
wget https://github.com/cloudai-fusion/cloudai-fusion/releases/download/v1.0.0/apiserver-linux-amd64
chmod +x apiserver-linux-amd64
./apiserver-linux-amd64 --config /etc/cloudai-fusion/config.yaml
```

---

## Docker Deployment Instructions

### Step 1: Verify System Requirements
```bash
docker --version    # Required: 24.0+
docker-compose --version  # Required: 2.21+
kubectl version --client  # Optional: if using K8s
```

### Step 2: Configure Environment Variables
Create `docker/.env` file:
```bash
DB_URL=postgres://user:password@db-host:5432/cloudai?sslmode=require
REDIS_URL=redis://redis-host:6379
LOG_LEVEL=info
API_PORT=8080
CLUSTER_ID=production-cluster-1
```

### Step 3: Launch Stack
```bash
cd docker
docker-compose up -d
docker-compose ps    # Verify all containers running
```

### Step 4: Health Check
```bash
curl http://localhost:8080/health
# Expected response: {"status":"healthy","timestamp":"2026-09-05T16:00:00Z"}
```

### Step 5: Initialize Scheduler
```bash
docker exec cloudai-fusion-apiserver ./cafctl manifest create --cluster production-cluster-1
docker logs -f cloudai-fusion-apiserver    # Monitor startup logs
```

---

## Kubernetes Production Deployment

### Prerequisites
- Kubernetes cluster v1.27+ 
- Helm v3.12+ installed
- PostgreSQL database accessible (external or via Helm chart)
- Redis cache accessible (external or via Helm chart)

### Installation Steps

#### 1. Create Namespace
```bash
kubectl create namespace cloudai-system
```

#### 2. Install Dependencies via Helm
```bash
helm install postgres bitnami/postgresql --set auth.password=mysecretpassword --namespace cloudai-system
helm install redis bitnami/redis --set architecture=standalone --namespace cloudai-system
```

#### 3. Deploy CloudAI Fusion
```bash
cat > values-production.yaml << EOF
replicas: 3
resources:
  requests:
    memory: "2Gi"
    cpu: "1000m"
  limits:
    memory: "4Gi"
    cpu: "2000m"
config:
  DB_URL: postgresql://postgres-user:mysecretpassword@postgres-headless.cloudai-system.svc.cluster.local:5432/cloudai_fusion
  REDIS_URL: redis://redis-master.cloudai-system.svc.cluster.local:6379
  LOG_LEVEL: info
  CLUSTER_ID: production-cluster
EOF

helm install cloudai-fusion . --values values-production.yaml --namespace cloudai-system
```

#### 4. Verify Deployment
```bash
kubectl get pods -n cloudai-system
kubectl get svc cloudai-fusion-apiserver -n cloudai-system
kubectl logs -l app=cloudai-fusion-apiserver -n cloudai-system
```

#### 5. Load Test
```bash
ab -n 10000 -c 100 http://cloudai-fusion-apiserver.cloudai-system.svc.cluster.local/api/v1/schedule
```

---

## Configuration Reference

### Critical Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DB_URL` | Yes | - | PostgreSQL connection string |
| `REDIS_URL` | No | localhost:6379 | Redis connection string |
| `LOG_LEVEL` | No | info | Log verbosity (debug/info/warn/error) |
| `API_PORT` | No | 8080 | API server listening port |
| `CLUSTER_ID` | Yes | - | Unique identifier for this cluster |
| `TLS_ENABLED` | No | false | Enable TLS termination |
| `TLS_CERT_PATH` | If TLS=yes | - | Certificate file path |
| `TLS_KEY_PATH` | If TLS=yes | - | Key file path |

### Database Schema Setup

```sql
-- Run on PostgreSQL database
CREATE DATABASE cloudai_fusion;
\c cloudai_fusion

-- Apply schema migration
psql -U postgres -d cloudai_fusion -f migrations/001_init.sql
psql -U postgres -d cloudai_fusion -f migrations/002_scheduler_tables.sql
psql -U postgres -d cloudai_fusion -f migrations/003_evidence_tables.sql

-- Verify tables created
\dt
```

### Cache Initialization

```bash
# Connect to Redis and initialize key structure
redis-cli -h redis-host -p 6379
> CONFIG SET maxmemory-policy allkeys-lru
> CONFIG SET maxmemory 2gb
> FLUSHALL
```

---

## Security Hardening Checklist

- [ ] **TLS Certificates**: Generate self-signed certificates or use Let's Encrypt
  ```bash
  openssl req -x509 -newkey rsa:4096 -keyout tls.key -out tls.crt -days 365
  ```

- [ ] **RBAC Setup**: Create Kubernetes service accounts and roles
  ```bash
  kubectl create serviceaccount cloudai-fusion -n cloudai-system
  kubectl create role cloudai-fusion-role -n cloudai-system \
    --verb=get,list,watch --resource=pods,services,deployments
  kubectl create rolebinding cloudai-fusion-binding -n cloudai-system \
    --role=cloudai-fusion-role --serviceaccount=cloudai-system:cloudai-fusion
  ```

- [ ] **Secret Management**: Use Kubernetes secrets for sensitive data
  ```bash
  kubectl create secret generic cloudai-fusion-secrets \
    --from-literal=DB_PASSWORD=mysecretpassword \
    --from-literal=REDIS_PASSWORD=redissecret \
    -n cloudai-system
  ```

- [ ] **Network Policies**: Restrict pod-to-pod communication
  ```bash
  cat > network-policy.yaml << EOF
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
    egress:
    - to:
      - podSelector:
          matchLabels:
            app: postgres
    - to:
      - podSelector:
          matchLabels:
            app: redis
  EOF
  kubectl apply -f network-policy.yaml
  ```

- [ ] **Audit Logging**: Enable detailed request logging
  ```bash
  export LOG_LEVEL=detailed_audit
  # Logs will include full request/response payloads for compliance tracking
  ```

---

## Performance Tuning Recommendations

### Memory Allocation
```bash
# For high-throughput scheduling workloads (10K+ ops/sec)
export GOGC=20    # Lower GC interval, higher memory usage
export GOMEMLIMIT=4GiB

# Standard workloads (1K-5K ops/sec)
export GOGC=100   # Default value
export GOMEMLIMIT=2GiB
```

### Database Optimization
```sql
-- PostgreSQL tuning parameters (postgresql.conf)
shared_buffers = 2GB
effective_cache_size = 6GB
maintenance_work_mem = 256MB
work_mem = 64MB
max_connections = 200
```

### Container Resource Limits
```yaml
# In Helm values or deployment YAML
resources:
  requests:
    memory: "2Gi"
    cpu: "1000m"
  limits:
    memory: "4Gi"
    cpu: "2000m"
```

---

## Disaster Recovery Procedures

### Backup Database
```bash
# Daily automated backup script
pg_dump -U postgres -F c -b -v -f /backups/cloudai-fusion-backup-$(date +%Y%m%d).sql cloudai_fusion

# Rotate old backups (keep last 7 days)
find /backups -name "*.sql" -mtime +7 -delete
```

### Restore Database
```bash
# Point recovery to latest backup
PGPASSWORD=mysecretpassword pg_restore -U postgres -d cloudai_fusion /backups/cloudai-fusion-backup-YYYYMMDD.sql

# Verify restore completeness
psql -U postgres -d cloudai_fusion -c "SELECT COUNT(*) FROM workloads;"
psql -U postgres -d cloudai_fusion -c "SELECT MAX(created_at) FROM evidence_ledger;"
```

### Rollback Deployment
```bash
# Kubernetes rollback to previous version
kubectl rollout undo deployment/cloudai-fusion-apiserver -n cloudai-system

# Revert to specific revision
kubectl rollout undo deployment/cloudai-fusion-apiserver --to-revision=5 -n cloudai-system

# Check rollout history
kubectl rollout history deployment/cloudai-fusion-apiserver -n cloudai-system
```

### Emergency Stop & Restart
```bash
# Graceful shutdown (allows in-flight schedules to complete)
kubectl delete pod cloudai-fusion-apiserver-xxx -n cloudai-system

# Immediate stop (forces termination)
kubectl delete pod cloudai-fusion-apiserver-xxx -n cloudai-system --force

# Restart with clean state (resets everything)
kubectl delete statefulset cloudai-fusion-apiserver -n cloudai-system
kubectl apply -f helm/cloudai-fusion/templates/statefulset.yaml -n cloudai-system
```

---

## Support & Troubleshooting

### Common Issues

**Issue**: Pods stuck in CrashLoopBackOff
```bash
# Solution: Check application logs first
kubectl logs <pod-name> -n cloudai-system --previous

# Check database connectivity
kubectl run test-db-check --rm -it --image=postgres:16 -- bash -c "psql -h postgres-cloudai -U postgres -d cloudai_fusion -c 'SELECT 1'"
```

**Issue**: API responses very slow (>1s latency)
```bash
# Solution: Check GC pressure
export LOG_LEVEL=gcp_metrics
kubectl logs <pod-name> -n cloudai-system | grep "GC stats"

# Increase memory if needed
kubectl patch deployment cloudai-fusion-apiserver -n cloudai-system -p '{"spec":{"template":{"spec":{"containers":[{"name":"apiserver","resources":{"limits":{"memory":"8Gi"}}}]}}}}'
```

**Issue**: High CPU utilization (>80%)
```bash
# Solution: Reduce replica count or scale horizontally
kubectl scale deployment/cloudai-fusion-apiserver --replicas=2 -n cloudai-system

# Check if scheduler load is normal
curl http://cloudai-fusion-apiserver.cloudai-system.svc.cluster.local/metrics | grep scheduler_queue_length
```

### Contact Information

- **GitHub Issues**: https://github.com/cloudai-fusion/cloudai-fusion/issues
- **Slack Channel**: #cloudai-fusion-support (invite link TBD)
- **Email**: support@cloudai-fusion.io (placeholder)

---

*Deployment guide generated: September 5, 2026*
*Compatible with CloudAI Fusion v1.0.0 release only*
*For production deployments, consult with DevOps team before applying changes*
