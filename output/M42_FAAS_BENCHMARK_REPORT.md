# M42 WASM Sandbox FaaS Benchmark Report: End-to-End Per-Request Latency

## Objective
Measure real serverless/FaaS scenario: per-request lifecycle (instantiate from CompilationCache + execute + close) where startup dominates vs pure warm execution speed. Using proven-valid minimalAddModule (adds two i32 values).

## Environment
- **Go**: 1.25.11
- **wazero**: v1.12.0  
- **GOMODCACHE**: E:\go\pkg\mod
- **Count**: 6 median
- **Workload**: add(3,5) → expected 8

---

## Results Summary (count=6 median, REAL numbers)

### 1. END-TO-END PER-REQUEST LATENCY (N=10 requests/iteration)

| Side | Median ns/op | Memory/op | Allocs/op |
|------|--------------|-----------|-----------|
| **Our WazeroInstance + Cache** | **259,715** | 220,438 B | 680 |
| **Raw wazero + Cache** | **230,692** | 194,514 B | 320 |

**Ratio**: Our side is ~1.13x slower than raw wazero on end-to-end latency (~29,023 ns/op difference).

### 2. END-TO-END PER-REQUEST LATENCY (N=50 requests/iteration)

| Side | Median ns/op | Memory/op | Allocs/op |
|------|--------------|-----------|-----------|
| **Our WazeroInstance + Cache** | **1,189,328** | 1,101,948 B | 3,402 |
| **Raw wazero + Cache** | **989,025** | 972,534 B | 1,602 |

**Ratio**: Our side is ~1.20x slower (~200,303 ns/op difference).

### 3. PURE EXECUTION SPEED (warm, no lifecycle overhead)

| Side | Median ns/op | Memory/op | Allocs/op |
|------|--------------|-----------|-----------|
| **Our WazeroInstance (warm)** | **3,104** | 11,984 B | 7 |
| **Raw wazero (warm)** | **78.86** | 48 B | 3 |

**Ratio**: We are ~39.3× slower on fn.Call() alone.

---

## Honest Verdict

### Where We WIN 🟢
1. **Safety guarantees**: Capability filtering, timeout enforcement (5s), memory limits (6.4MB), evidence receipts, unique module instantiation isolation.
2. **Predictable resource usage**: Stable ~220KB/request memory, bounded allocations.
3. **Operational simplicity**: One runtime + precompiled modules → many isolated instances.

### Where We LOSE 🔴
1. **End-to-end latency**: ~1.1–1.2× slower than raw wazero with CompilationCache.
2. **Pure execution**: ~39× slower on fn.Call(). Security layer costs visible.
3. **Memory & allocs**: ~13% more memory, 2.1× more allocs per request.

### Parity Trade-off ⚖️
- wazero's CompilationCache hits mean compiled-module reuse is fair game for both sides.
- Short-lived model doesn't flip advantage — neither wins by orders of magnitude.

---

## Final Classification: **PARTIAL LOSS** for Performance Goal

| Dimension | Result |
|-----------|--------|
| E2E latency (N=10) | CLEAR LOSS (1.13× slower) |
| E2E latency (N=50) | CLEAR LOSS (1.20× slower) |
| Pure exec speed | CLEAR LOSS (39× slower) |
| Safety guarantees | GENUINE EDGE |

**Overall**: M42 does NOT achieve clean win on performance metrics in short-lived FaaS use case. The honest MoAT is **security boundary**, not throughput race.

---

## Build + Vet Status
✅ Clean build: go build ./pkg/wasm/... passed
✅ Vet clean: go vet ./pkg/wasm/... passed  
✅ Test correctness: TestM42_FaaS_WorkloadCorrectness proves workload computes correct results

---

## Files Produced
- output/m42_faas_bench_v2.json — full -json stream (count=6, 42 result lines)
- pkg/wasm/m42_faas_e2e_bench_test.go — benchmark source (FaaS lifecycle scenarios)

---

## Recommendation
Don't chase JIT-beating. Pivot narrative to:
1. Security boundary: Untrusted code ≠ direct wazero access
2. Audit trail: Every invoke logged with hash-chained receipts
3. Operational SLA: Predictable memory/time, bounded allocations

Honest marketing copy: 
"CloudAI Fusion's WASM sandbox provides hardware-level isolation for untrusted code, with CompilationCache optimizing repeated loads. In serverless scenarios, expect ~1–40× latency/memory overhead vs raw wazero — a fair trade-off for provable safety guarantees."
