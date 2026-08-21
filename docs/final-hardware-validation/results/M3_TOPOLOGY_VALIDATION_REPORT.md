# M3 — GPU Topology-Aware Scheduling — Hardware Validation Report

**Module:** M3 (GPU-aware Kubernetes Abstraction Layer — Topology-Aware Scheduling)
**Date:** 2026-08-21
**Validator instance:** Alibaba Cloud `gn7e-c16g1.4xlarge` (single NVIDIA A100-SXM4-80GB)
**InstanceId:** `i-bp16z4clnn8maewmee04` @ cn-hangzhou, IP `47.110.70.231`
**Driver / CUDA:** 610.57.04 / CUDA 12.4, Go 1.25.9 linux/amd64

> **HONESTY BANNER.** This validation ran on a **single** A100. A single GPU **cannot**
> exercise multi-GPU NVLink/NVSwitch topology — that requires `gn7e-c16g1.32xlarge`
> (8× A100), which was **sold out** at validation time. Everything below is explicitly
> tagged as **hardware-confirmed**, **simulated**, or **needs-8xGPU**. No multi-GPU
> NVLink number in this report was measured on real hardware; those remain
> synthetic/model-based and are flagged accordingly.

---

## 1. Real Hardware Topology (collected via `nvidia-smi`)

### 1.1 `nvidia-smi topo -m`
```
        GPU0    CPU Affinity    NUMA Affinity   GPU NUMA ID
GPU0     X      0-15            0               N/A
```
Single GPU, so the connection matrix has only the self (`X`) cell. No GPU-GPU edges exist to measure.

### 1.2 PCI / NUMA / PCIe generation
| Property | Value | Source |
|---|---|---|
| PCI bus id | `00000000:00:07.0` | `nvidia-smi --query-gpu=pci.bus_id,pci.domain,pci.device` |
| PCI domain / device | `0x0000` / `0x07` | same |
| CPU affinity | cores `0-15` | `topo -m` |
| NUMA affinity | node `0` | `topo -m` |
| NUMA nodes (host) | `1` (node0 CPUs 0-15) | `lscpu \| grep -i numa` |
| PCIe link gen | current **4** / max **4** (Gen4) | `pcie.link.gen.current/max` |
| PCIe link width | current **16** / max **16** (x16) | `pcie.link.width.current/max` |
| sysfs `numa_node` | `-1` | `/sys/class/pci_bus/*/device/numa_node` (VM abstraction) |
| MIG mode | **Disabled** | `mig.mode.current` (cleaned up after M2 Task 216) |

### 1.3 NVLink status
```
GPU 0: NVIDIA A100-SXM4-80GB (UUID: GPU-d4f8358e-f172-883e-4a3c-276d913c8115)
Device does not have or support Nvlink
```
On this single-GPU **VM passthrough**, no NVLink peer is exposed. The A100-SXM4 silicon
physically carries 12 NVLink-3 lanes (600 GB/s aggregate), but with no peer GPU the
links are inactive and **cannot be benchmarked** here → **needs-8xGPU**.

---

## 2. M3 dense-k-subgraph Benchmark (run on the A100 node, 16 vCPU)

Source: `pkg/scheduler/dense_k_subgraph.go` (668 lines). The dense-k-subgraph (DkS)
problem is NP-hard: GPUs form a weighted undirected graph `G=(V,E,w)`; pick `k` vertices
maximizing intra-subset bandwidth `W(S)`. Solvers: **ExactBB** (branch-and-bound with an
admissible upper-bound prune) and **Greedy2Opt** (greedy seed + 2-opt local search).
Baselines model K8s device-plugin behavior: FirstFit, BinPack (MostAllocated),
K8sDefault (LeastAllocated/spread), Random.

`BenchmarkDenseKDualSocketA100` (ns/op, freshly run on `47.110.70.231`):

