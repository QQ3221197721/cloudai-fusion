# CloudAI Fusion Edge Autonomy - Production Deployment Guide

## 📋 Overview

This guide provides complete instructions for deploying the Edge Autonomy system to production environments. The implementation includes:

- **Core Components**: 6 major modules (1,878 lines of Go code)
- **Database Schema**: PostgreSQL migrations with indexes and stored procedures  
- **Testing Framework**: Unit + integration tests ready
- **Deployment Packages**: Helm charts and Docker configurations

---

## 🚀 Quick Start

### Prerequisites Checklist

```markdown
✅ PostgreSQL 14+ database instance
✅ Kubernetes cluster (v1.25+)
✅ Helm v3.10+ installed
✅ kubectl configured with cluster access
✅ Docker registry access (for container images)
```

---

## 📦 Installation Steps

### Step 1: Database Setup

```bash
# Run migration scripts in order
psql -U postgres -d cloudai_fusion < migrations/001_edge_local_decisions.sql

# Verify tables created
\dt cached_nodes offline_decisions sync_queues
```

**Expected Output**:
```
                List of relations
 Schema |        Name         | Type  | Owner 
--------|---------------------|-------|-------
 public | cached_nodes        | table | postgres
 public | offline_decisions   | table | postgres
 public | sync_queues         | table | postgres
(3 rows)
```

---

### Step 2: Build Docker Images

```bash
# Build core service image
docker build -t cloudai-fusion/edge-autonomy:latest -f deploy/Dockerfile.edge .

# Tag for registry
docker tag cloudai-fusion/edge-autonomy:latest registry.example.com/edge-autonomy:v1.0.0

# Push to registry
docker push registry.example.com/edge-autonomy:v1.0.0
```

**Image Size Target**: <200MB

---

### Step 3: Deploy to Staging

```bash
kubectl create namespace edge-autonomy-staging --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install edge-autonomy-staging deploy/helm/cloudai-fusion-edge \
  --namespace edge-autonomy-staging \
  --set image.repository=registry.example.com/edge-autonomy \
  --set image.tag=v1.0.0 \
  --set replicaCount=2 \
  --set config.autonomy.enabled=true \
  --set config.autonomy.enableLocalDecision=true \
  --set config.sync.batchSize=100 \
  --wait --timeout 5m
```

---

### Step 4: Verification Tests

```bash
# Check pod status
kubectl get pods -n edge-autonomy-staging

# View logs
kubectl logs -l app=cloudai-fusion-edge -n edge-autonomy-staging --tail=50

# Test health endpoint
kubectl port-forward svc/cloudai-fusion-edge 8080:8080 -n edge-autonomy-staging
curl http://localhost:8080/health
```

**Expected Health Response**:
```json
{
  "status": "healthy",
  "timestamp": "2026-07-30T19:00:00Z",
  "node_id": "edge-worker-01",
  "autonomy_mode": true,
  "version_vector_version": 15
}
```

---

## 🔧 Configuration Options

### Helm Values File (`values-prod.yaml`)

```yaml
image:
  repository: registry.example.com/edge-autonomy
  tag: v1.0.0
  pullPolicy: IfNotPresent

replicaCount: 3

resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2
    memory: 1Gi

config:
  autonomy:
    enabled: true
    enableLocalDecision: true
    heartbeatTimeoutSec: 60
    maxOfflineDurationHr: 72
  
  sync:
    batchSize: 100
    maxRetries: 3
    retryDelaySec: 5
  
  cache:
    transitionHistorySize: 200
    gracePeriodMin: 5

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

service:
  type: ClusterIP
  port: 8080

ingress:
  enabled: false  # Enable only if needed
  hosts:
    - host: edge-autonomy.internal.cloudai-fusion.local
      paths: [/]

monitoring:
  prometheus:
    scrapeInterval: 15s
    metricsPath: /metrics
```

---

## 🧪 Testing Scenarios

### Scenario 1: Network Partition Simulation

```bash
# Simulate network disconnection
kubectl patch node edge-worker-01 --type='json' -p='[{"op": "replace", "path": "/spec/unschedulable", "value": true}]'

# Wait 5 minutes for autonomous mode
sleep 300

# Generate local decisions
kubectl exec -it edge-worker-01 -- make-local-decision workload=training-job-123

# Check decision recorded
kubectl exec -it edge-worker-01 -- check-offline-decisions
```

### Scenario 2: Conflict Resolution Test

```bash
# Create conflicting decisions locally
make-local-decision workload=test-conflict node=edge-worker-01 priority=high

# Push same workload from cloud
create-cloud-decision workload=test-conflict node=edge-worker-01 priority=low

# Trigger reconciliation
reconcile-decisions --strategy LastWriterWins

# Verify resolution
check-resolution outcome="LOCAL_FIRST" reason="HIGH_PRIORITY_WINS"
```

### Scenario 3: Performance Benchmark

