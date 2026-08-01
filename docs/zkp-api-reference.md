# CloudAI Fusion Zero-Knowledge Proof API Reference

**Version**: v1.0  
**Last Updated**: 2026-08-05  

---

## 📋 Overview

This document describes the Zero-Knowledge Proof (ZKP) API endpoints for verifiable AI training provenance and model commitment using Poseidon hash functions.

---

## 🔑 Authentication

All API endpoints require Bearer token authentication:

```bash
Authorization: Bearer <your-api-token>
```

---

## 📊 Training Provenance Endpoints

### `POST /api/v1/zkp/training/trace`

**Description**: Capture a single training step trace with cryptographic commitment

**Request Body**:
```json
{
  "epoch": 1,
  "step": 100,
  "model_hash": "0x1234...abcd",
  "weights_hash": "0x5678...efgh",
  "metrics": {
    "loss": 0.5,
    "accuracy": 0.95,
    "learning_rate": 0.001,
    "gradient_norm": 0.1,
    "epoch_time_sec": 10.5,
    "gpu_util_percent": 85.0
  }
}
```

**Response** (`200 OK`):
```json
{
  "success": true,
  "proof_id": "proof_abc123",
  "epoch": 1,
  "step": 100,
  "commitment_hash": "poseidon_hash_value",
  "timestamp": "2026-08-05T10:30:00Z"
}
```

**Error Responses**:
- `400 Bad Request`: Invalid input format
- `500 Internal Server Error`: Processing failed

---

### `GET /api/v1/zkp/tracing/state`

**Description**: Get current training tracing state

**Query Parameters**: None

**Response** (`200 OK`):
```json
{
  "current_epoch": 5,
  "total_steps": 50,
  "average_loss": 0.35,
  "last_gradient_norm": 0.15,
  "model_hash": "0x1234...abcd",
  "last_update": "2026-08-05T10:35:00Z"
}
```

---

### `GET /api/v1/zkp/tracing/history`

**Description**: Get historical traces for a range of epochs/steps

**Query Parameters**:
- `epoch_start` (optional): Starting epoch (default: 1)
- `epoch_end` (optional): Ending epoch (default: last)
- `step_limit` (optional): Maximum steps per epoch (default: 100)

**Example Request**:
```bash
GET /api/v1/zkp/tracing/history?epoch_start=1&epoch_end=5&step_limit=10
```

**Response** (`200 OK`):
```json
{
  "traces": [
    {
      "epoch": 1,
      "step": 10,
      "loss": 0.95,
      "commitment": "poseidon_hash_value",
      "timestamp": "2026-08-05T10:30:00Z"
    },
    ...
  ],
  "total_count": 50,
  "epochs_covered": [1, 2, 3, 4, 5]
}
```

---

### `POST /api/v1/zkp/model/commit`

**Description**: Create new model commitment using Poseidon hash

**Request Body**:
```json
{
  "model_name": "my-model-v1",
  "model_type": "transformer",
  "parameters_count": 100000000,
  "training_data_hash": "dataset_hash_value",
  "hyperparameters": {
    "learning_rate": 0.001,
    "batch_size": 32,
    "optimizer": "adamw"
  }
}
```

**Response** (`200 OK`):
```json
{
  "success": true,
  "commitment_id": "commit_xyz789",
  "model_name": "my-model-v1",
  "poseidon_commitment": "0xposeidon...",
  "verification_key": "vk_abc123",
  "created_at": "2026-08-05T11:00:00Z"
}
```

---

## 🔍 Poseidon Hash Endpoints

### `POST /api/v1/zkp/hash/compute`

**Description**: Compute Poseidon hash over arbitrary data

**Request Body**:
```json
{
  "input": "base64_encoded_input_data",
  "input_format": "bytes|text|hex"
}
```

**Response** (`200 OK`):
```json
{
  "success": true,
  "hash": "0xposeidon_hash_result",
  "algorithm": "Poseidon",
  "field_size": 256,
  "computation_time_ms": 2.5
}
```

