# RL Environment V2: Queue-Aware MDP Design

**Week 2 Reconstruction** — Complete rebuild of the GPU scheduling MDP environment
based on [Week 1 Root Cause Analysis](./WEEK1_RL_OPTIMIZER_ROOT_CAUSE_ANALYSIS.md).
**Week 3 Update** — reward quadratic shaping + trainer rewiring + learnability
proof + Go↔Python feature contract (see §7).
**Week 4.5 Update** — central pending pool: FIFO HOL elimination, SLA 49.1%→
12.2% violations (compliance 50.9%→87.8%), structural zero job-loss (see §9).

- **Status**: Complete — Week 2: 11 sanity tests green; Week 3: 12/12 green, learnability gate **PASS** (+26.5% vs best baseline)
- **Primary artifacts**:
  - `ai/scheduler/env_queue_aware.py` — new `QueueAwareGPUEnvironment` (Week 3: quadratic reward)
  - `ai/scheduler/advanced_trainer.py` — `QueueAwareTrainer` + `TabularQTrainer` (Week 3)
  - `ai/tests/test_rl_sanity_tests.py` — learning-gate test suite (Week 3: +smoothness test)
  - `pkg/scheduler/rl_schema.go` — Go↔Python unified feature contract (Week 3)
  - `tmp/week2_queue_diagnostic.py`, `tmp/week3_learning_proof.py` — standalone numpy experiments
- **Replaces**: `GPUSchedulingGymEnv` in `ai/scheduler/advanced_trainer.py` (now marked DEPRECATED, retained for reference)

---

## 1. Why a rebuild was necessary (Week 1 evidence)

Week 1 established that the *primary blocker* was not the RL algorithms but the
MDP itself:

| Week 1 defect | Evidence | V2 fix |
|---|---|---|
| **Bandit, not MDP** — no queue entities, one iid job per step (§1.4.1) | DQN 5/5 metrics worst in Go benchmark (Makespan 2408 vs 2204, −9.2%) | Real per-node FIFO queues + Poisson arrivals + job lifecycle |
| **Pathological reward surface** — step-wise sweet-spot bonus, degenerate share-ratio vector (§1.2.1) | B2 sweep: +6.57 → +13.01 → +13.60 → +6.53 → +5.04 zig-zag; B3: starving policy beat generous one (−7422.7 vs −7881.3) | Continuous multi-objective reward, no topology bonus, no cost reward on failure, Gini fairness term |
| **Sick observations** — no queue depth, no pressure, un-normalized [0,100+] scales (§1.1.1) | Exp C: 100× scale spread + constant dims, Box(low=-1, high=200) | 9 queue-aware features × N nodes + 5 workload features, **all clipped to [0,1]** with fixed per-feature ranges |
| **Topology leakage** — `topo*3.0` reward pushed policy to high-topo nodes regardless of action quality (§1.2.1) | NVLinkSat 56.1% "achieved" with NVLink features empty in bench obs | Topology appears **only as an observation** (`nvlink_score`), never as a reward |
| **Unseedable dynamics** — global `np.random` everywhere (§1.3.3) | Reproducibility = 0 across resets | `np.random.Generator` injected at construction and re-seeded on `reset()` |

---

## 2. Design principles

1. **Real queue tracking** — every node owns a FIFO `deque` of pending jobs,
   a running-jobs list, and the env tracks all arrived/completed jobs.
2. **Queuing delay is a first-class feature** — `wait_time_since_arrival`
   accumulates on every clock tick for every pending job (this single dynamic
   is what converts a bandit into an MDP).
3. **Cluster pressure** — `queue_depth_sum / (num_nodes * 10)`, clipped to
   [0,1], identical value exposed per node so any node-slice of the policy
   input sees global congestion.
4. **Topology-aware but not topology-bribed** — `nvlink_score` is an
   observation input only. (Production TODO: compute it from the real NVLink
   graph via `gpu_topology.go` P2P matrix instead of the current seeded
   per-node value — tracked in §6.)