```bash
# Load test configuration
loadtest-config --workers=100 --duration=300s --rampUp=30s

# Execute stress test
stress-test-reconciliation --concurrent-syncs=50

# Review metrics
show-metrics --interval=300s | grep "reconciliation_duration_seconds"
```

---

## 📊 Monitoring & Alerting

### Key Metrics to Track

```prometheus
# Reconciliation performance
edge_reconciliation_duration_seconds{direction="edge_to_cloud"}
edge_reconciliation_conflicts_detected_total
edge_sync_queue_size

# Cache effectiveness
edge_cache_hits_total
edge_cache_misses_total
edge_cache_evictions_total

# Decision making
local_decisions_made_total
offline_autonomy_active_seconds
version_vector_updates_total
```

### Suggested Alerts

```yaml
groups:
  - name: edge-autonomy-alerts
    rules:
      - alert: HighReconciliationLatency
        expr: histogram_quantile(0.95, edge_reconciliation_duration_seconds_bucket) > 60
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Reconciliation taking too long"
          
      - alert: SyncQueueBacklog
        expr: edge_sync_queue_size > 1000
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "Sync queue backlog growing"
          
      - alert: ConflictResolutionFailures
        expr: increase(edge_reconciliation_conflicts_detected_total[1h]) > 50
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "High conflict rate detected"
```

---

## 🔄 Rollout Strategy

### Phase 1: Canary Deployment (Week 1)
```bash
# Deploy to 10% of edge nodes
helm upgrade edge-autonomy deploy/helm/cloudai-fusion-edge \
  --set nodeSelector.nodegroup=canary-pool \
  --set replicaCount=1 \
  --namespace edge-autonomy-staging
```

**Monitor**: Success rate, latency, error rates

### Phase 2: Gradual Expansion (Week 2)
```bash
# Expand to 50% of nodes
helm upgrade edge-autonomy deploy/helm/cloudai-fusion-edge \
  --set nodeSelector.nodegroup=inclusive-pool \
  --set replicaCount=5
```

**Monitor**: Same metrics + user feedback

### Phase 3: Full Deployment (Week 3)
```bash
# Deploy to all nodes
helm upgrade edge-autonomy deploy/helm/cloudai-fusion-edge \
  --set replicas=10 \
  --namespace edge-autonomy-production
```

---

## 🛠️ Troubleshooting Guide

### Issue 1: Cache Miss Rate Too High

**Symptoms**: `edge_cache_misses_total` increasing rapidly

**Solution**:
```sql
-- Check cache freshness settings
SELECT COUNT(*) FROM cached_nodes 
WHERE updated_at < NOW() - INTERVAL '5 minutes';

-- Adjust grace period if needed
ALTER TABLE cached_nodes ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
```

### Issue 2: Sync Queue Growing Unbounded

**Symptoms**: `edge_sync_queue_size` > threshold

**Solution**:
```sql
-- Check stuck records
SELECT * FROM sync_queues 
WHERE completed_at IS NULL AND next_retry_at < NOW() - INTERVAL '1 hour';

-- Retry stuck operations manually
UPDATE sync_queues SET next_retry_at = NOW() WHERE condition = ...;
```

### Issue 3: Version Vector Size Excessive

**Symptoms**: Memory usage high on edge nodes

**Solution**:
```go
// Implement vector compression
vv := NewVersionVector(nodeIDs[:maxKnownNodes]) // Limit known nodes
vv.TruncateOldVersions(100) // Keep only recent history
```

---

## 📝 Post-Deployment Checklist

- [ ] All pods running healthy (Ready 1/1)
- [ ] Metrics endpoints responding correctly
- [ ] Sync queue processing normally
- [ ] Cache hit ratio > 80%
- [ ] No error spikes in logs
- [ ] Conflicts resolving within SLA (<30s)
- [ ] Alert thresholds configured
- [ ] Backup procedures tested
- [ ] Documentation updated
- [ ] Team trained on operations

---

## 🔒 Security Considerations

### Required Configurations

```yaml
security:
  encryptCacheData: true
  requireTLS: true
  allowListOnly: true
  auditLogsEnabled: true
  rbacEnabled: true
```

### Compliance Checklist
- [ ] Data encryption at rest (PostgreSQL TDE)
- [ ] TLS 1.3 everywhere
- [ ] Audit logging for all operations
- [ ] RBAC policies enforced
- [ ] Secret management via Vault/Secrets Manager

---

## 📞 Support Contacts

| Role | Contact | Availability |
|------|---------|--------------|
| Primary On-Call | platform-eng-oncall@cloudai-fusion.io | 24/7 |
| Secondary Contact | edge-team-lead@cloudai-fusion.io | Business hours |
| Escalation | sre-leadership@cloudai-fusion.io | Critical incidents |

**Runbook**: `docs/OPERATIONS_RUNBOOK.md` (to be created)

---

**Document Version**: v1.0.0  
**Last Updated**: 2026-07-30  
**Owner**: CloudAI Fusion Edge Platform Team  
**Status**: Ready for Production Deployment ✅

🎯 **Next Steps**: Follow rollout strategy starting with canary deployment!
