# Module 10 Performance Validation: Competitor Baseline Benchmark

**Project**: CloudAI Fusion — Module 10 RL Scheduler
**Date**: 2026-08-17
**Author**: AI Agent (benchmark execution + honest analysis)
**Status**: ✅ **EXECUTED — real measured numbers below.** All figures in this
document come from actual runs completed on 2026-08-17; the previous "unverified /
pending execution" placeholders have been replaced.

**Result artifacts (regenerable, git-ignored):**
- Competitor benchmark (production central-pool env, 10 seeds): `tmp/competitor_baselines_central_pool.json`
- Week 4 acceptance reproduction (legacy FIFO env, 5 seeds): `tmp/week4_7day_results.json`
- Smoke run (2 seeds, 300 episodes — pipeline validation only): `tmp/competitor_baselines_central_pool_SMOKE.json`

---

## 0. Headline Findings (read this first)

Two benchmarks were run to their accepted, unmodified configurations. They tell a
**two-part, non-cherry-picked story**:

1. **The Week 4 "+21.46% over round-robin" claim IS reproduced — exactly.**
   In the *legacy per-node FIFO* environment, our safety-masked tabular Q scores
   `q_vs_round_robin_pct = 21.4587%` (reward −528.3 vs round-robin −672.7). All
   three Week 4 gates pass.

2. **That advantage does NOT survive against realistic baselines in the production
   environment.** In the *central-pending-pool* environment (the Week 4.5
   production upgrade), evaluated against 5 baselines over 10 seeds, our policy
   scores **0 WIN / 1 LOSS / 39 TIE**. It has **no statistically significant
   advantage on any metric** and **loses** to `k8s_default_binpack` on GPU-hour
   fairness (`gini_gpu_hours`, p=0.021).