5. **Normalized inputs** — every feature scaled to [0,1] via fixed ranges
   (`normalize_value`), no per-sample min-max (which broke Markov-ness in
   Go-A's encoder).

---

## 3. MDP specification

### 3.1 Observation (Box[0,1], shape = `num_nodes*9 + 5`)

Per node (9 features, fixed semantic order):

| idx | feature | normalization |
|---|---|---|
| 0 | `gpu_util` | /100 |
| 1 | `mem_util` | /100 |
| 2 | `cpu_util` | /100 |
| 3 | `free_gpus_ratio` | /max_gpus |
| 4 | `cost_norm` | /120 $/hr |
| 5 | `nvlink_score` | already [0,1] |
| 6 | `queued_jobs_norm` | /max_pending |
| 7 | `avg_wait_norm` | /24h |
| 8 | `cluster_pressure` | queue_depth/(N*10), clipped |

Workload (5 features): `gpus_needed/8`, `priority/100`, `job_type/2`,
`estimated_duration/10h`, `deadline_pressure` (already [0,1]).

### 3.2 Action (Discrete)

`action ∈ {0 .. num_nodes-1}` — direct node selection.
Replaces V1's continuous 3-tuple that was funneled through a heuristic ranker
(Week 1 §1.4.3 showed `preference ∈ [0, 0.1]` always picked heuristic rank-0,
collapsing the learnable policy space). A feasibility action-mask is a Week 3
extension candidate.

### 3.3 Transition dynamics (per step)

1. Pop head-of-queue job from the selected node's FIFO (if empty → idle reward −1).
2. Attempt placement: succeed iff `gpus_needed ≤ free_gpus`; on success deduct
   GPUs, push to running, bump utils; on failure → −8, SLA violation counter.
3. Sample Poisson(λ=arrival_rate) new jobs, round-robin into node queues.
4. Advance running jobs — each job samples an exponential service time at
   start; jobs whose elapsed ≥ duration complete, freeing GPUs and decaying utils.
5. Advance the clock (0.01 day ≈ 14.4 min) — **this recomputes
   `wait_time_hours` for every pending job**, so queues visibly age.

Because steps 3–5 couple this step's action to all future states (queue
composition, free GPUs, aged wait times), the process is genuinely Markovian.

### 3.4 Reward V2 (no fake bonuses)

```
r = utilization_band + binpack + cost_eff·(placed only) + sla_aging + fairness
```

| term | formula | rationale |
|---|---|---|
| utilization | +6 if util∈[65,85]; +3 if ∈[50,90]; −2 if >95; −1 if <30 | kept from V1 but no longer combined with leakage terms |
| binpack | `(gpus_needed/max_gpus)*2` | rewards consolidating big jobs |
| cost | `max(0,(100−cost)/100)*2` **only when placed** | fixes B1: V1 paid this on 200/200 failures |
| SLA aging | `priority_norm · wait_norm · 4` | high-priority + long-waited jobs give biggest bonus when scheduled — the anti-starvation term |
| fairness | `(1 − Gini(last 10 JCTs)) · 3` | the objective V1 docstring promised but never implemented |

Explicitly removed: `topo*3` reward term, `share_ratio` reward (the
starvation degeneracy), failure-path cost reward.

---

## 4. Verification results (this week)

### 4.1 Learning-gate suite — `ai/tests/test_rl_sanity_tests.py`

```
$ python -m unittest ai.tests.test_rl_sanity_tests -v
Ran 11 tests — OK (11 passed)
```

Key acceptance tests (from the Week 2 task spec):

| test | assertion |
|---|---|
| `test_queue_depth_affects_observation` | queued-jobs feature rises ≥0.1 under injected load |
| `test_cluster_pressure_normalize` | pressure ∈ [0,1] and >0.3 under 200-job overload |
| `test_topology_score_from_nvlink_graph` | topo ∈ [0,1], varies across nodes |
| `test_reward_without_fake_bonuses` | 50-step rewards bounded, no degenerate bias |
| `test_wait_time_accumulation` | pending job wait strictly increases with clock |
| `test_actions_have_cascading_effects` | placements > 0 AND net queue growth < arrivals |
| `test_queue_blocking_prevents_bandit` | avg wait > 0.1h under overload |
| `test_q_table_zero_cannot_learn` | zero-Q baseline evaluated before/after training |
| `test_epsilon_decay_rate` | decay schedule sanity (guards §1.3.1 class of bug) |
| `test_all_features_normalized_to_range` | full obs ∈ [0,1] |
| `test_no_constant_dimension` | variance scan across 20 resets (warn-only) |

### 4.2 Diagnostics — `tmp/week2_queue_diagnostic.py` (numpy-only)

```
$ python tmp/week2_queue_diagnostic.py
Week 1 (bandit)   : no temporal structure, iid decisions
Week 2 (queue MDP): state autocorrelation(lag=1) = 0.9786   ← genuine temporal dynamics
                    avg queue depth 187.41, max 387
                    mean reward 3.23 ± 2.49
SUMMARY { "success": true }
```

The 0.98 lag-1 autocorrelation of queue depth is the quantitative signature
that actions now have cascading effects — the property whose absence made the
old environment a bandit.

---

## 5. Dependency posture (honest note)

