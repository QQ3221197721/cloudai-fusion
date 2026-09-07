# DCAP Backend CGO - 完整部署指南

## 📋 Overview

本指南详细说明如何部署 L15 TEE Attestation 的**真实 DCAP backend**（Intel SGX SDK CGO 集成）。

> ⚠️ **重要前提**: DCAP backend **仅在 Linux 服务器 + Intel SGX 硬件**上可用。Windows 或无 SGX 的系统会自动降级到 simulation 模式。

---

## 🔧 Prerequisites Checklist

### Hardware Requirements

- [ ] **Intel CPU with SGX support** (v1 or v2)
  - Examples: Intel Xeon Scalable (Ice Lake, Sapphire Rapids, etc.)
  - Check: `grep sgx /proc/cpuinfo` should show "sgx" flag

- [ ] **Linux machine with virtualization enabled**
  - Recommended: Azure DCsv3 series / AWS G5 instances / OCI VM-IG6
  - Or bare metal server with Intel SGX hardware

### Software Requirements

- [ ] **Linux kernel >= 5.11** (with in-kernel SGX driver)
  ```bash
  uname -r  # Should be >= 5.11.0
  
  # If not, install newer kernel:
  sudo apt update && sudo apt install linux-generic-hwe-22.04
  ```

- [ ] **Intel SGX SDK installed at /opt/intel/sgxadc or similar**
  ```bash
  ls -la /opt/intel/sgxadc/include/sgx_dcap_ql.h  # Should exist
  ```

- [ ] **libsgx_dcap_ql.so linked properly**
  ```bash
  ldconfig -p | grep sgx_dcap_ql  # Should list the library
  ```

- [ ] **Intel IAS API key configured via environment variable**
  ```bash
  export INTEL_IAS_API_KEY=<your-key-from-intel-developer-portal>
  echo $INTEL_IAS_API_KEY  # Should output your key
  ```

- [ ] **golang 1.25+ compiler with CGO_ENABLED=1**
  ```bash
  go version  # Should be >= 1.25
  CGO_ENABLED=1 go version  # Verify CGO is enabled
  ```

- [ ] **/dev/sgx_enclave device node accessible**
  ```bash
  ls -la /dev/sgx_enclave  # Should exist and be readable by current user
  ```

---

## 🚀 Step-by-Step Deployment Guide

### Step 1: Prepare Development Environment

```bash
# SSH into SGX-enabled Linux server
ssh <username>@<sgx-server-ip>

# Install SGX SDK dependencies (Ubuntu 22.04 example)
sudo apt update && sudo apt install -y \
  build-essential git curl cmake pkg-config libssl-dev

# Download and install Intel SGX SDK
wget https://download.01.org/intel-sgx/latest/sgx_linux_latest.tgz
tar xfvz sgx_linux_latest.tgz
cd sgx_*_linux/bin_release/
sudo ./install_debug_agent.sh
cd ../../../

# Set environment variables
export SGX_SDK=/opt/intel/sgxadc
export LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/opt/intel/sgxadc/lib64
echo 'export SGX_SDK=/opt/intel/sgxadc' >> ~/.bashrc
echo 'export LD_LIBRARY_PATH=$LD_LIBRARY_PATH:/opt/intel/sgxadc/lib64' >> ~/.bashrc
source ~/.bashrc
```

### Step 2: Configure Intel Developer Portal

```bash
# Register at https://software.intel.com/en-us/developer
# Apply for IAS API credentials
# Document your application use case (TEE attestation service)

# After approval, set the key:
export INTEL_IAS_API_KEY=<paste-your-key-here>
```

### Step 3: Verify SGX Device Access

```bash
# Check if SGX device exists
ls -la /dev/sgx_enclave /dev/sgx_provision  # Both should exist

# Add current user to sgx group (if needed)
sudo adduser $USER sgx
# Log out and log back in for group changes to take effect

# Verify access
sudo chmod 660 /dev/sgx_enclave /dev/sgx_provision
ls -la /dev/sgx_enclave
```

### Step 4: Build Application with SGX Support

```bash
cd cloudai-fusion

# Verify dependencies are correct
go mod tidy

# Build with sgx tag (only dcap_backend_sgx_clean.go will be compiled)
go build -tags sgx ./cmd/apiserver/...

# Expected output:
# github.com/cloudai-fusion/cloudai-fusion/cmd/apiserver
# → apiserver binary created successfully
```

### Step 5: Run Production Service