| k | exact-bnb | greedy-2opt | binpack | first-fit | k8s-default | random |
|---|---|---|---|---|---|---|
| 2 | 3053 | 1625 | 267.5 | 116.9 | 270.5 | 216.3 |
| 3 | 4941 | 2415 | 271.8 | 122.2 | 276.8 | 239.1 |
| 4 | 6198 | 3182 | 278.6 | 127.8 | 284.5 | 248.5 |
| 5 | 13920 | 3821 | 289.6 | 139.5 | 297.6 | 271.6 |
| 6 | 10408 | 4291 | 295.5 | 145.7 | 307.9 | 291.6 |
| 7 | 4684 | 4517 | 307.5 | 156.6 | 322.2 | 319.3 |
| 8 | 1426 | 4292 | 316.1 | 166.0 | — | — |

**Reading the data (honest):**
- **greedy-2opt vs exact-bnb:** at the hardest point (k=5) greedy is **~3.6× faster**
  (3821 vs 13920 ns) while exact returns the provably optimal placement. This is the
  intended trade-off — exact for small k, greedy near-optimal for latency-sensitive paths.
- **exact-bnb is NOT monotone in k** (k=8 = 1426 ns < k=5 = 13920 ns): the admissible
  bound prunes aggressively when k approaches |V|, which is expected B&B behavior.
- The K8s-style baselines (first-fit/binpack) are ~10–100× *faster* in raw ns/op because
  they do **no** topology optimization at all — they treat GPUs as an opaque integer
  count. The point of M3 is **placement quality**, not solver latency (see §2.1).

### 2.1 Placement quality: TopologyAware vs K8s (`TestTopologyAwareVsK8sDefault`)
16 GPUs / 4 NVLink islands, 10 seeds, Welch's t-test (α=0.05). **Run on the A100 node:**

| Metric | vs K8s-BinPack | vs K8s-Spread | Verdict |
|---|---|---|---|
| NVLink Affinity % | 100.0 vs 66.8 (p=0.00700**, d=1.55) | 100.0 vs 64.2 (p=0.00608**, d=1.59) | **WIN** (very large) |
| GPU Utilization % | 28.5 vs 28.5 (p=1.000) | 28.5 vs 28.4 (p=0.940) | DRAW |
| MIG Gini (frag) | 0.2438 vs 0.2438 (p=1.000) | 0.2438 vs 0.2909 (p=0.01485*, d=-1.21) | 1 WIN / 1 DRAW |

**TopologyAware significant wins: 3 / 6 comparisons.** GPU utilization is a genuine DRAW
(topology awareness does not change how many GPUs are used, only *which* ones).

> ⚠️ The 16-GPU / 4-island topology in this test is **MOCK/SYNTHETIC**. The test file
> itself discloses this. It measures placement-decision *quality* of the algorithm, not
> real NVLink bandwidth. Real 8× A100 NVLink measurement is **needs-8xGPU**.

---

## 3. Topology Model vs Real Hardware Consistency

The M3 bandwidth-tier constants in `dense_k_subgraph.go`:

| Tier constant | Model value (GB/s) | Single-A100 verification |
|---|---|---|
| `BandwidthTierNVSwitch` | 900 | **needs-8xGPU** (DGX/HGX NVSwitch fabric) |
| `BandwidthTierNVLink` | 600 | matches A100-SXM4 spec, but **needs-8xGPU** to measure (no peer) |
| `BandwidthTierPCIeSwitch` | 32 (Gen4 x16) | ✅ **hardware-confirmed** — real GPU reports PCIe **Gen4 x16** (`pcie.link.gen=4`, `width=16`) |
| `BandwidthTierCrossSocket` | 16 | plausible (dual-socket UPI); this VM is single-NUMA so not exercised |
| `BandwidthTierCrossNode` | 8 | plausible; single-node VM, not exercised |

- **CPU/NUMA affinity model:** the code's NUMA-affinity weighting is consistent with the
  real `topo -m` output (GPU0 → NUMA node0, CPUs 0-15). The single-NUMA VM means the
  cross-socket penalty path is never triggered here → model is reasonable but only the
  single-node branch is exercised. **partially hardware-confirmed**.
- **PCIe assumption is exactly right:** the 32 GB/s Gen4-x16 tier matches the silicon
  this GPU actually reports. This is the one bandwidth constant we can fully confirm.
- **Everything NVLink/NVSwitch:** matches published A100/H100 specs but is **unverifiable
  on one GPU** → `needs-8xGPU`. The code comment at line 16 already declares the topology
  data synthetic; this report does not overturn that.

---