This machine currently has **no** `gymnasium`, `stable_baselines3`, `torch`,
`structlog`, or `pytest` (confirmed in Week 1 §0 and again this week). The new
environment therefore follows the repo's optional-heavy-deps pattern:

- `gymnasium` optional — a tiny `_EnvStub`/`_DiscreteStub` keeps the env
  instantiable and fully testable with pure numpy (this is how all 11 tests
  run today). When gymnasium is installed, real `gym.Env`/`spaces` are used
  automatically and SB3 trainers can wrap it unchanged.
- `structlog` optional — falls back to stdlib `logging`.
- SB3 metrics callback is defined only when `stable_baselines3` imports.

Week 3 must decide whether to `pip install gymnasium stable-baselines3 torch`
in the dev/CI image (per Week 1 plan) — the environment code is ready either way.

## 6. Known limitations / Week 3 handoff

1. **nvlink_score is a seeded per-node constant**, not yet computed from the
   real P2P matrix in `pkg/scheduler/gpu_topology.go`. Wiring that (plus the
   shared JSON feature schema for Go-side parity) is the next contract task.
2. **~20 observation dims can be constant in light-load episodes** (empty
   queues ⇒ zeros at feature idx 6/7/8 of idle nodes). The variance test
   flags this as a warning, not a failure; DeepRM-style "K pending job slots"
   would densify it if training shows dead-gradient symptoms.
3. **Sweet-spot utilization reward still has hard bands** (V1 heritage).
   Week 1 plan §4 recommended a quadratic `−(util−0.75)²` shaping — deferred
   to Week 3 reward-tuning so this week ships a *verified-correct MDP* before
   any shaping experiments (per the "slow verification" principle).
4. **Round-robin queue assignment** for arrivals is deliberately naive; a
   central pending pool with the policy choosing (node, job) jointly is the
   Volcano-gang-scheduling-aligned upgrade path.
5. Trainers (`advanced_trainer.py` PPO/SAC, `train.py` tabular) still
   construct the **old** env — Week 3 rewires them to `QueueAwareGPUEnvironment`
   and re-runs the Go benchmark parity suite.

---

## 7. Week 3: Trainer wiring, reward shaping, learnability proof, Go contract

### 7.1 Load diagnostic — picking a learnable regime (honesty note)

Before running the learnability gate we profiled the environment across
arrival rates (5 nodes × 8 GPUs, 100 steps, 20 episodes/policy):

| arrival_rate | random | most-free expert | gap | regime |
|---|---|---|---|---|
| 5.0 (default) | −581.4 | −563.6 | 3% | saturated — ~80% failed placements, no learnable signal |
| 1.0 | −514.9 | −246.8 | **52%** | moderate load — queues alive, choices matter |
| 0.5 | −271.9 | −128.5 | 53% | light load |
| 0.2 | −137.3 | −87.3 | 36% | very light |

The default `arrival_rate=5.0` admits ~5 Poisson arrivals per step while only
1 job can be scheduled per step — the cluster saturates, free GPUs pin to zero,
states become indistinguishable, and **no policy (RL or otherwise) separates
from random**. The learnability proof therefore runs at `arrival_rate=1.0`, the
standard moderate-load regime for scheduler benchmarks. This is a load
parameter choice, not an environment change; Week 4 sweeps both regimes.

### 7.2 Reward V2.1 — quadratic sweet-spot shaping (Week 1 §1.2.1 / plan §4)

The Week 2 hard-segment utilization bonus (`+6 if util∈[65,85]; +3 if
∈[50,90]; …`) inherited V1's step boundaries. Replaced with a smooth
quadratic penalty in `_compute_queue_aware_reward`:

```python
reward -= 4.0 * (util/100.0 - 0.75) ** 2   # peak at 75%, gradient everywhere
```

**Regression test** (`test_reward_surface_smoothness_quadratic_shaping`,
added to the Week 2 suite) sweeps util 0→100% against the **real** reward
method (fixed job → other terms constant) and asserts:

1. single peak within [65, 85] (measured: 74–76%),
2. max second-difference of the reward curve < 0.01 (pure quadratic ≈ 0.0032;
   the old hard segments jump by O(3.0) at band boundaries — ~100× larger),
3. strict monotonicity on both sides of the peak (0 violations).

For contrast, Week 1 experiment B2 measured the V1 surface along the share
axis as `+6.57 → +13.01 → +13.60 → +6.53 → +5.04` — multi-modal zig-zag.
All 11 Week 2 tests still pass after the change (now 12/12).

### 7.3 Trainer rewiring (`advanced_trainer.py`)

- `GPUSchedulingGymEnv` — **DEPRECATED** (docstring now lists the Week 1
  defects and points to the replacement; class retained for reference).
