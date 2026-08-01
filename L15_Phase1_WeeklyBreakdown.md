# 📅 L15 Phase 1 - Detailed Weekly Breakdown

**Total Duration**: 6 Weeks (Week 1-6)  
**Target Milestone**: Production-ready TEE attestation system

---

## 🗓️ Week 1: Intel IAS REST Client Foundation (Days 1-7)

### Day 1-2: Core Structure Design
- [x] Design `IASClient` struct with HTTP client configuration
- [ ] Implement `NewIASClient()` constructor
- [ ] Add hardcoded Intel Root CA PEM (with TODO: make dynamic fetch)
- [ ] Configure TLS settings (MinVersion=1.2, connection pooling)

**Deliverable**: `pkg/tee/intel_ias_client.go` (lines 1-80)

---

### Day 3-4: Quote Verification Logic
- [ ] Implement `InspectQuote()` method with JSON parsing
- [ ] Add error handling for all HTTP status codes
- [ ] Create `IASResponse` struct with proper tags
- [ ] Write base64 encoding/decoding utilities

**Deliverable**: Same file continued (lines 81-150)

---

### Day 5: Certificate Chain Verification
- [ ] Design `CertChainVerifier` interface
- [ ] Implement recursive issuer lookup
- [ ] Add validity period checks (NotBefore/NotAfter)
- [ ] Implement basic CRL cache stub

**Deliverable**: `pkg/tee/cert_chain_verification.go`

---

### Day 6: Mock Server Implementation
- [ ] Set up Python Flask server (mock_ias_server.py)
- [ ] Implement `/ias/v2/inspect` endpoint
- [ ] Add query params for test modes (--mode=random/fail/revoked)
- [ ] Document usage examples

**Deliverable**: `internal/tee/mock_ias_server.go(.py)`

---

### Day 7: Unit Test Suite
- [ ] Write `TestNewIASClient()` with negative test cases
- [ ] Implement `TestInspectQuoteSuccess()` with mock server
- [ ] Add `TestInspectQuoteRevoked()` and `TestInspectQuoteFail()`
- [ ] Run coverage report (target >80%)

**Deliverable**: `pkg/tee/intel_ias_client_test.go`

---

## 🗓️ Week 2: Certificate Validation + Documentation (Days 8-14)

### Day 8-9: Enhanced Certificate Checks
- [ ] Add CRL downloading logic (HTTPS fetch)
- [ ] Implement OCSP stapling stub
- [ ] Verify Extended Key Usage (EKU) extension
- [ ] Check Basic Constraints (CA:TRUE for intermediates)

**Deliverable**: Extend `cert_chain_verification.go`

---

### Day 10-11: Integration Test Framework
- [ ] Create `internal/tee/integration_test.go`
- [ ] Set up test fixtures (sample SGX quotes)
- [ ] Write end-to-end flow tests
- [ ] Add golden file comparison for responses

**Deliverable**: Integration test suite

---

### Day 12-13: Documentation Creation
- [ ] Write `docs/tee-hardware-setup.md`: Local dev environment
- [ ] Create `README.md` examples section
- [ ] Generate godoc HTML output
- [ ] Add API reference for all public interfaces

**Deliverable**: Complete documentation package

---

### Day 14: Code Review + Refactor
- [ ] Run `golangci-lint` and fix all issues
- [ ] Perform internal code review (self-review checklist)
- [ ] Optimize hot paths (if needed)
- [ ] Update CHANGELOG with new features

**Deliverable**: Clean PR candidate

---

## 🗓️ Week 3: AWS Nitro CLI Wrapper (Days 15-21)

### Day 15-16: CLI Command Execution Layer
- [ ] Design `NitroCLI` struct with exec wrapper
- [ ] Implement `RunEnclave()` method using `exec.Command`
- [ ] Add timeout control (max 5 minutes per operation)
- [ ] Capture stdout/stderr for debugging

**Deliverable**: `pkg/tee/aws_nitro_cli.go`

---

### Day 17-18: PCA (Proof of Concept Authority) Integration
- [ ] Import `aws-sdk-go-v2/service/ec2`
- [ ] Implement `GetEnclaveQuote()` method
- [ ] Add IAM permission validation helper
- [ ] Handle AWS retry policies (exponential backoff)

**Deliverable**: Same file continued (PCA client)

---

### Day 19: Enclave Binary Management
- [ ] Define `EnclaveBinary` interface
- [ ] Implement `LoadFromFile()` and `ValidateHash()`
- [ ] Add SHA-256 hash verification
- [ ] Support binary signing preparation (placeholder)

**Deliverable**: `pkg/tee/enclave_binary.go`

---

### Day 20-21: AWS Integration Tests
- [ ] Mock AWS SDK calls (using `aws.MockEC2`)
- [ ] Write unit tests for `NitroCLI` commands
- [ ] Test error recovery scenarios (instance unavailable, etc.)
- [ ] Record coverage metrics

**Deliverable**: Test suite + coverage report

---

## 🗓️ Week 4: Enclave Build Pipeline (Days 22-28)

### Day 22-23: Dockerfile.sgx Creation
```dockerfile
# cloudai-fusion/Dockerfile.sgx
FROM ubuntu:22.04 as sgx-builder

# Install Intel SGX SDK
RUN apt-get update && \
    apt-get install -y wget gnupg && \
    wget -O - https://download.01.org/intel-sgx/sgx_repo/ubuntu/intel-sgx-debu.key | apt-key add - && \
    echo "deb https://download.01.org/intel-sgx/sgx_repo/ubuntu jammy main" > /etc/apt/sources.list.d/intel-sgx.list && \
    apt-get update && \
    apt-get install -y sgx-default-simulated-dcap-attestation-server

COPY . /build
WORKDIR /build
RUN make sgx-build
```

