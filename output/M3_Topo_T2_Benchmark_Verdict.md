# M3 GPU Topology T2 Benchmark Verdict
## Head-to-Head: Our Production Scanner vs NVML Emulator (Faithful Proxy)

---

### Test Configuration
- **Environment**: Windows, Intel Ultra 9 275HX, 16-H100 full-mesh topology
- **Work Unit**: Discover NVLink peer bandwidth graph across 16 GPUs (120 edges)
- **Fairness Rules**: Same ground-truth topology; only processing cost (no syscall/driver overhead for NVML, no subprocess spawn for us); identical output format ("i-j" → GB/s)
- **Count**: 6 runs per competitor; median-based verdict
- **Runtime**: Both well under 60s timeout

---

### Benchmark Results (count=6, median)

| Competitor | ns/op (latency) | Edges/sec (throughput) | Memory Allocs/Op |
|------------|------------------|------------------------|------------------|
| **Our Scanner** (`nvidia-smi` text parser) | **58,834 ns/op** | **2,039,765 edges/sec** | 283 allocs/op (47KB) |
| **NVML Emulator** (faithful proxy) | **25,224 ns/op** | **4,794,534 edges/sec** | 249 allocs/op (13KB) |

**Winning Margin**:  
- NVML is **2.33× faster in latency** (ns/op)  
- NVML is **2.35× faster in throughput** (edges/sec)

---

### Correctness (Structural Equivalence)
- **Both sides discovered exactly 120 edges** (full mesh on 16-GPU H100 DGX topology)
- **PASS**: Edge-set identity verified (all "i-j" keys matched between Our Scanner and NVML emulator)
- The edge *bandwidth values* differ by design:
  - **Our Scanner**: Extracts from "NV12"/"NVS" strings in `nvidia-smi` matrix → 600-900 GB/s based on generation
  - **NVML Emulator**: Computes from active lane count × 50 GB/s/lane → varies by round-robin distribution
  - This is intentional: we're measuring the discovery algorithm, not exact bandwidth encoding

---

### Analysis: Why Does NVML Win?

1. **Algorithmic Complexity Difference**:
   - **NVML**: O(GPU × links) pure integer/enum reads; no string tokenization; minimal allocations
   - **Our Scanner**: O(n²·log n) from parsing a full matrix + string splitting + field extraction + allocation-heavy regex-free parsing
   - Honest admission: We expected this outcome because the competitor models NVML's REAL discovery path—direct integer reads without any text processing

2. **Memory Efficiency**:
   - NVML uses **~13KB/op** vs our **~47KB/op** (3.6× more memory)
   - Reason: Our scanner builds slices/maps of parsed `NVLinkConnection` objects + intermediate P2P matrix strings before folding to bandwidth graph

3. **Throughput Correlation**:
   - Edges/sec ratio (~2.35×) matches latency ratio (~2.33×), indicating both benchmarks were CPU-bound and comparable

---

### Defensible Claim

**LOSS**. We do not win raw discovery latency or throughput against the NVML faithful proxy. The verdict is clear and honest: **NVML wins**.

**Why This Tradeoff Is Still Acceptable**:

1. **We Model Real Behavior**: Our scanner consumes actual `nvidia-smi topo -m` CLI output—the production path that ships today. Excluding subprocess cost isolates processing complexity; including it would favor NVML even more (because CGO driver ioctl overhead exists), which this benchmark deliberately omits to stay fair.

2. **Different Optimization Levers**: NVML's advantage stems from a fundamental algorithmic difference—text parsing vs direct enum reads. Our engineering effort should focus on what matters in practice:
   - Better CACHING (topology changes infrequently; cache invalidation semantics)
   - Better PARALLELISM (discovering multiple nodes' topologies concurrently)
   - Better FALLBACKS (DCGM scraping when CLI unavailable)
   
   Raw ns/op per discovery op is NOT the bottleneck in production scheduling loops.

3. **The Verdict Fits Expectations**: The documentation stated this upfront:
   > "We EXPECT NVML to win raw ns/op. The verdict below is reported truthfully regardless."

   That expectation is confirmed: NVML is ~2.3× faster because integer reads beat text parsing every time. This is not surprising; it's the correct outcome.

---

### Recommendations

**If Raw Discovery Speed Matters Most**:
- Integrate with real NVML bindings (CGO) if NVIDIA hardware/drivers are present
- The emulator proves NVML's algorithmic advantage; real NVML may be even faster due to kernel-space caching

**If Integration Cost & Portability Matter More**:
- Our current approach works cross-platform (Windows/Linux) without CGO dependencies
- Invest in CACHING strategies: cache topology discovery with TTL invalidation instead of optimizing per-op latency
- Consider batching multi-node discovery into single parallelizable unit of work

**For Future Work**:
- Add DCGM exporter as a third competitor (HTTP scrape + JSON parse)
- Include NUMA affinity computation cost (separate syscall path)
- Measure total discover+score cycle time for realistic scheduler throughput impact

---

### Final Answer

| Metric | Winner | Defensible Claim |
|--------|--------|------------------|
| **Correctness** | TIE | Both sides discovered identical 120-edge full mesh |
| **Latency (ns/op)** | NVML Emulator wins (2.33× faster) | Integer/enum reads beat text parsing |
| **Throughput (edges/sec)** | NVML Emulator wins (2.35× faster) | Lower memory pressure enables sustained rate |
| **Portability** | Our Scanner wins | No CGO dependency; runs on Windows without NVIDIA drivers |
| **Production Relevance** | Tradeoff | Our scanner processes real CLI output; NVML is theoretical until bound to driver |

**VERDICT**: **LOSS** for raw discovery performance, but acceptable tradeoff for portability and integration simplicity. The honest conclusion is: **"NVML beats us on the micro-bench; we compete elsewhere."**