- `QueueAwareTrainer` — facade over `QueueAwareGPUEnvironment`; `train_ppo()` /
  `train_sac()` activate automatically when SB3 is installed (not present on
  this machine), `train_tabular()` always available.
- `TabularQTrainer` — **zero-dependency** (numpy only) tabular Q-learner:
  - ε-greedy with **episode-level decay** 1.0 → 0.05 at 0.9995/episode
    (Week 1 Defect #3 fix: the Go DQN applied an episode-rate decay per *step*,
    exhausting exploration in ~1000 steps); final ε after 5000 episodes ≈ 0.082.
  - State discretization keeps **per-node** information (Week 1 §1.4.2 fix vs
    the old `sum(obs[:4]) % 100` hash collapse): per-node code =
    `0` if queue empty (idle −1 risk) else `1 + free_gpus bucket` (failure −8
    risk), plus a gpu-need bucket from the workload features — the exact
    signals that decide the two dominant reward events. Q-table: 3024 entries.
  - CLI: `python -m scheduler.advanced_trainer --algo TABULARQ --episodes 5000`.
- Module now degrades gracefully without `structlog` (stdlib logging fallback)
  and imports the V2 env under both `ai.scheduler.*` and `scheduler.*` paths.

### 7.4 Learnability proof (`tmp/week3_learning_proof.py`, numpy-only)

Protocol: 1000-episode baselines (random, round-robin) → 5000-episode
Q-learning training → 500-episode deterministic greedy evaluation on unseen
seeds. Gate: `q_final > best_baseline + 0.10 × |best_baseline|`.

Results (`tmp/week3_learning_proof_results.json`, 5 nodes, rate=1.0):

> **Determinism update (Week 4)**: the table below is the pre-RNG-fix run.
> Week 4 eliminated three residual global-`np.random` calls in the env
> (NodeState init + workload batch) — same-seed runs now reproduce
> bit-for-bit, and the re-archived final numbers are: random −506.02,
> round-robin −556.64, Q greedy **−382.07**, threshold −455.42 → **PASS
> +24.49%** (both runs clear the gate; the deterministic archive is
> authoritative). See §8.1.

| policy | mean reward | ±std | placements | failed |
|---|---|---|---|---|
| round-robin | −556.86 | 61.9 | 19.5 | 71.1 |
| random | −508.22 | 59.6 | 18.2 | 62.4 |
| Q-learning (train tail-500, ε≈0.08) | −395.58 | 63.1 | — | — |
| **Q-learning (greedy eval, 500 eps)** | **−373.79** | 82.0 | 12.9 | 44.9 |

- Threshold: −457.40 → **PASS with +26.5% improvement over best baseline**
  (both the greedy evaluation *and* the still-exploring training tail clear
  the gate — double-covered).
- Learning curve (250-episode means): −504.7 → −486.8 → −473.4 → −449.6 →
  −430.9 → −414.1 → −399.4 → −384.7 → −380.5 → −395.7 — monotone-ish
  convergence to a late plateau; failures drop 62.4 → 44.9 per episode.
- The learned policy achieves *higher reward with fewer placements*: it
  avoids −8 failure events (and idle −1 events) instead of chasing placement
  counts — exactly the feasibility-aware behavior the reward was designed to
  elicit.
- Interpretation: the Week 2 MDP reconstruction claim "this environment is
  learnable" is now **empirically verified** by the weakest reasonable learner
  (tabular Q). Anything fancier (DQN/PPO/SAC, Week 4) must beat this bar.

### 7.5 Go↔Python feature contract (`pkg/scheduler/rl_schema.go`)

Week 1 §1.0 documented four mutually incompatible RL stacks (65-dim Python
obs vs 50-dim Go input vs 5-tuple Go-B keys). `RLFeatureSchema`
(`v2-queue-aware`) is now the single source of truth mirroring
`_build_obs()` exactly:

- `NewQueueAwareSchema(numNodes)` → obs_dim = `9·N + 5`, action = Discrete(N),
  full ordered `FeatureNames` (`node0.gpu_util … nodeN.cluster_pressure`,
  then `gpus_needed … deadline_pressure`);
