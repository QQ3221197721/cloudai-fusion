# CloudAI Fusion Production Deployment Checklist

**Date**: 2026-08-05  
**Version**: 1.0  

---

## 📋 Pre-Deployment Checklist

### Infrastructure Requirements
- [ ] Kubernetes cluster available (min 3 nodes recommended)
- [ ] Database connection string configured in secrets manager
- [ ] Helm v3+ installed and configured
- [ ] kubectl configured with cluster access
- [ ] Namespace `cloudai-fusion-production` exists or auto-create enabled
- [ ] Resource quotas defined for namespace

### Security Prerequisites
- [ ] TLS certificates for internal services configured
- [ ] Network policies defined for pod-to-pod communication
- [ ] RBAC roles configured for deployment service account
- [ ] Secret encryption at rest enabled
- [ ] Pod security policy/standards enforced

### Monitoring & Observability
- [ ] Prometheus/Grafana stack deployed
- [ ] Alert rules configured for critical metrics
- [ ] Distributed tracing enabled (OpenTelemetry/jaeger)
- [ ] Log aggregation configured (ELK/Loki)
- [ ] Health check endpoints accessible

---

## 🔧 Soft Delete Audit Trail Deployment Steps

### Step 1: Database Migration
```bash
# Run migrations manually first
psql -v ON_ERROR_STOP=1 --set ON_ERROR_ROLLBACK=on \
     -f migrations/002_soft_delete_audit.sql \
     "$DATABASE_URL"

# Verify tables created
psql -c "\dt" "$DATABASE_URL" | grep -E "(audit_logs|orders|workloads)"
```

**Verification Criteria**:
- [ ] `audit_logs` table created successfully
- [ ] Indexes on `table_name`, `record_id`, `created_at` exist
- [ ] Triggers attached to all audited tables
- [ ] No errors during migration execution

### Step 2: Application Configuration
```yaml
# Create ConfigMap
kubectl create configmap soft-delete-config \
  --from-literal=AUDIT_ENABLED=true \
  --from-literal=DB_CONNECTION_STRING="$DATABASE_URL" \
  -n cloudai-fusion-production
```

**Configuration Validation**:
- [ ] Audit trail enabled in application config
- [ ] Database connection tested successfully
- [ ] Minimum 10 character deletion reason enforced
- [ ] 7-year retention policy configured

### Step 3: Kubernetes Deployment
```bash
# Deploy via Helm (placeholder template)
helm upgrade --install cloudai-fusion-soft-delete \
  deploy/helm/cloudai-fusion-soft-delete \
  --namespace cloudai-fusion-production \
  --wait --timeout 5m
```

**Deployment Verification**:
- [ ] All pods reach Ready state
- [ ] Initial health checks pass
- [ ] Resource limits applied correctly
- [ ] Rolling update strategy successful

### Step 4: Post-Deployment Validation
```bash
./scripts/verify-deployment.sh --component soft-delete
```

**Validation Checklist**:
- [ ] API endpoints responding (health, delete, restore, history)
- [ ] Audit logs being written to database
- [ ] Trigger functionality verified
- [ ] Performance benchmarks met (<5ms latency)
- [ ] Rollback procedure tested

---

## 🔐 WASM Sandbox Hardening Deployment Steps

### Step 1: Security Configuration
```bash
# Validate security configuration
cat > /tmp/wasm-security-validate.yaml << EOF
securityConfig:
  cpuLimit: 2.0
  memoryLimitMB: 256
  syscallLimit: 10000
  timeLimitSec: 60
  networkEnabled: false
  diskAccess: false
  allowPrivileged: false
EOF

helm template wasm-sandbox deploy/helm/cloudai-fusion-wasm \
  --values /tmp/wasm-security-validate.yaml
```

**Configuration Verification**:
- [ ] CPU limit ≤ 2 cores
- [ ] Memory limit ≤ 256MB
- [ ] Syscall limit reasonable (< 10k)
- [ ] Time limit appropriate (< 60s)
- [ ] Network disabled by default
- [ ] Disk write access restricted
- [ ] Privileged mode completely disabled

### Step 2: Resource Quotas Setup
```bash
# Create resource quota for sandboxed executions
kubectl apply -f - << EOF
apiVersion: v1
kind: ResourceQuota
metadata:
  name: wasm-execution-quota
  namespace: cloudai-fusion-production
spec:
  hard:
    requests.cpu: "4"
    requests.memory: 2Gi
    limits.cpu: "8"
    limits.memory: 4Gi
EOF
```