---

### `POST /api/v1/zkp/hash/verify`

**Description**: Verify that input produces expected Poseidon hash

**Request Body**:
```json
{
  "input": "base64_encoded_input_data",
  "expected_hash": "0xexpected_poseidon_hash"
}
```

**Response** (`200 OK`):
```json
{
  "valid": true,
  "computed_hash": "0xposeidon_hash_result",
  "matches_expected": true
}
```

**Error Response**:
- `400 Bad Request`: Invalid input or hash format

---

## 🎯 Benchmark Endpoints

### `GET /api/v1/zkp/benchmarks/performance`

**Description**: Get performance comparison between Poseidon and SHA256

**Response** (`200 OK`):
```json
{
  "benchmark_results": {
    "poseidon": {
      "avg_throughput_mbps": 1500,
      "avg_latency_us": 2.5,
      "memory_usage_kb": 128
    },
    "sha256": {
      "avg_throughput_mbps": 1200,
      "avg_latency_us": 3.2,
      "memory_usage_kb": 256
    },
    "performance_ratio": 1.25,
    "benchmark_date": "2026-08-05",
    "test_config": {
      "input_sizes_mb": [1, 10, 100],
      "iterations_per_size": 100
    }
  }
}
```

---

### `POST /api/v1/zkp/benchmarks/run`

**Description**: Run custom benchmarks

**Request Body**:
```json
{
  "test_configs": [
    {
      "input_size_mb": 1,
      "iterations": 100
    },
    {
      "input_size_mb": 10,
      "iterations": 100
    }
  ]
}
```

**Response** (`200 OK`):
```json
{
  "benchmark_id": "bench_abc123",
  "status": "completed",
  "results": [...same as GET endpoint...]
}
```

---

## 🔐 Verification Endpoints

### `POST /api/v1/zkp/proof/verify`

**Description**: Verify a training proof was generated correctly

**Request Body**:
```json
{
  "proof_id": "proof_abc123",
  "public_inputs": {
    "epoch": 1,
    "step": 100
  },
  "expected_model_hash": "0xexpected..."
}
```

**Response** (`200 OK`):
```json
{
  "valid": true,
  "proof_id": "proof_abc123",
  "verified_at": "2026-08-05T11:30:00Z",
  "verification_details": {
    "zksnark_verification": true,
    "poseidon_commitment_verified": true,
    "integrity_check_passed": true
  }
}
```

---

## 💡 Usage Examples

### Example 1: Capturing Training Trace

```bash
curl -X POST https://api.cloudai-fusion.io/api/v1/zkp/training/trace \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "epoch": 1,
    "step": 100,
    "model_hash": "0x1234567890abcdef",
    "weights_hash": "0xfedcba0987654321",
    "metrics": {
      "loss": 0.5,
      "accuracy": 0.95,
      "learning_rate": 0.001
    }
  }'
```

### Example 2: Checking Tracing State

```bash
curl -X GET https://api.cloudai-fusion.io/api/v1/zkp/tracing/state \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Example 3: Running Benchmarks

```bash
curl -X GET https://api.cloudai-fusion.io/api/v1/zkp/benchmarks/performance \
  -H "Authorization: Bearer YOUR_TOKEN"
```

---

## ⚠️ Rate Limits

| Endpoint | Rate Limit |
|----------|------------|
| `/training/trace` | 1000 requests/hour |
| `/tracing/*` | 5000 requests/hour |
| `/hash/*` | 10000 requests/hour |
| `/benchmarks/*` | 100 requests/hour |
| `/proof/verify` | 500 requests/hour |

Exceeding rate limits returns `429 Too Many Requests`.

---

## 📞 Support

For issues or questions:
- Email: zkp-support@cloudai-fusion.io
- GitHub Issues: https://github.com/cloudai-fusion/cloudai-fusion/issues
- Documentation: https://docs.cloudai-fusion.io/zkp