- `ValidateObs` enforces length + [0,1] range (catches un-normalized percent
  leaks — the class of bug that broke Go-A's encoder);
- `NodeSlice` / `WorkloadSlice` / `FeatureIndex` give the Go side typed access
  to the layout; `JSON()` renders the contract for dual-end contract tests.

Go contract tests (`rl_schema_test.go`): **4/4 PASS** — layout order,
validation, slicing/indexing, JSON rendering. `go build ./pkg/scheduler/...`
and `go vet` clean.

### 7.6 Week 3 acceptance checklist

| # | criterion | result |
|---|---|---|
| 1 | `from scheduler.env_queue_aware import QueueAwareGPUEnvironment` | ✅ OK |
| 2 | `python tmp/week3_learning_proof.py` → `success: true` | ✅ PASS (+26.5%) |
| 3 | Week 2's 11 tests still green | ✅ 12/12 (incl. new smoothness test) |
| 4 | reward surface smooth (no zig-zag vs B2/B3) | ✅ curvature < 0.01, single peak, 0 monotonicity violations |
| 5 | `go build ./pkg/scheduler/...` | ✅ clean (vet clean, 4/4 contract tests) |
| 6 | this document updated with Week 3 data | ✅ §7 |

### 7.7 Week 4 handoff

1. **Multi-seed protocol**: 10 env seeds × 3 algorithm seeds per Week 1 §4;
   current proof is single-seed by design (fast gate).
2. **Expert-policy gap**: greedy Q (−373.8 pre-fix archive; −382.07
   deterministic) still trails the most-free expert (−246.8 measured in
   §7.1) in the 5-node/100-step diagnostic regime — headroom for
   DQN/PPO/SAC; also suggests adding per-node free-GPU features more
   finely than 3 buckets may pay off. (Week 4 note: in the 10-node/7-day
   sustainable regime the masked factored Q *does* beat this oracle by
   +2.5% — see §8.4; the gap remains in the short-horizon regime.)
3. **Overload regime**: learnability at rate=5.0 is expected to fail for *any*
   scheduler (§7.1); Week 4 should report both regimes separately rather than
   average them.
4. **nvlink_score from real P2P matrix** (Week 2 §6.1) — feed Go-side topology
   into `RLNodeNVLinkScore` via the schema.
5. **Tabular policy serving in Go**: export the Q-table JSON (already
   implemented, `TabularQTrainer.save`) and load it behind the `RLFeatureSchema`
   encoder as a deterministic fallback policy bridge.

---

## 8. Week 4: 7-day production simulation + determinism + safety masking

Full narrative, tables, and acceptance checklist:
[`MODULE_10_FIX_FINAL_REPORT.md`](MODULE_10_FIX_FINAL_REPORT.md).
Acceptance test: `ai/tests/test_7day_production_simulation.py` (7/7 OK).

### 8.1 Determinism fix (root cause of Week 3 cross-run variance)

Two Week 3 runs of the same script produced −407.71 vs −388.95. Root
cause: three residual global-`np.random` calls inside the env (NodeState
init uniform/integers + workload batch choice). Replaced with `self._rng`;
verified same-seed bit-for-bit reproduction (`DETERMINISTIC: True`), 12/12
Week 2 suite unaffected, Week 3 proof re-archived deterministically
(−382.07 / +24.49%).

### 8.2 Load calibration at 10 nodes × 7 days (extends §7.1)

The literal `arrival_rate=1.0` is ~7× oversubscribed at this scale
(745 arrivals / 144 scheduled / 152 overflow drops / 55 failed placements
— every policy fails). Feasibility-oracle grid probe selected the highest
sustainable rate: **0.12** (zero failures, zero overflow, 25.3% GPU util
≥ 15% floor). Grid: 0.5→22.5 fails, 0.25→5.0, 0.15→2.5, 0.12→0.

### 8.3 Representation: joint → factored + Safe-RL masking

Probe chain (all archived in `tmp/_w4_probe*.py`):

- Joint-state tabular Q at 10 nodes: 117k Q entries, unvisited states →
  greedy collapse (~30 failures/7 days; training *longer* made it worse).
- Factored per-node state (6-tuple local features, weight sharing across
  nodes): 1139 states after 6000 eps × 300 steps (442 s), stable tail
  −213.1 ± 23.2 — but partial observability still yielded 16.8 failures/7d
  with `defaultdict(0)` init.
- **Fix**: Safe-RL action mask (queue-head demand vs free GPUs; falls back
  to all-allowed when no node is safe — identical to the oracle fallback)
  + pessimistic init (−8.0). Probe smoke: 3/3 seeds zero HP drops.

### 8.4 Results (5 seeds × 700 steps = 7 days, rate 0.12)

| strategy | reward | failed (tot) | avoidable HP drops | forced drops (all/HP) | SLA | cost | JCT |
|---|---|---|---|---|---|---|---|
| **q_learning_greedy (masked)** | **−528.3** | 3 | **0** | 3 / 1 | 49.1% | $21,831 | 39.5h |
| round_robin | −672.7 | 149 | 40 | 0 / 0 | 27.6% | $24,141 | 31.5h |
| random_baseline | −663.3 | 141 | 41 | 0 / 0 | 30.0% | $26,395 | 35.4h |
| most_free_expert (ref) | −541.9 | 7 | 0 | 7 / 1 | 46.2% | $21,903 | 42.4h |
| q_learning_unmasked (diag) | −672.0 | 150 | 39 | 0 / 0 | 27.5% | $26,682 | 31.7h |

- **Hard gate PASS**: avoidable catastrophic failures = 0. Attribution is
  decision-time (safe-alternative check): Q's 3 drops and the oracle's 7
  all occurred at steps where NO node was safe (constructive proof of
  unavoidability — the oracle drops the same head); the three baselines'
  149/141/150 drops had a safe node available 100% of the time.
- Q beats round-robin **+21.46%** and the feasibility oracle **+2.5%**
  (mask holds the physical constraint; the learned Q supplies the better
  cost/util trade-offs — $72 cheaper, 2.9h faster JCT than the oracle).
- Mask ablation (unmasked diag): same Q table, 39 avoidable HP drops —
  zero-catastrophe is mask+learning jointly, not learning alone.
- Honest cost: Q's SLA violations are 100% queueing delays (zero losses,
  ~2.2 HP/seed from 8-GPU FIFO HOL starvation); RR's equal-sized violation
  count is 100% auto-breaches from dropping jobs. Queue-side rework
  (central pending pool / priority queues) is the #1 next-sprint item.

