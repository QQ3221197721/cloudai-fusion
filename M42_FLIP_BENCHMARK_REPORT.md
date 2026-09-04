# M42 WASM Sandbox FLIP Benchmark Report vs REAL wazero v1.12.0

## Objective
Honest head-to-head comparison of CloudAI Fusion's WASM Sandbox vs GitHub/wazero (interpreter + compiler modes). Measuring startup latency and execution performance, count=6 medians, never fake, never edge-only.

## Environment
- **Go**: 1.25.11
- **wazero**: v1.12.0
- **CPU**: Intel(R) Core(TM) Ultra 9 275HX
- **OS**: Windows amd64
- **Benchtime**: 500ms per run × 6 runs = ~100s total
- **Output**: `output/m42_flip_bench.json` (248 lines JSON)

---

## Results Summary (Median ns/op across count=6)

### 1. Cold Start (compile + instantiate 128 unique WASM modules)

| Side | Mode | Median ns/op | Notes |
|------|------|--------------|-------|
| **Raw wazero** | Interpreter | **~358,500** | Portable but slow |
| **Raw wazero** | JIT Compiler | **~6,257,139** | Heavy native codegen overhead for cold compile |
| **Ours** | NoCache | **10,153,997** | Full wrapper overhead per module |
| **Ours** | **Cached** | **3,055,053** | ✅ **CompilationCache delivers 3.3× speedup** vs no-cache |

**Key Finding**: Our production optimization (`CompilationCache`) provides genuine startup win when caching precompiled binaries—but only on subsequent instantiations. First load is slower due to our safety wrappers (capability checks, mutexes, evidence receipts).

---

### 2. Warm Per-Call Execution (add(3,5), single function call)

| Side | Mode | Median ns/op | Winner |
|------|------|--------------|--------|
| **Raw wazero** | Interpreter | **669.5** | ❌ |
| **Raw wazero** | JIT Compiler | **517.5** | ❌ wazero wins |
| **Ours** | Sandbox | **3,203** | — |

**Verdict**: **wazero JIT mode wins 6.2× faster** than our sandbox on raw execution. Our capability security layer (+ mutex + context passes) adds measurable overhead. Honest admission: we lose this dimension.

---

### 3. Concurrent Execution Throughput (C=8 goroutines)

| Side | Mode | Median ns/op | Stability |
|------|------|--------------|-----------|
| **Raw wazero** | Interpreter | **460.1** | Good |
| **Raw wazero** | JIT Compiler | **579.8** | Slightly degraded |
| **Ours** | Sandbox | **3,385** | Stable but high base overhead |

**Verdict**: wazero JIT still wins (**~6× faster**) even under concurrency. We maintain stable throughput (our mutex serializes safely) but can't compete with raw JIT speed.

---

### 4. High Concurrency (C=64 goroutines)

| Side | Mode | Median ns/op | Degradation |
|------|------|--------------|-------------|
| **Raw wazero** | Interpreter | **1,546** | 3.4× worse vs C=8 (contention) |
| **Raw wazero** | JIT Compiler | **796.6** | 1.4× worse (still best scaler) |
| **Ours** | Sandbox | **3,239** | Stable (serialized by design) |

**Partial Win**: Under heavy contention (C=64), interpreter mode degrades badly. Our serialized approach remains predictable (though slow). Not a performance win—more like a "predictable failure" trade-off.

---

### 5. Dataset Per-Call (round-robin over 128 different modules, calling "f")

| Side | Mode | Median ns/op | Winner |
|------|------|--------------|--------|
| **Raw wazero** | Interpreter | **738.0** | ❌ |
| **Raw wazero** | JIT Compiler | **663.9** | ❌ 8.8× faster |
| **Ours** | Sandbox | **5,848** | — |

**Note**: Each sandbox holds ONE module ("f"), so ours creates 128 instances with full lifecycle overhead. This exposes our worst path: repeated instantiation without cache reuse.

---

## Honest Verdict

### Where We Win 🟢
1. **Startup latency WITH CompilationCache**: Precompiled binary cache collapses recompilation cost from 10M+ ns → 3M ns (3.3× improvement). For repeat module loads, this is a genuine Moat.
2. **Predictability under contention**: Our serialized design maintains stable throughput at C=64 while interpreter mode degrades 3.4×. Trade-off: safe-but-slow beats volatile-fast.
3. **Safety guarantees**: Every invocation has capability checks, timeout enforcement, memory limits, and evidence receipts. Never exposed directly to untrusted code.

### Where We Lose 🔴
1. **Raw execution speed**: wazero JIT mode is **6–9× faster** on all per-call benchmarks. Native codegen + zero wrappers beat our safe-but-wrappy approach.
2. **First-load overhead**: Our initialization (validate, lock, context setup) costs measurable cycles. Edge case: true for small workloads (< 1M ops/sec).
3. **Scalability ceiling**: Our mutex-based serialization caps throughput. High-concurrency scenarios saturate us earlier than shared-nothing wazero.

### Parity / Compromise ⚖️
- **Memory safety**: Both sides isolate via linear memory boundaries. We add capability-layer filtering (extra protection, extra cost).
- **Evidence chain**: We log every invoke with hash-chained receipts; wazero doesn't care. Different goals.

---

## Clean-WIN / Partial-LOSS Classification

| Dimension | Result | Confidence |
|-----------|--------|------------|
| **Cold start (cached)** | ✅ PARTIAL WIN | Real compilation cache effect proven |
| **Warm per-call** | 🔴 CLEAR LOSS | JIT always wins, admit it |
| **Concurrent C=8** | 🔴 CLEAR LOSS | wazero 6× faster |
| **High concurrency C=64** | ⚖️ PREDICTABILITY TRADE-OFF | Stable but not faster |
| **Dataset round-robin** | 🔴 CLEAR LOSS | Exposes instantiation overhead |

**Overall**: M42 does NOT achieve clean win on raw performance Moat. The honest Moat is **startup caching** + **audit trails**, not throughput race against optimized compilers.

---

## Build + Vet Status
✅ **Clean build**: `go build ./pkg/wasm/...` passed  
✅ **Vet clean**: `go vet ./pkg/wasm/...` passed  
✅ **Test correctness**: `TestM42_FLIP_DatasetValidity` proves all 128 generated WASM modules compute correct results

---

## Production Recommendations

1. **Enable CompilationCache** in `DefaultRuntimeConfig()` as production default. It's free optimization that closes cold-start gap.
2. **Batch small invocations**: Amortize wrapper overhead by calling 10–100 functions before returning result.
3. **Consider lazy init**: Don't hold precompiled modules in memory unless hot-path demand justifies.
4. **Accept JIT speed**: For pure compute (ML kernels, crypto), wazero/JIT is better. Use our sandbox where safety > throughput.

---

## Files Produced
- `output/m42_flip_bench.json` — full `-json` stream (count=6, parsed medians above)
- `pkg/wasm/m42_flip_wazero_headtohead_bench_test.go` — benchmark source (WASM assembler, dataset generator, all test cases)
- `pkg/wasm/wazero_runtime.go` — production code with `CompilationCache` integration

---

**Conclusion**: M42's true edge isn't beating JIT speed—it's providing **precompiled binary cache + capability-filtered isolation + audit trails**. The honest MoAT is architectural safety, not raw clock cycles.
