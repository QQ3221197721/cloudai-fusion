# CloudAI Fusion v1.0.0 - REST API Reference

**Version**: 1.0.0  
**Date**: September 5, 2026  
**Base URL**: `http://localhost:8080/api/v1`  

---

## Authentication

All endpoints require Bearer token authentication (optional for local dev):
```bash
curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8080/health
```

Production requires mTLS certificates configured in server deployment.

---

## Core Endpoints

### Health Check

**Endpoint**: `GET /health`

**Purpose**: Verify service availability and readiness

**Response**:
```json
{
  "status": "healthy",
  "timestamp": "2026-09-05T16:00:00Z",
  "uptime_seconds": 3600,
  "version": "1.0.0"
}
```

**cURL Example**:
```bash
curl http://localhost:8080/health
```

---

### Schedule Workload

**Endpoint**: `POST /schedule`

**Purpose**: Submit a new workload to the scheduler queue

**Request Body**:
```json
{
  "name": "train-model-v1",
  "type": "training",
  "gpu_count": 4,
  "timeout_minutes": 120,
  "priority": "high",
  "requirements": {
    "nvlink_required": true,
    "numa_affinity": "same_node",
    "min_bandwidth_gbps": 600
  },
  "labels": {
    "team": "ml-training",
    "project": "model-v2"
  }
}
```

**Response**:
```json
{
  "job_id": "job-abc123def456",
  "status": "queued",
  "created_at": "2026-09-05T16:00:00Z",
  "estimated_start": "2026-09-05T16:05:00Z"
}
```

**Error Codes**:
- `400`: Invalid request body schema
- `409`: Job name already exists
- `429`: Rate limit exceeded

**cURL Example**:
```bash
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{
    "name": "train-model-v1",
    "type": "training",
    "gpu_count": 4,
    "timeout_minutes": 120,
    "priority": "high"
  }'
```

---

### List Jobs

**Endpoint**: `GET /jobs`

**Purpose**: Retrieve all scheduled jobs with optional filtering

**Query Parameters**:
| Parameter | Type | Description |
|-----------|------|-------------|
| `status` | string | Filter by status: queued/pending/completed/failed/cancelled |
| `limit` | int | Max results (default: 100, max: 1000) |
| `offset` | int | Pagination offset |
| `priority` | string | Filter by priority: low/normal/high |

**Response**:
```json
{
  "total_count": 150,
  "jobs": [
    {
      "job_id": "job-abc123def456",
      "name": "train-model-v1",
      "type": "training",
      "status": "completed",
      "created_at": "2026-09-05T16:00:00Z",
      "completed_at": "2026-09-05T17:30:00Z",
      "assignment": {
        "node_name": "gpu-node-03",
        "gpu_indices": [0,1,2,3],
        "score": 142.5
      }
    }
  ],
  "pagination": {
    "limit": 100,
    "offset": 0,
    "has_more": true
  }
}
```

**cURL Example**:
```bash
curl "http://localhost:8080/api/v1/jobs?status=completed&limit=50"
```

---

### Get Job Details

**Endpoint**: `GET /jobs/{job_id}`

**Purpose**: Retrieve detailed information about a specific job

**Path Parameters**:
- `job_id`: Job identifier (e.g., `job-abc123def456`)

**Response**:
```json
{
  "job_id": "job-abc123def456",
  "name": "train-model-v1",
  "type": "training",
  "status": "pending",
  "gpu_count": 4,
  "priority": "high",
  "created_at": "2026-09-05T16:00:00Z",
  "updated_at": "2026-09-05T16:02:00Z",
  "timeout_minutes": 120,
  "requirements": {
    "nvlink_required": true,
    "numa_affinity": "same_node"
  },
  "scheduled_assignment": null,
  "candidate_scoreboard": [
    {
      "node_name": "gpu-node-01",
      "score": 135.2,
      "reasons": ["nvlink-scored", "connected-gpus=8"]
    },
    {
      "node_name": "gpu-node-02", 
      "score": 128.7,
      "reasons": ["insufficient-gpus"]
    }
  ]
}
```

**cURL Example**:
```bash
curl http://localhost:8080/api/v1/jobs/job-abc123def456
```