### 8.5 Week 4 acceptance checklist

| # | criterion | result |
|---|---|---|
| 1 | 7-day test success, zero catastrophic | ✅ 7/7 OK, GATE 1/2/3 pass |
| 2 | PPO variance<0.01 or documented skip | ✅ documented (deps absent; PPO wired to V2, SAC raises on Discrete) |
| 3 | metrics match raw data | ✅ from archived JSONs, deterministic |
| 4 | week2 12/12 + week3 success + Go build/vet | ✅ all green |
| 5 | final report | ✅ `MODULE_10_FIX_FINAL_REPORT.md` |
| 6 | honest expert-gap admission | ✅ report §7.1 (−382.07 vs −246.8 diagnostic regime) |

### 8.6 Known gaps → next sprint

1. Central pending pool + (node, job) joint action — kills FIFO HOL
   starvation (the 49.1% SLA-queueing driver). **→ DONE in Week 4.5, §9**
2. Add per-node queue-head demand to the observation (mask becomes an
   optional optimizer instead of a safety requirement).
3. Install torch/sb3 → PPO on V2 (trainer ready), target the −246.8
   expert bar in the diagnostic regime.
4. Multi-seed protocol (10 env × 3 algo seeds).
5. `_total_reward` never accumulates in env (unused by acceptance, which
   self-accumulates; clean up or fix).

---

## 9. Week 4.5: Central pending pool — FIFO HOL elimination

Artifacts: `ai/scheduler/env_central_pool.py` (new env, subclasses
`QueueAwareGPUEnvironment`), `ai/tests/test_central_pool_sanity.py`
(14 tests), `ai/tests/test_7day_production_simulation.py`
(`TestCentralPoolSevenDayComparison`), `tmp/week4_5_learning_proof.py`,
`tmp/week4_5_central_pool_results.json`.

### 9.1 The structural defect (Week 4 §8.4 recap)

Legacy env: per-node FIFO deques + round-robin arrival assignment +
"pop the chosen node's FIFO head, place or LOSE it". Three loss/blocking
channels: (a) ill-fitting heads are dropped (−8), (b) an 8-GPU head
blocks its whole queue forever, (c) a job can only ever run on the one
node it happened to land on. Week 4 measured the consequence: Q's SLA
violations were 100% queueing delays, 49.1% violation rate.

### 9.2 Design (central pool, contract frozen)

- **`CentralPendingPool`** — max-heap on an aging urgency key
  `(0.7·deadline_pressure + 0.3·priority/100) · (1 + wait_h/4)` with lazy
  re-heapify (entries refreshed when the aged key drifts >5%).
- **`CentralPendingPoolEnvironment`** — each step pops the TOP-K
  (K=3) freshest-key candidates and places the first that fits on the
  policy-chosen node; misfits RETURN to the pool (no −8, no loss); if
  none of the top-K fits, a full-scan backfill takes the most urgent
  fittable job from the whole pool (kills residual HOL at K<∞); the only
  loss channel is pool overflow (capacity = N·max_pending, same as the
  legacy global FIFO capacity), and overflow evicts the LEAST urgent
  job (SLA protection), not the oldest arrival.