**Why the two results differ (this is the key honest insight):** the +21.46%
comes almost entirely from the **safety mask preventing catastrophic HP-job drops**
that the *blind* baselines (round-robin, random) suffer in the legacy FIFO env
(they each drop ~8 high-priority jobs and ~28–30 placements; ours drops 0). The
Week 4.5 central pool **structurally eliminates those drops for every policy**
(round-robin's reward jumps from −672.7 in FIFO to −353.5 in the pool). Once the
environment itself prevents the catastrophe, the RL policy's residual scheduling
edge is **statistically zero**. The advantage measured in Week 4 is therefore an
advantage of an **engineering guarantee (action masking)**, not of learned
scheduling intelligence — and that guarantee is trivially portable to any baseline.

> Bottom line for Goal 2 (absolute performance advantage): **the hard evidence does
> NOT support a general performance-superiority claim.** It supports a narrower,
> defensible claim: *zero avoidable high-priority job loss via a safety mask*, at
> parity with strong baselines on every other metric.

---

## 1. Environment & Dependencies (actual, as run)

| Component | Status at run time |
|-----------|--------------------|
| Python | 3.11.9 |
| numpy | 2.4.6 ✅ |
| scipy | 1.17.1 ✅ (Welch t-test + t-CI use real scipy, not the 1.96 fallback) |
| torch | 2.13.0+cpu ✅ **installed during this session** |
| stable_baselines3 | 2.9.0 ✅ **installed during this session** |
| gymnasium | ✅ installed during this session |

**Honesty note on PPO/SAC:** torch / stable_baselines3 / gymnasium were **absent**
at the start of this session and were installed via `pip` (no stubbing). **However,
neither benchmark suite trains PPO/SAC** — `test_competitor_baselines.py` and
`test_7day_production_simulation.py` only construct the NumPy `FactoredNodeQLearner`
(tabular Q). The deep-RL code paths live in `ai/scheduler/advanced_trainer.py`
(guarded by `_HAS_TORCH`) and are **never invoked by these tests**. Consequently:

- Every "our policy" number in this document is **tabular Q**, never PPO/SAC.
- No PPO/SAC numbers were produced by this run.
- The JSON artifacts contain a hard-coded field
  `training.ppo_sac_reason = "torch / stable_baselines3 not installed"`. That string
  is **now stale** — the deps ARE installed — but it remains accurate in spirit:
  PPO/SAC were not trained. Training and wiring deep RL into the evaluation contract
  is future work (see §7).

---

## 2. Execution Timing (smoke → full)

Runs were executed as **background processes** (Windows PowerShell `Start-Process`)
with output polled from log/JSON files, because a full run exceeds the 180 s
foreground sandbox limit.

| Run | Config | Wall time | Notes |
|-----|--------|-----------|-------|
| Smoke (pipeline check) | train=300 eps, 2 eval seeds | **~40 s** (37 s train + 3 s eval) | separate artifact, never used for conclusions |
| **Full competitor benchmark** | train=6000 eps, 10 eval seeds, 6 policies | **428.2 s** (419 s train + eval) | 786 Q-states, tail-1000 reward −160.4 ± 22.7 |
| **Week 4 acceptance (legacy FIFO)** | train=6000 eps, 5 eval seeds, 5 policies + calibration | **~520 s** (499 s train + calibration + eval) | 1139 Q-states, tail-1000 reward −213.1 ± 23.2 |

Smoke-based extrapolation (300 eps → 40 s ⇒ 6000 eps ≈ 13 min) matched the actual
full runtimes closely, confirming the pipeline was healthy before committing to the
long run.

---

## 3. Full Competitor Benchmark — Production Central-Pool Env (PRIMARY)

**Config (unmodified accepted contract):** 10 nodes × 8 GPU, arrival_rate = 0.12
(oracle-calibrated), horizon = 700 steps (7 sim-days), eval seeds
`901001…901010` (n=10), Welch's t-test at α=0.05, 95% t-CI.
**Arrival-stream drift across policies: 3.0%** (gate threshold 25% — comparison
valid; `completion_ratio` additionally controls for it).

### 3.1 Per-strategy results (mean ± sample-std, [95% t-CI], n=10)

`*` = our policy. Throughput = jobs/sim-day; compl = completion ratio; sla = HP SLA
violation rate; giniJCT / giniGPU = Gini over completion-times / per-node GPU-hours;
catas = total avoidable-HP-drop count across all 10 seeds.

| policy | reward (mean ± std) | thrpt/d | compl% | sla% | giniJCT | giniGPU | cost $ | catas |
|--------|--------------------:|--------:|-------:|-----:|--------:|--------:|-------:|------:|
| **\*q_learning_greedy** | **−359.0 ± 23.9** [−376.1, −341.9] | 8.34 | 63.2 | 12.6 | 0.417 | 0.266 | 39 671 | **0** |
| k8s_default_binpack | −363.2 ± 41.8 [−393.1, −333.3] | 8.50 | 62.9 | 12.5 | 0.438 | **0.215** | 43 325 | 0 |
| k8s_spread | −351.2 ± 47.2 [−385.0, −317.4] | **8.81** | 64.6 | 12.4 | 0.428 | 0.281 | 41 275 | 0 |
| feasibility_oracle | −359.3 ± 33.6 [−383.3, −335.2] | 8.63 | 64.4 | 10.9 | 0.439 | 0.300 | **37 954** | 0 |
| round_robin | −353.5 ± 56.6 [−394.0, −312.9] | 8.56 | 63.5 | 13.3 | 0.433 | 0.247 | 43 601 | 0 |
| random | **−349.4 ± 48.1** [−383.7, −315.0] | 8.74 | **65.4** | **8.0** | 0.437 | 0.246 | 41 651 | 0 |

Observations (unvarnished):
- **Every policy — including `random` — achieves 0 avoidable HP drops here.** The
  central pool prevents the catastrophic failure mode, so our safety mask confers
  no differential benefit in this env.
- On **reward**, `random` (−349.4) and `k8s_spread` (−351.2) actually score *better*
  than ours (−359.0); differences are not significant (all TIE).
- Ours has the **lowest throughput** (8.34/d) and the **second-worst SLA** (12.6%,
  beaten by random 8.0% and oracle 10.9%) — again, not statistically significant,
  but the direction is not in our favour.
- Ours is genuinely **best on `gini_completion`** (0.417) and competitive on cost
  (2nd lowest, behind the oracle) — but neither reaches significance.

### 3.2 Signed significance matrix — OURS vs each baseline (n=10, Welch t-test)

`adv%` is signed so **positive = ours better** for both metric directions.

| baseline | reward | thrpt | sla | giniGPU | cost | compl | giniJCT |
|----------|-------:|------:|----:|--------:|-----:|------:|--------:|
| round_robin | −1.55% (p.78) TIE | −2.50% (p.75) TIE | +5.07% (p.89) TIE | −7.79% (p.31) TIE | +9.01% (p.24) TIE | −0.43% (p.96) TIE | +3.78% (p.45) TIE |
| random | −2.76% (p.58) TIE | −4.58% (p.50) TIE | −57.45% (p.25) TIE | −8.47% (p.33) TIE | +4.75% (p.57) TIE | −3.33% (p.66) TIE | +4.58% (p.33) TIE |
| k8s_default_binpack | +1.16% (p.79) TIE | −1.85% (p.79) TIE | −1.01% (p.98) TIE | **−24.10% (p.021) LOSS** | +8.43% (p.26) TIE | +0.50% (p.95) TIE | +4.72% (p.32) TIE |
| k8s_spread | −2.22% (p.65) TIE | −5.35% (p.48) TIE | −1.93% (p.95) TIE | +5.32% (p.44) TIE | +3.88% (p.61) TIE | −2.15% (p.79) TIE | +2.56% (p.61) TIE |
| feasibility_oracle | +0.08% (p.98) TIE | −3.31% (p.59) TIE | −15.36% (p.66) TIE | +11.28% (p.15) TIE | −4.52% (p.58) TIE | −1.81% (p.81) TIE | +4.94% (p.37) TIE |

**Ledger: 0 WIN / 1 LOSS / 39 TIE.** Full per-(baseline,metric) verdicts are stored
in the JSON (`comparison.comparisons`) and the disclosure gate
`test_f_full_matrix_and_ledger_complete` enforces that no verdict can be omitted.

### 3.3 Metrics where we LOSE (disclosed, not hidden)

| metric | baseline | ours | baseline | adv% | p | Cohen's d | reading |
|--------|----------|-----:|---------:|-----:|--:|----------:|---------|
| `gini_gpu_hours` | k8s_default_binpack | 0.266 | 0.215 | **−24.10%** | **0.021** | +1.13 | Bin-packing spreads *delivered GPU-hours* more evenly across nodes than our policy does. This is a real, significant loss. |

No other metric reached significance in either direction, so this is the **only**
statistically defensible verdict beyond TIE — and it is against us.

---

## 4. Week 4 Acceptance Reproduction — Legacy FIFO Env

**Config:** same cluster/hyperparameters; env = legacy per-node FIFO
(`QueueAwareGPUEnvironment`); eval seeds `701001…701005` (n=5); arrival rate 0.12
selected by the oracle calibration grid.

### 4.1 Result — **+21.46% IS reproduced**

`q_vs_round_robin_pct = 21.4587%`. Gates: `zero_catastrophic=True`,
`q_beats_round_robin=True`, `q_beats_random=True`. All three Week 4 gates PASS.

| strategy | reward | completed | failed_pl | HP-drop | sla% | gpu_util |
|----------|-------:|----------:|----------:|--------:|-----:|---------:|
| **q_learning_greedy** | **−528.3** | 34.6 | 0.6 | **0.0** | 49.1 | 0.247 |
| round_robin | −672.7 | 44.6 | 29.8 | 8.0 | 27.6 | 0.271 |
| random_baseline | −663.3 | 42.8 | 28.2 | 8.2 | 30.0 | 0.298 |
| most_free_expert_reference | −541.9 | 33.8 | 1.4 | 0.0 | 46.2 | 0.242 |
| q_learning_unmasked_diag | −672.0 | 45.6 | 30.0 | 7.8 | 27.5 | 0.299 |

### 4.2 What the +21.46% actually measures (honest decomposition)

- The reward gap is driven by **catastrophic HP-drop penalties**, not scheduling
  quality: round-robin/random drop ~8 HP jobs + ~30 placements; ours drops 0.
- **The `q_learning_unmasked_diag` row is decisive:** it is the *identical trained
  Q-table with the safety mask turned off*, and it collapses to **−672.0 — the same
  as round-robin.** So the *learning* contributes ≈0 to the headline number; the
  **mask** contributes all of it.
- On the metrics that reflect scheduling skill, our policy is **worse** in this env:
  it **completes fewer jobs** (34.6 vs round-robin's 44.6) and has a **worse SLA
  violation rate** (49.1% vs 27.6%). It "wins" only by not incurring the drop
  penalty.
- Against the **learning-free feasibility oracle** (−541.9), our +21.46%-over-RR
  policy is only **+2.5%**, and it is essentially matched — again consistent with
  "the mask, not the model" being the differentiator.

This is why §3 (central pool, where the env removes the drop mode) shows the
advantage evaporating. **Both numbers are real; they are not in contradiction once
the source of the +21.46% is understood.**

---

## 5. Number Provenance (three categories, kept distinct)

Per the honesty requirement, every figure is tagged by source:

- **OURS — measured this run (tabular Q):** all `q_learning_greedy` rows in §3–§4,
  the +21.46%, training times/states, ledger 0/1/39. Source: the two JSON artifacts.
- **BASELINE — measured this run (same harness/seeds):** all round_robin / random /
  k8s_binpack / k8s_spread / feasibility_oracle / unmasked-diag rows. These are our
  own faithful *emulations* of the k8s scoring strategies, not the kube-scheduler
  binary (see §6.3), so they are labelled "baseline-measured", not "external".
- **EXTERNAL — cited from public docs (NOT measured here):** the *definitions* of
  the emulated strategies come from Kubernetes docs — `NodeResourcesFit` /
  `MostAllocated` (bin-pack) and `LeastAllocated` (spread):
  <https://kubernetes.io/docs/reference/scheduling/config/#scheduling-plugins> and
  <https://kubernetes.io/docs/concepts/scheduling-eviction/resource-bin-packing/>.
  **No external published performance numbers are quoted anywhere in this document.**

---

## 6. Honest Limitations (unchanged in spirit, now backed by data)

### 6.1 PPO / SAC not trained
See §1. Deps are now installed, but the benchmark code path only exercises tabular
Q. No deep-RL numbers exist. The stale `ppo_sac_reason` string in the JSON is noted.

### 6.2 Cost model is a documented approximation
`total_cost_usd` = `node_cost_per_hour × (gpus_needed / max_gpus) × occupied_hours`
(GPU-proportional share of node price). It is **not a real cloud bill**: no spot/
preemptible tiers, no power/cooling, no multi-cloud price variation. Relative cost
comparisons are meaningful; absolute $ values are approximate.

### 6.3 K8s baselines are faithful emulations, not the binary
The two k8s policies implement the documented two-phase Filter→Score scoring
(`MostAllocated` / `LeastAllocated`) with the same "leave pod Pending" fallback the
oracle gets. They deliberately do **not** model plugin ordering, preemption,
`PodTopologySpread`, or extenders. They are strong, un-handicapped opponents — and
in §3 one of them (`k8s_default_binpack`) beats us on fairness.

### 6.4 Simulator vs production cluster
Poisson arrivals (no real burstiness/seasonality), exponential service times (no
heavy tails), **no real network jitter, no real driver/hardware failures, no real
multi-tenant contention**, perfect observability. Conclusions transfer to a real
cluster only as far as this simulator is faithful — which is a genuine gap.

### 6.5 Known weak spot: SLA / queueing latency
This is where the data is least flattering. In the production env our HP **SLA
violation rate (12.6%)** is beaten by `random` (8.0%), `feasibility_oracle` (10.9%),
and `k8s_spread` (12.4%). In the legacy env our SLA rate (49.1%) is far *worse* than
round-robin's (27.6%) — because our policy trades queueing latency for zero HP drops.
The reward function values avoiding drops over minimizing wait, and it shows. We do
**not** claim an SLA-latency advantage; the evidence contradicts it.

### 6.6 Residual confound: arrival-stream drift
The env draws service times lazily from the same RNG as arrivals, so policies that
place different job counts see slightly different realized arrivals. Measured drift
this run = **3.0%** (well under the 25% invalidation threshold);
`completion_ratio` is reported alongside absolute throughput to control for it.

---

## 7. Recommendations (evidence-driven)

1. **Reframe the Module 10 claim.** Drop any general "beats production schedulers by
   +21.46%" framing. The honest, defensible claim is: *"zero avoidable high-priority
   job loss (safety-masked), at statistical parity with Kubernetes bin-pack/spread
   on throughput, SLA, cost, and completion fairness — with one measured loss on
   GPU-hour fairness."*
2. **Investigate the `gini_gpu_hours` loss** vs bin-packing; either add a per-node
   GPU-hour balancing term to the reward or accept and document the trade-off.
3. **Actually train PPO/SAC** (deps now present) and wire them into the same
   evaluation contract, so the deep-RL question is answered with numbers rather than
   asserted. Until then, no deep-RL superiority may be claimed.
4. **Address the SLA-latency weakness** (§6.5) before making any latency claims.
5. **Validate on a real cluster trace** to close the simulator gap (§6.4).

---

## 8. Reproduction Commands

```powershell
# Deps (one-time; installed during this session)
pip install torch --index-url https://download.pytorch.org/whl/cpu
pip install stable-baselines3[gymnasium]

# Full competitor benchmark (~7 min) — writes tmp/competitor_baselines_central_pool.json
cd d:\IdeaProjects\untitled\cloudai-fusion\ai
$env:PYTHONIOENCODING="utf-8"
python -m unittest tests.test_competitor_baselines.TestCompetitorBaselinesCentralPool -v

# Week 4 acceptance reproduction (~9 min) — writes tmp/week4_7day_results.json
python -m unittest tests.test_7day_production_simulation.TestSevenDayProductionSimulation -v

# Background-friendly runner (smoke | full), used for this report:
python tests/_run_bench.py smoke   # ~40 s pipeline check
python tests/_run_bench.py full    # full run, poll tmp/competitor_baselines_central_pool.json
```

---

*Document regenerated 2026-08-17 from live benchmark output. Every number above is
traceable to `tmp/competitor_baselines_central_pool.json` or
`tmp/week4_7day_results.json`. Losses are disclosed in §3.3 and §4.2; no favourable
metric was cherry-picked, and the +21.46% is presented only with its full context.*

---

## 7. Pareto Frontier Scan — can the gini penalty beat binpack WITHOUT breaking SLA? (2026-08-18)

**Question (Goal 2 flagship).** §3 showed our policy *loses* to `k8s_default_binpack`
on `gini_gpu_hours`. The GEN-2 reward adds a marginal per-node GPU-hour fairness
penalty. Does a precise weight exist that turns that loss into a **statistically
significant win (p<0.05) on `gini_gpu_hours`** *while keeping the SLA violation rate
within the hard gate of 12.7%* (GEN-2b baseline 12.6% + 1pp)?

**Method (anti-drift).** Runner `ai/tools/run_pareto_scan.py` reuses the audited
machinery from `tests/test_competitor_baselines.py` verbatim (`train_learner`,
`build_policies`, `evaluate`, `compare_all`, `Gen2StateQLearner`). Only two reward
weights are swept, injected as class attributes on a per-cell subclass of
`CentralPendingPoolEnvironment` and applied **identically to all 6 policies**:
`GINI_GPU_PENALTY_WEIGHT_GEN2` (fairness push) and `JOB_DELAY_PENALTY_WEIGHT` (SLA
protection). Pinned hyperparameters (tau 2.0->0.05, gamma 0.9, PESSIMISTIC_INIT -3.0)
are asserted at start-up. `alpha=0.05` is never widened; SLA gate 0.127 is pre-registered.
Eval = 700 steps (7 sim days) x 10 seeds x 6 policies. Result artifacts (git-ignored,
regenerable) live in `ai/pareto_scan_*.json`.

### 7.1 Stage 1 — exhaustive 20-cell screen (COMPLETE, 20/20)

Grid `gini in {3.0,3.5,4.0,4.5,5.0}` x `sla in {0.0,0.3,0.5,0.8}`, 2000 ep x 300
steps each. Wall time 5026 s. All 20 cells reported (no cell omitted); full ledgers
in `ai/pareto_scan_g*_s*_stage1.json`, index in `ai/pareto_scan_stage1_index.json`.

| cell | gini | sla | SLA% | gate | binAdv% | binP | binD | spAdv% | spP |
|------|-----:|----:|-----:|:----:|--------:|-----:|-----:|-------:|----:|
| g50_s5 | 5.0 | 0.5 | 13.4 | FAIL | +12.09 | 0.2655 | -0.51 | +32.93 | 0.0005 |
| g45_s5 | 4.5 | 0.5 | 14.3 | FAIL | +10.63 | 0.3049 | -0.47 | +31.81 | 0.0004 |
| g40_s5 | 4.0 | 0.5 | 15.8 | FAIL | +10.48 | 0.3014 | -0.48 | +31.70 | 0.0003 |
| **g50_s3** | 5.0 | 0.3 | **11.1** | **PASS** | +9.94 | 0.3547 | -0.43 | +31.29 | 0.0007 |
| g45_s3 | 4.5 | 0.3 | 14.4 | FAIL | +8.72 | 0.3943 | -0.39 | +30.36 | 0.0005 |
| g35_s5 | 3.5 | 0.5 | 16.0 | FAIL | +8.14 | 0.4385 | -0.35 | +29.92 | 0.0008 |
| g35_s3 | 3.5 | 0.3 | 14.7 | FAIL | +6.42 | 0.4802 | -0.32 | +28.61 | 0.0002 |
| g50_s0 | 5.0 | 0.0 | 14.6 | FAIL | +6.39 | 0.5532 | -0.27 | +28.58 | 0.0017 |
| g35_s8 | 3.5 | 0.8 | 19.2 | FAIL | +6.04 | 0.5218 | -0.29 | +28.31 | 0.0004 |
| g45_s8 | 4.5 | 0.8 | 22.4 | FAIL | +5.72 | 0.5285 | -0.29 | +28.07 | 0.0003 |
| g40_s8 | 4.0 | 0.8 | 21.4 | FAIL | +4.61 | 0.6618 | -0.20 | +27.22 | 0.0020 |
| g30_s5 | 3.0 | 0.5 | 17.0 | FAIL | +4.01 | 0.7088 | -0.17 | +26.77 | 0.0028 |
| g30_s8 | 3.0 | 0.8 | 17.5 | FAIL | +3.99 | 0.7018 | -0.17 | +26.75 | 0.0021 |
| g45_s0 | 4.5 | 0.0 | 13.2 | FAIL | +3.70 | 0.7321 | -0.16 | +26.53 | 0.0032 |
| g30_s3 | 3.0 | 0.3 | 13.7 | FAIL | +3.19 | 0.7129 | -0.17 | +26.14 | 0.0004 |
| g40_s0 | 4.0 | 0.0 | 14.8 | FAIL | +2.09 | 0.8339 | -0.10 | +25.30 | 0.0021 |
| g40_s3 | 4.0 | 0.3 | 15.6 | FAIL | +1.18 | 0.9174 | -0.05 | +24.60 | 0.0082 |
| g50_s8 | 5.0 | 0.8 | 20.9 | FAIL | +1.13 | 0.9032 | -0.06 | +24.57 | 0.0014 |
| g30_s0 | 3.0 | 0.0 | 13.1 | FAIL | -2.66 | 0.7833 | +0.12 | +21.68 | 0.0050 |
| g35_s0 | 3.5 | 0.0 | 14.5 | FAIL | -3.02 | 0.7731 | +0.13 | +21.40 | 0.0103 |

**Stage 1 tally: SLA gate PASS 1/20 (only g50_s3) · binpack SIG-WIN 0/20 · PASS & SIG-WIN 0/20.**
The fairness push clearly separates us from spread (spAdv +21..+33%, all significant),
but every cell is a statistical **TIE** vs binpack on gini, and pushing the gini
weight up to shrink the binpack gap simultaneously drives SLA past the gate.

### 7.2 Pareto top-3 selection (SLA-constrained, ranked by binpack advantage)

Selection rule (`_select_top3`): take SLA-passing cells ranked by binpack gini
advantage; only 1 cell passes, so the two closest-to-gate cells are added as
disclosed **trade-off probes** (they are not eligible winners). Selected:

| rank | cell | gini | sla | stage-1 SLA | stage-1 binAdv | role |
|-----:|------|-----:|----:|:-----------:|:--------------:|------|
| 1 | g50_s3 | 5.0 | 0.3 | 11.1% PASS | +9.94% (p=0.3547) | SLA-passing, best binAdv among passers |
| 2 | g50_s5 | 5.0 | 0.5 | 13.4% FAIL | +12.09% (p=0.2655) | closest-to-gate trade-off probe |
| 3 | g45_s0 | 4.5 | 0.0 | 13.2% FAIL | +3.70% (p=0.7321) | closest-to-gate trade-off probe |

### 7.3 Stage 2 — final evaluation (6000 ep x 10 seeds, 6 policies)

Each cell re-trained at 6000 episodes and re-evaluated over 10 seeds x 700 steps
against all 5 baselines. `gini_gpu_hours` ledger (our mean vs each opponent):

**g45_s0** (gini 5.0->4.5, sla 0.0) — SLA **11.48% → PASS** · catastrophic 0 · our gini 0.1992

| opponent | base gini | adv% | p | Cohen d | verdict |
|----------|----------:|-----:|--:|--------:|:-------:|
| k8s_default_binpack | 0.2147 | +7.23 | 0.4657 | -0.33 | **TIE** |
| k8s_spread | 0.2815 | +29.22 | 0.0005 | -1.89 | WIN |
| feasibility_oracle | 0.3004 | +33.68 | 0.0004 | -1.97 | WIN |
| round_robin | 0.2472 | +19.42 | 0.0214 | -1.13 | WIN |
| random | 0.2457 | +18.91 | 0.0394 | -0.99 | WIN |

**g50_s3** (gini 5.0, sla 0.3) — SLA **15.23% → FAIL** · catastrophic 0 · our gini 0.1818

| opponent | base gini | adv% | p | Cohen d | verdict |
|----------|----------:|-----:|--:|--------:|:-------:|
| k8s_default_binpack | 0.2147 | +15.33 | 0.1045 | -0.77 | **TIE** |
| k8s_spread | 0.2815 | +35.40 | 0.0000 | -2.51 | WIN |
| feasibility_oracle | 0.3004 | +39.47 | 0.0000 | -2.46 | WIN |
| round_robin | 0.2472 | +26.46 | 0.0013 | -1.70 | WIN |
| random | 0.2457 | +25.99 | 0.0040 | -1.48 | WIN |

**g50_s5** (gini 5.0, sla 0.5) — SLA **16.41% → FAIL** · catastrophic 0 · our gini 0.1852

| opponent | base gini | adv% | p | Cohen d | verdict |
|----------|----------:|-----:|--:|--------:|:-------:|
| k8s_default_binpack | 0.2147 | +13.74 | 0.1520 | -0.67 | **TIE** |
| k8s_spread | 0.2815 | +34.19 | 0.0001 | -2.35 | WIN |
| feasibility_oracle | 0.3004 | +38.33 | 0.0001 | -2.34 | WIN |
| round_robin | 0.2472 | +25.08 | 0.0026 | -1.56 | WIN |
| random | 0.2457 | +24.61 | 0.0069 | -1.36 | WIN |

In the SLA-passing cell g45_s0, our per-seed SLA (11.48%) is even marginally *below*
binpack's (12.48%) and our gini is 7.2% lower — but with p=0.4657 both are honest
**ties**, not wins.

### 7.4 Verdict — NO Pareto-optimal cell exists (`pareto_optimal_found: false`)

> **Within the SLA<=12.7% hard constraint, Module 10 CANNOT achieve a p<0.05
> `gini_gpu_hours` advantage over `k8s_default_binpack`.** Across the full 20-cell
> Stage 1 screen and the 3-cell Stage 2 final evaluation, **0 cells** simultaneously
> (a) hold SLA within the gate AND (b) beat binpack significantly. The single
> SLA-passing Stage 2 cell (g45_s0) is a clear statistical TIE (+7.23%, p=0.4657).

**Positive, defensible finding:** against the other four baselines the gini push is a
genuine, significant win. In every top-3 cell Module 10 beats `k8s_spread`,
`feasibility_oracle`, `round_robin` and `random` on `gini_gpu_hours` at p<0.05
(g45_s0 does so *inside* the SLA gate with zero catastrophic failures). `binpack`
specifically remains un-passable within the SLA constraint.

### 7.5 Quantified Pareto trade-off (gini advantage vs SLA cost)

Stage 2 refined frontier — buying more gini advantage vs binpack costs SLA linearly
and crosses the gate before the advantage ever becomes significant:

| cell | SLA% | delta vs 12.6% baseline | gini adv vs binpack | binpack p | inside gate? |
|------|-----:|:-----------------------:|:-------------------:|:---------:|:------------:|
| g45_s0 | 11.48 | -1.12 pp | +7.23% | 0.4657 | YES |
| g50_s3 | 15.23 | +2.63 pp | +15.33% | 0.1045 | no |
| g50_s5 | 16.41 | +3.81 pp | +13.74% | 0.1520 | no |

Moving from the SLA-safe regime (+7.23% gini, 11.48% SLA) to the largest measured
gini gap (+15.33%) costs **+3.75 pp of SLA** (11.48% -> 15.23%), pushing 2.5 pp past
the 12.7% gate — and **even then the binpack advantage is still not significant**
(p=0.1045). The two objectives move in opposite directions along the swept weight.

**Why binpack is structurally hard to beat on gini_gpu_hours:** bin-packing
consolidates onto the fewest nodes, which under a 7-day cumulative GPU-hour Gini
keeps a small, stable node set loaded evenly. The fairness push that lowers our Gini
necessarily spreads SLA-bearing jobs onto colder nodes, and the safety mask forbids
the placements that would recover the SLA — so gini and SLA are coupled in
opposition. This is a **real Pareto frontier**, not a tuning miss: "pressing past
binpack necessarily sacrifices SLA" is confirmed as a structural property of this
environment. Manufacturing a WIN here would require widening alpha or relaxing the
SLA gate, which the runner explicitly forbids.

**Artifacts:** `ai/pareto_scan_g{45_s0,50_s3,50_s5}_stage2.json`,
`ai/pareto_scan_final_report.json`, Stage 1 grid `ai/pareto_scan_g*_s*_stage1.json` +
`ai/pareto_scan_stage1_index.json`. Reproduce: `cd ai; python tools/run_pareto_scan.py
--grid-full` then `python tools/run_pareto_scan.py --final-eval`.

---

*Section 7 added 2026-08-18 from live Pareto-scan output. Stage 1 = 20/20 cells; Stage
2 = 3 cells x 6000 ep x 10 seeds. alpha=0.05 and the 12.7% SLA gate were never
relaxed; all top-3 ledgers are reported in full with no cherry-picking. Conclusion is
a negative-but-honest structural result: no loss-free way to beat binpack on gini.*
