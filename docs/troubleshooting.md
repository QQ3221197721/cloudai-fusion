# CloudAI Fusion Troubleshooting Guide

> **Purpose**: Rapid diagnosis and resolution of common platform issues

## Table of Contents

- [1. Diagnostic Workflow](#1-diagnostic-workflow)
- [2. API Server Issues](#2-api-server-issues)
- [3. Scheduler Issues](#3-scheduler-issues)
- [4. GPU & Node Issues](#4-gpu--node-issues)
- [5. AI Engine Issues](#5-ai-engine-issues)
- [6. Edge Computing Issues](#6-edge-computing-issues)
- [7. Database Issues](#7-database-issues)
- [8. Authentication & Authorization](#8-authentication--authorization)
- [9. Networking Issues](#9-networking-issues)
- [10. FinOps & Cost Issues](#10-finops--cost-issues)
- [11. Plugin Issues](#11-plugin-issues)
- [12. Performance Issues](#12-performance-issues)
- [13. Emergency Response Procedures](#13-emergency-response-procedures)
- [14. Log Analysis Patterns](#14-log-analysis-patterns)
- [15. Diagnostic Commands Cheat Sheet](#15-diagnostic-commands-cheat-sheet)

---

## 1. Diagnostic Workflow

For any issue, follow this systematic approach:

```
1. IDENTIFY    → What is the symptom? Which component?
2. SCOPE       → Single node? Cluster-wide? All environments?
3. TIMELINE    → When did it start? Any recent changes?
4. COLLECT     → Gather logs, metrics, events
5. CORRELATE   → Check related components and dependencies
6. RESOLVE     → Apply fix from known patterns below
7. VERIFY      → Confirm resolution with health checks
8. DOCUMENT    → Record the incident and root cause
```

### Quick Health Check

```bash
# Platform overall health
curl http://localhost:8080/healthz

# Component-specific health
curl http://localhost:8080/readyz
curl http://localhost:8090/healthz    # AI Engine

# Kubernetes resources
kubectl get pods -n cloudai-fusion
kubectl get events -n cloudai-fusion --sort-by='.lastTimestamp' | tail -20
```

---

## 2. API Server Issues

### Problem: API Server Returns 502/503

**Symptoms**: Load balancer returning 502 Bad Gateway or 503 Service Unavailable

**Diagnosis**:
```bash
# Check pod status
kubectl get pods -n cloudai-fusion -l app=cloudai-apiserver

# Check pod logs
kubectl logs -n cloudai-fusion -l app=cloudai-apiserver --tail=50

# Check resource usage
kubectl top pods -n cloudai-fusion -l app=cloudai-apiserver
```

**Common Causes & Fixes**:

| Cause | Evidence | Fix |
|-------|----------|-----|
| OOMKilled | `Reason: OOMKilled` in pod events | Increase memory limits |
| DB connection failed | `failed to connect to database` in logs | Check PostgreSQL health |
| Port conflict | `bind: address already in use` | Change port or kill conflicting process |
| Config error | `invalid configuration` at startup | Validate `cloudai-fusion.yaml` |

### Problem: Slow API Response Times

**Symptoms**: P99 latency > 1 second

**Diagnosis**:
```bash
# Check API metrics
curl http://localhost:8080/metrics | grep cloudai_api_request_duration

# Check database query performance
kubectl exec -it <pg-pod> -- psql -c "SELECT * FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10;"

# Check Redis connection
kubectl exec -it <api-pod> -- redis-cli -h redis ping
```

**Fixes**:
1. **Database slow queries**: Add missing indexes, enable query cache
2. **Connection pool exhaustion**: Increase `max_open_conns` in config
3. **Redis timeout**: Check Redis Sentinel health, increase pool size
4. **Excessive logging**: Switch to `info` level in production

### Problem: API Returns 401 Unauthorized

**Diagnosis**:
```bash
# Verify JWT token
curl -v -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/clusters

# Check token expiry
echo "<token>" | cut -d'.' -f2 | base64 -d 2>/dev/null | jq '.exp'

# Check JWT secret consistency across replicas
kubectl get secret cloudai-secrets -n cloudai-fusion -o jsonpath='{.data.jwt-secret}'
```

**Fixes**: Re-authenticate to get fresh token; ensure JWT secret is same across all replicas.

---

## 3. Scheduler Issues

### Problem: Workloads Stuck in "Pending"

**Symptoms**: Workloads not progressing from pending to scheduled

**Diagnosis**:
```bash
# Check scheduler health
kubectl logs -n cloudai-fusion -l app=cloudai-scheduler --tail=100

# Check for available GPU nodes
kubectl get nodes -l nvidia.com/gpu --show-labels
kubectl describe node <gpu-node> | grep -A 5 "Allocated resources"

# Check workload requirements
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/workloads?status=pending
```

**Common Causes**:

| Cause | Fix |
|-------|-----|
| No GPU nodes available | Add GPU nodes or wait for running jobs to complete |
| GPU type mismatch | Check `gpu_type_required` matches available GPUs |
| Node taints | Add appropriate tolerations to workload |
| Resource quota exceeded | Contact admin to increase tenant quota |
| Scheduler crash/restart | Check scheduler pod logs and events |

### Problem: Suboptimal GPU Placement

**Symptoms**: Multi-GPU training jobs have slow inter-GPU communication

**Diagnosis**:
```bash
# Check topology awareness
curl http://localhost:8080/api/v1/clusters/{id}/gpu-topology

# Check NCCL logs in training pod
kubectl logs <training-pod> | grep NCCL

# Verify NVLink connections
kubectl exec -it <gpu-node-agent> -- nvidia-smi topo -m
```

**Fix**: Enable `gpu_topology_discovery: true` in scheduler config; ensure `NCCL_DEBUG=INFO` in training environment.

---

## 4. GPU & Node Issues

### Problem: GPU Not Detected

**Diagnosis**:
```bash
# On the node
nvidia-smi
nvidia-smi -L

# Check NVIDIA device plugin
kubectl get pods -n kube-system -l name=nvidia-device-plugin-ds
kubectl logs -n kube-system <nvidia-device-plugin-pod>

# Check node allocatable resources
kubectl describe node <node> | grep nvidia.com/gpu
```

**Fixes**:
1. Install/update NVIDIA driver: `apt install nvidia-driver-550`
2. Restart NVIDIA device plugin DaemonSet
3. Check NVIDIA container toolkit installation

### Problem: GPU Out of Memory (OOM)

**Symptoms**: `CUDA error: out of memory` in workload logs

**Diagnosis**:
```bash
# Check GPU memory on node
kubectl exec -it <agent-pod> -- nvidia-smi --query-gpu=memory.used,memory.total --format=csv

# Check for orphaned GPU processes
kubectl exec -it <agent-pod> -- nvidia-smi --query-compute-apps=pid,name,used_gpu_memory --format=csv
```

**Fixes**:
1. Reduce batch size or model size
2. Enable gradient checkpointing
3. Use MIG partitioning for memory isolation
4. Request larger GPU (A100 80GB vs 40GB)

### Problem: GPU Temperature Critical (>90C)

**Immediate Action**:
```bash
# Check temperature
nvidia-smi --query-gpu=temperature.gpu --format=csv

# Reduce power limit immediately
nvidia-smi -pl 200  # Watts

# Check fan status
nvidia-smi --query-gpu=fan.speed --format=csv
```

**Root Causes**: Faulty cooling, dust buildup, ambient temperature, sustained workload without thermal management.

---

## 5. AI Engine Issues

### Problem: AI Engine Not Responding

**Diagnosis**:
```bash
# Check AI Engine pods
kubectl get pods -n cloudai-fusion -l app=cloudai-ai-engine
kubectl logs -n cloudai-fusion -l app=cloudai-ai-engine --tail=100

# Check Python dependencies
kubectl exec -it <ai-pod> -- pip list | grep -E "fastapi|uvicorn|torch"
```

### Problem: LLM Responses Are Empty/Failing

**Diagnosis**:
```bash
# Check LLM backend status
curl http://localhost:8090/api/v1/models/status

# Check API keys
kubectl get secret cloudai-secrets -o jsonpath='{.data.openai-api-key}' | base64 -d

# Test LLM directly
curl http://localhost:8090/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"test"}'
```

**Fixes**:
1. **No API key set**: Agents fall back to rule-based logic (expected behavior)
2. **API key expired**: Rotate key in secrets
3. **Rate limited**: Reduce request frequency or upgrade API plan
4. **Ollama not running**: Start with `ollama serve`

---

## 6. Edge Computing Issues

### Problem: Edge Node Stuck Offline

**Diagnosis**:
```bash
# Check edge node status
curl http://localhost:8080/api/v1/edge/nodes

# Check offline queue size
curl http://localhost:8080/api/v1/edge/nodes/{id}/offline-queue

# Check network from edge node
ping <cloud-api-endpoint>
curl -v http://<cloud-api-endpoint>/healthz
```

**Fixes**:

| State | Action |
|-------|--------|
| Disconnecting | Wait for auto-transition (network may be flaky) |
| Offline | Check and restore network connectivity |
| Recovering | Monitor sync progress; should auto-resolve |
| Degraded | Check local health: disk space, GPU driver, services |
| Failed | Restart edge agent; check hardware |

### Problem: Edge-Cloud Sync Conflicts

**Diagnosis**:
```bash
# Check conflict resolution log
curl http://localhost:8080/api/v1/edge/nodes/{id}/sync/conflicts

# Check vector clock state
curl http://localhost:8080/api/v1/edge/nodes/{id}/sync/vector-clock
```

**Fix**: Default conflict resolution is LWW (Last Writer Wins). For custom resolution, configure the ConflictResolver with `cloud_wins` or `merge` strategy.

### Problem: Model Deployment to Edge Fails

**Diagnosis**:
```bash
# Check edge node disk space
curl http://localhost:8080/api/v1/edge/nodes/{id}/health

# Check compression pipeline status
curl http://localhost:8080/api/v1/edge/models/{model_id}/compression-status
```

**Fixes**:
1. Insufficient disk: Free space or expand storage on edge device
2. Model too large: Enable compression with `pruning` + `quantization_aware_training`
3. Incompatible runtime: Check edge node inference runtime (TensorRT/OpenVINO version)

---

## 7. Database Issues

### Problem: Database Connection Failures

**Diagnosis**:
```bash
# Check PostgreSQL status
kubectl get pods -n cloudai-fusion -l app=postgresql
kubectl exec -it <pg-pod> -- pg_isready

# Check connection count
kubectl exec -it <pg-pod> -- psql -c "SELECT count(*) FROM pg_stat_activity;"

# Check max connections
kubectl exec -it <pg-pod> -- psql -c "SHOW max_connections;"
```

**Fixes**:

| Issue | Fix |
|-------|-----|
| Too many connections | Deploy PgBouncer; increase `max_connections` |
| Disk full | Expand PVC; run `VACUUM FULL` |
| Replication lag | Check network between primary and replica |
| Slow queries | Run `EXPLAIN ANALYZE` on problematic queries |

### Problem: Redis Connection Issues

```bash
# Check Redis Sentinel
kubectl exec -it <redis-pod> -- redis-cli -p 26379 SENTINEL master mymaster
kubectl exec -it <redis-pod> -- redis-cli INFO replication
```

---

## 8. Authentication & Authorization

### Problem: "Permission Denied" for Valid User

**Diagnosis**:
```bash
# Decode JWT to check role
echo "<token>" | cut -d'.' -f2 | base64 -d 2>/dev/null | jq '.role'

# Check RBAC permissions for the role
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:8080/api/v1/auth/roles/{role}/permissions
```

**Fix**: Ensure user has the required role. Role hierarchy: admin > operator > developer > viewer.

### Problem: JWT Token Expired Immediately

**Diagnosis**: Check system clock synchronization across nodes.

```bash
# Check time sync
timedatectl status
ntpstat
```

**Fix**: Sync NTP across all nodes; increase `token_expiry` in config if too short.

---

## 9. Networking Issues

### Problem: Service Mesh (Cilium) Communication Failures

```bash
# Check Cilium status
cilium status
cilium connectivity test

# Check network policies
kubectl get ciliumnetworkpolicies -n cloudai-fusion
```

### Problem: Cross-Cluster Communication Fails

```bash
# Check multi-cluster mesh
cilium clustermesh status

# Verify DNS resolution
kubectl exec -it <test-pod> -- nslookup <service>.<namespace>.svc.cluster.local
```

---

## 10. FinOps & Cost Issues

### Problem: Spot Price Data Not Updating

**Diagnosis**: Check cloud provider API connectivity and credentials.

```bash
curl http://localhost:8080/api/v1/providers/{name}/gpu-instances
```

**Fix**: Verify cloud provider credentials; check API rate limits.

### Problem: Cost Report Shows Zero

**Fix**: Ensure cost data is being ingested. Check cloud provider billing API access and IAM permissions for cost data.

---

## 11. Plugin Issues

### Problem: Plugin Fails to Load

**Diagnosis**:
```bash
# Check plugin registry
curl http://localhost:8080/api/v1/plugins/installed

# Check plugin logs
kubectl logs -n cloudai-fusion <apiserver-pod> | grep "plugin"
```

**Common Causes**:

| Error | Fix |
|-------|-----|
| `cyclic dependency detected` | Remove circular dependency between plugins |
| `plugin X not found in registry` | Install missing dependency plugin first |
| `extension point mismatch` | Plugin doesn't implement required interface |
| `version conflict` | Upgrade conflicting plugins to compatible versions |

---

## 12. Performance Issues

### Problem: High Memory Usage on API Server

```bash
# Get Go runtime stats
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Check for goroutine leaks
curl http://localhost:8080/debug/pprof/goroutine?debug=1
```

### Problem: Prometheus Scrape Failures

```bash
# Check targets
curl http://localhost:9090/api/v1/targets | jq '.data.activeTargets[] | select(.health!="up")'
```

---

## 13. Emergency Response Procedures

### Severity Levels

| Level | Definition | Response Time | Escalation |
|-------|-----------|---------------|------------|
| **P0** | Platform down, all users affected | 5 min | Immediate page to on-call |
| **P1** | Major feature broken, >10% users affected | 15 min | Page on-call + engineering lead |
| **P2** | Feature degraded, workaround exists | 1 hour | Slack alert |
| **P3** | Minor issue, low impact | 24 hours | Ticket |

### P0 Emergency Procedure

```
1. ACKNOWLEDGE  → Respond to page within 5 minutes
2. ASSESS       → Identify affected components (health checks)
3. COMMUNICATE  → Post in #incident channel with status
4. MITIGATE     → Apply immediate fix (rollback, restart, failover)
5. VERIFY       → Confirm service restored via health checks
6. RCA          → Schedule post-incident review within 48 hours
```

### Emergency Rollback

```bash
# Rollback to last known good version
kubectl rollout undo deployment/cloudai-apiserver -n cloudai-fusion
kubectl rollout undo deployment/cloudai-scheduler -n cloudai-fusion

# Verify rollback
kubectl rollout status deployment/cloudai-apiserver -n cloudai-fusion
curl http://localhost:8080/healthz
```

---

## 14. Log Analysis Patterns

### Common Error Patterns

```bash
# Database errors
kubectl logs <pod> | grep -E "database|pg_|sql" | tail -20

# Authentication failures
kubectl logs <pod> | grep -E "unauthorized|auth|jwt|token" | tail -20

# Scheduling failures
kubectl logs <pod> | grep -E "schedule|gpu|topology|placement" | tail -20

# Edge sync errors
kubectl logs <pod> | grep -E "sync|offline|edge|vector_clock" | tail -20

# OOM events
kubectl get events -n cloudai-fusion | grep OOM
```

---

## 15. Diagnostic Commands Cheat Sheet

```bash
# === Platform Health ===
curl localhost:8080/healthz                    # API server
curl localhost:8090/healthz                    # AI engine
kubectl get pods -n cloudai-fusion             # All pods

# === Logs ===
kubectl logs -n cloudai-fusion -l app=cloudai-apiserver --tail=100
kubectl logs -n cloudai-fusion -l app=cloudai-scheduler --tail=100 -f

# === Resources ===
kubectl top pods -n cloudai-fusion
kubectl top nodes
kubectl describe node <node> | grep -A 20 "Allocated resources"

# === GPU ===
nvidia-smi                                     # GPU status
nvidia-smi topo -m                            # GPU topology
nvidia-smi --query-gpu=utilization.gpu,memory.used,temperature.gpu --format=csv

# === Database ===
kubectl exec -it <pg-pod> -- pg_isready
kubectl exec -it <pg-pod> -- psql -c "SELECT count(*) FROM pg_stat_activity;"

# === Redis ===
kubectl exec -it <redis-pod> -- redis-cli ping
kubectl exec -it <redis-pod> -- redis-cli INFO memory

# === Network ===
kubectl exec -it <pod> -- curl -v http://<service>:8080/healthz
kubectl exec -it <pod> -- nslookup <service>.cloudai-fusion.svc

# === Events ===
kubectl get events -n cloudai-fusion --sort-by='.lastTimestamp' | tail -30
```
