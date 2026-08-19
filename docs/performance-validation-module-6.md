# Module 6 Event Fabric — Performance Validation

> Roadmap Top-10 #10. Honest throughput positioning of the in-process Event
> Message Fabric (`pkg/eventbus`) against NATS and Kafka. The goal is **not**
> "we beat distributed MQs on raw throughput" — it is to state, with reproducible
> local numbers and cited public numbers, where we sit and what our real
> differentiator is: **cryptographic evidence attached to every routed event.**

Reproduce (PowerShell, `&&` disabled — use `;`):

```powershell
go env -w GOMODCACHE=E:\go\pkg\mod
cd d:\IdeaProjects\untitled\cloudai-fusion; go build ./pkg/eventbus/...
cd d:\IdeaProjects\untitled\cloudai-fusion; go test ./pkg/eventbus/... -run=^$ -bench=BenchmarkFabric -benchmem -count=3
```

---

## (a) Local benchmark numbers (re-run on this machine)

- **Machine:** Intel(R) Core(TM) Ultra 9 275HX, `goos=windows`, `goarch=amd64`, Go toolchain from the workspace.
- **Date:** 2026-08-17
- **Command:** `go test ./pkg/eventbus/... -run=^$ -bench=BenchmarkFabric -benchmem -count=3`
- **Source:** [`pkg/eventbus/fabric_bench_test.go`](../pkg/eventbus/fabric_bench_test.go), [`pkg/eventbus/fabric.go`](../pkg/eventbus/fabric.go)

Raw output (3 iterations each):

```
BenchmarkFabric_Forward-24                 2003875   559.4 ns/op   1787503 events/sec   544 B/op    6 allocs/op
BenchmarkFabric_Forward-24                 1975858   659.3 ns/op   1516687 events/sec   544 B/op    6 allocs/op
BenchmarkFabric_Forward-24                 1412238   867.6 ns/op   1152592 events/sec   544 B/op    6 allocs/op
BenchmarkFabric_RouteEvent_NoEvidence-24  24156972    44.08 ns/op 22687933 events/sec     0 B/op    0 allocs/op
BenchmarkFabric_RouteEvent_NoEvidence-24  22401768    45.71 ns/op 21875379 events/sec     0 B/op    0 allocs/op
BenchmarkFabric_RouteEvent_NoEvidence-24  29775639    45.65 ns/op 21904487 events/sec     0 B/op    0 allocs/op
BenchmarkFabric_RouteEvent_WithEvidence-24   32055 35217   ns/op    28396 events/sec   777 B/op   11 allocs/op
BenchmarkFabric_RouteEvent_WithEvidence-24   31600 38163   ns/op    26203 events/sec   777 B/op   11 allocs/op
BenchmarkFabric_RouteEvent_WithEvidence-24   31114 39173   ns/op    25528 events/sec   778 B/op   11 allocs/op
```

Summary — **what each benchmark actually measures matters** (this is the crux of an honest comparison):

| Benchmark | What it measures | ns/op | allocs/op | Throughput (events/sec) |
|---|---|---|---|---|
| `Fabric_RouteEvent_NoEvidence` | Routing **decision** only: well parse + hop-cap check + terminal-hop L8 branch. **No fan-out `Publish`.** | 44–46 ns | **0** (zero-alloc) | ~22M |
| `Fabric_Forward` | Real single-hop forward: derive new event (metadata map + struct) + `bus.Publish`. This is an actual in-process message emission. | 559–868 ns | 6 (544 B) | ~1.1M–1.8M |
| `Fabric_RouteEvent_WithEvidence` | Full consume path **with Ed25519 receipt signing** into the hash-chained ledger. | 35–39 µs | 11 (777 B) | ~26K–28K |

## (b) Reproducibility vs Maria's numbers

| Path | Maria's reported | This machine (3-run) | Verdict |
|---|---|---|---|
| Pure routing (zero-alloc) | ~50M events/sec, ~20 ns/op, 0 alloc | ~22M events/sec, 44–46 ns/op, **0 alloc** | Zero-alloc **reproduced**; ns/op ~2.2× higher, throughput ~22M not 50M — **different, reported honestly** (hardware/measurement variance) |
| With Ed25519 evidence | ~40K events/sec, ~17–40 µs/op* | ~26–28K events/sec, 35–39 µs/op, 11 alloc | Same **order of magnitude reproduced**; slightly below 40K, µs/op within range |