**Deliverable**: `Dockerfile.sgx`

---

### Day 24-25: Makefile Rules
```makefile
.PHONY: sgx-build sgx-sign generate-quote clean

sgx-build:
	docker build -f Dockerfile.sgx -t cloudai-fusion/enclave-builder .

sgx-sign:
	sgx_sign sign -key enclave.key -path enclave.pcf -out enclave_signed.pcf

generate-quote:
	./tools/generate_quote.sh enclave.pcf > quote.hex

clean:
	rm -rf build/ enclave.pcf quote.hex
```

**Deliverable**: `Makefile.sgx`

---

### Day 26-28: CI Pipeline Skeleton
**File**: `.github/workflows/build-enclave.yml`

```yaml
name: Build Enclave Binary
on:
  push:
    tags: ['v*']

jobs:
  sgx-build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup Intel SGX SDK
        run: |
          # Download and configure SGX simulator
          wget ...
          
      - name: Build enclave
        run: make sgx-build
        
      - name: Generate quote
        run: make generate-quote
        
      - name: Upload artifacts
        uses: actions/upload-artifact@v4
        with:
          name: enclave-quotes
          path: quotes/*.json
```

**Deliverable**: CI workflow file

---

## 🗓️ Week 5: Provider Interface Standardization (Days 29-35)

### Day 29-30: HardwareProvider Interface Refinement
- [ ] Define common interface in `hardware_providers.go`
- [ ] Abstract `VerifyQuote()` signature consistency
- [ ] Add standardized error types (`TEEError`, `AttestationFailed`)
- [ ] Implement logging hooks for both providers

**Deliverable**: Updated interface definitions

---

### Day 31-32: Error Handling Standardization
- [ ] Create `errors.go` with custom error types
- [ ] Implement `Unwrap()` for error chaining
- [ ] Add detailed error messages with remediation hints
- [ ] Write unit tests for error formatting

**Deliverable**: `pkg/tee/errors.go`

---

### Day 33-35: Logging Integration
- [ ] Integrate with CloudAI logger framework (`pkg/logging`)
- [ ] Add structured logs for all attestation events
- [ ] Trace IDs propagation across provider boundary
- [ ] Redaction rules for sensitive data (quotes, keys)

**Deliverable**: Logger middleware

---

## 🗓️ Week 6: E2E Testing + Production Readiness (Days 36-42)

### Day 36-37: Full System Integration Tests
- [ ] Orchestrate Intel IAS flow end-to-end
- [ ] Test failover between real/mock modes
- [ ] Measure latency under load (stress test: 100 QPS)
- [ ] Document performance benchmarks

**Deliverable**: `pkg/tee/integration_test.go`

---

### Day 38-39: Production Checklist Implementation
- [ ] Add circuit breaker pattern (max 3 retries, then degrade)
- [ ] Implement caching layer for valid quotes (24h TTL)
- [ ] Configure feature flags for gradual rollout
- [ ] Set up health checks (`/healthz?component=tee`)

**Deliverable**: Production-grade deployment configs

---

### Day 40-41: Documentation + Runbooks
- [ ] Update `docs/production-tee-deployment.md`
- [ ] Create troubleshooting guide (FAQ format)
- [ ] Add monitoring dashboard specs (Grafana JSON)
- [ ] Write incident response playbooks

**Deliverable**: Complete ops manual

---

### Day 42: Final Sign-off + Release Preparation
- [ ] Run all tests locally + CI
- [ ] Generate release notes
- [ ] Bump version number (v0.2.0 -> v0.3.0)
- [ ] Tag commit and publish draft release

**Deliverable**: Production release candidate

---

## 📈 Weekly KPI Tracking

| Week | Deliverable | Status | Issues Encountered |
|------|-------------|--------|-------------------|
| Week 1 | Intel IAS Client | ⏳ In Progress | None so far |
| Week 2 | Certificate Validation | ⏳ Pending | Need Intel Root CA updates |
| Week 3 | AWS Nitro CLI | ❌ Not Started | Requires AWS account setup |
| Week 4 | Enclave Build | ❌ Not Started | Docker image optimization needed |
| Week 5 | Interface Standards | ❌ Not Started | Depends on Weeks 3-4 |
| Week 6 | Production Ready | ❌ Not Started | Buffer time for unexpected issues |

---

## 🚨 Risk Mitigation

### High-Risk Items
1. **Intel IAS API Rate Limits**
   - Impact: Slow quote verification
   - Mitigation: Cache results for 24h, batch multiple requests
   
2. **AWS EC2 Instance Availability**
   - Impact: Nitro enclave creation failures
   - Mitigation: Fallback to simulated mode with warning logs

3. **Docker Image Size**
   - Impact: Slow CI pipeline (>2 hours)
   - Mitigation: Multi-stage builds, scratch images

### Contingency Plan
If any major blocker occurs:
1. Drop that specific feature temporarily
2. Mark as "simulated" in `/api/v1/capabilities`
3. Proceed with other components
4. Revisit blocked item after stakeholder discussion

---

**Next Action**: Begin Week 1 Day 1 tasks immediately upon approval!
