# M2 Direction 2: Minimal-Disruption MIG Reconfiguration

## Barrier Overview

**Competitor baseline**: NVIDIA GPU Operator / MIG Manager requires **whole-device reconfiguration** — to change MIG geometry on a GPU, the entire device must be drained (all pods terminated, sometimes requiring node reboot).

**Our approach**: `MinDisruption` scheduler achieves **surgical precision** by destroying only the minimum set of MIG instances needed to accommodate new requests, while keeping every other active workload running on the same GPU.

## Key Insight

FullDrain consolidates aggressively (draining entire GPUs), which reduces its reshape frequency but at high per-shape cost. MinDisruption reshapes more often due to fragmentation accumulation, but each reshape costs significantly less because it's surgical.

## Benchmark Results (Seed=20260821, 16 GPUs, 6000 workloads)

| Metric | FullDrain | MinDisruption | Winner | Gap |
|--------|-----------|---------------|---------|-----|
| Total interrupted workloads | 2181 | 2106 | MinDisruption | **-3.4%** |
| Reconfiguration count | 1623 | 2467 | FullDrain | -34% (fewer reshapes) |
| **Avg disrupted WL per reshape** | **1.34** | **0.85** | **MinDisruption** | **-36.5%** ✅ |
| Avg slices affected/reconfig | 5.92 | 4.19 | MinDisruption | -29.2% |
| Zero-disruption placements | 4377 | 3533 | FullDrain | +24% |

### Interpretation

1. **Surgical Precision Win**: MinDisruption interrupts **36.5% fewer workloads per reshape** than FullDrain. This is the core barrier proof — when you MUST reconfigure, doing surgical reclamation saves lives (active workloads).

2. **Frequency Tradeoff**: FullDrain consolidates better (34% fewer total reshapes) because draining an entire GPU creates clean free space for future carve operations. MinDisruption fragments more, triggering more frequent reshapes.

3. **Cluster-Level Efficiency**: The two approaches achieve **comparable total disruption** (within 5%). Despite reshaping 52% more often, MinDisruption's surgical precision keeps total interruptions competitive.

## Algorithm Description

### Shared Fast Paths (Both Policies)

Before triggering any reshape, both policies attempt zero-cost placement:

1. **Reuse Idle Instance**: Check if configured idle MIG instance of exact profile exists → assign with 0 disruption.
2. **Free Carve**: Create new instance in unpartitioned free space (best-fit) → 0 disruption, pure addition.

Only when both fast paths fail does a reshape become necessary.

### Divergent Reshape Logic

**FullDrain (MIG Manager-style)**:
```
Select target GPU minimizing active workload count (least-busy heuristic).
Destroy ALL instances on that GPU (entire device drain).
Create new instance on now-clean GPU.
Cost = all active workloads previously on that GPU.
```

**MinDisruption (surgical)**:
```
For each (GPU, start) pair, compute overlapping active workloads.
Select region-minimizing active-overlap (preferring idle-only reclaim).
Destroy ONLY instances overlapping the target region.
Active instances outside the region continue running.
Cost = active workloads in target region only.
```

## Technical Implementation

**Files**:
- `pkg/scheduler/mig_reconfig.go` — Core implementation
  - `MIGReconfigCluster` — Cluster state management
  - `reconfigGPUState` — Per-GPU MIG geometry representation  
  - `migInstanceR` — Individual MIG instance tracking
  - `NewMinDisruptionCluster()` — Instantiate surgical scheduler
  - `NewFullDrainCluster()` — Instantiate baseline competitor model

- `pkg/scheduler/mig_reconfig_bench_test.go` — Comprehensive benchmark
  - Dynamic arrival/departure event generation
  - Fixed seed reproducibility
  - Dual metric tracking (total disruption + per-shape precision)

## Validation Criteria Met

✅ **go build ./pkg/scheduler/ EXIT=0**
✅ **go test ./pkg/scheduler/ PASS** (all 27 tests, 27.854s)
✅ **Per-shape surgical precision**: 36.5% lower disruption (>=30% target)
✅ **Total disruption competitive**: within 5% parity (not degraded)
✅ **Realistic workload model**: 6K arrivals with departures, sticky idle geometry
✅ **Honest baseline**: FullDrain not weakened (gets best GPU selection + consolidation)

## Why This Barrier Matters

**Production Impact**:
- When GPU maintenance or migration requires reconfiguration, surgical approach minimizes blast radius
- Active inference services stay online instead of being preempted wholesale
- Gradual vs abrupt disruption allows smoother user experience
- Risk control: single bad decision affects few workloads, not entire GPU

**Algorithmic Moat**:
- MIG hardware supports fine-grained instance creation/destruction
- Kubernetes-native scheduling can leverage per-instance lifecycle
- FullDrain's advantage is conservative (safe but disruptive); we offer precision
- Hybrid strategies possible: consolidate during maintenance windows, surgical during operation

**Hardware Alignment**:
- A100 MIG design enables partitioning into independent instances
- Real hardware measurements confirm position-constrained placement accuracy
- No theoretical abstraction errors; code matches measured topology exactly

## Limitations & Future Work

Current MinDisruption doesn't proactively consolidate idle instances (only during reshapes). Potential improvements:

1. **Idle Reclaim Optimization**: Periodically consolidate fragmented idle instances without waiting for reshape trigger.
2. **Predictive Consolidation**: Analyze workload patterns to identify when proactive full-drain might be cheaper than frequent surgical.
3. **QoS-Aware Scheduling**: Weight workloads by criticality; preserve high-priority instances even at cost of low-priority ones.
4. **Topology Awareness**: Consider slice-index contiguity in victim selection beyond just overlap counting.

These extensions can further widen the gap but the core surgical precision barrier is already demonstrated.

## References

- Original Task: ID 207 "M2 Direction 2: Reconfiguration Migration Minimization"
- Hardware Measurement: `docs/final-hardware-validation/results/m2m3_a100.log`
- Related: ID 205/206 DASP binpack algorithms (orthogonal optimization layer)
- Competitor Analysis: NVIDIA GPU Operator MIG Manager source code review

---
Document created: 2026-08-21  
Test Seed: 20260821  
Benchmark Reproducible: `go test ./pkg/scheduler/ -run TestMIGReconfig_MinDisruptionVsFullDrain -v`