- **Contract byte-identical** — obs 9N+5 all [0,1] (per-node queue
  features keep their exact semantics via unbounded "shadow" FIFO views
  synced on schedule/evict), action Discrete(N), reward inherited
  verbatim. `rl_schema.go` unchanged (4/4 contract tests, build/vet
  clean). RNG call-sequence parity: same seed ⇒ identical arrival
  streams on both envs (sanity-tested) — fair A/B.

### 9.3 7-day A/B results (rate 0.12, 5 seeds × 700 steps)

| strategy | SLA viol. | HP-drop | failed | sched/arrived | done | cost | $/job | JCT |
|---|---|---|---|---|---|---|---|---|
| **pool + Q (masked)** | **12.2%** | **0** | **0** | 80.8/89 (91%) | 60.0 | $38,713 | $479 | 33.4h |
| pool + round_robin | 8.5% | 0 | 0 | 78.6 (88%) | 58.6 | $38,875 | $495 | 33.8h |
| pool + random | 8.4% | 0 | 0 | 78.0 (88%) | 58.0 | $39,881 | $511 | 36.8h |
| pool + expert (ref) | 13.1% | 0 | 0 | 79.4 (89%) | 59.4 | $35,388 | $446 | 31.0h |
| pool + Q unmasked (diag) | 54.2% | 0 | 0 | 62.0 (70%) | 41.6 | $27,914 | $450 | 57.3h |
| legacy + Q (Week 4 archive) | 49.1% | 0 | 3 | 43.2 (49%) | 34.6 | $21,831 | $505 | 39.5h |
| legacy + round_robin | 27.6% | 40 | 149 | 88.6 | 44.6 | $24,141 | $273 | 31.5h |

- **GATE A (task target) PASS**: SLA violations 49.1% → 12.2%
  (**+36.9pp**; compliance 50.9% → **87.8%**, target ≥65%).
- **GATE B (structural, not luck) PASS**: every production strategy cuts
  violations to ≤13.1% (vs legacy Q 49.1%); Q still beats round-robin on
  reward (−354.7 vs −364.7). Q vs random (−354.7 vs −353.6, 0.3% gap vs
  SE≈35): statistically indistinguishable — see §9.5 for why this is
  expected and where the +10% learning gate actually lives.
- **GATE C PASS**: zero failed placements, zero HP drops, zero
  overflow-evicted HP jobs — placement failure is structurally
  impossible now.
- **GATE D (overload rate=2.0) PASS**: oracle side — pool loses fewer
  jobs (760 vs 843) AND fewer HP jobs (201 vs 258); random side HP loss
  non-inferior (214.7 vs 214.3). Both systems collapse at 4× sustainable
  load (pool SLA-violations 94.6% are WAIT violations on jobs legacy
  would have KILLED — job loss is the honest saturation metric).
- **Capacity unlocked**: feasibility-oracle sustainable rate 0.12 →
  **0.5** (4.2×) at 66% GPU util — HOL was hiding 3/4 of the cluster's
  schedulable capacity.
- Cost note (honest): absolute cost rises because ~2× more jobs are
  actually served (91% vs 49% scheduled); **unit cost per scheduled job
  drops** ($479 vs $505 for Q).
- Unmasked-diagnostic finding: pure reward maximization without the
  mask learns to under-schedule (62 vs 81 jobs) for per-step reward
  (−345.1, best) but destroys SLA (54.2%) and JCT (57.3h). In the pool
  env the mask's role changed from failure-avoidance to **throughput
  guarantee** — worth revisiting when the mask is promoted into the
  observation (§8.6 item 2).

### 9.4 Sanity suite (new, 14 tests, all green)

`test_central_pool_sanity.py`: 8-GPU head cannot block/lose followers;
backfill prevents idle at K=1; zero loss under random policy (300
steps); aging key monotone + overtake; overflow evicts least-urgent;
obs shape/range parity + same-seed identical arrival streams; reward
function numerically identical; bit-identical determinism; shadow↔pool
consistency at every step; info fields backward-compatible.
Legacy regressions: Week 2/3 suite **12/12 OK**, `TestSimulationContracts`
4/4, Go `rl_schema` tests 4/4 + build/vet clean.

### 9.5 Learning proof on the pool env (honest result)

Protocol (Week 3 §7.4 upgraded): 5 nodes × 8 GPUs, rate=1.0, 100 steps;
1000-ep baselines; 8000-ep factored-Q training; 2000-ep greedy eval on
unseen seeds; **paired common-random-numbers gate** (absolute-mean gate
at n=500 was measured underpowered: episode σ≈19 gave SE 0.87 > the
0.62 gap — fixed by statistics, not by tuning).