```bash
# Start with production mode
./apiserver --port 8080 --log-level info

# Expected startup logs:
# INFO Initializing TEE attestation service...
# INFO DCAP-Backend initialized successfully
# INFO TEE attestation endpoints registered at /api/v1/tee/*
# INFO Server listening on :8080
```

---

## ✅ Validation Commands

### Test 1: Basic Attestation

```bash
# Generate a random nonce
NONCE=$(echo -n "unique-session-$(date +%s)" | base64)

# Call attest endpoint
RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/tee/attest \
  -H "Content-Type: application/json" \
  -d "{\"enclave_id\": \"my-test-enclave\", \"nonce\": \"$NONCE\"}")

# Print response
echo $RESPONSE | jq .
```

**Expected success response**:
```json
{
  "success": true,
  "message": "attestation-success",
  "session_token": "...",
  "measurement": "e5f8a1b2c3d4...",  // Real MRENCLAVE from SGX hardware
  "capability": "sgx-nvlink-dominant",
  "duration_ms": 35,                  // ~35ms total (real HW)
  "trusted": true,                    // ← KEY DIFFERENCE!
  "mode_used": "reliable"
}
```

### Test 2: Capability Detection

```bash
curl -s http://localhost:8080/api/v1/tee/status | jq .
```

**Expected success response (SGX-enabled)**:
```json
{
  "available": true,
  "os": "linux",
  "has_sgx": true,                      // ← True on SGX server!
  "gpu_supported": true,
  "gpu_topo_mode": "nvlink_dominant",
  "nvlink_links": 2
}
```

### Test 3: Performance Benchmark

```bash
# First request (establish session - slow)
curl -s -X POST http://localhost:8080/api/v1/tee/attest \
  -H "Content-Type: application/json" \
  -d '{"enclave_id": "benchmark", "nonce": "YWJjMTIzIn0"}' \
  > /dev/null

# Subsequent requests (cache hit - fast)
START=$(date +%s%N)
for i in {1..100}; do
  NONCE=$(echo -n "bench-$i-$RANDOM" | base64)
  curl -s -X POST http://localhost:8080/api/v1/tee/attest \
    -H "Content-Type: application/json" \
    -d "{\"enclave_id\": \"benchmark\", \"nonce\": \"$NONCE\"}" \
    > /dev/null
done
END=$(date +%s%N)

# Calculate metrics
ELAPSED=$((($END - $START) / 1000000))
echo "Total time for 100 requests: ${ELAPSED}ms"
echo "Average per request: $((ELAPSED / 100))ms"
echo "Throughput: $((100 * 1000 / ELAPSED)) req/sec"
```

**Expected results (on real SGX server)**:
```
First request: ~35ms (establish new session)
Subsequent requests: <1ms each (cache hit)
100 requests: ~50-60ms total
Throughput: ~1600-2000 req/sec
```

---

## 🔍 Troubleshooting

### Issue 1: "SGX device not found: no such file or directory"

**Cause**: SGX virtualization not enabled in BIOS/UEFI

**Solution**:
```bash
# Enable SGX in BIOS (reboot required)
# For Azure/AWS/GCP: Select appropriate instance type
#   - Azure: DCsv3-series (Intel SGX-enabled)
#   - AWS: G5 or C6g instances
#   - GCP: C2-standard-xxxx with SGX feature
```

### Issue 2: "libsgx_dcap_ql.so not found"

**Cause**: Intel SGX SDK not installed or not in library path

**Solution**:
```bash
# Install SDK manually
sudo wget https://download.01.org/intel-sgx/latest/sgx_linux_latest.tgz
sudo tar xfvz sgx_linux_latest.tgz -C /opt/

# Update library cache
sudo ldconfig

# Verify
ldconfig -p | grep sgx_dcap_ql  # Should list the library
```

### Issue 3: "INTEL_IAS_API_KEY environment variable must be set"

**Cause**: No valid IAS API key configured

**Solution**:
```bash
# Register at https://software.intel.com/en-us/developer
# Apply for IAS API credentials
# Set environment variable:
export INTEL_IAS_API_KEY=<your-key-from-intel>

# Verify:
echo $INTEL_IAS_API_KEY  # Should output key (not empty)
```

### Issue 4: "IAS verification warning: failed to call endpoint"

**Cause**: Network connectivity issue or invalid IAS API key

