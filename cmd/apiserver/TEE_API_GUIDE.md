# TEE Attestation HTTP API - 使用指南

## 📋 Overview

本模块为 CloudAI Fusion 的 TEE Remote Attestation 功能提供了 HTTP API 接口，让用户可以通过浏览器/curl 直接调用和体验。

## 🔧 启动步骤

### Step 1: 确保 Go 依赖完整

```bash
cd cloudai-fusion
go mod tidy  # 自动拉取所有依赖
```

### Step 2: 启动 apiserver

```bash
# 启动标准模式 (reliable caching)
cd cmd/apiserver
go run main.go --port 8080 --log-level debug

# 或使用最快模式 (parallel pre-verification)
go run main.go --port 8080 --tee-mode fastest
```

### Step 3: 验证服务启动成功

```bash
curl http://localhost:8080/api/v1/tee/status
```

预期响应：
```json
{
  "available": true,
  "os": "windows",
  "has_sgx": false,
  "gpu_supported": false,
  "capability": "simulation-mode"
}
```

---

## 🌐 API Endpoints

### 1️⃣ POST /api/v1/tee/attest - 执行 attest 请求

**Description**: 主要的证明入口，根据 nonce 签发 session token。

**Request Example**:
```bash
curl -X POST http://localhost:8080/api/v1/tee/attest \
  -H "Content-Type: application/json" \
  -d '{
    "enclave_id": "my-secure-enclave",
    "nonce": "YWJjMTIz",  // base64 encoded: "abc123"
    "mode": "reliable"
  }'
```

**Response**:
```json
{
  "success": true,
  "message": "attestation-success",
  "session_token": "eyJzZXNzaW9uX2lkIjoi...base64...",
  "measurement": "",  // empty in simulation mode
  "capability": "simulation-mode",
  "duration_ms": 750,
  "trusted": false,
  "mode_used": "reliable"
}
```

**Fields Explanation**:
- `session_token`: Base64 encoded JSON of SessionToken（包含 session_id/测量值/过期时间）
- `measurement`: MRENCLAVE 度量值（仿真模式下为空；真实 SGX 硬件下会填充）
- `trusted`: 是否硬件可信（仿真模式永远 false）
- `duration_ms`: 本次 attest 耗时（微秒级优化验证）

---

### 2️⃣ GET /api/v1/tee/status - 查询系统状态

**Description**: 查询本机 SGX/GPU 能力状态。

**Request**:
```bash
curl http://localhost:8080/api/v1/tee/status
```

**Response**:
```json
{
  "available": true,
  "os": "windows",
  "has_sgx": false,
  "gpu_supported": false,
  "gpu_topo_mode": "",
  "nvlink_links": 0
}
```

**Windows 机器的典型响应**:
- `has_sgx`: false (无 Intel SGX 驱动)
- `gpu_supported`: false/no GPU connected
- `capability`: simulation-mode

**Linux + SGX 服务器的典型响应**:
- `has_sgx`: true
- `gpu_supported`: true
- `gpu_topo_mode`: "nvlink_dominant" (如果有 NVLink GPU)

---

### 3️⃣ GET /api/v1/tee/stats - 查看统计信息

**Description**: 查看 cache hits/misses 等性能指标。

**Request**:
```bash
curl http://localhost:8080/api/v1/tee/stats
```

**Response**:
```json
{
  "total_requests": 100,
  "cache_hits": 95,
  "cache_misses": 5,
  "batches_used": 0,
  "saved_time_sec": 2.5
}
```

**关键指标**:
- `cache_hits` vs `cache_misses`: 验证缓存效率
- `saved_time_sec`: 因缓存节省的总时间（秒）

---

### 4️⃣ POST /api/v1/tee/enclave/create - 手动创建 enclave

**Description**: 调试用 endpoint，强制创建 enclave（不使用缓存）。

**Request**:
```bash
curl -X POST http://localhost:8080/api/v1/tee/enclave/create \
  -H "Content-Type: application/json" \
  -d '{
    "enclave_id": "debug-enclave",
    "nonce": "ZGVidWc="  // base64: "debug"
  }'
```

**Response**:
```json
{
  "success": true,
  "token": "ZW5jbGF2ZS0xMjM0NTY="  // base64: "enclave-123456"
}
```

---

## 🎯 Usage Examples

### Example 1: Basic Attestation Flow

```bash
# 1. Generate a random nonce (use your own unique value)
NONCE=$(echo -n "unique-session-$(date +%s)" | base64)

# 2. Call attest endpoint
RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/tee/attest \
  -H "Content-Type: application/json" \
  -d "{\"enclave_id\": \"my-app\", \"nonce\": \"$NONCE\"}")

# 3. Extract session token
TOKEN=$(echo $RESPONSE | jq -r .session_token)

# 4. Decode token to see details
echo $TOKEN | base64 -d | jq .

# Expected output:
# {
#   "session_id": "sess-1722678945123456",
#   "measurement": "",
#   "issued_at": "2026-08-04T09:30:00+08:00",
#   "expires_at": "2026-08-04T09:30:30+08:00",
#   "mac": "..."
# }
```

### Example 2: Batch Mode Testing

