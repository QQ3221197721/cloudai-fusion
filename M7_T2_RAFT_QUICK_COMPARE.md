# M7 Raft T2 Benchmark - Quick Comparison Table

## Commit Latency (ns/op) - Count=6 Median

```
┌─────────────────────────┬───────────────┬────────────────┬─────────────┐
│ Metric                  │ Raw Hashi     │ RealRaftNode   │ Ratio       │
├─────────────────────────┼───────────────┼────────────────┼─────────────┤
│ Median latency          │ 5,458 ns      │ 25,543 ns      │ ~4.7x SLOWER│
│ Min latency             │ 5,346 ns      │ 25,182 ns      │ ~4.7x       │
│ Max latency             │ 5,629 ns      │ 26,440 ns      │ ~4.7x       │
│ Std dev                 │ ~111 ns       │ ~505 ns        │ ~4.5x       │
└─────────────────────────┴───────────────┴────────────────┴─────────────┘
```

## Throughput (entries/sec) - Count=6 Median

```
┌─────────────────────────┬─────────────────┬──────────────────┬──────────┐
│ Metric                  │ Raw Hashi       │ RealRaftNode     │ Delta    │
├─────────────────────────┼─────────────────┼──────────────────┼──────────┤
│ Median throughput       │ 184,716/s       │ 39,246/s         │ ~4.7x    │
│ Min throughput          │ 177,680/s       │ 37,821/s         │ ~4.7x    │
│ Max throughput          │ 187,088/s       │ 39,716/s         │ ~4.7x    │
│ Std dev                 │ ~3,600/s        │ ~700/s           │ ~5.1x    │
└─────────────────────────┴─────────────────┴──────────────────┴──────────┘
```

## Memory & Allocations

```
┌─────────────────────────┬───────────────┬────────────────┬────────────┐
│ Metric                  │ Raw Hashi     │ RealRaftNode   │ Overhead   │
├─────────────────────────┼───────────────┼────────────────┼────────────┤
│ Bytes per op            │ 1,603 B       │ 5,326 B        │ +3.3x      │
│ Allocations per op      │ 24 allocs     │ 65 allocs      │ +2.7x      │
│ Evidence layer overhead │ baseline      │ ~3,723 B       │ Ed25519    │
└─────────────────────────┴───────────────┴────────────────┴────────────┘
```

## Definitive Verdict

### ❌ LOSS on Raw Speed

| Measure | Result | Interpretation |
|---------|--------|----------------|
| **Latency** | 25,543 / 5,458 = **~4.7x slower** | Our verifiable path costs ~20 microseconds per commit |
| **Throughput** | 39,246 / 184,716 = **~4.7x lower** | Trading speed for provable consensus |
| **Memory** | 5,326 B / 1,603 B = **~3.3x more** | Evidence receipts stored in-memory |
| **Allocations** | 65 / 24 = **~2.7x more** | JSON marshaling + struct copies |

### ✅ WIN on Trustworthiness

| Capability | RealRaftNode | Raw Hashi | Why It Matters |
|------------|--------------|-----------|----------------|
| **Signed commits** | ✅ Yes | ❌ No | Detect log tampering, replay attacks |
| **Hash-chained proof** | ✅ Yes | ❌ No | Cryptographic non-repudiation |
| **Leadership audit trail** | ✅ Yes | ❌ No | Prove who was leader when |
| **Same consensus engine** | ✅ hashicorp/raft v1.6.1 | ✅ Same | Production-grade reliability |
| **Zero election overhead** | ✅ Verified | N/A | Evidence NOT on election path |

---

## Bottom Line (Plain English)

**Q**: Should we claim "RealRaftNode is faster than raw hashicorp/raft"?

**A**: **NO**. We measured ~4.7x **slower**, not faster. That's an honest **LOSS** verdict.

**Q**: Then why use RealRaftNode?

**A**: Because **some problems need provable consensus**. If you're building:
- ✅ **Security-critical systems** where audit trails matter → **USE RealRaftNode**
- ✅ **Financial ledgers** requiring cryptographic receipts → **USE RealRaftNode**  
- ✅ **Compliance workloads** needing non-repudiation → **USE RealRaftNode**

But if you're building:
- ❌ **High-frequency trading logs** at 1M+/sec → **DON'T USE RealRaftNode**
- ❌ **Pure throughput optimization** → **USE RAW HASHI**
- ❌ **Internal service mesh** without trust requirements → **USE RAW HASHI**

---

## One-Liner Claim (Defensible)

> "**RealRaftNode achieves ~39K entries/sec with 25.5µs latency while providing cryptographically signed, hash-chained proofs of every consensus commit — trading ~4.7x raw throughput for verifiability that raw hashicorp/raft cannot provide.**"

---

*Generated Tuesday, August 25, 2026 11:41 AM PST • Anti-Fiasco Rules Compliant • Truth Over Hype*