\* The brief quoted "17–40ms/op" for the evidence path; at ~40K events/sec the per-op cost is ~25 µs, so this is read as a **µs** typo. Our measured 35–39 µs/op is consistent with that reading.

**Honest note on the two discrepancies:**
- **The zero-allocation property is confirmed** on the pure-routing path (0 B/op, 0 allocs/op across all 3 runs). The absolute ns/op differs (45 ns here vs Maria's 20 ns). Benchmark ns/op is the reliable figure; the derived `events/sec` metric is `1/ns` and inherits the same variance. This is most likely a different CPU / power/thermal state, not a code change. Reporting the new number as-is per the honesty rule.
- The signed path lands at ~26–28K/sec here rather than 40K. Both confirm the same conclusion: the Ed25519 signature dominates and the signed path is **~800× slower** than the unsigned routing decision. That gap is the entire point of section (d).

---

## (c) Comparison table — ours vs NATS vs Kafka

**Read the "What is measured / preconditions" column before the number.** These systems are not measured the same way, and the table says so on every row.

| System / mode | Throughput | What is measured / preconditions | Source |
|---|---|---|---|
| **Ours — routing decision** (`RouteEvent_NoEvidence`) | ~22M events/sec (45 ns/op, 0 alloc) | **In-process CPU micro-op**: routing decision only, no serialization, no network, no persistence, **no fan-out publish**. Not an end-to-end message. | Local bench, this doc §(a) |
| **Ours — in-process publish** (`Fabric_Forward`) | ~1.1M–1.8M events/sec | **In-process, single machine**: derive event + publish into the in-memory bus (buffered channel). No network, no persistence, no durability. This is the honest "real message emitted" number. | Local bench, this doc §(a) |
| **Ours — evidence-signed consume** (`RouteEvent_WithEvidence`) | ~26K–28K events/sec (35–39 µs/op) | In-process + **Ed25519 signature per event** into an offline-verifiable hash-chained receipt ledger. No network. | Local bench, this doc §(a) |
| **NATS core** (in-memory, at-most-once) | "millions to tens of millions msg/sec per node, microsecond latency" (vendor-claimed ceiling) | **Client↔server over the network**, fire-and-forget, **no persistence**, no delivery guarantee. Closest architectural analog to our in-process publish, but includes a network hop. | NATS is documented as at-most-once/no-persistence in core mode [1]; third-party summary of the "tens of millions msg/sec per node" claim [2] |
| **NATS JetStream** (persistent) | ~200K–400K msg/sec | 4 vCPU / 8 GB VPS, NVMe, **with persistence** (durable stream) | onidel 2025 benchmark [3] |
| **Apache Kafka** (canonical) | ~2M writes/sec | **3-broker cluster** ("three cheap machines"), batched, replicated, persisted to disk (distributed commit log) | LinkedIn/Confluent "2 Million Writes Per Second" [4][5] |
| **Apache Kafka** (VPS, batched) | ~500K–1M+ msg/sec | 4 vCPU / 8 GB VPS, **batching enabled**, persisted | onidel 2025 benchmark [3] |

**Sources:**
- [1] NATS core semantics (at-most-once, no persistence): https://docs.nats.io/concepts/what-is-nats
- [2] Third-party summary of NATS's per-node throughput claim: https://robustmq.com/en/Blogs/38
- [3] onidel, *NATS JetStream vs RabbitMQ vs Apache Kafka on VPS in 2025* (4 vCPU/8 GB VPS benchmark): https://onidel.com/blog/nats-jetstream-rabbitmq-kafka-2025-benchmarks
- [4] LinkedIn Engineering, *Benchmarking Apache Kafka: 2 Million Writes Per Second (On Three Cheap Machines)*: https://www.linkedin.com/blog/engineering/open-source/benchmarking-apache-kafka-2-million-writes-second-three-cheap-machines
- [5] Confluent, *Apache Kafka Performance*: https://developer.confluent.io/learn/kafka-performance/

> **Why the ~22M number is NOT quoted as "we beat NATS":** it measures a routing
> decision with no publish, no serialization, and no network. The honest
> like-for-comparison to NATS core is our in-process publish path (~1.1M–1.8M/sec),
> and even that has **no network hop and no persistence** while NATS core is
> measured client↔server. Comparing a process-internal function call to a
> networked broker is not apples-to-apples; the preconditions column exists so no
> reader is misled.

---

## (d) The two modes and where each fits

The fabric has two operating modes, and they target different problems:

**1. Pure in-memory routing** (evidence disabled — `SetEvidence(nil)`, the default)
- Throughput: routing decision ~22M/sec (zero-alloc); real in-process publish ~1.1M–1.8M/sec.
- **Fits:** low-latency, high-fan-out event distribution *within a single process* — the 16-well AISecOps signal fabric where events hop between wells with a hard 8-hop cap. This is the mode that is architecturally comparable to **NATS core in-memory** (fire-and-forget, no persistence), with the caveat that ours is in-process and NATS core is networked.
- **Does not provide:** cross-host delivery, durability, or replay. If you need those, you use NATS/Kafka underneath — the fabric is designed to sit *on top of* a real bus (see [`pkg/eventbus/nats.go`](../pkg/eventbus/nats.go)), not to replace it.

**2. Evidence-signed routing** (`SetEvidence(builder)`)
- Throughput: ~26K–28K/sec, bounded by Ed25519 signing cost (35–39 µs/op).
- **Fits:** compliance / non-repudiation scenarios where every consumed event must carry a cryptographic, offline-verifiable receipt in a hash-chained ledger ([`fabric.go` `signConsumed`](../pkg/eventbus/fabric.go)). This is the differentiator: **NATS and Kafka do not natively emit a per-message cryptographic evidence chain.**
- **Ceiling is a design trade-off, not a defect:** you are paying one Ed25519 signature per event to buy non-repudiable audit. 100K/sec is not reachable on this path because a single signature costs tens of microseconds; that is a property of the cryptography, not of the routing code.

---

## (e) Honest architecture positioning & differentiated value

**Architecture positioning (the disclosure):**
- Ours is an **in-process event message fabric** (a routing layer inside one Go process over an in-memory or NATS-backed bus). NATS and Kafka are **distributed message middleware** with client/server networking, clustering, persistence, and replication.
- These are **different architectural tiers.** A raw throughput number from a process-internal function call cannot be fairly compared to a networked, persisted, replicated broker. Every number in §(c) is annotated with its preconditions (single machine vs cluster, network vs in-process, persistence vs none) precisely so the comparison is not weaponized into a false claim.
- Our high pure-routing figure comes **entirely from the absence of network and persistence.** State that plainly: remove those two costs from any MQ and its in-memory hot path also jumps by orders of magnitude (that is exactly why NATS core is "tens of millions/sec" and JetStream is "hundreds of thousands/sec").
- The evidence path at ~26–28K/sec is the **inherent cost of one Ed25519 signature per event** — a security/performance trade-off we accept deliberately.

**Differentiated value (the conclusion):**
> The Event Fabric's defensible value is **not** out-throughputing distributed
> MQs. It is **"routing with a cryptographic evidence chain attached to every
> consumed event."** NATS and Kafka move messages faster across a network and
> persist them durably — that is their job, and we can run on top of them. What
> they do **not** do out of the box is produce a signed, hash-chained,
> offline-verifiable receipt for every routed event, bounded hop propagation with
> guaranteed L8 SOAR consumption at the terminal hop, and honest
> real-vs-simulated backend capability reporting. That evidence-native routing —
> paid for at ~26–28K events/sec — is the moat, and it is honestly a throughput
> trade-off, not a throughput win.

---

## Acceptance checklist

- [x] Re-ran the 3 existing benchmarks locally with `-count=3`; numbers reported verbatim in §(a).
- [x] Reproducibility vs Maria stated honestly in §(b): zero-alloc confirmed; absolute ns/op and signed-path throughput differ and are reported as-is (not massaged to match).
- [x] Comparison table with **every** number annotated by source link and preconditions (§c). NATS/Kafka numbers cited from public sources only — none from memory.
- [x] Two modes and their fit documented (§d).
- [x] Architecture-difference disclosure and differentiated-value conclusion (§e): value is evidence-native routing, not raw throughput dominance.