---

### Cancel Job

**Endpoint**: `DELETE /jobs/{job_id}`

**Purpose**: Cancel a queued or pending job (cannot cancel running/completed jobs)

**Path Parameters**:
- `job_id`: Job identifier

**Response**:
```json
{
  "job_id": "job-abc123def456",
  "previous_status": "pending",
  "new_status": "cancelled",
  "cancelled_at": "2026-09-05T16:10:00Z",
  "released_resources": {
    "gpu_count": 4,
    "memory_bytes": 17179869184
  }
}
```

**Error Codes**:
- `404`: Job not found
- `409`: Cannot cancel job in current state (only queued/pending allowed)

**cURL Example**:
```bash
curl -X DELETE http://localhost:8080/api/v1/jobs/job-abc123def456
```

---

### Get Scheduler Metrics

**Endpoint**: `GET /metrics/scheduler`

**Purpose**: Retrieve real-time scheduler performance metrics

**Response**:
```json
{
  "queue_length": 42,
  "running_jobs": 18,
  "scheduled_last_minute": 15,
  "avg_scheduling_latency_ms": 12.5,
  "p99_scheduling_latency_ms": 45.2,
  "nodes_total": 8,
  "nodes_available": 6,
  "total_gpu_capacity": 64,
  "gpu_utilization_avg": 68.5,
  "fragmentation_factor": 0.92,
  "active_policies": ["DASP", "CostAware"],
  "rl_optimizer_training_enabled": false,
  "last_snapshot_timestamp": "2026-09-05T16:00:00Z"
}
```

**Prometheus Format Endpoint**: `GET /metrics` (OpenMetrics compatible)

**cURL Example**:
```bash
curl http://localhost:8080/metrics/scheduler
```

---

### Create Manifest

**Endpoint**: `POST /manifests`

**Purpose**: Create a reusable workload manifest template

**Request Body**:
```json
{
  "name": "gpu-training-template",
  "description": "Standard multi-GPU training job template",
  "spec": {
    "gpu_count": 8,
    "timeout_minutes": 240,
    "priority": "high",
    "requirements": {
      "nvlink_required": true,
      "numa_affinity": "same_node"
    }
  },
  "labels": {
    "template_type": "training",
    "team": "ml-infra"
  }
}
```

**Response**:
```json
{
  "manifest_id": "manifest-def789ghi012",
  "name": "gpu-training-template",
  "created_at": "2026-09-05T16:00:00Z",
  "usage_count": 0
}
```

**cURL Example**:
```bash
curl -X POST http://localhost:8080/api/v1/manifests \
  -H "Content-Type: application/json" \
  -d '{"name":"gpu-training-template","spec":{"gpu_count":8,"priority":"high"}}'
```

---

## Error Responses

### Standard Error Format

```json
{
  "error_code": "INVALID_REQUEST",
  "message": "Request body failed validation: missing required field 'gpu_count'",
  "details": {
    "field": "gpu_count",
    "reason": "required integer > 0"
  },
  "request_id": "req-xyz123",
  "timestamp": "2026-09-05T16:00:00Z"
}
```

### Common Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `INVALID_REQUEST` | 400 | Request body schema validation failed |
| `NOT_FOUND` | 404 | Resource does not exist |
| `CONFLICT` | 409 | State conflict (e.g., cannot modify completed job) |
| `RATE_LIMITED` | 429 | Too many requests from this client |
| `UNAUTHORIZED` | 401 | Missing or invalid authentication token |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `INTERNAL_ERROR` | 500 | Server-side error (contact admin) |

---

## Rate Limiting

Production deployments enforce rate limits:
- **Default**: 100 requests/minute per IP/client
- **Schedule endpoint**: Additional burst allowance (50 requests/burst)

Rate limit headers included in responses:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 85
X-RateLimit-Reset: 1630876800
```

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| v1.0.0 | 2026-09-05 | Initial production release |

For compatibility guarantees, prefix requests with API version:
```bash
curl -H "API-Version: v1" http://localhost:8080/api/v1/schedule
```

---

*API reference generated: September 5, 2026*
*Compatible with CloudAI Fusion v1.0.0*
