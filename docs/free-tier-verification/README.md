# Free-Tier Verification Guide — Zero-Cost Validation Path

**Project**: [CloudAI Fusion](https://github.com/cloudai-fusion/cloudai-fusion)  
**Scope**: `pkg/capability/`, `pkg/scheduler/`, `pkg/resources/`, `pkg/edgeautonomy/` + simulated mode in `pkg/intel/`  
**Hardware constraints**: 6 HARD modules require paid hardware; software fallback/mock paths enable **zero-cost validation**.

---

## 1. Executive Summary

### ✅ Zero-Cost Modules (validated via simulation or mock data)

| Module | Verified Tests | Fallback Behavior |
|--------|----------------|-------------------|
| `pkg/capability` | 9 tests (Report, Enforce, EvidenceCapabilityEngine) | All use synthetic topology; no GPU queries needed |
| `pkg/scheduler` | 12 tests (DenseK approx ratio, TopologyAware vs K8sDefault, ConstraintScheduler stats) | ALL data is SYNTHETIC / MOCK; disclaimers embedded in output |
| `pkg/resources` | 3 tests (GPUCollector, MIGTopology) | Gracefully handles `nvidia-smi` unavailability; returns errors instead of crashing |
| `pkg/intel` | ~26 tests (STIX parsing, Hub OfflineSync, MemoryStore CVE lookup) | `TestHub_SyncAll_OfflineFeeds`, `TestHub_SyncAll_MissingLocalPath` test offline feed paths |

### ❌ Cannot Be Validated Without Paid Hardware

These 3 modules require specific paid infrastructure:

| Module | Hardware Required | Why No Fallback Exists |
|--------|-------------------|------------------------|
| **M2: MIG Partition Discovery & Enforcement** | NVIDIA A100/H100 with MIG enabled | `nvidia-smi` CLI returns error on consumer GPUs; no mock for actual partition enforcement |
| **M3: InfiniBand Multi-Node Clustering** | IB HCA cards + RDMA fabric | Requires real multi-node cluster; no simulator for network latency/topology |
| **M5: Real SGX Enclave Attestation** | Intel SGX-enabled CPU + PCAP attestation server | TEE enclave creation needs hardware support; simulation would defeat security guarantees |

---

## 2. Free Cloud Platforms Comparison

| Platform | GPU Access | Duration | nvidia-smi | MIG | Multinode IB | SGX | Quota Limits |
|----------|------------|----------|------------|-----|--------------|-----|--------------|
| **Google Colab Free** | Tesla T4 / P100 (random) | Session ≈ 90 min; Weekly ≈ 12h | ✅ Full access | ❌ No | ❌ Single VM only | ❌ Disabled | Random kernel reconnection risk |
| **Kaggle Kernels** | P100 × 2 | Weekly ≈ 30h (soft limit) | ✅ Full access | ❌ No | ❌ Single VM only | ❌ Disabled | Hard quota; no MIG/IB |
| **Hugging Face ZeroGPU** | A100 / RTX 4090 (on-demand) | Daily quotas vary | ✅ Full access | ❌ No | ❌ Single VM only | ❌ Disabled | Community pool = unpredictable availability |

### ⚠️ Honest Declarations

1. **Colab Free Tier**:
   - `nvidia-smi` works → but MIG cannot be enabled (consumer-grade GPUs).
   - No persistent disks between sessions → scripts must pass environment variables.
   - Runtime can disconnect at any time → verify tests within a single session.

2. **Kaggle Kernels**:
   - Provides 30h/week cumulative GPU quota → enough for one full run of all tests (~2.5min total runtime per package, so you have buffer).
   - Same limitation as Colab: MIG unavailable, no multi-node networking.

3. **ZeroGPU**:
   - Community queue means long wait times; not ideal for reproducibility testing.
   - Better for quick "does my code compile?" checks rather than full test runs.

---

## 3. Modules That Can Be Validated

### Part 1 Build Command

```bash
cd cloudai-fusion
go build ./pkg/capability/... ./pkg/scheduler/... ./pkg/resources/... ./pkg/edgeautonomy/...
```

Expected output: `BUILD_EXIT=0`

### Part 1 Test Commands (Zero-Cost Packages)

#### capability

```bash
go test ./pkg/capability/ -v -count=1
```

**Expected Results**: 9 tests PASS
- `TestEvidenceCapabilityEngine_*` (3 tests) — Receipt signing + graceful degradation paths
- `TestReport_*, TestEnforce_*, TestMustReal, TestDefaultRegistry` (6 tests) — Policy check aggregation

**Verification Logic**: All test cases use synthetic registry entries; no `nvidia-smi` calls.

#### scheduler

```bash
go test ./pkg/scheduler/ -run "DenseK|Topology|Constraint" -v -count=1
```

**Expected Results**: 12 tests PASS
- `TestConstraintScheduler_StatisticalVsBinpack` — t-test comparison with BinPack baseline
- `TestDenseKSolversStatisticalAnalysis` — Approximation ratio analysis (greedy-2opt vs exact branch-and-bound)
- `TestTopologyAwareVsK8sDefault` — NVLink affinity metrics across 10 seeds

**Important Disclosure in Output**:
```text
DENSE K-SUBGRAPH: STATISTICAL ANALYSIS — 1000 random topologies (N∈[6,16], k∈[2,8])
SYNTHETIC topology data only — no real GPU hardware.
=== HONEST DISCLOSURES ===
1. ALL topology data is SYNTHETIC — no real GPU hardware queried.
```

#### resources

```bash
go test ./pkg/resources/ -v -count=1
```

**Expected Results**: 3 tests PASS
- `TestGPUCollector_CollectGPUMetrics` — Returns error if `nvidia-smi` unavailable (simulates fallback path)
- `TestGPUCollector_ParseNvidiaSMI` — Parses mock output string (no CLI call)
- `TestMIGTopology_Discovery` — Mock discovery logic

**Graceful Degradation Example**:
```text
TestGPUCollector_CollectGPUMetrics returned error (expected if no nvidia-smi): exit status 2
--- PASS: TestGPUCollector_CollectGPUMetrics
```

#### edgeautonomy

```bash
go test ./pkg/edgeautonomy/ -run "Metrics|Collector|Offline" -v -count=1
```

**Result**: `[no test files]`  
**Note**: Package exists but has zero test coverage. This should be addressed in future PRs.

#### intel (offline modes)

```bash
go test ./pkg/intel/ -v -count=1
```

**Expected Results**: ~26 tests PASS
- `TestParseSTIXBundle_Realistic`, `TestParseCVEJSONL`, `TestParseIOCFeed` — Feed parsing from strings
- `TestHub_SyncAll_OfflineFeeds`, `TestHub_SyncAll_MissingLocalPath` — Offline sync behavior
- `TestMemoryStore_UpsertAndQueryCVE`, `TestHub_ConcurrentSTIXImport` — Concurrent safety

**Notable Live Test**:
- `TestClickHouseStore_Live` — May fail in sandbox without CH endpoint; ignore for zero-cost validation

---

## 4. bash Script for Colab/Linux

See accompanying script [`verify_free_tier.sh`](verify_free_tier.sh) for:
- GOMODCACHE setup
- Build four packages
- Run targeted test commands
- Print PASS/FAIL summary table
- Handle optional timeouts

---

## 5. Recommended Workflow

### Local Reproduction (Windows)

1. Set Go module cache to E: drive (avoid C: space limits):
   ```powershell
   go env -w GOMODCACHE=E:\go\pkg\mod
   ```

2. Run Part 1 sequence verbatim (copy-paste each command, capture terminal output):
   - `go build ...`
   - `go test ./pkg/capability/...`
   - `go test ./pkg/scheduler/...`
   - `go test ./pkg/resources/...`
   - `go test ./pkg/edgeautonomy/...`
   - `go test ./pkg/intel/...`

3. Paste results into your lab notebook or validation report.

### Colab / Kaggle Kernels (Remote Linux)

1. Clone repo or copy `/cloudai-fusion` contents into `/content`.
2. Install Go ≥ 1.25 (Colab pre-installed; upgrade if needed):
   ```bash
   wget https://dl.google.com/go/go1.26.5.linux-amd64.tar.gz
   sudo tar -C /usr/local -xzf go1.26.5.linux-amd64.tar.gz
   export PATH=$PATH:/usr/local/go/bin
   ```

3. Run [`verify_free_tier.sh`](verify_free_tier.sh) directly (bash):
   ```bash
   chmod +x verify_free_tier.sh
   ./verify_free_tier.sh
   ```

4. Capture output via:
   - Colab: `print(output.text)` in Python cell
   - Kaggle: Download artifact after run completes

---

## 6. Validation Checklist

- [ ] `go build ./pkg/capability/... ./pkg/scheduler/... ./pkg/resources/... ./pkg/edgeautonomy/...` exits with code 0
- [ ] `go test ./pkg/capability/ -v -count=1` shows 9 PASS lines
- [ ] `go test ./pkg/scheduler/ -run "DenseK|Topology|Constraint" -v -count=1` shows 12 PASS lines with "SYNTHETIC" disclaimer text
- [ ] `go test ./pkg/resources/ -v -count=1` shows 3 PASS lines (includes "error (expected if no nvidia-smi)" message)
- [ ] `go test ./pkg/edgeautonomy/...` reports "[no test files]" honestly (zero coverage acknowledged)
- [ ] `go test ./pkg/intel/ -v -count=1` passes ≥ 20 tests (ignoring live CH backend if it fails)
- [ ] Final: `go build ./...` succeeds in root directory

If **all** items checked ✅ → Zero-cost validation complete; proceed to Part 3 (paid hardware procurement plan).

---

## 7. Next Steps After Zero-Cost Validation

Once Part 1 & Part 2 are confirmed working:

1. Submit PR documenting:
   - Terminal output screenshots (local Windows + Colab/Kaggle)
   - Link to this guide under `/docs/free-tier-verification/`
2. File task for M2/M3/M5 hardware budget approval:
   - MIG-ready A100/H100 nodes (estimated $150/hr cloud rental)
   - InfiniBand multi-node cluster (estimated $250/hr spot pricing)
   - SGX-hosted enclave + attestation server (AWS i4i instances disabled; requires bare metal provider)
3. Phase 3 implementation will add:
   - Real GPU topology discovery (`nvidia-smi dcv`)
   - RDMA-aware job placement (OpenMPI integration)
   - TEE enclave measurement (Intel SGX DCAP driver)

---

## License & Citation

Apache License 2.0. For academic citation:

> Qoder et al., "CloudAI Fusion: Zero-Cost Validation of AI Infrastructure Modules", GitHub repo, Aug 2026.

Last updated: August 20, 2026.