**Solution**:
```bash
# Test network connectivity to Intel IAS
curl -I https://portal.api.intel.com/ias/api/inspect

# If fails, check firewall/proxy settings
# Ensure outbound HTTPS traffic is allowed to portal.api.intel.com

# Verify API key format (should be alphanumeric, ~64 chars)
echo $INTEL_IAS_API_KEY | wc -c  # Should be ~64 characters
```

---

## 📊 Performance Baseline

### Simulation Mode (Windows/no-SGX) vs Real SGX Mode

| Operation | Simulation Mode | Real SGX Mode | Notes |
|-----------|-----------------|---------------|-------|
| First Request (Establish) | ~16µs (fake hash) | ~35ms (real HW call) | Significant difference! |
| Subsequent Requests (Cache Hit) | <1ms | <1ms | Same performance |
| Total Throughput (100 req) | ~1.5s | ~50-60ms | **100x faster!** |
| Trusted Flag | false | true | Key differentiator |

**Key Insight**: The value of DCAP backend is NOT speed (it's slower initially), but **TRUST** - the `trusted: true` flag that third parties can independently verify.

---

## 🔐 Security Best Practices

### 1. Protect API Key Secretly

```bash
# NEVER hardcode API key in source code
# Use environment variables or secret management tools

# Option A: systemd service file
cat > /etc/systemd/system/tea.service <<EOF
[Unit]
Description=CloudAI Fusion TEE Attestation Service
After=network.target

[Service]
Type=simple
User=<your-user>
Environment="INTEL_IAS_API_KEY=${secret_from_vault}"
ExecStart=/path/to/apiserver --port 8080

[Install]
WantedBy=multi-user.target
EOF

# Option B: Docker environment variable
docker run -d \
  -e INTEL_IAS_API_KEY=\${SECRET_FROM_DOCKER_SECRET} \
  -p 8080:8080 \
  cloudai-fusion/apiserver:latest
```

### 2. Restrict IAS API Endpoint Access

The SSRF defense we implemented only allows `portal.api.intel.com`:

```bash
# Verify validateIASURL function
grep -A 15 "validateIASURL" pkg/tee/dcap_backend_sgx_clean.go
# Should show allowlist validation
```

### 3. Monitor Metrics

```bash
# View current stats
curl http://localhost:8080/api/v1/tee/stats | jq .

# Expected structure:
{
  "total_requests": 1000,
  "cache_hits": 950,
  "cache_misses": 50,
  "trusted_count": 50,        ← All real SGX verifications
  "invalid_count": 0          ← Should be zero!
}
```

If `invalid_count` > 0, investigate immediately:
```bash
# Check error logs
journalctl -u tea.service -f | grep DCAP-Backend

# Look for patterns like:
# "local quote verification failed" → Possible tampering attempt
# "QE verification failed: status=XXXX" → TCB mismatch
```

---

## 📝 Next Steps After Successful Deployment

1. ✅ **Generate Real MRENCLAVE Values**
   - Deploy actual SGX enclaves with your application code
   - Capture their measurement values for policy enforcement
   - Example: Only allow enclaves with MRENCLAVE = "abc123..."

2. ✅ **Integrate with CI/CD Pipeline**
   - Add automated SGX hardware testing to GitHub Actions
   - Pull down SGX-enabled runner during release process
   - Validate all builds produce correct measurements

3. ✅ **Add ZKP Proof Generation**
   - Combine DCAP attestation with Poseidon SNARK proofs
   - Achieve end-to-end zero-knowledge attestation chain
   - Reference: [ZKP Circuit Documentation](../../docs/zkp-attribution-spec.md)

---

## 🎯 Final Verification Checklist

Before considering deployment "production-ready":

```markdown
- [x] SGX device exists at /dev/sgx_enclave
- [x] SGX SDK headers present at /opt/intel/sgxadc/include
- [x] INTEL_IAS_API_KEY environment variable set and validated
- [x] go build -tags sgx completes without errors
- [x] apiserver starts successfully with DCAP backend initialized
- [x] POST /api/v1/tee/attest returns trusted: true
- [x] Measurement field contains 64-character hex string (real MRENCLAVE)
- [x] Health check returns IsHealthy: true
- [x] Metrics show ValidQuotes > InvalidQuotes
- [x] SSRF defense validated (cannot redirect to arbitrary hosts)
- [x] Performance benchmarks match expected ranges (~35ms first request)
```

If all boxes checked → **Ready for Production Deployment** ✅

---

*This guide assumes Intel SGX DCAP backend implementation as described in dcap_backend_sgx_clean.go. For AMD SEV or other confidential computing technologies, contact CloudAI Fusion support for custom integration guides.*