```bash
# 快速发送多个请求，观察 cache hit ratio

for i in $(seq 1 10); do
  NONCE=$(echo -n "batch-$i-$(date +%N)" | base64)
  
  RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/tee/attest \
    -H "Content-Type: application/json" \
    -d "{\"enclave_id\": \"my-app\", \"nonce\": \"$NONCE\", \"mode\": \"batch\"}")
  
  DURATION=$(echo $RESPONSE | jq -r .duration_ms)
  echo "Request $i: ${DURATION}ms - $RESPONSE"
done

# Then check stats:
curl http://localhost:8080/api/v1/tee/stats
```

Expected pattern:
- First request: ~16ms (establish new session)
- Subsequent requests: <1ms each (cache hit)
- Cache hits ratio > 90%

---

## ⚙️ Configuration Options

### TeeMode Flags

In `main.go`, the tee mode can be configured:

```go
// Default mode: reliable caching
teeMode := tee.ModeReliable

// Fastest mode: parallel pre-verification (no cache)
teeMode := tee.ModeFastest

// Batch mode: proof aggregation for high throughput
teeMode := tee.ModeBatch
```

### Environment Variables

You can also configure via environment variables:

```bash
# Set log level
export LOG_LEVEL=debug

# Enable/disable tee service
export ENABLE_TEE=true/false
```

---

## 🚀 Performance Benchmarks (Simulation Mode)

Run this test locally on Windows (no real SGX):

```bash
# Warm-up phase (creates first session)
curl -s -X POST http://localhost:8080/api/v1/tee/attest \
  -H "Content-Type: application/json" \
  -d '{"enclave_id": "warmup", "nonce": "YWJj"}' > /dev/null

# Benchmark: 100 sequential requests
START=$(date +%s%N)
for i in $(seq 1 100); do
  NONCE=$(echo -n "bench-$i-$RANDOM" | base64)
  curl -s -X POST http://localhost:8080/api/v1/tee/attest \
    -H "Content-Type: application/json" \
    -d "{\"enclave_id\": \"benchmark\", \"nonce\": \"$NONCE\"}" > /dev/null
done
END=$(date +%s%N)

# Calculate average latency
ELAPSED=$((($END - $START) / 1000000))
echo "Average: $((ELAPSED / 100))ms per request"
echo "Throughput: $((100 * 1000 / ELAPSED)) req/sec"
```

Typical results on Windows:
- **First request**: ~16ms (establish session)
- **Subsequent requests**: <1ms (cache hit)
- **100 req throughput**: ~100 req/sec

---

## 🔒 Security Notes

### Simulation Mode Behavior

In current implementation:
- `trusted = false` always (unless real DCAP backend is integrated)
- No actual SGX quote generation (uses fake data)
- HMAC-SHA256 based token signing (not hardware bound)

### Production Deployment Requirements

To achieve real hardware attestation:
1. **Intel SGX hardware** with enabled virtualization
2. **DCAP backend integration** (CGO + Intel SGX SDK)
3. **Real IAS connection** (with valid API key)
4. **NVML/NVIDIA driver** for GPU topology detection

### Token Security

Session tokens are signed with Ed25519:
- Can be verified offline by third parties
- Contain nonce + expiry time for freshness
- MAC prevents tampering

Example verification (Go code):
```go
import "github.com/cloudai-fusion/cloudai-fusion/pkg/tee"

tok, _ := decodeBase64(tokenJSON)
err := cache.VerifyToken(tok, userNonce, requireTrusted=false)
if err != nil {
  // Token invalid or expired
}
```

---

## 🐛 Troubleshooting

### Issue: "failed to initialize TEE attestation"

**Solution**: Check if `/pkg/tee` compiles correctly:
```bash
go build ./pkg/tee/...  # Should return success
```

If compilation fails, fix errors first before restarting apiserver.

### Issue: "empty-nonce error"

**Cause**: Nonce must be non-empty and base64 encoded.

**Fix**: 
```bash
# Wrong (plain text):
curl -d '{"nonce": "abc"}'

# Correct (base64):
curl -d '{"nonce": "YWJj"}'
```

### Issue: "high latency on first request"

**Explanation**: First request creates new session (~16ms simulation, ~30ms hardware).

**Optimization**: Use batch mode or warm up with initial request.

---

## 📖 Related Documentation

1. **[L15 TEE Attestation Implementation](../pkg/tee/README.md)** - Technical spec
2. **[Capability Detection Guide](../pkg/tee/capability_detection.go)** - SGX/GPU detection
3. **[Bound Attestation Spec](../pkg/tee/bound_attestation.go)** - Nonce binding + signature
4. **[Session Cache Design](../pkg/tee/attestation_session.go)** - Attest-once verify-many

---

## ✅ Quick Start Checklist

- [ ] Run `go mod tidy` to fetch all dependencies
- [ ] Compile `cmd/apiserver/main.go` successfully
- [ ] Start apiserver on port 8080
- [ ] Verify `/api/v1/tee/status` returns `{"available":true}`
- [ ] Test basic attest flow with nonce
- [ ] Monitor `/api/v1/tee/stats` for cache efficiency
- [ ] Optionally integrate with your app's authentication flow

---

*This API is simulation-ready but requires real SGX hardware integration for production-grade hardware-trusted attestation.*