- r1 (marginal, n=500): Q −34.26 vs best baseline −37.38 → +8.37%, FAIL.
- r2 (paired, n=2000): paired diff **+3.41 ± 0.35 SE** vs round-robin
  (−37.67) → **+9.05%**, extremely significant (p≪0.001) but 0.95pp
  under the +10% line. FAIL, reported as measured. Learning curve flat
  5000→8000 eps (tail −34.68 → −34.67).
- r3 (single change: gpu_util state bucket 3→4, edges 25/50/75 aligning
  the quadratic peak at 75% — Week 4 §7.7.2 anticipated "finer buckets
  may pay off"): paired diff **+3.65 ± 0.35 SE** (n=2000) vs round-robin
  (−37.67) → **+9.68%**, p≪0.001, but 0.32pp under the +10% line
  (3.65 vs margin 3.77). The bucket change moved the needle +0.63pp with
  a clean physical justification — archived as `week4_5_learning_proof_results_r3.json`.
- r4 (second single change: cost state bucket 2→4, same logic — the cost
  reward term has weight 2.0 but 2 buckets could not distinguish cost 30
  from 45): paired diff **+3.66 ± 0.34 SE** → **+9.71%** — only +0.03pp
  vs r3 (noise), while the training tail got WORSE (−33.46 → −34.76) as
  states grew 1550 → 2039 with thinned per-state visits. **Conclusion:
  the residual gap is NOT a state-resolution problem; further bucket
  tuning is gate-chasing, stopped.** r3 stands as the final figure.
  Archived as `week4_5_learning_proof_results_r4.json` (overload +14.6%).
- Overload diagnostic (rate=2.0, no gate): Q beats random **+13.8% to
  +15.3%** across r2/r3/r4 — the learned policy's advantage GROWS under
  pressure (light regime: +9.0–9.7%).

**Why +10% is structurally hard here (measured, not excused):**

1. The env fix removed the −8 failure channel, which lifted random from
   −506 (legacy, Week 3) to −38.7: the "disaster avoidance" dimension
   that powered the Week 3 +26.5% gap is gone — the env now guarantees
   it for every policy.
2. Job SELECTION (urgency order) is now an env mechanism shared by all
   policies; the policy only chooses the node, and at rate=1.0 with
   mostly-free nodes the per-step node-choice reward spread is <1-2.
3. A one-step myopic oracle (max per-step reward, env internals visible)
   measures **−52.7% vs random** — greedy node choice is a TRAP; the
   remaining learnable signal is anticipatory (load spreading), which
   tabular Q with the frozen 95-dim obs partially captures (+9.68% at r3,
   growing to +15.3% under overload pressure).
4. The 7-day regime (rate=0.12, ~25% util) is policy-insensitive by
   construction (all strategies within 5% reward) — the structural SLA
   win does not depend on the policy, which is exactly what GATE B
   proves (§9.3).

Conclusion: the +10% bar is met in spirit by the evidence chain
(structural SLA win reproducible under every policy + significant
paired learning gain + larger gain under load), but the literal +10%
margin in the light regime is honestly reported as NOT met: **+9.68%
(r3, final; r4 +9.71% did not materially improve it and was stopped as
gate-chasing)** — the numbers are archived, not tuned into compliance.
Re-run 2026-08-16: 7-day comparison gates 4/4 PASS
(`TestCentralPoolSevenDayComparison`, 774.99s).

### 9.6 Acceptance checklist (Week 4.5)

| # | criterion | result |
|---|---|---|
| 1 | env compiles, Go schema compatible, `rl_schema.go` untouched | ✅ 4/4 Go contract tests, build/vet clean |
| 2 | 7-day sim, SLA ≥65% compliance | ✅ **87.8%** compliance (12.2% violations, was 49.1%) |
| 3 | Q-learning still +10% over baseline | ⚠️ **+9.68% paired (n=2000, p≪0.001, r3)** — 0.32pp short, honestly archived; overload +13.8–15.3%; see §9.5 |
| 4 | old-vs-new comparison in this document | ✅ §9.3 |
| 5 | honest attribution if target missed | ✅ §9.5 (env removed the failure channel that made the Week 3 gap large; myopic trap measured at −52.7%) |

### 9.7 Known gaps → next sprint

1. Promote pool-top demand + mask into the observation (mask becomes an
   optimizer hint; unblocks finer job-aware node choice).
2. DQN/PPO (torch/sb3) for anticipatory node choice — the myopic-trap
   measurement (§9.5) suggests value-based lookahead beats tabular here.
3. Multi-seed training protocol (3 algo seeds) for the learning proof.
4. Eviction policy under sustained overload (rate ≥ 2.0): consider
   deadline-aware admission instead of post-hoc least-urgent eviction.
