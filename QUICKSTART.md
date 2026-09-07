# CloudAI Fusion v1.0.0 - Quick Start Guide

**Time to First Schedule**: < 5 minutes!

---

## Step 1: Prerequisites (2 minutes)

Install required tools:

```bash
# Linux/MacOS
curl -LO https://dl.google.com/go/dl/go1.26.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.26.*.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Docker
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER

# Kubernetes (optional)
curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.27.0/bin/linux/amd64/kubectl
chmod +x kubectl
sudo mv kubectl /usr/local/bin/
```

Verify installation:
```bash
go version      # Expected: go version go1.26...
docker --version    # Expected: Docker version 24.0+
kubectl version --client  # Optional
```

---

## Step 2: Clone Repository (30 seconds)

```bash
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion
```

---

## Step 3: Build & Run Locally (2 minutes)

### Option A: Local Development (Recommended for Testing)

```bash
# Build binary
go build -o apiserver ./cmd/apiserver

# Create minimal config
cat > config.yaml << EOF
cluster_id: local-dev-cluster
log_level: debug
db_url: postgresql://postgres:password@localhost:5432/cloudai_fusion?sslmode=disable
redis_url: localhost:6379
EOF

# Run server
./apiserver --config config.yaml
```

API now available at http://localhost:8080

**Health Check**:
```bash
curl http://localhost:8080/health
# Expected: {"status":"healthy","timestamp":"..."}
```

### Option B: Docker Compose (Full Stack in 1 Command)

```bash
cd docker
docker-compose up -d

# Verify everything running
docker-compose ps

# Access API
curl http://localhost:8080/health

# View logs
docker-compose logs -f apiserver
```

### Option C: Kubernetes Cluster (Production)

```bash
# Install Helm chart
helm repo add cloudai-fusion https://cloudai-fusion.github.io/helm-charts
helm install cloudai-fusion cloudai-fusion/fusion \
  --namespace cloudai-system --create-namespace \
  --set replicas=3 \
  --set config.DB_URL="postgresql://user:pass@db-host:5432/cloudai" \
  --set config.REDIS_URL="redis://redis-host:6379"

# Wait for rollout
kubectl rollout status deployment/cloudai-fusion-apiserver -n cloudai-system

# Get external IP if using LoadBalancer service
kubectl get svc cloudai-fusion-apiserver -n cloudai-system
```

---

## Step 4: Use cafctl CLI (1 minute)

### Build CLI Tool

```bash
go build -o cafctl ./cmd/cafctl
export PATH=$PWD:$PATH
```

### Basic Commands

```bash
# View help
cafctl --help

# Create workload manifest
cafctl manifest create --name test-job --gpu-count 2 --timeout 30m

# Submit job to scheduler
cafctl submit test-job.yaml

# List all jobs
cafctl list jobs

# Check job status
cafctl get job test-job-123

# Cancel job
cafctl cancel test-job-123

# View scheduling metrics
cafctl metrics scheduler
```

### Real Example Workflow

```bash
# 1. Create GPU allocation request
cat > my-job.yaml << EOF
name: train-model-v1
type: training
gpu_count: 4
timeout: 2h
priority: high
requirements:
  nvlink_required: true
  numa_affinity: same_node
EOF

cafctl manifest create my-job.yaml

# 2. Submit to cluster
cafctl submit my-job.yaml

# 3. Monitor progress
cafctl watch my-job-123

# 4. When complete, analyze results
cafctl history my-job-123
cafctl evidence my-job-123
```

---

## Step 5: Test Scheduling (2 minutes)

### Scenario 1: Single GPU Job
```bash
# Submit small job
echo '{"name":"test-gpu-single","type":"inference","gpu_count":1,"priority":"normal"}' | \
  cafctl submit --stdin

# Should schedule within milliseconds
cafctl get job $(cafctl list jobs | tail -1 | awk '{print $1}')
```

### Scenario 2: Multi-GPU NVLink Job
```bash
# Request NVLink-connected GPUs
cat > nvlink-job.yaml << EOF
name: train-distributed
type: training
gpu_count: 8
requirements:
  nvlink_required: true
  min_bandwidth_gbps: 600
EOF

cafctl submit nvlink-job.yaml
# Scheduler will place on same node with full NVLink connectivity
```

### Scenario 3: Cost-Aware Batch
```bash
# Budget-constrained batch processing
cat > cost-job.yaml << EOF
name: batch-inference
type: inference
gpu_count: 16
max_cost_per_hour: 50.0
EOF

cafctl submit cost-job.yaml
# Will choose cheapest available nodes meeting constraints
```

---

## Next Steps

After successful deployment:

1. **Configure Production Settings** (See `DEPLOYMENT_GUIDE.md`)
   - Set up TLS certificates
   - Configure RBAC for security
   - Tune database parameters

2. **Monitor Performance**
   ```bash
   curl http://localhost:8080/metrics
   ```

3. **Integrate Your Applications** (See `API_REFERENCE.md`)
   - Use REST endpoints for programmatic access
   - Implement custom schedulers as plugins

4. **Scale Up**
   - Add more nodes to cluster
   - Increase replica count for high availability
   - Enable auto-scaling based on queue depth

---

## Troubleshooting

**Problem**: `cannot connect to database`
- Solution: Check DB_URL environment variable, ensure PostgreSQL is running
- Debug: `psql $(DB_URL) -c "SELECT 1"`

**Problem**: `scheduler not responding`
- Solution: Check logs, verify Redis connection
- Debug: `docker-compose logs apiserver | grep -i error`

**Problem**: `job stuck in pending state`
- Solution: Check node resource availability
- Debug: `cafctl describe job <job-id>`

For more issues, see `TROUBLESHOOTING.md` or open GitHub issue.

---

*Quick Start generated: September 5, 2026*
*Compatible with v1.0.0 only*