**Resource Verification**:
- [ ] Quotas aligned with total executor capacity
- [ ] No single plugin can exhaust all resources
- [ ] Fair allocation across multiple plugins

### Step 3: Kubernetes Deployment
```bash
helm upgrade --install cloudai-fusion-wasm \
  deploy/helm/cloudai-fusion-wasm \
  --namespace cloudai-fusion-production \
  --set replicaCount=3 \
  --set securityConfig.networkEnabled=false \
  --wait --timeout 5m
```

**Deployment Verification**:
- [ ] Three replicas running (high availability)
- [ ] Resource monitors active
- [ ] Network filter blocking unauthorized connections
- [ ] File system guard enforcing mount restrictions
- [ ] Metrics endpoint accessible (/metrics)

### Step 4: Security Testing
```bash
# Test isolation capabilities
./scripts/test-wasm-isolation.sh --stress-tests

# Verify network filtering
kubectl exec wasm-executor-pod -- curl http://evil.com:22

# Should be blocked by network filter
```

**Security Verification**:
- [ ] Network connections properly filtered
- [ ] File system writes restricted
- [ ] CPU/memory limits enforced
- [ ] Execution timeout working correctly
- [ ] Syscall limits respected

---

## ⚠️ Rollback Procedures

### Soft Delete Rollback
If migration fails or causes issues:
```bash
# Option 1: Manual rollback (requires custom script)
psql -f migrations/002_soft_delete_rollback.sql "$DATABASE_URL"

# Option 2: Restore from backup
pg_restore -d cloudai_fusion_backup backup-file.dump

# Option 3: Revert helm release
helm rollback cloudai-fusion-soft-delete <previous-version>
```

### WASM Sandbox Rollback
If security policies cause issues:
```bash
# Update configuration to less restrictive values
helm set securityConfig.cpuLimit 4.0 cloudai-fusion-wasm

# If still problematic, rollback entirely
helm rollback cloudai-fusion-wasm <previous-version>
```

### Emergency Shutdown
```bash
# Scale down deployments immediately
kubectl scale deployment cloudai-fusion-soft-delete --replicas=0 -n cloudai-fusion-production
kubectl scale deployment cloudai-fusion-wasm --replicas=0 -n cloudai-fusion-production
```

---

## 📊 Monitoring After Deployment

### Key Metrics to Monitor

#### Soft Delete Audit Trail
- **Audit log insertion rate**: Should be consistent with user activity
- **Query performance**: Keep under 10ms average
- **Table growth rate**: ~100-200 records/day expected
- **Trigger latency**: Sub-millisecond overhead

#### WASM Executor
- **Plugin execution count**: Track per-hour basis
- **Resource utilization**: CPU < 80%, Memory < 90%
- **Limit violations**: Should be zero (alerts if any occur)
- **Cache hit rate**: Target > 70%

### Alert Thresholds

#### Critical Alerts (Immediate Response Required)
- WASM executor crash/restart
- Soft delete audit log failures
- Database connection loss
- Security policy violation detected

#### Warning Alerts (Monitor Closely)
- Resource utilization > 80%
- Cache hit rate dropping below 60%
- Audit query latency > 50ms
- Plugin execution timeout rate > 1%

---

## 🎯 Post-Deployment Tasks (Day 1)

- [ ] Review all application logs for errors
- [ ] Check monitoring dashboards for anomalies
- [ ] Verify alert rules triggered appropriately
- [ ] Test rollback procedures thoroughly
- [ ] Document any configuration adjustments made
- [ ] Schedule team training session on new features

---

## ✅ Sign-Off Section

**Deployment Approved By**: ____________________ Date: __________  
**Technical Lead**: ____________________ Date: __________  
**Security Officer**: ____________________ Date: __________  
**Operations Manager**: ____________________ Date: __________  

**Post-Deployment Review Scheduled**: Date: __________  
**Review Meeting Attendance Required**: Yes

---

**Document Version**: 1.0  
**Last Updated**: 2026-08-05  
**Next Review Date**: 2026-08-12  
**Owner**: CloudAI Fusion Engineering Team

🔒 **Production deployment requires full checklist completion before sign-off!** 🔒