## 4. M3 Four-Goal Scorecard (honest)

| Goal | Status | Confidence tag | Evidence |
|---|---|---|---|
| **T1 — Developer integration (CLI)** | ✅ **MET** | **hardware-confirmed** (single-GPU) | `cafctl gpu {list,topology,allocate}` in `cmd/cafctl/cmd_gpu.go`. Cross-compiled Linux binary run on the real A100: `gpu list` → `NVIDIA A100-SXM4-80GB / 81920 MB / driver 610.57.04`; `gpu topology` → correct `topo -m` matrix; `--json` valid. **Fixed 2 real bugs found on hardware** (see §5). |
| **T2 — Performance benchmark** | ✅ **MET** (algorithm) | algorithm hardware-run; topology **simulated** | dense-k-subgraph exact+greedy benchmark ran on A100 node (§2). TopologyAware **100%** NVLink affinity vs K8s **66.8%/64.2%**, p<0.01, Cohen's d 1.55/1.59, 3/6 significant wins (§2.1). Topology graph is mock; multi-GPU real NVLink = **needs-8xGPU**. |
| **T3 — Technical barrier** | 🟡 **PARTIAL** | partially hardware-confirmed | DkS NP-hard exact (B&B) + approximate (greedy+2-opt) solver is genuinely original vs K8s' opaque-integer GPU model. PCIe Gen4-x16 tier **hardware-confirmed**; NVLink/NVSwitch tiers spec-correct but **needs-8xGPU**. Barrier is real but its multi-GPU differentiator is unproven on this hardware. |
| **T4 — UX/UI Dashboard** | ❌ **NOT MET** | gap | No M3-specific topology-visualization page under `cloudai-fusion-web/src/pages/`. `gpu/MigMps.tsx` = M2 MIG/MPS config; `gpu/Scheduler.tsx` = M10 evidence-ledger scheduling. No NVLink/PCIe topology graph view exists. |

**Summary: 2 MET (T1, T2) + 1 PARTIAL (T3) + 1 NOT MET (T4).**

---

## 5. Real bugs found & fixed on hardware (`cmd/cafctl/cmd_gpu.go`)

Running the CLI on the real A100 surfaced two invocation bugs that would have silently
failed on any real machine (T1 was "code exists" but broken until this validation):

1. `nvidia-smi topology -g all -t GPU -m -T` → **`ERROR: Option -T is invalid for -m`**.
   Fixed to canonical `nvidia-smi topo -m`; row-count logic switched from `Contains(line,"==")`
   to `HasPrefix(TrimSpace(line),"GPU")`.
2. `nvidia-smi --query-gpu=... -o csv,noheader,nounits` → **`ERROR: Option -o is not recognized`**.
   Fixed to `--format=csv,noheader,nounits` (in both `runGPUList` and `runGPUListJSON`).

Post-fix: `go build ./cmd/cafctl/` clean, `go vet` clean, and all three subcommands
verified working on the live A100 (§4 T1 evidence).

---

## 6. Honest Conclusion — how close is M3 on a single A100?

- **Fully proven on this hardware:** T1 (CLI works end-to-end on the real A100 after 2
  bug fixes) and the PCIe Gen4-x16 bandwidth model constant (32 GB/s).
- **Proven as algorithm, not as hardware topology:** T2 — the dense-k-subgraph solver and
  the statistical placement-quality advantage over K8s baselines are real and reproducible,
  but they run over a **synthetic** 16-GPU/4-island graph.
- **Still missing to reach 4/4:**
  1. **T4 Dashboard** — needs a real M3 NVLink/PCIe topology-visualization page (pure
     software gap, buildable without more GPUs).
  2. **T3/T2 multi-GPU confirmation** — the NVLink (600 GB/s) and NVSwitch (900 GB/s)
     tiers and the multi-GPU affinity advantage require **8× A100 (`gn7e-c16g1.32xlarge`)**,
     which was sold out. This is a hardware-availability gap, not a code gap.

**Bottom line:** on a single A100, M3 reaches **T1 hardware-confirmed + T2 algorithm-confirmed**,
with **T3 partially confirmed** and **T4 an open software gap**. The only thing blocking a
fully hardware-backed T2/T3 is access to a multi-GPU NVLink node.
